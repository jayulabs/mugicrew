// Command mugicrew is the single Application that consumes a Signal and drives
// the bead graph. With no args it opens the human-action TUI (a bubbletea
// skeleton for now); `signal new` files a Signal (spec §7.1), `signal triage`
// opens its charting session (§7.2), `pivot confirm` / `pivot escalate` are the
// triage session's sweeps (ADR 0046 §5), the hidden `hitl-run` wraps a TUI-launched
// session's pane (ADR 0037), the hidden `hitl-hook` records its waiting bit from
// a Claude Code hook (ADR 0099), and `reconcile` is the idempotent
// accept-path verb that session runs (§8.2); `ps` lists active AFK efforts and
// their liveness (ADR 0032); `stats` sums Session cost and Agent time per bead
// from the event log (ADR 0061); `loop` is the foreground dispatch loop under the
// single-loop lock, exec'd in place on main's pinned /nix/store build, `loop
// start` launches it detached on that build, `loop status` reports it
// off/on/draining with that build and `loop stop [--force]` drains or forces it (ADR 0050, ADR 0053, ADR 0060);
// `abort <bead-id>` stops one bead's in-flight effort, leaving it paused (ADR 0083).
// A leading `-C <dir>`, before any verb or none, runs it on that Workspace (ADR 0089).
// A user verb refuses when bd resolves no Bead Graph, and exports it as BEADS_DIR.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/brook"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/franky"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jinbe"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/luffy"
	"jayulabs.org/mugicrew/nami"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/sanji"
	"jayulabs.org/mugicrew/session"
	"jayulabs.org/mugicrew/tui"
	"jayulabs.org/mugicrew/usopp"
	"jayulabs.org/mugicrew/zoro"
)

