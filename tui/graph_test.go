package tui

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// stubBeads is a nested Initiative with a Chart (a paused and an in-flight member,
// one with a long description) and a closed root Chart with a closed member.
func stubBeads() []bd.Bead {
	long := strings.Repeat("A long line of description that wraps in the pane. ", 40)
	return []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship v2", Created: epoch},
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Graph tab — chart", Parent: "ini", Created: epoch},
		{ID: "paused", Kind: contract.KindBuild, Status: "open", Title: "Paused build", Parent: "ch", Labels: []string{"paused:manual"}, Created: epoch.Add(3 * time.Hour)},
		{ID: "run", Kind: contract.KindBuild, Status: "in_progress", Title: "In flight build", Parent: "ch", Started: time.Now().Add(-2 * time.Hour), Created: epoch.Add(2 * time.Hour)},
		{ID: "long", Kind: contract.KindBuild, Status: "open", Title: "Long description", Parent: "ch", Description: long, Priority: 2, Created: epoch.Add(time.Hour)},
		{ID: "old", Kind: contract.KindChart, Container: true, Status: "closed", Title: "Old chart", Created: epoch},
		{ID: "oldb", Kind: contract.KindBuild, Status: "closed", Title: "Old closed build", Parent: "old", Created: epoch},
	}
}

