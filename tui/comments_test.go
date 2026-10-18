package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// commentModel is a Graph-tab Model on one bead with n comments, listComments recording its calls.
func commentModel(t *testing.T, n int, calls *[]string) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	orig := listComments
	t.Cleanup(func() { listComments = orig })
	listComments = func(id string) ([]bd.Comment, error) {
		*calls = append(*calls, id)
		return []bd.Comment{{Author: "Franky", Text: "hello there", Created: time.Now().Add(-time.Hour)}}, nil
	}
	m := sized(t, 100, 40)
	m, _ = send(t, m, loadedMsg{beads: commentBeads(n), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	return selectBead(t, m, "b")
}

func commentBeads(n int) []bd.Bead {
	return []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship", Created: epoch},
		{ID: "b", Kind: contract.KindBuild, Status: "open", Title: "A build", Parent: "ini", CommentCount: n, Created: epoch},
	}
}

func TestCommentsFetchedOncePerOpenDrawer(t *testing.T) {
	var calls []string
	m := commentModel(t, 1, &calls)
	if len(calls) != 0 {
		t.Fatalf("closed drawer fetched: %v", calls)
	}
	m = press(t, m, runeKey("d")) // open the drawer: fetch, and a tick must not repeat it
	if len(calls) != 1 {
		t.Fatalf("open drawer: calls = %v", calls)
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "hello there") || !strings.Contains(got, "Franky · 1h") || !strings.Contains(got, "COMMENTS (1)") {
		t.Errorf("comments not shown:\n%s", got)
	}
	m, _ = send(t, m, tickMsg{})
	if len(calls) != 1 {
		t.Errorf("repeat updates refetched: %v", calls)
	}
	// a bumped count misses the (id, count) key
	m, cmd := send(t, m, loadedMsg{beads: commentBeads(2), live: map[string]bool{}, sessions: map[string]session.Info{}})
	settle(t, m, cmd)
	if len(calls) != 2 {
		t.Errorf("bumped count: calls = %v", calls)
	}
}

func TestCommentsNotFetchedForClosedDrawerOrZeroCount(t *testing.T) {
	var calls []string
	m := commentModel(t, 0, &calls)
	press(t, m, runeKey("d"))
	if len(calls) != 0 {
		t.Errorf("count 0 fetched: %v", calls)
	}
}

func TestF5RetriesCommentsError(t *testing.T) {
	var calls []string
	m := commentModel(t, 1, &calls)
	listComments = func(id string) ([]bd.Comment, error) { calls = append(calls, id); return nil, errors.New("bd down\nhint: retry") }
	m = press(t, m, runeKey("d"))
	if got := ansi.Strip(m.View()); !strings.Contains(got, "couldn't load comments: bd down hint: retry") {
		t.Fatalf("error line missing:\n%s", got)
	}
	m, _ = send(t, m, tickMsg{})
	if len(calls) != 1 {
		t.Fatalf("error entry refetched without f5: %v", calls)
	}
	press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if len(calls) != 2 {
		t.Errorf("f5 did not retry: %v", calls)
	}
}

func TestIdeateDetailShowsSharedBody(t *testing.T) {
	bs := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship", Notes: "init note", CommentCount: 1, Created: epoch},
		{ID: "s", Kind: contract.KindSignal, Status: "closed", Title: "A signal", Parent: "ini", Notes: "sig note", Labels: []string{"x"}, Created: epoch},
	}
	cm := map[commentKey]commentEntry{{"s", 1}: {cs: []bd.Comment{{Author: "Franky", Text: "c1", Created: time.Now()}}}}
	tab := ideaTab{ideaData: ideaData{beads: bs, gate: graphData{beads: bs, w: 100, comments: cm}, w: 100, h: 30}}
	got := ansi.Strip(tab.beadText(&bs[1]))
	for _, want := range []string{"parent      Ship\nlabels      x\n\n", "closed ", "── NOTES ", "sig note"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if got := ansi.Strip(tab.beadText(&bs[0])); !strings.Contains(got, "init note") {
		t.Errorf("Initiative body missing:\n%s", got)
	}
}

func TestIdeateDrawerFetchesComments(t *testing.T) {
	var calls []string
	stubCommonDir(t, t.TempDir())
	orig := listComments
	t.Cleanup(func() { listComments = orig })
	listComments = func(id string) ([]bd.Comment, error) { calls = append(calls, id); return nil, nil }
	bs := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship", Priority: 1, CommentCount: 1, Created: epoch},
		{ID: "s", Kind: contract.KindSignal, Status: "open", Title: "A signal", Priority: 1, Parent: "ini", CommentCount: 1, Created: epoch},
	}
	m := sized(t, 100, 40)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("1"))
	for i := 0; i < 6 && !(slices.Contains(calls, "ini") && slices.Contains(calls, "s")); i++ {
		m = press(t, m, runeKey("j"))
		m = press(t, m, runeKey("d"))
		m = press(t, m, runeKey("esc"))
	}
	if !slices.Contains(calls, "ini") || !slices.Contains(calls, "s") {
		t.Fatalf("Ideate drawer must fetch for the Initiative header and the Signal: %v", calls)
	}
	seen := map[string]bool{}
	for _, c := range calls {
		if seen[c] {
			t.Errorf("refetched %s: %v", c, calls)
		}
		seen[c] = true
	}
}

func TestFocusDrawerShowsSectionsAndFetchesComments(t *testing.T) {
	var calls []string
	stubCommonDir(t, t.TempDir())
	orig := listComments
	t.Cleanup(func() { listComments = orig })
	listComments = func(id string) ([]bd.Comment, error) {
		calls = append(calls, id)
		return []bd.Comment{{Author: "Franky", Text: "focus note", Created: time.Now().Add(-time.Hour)}}, nil
	}
	bs := focusFixture()
	for i := range bs {
		if bs[i].ID == "ini" {
			bs[i].CommentCount = 1
			bs[i].Notes = "some notes"
			bs[i].Description = "" // the fixture body would scroll the sections off the drawer
		}
	}
	m, _ := focusModel(t, 160, 40, bs, nil, map[string]bool{})
	f := press(t, ideaFocus(t, m), runeKey("d"))
	if !slices.Contains(calls, "ini") {
		t.Fatalf("Focus drawer did not fetch comments: %v", calls)
	}
	got := ansi.Strip(f.View())
	for _, w := range []string{"── NOTES", "── COMMENTS (1)", "focus note"} {
		if !strings.Contains(got, w) {
			t.Errorf("Focus drawer lacks %q:\n%s", w, got)
		}
	}
}
