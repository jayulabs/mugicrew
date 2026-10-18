// Package franky holds Franky, the AFK build/critique/conflict workhorse. He owns
// the three Checkpoint leaf kinds — build, critique, conflict — whose work reaches
// the Checkpoint branch. Each handler forks an agent that authors +
// commits its slice in its routed tree, its own effort/<id> (a Build or Critique
// squash-integrates onto checkpoint/<parent-id>; a Conflict replace-CAS publishes
// its rebase of it), then
// Franky drives the graph: one shared close tail (Jinbe spawns the Review).
// All three are AFK; his one HITL kind, the Surgery, is a dictation
// session Launch opens on its own effort/<id> (Spec D4, Spec D1).
package franky

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// Franky is the concrete build/critique/conflict crewmate. It carries no state;
// bd access and the harness arrive on the ctx (spec §4), mirroring Nami/Zoro.
type Franky struct{}

// Franky satisfies the crewmate contract.
var _ contract.Crewmate = Franky{}

// Run is the AFK path the loop dispatches. It re-reads the bead's current graph
// neighborhood (the loop hands only ID+Kind) and switches on kind: build and
// critique share the implement job (build.md), conflict re-runs the rebase
// (conflict.md). An unowned kind is a loud error, mirroring Nami.Run.
func (Franky) Run(ctx contract.Ctx, bead contract.Bead) error {
	b, err := showBead(bead.ID)
	if err != nil {
		return err
	}
	switch b.Kind {
	case contract.KindBuild, contract.KindCritique:
		return runImplement(ctx, b)
	case contract.KindConflict:
		return runConflict(ctx, b)
	default:
		return fmt.Errorf("franky.Run: kind %q has no handler", b.Kind)
	}
}

// Launch composes the HITL command a human opens. Franky's one HITL kind is the
// Surgery (Spec D4): l on its row opens a fresh dictation session over
// surgeryPrompt, routed to effort/<id> (integrating onto checkpoint/<parent>) by
// the route arm. The prompt names the Checkpoint the Surgery joins and its bd
// context, read at launch (surgeryContext). Build, critique
// and conflict are AFK (loop-only), so Launch errors loudly for them rather than
// compose a bad command, mirroring Sanji's Run guard.
func (Franky) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindSurgery {
		return nil, fmt.Errorf("franky.Launch: kind %q is AFK (build/critique/conflict), not launchable — dispatched via Run", bead.Kind)
	}
	a, err := surgeryContext(bead.ID)
	if err != nil {
		return nil, err
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusUnder(surgeryPrompt(a), bead.ID, showBead))
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

// Seams over the bd legs the handlers drive, so the close and pivot tails are
// testable with a fake harness and no live bd/git (mirrors zoro/nami's var
// seam). These alias the real package funcs; the franky test swaps them.
var (
	showBead  = bd.Show
	closeBead = bd.Close

	openPivots           = bd.OpenPivots
	pivotsDiscoveredFrom = bd.PivotsDiscoveredFrom
	raisePivot           = bd.RaisePivot
	specOf               = bd.SpecOf
	surgeriesOf          = bd.SurgeriesOf
	childrenOfKind       = bd.ChildrenOfKind

	commitsAhead         = git.Repo.CommitsAhead
	commitsByTrailer     = git.Repo.CommitsByTrailer
	checkpointChildCount = bd.ChildCount
	integrateSurgery     = integrateEffort
)

// integrateEffort is surgery end's first leg (Spec D8, FR 33): mugicrew
// integrate in the Surgery's tree on effort/<id>. Run elsewhere, it refuses while
// effort/<id> exists, since counting then would miss the tree's work; with no
// effort branch there is nothing to integrate.
func integrateEffort(sg bd.Bead, r git.Repo) error {
	var child session.Child
	id, err := git.CurrentEffort()
	if err != nil && r != "" { // a nested Surgery's host is detached: its one child on effort/<id> integrates
		if cwd, werr := os.Getwd(); werr == nil {
			if kids := session.ProbeChildren(cwd); len(kids) == 1 {
				child, id, err = kids[0], kids[0].Effort, nil
			}
		}
	}
	if err != nil || id != sg.ID {
		refs, err := r.EffortRefs()
		if err == nil && refs[sg.ID] {
			err = fmt.Errorf("surgery end: run it in the Surgery's tree on %s, so its work is integrated first", git.EffortBranch(sg.ID))
		}
		return err
	}
	by := eventlog.ByHuman
	if session.InPane() {
		by = eventlog.ByAgent
	}
	if rc := session.IntegrateIn(child, sg, by, os.Stdout); rc != session.ExitIntegrated {
		return fmt.Errorf("surgery end: mugicrew integrate exited %d; nothing was closed", rc)
	}
	return nil
}

