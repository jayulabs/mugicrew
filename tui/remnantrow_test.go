package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/remnant"
	"jayulabs.org/mugicrew/session"
)

const remnantBody = "Remnant of cp, which a Forge merge landed at abc.\nLanded-tip: t1\nStranded: t1..h1\n" +
	"Kept: effort/k1 c1..h1\nKept: effort/k2 c2..h2\n" +
	"Child: k1 build Do — obsolete: container landed; carried by rm\nChild: k2 build Two — x\nChild: k3 critique Three — y"

func remnantFixture(mut ...func(*bd.Bead)) []bd.Bead {
	rm := dbead("rm", contract.KindRemnant, "open", "", 1, func(b *bd.Bead) {
		b.Title, b.Related, b.Description = "Remnant of Landed", []string{"cp"}, remnantBody
	})
	for _, f := range mut {
		f(&rm)
	}
	return []bd.Bead{
		dbead("i1", contract.KindInitiative, "open", "", 1),
		dbead("cp", contract.KindCheckpoint, "closed", "i1", 1, isContainer, func(b *bd.Bead) { b.Title = "Landed" }),
		dbead("cn", contract.KindCheckpoint, "closed", "", 2, isContainer, func(b *bd.Bead) { b.Title = "Loose" }),
		rm,
	}
}

func TestRemnantSideRowsFollowTheLandedCheckpointsInitiative(t *testing.T) {
	for name, tc := range map[string]struct {
		beads []bd.Bead
		want  string
	}{
		"initiative": {remnantFixture(), "# Actionable (1),A rm,# Live (0),.,# Specs (0),.,# Checkpoints (0),.,# Initiatives (1),# t i1,T rm,# No Initiative (0),."},
		"none": {remnantFixture(func(b *bd.Bead) { b.Related = []string{"cn"} }),
			"# Actionable (1),A rm,# Live (0),.,# Specs (0),.,# Checkpoints (0),.,# Initiatives (0),.,# No Initiative (1),T rm"},
	} {
		if got := strings.Join(eshape(execList(tc.beads, nil)), ","); got != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", name, got, tc.want)
		}
	}
}

func TestRemnantRowHintSummaryAndDetail(t *testing.T) {
	g := graphData{beads: remnantFixture(), strandCommits: map[string]int{"rm": 3}, w: 120, h: 40}
	for _, r := range oldExecList(g, nil) {
		if r.b == nil || r.b.ID != "rm" {
			continue
		}
		tab := execTab{g: g}
		left, right := tab.body(r, 0, false, "")
		if !strings.Contains(ansi.Strip(left), "stranded from → Landed") {
			t.Errorf("%d: hint missing: %q", r.kind, ansi.Strip(left))
		}
		if !strings.Contains(ansi.Strip(right), "3 commits · 2 kept · 3 children") {
			t.Errorf("%d: summary = %q", r.kind, ansi.Strip(right))
		}
	}
	d := ansi.Strip(execTab{g: g, w: 120, h: 40}.rowText(eRow{kind: eAct, b: &g.beads[3], cp: &g.beads[1], rel: remnantRel}))
	for _, want := range []string{"stranded    t1..h1", "kept        effort/k1 c1..h1", "kept        effort/k2 c2..h2", "child       k1 build Do — obsolete: container landed; carried by rm", "child       k3 critique Three — y"} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
}

func TestRemnantCommitCountOmittedWhenRangeUnreadable(t *testing.T) {
	g := graphData{beads: remnantFixture()}
	if s := g.strandSummary(&g.beads[3]); s != "2 kept · 3 children" {
		t.Errorf("summary = %q", s)
	}
	g = graphData{beads: remnantFixture(func(b *bd.Bead) { b.Description = "Landed-tip: t1" })}
	if s := g.strandSummary(&g.beads[3]); s != "0 commits · 0 kept · 0 children" {
		t.Errorf("summary = %q", s)
	}
}

