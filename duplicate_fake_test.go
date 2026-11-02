package main

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// heldType is the edge type from → to in the fake graph, "" for none.
func heldType(t *testing.T, f *bdtest.Fake, from, to string) string {
	t.Helper()
	rows, err := f.Edges(from, bd.Down, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == to {
			return r.Type
		}
	}
	return ""
}

// The duplicate door over the bd fake, with the real carry, live-head and release reads.
func TestRunDuplicateOnFake(t *testing.T) {
	deferStubs := func(t *testing.T) {
		orb, osb, ocb, olh, oes := retireBead, showBead, carryBeads, liveHeadOf, endedSkips
		retireBead, showBead, carryBeads, liveHeadOf, endedSkips = bd.Retire, bd.Show, bd.Carry, bd.LiveHead, bd.EndedSkips
		t.Cleanup(func() { retireBead, showBead, carryBeads, liveHeadOf, endedSkips = orb, osb, ocb, olh, oes })
	}
	dup := func() error { return runDuplicate([]string{"d", "o"}, io.Discard) }

	t.Run("dependents and a latch move onto the unclosed original", func(t *testing.T) {
		deferStubs(t)
		evs := eventlogtest.Capture(t)
		f := bdtest.New(t,
			bd.Bead{ID: "d", Status: "open", Kind: contract.KindSignal, BlockedBy: []string{"l"}},
			bd.Bead{ID: "o", Status: "open", Kind: contract.KindSignal},
			bd.Bead{ID: "l", Status: "open", Kind: contract.KindLatch},
			bd.Bead{ID: "x", Status: "open", BlockedBy: []string{"d"}},
			bd.Bead{ID: "y", Status: "open", BlockedBy: []string{"d"}, CondBlockedBy: []string{"d"}},
			bd.Bead{ID: "z", Status: "closed", BlockedBy: []string{"d"}},
		)
		var out strings.Builder
		if err := runDuplicate([]string{"d", "o"}, &out); err != nil {
			t.Fatal(err)
		}
		if got := heldType(t, f, "x", "o"); got != "blocks" {
			t.Errorf("x → o = %q, want blocks", got)
		}
		if got := heldType(t, f, "y", "o"); got != "conditional-blocks" {
			t.Errorf("y → o = %q, want conditional-blocks", got)
		}
		if got := heldType(t, f, "o", "l"); got != "blocks" {
			t.Errorf("o → l = %q, want blocks", got)
		}
		if f.Bead("d").Status != "closed" {
			t.Error("duplicate left open")
		}
		if want := "duplicate d: closed duplicate of o\ncarried 3 onto o\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		var ev *eventlogtest.Event
		for i := range *evs {
			if (*evs)[i].Name == "carry.done" {
				ev = &(*evs)[i]
			}
		}
		if ev == nil {
			t.Fatalf("no carry.done in %v", eventlogtest.Names(*evs))
		}
		sk, _ := ev.Attrs["skipped"].([]bd.CarrySkip)
		if ev.Attrs["successor"] != "o" || !slices.Equal(sk, []bd.CarrySkip{{Bead: "z", Reason: "closed"}}) {
			t.Errorf("carry.done attrs = %v, want successor o, z skipped closed", ev.Attrs)
		}
		// A re-run on the closed duplicate adds nothing and does no harm.
		if err := dup(); err != nil {
			t.Fatalf("re-run: %v", err)
		}
		if got := heldType(t, f, "x", "o"); got != "blocks" {
			t.Errorf("after re-run x → o = %q", got)
		}
	})

	t.Run("an Initiative head takes no latch", func(t *testing.T) {
		deferStubs(t)
		evs := eventlogtest.Capture(t)
		f := bdtest.New(t,
			bd.Bead{ID: "d", Status: "open", Kind: contract.KindSignal, BlockedBy: []string{"l"}},
			bd.Bead{ID: "o", Status: "open", Kind: contract.KindInitiative},
			bd.Bead{ID: "l", Status: "open", Kind: contract.KindLatch},
		)
		if err := dup(); err != nil {
			t.Fatal(err)
		}
		if got := heldType(t, f, "o", "l"); got != "" {
			t.Errorf("o → l = %q, want none", got)
		}
		for _, e := range *evs {
			if e.Name == "carry.done" {
				if sk, _ := e.Attrs["skipped"].([]bd.CarrySkip); !slices.Equal(sk, []bd.CarrySkip{{Bead: "l", Reason: "initiative"}}) {
					t.Errorf("skipped = %v, want l initiative", sk)
				}
				return
			}
		}
		t.Error("no carry.done")
	})

	t.Run("an original closed charted carries onto the Chart", func(t *testing.T) {
		deferStubs(t)
		f := bdtest.New(t,
			bd.Bead{ID: "d", Status: "open", Kind: contract.KindSignal},
			bd.Bead{ID: "o", Status: "closed", Kind: contract.KindSignal},
			bd.Bead{ID: "c", Status: "open", Kind: contract.KindChart, CausedBy: "o"},
			bd.Bead{ID: "x", Status: "open", BlockedBy: []string{"d"}},
		)
		if err := dup(); err != nil {
			t.Fatal(err)
		}
		if got := heldType(t, f, "x", "c"); got != "blocks" {
			t.Errorf("x → c = %q, want blocks", got)
		}
		if got := heldType(t, f, "x", "o"); got != "" {
			t.Errorf("x → o = %q, want none", got)
		}
	})

	t.Run("an ended chain adds no edge and releases, recorded", func(t *testing.T) {
		deferStubs(t)
		evs := eventlogtest.Capture(t)
		f := bdtest.New(t,
			bd.Bead{ID: "d", Status: "open", Kind: contract.KindSignal},
			bd.Bead{ID: "o", Status: "closed", Kind: contract.KindSignal},
			bd.Bead{ID: "x", Status: "open", BlockedBy: []string{"d"}},
			bd.Bead{ID: "y", Status: "open", BlockedBy: []string{"d"}, CondBlockedBy: []string{"d"}},
			bd.Bead{ID: "z", Status: "closed", BlockedBy: []string{"d"}},
		)
		var out strings.Builder
		if err := runDuplicate([]string{"d", "o"}, &out); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"x", "y"} {
			if got := heldType(t, f, id, "o"); got != "" {
				t.Errorf("%s → o = %q, want none", id, got)
			}
		}
		if f.Bead("d").Status != "closed" {
			t.Error("duplicate left open")
		}
		if want := "duplicate d: closed duplicate of o\nchain of o ended: released 2\n"; out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		for _, e := range *evs {
			if e.Name == "carry.done" {
				want := []bd.CarrySkip{{Bead: "x", Reason: "ended"}, {Bead: "y", Reason: "ended"}}
				if sk, _ := e.Attrs["skipped"].([]bd.CarrySkip); !slices.Equal(sk, want) {
					t.Errorf("skipped = %v, want %v", sk, want)
				}
				return
			}
		}
		t.Error("no carry.done")
	})

	t.Run("a failed live-head read leaves the duplicate open", func(t *testing.T) {
		deferStubs(t)
		f := bdtest.New(t,
			bd.Bead{ID: "d", Status: "open", Kind: contract.KindSignal},
			bd.Bead{ID: "o", Status: "open", Kind: contract.KindSignal},
			bd.Bead{ID: "x", Status: "open", BlockedBy: []string{"d"}},
		)
		f.FailOn("Show", "o", errors.New("boom"))
		if err := dup(); err == nil {
			t.Fatal("read error swallowed")
		}
		if f.Bead("d").Status == "closed" || heldType(t, f, "d", "o") != "" {
			t.Error("duplicate closed or superseded despite the failure")
		}
	})
}
