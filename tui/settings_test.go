package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/loop"
)

// settingsModel is a model refreshed through load() with the pool reader and bd
// stubbed: two AFK Builds in flight, an in-progress HITL bead and an open Build
// that take no slot.
func settingsModel(t *testing.T, s loop.Setting) Model {
	t.Helper()
	origPool, origList := readPool, listAll
	t.Cleanup(func() { readPool, listAll = origPool, origList })
	readPool = func() loop.Setting { return s }
	stubAutoExit(t, loop.AutoExit{D: time.Minute, Source: "default"})
	listAll = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "b1", Kind: contract.KindBuild, Status: "in_progress", Title: "one"},
			{ID: "b2", Kind: contract.KindCritique, Status: "in_progress", Title: "two"},
			{ID: "g", Kind: contract.KindGrilling, Status: "in_progress", Title: "hitl"},
			{ID: "b3", Kind: contract.KindBuild, Status: "open", Title: "queued"},
		}, nil
	}
	m := New(nil, nil)
	m.w, m.h = 100, 24
	next, _ := m.Update(m.load()())
	return next.(Model)
}

// FR1, FR2, FR4: * jumps to Settings from any tab, the arrows cycle through it
// after Stats, and the tab bar shows *Settings.
func TestSettingsTabReachable(t *testing.T) {
	m := settingsModel(t, loop.Setting{N: 3, Source: "default"})
	if bar := ansi.Strip(strings.Split(m.View(), "\n")[0]); !strings.Contains(bar, "#Stats  *Settings") {
		t.Errorf("tab bar lacks *Settings after #Stats: %q", bar)
	}
	for _, from := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats} {
		m.tab = from
		if got, _ := send(t, m, runeKey("*")); got.tab != tabSettings {
			t.Errorf("* from %v landed on %v", from, got.tab)
		}
	}
	m.tab = tabStats
	m, _ = send(t, m, keyNamed("right"))
	if m.tab != tabSettings || !strings.Contains(plain(m.View()), "AFK pool") {
		t.Fatalf("right from Stats: tab %v, view:\n%s", m.tab, m.View())
	}
	if next, _ := send(t, m, keyNamed("right")); next.tab != tabGraph {
		t.Errorf("right from Settings landed on %v, want Graph", next.tab)
	}
	m.tab = tabGraph
	if next, _ := send(t, m, keyNamed("left")); next.tab != tabSettings {
		t.Errorf("left from Graph landed on %v, want Settings", next.tab)
	}
	if next, _ := send(t, m, keyNamed("shift+tab")); next.tab != tabSettings {
		t.Errorf("shift+tab from Graph landed on %v, want Settings", next.tab)
	}
}

// FR5–9, FR25: one row per source form, the shared value under a local override,
// the in-flight count over AFK in_progress beads only, and a dimmed gloss under it.
func TestSettingsPoolRow(t *testing.T) {
	for _, c := range []struct {
		s    loop.Setting
		want string
	}{
		{loop.Setting{N: 3, Source: "default"}, "AFK pool 3 (default) 2 in flight"},
		{loop.Setting{N: 4, Source: "config.toml"}, "AFK pool 4 (config.toml) 2 in flight"},
		{loop.Setting{N: 5, Source: "local"}, "AFK pool 5 (local) 2 in flight"},
		{loop.Setting{N: 5, Source: "local", Shared: 3}, "AFK pool 5 (local; shared 3) 2 in flight"},
		{loop.Setting{N: 4, Source: "config.toml", Unusable: "local"}, "AFK pool 4 (unusable → config.toml) 2 in flight"},
		{loop.Setting{N: 3, Source: "default", Unusable: "config.toml"}, "AFK pool 3 (unusable → default) 2 in flight"},
	} {
		m, _ := send(t, settingsModel(t, c.s), runeKey("*"))
		ls := statsLines(m)
		if ls[0] != c.want {
			t.Errorf("%+v: row %q, want %q", c.s, ls[0], c.want)
		}
		if !strings.Contains(ls[1], "AFK efforts") {
			t.Errorf("%+v: no gloss under the row: %q", c.s, ls[1])
		}
	}
}

