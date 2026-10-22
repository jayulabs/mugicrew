// Package chopper is Chopper, the crew's doctor (ADR 0126): deterministic Go,
// kindless, with no claude session, that repairs a bead stranded by a dead effort
// only when a human asks (`mugicrew recover <bead>`). Recover judges the holder
// on this host, fences the bead with paused:recovering, re-judges, then drops
// every leftover in one fixed, idempotent order (ADR 0127): a stopped rebase,
// the effort tree, the Drafts, the claim, and last the pauses the stranding
// left. It never repairs under a holder that reads alive, and never commits WIP
// onto effort/<id>.
package chopper

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// The verdicts that license a repair (ADR 0127 §10): a dead holder, an ambiguous
// one the loop lock and the TUI-alive read resolved, or one the human forced.
const (
	Dead              = "dead"
	AmbiguousResolved = "ambiguous-resolved"
	Forced            = "forced"
)

// The verdicts of a refused holder: one seen running, or one only the loop off,
// no other TUI, or --force resolves (the TUI points it at mugicrew recover --force).
const (
	Alive     = "alive"
	Ambiguous = "ambiguous"
)

// Holder is what Judge reads of a bead's holder, gathered on this host.
type Holder struct {
	Bead       bd.Bead
	Placement  route.Placement
	RouteErr   error
	Session    session.Info // the bead's HITL session; zero (None) is gone
	Checkup    session.Info // the session of the open Checkup fencing it (paused:checkup); zero is gone or none open
	SessionErr error        // the session table read failed: never read as gone
	Probe      []int        // live mugicrew/claude pids naming the bead (own lineage excluded)
	Cwd        []int        // pids whose cwd lies in the bead's tree (own lineage excluded)
	CwdErr     error        // cwds can't be read: never read as no holder
	Held       bool         // a dispatcher holds its per-effort lock (ADR 0155)
	LockFree   bool         // the loop lock reads off
	TUIAlive   bool         // a tui-* session but the caller's own is on the repo's server
	TUIErr     error        // the TUI-alive read failed
	Force      bool         // --force
}

// Refused is a refusal: nothing is repaired, and the reason names the way out.
// Verdict is the holder's reading, alive or ambiguous, "" for any other refusal.
type Refused struct{ Reason, Verdict string }

func (r *Refused) Error() string { return r.Reason }

// Failed is a repair step's error: a re-run of recover rolls forward from the
// step that failed. Past the fence, paused:recovering stays.
type Failed struct {
	Bead, Step string
	Err        error
}

func (f *Failed) Error() string {
	left := "paused:recovering stays"
	if f.Step == stepCheck {
		left = "nothing changed"
	}
	return fmt.Sprintf("%s: %s failed: %v; %s, re-run mugicrew recover %s", f.Bead, f.Step, f.Err, left, f.Bead)
}

func (f *Failed) Unwrap() error { return f.Err }

// stepCheck is the inventory read before the fence: its failure wrote nothing.
const stepCheck = "check"

// afk reports whether b's kind runs AFK (the registry's Mode).
func afk(b bd.Bead) bool {
	return registry.IsMode(b.Kind, b.Labels, registry.AFK)
}

