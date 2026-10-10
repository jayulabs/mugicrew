package session

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// ErrNoTmux is the refusal when tmux is off PATH: tmux is required, with no
// fallback to an unwrapped launch or a TUI outside tmux (ADR 0036 §6, ADR 0065).
var ErrNoTmux = errors.New("tmux not found on PATH (required for the TUI and HITL sessions, ADR 0036)")

// maxSock is the longest socket path we accept. sun_path is 108 bytes on Linux,
// 104 on macOS, both counting the NUL; take the smaller.
const maxSock = 103

// socket is the repo's mugicrew tmux server socket,
// <git-common-dir>/mugicrew/tmux.sock (ADR 0036 §2), next to events.jsonl. A var
// so the session tests run outside a repo.
var socket = func() (string, error) {
	common, err := git.CommonDir()
	if err != nil {
		return "", err
	}
	return socketIn(common)
}

// socketIn is socket under common: its dir is created; a path too long for a unix
// socket is a clear error, not tmux's.
func socketIn(common string) (string, error) {
	sock := filepath.Join(common, "mugicrew", "tmux.sock")
	if len(sock) > maxSock {
		return "", fmt.Errorf("tmux socket path %s is %d bytes, over the %d-byte unix socket limit (move the repo to a shorter path)", sock, len(sock), maxSock)
	}
	return sock, os.MkdirAll(filepath.Dir(sock), 0o755)
}

// tmuxName is b's tmux session name: the bead id with the . and : tmux rewrites
// already swapped for _ (ADR 0036 §3), so a target of "="+name matches exactly.
// @bead keeps the exact id.
// ponytail: not injective — mc-a.1 and mc-a_1 share a name (new-session refuses the
// second); bd's generated ids don't mix . and _ that way. Escape _ too if they ever do.
func tmuxName(beadID string) string { return strings.NewReplacer(".", "_", ":", "_").Replace(beadID) }

// tmuxCmd is `tmux -S sock args...` with TMUX and TMUX_PANE dropped from its
// environment, so it nests inside the user's own tmux instead of being refused
// (ADR 0042 §4).
func tmuxCmd(sock string, args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-S", sock}, args...)...)
	cmd.Env = noTMUX(os.Environ())
	return cmd
}

// noTMUX is a copy of env without TMUX and TMUX_PANE.
func noTMUX(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=")
	})
}

// envFileVar names the env handoff file a pane's hitl-run applies (FR5, ADR 0037
// note): the only -e a launch or resume passes.
const envFileVar = "MUGICREW_ENV_FILE"

// writeEnvFile writes the launcher's env, minus TMUX and TMUX_PANE, to a fresh
// 0600 file under $XDG_RUNTIME_DIR (else os.TempDir()) and returns its path. The
// tmux server would hand a pane the env it captured at its own start, and a tmux
// client command is too small to carry the env inline, so the
// pane's hitl-run reads it from here. KEY=VAL entries are NUL-separated, like
// /proc/self/environ, so a newline in a value survives.
// ponytail: a pane that dies before hitl-run applies the file (a broken exe)
// leaves it behind; $XDG_RUNTIME_DIR is per-user and cleared at logout. Sweep
// stale mugicrew-env-* files on Start if the os.TempDir() fallback matters.
func writeEnvFile() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "mugicrew-env-*") // CreateTemp makes it 0600
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(strings.Join(noTMUX(os.Environ()), "\x00"))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// envArgs is the -e that hands a pane its env file.
func envArgs(envFile string) []string { return []string{"-e", escSep(envFileVar + "=" + envFile)} }

// preamble is the server-wide setup (tmux argv after -S) that both a HITL Start
// and the TUI self-launch run first, since either may be the one to start the
// server (ADR 0065 §4): remain-on-exit failed (ADR 0037 §2), and F12 and F11 in
// the HITL key table only, so neither does anything on a TUI session (ADR 0065
// §5). F11 cancels an Auto-exit by unsetting the pressing client's session's
// @autofinish (Spec D5): no TUI, no subprocess, a no-op when unarmed.
func preamble() []string {
	return []string{
		"start-server", ";",
		"set-option", "-gw", "remain-on-exit", "failed", ";",
		"bind-key", "-T", hitlTable, "F12", "if-shell", "-F", ownTUI,
		"run-shell -C 'switch-client -t =" + ownTUI + "'", "detach-client", ";",
		"bind-key", "-T", hitlTable, "F11", "set-option", "-u", "@autofinish",
	}
}

