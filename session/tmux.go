package session

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
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
// /proc/self/environ, so a newline in a value survives. extra (an AgentCommand's
// profile Env) follows the launcher's env, so applying the file in order lets it
// win (ADR 0125): a credential rides here, never in argv.
// ponytail: a pane that dies before hitl-run applies the file (a broken exe)
// leaves it behind; $XDG_RUNTIME_DIR is per-user and cleared at logout. Sweep
// stale mugicrew-env-* files on Start if the os.TempDir() fallback matters.
func writeEnvFile(extra map[string]string) (string, error) {
	env := noTMUX(os.Environ())
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return writeHandoff("mugicrew-env-*", strings.Join(env, "\x00"))
}

// writeHandoff writes data to a fresh 0600 temp file named by pattern and returns its path.
func writeHandoff(pattern, data string) (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, pattern) // CreateTemp makes it 0600
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// writeArgvFile writes the agent argv, NUL-separated, to a fresh 0600 file under
// $XDG_RUNTIME_DIR (else os.TempDir()) and returns its path: the pane's hitl-run
// reads it (--argv-file), so the prompt rides no tmux client command, whose size
// tmux caps near 16 KiB, as the env rides writeEnvFile's.
// ponytail: same stale-file ceiling as writeEnvFile.
func writeArgvFile(argv []string) (string, error) {
	return writeHandoff("mugicrew-argv-*", strings.Join(argv, "\x00"))
}

func removeFiles(paths ...string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// preamble is the server-wide setup (tmux argv after -S) that both a HITL Start
// and the TUI self-launch run first, since either may be the one to start the
// server (ADR 0065 §4): remain-on-exit failed (ADR 0037 §2), and F12, F11 and F10
// in the HITL key table only, so none does anything on a TUI session (ADR 0065
// §5). F11 cancels an Auto-exit by unsetting the pressing client's session's
// @autofinish (Spec D5): no TUI, no subprocess, a no-op when unarmed. F10 shows
// the session's @bead in a side pane, or focuses the one already open.
//
// A session's key-table is its default table and tmux never falls back to root
// from it, so every mouse event in a HITL session would be dropped. The run-shell
// after start-server (so ~/.tmux.conf has loaded) copies root's mouse bindings
// (key name, not command text, has Mouse, Wheel or Click) into the HITL table,
// before the F-key binds so none can overwrite F10-F12. run-shell sets $TMUX, so
// tmux there is this server, not the human's outer one. Re-binding replaces, so
// every Start is safe; root rebinds on a running server land at the next Start.
func preamble() []string {
	return []string{
		"start-server", ";",
		"set-option", "-gw", "remain-on-exit", "failed", ";",
		"run-shell", copyMouse, ";",
		"bind-key", "-T", hitlTable, "F12", "if-shell", "-F", ownTUI,
		"run-shell -C 'switch-client -t =" + ownTUI + "'", "detach-client", ";",
		"bind-key", "-T", hitlTable, "F11", "set-option", "-u", "@autofinish", ";",
		"bind-key", "-T", hitlTable, "F10", "if-shell", "-F", beadPane,
		"run-shell -C 'select-pane -t " + beadPane + "'", beadSplit(beadExe()),
	}
}

// copyMouse lists root's bindings, keeps the mouse-keyed ones retargeted at the
// HITL table, and sources them back into the same server.
const copyMouse = `tmux list-keys -T root | sed -n -E 's/^(bind-key +(-r +)?)-T root +([^ ]*(Mouse|Wheel|Click)[^ ]* .*)$/\1-T ` + hitlTable + ` \3/p' | tmux source-file -`

// hitlTable is the key table every HITL session uses: it binds only F12, F11 and
// F10, so other keys reach the pane and the prefix still works.
const hitlTable = "mchitl"

// beadPane is a format naming the current window's bead pane (the one marked
// @beadpane), "" if none: F10 focuses it rather than split another. All tmux-side.
const beadPane = "#{P:#{?#{@beadpane},#{pane_id},}}"

// beadExe names the mugicrew executable the bead pane runs; a var so a test can
// point it at a stub. The binding is server-wide, so it is an absolute path, not PATH.
var beadExe = func() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "mugicrew"
}

