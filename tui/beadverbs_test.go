package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// verbBeads is actionBeads plus the rows that exercise the bead-verb gates: an open
// Checkup, a Landing recording an MR link, a Chart and a Checkpoint (no link of their
// own), a closed bead with an open blocker and a bead a pending write holds.
func verbBeads() []bd.Bead {
	return append(actionBeads(),
		bd.Bead{ID: "ck", Kind: contract.KindCheckup, Status: "open", Title: "Repair it", Created: epoch},
		bd.Bead{ID: "land", Kind: contract.KindLanding, Status: "open", Title: "Landing", Parent: "cp", Description: "MR link: " + mrURL, Created: epoch},
		bd.Bead{ID: "cha", Kind: contract.KindChart, Container: true, Status: "open", Title: "A chart", Created: epoch},
		bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "A checkpoint", Created: epoch},
		bd.Bead{ID: "cblk", Kind: contract.KindSignal, Status: "closed", Title: "Closed, blocked", BlockedBy: []string{"dep"}, Created: epoch},
		bd.Bead{ID: "pnd", Kind: contract.KindSignal, Status: "open", Title: "Write pending", Created: epoch},
		bd.Bead{ID: "full", Kind: contract.KindSignal, Status: "open", Title: "Every verb", BlockedBy: []string{"dep"}, Description: "MR link: " + mrURL, Created: epoch},
		bd.Bead{ID: "kid", Kind: contract.KindSignal, Status: "open", Title: "In an Initiative", Parent: "ini", Created: epoch},
	)
}

// verbModel is Graph over verbBeads with every status shown and the Unparented group
// open; calls logs the seams the verbs reach.
func verbModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	t.Setenv("BROWSER", "x")
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: verbBeads(), live: map[string]bool{"live": true}, sessions: verbSessions()})
	m, _ = send(t, m, runeKey("4"))
	m = pick(t, m, "s", "any")
	m.pending = map[string]string{"pnd": "updating", "kid": "updating"}
	return m, calls
}

func verbSessions() map[string]session.Info {
	ss := actionSessions()
	return ss
}

// onRow puts Graph's cursor on row sel without moving through the rows.
func onRow(m Model, sel string) Model {
	m.focus = focus{} // Graph is the reference for the focus view too
	m.tabs = slices.Clone(m.tabs)
	g := m.tabs[tabGraph].(graphTab)
	g.sel = sel
	m.tabs[tabGraph] = g
	return m
}

// verbKeys are every key a bead verb binds, one press per key.
func verbKeys() []string {
	var ks []string
	for _, v := range beadVerbs {
		ks = append(ks, v.Keys()...)
	}
	return ks
}

// verbSet is the bead verbs among bs, by first key.
func verbSet(bs []key.Binding) []string {
	var out []string
	for _, b := range bs {
		for _, v := range beadVerbs {
			if len(b.Keys()) > 0 && b.Keys()[0] == v.Keys()[0] {
				out = append(out, b.Keys()[0])
			}
		}
	}
	slices.Sort(out)
	return out
}

// outcome is what a key press shows: the handled bit, the status line, the top modal's
// title and the seam calls it made once its command ran.
type outcome struct {
	handled     bool
	status, top string
	writes      []string
}

func press1(t *testing.T, m Model, calls *[]string, k string) outcome {
	t.Helper()
	*calls = nil
	m.status = ""
	next, cmd := send(t, m, runeKey(k))
	o := outcome{handled: cmd != nil || next.View() != m.View()}
	next = settle(t, next, cmd)
	o.status = next.status
	if n := len(next.modals); n > 0 {
		o.top = next.modals[n-1].title
	}
	o.writes = slices.Clone(*calls)
	return o
}