// hitlTable is the key table every HITL session uses: it binds only F12 and F11,
// so other keys reach the pane and the prefix still works.
const hitlTable = "mchitl"

// ttyOption is the user option a TUI session carries naming its own client's tty
// (RecordClient): the client → TUI map F12 reads.
const ttyOption = "@tty"

// ownTUI is a format naming the TUI session whose @tty is the pressing client's
// tty, "" if none — no TUI for this terminal, or it is gone, so F12 detaches.
// All tmux-side: F12 works with the TUI hung. run-shell -C, since switch-client
// doesn't expand its -t.
const ownTUI = "#{S:#{?#{==:#{" + ttyOption + "},#{client_tty}},#{session_name},}}"

// launchArgs is the pure tmux argv (after -S) that starts one bead's detached
// session: the preamble first so a pane that dies at once is already kept, then
// new-session running the wrapper with envFile's env, then its tags.
// new-session refuses a duplicate name (ADR 0036 §3). The tags ride the same ;-joined command so the session is
// never listed untagged: an untagged live session's tree would read as an orphan.
func launchArgs(sess, cwd, envFile string, pane []string, t sessionTags) []string {
	args := append(preamble(), ";", "new-session", "-d", "-s", sess, "-c", literal(cwd))
	args = append(append(args, envArgs(envFile)...), "--")
	return append(append(append(appendPane(args, pane), ";"), tagArgs(sess, t)...), unmarkArgs(sess)...)
}

// sessionTags is what a session carries: its bead, the agent's session id and its
// worktree ("" = repo root) — the @ tags, and the attrs hitl-run records.
type sessionTags struct {
	bead              bd.Bead
	session, worktree string
}

// tagArgs tags the started session with its @ options (ADR 0036 §1) and its bar.
func tagArgs(sess string, t sessionTags) []string {
	return setArgs(sess, append([][2]string{
		{"@bead", t.bead.ID}, {"@session", t.session}, {"@worktree", t.worktree},
	}, bar(t.bead)...))
}

// bar is a HITL session's status line (ADR 0066 §3) and key table (ADR 0065 §5): b
// on the left, both ways out on the right, no window list, no prefix keys. A dead
// pane keeps it.
func bar(b bd.Bead) [][2]string {
	return [][2]string{
		{"status-left", literal(b.ID + " · " + b.Title)}, {"status-left-length", "200"},
		{"status-right", rightBar(b.ID, false)}, {"key-table", hitlTable},
		{"window-status-format", ""}, {"window-status-current-format", ""},
	}
}

// rightBar is a HITL session's status-right: both ways out, flipped loud (green,
// reversed) once its bead is closed (ADR 0066 §4).
func rightBar(id string, closed bool) string {
	if closed {
		return "#[fg=green,reverse] ✓ " + literal(id) + " closed — /exit to finish · F12 back to TUI "
	}
	return "F12 back to TUI · /exit to finish"
}

// autoExitBar is an armed HITL session's status-right, left the time to its
// Auto-exit deadline, in M:SS with a part second rounded up (Spec D4).
func autoExitBar(left time.Duration) string {
	s := int((max(left, 0) + time.Second - 1) / time.Second)
	return fmt.Sprintf("auto-exit in %d:%02d · F11 cancels", s/60, s%60)
}

// SetBars sets each alive HITL session's status-right from its bead's closed-ness
// (ADR 0066 §4), keyed by bead id: the TUI derives it every tick, so a reopen
// reverts. A session listed armed is skipped: its bar is hitl-run's countdown
// (Spec D4). One tmux call per tick; several TUIs writing the same value is harmless.
// ponytail: a session gone between the list and the set fails the call and stops the
// rest of its ;-chain for that tick; the next tick re-derives.
func SetBars(closed map[string]bool, sessions map[string]Info) error {
	args := barsArgs(closed, sessions)
	if len(args) == 0 {
		return nil
	}
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	return runTmux(sock, args)
}

// barsArgs is SetBars' ;-joined set-option argv, in bead-id order, skipping each
// session sessions lists armed; nil if none is left.
func barsArgs(closed map[string]bool, sessions map[string]Info) []string {
	var args []string
	for _, id := range slices.Sorted(maps.Keys(closed)) {
		if !sessions[id].AutoExit.IsZero() {
			continue
		}
		right := [][2]string{{"status-right", rightBar(id, closed[id])}}
		args = append(append(args, setArgs(tmuxName(id), right)...), ";")
	}
	if len(args) == 0 {
		return nil
	}
	return args[:len(args)-1]
}

