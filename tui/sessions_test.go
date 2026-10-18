package tui

import (
	"maps"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func sessionsFixture() ([]bd.Bead, map[string]session.Info) {
	ago := func(d time.Duration) func(*bd.Bead) { return func(b *bd.Bead) { b.Started = time.Now().Add(-d) } }
	bs := []bd.Bead{
		dbead("turn", contract.KindSignal, "in_progress", "", 1, titled("Turn one"), ago(3*time.Hour)),
		dbead("ask", contract.KindBuild, "in_progress", "", 1, titled("Ask one"), ago(5*time.Hour)),
		dbead("work", contract.KindChart, "in_progress", "", 1, titled("Working one"), ago(10*time.Minute)),
		dbead("exit", contract.KindGrilling, "in_progress", "", 1, titled("Exited one"), ago(2*time.Hour)),
		dbead("disc", contract.KindDiscussion, "in_progress", "", 1, titled("Robin chat"), ago(30*time.Minute)),
		dbead("clos", contract.KindSignal, "in_progress", "", 1, titled("Closing one"), ago(time.Hour)),
		dbead("none", contract.KindSignal, "open", "", 1, titled("No session")),
		dbead("afk", contract.KindBuild, "in_progress", "", 1, titled("AFK effort")),
		dbead("done", contract.KindSignal, "closed", "", 1, titled("Done one"), ago(4*time.Hour)),
		dbead("cx", contract.KindSignal, "closed", "", 1, titled("Closed exited"), ago(time.Hour)),
	}
	sess := map[string]session.Info{
		"turn": {State: session.Alive, Waiting: session.WaitTurn},
		"ask":  {State: session.Alive, Waiting: session.WaitAsk},
		"work": {State: session.Alive},
		"exit": {State: session.Exited},
		"disc": {State: session.Alive},
		"clos": {State: session.Alive, Closing: true},
		"done": {State: session.Alive},
		"cx":   {State: session.Exited},
		"gone": {State: session.Alive}, // no bead in the snapshot
	}
	return bs, sess
}

func sessionsModel(t *testing.T) Model {
	t.Helper()
	bs, sess := sessionsFixture()
	m := sized(t, 120, 30)
	// Turn one (claimed 3h ago) starts waiting before Ask one (claimed 5h ago): wait order is
	// the reverse of bead start order.
	early := maps.Clone(sess)
	early["ask"] = session.Info{State: session.Alive}
	t0 := time.Now().Add(-time.Hour)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{"afk": true}, sessions: early, started: t0})
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{"afk": true}, sessions: sess, started: t0.Add(30 * time.Minute)})
	return m
}

func sessionsView(m Model) string { return ansi.Strip(m.View()) }

func TestSessionsOpenAndClose(t *testing.T) {
	m := sessionsModel(t)
	for _, k := range []string{"1", "3", "5"} {
		m = press(t, m, runeKey(k))
		m = press(t, m, runeKey("S"))
		if len(m.modals) != 1 || !m.modals[0].sessions {
			t.Fatalf("S on tab %s opened no Sessions modal", k)
		}
		m = press(t, m, runeKey("S"))
		if len(m.modals) != 0 {
			t.Errorf("S did not close on tab %s", k)
		}
	}
	m = press(t, m, runeKey("S"))
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.modals) != 0 {
		t.Error("esc did not close")
	}
	fm := ideaFocus(t, recentsModel(t, focusFixture()))
	fm = press(t, fm, runeKey("S"))
	if len(fm.modals) != 1 || !focused(fm) {
		t.Errorf("S in focus view: modals %d focused %v", len(fm.modals), focused(fm))
	}
}

func TestSessionsShellKeysInert(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	tab := m.tab
	for _, k := range []string{"2", "o", "?", "tab", "q", "R", "D"} {
		m = press(t, m, runeKey(k))
		if len(m.modals) != 1 || !m.modals[0].sessions || m.tab != tab || m.ref {
			t.Fatalf("shell key %q acted under Sessions", k)
		}
	}
}

// sessionsPick opens the modal and walks the highlight to bead id.
func sessionsPick(t *testing.T, m Model, id string) Model {
	t.Helper()
	m = press(t, m, runeKey("S"))
	for i := 0; m.modals[0].id != id; i++ {
		if i > 10 {
			t.Fatalf("no row %q", id)
		}
		m = press(t, m, runeKey("j"))
	}
	return m
}

