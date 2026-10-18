package tui

// The Settings tab (ADR 0109; Spec FR54–56): one full-width pane of setting rows,
// each drawn from what the refresh cached (ADR 0101): View never reads a file.

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
// [kind_profiles] maps, the sorted catalog names (the +/- cycle), and the
// unrunnable reason of each catalog or mapped name that can't run.
type kindProfiles struct {
	local, shared map[string]string
	catalog       []string
	profiles      map[string]agentprofile.Profile // the loaded catalog, for the batch runnable check
	catalogPath   string                          // the checked-in catalog file, for the empty-catalog flash
	reasons       map[string]string               // name → ADR 0123 §1 reason; absent = runnable
}

// readKindProfiles is the refresh's mapping and catalog read, a var as readPool is.
var readKindProfiles = func() kindProfiles {
	root := repoRoot()
	common, _ := git.CommonDirOf(root) // unresolved ⇒ "" ⇒ a missing local catalog layer
	local, shared := loop.KindProfiles(root)
	oe := cmp.Or(os.Getenv("OE_ROOT"), root) // where agentprofile.Load reads the checked-in layer
	loaded := agentprofile.Load(root, common)
	return kindProfiles{local: local, shared: shared, catalog: slices.Sorted(maps.Keys(loaded)), profiles: loaded,
		catalogPath: filepath.Join(oe, "agent-profiles.toml")}
}

// saveKindProfile is enter's local [kind_profiles] write ("" name deletes the
// key), a var as savePool is.
var saveKindProfile = func(key, name string) error {
	root := repoRoot()
	return loop.SetLocalKindProfile(root, key, name)
}

// profileReasons checks the loaded catalog in one ctx.ProfileReasons call (not
// RefuseProfile: this is no refusal, so no profile.unusable); a mapped name
// absent from the catalog is "not in catalog" without a call, profiled ctx or
// not. A ctx that isn't profiled runs every catalog name on its own harness, so
// none of those is unrunnable.
func profileReasons(ctx contract.Ctx, kp kindProfiles) map[string]string {
	out := map[string]string{}
	if p, ok := ctx.(profiled); ok {
		if r := p.ProfileReasons(kp.profiles); r != nil {
			out = r
		}
	}
	for _, name := range slices.Concat(slices.Collect(maps.Values(kp.local)), slices.Collect(maps.Values(kp.shared))) {
		if !slices.Contains(kp.catalog, name) {
			out[name] = "not in catalog"
		}
	}
	return out
}

// profiled is main's Ctx (ADR 0125) as far as Settings needs it: the batch
// runnability check of a loaded catalog. A Ctx that isn't one runs everything.
type profiled interface {
	ProfileReasons(catalog map[string]agentprofile.Profile) map[string]string
}