// setArgs is the ;-joined set-option argv setting each name/value on session sess.
func setArgs(sess string, opts [][2]string) []string {
	target := exact(sess) + ":" // set-option -t takes a pane target; the : makes it a session
	var args []string
	for _, kv := range opts {
		args = append(args, "set-option", "-t", target, kv[0], escSep(kv[1]), ";")
	}
	return args[:len(args)-1]
}

// literal escapes s for an argument tmux expands as a format (status-left, -c):
// a literal # must be ##.
func literal(s string) string { return strings.ReplaceAll(s, "#", "##") }

// exact is the -t target that matches session sess by its exact name, not as a
// prefix or pattern.
func exact(sess string) string { return "=" + sess }

// appendPane appends the pane argv to a tmux argv, each word escSep'd.
func appendPane(args, pane []string) []string {
	for _, a := range pane {
		args = append(args, escSep(a))
	}
	return args
}

// escSep keeps a trailing ; in an argument literal: tmux reads any argv word
// ending in ; as a command separator unless it ends in \; instead.
func escSep(s string) string {
	if strings.HasSuffix(s, ";") {
		return s[:len(s)-1] + `\;`
	}
	return s
}

// paneArgs is the pane's command: the hidden hitl-run wrapper (ADR 0037 §1) by
// its absolute path, carrying the event attrs it records (ADR 0029 amendment),
// then -- and the agent argv.
func paneArgs(exe string, t sessionTags, argv []string) []string {
	out := []string{exe, "hitl-run"}
	attrs := eventlog.BeadAttrs(t.bead)
	for i := 0; i+1 < len(attrs); i += 2 {
		out = append(out, fmt.Sprintf("--%v=%v", attrs[i], attrs[i+1]))
	}
	out = append(out, "--session="+t.session, "--worktree="+t.worktree, "--")
	return append(out, argv...)
}

// lookTmux is exec.LookPath("tmux"), a var so the no-tmux refusal is testable.
var lookTmux = func() error { _, err := exec.LookPath("tmux"); return err }

// serverSocket is the preamble of every call on the mugicrew tmux server: ErrNoTmux when
// tmux is off PATH (ADR 0036 §6), else the server's socket.
func serverSocket() (string, error) {
	if lookTmux() != nil {
		return "", ErrNoTmux
	}
	return socket()
}

// Start launches b's agent command as a detached session on the repo's mugicrew
// tmux server (ADR 0036, ADR 0039 §4): it places the session (fresh worktree or
// repo root, the busy interlock surfacing as git.Busy), deletes b's leftover
// Drafts, then has tmux run the hitl-run wrapper there. The wrapper owns the
// session's end — events and the clean-exit teardown — so Start's only teardown
// is undoing a placement tmux never took. Attach with SwitchTo.
func Start(b bd.Bead, ac contract.AgentCommand) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	env, err := writeEnvFile()
	if err != nil {
		return err
	}
	cwd, wt, teardown, err := placeTree(b)
	if err != nil {
		os.Remove(env)
		return err
	}
	// A fresh launch means no session: gone reads as never launched (ADR 0038 §1),
	// so any Drafts are a dead session's, and the new one's idempotency guards must
	// not adopt them (ADR 0059 §6) — a repo-root leaf is never an orphan to drop.
	if err := DropDrafts(b); err != nil {
		os.Remove(env)
		teardown()
		return err
	}
	sess, t := tmuxName(b.ID), sessionTags{b, ac.SessionID, wt}
	// ponytail: tmux stops the sequence at the first failing command, so an error is
	// new-session's (no pane took the placement); the tags are fixed options on the
	// session it just made and don't fail. If they ever can, check for an untagged
	// =sess before teardown — not kill-session, which would hit a duplicate's live pane.
	if err := runTmux(sock, launchArgs(sess, cwd, env, paneArgs(exe, t, ac.Argv), t)); err != nil {
		os.Remove(env) // no pane took it
		teardown()
		return err
	}
	return nil
}