// Judge is the verdict (ADR 0127 §2 as amended on mc-yr7o.6), pure: a verdict
// licensing the repair, else a *Refused. An AFK holder the probe or the cwd scan
// sees, or whose per-effort lock is held (ADR 0155), is alive, even forced. --force is refused unless the loop lock is free.
// An AFK bead on a tree route the probe reads dead and that is paused abandoned is
// dead. An abandoned in-process one is ambiguous and only --force resolves it; a
// not-abandoned one is ambiguous and resolves when the loop lock is free and no
// TUI is alive, or with --force. paused:recovering stands in for abandoned: only
// a licensed verdict fences, and a repair that failed partway may already have
// lifted abandoned. A HITL bead is judged by its session: Alive (closing or
// not) is alive, Exited is resumable (ADR 0037), and gone after a table read
// that succeeded is dead. A bead whose kind the registry does not know is not
// a mugicrew bead and is refused first. Every other shape refuses.
func Judge(h Holder) (string, error) {
	id := h.Bead.ID
	verdict := func(v, format string, a ...any) (string, error) {
		return "", &Refused{Reason: id + ": " + fmt.Sprintf(format, a...), Verdict: v}
	}
	refuse := func(format string, a ...any) (string, error) { return verdict("", format, a...) }
	_, kindErr := registry.Lookup(h.Bead.Kind)
	switch {
	case kindErr != nil:
		return refuse("not a mugicrew bead (kind %q is unknown)", h.Bead.Kind)
	case len(h.Probe) > 0:
		return verdict(Alive, "alive (pids %s); stop it with mugicrew abort %s", dispatch.FormatPids(h.Probe), id)
	case len(h.Cwd) > 0:
		return verdict(Alive, "alive: pids %s run inside its tree; stop it with mugicrew abort %s, or kill them", dispatch.FormatPids(h.Cwd), id)
	case h.CwdErr != nil: // alive: refuse (ADR 0127 §2) in effect, but the verdict stays unclassified, like SessionErr
		return refuse("can't read process cwds, so a live process may still hold its tree: %v", h.CwdErr)
	case h.Held:
		return verdict(Alive, "held by its dispatcher (the loop or a run-now host); stop it with mugicrew abort %s", id)
	case h.Force && !h.LockFree:
		return "", forceRefused(id)
	case h.SessionErr != nil:
		return refuse("can't read the session table: %v", h.SessionErr)
	case h.Checkup.State == session.Alive || h.Checkup.State == session.Exited:
		return refuse("a Checkup is open on %s", h.Bead.Title)
	case h.Session.State == session.Alive:
		return verdict(Alive, "alive: its HITL session %s is live", h.Session.Session)
	case h.Session.State == session.Exited:
		return refuse("its HITL session %s exited and is resumable; to discard it and the Drafts a resume would publish, drop it from the TUI", h.Session.Session)
	case bd.PausedBy(h.Bead, bd.PausedCheckup) && h.Bead.Status != "in_progress":
		return Dead, nil // a leftover Checkup fence: its session is gone or it is closed
	case !afk(h.Bead):
		return Dead, nil // a HITL bead whose session is gone
	case h.RouteErr != nil:
		return refuse("can't route it: %v", h.RouteErr)
	case abandoned(h.Bead) && h.Placement != route.RepoRoot:
		return Dead, nil
	case h.Force:
		return Forced, nil
	case abandoned(h.Bead):
		return verdict(Ambiguous, "runs in-process: a hostless claude or its tools may still be writing; check pgrep -a claude and your own shells, then re-run with --force")
	case !h.LockFree:
		return verdict(Ambiguous, "not paused abandoned and the loop is on, so its host may still be live; stop the loop or re-run with --force once it is off")
	case h.TUIErr != nil:
		return refuse("not paused abandoned and can't read whether a TUI is alive: %v", h.TUIErr)
	case h.TUIAlive:
		return verdict(Ambiguous, "not paused abandoned and a TUI is alive, so its host may still be live; quit it or re-run with --force")
	}
	return AmbiguousResolved, nil
}

// forceRefused is --force while the loop lock is held: forcing never races a loop.
func forceRefused(id string) error {
	return &Refused{Reason: id + ": --force needs the loop off; stop it with mugicrew loop stop, then re-run once the loop is off"}
}

// Inventory is a bead's leftovers: its claim and pauses (on Bead), its effort
// tree, whether it is stopped mid-rebase and how many changed files it holds, and
// its Drafts. A nested effort's Tree is the host and Child the repo = Dir's worktree
// in it, the holder (ADR 0169); Rebasing and Files then speak of both.
type Inventory struct {
	Bead     bd.Bead
	Tree     string
	Child    string
	Dir      string
	Rebasing bool
	Files    int
	Drafts   []string
	Checkup  string // the open Checkup's id when paused:checkup stands
}