// graphModel is a Model on the Graph tab loaded with stubBeads.
func graphModel(t *testing.T, w, h int) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	m := sized(t, w, h)
	m.launching = map[string]bool{"run": true}
	m, _ = send(t, m, loadedMsg{beads: stubBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	return m
}

// press sends a key and settles any status it returns.
func press(t *testing.T, m Model, k tea.KeyMsg) Model {
	t.Helper()
	m, cmd := send(t, m, k)
	return settle(t, m, cmd)
}

func sel(m Model) string { return m.tabAt(tabGraph).(graphTab).sel }

func selectBead(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 20 {
		if sel(m) == id {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no row %s; at %s", id, sel(m))
	return m
}

func TestGraphRendersV1Tree(t *testing.T) {
	v := ansi.Strip(graphModel(t, 120, 30).View())
	for _, want := range []string{
		"▾", "INITIATIVE", "Ship v2", "landed 0/1", // the Initiative head with its progress
		"CHART", "Graph tab", "0/3", // the nested head, its closed/total
		"BUILD", "Paused build", "‖", // pause glyph
		"In flight build", "launching…", "◐", // in-flight marker and status glyph
	} {
		if !strings.Contains(v, want) {
			t.Errorf("View lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Old chart") {
		t.Errorf("the default shows a closed root:\n%s", v)
	}
	if i, j := strings.Index(v, "Graph tab"), strings.Index(v, "Paused build"); i > j {
		t.Errorf("the nested Chart's members render under it")
	}
}

func TestGraphDefaultFoldAndSeed(t *testing.T) {
	m := graphModel(t, 120, 30)
	if v := ansi.Strip(m.View()); strings.Contains(v, "Old chart") {
		t.Errorf("the default hides the closed root:\n%s", v)
	}
	if m = press(t, m, runeKey("H")); strings.Contains(ansi.Strip(m.View()), "Old chart") || strings.Contains(m.status, "closed root") {
		t.Errorf("H must be inert:\n%s", m.View())
	}
	if bar := lines(m.View()); strings.Contains(bar[len(bar)-1], "h closed") {
		t.Errorf("h is still hinted: %q", bar[len(bar)-1])
	}
	// the closed-root seed holds, and a query's expand-all leaves it be.
	if !gr(m).folds()["old"] {
		t.Error("closed root group not seeded folded")
	}
	// space on a leaf does nothing
	m = selectBead(t, m, "paused")
	before := m.View()
	if m = press(t, m, tea.KeyMsg{Type: tea.KeySpace}); m.View() != before {
		t.Error("space on a leaf changed the frame")
	}
	// space folds the open Initiative.
	m = selectBead(t, m, "ini")
	m = press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.View()); strings.Contains(v, "Paused build") || strings.Contains(v, "0/3") {
		t.Errorf("space should fold the Initiative:\n%s", v)
	}
	// a refresh keeps the folds the human made, and the seed is not re-applied.
	m, _ = send(t, m, loadedMsg{beads: stubBeads()})
	if v := ansi.Strip(m.View()); strings.Contains(v, "Paused build") || !gr(m).folds()["old"] {
		t.Errorf("a refresh must keep the folds:\n%s", v)
	}
}

func TestGraphSelectionSurvivesRefresh(t *testing.T) {
	m := graphModel(t, 120, 30)
	m = selectBead(t, m, "long")
	more := append([]bd.Bead{{ID: "new", Kind: contract.KindBuild, Status: "open", Title: "Newest", Parent: "ch", Priority: 0, Created: epoch.Add(24 * time.Hour)}}, stubBeads()...)
	m, _ = send(t, m, loadedMsg{beads: more})
	if got := sel(m); got != "long" {
		t.Errorf("selection after refresh = %q, want long", got)
	}
	m, _ = send(t, m, loadedMsg{beads: stubBeads()[:2]}) // the selected bead vanished
	if got := sel(m); got == "long" || got == "" {
		t.Errorf("a vanished selection should fall to a row, got %q", got)
	}
}

func TestGraphSelectionHighlightedFullWidth(t *testing.T) {
	m := graphModel(t, 120, 30)
	var marked []string
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "❯") {
			marked = append(marked, l)
		}
	}
	if len(marked) != 1 || lipgloss.Width(marked[0]) != 120 {
		t.Errorf("want one selected line padded to 120, got %q", marked)
	}
}

func TestGraphMoveKeys(t *testing.T) {
	m := graphModel(t, 120, 30)
	first := sel(m)
	for _, k := range []tea.KeyMsg{runeKey("j"), {Type: tea.KeyDown}} {
		m = press(t, m, k)
	}
	second := sel(m)
	if first == second {
		t.Fatal("j/↓ did not move")
	}
	for _, k := range []tea.KeyMsg{runeKey("k"), {Type: tea.KeyUp}} {
		m = press(t, m, k)
	}
	if sel(m) != first {
		t.Errorf("k/↑ did not move back: %q", sel(m))
	}
	if m = press(t, m, runeKey("k")); sel(m) == first { // wraps
		t.Error("k at the top should wrap")
	}
}

func TestGraphDetailPane(t *testing.T) {
	m := graphModel(t, 120, 44)
	m = selectBead(t, m, "long")
	if strings.Contains(ansi.Strip(m.View()), "ON LAUNCH") {
		t.Fatal("detail shown before d")
	}
	m = press(t, m, runeKey("d"))
	v := ansi.Strip(m.View())
	for _, want := range []string{"○· BUILD P2 · Long description · long  AFK", "A long line of description"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "│") {
		t.Error("two-pane split lacks its border")
	}
	body := func(m Model) string { return strings.Join(lines(m.View())[4:], "\n") }
	top := body(m)
	for _, k := range []tea.KeyMsg{{Type: tea.KeyPgDown}, {Type: tea.KeyCtrlD}} {
		next := press(t, m, k)
		if body(next) == top || sel(next) != "long" {
			t.Errorf("%v should scroll the detail and keep the selection", k)
		}
		if back := press(t, next, tea.KeyMsg{Type: tea.KeyPgUp}); body(back) == body(next) {
			t.Error("pgup did not scroll back")
		}
	}
	// ctrl+u scrolls up from a scrolled pane
	down := press(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if body(press(t, down, tea.KeyMsg{Type: tea.KeyCtrlU})) == body(down) {
		t.Error("ctrl+u did not scroll")
	}
	// j/k still move the selection, the detail follows and starts at the top
	down = press(t, down, runeKey("k"))
	if sel(down) == "long" || strings.Contains(ansi.Strip(down.View()), "A long line of description") {
		t.Errorf("k should move and the detail follow:\n%s", ansi.Strip(down.View()))
	}
	back := press(t, down, runeKey("j"))
	if sel(back) != "long" || body(back) != top {
		t.Error("coming back to a bead should show its detail from the top")
	}
	// d and esc both close
	for _, k := range []tea.KeyMsg{runeKey("d"), {Type: tea.KeyEsc}} {
		if strings.Contains(ansi.Strip(press(t, m, k).View()), "ON LAUNCH") || strings.Contains(ansi.Strip(press(t, m, k).View()), "AFK — build") {
			t.Errorf("%v should close the detail", k)
		}
	}
	// the scroll keys do nothing once it is closed
	closed := press(t, m, runeKey("d"))
	if press(t, closed, tea.KeyMsg{Type: tea.KeyPgDown}).View() != closed.View() {
		t.Error("pgdn with the detail closed changed the frame")
	}
}

func TestGraphDetailShowsPauseAndLaunchPreview(t *testing.T) {
	m := graphModel(t, 120, 40)
	m = selectBead(t, m, "paused")
	m = press(t, m, runeKey("d"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "paused") || !strings.Contains(v, "manual") {
		t.Errorf("detail lacks the pause line:\n%s", v)
	}
	m = selectBead(t, m, "ch")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "CONTAINER") {
		t.Errorf("detail lacks the launch preview:\n%s", v)
	}
}

// At every width the open detail is a bottom drawer: the list's top half, a ─ rule,
// then the detail, with no [i/n] suffix (ADR 0171 §2).
func TestGraphDrawer(t *testing.T) {
	for _, w := range []int{83, 84, 120} {
		m := press(t, selectBead(t, graphModel(t, w, 30), "long"), runeKey("d"))
		ls := lines(ansi.Strip(m.View()))
		rule := -1
		for i, l := range ls {
			if strings.HasPrefix(l, strings.Repeat("─", w)) {
				rule = i
			}
		}
		v := strings.Join(ls, "\n")
		if rule < 0 || strings.Contains(v, "[5/") {
			t.Fatalf("w=%d: no rule or has [i/n]:\n%s", w, v)
		}
		top, bottom := strings.Join(ls[:rule], "\n"), strings.Join(ls[rule:], "\n")
		if !strings.Contains(top, "└   ○· BUILD P2 · Long description · long") {
			t.Errorf("w=%d: selected row not visible in the list:\n%s", w, v)
		}
		if !strings.Contains(bottom, "○· BUILD P2 · Long description · long") {
			t.Errorf("w=%d: detail below the rule missing:\n%s", w, v)
		}
		if !strings.Contains(ansi.Strip(press(t, m, runeKey("d")).View()), "Paused build") {
			t.Errorf("w=%d: closing restores the list", w)
		}
	}
}

// The frame fits every size from the floor up, with the detail open or shut.
func TestGraphFrameFits(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {83, 24}, {84, 24}, {200, 50}} {
		for _, open := range []bool{false, true} {
			m := graphModel(t, sz[0], sz[1])
			if open {
				m = press(t, m, runeKey("d"))
			}
			ls := lines(m.View())
			if len(ls) != sz[1] {
				t.Fatalf("%dx%d open=%v: %d lines", sz[0], sz[1], open, len(ls))
			}
			for _, l := range ls {
				if lipgloss.Width(l) > sz[0] {
					t.Errorf("%dx%d open=%v: %q is %d wide", sz[0], sz[1], open, l, lipgloss.Width(l))
				}
			}
		}
	}
}

func TestGraphHintsFollowSelection(t *testing.T) {
	bar := func(m Model) string { l := lines(m.View()); return l[len(l)-1] }
	m := graphModel(t, 200, 40) // lands on the Initiative head
	if b := bar(m); !strings.Contains(b, "space fold") || !strings.Contains(b, "d detail") || !strings.HasSuffix(strings.TrimRight(b, " "), "? keys") {
		t.Errorf("header hints = %q", b)
	}
	if b := bar(selectBead(t, m, "run")); strings.Contains(b, "space") || !strings.Contains(b, "d detail") {
		t.Errorf("leaf hints = %q", b)
	}
	open := press(t, m, runeKey("d"))
	if b := bar(open); !strings.Contains(b, "d close") || strings.Contains(b, "pgup/pgdn") || strings.Contains(b, "d detail") {
		t.Errorf("detail-open hints = %q", b)
	}
	if b := bar(m); strings.Contains(b, "j/k") {
		t.Errorf("move keys are not hints: %q", b)
	}
	ref := ansi.Strip(press(t, m, runeKey("?")).View())
	if !strings.Contains(ref, "j/k") || !strings.Contains(ref, "move") {
		t.Errorf("? lacks the move keys:\n%s", ref)
	}
}

// Rendering the Graph, detail open, starts no subprocess (ADR 0101).
func TestGraphRendersNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m := graphModel(t, 120, 40)
	for _, id := range []string{"ini", "ch", "paused", "run"} {
		m = press(t, selectBead(t, m, id), runeKey("d"))
		m.View()
		m = press(t, m, runeKey("d"))
		m.View()
	}
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("Graph rendering started subprocesses:\n%s", got)
	}
}