// FR35: keys other tabs handle are dropped on Settings; +/- never reach a bead's priority.
func TestSettingsKeysScoped(t *testing.T) {
	m := pausedGraph(t)
	m.selInbox = "ext"
	m.pool = loop.Setting{N: 3, Source: "default"}
	m, _ = send(t, m, runeKey("*"))
	before := m.View()
	for _, k := range []string{"l", "b", "t", "p", "x", "e", "E", "n", "u", "z", "=", "f", "/", "R", "d", "h", "w", " ", "tab"} {
		got, cmd := send(t, m, keyNamed(k))
		if cmd != nil || got.View() != before || got.editMode != editNone {
			t.Errorf("%q on Settings acted: cmd=%v edit=%v", k, cmd != nil, got.editMode)
		}
	}
	for _, k := range []string{"+", "-"} {
		if got, cmd := send(t, m, keyNamed(k)); cmd != nil || len(got.pending) != 0 || got.staged == 0 {
			t.Errorf("%q on Settings: cmd=%v pending=%v staged=%d; want only a staged pool", k, cmd != nil, got.pending, got.staged)
		}
	}
}

// FR3: at the 40-col floor the row's value stays visible and, off Settings, its
// label shrinks to *.
func TestSettingsNarrowKeepsValue(t *testing.T) {
	m, _ := send(t, settingsModel(t, loop.Setting{N: 4, Source: "config.toml", Unusable: "local"}), runeKey("*"))
	m.w = floorCols
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if lipgloss.Width(l) > floorCols {
			t.Errorf("line %q is %d wide", l, lipgloss.Width(l))
		}
	}
	if ls := statsLines(m); !strings.HasPrefix(ls[0], "AFK pool 4 (") {
		t.Errorf("narrow row lost its value: %q", ls[0])
	}
	if bar := ansi.Strip(m.tabBar()); !strings.Contains(bar, "*Settings") {
		t.Errorf("active Settings keeps its full name: %q", bar)
	}
	m.tab = tabGraph
	if bar := ansi.Strip(m.tabBar()); strings.Contains(bar, "Settings") || !strings.Contains(bar, " # * ") {
		t.Errorf("off Settings at the floor, want the * short form: %q", bar)
	}
}

// FR34: every tab's ? lists * as the Settings jump; the Settings footer keeps ? and q.
func TestSettingsHelp(t *testing.T) {
	for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats, tabSettings} {
		listed := map[string]string{}
		for _, k := range helpKeys(tb, "ctrl+b") {
			listed[k.k] = k.label
		}
		if listed["*"] != "Settings tab" {
			t.Errorf("%v: ? lacks * Settings tab", tb)
		}
	}
	m, _ := send(t, settingsModel(t, loop.Setting{N: 3, Source: "default"}), runeKey("*"))
	if f := plain(m.footer()); !strings.HasSuffix(f, "? · q") || strings.Contains(f, "│") {
		t.Errorf("Settings footer %q, want no bead keys and ? · q", f)
	}
}

// FR10–13, FR16–17, FR26: +/- stage with floor 1 and no ceiling and write nothing;
// the row shows N → M (unsaved), the label *Settings• even from another tab, a
// refresh that moves the base keeps the staged value and shows file now F, and esc
// discards it.
func TestSettingsStagedEdit(t *testing.T) {
	saves := stubSavePool(t, nil)
	m, _ := send(t, settingsModel(t, loop.Setting{N: 2, Source: "default"}), runeKey("*"))
	for _, k := range []string{"-", "-", "-"} { // 2 → 1, then 1 holds
		m, _ = send(t, m, runeKey(k))
	}
	if ls := statsLines(m); m.staged != 1 || ls[0] != "AFK pool 2 → 1 (unsaved) (default) 2 in flight" {
		t.Errorf("- floor: staged %d row %q", m.staged, ls[0])
	}
	for range 20 {
		m, _ = send(t, m, runeKey("+"))
	}
	if m.staged != 21 {
		t.Errorf("+ ×20 from 1 staged %d, want 21 (no ceiling)", m.staged)
	}
	m, _ = send(t, m, keyNamed("left"))
	if bar := ansi.Strip(m.tabBar()); !strings.Contains(bar, "*Settings•") {
		t.Errorf("staged off Settings, bar %q lacks *Settings•", bar)
	}
	next, _ := m.Update(loadedMsg{pool: loop.Setting{N: 6, Source: "local"}})
	m, _ = send(t, next.(Model), runeKey("*"))
	if ls := statsLines(m); ls[0] != "AFK pool 2 → 21 (unsaved; file now 6) (local) 0 in flight" {
		t.Errorf("moved base: row %q", ls[0])
	}
	m, _ = send(t, m, keyNamed("esc"))
	if ls := statsLines(m); m.staged != 0 || ls[0] != "AFK pool 6 (local) 0 in flight" || strings.Contains(m.tabBar(), "•") {
		t.Errorf("esc: staged %d row %q", m.staged, ls[0])
	}
	if len(*saves) != 0 {
		t.Errorf("stepping saved %v", *saves)
	}
}

