package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// stubSeams replaces every bd seam var (and the tmux ones the moved handlers
// use) with a recorder that succeeds, restoring them on cleanup. Each call
// appends "<seam> <args>" to the returned log.
func stubSeams(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	rec := func(name string, args ...any) {
		calls = append(calls, strings.TrimSpace(fmt.Sprintln(append([]any{name}, args...)...)))
	}
	origs := []func(){}
	save := func(restore func()) { origs = append(origs, restore) }
	{
		o := updateBead
		save(func() { updateBead = o })
		updateBead = func(id string, args []string) error { rec("updateBead", id, args); return nil }
	}
	{
		o := closeBead
		save(func() { closeBead = o })
		closeBead = func(id, r string) error { rec("closeBead", id); return nil }
	}
	{
		o, u := pauseBead, unpauseBead
		save(func() { pauseBead, unpauseBead = o, u })
		pauseBead = func(id, c string) error { rec("pauseBead", id); return nil }
		unpauseBead = func(id, c string) error { rec("unpauseBead", id); return nil }
	}
	{
		s, su, r := newSignal, newSurgery, newRollback
		save(func() { newSignal, newSurgery, newRollback = s, su, r })
		newSignal = func(string, string) (string, error) { rec("newSignal"); return "sig1", nil }
		newSurgery = func(string, string) (string, string, error) { rec("newSurgery"); return "srg1", "cp1", nil }
		newRollback = func(string, string, string) (string, error) { rec("newRollback"); return "rb1", nil }
	}
	{
		c, u := hitlClaim, hitlUnclaim
		save(func() { hitlClaim, hitlUnclaim = c, u })
		hitlClaim = func(id string) error { rec("hitlClaim", id); return nil }
		hitlUnclaim = func(id string) error { rec("hitlUnclaim", id); return nil }
	}
	{
		r, ip, p, s, c, l := readyFrontier, inProgressBeads, pausedBeads, showBead, claimBead, listAll
		save(func() { readyFrontier, inProgressBeads, pausedBeads, showBead, claimBead, listAll = r, ip, p, s, c, l })
		readyFrontier = func() ([]bd.Bead, error) { rec("readyFrontier"); return nil, nil }
		inProgressBeads = func() ([]bd.Bead, error) { rec("inProgressBeads"); return nil, nil }
		pausedBeads = func() ([]bd.Bead, error) { rec("pausedBeads"); return nil, nil }
		showBead = func(id string) (bd.Bead, error) { rec("showBead", id); return bd.Bead{ID: id}, nil }
		claimBead = func(b bd.Bead) error { rec("claimBead", b.ID); return nil }
		listAll = func() ([]bd.Bead, error) { rec("listAll"); return nil, nil }
	}
	{
		d, rs, ds := dropOrphan, readSessions, dropSession
		save(func() { dropOrphan, readSessions, dropSession = d, rs, ds })
		dropOrphan = func(wt string) error { rec("dropOrphan", wt); return nil }
		readSessions = func() (map[string]session.Info, error) { rec("readSessions"); return nil, nil }
		dropSession = func(b bd.Bead, _ session.Info) error { rec("dropSession", b.ID); return nil }
	}
	t.Cleanup(func() {
		for _, f := range origs {
			f()
		}
	})
	return &calls
}