// beadSplit opens the bead pane: the hidden bead-detail subcommand for the pressing
// session's @bead, at the pane's own width, in less -R, 40% wide on the right, from
// the active pane's path (the session's path when the pane is dead and expands it to
// "") so it sees the same Workspace; q ends less and so the pane. Stderr rides the
// pipe, so a failed render stays readable. split-window leaves the command
// unexpanded, so the pane reads @bead and its width itself (tmux sets $TMUX and
// $TMUX_PANE in it); the id reaches the subcommand as a quoted command-substitution
// result and is never spliced into the command text. exe is shell-quoted, then
// escaped for the tmux double quotes around the command.
func beadSplit(exe string) string {
	sh := "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	sh += ` bead-detail "$(tmux display-message -p '#{@bead}')" "$(tmux display-message -p '#{pane_width}')" 2>&1 | less -R`
	sh = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(sh)
	return `split-window -h -l 40% -c '#{?pane_current_path,#{pane_current_path},#{session_path}}' "` + sh + `" ; set-option -p @beadpane 1`
}

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
// new-session running the wrapper, then its tags.
// new-session refuses a duplicate name (ADR 0036 §3). The tags ride the same ;-joined command so the session is
// never listed untagged: an untagged live session's tree would read as an orphan.
func launchArgs(sess, cwd string, pane []string, t sessionTags) []string {
	args := append(preamble(), ";", "new-session", "-d", "-s", sess, "-c", literal(cwd))
	return append(append(append(appendPane(append(args, "--"), pane), ";"), tagArgs(sess, t)...), unmarkArgs(sess)...)
}

// sessionTags is what a session carries: its bead, the agent's session id, its
// worktree ("" = repo root) and its Agent Profile ("" = the built-in, ADR 0122
// §6) — the @ tags, and the attrs hitl-run records.
type sessionTags struct {
	bead                       bd.Bead
	session, worktree, profile string
}

// tagArgs tags the started session with its @ options (ADR 0036 §1) and its bar.
func tagArgs(sess string, t sessionTags) []string {
	return setArgs(sess, append([][2]string{
		{"@bead", t.bead.ID}, {"@session", t.session}, {"@worktree", t.worktree}, {"@profile", t.profile},
	}, bar(t.bead)...))
}

// bar is a HITL session's status line (ADR 0066 §3) and key table (ADR 0065 §5): b
// on the left, F10 and both ways out on the right, no window list, no prefix keys. A dead
// pane keeps it.
func bar(b bd.Bead) [][2]string {
	left := plainSBRN(b)
	if b.Kind == contract.KindDiscussion { // ADR 0194 §2
		left = b.Title + " · Robin discussion"
	} else if b.Kind == contract.KindCheckup {
		left = b.Title + " · Chopper checkup"
	}
	return [][2]string{
		{"status-left", literal(left)}, {"status-left-length", "200"},
		{"status-right", rightBar(b.ID, false)}, {"key-table", hitlTable},
		{"window-status-format", ""}, {"window-status-current-format", ""},
	}
}

// plainSBRN is b's standard bead row notation as plain text, no dot slot (GLOSSARY),
// for the launch-time bar: the glyph is b's own status, so the TUI's richer blocked
// and paused glyphs arrive on the first tick. ponytail: mirrors the TUI's glyph and
// kind-tag text (container kinds lack the TUI's tag padding); the tick overwrites it, so a drift costs one tick, accepted.
func plainSBRN(b bd.Bead) string {
	glyph := map[string]string{"closed": "✓", "in_progress": "◐", "blocked": "⊘"}[b.Status]
	if glyph == "" {
		glyph = "○"
	}
	kind := strings.ToUpper(string(b.Kind))
	if kind == "" {
		kind = "—"
	}
	return fmt.Sprintf("%s %s P%d · %s · %s", glyph, kind, b.Priority, b.Title, b.ID)
}