// FR15: enter with nothing staged, or staged equal to the latest cached value, issues no Cmd.
func TestSettingsEnterNoop(t *testing.T) {
	stubSavePool(t, nil)
	m, _ := send(t, settingsModel(t, loop.Setting{N: 3, Source: "default"}), runeKey("*"))
	if _, cmd := send(t, m, keyNamed("enter")); cmd != nil {
		t.Error("enter with nothing staged issued a Cmd")
	}
	m, _ = send(t, m, runeKey("+"))
	next, _ := m.Update(loadedMsg{pool: loop.Setting{N: 4, Source: "local"}})
	if _, cmd := send(t, next.(Model), keyNamed("enter")); cmd != nil {
		t.Error("enter with staged == cached value issued a Cmd")
	}
}

// FR14, FR21–24: enter saves off the event loop under the "AFK pool" pending key; a
// second enter is refused; success reads saved (this machine), clears staged and
// reloads; failure reads save failed and keeps staged.
func TestSettingsSave(t *testing.T) {
	saves := stubSavePool(t, nil)
	m, _ := send(t, settingsModel(t, loop.Setting{N: 3, Source: "default"}), runeKey("*"))
	m, _ = send(t, m, runeKey("+"))
	m, first := send(t, m, keyNamed("enter"))
	if first == nil || m.pending["AFK pool"] == "" || len(*saves) != 0 {
		t.Fatalf("enter: cmd %v pending %v saves %v", first != nil, m.pending, *saves)
	}
	if again, cmd := send(t, m, keyNamed("enter")); cmd != nil || again.status != "AFK pool: write in flight, wait" {
		t.Errorf("second enter: cmd %v status %q; want refused", cmd != nil, again.status)
	}
	next, reload := m.Update(first())
	m = next.(Model)
	if m.status != "AFK pool 4 saved (this machine)" || m.staged != 0 || len(m.pending) != 0 || reload == nil || !slices.Equal(*saves, []int{4}) {
		t.Errorf("saved: status %q staged %d pending %v reload %v saves %v", m.status, m.staged, m.pending, reload != nil, *saves)
	}

	stubSavePool(t, errors.New("disk full"))
	m, _ = send(t, m, runeKey("+"))
	m, cmd := send(t, m, keyNamed("enter"))
	next, reload = m.Update(cmd())
	if m = next.(Model); m.status != "AFK pool save failed: disk full" || m.staged != 4 || reload != nil {
		t.Errorf("failed: status %q staged %d reload %v", m.status, m.staged, reload != nil)
	}
}

// FR33–34: the footer shows +/- change, and enter save · esc discard only while staged; ? lists them.
func TestSettingsFooterHints(t *testing.T) {
	m, _ := send(t, settingsModel(t, loop.Setting{N: 3, Source: "default"}), runeKey("*"))
	if f := plain(m.footer()); !strings.Contains(f, "+/- change") || strings.Contains(f, "enter") || !strings.HasSuffix(f, "? · q") {
		t.Errorf("unstaged footer %q", f)
	}
	m, _ = send(t, m, runeKey("+"))
	if f := plain(m.footer()); !strings.Contains(f, "+/- change · enter save · esc discard") || !strings.HasSuffix(f, "? · q") {
		t.Errorf("staged footer %q", f)
	}
	listed := map[string]bool{}
	for _, k := range helpKeys(tabSettings, "ctrl+b") {
		listed[k.k] = true
	}
	for _, k := range []string{"j/k/↓/↑", "+/-", "enter", "esc", "L"} {
		if !listed[k] {
			t.Errorf("Settings ? lacks %q", k)
		}
	}
}

