package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jira"
)

// pressN presses n then the New menu's row key.
func pressN(t *testing.T, m Model, row string) Model {
	t.Helper()
	return press(t, press(t, m, runeKey("n")), runeKey(row))
}

func newOpen(m Model) bool { return len(m.modals) > 0 && m.modals[len(m.modals)-1].newMenu }

func TestNewMenuOpensOnEveryTabAndFocus(t *testing.T) {
	base := graphModel(t, 120, 30)
	for i := range tabNames {
		m := press(t, base, runeKey(tabDefs[i].key))
		o := press(t, m, runeKey("n"))
		if !newOpen(o) {
			t.Errorf("n did not open the menu on %s", tabNames[i])
			continue
		}
		v := ansi.Strip(o.View())
		for _, want := range []string{"s   New Signal", "i   New Initiative", "d   New Discussion", "↑/↓ • enter • n/esc"} {
			if !strings.Contains(v, want) {
				t.Errorf("%s: menu lacks %q:\n%s", tabNames[i], want, v)
			}
		}
		if h := o.modals[0].bindings()[0].Help(); h.Key+" "+h.Desc != "↑/↓ move" {
			t.Errorf("move help = %q %q", h.Key, h.Desc)
		}
		if newOpen(press(t, o, runeKey("n"))) || newOpen(press(t, o, escKey())) {
			t.Errorf("n/esc left the menu open on %s", tabNames[i])
		}
	}
	f := ideaFocus(t, recentsModel(t, focusFixture()))
	if !newOpen(press(t, f, runeKey("n"))) {
		t.Error("n did not open the menu in the focus view")
	}
}

func TestNewMenuSwallowsKeys(t *testing.T) {
	m := press(t, graphModel(t, 120, 30), runeKey("n"))
	for _, k := range []string{"1", "g", "q", "?", "R", "t"} {
		if o := press(t, m, runeKey(k)); !newOpen(o) || o.tab != m.tab || o.ref {
			t.Errorf("%s acted under the New menu", k)
		}
	}
	if o := press(t, m, runeKey("j")); o.modals[0].cur != 1 {
		t.Error("j did not move")
	}
}

func TestNewMenuInertInInputsAndConfirms(t *testing.T) {
	m := graphModel(t, 120, 30)
	if o := press(t, press(t, m, runeKey("/")), runeKey("n")); newOpen(o) {
		t.Error("n opened the menu inside the / filter")
	}
	if o := press(t, m.push(confirmDialog("x", nil, "", nil)), runeKey("n")); newOpen(o) {
		t.Error("n opened the menu over a confirm")
	}
	if o := press(t, press(t, m, runeKey("?")), runeKey("n")); newOpen(o) || !o.ref {
		t.Error("n acted under ?")
	}
	if o := pressN(t, m, "s"); newOpen(press(t, o, runeKey("n"))) {
		t.Error("n opened the menu over a form")
	}
}

func TestNewMenuRowsOpenForms(t *testing.T) {
	m := graphModel(t, 120, 30)
	if o := pressN(t, m, "s"); len(o.modals) != 1 || o.modals[0].sf.noun != "Signal" {
		t.Errorf("n s: %v", o.modals)
	}
	if o := pressN(t, m, "i"); len(o.modals) != 1 || o.modals[0].sf.noun != "Initiative" {
		t.Errorf("n i: %v", o.modals)
	}
	// enter picks the highlighted row
	if o := press(t, press(t, m, runeKey("n")), enterKey()); len(o.modals) != 1 || o.modals[0].sf.noun != "Signal" {
		t.Errorf("n enter: %v", o.modals)
	}
	f := ideaFocus(t, recentsModel(t, focusFixture()))
	if o := pressN(t, f, "s"); len(o.modals) != 1 || o.modals[0].sf.id != "" {
		t.Errorf("n s from the focus view is scoped: %+v", o.modals)
	}
}

func TestNewMenuDiscussionRow(t *testing.T) {
	var titles []string
	old := newDiscussion
	newDiscussion = func(title, _ string) (string, error) { titles = append(titles, title); return "d1", nil }
	defer func() { newDiscussion = old }()
	_, cmd := send(t, press(t, graphModel(t, 120, 30), runeKey("n")), runeKey("d"))
	if cmd == nil {
		t.Fatal("n d started nothing")
	}
	if msg, ok := cmd().(discussionCreatedMsg); !ok || msg.err != nil || len(titles) != 1 || !strings.HasPrefix(titles[0], "Discussion ") {
		t.Errorf("n d: titles %v", titles)
	}
}

func TestNewHintAndReference(t *testing.T) {
	m := graphModel(t, 160, 30)
	bar := ansi.Strip(m.hintsView())
	if strings.Count(bar, "n new") != 1 || !strings.Contains(bar, "g goto • n new") {
		t.Errorf("bar = %q", bar)
	}
	ref := ansi.Strip(press(t, m, runeKey("?")).View())
	if !strings.Contains(ref, "n new ›") || !strings.Contains(ref, "New Initiative") {
		t.Errorf("? lacks the n new group:\n%s", ref)
	}
}

func TestNewMenuBindingCoverage(t *testing.T) {
	m := press(t, graphModel(t, 120, 30), runeKey("n"))
	checkCoverage(t, "New menu", m, false)
}

