package tui

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
	"jayulabs.org/mugicrew/sanji"
	"jayulabs.org/mugicrew/session"
)

// buildGraph groups leaves under their Chart container, collects parentless
// leaves — open Signals and Rollbacks — into Unparented (first), and drops
// bd-native beads that carry no kind: label (spec §9.1).
func TestBuildGraphGroupsByChartAndUnparented(t *testing.T) {
	beads := []bd.Bead{
		{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X — detail", Status: "in_progress"},
		{ID: "g1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open"},
		{ID: "r1", Kind: contract.KindResearch, Parent: "chart1", Status: "in_progress"},
		{ID: "sig1", Kind: contract.KindSignal, Status: "open"},  // parentless → unparented
		{ID: "rb1", Kind: contract.KindRollback, Status: "open"}, // unparented, like a Signal
		{ID: "plan1", Kind: "", Title: "a planning bead"},        // no kind → dropped
	}
	groups := buildGraph(beads)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups (unparented + chart1), got %d", len(groups))
	}
	if groups[0].name != "Unparented" || len(groups[0].members) != 2 || groups[0].members[0].ID != "sig1" || groups[0].members[1].ID != "rb1" {
		t.Errorf("unparented group wrong: %+v", groups[0])
	}
	if groups[1].key != "chart1" || groups[1].name != "Chart: X" {
		t.Errorf("chart group head/name wrong: key=%q name=%q", groups[1].key, groups[1].name)
	}
	if len(groups[1].members) != 2 {
		t.Errorf("chart1 should have 2 members, got %d", len(groups[1].members))
	}
	for _, g := range groups {
		for _, m := range g.members {
			if m.ID == "plan1" {
				t.Errorf("kind-less planning bead must be dropped from the graph")
			}
		}
	}
}

// A collapsed group hides its members but keeps its (selectable) head in the nav
// order (spec §9.1: children under a collapsed group skipped, chart heads kept).
func TestNavRowsRespectsCollapse(t *testing.T) {
	groups := buildGraph([]bd.Bead{
		{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
		{ID: "g1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open"},
		{ID: "g2", Kind: contract.KindResearch, Parent: "chart1", Status: "open"},
	})
	open := navRows(groups, map[string]bool{})
	if len(open) != 3 || open[0].sel != "chart1" || open[1].sel != "g1" || open[2].sel != "g2" {
		t.Fatalf("expanded rows wrong: %+v", open)
	}
	// tree connectors: only the last member of a group is a └ (last), the rest ├.
	if open[1].last || !open[2].last {
		t.Errorf("last flags wrong: g1.last=%v g2.last=%v (want false,true)", open[1].last, open[2].last)
	}
	folded := navRows(groups, map[string]bool{"chart1": true})
	if len(folded) != 1 || folded[0].sel != "chart1" || !folded[0].header {
		t.Fatalf("collapsed should keep only the head row, got %+v", folded)
	}
}

// The Inbox shows open HITL kinds only, in beadCmp order (closed last, priority,
// then freshest); AFK kinds never gate it, and closed HITL beads are excluded (spec
// §9.2) — unless one has a session, which stays visible (ADR 0038 §1, FR20).
func TestInboxListOpenHITLSorted(t *testing.T) {
	now := time.Now()
	beads := []bd.Bead{
		{ID: "old", Kind: contract.KindSignal, Status: "open", Created: now.Add(-3 * time.Hour)},
		{ID: "low", Kind: contract.KindSignal, Status: "open", Priority: 3, Created: now}, // newest, but P3
		{ID: "new", Kind: contract.KindGrilling, Status: "open", Created: now.Add(-1 * time.Hour)},
		{ID: "afk", Kind: contract.KindResearch, Status: "open", Created: now},       // AFK: never in inbox
		{ID: "done", Kind: contract.Kind("unowned"), Status: "closed", Created: now}, // closed: excluded
		{ID: "proto", Kind: contract.KindPrototype, Status: "open", Created: now.Add(-2 * time.Hour)},
		{ID: "held", Kind: contract.KindSignal, Status: "closed", Created: now.Add(-4 * time.Hour)}, // closed, live session
	}
	got := inboxList(beads, map[string]session.Info{"held": {State: session.Alive}})
	var ids []string
	for _, b := range got {
		ids = append(ids, b.ID)
	}
	want := []string{"new", "proto", "old", "low", "held"} // P0 freshest → oldest, then P3, closed last; AFK + session-less closed dropped
	if len(ids) != len(want) {
		t.Fatalf("inbox = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("inbox order = %v, want %v", ids, want)
		}
	}
}

// The Inbox lists only work the human can take now (FR14–16): an open bead with
// any open blocker drops out unless it has a session, live or exited. A closed
// blocker unblocks; a blocker missing from the snapshot counts as open. Graph
// and Search still show the blocked bead.
func TestInboxListHidesBlocked(t *testing.T) {
	beads := []bd.Bead{
		{ID: "free", Kind: contract.KindSignal, Status: "open"},
		{ID: "done", Kind: contract.KindSignal, Status: "closed"},
		{ID: "blk", Kind: contract.KindGrilling, Status: "open", Title: "Waits on free", BlockedBy: []string{"free"}},
		{ID: "gone", Kind: contract.KindGrilling, Status: "open", BlockedBy: []string{"missing"}},
		{ID: "unblk", Kind: contract.KindGrilling, Status: "open", BlockedBy: []string{"done"}},
		{ID: "live", Kind: contract.KindGrilling, Status: "open", BlockedBy: []string{"free"}},
		{ID: "exited", Kind: contract.KindGrilling, Status: "open", BlockedBy: []string{"free"}},
	}
	sessions := map[string]session.Info{"live": {State: session.Alive}, "exited": {State: session.Exited}}
	var ids []string
	for _, b := range inboxList(beads, sessions) {
		ids = append(ids, b.ID)
	}
	slices.Sort(ids)
	if want := []string{"exited", "free", "live", "unblk"}; !slices.Equal(ids, want) {
		t.Fatalf("inbox = %v, want %v", ids, want)
	}

	m := New(nil, nil)
	m.w, m.h = 120, 30
	m.tab = tabInbox
	next, _ := m.Update(loadedMsg{beads: beads})
	m = next.(Model)
	if v := m.View(); strings.Contains(v, "Waits on free") {
		t.Fatalf("Inbox view shows blocked bead:\n%s", v)
	}
	if rows, _ := m.searchRows(); !slices.ContainsFunc(rows, func(b bd.Bead) bool { return b.ID == "blk" }) {
		t.Fatalf("Search rows lack blocked bead")
	}
	m.tab = tabGraph
	if v := m.View(); !strings.Contains(v, "Waits on free") {
		t.Fatalf("Graph view lacks blocked bead:\n%s", v)
	}
}

// The Inbox collapses a Checkpoint's pivots to one lead row — its oldest open
// pivot — so the human triages a Checkpoint once (FR20–22, ADR 0045/0046). The
// gate is the Checkpoint's blocks edge (BlockedBy). A pivot with a session stays
// visible (ADR 0038 §1); pivots on different Checkpoints each get a row; a pivot
// no Checkpoint blocks yet stands alone.
func TestInboxListLeadPivotPerCheckpoint(t *testing.T) {
	now := time.Now()
	at := func(h int) time.Time { return now.Add(time.Duration(-h) * time.Hour) }
	pivot := func(id, status string, h int) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindPivot, Status: status, Title: "Pivot " + id, Created: at(h)}
	}
	cp := func(id string, blockedBy ...string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: blockedBy}
	}
	for _, c := range []struct {
		name     string
		beads    []bd.Bead
		sessions map[string]session.Info
		want     []string
	}{
		{"one lead per Checkpoint, the oldest",
			[]bd.Bead{cp("cp1", "p1", "p2", "p3"), pivot("p1", "open", 2), pivot("p2", "open", 5), pivot("p3", "open", 1)},
			nil, []string{"p2"}},
		{"next-oldest promoted when the lead closes",
			[]bd.Bead{cp("cp1", "p1", "p2", "p3"), pivot("p1", "open", 2), pivot("p2", "closed", 5), pivot("p3", "open", 1)},
			nil, []string{"p1"}},
		{"a claimed lead stays the lead",
			[]bd.Bead{cp("cp1", "p1", "p2"), pivot("p1", "open", 2), pivot("p2", "in_progress", 5)},
			map[string]session.Info{"p2": {State: session.Alive}}, []string{"p2"}},
		{"a claimed lead whose session was lost stays visible, alone",
			[]bd.Bead{cp("cp1", "p1", "p2"), pivot("p1", "open", 2), pivot("p2", "in_progress", 5)},
			nil, []string{"p2"}},
		{"a non-lead pivot with a session stays visible",
			[]bd.Bead{cp("cp1", "p1", "p2"), pivot("p1", "open", 2), pivot("p2", "open", 5)},
			map[string]session.Info{"p1": {State: session.Alive}}, []string{"p1", "p2"}},
		{"a closed pivot with a session stays visible",
			[]bd.Bead{cp("cp1", "p1", "p2"), pivot("p1", "open", 2), pivot("p2", "closed", 5)},
			map[string]session.Info{"p2": {State: session.Exited}}, []string{"p1", "p2"}},
		{"pivots on different Checkpoints each get a row",
			[]bd.Bead{cp("cp1", "p1", "p2"), cp("cp2", "q1", "q2"),
				pivot("p1", "open", 2), pivot("p2", "open", 3), pivot("q1", "open", 1), pivot("q2", "open", 4)},
			nil, []string{"p2", "q2"}},
		{"a pivot no Checkpoint blocks stands alone",
			[]bd.Bead{cp("cp1", "p1"), pivot("p1", "open", 2), pivot("loose", "open", 3)},
			nil, []string{"p1", "loose"}},
	} {
		var ids []string
		for _, b := range inboxList(c.beads, c.sessions) {
			ids = append(ids, b.ID)
		}
		if !slices.Equal(ids, c.want) {
			t.Errorf("%s: inbox = %v, want %v", c.name, ids, c.want)
		}
	}
}

