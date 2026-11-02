package remnant

import (
	"errors"
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
)

// RespinSeams are Respin's graph and git writes beyond an exit's own, a field set so
// tests fake them.
type RespinSeams struct {
	Seams
	SpecOf       func(checkpoint string) (string, bool, error)
	Initiative   func(b bd.Bead) (string, error)
	RespinOf     func(remnant string) (string, bool, error)
	SpawnRespin  func(remnant string, s bd.Successor) (string, error)
	Publish      func(remnant, reason string) error
	Mainline     func() (string, error)
	Replay       func(tip, mainline, head string) (git.Replay, error)
	Seed         func(mainline, tree, subject string, trailers ...string) (string, error)
	Cut          func(name, base string) (bool, error)
	Children     func(id string) ([]bd.Bead, error)
	NewMember    func(parent string, s bd.Successor) (string, error)
	SupersededBy func(old string) (string, error)
	Supersede    func(old, with string) error
	Adopt        func(old, new string) error
}

// RealRespin is RespinSeams over bd and the cwd's repo.
var RealRespin = RespinSeams{
	Seams:       Real,
	SpecOf:      bd.SpecOf,
	Initiative:  bd.InitiativeOf,
	RespinOf:    func(r string) (string, bool, error) { return bd.SuccessorOf(r, contract.KindCheckpoint) },
	SpawnRespin: spawnRespin,
	Publish:     publishRespin,
	Mainline: func() (string, error) {
		return git.FetchMainline(git.Repo("").ForgeRemote())
	},
	Replay:       git.Repo("").ReplayOnto,
	Seed:         git.Repo("").SeedCommit,
	Cut:          git.CutBranch,
	Children:     bd.ListChildren,
	NewMember:    bd.SpawnChild,
	SupersededBy: bd.SupersededBy,
	Supersede:    bd.Supersede,
	Adopt:        git.Repo("").AdoptEffort,
}

func spawnRespin(remnant string, s bd.Successor) (string, error) {
	id, _, err := bd.SpawnSuccessorIfAbsent(remnant, s)
	return id, err
}

// publishRespin publishes the Remnant's Drafts (the respin) with the Remnant's close as
// the batch's closing: the one atomic commit point.
func publishRespin(remnant, reason string) error {
	_, err := bd.Publish(remnant, nil, &bd.Closing{ID: remnant, Reason: reason})
	return err
}

// RespinResult is what a respin did, for the caller to print.
type RespinResult struct {
	Result
	Checkpoint string   // the respin's id
	Title      string   // the respin's title
	Conflict   string   // the replay Conflict's title, "" when the replay was clean
	Refiled    []string // new member ids
}

// RespinTitle is the title of the respin of the landed Checkpoint.
func RespinTitle(landed bd.Bead) string { return "Respin of " + landed.Title }

// respinBody is the respin's body (Spec FR 36): never a Base:/Tip: line.
func respinBody(rem, landed bd.Bead, sha, spec, tip, strandedLine string) string {
	lines := []string{fmt.Sprintf("Respin of %s, which a Forge merge landed at %s; Remnant %s.", landed.ID, sha, rem.ID)}
	if spec != "" {
		lines = append(lines, "Spec: "+spec)
	}
	lines = append(lines, bd.FormatTipLines(tip, strandedLine)...)
	return strings.Join(lines, "\n")
}

