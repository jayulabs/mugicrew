package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func closedAt(d time.Duration) func(*bd.Bead) {
	return func(b *bd.Bead) { b.Closed = epoch.Add(d) }
}

func cancelled(b *bd.Bead) { b.CloseReason = "cancelled: dropped" }

// initsFixture: open Initiatives a (P1), b (P2, older than c) and c (P2), a paused
// one, closed d (closed last, updated first), e (closed first, updated last) and a
// cancelled f; a Draft Initiative and a member Signal that are not rows.
func initsFixture() []bd.Bead {
	return []bd.Bead{
		dbead("a", contract.KindInitiative, "open", "", 1, titled("Alpha")),
		dbead("b", contract.KindInitiative, "open", "", 2, titled("Bravo")),
		dbead("c", contract.KindInitiative, "in_progress", "", 2, titled("Charlie"), newer(time.Hour)),
		dbead("pz", contract.KindInitiative, "open", "", 3, titled("Paused one"), labeled("paused:manual")),
		dbead("d", contract.KindInitiative, "closed", "", 1, titled("Delta"), closedAt(3*time.Hour), func(b *bd.Bead) { b.Updated = epoch }),
		dbead("e", contract.KindInitiative, "closed", "", 1, titled("Echo"), closedAt(time.Hour), func(b *bd.Bead) { b.Updated = epoch.Add(9 * time.Hour) }),
		dbead("f", contract.KindInitiative, "closed", "", 1, titled("Foxtrot"), closedAt(2*time.Hour), cancelled),
		dbead("z", contract.KindInitiative, "closed", "", 1, titled("Zulu")), // no close time: after every timed one
		dbead("draft", contract.KindInitiative, "deferred", "", 1, titled("Draftish")),
		dbead("s1", contract.KindSignal, "open", "a", 1, titled("member signal")),
	}
}

func initsModel(t *testing.T, beads []bd.Bead, sess map[string]session.Info) Model {
	t.Helper()
	m, _ := focusModel(t, 120, 56, beads, sess, map[string]bool{})
	return press(t, m, runeKey("5"))
}

func initsBody(m Model) string { return ansi.Strip(m.View()) }

func indexOfAll(t *testing.T, v string, ss ...string) {
	t.Helper()
	at := -1
	for _, s := range ss {
		i := strings.Index(v, s)
		if i < 0 || i < at {
			t.Fatalf("%q missing or out of order in:\n%s", s, v)
		}
		at = i
	}
}

func TestInitsRows(t *testing.T) {
	m := initsModel(t, initsFixture(), nil)
	v := initsBody(m)
	indexOfAll(t, v, "Open (4)", "Alpha", "Charlie", "Bravo", "Paused one", "Closed (4)")
	for _, no := range []string{"Draftish", "member signal", "landed", "drained", "under way", "paused", "Delta"} {
		if strings.Contains(v, no) {
			t.Errorf("view shows %q:\n%s", no, v)
		}
	}
	if !strings.Contains(v, "‖") {
		t.Errorf("row lacks the paused glyph:\n%s", v)
	}
	// Closed is folded; space on its header opens it: most recently closed first.
	m = initsAt(t, m, initsClosed)
	if !strings.Contains(cursorLine(m), "Closed (4)") {
		t.Fatalf("cursor line %q", cursorLine(m))
	}
	v = initsBody(press(t, m, runeKey(" ")))
	indexOfAll(t, v, "Closed (4)", "Delta", "Foxtrot", "Echo", "Zulu")
	if strings.Count(v, "cancelled") != 1 {
		t.Errorf("want one cancelled tag:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "Foxtrot") && !strings.Contains(l, "P1 · Foxtrot · f · cancelled") {
			t.Errorf("cancelled row %q lacks dot layout", l)
		}
	}
	if after := initsBody(press(t, press(t, m, runeKey(" ")), runeKey(" "))); strings.Contains(after, "Delta") {
		t.Errorf("space twice did not fold Closed:\n%s", after)
	}
}

