package tui

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/rebase"
	"jayulabs.org/mugicrew/session"
)

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func enter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func esc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }

// formModel adds an open unclaimed Signal and an unclosed chain head to the stub graph.
func formModel(t *testing.T) Model {
	t.Helper()
	m := sized(t, 200, 30)
	bs := append(stubBeads(),
		bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch},
		bd.Bead{ID: "claimed", Kind: contract.KindSignal, Status: "open", Assignee: "x", Title: "Claimed signal", Created: epoch},
	)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	return unfoldUnparented(t, m)
}

func hints(m Model) string {
	ls := lines(m.View())
	return ls[len(ls)-1]
}

// seam swaps *p for v until the test ends.
func seam[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	t.Cleanup(func() { *p = old })
	*p = v
}

func TestSurgeryFilesThenDictates(t *testing.T) {
	seam(t, &newSurgery, func(title, p, ini string, req bool) (string, string, error) { return "sg", "cp", nil })
	calls := stubActions(t)
	m := formModel(t)
	m.crew = map[string]contract.Crewmate{"franky": fakeCrew{}}
	m = typeText(t, pressN(t, m, "u"), "Fix")
	m, cmd := send(t, m, enter())
	m, _ = send(t, m, cmd())
	if m.attachTo != "sg" || !m.launching["sg"] {
		t.Errorf("dictation not started: attachTo=%q launching=%v", m.attachTo, m.launching)
	}
	if has(*calls, "hitlClaim sg") {
		t.Errorf("a Surgery's filing already claimed it: %v", *calls)
	}
}

// A Surgery's first launch goes through launchNow, so a branch an orphaned worktree
// holds opens the orphan dialog (Spec FR25).
func TestSurgeryLaunchBusyOnOrphanOpensDialog(t *testing.T) {
	seam(t, &newSurgery, func(title, p, ini string, req bool) (string, string, error) { return "sg", "cp", nil })
	stubActions(t)
	busy := exec.Command("sh", "-c", "exit 128").Run()
	seam(t, &startSession, func(bd.Bead, contract.AgentCommand) error { return busy })
	seam(t, &git.WorktreeOn, func(branch string) (git.Holder, bool, error) {
		if branch == "" {
			return git.Holder{}, false, nil
		}
		return git.Holder{Path: "/tmp/mugicrew-wt-x/wt"}, true, nil
	})
	seam(t, &listAll, func() ([]bd.Bead, error) { return nil, nil })
	m := formModel(t)
	m.crew = map[string]contract.Crewmate{"franky": fakeCrew{}}
	m = typeText(t, pressN(t, m, "u"), "Fix")
	m, cmd := send(t, m, enter()) // files, then the write's result starts the launch
	m, cmd = send(t, m, cmd())
	m = settle(t, m, cmd)
	if len(m.modals) != 1 || !strings.Contains(ansi.Strip(m.View()), "Drop orphaned worktree?") {
		t.Errorf("no orphan dialog:\n%s", m.View())
	}
}

// E is no surface's raw edit: it opens no dialog and writes nothing.
func TestERawEditIsGone(t *testing.T) {
	stubExec(t)
	t.Setenv("EXPLORER", "ls")
	var called bool
	seam(t, &updateBead, func(string, []string) error { called = true; return nil })
	m := selectBead(t, formModel(t), "long")
	if n := press(t, m, runeKey("E")); len(n.modals) != 0 || called {
		t.Errorf("E: modals %d, bd update %v", len(n.modals), called)
	}
}