// respawnArgs is the pure tmux argv (after -S) that reruns an exited session's
// dead pane in its kept worktree. No -k: tmux refuses to respawn a live pane, so a
// stale "exited" read can't kill a running agent. The session and its @ tags stay;
// the bar is set afresh for b, so a session launched by an older mugicrew gets it,
// and the waiting bit and Closing mark are unset.
// The pane gets envFile's env, as at launch.
func respawnArgs(sess, cwd, envFile string, pane []string, b bd.Bead) []string {
	args := append([]string{"respawn-pane", "-t", exact(sess) + ":", "-c", literal(cwd)}, envArgs(envFile)...)
	return append(append(append(appendPane(append(args, "--"), pane), ";"), setArgs(sess, bar(b))...), unmarkArgs(sess)...)
}

// unmarkArgs is the ;-led set-option argv unsetting session sess's waiting bit,
// Closing mark and Auto-exit deadline, so a fresh or resumed pane starts not
// waiting (ADR 0099 §6) and disarmed (ADR 0111), and a resume after a failed
// close doesn't read Closing (ADR 0108 §2).
func unmarkArgs(sess string) []string {
	return []string{";", "set-option", "-u", "-t", exact(sess) + ":", "@waiting",
		";", "set-option", "-u", "-t", exact(sess) + ":", "@closing",
		";", "set-option", "-u", "-t", exact(sess) + ":", "@autofinish"}
}

// resumePane is a paneArgs wrapper command started in resume mode, so hitl-run
// records hitl.resumed instead of hitl.launched (ADR 0029 amendment).
func resumePane(pane []string) []string {
	return slices.Insert(pane, slices.Index(pane, "hitl-run")+1, "--resume")
}

// Resume reruns b's exited session s (its @session / @worktree, as Sessions read them)
// in the kept worktree through the hitl-run wrapper, so a later clean exit tears
// that worktree down as at launch (ADR 0037 §4, ADR 0039 §1, §4). The agent argv
// is h's ResumeCommand; its error (a pruned transcript) is Resume's, with nothing
// spawned. A kept worktree that is gone is an error, nothing spawned: tmux
// would fall back to $HOME. A repo-root leaf ("" worktree) resumes from the repo
// root. Attach with SwitchTo.
func Resume(b bd.Bead, s Info, h contract.Harness) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	ac, err := h.ResumeCommand(s.Session)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd := s.Worktree
	if cwd != "" {
		if _, err := os.Stat(cwd); err != nil {
			return fmt.Errorf("worktree %s is gone: %w", cwd, err)
		}
	} else {
		if cwd, err = git.RepoRoot(); err != nil {
			return err
		}
	}
	pane := resumePane(paneArgs(exe, sessionTags{b, ac.SessionID, s.Worktree}, ac.Argv))
	env, err := writeEnvFile()
	if err != nil {
		return err
	}
	if err := runTmux(sock, respawnArgs(tmuxName(b.ID), cwd, env, pane, b)); err != nil {
		os.Remove(env) // no pane took it
		return err
	}
	return nil
}

// deleteDrafts is bd.DeleteDrafts, a var so Start and Drop are testable without
// a live bd.
var deleteDrafts = bd.DeleteDrafts

// DropDrafts deletes the Drafts of b's dropped or gone session (ADR 0059 §6),
// so a relaunch's idempotency guards don't adopt the dead session's work,
// recording draft.deleted with b's common attrs and the ids, or draft.failed,
// then each deleted container Draft's branch outcome.
// None records nothing.
func DropDrafts(b bd.Bead) error {
	ids, branches, err := deleteDrafts(b.ID)
	err = eventlog.RecordDrafts("draft.deleted", b.ID, ids, err, eventlog.BeadAttrs(b)...)
	eventlog.RecordBranches(branches, eventlog.BeadAttrs(b)...)
	return err
}

// killSession runs a kill-session argv on the server, a var so Drop's order is
// testable without tmux.
var killSession = runTmux

