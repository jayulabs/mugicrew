package tui

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// The drawer's drafted by line names the creator and its state over every D4 arm
// (Spec mc-pyamf D4), and a failed session read makes a HITL creator unknown, never gone.
func TestDraftDrawerNamesCreatorState(t *testing.T) {
	hitl := func(mut ...func(*bd.Bead)) bd.Bead {
		return dbead("cr", contract.KindSurvey, "in_progress", "", 2, mut...)
	}
	afk := func(status string, mut ...func(*bd.Bead)) bd.Bead {
		return dbead("cr", contract.KindBuild, status, "", 2, mut...)
	}
	elsewhere := func(b *bd.Bead) { b.Assignee = "someone-else-entirely" }
	paused := func(b *bd.Bead) { b.Labels = append(b.Labels, "paused:"+bd.PausedCheckup) }
	sess := func(s session.Info) map[string]session.Info { return map[string]session.Info{"cr": s} }
	for _, c := range []struct {
		name     string
		creator  *bd.Bead
		label    bool
		sessions map[string]session.Info
		live     map[string]bool
		sessErr  error
		launched bool
		want     string
		why      []string // substrings of draftWhy's sentence
	}{
		{"hitl alive", ptr(hitl()), true, sess(session.Info{State: session.Alive}), nil, nil, false, "· live", []string{"Drafts publish when t cr (cr) exits"}},
		{"hitl closing", ptr(hitl()), true, sess(session.Info{State: session.Alive, Closing: true}), nil, nil, false, "· publishing", []string{"Drafts publish when t cr (cr) exits"}},
		{"hitl exited", ptr(hitl()), true, sess(session.Info{State: session.Exited}), nil, nil, false, "· exited, resumable", []string{"resume", "drop"}},
		{"hitl absent", ptr(hitl()), true, nil, nil, nil, false, "· gone", []string{"recover"}},
		{"hitl closed absent", ptr(dbead("cr", contract.KindSurvey, "closed", "", 2)), true, nil, nil, nil, false, "· gone", []string{"recover"}},
		{"hitl read failed", ptr(hitl()), true, nil, nil, errors.New("tmux"), false, "· unknown (session table unreadable)", []string{"unreadable", "unknown"}},
		{"hitl fenced", ptr(hitl(paused)), true, nil, nil, nil, false, "· Checkup-fenced", []string{"Checkup"}},
		{"afk alive", ptr(afk("in_progress")), true, nil, map[string]bool{"cr": true}, nil, false, "· live", []string{"Drafts publish when t cr (cr) exits"}},
		{"afk dead", ptr(afk("in_progress")), true, nil, nil, nil, false, "· stranded", []string{"recover"}},
		{"afk released", ptr(afk("open")), true, nil, nil, nil, false, "· stranded (released)", []string{"recover"}},
		{"starting", ptr(afk("open")), true, nil, nil, nil, true, "· starting", []string{"Drafts publish when t cr (cr) exits"}},
		{"claimed elsewhere", ptr(hitl(elsewhere)), true, nil, nil, nil, false, "· claimed by someone-else-entirely", []string{"someone-else-entirely"}},
		{"creator missing", nil, true, nil, nil, nil, false, "cr · gone", []string{"recover"}},
		{"no label", nil, false, nil, nil, nil, false, "no creator", []string{"Drafts publish when their creator exits"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mut []func(*bd.Bead)
			if c.label {
				mut = append(mut, labeled(contract.DraftedBy("cr")))
			}
			beads := []bd.Bead{dbead("dr", contract.KindBuild, "deferred", "", 2, mut...)}
			if c.creator != nil {
				beads = append(beads, *c.creator)
			}
			g := graphData{beads: beads, sessions: c.sessions, live: c.live, sessErr: c.sessErr, w: 100, h: 40}
			if c.launched {
				g.launched = map[string]time.Time{"cr": time.Now()}
			}
			got := ansi.Strip(graphTab{graphData: g, sel: "dr"}.detailBody())
			line := ""
			for _, l := range strings.Split(got, "\n") {
				if strings.HasPrefix(l, "drafted by") {
					line = l
				}
			}
			if line == "" || !strings.Contains(line, c.want) {
				t.Errorf("drafted by line = %q, want it to contain %q\n%s", line, c.want, got)
			}
			var draft *bd.Bead
			for i := range beads {
				if beads[i].ID == "dr" {
					draft = &beads[i]
				}
			}
			why := g.draftWhy(draft)
			for _, w := range c.why {
				if !strings.Contains(why, w) {
					t.Errorf("draftWhy = %q, want it to contain %q", why, w)
				}
			}
		})
	}
}