// implementPrompt (build.md) carries implement's four steps inline over one Build
// or Critique: test-first from the tdd Crew doc, check as you go, the full suite at
// the end, and a self-pass from the code-review Crew doc. conflictPrompt
// (conflict.md) carries the merge-conflict resolution steps inline over a conflict
// bead, with the base SHA to rebase onto, and a numbered-file clash goes to the
// sequence-renumber Crew doc (ADR 0030). Both read the checkpoint-rules Crew doc for the
// effective Spec, Critiques and pivots (ADR 0005, ADR 0043–0045, ADR 0087,
// ADR 0088), keeping only the commands with their own ids filled in. Both carry the Checkpoint's Spec resolved through bd.SpecOf, so a
// Rollback successor's agent reads the real Spec, not its Rollback (Spec
// Decision 6), mirroring Usopp's reviewPrompt. Its Surgeries ride alongside
// (intent.md); the pivot section keys on the Spec alone (Spec D6). Both replace only the terminal
// graph mutation with Go — the agent authors + commits, Franky closes (FR25). Pure — asserted through the
// render tests, mirroring Zoro's slicingPrompt.
//
//go:embed prompts/build.md
var buildSrc string

//go:embed prompts/conflict.md
var conflictSrc string

//go:embed prompts/intent.md
var intentSrc string

var (
	buildTmpl    = promptTmpl("build", buildSrc)
	conflictTmpl = promptTmpl("conflict", conflictSrc)
)

// promptTmpl parses src with intent.md's shared partials.
func promptTmpl(name, src string) *template.Template {
	return template.Must(template.Must(template.New(name).Parse(src)).Parse(intentSrc))
}

// promptArgs names build.md's and conflict.md's slots; Base is conflict's only,
// Checkpoint build.md's.
// SpecID and Surgeries are independent: either, both or neither.
type promptArgs struct {
	BeadID     string    // the Build, Critique or Conflict
	Checkpoint string    // a Build's or Critique's Checkpoint, its effort's container
	Base       string    // the SHA a Conflict, or a Base-pinned Critique, rebases onto
	SpecID     string    // the Checkpoint's Spec
	Surgeries  []bd.Bead // the Checkpoint's Surgeries, filing order: ID and Title are rendered
}

// Draft is the prompt's shared Draft block.
func (a promptArgs) Draft() string { return contract.DraftInstruction(a.BeadID) }

// surgeryPrompt (surgery.md) is one dictation session over a Surgery: start
// (read the Workspace docs, claim, read the record and git log, rebuild a missing
// record, re-read the Checkpoint's children), dictation, the per-session record,
// stop (the idle line) and end (mugicrew surgery end). It names the Checkpoint
// the Surgery joins and renders each context block only when non-empty (Spec
// D10). No pivot section: a Surgery never raises a pivot, Spec or not.
// The ClosingLine splice is the Launch invoker's, unchanged. Pure.
//
//go:embed prompts/surgery.md
var surgeryTmpl string

// surgeryArgs is surgery.md's data, read from bd at launch. One template serves
// a fresh Checkpoint (only the opener) and a joined one (every block).
type surgeryArgs struct {
	ID              string // the Surgery
	Checkpoint      string // the Checkpoint it joins
	CheckpointTitle string
	SpecID          string // the Checkpoint's Spec, "" if none
	SpecTitle       string
	Earlier         []bd.Bead // closed (abandoned included) sibling Surgeries, filing order
	Builds          []bd.Bead // open Builds on the Checkpoint
	Critiques       []bd.Bead // open Critiques on the Checkpoint
}

func surgeryPrompt(a surgeryArgs) string {
	return fmt.Sprintf(surgeryTmpl, a.ID, contract.DraftInstruction(a.ID), surgeryOpener(a), surgeryBlocks(a), regexp.QuoteMeta(a.ID))
}

func surgeryOpener(a surgeryArgs) string {
	return fmt.Sprintf("for Checkpoint %s — %s", a.Checkpoint, a.CheckpointTitle)
}

// beadList renders "- id: title" lines.
func beadList(bs []bd.Bead) string {
	lines := make([]string, len(bs))
	for i, x := range bs {
		lines[i] = fmt.Sprintf("- %s: %s", x.ID, x.Title)
	}
	return strings.Join(lines, "\n")
}

