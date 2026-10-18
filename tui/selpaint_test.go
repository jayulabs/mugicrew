package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func selectedLine(t *testing.T, b *bd.Bead) string {
	t.Helper()
	forceANSI256(t)
	row := " " + kindTag(b) + " " + prio(0) + " " + b.Title + " " + softSt.Render("mc-abc1") + " " + dimSt.Render("hint")
	return listLines(1, func(int) (string, string) { return row, dimSt.Render("3m") }, 60, 0)[0]
}

func TestSelectedRowKeepsBadgeAndPriority(t *testing.T) {
	l := selectedLine(t, &bd.Bead{Kind: contract.KindSignal, Title: "Title"})
	for _, want := range []string{"38;5;186m", "SIGNAL", "P0"} {
		if !strings.Contains(l, want) {
			t.Errorf("selected row lost %q: %q", want, l)
		}
	}
	if strings.Contains(l, "38;5;248m") || strings.Contains(l, "38;5;244m") {
		t.Errorf("selected row kept grey text styling: %q", l)
	}
	if !strings.HasPrefix(ansi.Strip(l), "❯") || ansi.StringWidth(l) != 60 {
		t.Errorf("marker/width wrong: %q", ansi.Strip(l))
	}
}

func TestSelectedContainerKeepsChipBackground(t *testing.T) {
	l := selectedLine(t, &bd.Bead{Kind: contract.KindChart, Container: true, Title: "T"})
	if !strings.Contains(l, "48;5;68") {
		t.Errorf("chip background lost: %q", l)
	}
	if !strings.Contains(l, "\x1b[1;30;48;5;68mCHART") {
		t.Errorf("chip text lost its black-on-kind-colour span: %q", l)
	}
}

func TestSelectedRowBackgroundSurvivesInnerReset(t *testing.T) {
	l := selectedLine(t, &bd.Bead{Kind: contract.KindSignal, Title: "Title"})
	i := strings.Index(l, "SIGNAL")
	rest := l[i:]
	reset := strings.Index(rest, "\x1b[0m")
	if reset < 0 || !strings.HasPrefix(rest[reset+4:], "\x1b[") {
		t.Errorf("background not re-applied after the badge's reset: %q", rest)
	}
}

func TestSepIsSoftGrey(t *testing.T) {
	forceANSI256(t)
	if s := sep(); !strings.Contains(s, "38;5;244m") || ansi.Strip(s) != " · " {
		t.Errorf("sep not soft grey: %q", s)
	}
}

// A real composed row (badgeRow), selected: dots separate title and ID, none
// follows the suffix, and badge and priority colour survive.
func TestSelectedBadgeRowDotsAndSuffix(t *testing.T) {
	forceANSI256(t)
	b := &bd.Bead{Kind: contract.KindSignal, Title: "Title", ID: "mc-abc1", Priority: 2}
	l := listLines(1, func(int) (string, string) { return " " + (graphData{}).sbrn(b, b.Title, false, 0), "3m" }, 60, 0)[0]
	plain := ansi.Strip(l)
	if !strings.Contains(plain, "SIGNAL P2 · Title · mc-abc1") || !strings.HasSuffix(strings.TrimSpace(plain), "3m") || strings.Contains(plain, "· 3m") {
		t.Errorf("dot layout wrong: %q", plain)
	}
	if !strings.Contains(l, "38;5;186m") || strings.Contains(l, "38;5;244m") {
		t.Errorf("selection styling wrong: %q", l)
	}
}

// fgBefore is the foreground colour SGR (38;5;N or 38;2;…) sitting right before the first
// occurrence of text in a rendered line, "" when the text carries none.
func fgBefore(t *testing.T, l, text string) string {
	t.Helper()
	i := strings.Index(l, text)
	if i < 0 {
		t.Fatalf("%q not in %q", text, l)
	}
	ms := sgrRe.FindAllString(l[:i], -1)
	if len(ms) == 0 || !strings.HasSuffix(l[:i], ms[len(ms)-1]) {
		return ""
	}
	if last := ms[len(ms)-1]; strings.Contains(last, "38;") {
		return last
	}
	return ""
}