// Drop removes b's session s, alive or exited (ADR 0037 §5): kill-session on
// the mugicrew server first, so no agent is left writing in the tree, then a forced
// remove of its @worktree and a delete of the bead's Drafts (ADR 0059 §6).
// Uncommitted changes are discarded; committed work is safe on the branch, which
// is free again. An effort/<id> with nothing ahead of its container goes too, so
// it never holds the container's drain (Spec FR 53). A failed kill
// removes nothing. A repo-root leaf ("" worktree) has no tree to remove.
func Drop(b bd.Bead, s Info) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	if err := killSession(sock, []string{"kill-session", "-t", exact(tmuxName(b.ID))}); err != nil {
		return err
	}
	err = DropDrafts(b) // both run: the session is gone, so a retried drop can't kill again
	if s.Worktree == "" {
		return err
	}
	// ponytail: kill-session only sends the hangup, it doesn't wait for the pane to
	// die; an agent that exits 0 on it has hitl-run remove the same tree, which
	// removeTree takes as done. Poll pane death first if a slow exit strands trees.
	if rerr := removeTree(s.Worktree); rerr != nil {
		return errors.Join(err, rerr)
	}
	return errors.Join(err, dropEmptyEffort(b))
}

// dropEmptyEffort deletes b's effort/<id> when it has no commits ahead of its
// container. A bead on no effort branch, or one already gone, is left alone. So is
// a replace-mode one: its drop-only rewrite reads as 0 ahead, yet it is a rewrite
// to publish (git.IntegrateReplace).
// ponytail: a replace effort's untouched branch is kept too; only AFK kinds route
// Replace, so Drop never reaches one today. Compare against d.Tip if one does.
func dropEmptyEffort(b bd.Bead) error {
	d, ok, _ := route.OnEffort(b)
	if !ok || d.Mode == route.Replace {
		return nil
	}
	if n, err := git.CommitsAhead(d.Container, d.Branch); err != nil || n > 0 {
		return nil // ponytail: a missing branch errors too; nothing to delete either way
	}
	return deleteBranch(d.Branch)
}

// HolderBead is the bead whose session (alive or exited) has wt as @worktree, ""
// if none: the holder a busy status names (ADR 0037 §7).
// ponytail: an exact path match, sound on Linux where @worktree and git's porcelain
// both carry NewWorktree's path; compare EvalSymlinks'd paths if TMPDIR is a symlink.
func HolderBead(wt string, sessions map[string]Info) string {
	if wt == "" {
		return ""
	}
	for id, s := range sessions {
		if s.Worktree == wt {
			return id
		}
	}
	return ""
}

// Orphan reports whether wt, the worktree holding a bead's branch, is orphaned by
// a crashed session (ADR 0041 §2): a mugicrew-wt-* tree that no session in
// sessions (Sessions' table, empty with no server running) has as @worktree.
func Orphan(wt string, sessions map[string]Info) bool {
	return git.IsTempWorktree(wt) && HolderBead(wt, sessions) == ""
}

// pruneWorktrees is git.PruneWorktrees, a var so DropOrphan's order is testable.
var pruneWorktrees = git.PruneWorktrees

// DropOrphan frees the branch an orphaned worktree wt holds (ADR 0041 §3): a
// forced remove with its mugicrew-wt-* temp parent, discarding its uncommitted
// edits, then a prune, which also forgets a tree whose directory is already gone.
// The relaunch that follows deletes the dead session's Drafts (Start).
// Only a human's y on the orphan confirm calls it (ADR 0041 §4).
func DropOrphan(wt string) error {
	if err := removeTree(wt); err != nil {
		return err
	}
	return pruneWorktrees()
}