func TestInitsWaitingDotAndNoBadge(t *testing.T) {
	sess := map[string]session.Info{"b": {State: session.Alive, Waiting: session.WaitTurn}}
	m := initsModel(t, initsFixture(), sess)
	var bravo string
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "Bravo") {
			bravo = l
		}
	}
	if !strings.Contains(bravo, "●") {
		t.Errorf("Bravo row lacks ●: %q", bravo)
	}
	if _, after, ok := strings.Cut(lines(m.View())[2], "5 Initiatives"); !ok || strings.Contains(after, "●") { // Ideate's own ●1 sits before it
		t.Errorf("tab label carries a badge: %q", lines(m.View())[2])
	}
	if n := m.tabAt(tabInits).count(); n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
}

func TestInitsEmptyStateAndZeroSections(t *testing.T) {
	v := initsBody(initsModel(t, []bd.Bead{dbead("x", contract.KindSignal, "open", "", 1)}, nil))
	if !strings.Contains(v, "no Initiatives yet — n i to start one") || strings.Contains(v, "Open (") {
		t.Errorf("empty state:\n%s", v)
	}
	v = initsBody(initsModel(t, []bd.Bead{dbead("a", contract.KindInitiative, "open", "", 1, titled("Alpha"))}, nil))
	indexOfAll(t, v, "Open (1)", "Alpha", "Closed (0)")
}

func TestInitsHints(t *testing.T) {
	m := initsModel(t, initsFixture(), nil)
	if h := strings.TrimSpace(ansi.Strip(m.hintsView())); h != "g goto • n new • space fold | ? keys" {
		t.Errorf("header hints = %q", h)
	}
}

func TestInitsTabBarAt40(t *testing.T) {
	m, _ := send(t, initsModel(t, initsFixture(), nil), tea.WindowSizeMsg{Width: 40, Height: 16})
	if bar := lines(m.View())[2]; !strings.Contains(bar, "5 Initiatives") {
		t.Errorf("tab bar = %q", bar)
	}
}

func initsSel(m Model) string { return m.tabAt(tabInits).(initsTab).sel }

