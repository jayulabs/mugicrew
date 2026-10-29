package tui

// The Settings tab (ADR 0109; Spec FR54–56): one full-width pane of setting rows,
// each drawn from what the refresh cached (ADR 0101): View never reads a file.

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
)

// repoRoot is the repo root, "" when unresolved ⇒ the layers read relative to cwd, as run-now does.
func repoRoot() string {
	root, _ := git.RepoRoot()
	return root
}

// readPool is the refresh's layered AFK pool read, a var so tests run without a live repo.
var readPool = func() loop.Setting {
	root := repoRoot()
	return loop.PoolSetting(root)
}

// savePool is enter's local override write, a var so tests stub it as they stub bd.
var savePool = func(n int) error {
	root := repoRoot()
	return loop.SetLocalPool(root, n)
}

// readAutoExit is the refresh's layered auto_exit read, a var as readPool is.
var readAutoExit = func() loop.AutoExit {
	root := repoRoot()
	return loop.AutoExitSetting(root)
}

// readLandStrategy is the refresh's Workspace land_strategy read (name, err), a var as readPool is.
// LandStrategyAt, never the silent-local setting or the cwd-bound git.LandStrategyName (Spec D5).
var readLandStrategy = func() (string, error) {
	return loop.LandStrategyAt(repoRoot())
}

// readInitiatives is the refresh's shared-only [initiatives] read, a var as readPool is.
var readInitiatives = func() cfglayer.Initiatives {
	root := repoRoot()
	return cfglayer.InitiativesSetting(root)
}

// saveAutoExit is enter's local auto_exit write, a var as savePool is.
var saveAutoExit = func(d time.Duration) error {
	root := repoRoot()
	return loop.SetLocalAutoExit(root, d)
}

// kindProfiles is one refresh's Kind profiles read (ADR 0136): both layers'
// [kind_profiles] maps, the merged profiles of the four files (bad tables
// included, ADR 0308 §1), and the unrunnable reason of each declared or mapped
// name that can't run.
type kindProfiles struct {
	local, shared map[string]string
	profiles      map[string]agentprofile.Entry // the Winners: every declared name, for the +/- cycle and the batch runnable check
	root          string                        // the Workspace root, for reasons' relative paths
	reasons       map[string]string             // name → ADR 0123 §1 reason; absent = runnable
}

// names are the +/- cycle's slots: every declared name, sorted together.
func (kp kindProfiles) names() []string { return slices.Sorted(maps.Keys(kp.profiles)) }

// readKindProfiles is the refresh's mapping and catalog read, a var as readPool is.
var readKindProfiles = func() kindProfiles {
	root := repoRoot()
	local, shared := loop.KindProfiles(root)
	return kindProfiles{local: local, shared: shared, profiles: cfglayer.Profiles(root).Winners, root: root}
}

// saveKindProfile is enter's local [kind_profiles] write ("" name deletes the
// key), a var as savePool is.
var saveKindProfile = func(key, name string) error {
	root := repoRoot()
	return loop.SetLocalKindProfile(root, key, name)
}

// saveAutoApprove is enter's local [approvals] auto_approve write (nil deletes the
// key), a var as savePool is.
var saveAutoApprove = func(v *bool) error {
	root := repoRoot()
	return loop.SetLocalAutoApprove(root, v)
}

// saveMouse is enter's local [tui] mouse write (nil deletes the key), a var as
// savePool is.
var saveMouse = func(v *bool) error {
	root := repoRoot()
	return loop.SetLocalMouse(root, v)
}

// saveTheme is enter's local [tui] theme write (nil deletes the key), a var as
// savePool is.
var saveTheme = func(v *string) error {
	root := repoRoot()
	return loop.SetLocalTheme(root, v)
}

// profileReasons checks the loaded catalog in one ctx.ProfileReasons call (not
// RefuseProfile: this is no refusal, so no profile.unusable); a mapped name
// that no file declares is "not declared" without a call, profiled ctx or
// not. A ctx that isn't profiled runs every catalog name on its own harness, so
// none of those is unrunnable.
func profileReasons(ctx contract.Ctx, kp kindProfiles) map[string]string {
	out := map[string]string{}
	if p, ok := ctx.(profiled); ok {
		if r := p.ProfileReasons(kp.root, kp.profiles); r != nil {
			out = r
		}
	}
	for _, name := range slices.Concat(slices.Collect(maps.Values(kp.local)), slices.Collect(maps.Values(kp.shared))) {
		if _, declared := kp.profiles[name]; !declared {
			out[name] = "not declared"
		}
	}
	return out
}

// profiled is main's Ctx (ADR 0125) as far as Settings needs it: the batch
// runnability check of a loaded catalog. A Ctx that isn't one runs everything.
type profiled interface {
	ProfileReasons(root string, declared map[string]agentprofile.Entry) map[string]string
}