func TestSessionsEnter(t *testing.T) {
	calls := stubActions(t)
	m := sessionsModel(t)
	m.ctx = harnessCtx{}
	var resumed string
	seam(t, &resumeSession, func(b bd.Bead, _ session.Info, _ contract.Harness) error { resumed = b.ID; return nil })
	for _, id := range []string{"turn", "disc"} { // an alive row and a Robin row attach
		n, cmd := send(t, sessionsPick(t, m, id), enterKey())
		settle(t, n, cmd)
		if len(n.modals) != 0 || !has(*calls, "switchSession "+id) {
			t.Errorf("%s: modals %d calls %v", id, len(n.modals), *calls)
		}
	}
	n, cmd := send(t, sessionsPick(t, m, "exit"), enterKey())
	settle(t, n, cmd)
	if resumed != "exit" {
		t.Errorf("resumed = %q", resumed)
	}
	// the bead leaves the snapshot under the highlight: status, no launch
	*calls = nil
	n = sessionsPick(t, m, "turn")
	n.beads = n.beads[1:]
	n, cmd = send(t, n, enterKey())
	if cmd != nil || len(*calls) != 0 || !strings.Contains(n.status, "no longer listed") {
		t.Errorf("gone: cmd %v calls %v status %q", cmd != nil, *calls, n.status)
	}
	e := sized(t, 120, 30) // the empty modal ignores enter
	e = press(t, e, runeKey("S"))
	if e, cmd = send(t, e, enterKey()); cmd != nil || len(e.modals) != 1 {
		t.Error("enter on the empty modal acted")
	}
}

func TestSessionsRowsAndOrder(t *testing.T) {
	v := sessionsView(press(t, sessionsModel(t), runeKey("S")))
	want := []string{"· Turn one · turn · your turn · 3h", "· Ask one · ask · needs approval · 5h",
		"· Working one · work · working · 10m", "· Robin chat · disc · working · 30m", "· Exited one · exit · exited · 2h", "· Done one · done · done — /exit · 4h"}
	last := -1
	for _, w := range want { // waiting first (longest-waiting on top, not earliest-claimed), then newest first
		i := strings.Index(v, w)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in\n%s", w, v)
		}
		last = i
	}
	items := strings.Join(press(t, sessionsModel(t), runeKey("S")).modals[0].items, "\n")
	for _, no := range []string{"Closing one", "Closed exited", "No session"} {
		if strings.Contains(items, no) {
			t.Errorf("%q listed", no)
		}
	}
}

func TestSessionsRowNotation(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	var rows []string
	for _, it := range m.modals[0].items {
		rows = append(rows, ansi.Strip(it))
	}
	col := -1
	for _, r := range rows {
		if strings.Contains(r, "●") {
			t.Errorf("dot in %q", r)
		}
		i := strings.Index(r, " · ") // the title starts after the priority separator
		if col >= 0 && i != col {
			t.Errorf("titles misaligned: %q", r)
		}
		col = i
		if !strings.Contains(r, "P1") {
			t.Errorf("no priority in %q", r)
		}
	}
	if !strings.HasPrefix(rows[0], "◐") || !strings.Contains(rows[0], "SIGNAL") || !strings.Contains(rows[1], "BUILD") {
		t.Errorf("head wrong: %q", rows)
	}
	n := sized(t, 60, 30)
	bs, sess := sessionsFixture()
	bs[0].Title = strings.Repeat("long ", 30)
	n, _ = send(t, n, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	r := ansi.Strip(press(t, n, runeKey("S")).modals[0].items[0])
	if !strings.Contains(r, "…") || !strings.HasSuffix(r, " · turn · your turn · 3h") {
		t.Errorf("narrow row %q", r)
	}
}

func TestSessionsEmpty(t *testing.T) {
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: nil, live: map[string]bool{}})
	m = press(t, m, runeKey("S"))
	for _, k := range []string{"j", "k", "down", "up"} {
		m = press(t, m, runeKey(k))
	}
	if !strings.Contains(sessionsView(m), "no sessions") {
		t.Errorf("no empty line:\n%s", sessionsView(m))
	}
}

func TestSessionsLiveRefresh(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	m = press(t, m, runeKey("j")) // Ask one
	if m.modals[0].id != "ask" {
		t.Fatalf("highlight on %q", m.modals[0].id)
	}
	bs, sess := sessionsFixture()
	delete(sess, "turn")                             // the row above goes away
	sess["ask"] = session.Info{State: session.Alive} // and Ask one stops waiting
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	v := sessionsView(m)
	if strings.Contains(v, "Turn one") || !strings.Contains(v, "· Ask one · ask · working") {
		t.Errorf("rows not re-derived:\n%s", v)
	}
	if m.modals[0].id != "ask" || !strings.Contains(ansi.Strip(m.modals[0].items[m.modals[0].cur]), "· ask · working") {
		t.Errorf("highlight left Ask one: %+v", m.modals[0].cur)
	}
	delete(sess, "ask") // the highlighted one goes: clamps
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	if m.modals[0].cur >= len(m.modals[0].items) {
		t.Error("cur past the list")
	}
}

