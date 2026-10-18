package bd

import (
	"errors"
	"slices"
	"testing"
)

// edge/bead build lineageSQL rows without bd.
func edge(from, to, typ string) []string { return []string{"e", from, to, typ} }
func bead(id, kind, created string) []string {
	return []string{"b", id, kind, created}
}

func TestLineageSubtree(t *testing.T) {
	l := NewLineage([][]string{
		bead("c", "kind:chart", "2026-10-01 00:00:00 +0000 UTC"),
		edge("c.1", "c", "parent-child"), // a closed bead is still an edge
		edge("c.1.1", "c.1", "parent-child"),
		edge("c.1.1.1", "c.1.1", "parent-child"),
		edge("c.2", "c", "parent-child"),
		edge("x", "c", "caused-by"), // not a parent edge
		edge("y", "c", "related"),
		edge("z", "c", "blocks"),
	})
	got := l.Subtree("c")
	slices.Sort(got)
	if want := []string{"c", "c.1", "c.1.1", "c.1.1.1", "c.2"}; !slices.Equal(got, want) {
		t.Errorf("Subtree(c) = %v, want %v", got, want)
	}
	if got := l.Subtree("c.1.1"); len(got) != 2 {
		t.Errorf("Subtree(c.1.1) = %v, want itself and its child", got)
	}
	if got := l.Subtree("gone"); !slices.Equal(got, []string{"gone"}) {
		t.Errorf("Subtree(unknown) = %v, want {gone}", got)
	}
}

func TestLineageSubtreeCycleTerminates(t *testing.T) {
	l := NewLineage([][]string{edge("a", "b", "parent-child"), edge("b", "a", "parent-child")})
	if got := l.Subtree("a"); len(got) != 2 {
		t.Errorf("Subtree over a cycle = %v, want a and b once", got)
	}
}

func TestLineageSQLRows(t *testing.T) {
	// A short row (malformed CSV line) is skipped, not a panic.
	if l := NewLineage([][]string{{"e", "a"}, {}, {"b"}}); len(l.Subtree("a")) != 1 {
		t.Error("short rows should be ignored")
	}
}

func spineIDs(t *testing.T, l *Lineage, id string) []string {
	t.Helper()
	st, err := l.Spine(id)
	if err != nil {
		t.Fatalf("Spine(%s): %v", id, err)
	}
	var ids []string
	for _, s := range st {
		ids = append(ids, s.ID+":"+s.Kind)
	}
	return ids
}

func TestLineageSpine(t *testing.T) {
	n := func(i int) string { return "2026-10-0" + string(rune('0'+i)) + " 00:00:00 +0000 UTC" }
	rows := [][]string{
		bead("sig", "kind:signal", n(1)), bead("dup", "kind:signal", n(1)),
		bead("ch", "kind:chart", n(2)), bead("ch.1", "kind:task", n(2)),
		bead("sp1", "kind:spec", n(3)), bead("sp2", "kind:spec", n(5)),
		bead("cp1", "kind:checkpoint", n(4)), bead("cp1.1", "kind:build", n(4)),
		bead("pv", "kind:pivot", n(5)), bead("latch", "kind:task", n(5)),
		bead("cp2", "kind:checkpoint", n(6)),
		bead("rb", "kind:rollback", n(7)), bead("cp3", "kind:checkpoint", n(8)),
		bead("land", "kind:landing", n(8)),
		bead("off", "kind:signal", n(9)), bead("ini", "kind:initiative", n(1)), bead("free", "kind:task", n(1)),
		edge("ch", "sig", "caused-by"), edge("ch.1", "ch", "parent-child"),
		edge("sp1", "ch", "caused-by"), edge("sp2", "ch", "caused-by"), edge("sp1", "sp2", "supersedes"),
		edge("cp1", "sp1", "caused-by"), edge("cp1.1", "cp1", "parent-child"),
		edge("cp1", "pv", "blocks"), edge("cp1", "latch", "blocks"), edge("pv", "sp1", "related"),
		edge("cp2", "sp2", "caused-by"),
		edge("rb", "cp2", "related"), edge("cp3", "rb", "caused-by"),
		edge("land", "cp3", "caused-by"),
		edge("off", "sig", "discovered-from"),
		edge("dup", "sig", "supersedes"),
		edge("ch", "ini", "parent-child"),
		edge("ini", "cp3", "caused-by"), // FR16: an Initiative is never pulled in
	}
	l := NewLineage(rows)
	want := []string{"sig:signal", "ch:chart", "sp1:spec", "sp2:spec", "cp1:checkpoint", "pv:pivot", "cp2:checkpoint", "rb:rollback", "cp3:checkpoint"}
	// Every stage, and a non-stage descendant, sees the same spine.
	for _, from := range []string{"sig", "sp2", "cp3", "rb", "pv", "cp1.1", "ch.1"} {
		if got := spineIDs(t, l, from); !slices.Equal(got, want) {
			t.Errorf("Spine(%s) = %v, want %v", from, got, want)
		}
	}
	if got := spineIDs(t, l, "dup"); !slices.Equal(got, []string{"dup:signal"}) {
		t.Errorf("Spine(dup) = %v, want itself", got)
	}
	st, _ := l.Spine("sig")
	for _, s := range st {
		if s.ID == "cp1" && !slices.Equal(s.Subtree, []string{"cp1", "cp1.1"}) {
			t.Errorf("cp1 subtree = %v", s.Subtree)
		}
		if s.ID == "ch" && !slices.Equal(s.Subtree, []string{"ch", "ch.1"}) {
			t.Errorf("ch subtree = %v", s.Subtree)
		}
	}
	for _, bad := range []string{"ini", "free", "gone"} {
		if _, err := l.Spine(bad); !errors.Is(err, ErrNotOnSpine) {
			t.Errorf("Spine(%s) err = %v, want not on a spine", bad, err)
		}
	}
}

func TestLineageSpineFastLaneAndCycle(t *testing.T) {
	l := NewLineage([][]string{
		bead("s", "kind:signal", "1"), bead("p", "kind:spec", "2"),
		edge("p", "s", "caused-by"),
	})
	if got := spineIDs(t, l, "p"); !slices.Equal(got, []string{"s:signal", "p:spec"}) {
		t.Errorf("fast lane = %v", got)
	}
	c := NewLineage([][]string{
		bead("a", "kind:spec", "1"), bead("b", "kind:spec", "2"),
		edge("a", "b", "caused-by"), edge("b", "a", "caused-by"), edge("a", "b", "supersedes"), edge("b", "a", "supersedes"),
	})
	if got := spineIDs(t, c, "a"); len(got) != 2 {
		t.Errorf("cycle = %v", got)
	}
}
