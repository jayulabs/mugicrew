package remnant

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
)

type respinFake struct {
	*fake
	respin     string
	spawned    []bd.Successor
	cut        map[string]string
	members    map[string]bd.Successor
	supersedes map[string]string
	blocks     [][2]string
	adopted    [][2]string
	noCut      map[string]bool
	replay     git.Replay
	seeds      []string
	published  bool
	n          int
}

const remnantBody = "Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: tip1\nStranded: tip1..head1\n" +
	"Kept: effort/b2 c..h\nKept: effort/b3 c..h\nKept: effort/c1 c..h\n" +
	"Child: b1 build Plan — obsolete: x\nChild: b2 build Refused — obsolete: x\nChild: b3 build Old — obsolete: x\n" +
	"Child: c1 critique Defect — obsolete: x\nChild: cf conflict Rebase — obsolete: x\nChild: rv review Review — obsolete: x\n" +
	"Errand: e1 Hold (a before-land hold)"

func newRespinFake(body string) *respinFake {
	f := &respinFake{fake: newFake(body), cut: map[string]string{}, members: map[string]bd.Successor{}, supersedes: map[string]string{}, noCut: map[string]bool{"b3": true}, replay: git.Replay{Tree: "tree1"}}
	f.beads["cp"] = bd.Bead{ID: "cp", Title: "Cp", Kind: contract.KindCheckpoint, Status: "closed"}
	for id, k := range map[string]contract.Kind{"b1": contract.KindBuild, "b2": contract.KindBuild, "b3": contract.KindBuild, "c1": contract.KindCritique, "cf": contract.KindConflict, "rv": contract.KindReview} {
		f.beads[id] = bd.Bead{ID: id, Kind: k, Status: "closed", Title: "T" + id, Description: "body " + id + "\nBase: aaa\nTip: bbb\nkeep"}
	}
	return f
}

func (f *respinFake) seams() RespinSeams {
	base := f.fake.seams()
	base.Block = func(a, b string) error { f.blocks = append(f.blocks, [2]string{a, b}); return nil }
	return RespinSeams{
		Seams:      base,
		SpecOf:     func(string) (string, bool, error) { return "spec1", true, nil },
		Initiative: func(bd.Bead) (string, error) { return "init1", nil },
		RespinOf:   func(string) (string, bool, error) { return f.respin, f.respin != "", nil },
		SpawnRespin: func(_ string, s bd.Successor) (string, error) {
			if f.respin == "" {
				f.respin = "new1"
				f.spawned = append(f.spawned, s)
			}
			return f.respin, nil
		},
		Publish: func(rem, reason string) error {
			f.published = true
			f.closed[rem] = reason
			return nil
		},
		Mainline: func() (string, error) { return "main1", nil },
		Replay:   func(tip, m, head string) (git.Replay, error) { return f.replay, nil },
		Seed: func(m, tree, subj string, tr ...string) (string, error) {
			f.seeds = append(f.seeds, subj+"|"+strings.Join(tr, "|"))
			return "seed1", nil
		},
		Cut: func(name, base string) (bool, error) {
			if _, ok := f.cut[name]; ok {
				return false, nil
			}
			f.cut[name] = base
			f.branches[name] = true
			return true, nil
		},
		Children: func(string) ([]bd.Bead, error) {
			var out []bd.Bead
			for id := range f.members {
				out = append(out, f.beads[id])
			}
			return out, nil
		},
		NewMember: func(parent string, s bd.Successor) (string, error) {
			f.n++
			id := "m" + string(rune('0'+f.n))
			f.members[id] = s
			f.beads[id] = bd.Bead{ID: id, Kind: s.Kind, Status: "open", Title: s.Title, Description: s.Description}
			return id, nil
		},
		SupersededBy: func(old string) (string, error) { return f.supersedes[old], nil },
		Supersede:    func(old, with string) error { f.supersedes[old] = with; return nil },
		Adopt: func(old, new string) error {
			if f.noCut[old] {
				return fmt.Errorf("adopt: %w", git.ErrNoCutRef)
			}
			f.adopted = append(f.adopted, [2]string{old, new})
			f.branches["effort/"+new] = true
			return nil
		},
	}
}

