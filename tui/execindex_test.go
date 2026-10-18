package tui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// The Execute tab shows the same rows, in the same order, and renders the same view whether
// the snapshot index is absent or built. The fixture holds members, Landed rows, Rollup-ranked
// Checkpoints and waiters of all three labels.
func TestExecIndexEquivalence(t *testing.T) {
	now := time.Now()
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	live, _ := execLiveFixture()
	beads := append(live.beads, landedFixture(now)...)
	g := live
	g.beads = beads
	g.w, g.h = 120, 40
	idx := g
	idx.idx = newSnapIndex(beads)

	for name, flip := range map[string]map[string]bool{
		"folded":   nil,
		"landed":   {landedKey: true},
		"expanded": execTab{g: g}.expanded(),
	} {
		plain, indexed := g.execList(flip), idx.execList(flip)
		if !slices.Equal(execAllKeys(plain), execAllKeys(indexed)) {
			t.Errorf("%s: keys differ\nnil:   %v\nindex: %v", name, execAllKeys(plain), execAllKeys(indexed))
		}
		a, b := execTab{g: g, flip: flip}, execTab{g: idx, flip: flip}
		if va, vb := a.view(120, 40), b.view(120, 40); va != vb {
			t.Errorf("%s: rendered view differs\nnil:\n%s\nindex:\n%s", name, va, vb)
		}
	}

	// every waiter label is exercised
	var rel []string
	for _, id := range []string{"cpA", "cs"} { // cs is the pivot-gated Checkpoint
		_, r := g.waiters(&g.beads[slices.IndexFunc(g.beads, func(b bd.Bead) bool { return b.ID == id })])
		rel = append(rel, r...)
	}
	for _, l := range []string{"gates", "before land", "after land"} {
		if !slices.Contains(rel, l) {
			t.Errorf("fixture lacks waiter label %q (got %v)", l, rel)
		}
	}
	if n := len(g.landedRows()); n != 2 {
		t.Errorf("landed rows = %d; want 2", n)
	}
	if n := len(g.cpsRows()); n == 0 {
		t.Errorf("no Rollup-ranked Checkpoints")
	}
}

func execAllKeys(rows []eRow) []string {
	return rowKeys(rows, func(r eRow) string { return fmt.Sprintf("%d|%s|%d", r.kind, r.key, r.depth) })
}

// BenchmarkExecView renders the Execute tab over ~2,600 beads of closed history: 170 closed
// Checkpoints with a closed Landing (some inside the 24h window), live Checkpoints with open and
// closed members, and gating Pivot, before-land and after-land Task waiters.
// Baseline (before the index-backed lookups, ns/frame): beads=1300 idx=true 71ms, idx=false 119ms;
// beads=2600 idx=true 261ms, idx=false 427ms. Both grow ~x3.7 per doubling (quadratic), since the
// tab scans every bead per closed Checkpoint whether or not the index is present.
// After children/members/Landed/Rollup go through the index (waiters and per-build work still
// scan): beads=1300 idx=true 31ms, idx=false 103ms; beads=2600 idx=true 106ms, idx=false 430ms.
// With the Actionable rows also computed once per row-list build (-benchtime 5x): beads=1300
// idx=true 32ms, idx=false 90ms; beads=2600 idx=true 74ms, idx=false 314ms.
// After indexed waiters too (10x runs, noisy host): beads=1300 idx=true 20ms, idx=false 147ms;
// beads=2600 idx=true 34ms, idx=false 452ms. Indexed grows ~x1.7 per doubling.
func BenchmarkExecView(b *testing.B) {
	for _, scale := range []int{1, 2} {
		now := time.Now()
		clock = func() time.Time { return now }
		var beads []bd.Bead
		add := func(id string, k contract.Kind, status, parent string, mut ...func(*bd.Bead)) {
			beads = append(beads, dbead(id, k, status, parent, 1, mut...))
		}
		cp, closed := contract.KindCheckpoint, func(bd *bd.Bead) { bd.Closed, bd.CloseReason = now.Add(-time.Hour), "x" }
		for i := range 85 * scale {
			c := fmt.Sprintf("old%d", i)
			add(c, cp, "closed", "", isContainer, closed)
			for k := range 4 {
				add(fmt.Sprintf("%sm%d", c, k), contract.KindBuild, "closed", c, closed)
			}
			landed := func(bd *bd.Bead) {
				bd.Closed, bd.CloseReason = now.Add(-time.Duration(i+1)*time.Hour), "landed: aaaaaaaa"
			}
			add(c+"L", contract.KindLanding, "closed", c, landed)
		}
		for i := range 85 * scale {
			c := fmt.Sprintf("cold%d", i)
			add(c, cp, "closed", "", isContainer, closed)
			add(c+"L", contract.KindLanding, "closed", c, func(bd *bd.Bead) {
				bd.Closed, bd.CloseReason = now.Add(-48*time.Hour), "landed: bbbbbbbb"
			})
		}
		for i := range 20 * scale {
			c := fmt.Sprintf("live%d", i)
			pv, tb := c+"pv", c+"tb"
			add(c, cp, "open", "", isContainer, func(bd *bd.Bead) { bd.BlockedBy = []string{pv, tb} })
			add(pv, contract.KindPivot, "open", "")
			add(tb, contract.KindTask, "open", "")
			add(c+"ta", contract.KindTask, "open", "", blockedBy(c))
			add(c+"ap", contract.KindApproval, "open", c)
			for k := range 5 {
				add(fmt.Sprintf("%sm%d", c, k), contract.KindBuild, "closed", c, closed)
				add(fmt.Sprintf("%so%d", c, k), contract.KindBuild, "open", c)
			}
		}
		for len(beads) < 1300*scale { // closed filler, no parent
			add(fmt.Sprintf("f%d", len(beads)), contract.KindSignal, "closed", "", closed)
		}
		for _, indexed := range []bool{true, false} {
			b.Run(fmt.Sprintf("beads=%d/idx=%t", len(beads), indexed), func(b *testing.B) {
				g := graphData{beads: beads, w: 120, h: 40, sessions: map[string]session.Info{}}
				if indexed {
					g.idx = newSnapIndex(beads)
				}
				t := execTab{g: g}
				b.ResetTimer()
				for range b.N {
					_ = t.view(120, 40)
				}
			})
		}
	}
	clock = time.Now
}

// Waiters match with and without the index for a Task in both cp.BlockedBy and blocked by cp
// (first case wins), a missing blocked-by id, a duplicated bead id, and a repeated blocked-by id.
func TestWaitersIndexEquivalence(t *testing.T) {
	cp := contract.KindCheckpoint
	beads := []bd.Bead{
		dbead("both", contract.KindTask, "open", "", 1, blockedBy("c")),
		dbead("c", cp, "open", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"both", "gone", "dup", "both"} }),
		dbead("dup", contract.KindPivot, "open", "", 1),
		dbead("x", contract.KindTask, "open", "", 1, func(b *bd.Bead) { b.BlockedBy = []string{"c", "c"} }),
		dbead("dup", contract.KindTask, "open", "", 1),
	}
	g := graphData{beads: beads}
	wantB, wantR := g.waiters(&beads[1])
	g.idx = newSnapIndex(beads)
	gotB, gotR := g.waiters(&beads[1])
	if !slices.Equal(wantB, gotB) || !slices.Equal(wantR, gotR) {
		t.Errorf("indexed waiters %v %v; scan %v %v", gotB, gotR, wantB, wantR)
	}
	if len(wantB) != 4 || wantR[0] != "before land" {
		t.Errorf("fixture: %v %v", wantB, wantR)
	}
}
