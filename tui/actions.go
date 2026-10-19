package tui

// Graph's action keys (FR26, FR32, FR34): launch, run-now, pause, priority and
// drop/abort/recover over the modal stack. Each guarded action opens a y/n
// dialog under ADR 0142 §7 and calls its seam only on confirm; every other one fires
// at once. Writes go off the event loop under the per-key guard (ADR 0098).

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// into narrows an action's (tea.Model, Cmd) back to the Model a dialog's act returns.
func into(next tea.Model, cmd tea.Cmd) (Model, tea.Cmd) { return next.(Model), cmd }

// pinned is the bead id in the current snapshot; false if a refresh dropped it.
func (m Model) pinned(id string) (bd.Bead, bool) {
	i := beadIndex(m.beads, id)
	if i < 0 {
		return bd.Bead{}, false
	}
	return m.beads[i], true
}

// actionKey runs Graph's action keys over the selection; ok is false for any other key.
func (m Model) actionKey(k tea.KeyMsg) (next tea.Model, cmd tea.Cmd, ok bool) {
	g := m.gateData()
	var act func(b bd.Bead) (tea.Model, tea.Cmd)
	switch {
	case key.Matches(k, keyLaunch):
		act = func(b bd.Bead) (tea.Model, tea.Cmd) {
			if g.launches(&b) {
				return m.launchAs(g, b, false)
			}
			return m.runNow(g, b)
		}
	case key.Matches(k, keyBg):
		act = func(b bd.Bead) (tea.Model, tea.Cmd) { return m.launchAs(g, b, true) }
	case key.Matches(k, keyPauseKey):
		act = func(b bd.Bead) (tea.Model, tea.Cmd) { return m.togglePause(g, b) }
	case key.Matches(k, keyDropKey):
		act = func(b bd.Bead) (tea.Model, tea.Cmd) { return m.dropKey(g, b) }
	case key.Matches(k, keyWithdrawKey):
		act = func(b bd.Bead) (tea.Model, tea.Cmd) { return m.withdrawKey(g, b) }
	case key.Matches(k, keyPrio):
		d := 1 // + and = raise (P-1), - lowers
		if s := k.String(); s == "+" || s == "=" {
			d = -1
		}
		act = func(b bd.Bead) (tea.Model, tea.Cmd) { return m.bumpPriority(b, d) }
	default:
		return m, nil, false
	}
	b := m.selection()
	if b == nil && key.Matches(k, keyWithdrawKey) {
		if i := m.initRow(); isOpenInitiative(i) {
			next, cmd = m.withdrawKey(g, *i)
			return next, cmd, true
		}
	}
	if b == nil {
		m.setStatus("nothing selected")
		if gt, isGraph := m.tabAt(m.tab).(graphTab); isGraph && gt.sel == unparentedSel {
			m.setStatus("Unparented is a group header — select a Signal and press enter to launch Luffy to triage it, or a Rollback and press enter to run it")
		}
		return m, nil, true
	}
	next, cmd = act(*b)
	return next, cmd, true
}

// prioDir is a priority key's step: + and = raise (P-1), - lowers (P+1).
func prioDir(k tea.KeyMsg) int {
	if s := k.String(); s == "+" || s == "=" {
		return -1
	}
	return 1
}