// resolveCwd (session.Placement) decides the triage session's cwd + teardown from
// its bead's route; interactiveExec runs the composed command; dropDrafts and
// publishDrafts bracket it as hitl-run does (ADR 0059 §4, §6). All are vars so the
// signal-triage path's seams are testable without a repo, a bd or a real process.
var (
	resolveCwd      = session.Placement
	interactiveExec = func(cmd *exec.Cmd) error { return cmd.Run() }
	dropDrafts      = session.DropDrafts
	publishDrafts   = session.PublishDrafts
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hitl-hook" { // hidden: a HITL pane's Claude Code hook (ADR 0099)
		session.HitlHook(os.Stdin, autoExitAt) // first, before anything that can fail: it always exits 0
		return
	}
	args, err := workspaceArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	watch := false
	if len(args) == 1 { // the TUI's `mugicrew --watch`
		if args, watch, err = cutWatch(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := requireGraph(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(args) == 0 {
		if err := runBare(watch); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	warn := func(msg string) { fmt.Fprintln(os.Stderr, msg) }
	slog.SetDefault(eventlog.Open(warn).Logger(args[0])) // proc is the verb: loop, reconcile, signal, ps
	switch {
	case args[0] == "signal" && len(args) >= 2 && args[1] == "new":
		err = runSignalNew(args[2:])
	case args[0] == "signal" && len(args) >= 2 && args[1] == "triage":
		err = runSignalTriage(args[2:])
	case args[0] == "surgery" && len(args) >= 2 && args[1] == "new":
		err = runSurgeryNew(args[2:])
	case args[0] == "surgery" && len(args) >= 2 && args[1] == "end":
		err = runSurgeryEnd(args[2:])
	case args[0] == "pivot" && len(args) >= 2 && args[1] == "confirm":
		err = runPivotConfirm(args[2:])
	case args[0] == "pivot" && len(args) >= 2 && args[1] == "escalate":
		err = runPivotEscalate(args[2:])
	case args[0] == "reconcile":
		err = runReconcile(args[1:])
	case args[0] == "loop":
		err = runLoopVerb(args[1:])
	case args[0] == "abort":
		err = runAbort(args[1:], os.Stdout)
	case args[0] == "ps":
		err = runPs(args[1:], os.Stdout)
	case args[0] == "stats":
		err = runStats(args[1:], os.Stdout)
	case args[0] == "integrate":
		os.Exit(runIntegrate(args[1:]))
	case args[0] == "chart":
		err = runChart(args[1:])
	case args[0] == "land-lineage":
		err = runLandLineage(args[1:], os.Stdout)
	case args[0] == "hitl-run": // hidden: the tmux pane's wrapper (ADR 0037)
		os.Exit(session.HitlRun(args[1:], claudecode.MeterCost))
	default:
		err = fmt.Errorf("mugicrew: unknown command %v", args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// autoExitAt is hitl-hook's auto_exit setting (ADR 0112), read at arm time from
// the root of the session's cwd (Spec D2, D8); a cwd outside git is its
// own root.
func autoExitAt(cwd string) time.Duration {
	root, err := git.TopLevelOf(cwd)
	if err != nil {
		root = cwd
	}
	return loop.AutoExitSetting(root).D
}

// workspaceArgs strips a leading global -C <dir> (Spec FR2), before any
// verb and on the bare TUI path, and makes the Workspace root it resolves to (FR3:
// git.RepoRoot, the same from a subdirectory or linked worktree) this process's
// cwd, before the event log opens. Every later RepoRoot/CommonDir then resolves
// that Workspace, and every re-exec (loop start's detached spawn, the pinned and
// restart execs, the TUI's o, tmux panes, reconcile children) inherits it as its
// cwd, so the -C needs no forwarding. Without -C nothing moves: a reconcile child
// keeps its worktree cwd.
func workspaceArgs(args []string) ([]string, error) {
	if len(args) == 0 || args[0] != "-C" {
		return args, nil
	}
	if len(args) < 2 {
		return nil, errors.New("usage: mugicrew [-C <dir>] [<verb> ...]")
	}
	if err := os.Chdir(args[1]); err != nil {
		return nil, fmt.Errorf("mugicrew -C: %w", err)
	}
	root, err := git.RepoRoot()
	if err != nil {
		return nil, fmt.Errorf("mugicrew -C %s: not inside a git repo: %w", args[1], err)
	}
	if err := os.Chdir(root); err != nil {
		return nil, fmt.Errorf("mugicrew -C: %w", err)
	}
	return args[2:], nil
}

// graphDir resolves the Bead Graph (bd where); a var so main's tests stub it.
var graphDir = bd.GraphDir

// requireGraph is the onboarding gate (Spec FR6): a user verb (bare TUI,
// signal new/triage, surgery new/end, integrate, chart append|drop, loop and its start/status/stop) resolves the Bead Graph from
// the Workspace root and refuses when none resolves. On success the graph's
// absolute dir becomes this process's BEADS_DIR, so every child it spawns (the
// detached loop, reconcile, tmux sessions, claude -p) hits that graph from any
// cwd with no per-call plumbing (FR8, FR9). Internal verbs (reconcile, pivot,
// abort, ps) only inherit it, as do the loop's hidden re-exec markers unless they
// inherited none: a pre-ADR-0089 loop's restart onto this image.
func requireGraph(args []string) error {
	user := len(args) == 0 ||
		args[0] == "loop" && (len(args) == 1 || args[1] == watchArg || !strings.HasPrefix(args[1], "--") || os.Getenv("BEADS_DIR") == "") ||
		args[0] == "signal" && len(args) >= 2 && (args[1] == "new" || args[1] == "triage") ||
		args[0] == "surgery" && len(args) >= 2 && (args[1] == "new" || args[1] == "end") ||
		args[0] == "integrate" || args[0] == "chart"
	if !user {
		return nil
	}
	root, err := git.RepoRoot()
	if err != nil {
		return err
	}
	dir, err := graphDir(root)
	if err != nil {
		return err
	}
	return os.Setenv("BEADS_DIR", dir)
}

// abortEffort is the shared abort (ADR 0083), a var so the CLI's tests stub it.
var abortEffort = dispatch.Abort

// runAbort is `mugicrew abort <bead-id>`: no prompt (typing the verb is the
// confirmation); it prints the pids signalled, or that the bead was only paused.
func runAbort(argv []string, w io.Writer) error {
	if len(argv) != 1 {
		return errors.New("usage: mugicrew abort <bead-id>")
	}
	pids, err := abortEffort(argv[0], "cli")
	if len(pids) > 0 {
		fmt.Fprintln(w, "signalled", dispatch.FormatPids(pids))
	} else if err == nil {
		fmt.Fprintln(w, "no live process; paused only")
	}
	return err
}

// runLandLineage is `mugicrew land-lineage <branch>` (Spec D1): it
// fast-forwards main onto a lineage branch under the land lock and prints the
// landed sha. CLI only; CommonDir finds the lock from main or any worktree.
func runLandLineage(argv []string, w io.Writer) error {
	if len(argv) != 1 {
		return errors.New("usage: mugicrew land-lineage <branch>")
	}
	sha, err := git.LandLineage(argv[0])
	if err == nil {
		fmt.Fprintln(w, sha)
	}
	return err
}

// appCtx is the minimal contract.Ctx the CLI verbs need: it hands the crewmate
// the harness. The loop assembles the fuller ctx it dispatches with
// (spec §5.2); this is the standalone-verb equivalent.
type appCtx struct{ harness contract.Harness }

func (c appCtx) Harness() contract.Harness { return c.harness }

// newCtx wires the one concrete backend (spec §5.2). Model rides Claude Code's
// native resolution — mugicrew configures none (no config framework, §5.2).
// ponytail: empty model = native resolution; add a source when one is needed.
func newCtx() appCtx { return appCtx{harness: claudecode.New("")} }

// crew is the owner→crewmate table the TUI and loop dispatch through (spec §5.2):
// Nami, Zoro, Sanji, Franky, Usopp, Luffy, and Jinbe are wired — each key matches
// its registry owner.
func crew() map[string]contract.Crewmate {
	return map[string]contract.Crewmate{
		"nami":   nami.Nami{},
		"zoro":   zoro.Zoro{},
		"sanji":  sanji.Sanji{},
		"franky": franky.Franky{},
		"usopp":  usopp.Usopp{},
		"luffy":  luffy.Luffy{},
		"jinbe":  jinbe.Jinbe{},
		"brook":  brook.Brook{},
	}
}

// runBare is a bare mugicrew (ADR 0065 §1): on the repo's private tmux server it
// runs the TUI in its pane, with the launcher's env; anywhere else it is the
// launcher that hosts the TUI as a TUI session there and attaches this terminal.
// watch (`mugicrew --watch`) rides into the pane, and the TUI's o forwards it.
func runBare(watch bool) error {
	inside, err := session.Inside()
	if err != nil {
		return err
	}
	if !inside {
		if watch {
			return session.LaunchTUI(watchArg)
		}
		return session.LaunchTUI()
	}
	if err := session.ApplyTUIEnv(); err != nil {
		return err
	}
	// The alt-screen owns the terminal: the TUI surfaces the event log's one-time
	// warning on its status line (Log.Warning) instead of stderr.
	evlog := eventlog.Open(nil)
	slog.SetDefault(evlog.Logger("tui"))
	return runTUI(evlog, watch)
}

// runTUI opens the human-action TUI (spec §9). It wires the crewmate ctx (the
// harness, for launch) and the crew table, and runs the bubbletea program in the
// alt-screen until the human quits.
func runTUI(log *eventlog.Log, watch bool) error {
	m := tui.New(newCtx(), crew()).WithLog(log).WithWatch(watch).WithUsage(claudecode.ReadUsage)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// runSignalTriage resolves a Signal and opens the composed charting session in
// the human's terminal (spec §7.2). Nami.Launch (via the harness) formats the
// command; this verb runs it interactively — the second Launch invoker alongside
// the TUI. Accept/reject happen inside the session (§7.3), not here.
func runSignalTriage(argv []string) error {
	if len(argv) != 1 {
		return errors.New("usage: mugicrew signal triage <id>")
	}
	bead, err := bd.Show(argv[0])
	if err != nil {
		return err
	}
	if bead.Kind != contract.KindSignal {
		return fmt.Errorf("signal triage: %s is kind %q, not a signal", bead.ID, bead.Kind)
	}
	lc, err := nami.Nami{}.Launch(contract.WithClosingLine(newCtx()), contract.Bead{ID: bead.ID, Kind: bead.Kind})
	if err != nil {
		return err
	}
	ac, ok := lc.(contract.AgentCommand)
	if !ok {
		return fmt.Errorf("signal triage: expected an agent command, got %T", lc)
	}
	// Run the session in the tree its kind routes to: a Signal triage
	// is a leaf, so this is repo-root cwd; the setup/teardown mirrors the TUI so an
	// in-tree kind opened here would get its worktree too. Teardown is unconditional.
	cwd, teardown, err := resolveCwd(bead)
	if err != nil {
		return err
	}
	defer teardown()
	return runInteractive(ac, cwd, bead)
}

// runInteractive execs a launch command in the human's TTY (their shell supplies
// stdin/out/err) with cwd as its working directory, the running half of the triage
// verb (spec §7.2). Thin wiring over os/exec — the argv is composed and tested in
// the harness; this only sets the cwd and hands the process the terminal. Like the
// TUI launch, it brackets the session with hitl.launched / hitl.exited (Spec FR29)
// — events go to the log file, never the TTY streams — and ends it
// like hitl-run: a dead run's leftover Drafts are deleted before the launch, and a
// clean exit publishes the session's Drafts (ADR 0059 §4, §6). A failed exit keeps
// them; the next launch deletes them.
func runInteractive(ac contract.AgentCommand, cwd string, b bd.Bead) error {
	if len(ac.Argv) == 0 {
		return errors.New("launch command has empty argv")
	}
	// ponytail: deletes by creator, so a still-resumable tmux session on the same
	// Signal loses its Drafts too; guard on session.Sessions() if both ever coexist.
	if err := dropDrafts(b); err != nil {
		return err
	}
	cmd := exec.Command(ac.Argv[0], ac.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	attrs := eventlog.BeadAttrs(b)
	if ac.SessionID != "" {
		attrs = append(attrs, "session", ac.SessionID)
	}
	slog.Info("hitl.launched", attrs...)
	readCost := claudecode.MeterCost(ac.SessionID)
	err := interactiveExec(cmd)
	cost := readCost().Attrs()
	if err == nil {
		err = publishDrafts(b.ID, attrs)
	}
	eventlog.RecordHitlExited(append(attrs, cost...), err)
	return err
}

// detachedArg, revArg and sourceArg are the hidden markers the launcher execs the
// pinned build's `loop` with (ADR 0060 §3, ADR 0090 §3): detached logs
// loop.started detached=true, rev carries the mainline SHA it was built from and
// source the source repo's git common dir it was built in, since the pinned image
// carries no stamp. A restart's exec adds lockFdArg, the inherited lock fd the new
// image adopts (ADR 0062 §5). Off-contract for humans, so usage never lists them.
// watchArg is both the user's flag (`loop [start] --watch`, `mugicrew --watch`)
// and, after --source, the marker that keeps a pinned image watched (ADR 0090 §4).
const (
	detachedArg = "--detached"
	revArg      = "--rev="
	sourceArg   = "--source="
	watchArg    = "--watch"
	lockFdArg   = "--lock-fd="
)

// sourceRoot is the source repo's absolute root, stamped in by the devShell's
// mugicrew wrapper (flake.nix mugicrewDev: -ldflags -X main.sourceRoot=…). Its
// presence alone means "from source" (ADR 0090 §1): an installed binary and a
// pinned store image are unstamped. A var so tests stamp it.
var sourceRoot string

// loopMarkers is one `loop` image's hidden argv: `loop [--detached]
// [--rev=<sha> [--source=<dir>] [--watch] [--lock-fd=N]]`. No rev is an installed
// binary's detached child, which runs as itself (ADR 0090 §2); lockFd is -1 for none.
type loopMarkers struct {
	detached, watch bool
	rev, source     string
	lockFd          int
}

// args is m as `loop`'s argv, the inverse of parseLoopMarkers.
func (m loopMarkers) args() []string {
	a := []string{"loop"}
	if m.detached {
		a = append(a, detachedArg)
	}
	if m.rev != "" {
		a = append(a, revArg+m.rev)
	}
	if m.source != "" {
		a = append(a, sourceArg+m.source)
	}
	if m.watch {
		a = append(a, watchArg)
	}
	if m.lockFd >= 0 {
		a = append(a, lockFdArg+strconv.Itoa(m.lockFd))
	}
	return a
}

// parseLoopMarkers reads `loop`'s argv (after the verb) as markers, in args'
// order; ok is false for anything else (a verb or a usage error). A bare
// --detached is an installed binary's `loop start` child. --source is optional
// so a pre-ADR-0090 image's restart, which passes none, still lands.
func parseLoopMarkers(argv []string) (m loopMarkers, ok bool) {
	m.lockFd = -1
	if len(argv) > 0 && argv[0] == detachedArg {
		m.detached, argv = true, argv[1:]
	}
	if len(argv) == 0 {
		return m, m.detached
	}
	if m.rev, ok = strings.CutPrefix(argv[0], revArg); !ok || m.rev == "" {
		return m, false
	}
	argv = argv[1:]
	if len(argv) > 0 {
		if src, ok := strings.CutPrefix(argv[0], sourceArg); ok && src != "" {
			m.source, argv = src, argv[1:]
		}
	}
	if len(argv) > 0 && argv[0] == watchArg {
		m.watch, argv = true, argv[1:]
	}
	if len(argv) > 0 {
		n, ok := strings.CutPrefix(argv[0], lockFdArg)
		fd, err := strconv.Atoi(n)
		if !ok || err != nil || fd < 0 {
			return m, false
		}
		m.lockFd, argv = fd, argv[1:]
	}
	return m, len(argv) == 0
}

// runLoopVerb routes `loop`: bare execs the foreground loop on the source's pinned
// build (an installed binary runs it in place); `start` launches it detached,
// `status` probes the single-loop lock and `stop [--force]` signals its holder
// (ADR 0053 §2, ADR 0060 §1, ADR 0090 §2). Marker-present argv is the loop image
// itself and goes straight to the loop, with no build.
// The verbs write no events, bar a bare loop or start that can't open the lock,
// build or exec (loop.failed, FR31). A repo that can't be resolved has no event
// log to write to, so that failure is plain for every verb.
func runLoopVerb(argv []string) error {
	if m, ok := parseLoopMarkers(argv); ok {
		return runLoop(m)
	}
	argv, watch, err := cutWatch(argv)
	if err != nil {
		return err
	}
	verb := strings.Join(argv, " ")
	if len(strings.Fields(verb)) != len(argv) { // an empty or spaced arg aliases another argv
		verb = "?"
	}
	run, ok := map[string]func(path, common string) error{
		"":              func(p, c string) error { return runLoopForeground(p, c, watch) },
		"start":         func(p, c string) error { return runLoopStart(p, c, watch, os.Stdout) },
		"status":        func(p, _ string) error { return runLoopStatus(p, os.Stdout) },
		"status --json": func(p, _ string) error { return runLoopStatusJSON(p, os.Stdout) },
		"stop":          func(p, _ string) error { return runLoopStop(p, false, os.Stdout) },
		"stop --force":  func(p, _ string) error { return runLoopStop(p, true, os.Stdout) },
	}[verb]
	if !ok {
		return errors.New(loopUsage)
	}
	path, common, err := resolveLoop()
	if err != nil {
		return err
	}
	return run(path, common)
}

// cutWatch strips a user's trailing --watch (Spec FR18) from `--watch` or
// `start --watch` (loop's argv, or the TUI's bare one); any other argv is returned
// as is. --watch needs the source stamp (FR19): refused here, at parse time, before
// any lock is taken or build runs.
func cutWatch(argv []string) ([]string, bool, error) {
	n := len(argv)
	if n == 0 || n > 2 || argv[n-1] != watchArg || n == 2 && argv[0] != "start" {
		return argv, false, nil
	}
	if sourceRoot == "" {
		return nil, false, errors.New("mugicrew: --watch needs a from-source mugicrew (the devShell's); an installed one never watches (ADR 0090)")
	}
	return argv[:n-1], true, nil
}

// resolveLoop is the loop's two homes: the lock, keyed on the Bead Graph
// (looplock.Resolve), and the Workspace's git common dir, under whose mugicrew/
// the rest of its state lives (loop.out, loop-build, events.jsonl, land.lock).
func resolveLoop() (path, common string, err error) {
	if common, err = git.CommonDir(); err != nil {
		return "", "", err
	}
	if path, err = looplock.Resolve(common); err != nil {
		return "", "", err
	}
	return path, common, nil
}

// spawnLoop starts the detached loop's process; a var so tests see its argv and
// SysProcAttr without spawning a loop.
var spawnLoop = func(cmd *exec.Cmd) error { return cmd.Start() }

// loopUsage is `loop`'s help: the verbs, never the hidden markers (ADR 0060 §3).
const loopUsage = `usage: mugicrew loop [--watch | start [--watch] | status [--json] | stop [--force]]
  From source (the devShell's mugicrew), loop and loop start both build and run
  the source repo's mainline as a pinned /nix/store mugicrew, whatever binary you
  typed: warm (mainline already built) ~1.5s, cold (after a mainline change) tens
  of seconds. An installed mugicrew runs the loop as itself, with no build.
  --watch (from source only; mugicrew --watch for the TUI's o) restarts the loop
  onto each new land on the source's mainline; unwatched, it stays on the build it
  started with until you stop and start it. The TUI and HITL sessions stay
  unpinned. To test loop changes, clone the source repo with your branch checked
  out as the clone's mainline, and run the clone's dev mugicrew with
  -C <scratch Workspace>: the clone is the source, the scratch repo (with its own
  bd graph) the Workspace.`

// execLoop replaces this process with argv (syscall.Exec); a var so tests
// assert the foreground launcher's exec without losing the test binary.
var execLoop = syscall.Exec

// resolveBuild builds the source repo's mainline mugicrew, pinned, from its git
// common dir with the out-link under the Workspace's, returning its rev and store
// binary (ADR 0060 §1, ADR 0090 §2); a var so tests stub it without nix.
var resolveBuild = looplock.MainlineBuild

// startWait bounds how long `loop start` waits for its child to take the lock.
const startWait = 10 * time.Second

// runLoopStart launches a detached loop (ADR 0053 §2–4, ADR 0060): it builds the
// source's pinned mugicrew and execs that store binary as `loop --detached
// --rev=<sha> --source=<dir>` (an installed binary: itself as `loop --detached`,
// ADR 0090 §2) in a new session (setsid), so the loop outlives the starter's
// terminal or TUI, with the starter's env and cwd, stdin /dev/null, and
// stdout+stderr on the Workspace's <common>/mugicrew/loop.out, truncated per
// start, never rotated.
// A held lock is refused unlogged, before any build (FR8); a start that can't
// open the lock, build or re-exec is loop.failed (FR31), with the build's error
// verbatim. It returns once the child holds the lock, or with the
// child's loop.out if it exits first — a race between two starts loses there, in
// the child's Acquire.
func runLoopStart(path, common string, watch bool, w io.Writer) error {
	m, bin, err := launchBuild(path, common, watch)
	if err != nil {
		return err
	}
	m.detached = true
	outPath := filepath.Join(common, "mugicrew", "loop.out")
	exited, pid, err := spawnDetached(outPath, bin, m.args())
	if err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
	for deadline := time.Now().Add(startWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		select {
		case err := <-exited:
			out, _ := os.ReadFile(outPath)
			return fmt.Errorf("loop failed to start (%v): %s", err, strings.TrimSpace(string(out)))
		default:
		}
		if looplock.Pid(path) == pid { // not Read: its probe could make the child's Acquire lose
			_, err := fmt.Fprintf(w, "loop started (pid %d)\n", pid)
			return err
		}
	}
	return fmt.Errorf("loop (pid %d) has not taken the lock after %s and may still be starting; see %s", pid, startWait, outPath)
}

// launchBuild is both launchers' prelude (ADR 0060 §1): a held lock is refused
// unlogged, before any build (FR8); a lock it can't read or a failed build is
// loop.failed, with nix's error verbatim (FR6, FR31). Off, a stamped binary
// returns the source mainline's pinned rev and source dir as markers, and its
// store binary, built from the source repo and rooted in the Workspace's common
// dir (ADR 0090 §2, §6). An unstamped one is a fixed tool: no build, no rev, and
// its own executable (ADR 0090 §2), so a foreign Workspace never sees nix.
func launchBuild(path, common string, watch bool) (m loopMarkers, bin string, err error) {
	m.lockFd, m.watch = -1, watch
	defer func() {
		if err != nil && !errors.As(err, new(*looplock.HeldError)) {
			slog.Error("loop.failed", "err", err.Error())
		}
	}()
	st, err := looplock.Read(path)
	if err != nil {
		return m, "", err
	}
	if st.State != looplock.Off {
		return m, "", fmt.Errorf("mugicrew: %w", &looplock.HeldError{Status: st})
	}
	if sourceRoot == "" {
		bin, err = os.Executable()
		return m, bin, err
	}
	if m.source, err = git.CommonDirOf(sourceRoot); err != nil {
		return m, "", err
	}
	m.rev, bin, err = resolveBuild(m.source, common)
	return m, bin, err
}

// runLoopForeground is bare `loop`'s launcher (ADR 0060 §1): it builds the
// source's pinned mugicrew and execs that store binary in place as `loop
// --rev=<sha> --source=<dir>`, so the pid, terminal and signals stay this
// process's; an installed binary runs the loop in place (ADR 0090 §2). Refusals
// and failures are launchBuild's, plus a failed exec (loop.failed).
func runLoopForeground(path, common string, watch bool) error {
	m, bin, err := launchBuild(path, common, watch)
	if err != nil {
		return err
	}
	if m.rev == "" {
		return runLoop(m)
	}
	err = execLoop(bin, append([]string{bin}, m.args()...), os.Environ())
	slog.Error("loop.failed", "err", err.Error()) // Exec returns only on failure
	return err
}

// spawnDetached starts `<bin> <args>` in its own session with its output on a
// truncated outPath, and reports the child's exit on the returned channel.
func spawnDetached(outPath, bin string, args []string) (<-chan error, int, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, 0, err
	}
	out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, 0, err
	}
	defer out.Close() // the child holds its own copy
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = out, out // nil Stdin is /dev/null; nil Env inherits ours
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := spawnLoop(cmd); err != nil {
		return nil, 0, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }() // reaps a child that dies before we return
	return exited, cmd.Process.Pid, nil
}

// runLoopStatus prints the loop's state as the lock reports it: off, on (pid N,
// main <short-rev>, <store path>), or draining (pid N, main <short-rev>, <store
// path>, K in flight: <titles>). An installed loop has no rev and names its
// executable; a holder that wrote neither omits both. A watched loop alone adds
// the ADR 0063 §3 displays: an on loop whose build of main failed adds a stale
// line, and a restart drain's first line adds ` → restarting onto main <short>`,
// its in-flight beads on the next; then a last line, watching <source repo>
// <mainline> (ADR 0090 §7).
func runLoopStatus(path string, w io.Writer) error {
	st, err := looplock.Read(path)
	if err != nil {
		return err
	}
	if st.State == looplock.Off || st.Pid == 0 { // pid 0: held, body not yet written
		_, err = fmt.Fprintln(w, st.State)
		return err
	}
	detail := []string{fmt.Sprintf("pid %d", st.Pid)}
	if st.Rev != "" {
		detail = append(detail, "main "+st.Rev[:min(7, len(st.Rev))])
	}
	if st.StorePath != "" {
		detail = append(detail, st.StorePath)
	}
	inflight := fmt.Sprintf("%d in flight: %s", len(st.Inflight), strings.Join(st.Inflight, ", "))
	var out string
	switch {
	case st.Watch && st.State == looplock.Draining && st.RestartTo != "":
		out = fmt.Sprintf("%s (%s) → restarting onto main %s\n%s\n", st.State, strings.Join(detail, ", "), st.RestartTo[:min(7, len(st.RestartTo))], inflight)
	case st.State == looplock.Draining:
		out = fmt.Sprintf("%s (%s)\n", st.State, strings.Join(append(detail, inflight), ", "))
	default:
		out = fmt.Sprintf("%s (%s)\n", st.State, strings.Join(detail, ", "))
		if st.Watch && st.FailedRev != "" {
			out += fmt.Sprintf("stale: build of main %s failed (see build.failed in events)\n", st.FailedRev[:min(7, len(st.FailedRev))])
		}
	}
	if st.Watch {
		out += fmt.Sprintf("watching %s %s\n", strings.TrimSuffix(st.Source, "/.git"), git.MainlineOf(st.Source))
	}
	_, err = io.WriteString(w, out)
	return err
}

// runLoopStatusJSON prints the lock's looplock.Status as one JSON object (state,
// pid, rev, store_path, watch, source, inflight, restart_to, failed_rev; empty
// ones omitted).
func runLoopStatusJSON(path string, w io.Writer) error {
	st, err := looplock.Read(path)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(st)
}

// signalLoop sends a stop signal to the loop's pid; a var so tests observe it.
var signalLoop = func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// forceWait bounds how long `loop stop` waits for the loop to act on its first
// SIGTERM (write draining) before returning, or before --force's second.
var forceWait = 10 * time.Second

// runLoopStop asks the lock holder to drain with one SIGTERM (ADR 0050); --force
// sends a second, never SIGKILL. Either waits until the loop reads draining: the
// Go runtime coalesces a repeat of a signal it has not yet delivered, and a plain
// stop that returned sooner would let a following stop read on and send what the
// loop takes as force. A restart drain is stopped like an on loop: the
// first signal turns it into a stop drain, which clears restart_to (ADR 0063 §5).
// A loop already in a stop drain is forced by the first, and nothing more is
// sent: its pid may be gone. Stopping an off loop says so and changes nothing
// (FR12). It never waits for the loop to exit.
func runLoopStop(path string, force bool, w io.Writer) error {
	st, err := looplock.Read(path)
	switch {
	case err != nil:
		return err
	case st.State == looplock.Off:
		_, err = fmt.Fprintln(w, "loop is off")
		return err
	case st.Pid == 0:
		return errors.New("loop is starting; retry")
	}
	stopDrain := func(st looplock.Status) bool { return st.State == looplock.Draining && st.RestartTo == "" }
	if err := signalLoop(st.Pid); err != nil || stopDrain(st) {
		return err
	}
	for deadline := time.Now().Add(forceWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		now, err := looplock.Read(path)
		switch {
		case err != nil:
			return err
		case now.Pid != st.Pid: // off (drained already) or another loop: nothing to force
			return nil
		case stopDrain(now) && force:
			return signalLoop(st.Pid)
		case stopDrain(now):
			return nil
		}
	}
	return fmt.Errorf("loop (pid %d) is not draining yet; a second loop stop will force it", st.Pid)
}

// runLoop starts the outer dispatch loop (spec §6). It assembles the crewmate
// ctx — the harness is wired in here, the deferred "wired in main" clause
// (§5.2) — plus the owner→crewmate table, and runs it under the single-loop lock.
// SIGINT, SIGTERM and SIGHUP all stop it: the first drains, the second forces
// (ADR 0050). Effort children run in their own process group, so a terminal's
// Ctrl-C or hangup reaches only the loop. m.detached marks a `loop start` child;
// a rev means this is the pinned build (ADR 0060 §3), so it leads PATH with its
// own bin dir before taking the lock: the reconcile child, claude -p, bd, git and
// agents' bare mugicrew all inherit it, and, watched, it follows m.source's
// mainline. No /nix/store self-check. m.lockFd (-1 for none) is the lock a restart's exec
// handed this image.
func runLoop(m loopMarkers) error {
	path, common, err := resolveLoop()
	if err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
	self, err := os.Executable() // the lock body names it, pinned or installed (ADR 0090 §7)
	if err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
	if m.rev != "" {
		if m.source == "" { // a pre-ADR-0090 launcher: self-hosted, its Workspace was its source, and it watched
			m.source, m.watch = common, true
		}
		os.Setenv("PATH", filepath.Dir(self)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	sig := make(chan os.Signal, 2) // room for drain + force arriving within one tick
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)
	l := &loop.Loop{
		Ctx:       newCtx(),
		Crew:      crew(),
		Interval:  pollInterval(),
		Rev:       m.rev,
		Bin:       self,
		SourceDir: m.source,
		CommonDir: common,
		Watch:     m.watch,
	}
	return serveLoop(path, m, self, func(onState func(loop.State)) (*loop.Restart, error) {
		return l.Run(sig, onState)
	})
}

// serveLoop runs run while holding the single-loop lock at path (ADR 0053 §1) and
// writes the loop's lifecycle events (ADR 0029): loop.started once held,
// loop.stopped with uptime on a clean exit, loop.failed when the lock can't be
// taken or run fails. A force stop (loop.ErrForced) has already logged loop.forced
// and returns at once. A lock held by another loop is refused unlogged (FR32).
// m.rev and storePath are the build the lock body names (empty when unpinned); run
// is handed the onState that maps each loop.State onto the lock body. An m.lockFd
// >= 0 is a restart's inherited lock: it is adopted, not acquired, its body is
// rewritten to this image's, and loop.started adds restarted=true (ADR 0062 §5).
// A run that returns a *loop.Restart is exec'd in place onto its build, holding
// the lock (execRestart).
func serveLoop(path string, m loopMarkers, storePath string, run func(onState func(loop.State)) (*loop.Restart, error)) error {
	var lk *looplock.Lock
	var err error
	started := []any{"detached", m.detached}
	source := "" // the lock body names a source only when watched (ADR 0090 §7)
	if m.watch {
		source = m.source
	}
	if m.lockFd < 0 {
		lk, err = looplock.Acquire(path, m.rev, storePath, source)
	} else {
		lk, err = looplock.Adopt(m.lockFd, path, m.rev, storePath, source)
		started = append(started, "restarted", true)
	}
	var held *looplock.HeldError
	if errors.As(err, &held) {
		return fmt.Errorf("mugicrew: %w", err)
	}
	if err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
	defer lk.Close()
	start := time.Now()
	slog.Info("loop.started", started...)
	r, err := run(func(s loop.State) {
		// best-effort: status only, dispatch never reads it
		lk.Write(looplock.Status{State: s.State, Rev: s.Rev, Inflight: bd.Titles(s.Inflight), RestartTo: s.RestartTo, FailedRev: s.FailedRev})
	})
	switch {
	case errors.Is(err, loop.ErrForced):
		return nil
	case err == nil && r != nil:
		err = execRestart(lk, r, m, storePath, time.Since(start))
	}
	if err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
	slog.Info("loop.stopped", "dur_ms", time.Since(start).Milliseconds())
	return nil
}

// execRestart execs r's build in place (ADR 0062 §5): same pid, terminal and
// session, as `<Bin> loop [--detached] --rev=<Rev> --source=<dir> [--watch]
// --lock-fd=N`, so each loop kind keeps its argv shape, source and watch (ADR 0090
// §3–4) and the new image adopts the lock fd, close-on-exec cleared, with the
// flock never lapsing.
// A pinned image (m.rev set) first strips the bin dir of self that runLoop put at
// the front of PATH, so the new image's own prepend keeps PATH bounded.
// loop.restarting (rev, store_path, dur_ms = uptime) is written just before the
// exec, which returns only on failure.
func execRestart(lk *looplock.Lock, r *loop.Restart, m loopMarkers, self string, uptime time.Duration) error {
	fd, err := lk.InheritFd()
	if err != nil {
		return err
	}
	env := os.Environ()
	if m.rev != "" {
		env = stripPathHead(env, filepath.Dir(self))
	}
	m.rev, m.lockFd = r.Rev, fd
	slog.Info("loop.restarting", "rev", r.Rev, "store_path", filepath.Dir(filepath.Dir(r.Bin)), "dur_ms", uptime.Milliseconds())
	return execLoop(r.Bin, append([]string{r.Bin}, m.args()...), env)
}

// stripPathHead returns env with dir dropped from the front of its PATH entry, and
// env unchanged when PATH does not lead with exactly dir. No store-path matching.
func stripPathHead(env []string, dir string) []string {
	for i, kv := range env {
		path, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		head, rest, _ := strings.Cut(path, string(os.PathListSeparator))
		if head != dir {
			return env
		}
		out := slices.Clone(env)
		out[i] = "PATH=" + rest
		return out
	}
	return env
}

// pollInterval is the §6.2 cadence with its one env override
// (MUGICREW_POLL_INTERVAL, a Go duration); default 15s.
// ponytail: env is the single override; a flag can join it if a use appears.
func pollInterval() time.Duration {
	if v := os.Getenv("MUGICREW_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		fmt.Fprintf(os.Stderr, "mugicrew: ignoring invalid MUGICREW_POLL_INTERVAL %q\n", v)
	}
	return loop.DefaultInterval
}

// runReconcile is the worktree re-entry verb (spec §8.2): the AFK dispatch
// subprocess and a charting/slicing agent both run it to drive a bead to its
// conformant next state. It resolves the owner from the kind via crewFor and
// dispatches through Crewmate.Run — NOT a hardcoded nami.Reconcile — so an AFK
// worktree kind whose agentic path lives in Run (research → runResearch) can
// re-enter here at all, not just kinds with a Reconcile handler (FR4).
// --spec <body-file> [--title <t>] is the Signal fast lane (ADR 0035 §10): it only
// seeds the Signal's Spec successor (nami.SeedSpec); the reconcile that follows
// takes whichever leg the graph then shows.
func runReconcile(argv []string) error {
	const usage = "usage: mugicrew reconcile <id> [--spec <body-file> [--title <t>]]"
	if len(argv) < 1 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	specFile := fs.String("spec", "", "fast lane: file holding the Spec body")
	title := fs.String("title", "", "fast lane: Spec title, no kind prefix (default: the signal title)")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || (*title != "" && *specFile == "") {
		return errors.New(usage)
	}
	bead, err := bd.Show(argv[0])
	if err != nil {
		return err
	}
	if *specFile != "" {
		if bead.Kind != contract.KindSignal {
			return fmt.Errorf("%s: --spec is valid only on a signal, %s is kind %q", usage, bead.ID, bead.Kind)
		}
		body, err := os.ReadFile(*specFile)
		if err != nil {
			return err
		}
		if err := nami.SeedSpec(bead, *title, string(body)); err != nil {
			return err
		}
	}
	c, err := crewFor(bead.Kind)
	if err != nil {
		return err
	}
	path := os.Getenv(dispatch.PublishEnv)
	os.Unsetenv(dispatch.PublishEnv) // a nested reconcile (an agent's) is not the dispatch child
	return dispatch.Reconcile(newCtx(), c, bead, path)
}

// crewFor resolves the crewmate that owns a kind through the same registry the
// loop dispatches by (registry.Lookup → crew() table), so the worktree reconcile
// verb and the in-process loop can't drift on who owns a kind. A registered kind
// with no crew() entry is a loud skip-able error, not a crash.
func crewFor(kind contract.Kind) (contract.Crewmate, error) {
	e, err := registry.Lookup(kind)
	if err != nil {
		return nil, err
	}
	c, ok := crew()[e.Owner]
	if !ok {
		return nil, fmt.Errorf("reconcile: no crewmate owns kind %q", kind)
	}
	return c, nil
}

// runPivotConfirm is the confirm sweep verb the pivot triage session calls (ADR
// 0046 §5): it files the corrected Spec body the session wrote with the human and
// retires the old Checkpoint (zoro.Confirm). Idempotent, so the session re-runs it
// after a partial failure. --title defaults to the old Spec's.
func runPivotConfirm(argv []string) error {
	const usage = "usage: mugicrew pivot confirm <pivot-id> --spec <body-file> [--title <t>]"
	if len(argv) < 1 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("pivot confirm", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	specFile := fs.String("spec", "", "file holding the corrected Spec body")
	title := fs.String("title", "", "corrected Spec title (default: the old Spec's)")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || *specFile == "" {
		return errors.New(usage)
	}
	body, err := os.ReadFile(*specFile)
	if err != nil {
		return err
	}
	return zoro.Confirm(argv[0], *title, string(body))
}

// runPivotEscalate is the escalate sweep verb the pivot triage session calls (ADR
// 0046 §5): it seeds a fresh Chart with the evidence file the session gathered
// and retires the old Checkpoint (zoro.Escalate). Idempotent, like confirm.
func runPivotEscalate(argv []string) error {
	const usage = "usage: mugicrew pivot escalate <pivot-id> --evidence <file>"
	if len(argv) < 1 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("pivot escalate", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	evidenceFile := fs.String("evidence", "", "file holding the escalation evidence")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || *evidenceFile == "" {
		return errors.New(usage)
	}
	evidence, err := os.ReadFile(*evidenceFile)
	if err != nil {
		return err
	}
	return zoro.Escalate(argv[0], string(evidence))
}

// runSignalNew files a Signal via the bd intake helper (spec §7.1). Title +
// description are the only fields; it prints the new bead ID.
func runSignalNew(argv []string) error {
	fs := flag.NewFlagSet("signal new", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once; don't let flag print it too
	title := fs.String("title", "", "signal title (required)")
	description := fs.String("description", "", "signal description (required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *title == "" || *description == "" {
		return errors.New("signal new: --title and --description are required")
	}

	id, err := bd.NewSignal(*title, *description)
	if err != nil {
		return err
	}
	fmt.Println(id)
	return nil
}

// runSurgeryNew files a Surgery and its own Checkpoint (Spec D3) and prints
// the Surgery id. It launches nothing: the Surgery starts idle, and l in the TUI
// opens its first session (ADR 0095 §6).
func runSurgeryNew(argv []string) error {
	const usage = "surgery new: --title is required (usage: mugicrew surgery new --title T [--problem P])"
	fs := flag.NewFlagSet("surgery new", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once; don't let flag print it too
	title := fs.String("title", "", "surgery title (required)")
	problem := fs.String("problem", "", "one-line problem (optional)")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *title == "" {
		return errors.New(usage)
	}

	id, cp, err := bd.NewSurgery(*title, *problem)
	if err != nil {
		if id != "" || cp != "" {
			return fmt.Errorf("surgery new: filed Checkpoint %q, Surgery %q before: %w", cp, id, err)
		}
		return err
	}
	fmt.Println(id)
	return nil
}

// runIntegrate is `mugicrew integrate` (Spec D3): no arguments, the bead
// read off the cwd's effort/<id> branch; it returns session.Integrate's exit code
// (0 integrated or nothing ahead, 2 conflict, 1 transient). by is backstop for the
// Go backstop's child, agent inside a dispatched effort or a HITL pane, else human.
func runIntegrate(argv []string) int {
	if len(argv) != 0 {
		fmt.Fprintln(os.Stderr, "usage: mugicrew integrate (run in a tree on effort/<bead-id>)")
		return session.ExitTransient
	}
	id, err := git.CurrentEffort()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return session.ExitTransient
	}
	b, err := bd.Show(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return session.ExitTransient
	}
	by := eventlog.ByHuman
	switch {
	case os.Getenv(session.BackstopEnv) != "":
		by = eventlog.ByBackstop
	case os.Getenv(eventlog.EffortEnv) != "" || session.InPane():
		by = eventlog.ByAgent
	}
	return session.Integrate(b, by, os.Stdout)
}

// editChart is the locked Chart-body edit, a var so the CLI's tests stub it.
var editChart = bd.EditChartBody

// runChart is `mugicrew chart append|drop <chart> <section> <line>` (Spec D9):
// a defog session's one-line Chart-body edit under the land lock.
func runChart(argv []string) error {
	if len(argv) != 4 || argv[0] != "append" && argv[0] != "drop" {
		return errors.New("usage: mugicrew chart append|drop <chart-id> <section> <line>")
	}
	return editChart(argv[1], argv[2], argv[3], argv[0] == "drop")
}

// runSurgeryEnd ends a Surgery (Spec D5): a thin shim over franky.EndSurgery,
// which Franky's surgery prompt runs after its clean-tree and green-gate checks.
func runSurgeryEnd(argv []string) error {
	if len(argv) != 1 || argv[0] == "" {
		return errors.New("usage: mugicrew surgery end <surgery-id>")
	}
	return franky.EndSurgery(argv[0])
}
