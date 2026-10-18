package claudecode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// FollowWait is how long FollowHandoff waits (Spec mc-11b8 D5): Idle after the source's
// exit, re-armed whenever a destination's jobs/<id>/state.json changes, but never
// past Ceiling after it; Poll apart.
type FollowWait struct{ Idle, Ceiling, Poll time.Duration }

// Destination is where a followed handoff chain ended: Cost is the last session's
// final cost-state as cumulative totals (unknown with Reason "timeout" or
// "not_found"), Last the timestamp of its last timestamped line (zero when unread).
type Destination struct {
	Cost   contract.Cost
	Last   time.Time
	Reason string
}

// tline is the transcript line shapes FollowHandoff tells apart: a cost-state, a
// continued-in record, or any timestamped line.
type tline struct {
	costState
	Timestamp string `json:"timestamp"`
	To        string `json:"continuedInSessionId"`
}

// tailOf is the last line of the transcript at path and the last timestamp found.
func tailOf(path string) (last tline, ts time.Time) {
	b, _ := os.ReadFile(path)
	lines := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	json.Unmarshal(lines[len(lines)-1], &last)
	for i := len(lines) - 1; i >= 0 && ts.IsZero(); i-- {
		var l tline
		if json.Unmarshal(lines[i], &l) == nil {
			ts, _ = time.Parse(time.RFC3339Nano, l.Timestamp)
		}
	}
	return
}

// FollowHandoff polls (holding no file open) for the destination session's final
// cost-state, following a trailing continued-in down the chain (a visited set
// breaks a cycle). It reads only by session id under the config dir.
func FollowHandoff(first string, exitedAt time.Time, w FollowWait) Destination {
	cur, seen := first, map[string]bool{first: true}
	deadline, ceiling := exitedAt.Add(w.Idle), exitedAt.Add(w.Ceiling)
	var d Destination
	found, stamp := false, ""
	for {
		if path, err := transcript(cur); err == nil {
			found = true
			last, ts := tailOf(path)
			if !ts.IsZero() {
				d.Last = ts
			}
			switch {
			case last.Type == "cost-state" && last.USD != nil:
				d.Cost = last.costState.cumulative()
				return d
			case last.Type == "continued-in" && last.To != "" && !seen[last.To]:
				cur, found, stamp = last.To, false, ""
				seen[cur] = true
				continue
			}
		}
		if dir, err := configDir(); err == nil {
			var st struct{ UpdatedAt json.RawMessage }
			b, _ := os.ReadFile(filepath.Join(dir, "jobs", cur, "state.json"))
			if json.Unmarshal(b, &st) == nil && string(st.UpdatedAt) != stamp {
				stamp = string(st.UpdatedAt)
				deadline = time.Now().Add(w.Idle)
			}
		}
		if now := time.Now(); now.After(deadline) || now.After(ceiling) {
			d.Reason = "timeout"
			if !found {
				d.Reason = "not_found"
			}
			return d
		}
		time.Sleep(w.Poll)
	}
}

// Remainder is final − base, per model too (a model the remainder never used is
// no entry): what a destination adds to the source's own spend, whose totals it
// carries on from.
func Remainder(final, base contract.Cost) contract.Cost {
	c := contract.Cost{Known: true, USD: final.USD - base.USD, APIms: final.APIms - base.APIms,
		Unpriced: final.Unpriced, Models: map[string]contract.ModelCost{}}
	for id, m := range final.Models {
		p := base.Models[id]
		d := m.Sub(p)
		if d != (contract.ModelCost{}) {
			c.Models[id] = d
		}
	}
	return c
}
