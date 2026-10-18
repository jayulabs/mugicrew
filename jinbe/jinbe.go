// Package jinbe holds Jinbe, the pure-Go Checkpoint land. He is the Checkpoint
// handler (the container kind:checkpoint): his Run spawns the bare Review once the
// Checkpoint drains with no Approval at head (Spec D8), and spawns the Landing
// of an approved one; the Landing (his kind:landing leaf, ADR 0145/0147) lands it by
// rebase+squash onto mainline through the land_strategy seam — no agent session runs
// to land — the reconcileChart twin, but AFK-Franky-flavored for the Checkpoint tail.
// He lands ONLY behind the approvedAtHead gate (a closed Approval whose
// Commit: pin still equals checkpoint/<id>'s tip, FR40) so a stuck or unreviewed
// Checkpoint fails safe — visible, never landed. A clean land closes the Landing, then the Checkpoint
// (the merged state, FR41); a rebase conflict becomes a Franky-owned kind:conflict
// member carrying the Base: pin (FR42). Idempotency keys on the land trailer: a
// post-land crash rolls forward and never strands. Land is AFK, so Launch errors
// loudly, mirroring Franky/Usopp.
//
// He also owns his first leaf kind, the human-filed kind:rollback (ADR 0047): a
// pure-Go forward revert of a landed Checkpoint's squash on main, then the handoff
// of the reverted work to a successor Checkpoint he seeds and stewards (ADR 0048).
package jinbe

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/nami"
)

// Jinbe is the concrete Checkpoint-land crewmate. It carries no state; bd access
// and the harness arrive on the ctx (spec §4), mirroring Nami/Zoro/Franky/Usopp.
type Jinbe struct{}

// Jinbe satisfies the crewmate contract.
var _ contract.Crewmate = Jinbe{}

// Run is the AFK path the loop dispatches. It re-reads the bead's current graph
// neighborhood (the loop hands only ID+Kind) and lands the Checkpoint or reverts
// the Rollback's target — no harness call, both are pure Go. An unowned kind is a
// loud error, mirroring Franky.Run.
func (Jinbe) Run(_ contract.Ctx, bead contract.Bead) error {
	b, err := showBead(bead.ID)
	if err != nil {
		return err
	}
	switch b.Kind {
	case contract.KindCheckpoint:
		return reconcileCheckpoint(b)
	case contract.KindRollback:
		return reconcileRollback(b)
	case contract.KindLanding:
		return reconcileLanding(b)
	}
	return fmt.Errorf("jinbe.Run: kind %q has no handler", b.Kind)
}

// Launch composes the HITL command a human opens — but the Checkpoint land and the
// Rollback are AFK (loop-only), so this must never be reached: it errors loudly rather than compose
// a bad command, mirroring Franky's/Usopp's Launch guard.
func (Jinbe) Launch(_ contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	return nil, fmt.Errorf("jinbe.Launch: kind %q is AFK (checkpoint land / rollback), not launchable — dispatched via Run", bead.Kind)
}

// Seams over the bd/git legs reconcileCheckpoint and reconcileRollback drive, so the approvedAtHead gate,
// the land-trailer idempotency roll-forward, and the conflict-raise path are testable
// with no live bd/git (mirrors franky/usopp's var seam). These alias the real package
// funcs; the jinbe test swaps them.
var (
	showBead             = bd.Show
	jiraPrefix           = bd.JiraPrefixOf
	closeBead            = bd.Close
	updateBody           = bd.Update
	spawnChild           = bd.SpawnChildIfAbsent
	spawnSignal          = bd.SpawnOffshootSignalIfAbsent
	closedChildrenOfKind = bd.ClosedChildrenOfKindBeads
	landBranch           = git.Land
	landStrategy         = resolveLandStrategy
	composeLanding       = git.Compose
	openCritiques        = func(id string) ([]bd.Bead, error) { return bd.OpenChildrenOfKindBeads(id, contract.KindCritique) }
	gateAttr             = func() string {
		root, err := git.RepoRoot()
		if err != nil {
			return ""
		}
		return cfglayer.Gate(root)
	}
	gateBuild = func(landingID, rev, attr string) (string, error) {
		common, err := git.CommonDir()
		if err != nil {
			return "", &looplock.InfraError{Err: err}
		}
		return looplock.GateBuild(context.Background(), common, landingID, rev, attr)
	}
	gateBuildAt     = looplock.GateBuild
	contextGate     = cfglayer.ContextGate
	repoDirOf       = contexts.RepoDirOf
	liveRepos       = contexts.Repos
	strategyFor     = git.LandStrategyFor
	pinnedGitlab    = git.Repo.PinnedGitlab
	sameBaseLand    = git.LandSameBase
	landingsOf      = bd.LandingsOf
	landedBase      = git.LandedBase
	landedTip       = git.Repo.LandedTip
	projectOf       = git.Repo.ForgeProject
	forgeMR         = forge.GetMR
	forgeAssertDesc = func(r git.Repo, mr *forge.MR, pin, trailer string) error {
		return r.AssertMRDescription(mr, pin, trailer)
	}
	forgeRetry     = forge.RetryPipeline
	forgeJobs      = forge.FailedJobs
	forgeTrace     = func(p forge.Project, job int) (string, error) { return forge.TraceTail(p, job, traceLines) }
	forgeTarget    = forge.TargetOf
	forgeSource    = forge.SourceOf
	fetchForge     = git.Repo.FetchMainline
	approvedAtHead = git.ApprovedAtHead
	branchHead     = git.Head
	isAncestor     = git.IsAncestor
	rollbackTarget = bd.RollbackTarget
	revertCommit   = git.Revert
	revertOnto     = git.RevertOnto
	pushRevertMR   = func(r git.Repo, id, trailer, pin string) (*git.LandPending, error) {
		return r.PushRevertMR(id, trailer, pin)
	}
	firstParent     = git.FirstParentOnMain
	forgeCloseMR    = forge.CloseMR
	forgeFindOpenMR = forge.FindOpenMR
	specOf          = bd.SpecOf
	spawnSuccessor  = bd.SpawnSuccessorIfAbsent
	successorOf     = bd.SuccessorOf
	cutBranch       = git.CutBranch
	reapply         = git.Reapply
	buildChildren   = bd.BuildChildren
	probeBase       = git.Repo.ResolveProbeBase
	probeConflict   = git.Repo.ProbeConflict
	repoHead        = func(r git.Repo) func(string) (string, error) { return r.Head }
	reviewDue       = func(id string, atHead func(string, string) bool) (bool, error) { return bd.ReviewDue(id, nil, atHead) }
	membersDrained  = func(id string) (bool, error) { return bd.MembersDrained(id, nil) }
	openBlockers    = bd.OpenBlockers
	initiativeOf    = bd.InitiativeOf
	postLandClashes = git.PostLandClashes
	subtreeIDs      = bd.SubtreeIDs
	causedBy        = bd.CausedBy
)

// Seams of threadArm (review threads, threads.go).
var (
	forgeDiscussions    = forge.Discussions
	forgeIsBot          = forge.IsBot
	forgeReply          = forge.Reply
	forgeResolve        = forge.Resolve
	graphPush           = bd.GraphPush
	spawnUnblockingTask = bd.SpawnUnblockingTaskIfAbsent
	forgeUser           = func() (string, error) {
		p, err := git.ForgeProject() // the reply-marker account is the Workspace's
		if err != nil {
			return "", err
		}
		return forge.CurrentUser(p)
	}
	newestByTrailer = git.Repo.NewestByTrailer
	childrenOfKind  = bd.ChildrenOfKind
	supersede       = bd.Supersede
	spawnThreadBead = bd.SpawnChild
)

// checkpointBranch is the branch a Checkpoint lands from (ADR 0010 §3: checkpoint/<id>)
// and checkpointTrailer the land trailer its squash commit carries (ADR 0010 §2:
// "Checkpoint: <id>"), both via git/'s naming rules so the same kind const names both
// and LandedBase greps back the exact string Land stamped. Pure, checkable without git.
func checkpointBranch(id string) string  { return git.BranchName(string(contract.KindCheckpoint), id) }
func checkpointTrailer(id string) string { return git.TrailerFor(string(contract.KindCheckpoint), id) }

// reconcileCheckpoint is the Checkpoint's container step, the reconcileChart twin,
// with two outcomes: spawn the Landing that lands an approved Checkpoint
// (FR40–FR42, ADR 0145), otherwise spawn the bare Review once drained (Spec D8,
// FR 29). It never lands itself. In fixed order, each leg idempotent so a re-run
// (or a crash mid-way) converges:
//
//  1. Roll-forward idempotency (FR42): if the land trailer is already on mainline the
//     Checkpoint has landed — close any Landing still open (landed: <sha>, a crash
//     between its land and close), then the Checkpoint if still open, and return.
//     The trailer is the durable landed fact, so a crash never strands.
//  2. The approvedAtHead gate (FR40): spawn the Landing ONLY when a closed Approval's
//     Commit: pin equals the live tip of checkpoint/<id> — NOT "0 open children". No
//     approval at head ⇒ never land unreviewed: spawn the Review if one is due
//     (spawnReviewIfDue), else leave the Checkpoint open and visible. Approved but
//     members not drained (one still open or holding effort/<id>, e.g. a Conflict
//     whose integrate didn't publish) ⇒ wait silently (Spec FR 28, FR 47). So does an
//     open incoming blocker, e.g. a before-land Task (the land hold, Spec FR 15).
//  3. Spawn the Landing (SpawnChildIfAbsent: one open at a time), in succession to
//     the at-head Approval and carrying its pin, and leave the Checkpoint open. The
//     Landing lands it (reconcileLanding) and the next pass closes it (step 1).
func reconcileCheckpoint(cp bd.Bead) error {
	if sha, landed, err := contexts.Landed(cp, checkpointTrailer(cp.ID), landedBase); err != nil {
		if foreign := (*git.ForeignLanding)(nil); errors.As(err, &foreign) {
			return bd.HoldForeign(cp, foreign) // the hold exists even with no Landing (ADR 0187 §3)
		}
		return err
	} else if landed {
		return closeLanded(cp, checkpointBranch(cp.ID), sha)
	}
	approval, err := checkpointApproval(cp)
	if err != nil {
		return err
	}
	if approval == nil {
		return spawnReviewIfDue(cp)
	}
	if drained, err := membersDrained(cp.ID); err != nil || !drained {
		return err
	}
	if held, err := openBlockers(cp.ID); err != nil || len(held) > 0 {
		return err
	}
	pin, _ := git.ParseCommit(approval.Description)
	_, _, err = spawnChild(cp.ID, bd.Successor{
		Kind:        contract.KindLanding,
		Title:       cp.Title,
		Description: git.CommitPin(pin),
		CausedBy:    approval.ID,
	})
	return err
}

