package tui

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/session"
)

// send routes through Update, unlike press (updateKey only), so the open
// reference gets the keys.
func send(t *testing.T, m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func runeKey(k string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)} }

// ? opens the tab's whole key reference in the body, tab bar and help line kept,
// every line inside the 40-col floor (ADR 0058 §4).
func TestHelpOverlayPerTab(t *testing.T) {
	common := []string{"q/ctrl+c", "←/→", "shift+tab", "j/k/↓/↑", "G", "I", "S", "H", "L", "o", "F5", "?"}
	want := map[tab][]string{
		tabGraph:    {"tab", "space/enter", "h", "n", "u", "f", "/", "esc", "l", "b", "t", "p", "x", "e", "E"},
		tabInbox:    {"tab", "n", "u", "f", "/", "esc", "l", "b", "x", "e", "E"},
		tabSearch:   {"tab", "enter", "/", "esc"},
		tabHistory:  {"enter/G"},
		tabLogs:     {"tab", "d", "e", "esc"},
		tabSettings: {"+/-", "enter", "esc"},
	}
	for tb, own := range want {
		m := pausedGraph(t)
		m.tab, m.w, m.h = tb, floorCols, 40
		m, _ = send(t, m, runeKey("?"))
		if !m.help {
			t.Fatalf("%v: ? did not open the reference", tb)
		}
		v := m.View()
		lines := strings.Split(v, "\n")
		if !strings.Contains(lines[0], "loop") || !strings.Contains(v, "close") {
			t.Errorf("%v: tab bar or help line lost:\n%s", tb, v)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > floorCols {
				t.Errorf("%v: line %q is %d wide", tb, l, lipgloss.Width(l))
			}
		}
		for _, k := range append(common, own...) {
			found := false
			for _, l := range lines[1:] {
				if strings.HasPrefix(l, k+" ") {
					found = true
				}
			}
			if !found {
				t.Errorf("%v: reference lacks %q:\n%s", tb, k, v)
			}
		}
	}
}

// ?/esc/q close it and q does not quit; every other key is swallowed.
func TestHelpOverlayKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{runeKey("?"), {Type: tea.KeyEsc}, runeKey("q")} {
		m := pausedGraph(t)
		m, _ = send(t, m, runeKey("?"))
		m, cmd := send(t, m, k)
		if m.help || cmd != nil {
			t.Errorf("%q: help=%v cmd=%v, want closed with no command", k.String(), m.help, cmd != nil)
		}
	}
	m := pausedGraph(t)
	m.selGraph = "ext"
	m.sessions = map[string]session.Info{"ext": {State: session.Exited}}
	m, _ = send(t, m, runeKey("?"))
	for _, k := range []tea.KeyMsg{runeKey("t"), runeKey("l"), runeKey("x"), runeKey("d"), runeKey("h"), runeKey("L"), {Type: tea.KeyRight}, {Type: tea.KeyCtrlC}, {Type: tea.KeyEnter}} {
		got, cmd := send(t, m, k)
		if cmd != nil || !got.help || got.tab != m.tab || got.editMode != editNone || got.hideClosedRoots != m.hideClosedRoots || got.status != m.status {
			t.Errorf("%q leaked through the reference: cmd=%v help=%v tab=%v mode=%v", k.String(), cmd != nil, got.help, got.tab, got.editMode)
		}
	}
}

// j/k scroll the reference on a short frame, clamped at both ends.
func TestHelpOverlayScroll(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h = floorCols, floorRows
	m, _ = send(t, m, runeKey("?"))
	first := strings.Split(m.View(), "\n")[1]
	for range 50 {
		m, _ = send(t, m, runeKey("j"))
	}
	if want := len(helpKeys(tabGraph, m.prefix)) - m.paneHeight(); m.helpOffset != want || want <= 0 {
		t.Fatalf("offset after j×50 = %d, want %d", m.helpOffset, want)
	}
	if strings.Split(m.View(), "\n")[1] == first {
		t.Error("j did not scroll the reference")
	}
	for range 50 {
		m, _ = send(t, m, runeKey("k"))
	}
	if m.helpOffset != 0 || strings.Split(m.View(), "\n")[1] != first {
		t.Errorf("k back to the top: offset %d", m.helpOffset)
	}
}

// ? is a text rune in the edit modes, never the reference.
func TestHelpIgnoredWhileEditing(t *testing.T) {
	m := New(nil, nil)
	m.editMode, m.editID = editTitle, "x"
	m, _ = send(t, m, runeKey("?"))
	if m.help || m.editBuf.text != "?" {
		t.Errorf("? in editTitle: help=%v buf=%q, want it typed", m.help, m.editBuf.text)
	}
}

