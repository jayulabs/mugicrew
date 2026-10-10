package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func closingBeads() []bd.Bead {
	return []bd.Bead{
		{ID: "ep", Kind: contract.KindChart, Container: true, Title: "Epic", Status: "open"},
		{ID: "cl", Kind: contract.KindSpec, Parent: "ep", Status: "closed", Title: "Closing spec"},
	}
}

func loadSessions(t *testing.T, m Model, sessions map[string]session.Info) Model {
	t.Helper()
	next, _ := m.Update(loadedMsg{beads: closingBeads(), sessions: sessions})
	return next.(Model)
}

// An alive Closing session reads closing…, not done — /exit, and l and x are
// refused on it (Spec FR31–32, FR37; ADR 0108 §2, §5).
func TestClosingRowRefusesAttachAndDrop(t *testing.T) {
	m := New(nil, nil)
	m.w, m.h, m.hideClosedRoots = 120, 40, false
	m = loadSessions(t, m, map[string]session.Info{"cl": {State: session.Alive, Session: "@1", Closing: true}})
	if suf := ansi.Strip(m.rowSuffix(m.beadByID("cl"))); !strings.HasSuffix(suf, "  closing…") || strings.Contains(suf, "/exit") {
		t.Errorf("Closing row suffix = %q, want closing… and no done — /exit", suf)
	}
	m.selGraph = "cl"
	for _, k := range []string{"l", "x"} {
		got := press(t, m, k)
		if got.status != "cl is closing — wait" || got.editMode != editNone {
			t.Errorf("%s on a Closing row: status %q mode %d, want refused", k, got.status, got.editMode)
		}
	}
}

// Closing → dead flashes close failed once; the seeding read flashes nothing;
// Closing → gone flashes nothing new here (hitl.exited's finished covers it)
// (Spec FR34–35).
func TestClosingFailedFlashOnce(t *testing.T) {
	closing := map[string]session.Info{"cl": {State: session.Alive, Closing: true}}
	dead := map[string]session.Info{"cl": {State: session.Exited, RC: 1, Closing: true}}

	m := New(nil, nil)
	if m = loadSessions(t, m, dead); m.status != "" {
		t.Fatalf("the seeding read must not flash: %q", m.status)
	}
	m = loadSessions(t, New(nil, nil), closing)
	m.editMode, m.status = editConfirmDrop, "drop?"
	if m = loadSessions(t, m, dead); m.status != "drop?" {
		t.Fatalf("an open confirm keeps its prompt; got %q", m.status)
	}
	m.editMode = editNone
	if m = loadSessions(t, m, dead); m.status != "✕ cl close failed — resume or drop" {
		t.Fatalf("Closing → dead status = %q, want the close failed flash", m.status)
	}
	m.status = ""
	if m = loadSessions(t, m, dead); m.status != "" {
		t.Errorf("the flash must not repeat: %q", m.status)
	}

	m = loadSessions(t, New(nil, nil), closing)
	if m = loadSessions(t, m, nil); m.status != "" {
		t.Errorf("Closing → gone must not flash close failed: %q", m.status)
	}
}

// An alive, armed session reads auto-exit in <coarse>… in place of the done word
// and the plain alive look; launching…, a pending verb and closing… still win, and
// an unarmed row is unchanged (Spec D7, FR16; ADR 0111).
func TestAutoExitRowSuffix(t *testing.T) {
	armed := func(d time.Duration) session.Info {
		return session.Info{State: session.Alive, Waiting: session.WaitTurn, AutoExit: time.Now().Add(d)}
	}
	sig := &bd.Bead{ID: "cl", Kind: contract.KindSignal, Status: "in_progress"}
	suffix := func(m Model, b *bd.Bead) string { return ansi.Strip(m.rowSuffix(b)) }

	m := loadSessions(t, New(nil, nil), map[string]session.Info{"cl": armed(2*time.Minute + 30*time.Second)})
	if got := suffix(m, m.beadByID("cl")); !strings.HasSuffix(got, "  auto-exit in 2m…") || strings.Contains(got, "/exit") {
		t.Errorf("armed done row = %q, want auto-exit in 2m… over done — /exit", got)
	}
	if got := suffix(m, sig); !strings.HasSuffix(got, "  auto-exit in 2m…") || strings.Contains(got, "your turn") {
		t.Errorf("armed Signal row = %q, want auto-exit in 2m… over the alive look", got)
	}
	m = loadSessions(t, New(nil, nil), map[string]session.Info{"cl": armed(45*time.Second + 500*time.Millisecond)})
	if got := suffix(m, sig); !strings.HasSuffix(got, "  auto-exit in 45s…") {
		t.Errorf("armed row = %q, want auto-exit in 45s…", got)
	}

	launching, pending := m, m
	launching.launching = map[string]bool{"cl": true}
	pending.pending = map[string]string{"cl": "pausing"}
	for want, mm := range map[string]Model{"launching…": launching, "pausing…": pending} {
		if got := suffix(mm, sig); !strings.HasSuffix(got, "  "+want) {
			t.Errorf("row = %q, want %s to beat auto-exit", got, want)
		}
	}
	closing := armed(time.Minute)
	closing.Closing = true
	m = loadSessions(t, New(nil, nil), map[string]session.Info{"cl": closing})
	if got := suffix(m, sig); !strings.HasSuffix(got, "  closing…") {
		t.Errorf("Closing armed row = %q, want closing… to beat auto-exit", got)
	}

	m = loadSessions(t, New(nil, nil), map[string]session.Info{"cl": {State: session.Alive}})
	if got := suffix(m, m.beadByID("cl")); !strings.HasSuffix(got, "  done — /exit") {
		t.Errorf("unarmed done row = %q, want done — /exit unchanged", got)
	}
}
