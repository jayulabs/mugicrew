package tui

import (
	"errors"
	"fmt"
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
	origs := []any{savePool, saveAutoExit, saveKindProfile}
	t.Cleanup(func() {
		savePool, saveAutoExit, saveKindProfile = origs[0].(func(int) error), origs[1].(func(time.Duration) error), origs[2].(func(string, string) error)
	})
	savePool = func(n int) error { saved = append(saved, "pool"+string(rune('0'+n))); return nil }
	saveAutoExit = func(d time.Duration) error { saved = append(saved, "exit"+d.String()); return nil }
	saveKindProfile = func(k, n string) error { saved = append(saved, "kind "+k+"="+n); return nil }
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
	for range 6 { // pool, Auto-exit, Initiatives, Auto-approve, default, approval, build
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
	for range 3 + last {
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

// The Auto-approve row shows the shared value and file, or unusable, right after
// Initiatives; +/-/enter/esc on it write nothing.
func TestSettingsAutoApproveRowReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   cfglayer.Approvals
		want string
	}{
		{"off", cfglayer.Approvals{Path: "/w/.mugicrew/config.toml"}, "off  (/w/.mugicrew/config.toml)"},
		{"on", cfglayer.Approvals{AutoApprove: true, Path: "/w/c.toml"}, "on  (/w/c.toml)"},
		{"unusable", cfglayer.Approvals{Unusable: true, Path: "/w/c.toml"}, "unusable  (/w/c.toml)"},
	} {
		m, saved := settingsModel(t)
		m, _ = send(t, m, loadedMsg{slowTier: true, approvals: tc.in})
		v := strings.Join(lines(m.View()), "\n")
		if !strings.Contains(v, "Auto-approve  "+tc.want) || !strings.Contains(v, "shared [approvals] in .mugicrew/config.toml: edit the file to change it") {
			t.Errorf("%s: row lacks %q:\n%s", tc.name, tc.want, v)
		}
		if strings.Index(v, "Auto-approve  ") < strings.Index(v, "Initiatives  ") {
			t.Errorf("%s: Auto-approve not after Initiatives", tc.name)
		}
		for range autoApproveRow {
			m, _ = send(t, m, runeKey("j"))
		}
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
		m = down(m, 6)
		m, _ = send(t, m, runeKey(c.k))
		want := map[string]string{"+": "sonnet", "=": "sonnet", "-": "haiku", "_": "haiku"}[c.k]
		if got := m.kindStaged["build"].to; got != want {
			t.Errorf("%q: kind staged %q, want %q", c.k, got, want)
		}
		m, _ = settingsModel(t) // an undeclared name: up to the first slot, down to the last (unset)
		m.kinds.local["build"] = "gone"
		m = down(m, 6)
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
