// Package zoro holds Zoro, the slicing crewmate. He owns spec/pivot and stewards
// the checkpoint (GLOSSARY.md). This THIN slice wires reconcileSpec — the
// idempotent Spec→Checkpoint-skeleton handler: an open kind:spec reconciles to a
// derived landed base → one Checkpoint epic spawned in succession →
// checkpoint/<id> cut from that base → an AFK slicing agent forked over the Spec
// (it lays the Builds itself, ADR 0015) → the Spec closed. Launch opens the one
// HITL kind, pivot: the triage session over a frozen Checkpoint (ADR 0046).
package zoro

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// Zoro is the concrete slicing crewmate. It carries no state; bd access and the
// harness arrive on the ctx (spec §4), mirroring Nami.
type Zoro struct{}

// Zoro satisfies the crewmate contract.
var _ contract.Crewmate = Zoro{}

// Run is the AFK path: the loop dispatches an open Spec, Run re-reads its current
// graph neighborhood and reconciles it to its Checkpoint skeleton (mirrors
// Nami.Run — Run IS reconcile for the dispatched bead; fresh graph each dispatch,
// spec §5/§8.2).
func (Zoro) Run(ctx contract.Ctx, bead contract.Bead) error {
	b, err := showBead(bead.ID)
	if err != nil {
		return err
	}
	return Zoro{}.Reconcile(ctx, b)
}

// Launch composes the HITL command a human opens for a pivot (ADR 0046 §2),
// mirroring Luffy.Launch: Go resolves the Checkpoint (reverse blocks) and the
// Spec (related) so the agent never works out the reverse-edge lookup. Placement
// is the route default (RepoRoot). Any other kind is a loud error.
func (Zoro) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindPivot {
		return nil, fmt.Errorf("zoro.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	cp, spec, err := pivotTargets(bead.ID)
	if err != nil {
		return nil, fmt.Errorf("zoro.Launch: %w", err)
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusByID(pivotPrompt(bead.ID, cp, spec), cp, showBead))
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

// pivotPrompt is the triage session a launched pivot runs (ADR 0046 §3–4): claim,
// read the evidence, recommend per pivot against the amend line, decide together,
// re-read for newcomers, execute dismiss/amend, or write the corrected Spec and
// run the confirm sweep (Confirm), or gather the evidence and run the escalate
// sweep (Escalate). The pivot model (ADR 0005, 0043–0045) and the
// check-before-write re-run rule (ADR 0003) live in the checkpoint-rules and
// ADR-FORMAT Crew docs it reads, not as citations in the prompt.
//
//go:embed prompts/pivot.md
var pivotTmpl string

func pivotPrompt(pivotID, checkpointID, specID string) string {
	return fmt.Sprintf(pivotTmpl, pivotID, checkpointID, specID, contract.DraftInstruction(pivotID))
}

// slicingPrompt is the fork-of-to-tickets workflow the AFK slicing session runs
// over an authored Spec: read the Spec body, slice it into Builds, and lay them
// as members of the already-cut Checkpoint (the agent lays the Builds itself —
// no SpawnBuildsIfAbsent, ADR 0015). Mirrors Nami's speccingTmpl seam. The
// vertical-slice, wide-refactor and prefactor rules stay inline (their only
// consumer); refer-by-title and domain vocabulary are the preamble's.
//
//go:embed prompts/slicing.md
var slicingTmpl string

func slicingPrompt(specID string) string {
	return fmt.Sprintf(slicingTmpl, specID, contract.DraftInstruction(specID))
}

// Reconcile drives a bead to its conformant next state (spec §8.2) — the single
// idempotent primitive behind the dispatched Run. It takes the already-read bead
// (its graph neighborhood), so no caller reads twice. Zoro's THIN slice owns one
// handler; an unowned kind is a loud error, mirroring Nami.
func (Zoro) Reconcile(ctx contract.Ctx, bead bd.Bead) error {
	switch bead.Kind {
	case contract.KindSpec:
		return reconcileSpec(ctx, bead)
	default:
		return fmt.Errorf("zoro.Reconcile: kind %q has no reconcile handler", bead.Kind)
	}
}

// checkpointBranch is the git branch a Spec's Checkpoint is cut on (ADR 0010 §3:
// checkpoint/<id>), built via git/'s naming helper so the convention lives in one
// place. Pure, so the name reconcileSpec computes is checkable without git.
func checkpointBranch(checkpointID string) string {
	return git.BranchName(string(contract.KindCheckpoint), checkpointID)
}

// checkpointSuccessor is the one Checkpoint a Spec spawns in succession (§8.4,
// ADR 0012/0014): a container kind:checkpoint epic that parents the Builds the
// slicing session lays. Pure, so the emitted shape (container, kind, title) is
// checkable without a DB.
func checkpointSuccessor(spec bd.Bead) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindCheckpoint,
		Container:   true,
		Title:       spec.Title,
		Description: "Checkpoint skeleton — Builds laid by the slicing session over the authored Spec (ADR 0015).",
		Context:     bd.ContextOf(spec),
		Repo:        bd.RepoStampOf(spec),
	}
}

