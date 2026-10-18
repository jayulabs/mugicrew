package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/session"
)

func discussionModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"robin": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{
		{ID: "dop", Kind: contract.KindDiscussion, Status: "open", Title: "Open talk", Created: epoch},
		{ID: "dip", Kind: contract.KindDiscussion, Status: "in_progress", Title: "Idle talk", Created: epoch},
	}})
	m, _ = send(t, m, runeKey("4"))
	if sel(m) == unparentedSel {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeySpace})
	}
	return m, calls
}

// A discussion leaf, open or in_progress, shows in the Graph tab and enter launches it:
// the open one is claimed, the idle in_progress one resumes against its claim (FR9).
func TestDiscussionGraphLeafLaunch(t *testing.T) {
	for _, c := range []struct {
		id    string
		claim bool
	}{{"dop", true}, {"dip", false}} {
		m, calls := discussionModel(t)
		m, cmd := at(t, m, c.id, enterKey())
		settle(t, m, cmd)
		if !has(*calls, "startSession "+c.id) || has(*calls, "hitlClaim") != c.claim {
			t.Errorf("%s: calls = %v, want claim=%v", c.id, *calls, c.claim)
		}
	}
}

// homeTab sends a discussion to the Graph tab (Recents jump).
func TestCheckupIsFreeStandingGraphLeaf(t *testing.T) {
	if got := homeTab(bd.Bead{Kind: contract.KindCheckup}, nil); got != tabGraph {
		t.Errorf("homeTab(checkup) = %d, want tabGraph", got)
	}
	if !restsClaimed(contract.KindCheckup) {
		t.Error("an in_progress Checkup must rest claimed, not read as stranded")
	}
}

func TestDiscussionHomeTabIsGraph(t *testing.T) {
	if got := homeTab(bd.Bead{Kind: contract.KindDiscussion}, nil); got != tabGraph {
		t.Errorf("homeTab = %d, want %d", got, tabGraph)
	}
}

// Dropping a discussion kills its session and closes the bead as dropped; other kinds
// are still unclaimed.
func TestDiscussionDropClosesAsDropped(t *testing.T) {
	for _, c := range []struct {
		kind contract.Kind
		want string
	}{{contract.KindDiscussion, "dropSession d;closeBead d dropped"}, {contract.KindSignal, "dropSession d;hitlUnclaim d"}} {
		calls := stubActions(t)
		seam(t, &closeBead, func(id, reason string) error { *calls = append(*calls, "closeBead "+id+" "+reason); return nil })
		seam(t, &readSessions, func() (map[string]session.Info, error) {
			return map[string]session.Info{"d": {Session: "s1", State: session.Alive}}, nil
		})
		dropRun("d", "s1", &bd.Bead{ID: "d", Kind: c.kind, Status: "in_progress"})
		if got := strings.Join(*calls, ";"); got != c.want {
			t.Errorf("%s: calls = %q, want %q", c.kind, got, c.want)
		}
	}
}

func robinModal(t *testing.T) (Model, *[]string) {
	t.Helper()
	m, calls := discussionModel(t)
	m.sessions = map[string]session.Info{"dip": {Session: "s", State: session.Alive, Waiting: session.WaitTurn}}
	return m, calls
}

// pickAt opens the Robin modal and moves to the row showing want.
func pickAt(t *testing.T, m Model, want string) Model {
	t.Helper()
	m, _ = send(t, m, runeKey("D"))
	for range m.modals[0].items {
		if strings.Contains(m.modals[0].items[m.modals[0].cur], want) {
			return m
		}
		m, _ = send(t, m, runeKey("j"))
	}
	t.Fatalf("no row %q in %q", want, m.modals[0].items)
	return m
}

