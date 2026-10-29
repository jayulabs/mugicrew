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
			if i > 0 && hintOf(it).rank < hintOf(next.menu.items[i-1]).rank {
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
			via, cmd := send(t, base, tea.MouseMsg{X: base.menu.x + 2, Y: base.menu.y + 1 + i, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			via = settle(t, via, cmd)
			if via.menu != nil {
				t.Fatalf("%s: menu stayed open after running", k)
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
