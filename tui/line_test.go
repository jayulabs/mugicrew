package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// FR2–4: the window holds the cursor cell and marks the clipped sides with …
func TestLineRenderWindow(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz" // 26 runes
	for _, tc := range []struct {
		name string
		l    line
		w    int
		want string
	}{
		{"short text is unchanged", line{"abc", 1}, 10, "abc"},
		{"short at end", lineAt("abc"), 10, "abc▏"},
		{"start: … right only", line{long, 0}, 9, "abcdefgh…"},
		{"end: … left only", lineAt(long), 9, "…tuvwxyz▏"},
		{"middle: … both", line{long, 13}, 9, "…klmnopq…"},
	} {
		got := ansi.Strip(tc.l.render(tc.w))
		if tc.l.cur < len([]rune(tc.l.text)) && !strings.Contains(tc.l.render(tc.w), cursorSty.Render(string([]rune(tc.l.text)[tc.l.cur]))) {
			t.Errorf("%s: no cursor cell in %q", tc.name, tc.l.render(tc.w))
		}
		if got != tc.want || ansi.StringWidth(got) > tc.w {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestSignalFormLongTitleKeepsCursor(t *testing.T) {
	m := pressN(t, formModel(t), "s")
	m = typeText(t, m, strings.Repeat("x", 120)+"END")
	if v := view(m); !strings.Contains(v, "…xxx") || !strings.Contains(v, "END▏") {
		t.Errorf("cursor not visible:\n%s", v)
	}
}

func TestFormDescriptionAlignsWithLabel(t *testing.T) {
	m := pressN(t, formModel(t), "s")
	m = typeText(t, press(t, press(t, m, tabKey()), tabKey()), "hello")
	var label, text = -1, -1
	for _, l := range strings.Split(view(m), "\n") {
		if i := strings.Index(l, "description:"); i >= 0 {
			label = ansi.StringWidth(l[:i])
		}
		if i := strings.Index(l, "hello"); i >= 0 {
			text = ansi.StringWidth(l[:i])
		}
	}
	if label < 0 || text != label {
		t.Errorf("label col %d, text col %d", label, text)
	}
}
