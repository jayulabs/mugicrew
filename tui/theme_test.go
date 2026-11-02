package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/mdrender"
	"jayulabs.org/mugicrew/registry"
	"jayulabs.org/mugicrew/session"
)

// forceTheme installs p and restores dark when the test ends, as forceANSI256 does
// for the color profile.
func forceTheme(t *testing.T, p palette) {
	t.Helper()
	applyTheme(p)
	t.Cleanup(func() { applyTheme(darkPalette) })
}

// repaint is dark with every color role changed to a distinct value, so a style
// that applyTheme forgot to rebuild keeps its dark SGR.
func repaint() palette {
	p := darkPalette
	n := 0
	next := func() lipgloss.Color {
		n++
		return lipgloss.Color(string(rune('0'+n/100)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10)))
	}
	v := reflect.ValueOf(&p).Elem()
	for i := 0; i < v.NumField(); i++ {
		if f := v.Field(i); f.Type() == reflect.TypeOf(lipgloss.Color("")) {
			f.Set(reflect.ValueOf(next()))
		}
	}
	p.Pause = map[string]lipgloss.Color{}
	for k := range darkPalette.Pause {
		p.Pause[k] = next()
	}
	p.Kind = map[contract.Kind]lipgloss.Color{}
	for k := range darkPalette.Kind {
		p.Kind[k] = next()
	}
	return p
}

func TestDarkPaletteComplete(t *testing.T) {
	v := reflect.ValueOf(darkPalette)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if c, ok := v.Field(i).Interface().(lipgloss.Color); ok && c == "" && name != "ShadeInk" {
			t.Errorf("role %s is empty", name)
		}
	}
	for _, k := range registry.AllKinds() {
		if darkPalette.Kind[k] == "" {
			t.Errorf("kind %q has no color", k)
		}
	}
	for _, c := range bd.PauseRank {
		if darkPalette.Pause[c] == "" {
			t.Errorf("pause cause %q has no color", c)
		}
	}
	if darkPalette.Pause[pauseInherited] == "" {
		t.Error("inherited pause has no color")
	}
}

// Dark equals today's literals: a few that no SGR-pinning test reaches.
func TestDarkPaletteIsTodaysLiterals(t *testing.T) {
	for name, c := range map[string]lipgloss.Color{
		"accent": accent, "hintBg": hintBg, "dim": dim, "inProgress": inProgSty.GetForeground().(lipgloss.Color),
		"unknown kind": kindFallback, "chip ink": chipInk,
		"hover": hoverRow.GetBackground().(lipgloss.Color), "anc": ancRow.GetBackground().(lipgloss.Color),
		"pick": pickRow.GetBackground().(lipgloss.Color), "teal": tealSt.GetForeground().(lipgloss.Color),
		"burn": burnSty.GetForeground().(lipgloss.Color), "caution": usageWarnSty.GetForeground().(lipgloss.Color),
	} {
		want := map[string]lipgloss.Color{
			"accent": "212", "hintBg": "236", "dim": "240", "inProgress": "220", "unknown kind": "244", "chip ink": "0",
			"hover": "17", "anc": "236", "pick": "237",
			"teal": "109", "burn": "180", "caution": "214",
		}[name]
		if c != want {
			t.Errorf("%s = %q, want %q", name, c, want)
		}
	}
	if ancRow.GetForeground() != (lipgloss.NoColor{}) || hoverRow.GetForeground() != (lipgloss.NoColor{}) || pickRow.GetForeground() != (lipgloss.NoColor{}) {
		t.Error("dark shades carry an ink")
	}
}

