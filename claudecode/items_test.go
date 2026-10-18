package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestItemParser(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"2026-10-15T08:44:00Z","message":{"content":"the launch prompt"}}`,
		`{"type":"user","isMeta":true,"message":{"content":"meta"}}`,
		`{"type":"user","message":{"content":"<local-command-stdout>x</local-command-stdout>"}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":""},{"type":"text","text":" hi \n"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls\nwc"}},{"type":"tool_use","id":"t2","name":"X","input":{"a":1}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"out\n"},{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"bad"}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"},{"type":"text","text":"ignored"}]}}`,
		`{"type":"user","message":{"content":"second human turn"}}`,
		`{"type":"assistant","isApiErrorMessage":true,"message":{"content":[{"type":"text","text":"429"}]}}`,
		`{"type":"cost-state"}`,
		`not json`,
	}
	var p ItemParser
	got := p.Write([]byte(strings.Join(lines, "\n") + "\n{\"type\":\"user\",\"mess"))
	got = append(got, p.Write([]byte(`age":{"content":"torn"}}`+"\n"))...)
	var kinds []string
	for _, it := range got {
		kinds = append(kinds, it.Kind)
	}
	want := "prompt text tool tool result result interrupt human apierr human"
	if strings.Join(kinds, " ") != want {
		t.Fatalf("kinds = %v, want %s", kinds, want)
	}
	if got[0].Text != "the launch prompt" || got[1].Text != "hi" || got[2].Full != "ls\nwc" || got[3].Full != `{"a":1}` {
		t.Errorf("bodies wrong: %+v", got[:4])
	}
	if got[4].IsErr || got[4].ID != "t1" || !got[5].IsErr || got[5].Text != "bad" || got[9].Text != "torn" {
		t.Errorf("results wrong: %+v", got[4:])
	}
}

func TestItemsFindsTranscriptUnderConfigDir(t *testing.T) {
	writeTranscript(t, "other", "")
	if _, ok := Items("abc"); ok {
		t.Fatal("missing transcript reported found")
	}
	writeTranscript(t, "abc", `{"type":"user","message":{"content":"p"}}`) // no trailing newline
	if got, ok := Items("abc"); !ok || len(got) != 1 {
		t.Errorf("Items = %v, %v", got, ok)
	}
}

// writeSessions puts each id's transcript body in one fresh config dir.
func writeSessions(t *testing.T, bodies map[string]string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	os.MkdirAll(filepath.Join(dir, "projects", "p"), 0o755)
	for id, b := range bodies {
		os.WriteFile(filepath.Join(dir, "projects", "p", id+".jsonl"), []byte(b), 0o644)
	}
}

func uline(uuid, text string) string {
	return `{"type":"user","uuid":"` + uuid + `","message":{"content":"` + text + `"}}` + "\n"
}

func cont(to string) string {
	return `{"type":"cost-state"}` + "\n" + `{"type":"continued-in","continuedInSessionId":"` + to + `"}` + "\n"
}

func kindsOf(its []Item) string {
	var k []string
	for _, it := range its {
		k = append(k, it.Kind)
	}
	return strings.Join(k, " ")
}

func TestItemsFollowsContinuedIn(t *testing.T) {
	writeSessions(t, map[string]string{
		"src": uline("u1", "go") + uline("u2", "one") + cont("dst"),
		"dst": uline("u1", "go") + uline("u2", "one") + uline("u3", "two"),
	})
	got, ok := Items("src")
	if !ok || kindsOf(got) != "prompt human continued human" || got[2].Text != "dst" || got[3].Text != "two" {
		t.Errorf("items = %+v, %v", got, ok)
	}
}

func TestItemsFollowsChainAndStopsAtCycle(t *testing.T) {
	writeSessions(t, map[string]string{
		"a": uline("u1", "go") + cont("b"),
		"b": uline("u2", "x") + cont("c"),
		"c": uline("u3", "y") + cont("a"), // back to a: stops, no marker
	})
	got, _ := Items("a")
	if kindsOf(got) != "prompt continued human continued human" {
		t.Errorf("chain = %s", kindsOf(got))
	}
}

func TestItemsMissingDestinationEndsWithMarker(t *testing.T) {
	writeSessions(t, map[string]string{"a": uline("u1", "go") + cont("gone")})
	got, ok := Items("a")
	if !ok || kindsOf(got) != "prompt continued" {
		t.Errorf("items = %s, %v", kindsOf(got), ok)
	}
}

func TestTailFollowOfACycleEndsOnItsOwn(t *testing.T) {
	writeSessions(t, map[string]string{
		"a": uline("u1", "go") + cont("b"),
		"b": uline("u2", "x") + cont("a"),
	})
	tl := NewTail("a")
	tl.Poll()
	if !tl.Ended() {
		t.Error("A→B→A follow does not end")
	}
}