// Settings keys (Spec FR55): enter/esc show only while the selected row is staged (FR56).
var (
	keyRowDown = key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "row"))
	keyRowUp   = key.NewBinding(key.WithKeys("k", "up"))
	keyStage   = key.NewBinding(key.WithKeys("+", "=", "-", "_"), key.WithHelp("+/-", "stage"))
	keySave    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	keyDiscard = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "discard"))
)

// settingsBindings are the verbs valid on the selected row now.
func (m Model) settingsBindings() []key.Binding {
	bs := []key.Binding{keyRowDown, keyRowUp, keyStage}
	if m.rowStaged() {
		bs = append(bs, keySave, keyDiscard)
	}
	return bs
}

// bodyH is the tab body's height.
func (m Model) bodyH() int { return max(m.h-chromeH, 1) }

// poolKey and autoExitKey are the saves' pending keys (ADR 0098 §3) and status prefixes.
const (
	poolKey     = "AFK pool"
	autoExitKey = "Auto-exit"
)

// autoExitStep is one +/- on the Auto-exit row (Spec FR27).
const autoExitStep = 30 * time.Second

// settingsKey is the staged edit (Spec FR10-16, FR21-24; D10):
// j/k pick the row; +/- step the selected row's staged value and write nothing;
// esc discards it; enter saves it as a press-time write under the row's pending
// key. ok is false for keys it leaves to updateKey.
func (m Model) settingsKey(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	switch k {
	case "j", "down":
		m.setRow = min(m.setRow+1, firstKindRow+len(kindKeys())-1)
		m.setTop = m.settingsTop(m.setTop)
		return m, nil, true
	case "k", "up":
		m.setRow = max(m.setRow-1, 0)
		m.setTop = m.settingsTop(m.setTop)
		return m, nil, true
	}
	if m.setRow >= firstKindRow {
		return m.kindEdit(k)
	}
	if m.setRow == initiativesRow { // read-only: Initiatives is shared
		return m, nil, stepOf(k) != stepNone || k == "esc" || k == "enter"
	}
	if m.setRow == autoApproveRow {
		return m.autoApproveEdit(k)
	}
	if m.setRow == mouseRow {
		return m.mouseEdit(k)
	}
	if m.setRow == themeRow {
		return m.themeEdit(k)
	}
	if m.setRow == 1 {
		return m.autoExitEdit(k)
	}
	cur := m.staged
	if cur == 0 {
		cur = m.pool.N
	}
	switch s := stepOf(k); {
	case s != stepNone:
		if s == stepDown && cur <= 1 {
			return m, nil, true
		}
		if m.staged == 0 {
			m.stagedFrom = m.pool.N
		}
		m.staged = cur + 1
		if s == stepDown {
			m.staged = cur - 1
		}
		if m.staged == m.pool.N { // stepped back to the value in force: nothing staged
			m.staged = 0
		}
	case k == "esc":
		m.staged = 0
	case k == "enter":
		n := m.staged
		if n == 0 || n == m.pool.N {
			return m, nil, true
		}
		return m.saveEdit(poolKey, func() error { return savePool(n) },
			func(m *Model) {
				if m.staged == n {
					m.staged = 0
				}
			}, fmt.Sprintf("%s %d saved (this machine)", poolKey, n))
	default:
		return m, nil, false
	}
	return m, nil, true
}

// autoApproveStage is the Auto-approve row's staged edit: to is on, off or
// (unset); from is the row's effective text when staging began.
type autoApproveStage struct{ to, from string }

// autoApproveKey is the save's pending key and status prefix.
const autoApproveKey = "Auto-approve"

const autoApproveUnset = "(unset)"

// autoApproveSlots is the +/- cycle, in the + direction.
var autoApproveSlots = []string{"on", "off", autoApproveUnset}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// autoApproveLocal is the machine-local key's value as a slot: the cached read's
// value when it came from the local layer, else (unset).
func (m Model) autoApproveLocal() string {
	if m.approvals.Source == "local" {
		return onOff(m.approvals.On)
	}
	return autoApproveUnset
}

// autoApproveEdit is settingsKey on the Auto-approve row (ADR 0353 §5): +/- cycle
// on → off → (unset) from the staged value else the local key; back at the local
// key nothing is staged; esc drops this row's edit; enter saves it with no extra
// confirmation.
func (m Model) autoApproveEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	local := m.autoApproveLocal()
	switch s := stepOf(k); {
	case s != stepNone:
		st := m.autoApproveStage
		if !m.autoApproveStaged {
			st = autoApproveStage{to: local, from: onOff(m.approvals.On)}
		}
		i := slices.Index(autoApproveSlots, st.to)
		if s == stepUp {
			i = (i + 1) % len(autoApproveSlots)
		} else {
			i = (i - 1 + len(autoApproveSlots)) % len(autoApproveSlots)
		}
		st.to = autoApproveSlots[i]
		m.autoApproveStage = st
		m.autoApproveStaged = st.to != local
	case k == "esc":
		m.autoApproveStaged = false
	case k == "enter":
		st := m.autoApproveStage
		if !m.autoApproveStaged || st.to == local {
			return m, nil, true
		}
		var v *bool
		if st.to != autoApproveUnset {
			b := st.to == "on"
			v = &b
		}
		saved := fmt.Sprintf("%s %s saved (this machine)", autoApproveKey, st.to)
		if v == nil {
			saved = fmt.Sprintf("%s unset saved (this machine; now %s)", autoApproveKey, onOff(m.autoApproveShared()))
		}
		return m.saveEdit(autoApproveKey, func() error { return saveAutoApprove(v) },
			func(m *Model) {
				if m.autoApproveStaged && m.autoApproveStage.to == st.to {
					m.autoApproveStaged = false
				}
			}, saved)
	default:
		return m, nil, false
	}
	return m, nil, true
}

