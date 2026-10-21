package session

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	"jayulabs.org/mugicrew/luffy"
	"jayulabs.org/mugicrew/route"
)

// runAgent runs the pane's agent in the pane's terminal, sending SIGINT to its pid
// alone (never the pane's process group, never hitl-run) on a receive from
// interrupt: the Auto-exit clock's fire (Spec D3). removeWorktree is the
// clean-exit teardown. Vars so HitlRun's exit → teardown/event mapping is
// testable without a real agent or repo.
var (
	runAgent = func(argv []string, interrupt <-chan struct{}) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		exited := make(chan struct{})
		defer close(exited)
		go func() {
			for {
				select {
				case <-interrupt:
					cmd.Process.Signal(os.Interrupt)
				case <-exited:
					return
				}
			}
		}()
		return cmd.Wait()
	}
	removeWorktree = removeSessionWorktree
)

// publish is bd.Publish, showBead / successorOf / listDrafts the graph reads that
// gate its close, and releaseClaim a clean exit's release (settle); vars so
// HitlRun is testable without a live bd.
var (
	publish      = bd.Publish
	showBead     = bd.Show
	successorOf  = bd.SuccessorOf
	listDrafts   = bd.Drafts
	releaseClaim = bd.Unclaim
)

// hupGrace is how long a clean exit waits for a drop's hangup to land before
// publishing: kill-session hangs up the agent and this wrapper together, and Go
// relays the wrapper's signal asynchronously. A var so tests run fast.
// ponytail: a fixed grace; a drop slower than it to hang up still races the
// publish — have Drop wait for pane death if that ever bites.
var hupGrace = 100 * time.Millisecond

// PublishDrafts publishes the Drafts bead's session created (ADR 0059 §4),
// recording draft.published with the ids, or draft.failed. None records nothing.
// The batch hands only a raise's gate and merge edges; the carry stays outside
// it (ADR 0085 §4). It closes the bead only when it is a Signal with a caused-by
// Chart, Spec or Initiative successor, so a charted Signal closes as its
// successor becomes visible (ADR 0096; Spec D2), a Signal with reroute Drafts
// (rerouted: <ids>, with the host's gate), or a Survey whose charting is done
// (Spec mc-r832 D8); every other prompt closes its own bead (§5). A failed graph
// read fails the publish, keeping the session for a resume or drop. After a
// reroute's batch lands, each merge's evidence is posted on its pivot.
func PublishDrafts(bead string, attrs []any) error {
	plan, err := sessionPlan(bead)
	var ids []string
	if err == nil {
		ids, err = publish(bead, plan.edges, plan.closing)
	}
	if err == nil {
		err = postMerges(plan.creator, plan.merges)
	}
	return eventlog.RecordDrafts("draft.published", bead, ids, err, attrs...)
}

// publishPlan is what PublishDrafts' batch hands: edges, the bead to close, and the
// merges whose evidence follows the batch.
type publishPlan struct {
	edges   []bd.Edge
	closing *bd.Closing
	merges  []luffy.RaiseMerge
	creator bd.Bead // the session's bead, whose marker line heads each merge's comment
}

// sessionPlan is PublishDrafts' plan: a Signal or Task with raise Drafts or recorded merges
// takes raiseSession's; else the close alone: the bead itself when it is a Signal
// with a Chart, Spec or Initiative successor (any status), or a Survey whose parent
// Chart is closed (push-back) or whose Drafts include a child of the Chart (fog
// seeded), else nil. A Go read, since the batch can't read: the keep path's fog
// and the Survey close then land in one transaction, never a drained fog-less
// Chart (Spec mc-r832 D8). The widening for a reroute is by kind, not a
// status-blind SuccessorOf (ADR 0096 §2).
func sessionPlan(bead string) (publishPlan, error) {
	b, err := showBead(bead)
	if err != nil {
		return publishPlan{}, err
	}
	var done bool
	switch b.Kind {
	case contract.KindSignal, contract.KindTask:
		if plan, ok, err := raiseSession(b); err != nil || ok {
			plan.creator = b
			return plan, err
		}
		if b.Kind == contract.KindSignal {
			done, err = signalCharted(bead)
		}
	case contract.KindSurvey:
		done, err = surveyDone(b)
	}
	if err != nil || !done {
		return publishPlan{}, err
	}
	return publishPlan{closing: &bd.Closing{ID: bead}}, nil
}