// initsAt moves the cursor to key by pressing j (the fixture's rows are fixed).
func initsAt(t *testing.T, m Model, key string) Model {
	t.Helper()
	for range 20 {
		if initsSel(m) == key {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("cursor never reached %q", key)
	return m
}

// refreshInits sends a snapshot of the fixture after edit.
func refreshInits(t *testing.T, m Model, edit func([]bd.Bead) []bd.Bead) Model {
	t.Helper()
	m, _ = send(t, m, loadedMsg{slowTier: true, beads: edit(initsFixture()), live: map[string]bool{}, efforts: map[string]bool{}})
	return m
}

func closeInit(id string, mod func(*bd.Bead)) func([]bd.Bead) []bd.Bead {
	return func(bs []bd.Bead) []bd.Bead {
		for i := range bs {
			if bs[i].ID == id {
				bs[i].Status = "closed"
				bs[i].Closed = epoch.Add(10 * time.Hour)
				if mod != nil {
					mod(&bs[i])
				}
			}
		}
		return bs
	}
}

func TestInitsCursorAcrossRefresh(t *testing.T) {
	// closes with Closed folded: the cursor lands on the Closed header
	m := initsAt(t, initsModel(t, initsFixture(), nil), "b")
	if n := refreshInits(t, m, closeInit("b", nil)); initsSel(n) != initsClosed {
		t.Errorf("closed, Closed folded: sel = %q, want %q", initsSel(n), initsClosed)
	}
	// closes with Closed unfolded: the cursor follows the row into Closed
	u := press(t, initsAt(t, initsModel(t, initsFixture(), nil), initsClosed), runeKey(" "))
	u = initsAt(t, u, "b")
	n := refreshInits(t, u, closeInit("b", nil))
	if initsSel(n) != "b" || !strings.Contains(cursorLine(n), "Bravo") {
		t.Errorf("closed, Closed unfolded: sel = %q, cursor %q", initsSel(n), cursorLine(n))
	}
	// cancelled is a close
	if n := refreshInits(t, m, closeInit("b", cancelled)); initsSel(n) != initsClosed {
		t.Errorf("cancelled: sel = %q, want %q", initsSel(n), initsClosed)
	}
	// a new Initiative born above the cursor never moves it
	born := func(bs []bd.Bead) []bd.Bead {
		return append(bs, dbead("n", contract.KindInitiative, "open", "", 0, titled("Newborn")))
	}
	if n := refreshInits(t, m, born); initsSel(n) != "b" || !strings.Contains(initsBody(n), "Newborn") {
		t.Errorf("born: sel = %q", initsSel(n))
	}
	// the selected Initiative vanishing: the row that was just above it (c above b)
	gone := func(bs []bd.Bead) []bd.Bead {
		return slices.DeleteFunc(bs, func(b bd.Bead) bool { return b.ID == "b" })
	}
	if n := refreshInits(t, m, gone); initsSel(n) != "c" {
		t.Errorf("vanished: sel = %q, want c", initsSel(n))
	}
}

// line2Of is the muted count line under the Initiatives row titled title.
func line2Of(m Model, title string) string {
	ls := lines(m.View())
	for i, l := range ls {
		if strings.Contains(l, title) && i+1 < len(ls) {
			return strings.TrimSpace(ls[i+1])
		}
	}
	return ""
}

// Billing revamp's subtree by hand: checkpoint cp (open); open descendants s1-s4, c,
// g, cp, mem = 8 (closed s5 and Draft s6 out); live s1, g = 2; waiting s1 and the
// side Task holding cp's land = 2, the back bar's ●N.
func TestInitsLine2Counts(t *testing.T) {
	sess := map[string]session.Info{
		"s1":    {State: session.Alive, Waiting: session.WaitTurn},
		"g":     {State: session.Alive},
		"task":  {State: session.Alive, Waiting: session.WaitAsk},
		"loose": {State: session.Alive, Waiting: session.WaitTurn},
	}
	m, _ := focusModel(t, 160, 40, focusFixture(), sess, map[string]bool{})
	m = press(t, m, runeKey("5"))
	want := "checkpoints 1 open · 0 closed · 8 open descendants · 2 live · 2 waiting"
	if got := line2Of(m, "Billing revamp"); got != want {
		t.Errorf("line 2 = %q, want %q", got, want)
	}
	if got := line2Of(m, "Empty one"); got != "checkpoints 0 open · 0 closed · 0 open descendants · 0 live · 0 waiting" {
		t.Errorf("empty Initiative line 2 = %q", got)
	}
	f := gotoFocus(t, press(t, m, runeKey("j"))) // Billing revamp is the first row
	if bar := backBarOf(f); !strings.Contains(bar, "●2") {
		t.Errorf("back bar %q lacks ●2", bar)
	}
}

// The Initiative's own waiting cut session counts in line-2 waiting, so it equals the back bar's ●N.
func TestInitsLine2WaitingIncludesInitiative(t *testing.T) {
	sess := map[string]session.Info{"ini": {State: session.Alive, Waiting: session.WaitTurn}}
	m, _ := focusModel(t, 160, 40, focusFixture(), sess, map[string]bool{})
	m = press(t, m, runeKey("5"))
	if got := line2Of(m, "Billing revamp"); !strings.HasSuffix(got, "· 1 waiting") {
		t.Errorf("line 2 = %q, want 1 waiting", got)
	}
	f := gotoFocus(t, press(t, m, runeKey("j")))
	if bar := backBarOf(f); !strings.Contains(bar, "●1") {
		t.Errorf("back bar %q lacks ●1", bar)
	}
}

// The counts move only on a snapshot refresh, never on a keypress or render.
func TestInitsLine2StaleUntilRefresh(t *testing.T) {
	m, _ := focusModel(t, 160, 40, focusFixture(), nil, map[string]bool{})
	m = press(t, m, runeKey("5"))
	if got := line2Of(m, "Billing revamp"); !strings.Contains(got, "0 live") {
		t.Fatalf("line 2 = %q", got)
	}
	m.sessions = map[string]session.Info{"g": {State: session.Alive}} // changed behind the cache
	m = press(t, press(t, m, runeKey("j")), runeKey("k"))
	if got := line2Of(m, "Billing revamp"); !strings.Contains(got, "0 live") {
		t.Errorf("counts changed without a refresh: %q", got)
	}
	m, _ = send(t, m, loadedMsg{slowTier: true, beads: focusFixture(), live: map[string]bool{}, sessions: m.sessions, efforts: map[string]bool{}})
	if got := line2Of(m, "Billing revamp"); !strings.Contains(got, "1 live") {
		t.Errorf("counts did not update on refresh: %q", got)
	}
}

// Cursor motion moves by rows: line 2 is never a stop.
func TestInitsCursorSkipsLine2(t *testing.T) {
	m := initsModel(t, initsFixture(), nil)
	cursor := func(m Model) (c string) {
		for _, l := range lines(m.View()) {
			if strings.Contains(l, "❯") {
				c += l
			}
		}
		return
	}
	for _, want := range []string{"Alpha", "Charlie"} { // from the Open header: row by row
		if m = press(t, m, runeKey("j")); !strings.Contains(cursor(m), want) {
			t.Errorf("cursor on %q, want %s", cursor(m), want)
		}
	}
}

// onInitsRow is the tab with the cursor on Alpha, the first Open row.
func onInitsRow(t *testing.T) (Model, *[]string) {
	t.Helper()
	m, calls := focusModel(t, 120, 40, initsFixture(), nil, map[string]bool{})
	return press(t, press(t, m, runeKey("5")), runeKey("j")), calls
}

func TestInitsEnterAndGFOpenFocus(t *testing.T) {
	for _, k := range []string{"enter", "g f"} {
		m, calls := onInitsRow(t)
		n, cmd := send(t, m, enterKey())
		if k == "g f" {
			n, cmd = send(t, press(t, m, runeKey("g")), runeKey("f"))
		}
		if !focused(n) || !strings.Contains(backBarOf(n), "Alpha") || !strings.Contains(backBarOf(n), "from Initiatives") {
			t.Fatalf("%q: no focus on Alpha: %q", k, backBarOf(n))
		}
		if cmd != nil || len(*calls) != 0 || len(n.launching) != 0 {
			t.Errorf("%q launched or attached: cmd=%v calls=%v", k, cmd != nil, *calls)
		}
		if back := press(t, n, escKey()); focused(back) || back.tab != tabInits {
			t.Errorf("%q: esc did not return to tab 5", k)
		}
	}
	// a closed row too
	m, _ := onInitsRow(t)
	for range 4 {
		m = press(t, m, runeKey("j"))
	}
	m = press(t, press(t, m, runeKey(" ")), runeKey("j")) // unfold Closed, onto Delta
	if n := press(t, m, enterKey()); !focused(n) || !strings.Contains(backBarOf(n), "Delta") {
		t.Errorf("closed row: %q", backBarOf(n))
	}
}

func TestInitsDetailDrawer(t *testing.T) {
	m, _ := onInitsRow(t)
	n := press(t, m, runeKey("d"))
	if v := initsBody(n); !strings.Contains(v, "0/1 member chains") {
		t.Errorf("no Initiative detail:\n%s", v)
	}
	if v := initsBody(press(t, n, escKey())); strings.Contains(v, "member chains") {
		t.Errorf("esc left the drawer open:\n%s", v)
	}
	if v := initsBody(press(t, n, runeKey("d"))); strings.Contains(v, "member chains") {
		t.Errorf("d did not close the drawer:\n%s", v)
	}
}

func TestInitsHeaderEnterAndI(t *testing.T) {
	m, _ := focusModel(t, 120, 40, initsFixture(), nil, map[string]bool{})
	m = press(t, m, runeKey("5"))
	n, cmd := send(t, m, enterKey())
	if cmd != nil || n.View() != m.View() || n.status != "" {
		t.Errorf("enter on a header changed something: %q", n.status)
	}
	n = gotoFocus(t, m) // an Initiatives section header is no Initiative
	if focused(n) {
		t.Error("g f on a header opened a focus")
	}
	if n = press(t, m, runeKey("i")); focused(n) || n.status != "" {
		t.Errorf("i is a key again: status %q", n.status)
	}
}

func TestInitsUnboundKeysDoNothing(t *testing.T) {
	m, _ := onInitsRow(t)
	for _, k := range []string{"b", "p", "+", "-", "x", "a", "A", "e"} {
		n, cmd := send(t, m, runeKey(k))
		if cmd != nil || n.View() != m.View() || n.status != "" {
			t.Errorf("%s moved state: status %q", k, n.status)
		}
	}
}

func TestInitsRowHintsAndCoverage(t *testing.T) {
	m, _ := onInitsRow(t)
	if h := strings.TrimSpace(ansi.Strip(m.hintsView())); h != "enter focus • t transcript • r retrospective | g goto • n new • d detail | ? keys" {
		t.Errorf("row hints = %q", h)
	}
	if h := strings.TrimSpace(ansi.Strip(press(t, m, runeKey("d")).hintsView())); !strings.Contains(h, "g goto • n new • d close") {
		t.Errorf("drawer hints = %q", h)
	}
	ref := press(t, m, runeKey("?"))
	if v := ansi.Strip(ref.View()); !strings.Contains(v, "focus") {
		t.Errorf("? lacks i:\n%s", v)
	}
	checkCoverage(t, "Inits row", m, true, "5", "J", "K", "ctrl+down", "ctrl+up")
	short, _ := send(t, m, tea.WindowSizeMsg{Width: 120, Height: 12})               // the detail overflows, so it scrolls
	open := press(t, press(t, short, runeKey("d")), tea.KeyMsg{Type: tea.KeyCtrlD}) // mid-scroll, so up and down both move
	checkCoverage(t, "Inits row drawer", open, true, "5", "J", "K", "ctrl+down", "ctrl+up")
}

// A large graph (thousands of beads, deep subtrees) must not stall the refresh fold: the
// counts are computed on the Update goroutine every snapshot.
func TestInitStatsOfStaysCheapOnALargeGraph(t *testing.T) {
	var bs []bd.Bead
	for in := 0; in < 8; in++ {
		root := fmt.Sprintf("in%d", in)
		bs = append(bs, dbead(root, contract.KindInitiative, "open", "", 1))
		parent := root
		for n := 0; n < 400; n++ { // a chain of 5 levels under each Initiative, many Checkpoints
			id := fmt.Sprintf("b%d-%d", in, n)
			kind := contract.KindTask
			if n%5 == 0 {
				kind = contract.KindCheckpoint
			}
			bs = append(bs, dbead(id, kind, "open", parent, 2))
			if n%5 != 4 {
				parent = id
			} else {
				parent = root
			}
		}
	}
	m := sized(t, 160, 40)
	m.beads = bs
	start := time.Now()
	got := m.initStatsOf()
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("initStatsOf over %d beads took %v, want < 50ms", len(bs), d)
	}
	if got["in0"].cpOpen != 80 {
		t.Errorf("in0 cpOpen = %d, want 80", got["in0"].cpOpen)
	}
}

// n opens the Initiative form from a row, a section header and the empty tab; saving
// files an Initiative, never a Signal, through the same two-step priority write.
func TestInitsNewOpensInitiativeForm(t *testing.T) {
	row, _ := onInitsRow(t)
	hdr, _ := focusModel(t, 120, 40, initsFixture(), nil, map[string]bool{})
	hdr = press(t, hdr, runeKey("5"))
	empty := initsModel(t, nil, nil)
	for name, m := range map[string]Model{"row": row, "header": hdr, "empty": empty} {
		m = pressN(t, m, "i")
		if len(m.modals) != 1 || !strings.Contains(view(m), "New Initiative") {
			t.Errorf("%s: n did not open the Initiative form:\n%s", name, view(m))
		}
	}
}

func TestInitsNewSavesInitiativeNotSignal(t *testing.T) {
	var got, upd []string
	m := initsModel(t, nil, nil) // before the seams: the model's setup installs its own
	seam(t, &newSignal, func(string, string) (string, error) { t.Fatal("filed a Signal"); return "", nil })
	seam(t, &newInitiative, func(title, body, _ string) (string, error) { got = []string{title, body}; return "i9", nil })
	seam(t, &updateBead, func(id string, a []string) error { upd = append([]string{id}, a...); return nil })
	m = typeText(t, pressN(t, m, "i"), "Seed")
	m = press(t, press(t, press(t, m, tabKey()), runeKey("+")), ctrlS())
	if !slices.Equal(got, []string{"Seed", ""}) || !slices.Equal(upd, []string{"i9", "--priority", "1"}) || len(m.modals) != 0 || !strings.Contains(m.status, "filed Initiative i9") {
		t.Fatalf("got %v upd %v modals %d status %q", got, upd, len(m.modals), m.status)
	}
}

func TestInitsNewEmptyTitleAndFailureKeepForm(t *testing.T) {
	m := pressN(t, initsModel(t, nil, nil), "i")
	seam(t, &newInitiative, func(string, string, string) (string, error) { t.Fatal("filed"); return "", nil })
	if m = press(t, m, ctrlS()); len(m.modals) != 1 || !strings.Contains(view(m), "title required") {
		t.Errorf("empty title:\n%s", view(m))
	}
	seam(t, &newInitiative, func(string, string, string) (string, error) { return "", errTest })
	m = press(t, typeText(t, m, "T"), ctrlS())
	if len(m.modals) != 1 || !strings.Contains(view(m), "boom") || m.modals[0].sf.title.text != "T" {
		t.Errorf("failed write lost the form:\n%s", view(m))
	}
	if m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc}); !strings.Contains(view(m), "this Initiative") {
		t.Errorf("discard prompt:\n%s", view(m))
	}
}