// The footer keeps ? and q at the floor, a sessioned bead's keys included.
func TestFooterHelpKeyFloor(t *testing.T) {
	for _, sess := range []bool{false, true} {
		for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats, tabSettings} {
			m := pausedGraph(t)
			m.selGraph, m.tab, m.w = "ext", tb, floorCols
			if sess {
				m.sessions = map[string]session.Info{"ext": {State: session.Exited}}
			}
			got := m.footer()
			if lipgloss.Width(got) > floorCols || !(strings.HasSuffix(got, "? · q") || strings.HasSuffix(got, "? q")) {
				t.Errorf("session=%v %v: footer %q (%d wide), want ? and q kept", sess, tb, got, lipgloss.Width(got))
			}
		}
	}
	m := pausedGraph(t)
	if got := m.footer(); !strings.Contains(got, "/ search · ? · q") {
		t.Errorf("wide footer %q, want ? next to q and nothing dropped", got)
	}
}

// Every key updateKey handles appears in some tab's reference, so the hand-kept
// table can't silently drift from Update (ADR 0058 §4).
func TestHelpCoversUpdateKey(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "tui.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats, tabSettings} {
		for _, k := range helpKeys(tb, "ctrl+b") {
			listed[k.k] = true // a bare "/" splits to nothing
			for _, s := range strings.Split(k.k, "/") {
				listed[s] = true
			}
		}
	}
	pretty := map[string]string{" ": "space", "left": "←", "right": "→", "down": "↓", "up": "↑", "f5": "F5"}
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "updateKey" {
			return true
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok {
					continue
				}
				k, _ := strconv.Unquote(lit.Value)
				if p, ok := pretty[k]; ok {
					k = p
				}
				if !listed[k] {
					t.Errorf("updateKey handles %q but no tab's ? reference lists it", k)
				}
			}
			return true
		})
		return false
	})
}

// ? lists the two tmux lines on every tab in the human's real prefix, read once
// at startup; a failed read falls back to tmux's default C-b (ADR 0066 §2).
func TestHelpTmuxPrefix(t *testing.T) {
	for _, c := range []struct {
		raw  string
		err  error
		want string
	}{{"C-a", nil, "ctrl+a"}, {"", errors.New("no server"), "ctrl+b"}} {
		readPrefix = func() (string, error) { return c.raw, c.err }
		t.Cleanup(func() { readPrefix = session.Prefix })
		m := New(nil, nil)
		next, _ := m.Update(m.prefixCmd()())
		m = next.(Model)
		for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats, tabSettings} {
			m.tab, m.w, m.h = tb, 120, 60
			m, _ = send(t, m, runeKey("?"))
			v := m.View()
			for _, l := range []string{c.want + " ?    tmux keys — all tmux commands work", c.want + " d    leave tmux — quits mugicrew"} {
				if !strings.Contains(v, l) {
					t.Errorf("prefix %q %v: reference lacks %q:\n%s", c.raw, tb, l, v)
				}
			}
			m, _ = send(t, m, runeKey("?"))
		}
	}
}

// Spec FR19: Graph and Inbox list b next to l; the other tabs don't list
// it, bar Stats, where b groups (Spec FR31).
func TestHelpBackgroundRow(t *testing.T) {
	for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats, tabSettings} {
		ks := helpKeys(tb, "ctrl+b")
		i := slices.IndexFunc(ks, func(k footerKey) bool { return k.k == "b" })
		if tb == tabStats {
			if i < 0 || strings.Contains(ks[i].label, "launch") {
				t.Errorf("Stats: b row %v, want the grouping", ks[max(i, 0)])
			}
		} else if on := tb == tabGraph || tb == tabInbox; on != (i >= 0) {
			t.Errorf("%v: b row present=%v, want %v", tb, i >= 0, on)
		} else if on && (ks[i].label != "launch or resume without switching" || ks[i-1].k != "l") {
			t.Errorf("%v: b row %q after %q, want it next to l", tb, ks[i].label, ks[i-1].k)
		}
	}
}

// Spec FR6: Inbox's ? lists G as the reveal; Graph and Logs keep the tab jump.
func TestHelpInboxReveal(t *testing.T) {
	for tb, want := range map[tab]string{tabInbox: "Graph, revealing the selection", tabGraph: "Graph tab", tabLogs: "Graph tab"} {
		var got []string
		for _, k := range helpKeys(tb, "ctrl+b") {
			if k.k == "G" {
				got = append(got, k.label)
			}
		}
		if len(got) != 1 || got[0] != want {
			t.Errorf("%v: G rows %q, want just %q", tb, got, want)
		}
	}
}
