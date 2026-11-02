package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
)

// settingsModel is a 100x24 model on the Settings tab, loaded with a pool of 3, an
// Auto-exit of 1m and a catalog of haiku/opus/sonnet; the save seams record into saved.
func settingsModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	prev := lipgloss.ColorProfile() // so the selection marker shows in View
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	var saved []string
	origs := []any{savePool, saveAutoExit, saveKindProfile, saveAutoApprove, saveMouse, loop.ReadMouse}
	origTheme, origReadTheme := saveTheme, loop.ReadTheme
	t.Cleanup(func() {
		saveTheme, loop.ReadTheme = origTheme, origReadTheme
		applyTheme(darkPalette)
		savePool, saveAutoExit, saveKindProfile, saveAutoApprove = origs[0].(func(int) error), origs[1].(func(time.Duration) error), origs[2].(func(string, string) error), origs[3].(func(*bool) error)
		saveMouse, loop.ReadMouse = origs[4].(func(*bool) error), origs[5].(func() loop.Mouse)
	})
	loop.ReadMouse = func() loop.Mouse { return loop.Mouse{Source: "default"} }
	saveMouse = func(v *bool) error {
		switch {
		case v == nil:
			saved = append(saved, "mouse unset")
		case *v:
			saved = append(saved, "mouse on")
		default:
			saved = append(saved, "mouse off")
		}
		return nil
	}
	// Theme saves would otherwise write the developer's real machine-local config.
	theme := loop.Theme{Source: "default"}
	loop.ReadTheme = func() loop.Theme { return theme }
	saveTheme = func(v *string) error {
		if v == nil {
			saved = append(saved, "theme unset")
		} else {
			saved = append(saved, "theme "+*v)
		}
		return nil
	}
	savePool = func(n int) error { saved = append(saved, "pool"+string(rune('0'+n))); return nil }
	saveAutoExit = func(d time.Duration) error { saved = append(saved, "exit"+d.String()); return nil }
	saveKindProfile = func(k, n string) error { saved = append(saved, "kind "+k+"="+n); return nil }
	saveAutoApprove = func(v *bool) error {
		switch {
		case v == nil:
			saved = append(saved, "approve unset")
		case *v:
			saved = append(saved, "approve on")
		default:
			saved = append(saved, "approve off")
		}
		return nil
	}
	m := sized(t, 100, 24)
	m, _ = send(t, m, loadedMsg{
		slowTier: true,
		pool:     loop.Setting{N: 3, Source: "default"},
		autoExit: loop.AutoExit{D: time.Minute, Source: "default"},
		kinds: kindProfiles{
			local: map[string]string{"build": "opus"}, shared: map[string]string{},
			profiles: map[string]agentprofile.Entry{"haiku": {}, "opus": {}, "sonnet": {}},
		},
	})
	m, _ = send(t, m, runeKey("*"))
	return m, &saved
}

// run applies a cmd's message so a startWrite's save executes.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	m, _ = send(t, m, cmd())
	return m
}

func hintKeys(m Model) []string {
	var ks []string
	for _, b := range m.tabBindings() {
		ks = append(ks, b.Help().Key)
	}
	return ks
}

func TestSettingsRows(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}) // the Theme row pushes the kinds down
	v := strings.Join(lines(m.View()), "\n")
	for _, want := range []string{"AFK pool", "3  (default)", "Auto-exit", "1m", "Kind profiles", "default", "build", "opus  (local)"} {
		if !strings.Contains(v, want) {
			t.Errorf("Settings lacks %q:\n%s", want, v)
		}
	}
}

// +/- stage and write nothing; enter saves at once with no dialog; the • tracks staging.
func TestSettingsStageSaveDiscard(t *testing.T) {
	m, saved := settingsModel(t)
	if strings.Contains(lines(m.View())[2], "•") {
		t.Fatal("• before any edit")
	}
	m, cmd := send(t, m, runeKey("+"))
	if cmd != nil || len(*saved) != 0 || len(m.modals) != 0 {
		t.Fatal("+ must only stage")
	}
	if !strings.Contains(lines(m.View())[2], "* Settings •") {
		t.Errorf("no • while staged:\n%s", m.View())
	}
	// esc discards without a save call
	d, _ := send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(*saved) != 0 || strings.Contains(lines(d.View())[2], "•") {
		t.Error("esc must discard silently and clear •")
	}
	m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.modals) != 0 || cmd == nil {
		t.Fatal("enter must start the save at once, no dialog")
	}
	m = run(t, m, cmd)
	if !slices.Equal(*saved, []string{"pool4"}) {
		t.Errorf("saved = %v", *saved)
	}
	if strings.Contains(lines(m.View())[2], "•") || !strings.Contains(m.status, "AFK pool 4 saved") {
		t.Errorf("after save: status %q\n%s", m.status, m.View())
	}
}

func TestSettingsSaveFailureKeepsStaged(t *testing.T) {
	m, _ := settingsModel(t)
	savePool = func(int) error { return errors.New("disk full") }
	m, _ = send(t, m, runeKey("+"))
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = run(t, m, cmd)
	if !strings.Contains(m.status, "AFK pool save failed: disk full") || !m.settingsStaged() {
		t.Errorf("status %q staged %v", m.status, m.settingsStaged())
	}
}