// The lead's detail names the other open pivots its session covers; a lone
// pivot or a non-lead sibling shows no such line.
func TestDetailListsOtherOpenPivots(t *testing.T) {
	now := time.Now()
	m := New(nil, nil)
	m.beads = []bd.Bead{
		{ID: "cp1", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: []string{"p1", "p2", "p3", "p4"}},
		{ID: "p1", Kind: contract.KindPivot, Status: "open", Title: "Pivot: FR3: lead", Created: now.Add(-5 * time.Hour)},
		{ID: "p2", Kind: contract.KindPivot, Status: "open", Title: "Pivot: FR7: second", Created: now.Add(-2 * time.Hour)},
		{ID: "p3", Kind: contract.KindPivot, Status: "open", Title: "Pivot: FR9: third", Created: now.Add(-1 * time.Hour)},
		{ID: "p4", Kind: contract.KindPivot, Status: "closed", Title: "Pivot: FR1: settled", Created: now.Add(-9 * time.Hour)},
		{ID: "lone", Kind: contract.KindPivot, Status: "open", Title: "Pivot: FR2: alone", Created: now},
	}
	m.w, m.h = 160, 40
	m.tab = tabInbox
	s, _ := m.detailLayout(m.beadByID("p1"), m.paneHeight(), 0, 0, 2)
	for _, want := range []string{"+2 other open pivots", "Pivot: FR7: second", "Pivot: FR9: third"} {
		if !strings.Contains(s, want) {
			t.Errorf("lead detail missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "FR1: settled") {
		t.Errorf("lead detail must not list a closed pivot:\n%s", s)
	}
	if s, _ := m.detailLayout(m.beadByID("lone"), m.paneHeight(), 0, 0, 2); strings.Contains(s, "other open pivot") {
		t.Errorf("a lone pivot lists no others:\n%s", s)
	}
	if s, _ := m.detailLayout(m.beadByID("p2"), m.paneHeight(), 0, 0, 2); strings.Contains(s, "other open pivot") {
		t.Errorf("only the lead lists the others, not a non-lead sibling:\n%s", s)
	}
	// A tie on Created: the first seen leads (as in the Inbox); its twin does not.
	m.beads[2].Created = m.beads[1].Created
	if s, _ := m.detailLayout(m.beadByID("p2"), m.paneHeight(), 0, 0, 2); strings.Contains(s, "other open pivot") {
		t.Errorf("a same-age twin of the lead lists no others:\n%s", s)
	}
}

// isHITL is registry-derived, not a parallel hand-maintained list: every kind's
// HITL-ness must match the registry's Mode (the sole AFK/HITL source), so a new
// HITL kind can't silently mislabel as AFK the way KindApproval did.
func TestIsHITLMatchesRegistry(t *testing.T) {
	hitl := []contract.Kind{
		contract.KindSignal, contract.KindGrilling,
		contract.KindPrototype, contract.KindApproval, contract.KindTask,
	}
	afk := []contract.Kind{
		contract.KindResearch, contract.KindChartConflict, contract.KindChart, contract.KindSpec, contract.KindBuild,
		contract.KindCritique, contract.KindConflict, contract.KindReview, contract.KindCheckpoint,
	}
	for _, k := range hitl {
		if !isHITL(k) {
			t.Errorf("isHITL(%q) = false, want true (registry Mode=hitl)", k)
		}
	}
	for _, k := range afk {
		if isHITL(k) {
			t.Errorf("isHITL(%q) = true, want false (registry Mode=afk)", k)
		}
	}
}

// The `h` toggle hides closed root beads: a closed root Chart drops whole, a
// closed parentless leaf drops from Unparented, while open roots and any member
// with a parent (not a root) stay. Off, everything passes through.
func TestHideClosedRootGroups(t *testing.T) {
	beads := []bd.Bead{
		{ID: "chartOpen", Kind: contract.KindChart, Container: true, Title: "Chart: A", Status: "in_progress"},
		{ID: "g1", Kind: contract.KindGrilling, Parent: "chartOpen", Status: "closed"}, // closed but has parent → kept
		{ID: "chartDone", Kind: contract.KindChart, Container: true, Title: "Chart: B", Status: "closed"},
		{ID: "sigOpen", Kind: contract.KindSignal, Status: "open"},   // open root → kept
		{ID: "sigDone", Kind: contract.KindSignal, Status: "closed"}, // closed root → dropped
	}
	groups := buildGraph(beads)

	off := hideClosedRootGroups(groups, false, nil)
	if len(off) != len(groups) {
		t.Fatalf("off should pass through: got %d want %d", len(off), len(groups))
	}

	on := hideClosedRootGroups(groups, true, nil)
	var heads, unparented []string
	for _, g := range on {
		if g.head != nil {
			heads = append(heads, g.head.ID)
		} else {
			for _, m := range g.members {
				unparented = append(unparented, m.ID)
			}
		}
	}
	if len(heads) != 1 || heads[0] != "chartOpen" {
		t.Errorf("closed root Chart should be hidden whole; heads=%v", heads)
	}
	if len(unparented) != 1 || unparented[0] != "sigOpen" {
		t.Errorf("only the open root leaf should remain in unparented; got %v", unparented)
	}
}

// Startup opening state: New opens with hide-closed-roots on, and the
// collapse seed folds every closed-head group (agreeing with the hide predicate)
// while leaving open-head and the head-less unparented group expanded.
func TestSeedCollapsedClosedRoots(t *testing.T) {
	if !New(nil, nil).hideClosedRoots {
		t.Errorf("New should open with hideClosedRoots true")
	}

	groups := buildGraph([]bd.Bead{
		{ID: "chartOpen", Kind: contract.KindChart, Container: true, Title: "Chart: A", Status: "in_progress"},
		{ID: "g1", Kind: contract.KindGrilling, Parent: "chartOpen", Status: "open"},
		{ID: "chartDone", Kind: contract.KindChart, Container: true, Title: "Chart: B", Status: "closed"},
		{ID: "g2", Kind: contract.KindResearch, Parent: "chartDone", Status: "open"},
		{ID: "sig", Kind: contract.KindSignal, Status: "open"}, // head-less unparented → never seeded
	})
	collapsed := map[string]bool{}
	seedCollapsedClosedRoots(groups, collapsed)

	if !collapsed["chartDone"] {
		t.Errorf("closed-head group should open collapsed")
	}
	if collapsed["chartOpen"] {
		t.Errorf("open-head group should open expanded")
	}
	if collapsed[""] {
		t.Errorf("head-less unparented group should not be seeded collapsed")
	}
}

// windowRows is the pure visual-row window behind the scrollable detail pane:
// wrap the body at width, split into visual rows, return the
// height-row window at offset — clamped so there is no blank overscroll and a
// body that fits renders unchanged.
func TestWindowRows(t *testing.T) {
	trim := func(rows []string) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = strings.TrimRight(r, " ")
		}
		return out
	}
	eq := func(got []string, want ...string) bool {
		got = trim(got)
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// body shorter than height: all rows, unchanged, no padding to height.
	if got := windowRows("a\nb", 20, 5, 0); !eq(got, "a", "b") {
		t.Errorf("short body: got %#v", trim(got))
	}
	// soft-wrap: a long line at a narrow width splits into visual rows, and the
	// window takes only `height` of them.
	if got := windowRows("aaaaa bbbbb ccccc ddddd", 5, 2, 0); !eq(got, "aaaaa", "bbbbb") {
		t.Errorf("soft-wrap window: got %#v", trim(got))
	}
	body := "l1\nl2\nl3\nl4\nl5"
	if got := windowRows(body, 20, 2, 2); !eq(got, "l3", "l4") {
		t.Errorf("offset 2: got %#v", trim(got))
	}
	// offset past the bottom clamps to the last full window (no blank overscroll).
	if got := windowRows(body, 20, 2, 99); !eq(got, "l4", "l5") {
		t.Errorf("overscroll clamp: got %#v", trim(got))
	}
	// negative offset clamps to the top.
	if got := windowRows(body, 20, 2, -3); !eq(got, "l1", "l2") {
		t.Errorf("negative clamp: got %#v", trim(got))
	}
}

func TestFollowOffset(t *testing.T) {
	cases := []struct {
		name               string
		total, height, sel int
		want               int
	}{
		{"shorter than window", 3, 10, 2, 0},
		{"near top clamps to 0", 20, 6, 1, 0},
		{"near bottom clamps to total-height", 20, 6, 19, 14},
		{"middle centres", 20, 6, 10, 7}, // 10 - 6/2
	}
	for _, c := range cases {
		off := followOffset(c.total, c.height, c.sel)
		if off != c.want {
			t.Errorf("%s: followOffset(%d,%d,%d) = %d, want %d", c.name, c.total, c.height, c.sel, off, c.want)
		}
		// invariant: the selection is inside the window in every case.
		if c.sel < off || c.sel >= off+c.height {
			t.Errorf("%s: sel %d not in window [%d,%d)", c.name, c.sel, off, off+c.height)
		}
	}
}

// j/k scroll the detail body only when the detail pane holds focus,
// and the offset clamps at both ends so there is no blank overscroll.
func TestDetailScrollFocusAndClamp(t *testing.T) {
	key := func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
	long := strings.Repeat("word ", 400) // wraps to far more visual rows than one pane
	m := New(nil, nil)
	m.beads = []bd.Bead{
		{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
		{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Description: long},
	}
	m.rebuildGroups()
	m.selGraph = "grl1"

	// Unfocused: j walks the master list, it does not scroll the body.
	next, _ := m.Update(key('j'))
	if got := next.(Model).detailOffset; got != 0 {
		t.Errorf("unfocused j must not scroll the body, offset=%d", got)
	}

	// Focused: j scrolls the body.
	m.focusDetail = true
	next, _ = m.Update(key('j'))
	m = next.(Model)
	if m.detailOffset != 1 {
		t.Fatalf("focused j should scroll to 1, got %d", m.detailOffset)
	}

	// k past the top clamps at 0.
	for i := 0; i < 5; i++ {
		next, _ = m.Update(key('k'))
		m = next.(Model)
	}
	if m.detailOffset != 0 {
		t.Errorf("k past top must clamp at 0, got %d", m.detailOffset)
	}

	// Hammering j clamps at the bottom (the renderer's max offset).
	for i := 0; i < 2000; i++ {
		next, _ = m.Update(key('j'))
		m = next.(Model)
	}
	_, maxOff := m.detailLayout(m.selectedBead(), m.paneHeight(), 0, 0, 0)
	if maxOff == 0 {
		t.Fatal("a long description should have a positive max offset")
	}
	if m.detailOffset != maxOff {
		t.Errorf("j hammered to offset %d, want clamp at maxOff %d", m.detailOffset, maxOff)
	}

	// Moving the selection resets the scroll back to the top of the new body.
	m.focusDetail = false
	next, _ = m.Update(key('j'))
	if got := next.(Model).detailOffset; got != 0 {
		t.Errorf("moving selection must reset scroll to 0, got %d", got)
	}
}

// The responsive render path picks a layout from the terminal geometry and re-
// evaluates it live on every render — no cached mode. Pure render over
// fixtures at the prototype's measured breakpoints; no live bubbletea program.
func TestResponsiveRenderPath(t *testing.T) {
	const border = "│" // splitPane's master BorderRight — present only two-pane
	long := strings.Repeat("word ", 200)
	// View minus the footer line: the footer's bead│view divider is also a │.
	body := func(v string) string { v = strings.TrimRight(v, "\n"); return v[:strings.LastIndex(v, "\n")] }
	base := New(nil, nil)
	base.beads = []bd.Bead{
		{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: Responsive", Status: "open"},
		{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Title: "A grilling leaf", Description: long},
	}
	base.rebuildGroups()
	base.selGraph = "grl1"
	at := func(w, h int) Model { m := base; m.w, m.h = w, h; return m }

	// Below the 40x12 floor: the too-small notice names required + actual, not a
	// starved layout.
	if v := at(36, 10).View(); !strings.Contains(v, "terminal too small") ||
		!strings.Contains(v, "need 40x12") || !strings.Contains(v, "have 36x10") {
		t.Errorf("36x10 should show the too-small notice with geometry, got:\n%s", v)
	}

	// Single pane below the breakpoint: no split border, selection visible, every
	// rendered row fits the terminal height.
	for _, g := range []struct{ w, h int }{{40, 12}, {55, 16}, {83, 18}} {
		v := at(g.w, g.h).View()
		if strings.Contains(body(v), border) {
			t.Errorf("%dx%d should be single pane (no split border), got:\n%s", g.w, g.h, v)
		}
		if !strings.Contains(v, "❯") {
			t.Errorf("%dx%d single pane must show the master selection", g.w, g.h)
		}
		if n := len(strings.Split(v, "\n")); n > g.h {
			t.Errorf("%dx%d rendered %d rows, exceeds terminal height", g.w, g.h, n)
		}
	}

	// At/above the breakpoint: today's two-pane split, unchanged.
	if v := at(84, 18).View(); !strings.Contains(v, border) {
		t.Errorf("84x18 should keep the two-pane split, got:\n%s", v)
	}

	// Single-pane focus follows focusDetail (Tab, spec §9.4) with no new key: the
	// detail pane shows the bead id and the master list no longer renders.
	det := at(55, 16)
	det.focusDetail = true
	if v := det.View(); !strings.Contains(v, "grl1") {
		t.Errorf("detail-focused single pane must show the bead detail, got:\n%s", v)
	} else if strings.Contains(v, "❯") {
		t.Errorf("detail-focused single pane must not render the master list")
	}

	// Fully reversible: each mode re-derives purely from geometry — two-pane →
	// single → too-small → back, no restart, no cached mode field.
	modes := func(w, h int) (small, split bool) {
		v := at(w, h).View()
		return strings.Contains(v, "terminal too small"), strings.Contains(body(v), border)
	}
	for _, c := range []struct {
		w, h                int
		wantSmall, wantSpit bool
	}{
		{84, 24, false, true},  // two-pane
		{50, 20, false, false}, // shrink to single
		{30, 8, true, false},   // shrink past floor
		{84, 24, false, true},  // grow back — restored
	} {
		if s, sp := modes(c.w, c.h); s != c.wantSmall || sp != c.wantSpit {
			t.Errorf("%dx%d: small=%v split=%v, want small=%v split=%v", c.w, c.h, s, sp, c.wantSmall, c.wantSpit)
		}
	}
}

// The footer is the single command surface (ADR 0026): exact strings
// per tab × single/two-pane × focus × selection, plus the [i/n] indicator.
// Pure render over fixtures — no live bubbletea program.
func TestSinglePaneChrome(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	beads := []bd.Bead{
		{ID: "hitl", Kind: contract.KindGrilling, Status: "open", Title: "HITL open"},
		{ID: "hitlB", Kind: contract.KindGrilling, Status: "open", Title: "HITL blocked", BlockedBy: []string{"hitl"}},
		{ID: "hitlS", Kind: contract.KindGrilling, Status: "blocked", Title: "HITL status=blocked"},
		{ID: "afk", Kind: contract.KindResearch, Status: "open", Title: "AFK ready"},
		{ID: "afkB", Kind: contract.KindResearch, Status: "open", Title: "AFK blocked", BlockedBy: []string{"hitl"}},
		{ID: "done", Kind: contract.KindResearch, Status: "closed", Title: "closed"},
		{ID: "hitlC", Kind: contract.KindGrilling, Status: "closed", Title: "HITL closed"},
		{ID: "hitlE", Kind: contract.KindGrilling, Container: true, Status: "open", Title: "HITL flagged epic"},
		{ID: "chD", Kind: contract.KindChart, Container: true, Status: "open", Title: "Chart: drained", Description: "Destination: x"},
		{ID: "k1", Kind: contract.KindResearch, Parent: "chD", Status: "closed", Title: "drained child"},
		{ID: "chO", Kind: contract.KindChart, Container: true, Status: "open", Title: "Chart: draining", Description: "Destination: x"},
		{ID: "k2", Kind: contract.KindResearch, Parent: "chO", Status: "open", Title: "open child"},
		{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "Checkpoint: ready"},
		{ID: "ap", Kind: contract.KindApproval, Parent: "cp", Status: "closed", Title: "approval"},
	}
	const (
		twoW    = 84
		singleW = 59
	)
	for _, c := range []struct {
		name   string
		tab    tab
		w      int
		detail bool   // focusDetail
		sel    string // "" = no selection
		want   string
	}{
		{"graph two-pane HITL open", tabGraph, twoW, false, "hitl", "l launch · p pause · e edit · L logs │ h hide-closed · o loop on · f kind · ? · q"},
		{"graph two-pane focus-independent", tabGraph, twoW, true, "hitl", "l launch · p pause · e edit · L logs │ h hide-closed · o loop on · f kind · ? · q"},
		{"graph HITL blocked", tabGraph, twoW, false, "hitlB", "l launch⚠ · p pause · e edit · L logs │ h hide-closed · o loop on · f kind · ? · q"},
		{"graph HITL status=blocked", tabGraph, twoW, false, "hitlS", "l launch⚠ · p pause · e edit · L logs │ h hide-closed · o loop on · f kind · ? · q"},
		{"graph AFK ready", tabGraph, twoW, false, "afk", "t run · p pause · e edit · L logs │ h hide-closed · o loop on · f kind · ? · q"},
		{"graph AFK blocked", tabGraph, twoW, false, "afkB", "p pause · e edit · L logs │ h hide-closed · o loop on · f kind · / search · ? · q"},
		{"graph closed", tabGraph, twoW, false, "done", "e edit · L logs │ n signal · h hide-closed · o loop on · f kind · / search · ? · q"},
		{"graph closed HITL", tabGraph, twoW, false, "hitlC", "e edit · L logs │ n signal · h hide-closed · o loop on · f kind · / search · ? · q"},
		// Container-ness is bd's epic flag everywhere: the header folds, the footer hides l, the chip is padded.
		{"graph HITL with epic flag", tabGraph, twoW, false, "hitlE", "p pause · e edit · L logs │ space fold · h hide-closed · o loop on · f kind · ? · q"},
		{"graph drained Chart header", tabGraph, twoW, false, "chD", "t spec · p pause · e edit · L logs │ space fold · h hide-closed · o loop on · ? · q"},
		{"graph draining Chart header", tabGraph, twoW, false, "chO", "p pause · e edit · L logs │ space fold · h hide-closed · o loop on · f kind · ? · q"},
		{"graph land-ready Checkpoint", tabGraph, twoW, false, "cp", "t land · p pause · e edit · L logs │ space fold · h hide-closed · o loop on · ? · q"},
		{"graph unparented header, no bead", tabGraph, twoW, false, unparentedSel, "n signal · space fold · h hide-closed · o loop on · f kind · / search · ? · q"},
		{"graph empty", tabGraph, twoW, false, "", "n signal · u surgery · h hide-closed · o loop on · f kind · / search · ? · q"},
		{"graph single master leaf (59 cols drops r, o, h)", tabGraph, singleW, false, "afk", "t run · p pause · e edit · L logs │ tab detail · ? · q"},
		{"graph single master header (59 cols drops r, o, h, space)", tabGraph, singleW, false, "chD", "t spec · p pause · e edit · L logs │ tab detail · ? · q"},
		{"graph single detail", tabGraph, singleW, true, "chD", "t spec · p pause · e edit · L logs │ tab list · ? · q"},
		{"inbox two-pane HITL", tabInbox, twoW, false, "hitl", "l launch · +/- pri · e edit │ n signal · f kind · / search · o loop on · ? · q"},
		{"inbox HITL blocked", tabInbox, twoW, false, "hitlB", "l launch⚠ · +/- pri · e edit │ n signal · f kind · / search · o loop on · ? · q"},
		{"inbox empty", tabInbox, twoW, false, "", "n signal · u surgery · f kind · / search · o loop on · ? · q"},
		{"inbox single master (59 cols drops r, o, /)", tabInbox, singleW, false, "hitl", "l launch · e edit │ tab detail · n signal · f kind · ? · q"},
		{"inbox single detail", tabInbox, singleW, true, "hitl", "l launch · e edit │ tab list · n signal · f kind · ? · q"},
		{"inbox AFK never t", tabInbox, twoW, false, "afk", "+/- pri · e edit │ n signal · u surgery · f kind · / search · o loop on · ? · q"},
		// Logs selects events, not beads: no bead keys, only view keys.
		{"logs two-pane", tabLogs, twoW, false, "hitl", "d +debug · e errors · o loop on · f kind · / search · ? · q"},
		{"logs single master (59 cols drops /)", tabLogs, singleW, false, "hitl", "tab detail d +debug e errors o loop on f kind ? q"},
		// 40-col floor: view keys drop right-first, bead keys, ? and q survive, tightened (ADR 0058 §5).
		{"graph 40 cols (drops tab too)", tabGraph, floorCols, false, "chD", "t spec p pause e edit L logs │ ? q"},
	} {
		m := New(nil, nil)
		m.beads = beads
		m.hideClosedRoots = false
		m.rebuildGroups()
		m.w, m.h = c.w, 24
		m.tab, m.focusDetail = c.tab, c.detail
		m.selGraph, m.selInbox = "", ""
		if c.tab == tabGraph {
			m.selGraph = c.sel
		} else {
			m.selInbox = c.sel
		}
		got := m.footer()
		if got != c.want {
			t.Errorf("%s: footer = %q, want %q", c.name, got, c.want)
		}
		if lipgloss.Width(got) > c.w || strings.Contains(got, "\n") {
			t.Errorf("%s: footer must fit %d cols on one line, got %q", c.name, c.w, got)
		}
		// Commands live only in the footer: the detail pane carries no action set.
		s, _ := m.detailLayout(m.selectedBead(), m.paneHeight(), 0, 0, 1)
		for _, ln := range strings.Split(s, "\n") {
			if strings.HasSuffix(strings.TrimSpace(ln), "e edit") {
				t.Errorf("%s: detail pane must not carry the actionBar, got:\n%s", c.name, s)
			}
		}
	}

	// The chip reads the same epic flag as the footer, so they can't disagree.
	for _, b := range beads {
		if chip := strings.Contains(kindTag(&b), " "); chip != b.Container {
			t.Errorf("%s: kindTag = %q, want chip iff Container=%v", b.ID, kindTag(&b), b.Container)
		}
	}

	// h names what pressing it will do, from the toggle state.
	m := New(nil, nil)
	m.w, m.h = twoW, 24
	if got := m.footer(); !strings.Contains(got, "h show-closed") {
		t.Errorf("closed roots hidden: footer = %q, want h show-closed", got)
	}

	// The status message stays on the line below the footer.
	m.status = "refreshing…"
	if got := m.helpLine(); !strings.HasSuffix(got, "\nrefreshing…") {
		t.Errorf("status must render below the footer, got %q", got)
	}

	// [i/n] list-position indicator: present only single-pane + detail-focused
	// (FR 20), absent two-pane (FR 21) and single-pane master-focused.
	b := &bd.Bead{ID: "grl1", Kind: contract.KindGrilling, Status: "open", Title: "A leaf"}
	single := New(nil, nil)
	single.w, single.h = 55, 16
	single.focusDetail = true
	if s, _ := single.detailLayout(b, single.paneHeight(), 0, 1, 3); !strings.Contains(s, "[2/3]") {
		t.Errorf("single-pane detail-focused must show [2/3], got:\n%s", s)
	}
	single.focusDetail = false
	if s, _ := single.detailLayout(b, single.paneHeight(), 0, 1, 3); strings.Contains(s, "[2/3]") {
		t.Errorf("single-pane master-focused must omit [i/n], got:\n%s", s)
	}
	two := New(nil, nil)
	two.w, two.h = 84, 24
	two.focusDetail = true
	if s, _ := two.detailLayout(b, two.paneHeight(), 0, 1, 3); strings.Contains(s, "[2/3]") {
		t.Errorf("two-pane detail must omit [i/n] (list is visible), got:\n%s", s)
	}
}

// guidedUpdateArgs builds the `bd update` flag args for a single guided-edit
// field. The value rides as a --flag arg: exec.Command hands argv
// straight to bd with no shell, so a multi-line description survives with its
// newlines intact — this locks that construction (the interactive $EDITOR step
// is out of scope, per the ticket).
func TestGuidedUpdateArgs(t *testing.T) {
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	if got := guidedUpdateArgs("title", "A new title"); !eq(got, []string{"--title", "A new title"}) {
		t.Errorf("title: got %#v", got)
	}
	desc := "First line.\nSecond line.\n\nA paragraph after a blank line."
	if got := guidedUpdateArgs("description", desc); !eq(got, []string{"--description", desc}) {
		t.Errorf("multi-line description must ride intact as one --description arg: got %#v", got)
	}
}

// The guided-edit key routing: e opens the picker; t enters the inline
// title editor pre-filled with the current title; E is the raw bd-update hatch;
// esc from the picker cancels. The commit paths shell out to bd/$EDITOR and are
// out of scope here.
func TestGuidedEditKeyRouting(t *testing.T) {
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	newModel := func() Model {
		m := New(nil, nil)
		m.beads = []bd.Bead{
			{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
			{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Title: "Existing title"},
		}
		m.rebuildGroups()
		m.selGraph = "grl1"
		return m
	}

	// e -> picker.
	m := newModel()
	next, _ := m.Update(key("e"))
	m = next.(Model)
	if m.editMode != editPick {
		t.Fatalf("e should open the picker, editMode=%d", m.editMode)
	}
	// t -> inline title editor pre-filled with the current title.
	next, _ = m.Update(key("t"))
	m = next.(Model)
	if m.editMode != editTitle || m.editBuf.text != "Existing title" {
		t.Errorf("t should open the title editor pre-filled; mode=%d buf=%q", m.editMode, m.editBuf.text)
	}

	// esc from the picker cancels back to no edit.
	m = newModel()
	next, _ = m.Update(key("e"))
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m2 := next.(Model); m2.editMode != editNone {
		t.Errorf("esc should cancel the picker, editMode=%d", m2.editMode)
	}

	// E -> raw bd-update hatch, buffer seeded with the flag prefix.
	m = newModel()
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if m2 := next.(Model); m2.editMode != editRaw || m2.editBuf.text != "--" {
		t.Errorf("E should open the raw hatch; mode=%d buf=%q", m2.editMode, m2.editBuf.text)
	}

	// Enter's Cmd updates the bead the edit opened on, not whatever a refresh drifted the
	// selection to (mirrors the launch pin).
	orig := updateBead
	defer func() { updateBead = orig }()
	var gotID string
	updateBead = func(id string, _ []string) error { gotID = id; return nil }
	for _, open := range []string{"E", "e"} {
		gotID = ""
		m = newModel()
		next, _ = m.Update(key(open))
		if open == "e" {
			next, _ = next.(Model).Update(key("t"))
		}
		m = next.(Model)
		m.editBuf = lineAt("--title x")
		m.selGraph = "chart1" // drifted
		if !strings.Contains(m.helpLine(), "grl1") {
			t.Errorf("%s: helpLine = %q, want the pinned grl1", open, m.helpLine())
		}
		if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
			cmd()
		}
		if gotID != "grl1" {
			t.Errorf("%s: enter updated %q, want the pinned grl1", open, gotID)
		}
	}
	// A refresh that drops the pinned bead cancels rather than editing a stub.
	gotID = ""
	m = newModel()
	next, _ = m.Update(key("E"))
	m = next.(Model)
	m.editBuf = lineAt("--title x")
	m.beads = m.beads[:1]
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if gotID != "" || next.(Model).status != "edit cancelled: grl1 no longer available" {
		t.Errorf("dropped bead: updated %q status %q, want cancelled", gotID, next.(Model).status)
	}
}

// The 2-field Signal create form: n on the Inbox opens the form, tab
// switches fields, text edits the focused field, and enter files a Signal via
// bd.NewSignal built from the two field values then refreshes via m.load(). The
// bd call is faked through the newSignal seam so no live bead is created. Also
// locks the empty-title guard.
func TestInboxCreateSignalForm(t *testing.T) {
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	typeStr := func(m Model, s string) Model {
		for _, r := range s {
			next, _ := m.Update(key(string(r)))
			m = next.(Model)
		}
		return m
	}

	orig := newSignal
	defer func() { newSignal = orig }()
	var gotTitle, gotDesc string
	var called int
	newSignal = func(title, desc string) (string, error) {
		gotTitle, gotDesc, called = title, desc, called+1
		return "sig-new", nil
	}

	// n on the Inbox opens the 2-field form.
	m := New(nil, nil)
	m.tab = tabInbox
	next, _ := m.Update(key("n"))
	m = next.(Model)
	if m.editMode != editCreate {
		t.Fatalf("n on Inbox should open the create form, editMode=%d", m.editMode)
	}

	// Type the title with an embedded space (bubbletea delivers space as KeySpace,
	// not a rune), tab to the description field, type that.
	m = typeStr(m, "Fix")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = typeStr(next.(Model), "it")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeStr(next.(Model), "det")

	// Enter submits in a Cmd: NewSignal(title, description) built from the two
	// fields, then its done message closes the form and fires a refresh so the new
	// bead appears.
	m, cmd := settleWrite(m.Update(tea.KeyMsg{Type: tea.KeyEnter}))
	if called != 1 || gotTitle != "Fix it" || gotDesc != "det" {
		t.Fatalf("submit should file NewSignal(\"Fix it\",\"det\") once, got n=%d (%q,%q)", called, gotTitle, gotDesc)
	}
	if m.editMode != editNone {
		t.Errorf("submit should close the form, editMode=%d", m.editMode)
	}
	if cmd == nil {
		t.Error("submit should refresh via m.load()")
	}

	// An empty (whitespace-only) title is rejected: no Signal filed, form stays open.
	called = 0
	me := New(nil, nil)
	me.tab = tabInbox
	next, _ = me.Update(key("n"))
	me = typeStr(next.(Model), "   ")
	next, _ = me.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if called != 0 {
		t.Errorf("empty title must not file a Signal (called=%d)", called)
	}
	if next.(Model).editMode != editCreate {
		t.Errorf("empty-title submit should keep the form open, editMode=%d", next.(Model).editMode)
	}
}

func TestTokenizeArgs(t *testing.T) {
	cases := map[string][]string{
		`--priority 1`:            {"--priority", "1"},
		`--status closed`:         {"--status", "closed"},
		`--reason "not doing it"`: {"--reason", "not doing it"},
		`--title 'a b'`:           {"--title", "a b"},
		`--assignee ""`:           {"--assignee", ""},
		``:                        nil,
		`   `:                     nil,
	}
	for in, want := range cases {
		got := tokenizeArgs(in)
		if len(got) != len(want) {
			t.Errorf("tokenizeArgs(%q) = %#v, want %#v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("tokenizeArgs(%q) = %#v, want %#v", in, got, want)
				break
			}
		}
	}
}

// launchFor routes a bead to its registry owner's Launch; an unowned kind is a
// loud error, not a silent no-op (spec §9.3).
type recordCrew struct{ launched contract.Bead }

func (r *recordCrew) Run(contract.Ctx, contract.Bead) error { return nil }
func (r *recordCrew) Launch(_ contract.Ctx, b contract.Bead) (contract.LaunchCommand, error) {
	r.launched = b
	return contract.AgentCommand{Argv: []string{"claude", "go"}}, nil
}

// run-now (t) on a ready AFK research leaf claims the bead BEFORE the background
// dispatch (Spec FR16), then runs the SAME shared entry point the loop
// calls, with a buffer writer so the alt-screen is never touched (FR7/FR8/FR19).
// Driven through the readyFrontier/claimBead/runDispatch seams — no live bd, no
// real reconcile. Completion (runDoneMsg) sets status and triggers refresh (FR10/11).
func TestRunNowClaimsThenDispatchesLeaf(t *testing.T) {
	origReady, origClaim, origDisp, origIP := readyFrontier, claimBead, runDispatch, inProgressBeads
	defer func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads = origReady, origClaim, origDisp, origIP
	}()

	r := bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "chart1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{r}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil } // pool empty → under the AFK pool

	var order []string
	claimBead = func(b bd.Bead) error { order = append(order, "claim:"+b.ID); return nil }
	var dispBead bd.Bead
	var dispOut io.Writer
	runDispatch = func(_ contract.Ctx, _ contract.Crewmate, b bd.Bead, out io.Writer, _ string, _ dispatch.InflightFunc) (dispatch.Outcome, error) {
		order = append(order, "run:"+b.ID)
		dispBead, dispOut = b, out
		return dispatch.Outcome{}, nil
	}

	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.beads = []bd.Bead{r}
	m.selGraph = r.ID

	next, cmd := settleRunNow(m.runNow())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("eligible run-now must return a background dispatch cmd")
	}
	if !strings.Contains(m.status, "dispatching") {
		t.Errorf("status = %q, want a dispatching hint", m.status)
	}
	// The tea.Cmd runs the effort in the background; execute it for the done msg.
	msg := cmd()
	done, ok := msg.(runDoneMsg)
	if !ok {
		t.Fatalf("run-now cmd must yield runDoneMsg, got %T", msg)
	}
	if dispBead.ID != "r1" {
		t.Errorf("dispatch got bead %q, want r1", dispBead.ID)
	}
	if dispOut == nil {
		t.Error("dispatch must receive a capture buffer, not a nil writer (FR8)")
	}
	// Claim must precede the run — the loop's claim-before-goroutine invariant (FR16).
	if len(order) != 2 || order[0] != "claim:r1" || order[1] != "run:r1" {
		t.Errorf("op order = %v, want [claim:r1 run:r1]", order)
	}
	// Completion sets a done status and refreshes the graph (FR10/FR11).
	next, refresh := m.Update(done)
	m = next.(Model)
	if refresh == nil {
		t.Error("runDoneMsg must trigger a refresh (m.load)")
	}
	if !strings.Contains(m.status, "done") {
		t.Errorf("completion status = %q, want done", m.status)
	}
}

// ADR 0069: a run-now that hits *MainDirty tells the human what to do in the
// status line, whether the move of main ran in-process (a Signal's anchor, a
// Rollback's revert: the typed error) or in a worktree reconcile child (a Chart or
// Checkpoint land: the child's last stderr line rides in the tail).
func TestRunNowMainDirtyStatus(t *testing.T) {
	dirty := &git.MainDirty{Path: "/repo"}
	for name, msg := range map[string]runDoneMsg{
		"in-process": {id: "ra", run: "ea", err: dirty},
		"child":      {id: "ra", run: "ea", err: errors.New("exit status 1"), tail: dirty.Error()},
	} {
		next, _ := withRuns("A").Update(msg)
		if s := next.(Model).helpLine(); !strings.Contains(s, "clean main, then re-run") {
			t.Errorf("%s: status line %q, want it to say clean main, then re-run", name, s)
		}
	}
}

// t is a no-op with a hint on any bead the loop would NOT dispatch this tick
// (Spec FR5): a HITL bead (not an AFK leaf), an AFK leaf that has dropped
// out of the ready frontier, or a container. None claims or dispatches.
func TestRunNowNoOpOnIneligible(t *testing.T) {
	origReady, origClaim, origDisp := readyFrontier, claimBead, runDispatch
	defer func() { readyFrontier, claimBead, runDispatch = origReady, origClaim, origDisp }()

	var claimed, dispatched bool
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		dispatched = true
		return dispatch.Outcome{}, nil
	}

	grl := bd.Bead{ID: "g1", Kind: contract.KindGrilling, Status: "open"}
	leaf := bd.Bead{ID: "r2", Kind: contract.KindResearch, Status: "open"}
	// A Draft Chart (still being charted) is excluded with no bd I/O — bd ready hides
	// Drafts, so it is never in the frontier (ADR 0059 §7).
	chart := bd.Bead{ID: "c1", Kind: contract.KindChart, Container: true, Status: "deferred", Description: contract.ChartPlaceholderBody}
	cases := []struct {
		name     string
		bead     bd.Bead
		ready    []bd.Bead // the frontier readyFrontier returns
		wantHint string    // the per-category no-op hint (Spec FR4)
	}{
		{"HITL bead in frontier is not an AFK leaf", grl, []bd.Bead{grl}, "HITL bead"},
		{"AFK leaf dropped from the frontier", leaf, nil, "blocked / not ready"},
		{"Draft Chart is not in the frontier", chart, nil, "blocked / not ready"},
	}
	for _, c := range cases {
		claimed, dispatched = false, false
		ready := c.ready
		readyFrontier = func() ([]bd.Bead, error) { return ready, nil }

		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.beads = []bd.Bead{c.bead}
		m.selGraph = c.bead.ID

		next, cmd := settleRunNow(m.runNow())
		m = next.(Model)
		if cmd != nil {
			t.Errorf("%s: ineligible run-now must not dispatch", c.name)
		}
		if claimed {
			t.Errorf("%s: ineligible run-now must not claim", c.name)
		}
		if dispatched {
			t.Errorf("%s: ineligible run-now must not run", c.name)
		}
		if !strings.Contains(m.status, c.wantHint) {
			t.Errorf("%s: status = %q, want per-category hint %q", c.name, m.status, c.wantHint)
		}
	}
}

