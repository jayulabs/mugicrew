package transcript

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/claudecode"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, got []string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	g := strings.Join(got, "\n") + "\n"
	if *update {
		os.WriteFile(p, []byte(g), 0o644)
	}
	want, err := os.ReadFile(p)
	if err != nil || string(want) != g {
		t.Errorf("%s differs (run with -update to rewrite):\n%s", name, g)
	}
}

var t0 = time.Date(2026, 10, 15, 1, 44, 0, 0, time.Local)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }


// hitl is a HITL session with a launch prompt, a human turn, an interrupt,
// mdrender'd text, a failed call (6 error lines), an API error and a call
// whose result never came; a backwards timestamp gives ✓ 0.0s.
func hitl() []claudecode.Item {
	return []claudecode.Item{
		{Kind: "prompt", TS: at(0), Text: "do the thing\nwith care\nplease"},
		{Kind: "text", TS: at(1), Text: "I will **start** now.\n\n- one\n- two"},
		{Kind: "tool", TS: at(2), Name: "Bash", ID: "a", Full: "go test ./...\necho done"},
		{Kind: "tool", TS: at(2), Name: "Read", ID: "b", Full: "/tmp/x.go"},
		{Kind: "result", TS: at(14), ID: "a", IsErr: true, Text: "e1\ne2\ne3\ne4\ne5\ne6"},
		{Kind: "result", TS: at(1), ID: "b", Text: "contents"},
		{Kind: "tool", TS: at(15), Name: "Grep", ID: "c", Full: `{"x":1}`},
		{Kind: "human", TS: at(20), Text: "actually, try the other approach"},
		{Kind: "interrupt", TS: at(25), Text: "[Request interrupted by user]"},
		{Kind: "apierr", TS: at(30), Text: "429 rate limited"},
		{Kind: "tool", TS: at(135), Name: "Bash", ID: "d", Full: "sleep 100"},
		{Kind: "result", TS: at(235), ID: "d", Text: "ok"},
	}
}

func TestRenderGolden(t *testing.T) {
	hs := &Session{ID: "0123456789abcdef", Mode: "HITL", Kind: "build", Crewmate: "franky", Start: t0, Outcome: "unended"}
	fs := &Session{ID: "fedcba9876543210", Mode: "AFK", Kind: "build", Crewmate: "franky", Start: at(7200), Outcome: "failed", Exit: 1, Dur: 90 * time.Second,
		Result: "it broke", Stderr: strings.Repeat("err line\n", 14)}
	ss := &Session{ID: "cccccccc22222222", Mode: "AFK", Kind: "build", Crewmate: "franky", Start: at(8000), Outcome: "failed", Exit: 2, Dur: 3 * time.Second,
		Result: "short", Stderr: "boom\nbang\n"}
	es := &Session{ID: "aaaaaaaa11111111", Mode: "AFK", Kind: "chart", Crewmate: "robin", Start: at(9000), Outcome: "ended", Dur: 5 * time.Second}
	for _, c := range []struct {
		name        string
		color, wrap bool
	}{{"color-wrap.txt", true, true}, {"plain-wrap.txt", false, true}, {"plain-nowrap.txt", false, false}} {
		o := Opts{Width: 60, Color: c.color, Wrap: c.wrap}
		out := Render(hs, hitl(), true, o)
		out = append(out, "", "")
		out = append(out, Render(fs, nil, false, o)...) // pruned + failed
		out = append(out, "", "")
		out = append(out, Render(ss, nil, false, o)...) // pruned + failed, short stderr
		out = append(out, "", "")
		out = append(out, Render(es, nil, false, o)...) // pruned + ended
		out = append(out, "", "")
		out = append(out, Render(&Session{ID: "bbbbbbbbbb", Mode: "AFK", Start: at(9100), Outcome: "unended"}, nil, false, o)...)
		golden(t, c.name, out)
	}
}

func TestRenderHidesNothingItWasNotGiven(t *testing.T) {
	// Hidden kinds never reach Render: the reader drops them (items_test).
	s := &Session{ID: "x", Mode: "AFK", Start: t0, Outcome: "ended"}
	if got := Render(s, nil, true, Opts{Width: 40}); len(got) != 1 {
		t.Errorf("empty transcript = %q, want header only", got)
	}
}

func TestTook(t *testing.T) {
	for d, want := range map[time.Duration]string{-time.Second: "0.0s", 1200 * time.Millisecond: "1.2s", 13 * time.Second: "13s", 125 * time.Second: "2m05s"} {
		if got := took(d); got != want {
			t.Errorf("took(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDursRoundsDownAcrossTheMinute(t *testing.T) {
	if got := durs(59990 * time.Millisecond); got != "59s" {
		t.Errorf("durs(59.99s) = %q, want 59s", got)
	}
}

func TestTookNeverPrintsTenPointZero(t *testing.T) {
	if got := took(9990 * time.Millisecond); got != "10s" {
		t.Errorf("took(9.99s) = %q, want 10s", got)
	}
}

func TestWrappedHumanLinesKeepBoldAndMarker(t *testing.T) {
	out := strings.Join(bodyOf(claudecode.Item{Kind: "human", TS: at(0), Text: strings.Repeat("word ", 20)}), "\n")
	if !strings.Contains(out, "\x1b[1m▍") || strings.Count(out, "\x1b[1m▍") < 2 {
		t.Errorf("continuation not bold: %q", out)
	}
}

func TestResultOfFlushedCallAddsNoBlankLine(t *testing.T) {
	out := bodyOf(
		claudecode.Item{Kind: "tool", TS: at(0), Name: "Bash", ID: "a", Full: "ls"},
		claudecode.Item{Kind: "text", TS: at(1), Text: "hi"},
		claudecode.Item{Kind: "result", TS: at(2), ID: "a", Text: "x"},
		claudecode.Item{Kind: "text", TS: at(3), Text: "bye"},
	)
	for i := 1; i < len(out); i++ {
		if out[i] == "" && out[i-1] == "" {
			t.Errorf("double blank line: %q", out)
		}
	}
}

func bodyOf(its ...claudecode.Item) []string {
	return body(its, Opts{Width: 40, Color: true, Wrap: true})
}