// autoApproveShared is what deleting the local key would expose: the checked-in
// value, else off.
func (m Model) autoApproveShared() bool {
	if m.approvals.SharedSet {
		return m.approvals.Shared
	}
	return m.approvals.Source == "config.toml" && m.approvals.On
}

// mouseKey is the Mouse mode save's pending key and status prefix.
const mouseKey = "Mouse mode"

// mouseLocal is the machine-local key's value as a slot, as autoApproveLocal is.
func (m Model) mouseLocal() string {
	if m.mouse.Source == "local" {
		return onOff(m.mouse.On)
	}
	return autoApproveUnset
}

// mouseShared is what deleting the local key would expose: the checked-in value,
// else off.
func (m Model) mouseShared() bool {
	if m.mouse.SharedSet {
		return m.mouse.Shared
	}
	return m.mouse.Source == "config.toml" && m.mouse.On
}

// mouseEdit is settingsKey on the Mouse mode row: the Auto-approve row's staged
// edit over [tui] mouse. A save that changes the resolved value switches mouse
// capture in the running TUI (setMouse).
func (m Model) mouseEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	local := m.mouseLocal()
	switch s := stepOf(k); {
	case s != stepNone:
		st := m.mouseStage
		if !m.mouseStaged {
			st = autoApproveStage{to: local, from: onOff(m.mouse.On)}
		}
		i := slices.Index(autoApproveSlots, st.to)
		if s == stepUp {
			i = (i + 1) % len(autoApproveSlots)
		} else {
			i = (i - 1 + len(autoApproveSlots)) % len(autoApproveSlots)
		}
		st.to = autoApproveSlots[i]
		m.mouseStage = st
		m.mouseStaged = st.to != local
	case k == "esc":
		m.mouseStaged = false
	case k == "enter":
		st := m.mouseStage
		if !m.mouseStaged || st.to == local {
			return m, nil, true
		}
		var v *bool
		if st.to != autoApproveUnset {
			b := st.to == "on"
			v = &b
		}
		saved := fmt.Sprintf("%s %s saved (this machine)", mouseKey, st.to)
		if v == nil {
			saved = fmt.Sprintf("%s unset saved (this machine; now %s)", mouseKey, onOff(m.mouseShared()))
		}
		return m.saveEditThen(mouseKey, func() error { return saveMouse(v) },
			func(m *Model) {
				if m.mouseStaged && m.mouseStage.to == st.to {
					m.mouseStaged = false
				}
			}, saved, func(m *Model) tea.Cmd { return m.setMouse(loop.ReadMouse().On) }) // re-read after the save so the live switch sees the file's resolved value; a small local read inside Update
	default:
		return m, nil, false
	}
	return m, nil, true
}

// themeKey is the Theme save's pending key and status prefix.
const themeKey = "Theme"

// themeSlots is the Theme row's +/- cycle, in the + direction.
var themeSlots = append(slices.Clone(loop.ThemeNames), autoApproveUnset)

// themeLocal is the machine-local key's value as a slot: the raw value when it
// names no preset, so a typo can be stepped away from.
func (m Model) themeLocal() string {
	if m.theme.Source != "local" {
		return autoApproveUnset
	}
	return cmp.Or(m.theme.Unknown, m.theme.Name)
}

// themeShared is the preset deleting the local key would expose: the checked-in
// value, else dark.
func (m Model) themeShared() string {
	switch {
	case m.theme.SharedSet:
		return m.theme.Shared
	case m.theme.Source == "config.toml":
		return m.theme.Name
	}
	return loop.ThemeNames[0]
}

// themeNow is the resolved preset a staged row compares its base against.
func themeNow(t loop.Theme) string { return cmp.Or(t.Name, loop.ThemeNames[0]) }

// themePreset is the one preset the TUI paints: the staged slot when the row is
// staged, else the resolved theme. "" = no theme read yet, so nothing to paint.
func (m Model) themePreset() string {
	switch {
	case m.themeStaged && m.themeStage.to == autoApproveUnset:
		return m.themeShared()
	case m.themeStaged && slices.Contains(loop.ThemeNames, m.themeStage.to):
		return m.themeStage.to
	}
	return m.theme.Name
}