// runNowHint gives one explanatory no-op message per ineligible row class (Spec
// FR4), mirroring launch's non-HITL guard: a closed bead, an open HITL
// bead (launch it), a not-drained Chart, a Draft Chart, and a
// blocked/not-ready bead all get a distinct, actionable hint — never a bare
// "not eligible". The press-time gate has already refused; this only classifies.
func TestRunNowIneligibleHints(t *testing.T) {
	frontier := func(b bd.Bead) []bd.Bead { return []bd.Bead{b} }
	closedLeaf := bd.Bead{ID: "r0", Kind: contract.KindResearch, Status: "closed"}
	hitl := bd.Bead{ID: "g1", Kind: contract.KindGrilling, Status: "open"}
	notDrained := bd.Bead{ID: "c1", Kind: contract.KindChart, Container: true, Status: "open", Description: "Destination: ship it"}
	draftChart := bd.Bead{ID: "c2", Kind: contract.KindChart, Container: true, Status: "deferred", Description: contract.ChartPlaceholderBody}
	blockedLeaf := bd.Bead{ID: "r1", Kind: contract.KindResearch, Status: "open"}
	checkpoint := bd.Bead{ID: "cp1", Kind: contract.KindCheckpoint, Container: true, Status: "open"}
	other := bd.Bead{ID: "u1", Kind: "unregistered", Status: "open"}
	cases := []struct {
		name  string
		bead  bd.Bead
		ready []bd.Bead
		want  string
	}{
		{"closed bead", closedLeaf, nil, "closed"},
		{"open HITL bead", hitl, frontier(hitl), "HITL bead"},
		{"charted Chart still has open children", notDrained, frontier(notDrained), "not drained"},
		{"charted Chart but blocked", notDrained, nil, "blocked / not ready"},
		{"Draft Chart (hidden from bd ready)", draftChart, nil, "blocked / not ready"},
		{"blocked / not-ready leaf", blockedLeaf, nil, "blocked / not ready"},
		{"checkpoint not ready to land", checkpoint, frontier(checkpoint), "Checkpoint not ready"},
		{"no run-now arm (fallback names every class, Rollback included)", other, frontier(other), "Rollback"},
	}
	for _, c := range cases {
		if got := runNowHint(c.bead, c.ready, "", nil); !strings.Contains(got, c.want) {
			t.Errorf("%s: runNowHint = %q, want substring %q", c.name, got, c.want)
		}
	}
}

func hasKey(ks []footerKey, k string) bool {
	for _, x := range ks {
		if x.k == k {
			return true
		}
	}
	return false
}

// The detail pane / footer reflects run-now eligibility from the already-loaded
// graph (Spec FR6), an I/O-free hint that may be one refresh stale: an open
// AFK leaf and a charted, drained Chart (every child closed) show the t affordance;
// a blocked/in-progress leaf, a charted Chart with an open child, a Draft Chart,
// a HITL bead and a closed bead do not. When affordable,
// the footer carries a t key and the launch preview augments the "runs via the
// loop" note.
func TestRunNowAffordance(t *testing.T) {
	drainedChart := bd.Bead{ID: "c1", Kind: contract.KindChart, Container: true, Status: "open", Description: "Destination: ship it"}
	openChart := bd.Bead{ID: "c2", Kind: contract.KindChart, Container: true, Status: "open", Description: "Destination: ship it"}
	cases := []struct {
		name  string
		bead  bd.Bead
		extra []bd.Bead // sibling beads (e.g. Chart children) in the model
		want  bool
	}{
		{"open AFK research leaf", bd.Bead{ID: "r1", Kind: contract.KindResearch, Status: "open"}, nil, true},
		{"in-progress leaf (already running)", bd.Bead{ID: "r2", Kind: contract.KindResearch, Status: "in_progress"}, nil, false},
		{"blocked leaf", bd.Bead{ID: "r3", Kind: contract.KindResearch, Status: "blocked"}, nil, false},
		{"charted, drained Chart (not-defogged)", drainedChart, []bd.Bead{{ID: "k1", Kind: contract.KindResearch, Parent: "c1", Status: "closed"}}, true},
		{"charted Chart with an open child", openChart, []bd.Bead{{ID: "k2", Kind: contract.KindResearch, Parent: "c2", Status: "open"}}, false},
		{"Draft Chart (not yet published)", bd.Bead{ID: "c3", Kind: contract.KindChart, Container: true, Status: "deferred", Description: contract.ChartPlaceholderBody}, nil, false},
		{"HITL bead", bd.Bead{ID: "g1", Kind: contract.KindGrilling, Status: "open"}, nil, false},
		{"closed leaf", bd.Bead{ID: "r4", Kind: contract.KindResearch, Status: "closed"}, nil, false},
	}
	for _, c := range cases {
		m := New(nil, nil)
		m.beads = append([]bd.Bead{c.bead}, c.extra...)
		b := &m.beads[0]
		if got := m.runNowAffordable(b); got != c.want {
			t.Errorf("%s: runNowAffordable = %v, want %v", c.name, got, c.want)
		}
		if c.want {
			if !hasKey(m.beadKeys(b), "t") {
				t.Errorf("%s: footer must offer t when affordable", c.name)
			}
			if !strings.Contains(m.launchPreview(b), "run it now") && !strings.Contains(m.launchPreview(b), "reconcile now") {
				t.Errorf("%s: launch preview must augment the note with the run-now affordance", c.name)
			}
		} else if hasKey(m.beadKeys(b), "t") {
			t.Errorf("%s: footer must NOT offer t when not affordable", c.name)
		}
	}
}

