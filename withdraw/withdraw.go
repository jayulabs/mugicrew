// Package withdraw is the verbs that drop unfinished work from the bead graph
// (Spec mc-5rttm D6, ADR 0310): Withdraw closes any unclosed chain head with its
// caused-by predecessors and everything under them, and Cancel closes a whole
// Initiative. Plan is their read-only view. The verbs own the act and its
// events; package bd stays event-blind and supplies the sets (bd.WithdrawSet,
// bd.CancelSet, bd.OrderedAfter).
package withdraw

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"syscall"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/session"
)

// Kind is which verb a Plan is for.
type Kind string

const (
	KindWithdraw Kind = "withdraw"
	KindCancel   Kind = "cancel"
)

// RefusalKind names why a verb refuses.
type RefusalKind string

const (
	InFlight      RefusalKind = "in-flight"
	OrderedAfter  RefusalKind = "ordered-after"
	OpenLanding   RefusalKind = "open-landing"
	ReleaseLocked RefusalKind = "release-locked"
)

// Refusal is one blocker: the beads in the way and the fix for them.
type Refusal struct {
	Kind  RefusalKind
	Beads []string
	Fix   string
	MR    string // the Merge Request an open-landing refusal is about (!<iid>), if any
}

// Reason is the refusal's name in the event log's reason attr.
func (r Refusal) Reason(k Kind) string {
	switch r.Kind {
	case InFlight:
		return "in-flight effort"
	case OpenLanding:
		return "open Landing"
	case ReleaseLocked:
		return "release refused: Require Initiatives"
	}
	return "ordered after the chain"
}

// Plan is the read-only view of a verb: what it would close and what refuses it.
// Building one writes nothing and records no event.
type Plan struct {
	Kind     Kind
	Target   string
	Set      []bd.Bead // leaves first, target last
	Refusals []Refusal
	Chains   []Chain  // cancel: every unclosed member chain, released ones included
	Locked   []Lock   // cancel: member chains that must close, not be released
	Released []string // cancel: the heads --release names, kept out of Set

	// Inventory is the cleanup the verb would perform (Spec D4), for the dialog to
	// tag rows from and the CLI to print before a discard.
	Inventory Inventory
}

// Chain is one unclosed member chain of a cancelled Initiative: its head and the
// beads a withdraw of it would close.
type Chain struct {
	Head  bd.Bead
	Beads []string
}

// Lock is a member chain whose release is refused, and why.
type Lock struct {
	Head, Why string
}

// Result is what a verb did.
type Result struct {
	Closed   []string
	Released []string
	// Removed and Kept are cleanup's outcome (Spec D5): what it took down and what
	// it left, with the holder or error. A kept item never undoes the close.
	Removed, Kept []Item
}

// Opts are a verb's inputs.
type Opts struct {
	// InFlight reports whether a bead has an effort running (the CLI's
	// loop.IsActiveAFKEffort or a live session); nil means none does.
	InFlight func(bd.Bead) bool
	// Reread, when set, supplies a fresh InFlight; the verb calls it for the
	// re-check under the fence, so the test never runs on a table read before the
	// fence was taken.
	Reread func() (func(bd.Bead) bool, error)
	// Release names the member chain heads a cancel releases out of the
	// Initiative instead of closing; every other unclosed chain closes.
	Release []string
	// Discard lets the verb close a chain whose inventory loses work (uncommitted
	// files, unintegrated or never-landed commits); without it the verb stops with
	// a *DiscardRequiredError and closes nothing.
	Discard bool
}

// LiveOpts are the Opts of a human-run verb: a bead is in flight when it is an
// active AFK effort or has an alive session, read from the session table now and
// again under the fence. A table that can't be read fails rather than guess.
func LiveOpts(read func() (map[string]session.Info, error)) (Opts, error) {
	now := func() (func(bd.Bead) bool, error) {
		sessions, err := read()
		if err != nil {
			return nil, err
		}
		return func(b bd.Bead) bool {
			return loop.IsActiveAFKEffort(b) || sessions[b.ID].State == session.Alive
		}, nil
	}
	in, err := now()
	return Opts{InFlight: in, Reread: now}, err
}

// RefusedError is a verb refused with nothing written: each Refusal names its
// beads and the fix.
type RefusedError struct {
	Verb, Target string
	Refusals     []Refusal
}