// closeLanded rolls a landed container forward: under gitlab it first closes any MR still open
// on the branch; its open Landings close landed: <sha> through the Open() guard, then the
// container itself — always the Landing first (ADR 0145 §3), so a container never closes
// over an open Landing.
func closeLanded(container bd.Bead, branch, sha string) error {
	landings, err := landingsOf(container.ID)
	if err != nil {
		return err
	}
	if name, _, err := landStrategy(); err != nil {
		return err
	} else if name == git.LandStrategyGitlab { // a merged trailer can beat the Landing's own tick (FR 20)
		lg, ok, err := legsFor(container, true)
		if err != nil || !ok { // drift: the tick scan holds it (as in reconcileLanding), so the Landing stays unclosed
			return err
		}
		l, hasLanding := bd.NewestLanding(landings)
		evBead := container
		if hasLanding {
			evBead = l
			if err := flagStalePin(l, container, branch, sha, lg); err != nil {
				return err
			}
		}
		if err := closeLandedMR(evBead, branch, lg); err != nil { // before any bead closes, so a failure retries
			// a held 429 returns nil before anything closes; the next tick retries (ADR 0185 §8)
			return holdRateLimited(evBead, "land.rate_limited", err, lg.attrs)
		}
		if hasLanding && l.Open() { // CloseLandings below closes it
			landOutcome(lg, l, eventlog.LandLanded, git.RecordedMR(l.Description), "sha", sha)
		}
		if lg.repo == "" { // a nested container skips the post-land renumber (out of scope: it has no child-scoped clash read)
			if err := mintRenumber(container, sha); err != nil {
				return err
			}
		}
	}
	if err := bd.CloseLandings(landings, sha, closeBead); err != nil {
		return err
	}
	return closeIfOpen(container, "")
}

// mintRenumber files the renumber Checkpoint when container, landed at sha on the
// fetched Forge mainline, is the later side of an ADR-number clash (ADR 0225); the
// earlier side files nothing. Run before the container closes, so a closed landed
// container implies its check ran.
func mintRenumber(container bd.Bead, sha string) error {
	clashes, err := postLandClashes(sha)
	if err != nil {
		return err
	}
	_, err = nami.MintRenumber(container, sha, clashes, nami.RenumberSeams{
		SpawnSuccessor: spawnSuccessor, CutBranch: cutBranch, BuildChildren: buildChildren,
		SpawnBuild: spawnChild, InitiativeOf: initiativeOf, SubtreeIDs: subtreeIDs, CausedBy: causedBy,
	})
	return err
}

// reconcileLanding is a Landing's one land attempt at one pinned commit (ADR 0145),
// for its Checkpoint or Chart parent. Idempotent: every leg re-derives from git, so a re-run
// converges. In fixed order:
//
//  0. Under gitlab, fetch the Forge's main and fast-forward local main to it first
//     (a nested repo fetches its own Forge remote; a diverged main is *git.MainDiverged,
//     the Landing left open), so a trailer a human landed on the Forge is seen below.
//  1. Trailer already on mainline ⇒ close landed: <sha>, no land.
//  2. Branch head ≠ the Commit: pin ⇒ close obsolete: branch advanced without
//     landing; the Checkpoint falls back to its Approval gate (a new Review).
//  3. Land through the configured land_strategy (a Checkpoint under local is first
//     built and landed by gatedLand, ADR 0150). A rebase conflict or ADR clash files
//     the parent's conflict member (a Checkpoint's, or a Chart's chart-conflict) and closes the Landing obsolete: conflict;
//     ErrLandPending leaves it open, nil; *MainDirty or any other error files nothing,
//     leaves it open and is returned for the next tick.
//  4. Landed ⇒ close landed: <sha>. The Checkpoint closes on its own next pass.
//
// A Forge 429 anywhere in the attempt (the MR read, the write leg, the pipeline retry,
// jobs or trace reads) leaves the Landing open and is logged once per Landing, not
// returned each tick (Spec FR 35).
func reconcileLanding(l bd.Bead) error {
	nested, err := reconcileLandingOnce(l)
	return holdRateLimited(l, "land.rate_limited", err, nested)
}

// holdRateLimited returns err unless it is a Forge 429, which it logs once per bead
// under event and swallows, so the bead stays open and the next tick retries (ADR 0185 §8).
// nested is the context/repo/into attrs the attempt resolved (Spec FR 30), nil for a
// Workspace one (FR 33): appended with no further bead read.
func holdRateLimited(b bd.Bead, event string, err error, nested []any) error {
	if !errors.Is(err, forge.ErrRateLimited) {
		rateLimited.Delete(b.ID)
		return err
	}
	if _, seen := rateLimited.LoadOrStore(b.ID, true); !seen {
		pruneRateLimited()
		attrs := append(eventlog.BeadAttrs(b), "mr", git.RecordedMR(b.Description))
		slog.Warn(event, append(attrs, nested...)...)
	}
	return nil
}

// reconcileLandingOnce returns the legs' nested attrs once resolved (nil for a Workspace land).
func reconcileLandingOnce(l bd.Bead) (nested []any, _ error) {
	if !l.Open() {
		return nil, nil
	}
	cp, err := showBead(l.Parent)
	if err != nil {
		return nested, err
	}
	if cp.Kind != contract.KindCheckpoint && cp.Kind != contract.KindChart {
		return nested, fmt.Errorf("jinbe.reconcileLanding: parent %s is a %s; only a Checkpoint's or Chart's Landing is handled", cp.ID, cp.Kind)
	}
	branch, trailer := git.BranchName(string(cp.Kind), cp.ID), git.TrailerFor(string(cp.Kind), cp.ID)
	name, strategy, err := landStrategy()
	if err != nil {
		return nested, err
	}
	lg, ok, err := legsFor(cp, name == git.LandStrategyGitlab)
	if err != nil || !ok { // drift: the tick scan holds it, and a land is the one irreversible step
		return nested, err
	}
	nested = lg.attrs
	var base string // the fetched Forge mainline, gitlab only
	if name == git.LandStrategyGitlab {
		if base, err = fetchForge(lg.repo); err != nil {
			return nested, err
		}
	}
	if sha, err := lg.landedBase(trailer); err == nil {
		return nested, landedClose(l, cp, branch, sha, name, lg) // before step 2: a stale pin merged is landed, not obsolete
	} else if foreign := (*git.ForeignLanding)(nil); errors.As(err, &foreign) {
		return nested, bd.HoldForeign(cp, foreign) // the Landing stays open (ADR 0187 §3)
	}
	pin, _ := git.ParseCommit(l.Description)
	head, err := lg.head("refs/heads/" + branch)
	if err != nil {
		return nested, err
	}
	if pin != head {
		return nested, closeIfOpen(l, "obsolete: branch advanced without landing")
	}
	if lg.repo != "" && name == git.LandStrategyLocal { // gitlab overwrites strategy with pinnedGitlab below
		if strategy, err = strategyFor(name, lg.tree); err != nil {
			return nested, err
		}
	}
	if name == git.LandStrategyGitlab {
		if done, err := mirrorMR(l, cp, branch, trailer, lg, base); done || err != nil {
			return nested, err
		}
	}
	// A Checkpoint's Decision Packet is the at-head Approval's body (empty for a pin-only
	// or pre-packet Approval); a Chart's Land MR body is its summary, and a local Chart land has none.
	var body string
	if cp.Kind == contract.KindCheckpoint {
		approval, err := checkpointApproval(cp)
		if err != nil {
			return nested, err
		}
		if approval != nil {
			body = git.DecisionPacket(approval.Description)
		}
		if name == git.LandStrategyLocal {
			return nested, gatedLand(l, cp, branch, trailer, pin, body, lg)
		}
	} else if name == git.LandStrategyGitlab {
		body = contract.ChartSummary(cp.Description)
	}
	if name == git.LandStrategyGitlab {
		// push and stamp the Landing's pin, not whatever the branch is by the lock (FR 9)
		jira, err := jiraPrefix(cp) // computed per Landing, never stored
		if err != nil {
			return nested, err
		}
		strategy = pinnedGitlab(lg.repo, pin, git.MRPresentation{Title: jira + cp.Title, Body: body})
	}
	sha, err := lg.land(branch, trailer, strategy)
	var pending *git.LandPending
	if errors.As(err, &pending) {
		eventlog.RecordLandPending(l, branch, pending.MR, pending.Pushed, pending.Opened, lg.attrs...)
		if err := recordMR(l, lg.mrPath, pending.MR); err != nil {
			return nested, err
		}
		pushGraph(l)
		return nested, nil
	}
	if !errors.Is(err, forge.ErrRateLimited) { // held once by reconcileLanding, not an error per tick (FR 35)
		eventlog.RecordLand(l, branch, sha, err, lg.attrs...)
	}
	return nested, landed(l, cp, sha, err, lg)
}

// landedClose closes a Landing whose trailer is on mainline at sha; under gitlab a
// stale-pin squash also files its offshoot Signal first.
func landedClose(l, cp bd.Bead, branch, sha, strategy string, lg legs) error {
	if strategy == git.LandStrategyGitlab {
		if err := flagStalePin(l, cp, branch, sha, lg); err != nil {
			return err
		}
		if err := closeLandedMR(l, branch, lg); err != nil {
			return err
		}
		landOutcome(lg, l, eventlog.LandLanded, git.RecordedMR(l.Description), "sha", sha)
	}
	return closeIfOpen(l, "landed: "+sha)
}

// closeLandedMR closes whatever MR is still open on a landed container's branch: GitLab
// allows one open MR per source branch and the merged one is no longer open, so any
// open one is stale (a fetch/push race's orphan, ADR 0185 §6). No iid comparison. It
// runs before the bead closes, so a failure leaves the bead open and the next tick retries.
func closeLandedMR(b bd.Bead, branch string, lg legs) error {
	iid, err := forge.CloseOpenMR(lg.proj, branch, forgeFindOpenMR, forgeCloseMR)
	if err != nil || iid == 0 {
		return err
	}
	eventlog.RecordLandMRClosed(b, iid, "container landed", lg.attrs...)
	return nil
}

// rateLimited remembers the Landings and Rollbacks whose 429 is already logged, so a Forge rate
// limit is one event, not one per tick (Spec FR 35).
var rateLimited sync.Map

// strandedLogged remembers the Rollbacks whose revert.stranded is already logged, so a
// Rollback left open for a human is one event, not one per tick.
var strandedLogged sync.Map

// pruneRateLimited drops the beads that closed (or vanished) while limited, which
// their reconcile never sees again; run on each new 429, so the map stays small.
func pruneRateLimited() { pruneClosed(&rateLimited) }

