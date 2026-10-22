package bd

import (
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// The merge sha behind a container's empty trailer commit is the hold Task's
// "Merge sha:" line, found by the newest Landing's recorded MR.
func TestResolveMergeSha(t *testing.T) {
	landing := func(body string) Bead {
		return Bead{ID: "l", Kind: contract.KindLanding, Status: "closed", Description: body, Closed: time.Unix(1, 0)}
	}
	hold := func(status, body string) Bead {
		return Bead{ID: "h", Kind: contract.KindTask, Status: status, Description: body}
	}
	body := "Merged MR: !7\nMerge sha: abc123\n"
	for _, c := range []struct {
		name      string
		landings  []Bead
		blockers  []Bead
		want      string
		wantEmpty string // a substring of the reason when no sha is found
	}{
		{"hold found closed", []Bead{landing("MR: !7\n")}, []Bead{hold("closed", body)}, "abc123", ""},
		{"hold found open", []Bead{landing("MR: !7\n")}, []Bead{hold("open", body)}, "abc123", ""},
		{"a nested-form MR line keys the same hold", []Bead{landing("MR: g/p!7\n")}, []Bead{hold("open", body)}, "abc123", ""},
		{"another MR's hold is skipped", []Bead{landing("MR: !7\n")}, []Bead{hold("open", "Merged MR: !8\nMerge sha: zzz\n"), hold("closed", body)}, "abc123", ""},
		{"hold missing", []Bead{landing("MR: !7\n")}, nil, "", "hold"},
		{"Merge sha line missing", []Bead{landing("MR: !7\n")}, []Bead{hold("closed", "Merged MR: !7\n")}, "", "Merge sha"},
		{"no Landing", nil, []Bead{hold("open", body)}, "", "Landing"},
		{"no recorded MR", []Bead{landing("Commit: x\n")}, []Bead{hold("open", body)}, "", "MR"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, why := ResolveMergeSha(c.landings, c.blockers)
			if got != c.want {
				t.Errorf("sha = %q, want %q", got, c.want)
			}
			if c.want == "" && !strings.Contains(why, c.wantEmpty) {
				t.Errorf("reason = %q, want it to mention %q", why, c.wantEmpty)
			}
			if c.want != "" && why != "" {
				t.Errorf("reason = %q, want none on a hit", why)
			}
		})
	}
}