// Every key the Graph handles is bound and every bound key handled, on a header,
// a leaf and with the detail open (FR30).
func TestGraphBindingCoverage(t *testing.T) {
	for name, m := range map[string]Model{
		"header":      graphModel(t, 120, 30),
		"leaf":        selectBead(t, graphModel(t, 120, 30), "long"),
		"detail":      scrolled(t, graphModel(t, 120, 30)),
		"single pane": scrolled(t, graphModel(t, 60, 30)),
	} {
		bound := map[string]bool{}
		for _, b := range m.modeBindings() {
			for _, k := range b.Keys() {
				bound[k] = true
			}
		}
		for _, k := range candidateKeys() {
			next, cmd := send(t, m, k)
			handled := cmd != nil || next.View() != m.View()
			if handled != bound[k.String()] && k.String() != "4" {
				t.Errorf("%s: key %q handled=%v bound=%v", name, k, handled, bound[k.String()])
			}
		}
	}
}

// scrolled opens the long bead's detail and scrolls it half a page, so every scroll
// key has room to move.
func scrolled(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, selectBead(t, m, "long"), runeKey("d"))
	return press(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
}

// A leaf title's style follows closed > hot > AFK mute > normal; headers stay
// plain, and only the title is muted.
func TestGraphMutesAFKLeafTitles(t *testing.T) {
	forceANSI256(t)
	beads := []bd.Bead{
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Header chart", Created: epoch},
		{ID: "sel", Kind: contract.KindTask, Status: "open", Title: "Selected hitl", Parent: "ch", BlockedBy: []string{"blk", "ch2"}, Created: epoch},
		{ID: "ch2", Kind: contract.KindChart, Container: true, Status: "open", Title: "Blocking chart", Created: epoch},
		{ID: "afk", Kind: contract.KindBuild, Status: "open", Title: "Afk open", Parent: "ch", Created: epoch.Add(time.Hour)},
		{ID: "hitl", Kind: contract.KindTask, Status: "open", Title: "Hitl open", Parent: "ch", Created: epoch.Add(2 * time.Hour)},
		{ID: "cafk", Kind: contract.KindBuild, Status: "closed", Title: "Afk closed", Parent: "ch", Created: epoch.Add(3 * time.Hour)},
		{ID: "chitl", Kind: contract.KindTask, Status: "closed", Title: "Hitl closed", Parent: "ch", Created: epoch.Add(4 * time.Hour)},
		{ID: "blk", Kind: contract.KindBuild, Status: "open", Title: "Afk blocker", Parent: "ch", Created: epoch.Add(5 * time.Hour)},
		{ID: "pz", Kind: contract.KindBuild, Status: "open", Title: "Afk paused", Parent: "ch", Labels: []string{"paused:manual"}, Created: epoch.Add(6 * time.Hour)},
	}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	m = pick(t, m, "s", "any") // the default hides the closed leaves
	m = selectBead(t, m, "sel")
	v := m.View()
	for title, want := range map[string]string{
		"Afk open":       softSt.Render("Afk open"),
		"Afk closed":     closedSty.Render("Afk closed"),
		"Hitl closed":    closedSty.Render("Hitl closed"),
		"Afk blocker":    pinkFg.Render("Afk blocker"),
		"Afk paused":     softSt.Render("Afk paused"),
		"Blocking chart": pinkFg.Render("Blocking chart"),
	} {
		if !strings.Contains(v, want) {
			t.Errorf("%s: view lacks %q", title, want)
		}
	}
	if strings.Contains(v, softSt.Render("Hitl open")) || strings.Contains(v, softSt.Render("Header chart")) {
		t.Errorf("HITL leaf or header muted:\n%s", v)
	}
	if !strings.Contains(v, pauseSty["paused:manual"].Render("‖")) {
		t.Errorf("paused glyph lost its colour:\n%s", v)
	}
	m = selectBead(t, m, "afk")
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(l), "Afk open") && !strings.Contains(l, "48;5;25") {
			t.Errorf("selected muted row lost its highlight: %q", l)
		}
	}
}

