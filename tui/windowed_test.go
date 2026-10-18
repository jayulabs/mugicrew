package tui

import (
	"crypto/sha256"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// windowFixture is a long mixed history: live Checkpoints with open/closed members, a
// blocker chain, landed Checkpoints, Charts with leaves, and an Initiative holding some.
func windowFixture(now time.Time) []bd.Bead {
	var beads []bd.Bead
	add := func(id string, k contract.Kind, status, parent string, mut ...func(*bd.Bead)) {
		beads = append(beads, dbead(id, k, status, parent, len(id)%4, mut...))
	}
	cp := contract.KindCheckpoint
	closed := func(b *bd.Bead) { b.Closed, b.CloseReason = now.Add(-time.Hour), "x" }
	add("ini", contract.KindInitiative, "open", "", isContainer)
	for i := range 12 {
		c := fmt.Sprintf("live%d", i)
		pv := c + "pv"
		parent := ""
		if i%3 == 0 {
			parent = "ini"
		}
		add(c, cp, "open", parent, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{pv} })
		add(pv, contract.KindPivot, "open", "")
		add(c+"ap", contract.KindApproval, "open", c)
		for k := range 4 {
			add(fmt.Sprintf("%sm%d", c, k), contract.KindBuild, "closed", c, closed)
			add(fmt.Sprintf("%so%d", c, k), contract.KindBuild, "open", c, blockedBy(pv))
		}
	}
	for i := range 10 {
		c := fmt.Sprintf("old%d", i)
		add(c, cp, "closed", "", isContainer, closed)
		add(c+"m", contract.KindBuild, "closed", c, closed)
		add(c+"L", contract.KindLanding, "closed", c, func(b *bd.Bead) {
			b.Closed, b.CloseReason = now.Add(-time.Duration(i+1)*time.Hour), "landed: aaaaaaaa"
		})
	}
	for i := range 8 {
		c := fmt.Sprintf("chart%d", i)
		parent := ""
		if i%2 == 0 {
			parent = "ini"
		}
		add(c, contract.KindChart, "open", parent, isContainer)
		for k, kind := range []contract.Kind{contract.KindSignal, contract.KindSurvey, contract.KindGrilling, contract.KindResearch} {
			add(fmt.Sprintf("%sd%d", c, k), kind, "open", c, labeled("afk"))
			add(fmt.Sprintf("%sx%d", c, k), kind, "closed", c, closed)
		}
	}
	for i := range 6 {
		add(fmt.Sprintf("sig%d", i), contract.KindSignal, "open", "")
		add(fmt.Sprintf("task%d", i), "", "open", "")
	}
	return beads
}

// frames renders v over every few selections at several heights, drawer open and shut.
func frames(keys []string, render func(sel string, open bool, h int) string) []string {
	var out []string
	for i, k := range keys {
		if i%5 != 0 && i != len(keys)-1 {
			continue
		}
		for _, h := range []int{9, 20} {
			for _, open := range []bool{false, true} {
				out = append(out, render(k, open, h))
			}
		}
	}
	return out
}

// TestWindowedTabsGolden pins Execute, Defog and Focus output byte-for-byte to the
// hashes recorded before row formatting was windowed (UPDATE_GOLDEN=1 re-records).
func TestWindowedTabsGolden(t *testing.T) {
	now := epoch.Add(30 * 24 * time.Hour) // fixed: ages in the golden must not drift with the date
	clock = func() time.Time { return now }
	defer func() { clock = time.Now }()
	beads := windowFixture(now)
	g := graphData{beads: beads, idx: newSnapIndex(beads), w: 100, h: 20, sessions: map[string]session.Info{}}
	all := func(m map[string]bool) map[string]bool { return m }
	_ = all
	var got []string
	rec := func(name string, fs []string) {
		for i, f := range fs {
			got = append(got, fmt.Sprintf("%s/%d %x", name, i, sha256.Sum256([]byte(f))))
		}
	}
	flip := expandAll(g.execList, func(f map[string]bool, r eRow) bool { return ecollapsed(f, r) }, func(r eRow) string { return r.key })
	for name, fl := range map[string]map[string]bool{"exec-folded": nil, "exec-open": flip} {
		keys := []string{}
		for _, r := range (execTab{g: g, flip: fl}).rows() {
			keys = append(keys, r.key)
		}
		rec(name, frames(keys, func(sel string, open bool, h int) string {
			return execTab{g: g, flip: fl, sel: sel, open: open}.view(100, h)
		}))
	}
	dflip := expandAll(g.defogList, func(f map[string]bool, r dRow) bool { return collapsed(f, r) }, func(r dRow) string { return r.key })
	for name, fl := range map[string]map[string]bool{"defog-folded": nil, "defog-open": dflip} {
		keys := []string{}
		for _, r := range (defogTab{g: g, flip: fl}).rows() {
			keys = append(keys, r.key)
		}
		rec(name, frames(keys, func(sel string, open bool, h int) string {
			return defogTab{g: g, flip: fl, sel: sel, open: open}.view(100, h)
		}))
	}
	m := Model{beads: beads, idx: g.idx, sessions: g.sessions, w: 100, h: 20 + chromeH}
	m.focus = focus{id: "ini", flip: map[string]bool{}}
	keys := []string{}
	for _, r := range m.focusRows() {
		keys = append(keys, r.key)
	}
	rec("focus", frames(keys, func(sel string, open bool, h int) string {
		mm := m
		mm.focus.sel, mm.focus.open = sel, open
		return mm.focusView(100, h)
	}))
	slices.Sort(got) // the maps above range in random order
	text := strings.Join(got, "\n") + "\n"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile("testdata/windowed.golden", []byte(text), 0o644)
	}
	want, err := os.ReadFile("testdata/windowed.golden")
	if err != nil {
		t.Fatal(err)
	}
	if text != string(want) {
		t.Errorf("windowed render differs from golden")
	}
	if len(got) < 100 {
		t.Errorf("only %d frames", len(got))
	}
}