// Settings keys (Spec FR55): enter/esc show only while the selected row is staged (FR56).
var (
	keyRowDown = key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "row"))
	keyRowUp   = key.NewBinding(key.WithKeys("k", "up"))
	keyStage   = key.NewBinding(key.WithKeys("+", "-"), key.WithHelp("+/-", "stage"))
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
	if m.setRow == initiativesRow { // read-only: the setting is shared, there is nothing to write locally
		return m, nil, k == "+" || k == "-" || k == "esc" || k == "enter"
	}
	if m.setRow == 1 {
		return m.autoExitEdit(k)
	}
	cur := m.staged
	if cur == 0 {
		cur = m.pool.N
	}
	switch k {
	case "+", "-":
		if k == "-" && cur <= 1 {
			return m, nil, true
		}
		if m.staged == 0 {
			m.stagedFrom = m.pool.N
		}
		m.staged = cur + 1
		if k == "-" {
			m.staged = cur - 1
		}
		if m.staged == m.pool.N { // stepped back to the value in force: nothing staged
			m.staged = 0
		}
	case "esc":
		m.staged = 0
	case "enter":
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

// autoExitEdit is settingsKey on the Auto-exit row: steps of 30s, floor off, no ceiling.
func (m Model) autoExitEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	cur := m.exit.D
	if m.exitStaged {
		cur = m.stagedExit
	}
	switch k {
	case "+", "-":
		if !m.exitStaged {
			m.exitFrom = m.exit.D
		}
		m.stagedExit = cur + autoExitStep
		if k == "-" {
			m.stagedExit = max(cur-autoExitStep, 0)
		}
		m.exitStaged = m.stagedExit != m.exit.D // back at the value in force: nothing staged
	case "esc":
		m.exitStaged = false
	case "enter":
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
	next, cmd := m.startWrite(slot(pk), "saving", func() writeDoneMsg {
		err := save()
		return writeDoneMsg{slot(pk), func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				m.setStatus(pk + " save failed: " + err.Error())
				return m, nil
			}
			unstage(&m)
			m.setStatus(saved)
			cmd := m.slowLoad()
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

// firstKindRow is the first Kind profiles row.
const firstKindRow = 3

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
// step through the sorted catalog plus a trailing (unset) slot, from the staged
// value else the local key; back at the local key nothing is staged; esc drops
// this row's edit only; enter saves it under the row's own pending key.
func (m Model) kindEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	key := kindRowKey(m.setRow)
	local := m.kinds.local[key]
	switch k {
	case "+", "-":
		if len(m.kinds.catalog) == 0 && local == "" {
			m.setStatus("no profiles in " + m.kinds.catalogPath)
			return m, nil, true
		}
		st, staged := m.kindStaged[key]
		if !staged {
			st = kindStage{to: local, from: m.kindEffective(key)}
		}
		slots := append(slices.Clone(m.kinds.catalog), "")
		i := slices.Index(slots, st.to)
		switch {
		case i < 0 && k == "+": // a name missing from the catalog: + to the first, - to the last slot
			i = 0
		case i < 0:
			i = len(slots) - 1
		case k == "+":
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
	case "esc":
		m.kindStaged = maps.Clone(m.kindStaged)
		delete(m.kindStaged, key)
	case "enter":
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
func (m Model) settingsStaged() bool { return m.staged != 0 || m.exitStaged || len(m.kindStaged) > 0 }

// rowStaged is whether the selected row holds an unsaved edit (ADR 0138 §2).
func (m Model) rowStaged() bool {
	switch m.setRow {
	case 0:
		return m.staged != 0
	case 1:
		return m.exitStaged
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
	return []settingRow{{
		name:  poolKey,
		value: fmt.Sprintf("%s  (%s)  %d in flight", n, sourceText(m.pool.Source, m.pool.Unusable, shared), m.inflight),
		gloss: "how many AFK efforts the loop and run-now start at once on this machine",
	}, {
		name:  autoExitKey,
		value: fmt.Sprintf("%s  (%s)", e, sourceText(m.exit.Source, m.exit.Unusable, eshared)),
		gloss: "how long a HITL session waits after its closing line before it exits by itself",
	}, {
		name:  "Initiatives",
		value: initiativesText(m.initiatives),
		gloss: "shared [initiatives] in .mugicrew/config.toml: edit the file to change it",
	}}
}

// initiativesText is the Initiatives row: both settings and the shared file they came from.
func initiativesText(i cfglayer.Initiatives) string {
	if i.Unusable {
		return "unusable  (" + i.Path + ")"
	}
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	return fmt.Sprintf("required %s · external ref %s  (%s)", onOff(i.Required), onOff(i.ExternalRef), i.Path)
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
// and the selected row's span.
func (m Model) settingsLines() ([]string, span) {
	var lines []string
	var sel span
	add := func(selected bool, ls ...string) {
		if selected {
			sel = span{len(lines), len(lines) + len(ls)}
		}
		lines = append(lines, ls...)
	}
	for i, r := range m.settingRows() {
		name := r.name
		if i == m.setRow { // the row +/- act on
			name = selSt.Render(name)
		}
		add(i == m.setRow, trunc(name+"  "+r.value, m.w))
		lines = append(lines, trunc(dimSt.Render("  "+r.gloss), m.w))
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Render("Kind profiles"),
		trunc(dimSt.Render("  the Agent Profile each kind's sessions launch on; unset falls back to default; — runs no agent session"), m.w))
	add(m.setRow == firstKindRow, m.kindRowLines("default", m.setRow == firstKindRow)...)
	lines = append(lines, dimSt.Render("  "+strings.Repeat("─", max(min(m.w-2, 40), 0))))
	i := 1 // kindKeys index; a — row has none, so the cursor and staging never reach it
	for _, k := range registry.AllKinds() {
		if e, _ := registry.Lookup(k); e.Mechanical {
			lines = append(lines, trunc(dimSt.Render(fmt.Sprintf("%-14s —", k)), m.w))
			continue
		}
		sel := m.setRow == firstKindRow+i
		add(sel, m.kindRowLines(string(k), sel)...)
		i++
	}
	return lines, sel
}

// settingsTop is the pane's first line after moving just enough from top to
// keep the selected row's lines in view (Spec FR39, ADR 0136); the top rows show
// the pane from its top. A clipped edge's line becomes its ↑/↓ marker, so the
// row keeps clear of both.
func (m Model) settingsTop(top int) int {
	lines, sel := m.settingsLines()
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
	if m.setRow <= initiativesRow {
		top = 0
	}
	return max(min(top, len(lines)-h), 0)
}

// settingsView is the Settings body: the pane lines from m.setTop, a clipped
// edge replaced by a dimmed ↑/↓ N more counting the lines it hides.
func (m Model) settingsView(h int) string {
	lines, sel := m.settingsLines()
	top := m.settingsTopIn(lines, sel, m.setTop) // a refresh that rewrapped a row since the last move
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

var selSt = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)

// trunc cuts s to at most n terminal cells, ending in … when cut.
func trunc(s string, n int) string { return ansi.Truncate(s, max(n, 1), "…") }

// settingsState is the Settings tab's cache from the last refresh and its staged
// edits; View reads only this (ADR 0101).
type settingsState struct {
	pool        loop.Setting
	exit        loop.AutoExit
	kinds       kindProfiles
	initiatives cfglayer.Initiatives
	inflight    int
	staged      int // the pool row's staged value; 0 = nothing staged
	stagedFrom  int // the pool in force when staging began
	exitStaged  bool
	stagedExit  time.Duration
	exitFrom    time.Duration
	kindStaged  map[string]kindStage
	setRow      int // 0 pool, 1 Auto-exit, 2 Initiatives, then the kinds from firstKindRow
	setTop      int // the pane's first line, moved by the cursor and resize (ADR 0136)
}