// Only a Draft's drawer carries the line.
func TestNonDraftDrawerHasNoDraftedBy(t *testing.T) {
	g := graphData{beads: []bd.Bead{dbead("b", contract.KindBuild, "open", "", 2, labeled(contract.DraftedBy("x")))}, w: 100, h: 40}
	if got := ansi.Strip(graphTab{graphData: g, sel: "b"}.detailBody()); strings.Contains(got, "drafted by") {
		t.Errorf("open bead shows drafted by:\n%s", got)
	}
}

func ptr[T any](v T) *T { return &v }

// The live loader carries readSessions' error into the snapshot (D4a), and a frame with a
// Draft row's drawer open runs no subprocess (ADR 0101 §4).
func TestDraftDrawerFromLoadRunsNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m := graphModel(t, 120, 40)
	bs := append(stubBeads(),
		dbead("crt", contract.KindSurvey, "in_progress", "", 2),
		dbead("drf", contract.KindBuild, "deferred", "", 2, labeled(contract.DraftedBy("crt"))))
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}, sessErr: errors.New("tmux down")})
	m = selectBead(t, pick(t, m, "s", "draft"), "drf")
	m = press(t, m, runeKey("d"))
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "drafted by") || !strings.Contains(v, "unknown (session table unreadable)") {
		t.Errorf("drawer lacks the unknown creator line:\n%s", v)
	}
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("Draft drawer render started subprocesses:\n%s", got)
	}
}

// draftRefusalModel is a Graph with a Draft under creator "cr" (live HITL session, or a
// stranded AFK effort) and a published child under the Draft.
func draftRefusalModel(t *testing.T, stranded bool) Model {
	t.Helper()
	return selectBead(t, pick(t, draftGraphModel(t, stranded), "s", "draft"), "drf")
}

// draftGraphModel is draftRefusalModel's Graph at the default view, nothing selected.
func draftGraphModel(t *testing.T, stranded bool) Model {
	t.Helper()
	m := graphModel(t, 120, 40)
	creator := dbead("cr", contract.KindSurvey, "in_progress", "", 2)
	sess := map[string]session.Info{"cr": {State: session.Alive}}
	if stranded {
		creator = dbead("cr", contract.KindBuild, "in_progress", "", 2)
		sess = map[string]session.Info{}
	}
	bs := append(stubBeads(), creator,
		dbead("drf", contract.KindChart, "deferred", "", 2, labeled(contract.DraftedBy("cr"))),
		dbead("kid", contract.KindSignal, "open", "drf", 2))
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	return m
}

// Every mutating key on a Draft row is refused with draftWhy on the status line, before a
// dialog or a write; d, t, R, y and g still work (Spec mc-pyamf D6).
func TestDraftRowRefusesMutatingKeys(t *testing.T) {
	for _, stranded := range []bool{false, true} {
		m := draftRefusalModel(t, stranded)
		want := m.gateData().draftWhy(m.selection())
		if want == "" {
			t.Fatalf("stranded=%v: no draftWhy on the Draft row", stranded)
		}
		if !stranded && !strings.Contains(want, "Drafts publish when") {
			t.Errorf("live creator wording = %q", want)
		}
		for _, k := range draftRefused {
			kk := runeKey(k)
			if k == "enter" {
				kk = enterKey()
			}
			next, cmd := send(t, m, kk)
			if cmd != nil || len(next.modals) != 0 {
				t.Errorf("stranded=%v %q: cmd=%v modals=%d, want a bare refusal", stranded, k, cmd != nil, len(next.modals))
			}
			if next.status != want {
				t.Errorf("stranded=%v %q: status %q, want %q", stranded, k, next.status, want)
			}
		}
		for _, k := range []string{"d", "t", "R", "y", "g"} {
			next, cmd := send(t, m, runeKey(k))
			if next.status == want {
				t.Errorf("stranded=%v %q was refused", stranded, k)
			}
			if cmd == nil && next.View() == m.View() && len(next.modals) == 0 && k != "y" {
				t.Errorf("stranded=%v %q did nothing", stranded, k)
			}
		}
	}
}