// kindTagW matches the rendered badge for every kind, Container and empty included.
func TestKindTagW(t *testing.T) {
	kinds := []contract.Kind{""}
	for k := range kindColors {
		kinds = append(kinds, k)
	}
	kinds = append(kinds, "unknown-kind")
	for _, k := range kinds {
		for _, c := range []bool{false, true} {
			b := &bd.Bead{Kind: k, Container: c}
			if got, want := kindTagW(b), lipgloss.Width(kindTag(b)); got != want {
				t.Errorf("kind %q container=%v: %d, want %d", k, c, got, want)
			}
		}
	}
}

// Only the drawn window is formatted: a 500-row list at height 10 calls line() 10 times.
func TestDrawRowsFormatsOnlyWindow(t *testing.T) {
	idx := make([]string, 500)
	for i := range idx {
		idx[i] = fmt.Sprint(i)
	}
	idx, _, _ = gapAt(idx, make([]string, 500), 250, 100)
	calls := 0
	line := func(i int) (string, string) { calls++; return fmt.Sprint("row", i), "" }
	out := drawRows(idx, line, nil, 251, 250, false, 40, 10, viewport.Model{}, nil)
	if calls != 10 || strings.Count(out, "\n") != 9 {
		t.Errorf("line called %d times, %d lines out", calls, strings.Count(out, "\n")+1)
	}
}

// TestWindowedIdeasInitsGolden pins Ideas and Initiatives output byte-for-byte to the
// hashes recorded before their row formatting was windowed (UPDATE_GOLDEN=1 re-records).
func TestWindowedIdeasInitsGolden(t *testing.T) {
	now := epoch.Add(30 * 24 * time.Hour) // fixed: ages in the golden must not drift with the date
	clock = func() time.Time { return now }
	defer func() { clock = time.Now }()
	beads := windowFixture(now)
	g := graphData{beads: beads, idx: newSnapIndex(beads), w: 100, h: 20, sessions: map[string]session.Info{}}
	var got []string
	rec := func(name string, fs []string) {
		for i, f := range fs {
			got = append(got, fmt.Sprintf("%s/%d %x", name, i, sha256.Sum256([]byte(f))))
		}
	}
	for _, name := range []string{"ideas-default", "ideas-flipped"} {
		fold := name == "ideas-flipped"
		base := ideaTab{ideaData: ideaData{beads: beads, gate: g, w: 100, h: 20}}
		if fold {
			base.flip = map[string]bool{}
			for _, r := range base.rows() {
				if r.foldable() {
					base.flip[r.key] = true
				}
			}
		}
		keys := []string{}
		for _, r := range base.rows() {
			keys = append(keys, r.key)
		}
		rec(name, frames(keys, func(sel string, open bool, h int) string {
			tab := base
			tab.sel, tab.open = sel, open
			return tab.view(100, h)
		}))
	}
	m := Model{beads: beads, idx: g.idx, sessions: g.sessions, w: 100, h: 20 + chromeH}
	stats := m.initStatsOf()
	for _, name := range []string{"inits-open", "inits-closed", "inits-both"} {
		fl := map[string]map[string]bool{"inits-open": nil, "inits-closed": {initsClosed: true}, "inits-both": {initsOpen: true, initsClosed: true}}[name]
		base := initsTab{g: g, stats: stats, flip: fl, w: 100, h: 20}
		keys := []string{}
		for _, r := range base.rows() {
			keys = append(keys, r.key)
		}
		rec(name, frames(keys, func(sel string, open bool, h int) string {
			tab := base
			tab.sel, tab.open = sel, open
			return tab.view(100, h)
		}))
	}
	text := strings.Join(got, "\n") + "\n"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile("testdata/windowed-ideas-inits.golden", []byte(text), 0o644)
	}
	want, err := os.ReadFile("testdata/windowed-ideas-inits.golden")
	if err != nil {
		t.Fatal(err)
	}
	if text != string(want) {
		t.Errorf("windowed render differs from golden")
	}
	if len(got) < 60 {
		t.Errorf("only %d frames", len(got))
	}
}