// D opens the Robin modal from every tab and the focus view; D or esc closes it.
func TestDiscussionModalOpensEverywhereAndCloses(t *testing.T) {
	for tab := range tabNames {
		m, _ := robinModal(t)
		m.tab = tab
		m, _ = send(t, m, runeKey("D"))
		if len(m.modals) != 1 || m.modals[0].title != "Discussions" {
			t.Fatalf("tab %d: modals = %d", tab, len(m.modals))
		}
		for _, k := range []tea.KeyMsg{runeKey("D"), escKey()} {
			c, _ := send(t, m, k)
			if len(c.modals) != 0 {
				t.Errorf("tab %d: %v left the modal open", tab, k)
			}
		}
	}
	m := ideaFocus(t, recentsModel(t, focusFixture()))
	if m, _ = send(t, m, runeKey("D")); len(m.modals) != 1 {
		t.Errorf("focus view: modals = %d", len(m.modals))
	}
}

// + New discussion leads, then the open discussions newest first with their state.
func TestDiscussionModalRows(t *testing.T) {
	m, _ := robinModal(t)
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{
		{ID: "old", Kind: contract.KindDiscussion, Status: "open", Title: "Older", Created: epoch},
		{ID: "new", Kind: contract.KindDiscussion, Status: "in_progress", Title: "Newer", Created: epoch.Add(time.Hour)},
		{ID: "gone", Kind: contract.KindDiscussion, Status: "closed", Title: "Closed", Created: epoch},
	}, sessions: map[string]session.Info{"new": {Session: "s", State: session.Alive, Waiting: session.WaitAsk}}})
	m, _ = send(t, m, runeKey("D"))
	items := m.modals[0].items
	if len(items) != 3 || !strings.Contains(items[0], "+ New discussion") ||
		!strings.Contains(items[1], "Newer") || !strings.Contains(items[1], "needs approval") ||
		!strings.Contains(items[2], "Older") || !strings.Contains(items[2], "no session") {
		t.Errorf("items = %q", items)
	}
	empty := sized(t, 100, 30)
	empty, _ = send(t, empty, runeKey("D"))
	if len(empty.modals[0].items) != 1 {
		t.Errorf("empty items = %q", empty.modals[0].items)
	}
}

// enter on + creates the timestamped bead then launches it; a create error is the status.
func TestDiscussionNewCreatesThenLaunches(t *testing.T) {
	m, calls := robinModal(t)
	var title string
	seam(t, &newDiscussion, func(ti, _ string) (string, error) { title = ti; return "nd", nil })
	m, _ = send(t, m, runeKey("D"))
	m, cmd := send(t, m, enterKey())
	drain(t, m, cmd, 4)
	if _, err := time.ParseInLocation("Discussion 2006-01-02 15:04", title, time.Local); err != nil {
		t.Errorf("title = %q: %v", title, err)
	}
	if !has(*calls, "hitlClaim nd") || !has(*calls, "startSession nd") {
		t.Errorf("calls = %v", *calls)
	}

	m, calls = robinModal(t)
	seam(t, &newDiscussion, func(string, string) (string, error) { return "", errTest })
	m, _ = send(t, m, runeKey("D"))
	m, cmd = send(t, m, enterKey())
	m = drain(t, m, cmd, 4)
	if len(*calls) != 0 || !strings.Contains(m.status, errTest.Error()) {
		t.Errorf("calls = %v, status = %q", *calls, m.status)
	}
}

// enter on a live row attaches, an exited one resumes, a claimed session-less one
// launches without a claim.
func TestDiscussionRowEnter(t *testing.T) {
	for _, c := range []struct {
		state *session.Info
		want  string
	}{
		{&session.Info{Session: "s", State: session.Alive}, "switchSession dip"},
		{nil, "startSession dip"},
	} {
		m, calls := robinModal(t)
		m.sessions = map[string]session.Info{}
		if c.state != nil {
			m.sessions["dip"] = *c.state
		}
		m = pickAt(t, m, "Idle talk")
		m, cmd := send(t, m, enterKey())
		settle(t, m, cmd)
		if !has(*calls, c.want) || has(*calls, "hitlClaim") {
			t.Errorf("%+v: calls = %v, want %s without claim", c.state, *calls, c.want)
		}
	}
	m, _ := robinModal(t)
	m.ctx = harnessCtx{}
	var resumed string
	seam(t, &resumeSession, func(b bd.Bead, _ session.Info, _ contract.Harness) error { resumed = b.ID; return nil })
	m.sessions = map[string]session.Info{"dip": {Session: "s", State: session.Exited}}
	m = pickAt(t, m, "Idle talk")
	m, cmd := send(t, m, enterKey())
	settle(t, m, cmd)
	if resumed != "dip" {
		t.Errorf("resumed = %q", resumed)
	}
}