func TestAgeSuffixHITLShowsAge(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindSignal, contract.KindChart, contract.KindSpec} {
		b := &bd.Bead{Kind: k, Status: "in_progress", Started: time.Now().Add(-2 * time.Hour)}
		got := ansi.Strip(ageSuffix(b))
		if strings.Contains(got, "hitl") || !strings.Contains(got, "2h") {
			t.Errorf("%s: ageSuffix = %q, want age and no hitl", k, got)
		}
	}
}

// footerRows are the blocker footer's rows in a stripped view: the !-led SBRN rows
// (the mark hugs the status glyph, unlike an in-list gutter) and the overflow row.
func footerRows(v string) []string {
	var out []string
	for _, l := range lines(ansi.Strip(v)) {
		if footRowRe.MatchString(l) {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}

var footRowRe = regexp.MustCompile(`^(![^ !]|… \+\d+ more blockers)`)

func footerBeads() []bd.Bead {
	return []bd.Bead{
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Header chart", Created: epoch},
		{ID: "sel", Kind: contract.KindTask, Status: "open", Title: "Selected hitl", Parent: "ch", BlockedBy: []string{"vis", "gone", "far", "gone-aaaaaaaaaaaa", "gone-bbbbbbbbbbbb", "gone-cccccccccccc"}, Created: epoch},
		{ID: "vis", Kind: contract.KindBuild, Status: "open", Title: "Visible blocker", Parent: "ch", Created: epoch.Add(time.Hour)},
		{ID: "far", Kind: contract.KindBuild, Status: "open", Title: "Faraway blocker with a very long title", Parent: "other", Created: epoch.Add(2 * time.Hour)},
		{ID: "gone", Kind: contract.KindBuild, Status: "open", Title: "Gone blocker", Parent: "other", Created: epoch.Add(4 * time.Hour)},
		{ID: "gone-aaaaaaaaaaaa", Kind: contract.KindBuild, Status: "open", Title: "Gone a", Parent: "other", Created: epoch.Add(5 * time.Hour)},
		{ID: "gone-bbbbbbbbbbbb", Kind: contract.KindBuild, Status: "open", Title: "Gone b", Parent: "other", Created: epoch.Add(6 * time.Hour)},
		{ID: "gone-cccccccccccc", Kind: contract.KindBuild, Status: "open", Title: "Gone c", Parent: "other", Created: epoch.Add(7 * time.Hour)},
		{ID: "other", Kind: contract.KindChart, Container: true, Status: "closed", Title: "Other chart", Created: epoch.Add(-time.Hour)},
		{ID: "free", Kind: contract.KindTask, Status: "open", Title: "Unblocked", Parent: "ch", Created: epoch.Add(3 * time.Hour)},
	}
}

func footerView(t *testing.T, w, h int, sel string, open bool) []string {
	t.Helper()
	m := sized(t, w, h)
	m, _ = send(t, m, loadedMsg{beads: footerBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	m = selectBead(t, m, sel)
	if open {
		m, _ = send(t, m, runeKey("d"))
	}
	return lines(ansi.Strip(m.View()))
}

// The footer lists only the selection's undrawn open blockers as SBRN rows; an off-screen blocker
// shows its ID; an unblocked selection gets none.
func TestGraphBlockerFooter(t *testing.T) {
	forceANSI256(t)
	foot := strings.Join(footerRows(strings.Join(footerView(t, 120, 20, "sel", false), "\n")), "\n")
	if !strings.Contains(foot, "gone") || strings.Contains(foot, "Visible blocker") || !strings.Contains(foot, "more blockers") {
		t.Errorf("footer = %q, want gone, overflow and not the drawn blocker", foot)
	}
	if rows := footerRows(strings.Join(footerView(t, 120, 20, "free", false), "\n")); len(rows) > 0 {
		t.Errorf("unblocked selection got footer %q", rows)
	}
}

// Five off-screen blockers show two SBRN rows (id and kind tag) and "… +3 more blockers".
func TestGraphBlockerFooterOverflow(t *testing.T) {
	forceANSI256(t)
	rows := footerRows(strings.Join(footerView(t, 120, 20, "sel", false), "\n"))
	if len(rows) != 3 || rows[2] != "… +3 more blockers" || !strings.HasSuffix(rows[0], "· gone") || !strings.Contains(rows[1], "BUILD") || !strings.HasSuffix(rows[1], "· far") {
		t.Errorf("footer rows = %q", rows)
	}
}

// Narrow, every footer row stays within the width.
func TestGraphBlockerFooterNarrow(t *testing.T) {
	forceANSI256(t)
	rows := footerRows(strings.Join(footerView(t, 40, 20, "sel", false), "\n"))
	if len(rows) == 0 {
		t.Fatal("no footer")
	}
	for _, l := range rows {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("footer row %q wider than 40 columns", l)
		}
	}
}

// With the drawer open the footer is the rows directly above the rule.
func TestGraphBlockerFooterAboveRule(t *testing.T) {
	forceANSI256(t)
	got := footerView(t, 80, 20, "sel", true)
	for i := len(got) - 1; i > 0; i-- {
		if strings.HasPrefix(got[i], "────") {
			if !strings.Contains(got[i-1], "more blockers") {
				t.Errorf("row above rule = %q, want footer", got[i-1])
			}
			return
		}
	}
	t.Errorf("no drawer rule in view:\n%s", strings.Join(got, "\n"))
}

// Blockers scrolled out of the window move into the footer, and the shorter
// list keeps the selection in view.
func TestGraphBlockerFooterScrolledOff(t *testing.T) {
	forceANSI256(t)
	got := footerView(t, 80, 12, "sel", false)
	if v := strings.Join(got, "\n"); !strings.Contains(v, "Selected hitl") || len(footerRows(v)) == 0 {
		t.Errorf("selection scrolled out:\n%s", v)
	}
}

// stubCommonDir points the commonDir seam at dir for the test.
func stubCommonDir(t *testing.T, dir string) {
	t.Helper()
	old := commonDir
	commonDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { commonDir = old })
}

// drawerWin shrinks the list by exactly the footer's rows, keeps the selection inside,
// keeps full height with no footer, and drops a footer that leaves no list row.
func TestDrawerFooterReservesRows(t *testing.T) {
	var ls []string
	for i := range 10 {
		ls = append(ls, fmt.Sprintf("row%d", i))
	}
	foot := func(n int) func(off, lh int) []string {
		return func(off, lh int) []string { return slices.Repeat([]string{"F"}, n) }
	}
	for _, c := range []struct{ n, h, want int }{{0, 6, 6}, {1, 6, 5}, {3, 6, 3}, {6, 6, 6}} {
		got := strings.Split(drawerWin(len(ls), func(i int) string { return ls[i] }, 9, false, 20, c.h, viewport.Model{}, foot(c.n)), "\n")
		list := 0
		for _, l := range got {
			if l != "F" {
				list++
			}
		}
		if c.n == 6 { // no room: footer dropped
			if len(got) != 6 || strings.Contains(strings.Join(got, ""), "F") {
				t.Errorf("tiny: %q", got)
			}
			continue
		}
		if list != c.want || len(got) != c.h && c.n > 0 || got[list-1] != "row9" && c.n > 0 {
			t.Errorf("n=%d: list=%d got=%q", c.n, list, got)
		}
	}
}

// A Chart's open blocker of any kind freezes its members; a Checkpoint's freezes them
// only when its kind doesn't admit a land-only hold (ADR 0268, ADR 0270).
func TestInheritedContainerHold(t *testing.T) {
	g := graphData{beads: []bd.Bead{
		{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "Prereq"},
		{ID: "piv", Kind: contract.KindPivot, Status: "open", Title: "Wrong FR"},
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", BlockedBy: []string{"sig"}},
		{ID: "survey", Kind: contract.KindSurvey, Status: "open", Parent: "ch"},
		{ID: "cpL", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: []string{"sig"}},
		{ID: "bL", Kind: contract.KindBuild, Status: "open", Parent: "cpL"},
		{ID: "cpP", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: []string{"piv"}},
		{ID: "bP", Kind: contract.KindBuild, Status: "open", Parent: "cpP"},
	}}
	for id, want := range map[string]bool{"survey": true, "bL": false, "bP": true, "ch": true} {
		if got := g.blocked(g.beadByID(id)); got != want {
			t.Errorf("blocked(%s) = %v, want %v", id, got, want)
		}
	}
	if bl := g.heldBy(g.beadByID("survey")); len(bl) != 1 || bl[0].ID != "sig" {
		t.Errorf("survey held by %v, want the Chart's Signal", bl)
	}
	if why := g.rowEnterWhy(g.beadByID("survey"), true); !strings.Contains(why, "Prereq") {
		t.Errorf("launch refusal %q doesn't name the blocker", why)
	}
}

// A blocks-edge target missing from the snapshot counts as closed: it blocks nothing
// and holds no member (the default set loads every non-closed bead).
func TestMissingBlockerIsClosed(t *testing.T) {
	g := graphData{beads: []bd.Bead{
		{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: []string{"resolved-pivot"}},
		{ID: "b", Kind: contract.KindBuild, Status: "open", Parent: "cp"},
	}}
	for _, id := range []string{"cp", "b"} {
		if b := g.beadByID(id); g.blocked(b) || len(g.heldBy(b)) != 0 {
			t.Errorf("%s: blocked=%v heldBy=%v, want neither", id, g.blocked(b), g.heldBy(b))
		}
	}
}