// Take gathers id's inventory. The tree is whichever worktree holds effort/<id>,
// or failing that one stopped mid-rebase on it (its HEAD detached), found in the
// cwd repo first and then, only if none holds it, in each declared repo dir.
func Take(id string) (Inventory, error) {
	b, err := show(id)
	if err != nil {
		return Inventory{Bead: bd.Bead{ID: id}}, err
	}
	inv := Inventory{Bead: b}
	branch := git.EffortBranch(id)
	h, _, err := holderAcross(branch, nil)
	if err == nil && h.Path == "" {
		if repos := repoDirs(); len(repos) > 0 {
			h, _, err = holderAcross(branch, repos)
		}
	}
	if err != nil {
		return inv, err
	}
	if h.Path != "" {
		inv.Tree, inv.Rebasing = h.Path, h.Rebasing
		if h.Dir != "" {
			inv.Tree, inv.Child, inv.Dir = h.Host, h.Path, h.Dir
		}
		// a prunable tree's dir is gone: nothing left to lose; a stopped rebase's
		// changed files are its own, counted once it is aborted
		if !h.Prunable && !inv.Rebasing {
			if err = inv.count(); err != nil {
				return inv, err
			}
		}
	}
	if bd.PausedBy(b, bd.PausedCheckup) {
		ck, _, err := openCheckupOn(id)
		if err != nil {
			return inv, err
		}
		inv.Checkup = ck.ID
	}
	inv.Drafts, err = drafts(id)
	return inv, err
}

// holder is where the effort's own work lives: the child for a nested holder, else Tree.
func (inv Inventory) holder() string { return cmp.Or(inv.Child, inv.Tree) }

// count sets Files: the holder's changed files, plus the host's for a nested one
// (--discard covers both). A host already gone has none.
func (inv *Inventory) count() (err error) {
	if inv.Files, err = dirtyFiles(inv.holder()); err != nil || inv.Child == "" {
		return err
	}
	if _, serr := os.Stat(inv.Tree); serr != nil {
		return nil
	}
	n, err := dirtyFiles(inv.Tree)
	inv.Files += n
	return err
}

// liftable are the causes recover lifts last; manual and integrate-conflict stay.
var liftable = []string{bd.PausedAbandoned, bd.PausedForeignHold, bd.PausedRecovering, bd.PausedCheckup}

// stranded reports whether inv holds anything recover repairs.
func (inv Inventory) stranded() bool {
	return inv.Bead.Status == "in_progress" || inv.Tree != "" || len(inv.Drafts) > 0 ||
		abandoned(inv.Bead) || bd.PausedBy(inv.Bead, bd.PausedCheckup)
}

// abandoned reports paused:abandoned, or recover's own fence standing in for it.
func abandoned(b bd.Bead) bool {
	return bd.PausedBy(b, bd.PausedAbandoned) || bd.PausedBy(b, bd.PausedRecovering)
}

// Opts are recover's flags: Discard drops a dirty tree's uncommitted files, Force
// overrides an ambiguous verdict, and ExcludeTUISession is the calling TUI's own
// session, which never counts as a TUI alive.
type Opts struct {
	Discard, Force    bool
	ExcludeTUISession string
}

// Report is what a repair did: its verdict ("" when there was nothing to
// recover), one line per step that acted, and the pauses left on the bead.
type Report struct {
	Verdict   string
	Lines     []string
	Remaining []string
}

