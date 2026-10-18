package tui

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// queueTab drives one queue tab for the launch-cursor tests: sel reads its cursor key.
type queueTab struct {
	name string
	tab  int
	sel  func(Model) string
	// model is a tab over n launchable Actionable beads x1..xn (plus a Live bead "lv").
	model func(t *testing.T, n int) Model
	at    func(t *testing.T, m Model, k string) Model
}

func queueTabs() []queueTab {
	return []queueTab{
		{"ideate", tabIdeate, ideaSel, func(t *testing.T, n int) Model {
			stubActions(t)
			beads := []bd.Bead{ini("ini", "open", "Ship", 1), sig("lv", "in_progress", 1, "ini")}
			for i := range n {
				beads = append(beads, sig("x"+fmt.Sprint(i+1), "open", 2, "ini"))
			}
			m := ideaModel(t, 100, 40, beads, map[string]session.Info{"lv": {State: session.Alive}})
			m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}}
			return m
		}, ideaAt},
		{"defog", tabDefog, defogSel, func(t *testing.T, n int) Model {
			stubActions(t)
			beads := []bd.Bead{dbead("ini", contract.KindInitiative, "open", "", 1), dbead("c", contract.KindChart, "open", "ini", 1),
				dbead("lv", contract.KindGrilling, "open", "c", 1)}
			for i := range n {
				beads = append(beads, dbead("x"+fmt.Sprint(i+1), contract.KindGrilling, "open", "c", 2+i))
			}
			m := sized(t, 160, 40)
			m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
			m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{"lv": {State: session.Alive}}})
			return press(t, treesOpen(m), runeKey("2"))
		}, defogAt},
		{"execute", tabExecute, execSel, func(t *testing.T, n int) Model {
			beads := []bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer), dbead("lv", contract.KindApproval, "open", "cp", 1)}
			for i := range n {
				beads = append(beads, dbead("x"+fmt.Sprint(i+1), contract.KindApproval, "open", "cp", 2+i))
			}
			m, _ := execModel(t, beads)
			return m
		}, execAt},
	}
}

// Launching from an Actionable row leaves the cursor in Actionable, on the row that took
// its place: the next, else the previous, else the placeholder (FR 1-5, 12).
func TestLaunchKeepsCursorInActionable(t *testing.T) {
	for _, q := range queueTabs() {
		for _, c := range []struct {
			name, from, want string
			n                int
			k                tea.KeyMsg
		}{
			{"b middle", "act:x2", "act:x3", 3, runeKey("b")},
			{"b last", "act:x3", "act:x2", 3, runeKey("b")},
			{"b only", "act:x1", actKey + "/none", 1, runeKey("b")},
			{"enter middle", "act:x2", "act:x3", 3, enterKey()},
			{"enter last", "act:x3", "act:x2", 3, enterKey()},
			{"enter only", "act:x1", actKey + "/none", 1, enterKey()},
		} {
			t.Run(q.name+" "+c.name, func(t *testing.T) {
				m := q.at(t, q.model(t, c.n), c.from)
				m, _ = send(t, m, c.k)
				if got := q.sel(m); got != c.want {
					t.Errorf("cursor %q, want %q", got, c.want)
				}
			})
		}
	}
}

// A launch from a Live row leaves the selection alone (an alive session just attaches or is named).
func TestLaunchFromLiveRowLeavesCursor(t *testing.T) {
	for _, q := range queueTabs() {
		t.Run(q.name, func(t *testing.T) {
			m := q.at(t, q.model(t, 2), "live:lv")
			m, _ = send(t, m, runeKey("b"))
			if got := q.sel(m); got != "live:lv" {
				t.Errorf("cursor %q", got)
			}
		})
	}
}

// reachable lists every row key the tab's cursor reaches, top to bottom.
func reachable(t *testing.T, q queueTab, m Model) map[string]bool {
	keys := map[string]bool{}
	for range 40 {
		keys[q.sel(m)] = true
		m = press(t, m, runeKey("j"))
	}
	return keys
}

// A launched bead stays in Live through snapshots that don't show it alive, and moves
// to Actionable only when the session really waits, a snapshot comes 30 s on, or the
// launch failed (FR 6-10).
func TestLaunchHoldSections(t *testing.T) {
	for _, q := range queueTabs() {
		launched := func(t *testing.T) Model {
			m := q.at(t, q.model(t, 2), "act:x1")
			m, _ = send(t, m, runeKey("b"))
			m, _ = send(t, m, launchDoneMsg{b: bd.Bead{ID: "x1"}, bg: true})
			return m
		}
		load := func(t *testing.T, m Model, after time.Duration, s map[string]session.Info, claimed ...bool) Model {
			sess := map[string]session.Info{"lv": {State: session.Alive}}
			maps.Copy(sess, s)
			beads := slices.Clone(m.beads)
			for i := range beads {
				if _, ok := s["x1"]; (ok || len(claimed) > 0) && beads[i].ID == "x1" {
					beads[i].Status = "in_progress" // the launch claimed it, its session shows
				}
			}
			m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess, started: time.Now().Add(after)})
			return m
		}
		for _, c := range []struct {
			name    string
			after   time.Duration
			s       map[string]session.Info
			want    string
			claimed bool
		}{
			{"claimed, no session yet", time.Second, nil, "live:x1", true},
			{"no session yet", time.Second, nil, "live:x1", false},
			{"alive and working", time.Second, map[string]session.Info{"x1": {State: session.Alive}}, "live:x1", false},
			{"alive and waiting", time.Second, map[string]session.Info{"x1": {State: session.Alive, Waiting: session.WaitTurn}}, "act:x1", false},
			{"30 s on, no session", launchHoldMax + time.Second, nil, "act:x1", false},
		} {
			t.Run(q.name+" "+c.name, func(t *testing.T) {
				m := load(t, launched(t), c.after, c.s, claimedArg(c.claimed)...)
				if keys := reachable(t, q, m); !keys[c.want] {
					t.Errorf("rows %v lack %s", keys, c.want)
				}
			})
		}
		t.Run(q.name+" failed launch", func(t *testing.T) {
			m := q.at(t, q.model(t, 2), "act:x1")
			m, _ = send(t, m, runeKey("b"))
			m, _ = send(t, m, launchDoneMsg{b: bd.Bead{ID: "x1"}, err: errors.New("boom")})
			if keys := reachable(t, q, m); !keys["act:x1"] {
				t.Errorf("rows %v lack act:x1", keys)
			}
		})
	}
}

func claimedArg(b bool) []bool {
	if b {
		return []bool{true}
	}
	return nil
}

// Launching the last Actionable bead parks the cursor on the placeholder, which has no bead:
// every queue tab must still render its detail (Execute once panicked in kindTag(nil)).
func TestLaunchLastRendersPlaceholderDetail(t *testing.T) {
	for _, q := range queueTabs() {
		t.Run(q.name, func(t *testing.T) {
			m := q.at(t, q.model(t, 1), "act:x1")
			m, _ = send(t, m, enterKey())
			if got := q.sel(m); got != actKey+"/none" {
				t.Fatalf("cursor %q, want the placeholder", got)
			}
			_ = m.View()
		})
	}
}
