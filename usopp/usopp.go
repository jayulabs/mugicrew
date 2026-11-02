// Package usopp holds Usopp, the AFK reviewer. He owns the one Checkpoint tail
// leaf kind — review — and mutates no tree: one bounded read-only pass over the
// built Checkpoint slice. The review agent runs the repo's two-axis
// code-review workflow whole, its human-report step replaced by findings→beads —
// it files a kind:critique child of the Checkpoint per confirmed blocking finding
// — then Usopp (Go) drives the hybrid clean→Approval transition: read the beads
// the agent wrote and, on zero open critiques, spawn the pinned Approval. Review
// is AFK, so Launch errors loudly, mirroring Franky's Launch.
package usopp

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Usopp is the concrete reviewer crewmate. It carries no state; bd access and the
// harness arrive on the ctx (spec §4), mirroring Nami/Zoro/Franky.
type Usopp struct{}

// Usopp satisfies the crewmate contract.
var _ contract.Crewmate = Usopp{}

// Run is the AFK path the loop dispatches. It re-reads the bead's current graph
// neighborhood (the loop hands only ID+Kind) and runs the one review pass. An
// unowned kind is a loud error, mirroring Franky.Run.
func (Usopp) Run(ctx contract.Ctx, bead contract.Bead) error {
	b, err := bd.Show(bead.ID)
	if err != nil {
		return err
	}
	switch b.Kind {
	case contract.KindReview:
		return runReview(ctx, b)
	case contract.KindForgeReview:
		return runForgeReview(ctx, b)
	}
	return fmt.Errorf("usopp.Run: kind %q has no handler", b.Kind)
}

// Launch composes the HITL command a human opens — but review is AFK (loop-only),
// so this must never be reached: it errors loudly rather than compose a bad
// command, mirroring Franky's Launch guard.
func (Usopp) Launch(_ contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	return nil, fmt.Errorf("usopp.Launch: kind %q is AFK (review), not launchable — dispatched via Run", bead.Kind)
}

// Seams over the git and config legs runReview drives, so the clean→Approval
// transition is testable with a fake harness and no live git. bd is reached only
// through the bd free functions, which a test points at a bdtest graph.
var (
	head          = git.Head
	declaredRepos = cfglayer.Repos
	changedPaths  = git.ChangedPaths
)

// reviewPrompt (review.md) runs the two-axis code-review Crew doc whole —
// Standards (incl. the Fowler smell baseline) and Spec — over the fixed diff
// main...checkpoint/<id> pinned at review start, with its findings going to
// findings→beads instead of a report. The effective-Spec/AMENDMENT rule, Critique
// filing and pivot raise-or-merge are the checkpoint-rules Crew doc, shared with
// Franky and Zoro (ADR 0005, ADR 0043–0045, ADR 0087, ADR 0088); the prompt keeps
// only the commands with its own ids filled in. The Surgeries are always
// read and passed beside the Spec: a Spec-less Checkpoint's Spec axis runs one
// Surgery check per Surgery, or reports "no spec available" with none; the pivot
// section is left out without a Spec (Spec D1, D6). Pure — asserted through the render
// test, mirroring Franky's implementPrompt.
//
//go:embed prompts/review.md
var reviewSrc string

var reviewTmpl = template.Must(template.New("review").Parse(reviewSrc))

// reviewArgs names review.md's slots so the runReview call site is
// transposition-safe: a silent swap (branch↔pin) of bare positional strings
// would slip past the compiler.
type reviewArgs struct {
	ReviewID     string    // the Review bead id
	CheckpointID string    // the Checkpoint under review, --parent for filed critiques
	SpecID       string    // the Spec axis's source, empty on a Spec-less Checkpoint
	Surgeries    []bd.Bead // the Checkpoint's Surgeries, filing order: ID and Title are rendered
	Branch       string    // checkpoint/<id>
	Pin          string    // the pinned SHA the diff is cut against
	Context      string    // the Checkpoint's declared context, empty when unfocused
	Subtree      string    // that context's path
	OutOfScope   []string  // changed paths outside Subtree, minus the exempt set; empty renders nothing
}

// Draft is the review's shared Draft block.
func (a reviewArgs) Draft() string { return contract.DraftInstruction(a.ReviewID) }

// reviewPrompt renders review.md from a reviewArgs. Pure, so the render is checkable
// without bd/git.
func reviewPrompt(a reviewArgs) string { return contract.Render(reviewTmpl, a) }