// A selected row keeps its state glyph's and liveness dot's colour, by source, and
// nothing else: grey glyphs, title, separators and suffix stay plain bold.
func TestSelectedRowKeepsGlyphAndDotColour(t *testing.T) {
	forceANSI256(t)
	mk := func(status string, labels ...string) bd.Bead {
		return bd.Bead{ID: "l", Kind: contract.KindBuild, Status: status, Title: "Hot title", Labels: labels}
	}
	parent := bd.Bead{ID: "c", Kind: contract.KindChart, Status: "open", Labels: []string{"paused:manual"}}
	child := mk("open")
	child.Parent = "c"
	blocked := mk("open")
	blocked.BlockedBy = []string{"o"}
	cases := []struct {
		name  string
		beads []bd.Bead
		sess  session.Info
		live  bool
		text  string // the glyph or dot character
		want  string // its SGR, "" for plain
	}{
		{"in progress", []bd.Bead{mk("in_progress")}, session.Info{}, true, "◐", "\x1b[38;5;220m"},
		{"manual pause", []bd.Bead{mk("open", "paused:manual")}, session.Info{}, false, "‖", "\x1b[38;5;213m"},
		{"inherited pause", []bd.Bead{parent, child}, session.Info{}, false, "‖", "\x1b[38;5;127m"},
		{"blocked", []bd.Bead{blocked, {ID: "o", Kind: contract.KindResearch, Status: "open"}}, session.Info{}, false, "⊘", sgrOf(warnSt.Bold(true).Render("x"))},
		{"alive dot", []bd.Bead{mk("in_progress")}, session.Info{}, true, "●", sgrOf(aliveSt.Render("x"))},
		{"waiting dot", []bd.Bead{mk("in_progress")}, session.Info{State: session.Alive, Waiting: session.WaitTurn}, true, "●", sgrOf(waitSt.Render("x"))},
		{"exited dot", []bd.Bead{mk("in_progress")}, session.Info{State: session.Exited}, false, "✕", sgrOf(deadSty.Render("x"))},
		{"dead dot", []bd.Bead{mk("in_progress")}, session.Info{}, false, "○", sgrOf(deadSty.Render("x"))},
		{"open", []bd.Bead{mk("open")}, session.Info{}, false, "○", ""},
		{"closed", []bd.Bead{mk("closed")}, session.Info{}, false, "✓", ""},
		{"no session", []bd.Bead{mk("closed")}, session.Info{}, false, "·", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := graphData{beads: c.beads, sessions: map[string]session.Info{"l": c.sess}, live: map[string]bool{"l": c.live}}
			b := g.beadByID("l")
			row := " " + g.sbrn(b, "Hot title", true, 0)
			l := listLines(1, func(int) (string, string) { return row, "3m" }, 60, 0)[0]
			if got := fgBefore(t, l, c.text); got != c.want {
				t.Errorf("%s: fg before %q = %q, want %q in %q", c.name, c.text, got, c.want, l)
			}
			if !strings.Contains(l, "\x1b[1;97;48;5;25m") && !strings.Contains(l, "48;5;25") || ansi.StringWidth(l) != 60 {
				t.Errorf("selection bar broken: %q", l)
			}
			for _, plain := range []string{"Hot title", "mc-", "3m"} {
				if strings.Contains(ansi.Strip(l), plain) && fgBefore(t, l, plain) != "" {
					t.Errorf("%q carries a colour: %q", plain, l)
				}
			}
		})
	}
}

// sgrOf is the opening SGR a rendered span starts with.
func sgrOf(s string) string { return sgrRe.FindString(s) }

// A span whose SGR equals a kept one (P0's bold red) but isn't from a kept source stays plain.
func TestSelectedRowSgrCollisionStaysPlain(t *testing.T) {
	forceANSI256(t)
	row := " " + prio0Sty.Render("collide") + " " + softSt.Render("soft")
	l := listLines(1, func(int) (string, string) { return row, "" }, 40, 0)[0]
	for _, w := range []string{"collide", "soft"} {
		if got := fgBefore(t, l, w); got != "" {
			t.Errorf("%q kept colour %q: %q", w, got, l)
		}
	}
}

// kindTag renders the kinds that once fell back to grey in their own colour; an
// unknown kind still falls back to 244.
func TestKindTagOwnColours(t *testing.T) {
	forceANSI256(t)
	for _, c := range []struct {
		kind contract.Kind
		want string
	}{
		{contract.KindSurgery, "38;5;93m"},
		{contract.KindInitiative, "38;5;255m"},
		{contract.KindLanding, "38;5;209m"},
		{"no-such-kind", "38;5;244m"},
	} {
		got := kindTag(&bd.Bead{Kind: c.kind})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s = %q, want %s", c.kind, got, c.want)
		}
		if c.kind != "no-such-kind" && strings.Contains(got, "38;5;244m") {
			t.Errorf("%s fell back to 244: %q", c.kind, got)
		}
	}
}
