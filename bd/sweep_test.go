package bd

import (
	"slices"
	"testing"
)

// IsWithdrawable is IsChainHead plus an unclosed, published Task; IsChainHead
// itself still says a Task is no chain head.
func TestIsWithdrawable(t *testing.T) {
	g := []node{
		{id: "task", kind: "task", status: "open"},
		{id: "claimed", kind: "task", status: "in_progress"},
		{id: "done", kind: "task", status: "closed"},
		{id: "draft", kind: "task", status: "deferred", draft: true},
		{id: "sig", kind: "signal", status: "open"},
		{id: "build", kind: "build", status: "open"},
	}
	all, _ := heads(t, g)
	by := func(id string) Bead { return all[slices.IndexFunc(all, func(b Bead) bool { return b.ID == id })] }
	for id, want := range map[string]bool{"task": true, "claimed": true, "done": false, "draft": false, "sig": true, "build": false} {
		if got := IsWithdrawable(by(id), all); got != want {
			t.Errorf("IsWithdrawable(%s) = %v, want %v", id, got, want)
		}
	}
	if IsChainHead(by("task"), all) {
		t.Error("IsChainHead(task) = true, want false")
	}
}

func sweepOf(t *testing.T, closing []string, g []node) Sweep {
	t.Helper()
	return sweepWith(t, false, closing, g)
}

// sweepBack sweeps as a chain head's withdraw does, with the backward rule on.
func sweepBack(t *testing.T, closing []string, g []node) Sweep {
	t.Helper()
	return sweepWith(t, true, closing, g)
}

func sweepWith(t *testing.T, backward bool, closing []string, g []node) Sweep {
	t.Helper()
	all, _ := heads(t, g)
	var set []Bead
	for _, id := range closing {
		set = append(set, all[slices.IndexFunc(all, func(b Bead) bool { return b.ID == id })])
	}
	return SweepTasks(set, all, backward)
}

func beforeIDs(s Sweep) []string {
	var out []string
	for _, w := range s.Before {
		out = append(out, w.Bead.ID)
	}
	return out
}

func leftOpenIDs(s Sweep) []string {
	var out []string
	for _, h := range s.LeftOpen {
		out = append(out, h.Bead.ID)
	}
	return out
}

func sweptIDs(s Sweep) []string {
	var out []string
	for _, w := range s.Swept {
		out = append(out, w.Bead.ID)
	}
	return out
}

