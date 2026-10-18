package tui

// Graph's in-place filter (FR37–44, ADR 0143): words plus status and kind facets
// narrow the tree to the matches and their ancestors, in phase order.

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// queryFile is the committed query's file under the common dir (ADR 0176 §4).
const queryFile = "graph-filter.json"

// The status picker's entries (ADR 0075 §1).
var statusItems = []string{"any", "unclosed", "open", "in_progress", "blocked", "deferred", "closed"}

// statusAny is the stored status that shows closed beads too; "unclosed" is the default.
const (
	statusAny      = "any"
	statusUnclosed = "unclosed"
	statusClosed   = "closed"
)

// query is the filter: whitespace words over title and id, a status and a kind
// facet (kind "" for any).
type query struct {
	words, status string
	kind          contract.Kind
}

// baseline is the query a new Graph starts at and esc returns to (ADR 0175): a
// default, not a filter.
var baseline = query{status: "unclosed"}

// active reports whether q differs from the baseline.
func (q query) active() bool { return q != baseline }

// statusOK reports whether b passes the status facet; a bead with a session passes
// under every status (ADR 0038 §2).
func (q query) statusOK(b bd.Bead, sessions map[string]session.Info) bool {
	switch q.status {
	case statusAny:
		return true
	case "unclosed":
		// Words at the baseline search every bead (Spec "Graph search at the default status matches closed beads" §1).
		return strings.TrimSpace(q.words) != "" || b.Status != "closed" || sessions[b.ID].Has()
	}
	return b.Status == q.status || sessions[b.ID].Has()
}

// match reports whether b passes the words and both facets; with no kind facet a
// kindless bead is kept (ADR 0143 §3).
func (q query) match(b bd.Bead, sessions map[string]session.Info) bool {
	if q.kind != "" && b.Kind != q.kind || !q.statusOK(b, sessions) {
		return false
	}
	return matchWords(q.words, b.Title+" "+b.ID)
}

// filterGroups keeps the beads q matches and their ancestors as context rows,
// dropping groups with neither; order is untouched. n counts matches only.
func filterGroups(groups []group, q query, sessions map[string]session.Info) (out []group, n int) {
	for _, g := range groups {
		keep := func(bs []bd.Bead) (kept []bd.Bead) {
			for _, b := range bs {
				if q.match(b, sessions) {
					kept = append(kept, b)
				}
			}
			return kept
		}
		f := g
		f.ctx, f.members, f.nested = map[string]bool{}, nil, nil
		if g.nested != nil {
			f.nested = map[string][]bd.Bead{}
		}
		for _, m := range g.members {
			kids := keep(g.nested[m.ID])
			hit := q.match(m, sessions)
			if !hit && len(kids) == 0 {
				continue
			}
			f.members = append(f.members, m)
			if hit {
				n++
			} else {
				f.ctx[m.ID] = true
			}
			if _, nests := g.nested[m.ID]; nests {
				f.nested[m.ID] = kids
				n += len(kids)
			}
		}
		headHit := g.head != nil && q.match(*g.head, sessions)
		if !headHit && len(f.members) == 0 {
			continue
		}
		switch {
		case g.head == nil:
			f.ctx[unparentedSel] = true
		case headHit:
			n++
		default:
			f.ctx[g.head.ID] = true
		}
		out = append(out, f)
	}
	return out, n
}

// countBeads is every bead the groups hold: the M of N of M.
func countBeads(groups []group) (n int) {
	for _, g := range groups {
		n += len(g.beads())
		if g.head != nil {
			n++
		}
	}
	return n
}

var keyStatus = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status"))

// pickGraphMsg opens a Graph facet picker: "status" or "kind".
type pickGraphMsg struct{ facet string }

// header is the line above the tree while text or a facet is active (FR41): the
// words, then the facets, then how many beads match of all.
func (g graphTab) header(w int) string {
	if !g.q.active() && !g.typing {
		return ""
	}
	return fitHeader(w-1, g.edit, g.headerWith)
}