func TestSettingsAutoExitSave(t *testing.T) {
	m, saved := settingsModel(t)
	m, _ = send(t, m, runeKey("j"))
	m, _ = send(t, m, runeKey("-"))
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	run(t, m, cmd)
	if !slices.Equal(*saved, []string{"exit30s"}) {
		t.Errorf("saved = %v", *saved)
	}
}

// An (unset) save deletes the local key.
func TestSettingsKindUnsetDeletesKey(t *testing.T) {
	m, saved := settingsModel(t)
	for range 8 { // pool, Auto-exit, Initiatives, Auto-approve, Mouse mode, Theme, default, approval, build
		m, _ = send(t, m, runeKey("j"))
	}
	m, _ = send(t, m, runeKey("+")) // opus → sonnet
	m, _ = send(t, m, runeKey("+")) // → (unset)
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	run(t, m, cmd)
	if !slices.Equal(*saved, []string{"kind build="}) {
		t.Errorf("saved = %v", *saved)
	}
}

// The hints differ by exactly enter/esc between a staged and an unstaged row.
func TestSettingsHintsStagedVsUnstaged(t *testing.T) {
	m, _ := settingsModel(t)
	plain := hintKeys(m)
	m, _ = send(t, m, runeKey("+"))
	if got := hintKeys(m); !slices.Equal(got, append(plain, "enter", "esc")) {
		t.Errorf("staged hints %v vs unstaged %v", got, plain)
	}
	m, _ = send(t, m, runeKey("j")) // the Auto-exit row is not staged
	if got := hintKeys(m); !slices.Equal(got, plain) {
		t.Errorf("unstaged row hints %v, want %v", got, plain)
	}
}

// Kind profile rows scroll with the cursor at the 40×12 floor.
func TestSettingsKindRowsScroll(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})
	last := len(kindKeys()) - 1
	for range 5 + last {
		m, _ = send(t, m, runeKey("j"))
	}
	v := strings.Join(lines(m.View()), "\n")
	if !strings.Contains(v, kindKeys()[last]) || !strings.Contains(v, "↑") {
		t.Errorf("cursor row not scrolled into view:\n%s", v)
	}
	if got := len(lines(m.View())); got != 12 {
		t.Errorf("frame is %d rows, want 12", got)
	}
}

// Binding coverage in both Settings modes: row unstaged, and staged.
func TestSettingsBindingCoverage(t *testing.T) {
	m, _ := settingsModel(t)
	for range firstKindRow {
		m, _ = send(t, m, runeKey("j")) // default kind row: every of j/k/+/- changes the frame
	}
	checkCoverage(t, "Settings unstaged", m, true, "*")
	m, _ = send(t, m, runeKey("+"))
	checkCoverage(t, "Settings staged", m, true, "*")
	if !slices.ContainsFunc(m.modeBindings(), func(b key.Binding) bool { return b.Help().Key == "enter" }) {
		t.Error("staged mode lacks enter")
	}
}

// The Initiatives row shows required/external ref and the shared file, or unusable;
// +/-/enter/esc on it write nothing.
func TestSettingsInitiativesRowReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   cfglayer.Initiatives
		want string
	}{
		{"off", cfglayer.Initiatives{Path: "/w/.mugicrew/config.toml"}, "required off · external ref off  (/w/.mugicrew/config.toml)"},
		{"on", cfglayer.Initiatives{Required: true, ExternalRef: true, Path: "/w/c.toml"}, "required on · external ref on  (/w/c.toml)"},
		{"unusable", cfglayer.Initiatives{Unusable: true, Path: "/w/c.toml"}, "unusable  (/w/c.toml)"},
	} {
		m, saved := settingsModel(t)
		m, _ = send(t, m, loadedMsg{slowTier: true, initiatives: tc.in})
		if v := strings.Join(lines(m.View()), "\n"); !strings.Contains(v, "Initiatives  "+tc.want) {
			t.Errorf("%s: row lacks %q:\n%s", tc.name, tc.want, v)
		}
		m, _ = send(t, m, runeKey("j"))
		m, _ = send(t, m, runeKey("j"))
		for _, k := range []tea.Msg{runeKey("+"), runeKey("-"), tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyEsc}} {
			var cmd tea.Cmd
			m, cmd = send(t, m, k)
			run(t, m, cmd)
		}
		if len(*saved) != 0 {
			t.Errorf("%s: wrote %v", tc.name, *saved)
		}
	}
}

