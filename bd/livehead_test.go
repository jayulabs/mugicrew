package bd

import (
	"errors"
	"testing"
)

// liveHead walks an unclosed bead to itself, else along its caused-by successors
// (preferring an unclosed one), else through its supersedes edge, over a fake graph.
func TestLiveHead(t *testing.T) {
	unclosedSet := func(ids ...string) map[string]bool {
		m := map[string]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	for _, c := range []struct {
		name     string
		unclosed map[string]bool
		succ     map[string][]string
		sup      map[string][]string
		from     string
		failOn   string
		want     string
		wantOK   bool
		wantErr  bool
	}{
		{name: "unclosed original", unclosed: unclosedSet("o"), from: "o", want: "o", wantOK: true},
		{name: "signal to chart", unclosed: unclosedSet("c"), succ: map[string][]string{"s": {"c"}}, from: "s", want: "c", wantOK: true},
		{name: "signal to checkpoint, only it open",
			unclosed: unclosedSet("cp"),
			succ:     map[string][]string{"s": {"c"}, "c": {"sp"}, "sp": {"cp"}}, from: "s", want: "cp", wantOK: true},
		{name: "duplicate of a duplicate",
			unclosed: unclosedSet("z"), sup: map[string][]string{"a": {"b"}, "b": {"z"}}, from: "a", want: "z", wantOK: true},
		{name: "pivot-superseded spec",
			unclosed: unclosedSet("sp2"), sup: map[string][]string{"sp": {"sp2"}}, from: "sp", want: "sp2", wantOK: true},
		{name: "prefers the unclosed successor",
			unclosed: unclosedSet("b"), succ: map[string][]string{"a": {"x", "b"}}, from: "a", want: "b", wantOK: true},
		{name: "pivot-superseded spec with obsolete checkpoint successor",
			unclosed: unclosedSet("sp2"), succ: map[string][]string{"sp": {"cp"}}, sup: map[string][]string{"sp": {"sp2"}}, from: "sp", want: "sp2", wantOK: true},
		{name: "escalated chart",
			unclosed: unclosedSet("c2"), succ: map[string][]string{"c": {"sp"}, "sp": {"cp"}}, sup: map[string][]string{"c": {"c2"}}, from: "c", want: "c2", wantOK: true},
		{name: "backtracks from a closed first successor",
			unclosed: unclosedSet("cp2"), succ: map[string][]string{"c": {"sp", "sp2"}, "sp2": {"cp2"}}, sup: map[string][]string{"sp": {"sp2"}}, from: "c", want: "cp2", wantOK: true},
		{name: "landed checkpoint", succ: map[string][]string{"s": {"c"}, "c": {"sp"}, "sp": {"cp"}}, from: "s"},
		{name: "rejected signal", from: "s"},
		{name: "cycle", sup: map[string][]string{"a": {"b"}, "b": {"a"}}, from: "a"},
		{name: "failed read", unclosed: unclosedSet("o"), from: "o", failOn: "o", wantErr: true},
	} {
		show := func(id string) (Bead, error) {
			if id == c.failOn {
				return Bead{}, errors.New("read failed")
			}
			b := Bead{ID: id, Status: "closed"}
			if c.unclosed[id] {
				b.Status = "open"
			}
			return b, nil
		}
		list := func(m map[string][]string) func(string) ([]Bead, error) {
			return func(id string) ([]Bead, error) {
				var out []Bead
				for _, n := range m[id] {
					b, _ := show(n)
					out = append(out, b)
				}
				return out, nil
			}
		}
		got, ok, err := liveHead(c.from, show, list(c.succ), list(c.sup))
		if got != c.want || ok != c.wantOK || (err != nil) != c.wantErr {
			t.Errorf("%s: liveHead(%s) = %q, %v, %v; want %q, %v, err=%v", c.name, c.from, got, ok, err, c.want, c.wantOK, c.wantErr)
		}
	}
}