func heldFixture() []bd.Bead {
	bs := remnantFixture(func(b *bd.Bead) { b.BlockedBy = []string{"r1", "r2", "pv"} })
	return append(bs,
		dbead("r1", contract.KindBuild, "in_progress", "cp", 1), dbead("r2", contract.KindCritique, "in_progress", "cp", 1),
		dbead("pv", contract.KindPivot, "open", "", 1, func(b *bd.Bead) { b.Title = "Spec off" }))
}

func TestHeldRemnantIsBlockedAndNamesItsHolds(t *testing.T) {
	g := graphData{beads: heldFixture()}
	rows := oldExecList(g, nil)
	var side *eRow
	for i, r := range rows {
		if r.kind == eAct && r.b.ID == "rm" {
			t.Errorf("a held Remnant is not Actionable")
		}
		if r.kind == eSide && r.b.ID == "rm" {
			side = &rows[i]
		}
	}
	if side == nil {
		t.Fatalf("no tree row: %v", eshape(rows))
	}
	left, _ := execTab{g: g}.body(*side, 0, false, "")
	if !strings.Contains(ansi.Strip(left), "held by 2 running · pivot Spec off") {
		t.Errorf("tree row = %q", ansi.Strip(left))
	}
	if why := g.rowEnterWhy(&g.beads[3], true); !strings.Contains(why, "blocked by") {
		t.Errorf("enter: %q", why)
	}
	if d := ansi.Strip(execTab{g: g, w: 120, h: 40}.rowText(*side)); !strings.Contains(d, "blocked by") {
		t.Errorf("detail lacks blocked by:\n%s", d)
	}
}

func TestRemnantTakeableRowIsBandedAndEnterLaunches(t *testing.T) {
	g := graphData{beads: remnantFixture(), sessions: map[string]session.Info{}}
	if why := g.rowEnterWhy(&g.beads[3], true); why != "" {
		t.Errorf("enter refuses: %q", why)
	}
	if a := g.actionable(); len(a) != 1 || a[0].b.ID != "rm" || a[0].rel != remnantRel {
		t.Errorf("actionable = %v", eshape(a))
	}
}

func stubRemnantDrop(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	orig := runRemnantDrop
	t.Cleanup(func() { runRemnantDrop = orig })
	runRemnantDrop = func(id string) (remnant.Result, error) { calls = append(calls, id); return remnant.Result{}, err }
	stubBranches(t, "checkpoint/cp", "effort/k1", "effort/k2")
	return &calls
}

// stubBranches makes exactly refs exist for the drop dialog's listing.
func stubBranches(t *testing.T, refs ...string) {
	t.Helper()
	orig := remnantSeams
	t.Cleanup(func() { remnantSeams = orig })
	remnantSeams.HasBranch = func(ref string) (bool, error) { return slices.Contains(refs, ref), nil }
}

func TestDropDialogOmitsARefAlreadyGone(t *testing.T) {
	stubRemnantDrop(t, nil)
	stubBranches(t, "effort/k1")
	m, _ := execModel(t, remnantFixture())
	v := view(press(t, execAt(t, m, "act:rm"), runeKey("X")))
	if strings.Contains(v, "checkpoint/cp") || !strings.Contains(v, "effort/k1") {
		t.Errorf("dialog should list only the live ref:\n%s", v)
	}
}

func TestXOnRemnantOpensDropDialogAndYDrops(t *testing.T) {
	calls := stubRemnantDrop(t, nil)
	m, _ := execModel(t, remnantFixture())
	m = press(t, execAt(t, m, "act:rm"), runeKey("X"))
	v := view(m)
	for _, want := range []string{"Drop Remnant of Landed?", "checkpoint/cp", "effort/k1", "effort/k2", "re-files nothing"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog lacks %q:\n%s", want, v)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("dropped before y: %v", *calls)
	}
	_ = press(t, m, runeKey("y"))
	if len(*calls) != 1 || (*calls)[0] != "rm" {
		t.Errorf("drop calls = %v", *calls)
	}
}

