package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// evLines is events lo..hi-1 as log lines, padded so a few hundred cross a chunk.
func evLines(lo, hi int) string {
	var b strings.Builder
	for i := lo; i < hi; i++ {
		fmt.Fprintf(&b, `{"msg":"e%d","pid":"1","pad":"%s"}`+"\n", i, strings.Repeat("x", i%900))
	}
	return b.String()
}

func seq(lo, hi int) (out []string) {
	for i := lo; i < hi; i++ {
		out = append(out, fmt.Sprintf("e%d", i))
	}
	return
}

func TestReadNewestAcrossChunksAndIntoDotOne(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	put(t, p+".1", evLines(0, 600), false)
	put(t, p, evLines(600, 1200)+`{"msg":"part`, false)
	evs, sk, st, cur, err := ReadNewest(p, 900)
	if err != nil || sk != 0 || !eq(msgs(evs), seq(300, 1200)) {
		t.Fatalf("got %d events (%v), skipped %d, err %v", len(evs), msgs(evs)[:1], sk, err)
	}
	if cur.File != 0 || cur.Done || cur.Off == 0 {
		t.Fatalf("cursor %+v", cur)
	}
	// the head State extends with no gap and no duplicate
	put(t, p, "\"}\n"+evLines(1200, 1203), true)
	tail, _, _, full, _ := ReadFrom(p, st)
	if full || !eq(msgs(tail), append([]string{"part"}, seq(1200, 1203)...)) {
		t.Fatalf("tail %v full=%v", msgs(tail), full)
	}
	// older pages walk back to the start of .1 and end Done
	sig := st.Sig
	var all []Event
	for !cur.Done {
		var older []Event
		older, _, cur, sig, err = ReadOlder(p, cur, sig, 250)
		if err != nil {
			t.Fatal(err)
		}
		all = append(older, all...)
	}
	if !eq(msgs(all), seq(0, 300)) {
		t.Fatalf("older pages %d events, first %v", len(all), msgs(all)[:1])
	}
}

func TestReadNewestShortLogsAndMissingDotOne(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	if evs, _, _, cur, err := ReadNewest(p, 5); err != nil || len(evs) != 0 || !cur.Done {
		t.Fatalf("no files: %v %+v %v", evs, cur, err)
	}
	put(t, p, evLines(0, 3)+"junk\n", false)
	evs, sk, _, cur, _ := ReadNewest(p, 5)
	if !eq(msgs(evs), seq(0, 3)) || sk != 1 || !cur.Done {
		t.Fatalf("short live, no .1: %v skipped=%d %+v", msgs(evs), sk, cur)
	}
	evs, _, _, cur, _ = ReadNewest(p, 2)
	if !eq(msgs(evs), seq(1, 3)) || cur.Done || cur.File != 1 {
		t.Fatalf("n=2: %v %+v", msgs(evs), cur)
	}
	older, _, cur, _, _ := ReadOlder(p, cur, Stat(p), 5)
	if !eq(msgs(older), seq(0, 1)) || !cur.Done {
		t.Fatalf("older: %v %+v", msgs(older), cur)
	}
}

func TestReadOlderAcrossRotation(t *testing.T) {
	setup := func() (string, State, Cursor) {
		p := filepath.Join(t.TempDir(), "events.jsonl")
		put(t, p+".1", evLines(0, 50), false)
		put(t, p, evLines(50, 100), false)
		_, _, st, cur, _ := ReadNewest(p, 20)
		return p, st, cur
	}
	rotate := func(p string) {
		if err := os.Rename(p, p+".1"); err != nil {
			t.Fatal(err)
		}
		put(t, p, evLines(100, 110), false)
	}
	// a live cursor is the same offset in .1 now
	p, st, cur := setup()
	rotate(p)
	older, _, cur, _, _ := ReadOlder(p, cur, st.Sig, 10)
	if !eq(msgs(older), seq(70, 80)) {
		t.Fatalf("live cursor after rotation: %v", msgs(older))
	}
	// a .1 cursor is Done: that file is gone
	p, st, cur = setup()
	_, _, cur, sig, _ := ReadOlder(p, cur, st.Sig, 40) // reaches into .1
	if cur.File != 0 || cur.Done {
		t.Fatalf("cursor %+v", cur)
	}
	rotate(p)
	older, _, cur, _, _ = ReadOlder(p, cur, sig, 10)
	if len(older) != 0 || !cur.Done {
		t.Fatalf("stale .1 cursor: %v %+v", msgs(older), cur)
	}
}

func TestReadRotatedFollowsHead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	put(t, p+".1", evLines(0, 5), false)
	put(t, p, evLines(5, 10), false)
	_, _, st, _, _ := ReadNewest(p, 4)
	put(t, p, evLines(10, 12), true) // unread when the rotation hits
	if err := os.Rename(p, p+".1"); err != nil {
		t.Fatal(err)
	}
	put(t, p, evLines(12, 15), false)
	evs, _, next, ok, err := ReadRotated(p, st)
	if err != nil || !ok || !eq(msgs(evs), seq(10, 15)) {
		t.Fatalf("%v ok=%v err=%v", msgs(evs), ok, err)
	}
	if tail, _, _, full, _ := ReadFrom(p, next); full || len(tail) != 0 {
		t.Fatalf("next state re-reads %v full=%v", msgs(tail), full)
	}
	if _, _, _, ok, _ := ReadRotated(p, next); ok {
		t.Fatal("no rotation since next, want ok=false")
	}
}