// syncTheme installs the painted preset's palette when it differs from the one
// last installed.
func (m *Model) syncTheme() {
	want := m.themePreset()
	if want == "" || want == m.painted {
		return
	}
	m.painted = want
	ApplyThemeName(want)
}

// themeEdit is settingsKey on the Theme row (ADR 0387): the Mouse mode row's
// staged edit over [tui] theme, except that each step and esc repaint the whole
// TUI (syncTheme), and a save re-reads the file to paint what it resolves to.
func (m Model) themeEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	local := m.themeLocal()
	switch s := stepOf(k); {
	case s != stepNone:
		st := m.themeStage
		if !m.themeStaged {
			st = autoApproveStage{to: local, from: themeNow(m.theme)}
		}
		i := slices.Index(themeSlots, st.to)
		switch {
		case i < 0 && s == stepUp: // an unknown local value: + to the first, - to the last slot
			i = 0
		case i < 0:
			i = len(themeSlots) - 1
		case s == stepUp:
			i = (i + 1) % len(themeSlots)
		default:
			i = (i - 1 + len(themeSlots)) % len(themeSlots)
		}
		st.to = themeSlots[i]
		m.themeStage = st
		m.themeStaged = st.to != local
		m.syncTheme()
	case k == "esc":
		m.themeStaged = false
		m.syncTheme()
	case k == "enter":
		st := m.themeStage
		if !m.themeStaged || st.to == local {
			return m, nil, true
		}
		var v *string
		if st.to != autoApproveUnset {
			v = &st.to
		}
		saved := fmt.Sprintf("%s %s saved (this machine)", themeKey, st.to)
		if v == nil {
			saved = fmt.Sprintf("%s unset saved (this machine; now %s)", themeKey, m.themeShared())
		}
		return m.saveEditThen(themeKey, func() error { return saveTheme(v) },
			func(m *Model) {
				if m.themeStaged && m.themeStage.to == st.to {
					m.themeStaged = false
				}
			}, saved, func(m *Model) tea.Cmd {
				m.theme = loop.ReadTheme() // re-read after the save so the paint sees the file's resolved value; a small local read inside Update
				m.syncTheme()
				return nil
			})
	default:
		return m, nil, false
	}
	return m, nil, true
}

// autoExitEdit is settingsKey on the Auto-exit row: steps of 30s, floor off, no ceiling.
func (m Model) autoExitEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	cur := m.exit.D
	if m.exitStaged {
		cur = m.stagedExit
	}
	switch s := stepOf(k); {
	case s != stepNone:
		if !m.exitStaged {
			m.exitFrom = m.exit.D
		}
		m.stagedExit = cur + autoExitStep
		if s == stepDown {
			m.stagedExit = max(cur-autoExitStep, 0)
		}
		m.exitStaged = m.stagedExit != m.exit.D // back at the value in force: nothing staged
	case k == "esc":
		m.exitStaged = false
	case k == "enter":
		d := m.stagedExit
		if !m.exitStaged || d == m.exit.D {
			return m, nil, true
		}
		return m.saveEdit(autoExitKey, func() error { return saveAutoExit(d) },
			func(m *Model) {
				if m.stagedExit == d {
					m.exitStaged = false
				}
			}, fmt.Sprintf("%s %s saved (this machine)", autoExitKey, fmtAutoExit(d)))
	default:
		return m, nil, false
	}
	return m, nil, true
}

// saveEdit is enter's press-time write under pending key pk: save runs off the
// loop; on success unstage clears the row's edit only if still unchanged (a
// value staged during the save is the human's next edit), then the saved status
// shows and the model reloads.
func (m Model) saveEdit(pk string, save func() error, unstage func(*Model), saved string) (tea.Model, tea.Cmd, bool) {
	return m.saveEditThen(pk, save, unstage, saved, nil)
}

// saveEditThen is saveEdit with after, run on a successful save for a command to
// batch with the reload (nil: none).
func (m Model) saveEditThen(pk string, save func() error, unstage func(*Model), saved string, after func(*Model) tea.Cmd) (tea.Model, tea.Cmd, bool) {
	next, cmd := m.startWrite(slot(pk), "saving", func() writeDoneMsg {
		err := save()
		return writeDoneMsg{slot(pk), func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				m.setStatus(pk + " save failed: " + err.Error())
				return m, nil
			}
			unstage(&m)
			m.setStatus(saved)
			var extra tea.Cmd
			if after != nil {
				extra = after(&m)
			}
			cmd := tea.Batch(m.slowLoad(), extra)
			return m, cmd // pool, auto-exit and kinds are slow-tier values: show the write at once
		}}
	})
	return next, cmd, true
}

// kindStage is a Kind profiles row's staged edit: to is a catalog name or ""
// for (unset); from is the row's effective value text when staging began.
type kindStage struct{ to, from string }

// initiativesRow is the read-only Initiatives row; rows 0 and 1 are the pool and Auto-exit.
const initiativesRow = 2