func TestRespinSpawnsOnceAndRefilesBuildsAndCritiques(t *testing.T) {
	f := newRespinFake(remnantBody)
	res, err := Respin(f.seams(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if res.Checkpoint != "new1" || res.Title != "Respin of Cp" || f.closed["r"] != "respun: new1" {
		t.Fatalf("res %+v closed %v", res, f.closed)
	}
	s := f.spawned[0]
	if s.Title != "Respin of Cp" || s.Parent != "init1" || !s.Container {
		t.Fatalf("spawn %+v", s)
	}
	for _, want := range []string{"Respin of cp, which a Forge merge landed at sq1; Remnant r.", "Spec: spec1", "Landed-tip: tip1", "Stranded: tip1..head1"} {
		if !strings.Contains(s.Description, want) {
			t.Errorf("body lacks %q:\n%s", want, s.Description)
		}
	}
	if strings.Contains(s.Description, "Base:") || strings.Contains(s.Description, "Tip:") {
		t.Errorf("body has a pin:\n%s", s.Description)
	}
	if f.cut["checkpoint/new1"] != "seed1" || len(f.seeds) != 1 ||
		f.seeds[0] != "replay: stranded work of Cp|Replayed: tip1..head1|Remnant: r" {
		t.Fatalf("cut %v seeds %v", f.cut, f.seeds)
	}
	if len(res.Refiled) != 4 || f.supersedes["b1"] == "" || f.supersedes["b2"] == "" || f.supersedes["b3"] == "" || f.supersedes["c1"] == "" ||
		f.supersedes["cf"] != "" || f.supersedes["rv"] != "" {
		t.Fatalf("supersedes %v refiled %v", f.supersedes, res.Refiled)
	}
	if f.members[f.supersedes["b1"]].Description != "body b1\nBase: aaa\nTip: bbb\nkeep" {
		t.Errorf("build body altered: %q", f.members[f.supersedes["b1"]].Description)
	}
	if d := f.members[f.supersedes["c1"]].Description; d != "body c1\nkeep" {
		t.Errorf("critique body keeps pins: %q", d)
	}
	if want := [][2]string{{"b2", f.supersedes["b2"]}, {"c1", f.supersedes["c1"]}}; !slices.Equal(f.adopted, want) {
		t.Errorf("adopted %v want %v", f.adopted, want)
	}
	if d := f.beads[f.supersedes["b3"]].Description; !strings.Contains(d, "Cherry-pick source: effort/b3 c..h (its cut ref is gone, so the effort was not adopted)") || strings.Contains(d, "Cherry-pick source: Kept:") {
		t.Errorf("unadopted body lacks cherry-pick source: %q", d)
	}
	if !slices.Contains(f.blocks, [2]string{"new1", "e1"}) {
		t.Errorf("errand edge not copied: %v", f.blocks)
	}
	if _, err := Respin(f.seams(), "r"); err != nil && !strings.Contains(err.Error(), "not open") {
		t.Fatal(err)
	}
}

func TestRespinRerunFilesNothingTwice(t *testing.T) {
	f := newRespinFake(remnantBody)
	s := f.seams()
	if _, err := Respin(s, "r"); err != nil {
		t.Fatal(err)
	}
	delete(f.closed, "r") // a crash before the close
	n := len(f.members)
	if _, err := Respin(s, "r"); err != nil {
		t.Fatal(err)
	}
	if len(f.spawned) != 1 || len(f.members) != n || len(f.seeds) != 1 {
		t.Fatalf("spawned %d members %d seeds %d", len(f.spawned), len(f.members), len(f.seeds))
	}
}

func TestRespinRefusals(t *testing.T) {
	for name, body := range map[string]string{
		"no tip":     "Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: unknown\nStranded: unknown (the landed squash carries no Tip:)",
		"absent tip": "Remnant of cp, which a Forge merge landed at sq1.",
	} {
		f := newRespinFake(body)
		if _, err := Respin(f.seams(), "r"); err == nil || f.respin != "" || f.closed["r"] != "" {
			t.Errorf("%s: err %v spawned %q closed %q", name, err, f.respin, f.closed["r"])
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRespinCutsMainlinePlusSeedCommitInARealRepo(t *testing.T) {
	// The nix sandbox has no git identity; the seed's own git commit-tree needs one.
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write := func(name, s string) { os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644) }
	write("a.txt", "one\n")
	write("b.txt", "b\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	tip := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "-qb", "stranded")
	write("a.txt", "ONE\n")
	gitIn(t, dir, "commit", "-qam", "stranded")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "-q", "main")
	write("b.txt", "bb\n")
	gitIn(t, dir, "commit", "-qam", "mainline")
	main := gitIn(t, dir, "rev-parse", "HEAD")

	r := git.Repo(dir)
	f := newRespinFake("Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: " + tip + "\nStranded: " + tip + ".." + head)
	s := f.seams()
	s.Mainline = func() (string, error) { return main, nil }
	s.Replay, s.Seed, s.Cut = r.ReplayOnto, r.SeedCommit, r.CutBranch
	if _, err := Respin(s, "r"); err != nil {
		t.Fatal(err)
	}
	cut := gitIn(t, dir, "rev-parse", "checkpoint/new1")
	if gitIn(t, dir, "rev-parse", cut+"^") != main {
		t.Fatal("seed is not one commit on mainline")
	}
	msg := gitIn(t, dir, "log", "-1", "--format=%B", cut)
	for _, want := range []string{"replay: stranded work of Cp", "Replayed: " + tip + ".." + head, "Remnant: r"} {
		if !strings.Contains(msg, want) {
			t.Errorf("seed lacks %q:\n%s", want, msg)
		}
	}
	if got := gitIn(t, dir, "show", cut+":a.txt") + gitIn(t, dir, "show", cut+":b.txt"); got != "ONEbb" {
		t.Errorf("tree %q", got)
	}
}

func TestRespinConflictingReplayCutsEmptyAndFilesReplayConflict(t *testing.T) {
	f := newRespinFake(remnantBody)
	f.replay = git.Replay{Conflicts: []string{"a.txt"}}
	res, err := Respin(f.seams(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if f.cut["checkpoint/new1"] != "main1" || len(f.seeds) != 0 {
		t.Fatalf("cut %v seeds %v", f.cut, f.seeds)
	}
	if res.Conflict != "Replay of stranded work of Cp" || f.closed["r"] != "respun: new1" {
		t.Fatalf("res %+v closed %v", res, f.closed)
	}
	var conflict string
	for id, m := range f.members {
		if m.Kind != contract.KindConflict {
			continue
		}
		if conflict != "" {
			t.Fatalf("two replay Conflicts: %v", f.members)
		}
		conflict = id
		for _, want := range []string{"Base: main1", "Tip: head1", "Replay-from: tip1", "variant: replay",
			"Seed-subject: replay: stranded work of Cp", "Seed-trailer: Replayed: tip1..head1", "Seed-trailer: Remnant: r"} {
			if !strings.Contains(m.Description, want) {
				t.Errorf("conflict body lacks %q:\n%s", want, m.Description)
			}
		}
	}
	if conflict == "" || len(res.Refiled) != 4 {
		t.Fatalf("conflict %q refiled %v", conflict, res.Refiled)
	}
	for _, id := range res.Refiled {
		if !slices.Contains(f.blocks, [2]string{id, conflict}) {
			t.Errorf("member %s not blocked by the replay Conflict: %v", id, f.blocks)
		}
	}
	// a re-run after a crash before the close files no second Conflict
	delete(f.closed, "r")
	n := len(f.members)
	if _, err := Respin(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if len(f.members) != n {
		t.Errorf("re-run filed members: %d -> %d", n, len(f.members))
	}
}

func TestRespinWaitsForAnOpenRenumberCheckpoint(t *testing.T) {
	f := newRespinFake(remnantBody)
	f.renum = []bd.Bead{{ID: "rn", Title: "Renumber 0405-a.md from cp", Kind: contract.KindCheckpoint, Status: "open"}}
	f.children = []bd.Bead{{ID: "late", Kind: contract.KindCritique, Title: "Defect", Status: "open"}}
	_, err := Respin(f.seams(), "r")
	if err == nil || !strings.Contains(err.Error(), "Renumber 0405-a.md from cp") {
		t.Fatalf("err %v", err)
	}
	if len(f.forced) != 0 || f.closed["late"] != "" {
		t.Fatalf("refused respin swept a late child: forced %v closed %v", f.forced, f.closed)
	}
	if f.respin != "" || f.closed["r"] != "" || len(f.cut) != 0 {
		t.Fatalf("refused respin wrote: respin %q closed %v cut %v", f.respin, f.closed, f.cut)
	}
	f.renum[0].Status = "closed" // for any reason
	f.children = nil
	if _, err := Respin(f.seams(), "r"); err != nil {
		t.Fatalf("hold did not lift: %v", err)
	}
	if len(f.members) != 4 {
		t.Errorf("a Renumber closed without landing files no sweep, members %d", len(f.members))
	}
}

func TestRespinSweepsLandedMovesLastBlockedByEveryMember(t *testing.T) {
	f := newRespinFake(remnantBody)
	f.renum = []bd.Bead{{ID: "rn", Title: "Renumber 0405-a.md from cp", Kind: contract.KindCheckpoint, Status: "closed", CloseReason: "landed: rnsha"}}
	s := f.seams()
	s.Moves = func(sha string) ([]string, error) {
		if sha != "rnsha" {
			t.Errorf("moves read from %q", sha)
		}
		return []string{"docs/adr/0405-a.md → docs/adr/0410-a.md"}, nil
	}
	var kids []bd.Bead
	s.Children = func(string) ([]bd.Bead, error) { return kids, nil }
	s.NewMember = func(parent string, sc bd.Successor) (string, error) {
		f.n++
		id := "m" + string(rune('0'+f.n))
		f.members[id] = sc
		f.beads[id] = bd.Bead{ID: id, Kind: sc.Kind, Status: "open", Description: sc.Description}
		kids = append(kids, bd.Bead{ID: id, Title: sc.Title, Kind: sc.Kind, Status: "open", Description: sc.Description})
		return id, nil
	}
	if _, err := Respin(s, "r"); err != nil {
		t.Fatal(err)
	}
	var sweepID string
	for id, m := range f.members {
		if strings.HasPrefix(m.Title, "Sweep references") {
			sweepID = id
			for _, want := range []string{"crew doc sequence-renumber.md", "step 5", "docs/adr/0405-a.md → docs/adr/0410-a.md", "new1"} {
				if !strings.Contains(m.Description, want) {
					t.Errorf("sweep body lacks %q:\n%s", want, m.Description)
				}
			}
			if m.Kind != contract.KindBuild {
				t.Errorf("sweep kind %v", m.Kind)
			}
		}
	}
	if sweepID == "" {
		t.Fatalf("no sweep member: %v", f.members)
	}
	var blockers []string
	for _, b := range f.blocks {
		if b[0] == sweepID {
			blockers = append(blockers, b[1])
		}
	}
	if len(blockers) != len(f.members)-1 {
		t.Errorf("sweep blocked by %v, members %v", blockers, f.members)
	}
	delete(f.closed, "r")
	n := len(f.members)
	if _, err := Respin(s, "r"); err != nil {
		t.Fatal(err)
	}
	if len(f.members) != n {
		t.Errorf("re-run filed a second sweep")
	}
}

// A Draft the sweep closed with the other unstarted children carries a draft mark on
// its Child: line; the respin leaves it closed rather than publishing it as a member.
func TestRespinDoesNotRefileDraftChild(t *testing.T) {
	body := strings.NewReplacer("Child: b1 build Plan — obsolete: x", "Child: b1 build Plan — obsolete: x"+draftMark,
		"Child: c1 critique Defect — obsolete: x", "Child: c1 critique Defect — obsolete: x"+draftMark).Replace(remnantBody)
	f := newRespinFake(body)
	res, err := Respin(f.seams(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if f.supersedes["c1"] != "" || f.supersedes["b1"] != "" || len(res.Refiled) != 2 {
		t.Fatalf("draft re-filed: supersedes %v refiled %v", f.supersedes, res.Refiled)
	}
}

func TestRespinCrashAfterNewMemberFilesNoSecondMember(t *testing.T) {
	f := newRespinFake(remnantBody)
	s := f.seams()
	boom := errors.New("boom")
	s.Supersede = func(old, with string) error { return boom }
	if _, err := Respin(s, "r"); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if len(f.members) != 1 {
		t.Fatalf("members after crash %v", f.members)
	}
	if _, err := Respin(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if len(f.members) != 4 || f.supersedes["b1"] != "m1" {
		t.Errorf("re-run refiled: members %v supersedes %v", f.members, f.supersedes)
	}
}

func TestRespinCrashAfterSupersedeStillAdopts(t *testing.T) {
	f := newRespinFake(remnantBody)
	s := f.seams()
	boom := errors.New("boom")
	s.Adopt = func(old, new string) error { return boom }
	if _, err := Respin(s, "r"); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if f.supersedes["b2"] == "" || len(f.adopted) != 0 {
		t.Fatalf("supersedes %v adopted %v", f.supersedes, f.adopted)
	}
	if _, err := Respin(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	if want := [][2]string{{"b2", f.supersedes["b2"]}, {"c1", f.supersedes["c1"]}}; !slices.Equal(f.adopted, want) {
		t.Errorf("adopted %v want %v", f.adopted, want)
	}
	if n := strings.Count(f.beads[f.supersedes["b3"]].Description, "Cherry-pick source"); n != 1 {
		t.Errorf("cherry-pick note count %d", n)
	}
}

func TestRespinAdoptErrorOtherThanMissingCutRefPropagates(t *testing.T) {
	f := newRespinFake(remnantBody)
	s := f.seams()
	boom := errors.New("git exploded")
	s.Adopt = func(old, new string) error { return boom }
	if _, err := Respin(s, "r"); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if _, closed := f.closed["r"]; closed {
		t.Error("Remnant closed over a failed adoption")
	}
	if strings.Contains(f.beads[f.supersedes["b3"]].Description, "Cherry-pick") {
		t.Error("error was reduced to a note")
	}
}

func TestRespinTwoStrandedCritiquesSharingATitleGetOwnMembers(t *testing.T) {
	body := "Remnant of cp, which a Forge merge landed at sq1.\nLanded-tip: tip1\nStranded: tip1..head1\n" +
		"Kept: effort/c1 c..h\nKept: effort/c2 c..h\n" +
		"Child: c1 critique Nits — obsolete: x\nChild: c2 critique Nits — obsolete: x"
	f := newRespinFake(body)
	f.beads["c2"] = bd.Bead{ID: "c2", Kind: contract.KindCritique, Status: "closed", Title: "Tc1", Description: "body c2"}
	if _, err := Respin(f.seams(), "r"); err != nil {
		t.Fatal(err)
	}
	a, b := f.supersedes["c1"], f.supersedes["c2"]
	if a == "" || b == "" || a == b || len(f.members) != 2 {
		t.Fatalf("supersedes %v members %v", f.supersedes, f.members)
	}
	if want := [][2]string{{"c1", a}, {"c2", b}}; !slices.Equal(f.adopted, want) {
		t.Errorf("adopted %v want %v", f.adopted, want)
	}
}

func TestChildLineMarksADraftByItsStatus(t *testing.T) {
	k := bd.Bead{ID: "d", Kind: contract.KindBuild, Title: "T", Status: "deferred"}
	if l := ChildLine(k, "x"); !strings.HasSuffix(l, draftMark) {
		t.Errorf("draft unmarked: %q", l)
	}
	k.Status = "open"
	if l := ChildLine(k, "x"); strings.HasSuffix(l, draftMark) {
		t.Errorf("published child marked: %q", l)
	}
}

func TestRespinRerunCutsAtTheExistingReplayConflictsBase(t *testing.T) {
	f := newRespinFake(remnantBody)
	f.replay = git.Replay{Conflicts: []string{"a.txt"}}
	s := f.seams()
	// a first run filed the Conflict at main1, then crashed before the cut
	f.respin = "new1"
	f.members["m9"] = bd.Successor{Kind: contract.KindConflict}
	f.beads["m9"] = bd.Bead{ID: "m9", Kind: contract.KindConflict, Status: "open",
		Description: nami.ReplayConflictBody("r", "Cp", nami.ReplayRange{Mainline: "main1", Tip: "tip1", Head: "head1"})}
	s.Mainline = func() (string, error) { return "main2", nil }
	s.Replay = func(tip, m, head string) (git.Replay, error) {
		t.Errorf("replayed against %s although the Conflict is already filed", m)
		return git.Replay{Conflicts: []string{"a.txt"}}, nil
	}
	if _, err := Respin(s, "r"); err != nil {
		t.Fatal(err)
	}
	if got := f.cut["checkpoint/new1"]; got != "main1" {
		t.Errorf("cut at %q, want the Conflict's Base main1", got)
	}
}

func TestRespinCrashAfterSpawnLeavesADraftAndRerunPublishesItWithTheClose(t *testing.T) {
	f := newRespinFake(remnantBody)
	s := f.seams()
	boom := errors.New("boom")
	s.Cut = func(string, string) (bool, error) { return false, boom }
	if _, err := Respin(s, "r"); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if len(f.spawned) != 1 || f.spawned[0].DraftedBy != "r" {
		t.Fatalf("respin not a Draft of the Remnant: %+v", f.spawned)
	}
	if f.published || f.closed["r"] != "" {
		t.Fatalf("published %v closed %v after crash", f.published, f.closed)
	}
	res, err := Respin(f.seams(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.spawned) != 1 || res.Checkpoint != "new1" || !f.published || f.closed["r"] != "respun: new1" {
		t.Errorf("spawned %d res %+v published %v closed %v", len(f.spawned), res, f.published, f.closed)
	}
}

func TestRespinRefusedByRenumberHoldCreatesNoDraft(t *testing.T) {
	f := newRespinFake(remnantBody)
	s := f.seams()
	s.Renumbers = func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "rn", Title: "Renumber", Kind: contract.KindCheckpoint, Status: "open"}}, nil
	}
	if _, err := Respin(s, "r"); err == nil {
		t.Fatal("respin not refused")
	}
	if len(f.spawned) != 0 || f.published || f.closed["r"] != "" {
		t.Errorf("spawned %v published %v closed %v", f.spawned, f.published, f.closed)
	}
}