// Seams over the bd/git legs reconcileSpec drives, so the Spec→Checkpoint-skeleton
// flow (derive base → spawn → cut → slicing session → close) is testable with a
// fake harness and no live bd/git. These alias the real package funcs; the zoro
// test swaps them — the same package-level var seam nami.go exposes.
var (
	causedBy       = bd.CausedBy
	landedBase     = git.LandedBase
	landStrategy   = git.LandStrategyName
	updateBody     = bd.Update
	fetchForge     = func() (string, error) { return git.FetchMainline(git.ForgeRemote()) }
	cutBranch      = git.CutBranch
	spawnSuccessor = bd.SpawnSuccessorIfAbsent
	showBead       = bd.Show
	closeBead      = bd.Close
	closeOnPublish = bd.CloseOnPublish
	branchOutcome  = deleteCheckpointBranch
	buildChildren  = bd.BuildChildren
	taskChildren   = bd.TaskChildren
	carry          = bd.Carry
	initiativeOf   = bd.InitiativeOf

	pivotCheckpoint = bd.PivotCheckpoint
	pivotSpec       = bd.PivotSpec

	// The child-repo legs of a nested context's bead (Spec D2, D3); repoOf reads
	// no config for an unlabelled bead.
	repoOf     = contexts.RepoOf
	stampCut   = contexts.StampCut
	repoLanded = func(r git.Repo, trailer string) (string, error) { return r.LandedBase(trailer) }
	repoCut    = func(r git.Repo, name, base string) (bool, error) { return r.CutBranch(name, base) }
	repoTip    = func(r git.Repo) (string, error) { return r.ResolveProbeBase() }
	repoFetch  = func(r git.Repo) (string, error) { return r.FetchMainline() }
)

// baseTrailer is the land trailer whose squash commit checkpoint/<id> is cut
// from (ADR 0010 §2), built via git/'s trailer rule so LandedBase greps back the
// exact same string the lander stamped. Pure, so the trailer
// checkpointBase computes is checkable without git — mirrors Nami's chartTrailer.
func baseTrailer(kind contract.Kind, id string) string {
	return git.TrailerFor(string(kind), id)
}

// checkpointBase derives the commit checkpoint/<id> is cut from (§8.4, ADR
// 0012/0014): walk the Spec —caused-by→ its Chart; a Chart-backed Spec cuts from
// the Chart's landed base ("Chart: <chartID>"), a direct Spec from its own
// anchor. A direct Spec reads its Anchor: pin first, under local and gitlab alike;
// with no pin it falls back to the legacy LandedBase "Spec: <specID>" trailer
// under both strategies (pre-change Specs, Specs anchored before a strategy
// flip); with neither it is a hard error, NEVER a rev-parse main fallback.
func checkpointBase(spec bd.Bead) (string, error) {
	chartID, found, err := causedBy(spec.ID, contract.KindChart)
	if err != nil {
		return "", err
	}
	kind, id := contract.KindSpec, spec.ID
	if found {
		kind, id = contract.KindChart, chartID
	}
	r := repoOf(spec)
	if found && r == "" { // a Workspace Chart child skips the land-strategy read (ADR 0157)
		return landedBase(baseTrailer(kind, id))
	}
	name, err := landStrategy()
	if err != nil {
		return "", err
	}
	gitlab := name == git.LandStrategyGitlab
	pin, pinned := "", false
	if !found {
		pin, pinned = git.ParseAnchor(spec.Description)
	}
	if r != "" && gitlab { // the pinned sha / Chart base is read from the child's fetched mainline
		if _, err := repoFetch(r); err != nil {
			return "", err
		}
	}
	if pinned {
		return pin, nil
	}
	var sha string
	if r != "" {
		sha, err = repoLanded(r, baseTrailer(kind, id)) // the child's trailer, never the Workspace's main
	} else {
		sha, err = landedBase(baseTrailer(kind, id))
	}
	if !found && errors.Is(err, git.ErrNotLanded) {
		return "", fmt.Errorf("zoro: Spec %s has neither an Anchor: pin nor a Spec: trailer; refusing to cut from main — restore the Anchor: line by hand from the triage.fastlane event's anchor attr", spec.ID)
	}
	return sha, err
}