func signalCharted(bead string) (bool, error) {
	for _, k := range []contract.Kind{contract.KindChart, contract.KindSpec, contract.KindInitiative} {
		if _, ok, err := successorOf(bead, k); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func surveyDone(s bd.Bead) (bool, error) {
	chart, err := showBead(s.Parent)
	if err != nil || chart.Status == "closed" {
		return err == nil, err
	}
	drafts, err := listDrafts(s.ID)
	return slices.ContainsFunc(drafts, func(d bd.Bead) bool { return d.Parent == s.Parent }), err
}

// settle is a clean exit's settle, run after its publish: a bead still
// in_progress goes back to open and unassigned, so a premature quit strands no
// claim (Spec mc-rjvx) and a Survey quit before it seeded fog returns to the
// Inbox (Spec mc-r832 D9). Judged by status, not by whether this launch claimed
// it, so a Signal the publish closed is left alone. Exempt: a Task (stays claimed
// and idle for l), a Surgery (claimed at filing, as a drop treats it) and a
// Discussion (an early /exit leaves it open and claimed for a rejoin, FR 35). A
// closed Survey whose parent Chart the session closed (push-back) with no caused-by Spec successor names chart/<parent>
// for HitlRun to delete once the tree is down (ADR 0130 §6, re-keyed on the
// Survey).
// In Go, so the prompt can't skip it.
func settle(bead string) (branch string, err error) {
	b, err := showBead(bead)
	if err != nil {
		return "", err
	}
	if b.Status == "in_progress" {
		if b.Kind == contract.KindTask || b.Kind == contract.KindSurgery || b.Kind == contract.KindDiscussion || b.Kind == contract.KindCheckup {
			return "", nil
		}
		return "", releaseClaim(bead)
	}
	if b.Kind != contract.KindSurvey || b.Status != "closed" {
		return "", nil
	}
	chart, err := showBead(b.Parent)
	if err != nil || chart.Status != "closed" {
		return "", err
	}
	if _, ok, err := successorOf(chart.ID, contract.KindSpec); err != nil || ok {
		return "", err
	}
	return git.BranchName(string(contract.KindChart), chart.ID), nil
}

// HitlRun is the hidden `mugicrew hitl-run` wrapper every tmux session pane runs
// (ADR 0037 §1): it records hitl.launched (hitl.resumed when started with
// --resume, ADR 0037 §4), swaps the server's stale env for the launcher's from
// the MUGICREW_ENV_FILE handoff (ADR 0037 note), exports the pane's address for
// hitl-hook (paneVar), runs the agent argv from --argv-file (writeArgvFile's),
// and returns the agent's exit status for the process to exit with. A clean exit
// (rc=0) first marks its session Closing and switches its viewers to their TUIs
// (ADR 0108 §1–2), then publishes the bead's Drafts (ADR 0059 §4), settles the
// bead (settle: releases it if still in_progress, bar a Task, Surgery or
// Discussion; a failed release fails the session) and tears the session's
// worktree down, then deletes a pushed-back Chart's branch after its Survey session (ADR 0130),
// so the branch is free (§2). The last act, after hitl.exited, is killing the
// pane's own tmux session (endSession), which removes it from the table whatever
// other panes are open in it (ADR 0037 (session-end-teardown-in-pane)). A non-zero exit or a signal keeps everything:
// remain-on-exit failed leaves the dead pane listed as exited and the
// worktree on disk for a resume (§3). It is the one place that sees every session
// end, so it writes hitl.exited / hitl.failed (ADR 0029 amendment), with the
// Session cost meter reads around the agent run for the session attr. meter is the
// harness's (claudecode.MeterCost), passed in so session stays harness-blind.
func HitlRun(args []string, meter func(sessionID string) func() contract.Metered) int {
	// Every --key=value before -- is an event attr (Start passes the bead attrs and
	// session); --worktree also names the tree a clean exit removes ("" = repo root).
	var attrs []any
	worktree, bead, sess, argvFile, envFile, startEvent := "", "", "", "", "", "hitl.launched"
	for _, a := range args {
		if a == "--resume" {
			startEvent = "hitl.resumed"
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !ok || !strings.HasPrefix(a, "--") {
			break
		}
		switch k {
		case "worktree":
			worktree = v
		case "argv-file":
			argvFile = v
		case "env-file":
			envFile = v
		default:
			attrs = append(attrs, k, v)
		}
		switch k {
		case "bead":
			bead = v
		case "session":
			sess = v
		}
	}
	if argvFile == "" {
		fmt.Fprintln(os.Stderr, "usage: mugicrew hitl-run [--resume] [--key=value ...] [--profile=<name>] [--worktree=<wt>] --argv-file=<path> [--env-file=<path>]")
		return 2
	}

	// The pane's signals are the agent's to handle: Ctrl-C must reach claude
	// without killing its wrapper, and a hangup (kill-session) must still leave
	// the wrapper alive to record hitl.failed. Notify, not Ignore: an ignored
	// disposition would be inherited by the agent.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	began := time.Now()
	slog.Info(startEvent, attrs...)
	tmuxEnv, pane := os.Getenv("TMUX"), os.Getenv("TMUX_PANE") // before applyEnvFile drops them
	// ponytail: only the agent gets the fresh env; this wrapper's logger was set
	// up by main from the server's env (MUGICREW_LOG_LEVEL, the effort id). Apply
	// the file in main before eventlog.Open if those ever need to be fresh.
	if err := applyEnvFile(envFile, "TERM"); err != nil {
		// Loud, not a fall-through: the stale server env is what FR5 rules out.
		fmt.Fprintln(os.Stderr, "mugicrew hitl-run:", err)
		eventlog.Record("hitl.exited", "hitl.failed", err, append(attrs, "dur_ms", time.Since(began).Milliseconds())...)
		return 1
	}
	// The pane's address for the agent's hitl-hook (Spec D3); none outside
	// tmux, so a stale one from the launcher's env never names another pane.
	if sock, _, _ := strings.Cut(tmuxEnv, ","); sock != "" && pane != "" {
		os.Setenv(paneVar, sock+","+pane)
	} else {
		os.Unsetenv(paneVar)
	}
	argv, err := readArgvFile(argvFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mugicrew hitl-run:", err)
		eventlog.Record("hitl.exited", "hitl.failed", err, append(attrs, "dur_ms", time.Since(began).Milliseconds())...)
		return 1
	}
	// After applyEnvFile: the agent's CLAUDE_CONFIG_DIR is where its transcript is.
	readCost := meter(sess)
	sessAttrs := slices.Clip(attrs) // the bead and session attrs, for the backstop's events
	fire, done := make(chan struct{}, 1), make(chan struct{})
	if sock, _, _ := strings.Cut(tmuxEnv, ","); sock != "" && pane != "" {
		go autoExitClock(sock, pane, bead, fire, done) // with no TUI open too (FR13)
	}
	err = runAgent(argv, fire)
	close(done)
	metered := readCost()
	cost, follow := Handoff(sessAttrs, metered, SpawnFollower) // resolves the log before the teardown
	dropped := err == nil && bead != "" && hungUp(hup)
	chartBranch, stranded := "", false
	if err == nil && bead != "" && !dropped {
		// The human is back before the close runs (ADR 0108 §1): @closing first, so
		// a TUI's first tick after the switch already reads Closing (§2). A failed
		// close leaves it set on the dead pane, telling it from a crash (§4).
		markClosing(tmuxEnv, pane)
		returnViewers(tmuxEnv, pane)
		// A failed publish fails the session: the pane and Drafts stay for a resume
		// (which publishes on its own clean exit) or a drop. A hangup is a drop's
		// kill-session: an agent exiting 0 on it must not publish what the drop deletes.
		if err = PublishDrafts(bead, attrs); err == nil {
			chartBranch, err = settle(bead)
		}
	}
	attrs = append(append(attrs, eventlog.ExitAttrs(err)...), "dur_ms", time.Since(began).Milliseconds())
	attrs = append(attrs, cost...)
	if dropped { // a drop is no finish: the TUI's ✓ finished flash skips it (ADR 0067 §2)
		attrs = append(attrs, "dropped", true)
	}
	var eb bd.Bead
	effort := false
	if err == nil && worktree != "" && !dropped {
		eb, effort, err = onEffort(bead)
	}
	switch {
	case err != nil || worktree == "":
	case effort:
		// The backstop owns an effort tree's teardown, so a clean exit never removes
		// a tree whose work has not reached the container (Spec D6, FR 20).
		// Its outcome rides on hitl.exited, which stays clean: a conflict keeps the
		// tree as an orphan l relaunches through (FR 40), not a resume, and is no
		// failure (FR 43); kept says effort/<id> outlives it, its bead reopened if
		// the session closed it.
		var terr error
		var kept bool
		stranded, kept = Backstop(eb, worktree, false, func() { terr = removeWorktree(worktree) }, os.Stdout, sessAttrs...)
		err = terr
		if stranded {
			attrs = append(attrs, "stranded", true)
		}
		if kept {
			attrs = append(attrs, "kept", true)
		}
	default:
		// A failed teardown fails the session: the non-zero exit keeps the pane and
		// its @worktree listed, so the stranded tree stays droppable. A drop never
		// integrates, and Drop keeps effort/<id> with whatever it has committed.
		err = removeWorktree(worktree)
	}
	// After the teardown, so an effort tree's backstop has integrated onto the
	// branch first; a stranded tree keeps it for its relaunch. A failed delete is
	// only branch.kept: the session's work is done either way.
	if err == nil && chartBranch != "" && !stranded {
		eventlog.RecordBranches([]bd.BranchOutcome{{Branch: chartBranch, Err: deleteBranch(chartBranch)}}, sessAttrs...)
	}
	follow() // before hitl.exited, which it never delays (Spec mc-11b8 D4)
	if eventlog.Record("hitl.exited", "hitl.failed", err, attrs...) == nil {
		// The close is done and recorded: end the session with every pane a human
		// or crewmate split into it (ADR 0037). Its SIGHUP lands on hup, unread now.
		if !dropped && pane != "" {
			endSession(tmuxEnv, pane)
		}
		return 0
	}
	return exitCode(err)
}

// The Auto-exit clock's seams (Spec D3, D4): how often it polls, the clock
// it reads, the tmux read (hitl-hook's too) and unset of the session's
// @autofinish deadline and @waiting bit, and its status-right write; a failed
// read is disarmed and not waiting. Vars so the clock is testable without tmux or
// a real wait.
var (
	autoExitPoll = time.Second
	clockNow     = time.Now
	readAutoExit = func(sock, pane string) (time.Time, Waiting) {
		out, err := tmuxCmd(sock, "display-message", "-p", "-t", pane, "#{@autofinish}\t#{@waiting}").Output()
		if err != nil {
			return time.Time{}, NotWaiting
		}
		dl, w, _ := strings.Cut(strings.TrimSuffix(string(out), "\n"), "\t")
		n, err := strconv.ParseInt(dl, 10, 64)
		if err != nil {
			return time.Time{}, Waiting(w)
		}
		return time.Unix(n, 0), Waiting(w)
	}
	unsetAutoExit = func(sock, pane string) {
		tmuxCmd(sock, "set-option", "-u", "-t", pane, "@autofinish").Run()
	}
	setBar = func(sock, pane, right string) {
		tmuxCmd(sock, "set-option", "-t", pane, "status-right", right).Run()
	}
)

// autoExitClock is hitl-run's Auto-exit clock (Spec D3, ADR 0111): until
// done, it polls pane's session about every autoExitPoll, and once its @autofinish
// deadline has passed it reads again. Still armed and still waiting on the human's
// turn, it fires (a send on fire, never blocking); either way it unsets
// @autofinish, so a reply or prompt racing the deadline wins and an agent that
// outlives its SIGINT is not fired at again until the next closing line re-arms.
// While armed it owns the bar, writing the countdown each tick, and the first
// tick that reads it cleared restores bead's normal bar (D4), its closed-ness
// read as the TUI reads it; the TUI's SetBars skips an armed session.
func autoExitClock(sock, pane, bead string, fire chan<- struct{}, done <-chan struct{}) {
	shown := false // the bar is the countdown
	for {
		select {
		case <-done:
			return
		case <-time.After(autoExitPoll):
		}
		dl, _ := readAutoExit(sock, pane)
		if dl.IsZero() {
			if shown {
				b, _ := showBead(bead) // ponytail: a failed read restores the open bar; the TUI's next tick flips it
				setBar(sock, pane, rightBar(bead, b.Status == "closed"))
				shown = false
			}
			continue
		}
		if now := clockNow(); now.Before(dl) {
			setBar(sock, pane, autoExitBar(dl.Sub(now)))
			shown = true
			continue
		}
		if dl, w := readAutoExit(sock, pane); !dl.IsZero() && w == WaitTurn {
			select {
			case fire <- struct{}{}:
			default:
			}
		}
		unsetAutoExit(sock, pane)
	}
}

// onEffort reports whether bead's session tree is on its own effort/<id> (route's
// decision, as dispatch's), returning the bead for the backstop. No bead, or one
// route can't place, is on none.
func onEffort(bead string) (bd.Bead, bool, error) {
	if bead == "" {
		return bd.Bead{}, false, nil
	}
	b, err := showBead(bead)
	if err != nil {
		return b, false, err
	}
	_, ok, _ := route.OnEffort(b)
	return b, ok, nil
}

// markClosing sets the @closing option on pane's session: its close is running
// (ADR 0108 §2). Best-effort: unmarked, the row just reads as before. A var so
// HitlRun's clean exit is testable without tmux.
var markClosing = func(tmuxEnv, pane string) {
	if pane == "" {
		return
	}
	sock, _, _ := strings.Cut(tmuxEnv, ",")
	if err := runTmux(sock, []string{"set-option", "-t", pane, "@closing", "1"}); err != nil {
		slog.Warn("hitl.closing", "err", err)
	}
}

// returnViewers switches every client viewing pane's session to its own TUI as a
// clean exit's close begins (ADR 0065 §6, ADR 0108 §1), so tmux's default
// detach-on-destroy only detaches clients with no TUI. Never "previous"/"last":
// that can be another HITL session. Best-effort, the session is done either way.
// A var so HitlRun's clean exit is testable without tmux.
var returnViewers = func(tmuxEnv, pane string) {
	if pane == "" {
		return // not in a tmux pane: no viewers
	}
	sock, _, _ := strings.Cut(tmuxEnv, ",")
	if err := returnClients(sock, pane); err != nil {
		slog.Warn("hitl.return", "err", err)
	}
}

// endSession kills the tmux session that holds pane (HitlRun calls it only with
// one), with every pane and window in it, as a clean finish's last act. The pane
// id resolves to its own session, so a name collision can't hit another (no
// =<name> lookup). Best-effort: a failed kill only logs. A var so HitlRun's
// clean exit is testable without tmux.
var endSession = func(tmuxEnv, pane string) {
	sock, _, _ := strings.Cut(tmuxEnv, ",")
	if err := runTmux(sock, killSessionArgv(pane)); err != nil {
		slog.Warn("hitl.ended", "err", err)
	}
}

// killSessionArgv is endSession's tmux argv: the pane id, never a session name.
// A func of its own so the argv is testable without tmux.
func killSessionArgv(pane string) []string {
	return []string{"kill-session", "-t", pane}
}

// hungUp reports whether the pane was hung up (kill-session), waiting hupGrace
// for the signal to land.
func hungUp(hup <-chan os.Signal) bool {
	select {
	case <-hup:
		return true
	case <-time.After(hupGrace):
		return false
	}
}

// applyEnvFile replaces the process env with the launcher's, from the file
// path names (writeEnvFile's), deleting the file at once since it holds the
// user's env. The pane's own keep vars survive (tmux's TERM describes the
// pane's terminal). No path, nothing to apply.
func applyEnvFile(path string, keep ...string) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	os.Remove(path)
	if err != nil {
		return fmt.Errorf("env handoff: %w", err)
	}
	kept := map[string]string{}
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			kept[k] = v
		}
	}
	os.Clearenv()
	for _, kv := range strings.Split(string(b), "\x00") {
		if k, v, ok := strings.Cut(kv, "="); ok && k != envFileVar {
			os.Setenv(k, v)
		}
	}
	for k, v := range kept {
		os.Setenv(k, v)
	}
	return nil
}