// The checkpoint run-now arm mirrors the loop's IsCheckpointCandidate branch:
// runNowAffordable's I/O-free approximation (checkpointLooksLandable) surfaces the t
// affordance for an open Checkpoint with a closed Approval and no open conflict/critique
// child, and withholds it otherwise. The git approvedAtHead leg is press-time only, so
// this is a hint, not the authoritative gate. When affordable, the footer offers
// "t land" and the launch preview says the Checkpoint lands via Jinbe.
func TestRunNowCheckpointArm(t *testing.T) {
	cp := func(id string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Container: true, Status: "open"}
	}
	cases := []struct {
		name  string
		bead  bd.Bead
		extra []bd.Bead
		want  bool
	}{
		{"approved & drained checkpoint", cp("cp1"),
			[]bd.Bead{{ID: "a1", Kind: contract.KindApproval, Parent: "cp1", Status: "closed"}}, true},
		{"no closed approval yet", cp("cp2"),
			[]bd.Bead{{ID: "a2", Kind: contract.KindApproval, Parent: "cp2", Status: "open"}}, false},
		{"open critique undrains it", cp("cp3"), []bd.Bead{
			{ID: "a3", Kind: contract.KindApproval, Parent: "cp3", Status: "closed"},
			{ID: "cr3", Kind: contract.KindCritique, Parent: "cp3", Status: "open"},
		}, false},
		{"open conflict undrains it", cp("cp4"), []bd.Bead{
			{ID: "a4", Kind: contract.KindApproval, Parent: "cp4", Status: "closed"},
			{ID: "cf4", Kind: contract.KindConflict, Parent: "cp4", Status: "open"},
		}, false},
	}
	for _, c := range cases {
		m := New(nil, nil)
		m.beads = append([]bd.Bead{c.bead}, c.extra...)
		b := &m.beads[0]
		if got := m.runNowAffordable(b); got != c.want {
			t.Errorf("%s: runNowAffordable = %v, want %v", c.name, got, c.want)
		}
		if c.want {
			if !hasKey(m.beadKeys(b), "t") {
				t.Errorf("%s: footer must offer t when landable", c.name)
			}
			if !strings.Contains(m.launchPreview(b), "land it now") {
				t.Errorf("%s: launch preview must say the Checkpoint lands via Jinbe", c.name)
			}
		} else if hasKey(m.beadKeys(b), "t") {
			t.Errorf("%s: footer must NOT offer t when not landable", c.name)
		}
	}
}

// t on an approved Checkpoint an open Task holds (Spec FR 45, D6): bd
// ready hides it, so t reads the loop's widened frontier (loop.Readmit) to reach
// it, then refuses naming the blocker in the preview's words; once the Task
// closes, the same t lands it.
func TestRunNowHeldCheckpointNamesBlockerThenLands(t *testing.T) {
	origReady, origOpen, origLand, origClaim, origDisp := readyFrontier, openBeads, checkpointLandHold, claimBead, runDispatch
	defer func() {
		readyFrontier, openBeads, checkpointLandHold, claimBead, runDispatch = origReady, origOpen, origLand, origClaim, origDisp
	}()
	task := bd.Bead{ID: "tk1", Kind: contract.KindTask, Status: "open", Title: "Rotate the key"}
	cp := bd.Bead{ID: "cp1", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: []string{"tk1"}}
	readyFrontier = func() ([]bd.Bead, error) {
		if task.Status == "open" {
			return []bd.Bead{task}, nil // bd ready hides the blocked Checkpoint
		}
		return []bd.Bead{cp}, nil
	}
	openBeads = func() ([]bd.Bead, error) { return []bd.Bead{task, cp}, nil }
	checkpointLandHold = func(bd.Bead) (bool, []bd.Bead, error) { // approved at HEAD and drained
		if task.Status == "open" {
			return false, []bd.Bead{task}, nil
		}
		return true, nil, nil
	}
	var claimed, dispatched bool
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		dispatched = true
		return dispatch.Outcome{}, nil
	}
	m := New(nil, map[string]contract.Crewmate{"jinbe": &recordCrew{}})
	m.beads = []bd.Bead{cp, task}
	m.selGraph = cp.ID

	next, cmd := settleRunNow(m.runNow())
	if want := "cp1: approved; land held by open blocker: Rotate the key"; next.(Model).status != want || cmd != nil || claimed {
		t.Errorf("held t: status = %q, cmd %v, claimed %v; want refusal %q", next.(Model).status, cmd != nil, claimed, want)
	}

	task.Status = "closed"
	m.beads = []bd.Bead{cp, task}
	_, cmd = settleRunNow(m.runNow())
	if cmd == nil {
		t.Fatal("t must land the Checkpoint once its blocker closes")
	}
	cmd()
	if !claimed || !dispatched {
		t.Errorf("released t: claimed %v, dispatched %v; want both", claimed, dispatched)
	}
}

// run-now on a drained defogged Chart (chart:defogged, 0 open children) dispatches
// its speccing reconcile through the same shared entry point (Spec FR3),
// mirroring the loop's candidates() drained-Chart arm. A defogged Chart with open
// children is NOT drained, so it is a no-op — the strict-parity gate the loop
// applies before it counts the Chart a candidate (FR5).
func TestRunNowDrainedChartArm(t *testing.T) {
	origReady, origClaim, origDisp, origKids, origIP := readyFrontier, claimBead, runDispatch, drainBlockers, inProgressBeads
	defer func() {
		readyFrontier, claimBead, runDispatch, drainBlockers, inProgressBeads = origReady, origClaim, origDisp, origKids, origIP
	}()

	chart := bd.Bead{ID: "c1", Kind: contract.KindChart, Container: true, Status: "open", Labels: []string{"chart:defogged"}}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{chart}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil } // pool empty → under the AFK pool

	cases := []struct {
		name    string
		open    int  // OpenChildren for the Chart
		wantRun bool // whether the Chart should dispatch
	}{
		{"drained defogged Chart dispatches", 0, true},
		{"defogged Chart with open children is not drained", 2, false},
	}
	for _, c := range cases {
		var claimed, dispatched bool
		claimBead = func(bd.Bead) error { claimed = true; return nil }
		runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
			dispatched = true
			return dispatch.Outcome{}, nil
		}
		open := c.open
		drainBlockers = func(string) (int, []bd.Bead, error) { return open, nil, nil }

		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.beads = []bd.Bead{chart}
		m.selGraph = chart.ID

		next, cmd := settleRunNow(m.runNow())
		m = next.(Model)
		if c.wantRun {
			if cmd == nil {
				t.Errorf("%s: must return a background dispatch cmd", c.name)
			}
			if cmd != nil {
				cmd() // execute so the goroutine seam records the dispatch
			}
			if !claimed {
				t.Errorf("%s: must claim the Chart", c.name)
			}
			if !dispatched {
				t.Errorf("%s: must dispatch the Chart's effort", c.name)
			}
		} else {
			if cmd != nil || claimed || dispatched {
				t.Errorf("%s: non-drained Chart must be a no-op", c.name)
			}
			if !strings.Contains(m.status, "not drained") {
				t.Errorf("%s: status = %q, want a not-drained hint", c.name, m.status)
			}
		}
	}
}

// A closed child still holding effort/<child> undrains its Chart (Spec
// D7, FRs 45/46): the refresh carries the effort refs, so the footer never offers
// t spec while the press gate would refuse, and that refusal names the bead with
// both ways out.
func TestHeldChildWithholdsSpecAndTheRefusalNamesIt(t *testing.T) {
	origReady, origClaim, origDrain := readyFrontier, claimBead, drainBlockers
	defer func() { readyFrontier, claimBead, drainBlockers = origReady, origClaim, origDrain }()

	chart := bd.Bead{ID: "c1", Kind: contract.KindChart, Container: true, Status: "open", Description: "Destination: ship it"}
	kid := bd.Bead{ID: "k1", Kind: contract.KindResearch, Parent: "c1", Status: "closed"}
	next, _ := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}}).Update(loadedMsg{beads: []bd.Bead{chart, kid}, efforts: map[string]bool{"k1": true}})
	m := next.(Model)
	if l := m.runNowLabel(&m.beads[0]); l != "" {
		t.Errorf("runNowLabel = %q, want none: k1 holds effort/k1", l)
	}

	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{chart}, nil }
	claimBead = func(bd.Bead) error { t.Error("a held Chart must not be claimed"); return nil }
	drainBlockers = func(string) (int, []bd.Bead, error) { return 0, []bd.Bead{kid}, nil }
	m.selGraph = chart.ID
	next, cmd := settleRunNow(m.runNow())
	if cmd != nil {
		t.Error("a held Chart must not dispatch")
	}
	for _, want := range []string{"not drained", "k1", "mugicrew integrate", "git branch -D effort/k1"} {
		if st := next.(Model).status; !strings.Contains(st, want) {
			t.Errorf("status = %q, want it to name %q", st, want)
		}
	}
}

// run-now re-derives the AFK pool's in-flight total from bd (loop.CountInflight) and
// honors the pool BEFORE claiming (Spec FR9/FR10): a full pool refuses with a
// status naming the pool and no claim/dispatch, whatever the in-flight efforts' owners;
// under the pool it runs, and a HITL inflight bead (a human working a grilling) takes
// no slot. The cross-process check-then-claim may overshoot by one (ADR 0070 §4) —
// documented, not tested.
func TestRunNowRefusesWhenPoolFull(t *testing.T) {
	origReady, origClaim, origDisp, origIP, origRoot := readyFrontier, claimBead, runDispatch, inProgressBeads, git.RepoRoot
	defer func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads, git.RepoRoot = origReady, origClaim, origDisp, origIP, origRoot
	}()
	root := t.TempDir() // no .mugicrew/config.toml: the default pool of 3
	git.RepoRoot = func() (string, error) { return root, nil }

	r := bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "chart1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{r}, nil }

	var claimed, dispatched bool
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		dispatched = true
		return dispatch.Outcome{}, nil
	}

	// 3 AFK efforts in flight, none nami's → the pool (3) is full.
	inProgressBeads = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "f0", Kind: contract.KindBuild, Status: "in_progress"},
			{ID: "f1", Kind: contract.KindBuild, Status: "in_progress"},
			{ID: "s0", Kind: contract.KindSpec, Status: "in_progress"},
		}, nil
	}

	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.beads = []bd.Bead{r}
	m.selGraph = r.ID

	next, cmd := settleRunNow(m.runNow())
	m = next.(Model)
	if cmd != nil || claimed || dispatched {
		t.Errorf("run-now with the pool full must not claim or dispatch (cmd=%v claimed=%v dispatched=%v)", cmd != nil, claimed, dispatched)
	}
	if !strings.Contains(m.status, "AFK pool full (3/3)") {
		t.Errorf("status = %q, want it to name the pool: AFK pool full (3/3)", m.status)
	}

	// Under the pool, regardless of owner: two nami AFK efforts plus a HITL grilling
	// (no slot) leave one slot, so the same nami run-now is admitted.
	inProgressBeads = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "r0", Kind: contract.KindResearch, Status: "in_progress"},
			{ID: "c0", Kind: contract.KindChart, Status: "in_progress"},
			{ID: "g0", Kind: contract.KindGrilling, Status: "in_progress"},
		}, nil
	}
	claimed, dispatched = false, false
	next, cmd = settleRunNow(m.runNow())
	m = next.(Model)
	if cmd == nil || !claimed {
		t.Errorf("under the pool run-now must claim and dispatch (cmd=%v claimed=%v); status %q", cmd != nil, claimed, m.status)
	}
}

// run-now honors the configured pool (Spec FR2/FR4): concurrency in the
// Workspace's .mugicrew/config.toml, re-read on every press, sets N.
func TestRunNowHonorsConfiguredPool(t *testing.T) {
	origReady, origClaim, origDisp, origIP, origRoot := readyFrontier, claimBead, runDispatch, inProgressBeads, git.RepoRoot
	defer func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads, git.RepoRoot = origReady, origClaim, origDisp, origIP, origRoot
	}()
	root := t.TempDir()
	git.RepoRoot = func() (string, error) { return root, nil }
	if err := os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	setPool := func(body string) {
		if err := os.WriteFile(filepath.Join(root, ".mugicrew", "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "chart1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{r}, nil }
	var claimed bool
	claimBead = func(bd.Bead) error { claimed = true; return nil }
	runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		return dispatch.Outcome{}, nil
	}
	inProgressBeads = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "f0", Kind: contract.KindBuild, Status: "in_progress"},
			{ID: "f1", Kind: contract.KindBuild, Status: "in_progress"},
		}, nil
	}
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.beads = []bd.Bead{r}
	m.selGraph = r.ID

	setPool("concurrency = 2\n")
	next, _ := settleRunNow(m.runNow())
	m = next.(Model)
	if claimed || !strings.Contains(m.status, "AFK pool full (2/2)") {
		t.Errorf("pool 2 with 2 in flight: claimed=%v status=%q, want a refusal naming (2/2)", claimed, m.status)
	}

	setPool("concurrency = 4\n")
	next, _ = settleRunNow(m.runNow())
	m = next.(Model)
	if !claimed {
		t.Errorf("pool raised to 4 with 2 in flight must claim on the next press; status %q", m.status)
	}
}

// A failed run surfaces the captured output tail in the status for a no-log-file
// diagnosis (Spec FR12); a successful run reports done with no tail. Both
// refresh the graph.
func TestRunDoneMsgStatusAndTail(t *testing.T) {
	next, cmd := New(nil, nil).Update(runDoneMsg{id: "r1", err: errors.New("boom"), tail: "line8\nline9"})
	m := next.(Model)
	if cmd == nil {
		t.Error("a failed run must still refresh the graph")
	}
	if !strings.Contains(m.status, "failed") || !strings.Contains(m.status, "line9") {
		t.Errorf("failure status = %q, want the error + tail", m.status)
	}
	next, _ = New(nil, nil).Update(runDoneMsg{id: "r1"})
	if s := next.(Model).status; !strings.Contains(s, "done") {
		t.Errorf("success status = %q, want done", s)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Errorf("lastLines(4 lines, 2) = %q, want \"c\\nd\"", got)
	}
	if got := lastLines("only", 5); got != "only" {
		t.Errorf("lastLines(short) = %q, want \"only\"", got)
	}
}

// settleLaunch runs an in-flight launch's Cmd and folds its launchDoneMsg back through
// Update, as the runtime would (Spec FR9); with nothing in flight it
// passes the result through untouched.
func settleLaunch(next tea.Model, cmd tea.Cmd) (Model, tea.Cmd) {
	m := next.(Model)
	if len(m.launching) == 0 || cmd == nil {
		return m, cmd
	}
	msg := cmd()
	if _, ok := msg.(launchDoneMsg); !ok {
		return m, func() tea.Msg { return msg }
	}
	next, cmd = m.Update(msg)
	return next.(Model), cmd
}

// settleWrite runs a pending background write's Cmd and feeds its writeDoneMsg
// back, returning the done closure's follow-on Cmd (a reload or a launch).
func settleWrite(next tea.Model, cmd tea.Cmd) (Model, tea.Cmd) {
	m := next.(Model)
	if len(m.pending) == 0 || cmd == nil {
		return m, cmd
	}
	next, cmd = m.Update(cmd())
	return next.(Model), cmd
}

// settleRunNow is settleWrite for t: run-now's reads and claim run in the Cmd, so
// the press only settles once its writeDoneMsg is fed back.
func settleRunNow(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) { return settleWrite(next, cmd) }

// launch's contention interlock: when startSession (session.Start →
// git.NewWorktree) fails with git's exit-128 refusal (the container branch is
// already checked out), the TUI shows a busy status and does NOT exec — no crash,
// no corrupted tree. Faked with a real exit-128 error via the startSession seam.
func TestLaunchShowsBusyOnExit128(t *testing.T) {
	origStart, origH := startSession, git.WorktreeOn
	defer func() { startSession, git.WorktreeOn = origStart, origH }()
	startSession = func(bd.Bead, contract.AgentCommand) error {
		return fmt.Errorf("git worktree add: %w: fatal: invalid reference: chart/chart1", exec.Command("sh", "-c", "exit 128").Run())
	}
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{Path: "/home/me/repo-feature"}, true, nil }

	grl := bd.Bead{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1"}
	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.beads = []bd.Bead{grl}
	m.selGraph = grl.ID

	m, teaCmd := settleLaunch(m.launch())
	if teaCmd != nil {
		t.Error("a busy launch must not exec a session")
	}
	if !strings.Contains(m.status, "busy") {
		t.Errorf("status = %q, want a busy status", m.status)
	}

	// No listed holder: not busy (ADR 0056) — the launch fails with git's error.
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{}, false, nil }
	m, teaCmd = settleLaunch(m.launch())
	if teaCmd != nil || strings.Contains(m.status, "busy") || !strings.HasPrefix(m.status, "launch: ") || !strings.Contains(m.status, "invalid reference") {
		t.Errorf("holderless refusal: status = %q, want launch: <git error>", m.status)
	}
}

func TestLaunchForRoutesToOwner(t *testing.T) {
	rc := &recordCrew{}
	crew := map[string]contract.Crewmate{"nami": rc}

	// A Signal routes to its owner's Launch like every other HITL kind (ADR 0039
	// §6) — no detour through the `mugicrew signal triage` verb.
	if _, err := launchFor(nil, crew, bd.Bead{ID: "sig1", Kind: contract.KindSignal}); err != nil {
		t.Fatalf("signal should route to nami: %v", err)
	}
	if rc.launched.ID != "sig1" {
		t.Errorf("nami.Launch got %q, want sig1", rc.launched.ID)
	}
	// A non-signal HITL kind routes to its registry owner's Launch.
	if _, err := launchFor(nil, crew, bd.Bead{ID: "grl1", Kind: contract.KindGrilling}); err != nil {
		t.Fatalf("grilling should route to nami: %v", err)
	}
	if rc.launched.ID != "grl1" {
		t.Errorf("nami.Launch got %q, want grl1", rc.launched.ID)
	}
	// kind:spec is a known-but-unowned kind → registry error surfaces.
	if _, err := launchFor(nil, crew, bd.Bead{ID: "s1", Kind: contract.KindSpec}); err == nil {
		t.Errorf("unowned kind should error, got nil")
	}
}

// A TUI Signal launch goes through the real Nami Launch and the claude harness,
// so it comes back carrying the pre-assigned session id hitl.launched records —
// the id the triage-verb detour never surfaced.
type harnessCtx struct{ h contract.Harness }

