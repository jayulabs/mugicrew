package session

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// SpawnFollower (the CLI triage path's spawn too) starts exactly when a
// destination is named, with D4's argv (Spec mc-11b8 FR21).
func TestSpawnFollower(t *testing.T) {
	orig := startFollower
	defer func() { startFollower = orig }()
	var got [][]string
	startFollower = func(_ string, args []string) error { got = append(got, args); return nil }
	attrs := []any{"bead", "b", "session", "s", "profile", "p"}
	SpawnFollower(attrs, contract.Metered{}, "/log/events.jsonl")
	if len(got) != 0 {
		t.Fatalf("spawned without a destination: %v", got)
	}
	SpawnFollower(attrs, contract.Metered{ContinuedIn: "d", Last: contract.Cost{Known: true, USD: 2}}, "/log/events.jsonl")
	if len(got) != 1 {
		t.Fatalf("spawns = %v", got)
	}
	argv := strings.Join(got[0], " ")
	for _, want := range []string{"hitl-follow", "--bead=b", "--session=s", "--profile=p", "--continued_in=d", `"USD":2`, "--exited_at=", "--events=/log/events.jsonl"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
}
