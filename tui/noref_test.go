package tui

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
)

func withRefOn(m Model, on bool) Model {
	m.initiatives = cfglayer.Initiatives{Required: on, ExternalRef: on}
	return m
}

// The marker shows on an unclosed Initiative with a blank ref, beside no body yet, and
// never off-setting, on a closed one, or on one that has a ref.
func TestNoRefMarker(t *testing.T) {
	bs := initsFixture()
	bs[1].ExternalRef = "PROJ-2" // Bravo has one
	bs[0].Description = "seed"   // Alpha is bodyless too
	m := initsModel(t, bs, nil)
	if v := initsBody(m); strings.Contains(v, "no ref") {
		t.Errorf("marker off-setting:\n%s", v)
	}
	v := initsBody(withRefOn(m, true))
	if n := strings.Count(v, "no ref"); n != 3 { // Alpha, in-progress Charlie, Paused one; not Bravo or the closed
		t.Errorf("want 3 markers, got %d:\n%s", n, v)
	}
	if !strings.Contains(v, "no body yet · no ref") {
		t.Errorf("marker not after no body yet:\n%s", v)
	}
	g := graphData{extRef: true}
	closed := bd.Bead{Kind: contract.KindInitiative, Status: "closed"}
	spaced := bd.Bead{Kind: contract.KindInitiative, Status: "open", ExternalRef: "  "}
	if g.noRef(&closed) || !g.noRef(&spaced) {
		t.Error("noRef: closed must not, whitespace ref must")
	}
}

func refForm(t *testing.T) Model {
	t.Helper()
	return pressN(t, withRefOn(initsModel(t, nil, nil), true), "i")
}

func TestInitiativeFormRefField(t *testing.T) {
	// off-setting: no ref field
	off := pressN(t, initsModel(t, nil, nil), "i")
	if strings.Contains(view(off), "ref:") {
		t.Errorf("ref field off-setting:\n%s", view(off))
	}

	// required: a title but no ref is refused before any create
	seam(t, &newInitiative, func(string, string, string) (string, error) { t.Fatal("filed"); return "", nil })
	m := press(t, typeText(t, refForm(t), "Seed"), ctrlS())
	if len(m.modals) != 1 || !strings.Contains(view(m), "ref required") {
		t.Errorf("blank ref:\n%s", view(m))
	}
	m = press(t, refForm(t), ctrlS())
	if !strings.Contains(view(m), "title required") {
		t.Errorf("empty title:\n%s", view(m))
	}

	// unique: a clash flashes in the form and files nothing
	seam(t, &initiativesByRef, func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "mc-old"}}, nil })
	m = typeText(t, refForm(t), "Seed")
	m = typeText(t, press(t, m, tabKey()), " PROJ-1 ")
	m = press(t, m, ctrlS())
	if len(m.modals) != 1 || !strings.Contains(view(m), "mc-old") {
		t.Errorf("clash:\n%s", view(m))
	}
}

func TestInitiativeFormRefTrimmedAndPassed(t *testing.T) {
	var got string
	seam(t, &initiativesByRef, func(string) ([]bd.Bead, error) { return nil, nil })
	seam(t, &newInitiative, func(_, _, ref string) (string, error) { got = ref; return "i1", nil })
	seam(t, &updateBead, func(string, []string) error { return errors.New("no update expected") })
	m := typeText(t, refForm(t), "Seed")
	m = typeText(t, press(t, m, tabKey()), " PROJ-1 ")
	press(t, m, ctrlS())
	if got != "PROJ-1" {
		t.Errorf("ref = %q, want PROJ-1", got)
	}
}