// pruneClosed drops the entries of m, keyed by bead id, whose bead is no longer open.
func pruneClosed(m *sync.Map) {
	m.Range(func(k, _ any) bool {
		if b, err := showBead(k.(string)); err == nil && !b.Open() {
			m.Delete(k)
		}
		return true
	})
}

// mirrorMR is a pending gitlab Landing's per-tick read of the MR it recorded: one MR
// read, no backoff. The fixed order merged → closed → conflict → pipeline-failed →
// pending (Spec FR 33). The MR read is by the Landing's own recorded iid, so a closed
// one is this Landing's and closes it obsolete; an opened one asserts its description
// from the snapshot and is done (the write leg runs only before MR: is recorded, FR 36).
// Any Forge error is returned. An obsolete Landing makes no Forge write (FR 34).
func mirrorMR(l, cp bd.Bead, branch, trailer string, lg legs, base string) (done bool, err error) {
	iid := git.RecordedMR(l.Description)
	if iid == 0 {
		return false, nil
	}
	mr, err := forgeMR(lg.proj, iid)
	if err != nil {
		return true, err // a 429 is held by reconcileLanding
	}
	if mr.State == forge.StateMerged {
		// LandedBase missed at the tick's fetch, but the merge may have landed since: fetch
		// again, and only a second miss is trailerless
		if _, err := fetchForge(lg.repo); err != nil {
			return true, err
		}
		if sha, err := lg.landedBase(trailer); err == nil {
			return true, landedClose(l, cp, branch, sha, git.LandStrategyGitlab, lg)
		} else if foreign := (*git.ForeignLanding)(nil); errors.As(err, &foreign) {
			return true, bd.HoldForeign(cp, foreign)
		} else if !errors.Is(err, git.ErrNotLanded) {
			return true, err // a failed read says nothing about the trailer
		}
		return true, holdTrailerless(cp, lg.mrPath, "merge request", "MR", git.TrailerFor(string(cp.Kind), cp.ID), iid, cmp.Or(mr.MergeCommitSHA, mr.SquashCommitSHA))
	}
	if mr.State == forge.StateOpened {
		if done, err := threadArm(l, cp, lg, mr, branch); done || err != nil { // first: a filed thread changes the tip the other arms would act on
			return done, err
		}
		if done, err := conflictArm(&l, cp, branch, trailer, mr, lg, base); done || err != nil {
			return done, err
		}
		recordMRStatus(&l, mr.DetailedMergeStatus)
		if mr.HeadPipeline != nil && mr.HeadPipeline.Status == forge.StatusFailed {
			// a head pipeline that ran on an older head (a force-push whose new pipeline is
			// not created yet, or skipped) is a previous attempt's failure: pending
			if heldPipeline(l.Description, *mr.HeadPipeline) { // checked first: a held run costs no Forge read
				return true, nil
			}
			if ran, err := ranOnHead(lg.proj, mr); err != nil {
				return true, err
			} else if ran {
				return pipelineFailed(l, cp, lg, *mr.HeadPipeline)
			}
			landOutcome(lg, l, eventlog.LandStalled, iid, "pipeline", mr.HeadPipeline.ID)
			return true, nil
		}
		// the pin cannot change during a Landing's life, so the push and re-list are redundant
		pin, _ := git.ParseCommit(l.Description)
		return true, forgeAssertDesc(lg.repo, mr, pin, trailer)
	}
	if mr.State != forge.StateClosed {
		return true, nil // locked (merge in progress) or unknown: pending, never the write leg (FR 26)
	}
	ref := git.MRRef(lg.mrPath, iid)
	body := fmt.Sprintf("MR %s for %s was closed on the Forge, so its Landing was closed obsolete.\nClosed MR: !%d\n"+
		"Fix: reopen or merge MR %s, or abandon the container, then close this Task. "+
		"The next Landing reuses a reopened MR or opens a fresh one.\n", ref, cp.ID, iid, ref)
	if _, err := holdTask(cp, fmt.Sprintf("Closed MR: !%d\n", iid), "merge request "+ref+" was closed", body); err != nil {
		return true, err
	}
	landOutcome(lg, l, eventlog.LandClosed, iid)
	return true, closeIfOpen(l, "obsolete: merge request closed")
}

// conflictArm keys conflict on facts, never detailed_merge_status (Spec FR 24–26): the
// MR's has_conflicts or a merge_error, else a local Compose trial against the fetched
// base (ADR-number clash, then rebase) whenever that base moved off the Landing's
// "Checked:" line. A conflict files the container's conflict member at the base and
// closes the Landing obsolete: conflict, the MR left open and untouched (FR 34).
func conflictArm(l *bd.Bead, cp bd.Bead, branch, trailer string, mr *forge.MR, lg legs, base string) (bool, error) {
	var fileErr error
	switch {
	case mr.HasConflicts || mr.MergeError != "":
		fileErr = fileConflict(cp, base, nil, lg.head, false)
	case checkedBase(l.Description) != base:
		_, _, _, err := lg.compose(branch, trailer, "")
		var conflict *git.RebaseConflict
		var clash *git.NumberClash
		switch {
		case errors.As(err, &conflict):
			fileErr = fileConflict(cp, conflict.Base, nil, lg.head, false)
		case errors.As(err, &clash):
			fileErr = fileConflict(cp, clash.Base, clash.Clashes, lg.head, false)
		case err != nil:
			return true, err
		default:
			return false, recordChecked(l, base)
		}
	default:
		return false, nil
	}
	if fileErr != nil {
		return true, fileErr
	}
	landOutcome(lg, *l, eventlog.LandConflict, mr.IID)
	return true, closeIfOpen(*l, "obsolete: conflict")
}

// recordMRStatus sets the Landing's display-only "MR status: <raw>" line (ADR 0227), skipping
// GitLab's transient statuses and a value already recorded. A failed write only warns: the
// next tick sees the body unchanged and retries, and a failed reconcile would read as retrying.
func recordMRStatus(l *bd.Bead, raw string) {
	switch raw {
	case "", "preparing", "checking", "unchecked", "approvals_syncing":
		return
	}
	if old, _ := git.ParseLine(l.Description, "MR status:"); old == raw {
		return
	}
	body := setBodyLine(l.Description, "MR status: ", "MR status: "+raw)
	if err := updateBody(l.ID, []string{"--description", body}); err != nil {
		slog.Warn("land.mr_status_write_failed", append(eventlog.BeadAttrs(*l), "err", err)...)
		return
	}
	l.Description = body
}

// checkedBase is the sha of the Landing's "Checked: <base sha>" line, "" when it has none.
func checkedBase(body string) string {
	sha, _ := git.ParseLine(body, "Checked:")
	return sha
}

// recordChecked sets the Landing's "Checked: <base>" line, replacing a stale one.
func recordChecked(l *bd.Bead, base string) error {
	body := setBodyLine(l.Description, "Checked: ", "Checked: "+base)
	if err := updateBody(l.ID, []string{"--description", body}); err != nil {
		return err
	}
	l.Description = body // later arms of this tick (the retry record) build on it, not the stale body
	return nil
}

// traceLines is how much of a failed job's trace a Critique or Task carries (ADR 0188 §6).
const traceLines = 80

// retriedLine and pipelineLine are the body lines naming a pipeline by id.
func retriedLine(id int) string  { return fmt.Sprintf("Retried: pipeline %d", id) }
func pipelineLine(id int) string { return fmt.Sprintf("Pipeline: %d", id) }

// retriedPipeline is whether b's body records "Retried: pipeline <id>".
func retriedPipeline(body string, id int) bool {
	return hasBodyLine(body, retriedLine(id))
}

// retryPipeline is the first-failure leg of ADR 0188 for b, a Landing or a Rollback: retry
// the pipeline, run record (its event), then record the retry on b's body. The retry POST
// precedes the record, so a crash between them retries twice at worst, never zero times.
func retryPipeline(p forge.Project, b *bd.Bead, pl forge.Pipeline, record func()) error {
	if err := forgeRetry(p, pl.ID); err != nil {
		return err
	}
	record()
	body := bd.AppendBodyLine(b.Description, retriedLine(pl.ID))
	if err := updateBody(b.ID, []string{"--description", body}); err != nil {
		return err
	}
	b.Description = body
	return nil
}

// hasBodyLine is whether the body holds line as a whole line, wherever it sits.
func hasBodyLine(body, line string) bool {
	return strings.Contains("\n"+body+"\n", "\n"+line+"\n")
}

// heldLine is the Landing's "Held:" line for this run of the pipeline: a Forge retry keeps
// the id but moves updated_at, so a re-run is classified afresh.
func heldLine(pl forge.Pipeline) string {
	return strings.TrimSpace(fmt.Sprintf("Held: pipeline %d %s", pl.ID, pl.UpdatedAt))
}

// heldPipeline is whether the Landing's body records this run of the pipeline as held: the
// failure's Task is filed, so a closed Task must not re-file it for the same run.
func heldPipeline(body string, pl forge.Pipeline) bool {
	return hasBodyLine(body, heldLine(pl))
}

func holdPipeline(l bd.Bead, pl forge.Pipeline) error {
	return updateBody(l.ID, []string{"--description", setBodyLine(l.Description, "Held: ", heldLine(pl))})
}

// setBodyLine is body with every line starting prefix replaced by line, appended when
// none was there.
func setBodyLine(body, prefix, line string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	lines = slices.DeleteFunc(lines, func(s string) bool { return strings.HasPrefix(s, prefix) })
	return strings.Join(append(lines, line), "\n") + "\n"
}

// infraReason is whether a job's failure_reason is the runner's or the scheduler's,
// not the code's (ADR 0188 §3).
func infraReason(reason string) bool {
	switch reason {
	case "runner_system_failure", "runner_unsupported", "scheduler_failure", "data_integrity_failure", "api_failure":
		return true
	}
	return strings.HasPrefix(reason, "stuck_")
}

// infraTaskLead opens the body of the Task a retried pipeline's infra failure files.
const infraTaskLead = "The retried pipeline failed again for an infra reason;"

// ranOnHead is whether the MR's head pipeline ran on the MR's current head: its sha for
// a branch pipeline, the merge commit's second parent for a merged-results one.
func ranOnHead(p forge.Project, mr *forge.MR) (bool, error) {
	pl := mr.HeadPipeline
	if !pl.MergedResults() {
		return pl.SHA == mr.SHA, nil
	}
	src, err := forgeSource(p, pl.SHA)
	return src == mr.SHA, err
}