// ideaParityModel is Ideate over a fixture that meets every bead row it draws: Signals
// (open, claimed, blocked, paused, in an Initiative and loose), their act and live twins,
// an Initiative header, a drained header, a cut session's header, the combined row and
// the drained cue row. Graph shows every status, so it can stand as the reference.
func ideaParityModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	t.Setenv("BROWSER", "x")
	blk := sig("blk", "open", 2, "")
	blk.BlockedBy = []string{"dep"}
	pz := sig("pz", "open", 2, "")
	pz.Labels = []string{"paused:manual"}
	linked := sig("lnk", "open", 2, "")
	linked.Description = "MR link: " + mrURL
	beads := slices.Concat(drainedBeads(), mixed(), []bd.Bead{blk, pz, linked, {ID: "dep", Kind: contract.KindBuild, Status: "open", Title: "dep", Created: epoch}})
	sess := map[string]session.Info{"mc-long-22": {State: session.Alive}, "ua": {State: session.Alive}}
	m := sized(t, 160, 50)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = treesOpen(m)
	m, _ = send(t, m, runeKey("4"))
	m = pick(t, m, "s", "any")
	m, _ = send(t, m, runeKey(tabDefs[tabIdeate].key))
	return m, calls
}

// ideaGo puts Ideate's cursor on row c.
func ideaGo(m Model, c string) Model {
	m.tabs = slices.Clone(m.tabs)
	t := m.tabs[tabIdeate].(ideaTab)
	t.sel = c
	m.tabs[tabIdeate] = t
	return m
}

// defogVerbModel is Defog's action fixture plus the rows the bead verbs hinge on: an MR
// Watch with no link yet and one with a link (the Merge Requests section), a closed Chart
// finished within 24h with an open blocker (U ?-only) and an open Chart in the Charts
// section. Graph shows every status, so the parity reference finds each bead.
func defogVerbModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	t.Setenv("BROWSER", "x")
	beads, sess := defogFixture()
	beads = append(beads, dbead("afkb", contract.KindResearch, "open", "c", 1, blockedBy("afk")),
		dbead("mrw", contract.KindMRWatch, "open", "dc", 1),
		dbead("mrl", contract.KindMRWatch, "open", "pc", 1, func(b *bd.Bead) { b.Description = "MR link: " + mrURL }),
		dbead("fin", contract.KindChart, "closed", "ini", 5, blockedBy("afk"), func(b *bd.Bead) { b.Closed = clock().Add(-time.Hour) }),
		dbead("cla", contract.KindGrilling, "closed", "c", 1))
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	seam(t, &readLandStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m.landName = git.LandStrategyGitlab
	m = treesOpen(m)
	d := m.tabs[tabDefog].(defogTab)
	d.flip[finKey] = true // Finished in last 24h is folded by default
	m.tabs[tabDefog] = d
	m = pick(t, press(t, m, runeKey("4")), "s", "any")
	return press(t, m, runeKey("2")), calls
}

// surface is a bead-row surface the parity harness walks: how to build its Model, and
// the role it gives each of its rows.
type surface struct {
	name  string
	model func(t *testing.T) (Model, *[]string)
	cur   func(m Model) []string // the cursors rows() offers, one per row
	go_   func(m Model, c string) Model
	role  func(m Model, c string) rowRole
}

// surfaces are the bead-row surfaces on the router. Each surface Build adds its entry.
var surfaces = []surface{{
	name:  "Graph",
	model: verbModel,
	cur: func(m Model) []string {
		var out []string
		for _, r := range m.tabAt(tabGraph).(graphTab).rows() {
			out = append(out, r.sel)
		}
		return out
	},
	go_:  onRow,
	role: func(Model, string) rowRole { return roleFull },
}, {
	name:  "Initiatives",
	model: initsVerbModel,
	cur: func(m Model) []string {
		var out []string
		for _, r := range m.tabAt(tabInits).(initsTab).rows() {
			out = append(out, r.key)
		}
		return out
	},
	go_: func(m Model, c string) Model {
		m.tabs = slices.Clone(m.tabs)
		t := m.tabs[tabInits].(initsTab)
		t.sel = c
		m.tabs[tabInits] = t
		return m
	},
	role: func(Model, string) rowRole { return roleFull },
}, {
	name:  "Ideate",
	model: ideaParityModel,
	cur: func(m Model) []string {
		var out []string
		for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
			if r.selectable() {
				out = append(out, r.key)
			}
		}
		return out
	},
	go_:  ideaGo,
	role: func(Model, string) rowRole { return roleFull },
}, {
	name:  "Defog",
	model: defogVerbModel,
	cur: func(m Model) []string {
		var out []string
		for _, r := range m.tabAt(tabDefog).(defogTab).rows() {
			out = append(out, r.key)
		}
		return out
	},
	go_: func(m Model, c string) Model {
		m.tabs = slices.Clone(m.tabs)
		d := m.tabs[tabDefog].(defogTab)
		d.sel = c
		m.tabs[tabDefog] = d
		return m
	},
	role: func(m Model, c string) rowRole {
		for _, r := range m.tabAt(tabDefog).(defogTab).rows() {
			if r.key == c {
				return r.role()
			}
		}
		return roleFull
	},
}, {
	name:  "Execute",
	model: execVerbModel,
	cur: func(m Model) []string {
		var out []string
		for _, r := range m.tabAt(tabExecute).(execTab).rows() {
			out = append(out, r.key)
		}
		return out
	},
	go_: func(m Model, c string) Model {
		m.tabs = slices.Clone(m.tabs)
		e := m.tabs[tabExecute].(execTab)
		e.sel = c
		m.tabs[tabExecute] = e
		return m
	},
	role: func(m Model, c string) rowRole {
		for _, r := range m.tabAt(tabExecute).(execTab).rows() {
			if r.key == c {
				return r.role()
			}
		}
		return roleFull
	},
}}