// autoApproveRow is the Auto-approve row, right after Initiatives .
const autoApproveRow = 3

// mouseRow is the Mouse mode row, right after Auto-approve.
const mouseRow = 4

// themeRow is the Theme row, right after Mouse mode.
const themeRow = 5

// firstKindRow is the first Kind profiles row.
const firstKindRow = 6

// kindKeys are the [kind_profiles] keys, one per row from firstKindRow: default, then the kinds.
func kindKeys() []string {
	keys := []string{"default"}
	for _, k := range registry.SessionKinds() {
		keys = append(keys, string(k))
	}
	return keys
}

// kindRowKey is the [kind_profiles] key of Settings row setRow (≥ firstKindRow).
func kindRowKey(setRow int) string { return kindKeys()[setRow-firstKindRow] }

// kindEdit is settingsKey on a Kind profiles row (Spec FR17-24, FR29): +/-
// step through every declared name, sorted, plus a trailing (unset) slot, from the staged
// value else the local key; back at the local key nothing is staged; esc drops
// this row's edit only; enter saves it under the row's own pending key.
func (m Model) kindEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	key := kindRowKey(m.setRow)
	local := m.kinds.local[key]
	switch s := stepOf(k); {
	case s != stepNone:
		if len(m.kinds.profiles) == 0 && local == "" {
			m.setStatus("no profiles declared in config.toml or agent-profiles.toml")
			return m, nil, true
		}
		st, staged := m.kindStaged[key]
		if !staged {
			st = kindStage{to: local, from: m.kindEffective(key)}
		}
		slots := append(m.kinds.names(), "")
		i := slices.Index(slots, st.to)
		switch {
		case i < 0 && s == stepUp: // an undeclared name: + to the first, - to the last slot
			i = 0
		case i < 0:
			i = len(slots) - 1
		case s == stepUp:
			i = (i + 1) % len(slots)
		default:
			i = (i - 1 + len(slots)) % len(slots)
		}
		st.to = slots[i]
		m.kindStaged = maps.Clone(m.kindStaged) // copy on write: an earlier Model keeps its map
		if m.kindStaged == nil {
			m.kindStaged = map[string]kindStage{}
		}
		m.kindStaged[key] = st
		if st.to == local {
			delete(m.kindStaged, key)
		}
	case k == "esc":
		m.kindStaged = maps.Clone(m.kindStaged)
		delete(m.kindStaged, key)
	case k == "enter":
		st, staged := m.kindStaged[key]
		if !staged || st.to == local {
			return m, nil, true
		}
		pk := "Kind profile " + key
		saved := fmt.Sprintf("%s %s saved (this machine)", pk, st.to)
		if st.to == "" {
			saved = fmt.Sprintf("%s unset saved (this machine; now %s)", pk, cmp.Or(m.kindFallback(key), "(built-in)"))
		}
		return m.saveEdit(pk, func() error { return saveKindProfile(key, st.to) },
			func(m *Model) {
				if m.kindStaged[key].to == st.to {
					m.kindStaged = maps.Clone(m.kindStaged)
					delete(m.kindStaged, key)
				}
			}, saved)
	default:
		return m, nil, false
	}
	return m, nil, true
}

// kindEffective is key's in-force profile name, or (built-in).
func (m Model) kindEffective(key string) string {
	return cmp.Or(loop.ResolveProfile(m.kinds.local, m.kinds.shared, key).Name, "(built-in)")
}

// kindFallback is what deleting key's local key would expose ("" = the built-in).
func (m Model) kindFallback(key string) string {
	local := maps.Clone(m.kinds.local)
	delete(local, key)
	return loop.ResolveProfile(local, m.kinds.shared, key).Name
}

// settingsStaged is whether any row holds an unsaved edit.
func (m Model) settingsStaged() bool {
	return m.staged != 0 || m.exitStaged || m.autoApproveStaged || m.mouseStaged || m.themeStaged || len(m.kindStaged) > 0
}

// rowStaged is whether the selected row holds an unsaved edit (ADR 0138 §2).
func (m Model) rowStaged() bool {
	switch m.setRow {
	case 0:
		return m.staged != 0
	case 1:
		return m.exitStaged
	case autoApproveRow:
		return m.autoApproveStaged
	case mouseRow:
		return m.mouseStaged
	case themeRow:
		return m.themeStaged
	case initiativesRow:
		return false
	}
	_, ok := m.kindStaged[kindRowKey(m.setRow)]
	return ok
}