func TestInitsEmptyHints(t *testing.T) {
	if h := strings.TrimSpace(ansi.Strip(initsModel(t, nil, nil).hintsView())); h != "n new | ? keys" {
		t.Errorf("empty hints = %q", h)
	}
}

// An Initiatives row is its SBRN, kind included, with no dot column.
func TestInitiativesRowIsSBRN(t *testing.T) {
	tab := initsTab{}
	b := ini("i1", "open", "Ship v2", 1)
	if got, want := ansi.Strip(func() string { s, _ := tab.line(iRow{key: "i1", b: &b}); return s }()), "○  INITIATIVE  P1 · Ship v2 · i1"; !strings.Contains(got, want) {
		t.Errorf("row %q, want it to contain %q", got, want)
	}
}

// Line 2 starts under the title on both forms, selected or not; the Closed header has one blank above it.
func TestInitsLine2AlignsAndClosedGap(t *testing.T) {
	m := initsModel(t, initsFixture(), nil)
	check := func(m Model, title string) {
		t.Helper()
		ls := lines(m.View())
		for i, l := range ls {
			if !strings.Contains(l, title) {
				continue
			}
			col := ansi.StringWidth(l[:strings.Index(l, title)])
			l2 := ls[i+1]
			if got := len(l2) - len(strings.TrimLeft(l2, " ")); got != col {
				t.Errorf("%s: line 2 indent %d, title col %d", title, got, col)
			}
			return
		}
		t.Errorf("%s not found", title)
	}
	m = press(t, m, runeKey("j"))
	check(m, "Alpha")
	check(m, "Bravo")
	gap := func(m Model) {
		t.Helper()
		ls := lines(m.View())
		for i, l := range ls {
			if strings.Contains(l, "Closed (") {
				if i < 2 || ls[i-1] != "" || ls[i-2] == "" {
					t.Errorf("want exactly one blank above Closed header: %q", ls)
				}
				return
			}
		}
		t.Error("no Closed header")
	}
	gap(m)
	gap(initsAt(t, m, initsOpen)) // cursor on a header
	for _, l := range lines(m.View())[:3] {
		if l == "" {
			t.Errorf("blank in the head of the list: %q", l)
		}
	}
	// Open empty: only closed beads
	only := slices.DeleteFunc(initsFixture(), func(b bd.Bead) bool { return b.Status != "closed" || b.Kind != contract.KindInitiative })
	gap(initsModel(t, only, nil))
}
