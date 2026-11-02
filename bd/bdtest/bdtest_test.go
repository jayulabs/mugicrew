package bdtest_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
)

func TestSeededBeadsReachBdReads(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-cp", Status: "open", Container: true},
		bdtest.Claimed(bd.Bead{ID: "t-b", Status: "open", Parent: "t-cp"}),
		bdtest.Draft("t-b", bd.Bead{ID: "t-d"}),
		bd.Bead{ID: "t-done", Status: "closed", Parent: "t-cp"},
	)
	if b, err := bd.Show("t-b"); err != nil || b.Status != "in_progress" || b.Assignee != bdtest.Actor {
		t.Fatalf("Show = %+v, %v; want the claimed seed", b, err)
	}
	if _, err := bd.Show("t-nope"); !bd.IsNotFound(err) {
		t.Errorf("Show(unknown) = %v, want a not-found error", err)
	}
	if ds, err := bd.Drafts("t-b"); err != nil || len(ds) != 1 || ds[0].ID != "t-d" {
		t.Errorf("Drafts = %+v, %v; want t-d", ds, err)
	}
	if kids, err := bd.ListChildren("t-cp"); err != nil || len(kids) != 2 {
		t.Errorf("ListChildren = %+v, %v; want both children, closed too", kids, err)
	}
	if ip, err := bd.InProgress(); err != nil || len(ip) != 1 || ip[0].ID != "t-b" {
		t.Errorf("InProgress = %+v, %v", ip, err)
	}
	if rs, err := bd.Ready(); err != nil || len(rs) != 1 || rs[0].ID != "t-cp" {
		t.Errorf("Ready = %+v, %v; want only the open container", rs, err)
	}
	f.Mutate("t-b", func(b *bd.Bead) { b.Title = "later" })
	if got, _ := bd.Show("t-b"); got.Title != "later" || f.Bead("t-b").Title != "later" {
		t.Errorf("a Mutate must show through Show and Bead, got %q", got.Title)
	}
	f.Seed(bd.Bead{ID: "t-new", Status: "open"})
	if _, err := bd.Show("t-new"); err != nil {
		t.Errorf("a mid-test Seed must be readable: %v", err)
	}
}

func TestAGraphDoesNotLeakToTheNextTest(t *testing.T) {
	bdtest.New(t)
	if _, err := bd.Show("t-cp"); !bd.IsNotFound(err) {
		t.Errorf("Show = %v, want not found: the first test's graph must be gone", err)
	}
}

func TestFailOnAndHeal(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "t-a", Status: "open"})
	down := errors.New("bd down")
	f.FailOn("Show", "t-a", down)
	if _, err := bd.Show("t-a"); !errors.Is(err, down) {
		t.Fatalf("Show = %v, want the injected fault", err)
	}
	f.Heal("Show", "t-a")
	if _, err := bd.Show("t-a"); err != nil {
		t.Errorf("Show after Heal = %v", err)
	}
	f.FailOn("List", "*", down)
	if _, err := bd.InProgress(); !errors.Is(err, down) {
		t.Errorf("List = %v, want the wildcard fault", err)
	}
	if calls := f.Calls(); len(calls) < 3 || calls[0] != "Show t-a" {
		t.Errorf("Calls = %v", calls)
	}
}

// TestSealRefusesTheCLI: while a fake is installed no bd exec may run, naming its argv.
func TestSealRefusesTheCLI(t *testing.T) {
	bdtest.New(t, bdtest.Draft("c", bd.Bead{ID: "t-d"}))
	if _, err := bd.RunIn("", "sql", "select 1"); err == nil || !strings.Contains(err.Error(), "sql select 1") || !strings.Contains(err.Error(), "sealed") {
		t.Errorf("RunIn = %v, want a seal refusal naming the argv", err)
	}
	if err := bd.Update("t-d", []string{"--priority", "1"}); err == nil || !strings.Contains(err.Error(), "sealed") {
		t.Errorf("a helper off the core = %v, want a seal refusal", err)
	}
}

func TestSealLiftsWithTheFake(t *testing.T) {
	t.Run("fake", func(t *testing.T) { bdtest.New(t) })
	if _, err := bd.RunIn("", "--version"); err != nil && strings.Contains(err.Error(), "sealed") {
		t.Errorf("RunIn = %v: the seal must lift at cleanup", err)
	}
}

func TestFakeIsRaceCleanUnderConcurrentReads(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "t-a", Status: "open"})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_, _ = bd.Show("t-a")
				_, _ = bd.Ready()
				_, _ = bd.OpenSet()
				f.Mutate("t-a", func(b *bd.Bead) { b.Priority++ })
			}
		}()
	}
	wg.Wait()
}

// recorder is a testing.TB that records Errorf instead of failing the test.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestFakeFailsAReadThenWriteWithoutALiveSerializer(t *testing.T) {
	var landed bd.Serializer
	if err := bd.WithLandLock(func(l bd.Serializer) error { landed = l; return nil }); err != nil {
		t.Skipf("land lock unavailable: %v", err)
	}
	released := false
	hold := bd.UnderHold("t-a", func() bool { return !released })
	released = true
	for name, s := range map[string]bd.Serializer{
		"nil Serializer":  nil,
		"an unheld claim": bd.UnderClaim("t-a"),
		"a released lock": landed,
		"a released hold": hold,
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{TB: t}
			f := bdtest.New(rec, bd.Bead{ID: "t-a", Status: "open", Title: "old"})
			ops := map[string]func() error{
				"SetField": func() error { return bd.Core(f).SetField("t-a", bd.TitleField, "new", s) },
				"Delete":   func() error { return bd.Core(f).Delete([]string{"t-a"}, s) },
				"Reopen":   func() error { return bd.Core(f).Reopen("t-a", s) },
			}
			for verb, op := range ops {
				before := len(rec.errs)
				if err := op(); err == nil || len(rec.errs) != before+1 || !strings.Contains(rec.errs[before], "ADR 0070 violation") {
					t.Errorf("%s = %v, errs %v; want an error and one ADR 0070 violation", verb, err, rec.errs)
				}
			}
			if b := f.Bead("t-a"); b.Title != "old" {
				t.Errorf("a refused write changed the bead: %+v", b)
			}
		})
	}
}