func TestRejectOnlyOnOpenUnclaimedSignal(t *testing.T) {
	var id, reason string
	seam(t, &closeBead, func(i, r string) error { id, reason = i, r; return nil })
	m := formModel(t)
	for _, other := range []string{"claimed", "long", "ch"} {
		o := selectBead(t, m, other)
		if strings.Contains(hints(o), "reject") {
			t.Errorf("%s hints z: %q", other, hints(o))
		}
		if n, cmd := send(t, o, runeKey("z")); cmd != nil || len(n.modals) != 0 {
			t.Errorf("z bound on %s", other)
		}
	}
	m = selectBead(t, m, "sig")
	if !strings.Contains(hints(m), "z reject") {
		t.Errorf("no z hint: %q", hints(m))
	}
	m = press(t, typeText(t, press(t, m, runeKey("z")), "dup"), esc())
	if id != "" {
		t.Fatal("esc rejected")
	}
	m = press(t, typeText(t, press(t, m, runeKey("z")), "dup"), enter())
	if id != "sig" || reason != "dup" || len(m.modals) != 0 {
		t.Errorf("reject %q %q", id, reason)
	}
	m = press(t, press(t, selectBead(t, m, "sig"), runeKey("z")), enter()) // blank reason
	if reason != rejectDefault {
		t.Errorf("blank reason %q", reason)
	}
}

func TestAdoptAndRelease(t *testing.T) {
	var head, ini, released string
	seam(t, &adoptChain, func(h, i string) error { head, ini = h, i; return nil })
	seam(t, &releaseChain, func(h string) error { released = h; return nil })
	m := formModel(t)
	// the Initiative itself and a closed bead take neither; a member takes A
	for _, id := range []string{"ini", "old"} {
		h := hints(selectBead(t, pick(t, m, "s", "any"), id)) // old is closed
		if strings.Contains(h, "adopt") || strings.Contains(h, "release") {
			t.Errorf("%s hints %q", id, h)
		}
	}
	s := selectBead(t, m, "sig")
	if h := hints(s); !strings.Contains(h, "a adopt") || strings.Contains(h, "release") {
		t.Errorf("unparented hints %q", h)
	}
	m = press(t, press(t, s, runeKey("a")), esc())
	if head != "" {
		t.Fatal("esc adopted")
	}
	m = press(t, press(t, s, runeKey("a")), enter())
	if head != "sig" || ini != "ini" {
		t.Errorf("adopt %q into %q", head, ini)
	}
	c := selectBead(t, m, "ch")
	if h := hints(c); !strings.Contains(h, "a/A adopt/release") {
		t.Errorf("member hints %q", h)
	}
	c = press(t, c, runeKey("A"))
	if released != "ch" || len(c.modals) != 0 {
		t.Errorf("release %q, modals %d", released, len(c.modals))
	}
}

// Under Require Initiatives A flashes the adopt-pointing reason and runs no release.
func TestReleaseRefusedWhenRequired(t *testing.T) {
	var released string
	seam(t, &releaseChain, func(h string) error { released = h; return nil })
	m := formModel(t)
	m.initiatives.Required = true
	c := press(t, selectBead(t, m, "ch"), runeKey("A"))
	if released != "" || !strings.Contains(c.status, "mugicrew initiative adopt ch <other>") {
		t.Errorf("released %q, status %q", released, c.status)
	}
}

// A refresh while a form is open leaves its target alone.
func TestRefreshKeepsFormTarget(t *testing.T) {
	var id, reason string
	seam(t, &closeBead, func(i, r string) error { id, reason = i, r; return nil })
	m := press(t, selectBead(t, formModel(t), "sig"), runeKey("z"))
	m = typeText(t, m, "no")
	m, _ = send(t, m, loadedMsg{beads: append([]bd.Bead{{ID: "aaa", Kind: contract.KindSignal, Status: "open", Title: "First", Created: epoch.Add(-time.Hour)}}, stubBeads()...), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, loadedMsg{beads: append(stubBeads(), bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch}), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, m, enter())
	if id != "sig" || reason != "no" {
		t.Errorf("closed %q %q", id, reason)
	}
}

// A stale reject dialog won't close a Signal someone claimed meanwhile.
func TestRejectRechecksGate(t *testing.T) {
	seam(t, &closeBead, func(i, r string) error { t.Fatal("closed a claimed signal"); return nil })
	m := press(t, selectBead(t, formModel(t), "sig"), runeKey("z"))
	bs := stubBeads()
	bs = append(bs, bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "in_progress", Assignee: "x", Title: "A signal", Created: epoch})
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, m, enter())
	if !strings.Contains(m.status, "no longer") {
		t.Errorf("status %q", m.status)
	}
}