// applyTheme rebuilds the derived copies, not just the bases.
func TestApplyThemeRebuildsDerivedStyles(t *testing.T) {
	forceANSI256(t)
	p := repaint()
	p.ShadeInk = "235"
	forceTheme(t, p)
	for name, got := range map[string]lipgloss.Style{
		"dead": deadSty, "blocked": blockedSty, "label": labelSt, "blockerMk": blockerMk, "prio0": prio0Sty, "prio1": prio1Sty,
		"usageWarn": usageWarnSty, "usageErr": usageErrSty, "section": sectionSt, "sel": selSt, "teal": tealSt,
		"failing": pauseSty[bd.PausedFailing], "inherit": inheritSty, "done": doneLook.sty, "look": sessionLooks[session.Alive].sty,
		"hintKey": hintHelp.Styles.ShortKey, "hintDesc": hintHelp.Styles.ShortDesc, "refKey": hintHelp.Styles.FullKey,
		"refDesc": hintHelp.Styles.FullDesc, "refSep": hintHelp.Styles.FullSeparator, "ellipsis": hintHelp.Styles.Ellipsis,
	} {
		fg, ok := got.GetForeground().(lipgloss.Color)
		if !ok || len(fg) != 3 || fg[0] != '0' {
			t.Errorf("%s still has a dark foreground %v", name, got.GetForeground())
		}
	}
	for name, st := range map[string]lipgloss.Style{"anc": ancRow, "pick": pickRow, "hover": hoverRow} {
		if st.GetForeground() != lipgloss.Color("235") {
			t.Errorf("%s lacks the shade ink", name)
		}
	}
	round := darkPalette
	applyTheme(round)
	if deadSty.GetForeground() != lipgloss.Color("203") {
		t.Error("dark not restored")
	}
}

// A burn drawn after applyTheme uses the newly installed style, with no recompute.
func TestBurnRepaintsOnTheme(t *testing.T) {
	forceANSI256(t)
	reset := burnNow.Add(2 * time.Hour)
	b := newBurn([]eventlog.Event{usageReadEv(burnNow.Add(-time.Minute), 50, reset)}, burnWins[0], burnNow)
	before := strings.Join(b.chart(60, 8), "\n")
	p := darkPalette
	p.Burn = "99"
	forceTheme(t, p)
	after := strings.Join(b.chart(60, 8), "\n")
	if before == after || !strings.Contains(after, "38;5;99") || strings.Contains(after, "38;5;180") {
		t.Errorf("burn chart did not repaint:\n%s", after)
	}
}

func TestApplyThemeSetsMarkdownPalette(t *testing.T) {
	p := darkPalette
	p.Accent, p.Soft, p.MDCode, p.MDLink = "1", "2", "3", "4"
	forceTheme(t, p)
	out := mdrender.Render("# H\n\n`c` [l](http://x)\n\n> q", 40)
	for _, want := range []string{"38;5;1m", "38;5;2m", "38;5;3m", "38;5;4m"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %q", want, out)
		}
	}
}

var presets = palettes

func TestPresetsComplete(t *testing.T) {
	for name, p := range presets {
		v := reflect.ValueOf(p)
		for i := 0; i < v.NumField(); i++ {
			fn := v.Type().Field(i).Name
			if c, ok := v.Field(i).Interface().(lipgloss.Color); ok && c == "" && !(fn == "ShadeInk" && name != "light") {
				t.Errorf("%s: role %s is empty", name, fn)
			}
		}
		for _, k := range registry.AllKinds() {
			if p.Kind[k] == "" {
				t.Errorf("%s: kind %q has no color", name, k)
			}
		}
		for _, c := range bd.PauseRank {
			if p.Pause[c] == "" {
				t.Errorf("%s: pause cause %q has no color", name, c)
			}
		}
		if p.Pause[pauseInherited] == "" {
			t.Errorf("%s: inherited pause has no color", name)
		}
	}
	if lightPalette.ShadeInk == "" {
		t.Error("light has no shade ink")
	}
	if !highContrastPalette.Bolder || lightPalette.Bolder || darkPalette.Bolder {
		t.Error("Bolder should be set only in high-contrast")
	}
}

func fgSGR(c lipgloss.Color) string { return lipgloss.NewStyle().Foreground(c).Render("x") }
func bgSGR(c lipgloss.Color) string { return lipgloss.NewStyle().Background(c).Render("x") }
func sgr(st lipgloss.Style) string  { return st.Render("x") }
func fgOf(st lipgloss.Style) string { return fgSGR(st.GetForeground().(lipgloss.Color)) }
func bgOf(st lipgloss.Style) string { return bgSGR(st.GetBackground().(lipgloss.Color)) }