func (e *RefusedError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s refused:", e.Verb, e.Target)
	for _, r := range e.Refusals {
		fmt.Fprintf(&sb, "\n  %s: %s", r.Kind, strings.Join(r.Beads, ", "))
		if r.MR != "" {
			fmt.Fprintf(&sb, " (%s)", r.MR)
		}
		fmt.Fprintf(&sb, "\n    fix: %s", r.Fix)
	}
	return sb.String()
}

// seams, vars so the tests stub the graph.
var (
	listAll      = bd.ListAll
	forceClose   = bd.ForceCloseAll
	acquire      = liveness.Acquire
	casClaim     = bd.Claim
	unclaim      = bd.Unclaim
	releaseChain = bd.ReleaseChain
	required     = bd.RequireInitiativesOn

	strategyName = git.LandStrategyName
	repoDirOf    = contexts.RepoDirOf
	projectOf    = git.Repo.ForgeProject
	getMR        = forge.GetMR
)

// PlanOf reads the graph and plans the verb target names: cancel for an Initiative,
// withdraw otherwise. A target that is no unclosed chain head (or Initiative) is a
// plain error, not a Refusal.
func PlanOf(target string, o Opts) (Plan, error) {
	all, err := listAll()
	if err != nil {
		return Plan{}, err
	}
	p, err := plan(target, all, o)
	if err != nil {
		return Plan{}, err
	}
	p.Inventory, err = takeInventory(p.Set, o)
	return p, err
}

func plan(target string, all []bd.Bead, o Opts) (Plan, error) {
	i := slices.IndexFunc(all, func(b bd.Bead) bool { return b.ID == target })
	if i < 0 {
		return Plan{}, fmt.Errorf("%s is not a bead", target)
	}
	t := all[i]
	p := Plan{Target: target}
	switch {
	case t.Kind == contract.KindInitiative && t.Unclosed():
		p.Kind = KindCancel
		if err := planRelease(&p, all, o.Release); err != nil {
			return Plan{}, err
		}
		p.Set = bd.CancelSet(target, all, p.Released...)
	case bd.IsChainHead(t, all):
		if len(o.Release) > 0 {
			return Plan{}, fmt.Errorf("--release applies to an Initiative cancel, not a withdraw of %s", target)
		}
		p.Kind, p.Set = KindWithdraw, bd.WithdrawSet(target, all)
	default:
		return Plan{}, fmt.Errorf("%s is not a chain head: only an unclosed Signal, Chart, Spec, Checkpoint or Rollback with nothing caused by it, or an unclosed Initiative, can be dropped", target)
	}
	if o.InFlight != nil {
		if busy := bd.IDs(bd.Filter(p.Set, o.InFlight)); len(busy) > 0 {
			p.Refusals = append(p.Refusals, Refusal{Kind: InFlight, Beads: busy, Fix: inFlightFix})
		}
	}
	rs, err := landingRefusals(p.Set, all, p.Kind)
	if err != nil {
		return Plan{}, err
	}
	p.Refusals = append(p.Refusals, rs...)
	if after := bd.IDs(bd.OrderedAfter(p.Set, all)); len(after) > 0 {
		p.Refusals = append(p.Refusals, Refusal{Kind: OrderedAfter, Beads: after, Fix: "they are ordered after a bead this would close: withdraw them first, or drop that ordering with bd dep remove"})
	}
	return p, nil
}

// planRelease fills p.Locked (under Require Initiatives every unclosed member
// chain) and p.Released, and refuses a --release naming a locked head. A head that
// is no unclosed member chain head of the Initiative is a plain error.
func planRelease(p *Plan, all []bd.Bead, release []string) error {
	for _, m := range all {
		if m.Parent == p.Target && bd.IsChainHead(m, all) {
			p.Chains = append(p.Chains, Chain{m, bd.IDs(bd.WithdrawSet(m.ID, all))})
		}
	}
	if required() {
		for _, m := range all {
			if m.Parent == p.Target && m.Unclosed() {
				p.Locked = append(p.Locked, Lock{m.ID, bd.ReleaseRequiredWhy(m.ID)})
			}
		}
	}
	for _, h := range release {
		i := slices.IndexFunc(all, func(b bd.Bead) bool { return b.ID == h })
		if i < 0 || all[i].Parent != p.Target || !bd.IsChainHead(all[i], all) {
			return fmt.Errorf("--release %s: not an unclosed member chain head of %s", h, p.Target)
		}
		if j := slices.IndexFunc(p.Locked, func(l Lock) bool { return l.Head == h }); j >= 0 {
			p.Refusals = append(p.Refusals, Refusal{Kind: ReleaseLocked, Beads: []string{h}, Fix: p.Locked[j].Why})
		} else if !slices.Contains(p.Released, h) {
			p.Released = append(p.Released, h)
		}
	}
	return nil
}