// pipelineFailed is the head-pipeline arm of mirrorMR (ADR 0188): the first failure
// is retried once and recorded (the Landing stays pending); a retried pipeline that
// fails again for an infra reason files a Task and stays pending; a real failure files
// a Critique (Checkpoint) or a Task (Chart) and closes the Landing obsolete, the MR
// left open. The retry POST precedes the record, so a crash between them retries twice
// at worst, never silently zero times.
func pipelineFailed(l, cp bd.Bead, lg legs, pl forge.Pipeline) (bool, error) {
	if !retriedPipeline(l.Description, pl.ID) {
		return true, retryPipeline(lg.proj, &l, pl, func() {
			landOutcome(lg, l, eventlog.LandRetried, git.RecordedMR(l.Description), "pipeline", pl.ID)
		})
	}
	if heldPipeline(l.Description, pl) {
		return true, nil // its Task was filed; a Forge retry moves updated_at and resumes the arm
	}
	pipelineKey := pipelineLine(pl.ID) + "\n"
	held, err := openBlockers(cp.ID) // an infra Task already filed for this pipeline: skip the jobs and traces
	if err != nil {
		return true, err
	}
	if slices.ContainsFunc(held, func(b bd.Bead) bool {
		return b.Kind == contract.KindTask && strings.Contains(b.Description, infraTaskLead) && strings.Contains(b.Description+"\n", pipelineKey)
	}) {
		return true, nil
	}
	jobs, err := forgeJobs(lg.proj, pl.ID)
	if err != nil {
		return true, err
	}
	infra := slices.ContainsFunc(jobs, func(j forge.Job) bool { return infraReason(j.FailureReason) })
	details, err := pipelineDetails(lg.proj, pl, jobs)
	if err != nil {
		return true, err
	}
	iid := git.RecordedMR(l.Description)
	title := failedTitle(pl.ID, lg.mrPath, iid) // holdTask prefixes the container
	details = mrLead(lg.mrPath, iid) + details
	if infra {
		if _, err := holdTask(cp, pipelineKey, title,
			infraTaskLead+" fix the runner, retry the pipeline on the Forge, then close this Task.\n"+details); err != nil {
			return true, err
		}
		landOutcome(lg, l, eventlog.LandPipelineFailed, git.RecordedMR(l.Description), "pipeline", pl.ID, "infra", true)
		return true, holdPipeline(l, pl)
	}
	if cp.Kind == contract.KindChart {
		if _, err := holdTask(cp, pipelineKey, title,
			"The retried pipeline failed on this Chart's branch; main or the CI config is likely broken. Fix it, then close this Task.\n"+details); err != nil {
			return true, err
		}
	} else if err := fileCritique(l, cp, lg.proj, pl, cp.Title+": "+title, details); err != nil {
		return true, err
	}
	landOutcome(lg, l, eventlog.LandPipelineFailed, git.RecordedMR(l.Description), "pipeline", pl.ID)
	return true, closeIfOpen(l, "obsolete: pipeline failed")
}

// pipelineDetails is the pipeline URL and the failing jobs' trace tails a pipeline-failure
// Task or Critique carries.
func pipelineDetails(p forge.Project, pl forge.Pipeline, jobs []forge.Job) (string, error) {
	details := fmt.Sprintf("%s\nPipeline URL: %s\n", pipelineLine(pl.ID), pl.WebURL)
	for _, j := range jobs {
		tail, err := forgeTrace(p, j.ID)
		if err != nil && !errors.Is(err, forge.ErrNotFound) {
			return "", err
		}
		details += fmt.Sprintf("\nJob: %s\nJob URL: %s\nfailure_reason: %s\n```\n%s\n```\n", j.Name, j.WebURL, j.FailureReason, tail)
	}
	return details, nil
}

// mrLead names a nested MR at the head of a pipeline-failure Task or Critique body; a
// Workspace's is "" (ADR 0230).
func mrLead(path string, iid int) string {
	if path == "" {
		return ""
	}
	return "Merge Request: " + git.MRRef(path, iid) + "\n"
}

// failedTitle is "pipeline <id> failed", plus " on merge request <group/proj>!<iid>" for a nested MR.
func failedTitle(id int, path string, iid int) string {
	t := fmt.Sprintf("pipeline %d failed", id)
	if path != "" {
		t += " on merge request " + git.MRRef(path, iid)
	}
	return t
}

// fileCritique files the Checkpoint's pipeline-failure Critique like a failed gate
// build (ADR 0152): Tip: and, for a merged-results pipeline, Base: = its target sha.
// Idempotent on (Tip, Pipeline: <id>).
func fileCritique(l, cp bd.Bead, p forge.Project, pl forge.Pipeline, title, details string) error {
	tip, _ := git.ParseCommit(l.Description)
	open, err := openCritiques(cp.ID)
	if err != nil {
		return err
	}
	for _, c := range open {
		if t, ok := git.ParseTip(c.Description); ok && t == tip && strings.Contains(c.Description+"\n", pipelineLine(pl.ID)+"\n") {
			return nil
		}
	}
	pins := git.TipPin(tip) + "\n"
	if pl.MergedResults() {
		base, err := forgeTarget(p, pl.SHA)
		if err != nil {
			return err
		}
		pins += git.BasePin(base) + "\n"
	}
	_, _, err = spawnChild(cp.ID, bd.Successor{
		Kind:           contract.KindCritique,
		Title:          title,
		Description:    "The Merge Request pipeline failed after one retry. Franky fixes it test-first; the tip moves and a fresh Review, Approval and Landing follow.\n" + pins + details,
		DiscoveredFrom: l.ID,
	})
	return err
}

// holdTask files the Brook-owned Task blocking cp, idempotent on key, titled
// "<container>: <what>".
func holdTask(cp bd.Bead, key, what, body string) (string, error) {
	id, _, err := bd.SpawnBlockingTask(cp.ID, key, cp.Title+": "+what, body)
	return id, err
}

// holdTrailerless is the merged arm's miss: the MR is merged and fetched, yet no
// trailer is on mainline (a hand-edited squash message dropped it). The Landing stays
// open; one Brook-owned Task blocks the container, idempotent on (container, MR iid)
// (Spec FR 19, ADR 0185 §5). noun and label name the MR in running text and at a sentence
// start: "merge request"/"MR" for a land's, "revert merge request"/"Revert MR" for a
// Rollback's. Returns the error that keeps the tick failing.
func holdTrailerless(cp bd.Bead, path, noun, label, trailer string, iid int, sha string) error {
	ref := git.MRRef(path, iid)
	// The key stays the bare "Merged MR: !<iid>" on purpose (Spec FR 29, ADR 0230): a nested MR must not re-key.
	body := fmt.Sprintf("%s %s for %s was merged at %s, but no %q trailer reached mainline.\nMerged MR: !%d\nMerge sha: %s\n"+
		"Fix: a hand-edited squash message dropped the trailer. Push a commit stamped with it to the Forge's main (not local main, which would diverge), then close this Task.\n"+
		"Check: did this merge add an ADR or other sequence number mainline already held? Post-land clash detection cannot see it without the trailer.\n",
		label, ref, cp.ID, sha, trailer, iid, sha)
	if _, err := holdTask(cp, fmt.Sprintf("Merged MR: !%d\n", iid), fmt.Sprintf("%s %s merged without its trailer", noun, ref), body); err != nil {
		return err
	}
	return fmt.Errorf("jinbe: %s %s for %s is merged but its trailer is not on mainline (merge sha %s)", noun, ref, cp.ID, sha)
}

// flagStalePin is nami.FlagStalePin over Jinbe's seams.
func flagStalePin(l, cp bd.Bead, branch, sha string, lg legs) error {
	return nami.FlagStalePin(l, cp, branch, sha, func(sha string) (string, bool) { return landedTip(lg.repo, sha) }, lg.head, contexts.AncestorIn(lg.repo, isAncestor), spawnSignal)
}

// recordMR appends "MR: !<iid>" (or "MR: <group/proj>!<iid>" for a nested repo's project, ADR 0230) to the Landing's body unless it already carries an
// MR: line, so later ticks and Landings know which MR this attempt was (Spec FR 12).
func recordMR(l bd.Bead, path string, iid int) error {
	_, err := bd.SetLineIfUnset(updateBody, l.ID, l.Description, "MR:", git.MRLine(path, iid))
	return err
}

// pushGraph is the Landing's automatic Graph push (ADR 0216): any failure is an
// event and never an error, so a side ref cannot stop code from landing. A foreign
// root also files the non-blocking Task.
func pushGraph(l bd.Bead) {
	name, err := forgeUser()
	if err != nil {
		eventlog.RecordGraphPush(l, "", err)
		return
	}
	_, err = graphPush(name)
	ref := bd.GraphRefOf(name)
	eventlog.RecordGraphPush(l, ref, err)
	var foreign *bd.ForeignGraphRef
	if errors.As(err, &foreign) {
		key := fmt.Sprintf("Graph ref: %s\nTheirs: %s\n", foreign.Ref, foreign.Theirs)
		body := key + fmt.Sprintf("Ours: %s\n\nAnother Bead Graph owns this ref, so the Graph push was refused and nothing was pushed.\n"+
			"Fix: check the GitLab account (`glab auth status`), or have the ref's owner confirm. mugicrew will not overwrite or delete the ref. Close this Task when settled.\n",
			foreign.Ours)
		if _, _, terr := spawnUnblockingTask(key, "Graph ref "+foreign.Ref+" holds another Bead Graph", body); terr != nil {
			eventlog.RecordGraphPush(l, ref, terr)
		}
	}
}

// landOutcome is eventlog.RecordLandOutcome with the legs' nested attrs (context=/repo=/into=), none for a Workspace land.
func landOutcome(lg legs, b bd.Bead, outcome eventlog.LandOutcome, mr int, extra ...any) {
	eventlog.RecordLandOutcome(b, outcome, mr, append(extra, lg.attrs...)...)
}

// legs are the git/gate legs a land drives, each the Workspace's (through the
// package seams) or, for a repo = container, the child's (Spec D7).
type legs struct {
	repo       git.Repo // the child's main checkout; "" for a Workspace land
	tree       git.Repo // the child inside this land tree (<tree>/<dir>), where Compose runs
	landedBase func(trailer string) (string, error)
	head       func(ref string) (string, error)
	compose    func(branch, trailer, packet string) (composed, base, pin string, err error)
	sameBase   func(composed, base, pin string) git.LandStrategy
	land       func(branch, trailer string, st git.LandStrategy) (string, error)
	gateAttr   func() string // read only on the gated path
	gateBuild  func(landingID, rev, attr string) (string, error)
	dir        string        // the child's dir from the Workspace root; "" for a Workspace land
	attrs      []any         // context=/repo=/into= on a nested land's events
	proj       forge.Project // the GitLab project of repo's Forge remote; zero unless legsFor ran under gitlab
	mrPath     string        // the project path an MR line names: "" for the Workspace's, else proj's path (ADR 0230)
}