// The Auto-approve row shows the value in force with its source, right after
// Initiatives (ADR 0353); its edit is covered below.
func TestSettingsAutoApproveRowShowsSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   loop.AutoApprove
		want string
	}{
		{"default", loop.AutoApprove{Source: "default"}, "off  (default)"},
		{"shared off", loop.AutoApprove{Source: "config.toml"}, "off  (config.toml)"},
		{"shared on", loop.AutoApprove{On: true, Source: "config.toml"}, "on  (config.toml)"},
		{"local alone", loop.AutoApprove{On: true, Source: "local"}, "on  (local)"},
		{"local over shared off", loop.AutoApprove{On: true, Source: "local", SharedSet: true}, "on  (local; shared off)"},
		{"local off over shared on", loop.AutoApprove{Source: "local", Shared: true, SharedSet: true}, "off  (local; shared on)"},
		{"unusable local", loop.AutoApprove{On: true, Source: "config.toml", Unusable: "local"}, "on  (unusable → config.toml)"},
		{"unusable shared", loop.AutoApprove{Source: "default", Unusable: "config.toml"}, "off  (unusable → default)"},
	} {
		m, _ := settingsModel(t)
		m, _ = send(t, m, loadedMsg{slowTier: true, approvals: tc.in})
		v := strings.Join(lines(m.View()), "\n")
		if !strings.Contains(v, "Auto-approve  "+tc.want) {
			t.Errorf("%s: row lacks %q:\n%s", tc.name, tc.want, v)
		}
		if _, after, _ := strings.Cut(v, "Auto-approve  "); strings.Contains(after, "edit the file") {
			t.Errorf("%s: gloss still says edit the file:\n%s", tc.name, v)
		}
		if strings.Index(v, "Auto-approve  ") < strings.Index(v, "Initiatives  ") {
			t.Errorf("%s: Auto-approve not after Initiatives", tc.name)
		}
	}
}

// Every registry kind is listed after default and its rule, in alphabetical
// order; a Mechanical kind is a — row, and the cursor never stops on it.
func TestSettingsKindsListMechanicalAsDash(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 80})
	ls := lines(m.View())
	var order []string
	for _, l := range ls[slices.IndexFunc(ls, func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "──") })+1:] {
		if f := strings.Fields(l); len(f) > 0 {
			order = append(order, f[0])
		}
	}
	want := make([]string, 0, len(registry.AllKinds()))
	for _, k := range registry.AllKinds() {
		want = append(want, string(k))
	}
	if !slices.Equal(order[:len(want)], want) {
		t.Fatalf("kind rows = %v, want %v", order, want)
	}
	for _, k := range []string{"checkpoint", "landing", "latch", "rollback"} {
		if !slices.ContainsFunc(ls, func(l string) bool {
			return strings.HasPrefix(l, k) && strings.HasSuffix(strings.TrimSpace(l), "—")
		}) {
			t.Errorf("no — row for %s:\n%s", k, strings.Join(ls, "\n"))
		}
	}
	// j from default visits exactly kindKeys(): the cursor skips — rows.
	for range firstKindRow {
		m, _ = send(t, m, runeKey("j"))
	}
	for i, key := range kindKeys() {
		if got := kindRowKey(m.setRow); got != key {
			t.Fatalf("step %d: cursor on %q, want %q", i, got, key)
		}
		m, _ = send(t, m, runeKey("j"))
	}
	// +/enter on the row before checkpoint never stage or save a — row.
	if len(m.kindStaged) != 0 {
		t.Errorf("staged %v", m.kindStaged)
	}
}

func TestSettingsGlossExplainsDash(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := strings.Join(lines(m.View()), "\n"); !strings.Contains(v, "unset falls back to default; — runs no agent session") {
		t.Errorf("gloss lacks the dash suffix:\n%s", v)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 40})
	if v := strings.Join(lines(m.View()), "\n"); !strings.Contains(v, "…") {
		t.Errorf("narrow gloss not cut:\n%s", v)
	}
}

// kindsModel is settingsModel with the Kind profiles read replaced by kp.
func kindsModel(t *testing.T, kp kindProfiles) Model {
	t.Helper()
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 140, Height: 80})
	m, _ = send(t, m, loadedMsg{
		slowTier: true,
		pool:     loop.Setting{N: 3, Source: "default"},
		autoExit: loop.AutoExit{D: time.Minute, Source: "default"},
		kinds:    kp,
	})
	for range firstKindRow {
		m, _ = send(t, m, runeKey("j"))
	}
	return m
}

// The +/- cycle is every declared name in one alphabetical list, a bad-table
// name included, and a staged bad-table name shows its reason (ADR 0308 §1-2).
func TestSettingsCycleIncludesBadTableName(t *testing.T) {
	why := "declared unusable: unknown key modle (from .mugicrew/config.toml)"
	m := kindsModel(t, kindProfiles{
		local: map[string]string{}, shared: map[string]string{},
		profiles: map[string]agentprofile.Entry{"zed": {}, "bad": {Err: errors.New("x")}, "alpha": {}},
		reasons:  map[string]string{"bad": why},
	})
	var seen []string
	for range 4 {
		m, _ = send(t, m, runeKey("+"))
		seen = append(seen, m.kindStaged[kindRowKey(m.setRow)].to)
		if m.kindStaged[kindRowKey(m.setRow)].to == "bad" && !strings.Contains(m.View(), "unrunnable: "+why) {
			t.Errorf("staged bad table lacks its reason:\n%s", m.View())
		}
	}
	if want := []string{"alpha", "bad", "zed", ""}; !slices.Equal(seen, want) {
		t.Errorf("cycle = %q, want %q", seen, want)
	}
}

