package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// keyRefScroll is every key that moves the ? box (Spec mc-bruk D7); only the j/k help shows.
var (
	refDown   = key.NewBinding(key.WithKeys("j", "down"))
	refUp     = key.NewBinding(key.WithKeys("k", "up"))
	refPgDown = key.NewBinding(key.WithKeys("pgdown", " ", "ctrl+d"))
	refPgUp   = key.NewBinding(key.WithKeys("pgup", "ctrl+u"))
	refTop    = key.NewBinding(key.WithKeys("g"))
	refBottom = key.NewBinding(key.WithKeys("G"))

	keyRefScroll = key.NewBinding(key.WithKeys(slices.Concat(refDown.Keys(), refUp.Keys(), refPgDown.Keys(), refPgUp.Keys(), refTop.Keys(), refBottom.Keys())...),
		key.WithHelp("j/k", "scroll"))
)

const (
	refTwoColsAt = 100 // from this width ? has two columns (FR22)
	refMaxW      = 72  // widest the one-column box gets
	refChromeH   = 7   // border ×2, title, blank, blank, footer, and the bar row beneath
)

// refSection is one headed group of key lines.
type refSection struct {
	head string
	keys []key.Binding
}

// refSections groups the bar's binding set (before any fit) into this row, this tab,
// navigation, g goto › and global (Spec mc-bruk D6, ADR 0229 §5).
func (m Model) refSections() (left, right []refSection) {
	var items []hintItem
	var moves []key.Binding
	add := func(b key.Binding, extra bool) {
		if m.tab != tabStats && b.Help().Key == "b" && b.Help().Desc != "group by" { // b is enter's background twin wherever beads are listed (ADR 0165)
			b = verbKey("b", "bg launch/resume (HITL)")
		}
		if b.Help().Key == "t" {
			b = verbKey("t", "transcript (HITL/AFK)")
		}
		it := hintOf(b)
		switch {
		case slices.Contains(offBar, b.Help().Key):
			moves = append(moves, b)
			return
		case extra: // handled but not offered for this row
			it.sec = secTabHint
		}
		items = append(items, it)
	}
	for _, b := range mergeAdopt(helpable(slices.Concat(m.tabBindings(), m.gotoBindings(), m.newBindings()))) {
		add(b, false)
	}
	for _, b := range helpable(m.tabExtra()) {
		add(b, true)
	}
	sortHints(items)
	row, tab, nav := inSection(items, secRowHint), inSection(items, secTabHint), inSection(items, secNavHint)
	left = []refSection{{"this row", row}, {"this tab", tab}, {"navigation", append(nav, moves...)}}
	if opts := m.gotoOpts(); len(opts) > 0 {
		right = append(right, refSection{"g goto ›", optBindings(opts)})
	}
	right = append(right, refSection{"n new ›", optBindings(m.newOpts())}, refSection{"global", shellKeys})
	return left, right
}

// refColumn renders sections as brand (bold accent) headings over key  desc lines, the keys padded to
// one width across the column.
func refColumn(secs []refSection) []string {
	kw := 0
	for _, s := range secs {
		for _, b := range s.keys {
			kw = max(kw, lipgloss.Width(b.Help().Key))
		}
	}
	st := hintHelp.Styles
	var ls []string
	for _, s := range secs {
		if len(s.keys) == 0 && s.head != "this row" { // only this row keeps its heading when empty (FR21)
			continue
		}
		if len(ls) > 0 {
			ls = append(ls, "")
		}
		ls = append(ls, brand.Render(s.head))
		if len(s.keys) == 0 {
			ls = append(ls, dimSt.Render("nothing for this row"))
		}
		for _, b := range s.keys {
			h := b.Help()
			ls = append(ls, st.FullKey.Inline(true).Render(h.Key)+strings.Repeat(" ", kw-lipgloss.Width(h.Key))+" "+st.FullDesc.Inline(true).Render(h.Desc))
		}
	}
	return ls
}

// refLayout is the reference's scrolling body: the lines (both columns side by side),
// the inner width and the visible height.
func (m Model) refLayout() (body []string, w, h int) {
	left, right := m.refSections()
	l, r := refColumn(left), refColumn(right)
	if m.w >= refTwoColsAt {
		n := max(len(l), len(r))
		lw := 0
		for _, s := range l {
			lw = max(lw, lipgloss.Width(s))
		}
		for i := 0; i < n; i++ {
			a, b := "", ""
			if i < len(l) {
				a = l[i]
			}
			if i < len(r) {
				b = r[i]
			}
			body = append(body, a+strings.Repeat(" ", lw-lipgloss.Width(a))+"   "+b)
		}
	} else {
		body = refColumn(slices.Concat(left, right)) // one key column across both lists
	}
	for _, s := range body {
		w = max(w, lipgloss.Width(s))
	}
	if m.w < refTwoColsAt {
		w = min(w, min(m.w, refMaxW)-4)
	}
	w = min(w, m.w-4)
	return body, w, max(min(len(body), m.h-refChromeH), 1)
}

// refKey is a key while ? is open: it scrolls, closes on ?/esc/q, and swallows the rest.
func (m Model) refKey(k tea.KeyMsg) Model {
	body, _, h := m.refLayout()
	off := m.refOff
	switch {
	case key.Matches(k, keyClose):
		m.ref = false
		return m
	case key.Matches(k, refDown):
		off++
	case key.Matches(k, refUp):
		off--
	case key.Matches(k, refPgDown):
		off += h
	case key.Matches(k, refPgUp):
		off -= h
	case key.Matches(k, refTop):
		off = 0
	case key.Matches(k, refBottom):
		off = len(body)
	}
	m.refOff = max(min(off, len(body)-h), 0)
	return m
}

// refView is the ? key reference: a centred, scrolling box over the frame, titled for
// the tab (FR19), whose title and footer never scroll off (FR24).
func (m Model) refView() string {
	body, w, h := m.refLayout()
	off := max(min(m.refOff, len(body)-h), 0)
	title := tabNames[m.tab]
	if m.focus.id != "" {
		title += " › focus"
	}
	foot := shortHelpView([]key.Binding{keyRefScroll, keyClose})
	if len(body) > h {
		c := fmt.Sprintf("%d–%d/%d", off+1, off+h, len(body))
		if off > 0 {
			c += " ▲"
		}
		if off+h < len(body) {
			c += " ▼"
		}
		foot += " " + dimSt.Render(c)
	}
	w = max(w, min(lipgloss.Width(foot), m.w-4))
	ls := append([]string{"keys — " + title, ""}, body[off:off+h]...)
	ls = append(ls, "", foot)
	for i, l := range ls {
		ls[i] = ansi.Truncate(l, w, "")
	}
	return m.placeBox(refBox.Width(w + 2).Render(strings.Join(ls, "\n")))
}