// Recover repairs bead id (ADR 0127 §1): --force with the loop on refuses before
// the bead is read; then check, fence with paused:recovering, re-check, abort a
// stopped rebase, drop the tree, delete the Drafts, unclaim, lift abandoned,
// foreign-hold and recovering, and failing iff it aborted a rebase. Each step is
// a no-op when its leftover is absent, so a re-run rolls forward. A refusal is a
// *Refused (one recover.refused; the fence stays if the re-check refused), a step
// error, a failed read included, a *Failed (one recover.failed; the fence stays
// past the fence), success one recover.done. No leftover at all is nothing to
// recover: no write, no event.
func Recover(id string, o Opts) (Report, error) {
	var r Report
	if o.Force { // before the bead is read, so these events carry only its ID (no parent or crewmate)
		st, err := loopState()
		if err != nil {
			return r, failed(bd.Bead{ID: id}, stepCheck, err)
		}
		if st != looplock.Off {
			return r, refused(bd.Bead{ID: id}, forceRefused(id))
		}
	}
	inv, err := Take(id) // on a show failure inv.Bead is only {ID}: that event carries no parent or crewmate
	if err != nil {
		return r, failed(inv.Bead, stepCheck, err)
	}
	if !inv.stranded() {
		return r, nil
	}
	if err := drift(inv.Bead); err != nil {
		return r, refused(inv.Bead, err)
	}
	self := lineage(os.Getpid())
	if _, err := check(inv, o, self, false); err != nil {
		return r, refusedOrFailed(inv.Bead, stepCheck, err)
	}
	fenced := !bd.PausedBy(inv.Bead, bd.PausedRecovering)
	if fenced {
		if err := pause(id, bd.PausedRecovering); err != nil {
			return r, failed(inv.Bead, "fence", err)
		}
		r.Lines = append(r.Lines, "fenced with paused:recovering")
	}
	if inv, err = Take(id); err != nil {
		return r, failed(inv.Bead, "re-check", err)
	}
	if r.Verdict, err = check(inv, o, self, fenced); err != nil { // the fence stays: the bead stays safe
		var rf *Refused
		if !errors.As(err, &rf) {
			return r, failed(inv.Bead, "re-check", err)
		}
		return r, refused(inv.Bead, &Refused{Reason: err.Error() + "; paused:recovering stays until a re-run repairs it"})
	}
	b := inv.Bead
	lifts := liftable
	if inv.Rebasing { // failing is the CutFailed loop the stopped rebase caused
		if err := abortRebase(inv.holder()); err != nil {
			return r, failed(b, "abort rebase", err)
		}
		r.Lines = append(r.Lines, "aborted the rebase in "+inv.holder())
		if err = inv.count(); err != nil {
			return r, failed(b, "abort rebase", err)
		}
		if err := dirty(inv, o); err != nil { // the fence stays; a re-run's abort is a no-op
			return r, refused(b, err)
		}
		// ponytail: a re-run past this point no longer sees the rebase, so leaves
		// failing for a human's bd label remove; record the abort on the bead if that bites.
		lifts = append(slices.Clone(liftable), bd.PausedFailing)
	}
	if inv.Tree != "" {
		if err := dropTree(inv.Tree); err != nil {
			return r, failed(b, "drop tree", err)
		}
		line := "dropped tree " + inv.Tree
		if inv.Child != "" {
			line += " (child " + inv.Child + " first)"
		}
		if inv.Files > 0 {
			line += fmt.Sprintf(", discarding %d changed files", inv.Files)
		}
		r.Lines = append(r.Lines, line)
	}
	var ids []string
	if len(inv.Drafts) > 0 {
		if ids, err = deleteDrafts(id); err != nil {
			return r, failed(b, "delete Drafts", err)
		}
		r.Lines = append(r.Lines, "deleted Drafts "+strings.Join(ids, ", "))
	}
	unclaimed := b.Status == "in_progress"
	if unclaimed {
		if err := unclaim(id); err != nil {
			return r, failed(b, "unclaim", err)
		}
		r.Lines = append(r.Lines, "unclaimed")
	}
	var lift []string
	for _, c := range lifts {
		if !bd.PausedBy(b, c) {
			continue
		}
		if err := unpause(id, c); err != nil {
			return r, failed(b, "lift "+c, err)
		}
		lift = append(lift, c)
	}
	if len(lift) > 0 {
		r.Lines = append(r.Lines, "lifted "+strings.Join(lift, ", "))
	}
	r.Remaining = slices.DeleteFunc(bd.PausedCauses(b), func(c string) bool { return slices.Contains(lift, c) })
	attrs := eventlog.BeadAttrs(b)
	if inv.Dir != "" {
		attrs = append(attrs, "repo", inv.Dir)
	}
	slog.Info("recover.done", append(attrs, "verdict", r.Verdict, "rebase_aborted", inv.Rebasing, "tree", inv.Tree, "discarded", inv.Files > 0,
		"drafts", ids, "unclaimed", unclaimed, "lifted", lift, "remaining", r.Remaining)...)
	return r, nil
}