// A mapped name no file declares reads "not declared".
func TestProfileReasonsNotDeclared(t *testing.T) {
	got := profileReasons(nil, kindProfiles{
		local: map[string]string{"build": "ghost"}, shared: map[string]string{"review": "here"},
		profiles: map[string]agentprofile.Entry{"here": {}},
	})
	if got["ghost"] != "not declared" || len(got) != 1 {
		t.Errorf("reasons = %v, want only ghost: not declared", got)
	}
}

// +/- on an empty merged set with no local key flash the empty-set text; a set
// holding only a bad table still gives slots.
func TestSettingsCycleEmptyMergedSetFlash(t *testing.T) {
	m := kindsModel(t, kindProfiles{local: map[string]string{}, shared: map[string]string{}})
	m, _ = send(t, m, runeKey("+"))
	if want := "no profiles declared in config.toml or agent-profiles.toml"; m.status != want {
		t.Errorf("flash = %q, want %q", m.status, want)
	}
	if len(m.kindStaged) != 0 {
		t.Errorf("staged %v", m.kindStaged)
	}
	m = kindsModel(t, kindProfiles{local: map[string]string{}, shared: map[string]string{},
		profiles: map[string]agentprofile.Entry{"bad": {Err: errors.New("x")}}})
	m, _ = send(t, m, runeKey("+"))
	if got := m.kindStaged[kindRowKey(m.setRow)].to; got != "bad" {
		t.Errorf("staged %q, want bad", got)
	}
}

// Each Settings editor steps up on +/= and down on -/_; the Initiatives row swallows all four.
func TestSettingsStepKeysAcceptShiftedAndUnshifted(t *testing.T) {
	down := func(m Model, n int) Model {
		for range n {
			m, _ = send(t, m, runeKey("j"))
		}
		return m
	}
	for _, c := range []struct{ k, pool, exit string }{
		{"+", "4", "1m30s"}, {"=", "4", "1m30s"}, {"-", "2", "30s"}, {"_", "2", "30s"},
	} {
		m, _ := settingsModel(t)
		m = down(m, 1)
		m, _ = send(t, m, runeKey(c.k))
		if got := m.stagedExit; got.String() != c.exit {
			t.Errorf("%q: auto-exit staged %v, want %s", c.k, got, c.exit)
		}
		m, _ = settingsModel(t)
		m, _ = send(t, m, runeKey(c.k))
		if got := fmt.Sprint(m.staged); got != c.pool {
			t.Errorf("%q: pool staged %s, want %s", c.k, got, c.pool)
		}
		m, _ = settingsModel(t) // the build row holds the declared name opus, between haiku and sonnet
		m = down(m, 8)
		m, _ = send(t, m, runeKey(c.k))
		want := map[string]string{"+": "sonnet", "=": "sonnet", "-": "haiku", "_": "haiku"}[c.k]
		if got := m.kindStaged["build"].to; got != want {
			t.Errorf("%q: kind staged %q, want %q", c.k, got, want)
		}
		m, _ = settingsModel(t) // an undeclared name: up to the first slot, down to the last (unset)
		m.kinds.local["build"] = "gone"
		m = down(m, 8)
		m, _ = send(t, m, runeKey(c.k))
		want = map[string]string{"+": "haiku", "=": "haiku", "-": "", "_": ""}[c.k]
		if got := m.kindStaged["build"].to; got != want {
			t.Errorf("%q: undeclared kind staged %q, want %q", c.k, got, want)
		}
		m, _ = settingsModel(t)
		m = down(m, 2)
		if _, _, ok := m.settingsKey(c.k); !ok {
			t.Errorf("%q not swallowed on the Initiatives row", c.k)
		}
	}
}

// approveModel is the Settings model with the cursor on the Auto-approve row and in as its read.
func approveModel(t *testing.T, in loop.AutoApprove) (Model, *[]string) {
	t.Helper()
	m, saved := settingsModel(t)
	m, _ = send(t, m, loadedMsg{slowTier: true, approvals: in})
	for range autoApproveRow {
		m, _ = send(t, m, runeKey("j"))
	}
	return m, saved
}

func approveRow(m Model) string {
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "Auto-approve  ") {
			return l
		}
	}
	return ""
}

// +/- cycle on → off → (unset) both ways from each local starting value, and
// landing back on the local value stages nothing.
func TestSettingsAutoApproveCycle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          loop.AutoApprove
		up, down    []string
		stagedAfter int // presses that return to the start
	}{
		{"local on", loop.AutoApprove{On: true, Source: "local"}, []string{"on → off", "on → (unset)"}, []string{"on → (unset)", "on → off"}, 3},
		{"local off", loop.AutoApprove{Source: "local"}, []string{"off → (unset)", "off → on"}, []string{"off → on", "off → (unset)"}, 3},
		{"unset", loop.AutoApprove{Source: "config.toml", On: true}, []string{"on → on", "on → off"}, []string{"on → off", "on → on"}, 3},
	} {
		for dir, want := range map[string][]string{"+": tc.up, "-": tc.down} {
			m, _ := approveModel(t, tc.in)
			for i, w := range want {
				m, _ = send(t, m, runeKey(dir))
				if !strings.Contains(approveRow(m), w+" (unsaved)") {
					t.Errorf("%s %s press %d: row %q lacks %q", tc.name, dir, i+1, approveRow(m), w)
				}
			}
			m, _ = send(t, m, runeKey(dir))
			if m.settingsStaged() || strings.Contains(approveRow(m), "unsaved") {
				t.Errorf("%s %s: landing back on the local value left a stage: %q", tc.name, dir, approveRow(m))
			}
		}
	}
}