// Respin cuts a new Checkpoint for the landed Checkpoint's Spec from fresh mainline,
// seeds it with the stranded range replayed three-way, re-files the Remnant's Builds
// and Critiques as its members, carries a before-land Errand's hold, and publishes the
// respin Draft with the Remnant's close respun: <checkpoint>. Each leg is skip-if-done
// so a re-run converges; that publish, with its closing, is the commit point. A
// conflicting replay cuts with no seed and files a replay Conflict that blocks every
// re-filed member until Franky's seed integrates.
func Respin(s RespinSeams, id string) (RespinResult, error) {
	// the Renumber hold is checked before take, which sweeps late children: a refused
	// respin has no side effects
	if cpID, ok, err := s.Landed(id); err != nil {
		return RespinResult{}, err
	} else if ok {
		renumbers, err := s.Renumbers(cpID)
		if err != nil {
			return RespinResult{}, err
		}
		for _, c := range renumbers {
			if c.Unclosed() { // the respin goes second (ADR 0408 §1)
				return RespinResult{}, fmt.Errorf("respin of Remnant %s waits on Renumber Checkpoint %s; it holds until that closes", id, c.Title)
			}
		}
	}
	rem, landed, swept, err := take(s.Seams, id)
	res := RespinResult{Result: Result{Swept: swept}}
	if err != nil {
		return res, err
	}
	renumbers, err := s.Renumbers(landed.ID)
	if err != nil {
		return res, err
	}
	rb := bd.ParseRemnantBody(rem.Description)
	tip := rb.LandedTip()
	if tip == "" || tip == "unknown" {
		return res, fmt.Errorf("Remnant %s has no Landed-tip: there is no merge base to replay from; drop or offshoot it", rem.Title)
	}
	head, strandedLine, sha := rb.StrandedHead(), rb.StrandedLine(), rb.LandedSHA()
	spec, _, err := s.SpecOf(landed.ID)
	if err != nil {
		return res, err
	}
	cpID, have, err := s.RespinOf(rem.ID)
	if err != nil {
		return res, err
	}
	needsCut := !have
	if have {
		ok, err := s.HasBranch(git.BranchName(string(contract.KindCheckpoint), cpID))
		if err != nil {
			return res, err
		}
		needsCut = !ok // already cut: its seed or lack of one is settled
	}
	var mainline string
	var replay git.Replay
	pinned := false // a replay Conflict from an earlier run already fixes the mainline
	if needsCut {
		if have {
			if mainline, pinned, err = existingReplayBase(s, cpID); err != nil {
				return res, err
			}
		}
		if !pinned {
			if mainline, err = s.Mainline(); err != nil {
				return res, err
			}
		}
		if head != "" && !pinned {
			if replay, err = s.Replay(tip, mainline, head); err != nil {
				return res, err
			}
		}
	}
	if !have {
		parent, err := s.Initiative(landed)
		if err != nil {
			return res, err
		}
		if cpID, err = s.SpawnRespin(rem.ID, bd.Successor{
			Kind:        contract.KindCheckpoint,
			Container:   true,
			Title:       RespinTitle(landed),
			Description: respinBody(rem, landed, sha, spec, tip, strandedLine),
			Parent:      parent,
			Context:     bd.ContextOf(landed),
			Repo:        bd.RepoStampOf(landed),
			DraftedBy:   rem.ID, // a Draft until the final publish, so no tick sees it memberless
		}); err != nil {
			return res, err
		}
	}
	res.Checkpoint, res.Title = cpID, RespinTitle(landed)
	conflictID := ""
	if needsCut {
		base := mainline
		rng := nami.ReplayRange{Mainline: mainline, Tip: tip, Head: head}
		// pinned: cut where the filed Conflict says, so its Replace integrate finds the container at Base:
		if pinned {
			res.Conflict = nami.ReplayConflictTitle(landed.Title)
		} else if head != "" && !replay.Clean() {
			// filed before the cut, so a re-run that finds the branch cut finds it too
			if conflictID, err = replayConflict(s, cpID, rem, landed, rng); err != nil {
				return res, err
			}
			res.Conflict = nami.ReplayConflictTitle(landed.Title)
		} else if head != "" {
			if base, err = s.Seed(mainline, replay.Tree, nami.ReplaySeedSubject(landed.Title),
				nami.ReplaySeedTrailers(rng, rem.ID)...); err != nil {
				return res, err
			}
		}
		if _, err = s.Cut(git.BranchName(string(contract.KindCheckpoint), cpID), base); err != nil {
			return res, err
		}
	}
	if conflictID == "" {
		if conflictID, err = existingReplayConflict(s, cpID); err != nil {
			return res, err
		}
		if conflictID != "" {
			res.Conflict = nami.ReplayConflictTitle(landed.Title)
		}
	}
	if res.Refiled, err = refile(s, rem, cpID, conflictID); err != nil {
		return res, err
	}
	if err = carryErrands(s, rem, cpID); err != nil {
		return res, err
	}
	if err = sweepMoves(s, renumbers, cpID); err != nil {
		return res, err
	}
	return res, s.Publish(rem.ID, "respun: "+cpID)
}