// surgeryBlocks is the optional context blocks, each present only when non-empty.
func surgeryBlocks(a surgeryArgs) string {
	var blocks []string
	if a.SpecID != "" {
		blocks = append(blocks, fmt.Sprintf(`## The Spec, as background

This Checkpoint implements Spec %[1]s: %[2]s  (read it with: bd show %[1]s)
The human's dictation is the intent; Usopp reviews this Surgery's commits against
the Surgery bead, not the Spec. If a dictated change visibly contradicts the Spec,
say so once, then follow the human: never block on it and never raise a pivot.`, a.SpecID, a.SpecTitle))
	}
	if len(a.Earlier) > 0 {
		blocks = append(blocks, "## Earlier Surgeries on this Checkpoint\n\nClosed, abandoned ones included, in filing order. Their commits are not yours;\nread what each did with bd comments <id>.\n\n"+beadList(a.Earlier))
	}
	if len(a.Builds) > 0 {
		blocks = append(blocks, "## Open Builds on this Checkpoint\n\nThey integrate onto the same branch as you, so your integrate may need a rebase.\n\n"+beadList(a.Builds))
	}
	if len(a.Critiques) > 0 {
		blocks = append(blocks, "## Open Critiques on this Checkpoint\n\nHeld, context only: never claim or close one from this session.\n\n"+beadList(a.Critiques))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n") + "\n\n"
}

// surgeryContext reads the Surgery's Checkpoint and what bd knows of it at launch.
func surgeryContext(id string) (surgeryArgs, error) {
	sg, err := showBead(id)
	if err != nil {
		return surgeryArgs{}, err
	}
	cp, err := showBead(sg.Parent)
	if err != nil {
		return surgeryArgs{}, err
	}
	a := surgeryArgs{ID: id, Checkpoint: cp.ID, CheckpointTitle: cp.Title}
	if a.SpecID, _, err = specOf(cp.ID); err != nil {
		return a, err
	}
	if a.SpecID != "" {
		spec, err := showBead(a.SpecID)
		if err != nil {
			return a, err
		}
		a.SpecTitle = spec.Title
	}
	all, err := surgeriesOf(cp.ID)
	if err != nil {
		return a, err
	}
	for _, s := range all {
		if s.ID != id && !s.Open() {
			a.Earlier = append(a.Earlier, s)
		}
	}
	if a.Builds, err = openOfKind(cp.ID, contract.KindBuild); err != nil {
		return a, err
	}
	a.Critiques, err = openOfKind(cp.ID, contract.KindCritique)
	return a, err
}

func openOfKind(cp string, k contract.Kind) ([]bd.Bead, error) {
	kids, err := childrenOfKind(cp, k)
	return slices.DeleteFunc(kids, func(b bd.Bead) bool { return !b.Open() }), err
}

func implementPrompt(a promptArgs) string { return contract.Render(buildTmpl, a) }
func conflictPrompt(a promptArgs) string  { return contract.Render(conflictTmpl, a) }

// intentOf is the Checkpoint's intent for a prompt: its Spec and its Surgeries,
// read independently (Spec D1, D6); neither on a headless Checkpoint.
func intentOf(checkpointID string) (specID string, surgeries []bd.Bead, err error) {
	if specID, _, err = specOf(checkpointID); err != nil {
		return "", nil, err
	}
	surgeries, err = surgeriesOf(checkpointID)
	return specID, surgeries, err
}

// runImplement is the build/critique handler: fork the implement agent over the
// bead (it authors + commits its slice to the Checkpoint branch, roll-forward
// commit-only-if-dirty inside the agent), then drive the graph. An errored Run
// leaves the bead open for the next tick, mirroring runResearch.
func runImplement(ctx contract.Ctx, bead bd.Bead) error {
	specID, surgeries, err := intentOf(bead.Parent)
	if err != nil {
		return err
	}
	// A Critique with a Base: pin is the failed-gate-build one (Spec D5): the
	// prompt rebases onto that exact base before the fix. Builds and unpinned
	// Critiques carry no Base.
	var base string
	if bead.Kind == contract.KindCritique {
		base, _ = git.ParseBase(bead.Description)
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusByID(implementPrompt(promptArgs{BeadID: bead.ID, Checkpoint: bead.Parent, Base: base, SpecID: specID, Surgeries: surgeries}), bead.Parent, showBead))
	if err != nil {
		return err
	}
	if _, err := ctx.Harness().Run(ctx, inv); err != nil {
		return sweptOr(bead.ID, err)
	}
	return afterRun(ctx, bead.ID)
}

// runConflict is the conflict handler: read the "Base: <sha>" pin off the bead body
// (git.ParseBase — the base the tail rebases onto, ADR 0004 §4), fork the resolve
// agent over it, then drive the same graph tail. A conflict bead with no Base: pin
// is malformed — fail loud rather than fork an agent with no base to rebase onto.
func runConflict(ctx contract.Ctx, bead bd.Bead) error {
	base, ok := git.ParseBase(bead.Description)
	if !ok {
		return fmt.Errorf("franky: conflict bead %s carries no Base: pin", bead.ID)
	}
	specID, surgeries, err := intentOf(bead.Parent)
	if err != nil {
		return err
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusByID(conflictPrompt(promptArgs{BeadID: bead.ID, Base: base, SpecID: specID, Surgeries: surgeries}), bead.Parent, showBead))
	if err != nil {
		return err
	}
	if _, err := ctx.Harness().Run(ctx, inv); err != nil {
		return sweptOr(bead.ID, err)
	}
	return afterRun(ctx, bead.ID)
}

// sweptOr is a failed fork's err — unless the bead was closed out from under the
// effort by a concurrent confirm sweep (FR49), which is done, not a failure: no
// error means dispatch records no failure and releases no claim.
func sweptOr(id string, err error) error {
	if b, serr := showBead(id); serr == nil && !b.Open() {
		return nil
	}
	return err
}

// afterRun re-reads the bead once the agent returns (its pre-dispatch snapshot is
// stale). If the agent raised a pivot — created one, or merged into an existing
// same-FR pivot, both discovered-from this bead — it drives the raise tail; else
// finish. An already-closed bead (a prior dispatch, or a
// concurrent confirm sweep) is done, not an error (FR49).
func afterRun(ctx contract.Ctx, id string) error {
	b, err := showBead(id)
	if err != nil {
		return err
	}
	if !b.Open() {
		return nil
	}
	pivots, err := pivotsDiscoveredFrom(b.ID)
	if err != nil {
		return err
	}
	if len(pivots) > 0 {
		spec, _, err := specOf(b.Parent)
		if err != nil {
			return err
		}
		return raisePivot(ctx, b, spec, pivots)
	}
	return finish(b)
}

// finish is the shared tail of all three handlers (Spec D8, FR 32): it
// only closes the bead. Jinbe spawns the Review from drained-Checkpoint state, so
// no member races another over deciding it is last (ADR 0103). The Open() guard
// makes a re-dispatch after a committed-but-not-closed crash converge without a
// double close.
func finish(bead bd.Bead) error {
	if !bead.Open() {
		return nil // already closed by a prior dispatch: no double close
	}
	return closeBead(bead.ID, "")
}

// EndSurgery is surgery end (Spec D5, FR27–30), the Franky-owned transition
// his surgery prompt runs once the human says end (and a human may run by hand).
// It refuses anything but an open kind:surgery under a kind:checkpoint. It first
// integrates the Surgery's effort/<id> (a failure closes nothing), then counts: with
// this Surgery's own "Surgery: <id>" squashes on mainline..checkpoint/<cp>, it only
// closes the Surgery: Jinbe spawns the Review once the Checkpoint drains (Spec D8).
// With none, it closes the Surgery abandoned, and the Checkpoint too only when
// nothing at all is ahead of mainline and the Surgery is its only child, so a
// joined Surgery never closes the Checkpoint under its Builds or earlier
// Surgeries. The branch is left. Gates and the tree are the prompt's check, not this verb's.
func EndSurgery(id string) error {
	sg, err := showBead(id)
	if err != nil {
		return err
	}
	if sg.Kind != contract.KindSurgery {
		return fmt.Errorf("surgery end: %s is kind:%s, not a Surgery", id, sg.Kind)
	}
	if !sg.Open() {
		return fmt.Errorf("surgery end: Surgery %s is already closed", id)
	}
	if sg.Parent == "" {
		return fmt.Errorf("surgery end: Surgery %s has no Checkpoint parent", id)
	}
	cp, err := showBead(sg.Parent)
	if err != nil {
		return err
	}
	if cp.Kind != contract.KindCheckpoint {
		return fmt.Errorf("surgery end: Surgery %s's parent %s is kind:%s, not a Checkpoint", id, cp.ID, cp.Kind)
	}
	r := contexts.RepoOf(cp) // a nested Checkpoint's branch and Surgery commits live in the child
	if err := integrateSurgery(sg, r); err != nil {
		return err
	}
	branch := git.BranchName(string(contract.KindCheckpoint), cp.ID)
	mainline := git.Mainline
	if r != "" {
		mainline = r.Mainline
	}
	n, err := commitsByTrailer(r, mainline(), branch, git.TrailerFor(string(contract.KindSurgery), id))
	if err != nil {
		return err
	}
	if n > 0 {
		return closeBead(id, "")
	}
	const reason = "abandoned: surgery ended with no commits"
	if err := closeBead(id, reason); err != nil {
		return err
	}
	ahead, err := commitsAhead(r, mainline(), branch)
	if err != nil || ahead > 0 {
		return err
	}
	kids, err := checkpointChildCount(cp.ID)
	if err != nil || kids > 1 {
		return err
	}
	return closeBead(cp.ID, reason)
}
