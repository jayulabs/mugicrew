package bd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// lineageSQL is one flat read of the lineage: every edge Subtree and Spine walk
// (tagged 'e': issue_id, depends_on_issue_id, type), then each bead's kind label and
// created time (tagged 'b': id, label, created_at). No recursive CTE (Dolt fails
// on them); it stays flat as the graph grows (ADR 0286).
const lineageSQL = "SELECT 'e', issue_id, depends_on_issue_id, type FROM dependencies " +
	"WHERE type IN ('parent-child','caused-by','supersedes','related','blocks') AND depends_on_issue_id IS NOT NULL " +
	"UNION ALL SELECT 'b', i.id, COALESCE(l.label,''), i.created_at FROM issues i " +
	"LEFT JOIN labels l ON l.issue_id = i.id AND l.label LIKE 'kind:%'"

// Lineage is the in-memory graph of the bead edges stats rolls up over.
type Lineage struct {
	kids    map[string][]string // parent id -> child ids
	parent  map[string]string   // child id -> parent id
	kind    map[string]string   // bead id -> kind:* label
	created map[string]string   // bead id -> created_at, as bd prints it
	edges   []lineageEdge       // caused-by, supersedes, related, blocks: Spine's walk
}

type lineageEdge struct{ from, to, typ string }

// LoadLineage reads the lineage with one bd sql.
func LoadLineage() (*Lineage, error) {
	rows, err := sqlRows(lineageSQL)
	if err != nil {
		return nil, fmt.Errorf("bd: read lineage: %w", err)
	}
	return NewLineage(rows), nil
}

// NewLineage builds the graph from lineageSQL's rows, skipping malformed ones. Pure.
func NewLineage(rows [][]string) *Lineage {
	l := &Lineage{kids: map[string][]string{}, parent: map[string]string{}, kind: map[string]string{}, created: map[string]string{}}
	for _, r := range rows {
		if len(r) < 4 {
			continue
		}
		switch r[0] {
		case "b":
			if r[2] != "" {
				l.kind[r[1]] = r[2]
			}
			l.created[r[1]] = r[3]
		case "e":
			if r[3] == "parent-child" { // child -> parent
				l.kids[r[2]] = append(l.kids[r[2]], r[1])
				l.parent[r[1]] = r[2]
			} else {
				l.edges = append(l.edges, lineageEdge{r[1], r[2], r[3]})
			}
		}
	}
	return l
}