// rightBar is a HITL session's status-right: F10 and both ways out, flipped loud (green,
// reversed) once its bead is closed (ADR 0066 §4).
func rightBar(id string, closed bool) string {
	if closed {
		return "#[fg=green,reverse] ✓ " + literal(id) + " closed — /exit to finish · F10 bead · F12 back to TUI "
	}
	return "F10 bead · F12 back to TUI · /exit to finish"
}

// autoExitBar is an armed HITL session's status-right, left the time to its
// Auto-exit deadline, in M:SS with a part second rounded up (Spec D4).
func autoExitBar(left time.Duration) string {
	s := int((max(left, 0) + time.Second - 1) / time.Second)
	return fmt.Sprintf("auto-exit in %d:%02d · F11 cancels", s/60, s%60)
}

// SetBars sets each alive HITL session's status-right from its bead's closed-ness
// (ADR 0066 §4) and its status-left from left, both keyed by bead id: the TUI derives
// them every tick, so a reopen reverts and a status, priority or title change shows.
// A session listed armed keeps its status-right, which is hitl-run's countdown
// (Spec D4), but still gets its status-left. One tmux call per tick; several TUIs
// writing the same value is harmless.
// ponytail: a session gone between the list and the set fails the call and stops the
// rest of its ;-chain for that tick; the next tick re-derives.
func SetBars(closed map[string]bool, left map[string]string, sessions map[string]Info) error {
	args := barsArgs(closed, left, sessions)
	if len(args) == 0 {
		return nil
	}
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	return runTmux(sock, args)
}