func (c harnessCtx) Harness() contract.Harness { return c.h }

func TestLaunchForSignalCarriesSessionID(t *testing.T) {
	crew := map[string]contract.Crewmate{"nami": nami.Nami{}}
	lc, err := launchFor(harnessCtx{claudecode.New("")}, crew, bd.Bead{ID: "sig1", Kind: contract.KindSignal})
	if err != nil {
		t.Fatalf("signal launch: %v", err)
	}
	ac, ok := lc.(contract.AgentCommand)
	if !ok || ac.SessionID == "" {
		t.Errorf("signal launch = %#v, want an AgentCommand with a SessionID", lc)
	}
}

// promptHarness records the prompt a Launch hands the harness.
type promptHarness struct {
	contract.Harness
	prompt string
}

func (h *promptHarness) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	h.prompt = inv.Prompt
	return contract.AgentCommand{Argv: []string{"claude", inv.Prompt}}, nil
}

// Every HITL crewmate's launched prompt carries the closing line exactly once,
// injected on the shared launch path (Spec D8). Zoro's pivot and Luffy's
// approval Launch read bd before reaching the harness, so they are covered by
// construction.
func TestLaunchForInjectsClosingLine(t *testing.T) {
	crew := map[string]contract.Crewmate{"nami": nami.Nami{}, "sanji": sanji.Sanji{}}
	for _, k := range []contract.Kind{contract.KindSignal, contract.KindGrilling, contract.KindPrototype} {
		h := &promptHarness{}
		if _, err := launchFor(harnessCtx{h}, crew, bd.Bead{ID: "mc-1", Kind: k, Title: "Fix the thing"}); err != nil {
			t.Fatalf("%s launch: %v", k, err)
		}
		if n := strings.Count(h.prompt, contract.ClosingLine()); n != 1 {
			t.Errorf("%s prompt carries the closing line %d times, want 1:\n%s", k, n, h.prompt)
		}
	}
}

// The ON LAUNCH preview of a HITL bead is static text naming the owner and kind
// (ADR 0101 §2): stable across renders, built with no Launch call; an unwired
// owner renders the ON LAUNCH: warning.
func TestLaunchPreviewStatic(t *testing.T) {
	cases := []struct {
		kind contract.Kind
		want string
	}{
		{contract.KindSignal, "ON LAUNCH — Nami opens an agent session for signal"},
		{contract.KindGrilling, "ON LAUNCH — Nami opens an agent session for grilling"},
		{contract.KindPrototype, "ON LAUNCH — Sanji opens an agent session for prototype"},
		{contract.KindPivot, "ON LAUNCH — Zoro opens an agent session for pivot"},
		{contract.KindApproval, "ON LAUNCH — Luffy opens an agent session for approval"},
		{contract.KindSurgery, "ON LAUNCH — Franky opens an agent session for surgery"},
		{contract.KindTask, "ON LAUNCH — Brook opens an agent session for task"},
	}
	crew := map[string]contract.Crewmate{}
	for _, o := range []string{"nami", "sanji", "zoro", "luffy", "franky", "brook"} {
		crew[o] = nil // wired; the preview never calls Launch
	}
	m := New(nil, crew)
	for _, c := range cases {
		b := &bd.Bead{ID: "h1", Kind: c.kind, Status: "open"}
		p1, p2 := ansi.Strip(m.launchPreview(b)), ansi.Strip(m.launchPreview(b))
		if p1 != c.want || p2 != p1 {
			t.Errorf("%s preview = %q then %q, want %q twice", c.kind, p1, p2, c.want)
		}
	}
	// An approved Checkpoint an open blocker holds keeps ⊘ and names the blocker
	// instead of offering t (Spec FR 44); several are comma-joined.
	for _, c := range []struct {
		blockers []bd.Bead
		want     string
	}{
		{[]bd.Bead{{ID: "tk1", Kind: contract.KindTask, Status: "open", Title: "Rotate the key"}},
			"CHECKPOINT — approved; land held by open blocker: Rotate the key. Use edit to intervene."},
		{[]bd.Bead{
			{ID: "tk1", Kind: contract.KindTask, Status: "open", Title: "Rotate the key"},
			{ID: "tk2", Kind: contract.KindTask, Status: "in_progress", Title: "Restart the loop"},
			{ID: "tk3", Kind: contract.KindTask, Status: "closed", Title: "Done already"},
		}, "CHECKPOINT — approved; land held by open blocker: Rotate the key, Restart the loop. Use edit to intervene."},
	} {
		held := New(nil, crew)
		cp := bd.Bead{ID: "cp1", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: bd.IDs(c.blockers)}
		held.beads = append([]bd.Bead{cp, {ID: "ap1", Kind: contract.KindApproval, Parent: "cp1", Status: "closed"}}, c.blockers...)
		b := &held.beads[0]
		if got := ansi.Strip(held.launchPreview(b)); got != c.want {
			t.Errorf("held-land preview = %q, want %q", got, c.want)
		}
		if ansi.Strip(held.stateGlyph(b)) != "⊘" || hasKey(held.beadKeys(b), "t") {
			t.Errorf("held Checkpoint must keep ⊘ and not offer t")
		}
	}
	unwired := New(nil, map[string]contract.Crewmate{})
	got := ansi.Strip(unwired.launchPreview(&bd.Bead{ID: "a1", Kind: contract.KindApproval, Status: "open"}))
	if want := `ON LAUNCH: no crewmate "luffy" wired for kind "approval"`; got != want {
		t.Errorf("unwired preview = %q, want %q", got, want)
	}
}

// launch starts the bead's session detached on tmux (session.Start gets the bead
// and the owner's agent command), then switches this terminal to it — start
// strictly first, no tea.ExecProcess suspending the TUI (ADR 0065 §1) — and
// reloads. No teardown and no TUI-written hitl.* events, which the in-pane
// hitl-run wrapper owns (ADR 0037 §6, ADR 0029 amendment). A switch failure is
// an attach: status.
func TestLaunchStartsThenSwitches(t *testing.T) {
	origStart, origSwitch, origList := startSession, switchSession, listAll
	defer func() { startSession, switchSession, listAll = origStart, origSwitch, origList }()
	listAll = func() ([]bd.Bead, error) { return nil, errors.New("reloaded") }
	var order []string
	var started bd.Bead
	var startedAC contract.AgentCommand
	startSession = func(b bd.Bead, ac contract.AgentCommand) error {
		started, startedAC = b, ac
		order = append(order, "start")
		return nil
	}
	var switchErr error
	switchSession = func(id string) error {
		order = append(order, "switch "+id)
		return switchErr
	}
	capt := eventlogtest.Capture(t)

	grl := bd.Bead{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1"}
	m := New(nil, map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}})
	m.beads = []bd.Bead{grl}
	m.selGraph = grl.ID

	next, teaCmd := settleLaunch(m.launch())
	if teaCmd == nil {
		t.Fatal("launch must reload")
	}
	if msg, isLoad := teaCmd().(errMsg); !isLoad || msg.err.Error() != "reloaded" {
		t.Error("launch's command must be the reload, not an ExecProcess")
	}
	if !slices.Equal(order, []string{"start", "switch grl1"}) {
		t.Errorf("order = %v, want start then switch grl1", order)
	}
	if started.ID != "grl1" || startedAC.SessionID != "sess-1" {
		t.Errorf("startSession got %q / %+v, want grl1 with the owner's command", started.ID, startedAC)
	}
	if st := next.status; st != "" || len(next.launching) != 0 {
		t.Errorf("status = %q launching = %v after a clean switch, want none", st, next.launching)
	}
	if len(*capt) != 0 {
		t.Errorf("TUI launch wrote events %v; hitl-run owns them", eventlogtest.Names(*capt))
	}

	switchErr = errors.New("no client is viewing this TUI")
	next, _ = settleLaunch(m.launch())
	if st := next.status; st != "attach: no client is viewing this TUI" {
		t.Errorf("status = %q, want the attach: switch error", st)
	}
}

// With tmux off PATH, l refuses with a status and launches nothing (ADR 0036 §6).
func TestLaunchRefusesWithoutTmux(t *testing.T) {
	origStart := startSession
	defer func() { startSession = origStart }()
	startSession = func(bd.Bead, contract.AgentCommand) error { return session.ErrNoTmux }

	grl := bd.Bead{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1"}
	m := New(nil, map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}})
	m.beads = []bd.Bead{grl}
	m.selGraph = grl.ID
	next, teaCmd := settleLaunch(m.launch())
	if teaCmd != nil {
		t.Error("no tmux must launch nothing")
	}
	if !strings.Contains(next.status, "tmux") {
		t.Errorf("status = %q, want the tmux refusal", next.status)
	}
}

type sessionLaunchCrew struct{ recordCrew }

func (sessionLaunchCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return contract.AgentCommand{Argv: []string{"claude", "go", "--session-id", "sess-1"}, SessionID: "sess-1"}, nil
}

// Liveness dot & detail line (ADR 0032 §5): a second glyph right after
// the unchanged status glyph — ● alive, ○ dead, · no verdict — plus a dim
// age-since-claim suffix on in_progress rows, and a liveness field in the detail.
// Glyphs and text only, never ANSI codes.
func TestLivenessDotAndDetailLine(t *testing.T) {
	now := time.Now()
	m := New(nil, nil)
	m.w, m.h = 100, 30
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: Liveness", Status: "open"},
		{ID: "slow", Kind: contract.KindResearch, Parent: "ch1", Status: "in_progress", Title: "Slow alive", Started: now.Add(-3 * time.Hour)},
		{ID: "crash", Kind: contract.KindResearch, Parent: "ch1", Status: "in_progress", Title: "Fresh dead", Started: now.Add(-4 * time.Minute)},
		{ID: "hitl", Kind: contract.KindGrilling, Parent: "ch1", Status: "in_progress", Title: "Human claim"},
		{ID: "idle", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "Not claimed"},
	}
	m.live = map[string]bool{"slow": true, "crash": false}
	m.rebuildGroups()

	line := func(id string) string {
		for _, l := range strings.Split(m.graphView(m.paneHeight()), "\n") {
			if strings.Contains(l, m.beadByID(id).Title) {
				return l
			}
		}
		t.Fatalf("no row for %s", id)
		return ""
	}
	for _, c := range []struct{ id, glyphs, suffix string }{
		{"slow", "◐●", "3h"},
		{"crash", "◐○", "4m"},
		{"hitl", "◐·", "hitl"},
		{"idle", "○·", ""},
	} {
		l := line(c.id)
		if !strings.Contains(l, c.glyphs+" ") {
			t.Errorf("%s: want %q right after the status glyph, got %q", c.id, c.glyphs, l)
		}
		if c.suffix != "" && !strings.Contains(l, m.beadByID(c.id).Title+"  "+c.suffix) {
			t.Errorf("%s: want trailing %q suffix, got %q", c.id, c.suffix, l)
		}
	}
	if _, after, _ := strings.Cut(line("idle"), "Not claimed"); strings.TrimSpace(strings.SplitN(after, "│", 2)[0]) != "" {
		t.Errorf("non-in_progress rows carry no age suffix, got %q", line("idle"))
	}

	for _, c := range []struct{ id, want string }{
		{"slow", "alive · claimed 3h ago"},
		{"crash", "dead — no reconcile/claude process · claimed 4m ago"},
		{"hitl", "— (HITL: no verdict)"},
	} {
		if d := m.detailView(m.beadByID(c.id), 0, 1, 30); !strings.Contains(d, "liveness") || !strings.Contains(d, c.want) {
			t.Errorf("%s detail: want liveness line %q, got:\n%s", c.id, c.want, d)
		}
	}

	// Narrow (Mobile-SSH) single pane: the row still fits the width.
	m.w = 40
	for _, l := range strings.Split(m.graphView(m.paneHeight())+"\n"+m.inboxView(m.paneHeight()), "\n") {
		if w := lipgloss.Width(l); w > 40 {
			t.Errorf("narrow row overflows 40 cols (%d): %q", w, l)
		}
	}
}

// The liveness map rides the existing refresh: load() runs the shared predicate
// (loop.IsActiveAFKEffort) and probe over the snapshot's in_progress AFK beads
// only, and hands the map to the model beside the beads — no separate timer.
func TestLoadProbesActiveAFKEfforts(t *testing.T) {
	origList, origProbe, origSess := listAll, probeLiveness, readSessions
	defer func() { listAll, probeLiveness, readSessions = origList, origProbe, origSess }()
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	listAll = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "afk", Kind: contract.KindBuild, Status: "in_progress"},
			{ID: "hitl", Kind: contract.KindGrilling, Status: "in_progress"},
			{ID: "open", Kind: contract.KindBuild, Status: "open"},
		}, nil
	}
	var asked []string
	probeLiveness = func(ids []string) map[string]bool { asked = ids; return map[string]bool{"afk": true} }

	msg := New(nil, nil).load()().(loadedMsg)
	if strings.Join(asked, ",") != "afk" {
		t.Errorf("probe asked about %v, want only the active AFK effort [afk]", asked)
	}
	m, _ := New(nil, nil).Update(msg)
	if !m.(Model).live["afk"] {
		t.Errorf("loaded liveness map not held on the Model: %v", m.(Model).live)
	}
}

// The done flip rides the refresh (ADR 0066 §4): a tick with the bead closed sets
// its alive session's bar loud, a later tick with it reopened sets it back, and an
// exited session gets nothing.
func TestLoadFlipsDoneBar(t *testing.T) {
	origList, origSess, origBars := listAll, readSessions, setBars
	defer func() { listAll, readSessions, setBars = origList, origSess, origBars }()
	readSessions = func() (map[string]session.Info, error) {
		return map[string]session.Info{"a": {State: session.Alive}, "x": {State: session.Exited}}, nil
	}
	var got map[string]bool
	setBars = func(c map[string]bool, _ map[string]session.Info) error { got = c; return nil }
	for _, status := range []string{"closed", "open"} {
		listAll = func() ([]bd.Bead, error) {
			return []bd.Bead{{ID: "a", Status: status}, {ID: "x", Status: "closed"}, {ID: "z", Status: "closed"}}, nil
		}
		New(nil, nil).load()()
		if want := map[string]bool{"a": status == "closed"}; !maps.Equal(got, want) {
			t.Errorf("bead %s: setBars(%v), want %v", status, got, want)
		}
	}
}

// blocked/openBlockers derive blocked-ness from the snapshot alone:
// only non-closed Ordering targets count, a closed bead is never blocked, and a
// target missing from the snapshot counts as open.
func TestBlockedAndOpenBlockers(t *testing.T) {
	m := New(nil, nil)
	m.beads = []bd.Bead{
		{ID: "open", Status: "open"},
		{ID: "wip", Status: "in_progress"},
		{ID: "done", Status: "closed"},
	}
	for _, c := range []struct {
		name        string
		b           bd.Bead
		want        []string
		wantBlocked bool
	}{
		{"blocker open", bd.Bead{Status: "open", BlockedBy: []string{"open"}}, []string{"open"}, true},
		{"blocker in_progress", bd.Bead{Status: "open", BlockedBy: []string{"wip"}}, []string{"wip"}, true},
		{"blocker closed", bd.Bead{Status: "open", BlockedBy: []string{"done"}}, nil, false},
		{"status=blocked, no edges", bd.Bead{Status: "blocked"}, nil, true},
		{"closed with open blocker", bd.Bead{Status: "closed", BlockedBy: []string{"open"}}, []string{"open"}, false},
		// non-Ordering edges never reach BlockedBy (parseBeads drops them); membership to an open bead doesn't block
		{"membership edge to open bead", bd.Bead{Status: "open", Parent: "open"}, nil, false},
		{"target missing from snapshot", bd.Bead{Status: "open", BlockedBy: []string{"gone"}}, []string{"gone"}, true},
		{"edge order kept, closed skipped", bd.Bead{Status: "open", BlockedBy: []string{"wip", "done", "open"}}, []string{"wip", "open"}, true},
	} {
		var got []string
		for _, t := range m.openBlockers(&c.b) {
			got = append(got, t.ID)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: openBlockers = %v, want %v", c.name, got, c.want)
		}
		if got := m.blocked(&c.b); got != c.wantBlocked {
			t.Errorf("%s: blocked = %v, want %v", c.name, got, c.wantBlocked)
		}
	}
}

// ⊘ replaces the state glyph wherever it renders — Graph rows, Container
// headers, Inbox rows, detail header — for dep-blocked and status=blocked beads,
// and never for closed ones.
func TestBlockedGlyphRendersEverywhere(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h = 120, 40
	m.hideClosedRoots = false
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: Gate", Status: "open", BlockedBy: []string{"ext"}},
		{ID: "ext", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "External blocker"},
		{ID: "dep", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "Dep blocked", BlockedBy: []string{"ext"}},
		{ID: "stb", Kind: contract.KindResearch, Parent: "ch1", Status: "blocked", Title: "Status blocked"},
		{ID: "cls", Kind: contract.KindResearch, Parent: "ch1", Status: "closed", Title: "Closed but edged", BlockedBy: []string{"ext"}},
		{ID: "grl", Kind: contract.KindGrilling, Parent: "ch1", Status: "open", Title: "Grill waits", BlockedBy: []string{"ext"}},
	}
	m.rebuildGroups()

	row := func(view, needle string) string {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, needle) {
				return l
			}
		}
		t.Fatalf("no line containing %q in:\n%s", needle, view)
		return ""
	}
	graph := m.graphView(m.paneHeight())
	for _, c := range []struct {
		needle string
		want   bool
	}{
		{"Gate", true}, // Container header
		{"Dep blocked", true},
		{"Status blocked", true},
		{"Grill waits", true},
		{"External blocker", false},
		{"Closed but edged", false},
	} {
		if got := strings.Contains(row(graph, c.needle), "⊘"); got != c.want {
			t.Errorf("Graph %q: ⊘ = %v, want %v", c.needle, got, c.want)
		}
	}
	m.sessions = map[string]session.Info{"grl": {State: session.Alive}} // a blocked bead stays in the Inbox only while it has a session
	if !strings.Contains(row(m.inboxView(m.paneHeight()), "Grill waits"), "⊘") {
		t.Error("Inbox row for a dep-blocked bead lacks ⊘")
	}
	for _, c := range []struct {
		id   string
		want bool
	}{{"dep", true}, {"stb", true}, {"cls", false}, {"ext", false}} {
		head := strings.SplitN(m.detailView(m.beadByID(c.id), 0, 1, 30), "\n", 2)[0]
		if got := strings.Contains(head, "⊘"); got != c.want {
			t.Errorf("detail header %s: ⊘ = %v, want %v (%q)", c.id, got, c.want, head)
		}
	}
}

