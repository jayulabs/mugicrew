package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/stats"
)

func TestBeadBody(t *testing.T) {
	ago := time.Now().Add(-3 * time.Hour)
	done := rollupEntry{loaded: true}
	g := graphData{w: 40, rollups: map[string]rollupEntry{"e": done, "ok": done}, comments: map[commentKey]commentEntry{
		{"l", 1}: {loading: true},
		{"e", 1}: {err: errors.New("bd down")},
		{"ok", 2}: {cs: []bd.Comment{
			{Author: "Franky", Text: "first note", Created: time.Now().Add(-2 * time.Hour)},
			{Author: "Alice", Text: "second **bold**", Created: time.Now().Add(-time.Hour)},
		}},
	}}
	for name, tc := range map[string]struct {
		b    bd.Bead
		desc bool
		has  []string // in order
		not  []string
	}{
		"open, all sections": {bd.Bead{Status: "open", Description: "d", Notes: "n", Design: "g", AcceptanceCriteria: "a"}, true,
			[]string{"── DESCRIPTION ─", "── NOTES ─", "── DESIGN ─", "── ACCEPTANCE ─"}, []string{"closed"}},
		"empty sections omitted": {bd.Bead{Status: "open", Notes: "n"}, true, []string{"── NOTES ─"}, []string{"DESCRIPTION", "DESIGN", "ACCEPTANCE"}},
		"desc=false":             {bd.Bead{Status: "open", Description: "d", Notes: "n"}, false, []string{"── NOTES ─"}, []string{"DESCRIPTION"}},
		"closed default reason":  {bd.Bead{Status: "closed", Closed: ago, Description: "d"}, true, []string{"closed      3h · Closed", "── DESCRIPTION ─"}, nil},
		"closed custom reason":   {bd.Bead{Status: "closed", Closed: ago, CloseReason: "landed: abc"}, true, []string{"3h · landed: abc"}, []string{"Closed"}},
		"count 0 no comments":    {bd.Bead{ID: "x", Status: "open", Notes: "n"}, true, nil, []string{"COMMENTS"}},
		"comments miss":          {bd.Bead{ID: "x", Status: "open", CommentCount: 2}, true, []string{"── COMMENTS (2) ─", "loading…"}, nil},
		"comments loading":       {bd.Bead{ID: "l", Status: "open", CommentCount: 1}, true, []string{"── COMMENTS (1) ─", "loading…"}, nil},
		"comments error":         {bd.Bead{ID: "e", Status: "open", CommentCount: 1}, true, []string{"── COMMENTS (1) ─", "couldn't load comments: bd down"}, []string{"loading…"}},
		"comments loaded": {bd.Bead{ID: "ok", Status: "open", Notes: "n", CommentCount: 2}, true,
			[]string{"── NOTES ─", "── COMMENTS (2) ─", "Franky · 2h", "first note", "Alice · 1h", "second bold"}, []string{"loading…", "**bold**"}},
	} {
		got := ansi.Strip(g.beadBody(&tc.b, tc.desc))
		at := 0
		for _, h := range tc.has {
			i := strings.Index(got[at:], h)
			if i < 0 {
				t.Errorf("%s: %q missing or out of order in:\n%s", name, h, got)
				break
			}
			at += i
		}
		for _, n := range tc.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: unexpected %q in:\n%s", name, n, got)
			}
		}
	}
}

func TestBeadBodyRollup(t *testing.T) {
	row := func(n int, wall int64, usd float64, partial int) stats.Row {
		return stats.Row{Sessions: n, WallMs: wall, Cost: stats.Cost{CostUSD: usd, CostPartial: partial}}
	}
	g := graphData{w: 60, rollups: map[string]rollupEntry{
		"ok":   {loaded: true, subtree: row(3, 90_000, 1.5, 0), spine: row(1, 3_600_000, 2.25, 0)},
		"part": {loaded: true, subtree: row(2, 1000, 1.23, 1), spine: row(1, 1000, 0, 1)},
		"err":  {loaded: true, err: errors.New("bd down\nhint")},
		"none": {loaded: true},
		"free": {loaded: true, notSpine: true, subtree: row(1, 2000, 0.5, 0)},
	}}
	for id, want := range map[string][]string{
		"ok":   {"subtree     1m30s · $1.50 · 3 sessions", "spine       1h0m0s · $2.25 · 1 session\n"},
		"part": {"$1.23+ · 2 sessions"},
		"load": {"subtree     loading…", "spine       loading…"},
		"err":  {"subtree     couldn't load: bd down hint", "spine       couldn't load: bd down hint"},
		"none": {"subtree     no sessions", "spine       no sessions"},
		"free": {"subtree     2s · $0.50 · 1 session", "spine       not on a spine"},
	} {
		b := bd.Bead{ID: id, Status: "closed", Closed: time.Now(), Description: "d"}
		got := ansi.Strip(g.beadBody(&b, true))
		at := 0
		for _, h := range append([]string{"closed ", "── ROLLUP · all retained log ─"}, append(want, "── DESCRIPTION")...) {
			i := strings.Index(got[at:], h)
			if i < 0 {
				t.Errorf("%s: %q missing or out of order in:\n%s", id, h, got)
				break
			}
			at += i
		}
	}
}