// launchAs is enter's and b's path: bg starts or resumes without the switch, and an
// alive session is only named, not attached (ADR 0100). An attach or a plain launch
// fires at once; only a bead with open blockers or a pause asks first (ADR 0142 §7).
func (m Model) launchAs(g graphData, b bd.Bead, bg bool) (tea.Model, tea.Cmd) {
	if why := g.launchWhy(&b); why != "" {
		m.setStatus(why)
		return m, nil
	}
	if s := m.sessions[b.ID]; s.Has() {
		// Reattach an alive session, resume an exited one in its kept worktree, whatever
		// the bead's status and with no blocked confirm: nothing new is launched (ADR 0038 §3).
		if s.State == session.Exited {
			ctx := m.ctx
			return m.startLaunch(b, bg, func() launchDoneMsg {
				pctx, err := resumeCtx(ctx, b, s.Profile)
				if err != nil {
					return launchDoneMsg{b: b, resume: true, refused: true, err: err}
				}
				return launchDoneMsg{b: b, resume: true, err: resumeSession(b, s, pctx.Harness())}
			})
		}
		if bg {
			m.setStatus("already running — enter attaches")
			return m, nil
		}
		m.attachTo = "" // an instant attach outranks any launch still in flight (ADR 0107 §6)
		next, cmd := m.attach(b.ID)
		return next, tea.Batch(cmd, m.recordRecent(b.ID))
	}
	blocked, pause := g.blocked(&b), g.pauseLine(&b)
	paused := pause != ""
	if !blocked && !paused {
		return m.launchNow(b, bg)
	}
	title := "Launch blocked bead?"
	switch {
	case blocked && paused:
		title = "Launch blocked, paused bead?"
	case paused:
		title = "Launch paused bead?"
	}
	blockers := g.heldBy(&b)
	prompt := func(w int) []string {
		var body []string
		if paused {
			body = append(body, ansi.Truncate("paused: "+pause, w, "…"))
		}
		if blocked {
			body = append(body, confirmBlockedPrompt("launch", b.Title, blockers, w))
		}
		return body
	}
	return m.push(promptDialog(title, prompt, b.ID,
		func(m Model, d dialog) (Model, tea.Cmd) {
			// Resolve the pinned id, not the selection: a refresh may have moved it.
			cur, ok := m.pinned(d.id)
			if !ok || m.gateData().launchWhy(&cur) != "" {
				m.setStatus("launch cancelled: " + d.id + " no longer available")
				return m, nil
			}
			return into(m.launchNow(cur, bg))
		})), nil
}

// launchNow is the launch path proper: owner routing, then a detached tmux session
// (worktree cwd, busy interlock) the TUI attaches to unless bg. The command is
// resolved inside the Cmd: a crewmate's Launch may read bd (ADR 0101 §3).
func (m Model) launchNow(b bd.Bead, bg bool) (tea.Model, tea.Cmd) {
	ctx, crew := m.ctx, m.crew
	return m.startLaunch(b, bg, func() launchDoneMsg {
		pctx, profile, err := launchCtx(ctx, b) // before the claim: a refusal neither claims nor pauses
		if err != nil {
			return launchDoneMsg{b: b, refused: true, err: err}
		}
		lc, err := launchFor(pctx, crew, b)
		if err != nil {
			return launchDoneMsg{b: b, err: err}
		}
		c, ok := lc.(contract.AgentCommand)
		switch {
		case !ok:
			return launchDoneMsg{b: b, err: fmt.Errorf("unknown command %T", lc)}
		case len(c.Argv) == 0:
			return launchDoneMsg{b: b, err: errors.New("empty command")}
		}
		c.Profile = profile
		// The bead is claimed first, as AFK dispatch does: a failed claim starts
		// nothing, and a failed start releases the claim (ADR 0034). A Surgery's
		// filing already claimed it, and an idle in_progress Task is resumed.
		resume := (b.Kind == contract.KindTask || b.Kind == contract.KindDiscussion || b.Kind == contract.KindCheckup) && b.Status == "in_progress"
		claim := b.Kind != contract.KindSurgery && !resume
		if claim {
			if err := hitlClaim(b.ID); err != nil {
				var held *bd.HeldError
				switch {
				case errors.As(err, &held):
					return launchDoneMsg{b: b, refused: true, reload: true, err: fmt.Errorf("%s is claimed by %s — not launched", b.ID, held.Holder)}
				case errors.Is(err, bd.ErrClaimedSince):
					return launchDoneMsg{b: b, refused: true, reload: true, err: fmt.Errorf("%s was claimed since the last refresh — not launched", b.ID)}
				}
				return launchDoneMsg{b: b, err: fmt.Errorf("claim: %w", err)}
			}
		}
		d, _ := route.Route(b) // startSession routes too: a route error never reaches the busy check
		err = startSession(b, c)
		if err != nil && claim {
			_ = hitlUnclaim(b.ID) // ponytail: a failed release strands the claim; bd unclaim by hand
		}
		msg := launchDoneMsg{b: b, context: contexts.Owner(b, d.Container), branch: d.Branch, err: err}
		if _, busy := contexts.Busy(msg.owner(), err, d.Branch); busy {
			msg.afk, msg.afkErr = afkHolder(d.Branch)
		}
		return msg
	})
}

