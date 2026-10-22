package bd

import (
	"slices"
	"strings"
	"testing"
)

// subtreeFixture is a parent-child store: id → {parent, issue_type}. Initiative I nests
// Charts C and D (members C.1, C.2, D.1); E is a top-level Chart (E.1, E.2); T is a plain
// task with child T.1; S is a Signal; Z sits under C.1, a third edge below I.
var subtreeFixture = map[string][2]string{
	"I": {"", "epic"}, "C": {"I", "epic"}, "C.1": {"C", "task"}, "C.2": {"C", "task"},
	"D": {"I", "epic"}, "D.1": {"D", "task"}, "Z": {"C.1", "task"},
	"E": {"", "epic"}, "E.1": {"E", "task"}, "E.2": {"E", "task"},
	"T": {"", "task"}, "T.1": {"T", "task"}, "S": {"", "task"},
}

// subtreeRows is what E returns on the fixture, computed by hand-listed rule: R =
// coalesce(grandparent, parent, X); rows R (+type), its children, its grandchildren.
func subtreeRows(x string) [][]string {
	if _, ok := subtreeFixture[x]; !ok {
		return nil
	}
	parent := func(i string) string { return subtreeFixture[i][0] }
	r := x
	if p := parent(x); p != "" {
		r = p
		if g := parent(p); g != "" {
			r = g
		}
	}
	rows := [][]string{{r, subtreeFixture[r][1]}}
	for _, id := range slices.Sorted(func(y func(string) bool) {
		for k := range subtreeFixture {
			if !y(k) {
				return
			}
		}
	}) {
		if p := parent(id); p == r || (p != "" && parent(p) == r) {
			rows = append(rows, []string{id, ""})
		}
	}
	return rows
}

func runSubtree(t *testing.T, x string, snapshot map[string]Bead) (Subtree, bool, []string, error) {
	t.Helper()
	orun := run
	t.Cleanup(func() { run = orun })
	var fetched []string
	run = func(args ...string) (string, error) {
		if args[0] == "sql" {
			if q := args[len(args)-1]; !strings.Contains(q, "'"+x+"'") {
				t.Errorf("E does not name %s: %s", x, q)
			}
			var sb strings.Builder
			sb.WriteString("id,issue_type\n")
			for _, r := range subtreeRows(x) {
				sb.WriteString(r[0] + "," + r[1] + "\n")
			}
			return sb.String(), nil
		}
		fetched = strings.Split(args[5], ",")
		var js []string
		for _, id := range fetched {
			js = append(js, `{"id":"`+id+`","status":"closed","parent":"`+subtreeFixture[id][0]+`"}`)
		}
		return "[" + strings.Join(js, ",") + "]", nil
	}
	st, ok, err := ListSubtree(x, snapshot)
	return st, ok, fetched, err
}

func TestListSubtreeHeads(t *testing.T) {
	for _, tc := range []struct {
		name, x, head string
		ids           []string
	}{
		{"depth-2 Initiative member", "C.1", "I", []string{"C", "C.1", "C.2", "D", "D.1", "I"}},
		{"top-level Chart member", "E.1", "E", []string{"E", "E.1", "E.2"}},
		{"the head itself", "E", "E", []string{"E", "E.1", "E.2"}},
		{"a Signal", "S", "S", []string{"S"}},
		{"child of a non-container task", "T.1", "T.1", []string{"T.1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, ok, _, err := runSubtree(t, tc.x, nil)
			if err != nil || !ok || st.Head != tc.head {
				t.Fatalf("head = %q ok=%v err=%v, want %q", st.Head, ok, err, tc.head)
			}
			if got := slices.Sorted(slices.Values(st.IDs)); !slices.Equal(got, tc.ids) {
				t.Errorf("ids = %v, want %v", got, tc.ids)
			}
			if len(st.Strays) != 0 {
				t.Errorf("strays = %v", st.Strays)
			}
		})
	}
}

// The Initiative's subtree holds nested heads' members (D.1) but not a third level.
func TestListSubtreeStopsAtTwoEdges(t *testing.T) {
	st, _, _, _ := runSubtree(t, "C.1", nil)
	for _, want := range []string{"I", "C", "C.1", "D", "D.1"} {
		if !slices.Contains(st.IDs, want) {
			t.Errorf("subtree lacks %s: %v", want, st.IDs)
		}
	}
	// Z sits three edges under I: the read cuts it off, so this pins the assumption (FR39).
	if slices.Contains(st.IDs, "Z") {
		t.Error("a third nesting level is in the subtree: the 2-edge reading changed")
	}
	// Asked from Z itself, the head found is C (its grandparent), which has a parent of its own:
	// the guard flags it instead of silently scoping to a mid-level head.
	z, ok, _, err := runSubtree(t, "Z", map[string]Bead{"C": {ID: "C", Parent: "I"}})
	if err != nil || !ok || z.Head != "C" || !slices.Equal(z.Strays, []string{"C"}) {
		t.Errorf("Z: head=%q strays=%v ok=%v err=%v", z.Head, z.Strays, ok, err)
	}
}

func TestListSubtreeMissingBead(t *testing.T) {
	st, ok, fetched, err := runSubtree(t, "gone", nil)
	if err != nil || ok || st.Head != "" || fetched != nil {
		t.Errorf("st=%+v ok=%v fetched=%v err=%v", st, ok, fetched, err)
	}
}

func TestListSubtreeSkipsSnapshotIDs(t *testing.T) {
	snap := map[string]Bead{"E": {ID: "E"}, "E.1": {ID: "E.1", Parent: "E"}, "E.2": {ID: "E.2", Parent: "E"}}
	st, ok, fetched, err := runSubtree(t, "E.1", snap)
	if err != nil || !ok || len(fetched) != 0 || len(st.Beads) != 0 {
		t.Errorf("fetched %v beads %v ok=%v err=%v", fetched, st.Beads, ok, err)
	}
	if !slices.Contains(st.IDs, "E") || !slices.Contains(st.IDs, "E.1") {
		t.Errorf("ids = %v: E's full set is handed back even when F is skipped", st.IDs)
	}
	snap = map[string]Bead{"I": {ID: "I"}}
	_, _, fetched, _ = runSubtree(t, "C.1", snap)
	if slices.Contains(fetched, "I") || !slices.Contains(fetched, "C.1") {
		t.Errorf("fetched = %v", fetched)
	}
}

func TestSubtreeStraysFlagsChainNotEndingAtHead(t *testing.T) {
	fetched := []Bead{{ID: "a", Parent: "H"}, {ID: "b", Parent: "a"}, {ID: "x", Parent: "elsewhere"}}
	got := strays("H", []string{"H", "a", "b", "x"}, fetched, nil)
	if !slices.Equal(got, []string{"x"}) {
		t.Errorf("strays = %v", got)
	}
}

func TestSubtreeQueryQuotesID(t *testing.T) {
	q := subtreeQuery("a'b")
	if !strings.Contains(q, "'a''b'") || strings.Contains(q, "%[") || strings.Contains(q, "with ") {
		t.Errorf("query = %s", q)
	}
}
