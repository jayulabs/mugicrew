package jinbe

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
)

// bodyStore is a one-bead fake of bd's body: showBead reads it, updateBody writes it.
type bodyStore struct {
	body    string
	writes  int
	showErr error
	putErr  error
}

func (s *bodyStore) stub(t *testing.T, id string) {
	t.Helper()
	oShow, oUpdate := showBead, updateBody
	t.Cleanup(func() { showBead, updateBody = oShow, oUpdate })
	showBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: id, Description: s.body}, s.showErr
	}
	updateBody = func(_ string, args []string) error {
		if s.putErr != nil {
			return s.putErr
		}
		s.writes++
		s.body = args[1]
		return nil
	}
}

func countEvents(evs []eventlogtest.Event, name string) int {
	return strings.Count(strings.Join(eventlogtest.Names(evs), " ")+" ", name+" ")
}

func TestHoldRateLimitedMarker(t *testing.T) {
	rl := fmt.Errorf("read: %w", forge.ErrRateLimited)
	other := errors.New("boom")
	for _, c := range []struct {
		name       string
		body       string
		err        error
		wantErr    error
		wantEvents int
		wantWrites int
		wantLine   bool
	}{
		{"a: no line, 429", "MR: !7\n", rl, nil, 1, 1, true},
		{"b: line, 429", "MR: !7\nRate limited: logged\n", rl, nil, 0, 0, true},
		{"c: line, success", "MR: !7\nRate limited: logged\n", nil, nil, 0, 1, false},
		{"d: line, other error", "MR: !7\nRate limited: logged\n", other, other, 0, 1, false},
		{"e: no line, success", "MR: !7\n", nil, nil, 0, 0, false},
		{"no line, other error", "MR: !7\n", other, other, 0, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &bodyStore{body: c.body}
			s.stub(t, "ws-land")
			evs := eventlogtest.Capture(t)
			l := bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Description: c.body}
			if err := holdRateLimited(l, "land.rate_limited", c.err, nil); err != c.wantErr {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
			if n := countEvents(*evs, "land.rate_limited"); n != c.wantEvents {
				t.Errorf("%d land.rate_limited events, want %d", n, c.wantEvents)
			}
			if s.writes != c.wantWrites {
				t.Errorf("%d writes, want %d", s.writes, c.wantWrites)
			}
			if got := strings.Count(s.body, "Rate limited: logged") == 1; got != c.wantLine {
				t.Errorf("body = %q, want line present=%v exactly once", s.body, c.wantLine)
			}
			if !strings.Contains(s.body, "MR: !7") {
				t.Errorf("body = %q, lost the other line", s.body)
			}
		})
	}
}

// A failed marker read or write only warns; the hold still returns nil.
func TestHoldRateLimitedMarkerWriteFailureWarns(t *testing.T) {
	for name, set := range map[string]func(*bodyStore){
		"write":   func(s *bodyStore) { s.putErr = errors.New("bd down") },
		"re-read": func(s *bodyStore) { s.showErr = errors.New("bd down") },
	} {
		t.Run(name, func(t *testing.T) {
			s := &bodyStore{body: "MR: !7\n"}
			set(s)
			s.stub(t, "ws-land")
			evs := eventlogtest.Capture(t)
			l := bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Description: s.body}
			if err := holdRateLimited(l, "land.rate_limited", forge.ErrRateLimited, nil); err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if countEvents(*evs, "land.dedup_write_failed") != 1 || countEvents(*evs, "land.rate_limited") != 1 {
				t.Errorf("events = %v, want the 429 and one dedup_write_failed", eventlogtest.Names(*evs))
			}
		})
	}
}

// The marker builds on the bead as it is now, not on the tick-start copy: an earlier arm's
// MR status: line survives (FR 9).
func TestHoldRateLimitedMarkerKeepsAnEarlierArmsLine(t *testing.T) {
	s := &bodyStore{body: "MR: !7\nMR status: mergeable\n"}
	s.stub(t, "ws-land")
	eventlogtest.Capture(t)
	stale := bd.Bead{ID: "ws-land", Description: "MR: !7\n"}
	if err := holdRateLimited(stale, "land.rate_limited", forge.ErrRateLimited, nil); err != nil {
		t.Fatal(err)
	}
	if want := "MR: !7\nMR status: mergeable\nRate limited: logged\n"; s.body != want {
		t.Errorf("body = %q, want %q", s.body, want)
	}
}

// Two ticks share nothing but the body the first wrote: the 429 is logged once.
func TestHoldRateLimitedLogsOnceAcrossTicks(t *testing.T) {
	s := &bodyStore{body: "MR: !7\n"}
	s.stub(t, "ws-land")
	evs := eventlogtest.Capture(t)
	for range 3 {
		l := bd.Bead{ID: "ws-land", Description: s.body}
		if err := holdRateLimited(l, "land.rate_limited", forge.ErrRateLimited, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := countEvents(*evs, "land.rate_limited"); n != 1 || s.writes != 1 {
		t.Errorf("%d events %d writes over three ticks, want 1 and 1", n, s.writes)
	}
}

// A closed revert MR with a successor but no fix-forward pin is stranded: logged once,
// the Rollback gaining Stranded: logged and keeping it.
func TestRevertStrandedLogsOnceAcrossTicks(t *testing.T) {
	r := pendingRevert(forge.MR{State: "closed"})
	r.successors = []bd.Successor{{Description: "plain successor"}}
	stubRollbackSeams(t, r)
	evs := eventlogtest.Capture(t)
	tick(t, r)
	if n := countEvents(*evs, "revert.stranded"); n != 1 || strings.Count(r.body, "Stranded: logged") != 1 {
		t.Fatalf("%d stranded events, body %q, want one and the line", n, r.body)
	}
	tick(t, r)
	tick(t, r)
	if n := countEvents(*evs, "revert.stranded"); n != 1 || strings.Count(r.body, "Stranded: logged") != 1 {
		t.Errorf("%d stranded events, body %q after later ticks, want still one", n, r.body)
	}
	if !slices.Contains(strings.Split(r.body, "\n"), "MR: !9") {
		t.Errorf("body = %q, lost the MR line", r.body)
	}
}