// readArgvFile is the agent argv from writeArgvFile's file, deleted at once since
// it holds the prompt. An empty or unreadable file is an error.
func readArgvFile(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	os.Remove(path)
	if err == nil && len(b) == 0 {
		err = errors.New("empty file")
	}
	if err != nil {
		return nil, fmt.Errorf("argv handoff: %w", err)
	}
	return strings.Split(string(b), "\x00"), nil
}

// exitCode is the status the wrapper passes through: the agent's own code, 128+n
// for a death by signal n (the shell convention), else 1.
func exitCode(err error) int {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return 1
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if c := ee.ExitCode(); c > 0 {
		return c
	}
	return 1
}

// removeSessionWorktree is removeTree run from the common dir, never
// from inside the tree it removes (the pane's cwd); the process-wide Chdir is fine
// since hitl-run exits right after.
func removeSessionWorktree(wt string) error {
	common, err := git.CommonDir()
	if err != nil {
		return err
	}
	var root string
	if len(declaredRepos(".")) > 0 { // no repo = declared: no git call
		var rerr error
		if root, rerr = repoRoot(); rerr != nil {
			return rerr
		}
	}
	if err := os.Chdir(common); err != nil {
		return err
	}
	if root != "" {
		if _, err := removeChildren(root, wt); err != nil {
			return err
		}
	}
	return removeTree(wt)
}