// Withdraw closes head, its unclosed caused-by predecessors and every unclosed bead
// under them, leaves first and head last, with reason withdrawn and no successor.
func Withdraw(head string, o Opts) (Result, error) { return run(head, KindWithdraw, o) }

// Cancel closes an Initiative's unclosed member chains, then the Initiative, with
// reason cancelled: <initiative>.
func Cancel(initiative string, o Opts) (Result, error) { return run(initiative, KindCancel, o) }

func run(target string, want Kind, o Opts) (Result, error) {
	all, err := listAll()
	if err != nil {
		return Result{}, failed(want, bd.Bead{ID: target}, "read", err, nil, nil)
	}
	p, err := plan(target, all, o)
	if err != nil {
		var pe *planFailure
		if errors.As(err, &pe) { // a read that failed, not a bad target
			return Result{}, failed(want, bd.Bead{ID: target}, "plan", err, nil, nil)
		}
		return Result{}, err
	}
	if p.Kind != want {
		return Result{}, fmt.Errorf("%s: %s is a %s target, use that verb", want, target, p.Kind)
	}
	if len(p.Refusals) > 0 {
		return Result{}, refuse(p)
	}
	// The fence (Spec D3): from here no claim can take a bead about to close.
	t := p.Set[len(p.Set)-1]
	f, blocked, err := fence(p.Set)
	if err != nil {
		f.drop()
		return Result{}, failed(want, t, "fence", err, nil, nil)
	}
	if blocked != "" {
		f.drop()
		p.Refusals = []Refusal{{Kind: InFlight, Beads: []string{blocked}, Fix: inFlightFix}}
		return Result{}, refuse(p)
	}
	all, err = listAll()
	if err != nil {
		f.drop()
		return Result{}, failed(want, t, "fence", err, nil, nil)
	}
	if o.Reread != nil {
		if o.InFlight, err = o.Reread(); err != nil {
			f.drop()
			return Result{}, failed(want, t, "fence", err, nil, nil)
		}
	}
	p2, err := plan(target, all, o)
	if err == nil && !slices.Equal(bd.IDs(p2.Set), bd.IDs(p.Set)) {
		err = errors.New("the closing set changed under the fence, try again")
	}
	if err != nil {
		f.drop()
		return Result{}, failed(want, t, "fence", err, nil, nil)
	}
	p = p2
	if len(p.Refusals) > 0 {
		f.drop()
		return Result{}, refuse(p)
	}
	inv, err := takeInventory(p.Set, o)
	if err != nil {
		f.drop()
		return Result{}, failed(p.Kind, t, "inventory", err, nil, nil)
	}
	if inv.Loses() && !o.Discard { // the CLI's safeguard, a view: nothing is logged
		f.drop()
		return Result{}, &DiscardRequiredError{Verb: string(p.Kind), Target: target, Inventory: inv}
	}
	for i, h := range p.Released {
		if err := releaseChain(h); err != nil {
			f.drop()
			return Result{Released: p.Released[:i]}, failed(p.Kind, t, "release "+h, err, nil, p.Released[:i])
		}
	}
	ids := bd.IDs(p.Set)
	reason := "withdrawn"
	if p.Kind == KindCancel {
		reason = "cancelled: " + target
	}
	if err := forceClose(ids, reason); err != nil {
		f.dropAfterClose()
		return Result{Released: p.Released}, failed(p.Kind, t, "close", err, ids, p.Released)
	}
	attrs := append(eventlog.BeadAttrs(t), "closed", ids, "reason", reason)
	if p.Kind == KindCancel {
		attrs = append(attrs, "released", append([]string{}, p.Released...))
	}
	slog.Info(string(p.Kind)+".done", attrs...)
	removed, kept := cleanup(inv, target, p.Set)
	f.release() // D6: close → cleanup → release, so no claim lands on a bead mid-cleanup
	return Result{Closed: ids, Released: p.Released, Removed: removed, Kept: kept}, nil
}

