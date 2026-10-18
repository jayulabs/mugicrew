package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessions(t *testing.T) {
	log := filepath.Join(t.TempDir(), "events.jsonl")
	rotated := `{"time":"2026-10-15T08:00:00Z","msg":"session.started","bead":"mc-a","session":"s1","kind":"build","crewmate":"franky"}
{"time":"2026-10-15T08:00:05Z","msg":"session.ended","bead":"mc-a","session":"s1","exit":0,"dur_ms":5000}
{"time":"2026-10-15T07:00:00Z","msg":"session.started","bead":"mc-old","session":"gone"}
`
	live := `{"time":"2026-10-15T09:00:00Z","msg":"hitl.launched","bead":"mc-a","session":"h1","kind":"signal","crewmate":"luffy"}
{"time":"2026-10-15T09:01:00Z","msg":"hitl.exited","bead":"mc-a","session":"h1","exit":0,"dur_ms":60000}
{"time":"2026-10-15T09:05:00Z","msg":"hitl.resumed","bead":"mc-a","session":"h1"}
{"time":"2026-10-15T08:30:00Z","msg":"session.started","bead":"mc-a","session":"s2","kind":"build","crewmate":"franky"}
{"time":"2026-10-15T08:31:00Z","msg":"session.failed","bead":"mc-a","session":"s2","exit":1,"dur_ms":60000,"result":{"result":"boom"},"stderr":"tail"}
{"time":"2026-10-15T08:40:00Z","msg":"hitl.launched","bead":"mc-a","kind":"signal"}
{"time":"2026-10-15T08:41:00Z","msg":"session.started","bead":"mc-b","session":"other"}
not json
`
	os.WriteFile(log+".1", []byte(rotated), 0o644)
	os.WriteFile(log, []byte(live), 0o644)

	ss, err := Sessions(log, "mc-a")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, " ") != "s1 s2 h1" {
		t.Fatalf("sessions = %v, want s1 s2 h1 (oldest first, no sessionless, no other bead)", ids)
	}
	s1, s2, h1 := ss[0], ss[1], ss[2]
	if s1.Mode != "AFK" || s1.Outcome != "ended" || s1.Dur != 5*time.Second || s1.Kind != "build" || s1.Crewmate != "franky" {
		t.Errorf("s1 = %+v", s1)
	}
	if s2.Outcome != "failed" || s2.Exit != 1 || s2.Result != "boom" || s2.Stderr != "tail" {
		t.Errorf("s2 = %+v", s2)
	}
	if h1.Mode != "HITL" || h1.Outcome != "unended" { // launched, exited, then resumed
		t.Errorf("h1 = %+v", h1)
	}
	if ss, _ := Sessions(log, "mc-old"); len(ss) != 1 || ss[0].Outcome != "unended" { // start with an end nowhere
		t.Errorf("rotated .1 bead = %v", ss)
	}
	if ss, _ := Sessions(log, "mc-none"); len(ss) != 0 {
		t.Errorf("unknown bead = %v", ss)
	}
}

func TestSessionsFromIndex(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "events.jsonl")
	idx := filepath.Join(dir, "sessions.jsonl")
	os.WriteFile(idx+".1", []byte(`{"time":"2026-10-01T08:00:00Z","msg":"session.started","bead":"mc-a","session":"old","kind":"build"}
{"time":"2026-10-01T08:00:05Z","msg":"session.ended","bead":"mc-a","session":"old","exit":0,"dur_ms":5000}
`), 0o644)
	os.WriteFile(idx, []byte(`{"time":"2026-10-15T08:00:00Z","msg":"session.started","bead":"mc-a","session":"s1"}
{"time":"2026-10-15T08:00:00Z","msg":"session.started","bead":"mc-a","session":"s1"}
{"time":"2026-10-15T08:01:00Z","msg":"session.failed","bead":"mc-a","session":"f1","exit":1,"result":{"result":"boom"},"stderr":"tail"}
`), 0o644)
	os.WriteFile(log, []byte(`{"time":"2026-10-15T08:00:00Z","msg":"session.started","bead":"mc-a","session":"s1"}
{"time":"2026-10-15T08:00:09Z","msg":"session.ended","bead":"mc-a","session":"s1","exit":0,"dur_ms":9000}
`), 0o644)
	ss, err := Sessions(log, "mc-a")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, " ") != "old s1 f1" {
		t.Fatalf("ids = %v", ids)
	}
	if o := ss[0]; o.Outcome != "ended" || o.Dur != 5*time.Second || o.Kind != "build" {
		t.Errorf("index-only = %+v", o)
	}
	if s1 := ss[1]; s1.Outcome != "ended" || s1.Dur != 9*time.Second {
		t.Errorf("index+log = %+v", s1)
	}
	if f := ss[2]; f.Outcome != "failed" || f.Result != "boom" || f.Stderr != "tail" {
		t.Errorf("failed from index = %+v", f)
	}
}