// No bd call runs inside Update (ADR 0098 §1, Spec FR2/FR19/FR29): every
// bd-touching key returns with zero seam calls and a Cmd; running the Cmd and
// feeding its message back makes the expected calls, clears the pending write
// and makes no bd call of its own (only tmux's readSessions stays inline).
func TestKeysWriteOffTheLoop(t *testing.T) {
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	beadModel := func() Model {
		m := New(nil, nil)
		m.beads = []bd.Bead{
			{ID: "chart1", Kind: contract.KindChart, Container: true, Title: "Chart: X", Status: "open"},
			{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1", Status: "open", Priority: 2, Title: "Grill me"},
		}
		m.rebuildGroups()
		m.selGraph = "grl1"
		return m
	}
	signalModel := func() Model {
		m := New(nil, nil)
		m.beads = []bd.Bead{{ID: "sig1", Kind: contract.KindSignal, Status: "open", Title: "Junk"}}
		m.rebuildGroups()
		m.tab, m.selInbox = tabInbox, "sig1"
		return m
	}
	orphanModel := func() Model { // l's busy launch already opened the orphan confirm
		m := beadModel()
		m.crew = map[string]contract.Crewmate{"nami": &recordCrew{}}
		m.editMode, m.editID, m.orphanWT = editConfirmOrphan, "grl1", "/wt/orphan"
		return m
	}
	runModel := func() Model { // an AFK bead the stubbed (empty) ready read no longer lists
		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.beads = []bd.Bead{{ID: "r1", Kind: contract.KindResearch, Parent: "chart1", Status: "open"}}
		m.rebuildGroups()
		m.selGraph = "r1"
		return m
	}
	sessionModel := func() Model {
		m := beadModel()
		m.sessions = map[string]session.Info{"grl1": {Session: "s1", Worktree: "/wt/g", State: session.Exited}}
		return m
	}
	editModel := func(mode editMode, buf string) func() Model {
		return func() Model {
			m := beadModel()
			m.editMode, m.editID, m.editBuf = mode, "grl1", lineAt(buf)
			return m
		}
	}
	createModel := func(surgery bool, of string) func() Model { // the form is open with a title typed
		return func() Model {
			m := New(nil, map[string]contract.Crewmate{"franky": &sessionLaunchCrew{}})
			m.tab, m.editMode, m.createTitle, m.createSurgery, m.createFor = tabInbox, editCreate, lineAt("Fix"), surgery, of
			return m
		}
	}
	descPath := filepath.Join(t.TempDir(), "desc.md")
	descModel := func() Model { // $EDITOR has returned with the edited body
		os.WriteFile(descPath, []byte("Body\n"), 0o600)
		return beadModel()
	}
	busyModel := func() Model { // l's fresh start that git refuses: a foreign tree holds the branch
		origStart, origOn := startSession, git.WorktreeOn
		t.Cleanup(func() { startSession, git.WorktreeOn = origStart, origOn })
		startSession = func(bd.Bead, contract.AgentCommand) error { return exec.Command("sh", "-c", "exit 128").Run() }
		git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{Path: "/home/me/repo-feature"}, true, nil }
		m := beadModel()
		m.crew = map[string]contract.Crewmate{"nami": &recordCrew{}}
		return m
	}
	bgModel := func(sessions map[string]session.Info) func() Model { // b's start or resume: never a switch (ADR 0100)
		return func() Model {
			origStart, origResume, origSwitch := startSession, resumeSession, switchSession
			t.Cleanup(func() { startSession, resumeSession, switchSession = origStart, origResume, origSwitch })
			startSession = func(bd.Bead, contract.AgentCommand) error { return nil }
			resumeSession = func(bd.Bead, session.Info, contract.Harness) error { return nil }
			switchSession = func(id string) error { t.Errorf("b switched to %s", id); return nil }
			m := beadModel()
			m.ctx, m.crew, m.sessions = harnessCtx{claudecode.New("")}, map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}}, sessions
			return m
		}
	}
	bgBlocked := func() Model { // b on a blocked bead opens l's confirm; y keeps the background intent
		m := bgModel(nil)()
		m.beads[1].Status = "blocked"
		m.rebuildGroups()
		return m
	}
	bgOrphan := func() Model { // b's busy launch already opened the orphan confirm; the relaunch after the drop never switches
		m := bgModel(nil)()
		m.editMode, m.editID, m.orphanWT, m.launchBg = editConfirmOrphan, "grl1", "/wt/orphan", true
		return m
	}
	for _, c := range []struct {
		name    string
		model   func() Model
		keys    []tea.Msg
		calls   []string // after the Cmd runs, before its message is fed back
		pending string   // the key the press holds pending; "" for l's own launching mark
		status  string   // after the done message
	}{
		{"+", beadModel, []tea.Msg{key("+")}, []string{"updateBead grl1 [--priority 1]"}, "grl1", "grl1 → P1"},
		{"-", beadModel, []tea.Msg{key("-")}, []string{"updateBead grl1 [--priority 3]"}, "grl1", "grl1 → P3"},
		{"p", beadModel, []tea.Msg{key("p")}, []string{"pauseBead grl1"}, "grl1", "paused grl1"},
		{"t", runModel, []tea.Msg{key("t")}, []string{"readyFrontier", "inProgressBeads", "pausedBeads"}, "r1", "r1 is blocked / not ready — run-now only drives a bead already in the bd ready frontier"},
		{"z", signalModel, []tea.Msg{key("z"), tea.KeyMsg{Type: tea.KeyEnter}, key("y")}, []string{"closeBead sig1"}, "sig1", "rejected sig1"},
		{"orphan y", orphanModel, []tea.Msg{key("y")}, []string{"dropOrphan /wt/orphan"}, "grl1", "launching Grill me… (attaches when ready)"},
		// The stubbed table is empty, so the re-read refuses the drop by name.
		{"x", sessionModel, []tea.Msg{key("x"), key("y")}, []string{"readSessions"}, "grl1", "drop cancelled: grl1's session changed, look again"},
		{"title", editModel(editTitle, "New"), []tea.Msg{tea.KeyMsg{Type: tea.KeyEnter}}, []string{"updateBead grl1 [--title New]"}, "grl1", "updated grl1"},
		{"raw", editModel(editRaw, "--priority 1"), []tea.Msg{tea.KeyMsg{Type: tea.KeyEnter}}, []string{"updateBead grl1 [--priority 1]"}, "grl1", "updated grl1"},
		{"n", createModel(false, ""), []tea.Msg{tea.KeyMsg{Type: tea.KeyEnter}}, []string{"newSignal"}, "new Signal", "filed Signal sig1"},
		{"R", createModel(false, "cp-b"), []tea.Msg{tea.KeyMsg{Type: tea.KeyEnter}}, []string{"newRollback"}, "new Rollback", "filed Rollback rb1 of cp-b"},
		{"u", createModel(true, ""), []tea.Msg{tea.KeyMsg{Type: tea.KeyEnter}}, []string{"newSurgery"}, "new Surgery", "launching Fix… (attaches when ready)"},
		{"description", descModel, []tea.Msg{descEditedMsg{id: "grl1", path: descPath}}, []string{"updateBead grl1 [--description Body]"}, "grl1", "updated grl1 description"},
		{"b", bgModel(nil), []tea.Msg{key("b")}, []string{"hitlClaim grl1"}, "", "started Grill me — l attaches"},
		{"b resume", bgModel(map[string]session.Info{"grl1": {Session: "s1", State: session.Exited}}), []tea.Msg{key("b")}, nil, "", "started Grill me — l attaches"},
		{"b blocked y", bgBlocked, []tea.Msg{key("b"), key("y")}, []string{"hitlClaim grl1"}, "", "started Grill me — l attaches"},
		{"b orphan y", bgOrphan, []tea.Msg{key("y")}, []string{"dropOrphan /wt/orphan"}, "grl1", "launching Grill me…"},
		{"busy launch", busyModel, []tea.Msg{key("l")}, []string{"hitlClaim grl1", "hitlUnclaim grl1", "listAll"}, "", "grl1: grilling branch busy — held by another session, retry"},
	} {
		// The row word a write shows while it files (ADR 0107 §1); creates and launches show none.
		word := map[string]string{"+": "updating…", "-": "updating…", "p": "pausing…", "t": "dispatching…",
			"z": "rejecting…", "orphan y": "dropping…", "x": "dropping…", "title": "updating…", "raw": "updating…",
			"description": "updating…", "b orphan y": "dropping…"}[c.name]
		t.Run(c.name, func(t *testing.T) {
			calls := stubSeams(t)
			var next tea.Model = c.model()
			var cmd tea.Cmd
			for _, k := range c.keys {
				next, cmd = next.Update(k)
			}
			m := next.(Model)
			if len(*calls) != 0 || cmd == nil {
				t.Fatalf("press: seam calls %q, cmd %v; want none and a Cmd", *calls, cmd != nil)
			}
			if c.pending != "" && m.pending[c.pending] == "" {
				t.Fatalf("pending = %v; want %q", m.pending, c.pending)
			}
			if suf := ansi.Strip(m.rowSuffix(m.beadByID(c.pending))); word != "" && !strings.HasSuffix(suf, "  "+word) {
				t.Errorf("press: row suffix %q; want %q", suf, word)
			}
			msg := cmd()
			if got := fmt.Sprint(*calls); got != fmt.Sprint(c.calls) {
				t.Errorf("Cmd calls = %q; want %q", *calls, c.calls)
			}
			*calls = nil
			next, _ = m.Update(msg)
			if m = next.(Model); m.pending[c.pending] != "" || m.status != c.status {
				t.Errorf("done: pending %v status %q; want cleared and %q", m.pending, m.status, c.status)
			}
			if suf := ansi.Strip(m.rowSuffix(m.beadByID(c.pending))); word != "" && strings.Contains(suf, word) {
				t.Errorf("done: row suffix %q still reads %q", suf, word)
			}
			if slices.ContainsFunc(*calls, func(s string) bool { return s != "readSessions" }) {
				t.Errorf("done: seam calls %q; want no bd call on the loop", *calls)
			}
		})
	}
}