// The adopt picker's keys are bound and handled (the input forms take free text, so
// theirs are not a binding set), and the form dialogs render without a subprocess.
func TestAdoptPickerCoverage(t *testing.T) {
	m := formModel(t)
	m, _ = send(t, m, loadedMsg{beads: append(stubBeads(), bd.Bead{ID: "ini2", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Second", Created: epoch},
		bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch}), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, selectBead(t, m, "sig"), runeKey("a"))
	bound := map[string]bool{}
	for _, b := range m.modeBindings() {
		for _, k := range b.Keys() {
			bound[k] = true
		}
	}
	for _, k := range candidateKeys() {
		if k.String() == "enter" {
			continue
		}
		next, cmd := send(t, m, k)
		if handled := cmd != nil || next.View() != m.View(); handled != bound[k.String()] {
			t.Errorf("key %q handled=%v bound=%v", k, handled, bound[k.String()])
		}
	}
}

func TestFormDialogsRunNoSubprocess(t *testing.T) {
	log := fakeTools(t)
	m := formModel(t)
	for _, c := range []struct{ id, k string }{{"sig", "z"}, {"sig", "e"}, {"sig", "a"}} {
		open := press(t, selectBead(t, m, c.id), runeKey(c.k))
		if len(open.modals) == 0 {
			t.Fatalf("%s did not open", c.k)
		}
		open.View()
	}
	pressN(t, m, "u").View()
	if got, _ := os.ReadFile(log); len(got) > 0 {
		t.Fatalf("form render started subprocesses:\n%s", got)
	}
}

var errTest = errors.New("boom")

// adopt re-checks the pinned head and the target Initiative when the human answers.
func TestAdoptCancelsWhenTargetGone(t *testing.T) {
	m := formModel(t)
	called := false
	old := adoptChain
	adoptChain = func(string, string) error { called = true; return nil }
	t.Cleanup(func() { adoptChain = old })
	m = press(t, selectBead(t, m, "sig"), runeKey("a"))
	m, _ = send(t, m, loadedMsg{beads: slices.DeleteFunc(slices.Clone(m.beads), func(b bd.Bead) bool { return b.ID == "ini" }), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, m, enterKey())
	if called || !strings.Contains(m.status, "adopt cancelled") {
		t.Errorf("called=%v status=%q", called, m.status)
	}
}

// A second n while the first create is in flight is refused at open, so no form is lost.
func TestNewRefusedAtOpenWhileWriteInFlight(t *testing.T) {
	m := formModel(t)
	m.pending = map[string]string{slot("new Signal"): "filing"}
	m = pressN(t, m, "s")
	if len(m.modals) != 0 || !strings.Contains(m.status, "write in flight") {
		t.Errorf("second n opened a form: modals=%d status=%q", len(m.modals), m.status)
	}
}

func TestAdoptReleaseGatesNeedNoGraphTab(t *testing.T) {
	beads := []bd.Bead{
		{ID: "i1", Kind: contract.KindInitiative, Status: "open"},
		{ID: "s1", Kind: contract.KindSignal, Status: "open"},
		{ID: "s2", Kind: contract.KindSignal, Status: "open", Parent: "i1"},
	}
	if !canAdopt(beads, &beads[1]) || canAdopt(beads, &beads[0]) {
		t.Error("canAdopt: a Signal may join an open Initiative, an Initiative never")
	}
	if canRelease(beads, &beads[1]) || !canRelease(beads, &beads[2]) {
		t.Error("canRelease: only an Initiative's member may leave")
	}
}

// joinModel adds a Checkpoint "cp" with a Build, the shape the join
// verdict reads, plus extra members.
func joinModel(t *testing.T, extra ...bd.Bead) Model {
	t.Helper()
	m := sized(t, 200, 30)
	bs := append(stubBeads(),
		bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "The checkpoint", Created: epoch},
		bd.Bead{ID: "cpb", Kind: contract.KindBuild, Status: "open", Title: "A build", Parent: "cp", Created: epoch},
	)
	m, _ = send(t, m, loadedMsg{beads: append(bs, extra...), live: map[string]bool{}, sessions: map[string]session.Info{}})
	return unfoldUnparented(t, press(t, m, runeKey("4")))
}

// The Adopt picker lists each Initiative as its SBRN (a non-live view: no dot slot).
func TestAdoptPickerItemsAreSBRN(t *testing.T) {
	m := formModel(t)
	m, _ = send(t, m, loadedMsg{beads: append(stubBeads(),
		bd.Bead{ID: "ini2", Kind: contract.KindInitiative, Container: true, Status: "open", Priority: 1, Title: "Second", Created: epoch},
		bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch}), live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, selectBead(t, m, "sig"), runeKey("a"))
	var got []string
	for _, it := range m.modals[0].items {
		got = append(got, ansi.Strip(it))
	}
	want := "○  INITIATIVE  P1 · Second · ini2" // kind tags padded to the widest, as on Recents
	if !slices.Contains(got, want) {
		t.Errorf("adopt picker items %q lack %q", got, want)
	}
}

// A long Initiative title is truncated in the Adopt picker, never into its bead id.
func TestAdoptPickerKeepsBeadID(t *testing.T) {
	m := sized(t, 120, 40)
	bs := append(stubBeads(),
		bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch},
		bd.Bead{ID: "mc-2xja", Kind: contract.KindInitiative, Container: true, Status: "open", Title: strings.Repeat("long title ", 20), Created: epoch},
	)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	m = press(t, selectBead(t, unfoldUnparented(t, m), "sig"), runeKey("a"))
	for _, l := range lines(m.View()) {
		if strings.Contains(l, "│") && strings.Contains(l, "long title") {
			_, tail := m.gateData().sbrnParts(&bs[len(bs)-1], false, 0)
			if !strings.Contains(l, "…") || !strings.Contains(l, strings.TrimSpace(ansi.Strip(tail))) {
				t.Errorf("row lost its id: %q", l)
			}
			return
		}
	}
	t.Fatal("no picker row")
}