func TestSettingsAutoApproveSave(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     loop.AutoApprove
		keys   []string
		saved  string
		status string
	}{
		{"on", loop.AutoApprove{Source: "default"}, []string{"+"}, "approve on", "Auto-approve on saved (this machine)"},
		{"off", loop.AutoApprove{Source: "default"}, []string{"+", "+"}, "approve off", "Auto-approve off saved (this machine)"},
		{"unset over shared on", loop.AutoApprove{Source: "local", On: true, SharedSet: true, Shared: true}, []string{"-"}, "approve unset", "Auto-approve unset saved (this machine; now on)"},
		{"unset to shared only", loop.AutoApprove{Source: "local", On: true}, []string{"+", "+"}, "approve unset", "Auto-approve unset saved (this machine; now off)"},
	} {
		m, saved := approveModel(t, tc.in)
		for _, k := range tc.keys {
			m, _ = send(t, m, runeKey(k))
		}
		m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if len(m.modals) != 0 || cmd == nil {
			t.Fatalf("%s: enter must save at once", tc.name)
		}
		m = run(t, m, cmd)
		if !slices.Equal(*saved, []string{tc.saved}) || !strings.Contains(m.status, tc.status) || m.settingsStaged() {
			t.Errorf("%s: saved %v status %q staged %v", tc.name, *saved, m.status, m.settingsStaged())
		}
	}
}

// esc drops only the Auto-approve row's stage; a failed save keeps it with the error.
func TestSettingsAutoApproveDiscardAndFailure(t *testing.T) {
	m, saved := approveModel(t, loop.AutoApprove{Source: "default"})
	m, _ = send(t, m, runeKey("k"))
	m, _ = send(t, m, runeKey("k"))
	m, _ = send(t, m, runeKey("k")) // pool row
	m, _ = send(t, m, runeKey("+"))
	for range autoApproveRow {
		m, _ = send(t, m, runeKey("j"))
	}
	m, _ = send(t, m, runeKey("+"))
	if got := hintKeys(m); !slices.Contains(got, "enter") {
		t.Errorf("staged row hints %v lack enter", got)
	}
	d, _ := send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(approveRow(d), "unsaved") || d.staged == 0 || slices.Contains(hintKeys(d), "enter") {
		t.Errorf("esc must drop only this row: %q pool staged %d", approveRow(d), d.staged)
	}
	saveAutoApprove = func(*bool) error { return errors.New("/x/config.toml: bad") }
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = run(t, m, cmd)
	if !strings.Contains(m.status, "Auto-approve save failed: /x/config.toml: bad") || !strings.Contains(approveRow(m), "unsaved") || len(*saved) != 0 {
		t.Errorf("status %q row %q", m.status, approveRow(m))
	}
}

// mouseModel is the Settings model with the cursor on the Mouse mode row and in as its read.
func mouseModel(t *testing.T, in loop.Mouse) (Model, *[]string) {
	t.Helper()
	m, saved := settingsModel(t)
	m, _ = send(t, m, loadedMsg{slowTier: true, mouse: in})
	for range mouseRow {
		m, _ = send(t, m, runeKey("j"))
	}
	return m, saved
}

func mouseRowText(m Model) string {
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "Mouse mode  ") {
			return l
		}
	}
	return ""
}

func TestSettingsMouseRowLabels(t *testing.T) {
	for _, tc := range []struct {
		in   loop.Mouse
		want string
	}{
		{loop.Mouse{Source: "default"}, "off  (default)"},
		{loop.Mouse{On: true, Source: "local", SharedSet: true}, "on  (local; shared off)"},
		{loop.Mouse{Source: "local", Shared: true, SharedSet: true}, "off  (local; shared on)"},
		{loop.Mouse{On: true, Source: "config.toml"}, "on  (config.toml)"},
		{loop.Mouse{Source: "default", Unusable: "local"}, "off  (unusable → default)"},
	} {
		m, _ := mouseModel(t, tc.in)
		if got := mouseRowText(m); !strings.Contains(got, tc.want) {
			t.Errorf("%+v: row %q lacks %q", tc.in, got, tc.want)
		}
	}
	m, _ := mouseModel(t, loop.Mouse{Source: "default"})
	if !strings.Contains(m.View(), "Shift-drag (Option/Alt on some terminals)") {
		t.Errorf("gloss must name the Shift-drag bypass:\n%s", m.View())
	}
}

func TestSettingsMouseCycle(t *testing.T) {
	m, _ := mouseModel(t, loop.Mouse{On: true, Source: "local"})
	for i, w := range []string{"on → off", "on → (unset)"} {
		m, _ = send(t, m, runeKey("+"))
		if !strings.Contains(mouseRowText(m), w+" (unsaved)") {
			t.Errorf("press %d: row %q lacks %q", i+1, mouseRowText(m), w)
		}
	}
	m, _ = send(t, m, runeKey("+"))
	if m.settingsStaged() {
		t.Errorf("back at the local value must stage nothing: %q", mouseRowText(m))
	}
	m, _ = send(t, m, runeKey("+"))
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsStaged() {
		t.Errorf("esc must discard")
	}
}

