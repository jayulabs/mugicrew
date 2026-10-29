package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestDrawerWinBoxesTheOpenDrawer(t *testing.T) {
	forceANSI256(t)
	const w, h = 30, 14
	vp := drawerVP(true, w, h, "alpha\nbeta", 0)
	line := func(i int) string { return "row" }
	got := drawerWin(20, line, 0, true, w, h, vp, nil, nil)
	ls := strings.Split(got, "\n")
	if len(ls) != h {
		t.Fatalf("height = %d, want %d:\n%s", len(ls), h, got)
	}
	plain := strings.Split(ansi.Strip(got), "\n")
	top, bot := plain[drawerH(true, h)], plain[h-1]
	if !strings.HasPrefix(top, "╭") || !strings.HasSuffix(top, "╮") || lipgloss.Width(top) != w {
		t.Errorf("top edge = %q", top)
	}
	if !strings.HasPrefix(bot, "╰") || !strings.HasSuffix(bot, "╯") || lipgloss.Width(bot) != w {
		t.Errorf("bottom edge = %q", bot)
	}
	for _, l := range plain[drawerH(true, h)+1 : h-1] {
		if !strings.HasPrefix(l, "│") || !strings.HasSuffix(l, "│") || lipgloss.Width(l) != w {
			t.Errorf("interior row = %q", l)
		}
	}
	if want := lipgloss.NewStyle().Foreground(accent).Render("╭" + strings.Repeat("─", w-2) + "╮"); ls[drawerH(true, h)] != want {
		t.Errorf("top edge %q lacks accent %q", ls[drawerH(true, h)], want)
	}
	if shut := drawerWin(20, line, 0, false, w, h, drawerVP(false, w, h, "x", 0), nil, nil); strings.ContainsAny(shut, "╭╮╰╯│") {
		t.Errorf("shut drawer has border glyphs:\n%s", shut)
	}
}

func TestDrawerWinTinyBodyDoesNotPanic(t *testing.T) {
	for h := 1; h <= 4; h++ {
		_ = drawerWin(5, func(int) string { return "r" }, 0, true, 3, h, drawerVP(true, 3, h, "x", 0), nil, nil)
	}
}

func TestListGeomHitBoxEdges(t *testing.T) {
	const h = 12
	g := listGeom{n: 3, h: h, open: true, at: func(int) hitTarget { return hitTarget{} }}
	for y := range h {
		got := g.hit(listView{}, y).kind
		want := hitNone
		if y > h/2 && y < h-1 {
			want = hitDrawer
		}
		if y >= h/2 && got != want {
			t.Errorf("y=%d kind = %v, want %v", y, got, want)
		}
	}
}