// An inline editor stays open and ignores keys while its commit files, keeps the
// typed text on failure and closes on success; the $EDITOR temp file outlives
// Update and goes when the commit's Cmd finishes (Spec FR10–12).
func TestEditCommitFiling(t *testing.T) {
	stubSeams(t)
	editing := func(mode editMode, buf string) Model {
		m := New(nil, nil)
		m.beads = []bd.Bead{{ID: "grl1", Kind: contract.KindGrilling, Status: "open", Title: "Old"}}
		m.rebuildGroups()
		m.selGraph = "grl1"
		m.editMode, m.editID, m.editBuf = mode, "grl1", lineAt(buf)
		return m
	}
	for _, fail := range []bool{true, false} {
		updateBead = func(string, []string) error {
			if fail {
				return fmt.Errorf("dolt locked")
			}
			return nil
		}
		for _, mode := range []editMode{editTitle, editRaw} {
			next, cmd := editing(mode, "--title New").Update(tea.KeyMsg{Type: tea.KeyEnter})
			for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("x")}, {Type: tea.KeyBackspace}, {Type: tea.KeyEsc}} {
				next, _ = next.Update(k)
			}
			if m := next.(Model); m.editMode != mode || m.editBuf.text != "--title New" {
				t.Fatalf("mode %d filing: mode %d buf %q; want keys ignored", mode, m.editMode, m.editBuf.text)
			}
			next, _ = next.Update(cmd())
			m := next.(Model)
			if fail && (m.editMode != mode || m.editBuf.text != "--title New" || m.status != "edit: dolt locked") {
				t.Errorf("mode %d failed: mode %d buf %q status %q; want kept and the error", mode, m.editMode, m.editBuf.text, m.status)
			}
			if !fail && (m.editMode != editNone || m.editBuf.text != "" || m.status != "updated grl1") {
				t.Errorf("mode %d done: mode %d buf %q status %q; want closed", mode, m.editMode, m.editBuf.text, m.status)
			}
		}
		// A write in flight on the bead (only the editor's own, as the openers refuse
		// a pending bead): the editor ignores keys, enter included, keeping its text.
		busy := editing(editTitle, "New")
		busy.pending = map[string]string{"grl1": "updating"}
		next, cmd := busy.Update(tea.KeyMsg{Type: tea.KeyEnter})
		next, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
		if m := next.(Model); cmd != nil || m.editBuf.text != "New" {
			t.Errorf("guarded commit: cmd %v buf %q; want ignored, text kept", cmd != nil, m.editBuf.text)
		}
		path := filepath.Join(t.TempDir(), "desc.md")
		os.WriteFile(path, []byte("Body"), 0o600)
		_, cmd = editing(editNone, "").Update(descEditedMsg{id: "grl1", path: path})
		if _, err := os.Stat(path); err != nil || cmd == nil {
			t.Fatalf("description: temp file %v, cmd %v after Update; want kept and a Cmd", err, cmd != nil)
		}
		cmd()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("description (fail %v): temp file still there after the Cmd ran", fail)
		}
	}
}