// A published child under a Draft is not a Draft: its keys still act, and a Draft's
// hints drop every refused verb while ? lists them under "this tab".
func TestDraftRowHintsAndChildKeys(t *testing.T) {
	m := draftRefusalModel(t, false)
	g := m.tabAt(tabGraph).(graphTab)
	for _, b := range g.bindings() {
		for _, k := range b.Keys() {
			if slices.Contains(draftRefused, k) {
				t.Errorf("Draft row hints refused key %q", k)
			}
		}
	}
	extra := map[string]bool{}
	for _, b := range g.extra() {
		for _, k := range b.Keys() {
			extra[k] = true
		}
	}
	for _, k := range draftRefused {
		if !extra[k] {
			t.Errorf("refused key %q not listed in extra()", k)
		}
	}
}

// n on a Draft Chart offers no rebase request.
func TestDraftChartNewMenuHasNoRebase(t *testing.T) {
	if canRebase(ptr(dbead("c", contract.KindChart, "deferred", "", 2))) {
		t.Errorf("canRebase accepts a Draft Chart")
	}
	if !canRebase(ptr(dbead("c", contract.KindChart, "open", "", 2))) {
		t.Errorf("canRebase refuses an open Chart")
	}
}

// The recover arm promises "x recovers it" only where xLabel offers recover; elsewhere it
// names `mugicrew recover <id>` so the refusal never points at a key that does nothing.
func TestDraftWhyRecoverWordingMatchesX(t *testing.T) {
	hitl := func(status string) bd.Bead { return dbead("cr", contract.KindSurvey, status, "", 2) }
	afk := func(status string) bd.Bead { return dbead("cr", contract.KindBuild, status, "", 2) }
	for _, c := range []struct {
		name    string
		creator *bd.Bead
		live    map[string]bool
		xRow    bool
	}{
		{"afk dead: x recovers", ptr(afk("in_progress")), nil, true},
		{"hitl in_progress gone: x recovers", ptr(hitl("in_progress")), nil, true},
		{"afk released", ptr(afk("open")), nil, false},
		{"hitl closed gone", ptr(hitl("closed")), nil, false},
		{"creator missing", nil, nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			beads := []bd.Bead{dbead("dr", contract.KindBuild, "deferred", "", 2, labeled(contract.DraftedBy("cr")))}
			if c.creator != nil {
				beads = append(beads, *c.creator)
			}
			g := graphData{beads: beads, live: c.live, w: 100, h: 40}
			got := g.draftWhy(g.beadByID("dr"))
			if row := strings.Contains(got, "x recovers it on its row"); row != c.xRow {
				t.Errorf("draftWhy = %q, x-on-row wording = %v, want %v", got, row, c.xRow)
			}
			if !c.xRow && !strings.Contains(got, "mugicrew recover cr") {
				t.Errorf("draftWhy = %q, want it to name `mugicrew recover cr`", got)
			}
		})
	}
}

// A published child under a Draft is not a Draft: with the Graph showing it, the refused-set
// keys reach their normal handlers and none puts the Draft refusal on the status line.
func TestDraftChildKeysStillAct(t *testing.T) {
	for _, stranded := range []bool{false, true} {
		m := selectBead(t, draftGraphModel(t, stranded), "kid")
		gd := m.gateData()
		if gd.draftWhy(gd.beadByID("kid")) != "" {
			t.Fatalf("stranded=%v: a published child inherited the Draft refusal", stranded)
		}
		refusal := gd.draftWhy(gd.beadByID("drf"))
		acted := 0
		for _, k := range draftRefused {
			kk := runeKey(k)
			if k == "enter" {
				kk = enterKey()
			}
			next, cmd := send(t, m, kk)
			if next.status == refusal {
				t.Errorf("stranded=%v %q on the child was refused as a Draft", stranded, k)
			}
			if cmd != nil || len(next.modals) != 0 || next.View() != m.View() {
				acted++
			}
		}
		if acted == 0 {
			t.Errorf("stranded=%v: no refused-set key acted on the published child", stranded)
		}
	}
}

// handled == bound with a Draft row selected on the Graph, live or stranded creator, and on
// its published child (Spec mc-pyamf Testing Decisions).
func TestBindingCoverageDraftRow(t *testing.T) {
	for _, stranded := range []bool{false, true} {
		for _, id := range []string{"drf", "kid"} {
			m := selectBead(t, draftGraphModel(t, stranded), id)
			checkCoverage(t, fmt.Sprintf("Graph %s stranded=%v", id, stranded), m, true, tabDefs[tabGraph].key, "J", "K", "ctrl+down", "ctrl+up")
		}
	}
}