// barsArgs is SetBars' ;-joined set-option argv, in bead-id order: a status-left for
// each session left names, a status-right for each not armed in sessions; nil if none.
func barsArgs(closed map[string]bool, left map[string]string, sessions map[string]Info) []string {
	var args []string
	for _, id := range slices.Sorted(maps.Keys(closed)) {
		var opts [][2]string
		if l, ok := left[id]; ok {
			opts = append(opts, [2]string{"status-left", literal(l)})
		}
		if sessions[id].AutoExit.IsZero() {
			opts = append(opts, [2]string{"status-right", rightBar(id, closed[id])})
		}
		if len(opts) > 0 {
			args = append(append(args, setArgs(tmuxName(id), opts)...), ";")
		}
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
// profile only when named, then --argv-file, where the agent argv waits (writeArgvFile's),
// and --env-file, the env handoff (writeEnvFile's) — on the command, not by -e, which
// would stick to the session for any later pane.
func paneArgs(exe string, t sessionTags, argvFile, envFile string) []string {
	out := []string{exe, "hitl-run"}
	attrs := eventlog.BeadAttrs(t.bead)
	for i := 0; i+1 < len(attrs); i += 2 {
		out = append(out, fmt.Sprintf("--%v=%v", attrs[i], attrs[i+1]))
	}
	out = append(out, "--session="+t.session, "--worktree="+t.worktree)
	if t.profile != "" {
		out = append(out, "--profile="+t.profile)
	}
	return append(out, "--argv-file="+argvFile, "--env-file="+envFile)
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
	env, err := writeEnvFile(ac.Env)
	if err != nil {
		return err
	}
	argv, err := writeArgvFile(ac.Argv)
	if err != nil {
		os.Remove(env)
		return err
	}
	cwd, wt, teardown, err := placeTree(b)
	if err != nil {
		removeFiles(env, argv)
		return err
	}
	// A fresh launch means no session: gone reads as never launched (ADR 0038 §1),
	// so any Drafts are a dead session's, and the new one's idempotency guards must
	// not adopt them (ADR 0059 §6) — a repo-root leaf is never an orphan to drop.
	if err := DropDrafts(b); err != nil {
		removeFiles(env, argv)
		teardown()
		return err
	}
	sess, t := tmuxName(b.ID), sessionTags{b, ac.SessionID, wt, ac.Profile}
	// ponytail: tmux stops the sequence at the first failing command, so an error is
	// new-session's (no pane took the placement); the tags are fixed options on the
	// session it just made and don't fail. If they ever can, check for an untagged
	// =sess before teardown — not kill-session, which would hit a duplicate's live pane.
	if err := runTmux(sock, launchArgs(sess, cwd, paneArgs(exe, t, argv, env), t)); err != nil {
		removeFiles(env, argv) // no pane took it
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
func respawnArgs(sess, cwd string, pane []string, b bd.Bead) []string {
	args := []string{"respawn-pane", "-t", exact(sess) + ":", "-c", literal(cwd)}
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

// Resume reruns b's exited session s (its @session / @worktree / @profile, as Sessions read them)
// in the kept worktree through the hitl-run wrapper, so a later clean exit tears
// that worktree down as at launch (ADR 0037 §4, ADR 0039 §1, §4). The agent argv
// is h's ResumeCommand (h built for s's profile), its Env riding the env file as at
// launch; its error (a pruned transcript) is Resume's, with nothing
// spawned. A kept worktree that is gone is an error, nothing spawned: tmux
// would fall back to $HOME. A repo-root leaf ("" worktree) resumes from the repo
// root. Attach with SwitchTo.
func Resume(b bd.Bead, s Info, h contract.Harness) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	e, _ := registry.Lookup(b.Kind) // an unknown kind resumes with the edit tools, as before
	ac, err := h.ResumeCommand(s.Session, e.ReadOnly)
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
	env, err := writeEnvFile(ac.Env)
	if err != nil {
		return err
	}
	argv, err := writeArgvFile(ac.Argv)
	if err != nil {
		os.Remove(env)
		return err
	}
	pane := resumePane(paneArgs(exe, sessionTags{b, ac.SessionID, s.Worktree, s.Profile}, argv, env))
	if err := runTmux(sock, respawnArgs(tmuxName(b.ID), cwd, pane, b)); err != nil {
		removeFiles(env, argv) // no pane took it
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

// Finish pulls b's session's Auto-exit deadline forward to now, so hitl-run's clock
// fires on its next poll and the normal close runs (ADR 0111 §4, Spec FR 2). It
// reports whether the session was armed; an unarmed one is left untouched. The
// read-then-set window is covered by the clock's own @waiting == turn re-check.
func Finish(b bd.Bead) (armed bool, err error) {
	sock, err := serverSocket()
	if err != nil {
		return false, err
	}
	pane := exact(tmuxName(b.ID)) + ":"
	if dl, _ := readAutoExit(sock, pane); dl.IsZero() {
		return false, nil
	}
	err = runTmux(sock, []string{"set-option", "-t", pane, "@autofinish", strconv.FormatInt(clockNow().Unix(), 10)})
	return err == nil, err
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
	// The child comes down before the host (FR 22): removing the host first strands
	// the child's admin entry with effort/<id> still checked out.
	if _, nerr := removeNested(s.Worktree); nerr != nil {
		return errors.Join(err, nerr)
	}
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
	ahead, del := git.CommitsAhead, deleteBranch
	if r := contexts.RepoOf(contexts.Owner(b, d.Container)); r != "" { // a nested effort's branch lives in the child
		ahead, del = r.CommitsAhead, r.DeleteBranch
	}
	if n, err := ahead(d.Container, d.Branch); err != nil || n > 0 {
		return nil // ponytail: a missing branch errors too; nothing to delete either way
	}
	return del(d.Branch)
}

// HolderBead is the bead whose session (alive or exited) has wt as @worktree, ""
// if none: the holder a busy status names (ADR 0037 §7).
// An exact path match: @worktree and git's porcelain both carry NewWorktree's
// symlink-resolved path.
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
//
// A nested effort's child worktrees come down first, then the host, the Workspace
// prune and a prune in each declared child (Spec FR 53, ADR 0169); a child prune
// that fails (the dir isn't cloned) is no error.
func DropOrphan(wt string) error {
	children, err := removeNested(wt)
	if err != nil {
		return err
	}
	if err := removeTree(wt); err != nil {
		return err
	}
	if err := pruneWorktrees(); err != nil {
		return err
	}
	for _, r := range children {
		pruneChild(r)
	}
	return nil
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
//
// A handoff file that no longer exists is no handoff (a session started by an older
// mugicrew still carries the stale variable); any other read error is returned.
func ApplyTUIEnv() error {
	path, ok := os.LookupEnv(envFileVar)
	if !ok {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return applyEnvFile(path, "TERM", "TMUX", "TMUX_PANE")
}

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
	env, err := writeEnvFile(nil)
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
	return switchClient(sock, ownPane(sock), tmuxName(beadID))
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
// yet attached, or away in a HITL session) or in a bead-tagged session (a nested
// TUI prototype) it writes nothing.
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
	out, err := tmuxCmd(sock, "list-clients", "-t", pane, "-F", "#{client_tty}\t#{session_name}\t#{@bead}").Output()
	if err != nil {
		return fmt.Errorf("tmux list-clients: %w", err)
	}
	tty, rest, _ := strings.Cut(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0], "\t")
	own, bead, _ := strings.Cut(rest, "\t")
	if tty == "" || bead != "" {
		return nil // no client, or a TUI nested in a bead session: the real TUI keeps its @tty
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
	Profile           string // its Agent Profile, @profile; "" is the built-in (ADR 0122 §6)
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

// Armed reports whether i is a live session with an Auto-exit deadline set, i.e.
// one Finish can pull forward (ADR 0111).
func (i Info) Armed() bool { return i.State == Alive && !i.AutoExit.IsZero() }

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
const listFormat = "#{@bead}\t#{@session}\t#{@worktree}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_dead_signal}\t#{@waiting}\t#{@closing}\t#{@autofinish}\t#{@profile}"

// Sessions reads the repo's mugicrew tmux server's session table, keyed by bead id
// — the only liveness source (ADR 0036 §1, §4–5). No tmux, or a server that isn't
// running (never started, killed, or a stale socket after a reboot), is an empty
// table; any other failure is an error. Orphan classification needs the difference
// — a failed read must not make a live session's tree an orphan.
func Sessions() (map[string]Info, error) {
	out, err := listSessions(listFormat)
	if err != nil || out == "" {
		return nil, err
	}
	return parseList(out), nil
}

// TUIAlive reports whether a TUI is alive in this repo: any tui-* session on its
// mugicrew tmux server but exclude, the caller's own (ADR 0127 §2). Sessions'
// discipline: no tmux or no server is none, any other failure an error.
func TUIAlive(exclude string) (bool, error) {
	out, err := listSessions("#{session_name}")
	for _, s := range strings.Fields(out) {
		if strings.HasPrefix(s, "tui-") && s != exclude {
			return true, nil
		}
	}
	return false, err
}

// ownPane is $TMUX_PANE if this process runs on sock's server, else "": pane ids
// are per server, so a foreign tmux's %N must never target the private one.
func ownPane(sock string) string {
	if !insideOf(os.Getenv("TMUX"), sock) {
		return ""
	}
	return os.Getenv("TMUX_PANE")
}

// OwnSession is the session $TMUX_PANE sits in on the repo's mugicrew tmux server:
// a TUI's own tui-<launcher pid>, for TUIAlive to exclude. "" outside a pane of
// that server: pane ids are per server.
func OwnSession() (string, error) {
	sock, err := serverSocket()
	if err != nil {
		return "", err
	}
	pane := ownPane(sock)
	if pane == "" {
		return "", nil
	}
	out, err := tmuxCmd(sock, "display-message", "-p", "-t", pane, "#{session_name}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// listSessions is list-sessions -F format on the repo's mugicrew tmux server: ""
// with no tmux or no server running, any other failure an error.
func listSessions(format string) (string, error) {
	sock, err := serverSocket()
	if errors.Is(err, ErrNoTmux) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var stderr strings.Builder
	cmd := tmuxCmd(sock, "list-sessions", "-F", format)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if noServer(stderr.String()) {
			return "", nil
		}
		return "", fmt.Errorf("tmux list-sessions: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
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
		if len(f) != 10 || f[0] == "" {
			continue
		}
		s := Info{Session: f[1], Worktree: f[2], Profile: f[9], State: Alive}
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

// watchOption is the pane user option tagging the Watch pane with the bead its
// transcript view shows, as @beadpane tags the bead pane: a second t on the bead finds it by this.
const watchOption = "@watchrun"

var (
	safeID   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	paneName = regexp.MustCompile(`^%[0-9]+$`)
)

// Watch opens, focuses, or reuses the read-only Watch pane running `mugicrew transcript
// <bead>` beside the TUI: a split of the TUI's own pane's window on the repo's private server.
func Watch(bead string) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	pane := ownPane(sock)
	if !paneName.MatchString(pane) || !safeID.MatchString(bead) {
		return fmt.Errorf("watch: not in a pane of this repo's tmux server, or bad bead id %q", bead)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return runTmux(sock, watchArgs(pane, exe, bead))
}

// watchArgs is the one tmux command (after -S) behind Watch: focus the pane in pane's
// window tagged with bead if there is one; else reuse a pane carrying the Watch tag for
// another bead (respawn-pane -k runs the verb for this one, re-tagged and selected); else
// split -h 40% and tag the new pane. pane and bead are validated by Watch (no format or
// quote metacharacters); exe and bead are single-quoted for tmux's command parser, nested
// once per if-shell level. run-shell needs -t pane: Watch runs outside any client, so
// without it the format expands against the "best" session, not pane's window (the F10
// binding copy of this pattern has a client, so it gets away without).
func watchArgs(pane, exe, bead string) []string {
	loop := func(cond string) string { return "#{P:#{?" + cond + ",#{pane_id},}}" }
	mine := loop("#{==:#{" + watchOption + "}," + bead + "}")
	any := loop("#{" + watchOption + "}")
	verb := "-e MUGICREW_WATCH_PANE=1 " + tmuxQuote(exe) + " transcript " + tmuxQuote(bead)
	tagged := tmuxQuote(bead)
	respawn := "respawn-pane -k -t " + any + " " + verb + " ; set-option -p -t " + any + " " + watchOption + " " + tagged +
		" ; select-pane -t " + any
	split := "split-window -h -l 40% -t " + pane + " " + verb + " ; set-option -p " + watchOption + " " + tagged
	return []string{
		"if-shell", "-F", "-t", pane, mine,
		"run-shell -t " + pane + " -C 'select-pane -t " + mine + "'",
		"if-shell -F -t " + pane + " " + tmuxQuote(any) + " " +
			tmuxQuote("run-shell -t "+pane+" -C "+tmuxQuote(respawn)) + " " + tmuxQuote(split),
	}
}

// tmuxQuote single-quotes s for tmux's command parser; an inner ' is closed, escaped, reopened.
func tmuxQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Attach runs an attach-session on beadID's session as a child with this
// terminal's stdio, for a caller outside the private server (compare LaunchTUI).
func Attach(beadID string) error {
	sock, err := serverSocket()
	if err != nil {
		return err
	}
	c := tmuxCmd(sock, "attach-session", "-t", exact(tmuxName(beadID)))
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
