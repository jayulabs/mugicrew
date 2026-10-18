package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
)

func forceANSI256(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestPrio(t *testing.T) {
	forceANSI256(t)
	if want := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")).Render("P0"); unkept(prio(0)) != want || !strings.Contains(prio(0), "1;") && !strings.Contains(prio(0), ";1") {
		t.Errorf("P0 = %q, want bold 203 %q", prio(0), want)
	}
	if want := lipgloss.NewStyle().Foreground(lipgloss.Color("217")).Render("P1"); unkept(prio(1)) != want || !strings.Contains(prio(1), "38;5;217") || strings.Contains(prio(1), "1m") {
		t.Errorf("P1 = %q, want plain 217 %q", prio(1), want)
	}
	if !strings.Contains(prio(0), "38;5;203") {
		t.Errorf("P0 = %q, want 203", prio(0))
	}
	for p, want := range map[int]string{2: "P2", 3: "P3", 4: "P4"} {
		if got := prio(p); got != want {
			t.Errorf("prio(%d) = %q, want %q", p, got, want)
		}
	}
}

func TestIdeaRowColoursP0(t *testing.T) {
	forceANSI256(t)
	m := ideaModel(t, 100, 24, []bd.Bead{sig("a", "open", 0, ""), sig("b", "open", 2, "")}, nil)
	if v := m.View(); !strings.Contains(v, prio(0)+sep()+"sig a") || !strings.Contains(v, " P2"+sep()+"sig b") {
		t.Errorf("view lacks styled P0 row / bare P2 row:\n%s", v)
	}
}

// unkept drops the selection-keep brackets, which are inert on an unselected row.
func unkept(s string) string { return strings.NewReplacer(keepOn, "", keepOff, "").Replace(s) }