// reconcileSpec is the Spec→Checkpoint-skeleton handler (§8.4). An open Spec
// reconciles in fixed order, each leg individually idempotent so a re-run (or a
// crash mid-way) converges: (1) derive the landed base — a missing anchor
// hard-fails and cuts nothing; (2) SpawnSuccessorIfAbsent the one Checkpoint
// epic (no second on re-run); (3) branch-if-absent cut checkpoint/<id> from that
// base (never main); (4) while the Spec is open AND no Build exists yet, fork
// the AFK slicing session over it (it lays the Builds, ADR 0015) — a re-dispatch
// after Builds are laid skips the fork (Nami-parity with reconcileChart's
// placeholder-body gate); (5) close the Spec behind the open() guard, ONLY
// after the fork returned AND laid >=1 kind:build member on the Checkpoint —
// the artifact existing, not the session merely having run (FR22). The Builds
// are Drafts by the Spec and the close is handed to the publish, so the whole
// slice, its blocks edges and the Spec's close become visible in one batch
// (ADR 0059, Spec D6, D11 row 3) — Franky never sees a partial slice.
// A fork failure propagates, and a fork that laid zero Builds returns without
// closing, so either way the Spec stays open and the loop re-dispatches — the
// same gate reconcileChart applies to the Spec-body-authored Chart close.
func reconcileSpec(ctx contract.Ctx, spec bd.Bead) error {
	base, err := checkpointBase(spec)
	if err != nil {
		return err
	}
	succ := checkpointSuccessor(spec)
	if succ.Parent, err = initiativeOf(spec); err != nil {
		return err
	}
	checkpointID, _, err := spawnSuccessor(spec.ID, succ)
	if err != nil {
		return err
	}
	// Re-entry: a Task-only Spec's Checkpoint already closed nothing-to-land on a
	// prior pass skips the cut and the fork and goes straight to the Spec's close.
	cp, err := showBead(checkpointID)
	if err != nil {
		return err
	}
	if !cp.Open() && strings.HasPrefix(cp.CloseReason, bd.NothingToLandReason) {
		if !spec.Open() {
			return nil
		}
		// A failed publish (effort.fail / Chopper recover) may have deleted the
		// Draft Tasks: re-count, and re-slice when none remain (FR19). The fork's
		// after: land edges target the closed Checkpoint and release at once.
		tasks, err := taskChildren(spec.ID)
		if err != nil {
			return err
		}
		if tasks == 0 {
			inv, err := contract.NewInvocation(spec.ID, contexts.Focus(slicingPrompt(spec.ID), spec))
			if err != nil {
				return err
			}
			if _, err := ctx.Harness().Run(ctx, inv); err != nil {
				return err
			}
			if tasks, err = taskChildren(spec.ID); err != nil {
				return err
			}
			builds, err := buildChildren(checkpointID)
			if err != nil {
				return err
			}
			if builds > 0 {
				return fmt.Errorf("reconcileSpec: checkpoint %s is closed before spec %s — the slicing fork must lay Builds only, not close the Checkpoint; leaving Spec open", checkpointID, spec.ID)
			}
			if tasks == 0 {
				return fmt.Errorf("reconcileSpec: slicing laid neither a Build nor a Task for spec %s; leaving it open", spec.ID)
			}
		}
		return closeOnPublish(ctx, spec.ID, "", nil)
	}
	if r := repoOf(spec); r != "" {
		if err = stampCut(spec, checkpointID, r, checkpointBranch(checkpointID)); err == nil {
			_, err = repoCut(r, checkpointBranch(checkpointID), base)
		}
	} else {
		_, err = cutBranch(checkpointBranch(checkpointID), base)
	}
	if err != nil {
		return err
	}
	if spec.Open() {
		// Gate the fork on no artifact existing yet: no kind:build member on the
		// Checkpoint and no kind:task dependent of the Spec (Nami-parity with
		// reconcileChart's placeholder-body gate). A re-fork is safe for Builds
		// (FR21) but republishes Tasks, whose drafted-by label publishing strips, so
		// a Spec holding Tasks is never re-forked.
		builds, err := buildChildren(checkpointID)
		if err != nil {
			return err
		}
		tasks, err := taskChildren(spec.ID)
		if err != nil {
			return err
		}
		if builds+tasks == 0 {
			inv, err := contract.NewInvocation(spec.ID, contexts.Focus(slicingPrompt(spec.ID), spec))
			if err != nil {
				return err
			}
			if _, err := ctx.Harness().Run(ctx, inv); err != nil {
				return err
			}
			// Re-read: gate the close on an artifact existing, not on the fork
			// returning nil (FR22, ADR 0015).
			if builds, err = buildChildren(checkpointID); err != nil {
				return err
			}
			if tasks, err = taskChildren(spec.ID); err != nil {
				return err
			}
			if builds+tasks == 0 {
				// Fail loud, leave the Spec open: the loop's failing tally pauses it.
				return fmt.Errorf("reconcileSpec: slicing laid neither a Build nor a Task for spec %s; leaving it open", spec.ID)
			}
		}
		if builds > 0 {
			// Gate the spec-close on the Checkpoint still being open. The slicing fork
			// lays Build members ONLY (slicing.md) and must never close the Checkpoint;
			// if it did (or a human/land closed it early), closing the Spec here would
			// bury a closed-Spec / closed-Checkpoint / open-Build corruption silently.
			// Fail loud and leave the Spec open so a human sees it, never a quiet close.
			cp, err := showBead(checkpointID)
			if err != nil {
				return err
			}
			if !cp.Open() {
				return fmt.Errorf("reconcileSpec: checkpoint %s is closed before spec %s — the slicing fork must lay Builds only, not close the Checkpoint; leaving Spec open", checkpointID, spec.ID)
			}
			// Carry the Spec's Ordering edges to the Checkpoint just before the close,
			// outside the publish batch (ADR 0085): its dependents then wait for the
			// Checkpoint to land. A carry error leaves the Spec open for a retry.
			moved, skipped, err := carry(spec.ID, checkpointID)
			if err := eventlog.RecordCarry(spec, checkpointID, moved, skipped, err); err != nil {
				return err
			}
			return closeOnPublish(ctx, spec.ID, "", nil)
		}
		if tasks > 0 {
			// Task-only: nothing lands, so the Checkpoint closes empty and its branch
			// goes (ADR 0015 close gate, amended). Carry first, as the Builds arm does:
			// a carry error closes nothing and leaves both beads open for the retry.
			cp, err := showBead(checkpointID)
			if err != nil {
				return err
			}
			if !cp.Open() {
				return fmt.Errorf("reconcileSpec: checkpoint %s is closed before spec %s — a Task-only Spec closes it itself; leaving Spec open", checkpointID, spec.ID)
			}
			moved, skipped, err := carry(spec.ID, checkpointID)
			if err := eventlog.RecordCarry(spec, checkpointID, moved, skipped, err); err != nil {
				return err
			}
			if err := closeBead(checkpointID, bd.NothingToLandReason+" Task-only Spec "+spec.ID); err != nil {
				return err
			}
			// A branch outcome never fails the pass, and no land lock is taken.
			if out, ok := branchOutcome(repoOf(spec), checkpointBranch(checkpointID)); ok {
				eventlog.RecordBranches([]bd.BranchOutcome{out}, "checkpoint", checkpointID, "spec", spec.ID)
			}
			return closeOnPublish(ctx, spec.ID, "", nil)
		}
	}
	return nil
}

// deleteCheckpointBranch deletes branch in r (a nested context's child repo, or
// the Workspace for ""), shaped on bd's deleteDraftBranches: a missing branch is
// skipped (ok false), one with work off mainline is kept, and git's refusal for a
// branch a worktree holds is kept too.
func deleteCheckpointBranch(r git.Repo, branch string) (bd.BranchOutcome, bool) {
	if _, err := r.Head("refs/heads/" + branch); err != nil {
		return bd.BranchOutcome{}, false
	}
	err := errors.New("has work")
	if r.IsAncestor("refs/heads/"+branch, r.Mainline()) {
		err = r.DeleteBranch(branch)
	}
	return bd.BranchOutcome{Branch: branch, Err: err}, true
}