// stubSavePool stubs the local override writer with err, recording each n it is handed.
func stubSavePool(t *testing.T, err error) *[]int {
	t.Helper()
	orig := savePool
	t.Cleanup(func() { savePool = orig })
	var saves []int
	savePool = func(n int) error {
		saves = append(saves, n)
		return err
	}
	return &saves
}

// stubAutoExit stubs the refresh's layered auto_exit read with a.
func stubAutoExit(t *testing.T, a loop.AutoExit) {
	t.Helper()
	orig := readAutoExit
	t.Cleanup(func() { readAutoExit = orig })
	readAutoExit = func() loop.AutoExit { return a }
}

// stubSaveAutoExit stubs the local auto_exit writer with err, recording each d it is handed.
func stubSaveAutoExit(t *testing.T, err error) *[]time.Duration {
	t.Helper()
	orig := saveAutoExit
	t.Cleanup(func() { saveAutoExit = orig })
	var saves []time.Duration
	saveAutoExit = func(d time.Duration) error {
		saves = append(saves, d)
		return err
	}
	return &saves
}

// exitModel is the Settings tab with the pool at 3 (default) and auto_exit a.
func exitModel(t *testing.T, a loop.AutoExit) Model {
	t.Helper()
	m := settingsModel(t, loop.Setting{N: 3, Source: "default"})
	stubAutoExit(t, a)
	next, _ := m.Update(m.load()())
	m, _ = send(t, next.(Model), runeKey("*"))
	return m
}

// Spec FR25-26: an Auto-exit row below the pool row, its value as 1m,
// 1m30s or off, its source read like the pool's, a dimmed gloss under it.
func TestSettingsAutoExitRow(t *testing.T) {
	for _, c := range []struct {
		a    loop.AutoExit
		want string
	}{
		{loop.AutoExit{D: time.Minute, Source: "default"}, "Auto-exit 1m (default)"},
		{loop.AutoExit{D: 90 * time.Second, Source: "config.toml"}, "Auto-exit 1m30s (config.toml)"},
		{loop.AutoExit{D: 0, Source: "local"}, "Auto-exit off (local)"},
		{loop.AutoExit{D: 30 * time.Second, Source: "local", Shared: 2 * time.Minute, SharedSet: true}, "Auto-exit 30s (local; shared 2m)"},
		{loop.AutoExit{D: time.Minute, Source: "local", SharedSet: true}, "Auto-exit 1m (local; shared off)"},
		{loop.AutoExit{D: time.Minute, Source: "default", Unusable: "local"}, "Auto-exit 1m (unusable → default)"},
		{loop.AutoExit{D: time.Hour, Source: "config.toml"}, "Auto-exit 1h (config.toml)"},
	} {
		ls := statsLines(exitModel(t, c.a))
		if ls[2] != c.want {
			t.Errorf("%+v: row %q, want %q", c.a, ls[2], c.want)
		}
		if !strings.Contains(ls[3], "closing line") {
			t.Errorf("%+v: no gloss under the row: %q", c.a, ls[3])
		}
	}
}

// Spec D10: j/k (↓/↑) select a row, and +/- stage only the selected one:
// the pool by 1, Auto-exit by 30s.
func TestSettingsRowSelect(t *testing.T) {
	m := exitModel(t, loop.AutoExit{D: time.Minute, Source: "default"})
	for _, k := range []string{"j", "down"} {
		next, _ := send(t, m, keyNamed(k))
		next, _ = send(t, next, runeKey("+"))
		if ls := statsLines(next); next.staged != 0 || ls[2] != "Auto-exit 1m → 1m30s (unsaved) (default)" {
			t.Errorf("%s then +: pool staged %d, row %q", k, next.staged, ls[2])
		}
		back, _ := send(t, next, keyNamed("j")) // the last row holds
		back, _ = send(t, back, keyNamed(map[string]string{"j": "k", "down": "up"}[k]))
		back, _ = send(t, back, runeKey("+"))
		if back.staged != 4 || !strings.Contains(statsLines(back)[2], "1m → 1m30s") {
			t.Errorf("%s, back up, +: pool staged %d, exit row %q", k, back.staged, statsLines(back)[2])
		}
	}
}

