package tui

import (
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const mrURL = "https://gl.example/g/p/-/merge_requests/7"

// mrRowModel is a gitlab Execute tab with the cursor on Watch id.
func mrRowModel(t *testing.T, id string) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	stubActions(t)
	m := sized(t, 160, 40)
	m.landName = "gitlab"
	m, _ = send(t, m, loadedMsg{beads: mrGraph("mergeable").beads, live: map[string]bool{}})
	return execAt(t, press(t, m, runeKey("3")), mrsPrefix+id)
}

// runW presses w and runs the returned Cmd's launch, feeding its message back.
func runW(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := send(t, m, runeKey("w"))
	if cmd == nil {
		return m
	}
	if msg, ok := cmd().(browserDoneMsg); ok {
		m, cmd = send(t, m, msg)
		if cmd != nil {
			cmd()
		}
	}
	return m
}

func stubLaunch(t *testing.T, fail func(argv []string) error) *[][]string {
	t.Helper()
	var ran [][]string
	seam(t, &runDetached, func(c *exec.Cmd) error {
		ran = append(ran, c.Args)
		if fail == nil {
			return nil
		}
		return fail(c.Args)
	})
	return &ran
}

func TestBrowserEnvOrderAndSubstitution(t *testing.T) {
	t.Setenv("BROWSER", "first %s --x::second")
	var got [][]string
	for _, c := range browserCmds(mrURL) {
		got = append(got, c.Args)
	}
	want := [][]string{
		{"sh", "-c", `first "$1" --x`, "sh", mrURL},
		{"sh", "-c", `second "$1"`, "sh", mrURL},
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("argv %q, want %q", got, want)
	}
}

func TestBrowserXdgOpenNeedsDisplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("xdg-open is linux's")
	}
	t.Setenv("BROWSER", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if c := browserCmds(mrURL); len(c) != 0 {
		t.Errorf("no display: %q", c[0].Args)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if c := browserCmds(mrURL); len(c) != 1 || !slices.Equal(c[0].Args, []string{"xdg-open", mrURL}) {
		t.Errorf("with display: %v", c)
	}
}

func TestWOpensFirstSuccessAndStops(t *testing.T) {
	got := yankLog(t)
	t.Setenv("BROWSER", "a:b:c")
	ran := stubLaunch(t, func(a []string) error {
		if a[2] == `a "$1"` {
			return errors.New("exit 1")
		}
		return nil
	})
	m := runW(t, mrRowModel(t, "w1"))
	if len(*ran) != 2 || (*ran)[1][2] != `b "$1"` {
		t.Errorf("ran %q: the first success must stop the chain", *ran)
	}
	if m.status != "opened !7 in browser" || len(*got) != 0 {
		t.Errorf("status %q, copied %v", m.status, *got)
	}
}

func TestWFailureCopiesOSC52(t *testing.T) {
	for _, env := range []string{"a:b", ""} {
		got := yankLog(t)
		t.Setenv("BROWSER", env)
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		stubLaunch(t, func([]string) error { return errors.New("exit 1") })
		m := runW(t, mrRowModel(t, "w1"))
		if m.status != "couldn't open a browser; copied !7's link" || !slices.Equal(*got, []string{mrURL}) {
			t.Errorf("BROWSER=%q: status %q, copied %v", env, m.status, *got)
		}
	}
}

func TestWWithoutLinkRunsNothing(t *testing.T) {
	got := yankLog(t)
	ran := stubLaunch(t, nil)
	t.Setenv("BROWSER", "x")
	m := runW(t, mrRowModel(t, "w2"))
	if len(*ran) != 0 || len(*got) != 0 || !strings.Contains(m.status, "no link yet") {
		t.Errorf("ran %v copied %v status %q", *ran, *got, m.status)
	}
}

func TestWOffAnMRRowIsNotOpen(t *testing.T) {
	ran := stubLaunch(t, nil)
	t.Setenv("BROWSER", "x")
	m := mrRowModel(t, "w1")
	m = press(t, m, runeKey("k")) // the section header
	if _, cmd := send(t, m, runeKey("w")); cmd != nil && len(*ran) != 0 {
		t.Error("w ran off an MR row")
	}
}

func TestEnterOnMRRowUnchanged(t *testing.T) {
	ran := stubLaunch(t, nil)
	m := mrRowModel(t, "w1")
	before := m.tabAt(tabExecute).(execTab).g.enterVerb(m.tabAt(tabExecute).(execTab).selected())
	_, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(*ran) != 0 {
		t.Errorf("enter launched a browser (verb %q, cmd %v)", before, cmd)
	}
}

// defogMRRowModel is a gitlab Defog tab with the cursor on the Chart Watch id.
func defogMRRowModel(t *testing.T, id string) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	stubActions(t)
	m := sized(t, 160, 40)
	m.landName = "gitlab"
	g := mrGraph("mergeable")
	g.beads[g.beadIndex("w3")].Description = "MR: !11\nMR link: " + mrURL
	m, _ = send(t, m, loadedMsg{beads: g.beads, live: map[string]bool{}})
	m = press(t, m, runeKey("2"))
	d := m.tabAt(tabDefog).(defogTab)
	d.sel = mrsPrefix + id
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabDefog] = d
	return m
}

func TestDefogWOpensChartMR(t *testing.T) {
	yankLog(t)
	t.Setenv("BROWSER", "a")
	ran := stubLaunch(t, nil)
	m := runW(t, defogMRRowModel(t, "w3"))
	if len(*ran) != 1 || m.status != "opened !11 in browser" {
		t.Errorf("ran %q status %q", *ran, m.status)
	}
}

func TestDefogWFailureCopiesOSC52(t *testing.T) {
	got := yankLog(t)
	t.Setenv("BROWSER", "a")
	stubLaunch(t, func([]string) error { return errors.New("exit 1") })
	m := runW(t, defogMRRowModel(t, "w3"))
	if m.status != "couldn't open a browser; copied !11's link" || !slices.Equal(*got, []string{mrURL}) {
		t.Errorf("status %q, copied %v", m.status, *got)
	}
}

func TestDefogWWithoutLinkRunsNothing(t *testing.T) {
	got := yankLog(t)
	ran := stubLaunch(t, nil)
	t.Setenv("BROWSER", "x")
	m := runW(t, defogMRRowModel(t, "w4"))
	if len(*ran) != 0 || len(*got) != 0 || !strings.Contains(m.status, "no link yet") {
		t.Errorf("ran %v copied %v status %q", *ran, *got, m.status)
	}
}

func TestDefogWHintOnlyOnMRRows(t *testing.T) {
	hasW := func(m Model) bool {
		for _, b := range m.tabAt(tabDefog).(defogTab).bindings() {
			if b.Keys()[0] == "w" {
				return true
			}
		}
		return false
	}
	m := defogMRRowModel(t, "w3")
	if !hasW(m) {
		t.Error("w missing from the hints on an MR row")
	}
	d := m.tabAt(tabDefog).(defogTab)
	d.sel = mrsKey
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabDefog] = d
	if hasW(m) {
		t.Error("w hinted on the section header")
	}
}
