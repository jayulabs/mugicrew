package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// Defog and Exec drawers end with the shared bead body, Initiative head rows included.
func TestDefogDrawerSharedBody(t *testing.T) {
	var calls []string
	m := commentModel(t, 0, &calls) // stubs listComments; the beads are replaced below
	beads, sess := defogFixture()
	for i := range beads {
		switch beads[i].ID {
		case "ini":
			beads[i].Description, beads[i].Notes, beads[i].CommentCount = "ini body", "ini notes", 2
		case "c":
			beads[i].Description, beads[i].Notes = chartBody, "chart notes"
		case "closed":
			beads[i].Description, beads[i].CloseReason = "closed q", "settled: use X"
		}
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	m = press(t, treesOpen(m), runeKey("2"))
	tab := m.tabAt(tabDefog).(defogTab)
	byID := func(id string) *bd.Bead { return tab.g.beadByID(id) }
	text := func(r dRow) string { return ansi.Strip(tab.rowText(r)) }

	if d := text(dRow{kind: dChild, b: byID("closed")}); !strings.Contains(d, "── DESCRIPTION") || !strings.Contains(d, "closed      ") || !strings.Contains(d, "· settled: use X") || strings.Contains(d, "reason") {
		t.Errorf("closed bead row:\n%s", d)
	}
	if d := text(dRow{kind: dChild, b: byID("closed")}); strings.Count(d, "settled: use X") != 1 {
		t.Errorf("close state stated more than once:\n%s", d)
	}
	d := text(dRow{kind: dChart, b: byID("c")})
	for _, w := range []string{"Destination", "Build the thing.", "Notes: 2 lines", "── NOTES", "chart notes"} {
		if !strings.Contains(d, w) {
			t.Errorf("chart row lacks %q:\n%s", w, d)
		}
	}
	if strings.Contains(d, "DESCRIPTION") {
		t.Errorf("chart row prints its description twice:\n%s", d)
	}
	if d := text(dRow{kind: dHead, init: byID("ini")}); !strings.Contains(d, "── DESCRIPTION") || !strings.Contains(d, "ini notes") || !strings.Contains(d, "COMMENTS (2)") {
		t.Errorf("dHead lacks the Initiative's body:\n%s", d)
	}
	if d := text(dRow{kind: dSum, chart: byID("c"), members: []*bd.Bead{byID("closed")}}); !strings.Contains(d, "— settled: use X") || strings.Contains(d, "──") {
		t.Errorf("dSum changed:\n%s", d)
	}

	// the fetch hook follows the head row's Initiative; summary rows fetch nothing
	m = defogAt(t, m, "i:ini")
	m = press(t, m, runeKey("d"))
	if len(calls) != 1 || calls[0] != "ini" {
		t.Errorf("dHead fetched %v, want [ini]", calls)
	}
}

func TestExecDrawerSharedBody(t *testing.T) {
	var calls []string
	beads := execDetailFixture()
	for i := range beads {
		switch beads[i].ID {
		case "i1":
			beads[i].Description, beads[i].Notes, beads[i].CommentCount = "ini body", "ini notes", 1
		case "cl":
			beads[i].Description, beads[i].CloseReason = "cl body", "done it"
		case "ap":
			beads[i].Design = "ap design"
		}
	}
	m, _ := execModel(t, beads)
	orig := listComments
	t.Cleanup(func() { listComments = orig })
	listComments = func(id string) ([]bd.Comment, error) { calls = append(calls, id); return nil, nil }
	m = unfolded(t, m)
	tab := m.tabAt(tabExecute).(execTab)
	byID := func(id string) *bd.Bead { return tab.g.beadByID(id) }
	text := func(r eRow) string { return ansi.Strip(tab.rowText(r)) }

	if d := text(eRow{kind: eClosed, b: byID("cl")}); !strings.Contains(d, "── DESCRIPTION") || !strings.Contains(d, "· done it") || strings.Contains(d, "state") || strings.Count(d, "done it") != 1 {
		t.Errorf("closed row:\n%s", d)
	}
	if d := text(eRow{kind: eMem, b: byID("ap")}); !strings.Contains(d, "state") || !strings.Contains(d, "── DESIGN") {
		t.Errorf("open row:\n%s", d)
	}
	if d := text(eRow{kind: eSec, init: byID("i1")}); !strings.Contains(d, "── DESCRIPTION") || !strings.Contains(d, "ini notes") || !strings.Contains(d, "COMMENTS (1)") {
		t.Errorf("Initiative row:\n%s", d)
	}
	if d := text(eRow{kind: eSum, cp: byID("cpA"), members: []*bd.Bead{byID("cl")}}); !strings.Contains(d, "— done it") || strings.Contains(d, "──") {
		t.Errorf("eSum changed:\n%s", d)
	}
	cp := byID("cpA")
	why := text(eRow{kind: eLanded, b: cp, members: []*bd.Bead{{ID: "land", CloseReason: "landed ok"}}})
	if !strings.Contains(why, "Landing land closed landed ok") {
		t.Errorf("eLanded why dropped:\n%s", why)
	}

	m = execAt(t, m, "i:i1")
	m = press(t, m, runeKey("d"))
	if len(calls) != 1 || calls[0] != "i1" {
		t.Errorf("Initiative row fetched %v, want [i1]", calls)
	}
	m = execAt(t, m, "cpA/closed")
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	if len(calls) != 1 {
		t.Errorf("eSum row fetched: %v", calls)
	}
}