// execVerbModel is Execute over its fixture plus a Landing recording an MR link under the
// open Checkpoint, an MR Watch, and a Checkpoint landed within the day; every fold open, so
// the walk meets the Initiative sections, Checkpoints, Specs, Landed, closed members and MR rows.
func execVerbModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	cp, ln := contract.KindCheckpoint, contract.KindLanding
	beads := append(execFixture(),
		dbead("lnk", ln, "open", "cpA", 5, described("MR link: "+mrURL)),
		dbead("mw", contract.KindMRWatch, "open", "cpA", 6, described("MR: !7\nMR link: "+mrURL)),
		dbead("lcp", cp, "closed", "", 1, isContainer),
		dbead("lcpL", ln, "closed", "lcp", 1, func(b *bd.Bead) { b.Closed, b.CloseReason = time.Now().Add(-time.Hour), "landed: aaaaaaaabbbbbbbb" }),
	)
	stubLaunch(t, nil)
	t.Setenv("BROWSER", "x")
	m, calls := execModel(t, beads)
	m.landName = git.LandStrategyGitlab
	m.tabs = slices.Clone(m.tabs)
	e := m.tabs[tabExecute].(execTab)
	e.flip = maps.Clone(e.flip)
	e.flip[landedKey] = true
	m.tabs[tabExecute] = e
	m = unfolded(t, m)
	return m, calls
}

// initsVerbModel is the Initiatives list over verbBeads plus a closed Initiative, both
// sections open.
func initsVerbModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	m, calls := verbModel(t)
	m, _ = send(t, m, loadedMsg{beads: append(verbBeads(), bd.Bead{ID: "cini", Kind: contract.KindInitiative, Container: true, Status: "closed", Title: "Closed initiative", Created: epoch}), live: map[string]bool{"live": true}, sessions: verbSessions()})
	m, _ = send(t, m, runeKey("5"))
	m.tabs = slices.Clone(m.tabs)
	t5 := m.tabs[tabInits].(initsTab)
	t5.flip = map[string]bool{initsClosed: true}
	m.tabs[tabInits] = t5
	m.pending = map[string]string{"pnd": "updating", "kid": "updating"}
	return m, calls
}

// The parity fixture holds an open and a closed Initiative, and enter still opens the
// focus view from either.
func TestInitsRowsKeepEnter(t *testing.T) {
	m, _ := initsVerbModel(t)
	for _, id := range []string{"ini", "cini"} {
		t.Run(id, func(t *testing.T) {
			t.Helper()
			r := m
			r.tabs = slices.Clone(r.tabs)
			it := r.tabs[tabInits].(initsTab)
			it.sel = id
			if it.selected() == nil {
				t.Fatalf("no %s row in the fixture", id)
			}
			r.tabs[tabInits] = it
			if next, _ := send(t, r, enterKey()); next.focus.id != id {
				t.Errorf("enter focused %q, want %q", next.focus.id, id)
			}
		})
	}
}

// roleAllowlist is the parity test's one explicit allowlist: the verbs a row role keeps
// on the bar, the rest of what Graph hints being ?-only (ADR 0360).
func roleAllowlist(role rowRole, live bool, hinted, extra []string) (h, e []string) {
	if role == roleFull {
		return hinted, extra
	}
	keep := "w"
	if role == roleClosed {
		keep = "c"
	}
	for _, k := range hinted {
		if k == keep || k == "x" && live {
			h = append(h, k)
		} else {
			e = append(e, k)
		}
	}
	e = append(e, extra...)
	slices.Sort(e)
	return h, e
}