func TestSettingsMouseSaveSwitchesCapture(t *testing.T) {
	for _, tc := range []struct {
		name      string
		in        loop.Mouse
		after     loop.Mouse // what the re-read finds once the save lands
		keys      []string
		saved     string
		status    string
		wantOn    bool
		wantCmd   bool
		wantEnums []string // the messages the switch command emits, in order
	}{
		{"on", loop.Mouse{Source: "default"}, loop.Mouse{On: true, Source: "local"}, []string{"+"}, "mouse on", "Mouse mode on saved (this machine)", true, true, []string{"disable", "enable"}},
		{"off", loop.Mouse{On: true, Source: "local"}, loop.Mouse{Source: "local"}, []string{"+"}, "mouse off", "Mouse mode off saved (this machine)", false, true, []string{"disable"}},
		{"unset to shared on", loop.Mouse{Source: "local", SharedSet: true, Shared: true}, loop.Mouse{On: true, Source: "config.toml"}, []string{"+"}, "mouse unset", "Mouse mode unset saved (this machine; now on)", true, true, []string{"disable", "enable"}},
		{"resolved value unchanged", loop.Mouse{Source: "default"}, loop.Mouse{Source: "local"}, []string{"+", "+"}, "mouse off", "Mouse mode off saved (this machine)", false, false, nil},
	} {
		m, saved := mouseModel(t, tc.in)
		m = m.WithMouse(tc.in.On)
		var tmuxCalls []bool
		setTUIMouse = func(on bool) error { tmuxCalls = append(tmuxCalls, on); return errors.New("tmux gone") }
		loop.ReadMouse = func() loop.Mouse { return tc.after }
		for _, k := range tc.keys {
			m, _ = send(t, m, runeKey(k))
		}
		m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil || len(m.modals) != 0 {
			t.Fatalf("%s: enter must save at once", tc.name)
		}
		msg := cmd()
		wd, ok := msg.(writeDoneMsg)
		if !ok {
			t.Fatalf("%s: got %T", tc.name, msg)
		}
		next, after := wd.apply(m)
		m = next.(Model)
		if !slices.Equal(*saved, []string{tc.saved}) || !strings.Contains(m.status, tc.status) || m.settingsStaged() {
			t.Errorf("%s: saved %v status %q", tc.name, *saved, m.status)
		}
		if m.mouseOn != tc.wantOn {
			t.Errorf("%s: mouseOn = %v", tc.name, m.mouseOn)
		}
		if got := mouseSwitch(after); !slices.Equal(got, tc.wantEnums) {
			t.Errorf("%s: capture commands %v, want %v", tc.name, got, tc.wantEnums)
		}
		wantTmux := []bool{tc.wantOn}
		if !tc.wantCmd {
			wantTmux = nil
		}
		if !slices.Equal(tmuxCalls, wantTmux) { // a setter error leaves capture as set (wantOn above)
			t.Errorf("%s: tmux mouse calls %v, want %v", tc.name, tmuxCalls, wantTmux)
		}
	}
}

// mouseSwitch lists the enable/disable mouse messages a command tree emits, in order.
func mouseSwitch(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	var out []string
	var walk func(msg tea.Msg)
	walk = func(msg tea.Msg) {
		name := fmt.Sprintf("%T", msg)
		switch name {
		case "tea.sequenceMsg", "tea.BatchMsg":
			v := reflect.ValueOf(msg)
			for i := range v.Len() {
				if c, ok := v.Index(i).Interface().(tea.Cmd); ok && c != nil {
					walk(c())
				}
			}
			return
		}
		switch name {
		case fmt.Sprintf("%T", tea.DisableMouse()):
			out = append(out, "disable")
		case fmt.Sprintf("%T", tea.EnableMouseAllMotion()):
			out = append(out, "enable")
		}
	}
	walk(cmd())
	return out
}

// Startup applies the resolved Mouse mode to the TUI's tmux session.
func TestInitSetsTmuxMouse(t *testing.T) {
	for _, on := range []bool{true, false} {
		var calls []bool
		setTUIMouse = func(v bool) error { calls = append(calls, v); return errors.New("tmux gone") }
		m := New(nil, nil).WithMouse(on)
		mouseSwitch(m.Init()) // runs the batch, tmux call included
		if !slices.Equal(calls, []bool{on}) {
			t.Errorf("on=%v: tmux calls %v", on, calls)
		}
	}
}

func TestSettingsMouseSaveOverMalformedRefused(t *testing.T) {
	m, _ := mouseModel(t, loop.Mouse{Source: "default"})
	saveMouse = func(*bool) error { return errors.New("/x/config.toml: bad") }
	m, _ = send(t, m, runeKey("+"))
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = run(t, m, cmd)
	if !strings.Contains(m.status, "Mouse mode save failed: /x/config.toml: bad") || !strings.Contains(mouseRowText(m), "unsaved") || m.mouseOn {
		t.Errorf("status %q row %q", m.status, mouseRowText(m))
	}
}