const inFlightFix = "abort the running effort first (x in the TUI, or mugicrew abort), then try again"

// refuse records <verb>.refused on the target and returns p's refusals as an error.
func refuse(p Plan) error {
	var reasons, blocking, mrs []string
	for _, r := range p.Refusals {
		reasons = append(reasons, r.Reason(p.Kind))
		blocking = append(blocking, r.Beads...)
		if r.MR != "" {
			mrs = append(mrs, r.MR)
		}
	}
	t := p.Set[len(p.Set)-1]
	attrs := append(eventlog.BeadAttrs(t), "reason", strings.Join(reasons, "; "), "blocking", blocking)
	if len(mrs) > 0 {
		attrs = append(attrs, "mr", strings.Join(mrs, ", "))
	}
	slog.Warn(string(p.Kind)+".refused", attrs...)
	return &RefusedError{Verb: string(p.Kind), Target: p.Target, Refusals: p.Refusals}
}

// held is the verb's fence: a liveness hold on every bead of the closing set and
// a status CAS on each open HITL one.
type held struct {
	holds []*liveness.Hold
	cas   []string
}

// fence takes the holds and CAS claims over set. A bead another dispatcher holds
// or claimed (a CAS mismatch) is returned as blocked; any other failure to take a
// hold (no lock dir) or a claim (bd broke) is err. Either way everything taken so far is still in f for drop to undo.
func fence(set []bd.Bead) (f *held, blocked string, err error) {
	f = &held{}
	for _, b := range set {
		h, err := acquire(b.ID)
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
			return f, b.ID, nil
		}
		if err != nil {
			return f, "", fmt.Errorf("hold %s: %w", b.ID, err)
		}
		f.holds = append(f.holds, h)
	}
	for _, b := range set {
		if b.Status != "open" || !isHITL(b.Kind) {
			continue
		}
		if err := casClaim(b.ID); err != nil {
			if errors.Is(err, bd.ErrClaimedSince) || errors.As(err, new(*bd.HeldError)) {
				return f, b.ID, nil
			}
			return f, "", fmt.Errorf("claim %s: %w", b.ID, err)
		}
		f.cas = append(f.cas, b.ID)
	}
	return f, "", nil
}

// release drops the holds; the CAS claims stay (their beads are closed).
func (f *held) release() {
	for _, h := range f.holds {
		h.Release()
	}
}

// drop undoes the fence: the CAS claims back to open, then the holds released.
func (f *held) drop() { f.dropExcept(nil) }

// dropAfterClose is drop after a close that failed part-way: bd close is not
// atomic and bd unclaim resets a bead to open, so a CAS'd bead a re-read finds
// closed stays closed. A re-read that fails unclaims them all.
func (f *held) dropAfterClose() {
	now, err := listAll()
	if err != nil {
		f.drop()
		return
	}
	var closed []string
	for _, b := range now {
		if !b.Unclosed() && slices.Contains(f.cas, b.ID) {
			closed = append(closed, b.ID)
		}
	}
	f.dropExcept(closed)
}

func (f *held) dropExcept(skip []string) {
	for _, id := range f.cas {
		if slices.Contains(skip, id) {
			continue
		}
		_ = unclaim(id) // best effort: a bead that did not revert is a bd fault to surface by hand
	}
	f.release()
}

func isHITL(k contract.Kind) bool {
	e, err := registry.Lookup(k)
	return err == nil && e.Mode == registry.HITL
}

// failed records <verb>.failed at Error and returns err. On a close, closed names
// the ids a re-read finds closed after all (bd close is not atomic). A cancel also
// names the chains it had released by then.
func failed(k Kind, t bd.Bead, step string, err error, tried, released []string) error {
	attrs := append(eventlog.BeadAttrs(t), "step", step, "err", err.Error())
	if tried != nil {
		now, _ := listAll()
		done := []string{}
		for _, b := range now {
			if !b.Unclosed() && slices.Contains(tried, b.ID) {
				done = append(done, b.ID)
			}
		}
		attrs = append(attrs, "closed", done)
	}
	if k == KindCancel {
		attrs = append(attrs, "released", append([]string{}, released...))
	}
	slog.Error(string(k)+".failed", attrs...)
	return err
}

