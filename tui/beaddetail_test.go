package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func TestBeadDetailIsDrawerTextWithoutLaunchPreview(t *testing.T) {
	beads := mdBeads()
	orig := listOpen
	t.Cleanup(func() { listOpen = orig })
	listOpen = func() ([]bd.Bead, error) { return beads, nil }

	const w = 60
	got, err := BeadDetail(nil, nil, "md", w)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ansi.Strip(got), "ON LAUNCH") {
		t.Errorf("pane text has the launch preview:\n%s", got)
	}
	m := sized(t, w, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	g := m.tabAt(tabGraph).(graphTab)
	g.sel = "md"
	g.rollups = map[string]rollupEntry{"md": {loaded: true, notSpine: true}} // what the pane filled
	if want := lipgloss.NewStyle().Width(drawerContentW(w)).Render(g.detailBody()); got != want {
		t.Errorf("pane text != drawer body:\n%q\nwant\n%q", got, want)
	}
	if full := g.detailText(); !strings.HasPrefix(full, g.detailBody()) || !strings.HasSuffix(ansi.Strip(full), ansi.Strip(g.launchPreview(g.selected()))) {
		t.Errorf("drawer text is not body then launch preview:\n%s", full)
	}
	for _, l := range strings.Split(got, "\n") {
		if lipgloss.Width(l) > w {
			t.Errorf("line wider than %d: %q", w, l)
		}
	}
	if strings.Contains(ansi.Strip(got), "**renderer**") {
		t.Errorf("description not rendered as Markdown:\n%s", got)
	}
}

func TestBeadDetailErrors(t *testing.T) {
	orig := listOpen
	t.Cleanup(func() { listOpen = orig })
	listOpen = func() ([]bd.Bead, error) { return mdBeads(), nil }
	if _, err := BeadDetail(nil, nil, "nope", 60); err == nil {
		t.Error("unknown id: want an error")
	}
	listOpen = func() ([]bd.Bead, error) { return nil, errors.New("bd down") }
	if _, err := BeadDetail(nil, map[string]contract.Crewmate{}, "md", 60); err == nil || !strings.Contains(err.Error(), "bd down") {
		t.Errorf("load failure: err = %v", err)
	}
}

func TestBeadDetailShowsComments(t *testing.T) {
	beads := mdBeads()
	beads[0].CommentCount = 1
	id := beads[0].ID
	origO, origC := listOpen, listComments
	t.Cleanup(func() { listOpen, listComments = origO, origC })
	listOpen = func() ([]bd.Bead, error) { return beads, nil }
	listComments = func(string) ([]bd.Comment, error) {
		return []bd.Comment{{Author: "Franky", Text: "pane comment", Created: time.Now()}}, nil
	}
	got, err := BeadDetail(nil, nil, id, 60)
	if err != nil {
		t.Fatal(err)
	}
	if s := ansi.Strip(got); !strings.Contains(s, "COMMENTS (1)") || !strings.Contains(s, "pane comment") {
		t.Errorf("pane has no comments:\n%s", s)
	}
}

func TestBeadDetailPaneHasRealRollup(t *testing.T) {
	beads := mdBeads()
	orig := listOpen
	t.Cleanup(func() { listOpen = orig })
	listOpen = func() ([]bd.Bead, error) { return beads, nil }
	got, err := BeadDetail(nil, nil, "md", 60)
	if err != nil {
		t.Fatal(err)
	}
	if v := ansi.Strip(got); !strings.Contains(v, "ROLLUP") || strings.Contains(v, "loading…") {
		t.Errorf("pane rollup missing or loading:\n%s", v)
	}
}