// x asks y/n and yes drops: kills the session, closes the bead dropped.
func TestDiscussionRowDrop(t *testing.T) {
	m, calls := robinModal(t)
	seam(t, &closeBead, func(id, reason string) error { *calls = append(*calls, "closeBead "+id+" "+reason); return nil })
	seam(t, &readSessions, func() (map[string]session.Info, error) { return m.sessions, nil })
	m = pickAt(t, m, "Idle talk")
	m, _ = send(t, m, runeKey("x"))
	if len(m.modals) != 1 || m.modals[0].kind != confirm {
		t.Fatalf("x did not open the y/n dialog: %d modals", len(m.modals))
	}
	m, cmd := send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if !has(*calls, "dropSession dip") || !has(*calls, "closeBead dip dropped") {
		t.Errorf("calls = %v", *calls)
	}
}

// A session-less discussion's drop deletes its Drafts, then closes the bead as dropped.
func TestDiscussionRowDropSessionlessDeletesDrafts(t *testing.T) {
	m, calls := robinModal(t)
	seam(t, &dropDrafts, func(b bd.Bead) error { *calls = append(*calls, "dropDrafts "+b.ID); return nil })
	seam(t, &closeBead, func(id, reason string) error { *calls = append(*calls, "closeBead "+id+" "+reason); return nil })
	m = pickAt(t, m, "Open talk")
	m, _ = send(t, m, runeKey("x"))
	m, cmd := send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if got := strings.Join(*calls, ";"); got != "dropDrafts dop;closeBead dop dropped" {
		t.Errorf("calls = %q", got)
	}
}

// A discussion's clean exit flashes ✓ <title> finished.
func TestDiscussionFinishedFlashNamesTitle(t *testing.T) {
	m, _ := robinModal(t)
	m, _ = send(t, m, logsMsg{path: "p", changed: true})
	m, _ = send(t, m, logsMsg{path: "p", changed: true, events: []eventlog.Event{{Msg: "hitl.exited", Bead: "dop", Time: time.Unix(5, 0), Raw: map[string]any{}}}, digest: []eventlog.Event{{Msg: "hitl.exited", Bead: "dop", Time: time.Unix(5, 0), Raw: map[string]any{}}}})
	if m.status != "✓ Open talk finished" {
		t.Errorf("status = %q", m.status)
	}
}

// A discussion row is its SBRN followed by the state and age tail, with no dot column.
func TestDiscussionRowIsSBRN(t *testing.T) {
	m, _ := robinModal(t)
	m, _ = send(t, m, runeKey("D"))
	var row string
	for _, it := range m.modals[0].items[1:] {
		if strings.Contains(it, "DISCUSSION") {
			row = ansi.Strip(it)
			break
		}
	}
	if row == "" || strings.Contains(row, "○·") || !strings.Contains(row, " P") || !strings.Contains(row, " · ") {
		t.Errorf("discussion row not SBRN: %q", row)
	}
}

// homeTab sends a Landing to its container's tab.
func TestLandingHomeTabFollowsContainer(t *testing.T) {
	beads := []bd.Bead{
		dbead("c", contract.KindChart, "open", "", 1),
		dbead("cp", contract.KindCheckpoint, "open", "", 1),
		dbead("cl", contract.KindLanding, "open", "c", 1),
		dbead("pl", contract.KindLanding, "open", "cp", 1),
	}
	if got := homeTab(beads[2], beads); got != tabDefog {
		t.Errorf("Chart's Landing home = %d, want Defog", got)
	}
	if got := homeTab(beads[3], beads); got != tabExecute {
		t.Errorf("Checkpoint's Landing home = %d, want Execute", got)
	}
}