// Every bead row hints and routes the bead verbs as the Graph reference does for the
// same bead (Spec mc-w2a8c, Testing Decisions); a row with no bead hints none and
// answers "nothing selected".
func TestBeadVerbParity(t *testing.T) {
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			base, calls := s.model(t)
			for _, c := range s.cur(base) {
				t.Run(c, func(t *testing.T) {
					m := s.go_(base, c)
					b := m.rowBead()
					if b == nil {
						if got := verbSet(m.tabBindings()); len(got) != 0 {
							t.Errorf("no-bead row hints %v", got)
						}
						for _, k := range verbKeys() {
							if o := press1(t, m, calls, k); !o.handled || !strings.HasPrefix(o.status, "nothing selected") && !strings.Contains(o.status, "Unparented") {
								t.Errorf("%q on a no-bead row: handled=%v status %q", k, o.handled, o.status)
							}
						}
						return
					}
					ref := onRow(m, b.ID)
					ref.tab = tabGraph
					rg := ref.tabAt(tabGraph).(graphTab)
					wantH, wantE := roleAllowlist(s.role(m, c), m.sessions[b.ID].Has(), verbSet(rg.bindings()), verbSet(rg.extra()))
					if got := verbSet(m.tabBindings()); !slices.Equal(got, wantH) {
						t.Errorf("%s: hinted %v, want %v", b.ID, got, wantH)
					}
					if got := verbSet(m.tabExtra()); !slices.Equal(got, wantE) {
						t.Errorf("%s: ?-only %v, want %v", b.ID, got, wantE)
					}
					for _, k := range verbKeys() {
						got, want := press1(t, m, calls, k), press1(t, ref, calls, k)
						if fmt.Sprint(got) != fmt.Sprint(want) {
							t.Errorf("%s: %q\n got  %+v\n want %+v", b.ID, k, got, want)
						}
					}
				})
			}
		})
	}
}

// c on a Checkup answers a reason, where it was once bound but silent.
func TestCheckupRowRefusesC(t *testing.T) {
	m, calls := verbModel(t)
	if o := press1(t, onRow(m, "ck"), calls, "c"); !strings.Contains(o.status, "Checkup") || o.top != "" {
		t.Errorf("c on a Checkup: %+v", o)
	}
}

// w opens the link a Landing records; a Chart and a Checkpoint carry none, so w is not
// hinted there and runs nothing; a bead with no link yet says so.
func TestWOnGraphRows(t *testing.T) {
	m, _ := verbModel(t)
	ran := stubLaunch(t, nil)
	if !slices.Contains(verbSet(onRow(m, "land").tabBindings()), "w") {
		t.Errorf("w not hinted on a Landing: %s", hintsOf(onRow(m, "land")))
	}
	if got := runW(t, onRow(m, "land")); !strings.HasPrefix(got.status, "opened ") || len(*ran) != 1 {
		t.Errorf("w on a Landing: status %q ran %v", got.status, *ran)
	}
	*ran = nil
	for _, id := range []string{"cha", "cp", "sig"} {
		r := onRow(m, id)
		if slices.Contains(verbSet(r.tabBindings()), "w") {
			t.Errorf("w hinted on %s", id)
		}
		if got := runW(t, r); !strings.Contains(got.status, "no link yet") || len(*ran) != 0 {
			t.Errorf("w on %s: status %q ran %v", id, got.status, *ran)
		}
	}
}

// A pending write answers "write in flight" before A's own gate.
func TestWriteInFlightPrecedesRelease(t *testing.T) {
	m, calls := verbModel(t)
	if o := press1(t, onRow(m, "kid"), calls, "A"); !strings.Contains(o.status, "write in flight") {
		t.Errorf("A on a pending bead: %+v", o)
	}
}

// +/- and p are offered on an unclosed bead only; U stays on a closed bead with an open blocker.
func TestClosedRowOffersNoPrioOrPause(t *testing.T) {
	m, _ := verbModel(t)
	got := verbSet(onRow(m, "cblk").tabBindings())
	for _, k := range []string{"+", "p"} {
		if slices.Contains(got, k) {
			t.Errorf("closed row hints %q: %v", k, got)
		}
	}
	if !slices.Contains(got, "U") {
		t.Errorf("closed row with an open blocker hints %v, want U", got)
	}
}