func TestMouseOffDropsEvents(t *testing.T) {
	m := sized(t, 100, 24)
	press := tea.MouseMsg{X: 5, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	for _, tc := range []struct {
		name string
		m    Model
	}{
		{"off", m},
		{"ref open", func() Model { m := m.WithMouse(true); m.ref = true; return m }()},
		{"modal open", func() Model { m := m.WithMouse(true); m.modals = []dialog{{}}; return m }()},
		{"below the floor", func() Model { m := m.WithMouse(true); m.w = floorCols - 1; return m }()},
	} {
		if tc.m.mouseLive() {
			t.Errorf("%s: mouse must not be live", tc.name)
		}
		next, cmd := tc.m.Update(press)
		if cmd != nil || !reflect.DeepEqual(next.(Model).selection(), tc.m.selection()) {
			t.Errorf("%s: event not dropped", tc.name)
		}
	}
	if !m.WithMouse(true).mouseLive() {
		t.Errorf("on, no dialog, above the floor: must be live")
	}
}

func TestMouseRearmsAfterHandBack(t *testing.T) {
	for _, on := range []bool{true, false} {
		m := sized(t, 100, 24).WithMouse(on)
		for name, msg := range map[string]tea.Msg{
			"resume":   tea.ResumeMsg{},
			"explorer": explorerDoneMsg{},
			"editor":   editorDoneMsg{},
		} {
			_, cmd := m.Update(msg)
			got := mouseSwitch(cmd)
			want := []string(nil)
			if on {
				want = []string{"enable"}
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s with mouse %v: re-arm %v, want %v", name, on, got, want)
			}
		}
	}
}

// themeModel is the Settings model with the cursor on the Theme row and in as its
// read; saveTheme records into the returned slice and ReadTheme returns *after.
// The colour profile is ANSI256 so a preset's background shows in View.
func themeModel(t *testing.T, in loop.Theme) (Model, *[]string, *loop.Theme) {
	t.Helper()
	m, _ := settingsModel(t)
	lipgloss.SetColorProfile(termenv.ANSI256) // settingsModel's cleanup restores the original
	origSave, origRead := saveTheme, loop.ReadTheme
	t.Cleanup(func() {
		saveTheme, loop.ReadTheme = origSave, origRead
		applyTheme(darkPalette)
	})
	var saved []string
	after := in
	saveTheme = func(v *string) error {
		if v == nil {
			saved = append(saved, "theme unset")
		} else {
			saved = append(saved, "theme "+*v)
		}
		return nil
	}
	loop.ReadTheme = func() loop.Theme { return after }
	m = m.WithTheme(in.Name)
	m, _ = send(t, m, loadedMsg{slowTier: true, theme: in})
	for range themeRow {
		m, _ = send(t, m, runeKey("j"))
	}
	return m, &saved, &after
}

func themeRowText(m Model) string {
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "Theme  ") {
			return l
		}
	}
	return ""
}

func TestSettingsThemeRowLabels(t *testing.T) {
	for _, tc := range []struct {
		in   loop.Theme
		want string
	}{
		{loop.Theme{Name: "dark", Source: "default"}, "dark  (default)"},
		{loop.Theme{Name: "light", Source: "local", Shared: "high-contrast", SharedSet: true}, "light  (local; shared high-contrast)"},
		{loop.Theme{Name: "light", Source: "config.toml"}, "light  (config.toml)"},
		{loop.Theme{Name: "dark", Source: "config.toml", Unknown: "solarized"}, `dark  (config.toml: unknown "solarized" → dark)`},
		{loop.Theme{Name: "light", Source: "config.toml", Unusable: "local"}, "light  (unusable → config.toml)"},
	} {
		m, _, _ := themeModel(t, tc.in)
		if got := themeRowText(m); !strings.Contains(got, tc.want) {
			t.Errorf("%+v: row %q lacks %q", tc.in, got, tc.want)
		}
	}
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	if !strings.Contains(strings.Join(lines(m.View()), "\n"), "light expects a light terminal background; +/- previews live, esc reverts; enter saves a machin") {
		t.Errorf("gloss missing:\n%s", m.View())
	}
}

func TestSettingsThemeUnknownReadsInWarn(t *testing.T) {
	forceANSI256(t)
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "config.toml", Unknown: "solarized"})
	want := warnSt.Render(`config.toml: unknown "solarized" → dark`)
	if !strings.Contains(m.View(), want) {
		t.Errorf("unknown part not in warn: want %q in\n%q", want, themeRowText(m))
	}
}