// blockModel is formModel with sig already waiting on claimed, plus a Draft.
func blockModel(t *testing.T) Model {
	t.Helper()
	m := formModel(t)
	bs := slices.Clone(m.beads)
	for i := range bs {
		if bs[i].ID == "sig" {
			bs[i].BlockedBy = []string{"claimed"}
		}
	}
	bs = append(bs, bd.Bead{ID: "draft", Kind: contract.KindTask, Status: "deferred", Title: "A draft", Created: epoch})
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	return m
}

func TestAddBlockerPicker(t *testing.T) {
	var blocked, blocker string
	var err error
	seam(t, &addBlocker, func(a, b string) error { blocked, blocker = a, b; return err })
	m := press(t, selectBead(t, blockModel(t), "sig"), runeKey("B"))
	if len(m.modals) != 1 {
		t.Fatalf("modals %d, status %q", len(m.modals), m.status)
	}
	rows := strings.Join(m.modals[0].items, "\n")
	for _, id := range []string{"long", "paused", "run", "ini"} {
		if !strings.Contains(rows, id) {
			t.Errorf("picker lacks %s:\n%s", id, rows)
		}
	}
	for _, id := range []string{"sig", "claimed", "draft", "oldb"} {
		if strings.Contains(rows, id) {
			t.Errorf("picker has %s:\n%s", id, rows)
		}
	}
	// the first row is the first target in beadCmp order
	picked := blockTargets(m.beads, &bd.Bead{ID: "sig", BlockedBy: []string{"claimed"}})[0].ID
	if first := m.modals[0].items[0]; !strings.Contains(first, picked) {
		t.Errorf("first row %q lacks %s", first, picked)
	}
	done := press(t, m, enter())
	if blocked != "sig" || blocker != picked || done.status != "sig now waits on "+picked {
		t.Errorf("wrote %q<-%q, status %q", blocked, blocker, done.status)
	}
	// a cycle error from bd surfaces and nothing else happens
	err = errors.New("would create a cycle")
	done = press(t, m, enter())
	if !strings.Contains(done.status, "would create a cycle") {
		t.Errorf("status %q", done.status)
	}
}