// launchDone handles a start/resume outcome: attach on success only if this launch
// still holds the attach slot; a b start, or an enter displaced from the slot, only
// names itself and reloads (ADR 0100, ADR 0107 §6). A start git refused because a
// listed worktree holds the branch goes to onBusy. The in-flight mark clears on
// every outcome. A success that lands under a dialog only reloads, rather than
// switching away from the open prompt.
func (m Model) launchDone(msg launchDoneMsg) (tea.Model, tea.Cmd) {
	m.launching = maps.Clone(m.launching)
	delete(m.launching, msg.b.ID)
	if msg.err == nil {
		m.launched = maps.Clone(m.launched)
		if m.launched == nil {
			m.launched = map[string]time.Time{}
		}
		m.launched[msg.b.ID] = time.Now()
	}
	slot := m.attachTo == msg.b.ID
	if slot {
		m.attachTo = ""
	}
	switch {
	case msg.err == nil && len(m.modals) > 0:
		m.setStatus("started " + cmp.Or(msg.b.Title, msg.b.ID) + " — enter attaches")
		return m, m.load()
	case msg.err == nil && !slot:
		s := "started " + cmp.Or(msg.b.Title, msg.b.ID) + " — enter attaches"
		if !msg.bg {
			s += " (downgraded: a later enter took the attach)"
		}
		m.setStatus(s)
		return m, m.load()
	case msg.err == nil:
		m.setStatus("")
		return m.attach(msg.b.ID)
	case msg.refused: // nothing ran, nothing changed: no reload, unless the snapshot was stale
		m.setStatus(msg.err.Error())
		if msg.reload {
			return m, m.load()
		}
		return m, nil
	case msg.resume:
		s := "resume: " + msg.err.Error()
		if fresh, ferr := readSessions(); ferr == nil && fresh[msg.b.ID].State == session.Exited {
			s += " — x drops it"
		}
		m.setStatus(s)
		return m, m.load()
	}
	if h, busy := contexts.Busy(msg.owner(), msg.err, msg.branch); busy {
		return m.onBusy(msg, h.Path)
	}
	m.setStatus(launchFailed(msg.b, msg.err))
	return m, nil
}

// attach switches this terminal's client to id's session (ADR 0065 §1); it reloads
// either way: the session may have just been started or resumed.
func (m Model) attach(id string) (tea.Model, tea.Cmd) {
	if err := switchSession(id); err != nil {
		m.setStatus("attach: " + err.Error())
	}
	return m, m.load()
}

// onBusy answers a fresh launch git refused as busy (ADR 0041 §2–3): an orphaned
// worktree holding the bead's branch opens the orphan dialog; any other holder
// keeps the busy status, naming the holding bead where one is known. Any read that
// fails keeps the plain busy status, never a dialog on a guess.
func (m Model) onBusy(msg launchDoneMsg, wt string) (tea.Model, tea.Cmd) {
	b, afk, afkErr := msg.b, msg.afk, msg.afkErr
	busy := func(holder string) (tea.Model, tea.Cmd) {
		m.setStatus(b.ID + ": " + string(b.Kind) + " branch busy — held by " + holder + ", retry")
		return m, nil
	}
	sessions, sessErr := readSessions()
	if afk != "" {
		return busy(afk + "'s AFK effort")
	}
	if id := session.HolderBead(wt, sessions); id != "" {
		return busy(id + "'s session")
	}
	for id := range m.launching { // a tree an in-flight launch is still building is never an orphan (ADR 0107 §5)
		if o, ok := m.pinned(id); ok {
			if d, err := route.Route(o); err == nil && d.Branch == msg.branch {
				return busy(id + "'s launch")
			}
		}
	}
	if afkErr != nil || sessErr != nil || !session.Orphan(wt, sessions) {
		return busy("another session")
	}
	if len(m.modals) > 0 { // never stack the orphan dialog on one the human is answering
		return busy("an orphaned worktree " + wt)
	}
	bg := msg.bg
	return m.push(promptDialog("Drop orphaned worktree?", func(w int) []string { return []string{confirmOrphanPrompt(b.Title, wt, w)} }, b.ID,
		func(m Model, d dialog) (Model, tea.Cmd) {
			cur, ok := m.pinned(d.id)
			if !ok {
				m.setStatus("launch cancelled: " + d.id + " no longer available")
				return m, nil
			}
			return into(m.startAct(cur.ID, "dropping orphan for", func() writeDoneMsg {
				err := dropOrphan(wt)
				return writeDoneMsg{cur.ID, func(m Model) (tea.Model, tea.Cmd) {
					if err != nil {
						m.setStatus("drop orphan: " + err.Error())
						return m, nil
					}
					return m.launchNow(cur, bg)
				}}
			}))
		})), nil
}

