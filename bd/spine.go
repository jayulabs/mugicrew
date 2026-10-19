package bd

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// spineEdgesSQL is one flat join of every spine-candidate edge with both ends'
// status and kind: label (no recursive CTE: Dolt fails them). The kind joins are
// LEFT, so a bead with two kind: labels comes back twice; spineOf keys it once.
// %s is the target id, whose own row ('self') carries its status when it has no edge.
const spineEdgesSQL = "SELECT d.issue_id, d.depends_on_issue_id, d.type, a.status, b.status, COALESCE(la.label,''), COALESCE(lb.label,'') " +
	"FROM dependencies d JOIN issues a ON a.id=d.issue_id JOIN issues b ON b.id=d.depends_on_issue_id " +
	"LEFT JOIN labels la ON la.issue_id=a.id AND la.label LIKE 'kind:%%' " +
	"LEFT JOIN labels lb ON lb.issue_id=b.id AND lb.label LIKE 'kind:%%' " +
	"WHERE d.type IN ('caused-by','parent-child','related','discovered-from') " +
	"UNION SELECT i.id, i.id, 'self', i.status, i.status, COALESCE(l.label,''), COALESCE(l.label,'') " +
	"FROM issues i LEFT JOIN labels l ON l.issue_id=i.id AND l.label LIKE 'kind:%%' WHERE i.id='%s' ORDER BY 1, 2, 3, 6, 7"

// spineRow is one spineEdgesSQL row: from depends on to, via typ.
type spineRow struct{ from, to, typ, fromStatus, toStatus, fromLabel, toLabel string }

// Spine is target's spine (ADR 0281): every bead id on it, sorted, and whether
// any is unclosed (a Draft or blocked bead counts, as Unclosed does).
func Spine(target string) (ids []string, open bool, err error) {
	rows, err := sqlRows(fmt.Sprintf(spineEdgesSQL, strings.ReplaceAll(target, "'", "''")))
	if err != nil {
		return nil, false, fmt.Errorf("bd: read spine edges: %w", err)
	}
	var edges []spineRow
	for _, r := range rows {
		if len(r) != 7 {
			return nil, false, fmt.Errorf("bd: spine edge row has %d columns, want 7: %v", len(r), r)
		}
		edges = append(edges, spineRow{r[0], r[1], r[2], r[3], r[4], r[5], r[6]})
	}
	ids, open = spineOf(target, edges)
	return ids, open, nil
}

// spineOf is Spine's pure walk over parsed rows, table-testable without bd.
func spineOf(target string, rows []spineRow) (ids []string, open bool) {
	status, kind := map[string]string{}, map[string]contract.Kind{}
	note := func(id, st, label string) {
		status[id] = st
		if _, ok := kind[id]; !ok { // first kind: label in spineEdgesSQL's ORDER BY wins
			kind[id] = kindFromLabels([]string{label})
		}
	}
	for _, r := range rows {
		note(r.from, r.fromStatus, r.fromLabel)
		note(r.to, r.toStatus, r.toLabel)
	}
	// follows: the closure rule, either direction.
	follows := func(r spineRow) bool {
		switch r.typ {
		case "caused-by", "parent-child":
			return kind[r.from] != contract.KindInitiative && kind[r.to] != contract.KindInitiative
		case "related":
			return kind[r.from] == contract.KindRollback || kind[r.from] == contract.KindPivot
		case "discovered-from":
			return kind[r.from] == contract.KindPivot
		}
		return false
	}
	// ponytail: walk rescans every row per visited node, O(spine × rows); an adjacency map if it outgrows ~0.03 s.
	spine := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if spine[id] {
			return
		}
		spine[id] = true
		for _, r := range rows {
			if !follows(r) {
				continue
			}
			if r.from == id {
				walk(r.to)
			} else if r.to == id {
				walk(r.from)
			}
		}
	}
	if kind[target] == contract.KindInitiative {
		spine[target] = true
		for _, r := range rows {
			switch {
			case r.typ == "parent-child" && r.to == target:
				walk(r.from)
			case r.typ == "caused-by" && r.from == target && kind[r.to] == contract.KindSignal:
				spine[r.to] = true
			}
		}
	} else {
		walk(target)
	}
	ids = make([]string, 0, len(spine))
	for id := range spine {
		ids = append(ids, id)
		open = open || status[id] != "closed"
	}
	slices.Sort(ids)
	return ids, open
}