// Under each preset every role's style, derived copies included, renders that preset's SGR.
func TestPresetStylesRenderThePresetsColors(t *testing.T) {
	forceANSI256(t)
	for name, p := range presets {
		forceTheme(t, p)
		for role, c := range map[string]struct {
			st lipgloss.Style
			c  lipgloss.Color
		}{
			"dim": {dimSt, p.Dim}, "warn": {warnSt, p.Warn}, "alive": {aliveSt, p.Alive}, "afk": {afkSt, p.AFK},
			"wait": {waitSt, p.WaitsOnYou}, "soft": {softSt, p.Soft}, "pink": {pinkFg, p.BlockerPink},
			"dead": {deadSty, p.Warn}, "blocked": {blockedSty, p.Warn}, "label": {labelSt, p.Soft}, "blockerMk": {blockerMk, p.BlockerPink},
			"prio0": {prio0Sty, p.P0Fg}, "prio1": {prio1Sty, p.P1Fg}, "usageWarn": {usageWarnSty, p.Caution}, "usageErr": {usageErrSty, p.Warn},
			"teal": {tealSt, p.StatsBars}, "burn": {burnSty, p.Burn}, "inprog": {inProgSty, p.InProgress},
			"failing": {pauseSty[bd.PausedFailing], p.Pause[bd.PausedFailing]}, "inherit": {inheritSty, p.Pause[pauseInherited]},
			"hintKey": {hintHelp.Styles.ShortKey, p.HintKey}, "hintDesc": {hintHelp.Styles.ShortDesc, p.HintDesc},
			"refKey": {hintHelp.Styles.FullKey, p.RefKey}, "refDesc": {hintHelp.Styles.FullDesc, p.RefDesc},
			"look": {sessionLooks[session.Alive].sty, p.Alive}, "lookDead": {sessionLooks[session.Exited].sty, p.Warn},
			"wait look": {waitLooks[session.WaitTurn].sty, p.WaitsOnYou}, "done": {doneLook.sty, p.Alive},
			"selMarker": {selMarker, p.SelMarker}, "tabInactive": {tabInactive, p.TabInactive}, "tabHover": {tabHover, p.TabHover},
		} {
			if got, want := fgOf(c.st), fgSGR(c.c); got != want {
				t.Errorf("%s/%s: %q, want %q", name, role, got, want)
			}
		}
		for role, c := range map[string]struct {
			st lipgloss.Style
			c  lipgloss.Color
		}{"sel": {selRow, p.SelBg}, "hover": {hoverRow, p.HoverBg}, "anc": {ancRow, p.AncestorBg}, "pick": {pickRow, p.PickerBg}} {
			if got, want := bgOf(c.st), bgSGR(c.c); got != want {
				t.Errorf("%s/%s bg: %q, want %q", name, role, got, want)
			}
		}
		for role, c := range map[string]struct{ got, want lipgloss.Color }{
			"chip ink": {chipInk, p.ChipInk}, "fallback": {kindFallback, p.UnknownKind},
			"accent": {accent, p.Accent},
		} {
			if c.got != c.want {
				t.Errorf("%s/%s = %q, want %q", name, role, c.got, c.want)
			}
		}
		for cause, c := range p.Pause {
			if cause != pauseInherited && fgOf(pauseSty[cause]) != fgSGR(c) {
				t.Errorf("%s: pause %s stale", name, cause)
			}
		}
		for k, c := range p.Kind {
			if kindColors[k] != c {
				t.Errorf("%s: kind %s stale", name, k)
			}
		}
		if (name == "light") != (hoverRow.GetForeground() == p.ShadeInk && ancRow.GetForeground() == p.ShadeInk && pickRow.GetForeground() == p.ShadeInk) {
			t.Errorf("%s: shade ink presence wrong", name)
		}
		if tabInactive.GetBold() != p.Bolder || tabHover.GetBold() != p.Bolder || hintHelp.Styles.FullKey.GetBold() != p.Bolder {
			t.Errorf("%s: Bolder not applied", name)
		}
	}
}

