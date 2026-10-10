package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// The Search status facet's two synthetic values (Spec FR21–22); any
// other value is an exact bd status.
const (
	statusAny      = "any"
	statusUnclosed = "unclosed" // every status but closed — blocked included, unlike bd.Bead.Open
)

// sortKey is the Search sort facet (FR24), each with a fixed direction.
type sortKey int

const (
	sortUpdated  sortKey = iota // newest updated first (the default)
	sortPriority                // P0 first
	sortCreated                 // newest created first
)

// sortAbbrev is each sort's name on the facet line (D4), sortName its s picker entry.
var (
	sortAbbrev = [...]string{sortUpdated: "upd", sortPriority: "pri", sortCreated: "cre"}
	sortName   = [...]string{sortUpdated: "updated", sortPriority: "priority", sortCreated: "created"}
)

// statusEntries is the f picker, in its fixed order (D6): any first as the unfiltered anchor.
var statusEntries = []string{statusAny, statusUnclosed, "open", "in_progress", "blocked", "deferred", "closed"}

// searchQuery is the Search tab's whole query (D3): in memory on the Model only,
// so a fresh process starts on New's defaults (ADR 0077).
type searchQuery struct {
	text   string        // whitespace-split words, each a case-insensitive substring of id or title
	status string        // statusAny, statusUnclosed, or a bd status
	kind   contract.Kind // "" = any
	sort   sortKey
}

// facets is the header's second line minus the count: `unclosed · any · ↓upd`.
func (q searchQuery) facets() string {
	return fmt.Sprintf("%s · %s · ↓%s", q.status, cmp.Or(string(q.kind), "any"), sortAbbrev[q.sort])
}

// searchResults is the Search list (D3): every kinded bead matching text AND
// status AND kind, in the sort's order with an id tiebreak, plus the kinded
// population before the facets (the M of "N of M"). Event beads never list
// (m.beads already drops them; the guard keeps the function honest on its own).
func searchResults(beads []bd.Bead, q searchQuery) ([]bd.Bead, int) {
	words := strings.Fields(strings.ToLower(q.text))
	var out []bd.Bead
	total := 0
	for _, b := range beads {
		if b.Kind == "" || b.Event {
			continue
		}
		total++
		switch {
		case q.status == statusUnclosed && b.Status == "closed",
			q.status != statusAny && q.status != statusUnclosed && b.Status != q.status,
			q.kind != "" && b.Kind != q.kind:
			continue
		}
		hay := strings.ToLower(b.ID + " " + b.Title)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) }) {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b bd.Bead) int {
		var c int
		switch q.sort {
		case sortPriority:
			c = cmp.Compare(a.Priority, b.Priority)
		case sortCreated:
			c = b.Created.Compare(a.Created)
		default:
			c = b.Updated.Compare(a.Updated)
		}
		return cmp.Or(c, cmp.Compare(a.ID, b.ID))
	})
	return out, total
}

// searchRows is the Search list as shown, with its N-of-M denominator.
func (m Model) searchRows() ([]bd.Bead, int) { return searchResults(m.beads, m.search) }

// sortValue is a result row's sort-key suffix (ADR 0074 §1): why it sits where it does.
func (m Model) sortValue(b *bd.Bead) string {
	switch m.search.sort {
	case sortPriority:
		return fmt.Sprintf("P%d", b.Priority)
	case sortCreated:
		return HumanAge(b.Created)
	}
	return HumanAge(b.Updated)
}

// searchParts is a result line's chrome before and after its title: the Inbox
// row with the sort key ahead of the usual suffix.
func (m Model) searchParts(b *bd.Bead) (pre, suf string) {
	marker := "  "
	if b.ID == m.selSearch {
		marker = selMarker.Render("❯ ")
	}
	return m.rowPrefix(marker, b), " " + dim.Render(m.sortValue(b)) + m.rowSuffix(b)
}

// searchRow renders one result line at column width w; only the title is cut,
// a closed one in the Graph's closed style.
func (m Model) searchRow(b *bd.Bead, w int) string {
	pre, suf := m.searchParts(b)
	title := fitRowTitle(b.Title, w, pre, suf)
	if b.Status == "closed" {
		title = closedSty.Render(title)
	}
	return pre + title + suf
}

// searchView is the Search body (D4): the two header lines pinned over the
// results, the selection's detail beside them.
func (m Model) searchView(height int) string {
	list, total := m.searchRows()
	mw := m.masterColW()
	head := dim.Render("/ search")
	if editing := m.editMode == editSearchText; editing || m.search.text != "" {
		// ponytail: a text wider than the column cuts its tail (and the cursor); scroll if long queries matter.
		if head = trunc("/ "+m.search.text, mw); editing && lipgloss.Width("/ "+m.editBuf.render()) <= mw {
			head = "/ " + m.editBuf.render()
		}
	}
	head += "\n" + trunc(fmt.Sprintf("%s   %d of %d", m.search.facets(), len(list), total), mw)
	if m.editMode == editSearchPick {
		return m.pickerView(head, height)
	}
	var lines []string
	if len(list) == 0 {
		lines = append(lines, dim.Render(noMatches))
	}
	var sel *bd.Bead
	selRow := 0
	for i := range list {
		b := &list[i]
		if b.ID == m.selSearch {
			selRow, sel = i, b
		}
		lines = append(lines, fitRow(m.searchRow(b, mw), mw))
	}
	detail := ""
	if sel != nil {
		detail = m.detailView(sel, selRow, len(list), height)
	}
	return m.layout(head, lines, selRow, detail, height)
}