// runReview is the one review handler (FR26–FR31): pin the Checkpoint branch HEAD
// at review start and write it onto the Review as its Commit: line (the Review was
// spawned BARE by Jinbe),
// resolve the Spec and the Surgeries for the Spec axis, fork the read-only review agent (it files
// critique beads, a pivot, or nothing), then drive the tail: a raised pivot wins
// over the clean→Approval transition. An errored fork leaves the Review open for
// the next tick, mirroring runImplement.
func runReview(ctx contract.Ctx, bead bd.Bead) error {
	if !bead.Open() {
		return nil // already closed by a prior dispatch: no double close / double approve
	}
	if bead.Parent == "" {
		return fmt.Errorf("usopp: review bead %s has no parent Checkpoint membership edge", bead.ID)
	}
	branch := git.BranchName(string(contract.KindCheckpoint), bead.Parent)
	pin, err := pinOf(bead.Parent, branch)
	if err != nil {
		return err
	}
	if next, changed := git.WithCommitPin(bead.Description, pin); changed {
		if err := bd.SetDescription(bead.ID, next, bd.UnderClaim(bead.ID)); err != nil {
			return err
		}
	}
	specID, _, err := bd.SpecOf(bead.Parent)
	if err != nil {
		return err
	}
	surgeries, err := bd.SurgeriesOf(bead.Parent)
	if err != nil {
		return err
	}
	args := reviewArgs{ReviewID: bead.ID, CheckpointID: bead.Parent, SpecID: specID, Surgeries: surgeries, Branch: branch, Pin: pin}
	focus, err := focusReview(&args)
	if err != nil {
		return err
	}
	prompt := reviewPrompt(args)
	if focus != "" {
		prompt += "\n\n" + focus
	}
	inv, err := contract.NewInvocation(bead.ID, prompt)
	if err != nil {
		return err
	}
	fresh, _, _, open, err := bd.RunSession(ctx, inv, bd.Show)
	if err != nil {
		return err
	}
	// The tie runs before anything publishes, even when the session closed its
	// own bead (open=false): dispatch still publishes that bead's Drafts after a
	// nil return.
	if err := bd.TieDrafts(fresh.ID, contract.KindCritique); err != nil {
		return err
	}
	if !open {
		return nil
	}
	if raised, err := bd.RaiseIfPivoted(ctx, fresh, specID); err != nil || raised {
		return err
	}
	return approveIfClean(ctx, fresh, pin)
}

// pinOf is the Checkpoint branch's live tip: read in the child repo for a repo
// context. With no repo = declared it reads no bead (ADR 0157's second rule).
func pinOf(checkpointID, branch string) (string, error) {
	if len(declaredRepos(".")) > 0 {
		cp, err := bd.Show(checkpointID)
		if err != nil {
			return "", err
		}
		if r := contexts.RepoOf(cp); r != "" {
			return r.Head(branch)
		}
	}
	return head(branch)
}

// focusReview fills the out-of-scope slots for a Checkpoint labelled with a declared
// context and returns its focus line ("" when none). With no contexts declared it
// reads no bead and does no git work; with an unlabelled Checkpoint, no git work.
func focusReview(a *reviewArgs) (string, error) {
	cs := contexts.Contexts(".")
	if len(cs) == 0 {
		return "", nil
	}
	cp, err := bd.Show(a.CheckpointID)
	if err != nil {
		return "", err
	}
	name := bd.ContextOf(cp)
	subtree, ok := cs[name]
	if name == "" || !ok {
		return "", nil
	}
	if contexts.RepoOf(cp) != "" { // the whole child is in scope: no paths to compute
		a.Context, a.Subtree = name, subtree
		return contexts.FullLine(cs, cp), nil
	}
	paths, err := changedPaths(git.Mainline(), a.Pin)
	if err != nil {
		return "", err
	}
	a.Context, a.Subtree, a.OutOfScope = name, subtree, contexts.OutOfScope(paths, subtree)
	return contexts.FullLine(cs, cp), nil
}