// landingRefusals is Spec D2: an open Landing in the set, or a Landing (the
// container's newest) or Rollback whose recorded MR the Forge may still merge,
// refuses so the Forge cannot merge work the graph dropped. The MR is confirmed by
// a Forge read, never a write; a failed read refuses too. The local land strategy
// has no MR, so only the open Landing applies.
func landingRefusals(set, all []bd.Bead, verb Kind) ([]Refusal, error) {
	var out []Refusal
	var maybe []bd.Bead // Landings and Rollbacks whose MR may be open
	for _, m := range set {
		var kids []bd.Bead
		for _, x := range all {
			if x.Parent == m.ID && x.Kind == contract.KindLanding {
				kids = append(kids, x)
			}
		}
		l, ok := bd.NewestLanding(kids)
		switch {
		case ok && l.Open():
			rf := Refusal{Kind: OpenLanding, Beads: []string{l.ID}, Fix: "let the Landing finish or close it"}
			if path, iid := git.RecordedMRPath(l.Description); iid != 0 {
				rf.MR = git.MRRef(path, iid)
				rf.Fix = fmt.Sprintf("close %s on the Forge, then %s again, or %s", rf.MR, verb, rf.Fix)
			}
			out = append(out, rf)
		case ok && mrMayBeOpen(l):
			maybe = append(maybe, l)
		}
		if m.Kind == contract.KindRollback && git.RecordedMR(m.Description) != 0 {
			maybe = append(maybe, m)
		}
	}
	if len(maybe) == 0 {
		return out, nil
	}
	name, err := strategyName()
	if err != nil {
		return nil, &planFailure{err}
	}
	if name != git.LandStrategyGitlab {
		return out, nil
	}
	for _, m := range maybe {
		path, iid := git.RecordedMRPath(m.Description)
		ref := git.MRRef(path, iid)
		var r git.Repo
		switch m.Kind {
		case contract.KindLanding:
			c, _ := containerOf(m, set)
			r, _ = repoDirOf(c)
		default:
			r, _ = repoDirOf(decidingBead(m, all))
		}
		proj, err := git.SameHostProject(r, projectOf)
		if err == nil {
			var mr *forge.MR
			if mr, err = getMR(proj, iid); err == nil {
				if mr.State == forge.StateOpened {
					out = append(out, Refusal{OpenLanding, []string{m.ID},
						fmt.Sprintf("close %s on the Forge, then %s again", ref, verb), ref})
				}
				continue
			}
		}
		out = append(out, Refusal{OpenLanding, []string{m.ID},
			fmt.Sprintf("can't confirm %s is closed (%v): check the Forge, then %s again", ref, err, verb), ref})
	}
	return out, nil
}

// planFailure is a plan that could not be read (the land strategy), which run
// records as <verb>.failed step plan; a bad target is a plain error with no event.
type planFailure struct{ err error }

func (e *planFailure) Error() string { return e.err.Error() }
func (e *planFailure) Unwrap() error { return e.err }

// decidingBead is the one bead a Rollback's context and repo are read from, as
// Jinbe reads them: the Rollback when it carries a repo: stamp, else the
// Checkpoint it rolled back (a bare Rollback copies no labels, ADR 0157).
func decidingBead(rb bd.Bead, all []bd.Bead) bd.Bead {
	if bd.RepoStampOf(rb) != "" {
		return rb
	}
	for _, id := range rb.Related {
		i := slices.IndexFunc(all, func(x bd.Bead) bool { return x.ID == id && x.Kind == contract.KindCheckpoint })
		if i >= 0 {
			return all[i]
		}
	}
	return rb
}

// mrMayBeOpen reads the graph proxy: l records an MR: line and did not close
// landed: or obsolete: merge request closed.
func mrMayBeOpen(l bd.Bead) bool {
	return git.RecordedMR(l.Description) != 0 &&
		!strings.HasPrefix(l.CloseReason, "landed: ") && !strings.HasPrefix(l.CloseReason, "obsolete: merge request closed")
}

// containerOf is the member of set that parents l.
func containerOf(l bd.Bead, set []bd.Bead) (bd.Bead, bool) {
	i := slices.IndexFunc(set, func(x bd.Bead) bool { return x.ID == l.Parent })
	if i < 0 {
		return bd.Bead{}, false
	}
	return set[i], true
}