// removeNested is removeChildren for a Workspace that declares a repo =, the
// no-config launch paying no git call. It returns the declared child repos, for
// DropOrphan's prune.
func removeNested(wt string) ([]git.Repo, error) {
	if len(declaredRepos(".")) == 0 {
		return nil, nil
	}
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	return removeChildren(root, wt)
}

// removeChildWorktree and pruneChild are the child repo's remove and prune, seams so
// DropOrphan's child-first order is testable.
var (
	repoRoot            = git.RepoRoot
	removeChildWorktree = func(r git.Repo, path string) error { return r.RemoveWorktree(path) }
	pruneChild          = func(r git.Repo) { r.PruneWorktrees() }
)

// removeChildren takes down the child trees materialized in the host wt. A nested
// tree's child comes down first: removing the host first strands the child's admin
// entry and keeps effort/<id> checked out there (Spec FR 22). It returns the
// declared child repos, sorted by dir; a failed child remove stops before the host
// is touched.
func removeChildren(root, wt string) ([]git.Repo, error) {
	var repos []git.Repo
	for _, dir := range slices.Sorted(maps.Values(declaredRepos(root))) {
		r := git.Repo(filepath.Join(root, dir))
		repos = append(repos, r)
		if fi, err := os.Stat(filepath.Join(wt, dir, ".git")); err == nil && fi.Mode().IsRegular() {
			if err := removeChildWorktree(r, filepath.Join(wt, dir)); err != nil {
				return repos, fmt.Errorf("remove child worktree %s: %w", filepath.Join(wt, dir), err)
			}
		}
	}
	return repos, nil
}

// gitRemoveWorktree is git.RemoveWorktree, a var so Drop and DropOrphan are
// testable without a repo.
var gitRemoveWorktree = git.RemoveWorktree

// removeTree drops wt with `git worktree remove --force` plus the mugicrew-wt-*
// temp parent git.NewWorktree made for it. A remove that fails with wt already
// gone from disk is no error: the tree is gone (hitl-run's clean exit beat a drop
// to it, or a crash left it prunable).
func removeTree(wt string) error {
	if err := gitRemoveWorktree(wt); err != nil {
		if _, statErr := os.Stat(wt); !errors.Is(statErr, fs.ErrNotExist) {
			return err
		}
	}
	if git.IsTempWorktree(wt) {
		// ponytail: best-effort — the tree is gone, so failing on its leftover temp
		// dir would keep the pane listed with a @worktree no resume can cd into.
		os.RemoveAll(filepath.Dir(wt))
	}
	return nil
}