// fmtAutoExit shows an Auto-exit duration as 1m, 1m30s, 45s, 1h or off.
func fmtAutoExit(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// settingRow is one setting: its name, its value in force, and a one-line gloss.
type settingRow struct{ name, value, gloss string }

// settingRows are the tab's rows; a later setting appends one (FR37).
func (m Model) settingRows() []settingRow {
	shared := ""
	if m.pool.Shared > 0 {
		shared = fmt.Sprint(m.pool.Shared)
	}
	n := fmt.Sprint(m.pool.N)
	if m.staged != 0 {
		n = stagedValue(fmt.Sprint(m.stagedFrom), fmt.Sprint(m.staged), n)
	}
	eshared := ""
	if m.exit.SharedSet {
		eshared = fmtAutoExit(m.exit.Shared)
	}
	e := fmtAutoExit(m.exit.D)
	if m.exitStaged {
		e = stagedValue(fmtAutoExit(m.exitFrom), fmtAutoExit(m.stagedExit), e)
	}
	autoApproveValue := autoApproveText(m.approvals)
	if m.autoApproveStaged {
		autoApproveValue = stagedValue(m.autoApproveStage.from, m.autoApproveStage.to, onOff(m.approvals.On))
	}
	mouseValue := mouseText(m.mouse)
	if m.mouseStaged {
		mouseValue = stagedValue(m.mouseStage.from, m.mouseStage.to, onOff(m.mouse.On))
	}
	themeValue := themeText(m.theme)
	if m.themeStaged {
		themeValue = stagedValue(m.themeStage.from, m.themeStage.to, themeNow(m.theme))
	}
	return []settingRow{{
		name:  poolKey,
		value: fmt.Sprintf("%s  (%s)  %d in flight", n, sourceText(m.pool.Source, m.pool.Unusable, shared), m.inflight) + orphanedText(m.orphaned),
		gloss: "how many AFK efforts the loop and run-now start at once on this machine",
	}, {
		name:  autoExitKey,
		value: fmt.Sprintf("%s  (%s)", e, sourceText(m.exit.Source, m.exit.Unusable, eshared)),
		gloss: "how long a HITL session waits after its closing line before it exits by itself",
	}, {
		name:  "Initiatives",
		value: initiativesText(m.initiatives),
		gloss: "shared [initiatives] in .mugicrew/config.toml: edit the file to change it",
	}, {
		name:  "Auto-approve",
		value: autoApproveValue,
		gloss: "on lets the crew approve Approvals itself; enter saves a machine-local override of the checked-in setting",
	}, {
		name:  mouseKey,
		value: mouseValue,
		gloss: "on captures the mouse in this TUI; Shift-drag (Option/Alt on some terminals) then selects text; enter saves a machine-local override",
	}, {
		name:  themeKey,
		value: themeValue,
		gloss: "light expects a light terminal background; +/- previews live, esc reverts; enter saves a machine-local override",
	}}
}

// initiativesText is the Initiatives row: both settings and the shared file they came from.
func initiativesText(i cfglayer.Initiatives) string {
	if i.Unusable {
		return "unusable  (" + i.Path + ")"
	}
	return fmt.Sprintf("required %s · external ref %s  (%s)", onOff(i.Required), onOff(i.ExternalRef), i.Path)
}

// autoApproveText is the Auto-approve row: on/off and its source (ADR 0353).
func autoApproveText(a loop.AutoApprove) string {
	shared := ""
	if a.SharedSet {
		shared = onOff(a.Shared)
	}
	return fmt.Sprintf("%s  (%s)", onOff(a.On), sourceText(a.Source, a.Unusable, shared))
}

// mouseText is the Mouse mode row: on/off and its source.
func mouseText(a loop.Mouse) string {
	shared := ""
	if a.SharedSet {
		shared = onOff(a.Shared)
	}
	return fmt.Sprintf("%s  (%s)", onOff(a.On), sourceText(a.Source, a.Unusable, shared))
}

// themeText is the Theme row: the preset and its source. An unknown value
// reads dark, with the unknown part in warnSt.
func themeText(t loop.Theme) string {
	name := themeNow(t)
	if t.Unknown != "" && t.Unusable == "" {
		return fmt.Sprintf("%s  (%s)", name, warnSt.Render(fmt.Sprintf("%s: unknown %q → %s", t.Source, t.Unknown, name)))
	}
	shared := ""
	if t.SharedSet {
		shared = t.Shared
	}
	return fmt.Sprintf("%s  (%s)", name, sourceText(t.Source, t.Unusable, shared))
}

// sourceText is a row's source: local, config.toml or default, unusable → that
// fallback, or local; shared <the checked-in value> ("" = none set).
func sourceText(source, unusable, shared string) string {
	switch {
	case unusable != "":
		return "unusable → " + source
	case shared != "":
		return source + "; shared " + shared
	}
	return source
}

// stagedValue is a staged row's value, noting a refresh that moved the base under
// the edit (Spec FR26).
func stagedValue(from, to, now string) string {
	if now != from {
		return fmt.Sprintf("%s → %s (unsaved; file now %s)", from, to, now)
	}
	return fmt.Sprintf("%s → %s (unsaved)", from, to)
}

// span is the selected row's pane lines [lo, hi): two when its reason wraps.
type span struct{ lo, hi int }

// settingsLines are every Settings pane line, each row's gloss dimmed under it,
// the selected row's span, and each line's row index (-1 for the title, blurb, rule,
// blanks and mechanical — kinds): a gloss or a wrapped reason carries its row's.
func (m Model) settingsLines() ([]string, span, []int) {
	var lines []string
	var rowOf []int
	var sel span
	row := 0
	add := func(ls ...string) {
		selected := m.setRow == row
		if selected {
			sel = span{len(lines), len(lines) + len(ls)}
		}
		lines = append(lines, ls...)
		for range ls {
			rowOf = append(rowOf, row)
		}
		row++
	}
	inert := func(ls ...string) {
		lines = append(lines, ls...)
		for range ls {
			rowOf = append(rowOf, -1)
		}
	}
	for i, r := range m.settingRows() {
		name := r.name
		if i == m.setRow { // the row +/- act on
			name = selSt.Render(name)
		}
		add(trunc(name+"  "+r.value, m.w), trunc(dimSt.Render("  "+r.gloss), m.w))
	}
	inert("", lipgloss.NewStyle().Bold(true).Render("Kind profiles"),
		trunc(dimSt.Render("  the Agent Profile each kind's sessions launch on; unset falls back to default; — runs no agent session"), m.w))
	add(m.kindRowLines("default", m.setRow == firstKindRow)...)
	inert(dimSt.Render("  " + strings.Repeat("─", max(min(m.w-2, 40), 0))))
	i := 1 // kindKeys index; a — row has none, so the cursor and staging never reach it
	for _, k := range registry.AllKinds() {
		if e, _ := registry.Lookup(k); e.Mechanical {
			inert(trunc(dimSt.Render(fmt.Sprintf("%-14s —", k)), m.w))
			continue
		}
		add(m.kindRowLines(string(k), m.setRow == firstKindRow+i)...)
		i++
	}
	return lines, sel, rowOf
}

// settingsTop is the pane's first line after moving just enough from top to
// keep the selected row's lines in view (Spec FR39, ADR 0136); the top rows show
// the pane from its top. A clipped edge's line becomes its ↑/↓ marker, so the
// row keeps clear of both.
func (m Model) settingsTop(top int) int {
	lines, sel, _ := m.settingsLines()
	return m.settingsTopIn(lines, sel, top)
}

// settingsTopIn is settingsTop over lines settingsLines already built.
func (m Model) settingsTopIn(lines []string, sel span, top int) int {
	h := m.bodyH()
	if bottom := top + h; sel.hi > bottom || sel.hi == bottom && bottom < len(lines) {
		top = sel.hi - h
		if sel.hi < len(lines) {
			top++ // the ↓ marker's line
		}
	}
	if top > 0 && sel.lo <= top {
		top = sel.lo - 1 // the ↑ marker's line
	}
	if m.setRow < firstKindRow {
		top = 0
	}
	return max(min(top, len(lines)-h), 0)
}

// settingsKeys names every pane line for the wheel's anchor: a row's lines by row and line, an
// inert line by its place.
func settingsKeys(rowOf []int) []string {
	keys := make([]string, len(rowOf))
	for k, r := range rowOf {
		keys[k] = "~" + strconv.Itoa(k)
		if r >= 0 {
			j := 0
			for ; k-j > 0 && rowOf[k-j-1] == r; j++ {
			}
			keys[k] = strconv.Itoa(r) + "/" + strconv.Itoa(j)
		}
	}
	return keys
}

// settingsWindow is the pane's first line as drawn in an h tall body: the held anchor line
// while the wheel has the pane Scrolled, else the cursor-following setTop.
func (m Model) settingsWindow(lines []string, sel span, rowOf []int, h int) int {
	if i := m.setLV.held(settingsKeys(rowOf)); i >= 0 {
		return max(min(i, len(lines)-h), 0)
	}
	return m.settingsTopIn(lines, sel, m.setTop)
}

// settingsWheel scrolls the pane by delta lines without moving the cursor.
func (m Model) settingsWheel(h, delta int) Model {
	lines, sel, rowOf := m.settingsLines()
	if len(lines) <= h {
		return m
	}
	top := min(max(m.settingsWindow(lines, sel, rowOf, h)+delta, 0), len(lines)-h)
	m.setLV = scrolledTo(settingsKeys(rowOf), top)
	return m
}

// settingsSnap returns the pane to following the cursor, as a key does: it keeps the held top
// while the cursor's row is in view and re-centres on it otherwise.
func (m Model) settingsSnap(h int) Model {
	if !m.setLV.scrolled {
		return m
	}
	lines, sel, rowOf := m.settingsLines()
	top := m.settingsWindow(lines, sel, rowOf, h)
	m.setLV = listView{}
	if sel.lo < top || sel.hi > top+h {
		top = sel.lo - (h-(sel.hi-sel.lo))/2
	}
	m.setTop = max(min(top, len(lines)-h), 0)
	return m
}

// settingsView is the Settings body: the pane lines from m.setTop, a clipped
// edge replaced by a dimmed ↑/↓ N more counting the lines it hides.
func (m Model) settingsView(h int) string {
	lines, sel, rowOf := m.settingsLines()
	top := m.settingsWindow(lines, sel, rowOf, h) // a refresh that rewrapped a row since the last move
	end := min(top+h, len(lines))
	vis := slices.Clone(lines[top:end])
	if top > 0 {
		vis[0] = dimSt.Render(fmt.Sprintf("  ↑ %d more", top+1))
	}
	if end < len(lines) {
		vis[len(vis)-1] = dimSt.Render(fmt.Sprintf("  ↓ %d more", len(lines)-end+1))
	}
	return strings.Join(vis, "\n")
}

// kindRowLines is one Kind profiles row (ADR 0137 §1-2, §5): the key padded to
// 14, the in-force profile and its source or (built-in), and an unrunnable
// reason on the same line if it fits, else on its own line indented two.
func (m Model) kindRowLines(key string, selected bool) []string {
	r := loop.ResolveProfile(m.kinds.local, m.kinds.shared, key)
	value := "(built-in)"
	if r.Key != "" {
		source, shared := "default", ""
		if r.Key == key {
			source = r.Source
			if r.Source == "local" {
				shared = m.kinds.shared[key]
			}
		}
		value = fmt.Sprintf("%s  (%s)", r.Name, sourceText(source, "", shared))
	}
	target := r.Name // what the row would launch on after a save: the marker speaks for it
	if st, ok := m.kindStaged[key]; ok {
		target, value = st.to, st.to
		if st.to == "" {
			target = m.kindFallback(key)
			value = "unset: " + cmp.Or(target, "(built-in)")
		}
		value = stagedValue(st.from, value, m.kindEffective(key))
	}
	name := fmt.Sprintf("%-14s", key)
	if selected {
		name = selSt.Render(name)
	}
	line := trunc(name+"  "+value, m.w)
	why, bad := m.kinds.reasons[target]
	if target == "" || !bad {
		return []string{line}
	}
	reason := warnSt.Render("unrunnable: " + why)
	if lipgloss.Width(line)+2+lipgloss.Width(reason) <= m.w {
		return []string{line + "  " + reason}
	}
	lines := []string{line}
	for _, l := range strings.Split(ansi.Wrap("unrunnable: "+why, max(m.w-2, 1), ""), "\n") {
		lines = append(lines, "  "+warnSt.Render(l))
	}
	return lines
}

var selSt lipgloss.Style

// trunc cuts s to at most n terminal cells, ending in … when cut.
func trunc(s string, n int) string { return ansi.Truncate(s, max(n, 1), "…") }

// settingsState is the Settings tab's cache from the last refresh and its staged
// edits; View reads only this (ADR 0101).
type settingsState struct {
	pool        loop.Setting
	exit        loop.AutoExit
	kinds       kindProfiles
	initiatives cfglayer.Initiatives
	approvals   loop.AutoApprove
	mouse       loop.Mouse
	theme       loop.Theme
	painted     string // the preset applyTheme last installed through the Model; "" = none yet
	inflight    int
	orphaned    int // AFK claims no live host holds: they hold no pool slot
	staged      int // the pool row's staged value; 0 = nothing staged
	stagedFrom  int // the pool in force when staging began
	exitStaged  bool
	stagedExit  time.Duration
	exitFrom    time.Duration
	kindStaged  map[string]kindStage

	autoApproveStaged bool
	autoApproveStage  autoApproveStage
	mouseStaged       bool
	mouseStage        autoApproveStage
	themeStaged       bool
	themeStage        autoApproveStage
	setRow            int      // 0 pool, 1 Auto-exit, 2 Initiatives, 3 Auto-approve, 4 Mouse mode, 5 Theme, then the kinds from firstKindRow
	setTop            int      // the pane's first line, moved by the cursor and resize (ADR 0136)
	setLV             listView // the wheel's Scrolled viewport: the top line's key holds through refreshes (ADR 0374)
}

// settingsHit is the pointer's view of body line y of the Settings body: the row whose
// lines View drew there, or nothing on an inert line or a ↑/↓ N more marker. It reads the
// same top and markers settingsView does.
func (m Model) settingsHit(h, y int) hitTarget {
	t := hitTarget{caret: -1, line: y}
	lines, sel, rowOf := m.settingsLines()
	top := m.settingsWindow(lines, sel, rowOf, h)
	end := min(top+h, len(lines))
	k := top + y
	switch {
	case y < 0 || k >= end:
	case top > 0 && y == 0, end < len(lines) && k == end-1: // a marker replaced the line
	case rowOf[k] >= 0:
		t.kind, t.key = hitRow, strconv.Itoa(rowOf[k])
		t.sel = rowOf[k] == m.setRow
	}
	return t
}

// orphanedText is the pool readout's orphan suffix, empty when there are none.
func orphanedText(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" · %d orphaned", n)
}