// Blocked-ness rides the existing snapshot: a refresh is still exactly one bd
// list call.
func TestLoadIsOneListCall(t *testing.T) {
	origList, origProbe, origSess := listAll, probeLiveness, readSessions
	defer func() { listAll, probeLiveness, readSessions = origList, origProbe, origSess }()
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	calls := 0
	listAll = func() ([]bd.Bead, error) { calls++; return nil, nil }
	probeLiveness = func([]string) map[string]bool { return nil }
	New(nil, nil).load()()
	if calls != 1 {
		t.Errorf("load made %d bd list calls, want 1", calls)
	}
}

// Container headers (Graph header row + detail header) render their kind as a
// chip — black bold padded UPPERCASE on the kind colour — while member leaves keep the
// plain coloured UPPERCASE tag.
func TestContainerKindChip(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	if kindColor(contract.KindCheckpoint) != "180" || kindColor(contract.KindChart) != "141" {
		t.Errorf("kindColor: checkpoint=%s chart=%s, want 180/141", kindColor(contract.KindCheckpoint), kindColor(contract.KindChart))
	}
	chip := func(name, bg string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color(bg)).Bold(true).Padding(0, 1).Render(name)
	}
	m := New(nil, nil)
	m.w, m.h = 120, 40
	m.hideClosedRoots = false
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: Gate", Status: "open"},
		{ID: "leaf", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "Member leaf"},
		{ID: "cp1", Kind: contract.KindCheckpoint, Container: true, Title: "Checkpoint: Ship", Status: "open"},
	}
	m.rebuildGroups()
	graph := m.graphView(m.paneHeight())
	line := func(needle string) string {
		for _, l := range strings.Split(graph, "\n") {
			if strings.Contains(l, needle) {
				return l
			}
		}
		t.Fatalf("no Graph line containing %q in:\n%s", needle, graph)
		return ""
	}
	for _, c := range []struct{ id, needle, want string }{
		{"ch1", "Gate", chip("CHART", "141")},
		{"cp1", "Ship", chip("CHECKPOINT", "180")},
	} {
		if !strings.Contains(line(c.needle), c.want) {
			t.Errorf("Graph header %s lacks chip %q: %q", c.id, c.want, line(c.needle))
		}
		head := strings.SplitN(m.detailView(m.beadByID(c.id), 0, 1, 30), "\n", 2)[0]
		if !strings.Contains(head, c.want) {
			t.Errorf("detail header %s lacks chip %q: %q", c.id, c.want, head)
		}
	}
	plain := lipgloss.NewStyle().Foreground(kindColor(contract.KindResearch)).Bold(true).Render("RESEARCH")
	if l := line("Member leaf"); !strings.Contains(l, plain) {
		t.Errorf("member leaf lacks plain UPPERCASE tag %q: %q", plain, l)
	}
}

// Every contract kind gets its own Palette A hue (Spec Slice B): pinned,
// pairwise distinct, clear of the status/warn colours and greys; unknown kinds
// fall back to 244.
func TestKindColorPalette(t *testing.T) {
	want := map[contract.Kind]lipgloss.Color{
		contract.KindSignal: "51", contract.KindGrilling: "75", contract.KindResearch: "80",
		contract.KindPrototype: "170", contract.KindChart: "141", contract.KindChartConflict: "161",
		contract.KindSpec: "147", contract.KindCheckpoint: "180", contract.KindBuild: "72",
		contract.KindReview: "108", contract.KindCritique: "167", contract.KindConflict: "160",
		contract.KindApproval: "41", contract.KindPivot: "130", contract.KindRollback: "99",
		contract.KindTask: "248",
	}
	reserved := map[lipgloss.Color]bool{"203": true, "214": true, "213": true, "220": true, "114": true, "111": true, "210": true, "240": true, "244": true}
	seen := map[lipgloss.Color]contract.Kind{}
	for k, c := range want {
		got := kindColor(k)
		if got != c {
			t.Errorf("kindColor(%s) = %s, want %s", k, got, c)
		}
		if reserved[got] {
			t.Errorf("kindColor(%s) = %s collides with a status/warn/grey colour", k, got)
		}
		if o, dup := seen[got]; dup {
			t.Errorf("kindColor(%s) = %s, same as %s", k, got, o)
		}
		seen[got] = k
	}
	for _, k := range []contract.Kind{"", "bogus"} {
		if got := kindColor(k); got != "244" {
			t.Errorf("kindColor(%q) = %s, want fallback 244", k, got)
		}
	}
}

// Split view dims the unfocused pane (Spec FR5): its content renders
// Faint with inner colours stripped so the dim shows; the focused pane and the
// single-pane layout carry no faint SGR. Tab flips which pane is faint.
func TestSplitPaneDimsUnfocused(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	const faint = "\x1b[2m"
	dimmed := func(s string) string { return lipgloss.NewStyle().Faint(true).Render(s) }
	sep := func(focused bool) string { return lipgloss.NewStyle().Foreground(borderColor(focused)).Render("│") }
	master, detail := []string{pinkFg.Render("MASTER")}, pinkFg.Render("DETAIL")

	m := New(nil, nil)
	m.w, m.h = 80, 24
	s := m.splitPane(master, detail, 3)
	if !strings.Contains(s, dimmed("DETAIL")) || strings.Contains(s, dimmed("MASTER")) || !strings.Contains(s, sep(true)) {
		t.Errorf("master focused: want detail faint, master not, accented separator:\n%q", s)
	}
	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	s = m.splitPane(master, detail, 3)
	if !strings.Contains(s, dimmed("MASTER")) || strings.Contains(s, dimmed("DETAIL")) || !strings.Contains(s, sep(false)) {
		t.Errorf("detail focused: want master faint, detail not, plain separator:\n%q", s)
	}

	m.w = 55 // single pane: only the focused pane shows, never faint
	for _, f := range []bool{false, true} {
		m.focusDetail = f
		if s := m.layout("", master, 0, detail, 3); strings.Contains(s, faint) {
			t.Errorf("single pane focusDetail=%v must not be faint:\n%q", f, s)
		}
	}
}

// The Graph gutter marks the selection's open blockers with ! and a folded
// header with how many it hides; everything else — and every Inbox row — gets
// no mark. Marks follow the selection.
func TestBlockerGutter(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h = 120, 40
	m.hideClosedRoots = false
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: Near", Status: "open"},
		{ID: "sel", Kind: contract.KindGrilling, Parent: "ch1", Status: "open", Title: "Selected one", BlockedBy: []string{"b1", "bx", "f1", "f2"}},
		{ID: "b1", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "Open blocker"},
		{ID: "bx", Kind: contract.KindResearch, Parent: "ch1", Status: "closed", Title: "Closed blocker"},
		{ID: "un", Kind: contract.KindResearch, Parent: "ch1", Status: "open", Title: "Unrelated", BlockedBy: []string{"b1"}},
		{ID: "ch2", Kind: contract.KindChart, Container: true, Title: "Chart: Far", Status: "open"},
		{ID: "f1", Kind: contract.KindResearch, Parent: "ch2", Status: "open", Title: "Far one"},
		{ID: "f2", Kind: contract.KindResearch, Parent: "ch2", Status: "in_progress", Title: "Far two"},
	}
	m.rebuildGroups()
	m.collapsed["ch2"] = true
	m.selGraph = "sel"

	gutter := func(needle string) string {
		for _, l := range strings.Split(m.graphView(m.paneHeight()), "\n") {
			if strings.Contains(l, needle) {
				return string([]rune(l)[0])
			}
		}
		t.Fatalf("no row %q", needle)
		return ""
	}
	for needle, want := range map[string]string{
		"Open blocker": "!", "Closed blocker": " ", "Unrelated": " ",
		"Selected one": " ", "Near": " ", "Far": "2",
	} {
		if got := gutter(needle); got != want {
			t.Errorf("sel=sel %q gutter = %q, want %q", needle, got, want)
		}
	}

	m.selGraph = "un"
	for needle, want := range map[string]string{"Open blocker": "!", "Far": " ", "Selected one": " "} {
		if got := gutter(needle); got != want {
			t.Errorf("sel=un %q gutter = %q, want %q", needle, got, want)
		}
	}

	m.selInbox = "sel"
	m.sessions = map[string]session.Info{"sel": {State: session.Alive}} // keeps blocked sel in the Inbox
	for _, l := range strings.Split(m.inboxView(m.paneHeight()), "\n") {
		if l != "" && !strings.HasPrefix(l, "❯") && !strings.HasPrefix(l, "  ") {
			t.Errorf("Inbox row carries a gutter: %q", l)
		}
	}
}

// l on a blocked HITL bead asks for inline y-only confirmation naming its open
// blockers (ADR 0026 §Blocked-launch confirm): y reaches the unchanged
// launch path; anything else cancels without exec. Unblocked HITL launches directly.
func TestBlockedLaunchConfirm(t *testing.T) {
	origStart, origSwitch := startSession, switchSession
	defer func() { startSession, switchSession = origStart, origSwitch }()
	execs := 0
	startSession = func(bd.Bead, contract.AgentCommand) error { execs++; return nil }
	switchSession = func(string) error { return nil }

	key := func(s string) tea.KeyMsg {
		switch s {
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	setup := func(sel bd.Bead, rest ...bd.Bead) Model {
		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.w, m.h = 64, 24
		m.beads = append([]bd.Bead{sel}, rest...)
		m.selGraph = sel.ID
		return m
	}
	blockers := []bd.Bead{
		{ID: "b1", Status: "open", Title: "First blocker with a fairly long title"},
		{ID: "b2", Status: "in_progress", Title: "Second blocker"},
		{ID: "b3", Status: "open", Title: "Third blocker"},
		{ID: "b4", Status: "open", Title: "Fourth blocker"},
		{ID: "b5", Status: "closed", Title: "Done blocker"},
	}
	grl := bd.Bead{ID: "g", Kind: contract.KindGrilling, Status: "open", Title: "Grill the plan",
		BlockedBy: []string{"b1", "b2", "b3", "b4", "b5"}}

	m := setup(grl, blockers...)
	next, cmd := m.Update(key("l"))
	m = next.(Model)
	if m.editMode != editConfirmLaunch || cmd != nil || execs != 0 {
		t.Fatalf("l on blocked HITL: mode=%v cmd=%v execs=%d, want confirm mode, no exec", m.editMode, cmd, execs)
	}
	p := m.status
	if !strings.HasPrefix(p, `launch "Grill the plan" anyway? blocked by: First blocker`) ||
		!strings.Contains(p, "…") || !regexp.MustCompile(` \+\d+$`).MatchString(p) ||
		strings.Contains(p, "Done blocker") || lipgloss.Width(p) > m.w {
		t.Errorf("prompt = %q (width %d), want truncated open blockers with +N within %d cols", p, lipgloss.Width(p), m.w)
	}
	// At the 40-col floor the prompt still fits one line: blockers fold into +N.
	if narrow := confirmLaunchPrompt(grl.Title, m.openBlockers(&grl), 40); lipgloss.Width(narrow) > 40 || !strings.HasSuffix(narrow, " +4") {
		t.Errorf("40-col prompt = %q (width %d), want ≤40 cols ending +4", narrow, lipgloss.Width(narrow))
	}
	// A wide terminal cuts nothing: the caps derive from the width, not fixed limits.
	long := "A plan title well past thirty columns wide"
	wideB := []*bd.Bead{{Title: "A blocker title that is also past twenty cols"}}
	if wide := confirmLaunchPrompt(long, wideB, 200); strings.Contains(wide, "…") {
		t.Errorf("200-col prompt = %q, want untruncated", wide)
	}
	if wide := confirmLaunchPrompt(long+long, nil, 200); strings.Contains(wide, "…") {
		t.Errorf("200-col marked-blocked prompt = %q, want untruncated", wide)
	}
	// The orphan path is never cut; the title takes the cut (fitTitle's trailing args).
	if o := confirmOrphanPrompt(long, "/wt/orphan-tree", 120); !strings.Contains(o, "/wt/orphan-tree (crashed") || !strings.Contains(o, "…") || lipgloss.Width(o) > 120 {
		t.Errorf("120-col orphan prompt = %q, want title cut, path kept, ≤120 cols", o)
	}
	lines := strings.Split(m.helpLine(), "\n")
	if lines[0] != "y launch · n/esc cancel" || lines[len(lines)-1] != p {
		t.Errorf("helpLine = %q, want footer 'y launch · n/esc cancel' over the prompt", m.helpLine())
	}

	// y proceeds into the launch path.
	m, cmd = settleLaunch(m.Update(key("y")))
	if m.editMode != editNone || cmd == nil || execs != 1 {
		t.Errorf("y: mode=%v cmd=%v execs=%d, want launched", m.editMode, cmd, execs)
	}

	// n / esc / enter / other cancel with no exec.
	for _, k := range []string{"n", "esc", "enter", "x"} {
		m := setup(grl, blockers...)
		next, _ := m.Update(key("l"))
		next, cmd := next.(Model).Update(key(k))
		m = next.(Model)
		if m.editMode != editNone || cmd != nil || m.status != "launch cancelled" || execs != 1 {
			t.Errorf("%s: mode=%v status=%q execs=%d, want cancelled, no exec", k, m.editMode, m.status, execs)
		}
	}

	// y launches the confirmed bead, not whatever the selection drifted to:
	// a refresh that drops g moves the selection onto an AFK bead; y must cancel.
	m = setup(grl, blockers...)
	next, _ = m.Update(key("l"))
	afk := bd.Bead{ID: "a", Kind: contract.KindBuild, Status: "open", Title: "AFK build"}
	next, _ = next.(Model).Update(loadedMsg{beads: append([]bd.Bead{afk}, blockers...)})
	m = next.(Model)
	m.selGraph = "a" // the drifted selection now sits on the AFK bead
	next, cmd = m.Update(key("y"))
	m = next.(Model)
	if cmd != nil || execs != 1 || m.status != "launch cancelled: g no longer available" {
		t.Errorf("y after g dropped: cmd=%v execs=%d status=%q, want cancelled, no exec", cmd, execs, m.status)
	}

	// status=blocked with no open Ordering edges.
	mb := bd.Bead{ID: "g", Kind: contract.KindGrilling, Status: "blocked", Title: "Grill the plan"}
	next, _ = setup(mb).Update(key("l"))
	// 64 cols leaves the title 12 of the wording's 52.
	if got := next.(Model).status; got != `"Grill the p…" is marked blocked (no open deps) — launch anyway?` {
		t.Errorf("marked-blocked prompt = %q", got)
	}

	// Unblocked HITL still launches directly.
	free := bd.Bead{ID: "g", Kind: contract.KindGrilling, Status: "open", Title: "Free", BlockedBy: []string{"b5"}}
	m, cmd = settleLaunch(setup(free, blockers...).Update(key("l")))
	if m.editMode != editNone || cmd == nil || execs != 2 {
		t.Errorf("unblocked l: mode=%v execs=%d, want direct launch", m.editMode, execs)
	}
}

// Left/right cycle over the five tabs in bar order, wrapping at both ends.
func TestTabsCycle(t *testing.T) {
	m := New(nil, nil)
	var got []tab
	for _, k := range []tea.KeyType{tea.KeyRight, tea.KeyRight, tea.KeyRight, tea.KeyRight, tea.KeyLeft, tea.KeyShiftTab} {
		next, _ := m.updateKey(tea.KeyMsg{Type: k})
		m = next.(Model)
		got = append(got, m.tab)
	}
	if want := []tab{tabInbox, tabSearch, tabHistory, tabLogs, tabHistory, tabSearch}; !slices.Equal(got, want) {
		t.Errorf("tabs = %v, want %v", got, want)
	}
}

// G/I/S/L jump to Graph/Inbox/Search/Logs from every tab with the arrows' side effects;
// the current tab's key is a no-op; edit modes keep them as text (ADR 0058 §1).
func TestTabJumps(t *testing.T) {
	keys := map[tab]string{tabGraph: "G", tabInbox: "I", tabSearch: "S", tabHistory: "H", tabLogs: "L"}
	for from := range tabCount {
		for to, k := range keys {
			m := New(nil, nil)
			m.tab, m.status, m.detailOffset = from, "stale", 3
			got := press(t, m, k)
			if got.tab != to {
				t.Errorf("%s from %v: tab=%v, want %v", k, from, got.tab, to)
			}
			if from == to && (got.status != "stale" || got.detailOffset != 3) {
				t.Errorf("%s on its own tab must be a no-op: status=%q offset=%d", k, got.status, got.detailOffset)
			}
			if from != to && (got.status != "" || got.detailOffset != 0) {
				t.Errorf("%s from %v: status=%q offset=%d, want the arrows' reset", k, from, got.status, got.detailOffset)
			}
		}
	}
	m := New(nil, nil)
	m.editMode, m.editID = editTitle, "x"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if got := next.(Model); got.tab != tabGraph || got.editBuf.text != "G" {
		t.Errorf("G in editTitle: tab=%v buf=%q, want it typed", got.tab, got.editBuf.text)
	}
}

// The tab bar colours each tab's key letter in the accent at every fallback
// width without adding a column (ADR 0058 §2).
func TestTabBarKeyLetters(t *testing.T) {
	prev := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prev)
	m := New(nil, nil)
	for _, w := range []int{120, 60, 45, floorCols} {
		m.w = w
		lipgloss.SetColorProfile(termenv.Ascii)
		plain := m.tabBar()
		lipgloss.SetColorProfile(termenv.ANSI256)
		got := m.tabBar()
		// Below 50 cols the six-tab bar shrinks to key letters (ADR 0072); only the width is checked there.
		names := w < 50 || strings.Contains(plain, "Graph") && strings.Contains(plain, "Inbox(0)") && strings.Contains(plain, "Search") && strings.Contains(plain, "History") && strings.Contains(plain, "Logs")
		if lipgloss.Width(got) != lipgloss.Width(plain) || !names {
			t.Errorf("w=%d: plain=%q (%d) vs coloured %d wide", w, plain, lipgloss.Width(plain), lipgloss.Width(got))
		}
		for _, l := range []string{"G", "I", "S", "H", "L", "#"} {
			if !regexp.MustCompile(`\x1b\[[0-9;]*38;5;` + string(tabKeyColor) + `[0-9;]*m` + l).MatchString(got) {
				t.Errorf("w=%d: key %s not in accent: %q", w, l, got)
			}
		}
	}
}

// Detached sessions surface on their bead's row from the tmux table alone (ADR
// 0038): ● alive / red ✕ exited / · none in the liveness column, a session line
// in the detail header, N live in the tab bar, a closed bead with a session kept
// under hide-closed, and l attaching an alive one whatever its status.
func TestSessionsOnRows(t *testing.T) {
	origList, origProbe, origSess, origSwitch := listAll, probeLiveness, readSessions, switchSession
	defer func() {
		listAll, probeLiveness, readSessions, switchSession = origList, origProbe, origSess, origSwitch
	}()
	listAll = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: S", Status: "open"},
			{ID: "up", Kind: contract.KindGrilling, Parent: "ch1", Status: "in_progress", Title: "Running one"},
			{ID: "rc", Kind: contract.KindGrilling, Parent: "ch1", Status: "open", Title: "Failed one"},
			{ID: "none", Kind: contract.KindGrilling, Parent: "ch1", Status: "open", Title: "Never launched"},
			{ID: "sig", Kind: contract.KindSignal, Status: "closed", Title: "Closed but live"},
			{ID: "gone", Kind: contract.KindSignal, Status: "closed", Title: "Closed no session"},
		}, nil
	}
	probeLiveness = func([]string) map[string]bool { return nil }
	readSessions = func() (map[string]session.Info, error) {
		return map[string]session.Info{
			"up":  {Worktree: "/wt/up", State: session.Alive},
			"rc":  {Worktree: "/wt/rc", State: session.Exited, RC: 3},
			"sig": {State: session.Alive},
		}, nil
	}
	mm, _ := New(nil, nil).Update(New(nil, nil).load()())
	m := mm.(Model)
	m.w, m.h = 120, 30

	graph := m.graphView(m.paneHeight())
	line := func(title string) string {
		for _, l := range strings.Split(graph, "\n") {
			if strings.Contains(l, title) {
				return l
			}
		}
		return ""
	}
	for _, c := range []struct{ title, glyphs string }{
		{"Running one", "◐●"}, {"Failed one", "○✕"}, {"Never launched", "○·"}, {"Closed but live", "✓●"},
	} {
		if l := line(c.title); !strings.Contains(l, c.glyphs+" ") {
			t.Errorf("%s: want %q, got %q", c.title, c.glyphs, l)
		}
	}
	if line("Closed no session") != "" {
		t.Error("a closed root with no session stays hidden under hide-closed")
	}
	// Alive + bead closed is done, waiting on /exit; alive + open is not (ADR 0067 §2).
	if l := line("Closed but live"); !strings.Contains(l, "done — /exit") {
		t.Errorf("an alive session on a closed bead reads done — /exit: %q", l)
	}
	if l := line("Running one"); strings.Contains(l, "done") {
		t.Errorf("an alive session on an open bead is not done: %q", l)
	}
	if d := m.detailView(m.beadByID("sig"), 0, 1, 30); !strings.Contains(d, "session     done — /exit") {
		t.Errorf("sig detail: want done — /exit, got:\n%s", d)
	}
	if v := m.View(); strings.Contains(v, "detached") {
		t.Errorf("no human-facing label says detached:\n%s", v)
	}
	if tb := m.tabBar(); !strings.Contains(tb, "2 live") {
		t.Errorf("tab bar should count 2 live sessions: %q", tb)
	}

	for _, c := range []struct{ id, want string }{
		{"up", "session     alive · /wt/up"}, {"rc", "session     exited (rc=3)"},
	} {
		if d := m.detailView(m.beadByID(c.id), 0, 1, 30); !strings.Contains(d, c.want) {
			t.Errorf("%s detail: want %q, got:\n%s", c.id, c.want, d)
		}
	}
	// A signal death shows the signal, not an rc (ADR 0038 §6).
	ms := m
	ms.sessions = map[string]session.Info{"rc": {State: session.Exited, Signal: "killed"}}
	if d := ms.detailView(ms.beadByID("rc"), 0, 1, 30); !strings.Contains(d, "session     exited (signal=killed)") {
		t.Errorf("signal detail: want exited (signal=killed), got:\n%s", d)
	}
	if d := m.detailView(m.beadByID("none"), 0, 1, 30); strings.Contains(d, "session ") {
		t.Errorf("no session line without a session:\n%s", d)
	}

	for _, c := range []struct{ id, want string }{{"up", "l attach"}, {"sig", "l attach"}, {"none", "l launch"}} {
		m.selGraph = c.id
		if f := m.footer(); !strings.Contains(f, c.want) {
			t.Errorf("%s footer: want %q, got %q", c.id, c.want, f)
		}
	}

	// l on the alive closed Signal attaches straight away: no launch, no confirm.
	var attached string
	switchSession = func(id string) error { attached = id; return nil }
	m.selGraph = "sig"
	mm, cmd := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if attached != "sig" || cmd == nil || mm.(Model).editMode != editNone {
		t.Errorf("l on an alive session should attach sig; attached=%q cmd=%v mode=%v", attached, cmd != nil, mm.(Model).editMode)
	}

	// A killed server lists nothing: every row falls back to ·.
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	mm, _ = m.Update(m.load()())
	if g := mm.(Model).graphView(m.paneHeight()); strings.ContainsAny(g, "●✕") {
		t.Errorf("no server: every row falls back to ·:\n%s", g)
	}
	if tb := mm.(Model).tabBar(); !strings.Contains(tb, "0 live") {
		t.Errorf("tab bar always shows N live, 0 included: %q", tb)
	}
}

