package eventlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func put(t *testing.T, p, s string, appendTo bool) {
	t.Helper()
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendTo {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(p, flag, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	// distinct mtime so the Sig moves even on coarse clocks
	at := time.Now().Add(time.Duration(len(s)) * time.Hour)
	_ = os.Chtimes(p, at, at)
}

func msgs(evs []Event) (out []string) {
	for _, e := range evs {
		out = append(out, e.Msg)
	}
	return
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReadFromAppendReadOnceAndPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	put(t, p, `{"msg":"a"}`+"\n", false)
	evs, _, st, full, err := ReadFrom(p, State{})
	if err != nil || !full || !eq(msgs(evs), []string{"a"}) {
		t.Fatalf("first read: %v full=%v err=%v", msgs(evs), full, err)
	}
	put(t, p, `{"msg":"b"}`+"\n"+`{"msg":"c`, true)
	evs, sk, st, full, _ := ReadFrom(p, st)
	if full || sk != 0 || !eq(msgs(evs), []string{"b"}) {
		t.Fatalf("append: %v skipped=%d full=%v", msgs(evs), sk, full)
	}
	put(t, p, `"}`+"\n", true)
	evs, sk, st, _, _ = ReadFrom(p, st)
	if sk != 0 || !eq(msgs(evs), []string{"c"}) {
		t.Fatalf("completed line: %v skipped=%d", msgs(evs), sk)
	}
	evs, _, _, full, _ = ReadFrom(p, st)
	if full || len(evs) != 0 {
		t.Fatalf("unchanged re-read returned %v", msgs(evs))
	}
}

func TestReadFromRotationAndTruncationFallBackToFull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	put(t, p, `{"msg":"a"}`+"\n"+`{"msg":"b"}`+"\n", false)
	_, _, st, _, _ := ReadFrom(p, State{})

	// rotation: live becomes .1, a new live starts
	if err := os.Rename(p, p+".1"); err != nil {
		t.Fatal(err)
	}
	put(t, p, `{"msg":"c"}`+"\n", false)
	evs, _, st, full, _ := ReadFrom(p, st)
	want, _, _ := Read(p)
	if !full || !eq(msgs(evs), msgs(want)) || !eq(msgs(evs), []string{"a", "b", "c"}) {
		t.Fatalf("rotation: %v full=%v", msgs(evs), full)
	}

	// truncation: live shorter than the offset
	put(t, p, "", false)
	evs, _, _, full, _ = ReadFrom(p, st)
	if !full || !eq(msgs(evs), []string{"a", "b"}) {
		t.Fatalf("truncation: %v full=%v", msgs(evs), full)
	}
}

func TestReadFromCombinedMatchesRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	put(t, p+".1", `{"msg":"a"}`+"\n"+"junk\n", false)
	put(t, p, `{"msg":"b"}`+"\n", false)
	evs, sk, _, _, _ := ReadFrom(p, State{})
	want, wsk, _ := Read(p)
	if !eq(msgs(evs), msgs(want)) || sk != wsk {
		t.Fatalf("ReadFrom %v/%d, Read %v/%d", msgs(evs), sk, msgs(want), wsk)
	}
}

func TestReadFromTruncatedThenRegrownPastOffsetReadsFull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	put(t, p, `{"msg":"a"}`+"\n"+`{"msg":"b"}`+"\n", false)
	_, _, st, _, _ := ReadFrom(p, State{})
	put(t, p, `{"msg":"long-new-event-1"}`+"\n"+`{"msg":"long-new-event-2"}`+"\n", false)
	evs, _, _, full, _ := ReadFrom(p, st)
	if !full || !eq(msgs(evs), []string{"long-new-event-1", "long-new-event-2"}) {
		t.Fatalf("regrown: %v full=%v", msgs(evs), full)
	}
}
