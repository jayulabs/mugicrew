package tui

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/session"
)

// TestGraphWindowedGolden pins Graph output byte-for-byte to the hashes recorded before
// its rows were formatted only for the drawn window (UPDATE_GOLDEN=1 re-records): every
// few selections, at two heights, drawer open and shut, plain, folded and filtered
// (dimmed context rows), with the blocker footer and ancestor shade in play.
func TestGraphWindowedGolden(t *testing.T) {
	now := epoch.Add(30 * 24 * time.Hour) // fixed: ages in the golden must not drift with the date
	clock = func() time.Time { return now }
	defer func() { clock = time.Now }()
	beads := windowFixture(now)
	base := graphTab{graphData: graphData{beads: beads, idx: newSnapIndex(beads), w: 100, h: 20, sessions: map[string]session.Info{}}, qfold: map[string]bool{}}
	var got []string
	for _, c := range []struct {
		name string
		q    query
		open bool // every group open
	}{
		{"default", baseline, false},
		{"open", baseline, true},
		{"any", query{status: statusAny}, true},
		{"words", query{status: statusAny, words: "m1"}, true},
	} {
		g := base
		g.q = c.q
		g = g.rebuild()
		if c.open {
			g.flip = expandAll(func(f map[string]bool) []row { gg := g; gg.flip = f; return gg.rows() }, func(f map[string]bool, r row) bool { return r.header && r.kids && gg(g, f)[r.collapseKey] }, func(r row) string { return r.collapseKey })
		}
		var keys []string
		for _, r := range g.rows() {
			if !r.context {
				keys = append(keys, r.sel)
			}
		}
		for i, f := range frames(keys, func(sel string, open bool, h int) string {
			gg := g
			gg.sel, gg.open = sel, open
			return gg.view(100, h)
		}) {
			got = append(got, fmt.Sprintf("%s/%d %x", c.name, i, sha256.Sum256([]byte(f))))
		}
	}
	text := strings.Join(got, "\n") + "\n"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile("testdata/graphwindowed.golden", []byte(text), 0o644)
	}
	want, err := os.ReadFile("testdata/graphwindowed.golden")
	if err != nil {
		t.Fatal(err)
	}
	if text != string(want) {
		t.Errorf("windowed Graph render differs from golden")
	}
	if len(got) < 100 {
		t.Errorf("only %d frames", len(got))
	}
}

// gg is g's folds with flip applied.
func gg(g graphTab, f map[string]bool) map[string]bool { g.flip = f; return g.folds() }