// legsFor resolves cp's land legs. ok is false when cp's repo: stamp disagrees with
// the live repo = (Spec FR 17): the caller returns without acting. Under gitlab it
// resolves the repo's Forge project once, so no Forge seam falls back to the cwd's.
func legsFor(cp bd.Bead, gitlab bool) (lg legs, ok bool, err error) {
	if bd.RepoStampOf(cp) != "" { // ADR 0157 zero-cost guard: unstamped pays no drift read
		if _, _, drifted := contexts.Drift(cp, liveRepos()); drifted {
			return legs{}, false, nil
		}
	}
	r, dir := repoDirOf(cp)
	var proj forge.Project
	if gitlab {
		if proj, err = resolveProject(r); err != nil {
			return legs{}, false, err
		}
	}
	if r == "" {
		return legs{landedBase: landedBase, head: branchHead, compose: composeLanding, sameBase: sameBaseLand,
			land: landBranch, gateAttr: gateAttr, gateBuild: gateBuild, proj: proj}, true, nil
	}
	root, err := git.TopLevelOf(".")
	if err != nil {
		return legs{}, false, err
	}
	tree := git.Repo(filepath.Join(root, dir))
	return legs{proj: proj, mrPath: proj.Path(),
		repo: r, dir: dir, tree: tree, landedBase: r.LandedBase, head: r.Head,
		compose: tree.Compose, sameBase: tree.LandSameBase, land: r.Land,
		gateAttr: func() string { return contextGate(root, bd.ContextOf(cp)) },
		gateBuild: func(landingID, rev, attr string) (string, error) {
			common, err := git.CommonDirOf(string(r))
			if err != nil {
				return "", &looplock.InfraError{Err: err}
			}
			return gateBuildAt(context.Background(), common, landingID, rev, attr)
		},
		attrs: contexts.NestedAttrs(cp, r, dir),
	}, true, nil
}

// landed closes l for the outcome of a land: a conflict or clash files the
// parent's conflict member and closes it obsolete: conflict, ErrLandPending leaves
// it open, any other error is returned, success closes it landed: <sha>.
func landed(l, cp bd.Bead, sha string, err error, lg legs) error {
	if err != nil {
		var conflict *git.RebaseConflict
		var clash *git.NumberClash
		switch {
		case errors.As(err, &conflict):
			err = fileConflict(cp, conflict.Base, nil, lg.head, false)
		case errors.As(err, &clash):
			err = fileConflict(cp, clash.Base, clash.Clashes, lg.head, false)
		case errors.Is(err, git.ErrLandPending):
			return nil
		default:
			return err
		}
		if err != nil {
			return err
		}
		return closeIfOpen(l, "obsolete: conflict")
	}
	return closeIfOpen(l, "landed: "+sha)
}

// maxGateAttempts bounds how often a gated Landing re-composes after mainline
// moves under it before it gives up for this tick (ADR 0150).
const maxGateAttempts = 3

// gatedLand lands a Checkpoint under the local strategy only after its composed
// squash passed the nix build, and moves main to exactly that commit (ADR 0150,
// 0151). Per attempt: compose; gate build; same-base land. A moved base re-runs the
// attempt; a moved pin closes the Landing obsolete; an infra failure is returned
// with the Landing open. Nothing is remembered across ticks.
func gatedLand(l, cp bd.Bead, branch, trailer, pin, packet string, lg legs) error {
	var lastBase, lastComposed string
	attr := lg.gateAttr()
	for attempt := 1; attempt <= maxGateAttempts; attempt++ {
		composed, base, composedPin, err := lg.compose(branch, trailer, packet)
		lastBase, lastComposed = base, composed
		if err != nil {
			eventlog.RecordLand(l, branch, "", err, lg.attrs...)
			return landed(l, cp, "", err, lg)
		}
		if composedPin != pin { // the container advanced past what Review and Approval saw (FR6, FR9)
			return closeIfOpen(l, "obsolete: branch advanced without landing")
		}
		gate := func(event string, extra ...any) {
			eventlog.RecordGate(event, l, pin, base, composed, attempt, extra...)
		}
		if dup, err := gateCritiqueOpen(cp.ID, base, pin); err != nil {
			return err
		} else if dup {
			gate("gate.failed", "dur_ms", int64(0), "drv", "", "critique", "deduped")
			return closeIfOpen(l, "obsolete: build failed")
		}
		if attr == "" { // no gate key: this Workspace lands ungated, visibly
			gate("gate.skipped")
		} else {
			gate("gate.started", "ref", "refs/mugicrew/gate/"+l.ID)
			start := time.Now()
			out, err := lg.gateBuild(l.ID, composed, attr)
			ms := time.Since(start).Milliseconds()
			if err != nil {
				return gateOutcome(l, cp, err, base, pin, lg, ms, gate)
			}
			gate("gate.passed", "dur_ms", ms, "out", out)
		}
		sha, err := lg.land(branch, trailer, lg.sameBase(composed, base, pin))
		var baseMoved *git.BaseMoved
		var pinMoved *git.PinMoved
		switch {
		case errors.As(err, &baseMoved):
			continue
		case errors.As(err, &pinMoved):
			eventlog.RecordLand(l, branch, "", err, lg.attrs...)
			return closeIfOpen(l, "obsolete: branch advanced without landing")
		}
		eventlog.RecordLand(l, branch, sha, err, lg.attrs...)
		return landed(l, cp, sha, err, lg)
	}
	eventlog.RecordGate("gate.pending", l, pin, lastBase, lastComposed, maxGateAttempts)
	return nil
}

// gateOutcome handles a failed gate build: an infra failure is logged and
// returned (the Landing stays open); a builder failure spawns a Critique and
// closes the Landing obsolete.
func gateOutcome(l, cp bd.Bead, err error, base, pin string, lg legs, ms int64, gate func(string, ...any)) error {
	var infra *looplock.InfraError
	if errors.As(err, &infra) {
		why := infra.Tail // already capped by looplock
		if why == "" {
			why = infra.Err.Error()
		}
		gate("gate.infra", "dur_ms", ms, "err", why, "timeout", infra.Timeout)
	}
	var builder *looplock.BuilderError
	if !errors.As(err, &builder) {
		return err
	}
	// ponytail: spawnChild dedupes on ANY open Critique, so one pinned to another
	// base absorbs this failure and no base-pinned Critique is filed. Accepted: a
	// held Checkpoint (CheckpointLandHold) is not re-gated while Critiques are open;
	// spawn with a pin match if that proves reachable.
	id, _, cerr := spawnChild(cp.ID, bd.Successor{
		Kind:           contract.KindCritique,
		Title:          "Checkpoint " + cp.ID + " fails the pinned nix build",
		Description:    gateCritiqueBody(builder, base, pin, lg),
		DiscoveredFrom: l.ID,
	})
	if cerr != nil {
		return cerr
	}
	gate("gate.failed", "dur_ms", ms, "drv", builder.Drv, "critique", id)
	return closeIfOpen(l, "obsolete: build failed")
}

// gateCritiqueOpen reports whether cp already has an open Critique pinned to this
// base and tip: the same failing build, already fix work for Franky.
func gateCritiqueOpen(cpID, base, tip string) (bool, error) {
	open, err := openCritiques(cpID)
	if err != nil {
		return false, err
	}
	for _, c := range open {
		b, bok := git.ParseBase(c.Description)
		t, tok := git.ParseTip(c.Description)
		if bok && tok && b == base && t == tip {
			return true, nil
		}
	}
	return false, nil
}

// gateCritiqueBody is the Critique a failed gate build files (ADR 0152): the
// Base:/Tip: pins Franky rebases onto and cuts from, the failing derivation, the
// log tail and a repro. lg names the flake attr that was built and, for a nested
// land, the child's dir.
func gateCritiqueBody(e *looplock.BuilderError, base, tip string, lg legs) string {
	attr := lg.gateAttr()
	where := "the repo root"
	if lg.dir != "" {
		where = "the root of the nested repo `" + lg.dir + "` (git -C " + lg.dir + ")"
	}
	return "The gate build of #" + attr + " failed on the composed squash of this Checkpoint. Franky rebases onto the pinned base, then fixes test-first.\n" +
		git.BasePin(base) + "\n" + git.TipPin(tip) + "\n" +
		"Drv: " + e.Drv + "\n```\n" + strings.TrimRight(e.Log, "\n") + "\n```\n" +
		"Repro: rebase the tip onto the base in a scratch tree, then run `nix build .#" + attr + " -L` at " + where + ".\n"
}

// resolveLandStrategy reads land_strategy from the cwd's repo root. An unusable
// value is an error naming it, never a fallback to local (ADR 0147).
func resolveLandStrategy() (string, git.LandStrategy, error) {
	root, err := git.TopLevelOf(".")
	if err != nil {
		return "", nil, err
	}
	return landStrategyAt(root)
}

func landStrategyAt(root string) (string, git.LandStrategy, error) {
	name, err := loop.LandStrategyAt(root)
	if err != nil {
		return "", nil, fmt.Errorf("jinbe: %w", err)
	}
	st, err := git.LandStrategyByName(name)
	return name, st, err
}

// reconcileRollback undoes a landed Checkpoint on main (ADR 0047), in fixed order:
//
//  1. Resolve the target over the Rollback's related edge, and reject an invalid
//     one — none, still open, or no Checkpoint: trailer on main — by closing the
//     Rollback "invalid: <why>" with nothing written.
//  2. Revert the target's squash forward on main with a Reverts-Checkpoint: <id>
//     trailer. git.Revert is idempotent on that trailer, so a crash resume or a
//     second Rollback against the same target writes no second revert (it still
//     hands off its own successor — succession is per Rollback, ADR 0048 §2). A
//     conflict fixes forward instead (ADR 0049): nothing written to main. Any other
//     failure is loud and leaves the Rollback open; a manual unclaim and
//     re-dispatch converges by idempotency (ADR 0052: no lease).
//  3. Hand the work off (ADR 0048, handOff): successor Checkpoint, its branch cut
//     at the revert and seeded with a reapply — or, fixing forward, cut at main HEAD
//     with no seed — and its one Build.
//  4. Close the Rollback — only once the handoff exists (ADR 0048 §6), reason
//     "fix-forward: revert conflicted" when fixing forward. A fix-forward leaves no
//     Reverts-Checkpoint: trailer, so its resume keys on the successor instead: once
//     spawned it pins the decision (pinnedFixForward) and the revert is not retried.
func reconcileRollback(rb bd.Bead) error {
	nested, err := reconcileRollbackOnce(rb)
	return holdRateLimited(rb, "revert.rate_limited", err, nested)
}

