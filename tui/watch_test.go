package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
)

func started(bead, session string) eventlog.Event {
	return eventlog.Event{Msg: "session.started", Bead: bead, Raw: map[string]any{"session": session}}
}

func stubWatch(t *testing.T, watchErr error) *[]string {
	t.Helper()
	var opened []string
	b := watchRun
	t.Cleanup(func() { watchRun = b })
	watchRun = func(bead string) error { opened = append(opened, bead); return watchErr }
	return &opened
}

// Enter on a live AFK row is refused with a pointer to t and x, and is not a hint; a dead row still reaches runNow.
func TestEnterOnLiveAFKRowRefuses(t *testing.T) {
	opened := stubWatch(t, nil)
	m, calls := actModel(t)
	if h := hintsOf(selectBead(t, m, "live")); strings.Contains(h, "enter") {
		t.Errorf("live hints %q name enter", h)
	}
	m, cmd := at(t, m, "live", enterKey())
	if cmd != nil || statusOf(m) != "live is running — t shows it · x aborts it" || len(*opened) != 0 || has(*calls, "readyFrontier") {
		t.Errorf("cmd=%v status=%q opened=%v calls=%v", cmd != nil, statusOf(m), *opened, *calls)
	}

	m, _ = actModel(t)
	m, cmd = at(t, m, "dead", enterKey())
	if cmd != nil || !strings.Contains(statusOf(m), "dead's effort is not running") || len(*opened) != 0 {
		t.Errorf("dead row: cmd=%v status=%q opened=%v", cmd != nil, statusOf(m), *opened)
	}
}

// t on a bead row calls Watch with the bead id on every bead tab, the focus view and the
// Sessions modal; a failure reads transcript: <err>; header rows have no t.
func TestTranscriptKey(t *testing.T) {
	opened := stubWatch(t, nil)
	m, _ := actModel(t)
	m, cmd := at(t, m, "live", runeKey("t"))
	m = settle(t, m, cmd)
	if len(*opened) != 1 || (*opened)[0] != "live" || statusOf(m) != "transcript live" {
		t.Errorf("opened=%v status=%q", *opened, statusOf(m))
	}
	// dead rows too: t does no per-row session check
	m, _ = actModel(t)
	m, cmd = at(t, m, "dead", runeKey("t"))
	settle(t, m, cmd)
	if len(*opened) != 2 || (*opened)[1] != "dead" {
		t.Errorf("opened=%v", *opened)
	}
	if h := hintsOf(selectBead(t, m, "dead")); !strings.Contains(h, "x ") && !strings.Contains(h, "t transcript") {
		t.Errorf("bead row hints %q lack t transcript", h)
	}

	stubWatch(t, errors.New("no server"))
	m, _ = actModel(t)
	m, cmd = at(t, m, "live", runeKey("t"))
	m = settle(t, m, cmd)
	if !strings.Contains(statusOf(m), "transcript: no server") {
		t.Errorf("status %q", statusOf(m))
	}
}

// firstBead moves down until the selection is a bead row.
func firstBead(t *testing.T, m Model) *bd.Bead {
	t.Helper()
	for range 40 {
		if b := m.selection(); b != nil {
			return b
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatal("no bead row")
	return nil
}

// t dispatches Watch with the bead id from each bead tab, the focus view and the
// Sessions modal; header rows have neither the hint nor an action.
func TestTranscriptOnEveryBinding(t *testing.T) {
	opened := stubWatch(t, nil)
	check := func(name string, m Model, want string) {
		t.Helper()
		*opened = nil
		m, cmd := send(t, m, runeKey("t"))
		settle(t, m, cmd)
		if len(*opened) != 1 || (*opened)[0] != want {
			t.Errorf("%s: opened %v, want [%s]", name, *opened, want)
		}
	}
	m, _ := defaultFocus(t)
	for _, k := range []string{"1", "2", "3", "4", "5"} {
		n := press(t, m, runeKey(k))
		// walk to a bead row, tolerating the cursor starting on a header
		for range 40 {
			if n.selection() != nil {
				break
			}
			n = press(t, n, runeKey("j"))
		}
		b := n.selection()
		if b == nil {
			continue // a tab whose fixture lists no bead row
		}
		check("tab "+k, n, b.ID)
		if h := ansi.Strip(n.hintsView()); !strings.Contains(h, "t transcript") {
			t.Errorf("tab %s hints %q lack t transcript", k, h)
		}
	}
	f, _ := onInitsRow(t)
	f, _ = send(t, f, enterKey())
	if !focused(f) {
		t.Fatal("no focus view")
	}
	check("focus", f, firstBead(t, f).ID)

	s := press(t, sessionsModel(t), runeKey("S"))
	check("sessions modal", s, s.modals[0].ids[s.modals[0].cur])
	if len(s.modals) != 1 {
		t.Errorf("modal stack %d", len(s.modals))
	}
}

// t transcript sits right after x on a bead row, and ? lists it with its HITL/AFK note.
func TestTranscriptHintsAndRef(t *testing.T) {
	m, _ := actModel(t)
	m = selectBead(t, m, "live")
	h := ansi.Strip(m.hintsView())
	if i, j := strings.Index(h, "x "), strings.Index(h, "t transcript"); j < 0 || i < 0 || j < i {
		t.Errorf("hints %q: want t transcript after x", h)
	}
	if v := ansi.Strip(press(t, m, runeKey("?")).View()); !strings.Contains(v, "transcript (HITL/AFK)") {
		t.Errorf("? lacks t:\n%s", v)
	}
	// a header row (Initiatives' first row) hints no t
	i, _ := onInitsRow(t)
	i = press(t, i, runeKey("k"))
	if i.selection() != nil || strings.Contains(ansi.Strip(i.hintsView()), "transcript") {
		t.Errorf("header row hints %q", ansi.Strip(i.hintsView()))
	}
	m, _ = actModel(t)
}