// headerWith is header around the rendered filter input.
func (g graphTab) headerWith(input string) string {
	var segs []string
	if g.typing {
		segs = append(segs, "/ "+input)
	} else if g.q.words != "" {
		segs = append(segs, "/ "+g.q.words)
	}
	if g.q.status != baseline.status {
		segs = append(segs, g.q.status)
	}
	if g.q.kind != "" {
		segs = append(segs, string(g.q.kind))
	}
	if g.searching {
		segs = append(segs, dimSt.Render("searching…"))
	}
	return strings.Join(segs, dimSt.Render(" · ")) + "   " + softSt.Render(fmt.Sprintf("%d of %d", g.matched, g.total))
}

// typingKey is one key of the open / input: it filters live, enter keeps, esc reverts.
func (g graphTab) typingKey(k tea.KeyMsg) graphTab {
	switch {
	case key.Matches(k, keyKeep):
		g.q.words, g.typing, g.visit = strings.TrimSpace(g.edit.text), false, false
	case key.Matches(k, keyRevert):
		g.typing = false
	default:
		g.edit = g.edit.apply(k)
	}
	return g.rebuild()
}

// clearOne is esc in normal mode: the text, then the status, then the kind (FR42). On
// an untouched scoped reveal it goes straight to the baseline (Spec "G reveal scopes
// Graph to the revealed bead", FR6).
func (g graphTab) clearOne() graphTab {
	g.visit = false
	switch {
	case g.q == g.scopedTo && g.scopedTo != query{}:
		g.q = baseline
	case g.q.words != "":
		g.q.words = ""
	case g.q.status != baseline.status:
		g.q.status = baseline.status
	default:
		g.q.kind = ""
	}
	return g.rebuild()
}

// pickGraph opens the status or kind picker; the current value is marked, even a
// kind no loaded bead has (ADR 0076 §3).
func (m Model) pickGraph(facet string) Model {
	g := m.tabAt(tabGraph).(graphTab)
	items, cur := statusItems, g.q.status
	if facet == "kind" {
		items, cur = []string{"any"}, string(g.q.kind)
		seen := map[contract.Kind]bool{}
		for _, b := range g.beads {
			if b.Kind != "" {
				seen[b.Kind] = true
			}
		}
		if g.q.kind != "" {
			seen[g.q.kind] = true
		}
		for _, k := range slices.Sorted(maps.Keys(seen)) {
			items = append(items, string(k))
		}
	}
	d := pickerDialog("Graph "+facet, items, "", func(m Model, d dialog) (Model, tea.Cmd) {
		g := m.tabAt(tabGraph).(graphTab)
		old, v := g.q, d.items[d.cur]
		wasAll := m.claimsAllClosed()
		g.visit = false // the human's own filter ends a Recents visit
		switch {
		case facet != "kind":
			g.q.status = v
		case d.cur == 0:
			g.q.kind = ""
		default:
			g.q.kind = contract.Kind(v)
		}
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabGraph] = g.rebuild()
		m = m.saveGraphFilter(old)
		m, sc := m.searchSync()
		return m, tea.Batch(sc, m.claimLoad(wasAll))
	})
	if cur != "" {
		d.cur = max(slices.Index(items, cur), 0)
	}
	return m.push(d)
}

// loadQuery reads the committed query; a missing, unreadable or unparsable file is the
// baseline, and a status or kind outside its vocabulary falls back alone (ADR 0176 §4).
func loadQuery(path string) query {
	b, err := os.ReadFile(path)
	if err != nil {
		return baseline
	}
	var f struct {
		Words, Status string
		Kind          contract.Kind
	}
	if json.Unmarshal(b, &f) != nil {
		return baseline
	}
	q := query{words: f.Words, status: f.Status, kind: f.Kind}
	if !slices.Contains(statusItems, q.status) {
		q.status = baseline.status
	}
	if q.kind != "" && !slices.Contains(contract.Kinds, q.kind) {
		q.kind = ""
	}
	return q
}

// saveQuery writes the committed query through a temp file renamed over its path.
func (g graphTab) saveQuery() error {
	b, err := json.Marshal(map[string]string{"words": g.q.words, "status": g.q.status, "kind": string(g.q.kind)})
	if err != nil {
		return err
	}
	return writeFileAtomic(g.qpath, b)
}
