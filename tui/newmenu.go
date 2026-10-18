package tui

// The New menu (ADR 0228's picker chrome): n opens a picker of the things the human can
// start, each fired by its own key.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/jira"
)

// The Jira seams, vars so tests reach neither acli nor bd. jiraSiteSet reads the
// Workspace's [jira] site at menu open; a malformed site counts as set so pressing j
// reports the problem.
var (
	intakeJira  = jira.Intake
	jiraSiteSet = func() bool { site, _ := cfglayer.JiraSite(repoRoot()); return site != "" }
)

var (
	keyNew      = verbKey("n", "new")
	keyNewClose = key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("n/esc", "close"))
	keyMoveUD   = key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↑/↓", "move")) // the picker's keyDown/keyUp pair
)

// newOpts builds the menu's rows in the fixed order s, j, i, u, r, d; the j row shows only
// with a [jira] site, the u row only when its target list is not empty and the r row only
// when the selection captured at open is an open Chart or Checkpoint (ADR 0241 §3).
func (m Model) newOpts() []gotoOpt {
	cp, ini := m.surgeryContext() // captured at open, so the form's default pick is where the human was
	opts := []gotoOpt{
		{keys: []string{"s"}, desc: "New Signal", run: func(m Model) (Model, tea.Cmd) { next, cmd, _ := m.openNewSignal(); return next.(Model), cmd }},
	}
	if m.jiraSet {
		opts = append(opts, gotoOpt{keys: []string{"j"}, desc: "New Signal from a Jira issue", run: func(m Model) (Model, tea.Cmd) {
			if m.refusePending(slot("intake jira")) {
				return m, nil
			}
			return m.push(jiraKeyDialog("")), nil
		}})
	}
	opts = append(opts, gotoOpt{keys: []string{"i"}, desc: "New Initiative", run: func(m Model) (Model, tea.Cmd) {
		next, cmd, _ := m.openNew(newInitiativeForm(m.initiatives.ExternalRef))
		return next.(Model), cmd
	}})
	if m.surgerySet {
		opts = append(opts, gotoOpt{keys: []string{"u"}, desc: "New Surgery", run: func(m Model) (Model, tea.Cmd) { return m.openSurgery(cp, ini) }})
	}
	if sel := m.selection(); sel != nil && canRebase(sel) {
		id := sel.ID // pinned at open, like the Surgery row
		opts = append(opts, gotoOpt{keys: []string{"r"}, desc: "New rebase request", run: func(m Model) (Model, tea.Cmd) { return m.rebaseRequest(id) }})
	}
	return append(opts, gotoOpt{keys: []string{"d"}, desc: "New Discussion", run: func(m Model) (Model, tea.Cmd) { return m.discussionNew() }})
}

// jiraKeyDialog is the one-field Jira key form, holding typed on a retry.
func jiraKeyDialog(typed string) dialog {
	d := inputDialog("New Signal from a Jira issue", []string{"key"}, "", fileJira)
	d.inputs[0] = lineAt(typed)
	return d
}

// fileJira files the typed key through jira.Intake off the event loop, on its own
// pending slot.
func fileJira(m Model, d dialog) (Model, tea.Cmd) {
	typed := strings.ToUpper(strings.TrimSpace(d.inputs[0].text))
	if typed == "" {
		d.body = []string{warnSt.Render("intake jira: key required")}
		return m.push(d), nil
	}
	pend := slot("intake jira")
	next, cmd := m.startWrite(pend, "filing", func() writeDoneMsg {
		res, err := intakeJira(repoRoot(), typed)
		return writeDoneMsg{pend, func(m Model) (tea.Model, tea.Cmd) {
			switch {
			case err != nil && res.ID == "":
				return m.retry(jiraKeyDialog(typed), "intake", errors.New(strings.TrimPrefix(err.Error(), "intake jira "+typed+": ")))
			case err != nil:
				m.setStatus(strings.TrimPrefix(err.Error(), "intake jira "+typed+": ")) // "filed <id> but could not supersede it onto <carrier>: …"
				return m, tea.Batch(m.load(), m.recordRecent(res.ID))
			}
			s := "filed " + res.ID + " from " + res.Key
			if res.Key != typed {
				s += " (" + typed + " is now " + res.Key + ")"
			}
			if res.Carrier != "" {
				s += ", superseded onto " + res.Carrier
			}
			m.setStatus(s)
			return m, tea.Batch(m.load(), m.recordRecent(res.ID))
		}}
	})
	mm := next.(Model)
	if cmd != nil { // not refused: the status names the key, not the slot
		mm.setStatus("filing " + typed + "…")
	}
	return mm, cmd
}

// newBindings is n, for the hints bar, the ? reference and binding coverage.
func (m Model) newBindings() []key.Binding { return []key.Binding{keyNew} }

// scanNew reads what decides the u row when n or ? opens, not while rendering (ADR 0101 §4).
func (m Model) scanNew() Model {
	m.surgerySet = slices.ContainsFunc(surgeryRows(m.beads, m.initiatives.Required), sgRow.selectable)
	return m
}

// newOpen is n: the menu over the frame.
func (m Model) newOpen() (tea.Model, tea.Cmd) {
	m = m.scanNew()
	opts := m.newOpts()
	items := make([]string, len(opts))
	rowKeys := make([][]string, len(opts))
	for i, o := range opts {
		items[i], rowKeys[i] = fmt.Sprintf("%-4s", strings.Join(o.keys, "/"))+o.desc, o.keys
	}
	d := pickerDialog("New", items, "", func(m Model, d dialog) (Model, tea.Cmd) { return opts[d.cur].run(m) })
	d.newMenu, d.shut, d.rowKeys, d.opts = true, keyNewClose, rowKeys, optBindings(opts)
	return m.push(d), nil
}