func TestAddBlockerRefusalsAndHints(t *testing.T) {
	m := blockModel(t)
	if h := hints(selectBead(t, m, "sig")); !strings.Contains(h, "B add blocker") {
		t.Errorf("hints %q", h)
	}
	for id, why := range map[string]string{"oldb": "closed beads can't be blocked", "draft": "Drafts can't be blocked"} {
		facet := "any"
		if id == "draft" {
			facet = "deferred"
		}
		s := selectBead(t, pick(t, m, "s", facet), id)
		if strings.Contains(hints(s), "add blocker") {
			t.Errorf("%s hints B", id)
		}
		if s = press(t, s, runeKey("B")); s.status != why || len(s.modals) != 0 {
			t.Errorf("%s status %q", id, s.status)
		}
	}
}

func TestAddBlockerStaleAndPending(t *testing.T) {
	called := false
	seam(t, &addBlocker, func(string, string) error { called = true; return nil })
	m := press(t, selectBead(t, blockModel(t), "sig"), runeKey("B"))
	picked := blockTargets(m.beads, &bd.Bead{ID: "sig", BlockedBy: []string{"claimed"}})[0]
	picked.Status = "closed"
	var bs []bd.Bead
	for _, b := range m.beads {
		if b.ID == picked.ID {
			b = picked
		}
		bs = append(bs, b)
	}
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, m, enter())
	if called || !strings.Contains(m.status, "cancelled") {
		t.Errorf("called %v, status %q", called, m.status)
	}
	m = selectBead(t, m, "sig")
	m.pending = map[string]string{"sig": "blocking"}
	if m = press(t, m, runeKey("B")); len(m.modals) != 0 || !strings.Contains(m.status, "in flight") {
		t.Errorf("modals %d, status %q", len(m.modals), m.status)
	}
}

func TestUnblockPicker(t *testing.T) {
	var blocked, blocker string
	var err error
	seam(t, &removeBlocker, func(a, b string) error { blocked, blocker = a, b; return err })
	seam(t, &closeBead, func(string, string) error { t.Error("close called"); return nil })
	m := blockModel(t)
	bs := slices.Clone(m.beads)
	for i := range bs {
		if bs[i].ID == "sig" {
			bs[i].BlockedBy = []string{"claimed", "oldb"} // oldb is closed
		}
	}
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, selectBead(t, m, "sig"), runeKey("U"))
	if len(m.modals) != 1 || len(m.modals[0].items) != 1 || !strings.Contains(m.modals[0].items[0], "claimed") {
		t.Fatalf("modals %d, status %q", len(m.modals), m.status)
	}
	done := press(t, m, enter())
	if blocked != "sig" || blocker != "claimed" || done.status != "sig no longer waits on claimed" {
		t.Errorf("wrote %q<-%q, status %q", blocked, blocker, done.status)
	}
	err = errors.New("boom")
	if done = press(t, m, enter()); !strings.Contains(done.status, "boom") {
		t.Errorf("status %q", done.status)
	}
}

func TestUnblockRefusalsAndHints(t *testing.T) {
	m := blockModel(t)
	if h := hints(selectBead(t, m, "sig")); !strings.Contains(h, "U unblock") {
		t.Errorf("hints %q", h)
	}
	s := selectBead(t, m, "long")
	if strings.Contains(hints(s), "unblock") {
		t.Errorf("hints U without blockers")
	}
	if s = press(t, s, runeKey("U")); s.status != "no open blockers" || len(s.modals) != 0 {
		t.Errorf("status %q", s.status)
	}
}

func TestUnblockStaleAndPending(t *testing.T) {
	called := false
	seam(t, &removeBlocker, func(string, string) error { called = true; return nil })
	m := press(t, selectBead(t, blockModel(t), "sig"), runeKey("U"))
	var bs []bd.Bead
	for _, b := range m.beads {
		if b.ID == "claimed" {
			b.Status = "closed"
		}
		bs = append(bs, b)
	}
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m = press(t, m, enter())
	if called || !strings.Contains(m.status, "cancelled") {
		t.Errorf("called %v, status %q", called, m.status)
	}
	m = selectBead(t, blockModel(t), "sig")
	m.pending = map[string]string{"sig": "unblocking"}
	if m = press(t, m, runeKey("U")); len(m.modals) != 0 || !strings.Contains(m.status, "in flight") {
		t.Errorf("modals %d, status %q", len(m.modals), m.status)
	}
}