// reconcileRollbackOnce returns the legs' nested attrs once resolved (nil for a Workspace target).
func reconcileRollbackOnce(rb bd.Bead) ([]any, error) {
	if !rb.Open() {
		return nil, nil // closed only at handoff (or as invalid): done, never re-hand-off
	}
	target, ok, err := rollbackTarget(rb.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return closeInvalidRollback(rb, rb, "invalid: no related Checkpoint")
	}
	cp, err := showBead(target)
	if err != nil {
		return nil, err
	}
	if cp.Open() {
		return closeInvalidRollback(rb, decidingBead(rb, cp), "invalid: "+target+" not landed")
	}
	// A nested target reverts and reapplies in its child (Spec FR 13, 17, 49); a stamp
	// that disagrees with the live repo = acts on nothing and the tick scan holds it.
	// One deciding bead drives drift, address and successor: the Rollback when it carries
	// a stamp (the copy the tick scan holds it on, so a relabel lifts the hold), else the target.
	dec := decidingBead(rb, cp)
	if bd.RepoStampOf(dec) != "" { // ADR 0157 zero-cost guard: unstamped pays no drift read
		if _, _, drifted := contexts.Drift(dec, liveRepos()); drifted {
			return nil, nil
		}
	}
	lg := rbLeg{landedBase: landedBase, revertOnto: revertOnto, firstParent: firstParent, cut: cutBranch, seed: reapply}
	revert := revertCommit
	r, dir := repoDirOf(dec)
	if r != "" {
		lg = rbLeg{repo: r, landedBase: r.LandedBase, revertOnto: r.RevertOnto, firstParent: r.FirstParentOnMain, cut: r.CutBranch, seed: r.Reapply}
		revert = r.Revert
		lg.attrs = contexts.NestedAttrs(dec, r, dir)
	}
	name, _, err := landStrategy()
	if err != nil {
		return lg.attrs, err
	}
	var base string // the fetched Forge mainline, gitlab only
	var proj forge.Project
	if name == git.LandStrategyGitlab {
		if proj, err = resolveProject(r); err != nil {
			return lg.attrs, err
		}
		if base, err = fetchForge(r); err != nil {
			return lg.attrs, err
		}
	}
	squash, err := lg.landedBase(checkpointTrailer(target))
	if errors.Is(err, git.ErrNotLanded) {
		return lg.attrs, closeRollback(rb, proj, "invalid: "+target+" has no land trailer", lg.attrs...)
	}
	if err != nil {
		return lg.attrs, err
	}
	if name == git.LandStrategyGitlab {
		lg.proj = proj
		if r != "" {
			lg.mrPath = proj.Path()
		}
		return lg.attrs, reconcileGitlabRollback(rb, cp, lg, squash, base)
	}
	conflict, err := pinnedFixForward(rb.ID)
	if err != nil {
		return lg.attrs, err
	}
	var sha string
	if conflict == nil {
		sha, err = revert(squash, git.RevertsTrailer(string(contract.KindCheckpoint), target))
		eventlog.RecordRevert(rb, target, squash, sha, err, lg.attrs...)
		if err != nil && !errors.As(err, &conflict) {
			return lg.attrs, err
		}
	}
	successor, err := handOff(rb, cp, squash, sha, conflict, lg.cut, lg.seed)
	eventlog.RecordHandoff(rb, successor, err)
	if err != nil {
		return lg.attrs, err
	}
	if conflict != nil {
		return lg.attrs, closeRollback(rb, proj, bd.FixForwardReason+" revert conflicted", lg.attrs...)
	}
	return lg.attrs, closeRollback(rb, proj, "", lg.attrs...)
}

// reconcileGitlabRollback is a gitlab Rollback's tick past the validity checks, on the
// fetched Forge main base (ADR 0213). It never moves local main and never merges:
//
//  1. The Reverts-Checkpoint: trailer is on main ⇒ a human merged the revert MR. Hand
//     off at its first-parent commit M (ADR 0211), keyed on that merge, not the Commit:
//     pin a Forge squash may have rewritten; revert.done before handoff.done (a handoff
//     that errors re-logs revert.done on each tick until it succeeds).
//  2. An MR: !<iid> is recorded ⇒ read it; while open, assert its description and
//     log revert.pending; its close, conflict and pipeline arms are revertMRArms.
//  3. Else build the revert on base (or reuse the Commit: pin), push it to
//     rollback/<id>, open or reuse the MR and record it.
func reconcileGitlabRollback(rb, cp bd.Bead, lg rbLeg, squash, base string) error {
	target := cp.ID
	reverts := git.RevertsTrailer(string(contract.KindCheckpoint), target)
	iid := git.RecordedMR(rb.Description)
	switch x, err := lg.landedBase(reverts); {
	case err == nil:
		conflict, err := pinnedFixForward(rb.ID)
		if err != nil {
			return err
		}
		if conflict != nil {
			return fixForwardRevert(rb, cp, lg, squash, iid, conflict) // a merged revert whose rebuild conflicted: finish the fix-forward
		}
		m, err := lg.firstParent(x)
		if err != nil {
			return err
		}
		eventlog.RecordRevert(rb, target, squash, m, nil, lg.revertAttrs(iid)...)
		successor, err := handOff(rb, cp, squash, m, nil, lg.cut, lg.seed)
		eventlog.RecordHandoff(rb, successor, err)
		if err != nil {
			return err
		}
		return closeRollback(rb, lg.proj, "", lg.attrs...)
	case !errors.Is(err, git.ErrNotLanded):
		return err
	}
	pin, _ := git.ParseCommit(rb.Description)
	if iid != 0 {
		return revertMRArms(rb, cp, lg, squash, base, reverts, iid, pin)
	}
	if pin == "" {
		var err error
		pin, err = lg.revertOnto(base, squash, reverts)
		var rc *git.RevertConflict
		if errors.As(err, &rc) {
			return fixForwardRevert(rb, cp, lg, squash, 0, rc) // no MR yet: closeRollback makes no Forge call
		}
		if err != nil {
			return err
		}
		if err := setLine(&rb, "Commit:", git.CommitPin(pin)); err != nil {
			return err
		}
		if err := setLine(&rb, "Checked:", "Checked: "+base); err != nil { // the base the pin was built on: no trial until main moves
			return err
		}
	}
	p, err := pushRevertMR(lg.repo, rb.ID, reverts, pin)
	if err != nil {
		return err
	}
	if err := setLine(&rb, "MR:", git.MRLine(lg.mrPath, p.MR)); err != nil {
		return err
	}
	eventlog.RecordRevertPending(rb, target, p.MR, p.Pushed, p.Opened, pin, lg.attrs...)
	return nil
}

// revertMRArms is a recorded revert MR's tick (ADR 0208, 0209): merged without its trailer
// holds on a Task; a human close abandons (or, with a successor already spawned, resumes
// the fix-forward); an open MR rebuilds on a conflict, retries a red pipeline once and
// holds on a Task for a real failure. The Landing-shaped mirrorMR arms are not reused.
func revertMRArms(rb, cp bd.Bead, lg rbLeg, squash, base, reverts string, iid int, pin string) error {
	mr, err := forgeMR(lg.proj, iid)
	if err != nil {
		return err
	}
	switch mr.State {
	case forge.StateMerged:
		// the merge may have landed since the tick's fetch: only a second miss is trailerless
		if _, err := fetchForge(lg.repo); err != nil {
			return err
		}
		if _, err := lg.landedBase(reverts); !errors.Is(err, git.ErrNotLanded) {
			return err // landed: the next tick hands off
		}
		return holdTrailerless(rb, lg.mrPath, "revert merge request", "Revert MR", reverts, iid, cmp.Or(mr.MergeCommitSHA, mr.SquashCommitSHA))
	case forge.StateClosed:
		conflict, err := pinnedFixForward(rb.ID)
		if err != nil {
			return err
		}
		if conflict != nil {
			return fixForwardRevert(rb, cp, lg, squash, iid, conflict) // Jinbe's own crash between spawn and close
		}
		if _, ok, err := successorOf(rb.ID, contract.KindCheckpoint); err != nil || ok {
			if ok {
				if _, seen := strandedLogged.LoadOrStore(rb.ID, true); !seen {
					pruneClosed(&strandedLogged)
					eventlog.RecordRevertStranded(rb, iid, lg.attrs...)
				}
			}
			return err // a successor without a fix-forward pin is not ours to abandon
		}
		eventlog.RecordRevertClosed(rb, iid, lg.attrs...)
		return closeRollback(rb, lg.proj, "abandoned: revert MR closed", lg.attrs...)
	case forge.StateOpened:
	default:
		return nil // locked or unknown: pending
	}
	if err := forgeAssertDesc(lg.repo, mr, "", reverts); err != nil {
		return err
	}
	conflicted := mr.HasConflicts || mr.MergeError != ""
	built := checkedBase(rb.Description) == base
	var next string
	var rc *git.RevertConflict
	if conflicted || !built {
		// one trial serves every arm: a pin already on this main has a stale Forge flag until
		// GitLab recomputes, so only a local conflict rebuilds it (rebuilding a clean pin
		// loops every tick); a moved main is tried before the Forge flags it
		var err error
		next, err = lg.revertOnto(base, squash, reverts)
		switch {
		case errors.As(err, &rc):
			conflicted = true
		case err != nil:
			return err
		case built:
			conflicted = false
		case !conflicted:
			if err := recordChecked(&rb, base); err != nil {
				return err
			}
		}
	}
	if conflicted {
		return rebuildRevert(rb, cp, lg, squash, base, reverts, iid, pin, next, rc)
	}
	if from, _ := git.ParseLine(rb.Description, "Rebuilt-from:"); from != "" && mr.SHA == from {
		// a rebuild re-pinned Commit: but its push failed: the MR head is still the replaced
		// pin. Any other head is a human's Forge-side Rebase and stays pending (ADR 0189).
		p, err := pushRevertMR(lg.repo, rb.ID, reverts, pin)
		if err != nil {
			return err
		}
		return closeStrayMR(rb, lg, p.MR, iid)
	}
	if pl := mr.HeadPipeline; pl != nil && pl.Status == forge.StatusFailed && !heldPipeline(rb.Description, *pl) {
		// a pipeline that ran on an older head is a previous attempt's failure: pending
		if ran, err := ranOnHead(lg.proj, mr); err != nil {
			return err
		} else if ran {
			if err := revertPipelineFailed(&rb, lg, iid, *pl); err != nil {
				return err
			}
		}
	}
	eventlog.RecordRevertPending(rb, cp.ID, iid, false, false, pin, lg.attrs...)
	return nil
}