// refile files an open member of the respin for each Build and Critique the Remnant
// lists as a Child:, superseding the old closed child. A Child: with a Kept: effort
// adopts it; one whose cut ref is gone is filed unadopted with the kept ref named as a
// cherry-pick source. Anything else is process, left closed.
func refile(s RespinSeams, rem bd.Bead, cpID, blocker string) ([]string, error) {
	rb := bd.ParseRemnantBody(rem.Description)
	claimed := map[string]bool{} // members already superseding one of the old children
	for _, c := range rb.Children() {
		id, err := s.SupersededBy(c.ID)
		if err != nil {
			return nil, err
		}
		if id != "" {
			claimed[id] = true
		}
	}
	var filed []string
	for _, c := range rb.Children() {
		oldID := c.ID
		old, err := s.Show(oldID)
		if err != nil {
			return filed, err
		}
		if old.Kind != contract.KindBuild && old.Kind != contract.KindCritique {
			continue
		}
		if strings.HasSuffix(c.Value, draftMark) {
			continue
		}
		newID, err := s.SupersededBy(oldID)
		if err != nil {
			return filed, err
		}
		if newID == "" { // member, edge and adoption are each skip-if-done, in crash order
			if newID, err = memberFor(s, cpID, old, claimed); err != nil {
				return filed, err
			}
			claimed[newID] = true
			filed = append(filed, newID)
			if blocker != "" {
				if err := s.Block(newID, blocker); err != nil {
					return filed, err
				}
			}
			if err := s.Supersede(oldID, newID); err != nil {
				return filed, err
			}
		}
		if k, ok := rb.KeptFor(git.EffortBranch(oldID)); ok {
			if err := adopt(s, oldID, newID, k.Value); err != nil {
				return filed, err
			}
		}
	}
	return filed, nil
}

// memberFor is the respin's member for old: one of cpID's Builds or Critiques already
// filed under old's kind and title (a crash after the create) that no other old child
// has claimed, else a new one: two stranded children may share a title.
func memberFor(s RespinSeams, cpID string, old bd.Bead, claimed map[string]bool) (string, error) {
	kids, err := s.Children(cpID)
	if err != nil {
		return "", err
	}
	for _, k := range kids {
		if k.Kind == old.Kind && k.Title == old.Title && k.Unclosed() && !claimed[k.ID] {
			return k.ID, nil
		}
	}
	body := old.Description
	if old.Kind == contract.KindCritique {
		body = dropPins(body)
	}
	return s.NewMember(cpID, bd.Successor{Kind: old.Kind, Title: old.Title, Description: body})
}

// adopt gives newID oldID's kept effort, once: effort/<new> existing is done. Only a
// missing cut ref falls back to a cherry-pick note on the member; any other error stops.
func adopt(s RespinSeams, oldID, newID, kept string) error {
	if ok, err := s.HasBranch(git.EffortBranch(newID)); err != nil || ok {
		return err
	}
	err := s.Adopt(oldID, newID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, git.ErrNoCutRef) {
		return err
	}
	note := "Cherry-pick source: " + kept + " (its cut ref is gone, so the effort was not adopted)"
	return s.AppendLines(newID, note)
}

// existingReplayConflict is the id of cpID's replay Conflict, "" when it has none.
func existingReplayConflict(s RespinSeams, cpID string) (string, error) {
	kids, err := s.Children(cpID)
	if err != nil {
		return "", err
	}
	for _, k := range kids {
		if k.Kind == contract.KindConflict && nami.BodyVariant(k.Description) == nami.ConflictReplay {
			return k.ID, nil
		}
	}
	return "", nil
}