// Assess is a press-time read of id for a confirm (the TUI's x): its inventory
// and verdict, with no write and no event. A dirty tree is not refused: stating it
// is the confirm's job, and Recover still refuses it unless Discard. No leftover
// is no verdict.
func Assess(id string, o Opts) (Inventory, string, error) {
	inv, err := Take(id)
	if err != nil || !inv.stranded() {
		return inv, "", err
	}
	v, err := judge(inv, o, lineage(os.Getpid()), false)
	return inv, v, err
}

// drift refuses a nested bead whose repo = moved since its cut: the old dir is no
// guide to the tree, so the human clears the paused:context hold first (Spec FR 55).
// The stamp lives on the container (ADR 0167), so an unstamped leaf is judged by its
// parent, but only when some repo = is declared: a Workspace without one pays no
// extra bd read (ADR 0157, Spec FR 4, FR 56).
func drift(b bd.Bead) error {
	if bd.RepoStampOf(b) == "" && b.Parent != "" && len(repoDirs()) > 0 {
		if c, err := show(b.Parent); err == nil {
			b = c
		}
	}
	if bd.RepoStampOf(b) == "" {
		return nil
	}
	if stamp, dir, drifted := contexts.Drift(b, repoDirs()); drifted {
		return &Refused{Reason: fmt.Sprintf("%s: cut in repo %q but repo = now %q; clear paused:context first (revert repo = or replace the repo: label with repo:<dir>) and re-run", b.ID, stamp, dir)}
	}
	return nil
}

// check judges inv's holder; a licensed holder's dirty tree is refused unless Discard.
func check(inv Inventory, o Opts, self []int, fenced bool) (string, error) {
	v, err := judge(inv, o, self, fenced)
	if err != nil {
		return "", err
	}
	if err := dirty(inv, o); err != nil {
		return "", err
	}
	return v, nil
}

// judge reads inv's holder on this host, its own lineage and o's TUI left out,
// and judges it. A fence set by this run (fenced) is no stand-in for abandoned:
// the re-check judges the bead as the first check did.
func judge(inv Inventory, o Opts, self []int, fenced bool) (string, error) {
	hs, err := look(o)
	if err != nil {
		return "", err
	}
	return judgeIn(hs, inv, o, self, fenced)
}

// hostState is the host state a verdict reads besides the bead's own holder: the loop
// lock, the tmux session table and whether a TUI is alive. List reads it once.
type hostState struct {
	lockFree           bool
	sessions           map[string]session.Info
	sessionErr, tuiErr error
	tuiAlive           bool
}

// look reads the host state once; o's excluded TUI session is left out of tuiAlive.
func look(o Opts) (hostState, error) {
	st, err := loopState()
	if err != nil {
		return hostState{}, err
	}
	hs := hostState{lockFree: st == looplock.Off}
	hs.sessions, hs.sessionErr = sessions()
	hs.tuiAlive, hs.tuiErr = tuiAlive(o.ExcludeTUISession)
	return hs, nil
}

// judgeIn judges inv against the host state hs already read.
func judgeIn(hs hostState, inv Inventory, o Opts, self []int, fenced bool) (string, error) {
	b := inv.Bead
	if fenced {
		b.Labels = slices.DeleteFunc(slices.Clone(b.Labels), func(l string) bool { return l == "paused:"+bd.PausedRecovering })
	}
	h := Holder{Bead: b, LockFree: hs.lockFree, Force: o.Force, Held: held(b.ID), Probe: slices.DeleteFunc(probe(b.ID), func(p int) bool { return slices.Contains(self, p) })}
	var d route.Destination
	d, h.RouteErr = route.Route(b)
	h.Placement = d.Placement
	h.Session, h.SessionErr = hs.sessions[b.ID], hs.sessionErr
	h.Checkup = hs.sessions[inv.Checkup] // "" is no key: zero
	if inv.Tree != "" {
		h.Cwd, h.CwdErr = inTree(inv.Tree, self)
	}
	h.TUIAlive, h.TUIErr = hs.tuiAlive, hs.tuiErr
	return Judge(h)
}

