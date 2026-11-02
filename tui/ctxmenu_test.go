package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
)

// rightAt is a right press at screen cell (x, y).
func rightAt(t *testing.T, m Model, x, y int) (Model, tea.Cmd) {
	t.Helper()
	return send(t, m.WithMouse(true), tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
}

func menuKeys(c *ctxMenu) []string {
	var ks []string
	for _, it := range c.items {
		ks = append(ks, it.Keys()[0])
	}
	return ks
}

// Every bead row of Defog (open, MR, closed) opens a menu holding the offered verbs in
// hint-rank order, ?-only ones included, unoffered ones out, each key once.
func TestMenuItemsAreTheOfferedVerbsInRankOrder(t *testing.T) {
	m, _ := defogVerbModel(t)
	opened := 0
	for y := m.bodyTop(); y < m.bodyTop()+m.h-chromeH; y++ {
		next, _ := rightAt(t, m, 12, y)
		if next.menu == nil {
			continue
		}
		opened++
		b := next.rowBead()
		if b.ID != next.menu.bead {
			t.Fatalf("menu bead %s, selection %s", next.menu.bead, b.ID)
		}
		hinted, extra := beadVerbBindings(next.gateData(), b, roleFull)
		got := menuKeys(next.menu)
		for _, h := range hinted {
			if !slices.Contains(got, h.Keys()[0]) && !slices.ContainsFunc(next.menu.items, func(i key.Binding) bool { return slices.Contains(i.Keys(), h.Keys()[0]) }) {
				t.Errorf("%s: offered verb %s missing from %v", b.ID, h.Keys()[0], got)
			}
		}
		for _, e := range extra {
			if slices.Contains(got, e.Keys()[0]) {
				t.Errorf("%s: unoffered verb %s listed", b.ID, e.Keys()[0])
			}
		}
		seen := map[string]bool{}
		for i, it := range next.menu.items {
			if seen[it.Keys()[0]] {
				t.Errorf("%s: %s listed twice", b.ID, it.Keys()[0])
			}
			seen[it.Keys()[0]] = true
			if i > 0 && i < len(next.menu.items)-1 && hintOf(it).rank < hintOf(next.menu.items[i-1]).rank { // the trailing g goto › is unsorted
				t.Errorf("%s: %v out of hint-rank order", b.ID, got)
			}
		}
	}
	if opened < 5 {
		t.Fatalf("only %d bead rows opened a menu", opened)
	}
}

// A closed row's menu lists U, the ?-only verb its hints bar drops.
func TestMenuListsQuestionOnlyVerbs(t *testing.T) {
	m, _ := defogVerbModel(t)
	var saw bool
	for y := m.bodyTop(); y < m.bodyTop()+m.h-chromeH; y++ {
		next, _ := rightAt(t, m, 12, y)
		if next.menu != nil && next.menu.bead == "fin" {
			saw = true
			if !slices.Contains(menuKeys(next.menu), "U") {
				t.Errorf("closed row menu lacks U: %v", menuKeys(next.menu))
			}
		}
	}
	if !saw {
		t.Fatal("no menu opened on the closed row")
	}
}

// openBeadMenu right-clicks the first body line that opens a menu.
func openBeadMenu(t *testing.T, m Model) Model {
	t.Helper()
	for y := m.bodyTop(); y < m.bodyTop()+m.h-chromeH; y++ {
		if n, _ := rightAt(t, m, 12, y); n.menu != nil {
			return n
		}
	}
	t.Fatal("no bead row opened a menu")
	return m
}

// Running an item has the same status and writes as pressing its key, refusals included.
func TestMenuItemRunsLikeItsKey(t *testing.T) {
	m, calls := defogVerbModel(t)
	for y := m.bodyTop(); y < m.bodyTop()+m.h-chromeH; y++ {
		base, _ := rightAt(t, m, 12, y)
		if base.menu == nil {
			continue
		}
		for i, it := range base.menu.items {
			k := it.Keys()[0]
			plain := base
			plain.menu = nil
			plain.status = ""
			*calls = nil
			viaKey, cmd := send(t, plain, keyMsgOf(k))
			viaKey = settle(t, viaKey, cmd)
			want, wantCalls := viaKey.status, slices.Clone(*calls)
			base.status = ""
			*calls = nil
			via, cmd := send(t, base, tea.MouseMsg{X: base.menu.x + 2, Y: base.menu.y + 1 + i + base.menu.rules*b2i(i == len(base.menu.items)-1 && hasGoto(base.menu.items)), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			via = settle(t, via, cmd)
			if (via.menu != nil) != isStepItem(it) { // only a step item keeps the menu open (ADR 0412 §7)
				t.Fatalf("%s: menu open=%v after running", k, via.menu != nil)
			}
			if via.status != want || !slices.Equal(wantCalls, *calls) || len(via.modals) != len(viaKey.modals) {
				t.Errorf("%s on %s: menu status %q writes %v modals %d; key status %q writes %v modals %d", k, base.menu.bead, via.status, *calls, len(via.modals), want, wantCalls, len(viaKey.modals))
			}
		}
	}
}

func TestMenuPlacementFlipsAndClamps(t *testing.T) {
	m := openBeadMenu(t, func() Model { m, _ := defogVerbModel(t); return m }())
	mw, mh := m.menu.mw, m.menu.mh
	m.menu = nil
	at := func(w, h, px, py int) *ctxMenu {
		n := m
		n.w, n.h = w, h
		return n.openMenu(px, py).menu
	}
	if c := at(160, 40, 5, 6); c.x != 5 || c.y != 6 {
		t.Errorf("at pointer: %+v", c)
	}
	if c := at(160, 40, 158, 6); c.x != 158-mw+1 {
		t.Errorf("right flip x=%d, want %d", c.x, 158-mw+1)
	}
	if c := at(160, 40, 5, 39); c.y != 39-mh+1 {
		t.Errorf("bottom flip y=%d, want %d", c.y, 39-mh+1)
	}
	if c := at(160, 40, 158, 39); c.x != 158-mw+1 || c.y != 39-mh+1 {
		t.Errorf("both flips: %+v", c)
	}
	if c := at(mw-2, mh-2, 1, 1); c.x < 0 || c.y < 0 {
		t.Errorf("not clamped: %+v", c)
	}
	if c := at(mw-2, mh-2, 0, 0); c.x != 0 || c.y != 0 {
		t.Errorf("origin: %+v", c)
	}
}

func TestMenuClosesWithoutActing(t *testing.T) {
	m, calls := defogVerbModel(t)
	open := openBeadMenu(t, m)
	sel := open.rowBead().ID
	*calls = nil
	// a left press outside only closes
	got, _ := clickXY(t, open, open.menu.x+open.menu.mw+3, open.menu.y)
	if got.menu != nil || got.rowBead().ID != sel || len(*calls) != 0 {
		t.Errorf("outside click: menu=%v sel=%s calls=%v", got.menu != nil, got.rowBead().ID, *calls)
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, runeKey("q"), runeKey("5")} {
		got, _ := send(t, open, k)
		if got.menu != nil || got.rowBead().ID != sel || len(*calls) != 0 || got.tab != open.tab {
			t.Errorf("%v: menu=%v tab=%d calls=%v", k, got.menu != nil, got.tab, *calls)
		}
	}
	// j moves the highlight, motion over an item moves it too
	got, _ = send(t, open, runeKey("j"))
	if got.menu == nil || got.menu.cur != 1 {
		t.Errorf("j: %+v", got.menu)
	}
	got, _ = send(t, got, tea.MouseMsg{X: got.menu.x + 2, Y: got.menu.y + 1, Action: tea.MouseActionMotion})
	if got.menu.cur != 0 {
		t.Errorf("motion left cur=%d", got.menu.cur)
	}
	// the menu is drawn: a rounded box in the frame
	if v := ansi.Strip(open.View()); !containsRune(v, '╭') {
		t.Error("no box drawn")
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestRightClickOffABeadRowDoesNothing(t *testing.T) {
	m, _ := defogVerbModel(t)
	before := queueSel(m)
	for y := 0; y < m.h; y++ {
		n, _ := rightAt(t, m, 5, y)
		if n.menu != nil {
			continue
		}
		if queueSel(n) != before {
			t.Fatalf("right-click at %d moved the selection", y)
		}
	}
}

func TestMenuClosesWhenRefreshRemovesItsBead(t *testing.T) {
	m, _ := defogVerbModel(t)
	open := openBeadMenu(t, m)
	var keep []bd.Bead
	for _, b := range open.beads {
		if b.ID != open.menu.bead {
			keep = append(keep, b)
		}
	}
	got, _ := send(t, open, loadedMsg{beads: keep, live: map[string]bool{}, sessions: open.sessions})
	if got.menu != nil {
		t.Error("menu stayed open after its bead vanished")
	}
}

// A right-press on an Initiative's group header in Ideate, Defog and Execute selects the
// header and opens that Initiative's menu: the verbs the keys offer there, enter included.
func TestMenuOnInitiativeHeader(t *testing.T) {
	forceANSI256(t)
	for _, c := range []struct{ tab, head, id string }{
		{"ideate", "Ship v2 · ini", "ini"},
		{"defog", "t ini · ini", "ini"},
		{"execute", "t i1 · i1", "i1"},
	} {
		m := queueFixtures(t)[c.tab]
		y := screenRow(t, m, c.head)
		n, _ := rightAt(t, m, 12, y)
		if n.menu == nil || n.menu.bead != c.id {
			t.Fatalf("%s: right-press on the header opened %+v, want a menu for %s", c.tab, n.menu, c.id)
		}
		open := n
		open.menu = nil
		if !strings.Contains(cursorLine(open), c.head) {
			t.Errorf("%s: selection is %q, want the header", c.tab, cursorLine(open))
		}
		want := n.menuItems()
		if len(want) != len(n.menu.items) {
			t.Errorf("%s: menu has %d items, the keys offer %d", c.tab, len(n.menu.items), len(want))
		}
	}
}

// A drained Initiative's header menu lists enter's cut, and choosing it runs as the key does.
func TestMenuOnDrainedHeaderRunsEnter(t *testing.T) {
	m, _ := ideaParityModel(t)
	y := -1
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "▾ ○·  INITIATIVE  P1 · Drained") {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("no drained Initiative header")
	}
	n, _ := rightAt(t, m, 12, y)
	if n.menu == nil {
		t.Fatal("no menu on the drained header")
	}
	i := slices.Index(menuKeys(n.menu), "enter")
	if i < 0 {
		t.Fatalf("header menu %v lacks enter", menuKeys(n.menu))
	}
	plain := n
	plain.menu = nil
	viaKey, _ := send(t, plain, keyMsgOf("enter"))
	via, _ := send(t, n, tea.MouseMsg{X: n.menu.x + 2, Y: n.menu.y + 1 + i, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if via.menu != nil || len(via.modals) != len(viaKey.modals) || via.status != viaKey.status {
		t.Errorf("enter from the menu differs from the key: %q vs %q", via.status, viaKey.status)
	}
}

// A header standing for no bead opens no menu and leaves the selection where it was.
func TestMenuOffBeadlessHeaderDoesNothing(t *testing.T) {
	forceANSI256(t)
	for _, tab := range []string{"ideate", "defog", "execute"} {
		for _, head := range []string{"Actionable (", "No Initiative ("} {
			m := queueFixtures(t)[tab]
			y := screenRow(t, m, head)
			before := cursorLine(m)
			n, _ := rightAt(t, m, 12, y)
			if n.menu != nil {
				t.Errorf("%s: %s opened a menu", tab, head)
			}
			if cursorLine(n) != before {
				t.Errorf("%s: %s moved the selection to %q", tab, head, cursorLine(n))
			}
		}
	}
}

// Left-click on an Initiative header still folds it.
func TestLeftClickInitiativeHeaderStillFolds(t *testing.T) {
	forceANSI256(t)
	clickClock(t)
	m := queueFixtures(t)["defog"]
	y := screenRow(t, m, "t ini · ini")
	before := ansi.Strip(m.View())
	m, _ = clickXY(t, m, 20, y)
	if ansi.Strip(m.View()) == before || m.menu != nil {
		t.Errorf("left-click did not fold the Initiative header:\n%s", ansi.Strip(m.View()))
	}
}

func menuItemFor(c *ctxMenu, k string) (key.Binding, int) {
	for i, it := range c.items {
		if it.Keys()[0] == k {
			return it, i
		}
	}
	return key.Binding{}, -1
}

// d is listed at its hint rank, labelled as the hints bar has it, and running it toggles the drawer
// as the key does.
func TestMenuListsDetailToggle(t *testing.T) {
	m, _ := defogVerbModel(t)
	shut := openBeadMenu(t, m)
	it, i := menuItemFor(shut.menu, "d")
	if i < 0 || it.Help().Desc != "detail" {
		t.Fatalf("shut-drawer menu lacks d detail: %v", menuKeys(shut.menu))
	}
	for j, o := range shut.menu.items {
		if (hintOf(o).rank < 2) != (j < i) && hintOf(o).rank != 2 {
			t.Errorf("d at %d is out of rank order: %v", i, menuKeys(shut.menu))
		}
	}
	plain := shut
	plain.menu = nil
	viaKey := press(t, plain, runeKey("d"))
	for name, act := range map[string]tea.Msg{
		"click": tea.MouseMsg{X: shut.menu.x + 2, Y: shut.menu.y + 1 + i, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		"key":   runeKey("d"),
	} {
		got, cmd := send(t, shut, act)
		got = settle(t, got, cmd)
		if got.menu != nil || got.View() != viaKey.View() {
			t.Errorf("%s: menu %v, drawer differs from the d key", name, got.menu != nil)
		}
	}
	shut.menu.cur = i
	if got := press(t, shut, tea.KeyMsg{Type: tea.KeyEnter}); got.menu != nil || got.View() != viaKey.View() {
		t.Errorf("enter: drawer differs from the d key")
	}
	if _, j := menuItemFor(openBeadMenu(t, viaKey).menu, "d"); j < 0 {
		t.Fatal("open-drawer menu lacks d")
	} else if d, _ := menuItemFor(openBeadMenu(t, viaKey).menu, "d"); d.Help().Desc != "close" {
		t.Errorf("open drawer labels d %q, want close", d.Help().Desc)
	}
}

// Stats' d dist is not the detail item.
func TestMenuNeverListsStatsDDist(t *testing.T) {
	m, _ := defogVerbModel(t)
	m.tab = tabStats
	for _, it := range m.menuItems() {
		if it.Keys()[0] == "d" {
			t.Errorf("Stats menu lists d %q", it.Help().Desc)
		}
	}
}

// gotoMenuModel is a menu on a row that has Goto options.
func gotoMenuModel(t *testing.T) Model {
	t.Helper()
	m, _ := defogVerbModel(t)
	for y := m.bodyTop(); y < m.bodyTop()+m.h-chromeH; y++ {
		if n, _ := rightAt(t, m, 12, y); n.menu != nil && hasGoto(n.menu.items) {
			return n
		}
	}
	t.Fatal("no row opened a menu with g goto ›")
	return m
}

// g is the last item, a rule is drawn right above it, and the box height counts the rule.
func TestMenuListsGotoBelowARule(t *testing.T) {
	m := gotoMenuModel(t)
	c := m.menu
	last := len(c.items) - 1
	if k := c.items[last].Keys()[0]; k != "g" || c.items[last].Help().Desc != "goto ›" {
		t.Fatalf("last item %v", menuKeys(c))
	}
	if c.mh != len(c.items)+1+2 {
		t.Errorf("box height %d, want items+rule+border", c.mh)
	}
	lines := c.view()
	if len(lines) != c.mh {
		t.Fatalf("%d drawn lines, height %d", len(lines), c.mh)
	}
	if rule := ansi.Strip(lines[len(lines)-3]); !strings.HasPrefix(rule, "├─") || !strings.HasSuffix(rule, "─┤") {
		t.Errorf("line above g is %q, want the rule", rule)
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-2]), "goto ›") {
		t.Errorf("g not drawn below the rule: %q", ansi.Strip(lines[len(lines)-2]))
	}
	if c.y+c.mh > m.h {
		t.Errorf("box bottom %d off a %d-row screen", c.y+c.mh, m.h)
	}
}

// Running g by click, enter and key opens the Goto menu as pressing g on the row does.
func TestMenuRunsGotoLikeTheKey(t *testing.T) {
	m := gotoMenuModel(t)
	g := len(m.menu.items) - 1
	plain := m
	plain.menu = nil
	want := press(t, plain, runeKey("g"))
	if len(want.modals) == len(plain.modals) {
		t.Fatal("g key opened no menu")
	}
	click := tea.MouseMsg{X: m.menu.x + 2, Y: m.menu.y + 1 + g + m.menu.rules, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	m.menu.cur = g
	for name, act := range map[string]tea.Msg{"click": click, "key": runeKey("g"), "enter": tea.KeyMsg{Type: tea.KeyEnter}} {
		got, cmd := send(t, m, act)
		got = settle(t, got, cmd)
		if got.menu != nil || len(got.modals) != len(want.modals) {
			t.Errorf("%s: menu %v, modals %d want %d", name, got.menu != nil, len(got.modals), len(want.modals))
		}
	}
}

// Motion over the rule keeps the highlight, a click on it closes the menu, j from the last verb lands on g.
func TestMenuRuleTakesNoHighlight(t *testing.T) {
	m := gotoMenuModel(t)
	c := m.menu
	ruleY := c.y + 1 + c.ruleRow()
	if c.itemAt(c.x+2, ruleY) != -1 {
		t.Fatal("the rule maps to an item")
	}
	if c.itemAt(c.x+2, ruleY+1) != len(c.items)-1 {
		t.Errorf("row below the rule maps to %d, want g", c.itemAt(c.x+2, ruleY+1))
	}
	got, _ := send(t, m, tea.MouseMsg{X: c.x + 2, Y: ruleY, Action: tea.MouseActionMotion})
	if got.menu == nil || got.menu.cur != c.cur {
		t.Errorf("motion over the rule moved the highlight")
	}
	m.menu.cur = len(c.items) - 2
	if got := press(t, m, runeKey("j")); got.menu == nil || got.menu.cur != len(c.items)-1 {
		t.Errorf("j from the last verb did not land on g")
	}
	got, _ = send(t, m, tea.MouseMsg{X: c.x + 2, Y: ruleY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if got.menu != nil {
		t.Errorf("click on the rule left the menu open")
	}
}

// A menu with no Goto options draws no rule.
func TestMenuWithoutGotoHasNoRule(t *testing.T) {
	m := gotoMenuModel(t)
	c := *m.menu
	c.items = c.items[:len(c.items)-1]
	c.rules, c.mh = 0, c.mh-2
	for _, l := range c.view() {
		if strings.Contains(ansi.Strip(l), "├") {
			t.Errorf("rule drawn without g: %q", ansi.Strip(l))
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// stepMenu opens the menu of the first bead row whose priority is prio and has step items.
func stepMenu(t *testing.T, m Model, prio int) Model {
	t.Helper()
	for y := m.bodyTop(); y < m.bodyTop()+m.h-chromeH; y++ {
		if n, _ := rightAt(t, m, 12, y); n.menu != nil && n.rowBead().Priority == prio && n.menu.hasStep() {
			return n
		}
	}
	t.Fatalf("no P%d bead row opened a menu with step items", prio)
	return m
}

func menuItemClick(m Model, i int) tea.MouseMsg {
	return tea.MouseMsg{X: m.menu.x + 2, Y: m.menu.y + 1 + i, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func stepIndex(c *ctxMenu, k string) int {
	return slices.IndexFunc(c.items, func(b key.Binding) bool { return b.Help().Key == k })
}

// +/- prio gives way to raise and lower in its rank slot, naming no P-value; none are left from +/- itself.
func TestMenuListsRaiseAndLowerForPrio(t *testing.T) {
	m, _ := defogVerbModel(t)
	m = stepMenu(t, m, 1)
	c := m.menu
	r, l := stepIndex(c, "+"), stepIndex(c, "-")
	if r < 0 || l != r+1 {
		t.Fatalf("items %v, want + then - adjacent", menuKeys(c))
	}
	if c.items[r].Help().Desc != "raise prio" || c.items[l].Help().Desc != "lower prio" {
		t.Errorf("labels %q %q", c.items[r].Help().Desc, c.items[l].Help().Desc)
	}
	if stepIndex(c, "+/-") >= 0 {
		t.Errorf("+/- prio still listed: %v", menuKeys(c))
	}
}

// At P0 there is no raise, at P4 no lower.
func TestMenuStepItemsStopAtTheBounds(t *testing.T) {
	m, _ := defogVerbModel(t)
	for _, tc := range []struct {
		p       int
		raise   bool
		lower   bool
		prepare func(Model) Model
	}{{0, false, true, nil}, {4, true, false, nil}} {
		n := m
		n.beads = slices.Clone(m.beads)
		for i := range n.beads {
			if n.beads[i].Status != "closed" && n.beads[i].Priority == 1 {
				n.beads[i].Priority = tc.p
			}
		}
		n = stepMenu(t, n, tc.p)
		if got := stepIndex(n.menu, "+") >= 0; got != tc.raise {
			t.Errorf("P%d raise listed=%v", tc.p, got)
		}
		if got := stepIndex(n.menu, "-") >= 0; got != tc.lower {
			t.Errorf("P%d lower listed=%v", tc.p, got)
		}
	}
}

// A click on a step item and its key both write the priority and leave the same menu open, highlight kept; repeated steps repeat.
func TestMenuStepStaysOpen(t *testing.T) {
	m, calls := defogVerbModel(t)
	m = stepMenu(t, m, 2)
	c0 := *m.menu
	r := stepIndex(m.menu, "+")
	for name, act := range map[string]tea.Msg{"click": menuItemClick(m, r), "key": runeKey("+")} {
		*calls = nil
		n, cmd := send(t, m, act)
		n = settle(t, n, cmd)
		if n.menu == nil || n.menu.bead != c0.bead || n.menu.x != c0.x || n.menu.y != c0.y {
			t.Fatalf("%s: menu %+v, want open at %d,%d on %s", name, n.menu, c0.x, c0.y, c0.bead)
		}
		if !slices.Contains(*calls, "updateBead "+c0.bead+" [--priority 1]") {
			t.Fatalf("%s: calls %v", name, *calls)
		}
		if n.menu.items[n.menu.cur].Help().Key != "+" {
			t.Errorf("%s: highlight on %q", name, n.menu.items[n.menu.cur].Help().Key)
		}
		// again: steps from P1 to P0, where raise drops out and the highlight clamps.
		*calls = nil
		n, cmd = send(t, n, menuItemClick(n, r))
		n = settle(t, n, cmd)
		if !slices.Contains(*calls, "updateBead "+c0.bead+" [--priority 0]") {
			t.Fatalf("%s: second step calls %v", name, *calls)
		}
		if n.menu == nil || stepIndex(n.menu, "+") >= 0 || n.menu.cur >= len(n.menu.items) || n.menu.mh != c0.mh-1 {
			t.Errorf("%s: at P0 menu %v cur %d", name, menuKeys(n.menu), n.menu.cur)
		}
	}
}

// esc, a click outside, another key and a non-step item close the menu; a step key does not.
func TestMenuStepClosesOnOtherInput(t *testing.T) {
	m, _ := defogVerbModel(t)
	m = stepMenu(t, m, 2)
	nonStep := slices.IndexFunc(m.menu.items, func(b key.Binding) bool { return !isStepItem(b) && b.Help().Key != "g" })
	cases := map[string]tea.Msg{
		"esc":     tea.KeyMsg{Type: tea.KeyEsc},
		"other":   runeKey("Q"),
		"outside": tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		"nonstep": menuItemClick(m, nonStep),
	}
	for name, act := range cases {
		n, cmd := send(t, m, act)
		n = settle(t, n, cmd)
		if n.menu != nil {
			t.Errorf("%s left the menu open", name)
		}
	}
}

// A refresh that removes the menu's bead closes it.
func TestMenuStepClosesWhenBeadVanishes(t *testing.T) {
	m, _ := defogVerbModel(t)
	m = stepMenu(t, m, 2)
	id := m.menu.bead
	var rest []bd.Bead
	for _, b := range m.beads {
		if b.ID != id {
			rest = append(rest, b)
		}
	}
	m, _ = send(t, m, loadedMsg{beads: rest, live: map[string]bool{}, sessions: m.sessions})
	if m.menu != nil {
		t.Errorf("menu stayed open on a gone bead")
	}
}
