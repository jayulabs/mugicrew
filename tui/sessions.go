package tui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

var (
	keySessions      = key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sessions"))
	keySessionsClose = key.NewBinding(key.WithKeys("esc", "S"), key.WithHelp("esc/S", "close"))
	keySessAttach    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "attach/resume"))
	keySessAbort     = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "abort"))
)

// sessRow is one Sessions modal row: its bead, whether it waits on the human, when it
// started, and the state word drawn.
type sessRow struct {
	waiting bool
	start   time.Time
	state   string
	b       bd.Bead
}

// sessionRows derives the modal's rows from the live snapshot: every alive or exited
// detached session not closing (a closed bead only when alive: done, ADR 0067 §4), the waiting ones first (longest
// first), then the rest newest first; and the AFK efforts (alive, no detached session),
// newest claim first.
func (m Model) sessionRows() (hitl, afk []sessRow) {
	g := m.gateData()
	var rows []sessRow
	for _, b := range m.beads {
		s := m.sessions[b.ID]
		closed := b.Status == "closed"
		if !s.Has() && g.liveOf(&b) == alive {
			afk = append(afk, sessRow{b: b, start: cmp.Or(b.Started, b.Created), state: "running"})
			continue
		}
		// a closed bead keeps its row only as a done session (alive, waiting on /exit)
		if !s.Has() || (closed && s.State != session.Alive) || (s.State == session.Alive && s.Closing) {
			continue
		}
		r := sessRow{b: b, start: cmp.Or(b.Started, b.Created), state: "working"}
		switch w := humanWait(s, closed); {
		case closed:
			r.state = doneLook.word
		case s.State == session.Exited:
			r.state = "exited"
		case w == session.WaitTurn:
			r.state, r.waiting = "your turn", true
		case w == session.WaitAsk:
			r.state, r.waiting = "needs approval", true
		}
		rows = append(rows, r)
	}
	slices.SortStableFunc(rows, func(a, b sessRow) int {
		if a.waiting != b.waiting {
			if a.waiting {
				return -1
			}
			return 1
		}
		if a.waiting {
			return m.waitSince[a.b.ID].Compare(m.waitSince[b.b.ID])
		}
		return b.start.Compare(a.start)
	})
	slices.SortStableFunc(afk, func(a, b sessRow) int { return b.start.Compare(a.start) })
	return rows, afk
}

// sessionItems are the rows' drawn lines: the bead in SBRN (no dot slot, kind column
// padded to the widest tag, as Recents) then the dim extras · <state> · <age>.
func (m Model) sessionItems(rows []sessRow) []string {
	g, inner, tagW := m.gateData(), m.pickerInner(), 0
	for _, r := range rows {
		tagW = max(tagW, lipgloss.Width(kindTag(&r.b)))
	}
	items := make([]string, len(rows))
	for i, r := range rows {
		tail := fmt.Sprintf(" · %s · %s", r.state, HumanAge(r.start))
		fixed := lipgloss.Width(g.sbrn(&r.b, "", false, tagW))
		title := ansi.Truncate(r.b.Title, max(inner-fixed-ansi.StringWidth(tail), 1), "…")
		items[i] = g.sbrn(&r.b, title, false, tagW) + dimSt.Render(tail)
	}
	return items
}

// sessionEnter is enter on a row: an AFK row takes the tab's run-now refusal, any
// other the tab rows' attach/resume path, on the bead the highlight was pinned to (d.id),
// not whatever index a refresh moved there.
func sessionEnter(m Model, d dialog) (Model, tea.Cmd) {
	b, ok := m.pinned(d.id)
	if !ok {
		m.setStatus("session " + d.id + " no longer listed")
		return m, nil
	}
	g := m.gateData()
	if g.launches(&b) {
		return into(m.launchAs(g, b, false))
	}
	return into(m.runNow(g, b))
}

// sessionsDialog is the modal over rows, the highlight on session id; when that session
// left, on the row now at cur (clamped).
func (m Model) sessionsDialog(hitl, afk []sessRow, id string, cur int) dialog {
	rows := slices.Concat(hitl, afk)
	d := pickerDialog("Sessions", m.sessionItems(rows), "", sessionEnter)
	d.sessions, d.shut = true, keySessionsClose
	// x keeps the modal under the y/n dialog: dropAct runs after the pop, so put it back
	d.dropAct = func(m Model, d dialog) (Model, tea.Cmd) { return into(m.push(d).sessionsDrop(d.id)) }
	if len(rows) == 0 {
		d.body = []string{"no sessions"}
		return d
	}
	d.heads, d.afkAt = map[int]string{}, len(hitl)
	if len(hitl) > 0 {
		d.heads[0] = "HITL"
	}
	if len(afk) > 0 {
		d.heads[len(hitl)] = "AFK"
	}
	d.ids = make([]string, len(rows))
	for i, r := range rows {
		d.ids[i] = r.b.ID
	}
	d.cur = slices.Index(d.ids, id)
	if d.cur < 0 {
		d.cur = min(cur, len(rows)-1)
	}
	d.id = d.ids[d.cur]
	return d
}

// sessionsOpen is S: the Sessions modal over the frame; a second S pops it.
func (m Model) sessionsOpen() (tea.Model, tea.Cmd) {
	if n := len(m.modals); n > 0 && m.modals[n-1].sessions {
		return m.pop(), nil
	}
	hitl, afk := m.sessionRows()
	return m.push(m.sessionsDialog(hitl, afk, "", 0)), nil
}

// sessionsSync re-derives every Sessions modal in the stack after a refresh, not only the
// top one (the drop dialog can sit over it): the highlight stays on its session, or clamps
// when that session left.
func (m Model) sessionsSync() Model {
	hitl, afk := m.sessionRows()
	m.modals = slices.Clone(m.modals)
	for i, d := range m.modals {
		if d.sessions {
			m.modals[i] = m.sessionsDialog(hitl, afk, d.id, d.cur)
		}
	}
	return m
}

// sessionsDrop is x: the highlighted session, pinned by bead id, drops through the same
// y/n dialog its tab (or the D modal, for a discussion) raises.
func (m Model) sessionsDrop(id string) (tea.Model, tea.Cmd) {
	b, ok := m.pinned(id)
	if !ok {
		m.setStatus("session " + id + " no longer listed")
		return m, nil
	}
	if b.Kind == contract.KindDiscussion {
		return m.discussionDrop(b)
	}
	return m.dropKey(m.gateData(), b)
}