// existingReplayBase is the Base: of the respin's replay Conflict, when an earlier run
// filed one: the branch must be cut there, not at a mainline that has moved since.
func existingReplayBase(s RespinSeams, cpID string) (string, bool, error) {
	kids, err := s.Children(cpID)
	if err != nil {
		return "", false, err
	}
	for _, k := range kids {
		if k.Kind == contract.KindConflict && nami.BodyVariant(k.Description) == nami.ConflictReplay {
			base, ok := git.ParseBase(k.Description)
			return base, ok, nil
		}
	}
	return "", false, nil
}

// replayConflict files the respin's replay Conflict, once: pinned Base: mainline and
// Tip: the old head, with the range start in its Replay-from: line (Spec mc-d8xi1 D6).
func replayConflict(s RespinSeams, cpID string, rem, landed bd.Bead, rng nami.ReplayRange) (string, error) {
	if id, err := existingReplayConflict(s, cpID); err != nil || id != "" {
		return id, err
	}
	return s.NewMember(cpID, bd.Successor{
		Kind:        contract.KindConflict,
		Title:       nami.ReplayConflictTitle(landed.Title),
		Description: nami.ReplayConflictBody(rem.ID, landed.Title, rng),
	})
}

// dropPins is body without its Base: and Tip: lines, so the member integrates in
// Squash mode rather than being closed moot by offPin.
func dropPins(body string) string {
	var keep []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "Base:") || strings.HasPrefix(l, "Tip:") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

// carryErrands copies each before-land Errand's blocks edge onto the respin, so the
// land hold applies to the work that will land.
func carryErrands(s RespinSeams, rem bd.Bead, cpID string) error {
	for _, id := range bd.ParseRemnantBody(rem.Description).ErrandIDs() {
		e, err := s.Show(id)
		if err != nil {
			return err
		}
		if !e.Unclosed() {
			continue
		}
		if err := s.Block(cpID, id); err != nil {
			return err
		}
	}
	return nil
}

// sweepTitlePrefix opens the title of the respin's sweep member, which marks it as filed.
const sweepTitlePrefix = "Sweep references to renumbered "

// sweepMoves files the respin's last member when a same-land Renumber Checkpoint landed
// its moves (ADR 0408 §4): a Franky Build worded as crew doc sequence-renumber.md step 5,
// blocked by every other member. It is skip-if-done, and re-adds the edges on a re-run.
func sweepMoves(s RespinSeams, renumbers []bd.Bead, cpID string) error {
	var moves []string
	for _, c := range renumbers {
		m, _, err := landedMoves(s.Moves, c)
		if err != nil {
			return err
		}
		moves = append(moves, m...)
	}
	if len(moves) == 0 {
		return nil
	}
	kids, err := s.Children(cpID)
	if err != nil {
		return err
	}
	sweep := ""
	for _, k := range kids {
		if k.Kind == contract.KindBuild && strings.HasPrefix(k.Title, sweepTitlePrefix) {
			sweep = k.ID
		}
	}
	if sweep == "" {
		if sweep, err = s.NewMember(cpID, bd.Successor{
			Kind:        contract.KindBuild,
			Title:       sweepTitlePrefix + strings.Join(moves, ", "),
			Description: sweepBody(cpID, moves),
		}); err != nil {
			return err
		}
		if kids, err = s.Children(cpID); err != nil {
			return err
		}
	}
	for _, k := range kids {
		if k.ID != sweep {
			if err := s.Block(sweep, k.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// sweepBody is the sweep Build's brief: crew doc sequence-renumber.md step 5 over the
// moves that landed, scoped to the respin branch's files and its own beads.
func sweepBody(cpID string, moves []string) string {
	var b strings.Builder
	b.WriteString("A Renumber Checkpoint landed these moves on mainline, and the respin's replayed and new work may still cite the old numbers. " +
		"Read crew doc sequence-renumber.md and do step 5 (sweep stale references to each moved number) for each move:\n\n")
	for _, m := range moves {
		fmt.Fprintf(&b, "Move: %s\n", m)
	}
	fmt.Fprintf(&b, "\nFile sweep: the files this branch changed (git diff --name-only $(git merge-base <mainline> HEAD) HEAD).\n")
	fmt.Fprintf(&b, "Bead sweep: Checkpoint %s and its member beads (bd children %s); no other bead.\n", cpID, cpID)
	return b.String()
}
