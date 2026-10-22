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
// `abort <bead-id>` stops one bead's in-flight effort, leaving it paused (ADR 0083);
// `recover` lists the beads dead efforts stranded; `recover <bead-id> [--force] [--discard]` repairs one (ADR 0127).
// A leading `-C <dir>`, before any verb or none, runs it on that Workspace (ADR 0089).
// A user verb refuses when bd resolves no Bead Graph, and exports it as BEADS_DIR.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"jayulabs.org/mugicrew/agentprofile"
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
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/brook"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/franky"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jinbe"
	"jayulabs.org/mugicrew/jira"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/luffy"
	"jayulabs.org/mugicrew/nami"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/robin"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/sanji"
	"jayulabs.org/mugicrew/session"
	"jayulabs.org/mugicrew/tui"
	"jayulabs.org/mugicrew/usopp"
	"jayulabs.org/mugicrew/withdraw"
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
	if len(os.Args) == 4 && os.Args[1] == "bead-detail" { // hidden: the F10 bead pane's detail text
		os.Exit(beadDetail(os.Args[2], os.Args[3], os.Stdout, os.Stderr))
	}
	leadPathWithSelf()
	args, err := workspaceArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Println(usage) // before the gate and the event log: help works anywhere, logs nothing
		return
	}
	if len(args) == 1 && args[0] == "--version" { // a flag, not a verb: before the gate, logs nothing
		fmt.Println(versionString())
		return
	}
	watch := false
	if len(args) == 1 { // the TUI's `mugicrew --watch`
		if args, watch, err = cutWatch(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(args) > 0 && args[0] == "init" { // bootstraps the graph, so before the gate and the event log; like doctor it is no graph-required verb
		os.Exit(runInit(args[1:], os.Stdin, os.Stdout))
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

	if args[0] == "doctor" { // before the event log opens: bare doctor is read-only, doctor fix writes no event (ADR 0301 §5)
		os.Exit(runDoctorCmd(args[1:], os.Stdout))
	}
	if args[0] == "hitl-follow" { // hidden: hitl-run's detached follower (ADR 0254); its log path is argv, not its cwd's repo
		if err := runHitlFollow(args[1:], followWait, os.Stderr); err != nil {
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
	case args[0] == "signal" && len(args) >= 2 && args[1] == "offshoot":
		err = runSignalOffshoot(args[2:], os.Stdout)
	case args[0] == "task" && len(args) >= 2 && args[1] == "new":
		err = runTaskNew(args[2:], os.Stdout)
	case args[0] == "intake" && len(args) >= 2 && args[1] == "jira":
		err = runIntakeJira(args[2:], os.Stdout)
	case args[0] == "latch" && len(args) >= 2 && args[1] == "jira":
		err = runLatchJira(args[2:], os.Stdout)
	case args[0] == "latch" && len(args) >= 2 && args[1] == "mr":
		err = runLatchMR(args[2:], os.Stdout)
	case args[0] == "triage" && len(args) >= 2 && args[1] == "locate":
		err = runTriageLocate(args[2:], os.Stdout)
	case args[0] == "probe":
		err = runProbe(args[1:], os.Stdout)
	case args[0] == "surgery" && len(args) >= 2 && args[1] == "new":
		err = runSurgeryNew(args[2:])
	case args[0] == "surgery" && len(args) >= 2 && args[1] == "end":
		err = runSurgeryEnd(args[2:])
	case args[0] == "pivot" && len(args) >= 2 && args[1] == "confirm":
		err = runPivotConfirm(args[2:])
	case args[0] == "pivot" && len(args) >= 2 && args[1] == "escalate":
		err = runPivotEscalate(args[2:])
	case args[0] == "pivot" && len(args) >= 2 && args[1] == "raise":
		err = runPivotRaise(args[2:], os.Stdout)
	case args[0] == "initiative":
		err = runInitiative(args[1:])
	case args[0] == "withdraw":
		err = runWithdraw(args[1:])
	case args[0] == "reconcile":
		err = runReconcile(args[1:])
	case args[0] == "loop":
		err = runLoopVerb(args[1:])
	case args[0] == "abort":
		err = runAbort(args[1:], os.Stdout)
	case args[0] == "run": // hidden: a run-now effort's detached host (ADR 0330)
		os.Exit(runRun(args[1:]))
	case args[0] == "recover":
		os.Exit(runRecover(args[1:], os.Stdout, os.Stderr))
	case args[0] == "checkup":
		os.Exit(runCheckup(args[1:], os.Stderr))
	case args[0] == "ps":
		err = runPs(args[1:], os.Stdout)
	case args[0] == "stats":
		err = runStats(args[1:], os.Stdout)
	case args[0] == "integrate":
		os.Exit(runIntegrate(args[1:]))
	case args[0] == "rebase":
		os.Exit(runRebase(args[1:], os.Stdout, os.Stderr))
	case args[0] == "chart":
		err = runChart(args[1:])
	case args[0] == "approval":
		err = runApproval(args[1:], os.Stdout)
	case args[0] == "land-lineage":
		err = runLandLineage(args[1:], os.Stdout)
	case args[0] == "hitl-run": // hidden: the tmux pane's wrapper (ADR 0037)
		os.Exit(session.HitlRun(args[1:], claudecode.MeterCost))
	case args[0] == "transcript":
		err = runTranscript(args[1:], os.Stdout)
	case args[0] == "doc-pane": // hidden: the HITL doc pane's viewer (ADR 0237)
		err = runDocPane(args[1:])
	default:
		err = fmt.Errorf("mugicrew: unknown command %v (see mugicrew --help)", args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := 1
		if ec := (exitCode{}); errors.As(err, &ec) {
			code = ec.code
		}
		os.Exit(code)
	}
}

// usage is mugicrew's top-level help: the user-facing verbs, never the hidden
// ones or the loop's markers (ADR 0060 §3, ADR 0037, ADR 0099).
const usage = `usage: mugicrew [-C <dir>] [--watch | <verb> ...]
  mugicrew                    open the TUI
  mugicrew --watch            open the TUI; its o starts a watched loop
  mugicrew -C <dir> ...       run in the Workspace containing <dir>
  mugicrew signal new --title T --description D            file a Signal
  mugicrew signal offshoot --from <cause> --drafted-by <bead> --order follow-up|prerequisite|none --title T --description D  file an offshoot Signal ordered against its cause's container
  mugicrew task new --drafted-by <bead> [--after <bead>]... [--before <bead>]... [--outside] --title T --description D  file a Task in the Initiative of the member it is ordered against
  mugicrew intake jira <KEY>                                file a Jira work item as a Signal
  mugicrew latch jira <KEY> --blocks <bead>...             hold beads until a Jira ticket is Done
  mugicrew latch mr <ref> --blocks <bead>...               hold beads until a GitLab merge request is merged
  mugicrew signal triage <id>                               triage a Signal in an interactive session
  mugicrew triage locate [--signal <id>] --token <t>... --path <p>... [--json]  say whether a Signal's code is only on one live Checkpoint
  mugicrew surgery new --title T [--problem P] [--checkpoint ID]  file a Surgery on a fresh Checkpoint, or join ID
  mugicrew surgery end <surgery-id>                         end a Surgery
  mugicrew probe mr (<ref> | --branch <b>) | pipeline <ref> (<id> | --main) | landed <id> | foreign [--context <c>]  print one word of MR or pipeline state, read-only
  mugicrew checkup <bead> [--problem "<one line>"]          repair a broken bead in a Chopper session
  mugicrew pivot confirm <pivot-id> --spec <body-file> [--title <t>] [--carries <id,…>]  file a pivot's corrected Spec
  mugicrew pivot escalate <pivot-id> --evidence <file> [--carries <id,…>]  re-chart a pivot from its evidence
  mugicrew pivot raise <task> [--pivot <f>] [--critique <f>] [--merge-into <pivot> <f>]  probe a Task's held Checkpoint, or raise a pivot or Critique on it
  mugicrew withdraw <head> [--discard]  drop any unclosed chain head or Task (initiative withdraw is its alias)
  mugicrew initiative cancel <initiative> [--release <head>]... [--discard]  drop an Initiative and its member chains
  mugicrew initiative adopt <head> <initiative> | release <head>  move a chain into or out of an Initiative
  mugicrew reconcile <id> [--spec <body-file> | --initiative <body-file>] [--title <t>]  drive a bead to its next state
  mugicrew reconcile <signal> --checkpoint <cp> [--pivot <f>]... [--critique <f>]... [--merge-into <pivot> <f>]...  reroute a Signal onto its host Checkpoint
  mugicrew loop [--watch | start [--watch] | status [--json] | stop [--force]]  run or control the dispatch loop
  mugicrew abort <bead-id>                                  abort a bead's running effort
  mugicrew recover [<bead-id> [--force] [--discard]]        list stranded beads, or repair one
  mugicrew ps [--json]                                      list active AFK efforts
  mugicrew stats [<bead>] [--spine] [--since <dur|date>] [--by crewmate|model|profile|kind|day] [--kind <kind>] [--json]  show session cost and agent time
  mugicrew transcript [--no-follow] [--session <prefix> | --last] [--no-pager] <bead>  read every session a bead ran
  mugicrew init [--prefix <p>]                              bootstrap this clone's server-mode bd store (never commits)
  mugicrew init workspace [--land-strategy local|gitlab] [--require-initiatives <bool>] [--external-ref <bool>] [--jira-site <host>] [--context name=[repo:|path:]dir]...  write the Workspace's shared settings to .mugicrew/config.toml in a linked worktree (never commits)
  mugicrew doctor                                           check the environment and Workspace are ready (read-only)
  mugicrew doctor fix                                       apply doctor's safe fixes, then re-check
  mugicrew integrate                                        squash this effort branch onto its Checkpoint
  mugicrew rebase <chart-or-checkpoint-id>                  file a rebase of an open Chart or Checkpoint onto mainline
  mugicrew chart append|drop <chart-id> <section> <line>    edit one line of a Chart section
  mugicrew approval rework <approval> --critique <file>...  file Critiques and retire the Approval for rework
  mugicrew land-lineage <branch>                            fast-forward main onto a lineage branch`

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
// signal new/triage, surgery new/end, checkup, integrate, chart append|drop, loop and its start/status/stop) resolves the Bead Graph from
// the Workspace root and refuses when none resolves. On success the graph's
// absolute dir becomes this process's BEADS_DIR, so every child it spawns (the
// detached loop, reconcile, tmux sessions, claude -p) hits that graph from any
// cwd with no per-call plumbing (FR8, FR9). Internal verbs (reconcile, pivot,
// abort, ps) only inherit it, as do the loop's hidden re-exec markers unless they
// inherited none: a pre-ADR-0089 loop's restart onto this image.
func requireGraph(args []string) error {
	user := len(args) == 0 ||
		args[0] == "loop" && (len(args) == 1 || args[1] == watchArg || !strings.HasPrefix(args[1], "--") || os.Getenv("BEADS_DIR") == "") ||
		args[0] == "signal" && len(args) >= 2 && (args[1] == "new" || args[1] == "triage" || args[1] == "offshoot") ||
		args[0] == "task" && len(args) >= 2 && args[1] == "new" ||
		args[0] == "triage" && len(args) >= 2 && args[1] == "locate" ||
		args[0] == "probe" ||
		args[0] == "intake" && len(args) >= 2 && args[1] == "jira" ||
		args[0] == "latch" && len(args) >= 2 && (args[1] == "jira" || args[1] == "mr") ||
		args[0] == "surgery" && len(args) >= 2 && (args[1] == "new" || args[1] == "end") ||
		args[0] == "checkup" || args[0] == "integrate" || args[0] == "rebase" || args[0] == "chart" || args[0] == "approval"
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

// inheritGraph is the non-refusing counterpart of requireGraph for the internal
// verbs ps and abort: a bare shell has no BEADS_DIR, so resolve the graph the
// dispatcher used and export it, else liveness reads the wrong lock dir. A graph
// that doesn't resolve is left to the verb's own bd call to report.
func inheritGraph() {
	if os.Getenv("BEADS_DIR") != "" {
		return
	}
	if root, err := git.RepoRoot(); err == nil {
		if dir, err := graphDir(root); err == nil {
			os.Setenv("BEADS_DIR", dir)
		}
	}
}

// abortEffort is the shared abort (ADR 0083), a var so the CLI's tests stub it.
var abortEffort = dispatch.Abort

// runAbort is `mugicrew abort <bead-id>`: no prompt (typing the verb is the
// confirmation); it prints the pids signalled, or that the bead was only paused.
func runAbort(argv []string, w io.Writer) error {
	if len(argv) != 1 {
		return errors.New("usage: mugicrew abort <bead-id>")
	}
	inheritGraph()
	pids, held, err := abortEffort(argv[0], "cli")
	if len(pids) > 0 {
		fmt.Fprintln(w, "signalled", dispatch.FormatPids(pids))
	} else if err == nil {
		fmt.Fprintln(w, dispatch.AbortNote(held))
	}
	return err
}

// recoverBead is Chopper's repair (ADR 0127) and listStranded its listing, vars so
// the CLI's tests stub them.
var (
	recoverBead  = chopper.Recover
	listStranded = chopper.List
)

// runRecover is `mugicrew recover [<bead-id> [--force] [--discard]]`. With no
// argument it prints the stranded beads, a pure view. With a bead there is no
// prompt (typing the verb is the confirmation): it prints one line per step that
// acted, then any pause left, and exits 0 repaired or nothing to recover, 1
// refused or a usage error, 2 a failed step (ADR 0127 §9).
func runRecover(argv []string, w, errw io.Writer) int {
	loop.StampAutoApprove()
	if len(argv) == 0 {
		if err := listStranded(w); err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		return 0
	}
	fs := flag.NewFlagSet("recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	discard := fs.Bool("discard", false, "drop a dirty tree's uncommitted files")
	force := fs.Bool("force", false, "override an ambiguous verdict; refused while the loop is on")
	var id string
	err := fs.Parse(argv)
	if err == nil && fs.NArg() > 0 { // the id may sit before the flags
		id = fs.Arg(0)
		err = fs.Parse(fs.Args()[1:])
	}
	if err != nil || id == "" || fs.NArg() > 0 {
		fmt.Fprintln(errw, "usage: mugicrew recover [<bead-id> [--force] [--discard]]")
		return 1
	}
	r, err := recoverBead(id, chopper.Opts{Discard: *discard, Force: *force})
	for _, l := range r.Lines {
		fmt.Fprintln(w, l)
	}
	if err == nil && r.Verdict == "" {
		fmt.Fprintln(w, id+": nothing to recover")
	}
	if err == nil && len(r.Remaining) > 0 {
		fmt.Fprintln(w, "still paused:", strings.Join(r.Remaining, ", "))
	}
	var f *chopper.Failed
	switch {
	case err == nil:
		return 0
	case errors.As(err, &f):
		fmt.Fprintln(errw, err)
		return 2
	}
	fmt.Fprintln(errw, err)
	return 1
}

// runLandLineage is `mugicrew land-lineage <branch>` (Spec D1): it
// fast-forwards main onto a lineage branch under the land lock and prints the
// landed sha, or under gitlab opens (or reuses) the branch's MR and prints its
// URL (ADR 0210). CLI only; CommonDir finds the lock from main or any worktree.
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

// ForKind re-resolves kind's Agent Profile at every effort (ADR 0125), so
// dispatch runs it on a backend built for that profile.
func (c appCtx) ForKind(kind contract.Kind) (contract.Ctx, string, error) {
	root, err := git.RepoRoot()
	if err != nil {
		return nil, "", err
	}
	return ctxForKind(root, kind)
}

// ForProfile resolves a recorded Agent Profile afresh for a HITL resume (ADR
// 0122 §6), ignoring the mapping.
func (c appCtx) ForProfile(name string) (contract.Ctx, error) {
	root, err := git.RepoRoot()
	if err != nil {
		return nil, err
	}
	return ctxForProfile(root, name)
}

// ProfileReasons checks the merged profiles' runnability in one batch (FR44).
func (c appCtx) ProfileReasons(root string, declared map[string]agentprofile.Entry) map[string]string {
	return profileReasons(root, declared)
}

var _ dispatch.Profiled = appCtx{}

// newCtx wires the built-in backend (spec §5.2): claudecode with no --model,
// Claude Code's native resolution. An AFK effort or HITL launch swaps in its
// profile's backend through ForKind, a HITL resume through ForProfile.
func newCtx() appCtx { return appCtx{harness: claudecode.New("")} }

// crew is the owner→crewmate table the TUI and loop dispatch through (spec §5.2):
// Nami, Zoro, Sanji, Franky, Usopp, Luffy, and Jinbe are wired — each key matches
// its registry owner.
func crew() map[string]contract.Crewmate {
	return map[string]contract.Crewmate{
		"nami":    nami.Nami{},
		"zoro":    zoro.Zoro{},
		"sanji":   sanji.Sanji{},
		"franky":  franky.Franky{},
		"usopp":   usopp.Usopp{},
		"luffy":   luffy.Luffy{},
		"jinbe":   jinbe.Jinbe{},
		"brook":   brook.Brook{},
		"robin":   robin.Robin{},
		"chopper": chopper.Chopper{},
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
			return launchTUI(watchArg)
		}
		return launchTUI()
	}
	if err := session.ApplyTUIEnv(); err != nil {
		return err
	}
	leadPathWithSelf() // ApplyTUIEnv reloaded the launcher's PATH
	// The alt-screen owns the terminal: the TUI surfaces the event log's one-time
	// warning on its status line (Log.Warning) instead of stderr.
	evlog := eventlog.Open(nil)
	slog.SetDefault(evlog.Logger("tui"))
	defer startPprof(os.Stderr)()
	_, err = tea.NewProgram(tui.New(newCtx(), crew()).WithLog(evlog).WithUsage(claudecode.ReadUsage).WithWatch(watch), tea.WithAltScreen()).Run()
	return err
}

// launchTUI is the self-launch onto the private server; a var so main's tests
// see the pane's argv without a tmux.
var launchTUI = session.LaunchTUI

// runSignalTriage resolves a Signal and opens its owner's triage session in the
// human's terminal (spec §7.2). The Signal's registry owner, Luffy (ADR 0129),
// formats the command via the harness; this verb runs it interactively — the
// second Launch invoker alongside the TUI. The six outcomes happen inside the
// session, not here.
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
	return triage(newCtx(), bead)
}

// triage launches a Signal bead through its registry owner (crewFor) on its
// kind's Agent Profile, resolved like the TUI's HITL launch (ADR 0125): an
// unrunnable profile is refused with nothing exec'd.
func triage(p dispatch.Profiled, bead bd.Bead) error {
	ctx, profile, err := p.ForKind(bead.Kind)
	if err := tui.RefuseProfile(bead, profile, "hitl", err); err != nil {
		return err
	}
	c, err := crewFor(bead.Kind)
	if err != nil {
		return err
	}
	lc, err := c.Launch(contract.WithClosingLine(ctx), contract.Bead{ID: bead.ID, Kind: bead.Kind})
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
	return runInteractive(ac, cwd, bead, profile)
}

// runInteractive execs a launch command in the human's TTY (their shell supplies
// stdin/out/err) with cwd as its working directory, the running half of the triage
// verb (spec §7.2). Thin wiring over os/exec — the argv is composed and tested in
// the harness; this only sets the cwd and hands the process the terminal. Like the
// TUI launch, it brackets the session with hitl.launched / hitl.exited (Spec FR29)
// — events go to the log file, never the TTY streams — and ends it
// like hitl-run: a dead run's leftover Drafts are deleted before the launch, and a
// clean exit publishes the session's Drafts (ADR 0059 §4, §6). A failed exit keeps
// them; the next launch deletes them. The profile's env (ac.Env) goes on top of
// ours, never into argv, and a named profile ("" = the built-in) is on hitl.*.
func runInteractive(ac contract.AgentCommand, cwd string, b bd.Bead, profile string) error {
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
	if len(ac.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range ac.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	attrs := eventlog.BeadAttrs(b)
	if ac.SessionID != "" {
		attrs = append(attrs, "session", ac.SessionID)
	}
	if profile != "" {
		attrs = append(attrs, "profile", profile)
	}
	slog.Info("hitl.launched", attrs...)
	readCost := claudecode.MeterCost(ac.SessionID)
	err := interactiveExec(cmd)
	metered := readCost()
	cost, follow := session.Handoff(attrs, metered, spawnFollower)
	if err == nil {
		err = publishDrafts(b.ID, attrs)
	}
	follow()
	eventlog.RecordHitlExited(append(attrs, cost...), err)
	return err
}

// spawnFollower is session.SpawnFollower; a var so tests spawn nothing.
var spawnFollower = session.SpawnFollower

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

// version is the npm release, stamped by CI (-ldflags -X main.version=…)
// independently of sourceRoot, so an npm build stays an installed binary.
var version string

func versionString() string {
	if version == "" {
		return "unknown"
	}
	return version
}

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
		"restart":       func(p, c string) error { return runLoopRestart(p, c, os.Stdout) },
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
const loopUsage = `usage: mugicrew loop [--watch | start [--watch] | status [--json] | stop [--force] | restart]
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
  bd graph) the Workspace.
  restart (from source, watched or not) builds main's latest and restarts the loop
  onto it, returning once the restart drain starts; it never waits out the drain.`

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
// A held lock is refused unlogged, before any build (FR8), except a stop drain,
// which it cancels instead (cancelDrain, ADR 0236); a restart drain is still
// refused. A start that can't
// open the lock, build or re-exec is loop.failed (FR31), with the build's error
// verbatim. It returns once the child holds the lock, or with the
// child's loop.out if it exits first — a race between two starts loses there, in
// the child's Acquire.
func runLoopStart(path, common string, watch bool, w io.Writer) error {
	if st, err := looplock.Read(path); err == nil && st.Pid != 0 && st.StopDrain() {
		if resumed, err := cancelDrain(path, st.Pid, w); resumed || err != nil {
			return err
		}
	}
	if err := git.PreflightGitlab(); err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
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

// cancelLoop sends the cancel signal to a stop-draining loop; a var so tests observe it.
var cancelLoop = func(pid int) error { return syscall.Kill(pid, syscall.SIGUSR1) }

// cancelDrain asks the stop-draining loop at pid to resume (ADR 0236) and waits
// up to forceWait for the lock to leave the stop drain. It builds and spawns
// nothing and writes no event: the loop is the single writer of lifecycle
// events. resumed is false when the loop went off (or another took the lock), so
// the caller starts fresh; ESRCH is no failure, the poll below sees the lock off.
func cancelDrain(path string, pid int, w io.Writer) (resumed bool, err error) {
	if err := cancelLoop(pid); err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, err
	}
	for deadline := time.Now().Add(forceWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		now, err := looplock.Read(path)
		switch {
		case err != nil:
			return false, err
		case now.Pid != pid:
			return false, nil
		case !now.StopDrain():
			_, err = fmt.Fprintf(w, "loop resumed (pid %d)\n", pid)
			return true, err
		}
	}
	return false, fmt.Errorf("loop (pid %d) did not leave its drain after %s; a loop built before cancel ignores it — stop and start it", pid, forceWait)
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
	if err := git.PreflightGitlab(); err != nil {
		slog.Error("loop.failed", "err", err.Error())
		return err
	}
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

// runLoopStatus prints the loop's state as the lock reports it (Status.Text).
func runLoopStatus(path string, w io.Writer) error {
	st, err := looplock.Read(path)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, st.Text())
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

// restartLoop asks a from-source loop to build main and restart onto it (ADR 0289);
// a var so tests observe it.
var restartLoop = func(pid int) error { return syscall.Kill(pid, syscall.SIGUSR2) }

func errStopping(pid int) error {
	return fmt.Errorf("loop (pid %d) is stopping; loop start cancels the stop drain", pid)
}

func errNotFromSource(pid int) error {
	return fmt.Errorf("loop (pid %d) is not from source; stop and start it to change builds", pid)
}

var errLoopOff = errors.New("loop is off; loop start runs main's latest build")

// runLoopRestart is `loop restart` (ADR 0289 §5, ADR 0288 §5): it refuses an off,
// installed or stop-draining loop unsignalled, reports a restart drain already
// under way, else sends SIGUSR2 and tails the event log from before the signal for
// the holder's answer. It returns once the restart drain starts, never waiting out
// the drain, and writes no events. The ack is bounded by forceWait; the build wait
// is not, but ends if the lock leaves the pid.
func runLoopRestart(path, common string, w io.Writer) error {
	short := looplock.Short
	st, err := looplock.Read(path)
	switch {
	case err != nil:
		return err
	case st.State == looplock.Off:
		return errLoopOff
	case st.Pid == 0:
		return errors.New("loop is starting; retry")
	case st.Rev == "":
		return errNotFromSource(st.Pid)
	case st.StopDrain():
		return errStopping(st.Pid)
	case st.State == looplock.Draining:
		_, err = fmt.Fprintf(w, "loop is already restarting onto main %s (pid %d, %d in flight)\n", short(st.RestartTo), st.Pid, len(st.Inflight))
		return err
	}
	evPath := filepath.Join(common, "mugicrew", "events.jsonl")
	_, _, mark, _, err := eventlog.ReadFrom(evPath, eventlog.State{})
	if err != nil {
		return err
	}
	if err := restartLoop(st.Pid); err != nil && !errors.Is(err, syscall.ESRCH) { // ESRCH: the lock check below says off
		return err
	}
	pid := strconv.Itoa(st.Pid)
	acked := false
	for deadline := time.Now().Add(forceWait); ; time.Sleep(20 * time.Millisecond) {
		evs, _, next, _, err := eventlog.ReadFrom(evPath, mark)
		if err != nil {
			return err
		}
		mark = next
		for _, ev := range evs {
			if ev.Pid != pid {
				continue
			}
			attr := func(k string) string { return fmt.Sprint(ev.Raw[k]) }
			switch ev.Msg {
			case "restart.requested":
				acked = true
				switch attr("outcome") {
				case "current":
					_, err = fmt.Fprintf(w, "loop is already on main %s\n", short(st.Rev))
					return err
				case "draining":
					now, err := looplock.Read(path)
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(w, "loop is already restarting onto main %s (pid %d, %d in flight)\n", short(now.RestartTo), st.Pid, len(now.Inflight))
					return err
				case "stopping":
					return errStopping(st.Pid)
				case "unsourced":
					return errNotFromSource(st.Pid)
				}
			case "restart.current":
				_, err = fmt.Fprintf(w, "loop is already on main %s\n", short(attr("rev")))
				return err
			case "build.failed":
				return fmt.Errorf("build of main %s failed (see build.failed in events)", short(attr("rev")))
			case "loop.draining":
				if _, ok := ev.Raw["restart_to"]; !ok { // a stop took over
					return errStopping(st.Pid)
				}
				_, err = fmt.Fprintf(w, "loop restarting onto main %s (pid %d, %s in flight)\n", short(attr("restart_to")), st.Pid, attr("inflight"))
				return err
			}
		}
		if now, err := looplock.Read(path); err != nil {
			return err
		} else if now.Pid != st.Pid {
			return errLoopOff
		}
		if !acked && time.Now().After(deadline) {
			return fmt.Errorf("loop (pid %d) did not answer the restart request (an old build); stop and start it", st.Pid)
		}
	}
}

// signalLoop sends a stop signal to the loop's pid; a var so tests observe it.
var signalLoop = func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// forceWait bounds how long `loop stop` waits for the loop to act on its first
// SIGTERM (write draining) before returning, or before --force's second.
var forceWait = 10 * time.Second

// runLoopStop asks the lock holder to drain with one SIGTERM (ADR 0050); --force
// sends a second, never SIGKILL, no sooner than loop.StopWindow after the first
// (the loop ignores a repeat inside it). Either waits until the loop reads
// draining: the Go runtime coalesces a repeat of a signal it has not yet
// delivered, and a plain stop that returned sooner would let a following stop
// read on and send what the loop takes as force. A restart drain is stopped
// like an on loop: the first signal turns it into a stop drain, which clears
// restart_to (ADR 0063 §5).
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
	if err := signalLoop(st.Pid); err != nil || st.StopDrain() {
		return err
	}
	for deadline := time.Now().Add(forceWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		now, err := looplock.Read(path)
		switch {
		case err != nil:
			return err
		case now.Pid != st.Pid: // off (drained already) or another loop: nothing to force
			return nil
		case now.StopDrain() && force:
			time.Sleep(loop.StopWindow) // draining was written after the loop took its window start; a second signal sooner is no force to the loop
			return signalLoop(st.Pid)
		case now.StopDrain():
			return nil
		}
	}
	return fmt.Errorf("loop (pid %d) is not draining yet; a second loop stop will force it", st.Pid)
}

// runLoop starts the outer dispatch loop (spec §6). It assembles the crewmate
// ctx — the harness is wired in here, the deferred "wired in main" clause
// (§5.2) — plus the owner→crewmate table, and runs it under the single-loop lock.
// SIGINT, SIGTERM and SIGHUP all stop it: the first drains, the second forces
// if it comes loop.StopWindow or more after the first (ADR 0050). Effort
// children run in their own process group, so a terminal's Ctrl-C or hangup
// reaches only the loop. m.detached marks a `loop start` child;
// a rev means this is the pinned build (ADR 0060 §3), which, watched, follows
// m.source's mainline. PATH already leads with its bin dir (leadPathWithSelf, from
// main): the reconcile child, claude -p, bd, git and agents' bare mugicrew
// inherit it. No /nix/store self-check. m.lockFd (-1 for none) is the lock a
// restart's exec handed this image.
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
	if m.rev != "" && m.source == "" { // a pre-ADR-0090 launcher: self-hosted, its Workspace was its source, and it watched
		m.source, m.watch = common, true
	}
	sig := make(chan os.Signal, 2) // room for drain + force arriving within one tick
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2)
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
		lk.Write(looplock.Status{State: s.State, Rev: s.Rev, Inflight: bd.Titles(s.Inflight), RestartTo: s.RestartTo, FailedRev: s.FailedRev, ResetsAt: s.ResetsAt, LimitSource: s.LimitSource})
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
// A pinned image (m.rev set) first strips the bin dir of self that leadPathWithSelf put
// at the front of PATH, so the new image's own prepend keeps PATH bounded.
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

// leadPathWithSelf prepends the running binary's dir to PATH so a crewmate's bare
// `mugicrew` resolves to the build that spawned it, even when the human's shell
// PATH lacks node_modules/.bin. Stamped (devShell wrapper) builds keep their PATH
// (ADR 0060 §4), and a PATH already leading with the dir is left alone, so nested
// processes never stack duplicate heads.
// ponytail: a shared bin dir (~/go/bin, Homebrew) pushes its siblings ahead too.
func leadPathWithSelf() {
	self, err := os.Executable()
	if sourceRoot != "" || err != nil {
		return
	}
	dir, path := filepath.Dir(self), os.Getenv("PATH")
	if head, _, _ := strings.Cut(path, string(os.PathListSeparator)); head == dir {
		return
	}
	if path != "" { // an empty entry would mean the cwd
		dir += string(os.PathListSeparator) + path
	}
	os.Setenv("PATH", dir)
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
// seeds the Signal's Spec successor (luffy.SeedSpec: Luffy triages, ADR 0129);
// the reconcile that follows takes whichever leg the graph then shows.
// --initiative <body-file> [--title <t>] is the INITIATIVE outcome (Spec D3):
// luffy.SeedInitiative spawns the Initiative and carries, and is the whole job —
// the Signal reconcile after it would refuse beside the Initiative.
// --checkpoint <cp> [--pivot <body-file>]... [--critique <body-file>]... [--merge-into
// <pivot> <evidence-file>]... is the REROUTE outcome (ADR 0322): luffy.SeedReroute files
// the Drafts and is the whole job, as --initiative is.
func runReconcile(argv []string) error {
	const usage = "usage: mugicrew reconcile <id> [--spec <body-file> | --initiative <body-file>] [--title <t>] [--ref <R>] [--context <name>]\n       mugicrew reconcile <signal> --checkpoint <cp> [--pivot <body-file>]... [--critique <body-file>]... [--merge-into <pivot> <evidence-file>]..."
	if len(argv) < 1 {
		return errors.New(usage)
	}
	argv, mergeArgs, ok := splitMergeInto(argv)
	if !ok {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	specFile := fs.String("spec", "", "fast lane: file holding the Spec body")
	iniFile := fs.String("initiative", "", "INITIATIVE: file holding the Initiative body")
	refFlag := fs.String("ref", "", "INITIATIVE: external issue ref the Initiative mirrors (default: the signal's own)")
	title := fs.String("title", "", "fast lane / INITIATIVE: successor title, no kind prefix (default: the signal title)")
	ctxName := fs.String("context", "", "ACCEPT / fast lane: declared context the Chart or Spec targets")
	var hosts []string
	var rerouteFiles []luffy.RerouteItem // Path holds the file until rerouteSignal reads Body
	fs.Func("checkpoint", "REROUTE: the host Checkpoint", func(v string) error { hosts = append(hosts, v); return nil })
	fs.Func("pivot", "REROUTE: file holding a pivot body (repeatable)", func(v string) error {
		rerouteFiles = append(rerouteFiles, luffy.RerouteItem{Pivot: true, Path: v})
		return nil
	})
	fs.Func("critique", "REROUTE: file holding a Critique body (repeatable)", func(v string) error {
		rerouteFiles = append(rerouteFiles, luffy.RerouteItem{Path: v})
		return nil
	})
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 ||
		(*specFile != "" && *iniFile != "") || (*title != "" && *specFile == "" && *iniFile == "") || (*refFlag != "" && *iniFile == "") {
		return errors.New(usage)
	}
	if rerouting := len(hosts) > 0 || len(rerouteFiles) > 0 || len(mergeArgs) > 0; rerouting {
		if len(hosts) == 0 || *specFile != "" || *iniFile != "" || *title != "" || *refFlag != "" || *ctxName != "" {
			return errors.New(usage)
		}
		if len(hosts) > 1 {
			return fmt.Errorf("reconcile --checkpoint: a second --checkpoint (%s) is refused: one reroute names one host", hosts[1])
		}
		bead, err := bd.Show(argv[0])
		if err != nil {
			return err
		}
		if bead.Kind != contract.KindSignal {
			return fmt.Errorf("%s: --checkpoint is valid only on a signal, %s is kind %q", usage, bead.ID, bead.Kind)
		}
		return rerouteSignal(bead, hosts[0], rerouteFiles, mergeArgs)
	}
	if err := luffy.ValidateContext(*ctxName); err != nil { // before any write
		return err
	}
	bead, err := bd.Show(argv[0])
	if err != nil {
		return err
	}
	file := *specFile
	if *iniFile != "" {
		file = *iniFile
	}
	if file != "" {
		if bead.Kind != contract.KindSignal {
			return fmt.Errorf("%s: --spec/--initiative is valid only on a signal, %s is kind %q", usage, bead.ID, bead.Kind)
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if *iniFile != "" {
			return luffy.SeedInitiative(bead, *title, string(body), *refFlag)
		}
		if err := luffy.SeedSpec(bead, *title, string(body), *ctxName); err != nil {
			return err
		}
	}
	c, err := crewFor(bead.Kind)
	if err != nil {
		return err
	}
	if l, ok := c.(luffy.Luffy); ok {
		l.Context = *ctxName
		c = l
	}
	path := os.Getenv(dispatch.PublishEnv)
	os.Unsetenv(dispatch.PublishEnv) // a nested reconcile (an agent's) is not the dispatch child
	return dispatch.Reconcile(newCtx(), c, bead, path)
}

// mergeArg is one `--merge-into <pivot> <evidence-file>` as given: the pivot, the file
// holding its evidence, and after, how many --pivot and --critique items came before it.
type mergeArg struct {
	pivot, file string
	after       int
}

// splitMergeInto lifts each `--merge-into <pivot> <evidence-file>` pair out of argv (a
// flag.FlagSet takes one value per flag), returning the rest and each pair with its
// argv position among --pivot and --critique; ok is false for a pair missing a value.
func splitMergeInto(argv []string) (rest []string, merges []mergeArg, ok bool) {
	items := 0
	for i := 0; i < len(argv); i++ {
		switch arg := strings.TrimLeft(argv[i], "-"); {
		case argv[i] == "--merge-into":
			if i+2 >= len(argv) {
				return nil, nil, false
			}
			merges = append(merges, mergeArg{pivot: argv[i+1], file: argv[i+2], after: items})
			i += 2
			continue
		case arg == "pivot" || arg == "critique" || strings.HasPrefix(arg, "pivot=") || strings.HasPrefix(arg, "critique="):
			items++
		}
		rest = append(rest, argv[i])
		if flagTakesValue(argv[i]) && i+1 < len(argv) { // its value is not a flag
			i++
			rest = append(rest, argv[i])
		}
	}
	return rest, merges, true
}

// flagTakesValue is whether arg is a reconcile flag written without "=", so its value
// is the next argv element.
func flagTakesValue(arg string) bool {
	switch strings.TrimLeft(arg, "-") {
	case "spec", "initiative", "ref", "title", "context", "checkpoint", "pivot", "critique":
		return strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=")
	}
	return false
}

// rerouteSignal reads the body files of a REROUTE and files it (luffy.SeedReroute).
// Every file is read before any write, so a missing one writes nothing.
func rerouteSignal(sig bd.Bead, host string, items []luffy.RerouteItem, mergeArgs []mergeArg) error {
	for i, it := range items {
		body, err := os.ReadFile(it.Path)
		if err != nil {
			return err
		}
		items[i].Body = string(body)
	}
	var merges []luffy.RaiseMerge
	for _, m := range mergeArgs {
		body, err := os.ReadFile(m.file)
		if err != nil {
			return err
		}
		merges = append(merges, luffy.RaiseMerge{Pivot: m.pivot, Evidence: string(body), After: m.after})
	}
	return luffy.SeedReroute(sig, host, items, merges)
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
	const usage = "usage: mugicrew pivot confirm <pivot-id> --spec <body-file> [--title <t>] [--carries <id,…>]"
	if len(argv) < 1 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("pivot confirm", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	specFile := fs.String("spec", "", "file holding the corrected Spec body")
	title := fs.String("title", "", "corrected Spec title (default: the old Spec's)")
	carries := fs.String("carries", "", "comma-separated open filed Critiques whose ask the corrected Spec folds")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || *specFile == "" {
		return errors.New(usage)
	}
	body, err := os.ReadFile(*specFile)
	if err != nil {
		return err
	}
	return zoro.Confirm(argv[0], *title, string(body), splitCarries(*carries))
}

// splitCarries is the ids of a --carries value, trimmed, without blanks or repeats.
func splitCarries(v string) []string {
	var ids []string
	for _, id := range strings.Split(v, ",") {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// runPivotEscalate is the escalate sweep verb the pivot triage session calls (ADR
// 0046 §5): it seeds a fresh Chart with the evidence file the session gathered
// and retires the old Checkpoint (zoro.Escalate). Idempotent, like confirm.
func runPivotEscalate(argv []string) error {
	const usage = "usage: mugicrew pivot escalate <pivot-id> --evidence <file> [--carries <id,…>]"
	if len(argv) < 1 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("pivot escalate", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	evidenceFile := fs.String("evidence", "", "file holding the escalation evidence")
	carries := fs.String("carries", "", "comma-separated open filed Critiques whose ask the evidence folds")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || *evidenceFile == "" {
		return errors.New(usage)
	}
	evidence, err := os.ReadFile(*evidenceFile)
	if err != nil {
		return err
	}
	return zoro.Escalate(argv[0], string(evidence), splitCarries(*carries))
}

// runPivotRaise is `mugicrew pivot raise <task> [--pivot <body-file>]... [--critique
// <body-file>]... [--merge-into <pivot> <evidence-file>]...`, Brook's stuck-exit raise.
// With no flag it is a probe: it names the one open, unlanded Checkpoint the Task holds,
// its Spec and branch, or says why a raise is refused, and writes nothing
// (luffy.ProbeRaise). With flags it files the raise as Drafts (luffy.RaiseFromTask);
// every body file is read before any write.
func runPivotRaise(argv []string, out io.Writer) error {
	const usage = "usage: mugicrew pivot raise <task> [--pivot <body-file>]... [--critique <body-file>]... [--merge-into <pivot> <evidence-file>]..."
	if len(argv) == 0 || argv[0] == "" || strings.HasPrefix(argv[0], "-") {
		return errors.New(usage)
	}
	task, rest := argv[0], argv[1:]
	var req luffy.RaiseRequest
	read := func(path string) (string, error) {
		body, err := os.ReadFile(path)
		return string(body), err
	}
	for len(rest) > 0 {
		var n int
		switch rest[0] {
		case "--pivot", "--critique":
			n = 2
		case "--merge-into":
			n = 3
		default:
			return errors.New(usage)
		}
		if len(rest) < n || slices.ContainsFunc(rest[1:n], func(a string) bool { return a == "" || strings.HasPrefix(a, "--") }) {
			return errors.New(usage)
		}
		body, err := read(rest[n-1])
		if err != nil {
			return err
		}
		switch rest[0] {
		case "--pivot":
			req.Pivots = append(req.Pivots, body)
		case "--critique":
			req.Critiques = append(req.Critiques, body)
		default:
			req.Merges = append(req.Merges, luffy.RaiseMerge{Pivot: rest[1], Evidence: body})
		}
		rest = rest[n:]
	}
	if req.None() {
		return luffy.ProbeRaise(task, out)
	}
	return luffy.RaiseFromTask(task, req)
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

// runIntakeJira files Jira work item <KEY> as a Signal (jira.Intake, ADR 0205)
// and prints the new Signal's id. The site comes from [jira] in the Workspace
// config (ADR 0206) and is checked before acli runs.
func runIntakeJira(argv []string, w io.Writer) error {
	if len(argv) != 1 {
		return errors.New("usage: mugicrew intake jira <KEY>")
	}
	typed := argv[0]
	root, err := git.RepoRoot()
	if err != nil {
		return err
	}
	res, err := jira.Intake(root, typed)
	if res.Key != "" && res.Key != typed {
		fmt.Fprintf(w, "%s is now %s\n", typed, res.Key)
	}
	if res.ID != "" {
		fmt.Fprintln(w, res.ID)
	}
	if err != nil {
		return err
	}
	if res.Carrier != "" {
		fmt.Fprintf(w, "%s superseded onto %s (already carries jira-%s)\n", res.ID, res.Carrier, res.Key)
	}
	return nil
}

// readInitiatives is the shared [initiatives] settings read, a var so tests fake it.
var readInitiatives = func() cfglayer.Initiatives {
	root, err := git.RepoRoot()
	if err != nil {
		return cfglayer.Initiatives{} // off, as luffy's seam reads it
	}
	return cfglayer.InitiativesSetting(root)
}

// Seams over the bd legs of the latch verb, swapped by the CLI tests.
var (
	showBead   = bd.Show
	latchOf    = bd.LatchOf
	newLatch   = bd.NewLatch
	blocksEdge = bd.Blocks
)

// checkLatchTargets refuses a --blocks target that does not exist or is an
// Initiative: blocking one would freeze every member Task through bd's block
// cascade, so the Latch belongs on the member actually waiting (Spec FR32).
func checkLatchTargets(verb, typed string, blocks []string) error {
	for _, id := range blocks {
		b, err := showBead(id)
		if err != nil {
			return fmt.Errorf("latch %s %s: no bead %s to block: %w", verb, typed, id, err)
		}
		if b.Kind == contract.KindInitiative {
			return fmt.Errorf("latch %s %s: %s is an Initiative; block the member that is actually waiting, not the Initiative", verb, typed, id)
		}
	}
	return nil
}

// runLatchJira files (or reuses) the one latch for a Jira key and makes it block
// each --blocks bead (Spec D4), then prints the latch id. Everything is read and
// checked before anything is filed. --drafted-by files the latch as that bead's
// Draft; a re-run adds only missing edges and never touches labels.
func runLatchJira(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew latch jira <KEY> --blocks <bead> [--blocks <bead>...] [--drafted-by <bead>]"
	fs := flag.NewFlagSet("latch jira", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	var blocks []string
	fs.Func("blocks", "bead the latch blocks (repeatable)", func(v string) error { blocks = append(blocks, v); return nil })
	draftedBy := fs.String("drafted-by", "", "file the latch as this bead's Draft")
	// flags may follow the key, so peel the key off first
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return errors.New(usage)
	}
	typed := argv[0]
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || len(blocks) == 0 {
		return errors.New(usage)
	}
	if err := checkLatchTargets("jira", typed, blocks); err != nil {
		return err
	}
	root, err := git.RepoRoot()
	if err != nil {
		return err
	}
	it, site, err := jira.Open("latch jira", root, typed)
	if err != nil {
		return err
	}
	if it.Key != typed {
		fmt.Fprintf(w, "%s is now %s\n", typed, it.Key)
	}
	if it.Done() {
		fmt.Fprintf(w, "%s is already done; no latch filed\n", it.Key)
		return nil
	}
	ref := "jira-latch-" + it.Key
	latch, found, err := latchOf(ref)
	if err != nil {
		return fmt.Errorf("latch jira %s: %w", typed, err)
	}
	id := latch.ID
	if !found {
		body := fmt.Sprintf("Watches: jira %[1]s\nhttps://%[2]s/browse/%[1]s\n\n%[3]s", it.Key, site, it.Summary)
		if id, err = newLatch("Jira "+it.Key+" is done", body, ref, *draftedBy); err != nil {
			return fmt.Errorf("latch jira %s: %w", typed, err)
		}
	}
	for _, target := range blocks {
		if err := blocksEdge(target, id); err != nil {
			return fmt.Errorf("latch jira %s: filed %s but could not make it block %s: %w", typed, id, target, err)
		}
	}
	fmt.Fprintln(w, id)
	return nil
}

// getMR is forge.GetMR, indirected so the CLI tests fake the Forge read.
var getMR = forge.GetMR

// runLatchMR files (or reuses) the one latch for a GitLab merge request and makes
// it block each --blocks bead, as runLatchJira does for a Jira key. The MR is read
// once, here; Brook re-reads it from the Watches: line.
func runLatchMR(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew latch mr <ref> --blocks <bead> [--blocks <bead>...] [--drafted-by <bead>]"
	fs := flag.NewFlagSet("latch mr", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	var blocks []string
	fs.Func("blocks", "bead the latch blocks (repeatable)", func(v string) error { blocks = append(blocks, v); return nil })
	draftedBy := fs.String("drafted-by", "", "file the latch as this bead's Draft")
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return errors.New(usage)
	}
	typed := argv[0]
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || len(blocks) == 0 {
		return errors.New(usage)
	}
	if err := checkLatchTargets("mr", typed, blocks); err != nil {
		return err
	}
	if name, err := git.LandStrategyName(); err != nil {
		return fmt.Errorf("latch mr %s: %w", typed, err)
	} else if name != git.LandStrategyGitlab {
		return fmt.Errorf("latch mr %s: needs land_strategy=gitlab (this Workspace is %s); set it in the Workspace config", typed, name)
	}
	fp, err := forgeProject()
	if err != nil {
		return fmt.Errorf("latch mr %s: %w; check the Forge remote", typed, err)
	}
	proj, iid, err := forge.ParseMRRef(typed, fp)
	if err != nil {
		return fmt.Errorf("latch mr %s: %w; use an MR URL on %s, <group/project>!<iid> or !<iid>", typed, err, fp.Host)
	}
	mr, err := getMR(proj, iid)
	if err != nil {
		return fmt.Errorf("latch mr %s: %w; check the ref and 'glab auth status --hostname %s'", typed, err, proj.Host)
	}
	disp := fmt.Sprintf("%s!%d", proj.Path(), iid)
	switch mr.State {
	case forge.StateMerged:
		fmt.Fprintf(w, "%s is already merged; no latch filed\n", disp)
		return nil
	case forge.StateOpened:
	default:
		return fmt.Errorf("latch mr %s: %s is %s, not merged; reopen it or block on another MR", typed, disp, mr.State)
	}
	canon := forge.MRCanonical(proj, iid)
	ref := "gitlab-mr-latch-" + canon
	latch, found, err := latchOf(ref)
	if err != nil {
		return fmt.Errorf("latch mr %s: %w", typed, err)
	}
	id := latch.ID
	if !found {
		body := fmt.Sprintf("Watches: mr %s\n%s\n\n%s", canon, mr.WebURL, mr.Title)
		if id, err = newLatch("MR "+disp+" is merged", body, ref, *draftedBy); err != nil {
			return fmt.Errorf("latch mr %s: %w", typed, err)
		}
	}
	for _, target := range blocks {
		if err := blocksEdge(target, id); err != nil {
			return fmt.Errorf("latch mr %s: filed %s but could not make it block %s: %w", typed, id, target, err)
		}
	}
	fmt.Fprintln(w, id)
	return nil
}

// newSurgeryOn is bd.NewSurgeryOn, indirected so the CLI tests fake bd's children reads.
var newSurgeryOn = bd.NewSurgeryOn

// runSurgeryNew files a Surgery and its own Checkpoint (Spec D3), or with
// --checkpoint joins an existing one (Spec D5), and prints the Surgery id. It
// launches nothing: the Surgery starts idle, and l in the TUI opens its first
// session (ADR 0095 §6).
func runSurgeryNew(argv []string) error {
	const usage = "surgery new: --title is required (usage: mugicrew surgery new --title T [--problem P] [--checkpoint ID | --initiative ID])"
	fs := flag.NewFlagSet("surgery new", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once; don't let flag print it too
	title := fs.String("title", "", "surgery title (required)")
	problem := fs.String("problem", "", "one-line problem (optional)")
	checkpoint := fs.String("checkpoint", "", "join this existing Checkpoint instead of filing a fresh one")
	initiative := fs.String("initiative", "", "file the fresh Checkpoint into this open Initiative")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *title == "" || (*checkpoint != "" && *initiative != "") {
		return errors.New(usage)
	}

	if *checkpoint != "" {
		id, err := newSurgeryOn(*checkpoint, *title, *problem)
		if err != nil {
			return fmt.Errorf("surgery new: %w", err)
		}
		fmt.Println(id)
		return nil
	}
	id, cp, err := bd.NewSurgery(*title, *problem, *initiative, readInitiatives().Required)
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
// (0 integrated or nothing ahead, 2 rebase or held conflict, 3 container landed, 4 no cut point,
// 5 refused off its pinned tip, 1 transient). by is backstop for the Go backstop's child, agent inside a
// dispatched effort or a HITL pane, else human.
func runIntegrate(argv []string) int {
	if len(argv) != 0 {
		fmt.Fprintln(os.Stderr, "usage: mugicrew integrate (run in a tree on effort/<bead-id>)")
		return session.ExitTransient
	}
	var child session.Child
	id, err := git.CurrentEffort()
	if err != nil && len(cfglayer.Repos(".")) > 0 && git.Detached() { // no repo = declared: today's refusal, no extra git call
		if cwd, werr := os.Getwd(); werr == nil {
			// a detached host: its one materialized child on effort/<id> integrates (Spec D6)
			switch kids := session.ProbeChildren(cwd); len(kids) {
			case 0:
			case 1:
				child, err, id = kids[0], nil, kids[0].Effort
			default:
				fmt.Fprintf(os.Stderr, "more than one nested repo is on an effort branch (%s); put all but one back on mainline, then re-run\n", kidDirs(kids))
				return session.ExitTransient
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return session.ExitTransient
	}
	b, err := bd.Show(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return session.ExitTransient
	}
	// ADR 0168 §2: only a bead carrying context: can sit in a child's tree, so an
	// unlabelled integrate stats up the path and reads config only at repo roots
	if child.Repo == "" {
		// a leaf never carries context:, so the label is read off its routed container;
		// contexts.Owner gates on the cwd's config, which a child's tree lacks, so the
		// config-only InsideChild probe runs first
		if cwd, werr := os.Getwd(); werr == nil && session.InsideChild(cwd) {
			owner := b
			if d, rerr := route.Route(b); rerr == nil {
				if _, cid := git.ParseBranchName(d.Container); cid != "" {
					if c, serr := contexts.ShowBead(cid); serr == nil {
						owner = c
					}
				}
			}
			if bd.ContextOf(owner) != "" {
				fmt.Fprintln(os.Stderr, "run mugicrew integrate from the Workspace tree")
				return session.ExitTransient
			}
		}
	}
	by := eventlog.ByHuman
	switch {
	case os.Getenv(session.BackstopEnv) != "":
		by = eventlog.ByBackstop
	case os.Getenv(eventlog.EffortEnv) != "" || session.InPane():
		by = eventlog.ByAgent
	}
	return session.IntegrateIn(child, b, by, os.Stdout)
}

// kidDirs names the dirs of the children an ambiguous integrate matched.
func kidDirs(kids []session.Child) string {
	dirs := make([]string, len(kids))
	for i, k := range kids {
		dirs[i] = k.Dir
	}
	return strings.Join(dirs, ", ")
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

// reworkApproval is the Approval rework act, a var so the CLI's tests stub it.
var reworkApproval = bd.ReworkApproval

// runApproval is `mugicrew approval rework <approval> --critique <file>
// [--critique <file>...]` (Spec FR6): it prints the published Critique ids.
func runApproval(argv []string, out io.Writer) error {
	const usage = "usage: mugicrew approval rework <approval> --critique <file> [--critique <file>...]"
	if len(argv) < 2 || argv[0] != "rework" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("approval rework", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var files stringList
	fs.Var(&files, "critique", "a Critique file: # Title, then a body")
	if err := fs.Parse(argv[2:]); err != nil || fs.NArg() != 0 || argv[1] == "" || strings.HasPrefix(argv[1], "-") {
		return errors.New(usage)
	}
	ids, err := reworkApproval(argv[1], files)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, strings.Join(ids, "\n"))
	return nil
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// withdrawChain, cancelInitiative, adoptChain, releaseChain and
// initiativeSessions are the guarded acts and the session table, vars so the
// CLI's tests stub them.
var (
	withdrawChain      = withdraw.Withdraw
	cancelInitiative   = withdraw.Cancel
	adoptChain         = bd.AdoptChain
	releaseChain       = bd.ReleaseChain
	initiativeSessions = session.Sessions
)

// runWithdraw is `mugicrew withdraw <head>`, and `mugicrew initiative withdraw
// <head>` its alias (Spec mc-5rttm D8): human-run. A bead is in flight when it is
// an active AFK effort or has a live session; a session table that can't be read
// refuses the act rather than guess.
func runWithdraw(argv []string) error {
	loop.StampAutoApprove()
	argv, discard := cutDiscard(argv)
	if len(argv) != 1 || argv[0] == "" {
		return errors.New("usage: mugicrew withdraw <head> [--discard]")
	}
	o, err := withdraw.LiveOpts(initiativeSessions)
	if err != nil {
		return err
	}
	o.Discard = discard
	res, err := withdrawChain(argv[0], o)
	if err == nil {
		fmt.Println("withdrawn", argv[0]+": closed", strings.Join(res.Closed, " "))
		printSwept(os.Stdout, argv[0], res)
		printKept(os.Stdout, res)
	}
	return err
}

// cutDiscard removes the --discard flag from argv: the human's say-so that a chain
// whose cleanup loses work may close anyway (Spec D8).
func cutDiscard(argv []string) (rest []string, discard bool) {
	for _, a := range argv {
		if a == "--discard" {
			discard = true
		} else {
			rest = append(rest, a)
		}
	}
	return rest, discard
}

// printSwept names the Tasks a withdraw of head closed with it (those ordered after
// the set, then those ordered before it), the Tasks it left open, and each land hold
// it released (Spec mc-xw3ez D9, mc-d5zdj D4).
func printSwept(w io.Writer, head string, res withdraw.Result) {
	for _, s := range res.Swept {
		fmt.Fprintf(w, "swept %s (withdrawn: %s)\n", s.Bead.ID, head)
	}
	for _, x := range res.Before {
		fmt.Fprintf(w, "swept %s (withdrawn: %s; before %s)\n", x.Bead.ID, head, x.Before)
	}
	for _, h := range res.LeftOpen {
		fmt.Fprintf(w, "left open %s: also before %s\n", h.Bead.ID, strings.Join(h.Also, ", "))
	}
	for _, c := range res.Holds {
		fmt.Fprintf(w, "land hold released: %s\n", c)
	}
}

// printKept names each cleanup item that stayed behind, for the human to clear by
// hand or with mugicrew recover; the close stands.
func printKept(w io.Writer, res withdraw.Result) {
	for _, k := range res.Kept {
		fmt.Fprintln(w, "kept", k)
	}
}

// runInitiative is `mugicrew initiative cancel <initiative>`, `withdraw <head>`
// (runWithdraw), and `adopt <head> <initiative>` / `release <head>` (Spec D5), the
// TUI's a/A as verbs.
func runInitiative(argv []string) error {
	loop.StampAutoApprove()
	const usage = "usage: mugicrew initiative cancel <initiative> [--release <head>]... [--discard] | withdraw <head> [--discard] | adopt <head> <initiative> | release <head>"
	switch {
	case len(argv) == 3 && argv[0] == "adopt":
		return adoptChain(argv[1], argv[2])
	case len(argv) == 2 && argv[0] == "release":
		return releaseChain(argv[1])
	case len(argv) > 0 && argv[0] == "withdraw":
		return runWithdraw(argv[1:])
	case len(argv) < 2 || argv[0] != "cancel":
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("initiative cancel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var release stringList
	fs.Var(&release, "release", "member chain head to release instead of close (repeatable)")
	discard := fs.Bool("discard", false, "close even though it discards uncommitted or unintegrated work")
	// the Initiative comes first, its flags after
	if fs.Parse(argv[2:]) != nil || fs.NArg() != 0 || strings.HasPrefix(argv[1], "-") {
		return errors.New(usage)
	}
	o, err := withdraw.LiveOpts(initiativeSessions)
	if err != nil {
		return err
	}
	o.Release, o.Discard = release, *discard
	res, err := cancelInitiative(argv[1], o)
	if err == nil {
		fmt.Println("cancelled", argv[1]+": closed", strings.Join(res.Closed, " "))
		if len(res.Released) > 0 {
			fmt.Println("released", strings.Join(res.Released, " "))
		}
		printKept(os.Stdout, res)
	}
	return err
}

// runSurgeryEnd ends a Surgery (Spec D5): a thin shim over franky.EndSurgery,
// which Franky's surgery prompt runs after its clean-tree and green-gate checks.
func runSurgeryEnd(argv []string) error {
	if len(argv) != 1 || argv[0] == "" {
		return errors.New("usage: mugicrew surgery end <surgery-id>")
	}
	return franky.EndSurgery(argv[0])
}

// beadDetail prints the TUI detail drawer's text for bead id at width, minus the launch
// preview, and returns the exit code. stdout is a pipe into less -R, so colour is forced.
func beadDetail(id, width string, out, errw io.Writer) int {
	w, err := strconv.Atoi(width)
	if err == nil && w < 1 {
		err = errors.New("width must be positive")
	}
	if err == nil {
		lipgloss.SetColorProfile(termenv.ANSI256)
		var text string
		if text, err = tui.BeadDetail(newCtx(), crew(), id, w); err == nil {
			fmt.Fprintln(out, text)
			return 0
		}
	}
	fmt.Fprintln(errw, "bead-detail:", err)
	return 1
}
