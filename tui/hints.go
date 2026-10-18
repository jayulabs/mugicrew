package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The hints bar's sections, in bar order (Spec mc-bruk D1, ADR 0229 §1): the selected
// row's verbs, the tab's, moving around, then the ? keys tail.
const (
	secRowHint = iota
	secTabHint
	secNavHint
	secRefHint
)

// hintRank is the shared drop rank (ADR 0229 §3; Spec D2): lower drops later. It is
// keyed by the help key; hintOf tells apart the verbs that share one.
var hintRank = map[string]int{
	"enter": 0, "esc": 1, "d": 2, "space": 3, "b": 4, "x": 5, "t": 6, "p": 7, "/": 8,
	"z": 9, "s": 10, "f": 11, "c": 12, "e": 13, "a/A": 14, "a": 14, "A": 14, "B": 14, "U": 14, "r": 15, "u": 16, "+/-": 19,
}

const tabExtraRank = 12 // v, e errors, w, b group by, +/- stage, and any verb with no rank

// offBar are the moving keys the bar never lists; ? keeps them (FR7).
var offBar = []string{"j/k", "J/K", "pgup/pgdn"}

// hintItem is a hint binding with the placement the shared table gives it.
type hintItem struct {
	key.Binding
	sec, rank int
	pinned    bool
	short     bool // key-only: rendered as just its key (FR10)
}

// hintOf resolves where b sits on the bar from its verb: the help key and, where one
// key means two verbs (e edit/errors, u undo/5h/wk, b bg/group by, +/- prio/stage, enter save/<verb>,
// esc back/clear/discard), its label. Tabs decide membership; this decides placement.
func hintOf(b key.Binding) hintItem {
	k, desc := b.Help().Key, b.Help().Desc
	it := hintItem{Binding: b, sec: secRowHint, rank: tabExtraRank}
	if r, ok := hintRank[k]; ok {
		it.rank = r
	}
	switch k {
	case "g", "n":
		it.sec, it.pinned = secNavHint, true
	case "?":
		it.sec, it.pinned = secRefHint, true
	case "d", "space":
		it.sec = secNavHint
		if k == "d" && desc != "detail" && desc != "close" {
			it.sec, it.rank = secTabHint, tabExtraRank
		}
	case "esc":
		if desc == "back" {
			it.sec, it.pinned = secNavHint, true
		} else {
			it.sec = secTabHint
		}
	case "enter":
		if desc == "save" {
			it.sec = secTabHint
		}
	case "/", "s", "f", "v", "w":
		it.sec = secTabHint
	case "e":
		if desc == "errors" || desc == "all" {
			it.sec, it.rank = secTabHint, tabExtraRank
		}
	case "u":
		if desc == "5h/wk" {
			it.sec, it.rank = secTabHint, tabExtraRank
		}
	case "b":
		if desc == "group by" {
			it.sec, it.rank = secTabHint, tabExtraRank
		}
	case "+/-":
		if desc == "stage" {
			it.sec, it.rank = secTabHint, tabExtraRank
		}
	}
	if _, ranked := hintRank[k]; !ranked && it.sec == secRowHint {
		it.sec = secTabHint // an unranked verb is a tab extra
	}
	return it
}

// mergeAdopt folds Graph's separate a adopt and A release into one a/A item labelled
// with whichever halves apply (FR9); Ideate already offers the merged binding.
func mergeAdopt(bs []key.Binding) []key.Binding {
	a := slices.IndexFunc(bs, func(b key.Binding) bool { return b.Help().Key == "a" })
	r := slices.IndexFunc(bs, func(b key.Binding) bool { return b.Help().Key == "A" })
	if a < 0 || r < 0 {
		return bs
	}
	bs = slices.Clone(bs)
	bs[a] = key.NewBinding(key.WithKeys("a", "A"), key.WithHelp("a/A", "adopt/release"))
	return slices.Delete(bs, r, r+1)
}

// sectionedBar is the hints bar: items in row | tab | nav | ? keys order, split by •
// within a section and | between, exactly w wide. When narrow it drops the highest-rank
// unpinned item (the later on a tie) until it fits, after first shortening labels to key-only; pinned items never drop and an
// overflowing remainder is clipped, never cut with … (FR12–14).
func sectionedBar(bs []key.Binding, w int, refLive bool) string {
	var items []hintItem
	for _, b := range mergeAdopt(helpable(bs)) {
		if !slices.Contains(offBar, b.Help().Key) {
			items = append(items, hintOf(b))
		}
	}
	if refLive {
		items = append(items, hintOf(keyRef))
	}
	sortHints(items)
	line := func() string { return " " + sectionedView(items) }
	fits := func() bool { return lipgloss.Width(line()) <= w }
	// shorten one item at a time, highest rank first; pins and ? keys last; enter keeps its verb
	shortKey := func(it hintItem) int {
		if it.pinned {
			return -2
		}
		return it.rank
	}
	for !fits() {
		best := -1
		for i, it := range items {
			if it.short || it.Help().Key == "enter" {
				continue
			}
			if best < 0 || shortKey(it) > shortKey(items[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		items[best].short = true
	}
	// then drop by rank; enter goes key-only when it is the last unpinned verb
	for !fits() {
		worst, unpinned := -1, 0
		for i, it := range items {
			if it.pinned {
				continue
			}
			unpinned++
			if worst < 0 || it.rank >= items[worst].rank {
				worst = i
			}
		}
		if worst < 0 {
			break
		}
		if unpinned == 1 && !items[worst].short {
			items[worst].short = true
			continue
		}
		items = slices.Delete(items, worst, worst+1)
	}
	return hintBar.Width(w).Render(ansi.Truncate(line(), w, ""))
}

// sortHints orders items by section; nav reads g, space, d, esc back whatever order
// the tab gave. The bar and ? share it so they never disagree.
func sortHints(items []hintItem) {
	navPos := func(it hintItem) int {
		if it.sec != secNavHint {
			return 0
		}
		return slices.Index([]string{"g", "n", "space", "d", "esc"}, it.Help().Key)
	}
	slices.SortStableFunc(items, func(a, b hintItem) int {
		return cmp.Or(cmp.Compare(a.sec, b.sec), cmp.Compare(navPos(a), navPos(b)))
	})
}

// inSection is the items of section s, as bindings (key-only where shortened).
func inSection(items []hintItem, s int) []key.Binding {
	var bs []key.Binding
	for _, it := range items {
		if it.sec == s {
			b := it.Binding
			if it.short {
				b.SetHelp(b.Help().Key, "")
			}
			bs = append(bs, b)
		}
	}
	return bs
}

// sectionedView joins the items by section, skipping empty ones so no | stands alone.
func sectionedView(items []hintItem) string {
	st := hintHelp.Styles
	var secs []string
	for s := secRowHint; s <= secRefHint; s++ {
		if bs := inSection(items, s); len(bs) > 0 {
			secs = append(secs, shortHelpView(bs))
		}
	}
	return strings.Join(secs, st.ShortSeparator.Inline(true).Render(" | "))
}