// Spec FR27: +/- step a staged Auto-exit by 30s, floor off, no ceiling;
// the moved-under-the-edit note and esc mirror the pool row's.
func TestSettingsAutoExitStagedEdit(t *testing.T) {
	saves := stubSaveAutoExit(t, nil)
	m, _ := send(t, exitModel(t, loop.AutoExit{D: time.Minute, Source: "default"}), keyNamed("j"))
	for _, k := range []string{"-", "-", "-"} { // 1m → 30s → off, then off holds
		m, _ = send(t, m, runeKey(k))
	}
	if ls := statsLines(m); ls[2] != "Auto-exit 1m → off (unsaved) (default)" {
		t.Errorf("- floor: row %q", ls[2])
	}
	for range 7 {
		m, _ = send(t, m, runeKey("+"))
	}
	if ls := statsLines(m); ls[2] != "Auto-exit 1m → 3m30s (unsaved) (default)" {
		t.Errorf("+ ×7 from off: row %q", ls[2])
	}
	m, _ = send(t, m, keyNamed("left"))
	if bar := ansi.Strip(m.tabBar()); !strings.Contains(bar, "*Settings•") {
		t.Errorf("staged off Settings, bar %q lacks *Settings•", bar)
	}
	next, _ := m.Update(loadedMsg{autoExit: loop.AutoExit{D: 2 * time.Minute, Source: "local"}})
	m, _ = send(t, next.(Model), runeKey("*"))
	if ls := statsLines(m); ls[2] != "Auto-exit 1m → 3m30s (unsaved; file now 2m) (local)" {
		t.Errorf("moved base: row %q", ls[2])
	}
	m, _ = send(t, m, keyNamed("esc"))
	if ls := statsLines(m); ls[2] != "Auto-exit 2m (local)" || strings.Contains(m.tabBar(), "•") {
		t.Errorf("esc: row %q", ls[2])
	}
	m, _ = send(t, m, runeKey("+"))
	m, _ = send(t, m, runeKey("-")) // stepped back to the value in force: nothing staged
	if strings.Contains(m.tabBar(), "•") {
		t.Errorf("back at the value in force still staged: %q", statsLines(m)[2])
	}
	if len(*saves) != 0 {
		t.Errorf("stepping saved %v", *saves)
	}
}

// Spec FR28: enter saves the staged Auto-exit under its own pending key
// (off saves 0), clears staged and reloads; failure keeps it; nothing staged is a no-op.
func TestSettingsAutoExitSave(t *testing.T) {
	saves := stubSaveAutoExit(t, nil)
	m, _ := send(t, exitModel(t, loop.AutoExit{D: time.Minute, Source: "default"}), keyNamed("j"))
	if _, cmd := send(t, m, keyNamed("enter")); cmd != nil {
		t.Error("enter with nothing staged issued a Cmd")
	}
	m, _ = send(t, m, runeKey("+"))
	m, cmd := send(t, m, keyNamed("enter"))
	if cmd == nil || m.pending["Auto-exit"] == "" {
		t.Fatalf("enter: cmd %v pending %v", cmd != nil, m.pending)
	}
	next, reload := m.Update(cmd())
	m = next.(Model)
	if m.status != "Auto-exit 1m30s saved (this machine)" || strings.Contains(m.tabBar(), "•") || reload == nil || !slices.Equal(*saves, []time.Duration{90 * time.Second}) {
		t.Errorf("saved: status %q reload %v saves %v", m.status, reload != nil, *saves)
	}

	stubSaveAutoExit(t, errors.New("disk full"))
	m, _ = send(t, m, runeKey("-"))
	m, _ = send(t, m, runeKey("-"))
	m, _ = send(t, m, runeKey("-")) // 1m (still cached) → off
	m, cmd = send(t, m, keyNamed("enter"))
	next, reload = m.Update(cmd())
	if m = next.(Model); m.status != "Auto-exit save failed: disk full" || !strings.Contains(statsLines(m)[2], "→ off (unsaved)") || reload != nil {
		t.Errorf("failed: status %q row %q reload %v", m.status, statsLines(m)[2], reload != nil)
	}
}
