package tui

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func failingResearch() bd.Bead {
	return bd.Bead{ID: "fail", Kind: contract.KindResearch, Status: "open", Title: "Failing research", Labels: []string{"paused:failing"}, Created: epoch}
}

// refusing swaps in a dispatch guard that refuses (or not) and counts its reads.
func refusing(t *testing.T, err error) *int {
	t.Helper()
	orig := refuseLegacy
	t.Cleanup(func() { refuseLegacy = orig })
	n := new(int)
	refuseLegacy = func() error { *n++; return err }
	return n
}

var errLegacy = errors.New("2 bead(s) still labelled kind:task: run `mugicrew doctor fix`")

// While a kind:task bead remains, enter on a HITL row shows the refusal and claims,
// starts and switches nothing; with none it launches (ADR 0355).
func TestLegacyTaskRefusesHITLLaunch(t *testing.T) {
	n := refusing(t, errLegacy)
	m, calls := actModel(t)
	m, cmd := at(t, m, "sig", enterKey())
	m = settle(t, m, cmd)
	if s := statusOf(m); !strings.Contains(s, "mugicrew doctor fix") {
		t.Errorf("status = %q; want the refusal naming mugicrew doctor fix", s)
	}
	if *n == 0 || has(*calls, "hitlClaim") || has(*calls, "startSession") || has(*calls, "switchSession") {
		t.Errorf("guard reads=%d calls=%v; want a refusal that acted on nothing", *n, *calls)
	}

	refusing(t, nil)
	m, calls = actModel(t)
	m, cmd = at(t, m, "sig", enterKey())
	settle(t, m, cmd)
	if !has(*calls, "startSession sig") {
		t.Errorf("calls = %v; want the launch to proceed with no kind:task bead", *calls)
	}
}

// The loop menu's start shows the same refusal and runs no loop verb; with none it starts.
func TestLegacyTaskRefusesLoopStart(t *testing.T) {
	refusing(t, errLegacy)
	calls := fakeLoopVerb(t)
	m := sized(t, 100, 30)
	m.loop = loopOff
	m, _ = send(t, m, runeKey("o"))
	m, cmd := send(t, m, runeKey("s"))
	m = settle(t, m, cmd)
	if s := statusOf(m); !strings.Contains(s, "mugicrew doctor fix") {
		t.Errorf("status = %q; want the refusal naming mugicrew doctor fix", s)
	}
	if len(*calls) != 0 || m.loopStarting {
		t.Errorf("verbs = %v starting=%v; want nothing started", *calls, m.loopStarting)
	}

	refusing(t, nil)
	calls = fakeLoopVerb(t)
	m = sized(t, 100, 30)
	m.loop = loopOff
	m, _ = send(t, m, runeKey("o"))
	m, cmd = send(t, m, runeKey("s"))
	settle(t, m, cmd)
	if len(*calls) != 1 {
		t.Errorf("verbs = %v; want one start", *calls)
	}
}

// Dispatching an AFK row refuses before claiming or forking the run host.
func TestLegacyTaskRefusesRunNow(t *testing.T) {
	refusing(t, errLegacy)
	beads := append(actionBeads(), failingResearch())
	m, _ := actModel(t)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"live": true}, sessions: actionSessions()})
	m, cmd := at(t, m, "fail", enterKey())
	m = settle(t, m, cmd)
	if s := statusOf(m); !strings.Contains(s, "mugicrew doctor fix") {
		t.Errorf("status = %q; want the refusal naming mugicrew doctor fix", s)
	}
}
