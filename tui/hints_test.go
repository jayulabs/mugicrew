package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func hintBarAt(bs []key.Binding, w int) string {
	return strings.TrimSpace(ansi.Strip(sectionedBar(bs, w, true)))
}

// A Graph open-bead row reads row | tab | nav | ? keys (mc-bruk FR1–2).
func TestGraphBarSections(t *testing.T) {
	m := press(t, graphModel(t, 120, 30), runeKey("d"))
	l := lines(m.View())
	bar := strings.TrimSpace(ansi.Strip(l[len(l)-1]))
	if parts := strings.Split(bar, " | "); len(parts) != 4 ||
		!strings.HasPrefix(parts[0], "enter ") || !strings.HasPrefix(parts[1], "/ ") ||
		!strings.HasPrefix(parts[2], "g goto") || !strings.Contains(parts[2], "d close") || parts[3] != "? keys" {
		t.Errorf("bar = %q", bar)
	}
	for _, off := range []string{"j/k", "J/K", "pgup"} {
		if strings.Contains(bar, off) {
			t.Errorf("%s on the bar: %q", off, bar)
		}
	}
}

// Descending widths drop +/- prio before d, keep enter longest, and pin g goto and ? keys.
func TestBarDropsByRank(t *testing.T) {
	bs := []key.Binding{
		verbKey("enter", "launch"), keyBg, verbKey("p", "pause"), keyEdit, keyPrio,
		keySearch, keyFold, keyDetail, keyGoto, keyNew,
	}
	if b := hintBarAt(bs, 200); b != "enter launch • b bg • p pause • e edit • +/- prio | / search | g goto • n new • space fold • d detail | ? keys" {
		t.Errorf("wide = %q", b)
	}
	has := func(b, s string) bool { return strings.Contains(b, s) }
	verbs := []string{"bg", "pause", "edit", "prio", "search", "new", "fold", "detail", "goto"}
	for w := 120; w >= 40; w-- {
		b := hintBarAt(bs, w)
		if lipgloss.Width(b) > w || has(b, "…") || !has(b, "g") || (!has(b, "keys") && !has(b, " ?")) {
			t.Fatalf("w=%d bar %q", w, b)
		}
		// every label goes key-only before any item disappears
		labels, items := 0, strings.Count(b, " • ")+strings.Count(b, " | ")+1
		for _, v := range verbs {
			if has(b, " "+v) {
				labels++
			}
		}
		if labels > 0 && items < len(bs)+1 {
			t.Errorf("w=%d dropped before every label was key-only: %q", w, b)
		}
		// enter keeps its verb longest among the unpinned
		if has(b, "enter launch") == false && (has(b, "b bg") || has(b, "d detail") || has(b, "p pause")) {
			t.Errorf("w=%d enter shortened before others: %q", w, b)
		}
	}
	// below the shorten floor, +/- prio is gone before d
	if b := hintBarAt(bs, 40); !has(b, "d") || has(b, "+/-") {
		t.Errorf("40 cols = %q", b)
	}
}

// A section with nothing left takes its | with it; a/A is one item; j/k are off the bar.
func TestBarEmptySectionsAndMergedAdopt(t *testing.T) {
	if b := hintBarAt([]key.Binding{keyNew, keyFold, keyGoto}, 80); b != "g goto • n new • space fold | ? keys" {
		t.Errorf("bar = %q", b)
	}
	if b := hintBarAt([]key.Binding{keyMove, keySection, keyScroll}, 80); b != "? keys" {
		t.Errorf("moving keys on the bar: %q", b)
	}
	if b := hintBarAt([]key.Binding{keyAdopt, keyRelease}, 80); b != "a/A adopt/release | ? keys" {
		t.Errorf("both halves = %q", b)
	}
	if b := hintBarAt([]key.Binding{keyRelease}, 80); b != "A release | ? keys" {
		t.Errorf("one half = %q", b)
	}
}

// One key, two verbs: placement follows the verb, not the letter.
func TestBarSharedKeysByVerb(t *testing.T) {
	bs := []key.Binding{keyErrOnly, keyEdit, keyGroup, keyBg, keyStage, keyPrio, keySave, keyDiscard}
	want := "e edit • b bg • +/- prio | e errors • b group by • +/- stage • enter save • esc discard | ? keys"
	if b := hintBarAt(bs, 200); b != want {
		t.Errorf("bar = %q, want %q", b, want)
	}
}

// A flat line (dialog box, / bar) keeps its labels when it fits and goes key-only when not (FR18).
func TestFlatHintsShortenToKeyOnly(t *testing.T) {
	bs := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "revert")),
	}
	if got := ansi.Strip(flatHints(bs, 40)); got != "enter keep • esc revert" {
		t.Errorf("fitting = %q", got)
	}
	if got := ansi.Strip(flatHints(bs, 12)); got != "enter • esc" {
		t.Errorf("narrow = %q", got)
	}
}

// Stats' d dist is a tab extra beside u; a Graph-style d detail stays in nav.
func TestBarStatsDistInTabSection(t *testing.T) {
	bs := []key.Binding{keyWindow, keyGroup, keyLimit, keyDist, keyDetail}
	want := "w window • b group by • u 5h/wk • d dist time/cost | d detail | ? keys"
	if b := hintBarAt(bs, 200); b != want {
		t.Errorf("bar = %q, want %q", b, want)
	}
}

func TestHintOfSplitsUByVerb(t *testing.T) {
	u := func(desc string) hintItem { return hintOf(key.NewBinding(key.WithKeys("u"), key.WithHelp("u", desc))) }
	if it := u("undo"); it.sec != secRowHint || it.rank != 16 {
		t.Errorf("u undo = sec %d rank %d, want row section rank 16", it.sec, it.rank)
	}
	if it := u("5h/wk"); it.sec != secTabHint || it.rank != tabExtraRank {
		t.Errorf("u 5h/wk = sec %d rank %d, want tab section rank %d", it.sec, it.rank, tabExtraRank)
	}
}