// rebuildRevert rebuilds the pinned revert on the fetched main (ADR 0208 §3): clean ⇒
// re-pin Commit: and force-push to the same MR; conflicted ⇒ fix forward. next and rc are
// the caller's trial of revertOnto on base.
func rebuildRevert(rb, cp bd.Bead, lg rbLeg, squash, base, reverts string, iid int, pin, next string, rc *git.RevertConflict) error {
	if rc != nil {
		return fixForwardRevert(rb, cp, lg, squash, iid, rc)
	}
	body := setBodyLine(setBodyLine(setBodyLine(rb.Description, "Commit:", git.CommitPin(next)), "Checked:", "Checked: "+base), "Rebuilt-from:", "Rebuilt-from: "+pin)
	if err := updateBody(rb.ID, []string{"--description", body}); err != nil {
		return err
	}
	rb.Description = body
	p, err := pushRevertMR(lg.repo, rb.ID, reverts, next)
	if err != nil {
		return err
	}
	if p.MR != iid {
		return closeStrayMR(rb, lg, p.MR, iid)
	}
	eventlog.RecordRevertRebuilt(rb, iid, pin, next, lg.attrs...)
	return nil
}

// closeStrayMR is the re-push's miss: a human closed the recorded MR (iid) under us and
// the push opened another (got). Close the new one and keep MR:, so the next tick
// abandons (FR 32) with none left open. A push onto the recorded MR is a no-op.
func closeStrayMR(rb bd.Bead, lg rbLeg, got, iid int) error {
	if got == iid {
		return nil
	}
	if err := forgeCloseMR(lg.proj, got); err != nil {
		return err
	}
	eventlog.RecordRevertMRClosed(rb, got, "recorded MR closed", lg.attrs...)
	return nil
}

// fixForwardRevert is ADR 0049 for a revert MR (ADR 0208 §3): the successor is spawned
// at the conflict's base, then closeRollback closes the MR and the Rollback.
func fixForwardRevert(rb, cp bd.Bead, lg rbLeg, squash string, iid int, conflict *git.RevertConflict) error {
	eventlog.RecordRevert(rb, cp.ID, squash, "", conflict, lg.revertAttrs(iid)...)
	successor, err := handOff(rb, cp, squash, "", conflict, lg.cut, lg.seed)
	eventlog.RecordHandoff(rb, successor, err)
	if err != nil {
		return err
	}
	return closeRollback(rb, lg.proj, bd.FixForwardReason+" revert conflicted", lg.attrs...)
}

// revertPipelineFailed is ADR 0188 for a revert MR (ADR 0208 §4): retry once and record
// it; a failure after the retry, infra or real, files a HITL Task blocking the Rollback
// with the pipeline URL and failing jobs' tails, and records Held: so a closed Task does
// not re-file for the same run.
func revertPipelineFailed(rb *bd.Bead, lg rbLeg, iid int, pl forge.Pipeline) error {
	if !retriedPipeline(rb.Description, pl.ID) {
		return retryPipeline(lg.proj, rb, pl, func() { eventlog.RecordRevertRetried(*rb, iid, pl.ID, lg.attrs...) })
	}
	jobs, err := forgeJobs(lg.proj, pl.ID)
	if err != nil {
		return err
	}
	infra := slices.ContainsFunc(jobs, func(j forge.Job) bool { return infraReason(j.FailureReason) })
	details, err := pipelineDetails(lg.proj, pl, jobs)
	if err != nil {
		return err
	}
	lead := "The retried pipeline failed on the revert MR; main may be red for the very breakage being rolled back. Merge the MR anyway to hand off, or close it to abandon this Rollback (a closed MR makes this Task moot).\n"
	if infra {
		lead = infraTaskLead + " fix the runner, retry the pipeline on the Forge, then merge the MR to hand off or close it to abandon this Rollback (a closed MR makes this Task moot).\n"
	}
	task, err := holdTask(*rb, pipelineLine(pl.ID)+"\n", failedTitle(pl.ID, lg.mrPath, iid), mrLead(lg.mrPath, iid)+lead+details)
	if err != nil {
		return err
	}
	eventlog.RecordRevertPipelineFailed(*rb, iid, pl.ID, infra, task, lg.attrs...)
	return holdPipeline(*rb, pl)
}

// setLine is bd.SetLineIfUnset on b, keeping b's body current so a second pin written
// in the same tick does not drop the first.
func setLine(b *bd.Bead, key, line string) error {
	body, err := bd.SetLineIfUnset(updateBody, b.ID, b.Description, key, line)
	if err == nil {
		b.Description = body
	}
	return err
}

// resolveProject is r's Forge project; a child's refuses another host than the
// Workspace's before any push or MR call (D10).
func resolveProject(r git.Repo) (forge.Project, error) { return git.SameHostProject(r, projectOf) }

// closeRollback closes rb, first closing a still-open revert MR it recorded (ADR 0208 §6):
// after any successor exists, before the bead, so a crash between them retries the MR.
func closeRollback(rb bd.Bead, p forge.Project, reason string, attrs ...any) error {
	if iid := git.RecordedMR(rb.Description); iid != 0 && rb.Open() {
		mr, err := forgeMR(p, iid)
		if err != nil {
			return err
		}
		if mr.State == forge.StateOpened {
			if err := forgeCloseMR(p, iid); err != nil {
				return err
			}
			eventlog.RecordRevertMRClosed(rb, iid, cmp.Or(reason, "handed off"), attrs...)
		}
	}
	return closeIfOpen(rb, reason)
}

// closeInvalidRollback closes rb before the tick resolved its project: a recorded revert
// MR is closed on dec's project (the Workspace's or its child's), never an empty one.
// It returns the nested attrs it resolved, so a 429 still logs them (Spec FR 30).
func closeInvalidRollback(rb, dec bd.Bead, reason string) (attrs []any, _ error) {
	var p forge.Project
	if git.RecordedMR(rb.Description) != 0 {
		if name, _, err := landStrategy(); err != nil {
			return nil, err
		} else if name == git.LandStrategyGitlab {
			r, dir := repoDirOf(dec)
			if p, err = resolveProject(r); err != nil {
				return nil, err
			}
			if r != "" {
				attrs = contexts.NestedAttrs(dec, r, dir)
			}
		}
	}
	return attrs, closeRollback(rb, p, reason, attrs...)
}

// rbLeg is a Rollback's repo-bound legs: the Workspace's (zero repo, nil attrs) or a
// nested child's, so no step of a nested Rollback reads or writes the Workspace's main.
type rbLeg struct {
	repo        git.Repo      // the child; "" for a Workspace Rollback
	attrs       []any         // context=/repo=/into= on a nested Rollback's revert events
	proj        forge.Project // the project every MR leg acts on: the Workspace's or the child's
	mrPath      string        // the child's project path an MR line names; "" for the Workspace's
	landedBase  func(trailer string) (string, error)
	revertOnto  func(base, squash, trailer string) (string, error)
	firstParent func(x string) (string, error)
	cut         func(name, base string) (bool, error)
	seed        func(branch, revert string) (string, error)
}

// revertAttrs is a revert event's into= (the repo's trunk) plus the MR iid when recorded.
func (lg rbLeg) revertAttrs(iid int) []any {
	attrs := slices.Clone(lg.attrs)
	if attrs == nil {
		attrs = []any{"into", git.Mainline()}
	}
	if iid != 0 {
		attrs = append(attrs, "mr", iid)
	}
	return attrs
}

// decidingBead is the one bead a nested Rollback's context and repo stamp are read
// from together: the Rollback when stamped, else the rolled-back Checkpoint.
func decidingBead(rb, cp bd.Bead) bd.Bead {
	if bd.RepoStampOf(rb) != "" {
		return rb
	}
	return cp
}

// handOff brings the reverted work back as a successor Checkpoint (ADR 0048), each
// leg skip-if-done so a crash anywhere resumes cleanly (ADR 0003): spawn it caused-by
// the Rollback (idempotent per Rollback; no supersedes edge — the target is history),
// cut checkpoint/<new> at the revert commit, seed it with the reapply commit, and
// spawn its one Build carrying the breakage evidence. Fixing forward (conflict
// non-nil, ADR 0049) the branch is cut at main HEAD with no seed — the code is still
// on main — and the Build also carries the conflict. The successor joins the
// rolled-back Checkpoint's Initiative only while it is unclosed (Spec D2, FR32).
// Returns the successor's id.
func handOff(rb, rolledBack bd.Bead, squash, revert string, conflict *git.RevertConflict,
	cutBranch func(name, base string) (bool, error), reapply func(branch, revert string) (string, error)) (string, error) {
	target := rolledBack.ID
	dec := decidingBead(rb, rolledBack)
	desc := "Successor of " + target + ", reverted on main by Rollback " + rb.ID + " (ADR 0048)."
	if conflict != nil {
		desc = "Successor of " + target + ", fixing forward for Rollback " + rb.ID + ": its revert conflicted, main is unchanged (ADR 0049).\n" +
			git.ConflictPin(conflict)
	}
	// The Spec line is required by Spec §4 ("its body states the Spec") for a
	// human or agent reading the body; Go readers still derive it with bd.SpecOf.
	spec, ok, err := specOf(target)
	if err != nil {
		return "", err
	}
	if ok {
		desc += "\nSpec: " + spec
	}
	parent, err := initiativeOf(rolledBack)
	if err != nil {
		return "", err
	}
	cp, _, err := spawnSuccessor(rb.ID, bd.Successor{
		Kind:        contract.KindCheckpoint,
		Container:   true,
		Title:       "Rollback of " + target,
		Description: desc,
		Parent:      parent,
		Context:     cmp.Or(bd.ContextOf(dec), bd.ContextOf(rb)),
		Repo:        bd.RepoStampOf(dec),
	})
	if err != nil {
		return "", err
	}
	branch := checkpointBranch(cp)
	base, body := revert, buildBody(rb.Description, target, squash)
	if conflict != nil {
		base, body = conflict.Base, fixForwardBody(rb.Description, squash, conflict.Paths)
	}
	if _, err := cutBranch(branch, base); err != nil {
		return cp, err
	}
	if conflict == nil {
		if _, err := reapply(branch, revert); err != nil {
			return cp, err
		}
	}
	// Any Build, open or closed: SpawnChildIfAbsent alone guards only an OPEN one, and
	// Franky may finish the Build before a crashed pass resumes.
	if n, err := buildChildren(cp); err != nil || n > 0 {
		return cp, err
	}
	_, _, err = spawnChild(cp, bd.Successor{
		Kind:        contract.KindBuild,
		Title:       "Fix what " + target + " broke on main",
		Description: body,
	})
	return cp, err
}

