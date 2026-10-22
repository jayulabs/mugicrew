package bd

import (
	"slices"
	"strings"
)

// Subtree is a bead's group head and everything under it (Graph's goto scope).
type Subtree struct {
	Head   string   // the group head: the container at the root of the parent chain, else the bead itself
	Self   bool     // the read self-scoped: the root is no container, so Head is the asked bead alone
	IDs    []string // every id in the head's subtree (E's set, Head included), closed ones too
	Beads  []Bead   // the subtree's beads fetched in full: IDs less those the snapshot already held
	Strays []string // fetched beads (or the head) whose parent chain does not end at Head: the depth guard's report
}

// subtreeSQL is the root-and-members read, one flat UNION (no CTEs: Dolt fails them), with
// {root} the root expression. R = coalesce(grandparent, parent, X) over parent-child edges;
// the rows are R (with its issue_type, so the caller applies the container check), its
// children and its grandchildren (second column ”). It couples to issues and dependencies
// as defaultIDsSQL does.
//
// Depth: the "at most 2 parent edges" assumption is a property of the data (containers nest
// only under Initiatives), not enforced by bd. A third level would be cut off here, so
// strays reports a head that itself has a parent: X three edges down.
const subtreeSQL = `select id, issue_type from issues where id={root}
union select d.issue_id, '' from dependencies d where d.type='parent-child' and d.depends_on_issue_id={root}
union select g.issue_id, '' from dependencies c join dependencies g on g.depends_on_issue_id=c.issue_id
  and g.type='parent-child' where c.type='parent-child' and c.depends_on_issue_id={root}`

// subtreeRoot is R as a scalar expression, {id} the quoted bead id.
const subtreeRoot = `coalesce((select g.depends_on_issue_id from dependencies p join dependencies g
  on g.issue_id=p.depends_on_issue_id and g.type='parent-child'
  where p.issue_id={id} and p.type='parent-child'),
 (select p.depends_on_issue_id from dependencies p where p.issue_id={id} and p.type='parent-child'),
 {id})`

func subtreeQuery(id string) string {
	q := "'" + strings.ReplaceAll(id, "'", "''") + "'"
	root := strings.ReplaceAll(subtreeRoot, "{id}", q)
	return strings.ReplaceAll(subtreeSQL, "{root}", root)
}

// ListSubtree reads the group of any member id in two bd calls: E (bd sql) names the head
// and the subtree's ids, then F (ListByID) fetches those not already in snapshot, which
// maps the caller's open/default beads by id. A root that is not a container gives a
// self-scope {id}. ok is false when E returns no rows: bd lacks the bead.
func ListSubtree(id string, snapshot map[string]Bead) (st Subtree, ok bool, err error) {
	rows, err := sqlRows(subtreeQuery(id))
	if err != nil {
		return Subtree{}, false, err
	}
	if len(rows) == 0 {
		return Subtree{}, false, nil
	}
	return buildSubtree(id, rows, snapshot, ListByID)
}

func buildSubtree(id string, rows [][]string, snapshot map[string]Bead, fetch func([]string) ([]Bead, error)) (Subtree, bool, error) {
	var head, rootType string
	var ids []string
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		if r[1] != "" { // only the root row carries its issue_type
			head, rootType = r[0], r[1]
		}
		if !slices.Contains(ids, r[0]) {
			ids = append(ids, r[0])
		}
	}
	self := head == "" || rootType != "epic"
	if self {
		head, ids = id, []string{id}
	}
	var need []string
	for _, i := range ids {
		if _, held := snapshot[i]; !held {
			need = append(need, i)
		}
	}
	beads, err := fetch(need)
	if err != nil {
		return Subtree{}, true, err
	}
	return Subtree{Head: head, Self: self, IDs: ids, Beads: beads, Strays: strays(head, ids, beads, snapshot)}, true, nil
}

// strays flags what breaks the two-edge assumption: a head with a parent of its own, and a
// fetched bead whose parent chain ends somewhere other than head. It reports, never refetches.
func strays(head string, ids []string, fetched []Bead, snapshot map[string]Bead) []string {
	known := make(map[string]Bead, len(snapshot)+len(fetched))
	for k, b := range snapshot {
		known[k] = b
	}
	for _, b := range fetched {
		known[b.ID] = b
	}
	var out []string
	if h, ok := known[head]; ok && h.Parent != "" && len(ids) > 1 { // a self-scope bead may have any parent
		out = append(out, head)
	}
	for _, b := range fetched {
		if b.ID == head {
			continue
		}
		p := b.Parent
		for hops := 0; ; hops++ {
			pb, loaded := known[p]
			if p == head || (!loaded && slices.Contains(ids, p)) {
				break // reached the head, or a subtree member not loaded to follow further
			}
			if p == "" || !loaded || hops > len(ids) {
				out = append(out, b.ID)
				break
			}
			p = pb.Parent
		}
	}
	return out
}