// A Draft sorts among its siblings where an open bead of the same priority and age would.
func TestDraftSortsLikeOpenSibling(t *testing.T) {
	order := func(status string) []string {
		beads := []bd.Bead{
			dbead("c", contract.KindChart, "open", "", 2),
			dbead("newer", contract.KindBuild, "open", "c", 2, newer(2*time.Hour)),
			dbead("mid", contract.KindBuild, status, "c", 2, newer(time.Hour)),
			dbead("older", contract.KindBuild, "open", "c", 2),
			dbead("urgent", contract.KindBuild, "open", "c", 1),
		}
		m := liveModel(t, 120, beads, nil, nil)
		var ids []string
		for _, r := range navRows(gr(m).groups, nil) {
			if r.beadID != "c" {
				ids = append(ids, r.beadID)
			}
		}
		return ids
	}
	if got, want := order("deferred"), order("open"); !slices.Equal(got, want) {
		t.Errorf("Draft sibling order %v, want an open bead's %v", got, want)
	}
}

// A folded header's "N live" trailer counts live sessions only; a Draft under it joins the
// closed/total and nothing else (Spec FR9, FR10).
func TestFoldedHeaderLiveUnchangedByDraft(t *testing.T) {
	trailer := func(withDraft bool) string {
		beads := []bd.Bead{
			dbead("c", contract.KindChart, "open", "", 2),
			dbead("b", contract.KindBuild, "open", "c", 2),
		}
		if withDraft {
			beads = append(beads, dbead("d", contract.KindBuild, "deferred", "c", 2, labeled(contract.DraftedBy("b"))))
		}
		m := liveModel(t, 120, beads, nil, map[string]session.Info{"b": {State: session.Alive}})
		m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
		return ansi.Strip(m.View())
	}
	if v := trailer(false); !strings.Contains(v, "1 live 0/1") {
		t.Fatalf("baseline trailer:\n%s", v)
	}
	if v := trailer(true); !strings.Contains(v, "1 live 0/2") {
		t.Errorf("with a Draft the trailer should be 1 live 0/2:\n%s", v)
	}
}

// A missing creator reads without a doubled id, and a closed AFK creator as gone like a
// closed HITL one.
func TestDraftWhyWording(t *testing.T) {
	g := graphData{beads: []bd.Bead{
		{ID: "d1", Status: "deferred", Labels: []string{"drafted-by:cr"}},
		{ID: "ac", Title: "afk", Status: "closed", Kind: contract.KindBuild},
		{ID: "d2", Status: "deferred", Labels: []string{"drafted-by:ac"}},
	}}
	if got := g.draftWhy(&g.beads[0]); strings.Contains(got, "cr (cr)") {
		t.Fatalf("doubled id: %q", got)
	}
	if _, s := g.creatorStateOf(&g.beads[2]); s != creatorGone {
		t.Fatalf("closed AFK creator state = %v; want gone", s)
	}
}

// A Draft whose creator is a Remnant is its respin Checkpoint: the row and the drawer name the
// respin re-run, never recover, which would delete it.
func TestDraftWhyRemnantNamesRespinRerun(t *testing.T) {
	for _, status := range []string{"open", "in_progress"} {
		rem := dbead("rem", contract.KindRemnant, status, "", 2)
		beads := []bd.Bead{dbead("dr", contract.KindCheckpoint, "deferred", "", 2, labeled(contract.DraftedBy("rem"))), rem}
		g := graphData{beads: beads, w: 100, h: 40}
		why := g.draftWhy(g.beadByID("dr"))
		if !strings.Contains(why, "re-run the respin of") || strings.Contains(why, "recover") {
			t.Errorf("%s: draftWhy = %q, want it to name the respin re-run and not recover", status, why)
		}
		if line := g.draftedByLine(g.beadByID("dr")); !strings.Contains(line, "re-run its respin") {
			t.Errorf("%s: drafted by = %q, want the respin re-run", status, line)
		}
	}
}

// A closed Remnant's Draft is no respin to re-run (respin refuses a closed Remnant):
// its row names recover, which deletes it.
func TestDraftWhyClosedRemnantDoesNotNameRespinRerun(t *testing.T) {
	rem := dbead("rem", contract.KindRemnant, "closed", "", 2)
	beads := []bd.Bead{dbead("dr", contract.KindCheckpoint, "deferred", "", 2, labeled(contract.DraftedBy("rem"))), rem}
	g := graphData{beads: beads, w: 100, h: 40}
	if why := g.draftWhy(g.beadByID("dr")); strings.Contains(why, "respin") || !strings.Contains(why, "recover") {
		t.Errorf("draftWhy = %q, want recover and no respin", why)
	}
	if line := g.draftedByLine(g.beadByID("dr")); strings.Contains(line, "respin") {
		t.Errorf("drafted by = %q, want no respin", line)
	}
}