// A bead with a write in flight refuses the e/E/d edit openers before $EDITOR
// opens, and a description edit that still lands on a pending bead keeps its temp
// file and names it rather than deleting what the human typed (Spec FR12).
func TestEditRefusedWhilePending(t *testing.T) {
	stubSeams(t)
	m := New(nil, nil)
	m.beads = []bd.Bead{{ID: "grl1", Kind: contract.KindGrilling, Status: "open", Title: "Old"}}
	m.rebuildGroups()
	m.selGraph = "grl1"
	m.pending = map[string]string{"grl1": "updating"}
	for _, k := range []string{"e", "E"} {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		if got := next.(Model); cmd != nil || got.editMode != editNone || got.status != "grl1: write in flight, wait" {
			t.Errorf("%s: cmd %v mode %d status %q; want refused", k, cmd != nil, got.editMode, got.status)
		}
	}
	pick := m
	pick.editMode, pick.editID = editPick, "grl1"
	next, cmd := pick.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if got := next.(Model); cmd != nil || got.editMode != editNone || got.status != "grl1: write in flight, wait" {
		t.Errorf("d: cmd %v mode %d status %q; want refused before $EDITOR", cmd != nil, got.editMode, got.status)
	}
	path := filepath.Join(t.TempDir(), "desc.md")
	os.WriteFile(path, []byte("Body"), 0o600)
	next, cmd = m.Update(descEditedMsg{id: "grl1", path: path})
	if _, err := os.Stat(path); err != nil || cmd != nil {
		t.Fatalf("pending description: temp file %v, cmd %v; want kept and no Cmd", err, cmd != nil)
	}
	if want := "grl1: write in flight, edit kept at " + path; next.(Model).status != want {
		t.Errorf("pending description status %q; want %q", next.(Model).status, want)
	}
}