func TestXOnRemnantRefusedWhileBlockedOrLive(t *testing.T) {
	calls := stubRemnantDrop(t, errors.New("unused"))
	m, _ := execModel(t, heldFixture())
	m = press(t, execAt(t, m, "side:rm"), runeKey("X"))
	if len(m.modals) != 0 || !strings.Contains(m.status, "blocked by") {
		t.Errorf("blocked: modals=%d status=%q", len(m.modals), m.status)
	}
	live := remnantFixture()
	m, _ = execModel(t, live)
	m, _ = send(t, m, loadedMsg{beads: live, live: map[string]bool{}, sessions: map[string]session.Info{"rm": {State: session.Alive}}})
	m = press(t, execAt(t, m, "live:rm"), runeKey("X"))
	if len(m.modals) != 0 || !strings.Contains(m.status, "live session") {
		t.Errorf("live: modals=%d status=%q", len(m.modals), m.status)
	}
	if len(*calls) != 0 {
		t.Errorf("drop ran: %v", *calls)
	}
}

func TestRespinHoldRendersAsAmberSuffixWithoutBlockingTheRow(t *testing.T) {
	rn := func(status string) bd.Bead {
		return dbead("rn", contract.KindCheckpoint, status, "i1", 2, isContainer, func(b *bd.Bead) { b.Title, b.CausedBy = "Renumber 0405-a.md from cp", "cp" })
	}
	const want = "respin waits on Renumber Renumber 0405-a.md from cp"
	g := graphData{beads: append(remnantFixture(), rn("open")), sessions: map[string]session.Info{}, w: 120, h: 40}
	if why := g.rowEnterWhy(&g.beads[3], true); why != "" {
		t.Errorf("hold blocks enter: %q", why)
	}
	seen := map[int]bool{}
	for _, r := range oldExecList(g, nil) {
		if r.b == nil || r.b.ID != "rm" {
			continue
		}
		seen[r.kind] = true
		left, _ := execTab{g: g}.body(r, 0, false, "")
		if !strings.Contains(ansi.Strip(left), want) {
			t.Errorf("%d: suffix missing: %q", r.kind, ansi.Strip(left))
		}
	}
	if !seen[eAct] || !seen[eSide] {
		t.Errorf("rows seen %v", seen)
	}
	if d := ansi.Strip(execTab{g: g, w: 120, h: 40}.rowText(eRow{kind: eAct, b: &g.beads[3], cp: &g.beads[1], rel: remnantRel})); !strings.Contains(d, want) {
		t.Errorf("detail lacks suffix:\n%s", d)
	}
	g = graphData{beads: append(remnantFixture(), rn("closed")), w: 120, h: 40}
	for _, r := range oldExecList(g, nil) {
		if r.b != nil && r.b.ID == "rm" {
			if left, _ := (execTab{g: g}).body(r, 0, false, ""); strings.Contains(ansi.Strip(left), "respin waits") {
				t.Errorf("closed Renumber still holds: %q", ansi.Strip(left))
			}
		}
	}
}

func TestRemnantDetailErrandFieldShowsTheWholeValue(t *testing.T) {
	g := graphData{beads: remnantFixture(func(b *bd.Bead) {
		b.Description += "\nErrand: e1 Hold (a before-land hold: an exit carries or closes it)"
	}), w: 120, h: 40}
	d := ansi.Strip(execTab{g: g, w: 120, h: 40}.rowText(eRow{kind: eAct, b: &g.beads[3], cp: &g.beads[1], rel: remnantRel}))
	if want := "errand      e1 Hold (a before-land hold: an exit carries or closes it)"; !strings.Contains(d, want) {
		t.Errorf("detail lacks %q:\n%s", want, d)
	}
}