// l on an exited session resumes it (ADR 0038 §3, ADR 0039): footer `l resume`,
// whatever the bead's status and with no blocked confirm; session.Resume gets the
// bead, its listed session and the ctx's harness, then the TUI switches to it. A
// Resume error (the harness's pruned transcript) is the status, nothing attached.
func TestLResumesExitedSession(t *testing.T) {
	origResume, origSwitch := resumeSession, switchSession
	defer func() { resumeSession, switchSession = origResume, origSwitch }()
	var gotBead bd.Bead
	var gotInfo session.Info
	var gotH contract.Harness
	var order []string
	resumeSession = func(b bd.Bead, s session.Info, h contract.Harness) error {
		gotBead, gotInfo, gotH = b, s, h
		order = append(order, "resume")
		return nil
	}
	var attached string
	switchSession = func(id string) error { attached = id; order = append(order, "switch"); return nil }

	h := claudecode.New("")
	m := New(harnessCtx{h}, nil)
	m.beads = []bd.Bead{
		{ID: "blk", Kind: contract.KindGrilling, Status: "open", Title: "Blocker"},
		{ID: "g", Kind: contract.KindGrilling, Status: "blocked", Title: "Failed", BlockedBy: []string{"blk"}},
	}
	m.sessions = map[string]session.Info{"g": {Session: "s1", Worktree: "/wt/g", State: session.Exited, RC: 1}}
	m.selGraph = "g"
	m.w, m.h = 120, 30
	if f := m.footer(); !strings.Contains(f, "l resume") {
		t.Errorf("footer = %q, want l resume", f)
	}
	mm, cmd := settleLaunch(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}))
	if cmd == nil || attached != "g" || mm.editMode != editNone {
		t.Errorf("l should resume then attach g; attached=%q cmd=%v mode=%v", attached, cmd != nil, mm.editMode)
	}
	if !slices.Equal(order, []string{"resume", "switch"}) {
		t.Errorf("order = %v, want resume then switch", order)
	}
	if gotBead.ID != "g" || gotInfo.Session != "s1" || gotH != h {
		t.Errorf("Resume got %q / %+v / %v", gotBead.ID, gotInfo, gotH)
	}

	// A Resume error is the status, nothing attached, and the rows reload. x is
	// hinted only while the re-read table still has the session exited: one another
	// TUI already resumed is alive, so the refresh is the answer, not a drop.
	origSess := readSessions
	defer func() { readSessions = origSess }()
	attached = ""
	resumeSession = func(bd.Bead, session.Info, contract.Harness) error {
		return errors.New("claude transcript for session s1 is gone")
	}
	for _, tc := range []struct {
		state session.State
		hint  bool
	}{{session.Exited, true}, {session.Alive, false}} {
		readSessions = func() (map[string]session.Info, error) {
			return map[string]session.Info{"g": {Session: "s1", State: tc.state}}, nil
		}
		mm, cmd = settleLaunch(m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}))
		st := mm.status
		if cmd == nil || attached != "" || !strings.Contains(st, "resume: claude transcript") {
			t.Errorf("%s: a Resume error must be the status with a reload, nothing attached; status=%q cmd=%v", tc.state, st, cmd != nil)
		}
		if strings.Contains(st, "x drops it") != tc.hint {
			t.Errorf("%s: status %q, want x hint %v", tc.state, st, tc.hint)
		}
	}
}

// x drops a bead's session, alive or exited (ADR 0038 §4, ADR 0037 §5): footer
// `x drop` only with a session; x opens a y-only confirm; n/esc leave it
// untouched; y drops via session.Drop, records hitl.dropped with the state, and
// reloads. x on a row with no session does nothing.
func TestXDropsSession(t *testing.T) {
	origDrop, origList, origAll := dropSession, readSessions, listAll
	defer func() { dropSession, readSessions, listAll = origDrop, origList, origAll }()
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	var dropped []string
	var dropErr error
	dropSession = func(b bd.Bead, s session.Info) error { dropped = append(dropped, b.ID+" "+s.Worktree); return dropErr }
	key := func(s string) tea.KeyMsg {
		if s == "esc" {
			return tea.KeyMsg{Type: tea.KeyEsc}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}

	m := New(nil, nil)
	m.w, m.h = 120, 30
	m.beads = []bd.Bead{
		{ID: "up", Kind: contract.KindGrilling, Status: "open", Title: "Alive one"},
		{ID: "rc", Kind: contract.KindGrilling, Status: "open", Title: "Exited one"},
		{ID: "none", Kind: contract.KindGrilling, Status: "open", Title: "No session"},
	}
	m.sessions = map[string]session.Info{
		"up": {Session: "s-up", Worktree: "/wt/up", State: session.Alive},
		"rc": {Session: "s-rc", Worktree: "/wt/rc", State: session.Exited, RC: 1},
	}
	readSessions = func() (map[string]session.Info, error) { return m.sessions, nil }

	m.selGraph = "none"
	if f := m.footer(); strings.Contains(f, "x drop") {
		t.Errorf("no session, no x hint: %q", f)
	}
	if mm, cmd := m.Update(key("x")); mm.(Model).editMode != editNone || cmd != nil {
		t.Error("x on a row with no session must do nothing")
	}

	for _, c := range []struct{ id, state string }{{"up", "alive"}, {"rc", "exited"}} {
		m.selGraph = c.id
		if f := m.footer(); !strings.Contains(f, "x drop") {
			t.Errorf("%s footer: want x drop, got %q", c.id, f)
		}
		for _, cancel := range []string{"n", "esc"} {
			mm, _ := m.Update(key("x"))
			if mm.(Model).editMode != editConfirmDrop || !strings.HasPrefix(mm.(Model).status, `drop "`) {
				t.Fatalf("%s: x should open the drop confirm; mode=%v status=%q", c.id, mm.(Model).editMode, mm.(Model).status)
			}
			if lines := strings.Split(mm.(Model).helpLine(), "\n"); lines[0] != "y drop · n/esc cancel" {
				t.Errorf("confirm footer = %q", lines[0])
			}
			mm, _ = mm.Update(key(cancel))
			if mm.(Model).editMode != editNone || len(dropped) != 0 {
				t.Errorf("%s %s must cancel untouched; dropped=%v", c.id, cancel, dropped)
			}
		}

		capt := eventlogtest.Capture(t)
		mm, _ := m.Update(key("x"))
		mm, cmd := settleWrite(mm.Update(key("y")))
		if want := c.id + " /wt/" + c.id; len(dropped) != 1 || dropped[0] != want || cmd == nil {
			t.Errorf("%s y: dropped=%v (want %q), reload=%v", c.id, dropped, want, cmd != nil)
		}
		if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"hitl.dropped"}) {
			t.Fatalf("%s events = %v", c.id, got)
		}
		if a := (*capt)[0].Attrs; a["state"] != c.state || a["session"] != "s-"+c.id || a["bead"] != c.id || a["err"] != nil {
			t.Errorf("%s hitl.dropped attrs = %v", c.id, a)
		}
		dropped = nil
	}

	// y re-reads the table: a session that ended, or was relaunched (new @session)
	// since the confirm opened, is not dropped by name.
	for _, fresh := range []map[string]session.Info{nil, {"up": {Session: "s-new", Worktree: "/wt/up", State: session.Alive}}} {
		m.selGraph = "up"
		mm, _ := m.Update(key("x"))
		readSessions = func() (map[string]session.Info, error) { return fresh, nil }
		mm, _ = settleWrite(mm.Update(key("y")))
		if len(dropped) != 0 || !strings.HasPrefix(mm.(Model).status, "drop cancelled") {
			t.Errorf("stale drop: dropped=%v status=%q", dropped, mm.(Model).status)
		}
	}
	// A refresh tick mid-confirm that already shows the relaunch (another TUI
	// dropped and relaunched, FR28) must not retarget the confirm: y compares
	// against the @session x was pressed on, not the last refresh.
	relaunched := map[string]session.Info{"up": {Session: "s-b2", Worktree: "/wt/up", State: session.Alive}}
	readSessions = func() (map[string]session.Info, error) { return relaunched, nil }
	m.selGraph = "up"
	mm, _ := m.Update(key("x"))
	mm, _ = mm.Update(loadedMsg{beads: m.beads, sessions: relaunched})
	mm, _ = settleWrite(mm.Update(key("y")))
	if len(dropped) != 0 || !strings.Contains(mm.(Model).status, "session changed") {
		t.Errorf("refreshed relaunch: dropped=%v status=%q", dropped, mm.(Model).status)
	}
	readSessions = func() (map[string]session.Info, error) { return m.sessions, nil }

	// A failed drop is the status and rides on the event as err.
	dropErr = errors.New("boom")
	capt := eventlogtest.Capture(t)
	mm, _ = m.Update(key("x"))
	mm, _ = settleWrite(mm.Update(key("y")))
	if !strings.Contains(mm.(Model).status, "drop: boom") || (*capt)[0].Attrs["err"] != "boom" {
		t.Errorf("status=%q attrs=%v", mm.(Model).status, (*capt)[0].Attrs)
	}
}

