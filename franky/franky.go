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
	"text/template"

	"jayulabs.org/mugicrew/bd"
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
// the route arm. Build, critique
// and conflict are AFK (loop-only), so Launch errors loudly for them rather than
// compose a bad command, mirroring Sanji's Run guard.
func (Franky) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindSurgery {
		return nil, fmt.Errorf("franky.Launch: kind %q is AFK (build/critique/conflict), not launchable — dispatched via Run", bead.Kind)
	}
	inv, err := contract.NewInvocation(bead.ID, surgeryPrompt(bead.ID))
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
	surgeryOf            = bd.SurgeryOf

	commitsAhead     = git.CommitsAhead
	integrateSurgery = integrateEffort
)

// integrateEffort is surgery end's first leg (Spec D8, FR 33): mugicrew
// integrate in the Surgery's tree on effort/<id>. Run elsewhere, it refuses while
// effort/<id> exists, since counting then would miss the tree's work; with no
// effort branch there is nothing to integrate.
func integrateEffort(sg bd.Bead) error {
	if id, err := git.CurrentEffort(); err != nil || id != sg.ID {
		refs, err := git.EffortRefs()
		if err == nil && refs[sg.ID] {
			err = fmt.Errorf("surgery end: run it in the Surgery's tree on %s, so its work is integrated first", git.EffortBranch(sg.ID))
		}
		return err
	}
	by := eventlog.ByHuman
	if session.InPane() {
		by = eventlog.ByAgent
	}
	if rc := session.Integrate(sg, by, os.Stdout); rc != session.ExitIntegrated {
		return fmt.Errorf("surgery end: mugicrew integrate exited %d; nothing was closed", rc)
	}
	return nil
}

// implementPrompt (build.md) carries implement's four steps inline over one Build
// or Critique: test-first from the tdd Crew doc, check as you go, the full suite at
// the end, and a self-pass from the code-review Crew doc. conflictPrompt
// (conflict.md) carries the merge-conflict resolution steps inline over a conflict
// bead, with the base SHA to rebase onto, and an ADR-number clash goes to the
// adr-renumber Crew doc (ADR 0030). Both read the checkpoint-rules Crew doc for the
// effective Spec, Critiques and pivots (ADR 0005, ADR 0043–0045, ADR 0087,
// ADR 0088), keeping only the commands with their own ids filled in. Both carry the Checkpoint's Spec resolved through bd.SpecOf, so a
// Rollback successor's agent reads the real Spec, not its Rollback (Spec
// Decision 6), mirroring Usopp's reviewPrompt. A Spec-less Checkpoint carries
// its Surgery as context instead (intent.md), and no pivot section (Spec
// D6). Both replace only the terminal
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
// At most one of SpecID and SurgeryID is set.
type promptArgs struct {
	BeadID     string // the Build, Critique or Conflict
	Checkpoint string // a Build's or Critique's Checkpoint, its effort's container
	Base       string // the SHA a Conflict rebases onto
	SpecID     string // the Checkpoint's Spec
	SurgeryID  string // a Spec-less Checkpoint's Surgery
}

// Draft is the prompt's shared Draft block.
func (a promptArgs) Draft() string { return contract.DraftInstruction(a.BeadID) }

// surgeryPrompt (surgery.md) is one dictation session over a Surgery: start
// (read the Workspace docs, claim, read the record and git log, rebuild a missing
// record), dictation, the per-session record, stop (the idle line) and end
// (mugicrew surgery end). No pivot section: a surgery Checkpoint has no Spec.
// The ClosingLine splice is the Launch invoker's, unchanged. Pure.
//
//go:embed prompts/surgery.md
var surgeryTmpl string

func surgeryPrompt(surgeryID string) string {
	return fmt.Sprintf(surgeryTmpl, surgeryID, contract.DraftInstruction(surgeryID))
}

func implementPrompt(a promptArgs) string { return contract.Render(buildTmpl, a) }
func conflictPrompt(a promptArgs) string  { return contract.Render(conflictTmpl, a) }

// intentOf is the Checkpoint's intent source for a prompt: its Spec, else its
// Surgery (Spec D6); neither on a headless Checkpoint.
func intentOf(checkpointID string) (specID, surgeryID string, err error) {
	specID, found, err := specOf(checkpointID)
	if err != nil || found {
		return specID, "", err
	}
	surgeryID, _, err = surgeryOf(checkpointID)
	return "", surgeryID, err
}

// runImplement is the build/critique handler: fork the implement agent over the
// bead (it authors + commits its slice to the Checkpoint branch, roll-forward
// commit-only-if-dirty inside the agent), then drive the graph. An errored Run
// leaves the bead open for the next tick, mirroring runResearch.
func runImplement(ctx contract.Ctx, bead bd.Bead) error {
	specID, surgeryID, err := intentOf(bead.Parent)
	if err != nil {
		return err
	}
	inv, err := contract.NewInvocation(bead.ID, implementPrompt(promptArgs{BeadID: bead.ID, Checkpoint: bead.Parent, SpecID: specID, SurgeryID: surgeryID}))
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
	specID, surgeryID, err := intentOf(bead.Parent)
	if err != nil {
		return err
	}
	inv, err := contract.NewInvocation(bead.ID, conflictPrompt(promptArgs{BeadID: bead.ID, Base: base, SpecID: specID, SurgeryID: surgeryID}))
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
// commits on checkpoint/<cp> since its merge-base with mainline, it only closes the
// Surgery: Jinbe spawns the Review once the Checkpoint drains (Spec D8).
// With none, it closes the Surgery abandoned, then the empty Checkpoint, and
// leaves the branch. Gates and the tree are the prompt's check, not this verb's.
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
	if err := integrateSurgery(sg); err != nil {
		return err
	}
	n, err := commitsAhead(git.Mainline(), git.BranchName(string(contract.KindCheckpoint), cp.ID))
	if err != nil {
		return err
	}
	if n == 0 {
		const reason = "abandoned: surgery ended with no commits"
		if err := closeBead(id, reason); err != nil {
			return err
		}
		return closeBead(cp.ID, reason)
	}
	return closeBead(id, "")
}