// dropKey is x: a session drops, a live AFK effort aborts, a dead one recovers,
// each behind a dialog (ADR 0142 §7).
func (m Model) dropKey(g graphData, b bd.Bead) (tea.Model, tea.Cmd) {
	if why := g.busyWhy(&b); why != "" { // never drop mid-start/resume or mid-close, nor stack on a write (ADR 0107 §2, ADR 0108 §5)
		m.setStatus(why)
		return m, nil
	}
	if b.Kind == contract.KindCheckup && !m.sessions[b.ID].Has() {
		return m.discussionDrop(b)
	}
	switch g.xLabel(&b) {
	case "drop":
		s := m.sessions[b.ID]
		sess := s.Session
		drop := func(m Model, d dialog) (Model, tea.Cmd) { return into(m.drop(d.id, sess)) }
		if s.Armed() { // x finishes or drops an armed session; unarmed stays drop-only
			d := promptDialog("Finish session?", func(w int) []string { return []string{confirmFinishPrompt(b.Title, w)} }, b.ID,
				func(m Model, d dialog) (Model, tea.Cmd) { return into(m.finish(d.id)) })
			d.alt = drop
			return m.push(d), nil
		}
		return m.push(promptDialog("Drop session?", func(w int) []string { return []string{confirmDropPrompt(b.Title, w)} }, b.ID,
			drop)), nil
	case "abort":
		return m.push(promptDialog("Abort effort?", func(w int) []string { return []string{confirmAbortPrompt(b.Title, w)} }, b.ID,
			func(m Model, d dialog) (Model, tea.Cmd) {
				id := d.id
				return into(m.startAct(id, "aborting", func() writeDoneMsg {
					pids, held, err := abortEffort(id, "tui")
					return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) { return m.abortDone(id, pids, held, err) }}
				}))
			})), nil
	case "recover":
		return m.openRecover(g, b)
	}
	if strandable(&b) && bd.ClaimedElsewhere(b.Assignee) { // restsClaimed kinds read differently on purpose (D6): x has nothing to offer there
		m.setStatus(claimLine(&b))
		return m, nil
	}
	if b.Kind == contract.KindInitiative { // a container with no effort (Spec D10)
		m.setStatus(b.ID + " is an Initiative — it has no effort to abort; cancel or withdraw it with mugicrew initiative")
	} else {
		m.setStatus(b.ID + ": no session to drop, effort to abort or leftover to recover")
	}
	return m, nil
}

// drop kills id's session and releases its claim, off the event loop; the bead and
// session are re-read inside so a refresh mid-dialog can't retarget y.
func (m Model) drop(id, sess string) (tea.Model, tea.Cmd) {
	var b *bd.Bead
	if p, ok := m.pinned(id); ok {
		b = &p
	}
	return m.startAct(id, "dropping", func() writeDoneMsg {
		status, reload := dropRun(id, sess, b)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			m.setStatus(status)
			if !reload {
				return m, nil
			}
			return m, m.load()
		}}
	})
}

// finish pulls id's Auto-exit deadline forward off the event loop; hitl-run's clock
// then runs the normal close. The bead is re-read so a refresh mid-dialog can't
// retarget y; a session that disarmed meanwhile is left alone.
func (m Model) finish(id string) (tea.Model, tea.Cmd) {
	p, ok := m.pinned(id)
	if !ok {
		m.setStatus("finish cancelled: " + id + " is no longer listed")
		return m, nil
	}
	return m.startAct(id, "finishing", func() writeDoneMsg {
		armed, err := finishSession(p)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			switch {
			case err != nil:
				m.setStatus("finish: " + err.Error())
				return m, nil
			case !armed:
				m.setStatus(id + " is no longer armed — not finished (x again to drop)")
			default:
				m.setStatus("finishing " + id + "'s session")
			}
			return m, m.load()
		}}
	})
}

// abortDone is the abort write's done closure: the signalled pids, paused only, or
// the error (a kill error keeps what was signalled), then a reload.
func (m Model) abortDone(id string, signalled []int, held bool, err error) (tea.Model, tea.Cmd) {
	pids := dispatch.FormatPids(signalled)
	switch {
	case err != nil && pids != "":
		m.setStatus("abort: " + err.Error() + " (signalled " + pids + ")")
	case err != nil:
		m.setStatus("abort: " + err.Error())
	case pids != "":
		m.setStatus("aborted " + id + ": signalled " + pids)
	default:
		m.setStatus("aborted " + id + ": " + dispatch.AbortNote(held))
	}
	return m, m.load()
}