// A create form stays open and ignores input while it files, refuses a second
// enter, keeps its title and description on failure and closes cleared on
// success (Spec FR14–15).
func TestCreateFormFiling(t *testing.T) {
	stubSeams(t)
	for _, fail := range []bool{true, false} {
		newSignal = func(string, string) (string, error) {
			if fail {
				return "", fmt.Errorf("dolt locked")
			}
			return "sig1", nil
		}
		m := New(nil, nil)
		m.tab, m.editMode, m.createTitle, m.createDesc = tabInbox, editCreate, lineAt("Fix"), lineAt("why")
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if m = next.(Model); m.status != "filing new Signal…" {
			t.Errorf("press: status %q; want filing new Signal…", m.status)
		}
		for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("x")}, {Type: tea.KeyTab}, {Type: tea.KeyBackspace}, {Type: tea.KeyEsc}} {
			next, _ = next.Update(k)
		}
		next, again := next.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if m := next.(Model); again != nil || m.editMode != editCreate || m.createTitle.text != "Fix" || m.createDesc.text != "why" || m.createField != 0 {
			t.Fatalf("filing: second enter cmd %v, mode %d fields %q/%q/%d; want refused, keys ignored", again != nil, m.editMode, m.createTitle.text, m.createDesc.text, m.createField)
		}
		next, _ = next.Update(cmd())
		m = next.(Model)
		if fail && (m.editMode != editCreate || m.createTitle.text != "Fix" || m.createDesc.text != "why" || m.status != "create: dolt locked") {
			t.Errorf("failed: mode %d fields %q/%q status %q; want kept and the error", m.editMode, m.createTitle.text, m.createDesc.text, m.status)
		}
		if !fail && (m.editMode != editNone || m.createTitle.text != "" || m.createDesc.text != "" || m.status != "filed Signal sig1") {
			t.Errorf("done: mode %d fields %q/%q status %q; want closed and cleared", m.editMode, m.createTitle.text, m.createDesc.text, m.status)
		}
		if fail { // the kept form is live again
			if next, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")}); next.(Model).createTitle.text != "Fix!" {
				t.Errorf("after failure: title %q; want the form live", next.(Model).createTitle.text)
			}
		}
	}
}

// t claims in its Cmd and tracks the effort only once claimed (Spec
// FR7–9, FR16): the press holds the bead pending and refuses a second t
// on it while another bead's t proceeds; the done message appends the effort and
// starts its dispatch on a good claim, and leaves m.runs alone on a failed one.
func TestRunNowClaimsOffTheLoop(t *testing.T) {
	calls := stubSeams(t)
	r1 := bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "chart1", Status: "open"}
	r2 := bd.Bead{ID: "r2", Kind: contract.KindResearch, Parent: "chart1", Status: "open"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{r1, r2}, nil }
	t1 := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}
	for _, claimErr := range []error{nil, fmt.Errorf("taken")} {
		claimBead = func(b bd.Bead) error { *calls = append(*calls, "claimBead "+b.ID); return claimErr }
		*calls = nil
		m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
		m.beads = []bd.Bead{r1, r2}
		m.rebuildGroups()
		m.selGraph = "r1"
		next, cmd := m.Update(t1)
		if m = next.(Model); cmd == nil || len(*calls) != 0 || m.status != "dispatching r1…" || len(m.runs) != 0 {
			t.Fatalf("press: cmd %v calls %q status %q runs %d; want a Cmd, no bd call, dispatching, nothing tracked", cmd != nil, *calls, m.status, len(m.runs))
		}
		if next, again := m.Update(t1); again != nil || next.(Model).status != "r1: write in flight, wait" {
			t.Errorf("second t: cmd %v status %q; want refused by the guard", again != nil, next.(Model).status)
		}
		other := m
		other.selGraph = "r2"
		if _, cmd2 := other.Update(t1); cmd2 == nil {
			t.Error("t on another bead must proceed while r1's claim is in flight")
		}
		msg := cmd()
		if !slices.Contains(*calls, "claimBead r1") {
			t.Fatalf("Cmd calls %q; want the claim", *calls)
		}
		next, follow := m.Update(msg)
		m = next.(Model)
		if claimErr == nil && (len(m.runs) != 1 || m.runs[0].Bead.ID != "r1" || m.runs[0].ID == "" || follow == nil) {
			t.Errorf("claimed: runs %+v follow %v; want r1 tracked with an effort id and its dispatch started", m.runs, follow != nil)
		}
		if claimErr != nil && (len(m.runs) != 0 || follow != nil || m.status != "run-now: claim r1: taken") {
			t.Errorf("claim failed: runs %+v follow %v status %q; want untracked and the claim error", m.runs, follow != nil, m.status)
		}
	}
}