func runTmux(sock string, args []string) error {
	if out, err := tmuxCmd(sock, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Inside reports whether this process runs on the repo's private tmux server, so a
// bare mugicrew runs the TUI in place instead of self-launching (ADR 0065 §1).
func Inside() (bool, error) {
	sock, err := socket()
	if err != nil {
		return false, err
	}
	return insideOf(os.Getenv("TMUX"), sock), nil
}

// insideOf reports whether $TMUX (tmuxEnv) names sock: its first comma field is
// the socket path exactly, no symlink resolution — tmux keeps the -S path as given.
func insideOf(tmuxEnv, sock string) bool {
	first, _, _ := strings.Cut(tmuxEnv, ",")
	return first == sock
}

// ApplyTUIEnv applies the self-launch's env handoff in the TUI pane, keeping the
// pane's own TERM, TMUX and TMUX_PANE; a mugicrew run in a pane by hand has none.
func ApplyTUIEnv() error { return applyEnvFile("TERM", "TMUX", "TMUX_PANE") }

// tuiArgs is the pure tmux argv (after -S) that starts a TUI session (ADR 0065
// §3): the preamble, new-session running pane in cwd, then status off and the
// window's remain-on-exit off, so a TUI crash ends the session instead of
// leaving a dead pane. No @bead (never listed) and no key-table (root). The
// options ride the same command, before tmux reaps a fast-dying pane.
func tuiArgs(sess, cwd string, pane []string) []string {
	args := appendPane(append(preamble(), ";", "new-session", "-d", "-s", sess, "-c", literal(cwd), "--"), pane)
	target := exact(sess) + ":"
	return append(args, ";", "set-option", "-t", target, "status", "off",
		";", "set-option", "-w", "-t", target, "remain-on-exit", "off")
}

// tuiPane is the TUI session's command: exe itself, given args, with stderr
// appended to out and the env handoff named for that one process only — not by
// -e, which would stick to the session for any later pane.
func tuiPane(exe, out, envFile string, args ...string) []string {
	return append([]string{"sh", "-c", `o=$1 f=$2; shift 2; MUGICREW_ENV_FILE="$f" exec "$0" "$@" 2>>"$o"`, exe, out, envFile}, args...)
}

// LaunchTUI is the launcher a bare mugicrew outside the private server runs
// (ADR 0065 §1–4): it starts its own TUI session running mugicrew, attaches this
// terminal to it as a child, and kills the session when the client exits for any
// reason (q, <prefix> d, a closed terminal, a crash), so no TUI outlives it. The
// pane's mugicrew runs with args (a TUI's --watch).
func LaunchTUI(args ...string) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return launchTUI(sock, cwd, exe, args...)
}

// launchTUI is LaunchTUI on sock with exe as the pane's program.
// ponytail: tui-<pid> is unique per live launcher; it collides with a bead's
// session only for a bead id literally tui-<n> (bd ids carry the repo prefix),
// where new-session refuses it.
// ponytail: one tui.out per repo, so a second terminal's launch truncates the
// first TUI's stderr (its writes still land, appended); name it tui-<pid>.out if
// crashes in concurrent TUIs get lost.
func launchTUI(sock, cwd, exe string, args ...string) error {
	out := filepath.Join(filepath.Dir(sock), "tui.out")
	if err := os.WriteFile(out, nil, 0o644); err != nil { // truncated per launch; the pane appends
		return err
	}
	env, err := writeEnvFile()
	if err != nil {
		return err
	}
	sess := fmt.Sprintf("tui-%d", os.Getpid())
	if err := runTmux(sock, tuiArgs(sess, cwd, tuiPane(exe, out, env, args...))); err != nil {
		os.Remove(env) // no pane took it
		return err
	}
	// A closed terminal hangs us up with the client: stay alive to kill the session.
	// A TERM kills it at once, which ends the client. Notify, not Ignore: an
	// ignored disposition would be inherited by the client.
	kill := []string{"kill-session", "-t", exact(sess)}
	signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	go func() { <-term; runTmux(sock, kill) }()
	attach := tmuxCmd(sock, "attach-session", "-t", exact(sess))
	attach.Stdin, attach.Stdout, attach.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = attach.Run()
	runTmux(sock, kill) // already gone after q or a crash
	return err
}

// SwitchTo moves this TUI's own client to beadID's session with a switch-client
// (ADR 0065 §1): the TUI keeps running on its own session while the human is
// away. Run from the TUI pane on the private server, so $TMUX_PANE names it.
func SwitchTo(beadID string) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	return switchClient(sock, os.Getenv("TMUX_PANE"), tmuxName(beadID))
}

// switchClient switches the client viewing pane's session to sess. The client is
// named by its tty with -c, resolved from pane, never left to tmux's implicit
// current-client lookup.
func switchClient(sock, pane, sess string) error {
	tty, err := ownClient(sock, pane)
	if err != nil {
		return err
	}
	if tty == "" {
		return errors.New("no client is viewing this TUI")
	}
	return runTmux(sock, switchArgs(tty, sess))
}