func TestSessionsInReference(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("?"))
	if !strings.Contains(sessionsView(m), "sessions") {
		t.Error("? omits S")
	}
	if v := sessionsView(press(t, sessionsModel(t), runeKey("S"))); !strings.Contains(v, "enter") {
		t.Errorf("hints omit enter:\n%s", v)
	}
}

func TestSessionsDrop(t *testing.T) {
	var dropped []string
	oldDrop, oldClose, oldRead, oldUnclaim := dropSession, closeBead, readSessions, hitlUnclaim
	t.Cleanup(func() { dropSession, closeBead, readSessions, hitlUnclaim = oldDrop, oldClose, oldRead, oldUnclaim })
	_, sess := sessionsFixture()
	readSessions = func() (map[string]session.Info, error) { return sess, nil }
	hitlUnclaim = func(string) error { return nil }
	dropSession = func(b bd.Bead, _ session.Info) error { dropped = append(dropped, "session "+b.ID); return nil }
	closeBead = func(id, why string) error { dropped = append(dropped, "close "+id+" "+why); return nil }

	open := func(row string) Model { // S, then j to the row
		m := press(t, sessionsModel(t), runeKey("S"))
		for m.modals[0].id != row {
			m = press(t, m, runeKey("j"))
		}
		return press(t, m, runeKey("x"))
	}
	for _, a := range []tea.KeyMsg{runeKey("n"), {Type: tea.KeyEsc}} {
		m := open("turn")
		if len(m.modals) != 2 || m.modals[1].title != "Drop session?" {
			t.Fatalf("x raised %+v", m.modals)
		}
		m = press(t, m, a)
		if len(m.modals) != 1 || !m.modals[0].sessions || len(dropped) != 0 {
			t.Fatalf("cancel dropped or lost the modal: %v", dropped)
		}
	}
	m := open("turn")
	m, cmd := send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if len(dropped) == 0 || dropped[0] != "session turn" {
		t.Errorf("y dropped %v", dropped)
	}
	dropped = nil
	m = open("disc")
	m, cmd = send(t, m, runeKey("y"))
	settle(t, m, cmd)
	if len(dropped) != 2 || dropped[1] != "close disc dropped" {
		t.Errorf("discussion y dropped %v", dropped)
	}
}

func TestSessionsXListed(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	if !strings.Contains(sessionsView(m), "t • x") { // the footer falls to keys alone at this width
		t.Errorf("hint missing x:\n%s", sessionsView(m))
	}
	e := sized(t, 120, 30)
	e, _ = send(t, e, loadedMsg{live: map[string]bool{}})
	press(t, press(t, e, runeKey("S")), runeKey("x")) // empty: no panic
}

func TestSessionsRefreshUnderDropDialog(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	for i := 0; i < 4; i++ {
		m = press(t, m, runeKey("j"))
	}
	m = press(t, m, runeKey("x"))
	bs, sess := sessionsFixture()
	m, _ = send(t, m, loadedMsg{beads: bs[:1], live: map[string]bool{}, sessions: sess})
	m = press(t, m, runeKey("n"))
	for _, k := range []string{"k", "j", "j"} {
		m = press(t, m, runeKey(k)) // must not panic on the shrunk list
		if d := m.modals[0]; d.cur != 0 || d.id != "turn" {
			t.Fatalf("highlight %d %q", d.cur, d.id)
		}
	}
}

func TestSessionsHITLHeading(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	v := sessionsView(m)
	h, r := strings.Index(v, "HITL"), strings.Index(v, "Turn one")
	if h < 0 || r < h {
		t.Fatalf("HITL heading missing or below the rows:\n%s", v)
	}
	for range 2 * len(m.modals[0].items) { // j wraps all the way round: only rows are ever highlighted
		if m.modals[0].id != m.modals[0].ids[m.modals[0].cur] || strings.Contains(m.modals[0].items[m.modals[0].cur], "HITL") {
			t.Fatalf("heading highlighted: %+v", m.modals[0].cur)
		}
		m = press(t, m, runeKey("j"))
	}
	m = press(t, m, runeKey("k"))
	if m.modals[0].cur != len(m.modals[0].items)-1 {
		t.Errorf("k wrap landed on %d", m.modals[0].cur)
	}
}

func TestSessionsNoHeadingWhenEmpty(t *testing.T) {
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: nil, live: map[string]bool{}})
	m = press(t, m, runeKey("S"))
	if v := sessionsView(m); len(m.modals[0].heads) != 0 || !strings.Contains(v, "no sessions") {
		t.Errorf("empty modal:\n%s", v)
	}
}

