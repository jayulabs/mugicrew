package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// stripBody makes the drained Initiative "d" bodyless (open, no Done when).
func stripBody(bs []bd.Bead) []bd.Bead {
	bs[0].Description = "just a seed"
	return bs
}

func TestBodylessIdeateSurfaces(t *testing.T) {
	m, _ := drainedModel(t, stripBody(drainedBeads()), nil)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "no body yet · enter: Luffy settles its body with you, then cuts phase 1") {
		t.Errorf("cue row:\n%s", v)
	}
	if strings.Contains(v, "all members closed") {
		t.Errorf("bodyless still claims all members closed:\n%s", v)
	}
	if !strings.Contains(v, "· no body yet") {
		t.Errorf("combined row lacks the cue:\n%s", v)
	}
	m = press(t, ideaAt(t, m, "i:d"), runeKey(" "))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Drained · d · drained · no body yet") {
		t.Errorf("folded header trailer:\n%s", v)
	}
}

func TestBodiedDrainedKeepsAllMembersClosed(t *testing.T) {
	m, _ := drainedModel(t, drainedBeads(), nil)
	v := ansi.Strip(m.View())
	if strings.Contains(v, "no body yet") || !strings.Contains(v, "all members closed") {
		t.Errorf("bodied drained changed:\n%s", v)
	}
}

func TestBodylessInitiativesRowsAndFocus(t *testing.T) {
	bs := focusFixture()
	bs[len(bs)-1].Description = "seed" // ini2 "Empty one": bodyless, no member
	bs[0].Description = "seed"         // ini "Billing revamp": bodyless with members
	m, _ := focusModel(t, 160, 40, bs, map[string]session.Info{}, map[string]bool{})
	m = press(t, m, runeKey("5"))
	if got, want := line2Of(m, "Empty one"), "no body yet · its cut session settles Done when and Phases first"; got != want {
		t.Errorf("line 2 = %q, want %q", got, want)
	}
	if v := ansi.Strip(m.View()); strings.Count(v, "no body yet") < 3 { // two line-1 cues + one line 2
		t.Errorf("rows lack line-1 cues:\n%s", v)
	}
	if got := line2Of(m, "Billing revamp"); !strings.HasPrefix(got, "checkpoints ") {
		t.Errorf("a member exists, so counts return: %q", got)
	}
	f := gotoFocus(t, press(t, m, runeKey("j")))
	if h := lines(f.View()); !strings.Contains(ansi.Strip(strings.Join(h, "\n")), "· ini · no body yet") {
		t.Errorf("focus header lacks the suffix:\n%s", ansi.Strip(f.View()))
	}
}

func TestBodylessLaunchPreview(t *testing.T) {
	g := graphTab{}
	b := ini("x", "open", "X", 1)
	g.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
	b.Description = ""
	if got := ansi.Strip(g.launchPreview(&b)); got != "" { // the no-body-yet cue lives on the rows and focus header (above)
		t.Errorf("bodyless HITL preview %q, want none", got)
	}
}

func TestGraphDrawerEndsAtBodyOnLaunchableHITL(t *testing.T) {
	b := sig("s", "open", 1, "")
	b.Description = "The body text."
	g := graphTab{}
	g.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
	g.beads = []bd.Bead{b}
	g.sel, g.w = "s", 80
	v := ansi.Strip(g.detailText())
	if !strings.HasSuffix(v, "The body text.") || strings.Contains(v, "ON LAUNCH") {
		t.Errorf("drawer does not end at its body:\n%q", v)
	}
}

func TestGraphDrawerEndsAtBodyOnInitiative(t *testing.T) {
	for _, body := range []string{"The body text.", ""} {
		b := ini("i", "open", "I", 1)
		b.Description = body
		g := graphTab{}
		g.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
		g.beads = []bd.Bead{b}
		g.sel, g.w = "i", 80
		v := ansi.Strip(g.detailText())
		if strings.Contains(v, "CONTAINER") || strings.Contains(v, "ON LAUNCH") || strings.HasSuffix(v, "\n") {
			t.Errorf("body %q: drawer does not end at its body:\n%q", body, v)
		}
	}
}