// recoverOpts are x's recover options, never forced (ADR 0127): the TUI's own
// session is left out of the TUI-alive read only when its run-now set holds
// nothing on id, since its own effort there is a live host.
func (m Model) recoverOpts(id string) func(discard bool) (chopper.Opts, error) {
	mine := slices.ContainsFunc(m.runs, func(r dispatch.Inflight) bool { return r.Bead.ID == id })
	return func(discard bool) (o chopper.Opts, err error) {
		o.Discard = discard
		if !mine {
			o.ExcludeTUISession, err = ownSession()
		}
		return o, err
	}
}

// openRecover reads the inventory and verdict now, under the bead's pending key (a
// pure read, but the key marks the row busy), so the dialog states what y repairs.
// An ambiguous verdict opens no dialog: the TUI never forces.
func (m Model) openRecover(g graphData, b bd.Bead) (tea.Model, tea.Cmd) {
	id, opts := b.ID, m.recoverOpts(b.ID)
	return m.startWrite(id, "checking", func() writeDoneMsg {
		o, err := opts(false)
		var inv chopper.Inventory
		var v string
		if err == nil {
			inv, v, err = assessRecover(id, o)
		}
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			var rf *chopper.Refused
			switch {
			case errors.As(err, &rf) && rf.Verdict == chopper.Ambiguous:
				m.setStatus(err.Error() + "; the TUI never forces: with the loop off, run mugicrew recover --force " + id)
			case err != nil:
				m.setStatus("recover: " + err.Error())
			case v == "":
				m.setStatus(id + ": nothing to recover")
			case len(m.modals) > 0: // a dialog opened meanwhile keeps the keys
				m.setStatus(id + ": recover read done; press x again")
			default:
				return m.push(promptDialog("Recover?", func(w int) []string { return []string{confirmRecoverPrompt(b.Title, v, inv, w)} }, id,
					func(m Model, d dialog) (Model, tea.Cmd) {
						if inv.Files == 0 {
							return into(m.recoverRun(d.id, false))
						}
						// A dirty tree is discarded only after a second yes.
						return m.push(confirmDialog("Discard changes?", []string{confirmDiscardPrompt(inv)}, d.id,
							func(m Model, d dialog) (Model, tea.Cmd) { return into(m.recoverRun(d.id, true)) })), nil
					})), nil
			}
			return m, nil
		}}
	})
}

// recoverRun is the confirmed recover, off the event loop. ponytail: Discard isn't
// bound to the confirmed file count; the verdict re-checked inside means no writer
// is left to grow it.
func (m Model) recoverRun(id string, discard bool) (tea.Model, tea.Cmd) {
	opts := m.recoverOpts(id)
	return m.startAct(id, "recovering", func() writeDoneMsg {
		o, err := opts(discard)
		var r chopper.Report
		if err == nil {
			r, err = recoverBead(id, o) // liveness re-checked inside
		}
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) { return m.recoverDone(id, r, err) }}
	})
}

// recoverDone shows what a recover did, or why it refused or failed, then reloads:
// a refusal or failure past the fence leaves paused:recovering to show.
func (m Model) recoverDone(id string, r chopper.Report, err error) (tea.Model, tea.Cmd) {
	switch {
	case err != nil:
		m.setStatus("recover: " + err.Error())
	case r.Verdict == "":
		m.setStatus(id + ": nothing to recover")
	default:
		s := "recovered " + id + ": " + strings.Join(r.Lines, "; ")
		if len(r.Remaining) > 0 {
			s += "; still paused " + strings.Join(r.Remaining, ", ")
		}
		m.setStatus(s)
	}
	return m, m.load()
}