func TestSettingsThemeCycleAndPaint(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	dark := m.View()
	if !strings.Contains(dark, "38;5;"+string(darkPalette.SelMarker)) {
		t.Fatalf("dark selection bg missing")
	}
	m, _ = send(t, m, runeKey("+")) // (unset) → dark: the resolved preset, nothing repainted
	if m.painted != "dark" {
		t.Errorf("first + paints dark, painted %q", m.painted)
	}
	for i, w := range []string{"light", "high-contrast"} {
		m, _ = send(t, m, runeKey("+"))
		if !strings.Contains(themeRowText(m), "→ "+w+" (unsaved)") {
			t.Errorf("press %d: row %q lacks %q", i+1, themeRowText(m), w)
		}
		if i == 0 && (!strings.Contains(m.View(), "38;5;"+string(lightPalette.SelMarker)) || strings.Contains(m.View(), "38;5;"+string(darkPalette.SelMarker))) {
			t.Errorf("+ must repaint in light")
		}
	}
	m, _ = send(t, m, runeKey("+")) // (unset), the local slot
	if m.themeStaged || m.settingsStaged() {
		t.Errorf("wrapped back to the local slot must stage nothing")
	}
	if m.View() != dark {
		t.Errorf("wrapped back must repaint dark")
	}
	m, _ = send(t, m, runeKey("-"))
	if !strings.Contains(themeRowText(m), "→ high-contrast (unsaved)") {
		t.Errorf("- wraps back from (unset): %q", themeRowText(m))
	}
}

func TestSettingsThemeEscReverts(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	dark := m.View()
	m, _ = send(t, m, runeKey("+"))
	if m.View() == dark {
		t.Fatalf("+ must repaint")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.themeStaged || m.View() != dark {
		t.Errorf("esc must drop the stage and repaint dark")
	}
}

func TestSettingsThemeUnsetPreviewsShared(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "high-contrast", Source: "local", Shared: "light", SharedSet: true})
	m, _ = send(t, m, runeKey("+")) // (unset)
	if m.painted != "light" {
		t.Errorf("(unset) must paint the shared value, painted %q", m.painted)
	}
	m, _, _ = themeModel(t, loop.Theme{Name: "high-contrast", Source: "local"})
	m, _ = send(t, m, runeKey("+"))
	if m.painted != "dark" {
		t.Errorf("(unset) with no shared value must paint dark, painted %q", m.painted)
	}
}

func TestSettingsThemeSave(t *testing.T) {
	for _, tc := range []struct {
		name          string
		in, after     loop.Theme
		keys          []string
		saved, status string
		painted       string
	}{
		{"light", loop.Theme{Name: "dark", Source: "default"}, loop.Theme{Name: "light", Source: "local"}, []string{"+", "+"}, "theme light", "Theme light saved (this machine)", "light"},
		{"unset", loop.Theme{Name: "light", Source: "local", Shared: "high-contrast", SharedSet: true}, loop.Theme{Name: "high-contrast", Source: "config.toml"}, []string{"+", "+"}, "theme unset", "Theme unset saved (this machine; now high-contrast)", "high-contrast"},
	} {
		m, saved, after := themeModel(t, tc.in)
		*after = tc.after
		for _, k := range tc.keys {
			m, _ = send(t, m, runeKey(k))
		}
		m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		wd, ok := cmd().(writeDoneMsg)
		if !ok {
			t.Fatalf("%s: enter must save at once", tc.name)
		}
		next, _ := wd.apply(m)
		m = next.(Model)
		if !slices.Equal(*saved, []string{tc.saved}) || !strings.Contains(m.status, tc.status) || m.settingsStaged() || m.painted != tc.painted {
			t.Errorf("%s: saved %v status %q staged %v painted %q", tc.name, *saved, m.status, m.settingsStaged(), m.painted)
		}
	}
}

func TestSettingsThemeFailedSaveKeepsStageAndPaint(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	saveTheme = func(*string) error { return errors.New("disk full") }
	m, _ = send(t, m, runeKey("+"))
	m, _ = send(t, m, runeKey("+"))
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	next, _ := cmd().(writeDoneMsg).apply(m)
	m = next.(Model)
	if !strings.Contains(m.status, "Theme save failed: disk full") || !m.themeStaged || m.painted != "light" {
		t.Errorf("status %q staged %v painted %q", m.status, m.themeStaged, m.painted)
	}
}

func TestSettingsThemeStageSurvivesTabSwitch(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	m, _ = send(t, m, runeKey("+"))
	m, _ = send(t, m, runeKey("+"))
	for range 3 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
	if !m.themeStaged || m.painted != "light" || !strings.Contains(m.View(), "38;5;"+string(lightPalette.Accent)) {
		t.Errorf("stage or paint lost on tab switch: staged %v painted %q", m.themeStaged, m.painted)
	}
}

func TestSettingsThemeStagedNotesMovedFile(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	m, _ = send(t, m, runeKey("+"))
	m, _ = send(t, m, runeKey("+"))
	m, _ = send(t, m, loadedMsg{slowTier: true, theme: loop.Theme{Name: "high-contrast", Source: "config.toml"}})
	if got := themeRowText(m); !strings.Contains(got, "dark → light (unsaved; file now high-contrast)") || m.painted != "light" {
		t.Errorf("row %q painted %q", got, m.painted)
	}
}

func TestSettingsThemeRefreshRepaintsWhenUnstaged(t *testing.T) {
	m, _, _ := themeModel(t, loop.Theme{Name: "dark", Source: "default"})
	m, _ = send(t, m, loadedMsg{slowTier: true, theme: loop.Theme{Name: "light", Source: "config.toml"}})
	if m.painted != "light" || !strings.Contains(m.View(), "38;5;"+string(lightPalette.Accent)) {
		t.Errorf("refresh must repaint, painted %q", m.painted)
	}
}