func TestValidSerializersLetReadThenWritesThrough(t *testing.T) {
	live := true
	for name, s := range map[string]bd.Serializer{
		"a held claim": bd.UnderClaim("t-h"),
		"a live hold":  bd.UnderHold("t-h", func() bool { return live }),
	} {
		t.Run(name, func(t *testing.T) {
			f := bdtest.New(t, bd.Bead{ID: "t-c", Status: "closed", Title: "old"}, bdtest.Claimed(bd.Bead{ID: "t-h"}), bd.Bead{ID: "t-x", Status: "open"})
			c := bd.Core(f)
			if err := c.SetField("t-c", bd.TitleField, "new", s); err != nil || f.Bead("t-c").Title != "new" {
				t.Errorf("SetField = %v, %+v", err, f.Bead("t-c"))
			}
			if err := c.Reopen("t-c", s); err != nil || f.Bead("t-c").Status != "open" {
				t.Errorf("Reopen = %v, %+v", err, f.Bead("t-c"))
			}
			if err := c.Delete([]string{"t-x"}, s); err != nil {
				t.Errorf("Delete = %v", err)
			}
			if _, err := bd.Show("t-x"); !bd.IsNotFound(err) {
				t.Errorf("Show(deleted) = %v, want not found", err)
			}
		})
	}
}

func TestDeleteDraftsDeletesACreatorsDraftsOnly(t *testing.T) {
	f := bdtest.New(t,
		bdtest.Claimed(bd.Bead{ID: "t-a", Status: "open"}),
		bdtest.Draft("t-a", bd.Bead{ID: "t-d1"}),
		bdtest.Draft("t-a", bd.Bead{ID: "t-d2"}),
		bdtest.Draft("t-other", bd.Bead{ID: "t-d3"}),
	)
	ids, _, err := bd.DeleteDrafts("t-a", bd.UnderClaim("t-a"))
	slices.Sort(ids)
	if err != nil || !slices.Equal(ids, []string{"t-d1", "t-d2"}) {
		t.Fatalf("DeleteDrafts = %v, %v", ids, err)
	}
	for _, id := range []string{"t-a", "t-d3"} {
		f.Bead(id) // fails the test if absent
	}
	if _, err := bd.Show("t-d1"); !bd.IsNotFound(err) {
		t.Errorf("Show(t-d1) = %v, want deleted", err)
	}
}

func TestFakeCreateEnforcesAGivenSerializer(t *testing.T) {
	rec := &recorder{TB: t}
	f := bdtest.New(rec, bd.Bead{ID: "t-a", Status: "open"}, bdtest.Claimed(bd.Bead{ID: "t-held", Status: "open"}))
	if _, err := f.Create(bd.CreateSpec{Title: "plain"}); err != nil || len(rec.errs) != 0 {
		t.Fatalf("Create without a Serializer = %v, errs %v; want a plain insert", err, rec.errs)
	}
	if _, err := f.Create(bd.CreateSpec{Title: "held", Serializer: bd.UnderClaim("t-held")}); err != nil || len(rec.errs) != 0 {
		t.Fatalf("Create under a held claim = %v, errs %v", err, rec.errs)
	}
	id, err := f.Create(bd.CreateSpec{Title: "unheld", Serializer: bd.UnderClaim("t-a")})
	if err == nil || len(rec.errs) != 1 || !strings.Contains(rec.errs[0], "ADR 0070 violation") {
		t.Fatalf("Create under an unheld claim = %q, %v, errs %v; want an ADR 0070 violation", id, err, rec.errs)
	}
	if got, _ := f.List(bd.Query{All: true}); len(got) != 4 {
		t.Errorf("%d beads, want the 2 seeded and 2 created (the refused create leaves none)", len(got))
	}
}

func TestFakeCreateMintsTheNextFreeChildSuffix(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "cp", Status: "open"}, bd.Bead{ID: "cp.1", Parent: "cp", Status: "closed", Labels: []string{"seeded"}})
	id, err := f.Create(bd.CreateSpec{Title: "kid", Parent: "cp"})
	if err != nil || id != "cp.2" {
		t.Fatalf("Create = %q, %v; want cp.2", id, err)
	}
	if got, err := f.Show("cp.1"); err != nil || got.Status != "closed" || !slices.Contains(got.Labels, "seeded") {
		t.Errorf("seeded cp.1 = %+v, %v; want it intact", got, err)
	}
}

func TestFakeCreateMintsBdsDefaultPriorityUnlessSet(t *testing.T) {
	f := bdtest.New(t)
	for _, c := range []struct {
		name string
		set  *int
		want int
	}{{"unset", nil, 2}, {"P0", ptr(0), 0}, {"P4", ptr(4), 4}} {
		id, err := f.Create(bd.CreateSpec{Title: c.name, Priority: c.set})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := f.Show(id); err != nil || got.Priority != c.want {
			t.Errorf("%s: priority = %d, %v; want %d", c.name, got.Priority, err, c.want)
		}
	}
}

func ptr(n int) *int { return &n }
