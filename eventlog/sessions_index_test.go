package eventlog

import (
	"os"
	"path/filepath"
	"testing"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestSessionIndexSink(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	idx := filepath.Join(dir, "sessions.jsonl")
	l := New(p, nil)
	start := `{"time":"2026-10-15T08:00:00Z","msg":"session.started","bead":"mc-a","session":"s1"}` + "\n"
	ended := `{"time":"2026-10-15T08:00:05Z","msg":"session.ended","bead":"mc-a","session":"s1","exit":0,"result":{"result":"x"}}` + "\n"
	failed := `{"time":"2026-10-15T08:01:00Z","msg":"session.failed","bead":"mc-a","session":"s2","exit":1,"result":{"result":"boom"},"stderr":"tail"}` + "\n"
	skip := []string{
		`{"msg":"hitl.continued","bead":"mc-a","session":"h1"}` + "\n",
		`{"msg":"session.started","bead":"mc-a"}` + "\n",
		`{"msg":"session.started","bead":"mc-a","session":""}` + "\n",
		`{"msg":"effort.started","bead":"mc-a","session":"s1"}` + "\n",
	}
	all := ""
	for _, s := range append([]string{start, ended, failed}, skip...) {
		l.Write([]byte(s))
		all += s
	}
	if got := readFile(t, p); got != all {
		t.Errorf("event log changed: %q", got)
	}
	got := readFile(t, idx)
	lns := lines(t, idx)
	if len(lns) != 3 || got[:len(start)] != start {
		t.Fatalf("index = %q", got)
	}
	if _, has := lns[1]["result"]; has || lns[1]["msg"] != "session.ended" || lns[1]["exit"] == nil {
		t.Errorf("ended should drop result only: %v", lns[1])
	}
	if got[len(got)-len(failed):] != failed {
		t.Errorf("failed should be copied byte for byte: %q", got)
	}
}

func TestSessionIndexFailsOpen(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	os.Mkdir(filepath.Join(dir, "sessions.jsonl"), 0o755) // a directory: unopenable as the index
	var warned []string
	l := New(p, func(s string) { warned = append(warned, s) })
	line := `{"msg":"session.started","bead":"mc-a","session":"s1"}` + "\n"
	if n, err := l.Write([]byte(line)); n != len(line) || err != nil {
		t.Errorf("Write = %d, %v", n, err)
	}
	if readFile(t, p) != line || len(warned) != 0 || l.Warning() != "" {
		t.Errorf("event log intact and silent: warned=%v", warned)
	}
}

func TestSessionIndexRotates(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "sessions.jsonl")
	os.WriteFile(idx, make([]byte, MaxSize), 0o644)
	line := `{"msg":"session.started","bead":"mc-a","session":"s1"}` + "\n"
	New(filepath.Join(dir, "events.jsonl"), nil).Write([]byte(line))
	if fi, err := os.Stat(idx + ".1"); err != nil || fi.Size() != MaxSize {
		t.Errorf(".1 = %v, %v", fi, err)
	}
	if readFile(t, idx) != line {
		t.Error("live index should be only the new line")
	}
}

func TestSessionIndexSeed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	idx := filepath.Join(dir, "sessions.jsonl")
	a := `{"msg":"session.started","bead":"mc-a","session":"s1"}` + "\n"
	b := `{"msg":"session.ended","bead":"mc-a","session":"s1","exit":0,"result":{"r":1}}` + "\n"
	c := `{"msg":"session.started","bead":"mc-b","session":"s2"}` + "\n"
	os.WriteFile(p+".1", []byte(a+`{"msg":"other"}`+"\n"+b), 0o644)
	os.WriteFile(p, []byte(c), 0o644)
	new := `{"msg":"session.started","bead":"mc-c","session":"s3"}` + "\n"
	New(p, nil).Write([]byte(new))
	ls := lines(t, idx)
	var got []string
	for _, m := range ls {
		got = append(got, m["session"].(string)+m["msg"].(string))
	}
	want := []string{"s1session.started", "s1session.ended", "s2session.started", "s3session.started"}
	if len(got) < 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[len(got)-1] != want[3] {
		t.Fatalf("index = %v", got)
	}
	if _, has := ls[1]["result"]; has {
		t.Error("seed should trim result")
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "sessions.jsonl.seed*")); len(entries) != 0 {
		t.Errorf("temp left: %v", entries)
	}
	if readFile(t, p) != c+new {
		t.Error("seed must not log events")
	}
}

func TestSessionIndexNoSeedAfterRotation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	os.WriteFile(p+".1", []byte(`{"msg":"session.started","bead":"mc-a","session":"s1"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sessions.jsonl.1"), nil, 0o644)
	New(p, nil).Write([]byte(`{"msg":"session.started","bead":"mc-b","session":"s2"}` + "\n"))
	if n := len(lines(t, filepath.Join(dir, "sessions.jsonl"))); n != 1 {
		t.Errorf("index lines = %d, want only the new record", n)
	}
}

func TestSessionIndexSeedRetries(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	idx := filepath.Join(dir, "sessions.jsonl")
	os.Mkdir(p+".1", 0o755) // unreadable as a log: the seed fails
	l := New(p, nil)
	l.Write([]byte(`{"msg":"session.started","bead":"mc-a","session":"s1"}` + "\n"))
	if _, err := os.Stat(idx); err == nil {
		t.Fatal("failed seed should leave the index absent")
	}
	os.Remove(p + ".1")
	l.Write([]byte(`{"msg":"session.started","bead":"mc-b","session":"s2"}` + "\n"))
	if n := len(lines(t, idx)); n < 2 {
		t.Errorf("index lines = %d", n)
	}
}