func rebaseModel(t *testing.T) Model {
	t.Helper()
	m := formModel(t)
	bs := append(slices.Clone(m.beads),
		bd.Bead{ID: "rchart", Kind: contract.KindChart, Status: "open", Title: "A chart", Created: epoch},
		bd.Bead{ID: "rcp", Kind: contract.KindCheckpoint, Status: "open", Title: "A checkpoint", Created: epoch},
	)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: map[string]session.Info{}})
	return m
}

func TestNewMenuRebaseRowOnlyOnOpenChartOrCheckpoint(t *testing.T) {
	m := rebaseModel(t)
	for id, want := range map[string]bool{"rchart": true, "rcp": true, "sig": false} {
		o := press(t, selectBead(t, m, id), runeKey("n"))
		if got := strings.Contains(ansi.Strip(o.View()), "r   New rebase request"); got != want {
			t.Errorf("%s: rebase row = %v, want %v", id, got, want)
		}
		if strings.Contains(hints(selectBead(t, m, id)), "r rebase") {
			t.Errorf("%s: Graph still hints r rebase", id)
		}
	}
	called := false
	seam(t, &requestRebase, func(string) (rebase.Outcome, error) { called = true; return rebase.Outcome{}, nil })
	press(t, selectBead(t, m, "sig"), runeKey("r"))
	if called {
		t.Error("r on Graph still rebases")
	}
}

func TestNewMenuRowOrderIncludesRebase(t *testing.T) {
	o := press(t, selectBead(t, rebaseModel(t), "rcp"), runeKey("n"))
	var got []string
	for _, k := range o.modals[0].rowKeys {
		got = append(got, k[0])
	}
	if want := "s,i,u,r,d"; strings.Join(got, ",") != want {
		t.Errorf("rows %v, want %s", got, want)
	}
}

func TestNewMenuRebaseFlashPerOutcome(t *testing.T) {
	cases := []struct {
		name string
		out  rebase.Outcome
		err  error
		want string
	}{
		{"filed", rebase.Outcome{Result: rebase.Filed, ID: "k1", Title: "Rebase onto mainline"}, nil, "filed: Rebase onto mainline (k1)"},
		{"requested", rebase.Outcome{Result: rebase.AlreadyRequested, ID: "k1", Title: "T"}, nil, "already requested: T (k1)"},
		{"conflict", rebase.Outcome{Result: rebase.OpenConflict, ID: "k2", Title: "C", Base: "b4se"}, nil, "already being rebased by open conflict: C (k2) Base: b4se"},
		{"on main", rebase.Outcome{Result: rebase.AlreadyOnMain, Base: "b4se"}, nil, "already on mainline: b4se"},
		{"refused", rebase.Outcome{Result: rebase.Refused, Reason: "rcp has already landed"}, nil, "rebase: refused: rcp has already landed"},
		{"mr warning", rebase.Outcome{Result: rebase.Filed, ID: "k1", Title: "T", Warning: "could not check for an open MR"}, nil, "filed: T (k1) — warning: could not check for an open MR"},
		{"error", rebase.Outcome{}, errors.New("boom"), "rebase: boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			seam(t, &requestRebase, func(id string) (rebase.Outcome, error) { got = id; return c.out, c.err })
			m := pressN(t, selectBead(t, rebaseModel(t), "rcp"), "r")
			if got != "rcp" || m.status != c.want {
				t.Errorf("id %q, status %q, want %q", got, m.status, c.want)
			}
		})
	}
}

// ADR 0349: under Require Initiatives A still releases a member Task.
func TestReleaseTaskPassesWhenRequired(t *testing.T) {
	var released string
	seam(t, &releaseChain, func(h string) error { released = h; return nil })
	m := formModel(t)
	m.initiatives.Required = true
	m, _ = send(t, m, loadedMsg{beads: append(stubBeads(), bd.Bead{ID: "tk", Kind: contract.KindTask, Status: "open", Parent: "ini", Created: epoch}), live: map[string]bool{}, sessions: map[string]session.Info{}})
	press(t, selectBead(t, m, "tk"), runeKey("A"))
	if released != "tk" {
		t.Errorf("released %q", released)
	}
}