// w ranks just below b (4.5) and X just below x (5.5): the full verb set only shortens
// labels at 100 and 120 columns, nothing drops; at 80 only the +/-, B and U (rank 14 and up) go (ADR 0359).
func TestFullVerbSetNeverDropsFromTheBar(t *testing.T) {
	for _, w := range []int{80, 100, 120} {
		m, _ := verbModel(t)
		m, _ = send(t, onRow(m, "full"), tea.WindowSizeMsg{Width: w, Height: 40})
		hinted := verbSet(m.tabBindings())
		for _, k := range []string{"w", "p", "z", "e", "+", "c", "a", "B", "U"} {
			if !slices.Contains(hinted, k) {
				t.Fatalf("fixture row doesn't hint %q: %v", k, hinted)
			}
		}
		l := lines(m.View())
		words := strings.Fields(strings.NewReplacer("•", " ", "|", " ").Replace(ansi.Strip(l[len(l)-1])))
		kept := []string{"w", "X", "p", "z", "e", "+/-", "c", "a", "B", "U"}
		if w == 80 { // the pinned g n d ? keys leave no room for the last three; w and X outlive them
			kept = []string{"w", "X", "p", "z", "e", "c", "a"}
		}
		for _, k := range kept {
			if !slices.Contains(words, k) {
				t.Errorf("%d cols: %q dropped from %q", w, k, l[len(l)-1])
			}
		}
	}
	w, b, x := hintOf(keyOpenMR), hintOf(keyBg), hintOf(keyDropKey)
	X := hintOf(keyWithdrawKey)
	if !(b.rank < w.rank && w.rank < x.rank && x.rank < X.rank) || w.sec != secRowHint || X.sec != secRowHint {
		t.Errorf("ranks b %v w %v x %v X %v, sections %d %d", b.rank, w.rank, x.rank, X.rank, w.sec, X.sec)
	}
	if s := hintOf(keyWindow); s.sec != secTabHint {
		t.Errorf("Stats' w window is section %d, want the tab's", s.sec)
	}
}

// An unhinted x on Ideate is routed and answers as dropKey does on Graph, never silently.
func TestIdeateUnhintedXAnswers(t *testing.T) {
	m, calls := ideaParityModel(t)
	m = ideaGo(m, "loose")
	if slices.Contains(verbSet(m.tabBindings()), "x") {
		t.Fatalf("fixture row hints x: %s", hintsOf(m))
	}
	got := press1(t, m, calls, "x")
	ref := onRow(m, "loose")
	ref.tab = tabGraph
	if want := press1(t, ref, calls, "x"); got.status == "" || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("x on Ideate %+v, want Graph's %+v", got, want)
	}
}

// enter, b and space keep their Ideate meanings beside the router.
func TestIdeateKeepsExemptKeys(t *testing.T) {
	m, _ := ideaParityModel(t)
	m = ideaGo(m, "loose")
	if h := hintsOf(m); !strings.Contains(h, "enter triage") || !strings.Contains(h, "b bg") {
		t.Errorf("hints %q", h)
	}
	if h := hintsOf(ideaGo(m, "i:ini")); !strings.Contains(h, "space fold") {
		t.Errorf("header hints %q", h)
	}
}

// w on a Defog MR row opens a recorded link, and says there is no link yet on a Watch
// before its backfill; a row with no bead hints no verb and answers "nothing selected".
func TestWOnDefogRows(t *testing.T) {
	m, _ := defogVerbModel(t)
	ran := stubLaunch(t, nil)
	at := func(k string) Model { return defogSurface().go_(m, k) }
	if got := runW(t, at(mrsPrefix+"mrl")); !strings.HasPrefix(got.status, "opened ") || len(*ran) != 1 {
		t.Errorf("w on a linked MR row: status %q ran %v", got.status, *ran)
	}
	*ran = nil
	if got := runW(t, at(mrsPrefix+"mrw")); !strings.Contains(got.status, "no link yet") || len(*ran) != 0 {
		t.Errorf("w on an MR row without a link: status %q ran %v", got.status, *ran)
	}
	for _, k := range []string{mrsKey, chartsKey, finKey} {
		if got := runW(t, at(k)); got.status != "nothing selected" {
			t.Errorf("w on %s: status %q", k, got.status)
		}
	}
}

