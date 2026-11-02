package loop

import (
	"slices"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Mode is who is asking the verdict: the loop's tick or a human's run-now press. The two
// differ only by the named list in ADR 0377 (loop-and-run-now-eligibility-differ…).
type Mode int

const (
	ModeLoop Mode = iota
	ModeRunNow
)

// Action is what a runnable verdict's dispatch does. ActionReview occurs only in ModeLoop.
type Action string

const (
	ActionRun       Action = "run"
	ActionSpec      Action = "spec"
	ActionLand      Action = "land"
	ActionRetry     Action = "retry"
	ActionRetryHold Action = "retry hold"
	ActionReview    Action = "review"
)

// Cause is the one reason a not-runnable verdict names: the first failing leg in ADR 0378
// (an-eligibility-verdict-names-one-cause…) order. The list is closed: callers map every
// cause to their own words, so a new one must show up in review.
type Cause string

const (
	CauseNotCandidate     Cause = "not-candidate"
	CausePaused           Cause = "paused"
	CauseInheritedPause   Cause = "inherited-pause"
	CauseContainerFrozen  Cause = "container-frozen"
	CauseBlocked          Cause = "blocked"
	CauseBehindMainline   Cause = "behind-mainline"
	CausePaced            Cause = "paced"
	CauseSurgeryHold      Cause = "surgery-hold"
	CauseUndrained        Cause = "undrained"
	CauseLandingOpen      Cause = "landing-open"
	CauseForeignLanding   Cause = "foreign-landing"
	CauseDefectOpen       Cause = "defect-open"
	CauseAwaitingApproval Cause = "awaiting-approval"
	CausePivotOpen        Cause = "pivot-open"
	CauseNoMembers        Cause = "no-members"
	CauseLandHeld         Cause = "land-held"
	CauseReviewDue        Cause = "review-due"
)

// Causes is every Cause, in precedence order where one applies.
var Causes = []Cause{
	CauseNotCandidate, CausePaused, CauseInheritedPause, CauseContainerFrozen, CauseBlocked, CauseBehindMainline, CausePaced,
	CauseSurgeryHold, CauseUndrained, CauseLandingOpen, CauseForeignLanding,
	CauseDefectOpen, CausePivotOpen, CauseAwaitingApproval, CauseNoMembers, CauseLandHeld, CauseReviewDue,
}

// Holder is a bead that holds a verdict's cause, whole so a caller's wording can name its
// title and kind. Branch is the effort branch of a held (closed, unintegrated) bead, else "".
type Holder struct {
	Bead   bd.Bead
	Branch string
}

// Verdict is the eligibility answer: Runnable with an Action, or one Cause with its Holders.
// PauseCause is the top-ranked pause cause verbatim (bd.PauseRank), set only for CausePaused.
// Foreign is set only for CauseForeignLanding. It holds no text.
type Verdict struct {
	Runnable   bool
	Action     Action
	Cause      Cause
	PauseCause string
	Holders    []Holder
	Foreign    *git.ForeignLanding
}

// Blocker is a raw blocker bead with the type of the edge that names it.
type Blocker struct {
	Bead bd.Bead
	Edge string
}

// Edge types a Blocker carries that the verdict acts on; the verdict ignores the rest
// (conditional-blocks), as the live read does.
const (
	EdgeBlocks      = "blocks"
	EdgeConditional = "conditional-blocks"
)

// Trailer is a Container's land trailer read: Landed with its Sha when the trailer is on
// mainline, or Foreign when another Bead Graph wrote it (then Landed is false).
type Trailer struct {
	Sha     string
	Landed  bool
	Foreign *git.ForeignLanding
}

// View is the raw graph facts the verdict reads (ADR 0379 (the-eligibility-verdict-reads-
// raw-graph…)). The verdict computes every leg itself over it, so the adapters differ only
// in what they read and how fresh it is. A failed read returns an error, never a cause.
type View interface {
	// Children is a Container's children, open and closed.
	Children(id string) ([]bd.Bead, error)
	// Efforts is the set of bead ids holding an effort/<id> branch.
	Efforts() (*git.EffortSet, error)
	Landed(b bd.Bead) (Trailer, error)
	// CheckpointTip is the checkpoint/<id> tip, "" when unknown.
	CheckpointTip(id string) (string, error)
	// Contains is whether sha is an ancestor of (or is) tip, in cp's repo. A failed read is an
	// error, never "no".
	Contains(cp bd.Bead, tip, sha string) (bool, error)
	// SameRepo is whether a and b live in the same git repo; a sha landed in another one is
	// unknown to the other's branches, so no ancestry read is made.
	SameRepo(a, b bd.Bead) bool
	Blockers(b bd.Bead) ([]Blocker, error)
	// Bead resolves an id, for an inherited-pause walk hop and a parent.
	Bead(id string) (bd.Bead, error)
}

// Inputs are the mode inputs the caller supplies: Paced in ModeLoop (nil never paces),
// Force in ModeRunNow (it waives the blocked and behind-mainline legs only).
type Inputs struct {
	Paced func(bd.Bead) bool
	Force bool
	// NoInheritable is the caller's word that no bead in its graph carries a manual or
	// context pause, so the inherited-pause walk is skipped (no bd show per hop).
	NoInheritable bool
}

// retryable is run-now's pass list of pause causes (ADR 0377 §2): a bead whose causes are
// all on it is retried by hand. Every other cause, one added later included, refuses.
var retryable = []string{bd.PausedFailing, bd.PausedIntegrateConflict, bd.PausedNotBuilt, bd.PausedUnmet, bd.PausedProfile, bd.PausedForeignHold}

// Judge is the eligibility verdict for b in mode over view. frontier is the ready set the
// caller has already widened (Readmit, straggler retirement, and in run-now the forced
// bead). It never writes. The error carries only failed reads.
func Judge(b bd.Bead, mode Mode, frontier []bd.Bead, in Inputs, view View) (Verdict, error) {
	no := func(c Cause, holders ...Holder) (Verdict, error) {
		return Verdict{Cause: c, Holders: holders}, nil
	}
	leaf, chart, cp := IsAFKLeaf(b), IsDrainedChartCandidate(b), IsCheckpointCandidate(b)
	if b.Status != "open" || !(leaf || chart || cp) {
		return no(CauseNotCandidate)
	}
	retry := Action("")
	if causes := bd.PausedCauses(b); len(causes) > 0 {
		if mode == ModeLoop || slices.ContainsFunc(causes, func(c string) bool { return !slices.Contains(retryable, c) }) {
			return Verdict{Cause: CausePaused, PauseCause: causes[0]}, nil
		}
		retry = ActionRetry
		if causes[0] == bd.PausedForeignHold {
			retry = ActionRetryHold
		}
	}
	if !in.NoInheritable {
		anc, err := inheritedPauseOf(b, frontier, view)
		if err != nil {
			return Verdict{}, err
		}
		if anc != nil {
			return no(CauseInheritedPause, Holder{Bead: *anc})
		}
	}
	if frozen, err := frozenBy(b, view); err != nil {
		return Verdict{}, err
	} else if len(frozen) > 0 {
		return no(CauseContainerFrozen, frozen...)
	}
	onFrontier := slices.ContainsFunc(frontier, func(f bd.Bead) bool { return f.ID == b.ID })
	if !onFrontier && cp { // a landed Checkpoint is past every blocker, a pivot's included: closeLanded must run
		if t, err := view.Landed(b); err != nil {
			return Verdict{}, err
		} else if t.Landed {
			onFrontier = true
		}
	}
	if !(mode == ModeRunNow && in.Force) && !onFrontier {
		held, err := blockedBy(b, view)
		if err != nil {
			return Verdict{}, err
		}
		return no(CauseBlocked, held...)
	}
	if !(mode == ModeRunNow && in.Force) && leaf && b.Kind == contract.KindBuild {
		behind, err := behindMainlineBy(b, view)
		if err != nil {
			return Verdict{}, err
		}
		if len(behind) > 0 {
			return no(CauseBehindMainline, behind...)
		}
	}
	if mode == ModeLoop && in.Paced != nil && in.Paced(b) {
		return no(CausePaced)
	}
	var (
		v   Verdict
		err error
	)
	switch {
	case leaf:
		v, err = leafArm(b, view)
	case chart:
		v, err = chartArm(b, view)
	default:
		v, err = checkpointArm(b, mode, view)
	}
	if err == nil && v.Runnable && retry != "" {
		v.Action = retry
	}
	return v, err
}

func runnable(a Action) Verdict { return Verdict{Runnable: true, Action: a} }

// inheritedPauseOf is b's nearest manual/context-paused ancestor, resolved off the
// frontier and then view.Bead; nil for none.
func inheritedPauseOf(b bd.Bead, frontier []bd.Bead, view View) (*bd.Bead, error) {
	byID := map[string]bd.Bead{}
	for _, f := range frontier {
		byID[f.ID] = f
	}
	lookup := func(id string) (bd.Bead, error) {
		if x, ok := byID[id]; ok {
			return x, nil
		}
		x, err := view.Bead(id)
		if err == nil {
			byID[id] = x
		}
		return x, err
	}
	id, err := bd.PausedAncestor(b, lookup)
	if err != nil || id == "" {
		return nil, err
	}
	anc := byID[id]
	return &anc, nil
}

// openBlocks is the blockers that hold: open, over a blocks edge.
func openBlocks(b bd.Bead, view View) ([]bd.Bead, error) {
	bl, err := view.Blockers(b)
	if err != nil {
		return nil, err
	}
	var out []bd.Bead
	for _, x := range bl {
		if x.Edge == EdgeBlocks && x.Bead.Open() {
			out = append(out, x.Bead)
		}
	}
	return out, nil
}

func holders(bs []bd.Bead) []Holder {
	out := make([]Holder, len(bs))
	for i, x := range bs {
		out[i] = Holder{Bead: x}
	}
	return out
}

// frozenBy is the open blockers of b's Checkpoint parent that freeze its members: any
// whose kind doesn't admit a land-only hold (ADR 0269, ADR 0270).
func frozenBy(b bd.Bead, view View) ([]Holder, error) {
	if b.Parent == "" {
		return nil, nil
	}
	p, err := view.Bead(b.Parent)
	if err != nil || p.Kind != contract.KindCheckpoint {
		return nil, err
	}
	blocks, err := openBlocks(p, view)
	if err != nil {
		return nil, err
	}
	return holders(slices.DeleteFunc(blocks, func(x bd.Bead) bool { return x.Kind.AdmitsLandOnlyHold() })), nil
}

// blockedBy is b's own open blockers plus those of the nearest ancestor container that
// freezes it: a Chart with any open blocker, a Checkpoint with a freezing one.
func blockedBy(b bd.Bead, view View) ([]Holder, error) {
	own, err := openBlocks(b, view)
	if err != nil {
		return nil, err
	}
	out := holders(own)
	seen := map[string]bool{b.ID: true}
	for id := b.Parent; id != "" && !seen[id]; {
		seen[id] = true
		anc, err := view.Bead(id)
		if err != nil {
			return nil, err
		}
		if anc.Unclosed() {
			blocks, err := openBlocks(anc, view)
			if err != nil {
				return nil, err
			}
			for _, h := range blocks {
				if anc.Kind == contract.KindChart || (anc.Kind == contract.KindCheckpoint && !h.Kind.AdmitsLandOnlyHold()) {
					return append(out, Holder{Bead: h}), nil
				}
			}
		}
		id = anc.Parent
	}
	return out, nil
}

// behindMainlineBy is the closed blocks blockers of Build b, a member of a Checkpoint, whose
// container landed on mainline outside b's Checkpoint with a land commit missing from
// checkpoint/<parent> (ADR 0420 (a-build-behind-mainline-is-held…)). An unknown tip holds nothing.
func behindMainlineBy(b bd.Bead, view View) ([]Holder, error) {
	if b.Parent == "" {
		return nil, nil
	}
	cp, err := view.Bead(b.Parent)
	if err != nil || cp.Kind != contract.KindCheckpoint {
		return nil, err
	}
	blockers, err := view.Blockers(b)
	if err != nil {
		return nil, err
	}
	var out []Holder
	tip, tipRead := "", false
	for _, x := range blockers {
		if x.Edge != EdgeBlocks || x.Bead.Status != "closed" {
			continue
		}
		container, ok, err := BlockerContainer(x.Bead, view.Bead)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if container.ID == cp.ID {
			continue
		}
		if !view.SameRepo(container, cp) {
			continue
		}
		t, err := view.Landed(container)
		if err != nil {
			return nil, err
		}
		if !t.Landed {
			continue
		}
		if !tipRead {
			if tip, err = view.CheckpointTip(cp.ID); err != nil {
				return nil, err
			}
			tipRead = true
		}
		if tip == "" {
			return nil, nil
		}
		in, err := view.Contains(cp, tip, t.Sha)
		if err != nil {
			return nil, err
		}
		if !in {
			out = append(out, Holder{Bead: x.Bead})
		}
	}
	return out, nil
}

// BlockerContainer is the container a blocks blocker belongs to for the behind-mainline leg: the
// blocker itself when it is a Chart or Checkpoint, else its parent when that is one. ok is false
// when it has none. bead resolves a parent id.
func BlockerContainer(blocker bd.Bead, bead func(id string) (bd.Bead, error)) (bd.Bead, bool, error) {
	if isContainer(blocker) {
		return blocker, true, nil
	}
	if blocker.Parent == "" {
		return bd.Bead{}, false, nil
	}
	parent, err := bead(blocker.Parent)
	if err != nil {
		return bd.Bead{}, false, err
	}
	return parent, isContainer(parent), nil
}

func isContainer(b bd.Bead) bool {
	return b.Kind == contract.KindChart || b.Kind == contract.KindCheckpoint
}

func leafArm(b bd.Bead, view View) (Verdict, error) {
	if b.Kind == contract.KindCritique && b.Parent != "" {
		kids, err := view.Children(b.Parent)
		if err != nil {
			return Verdict{}, err
		}
		if open := openOfKind(kids, contract.KindSurgery); len(open) > 0 {
			return Verdict{Cause: CauseSurgeryHold, Holders: holders(open)}, nil
		}
	}
	return runnable(ActionRun), nil
}

func openOfKind(kids []bd.Bead, kinds ...contract.Kind) []bd.Bead {
	return filter(kids, func(k bd.Bead) bool { return k.Open() && slices.Contains(kinds, k.Kind) })
}

// filter is the beads of bs that keep reports, in order, leaving bs untouched.
func filter(bs []bd.Bead, keep func(bd.Bead) bool) []bd.Bead {
	return slices.DeleteFunc(slices.Clone(bs), func(b bd.Bead) bool { return !keep(b) })
}

// undrainedChart is the Chart's undrained holders: the open non-paced children, then the
// held closed ones with their unintegrated effort branches.
func undrainedChart(kids []bd.Bead, efforts *git.EffortSet) []Holder {
	var out []Holder
	for _, k := range kids {
		if k.Open() && !k.Kind.PacedLeaf() {
			out = append(out, Holder{Bead: k})
		}
	}
	_, held := bd.DrainBlockersOver(kids, efforts)
	return append(out, heldHolders(held)...)
}

// undrainedMembers is the Checkpoint's undrained holders: the open members, then the closed
// members still holding an unintegrated effort branch (ADR 0394). It is the same predicate as bd.MembersDrainedOver
// (a landed-reason close with a kept ref holds too), so the holders are nil exactly when
// the members are drained.
func undrainedMembers(kids []bd.Bead, efforts *git.EffortSet) []Holder {
	var out []Holder
	for _, k := range kids {
		switch {
		case !bd.IsMember(k.Kind):
		case k.Open():
			out = append(out, Holder{Bead: k})
		case efforts.Held(k.ID):
			out = append(out, Holder{Bead: k, Branch: git.EffortBranch(k.ID)})
		}
	}
	return out
}

func heldHolders(held []bd.Bead) []Holder {
	out := make([]Holder, len(held))
	for i, k := range held {
		out[i] = Holder{Bead: k, Branch: git.EffortBranch(k.ID)}
	}
	return out
}

func chartArm(b bd.Bead, view View) (Verdict, error) {
	kids, err := view.Children(b.ID)
	if err != nil {
		return Verdict{}, err
	}
	efforts, err := view.Efforts()
	if err != nil {
		return Verdict{}, err
	}
	if open, held := bd.DrainBlockersOver(kids, efforts); open > 0 || len(held) > 0 {
		return Verdict{Cause: CauseUndrained, Holders: undrainedChart(kids, efforts)}, nil
	}
	t, err := view.Landed(b)
	if err != nil {
		return Verdict{}, err
	}
	if !t.Landed {
		if open := openOfKind(kids, contract.KindLanding); len(open) > 0 {
			return Verdict{Cause: CauseLandingOpen, Holders: holders(open)}, nil
		}
		if t.Foreign != nil {
			return Verdict{Cause: CauseForeignLanding, Foreign: t.Foreign}, nil
		}
	}
	return runnable(ActionSpec), nil
}

func checkpointArm(b bd.Bead, mode Mode, view View) (Verdict, error) {
	kids, err := view.Children(b.ID)
	if err != nil {
		return Verdict{}, err
	}
	// Deliberate (D2): this git read runs before the open-defect check below, so every
	// verdict on a Checkpoint pays it.
	t, err := view.Landed(b)
	if err != nil {
		return Verdict{}, err
	}
	if t.Landed { // the trailer wins over an open defect: closeLanded runs as soon as the land is seen
		return runnable(ActionLand), nil
	}
	if defects := openOfKind(kids, contract.KindConflict, contract.KindCritique); len(defects) > 0 {
		return Verdict{Cause: CauseDefectOpen, Holders: holders(defects)}, nil
	}
	if t.Foreign != nil {
		return Verdict{Cause: CauseForeignLanding, Foreign: t.Foreign}, nil
	}
	if open := openOfKind(kids, contract.KindLanding); len(open) > 0 {
		return Verdict{Cause: CauseLandingOpen, Holders: holders(open)}, nil
	}
	tip, err := view.CheckpointTip(b.ID)
	if err != nil {
		return Verdict{}, err
	}
	atHead := func(a bd.Bead) bool {
		pin, ok := git.ParseCommit(a.Description)
		return ok && tip != "" && pin == tip
	}
	efforts, err := view.Efforts()
	if err != nil {
		return Verdict{}, err
	}
	drainedHolders := func() []Holder { return undrainedMembers(kids, efforts) }
	if slices.ContainsFunc(kids, func(k bd.Bead) bool { return k.Kind == contract.KindApproval && k.ApprovedClosed() && atHead(k) }) {
		if h := drainedHolders(); h != nil {
			return Verdict{Cause: CauseUndrained, Holders: h}, nil
		}
		held, err := openBlocks(b, view)
		if err != nil {
			return Verdict{}, err
		}
		if len(held) > 0 {
			return Verdict{Cause: CauseLandHeld, Holders: holders(held)}, nil
		}
		return runnable(ActionLand), nil
	}
	blocks, err := openBlocks(b, view)
	if err != nil {
		return Verdict{}, err
	}
	if pivots := filter(blocks, func(x bd.Bead) bool { return x.Kind == contract.KindPivot }); len(pivots) > 0 {
		return Verdict{Cause: CausePivotOpen, Holders: holders(pivots)}, nil
	}
	if h := drainedHolders(); h != nil {
		return Verdict{Cause: CauseUndrained, Holders: h}, nil
	}
	if waiting := filter(kids, func(k bd.Bead) bool { return k.Kind == contract.KindApproval && k.Open() && atHead(k) }); len(waiting) > 0 {
		return Verdict{Cause: CauseAwaitingApproval, Holders: holders(waiting)}, nil
	}
	if !slices.ContainsFunc(kids, func(k bd.Bead) bool { return bd.IsMember(k.Kind) }) {
		return Verdict{Cause: CauseNoMembers}, nil
	}
	if mode == ModeRunNow {
		return Verdict{Cause: CauseReviewDue}, nil
	}
	return runnable(ActionReview), nil
}

// ContainsKey is the key of the ancestry fact "sha is an ancestor of tip", for the views that
// cache Contains answers.
func ContainsKey(tip, sha string) string { return sha + "@" + tip }