// buildBody is the successor Build's body (ADR 0048 §5): the Rollback's breakage
// evidence, or — filed with an empty body — the concrete first step.
func buildBody(rollbackBody, target, squash string) string {
	if strings.TrimSpace(rollbackBody) != "" {
		return rollbackBody
	}
	return "Reproduce the breakage on main at " + target + "'s land (" + squash + "), then fix it."
}

// fixForwardBody is a fix-forward successor Build's body (ADR 0049 §3): the evidence
// (or reproduce-then-fix on main HEAD), the target squash, the conflicted paths, and
// why the revert was skipped. The later lands are left for Franky to derive.
func fixForwardBody(rollbackBody, squash string, paths []string) string {
	if strings.TrimSpace(rollbackBody) == "" {
		rollbackBody = "Reproduce the breakage on main HEAD, then fix it."
	}
	return rollbackBody + "\n\nTarget squash: " + squash +
		"\n" + git.PathsPin(paths) +
		"\nThe revert was skipped because later lands built on it (git log " + squash + "..HEAD -- <paths>)."
}

// pinnedFixForward reads back a fix-forward decision a prior pass already acted on:
// the Rollback's successor Checkpoint exists and its body carries the
// ConflictPin (Fix-forward: base, conflicted paths) handOff stamped. Once that
// successor is spawned the decision is final, so a resume never retries the revert —
// main may have changed so it now applies cleanly, which would revert under a
// successor cut for fixing forward.
// No successor, or a clean-revert one (no Fix-forward: pin), returns nil: decide afresh.
func pinnedFixForward(rbID string) (*git.RevertConflict, error) {
	succ, ok, err := successorOf(rbID, contract.KindCheckpoint)
	if err != nil || !ok {
		return nil, err
	}
	b, err := showBead(succ)
	if err != nil {
		return nil, err
	}
	c, _ := git.ParseConflict(b.Description)
	return c, nil
}

// spawnReviewIfDue spawns the bare Review of a drained Checkpoint with no Approval
// at head (bd.ReviewDue). Jinbe re-derives it every tick under the Checkpoint's
// status-CAS claim, so no member has to decide it is last and a crash between a
// member's close and the spawn strands nothing; SpawnChildIfAbsent plus the
// open-Review leg of ReviewDue make a re-run spawn no second one (ADR 0103). An
// open Approval at head waits for the human; a stale one gets a fresh Review,
// whose pass obsoletes it.
func spawnReviewIfDue(cp bd.Bead) error {
	due, err := reviewDue(cp.ID, contexts.AtHead(cp, approvedAtHead))
	if err != nil || !due {
		return err
	}
	// the probe runs where a Landing would: the child for a repo = context (ADR 0166)
	repo, _ := repoDirOf(cp)
	head := branchHead
	if repo != "" {
		head = repoHead(repo)
	}
	base, err := probeBase(repo)
	if err != nil {
		return err
	}
	var conflict *git.RebaseConflict
	var clash *git.NumberClash
	switch err := probeConflict(repo, checkpointBranch(cp.ID), base); {
	case errors.As(err, &conflict):
		return fileConflict(cp, base, nil, head, true)
	case errors.As(err, &clash):
		return fileConflict(cp, base, clash.Clashes, head, true)
	case err != nil:
		return err
	}
	_, _, err = spawnChild(cp.ID, reviewSuccessor(cp.ID))
	return err
}

// reviewSuccessor is the one Review member a drained Checkpoint spawns: a bare
// kind:review leaf with NO Commit: pin, because Usopp pins git.Head at review
// start himself. Pure, so the emitted shape is checkable without a DB.
func reviewSuccessor(checkpointID string) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindReview,
		Container:   false,
		Title:       "Built slice of checkpoint " + checkpointID,
		Description: "Review the built Checkpoint slice — Usopp pins the branch HEAD at review start (no Commit: pin seeded here).",
	}
}

// checkpointApproval is the approvedAtHead gate (FR40): the first Approval closed approved
// child whose Commit: pin still equals checkpoint/<id>'s live tip, or nil. It scans ALL closed
// Approvals, not the first: after a stale-Approval self-heal a
// Checkpoint carries the stale obsoleted Approval (pin behind HEAD) AND the real at-head
// one, and a first-match read hits the stale pin and strands the land. No closed Approval
// at the tip (all pins behind it, a post-approval commit moved HEAD) is nil, and
// a drained Checkpoint then gets a fresh Review (Spec FR 31).
func checkpointApproval(cp bd.Bead) (*bd.Bead, error) {
	approvals, err := closedChildrenOfKind(cp.ID, contract.KindApproval)
	if err != nil {
		return nil, err
	}
	atHead := contexts.AtHead(cp, approvedAtHead)
	for _, a := range approvals {
		if a.ApprovedClosed() && atHead(a.Description, checkpointBranch(cp.ID)) {
			return &a, nil
		}
	}
	return nil, nil
}

// fileConflict raises the one open kind:conflict member that undrains the Checkpoint
// on a rebase collision (mirrors fileChartConflict — but Franky-owned AFK, unlike the
// HITL chart-conflict): idempotent via SpawnChildIfAbsent's open-child guard, so a
// re-run with the conflict still open files no second bead. The undrain is what keeps
// the loop off it: loop.CheckpointReady excludes a Checkpoint with an open conflict/
// critique child, so this bead being open drops it from land-candidacy until
// Franky resolves it. The member carries the Base: pin (git.BasePin) Franky's runConflict
// greps back to rebase onto, and the Tip: pin (git.TipPin) of the Checkpoint tip the
// failed land saw: Land never writes the branch (ADR 0102 §8), so the tip read now is
// it, and Franky's effort is cut there and replace-CASes it (Spec D3).
// Non-empty clashes is the number-clash variant: the body names the colliding
// numbered files to renumber instead. preReview words the body for a collision the probe found
// before the first Review (no Landing failed, nothing re-lands yet); the pins, kind and
// title stay the same so Franky's procedure is one. Returns nil on success — the
// conflict is now a bead, not a propagated error.
func fileConflict(cp bd.Bead, base string, clashes []string, head func(string) (string, error), preReview bool) error {
	tip, err := head("refs/heads/" + git.BranchName(string(cp.Kind), cp.ID))
	if err != nil {
		return err
	}
	if cp.Kind == contract.KindChart {
		variant := nami.ConflictRebase
		if len(clashes) > 0 {
			variant = nami.ConflictNumberClash
		}
		return spawnConflict(cp.ID, bd.Successor{
			Kind:        contract.KindChartConflict,
			Title:       cp.Title + " conflicts with main",
			Description: nami.ChartConflictBody(cp.ID, base, tip, variant, clashes),
		})
	}
	where, after := "Checkpoint-land ", "the Checkpoint re-lands"
	if preReview {
		where, after = "", "the Checkpoint is probed again and the Review follows"
	}
	body := where + "rebase conflict: checkpoint/" + cp.ID + " would not rebase cleanly onto mainline. Franky re-runs the rebase onto the pinned base, then this bead closes and " + after + ".\n"
	if len(clashes) > 0 {
		// a renumber Checkpoint's own beads widen to the container it was minted for (ADR 0225 §5)
		// only a renumber Checkpoint (caused-by a container) widens; skip the subtree reads otherwise
		var beads []string
		_, _, widened, err := nami.RenumberPred(cp.ID, causedBy)
		if err == nil && widened {
			beads, _, err = nami.RenumberScope(cp, subtreeIDs, causedBy)
		}
		if err != nil {
			return err
		}
		body = where + "numbered-file clash (ADR 0219): checkpoint/" + cp.ID + " adds a numbered file whose number mainline already holds in its sequence under another filename. Franky rebases onto the pinned base, then reads crew doc sequence-renumber.md and renumbers the branch's file to the next free number (fixing references to it), then this bead closes and " + after + ".\nclashes:\n- " + strings.Join(clashes, "\n- ") + "\n"
		if widened {
			body += "own beads: " + strings.Join(beads, ", ") + "\n"
		}
	}
	if preReview {
		body = "Found before review: " + body
	}
	return spawnConflict(cp.ID, bd.Successor{
		Kind:        contract.KindConflict,
		Container:   false,
		Title:       "Checkpoint " + cp.ID + " conflicts with main",
		Description: body + git.BasePin(base) + "\n" + git.TipPin(tip),
	})
}

// spawnConflict files a real Conflict (s.Kind: conflict or chart-conflict) under the
// container. An open requested rebase on it yields (ADR 0261): unclaimed, it is closed
// superseded: <title> before the filing, then linked to the new Conflict by bd's supersede
// edge; claimed, the open-child guard swallows the Conflict and the request finishes.
// Re-runs converge: a crash between close and file leaves no open child; one between
// file and edge is repaired from the closed request, the latest superseded for this title
// with no other Conflict filed after it. ponytail: that scan reads the kind's children each call.
func spawnConflict(containerID string, s bd.Successor) error {
	kids, err := childrenOfKind(containerID, s.Kind)
	if err != nil {
		return err
	}
	reason := "superseded: " + s.Title
	var old *bd.Bead
	for i, k := range kids {
		if nami.BodyVariant(k.Description) != nami.ConflictRequested {
			continue
		}
		switch {
		case k.Status == "open":
			if err := closeBead(k.ID, reason); err != nil {
				return err
			}
			kids[i].Status, kids[i].CloseReason, kids[i].Closed = "closed", reason, time.Now()
			old = &kids[i]
		case !k.Open() && k.CloseReason == reason && (old == nil || k.Closed.After(old.Closed)):
			old = &kids[i]
		}
	}
	id, created, err := spawnChild(containerID, s)
	if err != nil || old == nil {
		return err
	}
	if !created && !filedRightAfter(*old, id, kids) {
		return nil
	}
	return supersede(old.ID, id)
}

// filedRightAfter reports whether id, an open Conflict the open-child guard returned, is
// the real Conflict filed right after the closed request old: no other child was created
// since old closed, and id is not itself a (claimed) requested rebase.
func filedRightAfter(old bd.Bead, id string, kids []bd.Bead) bool {
	for _, k := range kids {
		if k.ID != id && k.ID != old.ID && !k.Created.Before(old.Closed) {
			return false
		}
	}
	i := slices.IndexFunc(kids, func(k bd.Bead) bool { return k.ID == id })
	return i >= 0 && nami.BodyVariant(kids[i].Description) != nami.ConflictRequested
}

// closeIfOpen closes b with reason behind the Open() guard so a re-dispatch after a
// committed-but-not-closed crash converges without a double close (mirrors the reconcile
// close guards).
func closeIfOpen(b bd.Bead, reason string) error {
	if b.Open() {
		return closeBead(b.ID, reason)
	}
	return nil
}