// Defog's drawer-open runs keep handled equal to bound on each bead-row role.
func TestDefogVerbRowCoverage(t *testing.T) {
	m, _ := defogVerbModel(t)
	for _, k := range []string{"i:ini", chartsPrefix + "c", finPrefix + "fin", mrsPrefix + "mrw", "afk"} {
		n := defogSurface().go_(m, k)
		checkCoverage(t, "Defog "+k, n, true, "2", "J", "K", "ctrl+down", "ctrl+up")
		short, _ := send(t, n, tea.WindowSizeMsg{Width: 120, Height: 13})               // the detail overflows, so it scrolls
		open := press(t, press(t, short, runeKey("d")), tea.KeyMsg{Type: tea.KeyCtrlD}) // mid-scroll, so up and down both move
		checkCoverage(t, "Defog "+k+" drawer", open, true, "2", "J", "K", "ctrl+down", "ctrl+up")
	}
}

// defogSurface is Defog's entry in surfaces.
func defogSurface() surface {
	for _, s := range surfaces {
		if s.name == "Defog" {
			return s
		}
	}
	panic("no Defog surface")
}

// The Execute fixture meets every row kind the walk must cover, and w opens a Landing's
// link but is not hinted on a Checkpoint row.
func TestExecuteVerbWalkCoversRowKinds(t *testing.T) {
	m, _ := execVerbModel(t)
	seen := map[int]bool{}
	var initHead bool
	for _, r := range m.tabAt(tabExecute).(execTab).rows() {
		seen[r.kind] = true
		initHead = initHead || r.kind == eSec && r.init != nil
	}
	for k, name := range map[int]string{eMR: "eMR", eCPS: "eCPS", eSPS: "eSPS", eLanded: "eLanded", eClosed: "eClosed", eSum: "eSum", ePlace: "ePlace"} {
		if !seen[k] {
			t.Errorf("walk never meets %s", name)
		}
	}
	if !initHead {
		t.Error("walk never meets an Initiative section")
	}
	at := func(k string) Model { return s0(m, k) }
	ran := stubLaunch(t, nil)
	if got := runW(t, at("lnk")); !strings.HasPrefix(got.status, "opened ") || len(*ran) != 1 {
		t.Errorf("w on a Landing: status %q ran %v", got.status, *ran)
	}
	for _, k := range []string{"cpA", "cps:cpA"} {
		if slices.Contains(verbSet(at(k).tabBindings()), "w") {
			t.Errorf("w hinted on %s", k)
		}
	}
}

func s0(m Model, k string) Model {
	m.tabs = slices.Clone(m.tabs)
	e := m.tabs[tabExecute].(execTab)
	e.sel = k
	m.tabs[tabExecute] = e
	return m
}

// The parity walk covers exactly the six bead-row surfaces: no entry missing, none extra.
func TestParityWalksAllSixSurfaces(t *testing.T) {
	var got []string
	for _, s := range surfaces {
		got = append(got, s.name)
	}
	slices.Sort(got)
	if want := []string{"Defog", "Execute", "Focus", "Graph", "Ideate", "Initiatives"}; !slices.Equal(got, want) {
		t.Errorf("surfaces %v, want %v", got, want)
	}
}

// History, Stats and Logs sit outside the router: w (window), e (errors) and every other
// bead-verb key keep their own meaning there.
func TestBeadVerbsLeaveHistoryStatsLogsAlone(t *testing.T) {
	for _, tab := range []int{tabHistory, tabStats, tabLogs} {
		m, _ := verbModel(t)
		m.tab = tab
		if m.routesBeadVerbs() {
			t.Errorf("tab %d is routed", tab)
		}
		for _, k := range verbKeys() {
			if _, _, ok := m.beadVerbRoute(runeKey(k)); ok {
				t.Errorf("tab %d: router claimed %q", tab, k)
			}
		}
	}
	for _, tab := range []int{tabIdeate, tabDefog, tabExecute, tabGraph, tabInits} {
		m, _ := verbModel(t)
		m.tab = tab
		if !m.routesBeadVerbs() {
			t.Errorf("tab %d is not routed", tab)
		}
	}
}
