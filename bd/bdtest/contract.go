package bdtest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

// Contract is the one suite that pins a bd.Core's observable behaviour: it runs against the
// fake on every go test (contract_test.go), and against a real scratch bd behind
// MUGICREW_BD_TEST=1. mk provisions the core once for the suite. Cases seed
// through the core's own writes and read back the ids it mints, so none assumes an empty
// graph or a literal id; they run serially on the one graph. The core has no status or
// assignee setter, so set is the per-leg hook that puts a bead in a state its writes can't
// reach: the fake mutates it, the real leg runs bd update.
func Contract(t *testing.T, mk func(t *testing.T) bd.Core, set func(t *testing.T, id, status, assignee string)) {
	c := mk(t)
	create := func(t *testing.T, s bd.CreateSpec) string {
		t.Helper()
		id, err := c.Create(s)
		if err != nil {
			t.Fatalf("Create(%+v) = %v", s, err)
		}
		return id
	}
	ids := func(bs []bd.Bead) []string {
		out := make([]string, len(bs))
		for i, b := range bs {
			out[i] = b.ID
		}
		slices.Sort(out)
		return out
	}

	t.Run("show reads a created bead", func(t *testing.T) {
		id := create(t, bd.CreateSpec{Title: "shown", Description: "body", Labels: []string{"ct:show"}})
		b, err := c.Show(id)
		if err != nil || b.ID != id || b.Title != "shown" || b.Status != "open" || !b.HasLabel("ct:show") {
			t.Fatalf("Show = %+v, %v", b, err)
		}
	})

	t.Run("show of an unknown id is not found", func(t *testing.T) {
		if _, err := c.Show("bdct-zz-nope"); !bd.IsNotFound(err) {
			t.Fatalf("Show(unknown) = %v, want a not-found error", err)
		}
	})

	t.Run("list default status set is every non-closed status", func(t *testing.T) {
		const label = "ct:statuses"
		open := create(t, bd.CreateSpec{Title: "o", Labels: []string{label}})
		held := create(t, bd.CreateSpec{Title: "h", Labels: []string{label}})
		draft := create(t, bd.CreateSpec{Title: "d", Labels: []string{label}, Deferred: true})
		done := create(t, bd.CreateSpec{Title: "c", Labels: []string{label}})
		if err := c.Claim(held, "open"); err != nil {
			t.Fatal(err)
		}
		if err := c.Close([]string{done}, "done", false); err != nil {
			t.Fatal(err)
		}
		got, err := c.List(bd.Query{Label: label})
		if want := ids([]bd.Bead{{ID: open}, {ID: held}, {ID: draft}}); err != nil || !slices.Equal(ids(got), want) {
			t.Fatalf("List default = %v, %v; want %v (closed %s absent)", ids(got), err, want, done)
		}
		all, err := c.List(bd.Query{Label: label, All: true})
		if err != nil || len(all) != 4 {
			t.Fatalf("List All = %v, %v; want the 4 seeded", ids(all), err)
		}
		only, err := c.List(bd.Query{Label: label, Status: []string{"deferred"}})
		if err != nil || !slices.Equal(ids(only), []string{draft}) {
			t.Fatalf("List deferred = %v, %v; want [%s]", ids(only), err, draft)
		}
	})

	t.Run("list limit caps and zero is unlimited", func(t *testing.T) {
		const label = "ct:limit"
		for range 3 {
			create(t, bd.CreateSpec{Title: "l", Labels: []string{label}})
		}
		two, err := c.List(bd.Query{Label: label, Limit: 2})
		if err != nil || len(two) != 2 {
			t.Fatalf("List Limit 2 = %d beads, %v; want 2", len(two), err)
		}
		all, err := c.List(bd.Query{Label: label})
		if err != nil || len(all) != 3 {
			t.Fatalf("List Limit 0 = %d beads, %v; want all 3", len(all), err)
		}
	})

	t.Run("list filters by ids, label pattern and external ref", func(t *testing.T) {
		a := create(t, bd.CreateSpec{Title: "a", Labels: []string{"ct:pat:one"}, ExternalRef: "ct-ref-1"})
		b := create(t, bd.CreateSpec{Title: "b", Labels: []string{"ct:pat:two"}})
		create(t, bd.CreateSpec{Title: "other", Labels: []string{"ct:elsewhere"}})
		if got, err := c.List(bd.Query{All: true, IDs: []string{a, b}}); err != nil || !slices.Equal(ids(got), ids([]bd.Bead{{ID: a}, {ID: b}})) {
			t.Fatalf("List IDs = %v, %v", ids(got), err)
		}
		if got, err := c.List(bd.Query{LabelPattern: "ct:pat:*"}); err != nil || !slices.Equal(ids(got), ids([]bd.Bead{{ID: a}, {ID: b}})) {
			t.Fatalf("List LabelPattern = %v, %v", ids(got), err)
		}
		if got, err := c.List(bd.Query{ExternalRef: "ct-ref-1"}); err != nil || !slices.Equal(ids(got), []string{a}) {
			t.Fatalf("List ExternalRef = %v, %v; want [%s]", ids(got), err, a)
		}
	})

	t.Run("ready hides a blocked child until its blocker closes", func(t *testing.T) {
		blocker := create(t, bd.CreateSpec{Title: "blocker"})
		child := create(t, bd.CreateSpec{Title: "child"})
		if err := c.AddEdge(child, blocker, "blocks"); err != nil {
			t.Fatal(err)
		}
		ready := func() []string {
			t.Helper()
			rs, err := c.Ready()
			if err != nil {
				t.Fatalf("Ready = %v", err)
			}
			return ids(rs)
		}
		if got := ready(); !slices.Contains(got, blocker) || slices.Contains(got, child) {
			t.Fatalf("Ready = %v; want the blocker, not its blocked child", got)
		}
		if err := c.Close([]string{blocker}, "done", false); err != nil {
			t.Fatal(err)
		}
		if got := ready(); !slices.Contains(got, child) {
			t.Fatalf("Ready = %v; want the child once its blocker closed", got)
		}
	})

	t.Run("ready is uncapped past 100 rows", func(t *testing.T) {
		var want []string
		for i := 0; i < 101; i++ {
			want = append(want, create(t, bd.CreateSpec{Title: "bulk"}))
		}
		rs, err := c.Ready()
		if err != nil {
			t.Fatalf("Ready = %v", err)
		}
		got := ids(rs)
		for _, id := range want {
			if !slices.Contains(got, id) {
				t.Fatalf("Ready returned %d rows, missing %s; want it uncapped", len(got), id)
			}
		}
	})

	t.Run("children equal list --parent --all", func(t *testing.T) {
		parent := create(t, bd.CreateSpec{Type: "epic", Title: "parent"})
		k1 := create(t, bd.CreateSpec{Title: "k1", Parent: parent})
		k2 := create(t, bd.CreateSpec{Title: "k2", Parent: parent})
		if err := c.Close([]string{k2}, "done", false); err != nil {
			t.Fatal(err)
		}
		kids, err := c.List(bd.Query{Parent: parent, All: true})
		if err != nil || !slices.Equal(ids(kids), ids([]bd.Bead{{ID: k1}, {ID: k2}})) {
			t.Fatalf("children = %v, %v; want closed ones too", ids(kids), err)
		}
		listed, err := c.List(bd.Query{Parent: parent, All: true, Limit: 1000})
		if err != nil || !slices.Equal(ids(listed), ids(kids)) {
			t.Fatalf("list --parent --all = %v, %v; want %v", ids(listed), err, ids(kids))
		}
	})

	t.Run("edges list both directions with their type", func(t *testing.T) {
		a := create(t, bd.CreateSpec{Title: "a"})
		b := create(t, bd.CreateSpec{Title: "b"})
		d := create(t, bd.CreateSpec{Title: "d"})
		if err := c.AddEdge(b, a, "blocks"); err != nil {
			t.Fatal(err)
		}
		if err := c.AddEdge(d, a, "related"); err != nil {
			t.Fatal(err)
		}
		down, err := c.Edges(b, bd.Down, "")
		if err != nil || len(down) != 1 || down[0].ID != a || down[0].Type != "blocks" {
			t.Fatalf("Edges(b, down) = %+v, %v; want a via blocks", down, err)
		}
		up, err := c.Edges(a, bd.Up, "")
		if err != nil || len(up) != 2 {
			t.Fatalf("Edges(a, up) = %+v, %v; want b and d", up, err)
		}
		blocks, err := c.Edges(a, bd.Up, "blocks")
		if err != nil || len(blocks) != 1 || blocks[0].ID != b {
			t.Fatalf("Edges(a, up, blocks) = %+v, %v; want only b", blocks, err)
		}
	})

	t.Run("comments come back oldest first", func(t *testing.T) {
		id := create(t, bd.CreateSpec{Title: "talked"})
		for _, text := range []string{"first", "second"} {
			if err := c.AddComment(id, text); err != nil {
				t.Fatal(err)
			}
		}
		cs, err := c.Comments(id)
		if err != nil || len(cs) != 2 || cs[0].Text != "first" || cs[1].Text != "second" {
			t.Fatalf("Comments = %+v, %v", cs, err)
		}
	})

	t.Run("a read-then-write under a held claim sets a field, reopens and deletes", func(t *testing.T) {
		id := create(t, bd.CreateSpec{Title: "old", Description: "d"})
		holder := create(t, bd.CreateSpec{Title: "holder"}) // a closed bead is reopened under another bead's claim
		if err := c.Claim(holder, "open"); err != nil {
			t.Fatal(err)
		}
		s := bd.UnderClaim(holder)
		if err := c.SetField(id, bd.TitleField, "new", s); err != nil {
			t.Fatal(err)
		}
		if err := c.Close([]string{id}, "done", false); err != nil {
			t.Fatal(err)
		}
		if err := c.Reopen(id, s); err != nil {
			t.Fatal(err)
		}
		b, err := c.Show(id)
		if err != nil || b.Title != "new" || b.Status != "open" {
			t.Fatalf("Show = %+v, %v; want the new title, reopened", b, err)
		}
		if err := c.Delete([]string{id}, s); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Show(id); !bd.IsNotFound(err) {
			t.Fatalf("Show(deleted) = %v, want not found", err)
		}
	})

	// held creates a bead this process holds: open, claimed, in_progress.
	held := func(t *testing.T) string {
		t.Helper()
		id := create(t, bd.CreateSpec{Title: "held"})
		if err := c.Claim(id, "open"); err != nil {
			t.Fatalf("Claim(%s) = %v", id, err)
		}
		return id
	}
	show := func(t *testing.T, id string) bd.Bead {
		t.Helper()
		b, err := c.Show(id)
		if err != nil {
			t.Fatalf("Show(%s) = %v", id, err)
		}
		return b
	}

	t.Run("claim takes an open bead and refuses a second claim", func(t *testing.T) {
		id := held(t)
		if b := show(t, id); b.Status != "in_progress" || b.Assignee != string(bd.Mine()) {
			t.Fatalf("claimed = %s held by %q", b.Status, b.Assignee)
		}
		if err := c.Claim(id, "open"); !errors.Is(err, bd.ErrClaimedSince) {
			t.Fatalf("re-Claim of my own claim = %v, want ErrClaimedSince", err)
		}
		if err := c.Claim("bdct-zz-nope", "open"); !bd.IsNotFound(err) {
			t.Fatalf("Claim(unknown) = %v, want not-found", err)
		}
	})

	t.Run("close keeps the assignee", func(t *testing.T) {
		id := held(t)
		if err := c.Close([]string{id}, "done", false); err != nil {
			t.Fatal(err)
		}
		if b := show(t, id); b.Status != "closed" || b.Assignee != string(bd.Mine()) {
			t.Fatalf("closed = %s held by %q; want closed, assignee kept", b.Status, b.Assignee)
		}
	})

	t.Run("unclaim releases an in_progress bead held by me", func(t *testing.T) {
		id := held(t)
		if err := c.Unclaim(id, bd.Mine()); err != nil {
			t.Fatalf("Unclaim = %v", err)
		}
		if b := show(t, id); b.Status != "open" || b.Assignee != "" {
			t.Fatalf("released = %s held by %q; want open, nobody", b.Status, b.Assignee)
		}
	})

	t.Run("unclaim refuses an in_progress bead held by someone else", func(t *testing.T) {
		id := held(t)
		err := c.Unclaim(id, "someone-else")
		if err == nil || !strings.Contains(err.Error(), "assignee mismatch") {
			t.Fatalf("Unclaim(mismatch) = %v, want an assignee mismatch", err)
		}
		if b := show(t, id); b.Status != "in_progress" || b.Assignee != string(bd.Mine()) {
			t.Fatalf("refusal wrote: %s held by %q", b.Status, b.Assignee)
		}
	})

	t.Run("unclaim refuses a closed bead, held by me or another, and never reopens it", func(t *testing.T) {
		for _, guard := range []bd.IfAssignee{bd.Mine(), "someone-else"} {
			id := held(t)
			if err := c.Close([]string{id}, "done", false); err != nil {
				t.Fatal(err)
			}
			err := c.Unclaim(id, guard)
			if err == nil || !strings.Contains(err.Error(), "cannot unclaim closed issue") {
				t.Fatalf("Unclaim(closed, guard %q) = %v, want cannot unclaim closed issue", guard, err)
			}
			if b := show(t, id); b.Status != "closed" || b.Assignee != string(bd.Mine()) {
				t.Fatalf("refusal wrote: %s held by %q", b.Status, b.Assignee)
			}
		}
	})

	t.Run("unclaim releases an open bead held by me", func(t *testing.T) {
		id := held(t)
		set(t, id, "open", string(bd.Mine()))
		if err := c.Unclaim(id, bd.Mine()); err != nil {
			t.Fatalf("Unclaim(open, held by me) = %v", err)
		}
		if b := show(t, id); b.Status != "open" || b.Assignee != "" {
			t.Fatalf("released = %s held by %q; want open, nobody", b.Status, b.Assignee)
		}
	})

	t.Run("unclaim refuses a blocked bead held by me", func(t *testing.T) {
		id := held(t)
		set(t, id, "blocked", string(bd.Mine()))
		err := c.Unclaim(id, bd.Mine())
		if err == nil || !strings.Contains(err.Error(), "failed to unclaim issue "+id+": no matching row") {
			t.Fatalf("Unclaim(blocked) = %v, want no matching row", err)
		}
		if b := show(t, id); b.Status != "blocked" || b.Assignee != string(bd.Mine()) {
			t.Fatalf("refusal wrote: %s held by %q", b.Status, b.Assignee)
		}
	})

	t.Run("unclaim refuses a closed bead held by another", func(t *testing.T) {
		id := held(t)
		if err := c.Close([]string{id}, "done", false); err != nil {
			t.Fatal(err)
		}
		set(t, id, "closed", "someone-else")
		err := c.Unclaim(id, bd.Mine())
		if err == nil || !strings.Contains(err.Error(), "cannot unclaim closed issue") {
			t.Fatalf("Unclaim(closed, held by another) = %v, want cannot unclaim closed issue", err)
		}
		if b := show(t, id); b.Status != "closed" || b.Assignee != "someone-else" {
			t.Fatalf("refusal wrote: %s held by %q", b.Status, b.Assignee)
		}
	})

	t.Run("claim of a bead held by another is a HeldError", func(t *testing.T) {
		id := create(t, bd.CreateSpec{Title: "theirs"})
		set(t, id, "in_progress", "someone-else")
		var h *bd.HeldError
		if err := c.Claim(id, "open"); !errors.As(err, &h) || h.Holder != "someone-else" {
			t.Fatalf("Claim(held by another) = %v, want HeldError{someone-else}", err)
		}
		if b := show(t, id); b.Assignee != "someone-else" {
			t.Fatalf("refusal wrote: held by %q", b.Assignee)
		}
	})

	t.Run("unclaim of an unknown id is not found", func(t *testing.T) {
		if err := c.Unclaim("bdct-zz-nope", bd.Mine()); !bd.IsNotFound(err) {
			t.Fatalf("Unclaim(unknown) = %v, want not-found", err)
		}
	})

	t.Run("labels add and remove", func(t *testing.T) {
		a := create(t, bd.CreateSpec{Title: "a"})
		b := create(t, bd.CreateSpec{Title: "b"})
		for _, id := range []string{a, b} {
			if err := c.AddLabels(id, "ct:x", "ct:y"); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.RemoveLabel([]string{a, b}, "ct:x"); err != nil {
			t.Fatalf("RemoveLabel = %v", err)
		}
		for _, id := range []string{a, b} {
			if got := show(t, id); got.HasLabel("ct:x") || !got.HasLabel("ct:y") {
				t.Fatalf("%s labels = %v; want only ct:y", id, got.Labels)
			}
		}
	})

	t.Run("remove edge drops a blocker", func(t *testing.T) {
		a := create(t, bd.CreateSpec{Title: "a"})
		b := create(t, bd.CreateSpec{Title: "b"})
		if err := c.AddEdge(b, a, "blocks"); err != nil {
			t.Fatal(err)
		}
		if err := c.RemoveEdge(b, a); err != nil {
			t.Fatalf("RemoveEdge = %v", err)
		}
		if es, err := c.Edges(b, bd.Down, ""); err != nil || len(es) != 0 {
			t.Fatalf("Edges after remove = %+v, %v", es, err)
		}
	})

	t.Run("batch applies all its ops", func(t *testing.T) {
		draft := create(t, bd.CreateSpec{Title: "d", Deferred: true})
		other := create(t, bd.CreateSpec{Title: "o"})
		fin := create(t, bd.CreateSpec{Title: "f"})
		if err := c.Batch([]bd.Op{bd.EdgeOp(other, draft, "blocks"), bd.OpenOp(draft, "renamed"), bd.CloseOp(fin, "sliced")}); err != nil {
			t.Fatalf("Batch = %v", err)
		}
		if b := show(t, draft); b.Status != "open" || b.Title != "renamed" {
			t.Fatalf("draft = %s %q", b.Status, b.Title)
		}
		if b := show(t, fin); b.Status != "closed" {
			t.Fatalf("fin = %s", b.Status)
		}
		if es, err := c.Edges(other, bd.Down, "blocks"); err != nil || len(es) != 1 || es[0].ID != draft {
			t.Fatalf("Edges = %+v, %v", es, err)
		}
	})

	t.Run("batch is atomic on a failing op", func(t *testing.T) {
		draft := create(t, bd.CreateSpec{Title: "d", Deferred: true})
		if err := c.Batch([]bd.Op{bd.OpenOp(draft, ""), bd.CloseOp("bdct-zz-nope", "")}); err == nil {
			t.Fatal("Batch with an unknown id succeeded")
		}
		if b := show(t, draft); b.Status != "deferred" {
			t.Fatalf("draft = %s after a failed batch; want deferred (nothing applied)", b.Status)
		}
	})
}