// runNow is enter on an AFK row: dispatch a ready AFK bead's owner effort through the same entry point
// the loop uses, in the background. Eligibility is the loop's parity contract, so a
// human can't force work through a side door; the one exception (which the CLI and loop lack) is a bead with open
// blockers, whose confirmed y lets it skip the ready-frontier test (ADR 0142 §7).
func (m Model) runNow(g graphData, b bd.Bead) (tea.Model, tea.Cmd) {
	if b.Kind == contract.KindInitiative { // an Initiative has no effort to run (Spec D7)
		m.setStatus(b.ID + " is an Initiative — a container with no effort to run")
		return m, nil
	}
	if why := g.runNowWhy(&b); why != "" {
		m.setStatus(why)
		return m, nil
	}
	if g.blocked(&b) && b.Status == "open" {
		blockers := g.heldBy(&b)
		return m.push(promptDialog("Run blocked bead now?", func(w int) []string { return []string{confirmBlockedPrompt("run", b.Title, blockers, w)} }, b.ID,
			func(m Model, d dialog) (Model, tea.Cmd) {
				cur, ok := m.pinned(d.id)
				if !ok {
					m.setStatus("run cancelled: " + d.id + " no longer available")
					return m, nil
				}
				return into(m.runNowGo(cur, true))
			})), nil
	}
	return m.runNowGo(b, false)
}

// transcript is t: open, focus or reuse the Watch pane on the bead's transcript view
// (ADR 0249). The tmux call runs off the event loop; the TUI checks no session, the
// pane itself says "no sessions recorded".
func (m Model) transcript(id string) (tea.Model, tea.Cmd) {
	return m.startAct(id, "opening transcript of", func() writeDoneMsg {
		err := watchRun(id)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			if err != nil {
				m.setStatus("transcript: " + err.Error())
			} else {
				m.setStatus("transcript " + id)
			}
			return m, nil
		}}
	})
}

// runNowGo runs the reads, eligibility, pool check and claim in one Cmd under the
// bead's pending key (ADR 0098 §6): a second enter, or any other write, on it is refused.
func (m Model) runNowGo(bead bd.Bead, force bool) (tea.Model, tea.Cmd) {
	crews, ctx := m.crew, m.ctx
	return m.startAct(bead.ID, "dispatching", func() writeDoneMsg {
		status, crew, r := claimRunNow(ctx, bead, crews, force)
		return writeDoneMsg{bead.ID, func(m Model) (tea.Model, tea.Cmd) {
			if crew == nil {
				m.setStatus(status)
				return m, nil
			}
			// Tracked only once claimed, so a concurrent loop tick's busy count is truthful.
			m.runs = append(slices.Clip(m.runs), r)
			return m, runNowCmd(m.ctx, crew, r)
		}}
	})
}

// togglePause is p, the bare manual pause (ADR 0057 §6): no paused:manual → add it,
// paused:manual → lift it, closed → refused; foreign-hold is never touched. No
// dialog (p reverses it). The status suffix is composed from the bead as seen at
// press time.
func (m Model) togglePause(g graphData, b bd.Bead) (tea.Model, tea.Cmd) {
	if b.Status == "closed" {
		m.setStatus(b.ID + ": closed beads are never dispatched")
		return m, nil
	}
	id, verb, doing, write, done, why := b.ID, "pause", "pausing", pauseBead, "paused "+b.ID, ""
	if bd.PausedBy(b, bd.PausedManual) {
		verb, doing, write, done = "unpause", "unpausing", unpauseBead, "unpaused "+b.ID
		if anc := g.inheritedPause(&b); anc != "" {
			why = " — still paused via " + anc
		} else if b.Kind == contract.KindInitiative {
			why = " — its member chains resume"
		} else if b.Container {
			why = " — its members resume"
		}
	} else {
		if bd.PausedBy(b, bd.PausedForeignHold) {
			why = " — foreign-hold stays until the loop lifts it"
		}
		if b.Kind == contract.KindInitiative { // down every member chain, at any depth
			why += " — its open member chains are out of dispatch too"
		} else if b.Container { // the pause cascades through membership (ADR 0081)
			why += " — its open members are out of dispatch too"
			if b.Status == "in_progress" {
				why += "; the running effort continues"
			}
		} else if b.Status == "in_progress" {
			why += " — the running effort continues"
		}
	}
	return m.startAct(id, doing, func() writeDoneMsg {
		err := write(id, bd.PausedManual)
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			return m.doneOrErr(verb, done+why, err)
		}}
	})
}

// minPriority and maxPriority are the bounds bumpPriority stops at: P0 (highest) and P4.
const minPriority, maxPriority = 0, 4