// Prefix is the private server's prefix key in tmux notation (e.g. C-b): the
// user's ~/.tmux.conf loads there, so it may not be tmux's default (ADR 0066 §2).
func Prefix() (string, error) {
	sock, err := serverSocket()
	if err != nil {
		return "", err
	}
	out, err := tmuxCmd(sock, "show", "-gv", "prefix").Output()
	if err != nil {
		return "", fmt.Errorf("tmux show prefix: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RecordClient records this TUI's own client tty as its session's @tty, the
// client → TUI map F12 reads (ADR 0065 §5). The TUI runs it at startup and each
// tick: a client's tty is stable for its attach. With no client on the TUI (not
// yet attached, or away in a HITL session) it writes nothing.
func RecordClient() error {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return nil // not in a pane: no TUI session to map
	}
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	return recordClient(sock, pane)
}

// recordClient is RecordClient on sock for the TUI in pane.
func recordClient(sock, pane string) error {
	out, err := tmuxCmd(sock, "list-clients", "-t", pane, "-F", "#{client_tty}\t#{session_name}").Output()
	if err != nil {
		return fmt.Errorf("tmux list-clients: %w", err)
	}
	tty, own, _ := strings.Cut(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0], "\t")
	if tty == "" {
		return nil
	}
	out, err = tmuxCmd(sock, "list-sessions", "-F", "#{session_name}\t#{"+ttyOption+"}").Output()
	if err != nil {
		return fmt.Errorf("tmux list-sessions: %w", err)
	}
	if args := recordArgs(tty, own, string(out)); args != nil {
		return runTmux(sock, args)
	}
	return nil
}

// recordArgs is the tmux argv (after -S) making own the only session whose @tty is
// tty, given list-sessions' name<TAB>@tty rows; nil if it already is. A terminal
// that moved to another TUI with tmux keys would otherwise leave two holders, and
// ownTUI would join their names into a target that matches neither.
// ponytail: an orphaned TUI session (its launcher SIGKILLed) keeps its @tty, so a
// later hand attach that reuses the pty number goes there instead of detaching.
func recordArgs(tty, own, sessions string) []string {
	var args []string
	held := false
	for _, line := range strings.Split(sessions, "\n") {
		name, t, _ := strings.Cut(line, "\t")
		switch {
		case t != tty:
		case name == own:
			held = true
		default:
			args = append(args, "set-option", "-u", "-t", exact(name)+":", ttyOption, ";")
		}
	}
	if held && args == nil {
		return nil
	}
	return append(args, "set-option", "-t", exact(own)+":", ttyOption, tty)
}

// returnClients switches each client viewing pane's session to the TUI session
// whose @tty is that client's tty (the F12 lookup, ADR 0065 §5), leaving clients
// with no TUI where they are.
func returnClients(sock, pane string) error {
	ttys, err := tmuxCmd(sock, clientArgs(pane)...).Output()
	if err != nil {
		return fmt.Errorf("tmux list-clients: %w", err)
	}
	sessions, err := tmuxCmd(sock, "list-sessions", "-F", "#{session_name}\t#{"+ttyOption+"}").Output()
	if err != nil {
		return fmt.Errorf("tmux list-sessions: %w", err)
	}
	if args := returnArgs(string(ttys), string(sessions)); args != nil {
		return runTmux(sock, args)
	}
	return nil
}

// returnArgs is the tmux argv (after -S) switching each client tty (one per line)
// to the session list-sessions' name<TAB>@tty rows map it to; nil if none has one.
func returnArgs(ttys, sessions string) []string {
	own := map[string]string{}
	for _, line := range strings.Split(sessions, "\n") {
		if name, tty, _ := strings.Cut(line, "\t"); tty != "" {
			own[tty] = name
		}
	}
	var args []string
	for _, tty := range strings.Fields(ttys) {
		if sess, ok := own[tty]; ok {
			args = append(append(args, switchArgs(tty, sess)...), ";")
		}
	}
	if args == nil {
		return nil
	}
	return args[:len(args)-1]
}

// ownClient is the tty of the client viewing pane's session, "" if none.
func ownClient(sock, pane string) (string, error) {
	if pane == "" {
		return "", errors.New("not in a tmux pane (TMUX_PANE unset)")
	}
	out, err := tmuxCmd(sock, clientArgs(pane)...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return "", fmt.Errorf("tmux: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	} else if err != nil {
		return "", fmt.Errorf("tmux: %w", err)
	}
	tty, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return tty, nil
}

// clientArgs is the tmux argv (after -S) printing the ttys of the clients viewing
// pane's session. Not display-message: with no client on the session it falls
// back to another session's client, which the switch would then steal.
// ponytail: a hand-attached second client on a TUI session is a mirror; the first
// listed is moved. Record each TUI's own tty at startup if mirrors ever matter.
func clientArgs(pane string) []string {
	return []string{"list-clients", "-t", pane, "-F", "#{client_tty}"}
}

// switchArgs is the tmux argv (after -S) moving the client on tty to sess exactly.
func switchArgs(tty, sess string) []string {
	return []string{"switch-client", "-c", tty, "-t", exact(sess)}
}

// State is a listed session's pane state. Its zero value is None: a bead with no
// Info has no session (never launched, or gone — indistinguishable, ADR 0038 §1),
// so sessions[id].State reads None for a missing id.
type State string

const (
	None   State = ""       // no session
	Alive  State = "alive"  // a live pane
	Exited State = "exited" // a dead pane remain-on-exit kept
)

// Info is one listed session (ADR 0036 §1): its @ tags and pane state. An exited
// pane carries its exit status RC, or the Signal that killed it, named as
// hitl.failed's signal attr is ("killed").
type Info struct {
	Session, Worktree string
	State             State
	RC                int
	Signal            string
	Waiting           Waiting
	Closing           bool      // a live pane whose hitl-run close is running (ADR 0108 §2)
	AutoExit          time.Time // its armed Auto-exit deadline, @autofinish (ADR 0111); zero is disarmed
}

// Waiting is a session's waiting bit, its @waiting option (ADR 0099 §3): what the
// agent waits on the human for. The TUI judges done and exited above it.
type Waiting string

const (
	NotWaiting Waiting = ""     // unset, or a value this mugicrew doesn't know
	WaitTurn   Waiting = "turn" // the agent ended its turn
	WaitAsk    Waiting = "ask"  // a permission prompt or question is up
)

// Has reports whether i is a listed session — the one spelling of "has a
// session?": sessions[id].Has() is false for a missing id.
func (i Info) Has() bool { return i.State != None }

// Live counts the alive sessions in a session table.
func Live(sessions map[string]Info) int {
	n := 0
	for _, s := range sessions {
		if s.State == Alive {
			n++
		}
	}
	return n
}

// listFormat is the list-sessions -F row parseList reads, tab-separated.
const listFormat = "#{@bead}\t#{@session}\t#{@worktree}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_dead_signal}\t#{@waiting}\t#{@closing}\t#{@autofinish}"

// Sessions reads the repo's mugicrew tmux server's session table, keyed by bead id
// — the only liveness source (ADR 0036 §1, §4–5). No tmux, or a server that isn't
// running (never started, killed, or a stale socket after a reboot), is an empty
// table; any other failure is an error. Orphan classification needs the difference
// — a failed read must not make a live session's tree an orphan.
func Sessions() (map[string]Info, error) {
	sock, err := serverSocket()
	if errors.Is(err, ErrNoTmux) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd := tmuxCmd(sock, "list-sessions", "-F", listFormat)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if noServer(stderr.String()) {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseList(string(out)), nil
}

// noServer reports whether tmux's stderr says no server is listening on the socket:
// "no server running on <sock>" (ECONNREFUSED: a stale socket file) or "error
// connecting to <sock> (No such file or directory)" (ENOENT: no socket yet). Any
// other "error connecting to" errno (EACCES, ENOTSOCK, ...) means a server may exist
// but can't be read, so it is not no-server.
func noServer(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "error connecting to") && strings.Contains(s, "(no such file or directory)")
}

// parseList is Sessions' pure half over list-sessions -F listFormat output. A row
// with no @bead (not a session Start launched) is skipped.
func parseList(out string) map[string]Info {
	m := map[string]Info{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 9 || f[0] == "" {
			continue
		}
		s := Info{Session: f[1], Worktree: f[2], State: Alive}
		if f[3] == "1" {
			s.State = Exited
		}
		s.RC, _ = strconv.Atoi(f[4])
		if n, err := strconv.Atoi(f[5]); err == nil && n > 0 { // a signal death has no status
			s.Signal = syscall.Signal(n).String()
		} else if s.State == Exited && s.RC > 128 && s.RC <= 128+64 {
			// hitl-run's 128+n for an agent killed by n (1–64 on Linux). ponytail: an agent
			// that itself exits 129–192 reads as a signal, the shell's own ambiguity.
			s.Signal = syscall.Signal(s.RC - 128).String()
		}
		if w := Waiting(f[6]); w == WaitTurn || w == WaitAsk {
			s.Waiting = w
		}
		s.Closing = s.State == Alive && f[7] != "" // a dead pane is exited, whatever @closing says
		if n, err := strconv.ParseInt(f[8], 10, 64); err == nil {
			s.AutoExit = time.Unix(n, 0)
		}
		m[f[0]] = s
	}
	return m
}
