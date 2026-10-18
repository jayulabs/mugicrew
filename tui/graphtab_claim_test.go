package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

const wantElse = "%s is claimed by zz-not-me — not yours to launch; bd unclaim %s --force once zz-not-me is gone"

// x on another actor's claim refuses in one line and releases nothing (Spec FR30, D6);
// the preview gives the same line.
func TestXOnClaimHeldElsewhereRefuses(t *testing.T) {
	t.Cleanup(bd.PinActor("me"))
	for _, c := range []struct {
		id   string
		kind contract.Kind
		cont bool
	}{{"se", contract.KindSignal, false}, {"ie", contract.KindInitiative, true}} {
		m, calls := actModel(t)
		bs := append(actionBeads(), bd.Bead{ID: c.id, Kind: c.kind, Container: c.cont, Status: "in_progress", Assignee: "zz-not-me", Title: "Elsewhere", Created: epoch})
		m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{"live": true}, sessions: actionSessions()})
		want := fmt.Sprintf(wantElse, c.id, c.id)
		if d := ansi.Strip(selectBead(t, m, c.id).tabAt(m.tab).(graphTab).detailText()); !strings.Contains(d, want) {
			t.Errorf("%s: preview lacks %q:\n%s", c.id, want, d)
		}
		m, cmd := at(t, m, c.id, runeKey("x"))
		m = settle(t, m, cmd)
		if s := statusOf(m); s != want {
			t.Errorf("%s: status = %q, want %q", c.id, s, want)
		}
		if has(*calls, "dropSession") || has(*calls, "recoverBead") || has(*calls, "assessRecover") || has(*calls, "hitlUnclaim") || len(m.modals) != 0 {
			t.Errorf("%s: acted on another actor's claim: %v", c.id, *calls)
		}
	}
}

func TestClaimLineByHolder(t *testing.T) {
	t.Cleanup(bd.PinActor("me"))
	g := graphData{}
	other := &bd.Bead{ID: "s", Kind: contract.KindSignal, Status: "in_progress", Assignee: "zz-not-me"}
	if w := g.launchWhy(other); !strings.Contains(w, "claimed by zz-not-me") || !strings.Contains(w, "bd unclaim s --force") {
		t.Errorf("launchWhy = %q", w)
	}
	if l := g.xLabel(other); l != "" {
		t.Errorf("xLabel = %q, want none", l)
	}
	mine := &bd.Bead{ID: "s", Kind: contract.KindSignal, Status: "in_progress"}
	if l := g.xLabel(mine); l != "recover" {
		t.Errorf("xLabel = %q, want recover", l)
	}
}

func TestXLabelStrandedInitiative(t *testing.T) {
	t.Cleanup(bd.PinActor("me"))
	g := graphData{}
	mk := func(status, who string) *bd.Bead {
		return &bd.Bead{ID: "i", Kind: contract.KindInitiative, Container: true, Status: status, Assignee: who}
	}
	for _, c := range []struct {
		b    *bd.Bead
		want string
	}{
		{mk("in_progress", ""), "recover"},
		{mk("in_progress", "zz-not-me"), ""},
		{mk("open", ""), ""},
	} {
		if l := g.xLabel(c.b); l != c.want {
			t.Errorf("xLabel(%s %q) = %q, want %q", c.b.Status, c.b.Assignee, l, c.want)
		}
	}
}