// bumpPriority moves b's priority by d (-1 raises, +1 lowers) with one background
// `bd update <id> --priority N`, then reloads (ADR 0098 §4). Past P0/P4 or on a
// closed bead nothing is written and the status says why.
func (m Model) bumpPriority(b bd.Bead, d int) (tea.Model, tea.Cmd) {
	if b.Status == "closed" {
		m.setStatus(b.Title + " is closed — priority unchanged")
		return m, nil
	}
	p := b.Priority + d
	if p < minPriority || p > maxPriority {
		m.setStatus(fmt.Sprintf("%s is already P%d", b.ID, b.Priority))
		return m, nil
	}
	id := b.ID
	return m.startAct(id, "updating", func() writeDoneMsg {
		err := updateBead(id, []string{"--priority", strconv.Itoa(p)})
		return writeDoneMsg{id, func(m Model) (tea.Model, tea.Cmd) {
			// Echo on the bead as it is now, so a press before the reload steps from p.
			if i := beadIndex(m.beads, id); err == nil && i >= 0 {
				m.beads = slices.Clone(m.beads)
				m.beads[i].Priority = p
			}
			return m.doneOrErr("priority", fmt.Sprintf("%s → P%d", id, p), err)
		}}
	})
}

// The dialogs' wording: the title cut to what the width leaves.
const (
	minPromptTitle   = 6
	markedBlockedFmt = `"%s" is marked blocked (no open deps) — %s anyway?`
	blockedByFmt     = `%s "%s" anyway? blocked by: `
	dropPromptFmt    = `drop "%s"? kills the session and discards uncommitted changes in its worktree`
	finishPromptFmt  = `finish "%s"?  y finish (clean exit, publishes) · d drop (discards) · n cancel`
	abortPromptFmt   = `abort "%s"? pauses it (paused:manual) and SIGTERMs its running effort`
	recoverPromptFmt = `recover "%s"? %s; leftovers: %s`
	orphanPromptFmt  = `"%s": branch held by an orphaned worktree %s (crashed session) — drop it and launch?`
)

// confirmBlockedPrompt names the open blockers by truncated title, as many as fit
// in w columns, the rest as +N; with none the bead is status=blocked by hand.
func confirmBlockedPrompt(verb, title string, blockers []*bd.Bead, w int) string {
	if len(blockers) == 0 {
		return fitTitle(markedBlockedFmt, title, w, verb)
	}
	room := (w - lipgloss.Width(fmt.Sprintf(blockedByFmt, verb, ""))) / 2
	titles := make([]string, len(blockers))
	for i, b := range blockers {
		titles[i] = b.Title
	}
	return foldTitles(fmt.Sprintf(blockedByFmt, verb, trunc(title, max(minPromptTitle, room))), titles, "", w)
}

func confirmDropPrompt(title string, w int) string   { return fitTitle(dropPromptFmt, title, w) }
func confirmFinishPrompt(title string, w int) string { return fitTitle(finishPromptFmt, title, w) }
func confirmAbortPrompt(title string, w int) string  { return fitTitle(abortPromptFmt, title, w) }

func confirmRecoverPrompt(title, verdict string, inv chopper.Inventory, w int) string {
	return fitTitle(recoverPromptFmt, title, w, verdict, chopper.Leftovers(inv))
}

// confirmDiscardPrompt is recover's second dialog on a dirty tree: the file count y loses.
func confirmDiscardPrompt(inv chopper.Inventory) string {
	return fmt.Sprintf("discard %d changed files in %s? recover drops them with the tree", inv.Files, inv.Tree)
}

// confirmOrphanPrompt never cuts the path: it is what the human is asked to discard.
func confirmOrphanPrompt(title, wt string, w int) string {
	return fitTitle(orphanPromptFmt, title, w, wt)
}

// fitTitle renders a prompt format whose first %s is the title, cut to the w columns
// the rest of the wording leaves (never below minPromptTitle).
func fitTitle(format, title string, w int, rest ...any) string {
	room := w - lipgloss.Width(fmt.Sprintf(format, append([]any{""}, rest...)...))
	return fmt.Sprintf(format, append([]any{trunc(title, max(minPromptTitle, room))}, rest...)...)
}

// initRow is the Initiative whose own header row the cursor rests on, on the tabs
// whose header rows carry no bead; nil elsewhere. X cancels it.
func (m Model) initRow() *bd.Bead {
	if m.focus.id != "" {
		return nil
	}
	switch t := m.tabAt(m.tab).(type) {
	case ideaTab:
		return t.cursor().init
	case defogTab:
		if r, _ := t.selRow(); r.kind == dHead {
			return r.init
		}
	case execTab:
		if r, _ := t.selRow(); r.kind == eSec {
			return r.init
		}
	}
	return nil
}