// Subtree is id plus every descendant by parent edges, closed beads included. An id
// unknown to the graph gives {id}.
func (l *Lineage) Subtree(id string) []string {
	seen := map[string]bool{id: true} // also stops a malformed cycle
	out := []string{id}
	for i := 0; i < len(out); i++ {
		for _, k := range l.kids[out[i]] {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// ErrNotOnSpine is Spine's error for a bead with no stage ancestor or unknown to bd.
var ErrNotOnSpine = errors.New("not on a spine")

// Stage is one spine row: a stage bead (kind without the "kind:" prefix) and its
// subtree ids. Stage subtrees are disjoint.
type Stage struct {
	ID, Kind string
	Subtree  []string
}

// SpineScope maps every bead in the stages' subtrees to its stage id, the
// stats.Filter scope of a spine rollup.
func SpineScope(stages []Stage) map[string]string {
	scope := map[string]string{}
	for _, st := range stages {
		for _, id := range st.Subtree {
			scope[id] = st.ID
		}
	}
	return scope
}

// stageRank orders the stage kinds; a pivot and a Rollback sort among Checkpoints
// by creation time, which puts a pivot after its Checkpoint and a Rollback between
// the two Checkpoints it joins (ADR 0284).
var stageRank = map[string]int{"signal": 0, "chart": 1, "spec": 2, "checkpoint": 3, "pivot": 3, "rollback": 3}

func (l *Lineage) stageKind(id string) string {
	k := strings.TrimPrefix(l.kind[id], "kind:")
	if _, ok := stageRank[k]; !ok {
		return ""
	}
	return k
}

// Spine is the ordered stages of the spine id sits on (ADR 0283, ADR 0284). Pure.
func (l *Lineage) Spine(id string) ([]Stage, error) {
	start := id
	for seen := map[string]bool{}; l.stageKind(start) == "" && !seen[start]; start = l.parent[start] {
		seen[start] = true
		if l.parent[start] == "" {
			return nil, fmt.Errorf("%s is %w", id, ErrNotOnSpine)
		}
	}
	if l.stageKind(start) == "" { // a parent cycle with no stage in it
		return nil, fmt.Errorf("%s is %w", id, ErrNotOnSpine)
	}
	// out lists the typ edge targets from `from`; in lists the beads with a typ edge to `to`.
	out := func(from, typ string) (r []string) {
		for _, e := range l.edges {
			if e.from == from && e.typ == typ {
				r = append(r, e.to)
			}
		}
		return
	}
	in := func(to, typ string) (r []string) {
		for _, e := range l.edges {
			if e.to == to && e.typ == typ {
				r = append(r, e.from)
			}
		}
		return
	}
	first := func(ids []string) string {
		if len(ids) > 0 {
			return ids[0]
		}
		return ""
	}
	sameKind := func(a, b string) bool { k := l.stageKind(a); return k != "" && k != "signal" && k == l.stageKind(b) }

	// hostOf resolves the Checkpoint a pivot or Critique serves: a pivot's blocker, a
	// Critique's parent.
	hostOf := func(id string) string {
		switch l.stageKind(id) {
		case "pivot":
			for _, cp := range in(id, "blocks") {
				if l.stageKind(cp) == "checkpoint" {
					return cp
				}
			}
		case "":
			if p := l.parent[id]; strings.TrimPrefix(l.kind[id], "kind:") == "critique" && l.stageKind(p) == "checkpoint" {
				return p
			}
		}
		return ""
	}
	// A rerouted Signal has a pivot or Critique caused-by it; its spine is the host's.
	if l.stageKind(start) == "signal" {
		for _, s := range in(start, "caused-by") {
			if h := hostOf(s); h != "" {
				start = h
				break
			}
		}
	}
	// A pivot or Rollback is reached by no succession edge: start from what it serves.
	switch l.stageKind(start) {
	case "pivot":
		if cps := in(start, "blocks"); len(cps) > 0 {
			start = cps[0]
		}
	case "rollback":
		if t := first(out(start, "related")); t != "" {
			start = t
		}
	}
	// Back to the root.
	root := start
	for seen := map[string]bool{}; !seen[root]; {
		seen[root] = true
		next := first(out(root, "caused-by"))
		if l.stageKind(next) == "rollback" {
			next = first(out(next, "related")) // as SpecOf does
		}
		if next == "" {
			for _, o := range in(root, "supersedes") {
				if sameKind(o, root) {
					next = o
				}
			}
		}
		if next == "" || l.stageKind(next) == "" {
			break
		}
		root = next
	}
	// Forward from the root.
	order := []string{root}
	seen := map[string]bool{root: true}
	rerouted := map[string]string{} // rerouted Signal -> its host Checkpoint
	add := func(id string) {
		if !seen[id] && l.stageKind(id) != "" {
			seen[id] = true
			order = append(order, id)
		}
	}
	addRerouted := func(host, via string) {
		for _, sg := range out(via, "caused-by") {
			if l.stageKind(sg) == "signal" && !seen[sg] {
				rerouted[sg] = host
				add(sg)
			}
		}
	}
	for i := 0; i < len(order); i++ {
		n := order[i]
		for _, s := range in(n, "caused-by") {
			add(s) // add skips non-stage kinds: a Landing or Approval, an Initiative
		}
		for _, s := range out(n, "supersedes") {
			if sameKind(n, s) {
				add(s)
			}
		}
		if l.stageKind(n) == "checkpoint" {
			for _, p := range out(n, "blocks") {
				if l.stageKind(p) == "pivot" {
					add(p)
					addRerouted(n, p)
				}
			}
			for _, c := range l.kids[n] {
				if hostOf(c) == n {
					addRerouted(n, c)
				}
			}
			for _, r := range in(n, "related") {
				if l.stageKind(r) == "rollback" {
					add(r)
				}
			}
		}
	}
	// Disjoint subtrees: the smallest claiming stage owns an id.
	subs := map[string][]string{}
	for _, s := range order {
		subs[s] = l.Subtree(s)
	}
	bySize := append([]string(nil), order...)
	sort.SliceStable(bySize, func(i, j int) bool { return len(subs[bySize[i]]) < len(subs[bySize[j]]) })
	owned := map[string]bool{}
	stages := map[string]*Stage{}
	for _, s := range bySize {
		st := &Stage{ID: s, Kind: l.stageKind(s)}
		for _, id := range subs[s] {
			if !owned[id] {
				owned[id] = true
				st.Subtree = append(st.Subtree, id)
			}
		}
		stages[s] = st
	}
	// ponytail: created_at compared as bd's text, fine within one timezone.
	byCreated := func(a, b string) bool { return l.created[a] < l.created[b] }
	var base, signals []string
	for _, s := range order {
		if rerouted[s] != "" {
			signals = append(signals, s)
		} else {
			base = append(base, s)
		}
	}
	sort.SliceStable(base, func(i, j int) bool {
		a, b := base[i], base[j]
		if ra, rb := stageRank[stages[a].Kind], stageRank[stages[b].Kind]; ra != rb {
			return ra < rb
		}
		return byCreated(a, b)
	})
	// A rerouted Signal sits right after its host, ahead of the host's pivots (ADR 0284).
	sort.SliceStable(signals, func(i, j int) bool { return byCreated(signals[i], signals[j]) })
	order = order[:0]
	for _, s := range base {
		order = append(order, s)
		for _, sg := range signals {
			if rerouted[sg] == s {
				order = append(order, sg)
			}
		}
	}
	res := make([]Stage, len(order))
	for i, s := range order {
		res[i] = *stages[s]
	}
	return res, nil
}