// setSearchText sets the query text and re-anchors the selection on the new results.
func (m *Model) setSearchText(s string) {
	m.search.text = s
	m.fixSelection()
}

// updateSearchText drives Search's / input (ADR 0073): every keystroke refilters
// live; enter keeps the text, esc reverts it to what it was on /. Every printable
// key is text and the rest are swallowed, so no tab jump or move fires (FR14, FR50).
func (m Model) updateSearchText(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editMode = editNone
		m.setSearchText(m.searchPrev)
	case tea.KeyEnter:
		m.editMode = editNone
	default:
		m.editBuf = m.editBuf.apply(msg)
		m.setSearchText(m.editBuf.text)
	}
	return m, nil
}

// pickEntries are the open picker's values (D6): status in its fixed order; any
// then every kind in the kinded population (plus the active one, should it have
// vanished) alphabetically; the sorts in their enum order.
func (m Model) pickEntries() []string {
	switch m.pickFacet {
	case "f":
		return statusEntries
	case "c":
		kinds := []string{}
		if m.search.kind != "" {
			kinds = append(kinds, string(m.search.kind))
		}
		for _, b := range m.beads {
			if b.Kind != "" && !b.Event {
				kinds = append(kinds, string(b.Kind))
			}
		}
		slices.Sort(kinds)
		return append([]string{"any"}, slices.Compact(kinds)...)
	}
	return sortName[:]
}

// facetValue is the facet's active value as its picker spells it.
func (m Model) facetValue(facet string) string {
	switch facet {
	case "f":
		return m.search.status
	case "c":
		return cmp.Or(string(m.search.kind), "any")
	}
	return sortName[m.search.sort]
}

// openPicker opens the f/c/s picker with its cursor on the active value.
func (m *Model) openPicker(facet string) {
	m.editMode, m.pickFacet, m.status = editSearchPick, facet, ""
	m.pickCur = m.facetValue(facet)
}

// updateSearchPick drives an open facet picker (D5): j/k move, enter sets the
// facet, esc cancels; every other key is swallowed, so nothing else can fire.
func (m Model) updateSearchPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	es := m.pickEntries()
	i := max(slices.Index(es, m.pickCur), 0)
	switch msg.String() {
	case "j", "down":
		m.pickCur = es[min(i+1, len(es)-1)]
	case "k", "up":
		m.pickCur = es[max(i-1, 0)]
	case "esc", "ctrl+c": // ctrl+c cancels like esc (ADR 0058)
		m.editMode = editNone
	case "enter":
		m.editMode = editNone
		switch v := es[i]; m.pickFacet {
		case "f":
			m.search.status = v
		case "c":
			m.search.kind = contract.Kind(v)
			if v == "any" {
				m.search.kind = ""
			}
		default:
			m.search.sort = sortKey(slices.Index(sortName[:], v))
		}
		m.fixSelection()
	}
	return m, nil
}

// searchPlace is where Search's selection and picker cursor sit (-1 if
// nowhere), taken before a reload replaces the beads.
func (m Model) searchPlace() (selIdx, pickIdx int) {
	list, _ := m.searchRows()
	return indexBead(list, m.selSearch), slices.Index(m.pickEntries(), m.pickCur)
}

// keepSearchPlace re-anchors Search after a reload (ADR 0076): a selected bead
// or picker value that dropped out hands over to the entry now at its old
// index, clamped to the last; one still present is kept by value.
func (m *Model) keepSearchPlace(selIdx, pickIdx int) {
	if list, _ := m.searchRows(); selIdx >= 0 && len(list) > 0 && indexBead(list, m.selSearch) < 0 {
		m.selSearch = list[min(selIdx, len(list)-1)].ID
	}
	if es := m.pickEntries(); m.editMode == editSearchPick && pickIdx >= 0 && !slices.Contains(es, m.pickCur) {
		m.pickCur = es[min(pickIdx, len(es)-1)]
	}
}

// pickerView is an open picker in place of the results (like the ? reference):
// the header lines, then one line per entry, the cursor's marked ❯ and the
// active value's ✓.
func (m Model) pickerView(head string, height int) string {
	es := m.pickEntries()
	cur, active := max(slices.Index(es, m.pickCur), 0), m.facetValue(m.pickFacet)
	lines := make([]string, len(es))
	for i, e := range es {
		marker := "  "
		if i == cur {
			marker = selMarker.Render("❯ ")
		}
		if e == active {
			e += dim.Render(" ✓")
		}
		lines[i] = trunc(marker+e, m.w)
	}
	rows := windowRows(strings.Join(lines, "\n"), m.w, height-2, followOffset(len(lines), height-2, cur))
	return head + "\n" + strings.Join(rows, "\n")
}

// searchKeys are the keys Search handles (D5): the global ones plus
// browsing, the text query and the facet pickers. No bead action is bound on Search; updateKey drops everything else.
var searchKeys = []string{"q", "ctrl+c", "left", "shift+tab", "right", "G", "I", "S", "H", "L", "#", "*", "tab", "j", "down", "k", "up", "enter", "/", "esc", "f", "c", "s", "?", "o", "f5", "z"}