// dirty refuses inv's tree holding changed files unless Discard.
func dirty(inv Inventory, o Opts) error {
	if inv.Files > 0 && !o.Discard {
		return &Refused{Reason: fmt.Sprintf("%s: its tree %s has %d uncommitted changed files; re-run with --discard to drop them", inv.Bead.ID, inv.Tree, inv.Files)}
	}
	return nil
}

// refusedOrFailed files err as a refusal, or as a failed step when it is a read
// error (a *Refused is the only judgment).
func refusedOrFailed(b bd.Bead, step string, err error) error {
	var rf *Refused
	if errors.As(err, &rf) {
		return refused(b, err)
	}
	return failed(b, step, err)
}

func refused(b bd.Bead, err error) error {
	slog.Warn("recover.refused", append(eventlog.BeadAttrs(b), "reason", err.Error())...)
	return err
}

func failed(b bd.Bead, step string, err error) error {
	slog.Error("recover.failed", append(eventlog.BeadAttrs(b), "step", step, "error", err.Error())...)
	return &Failed{Bead: b.ID, Step: step, Err: err}
}

// Recover's seams, vars in dispatch.Abort's style so tests record the call order.
var (
	show     = bd.Show
	graphDir = bd.GraphDir
	// holderAcross finds effort/<id>'s holder; repoDirs is the declared repo = dirs,
	// read only once the cwd repo holds nothing, and git-free when none is declared.
	holderAcross = git.HolderAcross
	repoDirs     = contexts.Repos
	// openCheckupOn is the open Checkup a paused:checkup fence belongs to.
	openCheckupOn = bd.OpenCheckupOn
	abortRebase   = git.AbortRebase
	dirtyFiles    = git.DirtyFiles
	drafts        = func(creator string) ([]string, error) {
		ds, err := bd.Drafts(creator)
		return bd.IDs(ds), err
	}
	sessions = session.Sessions
	tuiAlive = session.TUIAlive
	// loopState is the loop lock's state through looplock.Read: off is free; on and
	// draining both hold it.
	loopState = func() (string, error) {
		common, err := git.CommonDir()
		if err != nil {
			return "", err
		}
		dir, err := beadsDir()
		if err != nil {
			return "", err
		}
		path, err := looplock.ResolveIn(common, dir)
		if err != nil {
			return "", err
		}
		st, err := looplock.Read(path)
		return st.State, err
	}
	// held reads id's per-effort lock under the same graph dir as loopState.
	held = func(id string) bool {
		dir, err := beadsDir()
		return err == nil && liveness.HeldIn(dir, id)
	}
	probe    = liveness.Pids
	inTree   = liveness.InTree
	lineage  = liveness.Lineage
	pause    = bd.Pause
	unpause  = bd.Unpause
	dropTree = session.DropOrphan // worktree remove --force, its mugicrew-wt-* parent, prune
	// ponytail: a kept Draft branch's outcome goes unrecorded (no per-step events,
	// ADR 0127 §10); it's still there for git branch -D.
	deleteDrafts = func(creator string) ([]string, error) {
		ids, _, err := bd.DeleteDrafts(creator)
		return ids, err
	}
	unclaim = bd.Unclaim
)

// beadsDir is the graph dir the dispatcher used: $BEADS_DIR, else (recover
// inherits the graph, so a bare shell) resolved here without exporting it.
func beadsDir() (string, error) {
	if dir := os.Getenv("BEADS_DIR"); dir != "" {
		return dir, nil
	}
	root, err := git.RepoRoot()
	if err != nil {
		return "", err
	}
	return graphDir(root)
}