// l's fresh launch hitting busy (ADR 0041 §2–3): an orphaned mugicrew-wt-* holder
// opens the orphan confirm naming its path; y drops it then launches, n leaves it.
// A holder that is a session or a non-mugicrew tree keeps the plain busy status,
// naming the holding bead where known (ADR 0037 §7). A grilling works on its own
// effort/<id> (Spec D1), so no sibling's AFK effort can hold it.
func TestLaunchBusyOrphanConfirm(t *testing.T) {
	origStart, origSwitch, origOn, origDrop, origSess, origProbe, origList := startSession, switchSession, git.WorktreeOn, dropOrphan, readSessions, probeLiveness, listAll
	defer func() {
		startSession, switchSession, git.WorktreeOn, dropOrphan, readSessions, probeLiveness, listAll = origStart, origSwitch, origOn, origDrop, origSess, origProbe, origList
	}()
	const wt = "/tmp/mugicrew-wt-9/wt"
	holder, busy, starts := wt, true, 0
	var dropped []string
	startSession = func(bd.Bead, contract.AgentCommand) error {
		starts++
		if busy {
			return exec.Command("sh", "-c", "exit 128").Run()
		}
		return nil
	}
	switchSession = func(string) error { return nil }
	var askedBranch string
	git.WorktreeOn = func(branch string) (git.Holder, bool, error) {
		askedBranch = branch
		return git.Holder{Path: holder}, true, nil
	}
	dropOrphan = func(p string) error { dropped = append(dropped, p); busy = false; return nil }
	sessions := map[string]session.Info{}
	var sessErr error
	readSessions = func() (map[string]session.Info, error) { return sessions, sessErr }
	live := map[string]bool{}
	probeLiveness = func(ids []string) map[string]bool { return live }

	grl := bd.Bead{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Title: "Grill it"}
	chart := bd.Bead{ID: "chart1", Kind: contract.KindChart, Status: "in_progress"}
	// The AFK check reads a fresh bead list in the launch Cmd: the stale snapshot
	// below lacks chart1.
	var listErr error
	listAll = func() ([]bd.Bead, error) { return []bd.Bead{grl, chart}, listErr }
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	press := func(s string) (Model, tea.Cmd) {
		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.w, m.h = 200, 24
		m.beads = []bd.Bead{grl}
		m.selGraph = grl.ID
		m, cmd := settleLaunch(m.Update(key("l")))
		if s == "" {
			return m, cmd
		}
		return settleLaunch(settleWrite(m.Update(key(s))))
	}

	// Orphan → confirm naming the path; n cancels with nothing dropped.
	m, cmd := press("")
	if m.editMode != editConfirmOrphan || cmd != nil || !strings.Contains(m.status, wt) || !strings.Contains(m.status, "orphaned worktree") {
		t.Fatalf("orphan: mode=%v cmd=%v status=%q, want orphan confirm naming %s", m.editMode, cmd, m.status, wt)
	}
	if askedBranch != "effort/grl1" {
		t.Errorf("holder looked up on %q, want the grilling's effort/grl1", askedBranch)
	}
	m, cmd = press("n")
	if m.editMode != editNone || cmd != nil || len(dropped) != 0 {
		t.Errorf("n: mode=%v cmd=%v dropped=%q, want cancelled with nothing dropped", m.editMode, cmd, dropped)
	}

	// y drops the orphan, then the same launch proceeds and attaches.
	starts = 0
	m, cmd = press("y")
	if !slices.Equal(dropped, []string{wt}) || starts != 2 || cmd == nil {
		t.Errorf("y: dropped=%q starts=%d cmd=%v, want orphan dropped then a launch that execs", dropped, starts, cmd)
	}

	// A failed drop is the status; nothing launches.
	dropOrphan = func(string) error { return errors.New("locked") }
	busy, starts = true, 0
	if m, cmd = press("y"); m.status != "drop orphan: locked" || starts != 1 || cmd != nil {
		t.Errorf("failed drop: status=%q starts=%d cmd=%v, want drop orphan: locked and no launch", m.status, starts, cmd)
	}
	dropOrphan = func(p string) error { dropped = append(dropped, p); busy = false; return nil }

	// Not an orphan: a plain busy status, no confirm.
	for _, c := range []struct {
		name, holder, want string
		setup              func()
	}{
		{"session", wt, "held by grl2's session", func() { sessions["grl2"] = session.Info{Worktree: wt, State: session.Alive} }},
		{"foreign", "/home/me/repo-feature", "held by another session", func() {}},
		{"unreadable sessions", wt, "held by another session", func() { sessErr = errors.New("tmux: permission denied") }},
		{"unreadable beads", wt, "held by another session", func() { listErr = errors.New("dolt locked") }},
	} {
		busy, holder, dropped, sessErr, listErr = true, c.holder, nil, nil, nil
		clear(sessions)
		clear(live)
		c.setup()
		m, cmd = press("")
		if m.editMode != editNone || cmd != nil || len(dropped) != 0 || !strings.Contains(m.status, "busy") || !strings.Contains(m.status, c.want) {
			t.Errorf("%s holder: mode=%v status=%q, want plain busy %q", c.name, m.editMode, m.status, c.want)
		}
	}
}

// A closed bead's custom close reason shows in the detail header; empty and bd's
// default "Closed" show nothing. A long reason wraps at the pane's inner width and
// its extra lines come out of the description budget, so the pane still fits
// (Spec FR6).
func TestDetailCloseReason(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h = 100, 24
	b := bd.Bead{ID: "b1", Kind: contract.KindGrilling, Status: "closed", Title: "done",
		Description: strings.Repeat("line\n", 60)}
	render := func(reason string) (string, int) {
		b.CloseReason = reason
		return m.detailLayout(&b, m.paneHeight(), 0, 0, 1)
	}
	if s, _ := render("superseded by mc-z"); !strings.Contains(s, "superseded by mc-z") {
		t.Errorf("custom reason missing from detail:\n%s", s)
	}
	for _, r := range []string{"", bd.DefaultCloseReason} {
		if s, _ := render(r); strings.Contains(s, "reason") {
			t.Errorf("reason %q must show no line, got:\n%s", r, s)
		}
	}

	_, base := render("")
	_, shortOff := render("x")
	long := strings.Repeat("because ", 40) // wraps to several rows at innerW
	s, longOff := render(long)
	// The reason's field line, wrapped at innerW.
	rows := lineCount(lipgloss.NewStyle().Width(m.detailInnerW()).Render(fieldLine("reason", long)))
	if rows < 2 {
		t.Fatalf("long reason should wrap to several rows, got %d", rows)
	}
	if shortOff != base+1 || longOff != base+rows {
		t.Errorf("maxOff: none=%d one-line=%d wrapped=%d, want budget to shrink by the reason's rows (+1, +%d)", base, shortOff, longOff, rows)
	}
	if got := lineCount(lipgloss.NewStyle().Width(m.detailInnerW()).Render(s)); got > m.paneHeight() {
		t.Errorf("detail renders %d rows, exceeds pane height %d", got, m.paneHeight())
	}
}

// The keymap audit fixes (ADR 0058 §6): title/raw edits show a legend,
// ctrl+c cancels an edit mode instead of quitting, h is Graph-only, and l refuses
// a new launch on a closed HITL bead or a HITL container.
func TestKeymapAuditFixes(t *testing.T) {
	newModel := func() Model {
		m := New(nil, nil)
		m.w, m.h = floorCols, 20
		m.beads = []bd.Bead{
			{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
			{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Title: "T"},
			{ID: "done1", Kind: contract.KindGrilling, Parent: "chart1", Status: "closed", Title: "Done"},
			{ID: "hcon", Kind: contract.KindGrilling, Container: true, Status: "open", Title: "Mis-typed"},
		}
		m.rebuildGroups()
		m.selGraph = "grl1"
		return m
	}
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}

	// Legends: title and raw edit help lines carry enter commit|run · esc cancel at 40 cols.
	for _, c := range []struct{ open, want string }{{"et", "enter commit"}, {"E", "enter run"}} {
		var next tea.Model = newModel()
		for _, k := range c.open {
			next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		}
		m := next.(Model)
		h := m.helpLine()
		if !strings.Contains(h, c.want) || !strings.Contains(h, "esc cancel") {
			t.Errorf("%s help line missing legend %q · esc cancel: %q", c.open, c.want, h)
		}
		if w := lipgloss.Width(strings.Split(h, "\n")[0]); w > floorCols {
			t.Errorf("%s legend is %d cols, want <= %d", c.open, w, floorCols)
		}
	}

	// ctrl+c cancels pick/title/raw/create and does not quit.
	for _, open := range []string{"e", "et", "E", "n"} {
		m := newModel()
		if open == "n" {
			m.tab = tabInbox
		}
		var next tea.Model = m
		for _, k := range open {
			next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		}
		if next.(Model).editMode == editNone {
			t.Fatalf("%s did not open an edit mode", open)
		}
		next, cmd := next.(Model).Update(ctrlC)
		if next.(Model).editMode != editNone {
			t.Errorf("ctrl+c in %s: editMode=%d, want editNone", open, next.(Model).editMode)
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Errorf("ctrl+c in %s quit the TUI", open)
			}
		}
	}
	// ctrl+c in normal mode still quits.
	if _, cmd := newModel().Update(ctrlC); cmd == nil {
		t.Error("ctrl+c in normal mode should quit")
	} else if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c in normal mode should quit")
	}

	// h acts only on Graph.
	for _, tb := range []tab{tabInbox, tabLogs} {
		m := newModel()
		m.tab = tb
		if m = press(t, m, "h"); m.hideClosedRoots != newModel().hideClosedRoots {
			t.Errorf("h on tab %d toggled hideClosedRoots", tb)
		}
	}
	if m := press(t, newModel(), "h"); m.hideClosedRoots == newModel().hideClosedRoots {
		t.Error("h on Graph should toggle hideClosedRoots")
	}

	// l refuses a closed HITL bead and a HITL container: status set, nothing launched.
	origStart, origSwitch := startSession, switchSession
	defer func() { startSession, switchSession = origStart, origSwitch }()
	launched := 0
	startSession = func(bd.Bead, contract.AgentCommand) error { launched++; return nil }
	switchSession = func(string) error { launched++; return nil }
	for _, id := range []string{"done1", "hcon"} {
		m := newModel()
		m.crew = map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}}
		m.selGraph = id
		m = press(t, m, "l")
		if launched != 0 || m.status == "" || !strings.Contains(m.status, id) {
			t.Errorf("l on %s: launched=%d status=%q, want a refusal naming it", id, launched, m.status)
		}
		if w := lipgloss.Width(m.status); w > floorCols {
			t.Errorf("l on %s refusal is %d cols, want <= %d", id, w, floorCols)
		}
	}
}

// l runs the session start in a tea.Cmd (Spec FR9–FR10): while it is in
// flight the status and the bead's row (Graph and Inbox) say launching, and a
// further l on the same bead does nothing and starts nothing (ADR 0107 §5).
// The done msg attaches, or reads as a status error, and clears the in-flight mark.
func TestLaunchInFlightDebounce(t *testing.T) {
	origStart, origSwitch, origList := startSession, switchSession, listAll
	defer func() { startSession, switchSession, listAll = origStart, origSwitch, origList }()
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	starts, switches := 0, 0
	var startErr error
	startSession = func(bd.Bead, contract.AgentCommand) error { starts++; return startErr }
	switchSession = func(string) error { switches++; return nil }

	g1 := bd.Bead{ID: "g1", Kind: contract.KindSignal, Status: "open", Title: "First grill"}
	g2 := bd.Bead{ID: "g2", Kind: contract.KindSignal, Status: "open", Title: "Second grill"}
	up := bd.Bead{ID: "up", Kind: contract.KindSignal, Status: "open", Title: "Alive one"}
	m := New(nil, map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}})
	m.w, m.h = 160, 30
	m.beads = []bd.Bead{g1, g2, up}
	m.sessions = map[string]session.Info{"up": {Session: "s", State: session.Alive}}
	m.rebuildGroups()
	m.selGraph, m.selInbox = "g1", "g1"
	l := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}

	next, launch := m.Update(l)
	m = next.(Model)
	if launch == nil || starts != 0 || !m.launching["g1"] || m.status != "launching First grill… (attaches when ready)" {
		t.Fatalf("l: cmd=%v starts=%d launching = %v status=%q, want an async start in flight", launch != nil, starts, m.launching, m.status)
	}
	if g := m.graphView(m.paneHeight()); !strings.Contains(g, "launching…") {
		t.Errorf("graph row shows no launching marker:\n%s", g)
	}
	m.tab = tabInbox
	if v := m.inboxView(m.paneHeight()); !strings.Contains(v, "launching…") {
		t.Errorf("inbox row shows no launching marker:\n%s", v)
	}
	m.tab = tabGraph
	if next, cmd := m.Update(l); cmd != nil || switches != 0 {
		t.Errorf("l on g1 in flight: cmd=%v switches=%d, want a no-op", cmd != nil, switches)
	} else if next.(Model).status != "still launching First grill…" {
		t.Errorf("l on g1 in flight: status %q, want still launching", next.(Model).status)
	}
	if starts != 0 {
		t.Errorf("starts = %d before the cmd ran, want 0", starts)
	}

	// Success attaches and clears.
	next, cmd := m.Update(launch())
	m = next.(Model)
	if starts != 1 || switches != 1 || cmd == nil || len(m.launching) != 0 || m.status != "" {
		t.Errorf("done ok: starts=%d switches=%d launching = %v status=%q, want attached + cleared", starts, switches, m.launching, m.status)
	}

	// A plain failure is the status and clears; l works again.
	startErr = errors.New("tmux exploded")
	m.selGraph = "g2"
	m, cmd = settleLaunch(m.Update(l))
	if cmd != nil || len(m.launching) != 0 || m.status != "launch: tmux exploded" || switches != 1 {
		t.Errorf("done err: launching = %v status=%q switches=%d, want a status error, cleared", m.launching, m.status, switches)
	}
	if strings.Contains(m.graphView(m.paneHeight()), "launching…") {
		t.Error("launching marker outlived the launch")
	}

	// A success landing while a confirm is open reloads only: no attach, prompt kept.
	startErr = nil
	m.selGraph = "g1"
	next, launch = m.Update(l)
	m = next.(Model)
	m.editMode, m.status = editConfirmDrop, "drop it?"
	next, cmd = m.Update(launch())
	m = next.(Model)
	if switches != 1 || cmd == nil || len(m.launching) != 0 || m.status != "drop it?" || m.editMode != editConfirmDrop {
		t.Errorf("done mid-confirm: switches=%d launching = %v status=%q mode=%v, want reload only", switches, m.launching, m.status, m.editMode)
	}
}

// An exited session's resume runs async too (Spec FR9–FR10): while in
// flight, l does nothing and x can't drop the session out from under it.
func TestResumeInFlightDebounce(t *testing.T) {
	origResume, origSwitch := resumeSession, switchSession
	defer func() { resumeSession, switchSession = origResume, origSwitch }()
	resumes := 0
	resumeSession = func(bd.Bead, session.Info, contract.Harness) error { resumes++; return nil }
	switchSession = func(string) error { return nil }

	m := New(harnessCtx{claudecode.New("")}, nil)
	m.beads = []bd.Bead{{ID: "g", Kind: contract.KindGrilling, Status: "open", Title: "Failed"}}
	m.sessions = map[string]session.Info{"g": {Session: "s1", State: session.Exited}}
	m.selGraph = "g"
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	next, resume := m.Update(key("l"))
	m = next.(Model)
	if resume == nil || resumes != 0 || !m.launching["g"] || m.status != "launching Failed… (attaches when ready)" {
		t.Fatalf("l: resumes=%d launching = %v status=%q, want an async resume in flight", resumes, m.launching, m.status)
	}
	if _, cmd := m.Update(key("l")); cmd != nil {
		t.Error("a second l while resuming must do nothing")
	}
	if mm, _ := m.Update(key("x")); mm.(Model).editMode != editNone {
		t.Error("x must not open the drop confirm while the resume is in flight")
	}
	next, _ = m.Update(resume())
	if resumes != 1 || len(next.(Model).launching) != 0 {
		t.Errorf("done: resumes=%d launching = %v, want one resume, cleared", resumes, next.(Model).launching)
	}
}

// A waiting session (ADR 0099) reads its waiting word on its row and detail line
// in place of plain alive; done and exited outrank it, and launching… still wins.
func TestWaitingSessionLook(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h = 120, 30
	m.beads = []bd.Bead{
		{ID: "ch1", Kind: contract.KindChart, Container: true, Title: "Chart: W", Status: "open"},
		{ID: "al", Kind: contract.KindGrilling, Parent: "ch1", Status: "in_progress", Title: "Plain alive"},
		{ID: "tu", Kind: contract.KindGrilling, Parent: "ch1", Status: "in_progress", Title: "Turn ended"},
		{ID: "as", Kind: contract.KindGrilling, Parent: "ch1", Status: "in_progress", Title: "Asking"},
		{ID: "dn", Kind: contract.KindSignal, Status: "closed", Title: "Done waiting"},
		{ID: "ex", Kind: contract.KindGrilling, Parent: "ch1", Status: "open", Title: "Exited waiting"},
	}
	m.sessions = map[string]session.Info{
		"al": {State: session.Alive, Worktree: "/wt/al"},
		"tu": {State: session.Alive, Worktree: "/wt/tu", Waiting: session.WaitTurn},
		"as": {State: session.Alive, Worktree: "/wt/as", Waiting: session.WaitAsk},
		"dn": {State: session.Alive, Waiting: session.WaitTurn},
		"ex": {State: session.Exited, RC: 1, Waiting: session.WaitAsk},
	}
	m.rebuildGroups()
	line := func(title string) string {
		for _, l := range strings.Split(m.graphView(m.paneHeight()), "\n") {
			if strings.Contains(l, title) {
				return l
			}
		}
		t.Fatalf("no row for %s", title)
		return ""
	}
	for _, c := range []struct{ id, title, glyphs, suffix, detail string }{
		{"al", "Plain alive", "◐●", "", "session     alive · /wt/al"},
		{"tu", "Turn ended", "◐●", "hitl  your turn", "session     your turn · /wt/tu"},
		{"as", "Asking", "◐●", "hitl  needs approval", "session     needs approval · /wt/as"},
		{"dn", "Done waiting", "✓●", "done — /exit", "session     done — /exit"},
		{"ex", "Exited waiting", "○✕", "", "session     exited (rc=1)"},
	} {
		l := line(c.title)
		if !strings.Contains(l, c.glyphs+" ") {
			t.Errorf("%s: want %q, got %q", c.id, c.glyphs, l)
		}
		if c.suffix != "" && !strings.Contains(l, c.title+"  "+c.suffix) {
			t.Errorf("%s: want trailing %q, got %q", c.id, c.suffix, l)
		}
		if c.id != "tu" && c.id != "as" && (strings.Contains(l, "your turn") || strings.Contains(l, "needs approval")) {
			t.Errorf("%s: no waiting word on this row, got %q", c.id, l)
		}
		if d := m.detailView(m.beadByID(c.id), 0, 1, 30); !strings.Contains(d, c.detail) {
			t.Errorf("%s detail: want %q, got:\n%s", c.id, c.detail, d)
		}
	}
	if l := m.inboxView(m.paneHeight()); !strings.Contains(l, "Turn ended —  hitl  your turn") || !strings.Contains(l, "Asking —  hitl  needs approval") {
		t.Errorf("inbox rows carry the waiting word:\n%s", l)
	}
	m.launching = map[string]bool{"tu": true}
	if l := line("Turn ended"); !strings.Contains(l, "launching…") || strings.Contains(l, "your turn") {
		t.Errorf("launching… wins over waiting: %q", l)
	}
}