// stubJira sets the Jira seams for a test.
func stubJira(t *testing.T, set bool, f func(root, typed string) (jira.IntakeResult, error)) {
	t.Helper()
	os, oi := jiraSiteSet, intakeJira
	jiraSiteSet, intakeJira = func() bool { return set }, f
	t.Cleanup(func() { jiraSiteSet, intakeJira = os, oi })
}

func TestNewMenuJiraRowShownOnlyWithSite(t *testing.T) {
	stubJira(t, false, nil)
	m := press(t, graphModel(t, 120, 30), runeKey("n"))
	if strings.Contains(ansi.Strip(m.View()), "Jira") {
		t.Error("j row shown with no site")
	}
	checkCoverage(t, "New menu", m, false)
	if o := press(t, m, runeKey("j")); o.modals[0].kind != picker || o.modals[0].cur != 1 {
		t.Error("j without the row should move the cursor")
	}
	m = graphModel(t, 120, 30)
	m.jiraSet = true // as the slow load leaves it
	m = press(t, m, runeKey("n"))
	if !strings.Contains(ansi.Strip(m.View()), "j   New Signal from a Jira issue") {
		t.Error("j row hidden with a site")
	}
	checkCoverage(t, "New menu", m, false)
	if o := press(t, m, runeKey("j")); o.modals[0].kind != input || o.modals[0].cur != 0 {
		t.Errorf("j did not fire the row: %+v", o.modals[0])
	}
}

func TestNewMenuJiraFiles(t *testing.T) {
	var calls []string
	res := jira.IntakeResult{ID: "mc-1", Key: "ABC-1"}
	var err error
	stubJira(t, true, func(_, typed string) (jira.IntakeResult, error) { calls = append(calls, typed); return res, err })
	gm := graphModel(t, 120, 30)
	gm.jiraSet = true
	base := pressN(t, gm, "j")

	// empty input is refused without a call and keeps the form
	o := press(t, typeText(t, base, "  "), enterKey())
	if len(calls) != 0 || len(o.modals) != 1 || !strings.Contains(ansi.Strip(o.View()), "key required") {
		t.Fatalf("empty: calls %v modals %d", calls, len(o.modals))
	}

	run := func(m Model, typed string) (Model, string) {
		m, cmd := send(t, typeText(t, m, typed), enterKey())
		if cmd == nil {
			t.Fatal("no write started")
		}
		if m.status != "filing "+strings.ToUpper(typed)+"…" {
			t.Errorf("status = %q", m.status)
		}
		// a second press while pending is refused
		if two := pressN(t, m, "j"); !strings.Contains(two.status, "write in flight") || len(two.modals) != 0 {
			t.Errorf("second: %q, %d modals", two.status, len(two.modals))
		}
		msg := cmd()
		next, _ := m.Update(msg)
		if next.(Model).selection() == nil || base.selection() == nil {
			t.Fatal("no selection to compare")
		}
		if o := next.(Model); o.selection().ID != base.selection().ID {
			t.Errorf("cursor moved: %v -> %v", base.selection().ID, o.selection().ID)
		}
		return next.(Model), ""
	}

	m, _ := run(base, "abc-1")
	if calls[0] != "ABC-1" || m.status != "filed mc-1 from ABC-1" {
		t.Errorf("plain: %v %q", calls, m.status)
	}
	res = jira.IntakeResult{ID: "mc-2", Key: "NEW-9", Carrier: "mc-0"}
	if m, _ = run(base, "old-9"); m.status != "filed mc-2 from NEW-9 (OLD-9 is now NEW-9), superseded onto mc-0" {
		t.Errorf("rename+supersede: %q", m.status)
	}
	// pre-filing failure reopens the form holding the key
	res, err = jira.IntakeResult{}, errors.New("intake jira ABC-1: boom")
	if m, _ = run(base, "abc-1"); len(m.modals) != 1 || m.modals[0].inputs[0].text != "ABC-1" || !strings.Contains(ansi.Strip(m.View()), "intake: boom") || strings.Contains(ansi.Strip(m.View()), "intake jira") {
		t.Errorf("retry: %+v", m.modals)
	}
	// partial failure: status line, no reopen
	res, err = jira.IntakeResult{ID: "mc-3", Key: "ABC-1"}, errors.New("intake jira ABC-1: filed mc-3 but could not supersede it onto mc-0: x")
	if m, _ = run(base, "abc-1"); len(m.modals) != 0 || m.status != "filed mc-3 but could not supersede it onto mc-0: x" {
		t.Errorf("partial: %d %q", len(m.modals), m.status)
	}
}

func TestJiraSiteSetReadsTheWorkspaceConfig(t *testing.T) {
	root := t.TempDir()
	oRoot := git.RepoRoot
	git.RepoRoot = func() (string, error) { return root, nil }
	t.Cleanup(func() { git.RepoRoot = oRoot })
	if jiraSiteSet() {
		t.Error("set with no config")
	}
	if err := os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, ".mugicrew", "config.toml")
	if err := os.WriteFile(cfg, []byte("[jira]\nsite = \"acme.atlassian.net/browse\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !jiraSiteSet() { // a malformed site counts as set, so j reports the problem
		t.Error("malformed site not counted as set")
	}
}

func TestRefShowsJiraRowFromLoad(t *testing.T) {
	stubJira(t, true, nil)
	m := graphModel(t, 120, 30)
	m.jiraSet = true // as the slow load leaves it
	if o := press(t, m, runeKey("?")); !strings.Contains(ansi.Strip(o.View()), "New Signal from a Jira issue") {
		t.Error("? omits the j row before n has opened")
	}
}