// The relationships ADR 0372, 0179 and 0057 protect hold in every preset.
func TestPresetRelationships(t *testing.T) {
	for name, p := range presets {
		bgs := map[string]lipgloss.Color{"sel": p.SelBg, "hover": p.HoverBg, "anc": p.AncestorBg, "pick": p.PickerBg}
		for a, ca := range bgs {
			for b, cb := range bgs {
				if a < b && ca == cb {
					t.Errorf("%s: %s and %s share bg %s", name, a, b, ca)
				}
			}
		}
		if p.P1Fg == p.BlockerPink {
			t.Errorf("%s: P1 is the blocker pink", name)
		}
		if name != "dark" && p.Caution == p.WaitsOnYou {
			t.Errorf("%s: caution equals waits-on-you", name)
		}
		seen := map[lipgloss.Color]string{}
		for cause, c := range p.Pause {
			if prev, dup := seen[c]; dup {
				t.Errorf("%s: pause %s and %s share %s", name, prev, cause, c)
			}
			seen[c] = cause
		}
		for k, c := range p.Kind {
			if c == p.ChipInk {
				t.Errorf("%s: chip ink equals kind %s hue", name, k)
			}
		}
		if p.Kind[contract.KindDiscussion] != p.Kind[contract.KindCheckup] || p.Kind[contract.KindLanding] != p.Kind[contract.KindMRWatch] {
			t.Errorf("%s: shared kind hues diverge", name)
		}
		if (p.ShadeInk != "") != (name == "light") {
			t.Errorf("%s: shade ink = %q", name, p.ShadeInk)
		}
		if name == "high-contrast" {
			for _, c := range []lipgloss.Color{p.Dim, p.Soft, p.HintDesc, p.RefKey} {
				if c == "240" || c == "244" {
					t.Errorf("high-contrast has dim gray %s", c)
				}
			}
		}
		if name == "light" {
			v := reflect.ValueOf(p)
			for i := 0; i < v.NumField(); i++ {
				if c, ok := v.Field(i).Interface().(lipgloss.Color); ok {
					if n, err := strconv.Atoi(string(c)); err == nil && n < 16 {
						t.Errorf("light %s uses terminal-theme index %s", v.Type().Field(i).Name, c)
					}
				}
			}
		}
	}
}

// Switching presets and back leaves the Graph View byte-identical to the first dark render.
func TestThemeRoundTripRendersIdentically(t *testing.T) {
	forceANSI256(t)
	m := graphModel(t, 120, 40)
	first := m.View()
	for _, name := range []string{"light", "high-contrast"} {
		applyTheme(presets[name])
		if m.View() == first {
			t.Errorf("%s render equals dark", name)
		}
	}
	applyTheme(darkPalette)
	if m.View() != first {
		t.Error("dark render changed after a round trip")
	}
}

func TestWithThemePaintsFirstViewInPreset(t *testing.T) {
	forceANSI256(t)
	t.Cleanup(func() { applyTheme(darkPalette) })
	view := func(m Model) string {
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
		return m.View()
	}
	dark := view(New(nil, nil))
	if got := view(New(nil, nil).WithTheme("dark")); got != dark {
		t.Error("explicit dark differs from the default")
	}
	for _, name := range []string{"light", "high-contrast"} {
		applyTheme(darkPalette)
		got := view(New(nil, nil).WithTheme(name))
		applyTheme(darkPalette)
		if got == dark {
			t.Errorf("%s first View is still dark", name)
		}
		applyTheme(palettes[name])
		if want := view(New(nil, nil)); got != want {
			t.Errorf("%s first View != that palette's render", name)
		}
	}
	applyTheme(darkPalette)
	if New(nil, nil).WithTheme("solarized"); view(New(nil, nil)) != dark {
		t.Error("unknown name changed the palette")
	}
}

func TestBeadDetailFollowsAppliedTheme(t *testing.T) {
	forceANSI256(t)
	t.Cleanup(func() { applyTheme(darkPalette) })
	orig := listOpen
	t.Cleanup(func() { listOpen = orig })
	listOpen = func() ([]bd.Bead, error) { return mdBeads(), nil }
	dark, err := BeadDetail(nil, nil, "md", 60)
	if err != nil {
		t.Fatal(err)
	}
	ApplyThemeName("light")
	light, err := BeadDetail(nil, nil, "md", 60)
	if err != nil {
		t.Fatal(err)
	}
	if light == dark {
		t.Error("light drawer (and its Markdown) is byte-identical to dark")
	}
}