// approveIfClean is the hybrid clean→Approval transition (FR30/FR31). After the
// agent has filed its critique beads (or nothing), Go reads the Checkpoint's open
// kind:critique children — the beads the agent wrote, no brittle text parse — and
// on ZERO open spawns the Approval FIRST (stamped with the pinned SHA), THEN
// hands the Review's close to the publish in EVERY case — so the critiques the
// agent drafted become visible together with the close (Spec D6, D11 row 4). The
// Approval stays a one-call create (inventory row 15). The Approval is a membership child of the
// Checkpoint carrying the Commit: pin, born caused-by the clean Review (one create with
// its parent edge) and with NO HEAD re-check
// (a stale pin self-heals). Both legs are idempotent — SpawnChildIfAbsent's
// open-child guard and runReview's Open() guard — so a re-dispatch converges
// without a second Approval or a double close. Its read-then-writes (and
// obsoleteStaleApprovals') are serialized by the Review's claim plus graph
// ordering: no Build or critique is open while a Review runs (ADR 0070).
func approveIfClean(ctx contract.Ctx, bead bd.Bead, pin string) error {
	critiques, err := bd.OpenChildrenOfKindBeads(bead.Parent, contract.KindCritique)
	if err != nil {
		return err
	}
	pivots, err := bd.OpenPivots(bead.Parent)
	if err != nil {
		return err
	}
	approvalID := ""
	if len(critiques) == 0 && len(pivots) == 0 {
		cp, err := bd.Show(bead.Parent) // the Approval inherits the Checkpoint's title
		if err != nil {
			return err
		}
		if err := obsoleteStaleApprovals(bead.Parent, pin); err != nil {
			return err
		}
		if approvalID, _, err = bd.SpawnChildIfAbsent(bead.Parent, approvalSuccessor(cp.Title, pin, bead.ID)); err != nil {
			return err
		}
	}
	return bd.CloseOnPublish(ctx, bead.ID, reviewCloseReason(critiques, pivots, approvalID), nil)
}

// reviewCloseReason names a Review's outcome (ADR 0415): open Critiques win over a
// pivot (the findings are what the review produced), a pivot alone holds the
// Approval, and neither is a clean pass naming the Approval at head. Ids keep the
// graph-read order, joined ", " like the pivoted: reason.
func reviewCloseReason(critiques, pivots []bd.Bead, approvalID string) string {
	switch {
	case len(critiques) > 0:
		return bd.CritiquedReason + " " + strings.Join(bd.IDs(critiques), ", ")
	case len(pivots) > 0:
		return bd.HeldReason + " " + strings.Join(bd.IDs(pivots), ", ")
	}
	return bd.CleanReason + " " + approvalID
}

// obsoleteStaleApprovals closes any OPEN Approval of the Checkpoint whose Commit:
// pin no longer equals the reviewed HEAD, so this clean re-review's spawn cannot be
// swallowed by SpawnChildIfAbsent's open-child dedup reusing an Approval pinned to a
// superseded commit. That reuse leaves approvedAtHead false forever and
// Jinbe never lands — this is the "obsoleted and respawned off the new clean Review"
// self-heal GLOSSARY.md's Approval note and FR31/FR24 promise. An open Approval
// ALREADY at HEAD is left untouched for the spawn to reuse, so a plain re-dispatch
// (unchanged HEAD) neither churns nor double-approves.
func obsoleteStaleApprovals(checkpointID, pin string) error {
	open, err := bd.OpenChildrenOfKindBeads(checkpointID, contract.KindApproval)
	if err != nil {
		return err
	}
	for _, a := range open {
		if got, ok := git.ParseCommit(a.Description); !ok || got != pin {
			if err := bd.Close(a.ID, bd.ApprovalObsoletedReason); err != nil {
				return err
			}
		}
	}
	return nil
}

// approvalSuccessor is the one Approval a clean review hands off (FR31): a
// membership child kind:approval leaf of the Checkpoint, carrying the Commit: pin
// captured at review start (git.CommitPin, ParseCommit greps it back), titled
// exactly the Checkpoint's title (the kind rides on the label). Pure, so
// the emitted shape (leaf, kind, pinned body, caused-by) is checkable without a DB,
// mirroring Jinbe's reviewSuccessor. reviewID is the clean Review it certifies; an
// open Approval reused by SpawnChildIfAbsent keeps the edges it was born with.
func approvalSuccessor(checkpointTitle, pin, reviewID string) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindApproval,
		Container:   false,
		Title:       checkpointTitle,
		CausedBy:    reviewID,
		Description: "Clean review pass over the built Checkpoint slice at the pinned commit.\n" + git.CommitPin(pin),
		Serializer:  bd.UnderClaim(reviewID),
	}
}