// Spec D2: the Task sweep over a snapshot.
func TestSweepTasks(t *testing.T) {
	t.Run("post-land Task", func(t *testing.T) {
		s := sweepOf(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open"},
			{id: "tk", kind: "task", status: "open", blockedBy: []string{"cp"}},
		})
		if !slices.Equal(sweptIDs(s), []string{"tk"}) || s.Swept[0].After != "cp" || len(s.LeftOpen)+len(s.Releases) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("blocked on the Chart and the Spec", func(t *testing.T) {
		s := sweepOf(t, []string{"ch", "sp"}, []node{
			{id: "ch", kind: "chart", status: "open"},
			{id: "sp", kind: "spec", status: "open"},
			{id: "t1", kind: "task", status: "in_progress", blockedBy: []string{"ch"}},
			{id: "t2", kind: "task", status: "open", blockedBy: []string{"sp"}},
		})
		if !slices.Equal(sweptIDs(s), []string{"t1", "t2"}) || s.Swept[0].After != "ch" || s.Swept[1].After != "sp" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("transitive Task chain", func(t *testing.T) {
		s := sweepOf(t, []string{"cp"}, []node{
			{id: "t2", kind: "task", status: "open", blockedBy: []string{"t1"}},
			{id: "t1", kind: "task", status: "open", blockedBy: []string{"cp"}},
			{id: "cp", kind: "checkpoint", status: "open"},
		})
		if !slices.Equal(sweptIDs(s), []string{"t1", "t2"}) || s.Swept[1].After != "t1" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("outside blocker is Also", func(t *testing.T) {
		s := sweepOf(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open"},
			{id: "other", kind: "checkpoint", status: "open"},
			{id: "gone", kind: "checkpoint", status: "closed"},
			{id: "tk", kind: "task", status: "open", blockedBy: []string{"cp", "other", "gone"}},
		})
		if !slices.Equal(sweptIDs(s), []string{"tk"}) || !slices.Equal(s.Swept[0].Also, []string{"other"}) {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("pre-land Task is left open", func(t *testing.T) {
		s := sweepOf(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open"},
		})
		if len(s.Swept) != 0 || len(s.LeftOpen) != 1 || s.LeftOpen[0].Bead.ID != "tk" || s.LeftOpen[0].Before != "cp" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("pre- and post-land Task is swept", func(t *testing.T) {
		s := sweepOf(t, []string{"cp", "sp"}, []node{
			{id: "sp", kind: "spec", status: "open"},
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open", blockedBy: []string{"sp"}},
		})
		if !slices.Equal(sweptIDs(s), []string{"tk"}) || len(s.LeftOpen) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a lone Task gating a Checkpoint releases it", func(t *testing.T) {
		s := sweepOf(t, []string{"tk"}, []node{
			{id: "tk", kind: "task", status: "open"},
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
		})
		if !slices.Equal(s.Releases, []string{"cp"}) {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Checkpoint also blocked by a non-Task is no release", func(t *testing.T) {
		s := sweepOf(t, []string{"tk", "sig"}, []node{
			{id: "tk", kind: "task", status: "open"},
			{id: "sig", kind: "signal", status: "open"},
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk", "sig"}},
		})
		if len(s.Releases) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a non-Task after a swept Task stays ordered after", func(t *testing.T) {
		all, _ := heads(t, []node{
			{id: "cp", kind: "checkpoint", status: "open"},
			{id: "tk", kind: "task", status: "open", blockedBy: []string{"cp"}},
			{id: "sig", kind: "signal", status: "open", blockedBy: []string{"tk"}},
		})
		s := SweepTasks(all[:1], all, false)
		final := append(slices.Clone(all[:1]), s.Swept[0].Bead)
		if got := IDs(OrderedAfter(final, all)); !slices.Equal(got, []string{"sig"}) || len(s.Releases) != 0 {
			t.Errorf("ordered after %v, sweep %+v", got, s)
		}
	})
	t.Run("Drafts are excluded", func(t *testing.T) {
		s := sweepOf(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"dr"}},
			{id: "dr", kind: "task", status: "deferred", draft: true, blockedBy: []string{"cp"}},
		})
		if len(s.Swept)+len(s.LeftOpen) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
}

// ADR 0351: a chain's withdraw also before-closes the Tasks ordered before the set.
func TestSweepTasksBackwardRule(t *testing.T) {
	t.Run("a direct pre-land Task before-closes", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open"},
		})
		if !slices.Equal(beforeIDs(s), []string{"tk"}) || s.Before[0].Before != "cp" || len(s.LeftOpen)+len(s.Swept) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a transitive-only chain closes both", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "a", kind: "task", status: "open"},
			{id: "b", kind: "task", status: "open", blockedBy: []string{"a"}},
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"b"}},
		})
		if !slices.Equal(beforeIDs(s), []string{"b", "a"}) || s.Before[1].Before != "b" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Task also holding an outside Checkpoint stays open", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "other", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open"},
		})
		if len(s.Before) != 0 || !slices.Equal(leftOpenIDs(s), []string{"tk"}) ||
			s.LeftOpen[0].Before != "cp" || !slices.Equal(s.LeftOpen[0].Also, []string{"other"}) || len(s.Releases) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Task holding two closing Checkpoints before-closes", func(t *testing.T) {
		s := sweepBack(t, []string{"c1", "c2"}, []node{
			{id: "c1", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "c2", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open"},
		})
		if !slices.Equal(beforeIDs(s), []string{"tk"}) || s.Before[0].Before != "c1" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Task before a swept Task before-closes", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open"},
			{id: "post", kind: "task", status: "open", blockedBy: []string{"cp", "pre"}},
			{id: "pre", kind: "task", status: "open"},
		})
		if !slices.Equal(sweptIDs(s), []string{"post"}) || !slices.Equal(beforeIDs(s), []string{"pre"}) || s.Before[0].Before != "post" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("Hold Tasks on a closing Chart and Rollback before-close", func(t *testing.T) {
		s := sweepBack(t, []string{"ch", "rb"}, []node{
			{id: "ch", kind: "chart", status: "open", blockedBy: []string{"h1"}},
			{id: "rb", kind: "rollback", status: "open", blockedBy: []string{"h2"}},
			{id: "h1", kind: "task", status: "open"},
			{id: "h2", kind: "task", status: "open"},
		})
		if !slices.Equal(beforeIDs(s), []string{"h1", "h2"}) || s.Before[0].Before != "ch" || s.Before[1].Before != "rb" {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Task also blocking an outside Task stays open and that Task is not swept", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open"},
			{id: "out", kind: "task", status: "open", blockedBy: []string{"tk"}},
		})
		if len(s.Before)+len(s.Swept) != 0 || !slices.Equal(leftOpenIDs(s), []string{"tk"}) || !slices.Equal(s.LeftOpen[0].Also, []string{"out"}) {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Task blocking nothing unclosed is not pulled in", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open"},
			{id: "free", kind: "task", status: "open"},
			{id: "old", kind: "task", status: "open", blockedBy: []string{"gone"}},
			{id: "gone", kind: "task", status: "closed"},
		})
		if len(s.Before)+len(s.Swept)+len(s.LeftOpen) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Draft Task is never before-closed", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"dr"}},
			{id: "dr", kind: "task", status: "deferred", draft: true},
		})
		if len(s.Before)+len(s.LeftOpen) != 0 {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("a Task held open by a non-Checkpoint is left open", func(t *testing.T) {
		s := sweepBack(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"tk"}},
			{id: "tk", kind: "task", status: "open"},
			{id: "ch", kind: "chart", status: "open", blockedBy: []string{"tk"}},
		})
		if !slices.Equal(leftOpenIDs(s), []string{"tk"}) || !slices.Equal(s.LeftOpen[0].Also, []string{"ch"}) {
			t.Errorf("sweep = %+v", s)
		}
	})
	t.Run("with the rule off a pre-land Task stays left open", func(t *testing.T) {
		s := sweepOf(t, []string{"cp"}, []node{
			{id: "cp", kind: "checkpoint", status: "open", blockedBy: []string{"b"}},
			{id: "b", kind: "task", status: "open", blockedBy: []string{"a"}},
			{id: "a", kind: "task", status: "open"},
		})
		if len(s.Before) != 0 || !slices.Equal(leftOpenIDs(s), []string{"b"}) {
			t.Errorf("sweep = %+v", s)
		}
	})
}

// Withdraw's close is bd close --force, which closes an in_progress Task claimed by
// another actor (checked against a real bd 2026-10-22; Spec mc-xw3ez D4).
func TestForceCloseAllPassesForce(t *testing.T) {
	var got []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { got = args; return "", nil }
	if err := ForceCloseAll([]string{"a", "b"}, "withdrawn: tk"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"close", "a", "b", "--force", "--reason", "withdrawn: tk"}; !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}