func TestSessionsAFKSection(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	v := sessionsView(m)
	a, h := strings.Index(v, "│   AFK "), strings.Index(v, "│   HITL ")
	if h < 0 || a < h || !strings.Contains(v[a:], "AFK effort · afk · running · ") {
		t.Fatalf("AFK section missing or above HITL:\n%s", v)
	}
	d := m.modals[0]
	if d.heads[d.afkAt] != "AFK" || d.ids[len(d.ids)-1] != "afk" {
		t.Errorf("heads %v afkAt %d ids %v", d.heads, d.afkAt, d.ids)
	}
	// a dead effort (no liveness) is not listed
	bs, sess := sessionsFixture()
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	if v := sessionsView(m); strings.Contains(v, "AFK effort") || strings.Contains(v, "│   AFK ") {
		t.Errorf("dead effort listed:\n%s", v)
	}
}

func TestSessionsAFKOnlyAndNeither(t *testing.T) {
	bs := []bd.Bead{dbead("afk", contract.KindBuild, "in_progress", "", 1, titled("AFK effort"))}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{"afk": true}})
	m = press(t, m, runeKey("S"))
	if v, hd := sessionsView(m), m.modals[0].heads; len(hd) != 1 || hd[0] != "AFK" || strings.Contains(v, "no sessions") {
		t.Errorf("AFK only:\n%s", v)
	}
}

func TestSessionsAFKNavigationAndHints(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	n := len(m.modals[0].ids)
	if m.modals[0].afkAt != n-1 {
		t.Fatalf("afkAt %d of %d", m.modals[0].afkAt, n)
	}
	hints := func(m Model) string { return ansi.Strip(flatHints(m.modals[0].bindings(), 200)) }
	if h := hints(m); !strings.Contains(h, "attach/resume") || !strings.Contains(h, "drop") {
		t.Errorf("HITL hints %q", h)
	}
	m = press(t, m, runeKey("k")) // wraps from the top row onto the last: the AFK row
	if m.modals[0].id != "afk" {
		t.Fatalf("k landed on %q", m.modals[0].id)
	}
	if h := hints(m); strings.Contains(h, "enter") || !strings.Contains(h, "transcript") || !strings.Contains(h, "abort") || strings.Contains(h, "drop") {
		t.Errorf("AFK hints %q", h)
	}
	m = press(t, m, runeKey("k")) // crosses back over the boundary
	if m.modals[0].cur != n-2 {
		t.Errorf("k from AFK landed on %d", m.modals[0].cur)
	}
}

func TestSessionsAFKEnterRefusesAndXAborts(t *testing.T) {
	opened := stubWatch(t, nil)
	calls := stubActions(t)
	m := press(t, sessionsModel(t), runeKey("S"))
	m.logPath = "/log"
	m = press(t, m, runeKey("k"))
	x := press(t, m, runeKey("x"))
	if len(x.modals) != 2 || x.modals[1].title != "Abort effort?" || !x.modals[0].sessions {
		t.Fatalf("x raised %+v", x.modals)
	}
	n, cmd := send(t, m, enterKey())
	settle(t, n, cmd)
	if len(*opened) != 0 || has(*calls, "switchSession afk") || statusOf(n) != "afk is running — t shows it · x aborts it" {
		t.Errorf("opened %v calls %v status %q", *opened, *calls, statusOf(n))
	}
}

func TestSessionsAFKLeavesOnRefresh(t *testing.T) {
	m := press(t, sessionsModel(t), runeKey("S"))
	m = press(t, m, runeKey("k"))
	bs, sess := sessionsFixture()
	m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}, sessions: sess})
	d := m.modals[0]
	if d.cur >= len(d.items) || strings.Contains(strings.Join(d.items, ""), "AFK effort") || d.id != d.ids[d.cur] {
		t.Errorf("after AFK left: cur %d ids %v", d.cur, d.ids)
	}
}

// The HITL count skips Closing sessions and matches the modal's HITL rows (ADR 0108).
func TestCrewCountsHITLSkipsClosing(t *testing.T) {
	m := sessionsModel(t)
	delete(m.sessions, "gone") // the modal drops a session with no bead; the count has no bead to join
	all := func(string) bool { return true }
	rows, _ := m.sessionRows()
	alive := 0
	for _, r := range rows {
		if r.state != "exited" { // the count is of live sessions; an open exited row is listed, not counted
			alive++
		}
	}
	// work, turn, ask, disc, done (alive, not Closing); clos is Closing.
	if hitl, _, _, _ := m.crewCounts(all, nil); hitl != 5 || hitl != alive {
		t.Errorf("HITL = %d, modal alive rows = %d; want 5 and equal", hitl, alive)
	}
	s := m.sessions["clos"]
	s.Closing = false
	m.sessions["clos"] = s
	if hitl, _, _, _ := m.crewCounts(all, nil); hitl != 6 {
		t.Errorf("HITL after Closing cleared = %d, want 6", hitl)
	}
}
