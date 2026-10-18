package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

func isContainer(b *bd.Bead) { b.Container = true }
func isEvent(b *bd.Bead)     { b.Event = true }

// execFixture covers the research row table: a folded Checkpoint under an Initiative
// with every member state, a surgery Checkpoint, Specs, Drafts and events, and the
// Tasks and Pivots that wait on, hold and gate Checkpoint cpA.
func execFixture() []bd.Bead {
	cp := contract.KindCheckpoint
	return []bd.Bead{
		dbead("i1", contract.KindInitiative, "open", "", 1), dbead("i2", contract.KindInitiative, "open", "", 2),
		dbead("cpA", cp, "open", "i1", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"tb"} }),
		dbead("ap", contract.KindApproval, "open", "cpA", 1),
		dbead("sg", contract.KindSurgery, "in_progress", "cpA", 2),
		dbead("bl", contract.KindBuild, "open", "cpA", 3),
		dbead("blk", contract.KindApproval, "open", "cpA", 4, blockedBy("bl")),
		dbead("pz", contract.KindApproval, "open", "cpA", 4, labeled("paused:manual")),
		dbead("cl", contract.KindBuild, "closed", "cpA", 3),
		dbead("dr", contract.KindBuild, "deferred", "cpA", 3),
		dbead("ev", contract.KindBuild, "open", "cpA", 3, isEvent),
		dbead("sp", contract.KindSpec, "open", "i1", 2),
		dbead("sp2", contract.KindSpec, "open", "i1", 5, blockedBy("sp")),
		// cs is the pivot-gated Checkpoint: members of cpA would read blocked under it (ADR 0270).
		dbead("cs", cp, "open", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"pg", "pg2"} }),
		dbead("cd", cp, "deferred", "i1", 1, isContainer),
		dbead("pg", contract.KindPivot, "open", "", 1),
		dbead("pg2", contract.KindPivot, "open", "i2", 2),
		dbead("pu", contract.KindPivot, "open", "", 1),
		dbead("tb", contract.KindTask, "open", "", 3),
		dbead("ta", contract.KindTask, "open", "", 3, blockedBy("cpA")),
	}
}

func execList(beads []bd.Bead, flip map[string]bool) []eRow {
	return oldExecList(graphData{beads: beads}, flip)
}

// eshape renders rows as "# title", ". " placeholder, "A id" (Actionable), "C id",
// "S id", "M id" (open member), "T id" (side), "+ n" ('▸ N closed') and "x id".
func eshape(rs []eRow) (out []string) {
	tag := map[int]string{eAct: "A ", eCP: "C ", eSpec: "S ", eMem: "M ", eSide: "T ", eClosed: "x ", eCPS: "K ", eSPS: "P ", eLanded: "L "}
	for _, r := range rs {
		switch r.kind {
		case eSec:
			out = append(out, "# "+r.title)
		case ePlace:
			out = append(out, ".")
		case eSum:
			out = append(out, "+ "+string(rune('0'+len(r.members))))
		default:
			out = append(out, tag[r.kind]+r.b.ID)
		}
	}
	return
}

func TestExecList(t *testing.T) {
	beads := execFixture()
	cases := []struct {
		name  string
		beads []bd.Bead
		flip  map[string]bool
		want  string
	}{
		{"empty snapshot: every section with its placeholder", nil, nil, "# Actionable (0),.,# Live (0),.,# Specs (0),.,# Checkpoints (0),.,# Initiatives (0),.,# No Initiative (0),."},
		{"Actionable, Initiative groups, Checkpoints before Specs before Tasks and Pivots, No Initiative last, Checkpoints folded",
			beads, nil,
			"# Actionable (6),A ap,A pg,A sg,A pg2,A tb,A pz,# Live (0),.,# Specs (2),P sp,P sp2,# Checkpoints (2),K cpA,K cs,# Initiatives (2)," +
				"# t i1,C cpA,S sp,S sp2,T tb,T ta," +
				"# t i2,T pg2," +
				"# No Initiative (2),C cs,T pg"},
		{"unfolded Checkpoint: open members, then '▸ N closed' folded",
			beads, map[string]bool{"cpA": true},
			"# Actionable (6),A ap,A pg,A sg,A pg2,A tb,A pz,# Live (0),.,# Specs (2),P sp,P sp2,# Checkpoints (2),K cpA,K cs,# Initiatives (2)," +
				"# t i1,C cpA,M ap,M sg,M bl,M blk,M pz,+ 1,S sp,S sp2,T tb,T ta," +
				"# t i2,T pg2," +
				"# No Initiative (2),C cs,T pg"},
		{"unfolded '▸ N closed' lists the closed members",
			beads, map[string]bool{"cpA": true, "cpA/closed": true},
			"# Actionable (6),A ap,A pg,A sg,A pg2,A tb,A pz,# Live (0),.,# Specs (2),P sp,P sp2,# Checkpoints (2),K cpA,K cs,# Initiatives (2)," +
				"# t i1,C cpA,M ap,M sg,M bl,M blk,M pz,+ 1,x cl,S sp,S sp2,T tb,T ta," +
				"# t i2,T pg2," +
				"# No Initiative (2),C cs,T pg"},
		{"folded Actionable and Initiative hide their rows",
			beads, map[string]bool{actKey: true, "i:i1": true, "i:": true},
			"# Actionable (6),# Live (0),.,# Specs (2),P sp,P sp2,# Checkpoints (2),K cpA,K cs,# Initiatives (2),# t i1,# t i2,T pg2,# No Initiative (2)"},
		{"folded Initiatives hides every container and keeps each one's fold",
			beads, map[string]bool{treeKey: true, "cpA": true},
			"# Actionable (6),A ap,A pg,A sg,A pg2,A tb,A pz,# Live (0),.,# Specs (2),P sp,P sp2,# Checkpoints (2),K cpA,K cs,# Initiatives (2),# No Initiative (2),C cs,T pg"},
		{"a Checkpoint with nothing closed has no '▸ N closed'",
			[]bd.Bead{dbead("c", contract.KindCheckpoint, "open", "", 1, isContainer), dbead("m", contract.KindBuild, "open", "c", 1)},
			map[string]bool{"c": true},
			"# Actionable (0),.,# Live (0),.,# Specs (0),.,# Checkpoints (1),K c,# Initiatives (0),.,# No Initiative (1),C c,M m"},
		{"a Task or Pivot with no Initiative falls back to its Checkpoint's",
			[]bd.Bead{
				dbead("i", contract.KindInitiative, "open", "", 1),
				dbead("c", contract.KindCheckpoint, "open", "i", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"p"} }),
				dbead("p", contract.KindPivot, "open", "", 1),
			}, nil,
			"# Actionable (1),A p,# Live (0),.,# Specs (0),.,# Checkpoints (1),K c,# Initiatives (1),# t i,C c,T p,# No Initiative (0),."},
		{"every live Task is listed: Initiative child, freestanding, blocked, after-land; Drafts, closed and ungated Pivots are not",
			[]bd.Bead{
				dbead("i", contract.KindInitiative, "open", "", 1),
				dbead("ti", contract.KindTask, "open", "i", 1),
				dbead("tf", contract.KindTask, "open", "", 2),
				dbead("tk", contract.KindTask, "open", "", 3, blockedBy("tf")),
				dbead("c", contract.KindCheckpoint, "closed", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"tl"} }),
				dbead("tl", contract.KindTask, "open", "", 4),
				dbead("td", contract.KindTask, "deferred", "i", 5),
				dbead("tc", contract.KindTask, "closed", "i", 6),
				dbead("p", contract.KindPivot, "open", "", 1),
			}, nil,
			"# Actionable (3),A ti,A tf,A tl,# Live (0),.,# Specs (0),.,# Checkpoints (0),.,# Initiatives (1),# t i,T ti,# No Initiative (3),T tf,T tk,T tl"},
		{"a Task or Pivot waiting on no listed Checkpoint is not shown",
			[]bd.Bead{
				dbead("c", contract.KindCheckpoint, "closed", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"p"} }),
				dbead("p", contract.KindPivot, "open", "", 1),
			}, nil, "# Actionable (0),.,# Live (0),.,# Specs (0),.,# Checkpoints (0),.,# Initiatives (0),.,# No Initiative (0),."},
	}
	for _, c := range cases {
		if got := strings.Join(eshape(execList(c.beads, c.flip)), ","); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

// Actionable follows ADR 0170 §3: blocked, paused and done beads and after-land
// Tasks fall out; idle and live beads stay; a Pivot that gates nothing is absent.
func TestExecActionable(t *testing.T) {
	beads := append(execFixture(),
		dbead("lv", contract.KindApproval, "open", "cpA", 5))
	g := graphData{beads: beads, sessions: execSessions()}
	var ids []string
	for _, r := range g.actionable() {
		ids = append(ids, r.b.ID)
	}
	// ungated pu, after-land ta, blocked blk, and live lv are out; idle sg stays and own-paused pz closes the list
	if got, want := strings.Join(ids, ","), "ap,pg,sg,pg2,tb,pz"; got != want {
		t.Errorf("actionable = %s, want %s", got, want)
	}
	if g.execBadge() != 0 { // Actionable items alone don't count
		t.Errorf("badge = %d, want 0", g.execBadge())
	}
	g.sessions = map[string]session.Info{"lv": {State: session.Alive, Waiting: session.WaitTurn}}
	if g.execBadge() != 1 {
		t.Errorf("badge = %d, want 1", g.execBadge())
	}
	if r := g.actionable(); r[0].b.ID != "lv" || len(r) != 7 { // waiting band first
		t.Errorf("waiting lv not first of 7: %v", len(r))
	}
}

func execSessions() map[string]session.Info {
	return map[string]session.Info{"lv": {State: session.Alive}}
}

func execModel(t *testing.T, beads []bd.Bead) (Model, *[]string) {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	return execModelHere(t, beads)
}

// execModelHere is execModel with the commonDir seam left as the caller set it.
func execModelHere(t *testing.T, beads []bd.Bead) (Model, *[]string) {
	t.Helper()
	m, calls := execModelFresh(t, beads)
	return press(t, treesOpen(m), runeKey("3")), calls
}

// execModelFresh is execModelHere with the default folds, on the Ideate tab.
func execModelFresh(t *testing.T, beads []bd.Bead) (Model, *[]string) {
	t.Helper()
	calls := stubActions(t)
	m := sized(t, 160, 40)
	m.crew = map[string]contract.Crewmate{"luffy": fakeCrew{}, "nami": fakeCrew{}, "franky": fakeCrew{}}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: execSessions()})
	return m, calls
}

func execSel(m Model) string { return m.tabAt(tabExecute).(execTab).sel }

// execAt moves the cursor to the row with key k, unfolding cpA first so its members are there.
func execAt(t *testing.T, m Model, k string) Model {
	t.Helper()
	if execSel(m) == "" { // the unset selection is the first row
		m = press(t, press(t, m, runeKey("j")), runeKey("k"))
	}
	for range 40 {
		if execSel(m) == k {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no Execute row %s; at %q", k, execSel(m))
	return m
}

// unfolded is the model with cpA and its '▸ N closed' unfolded.
func unfolded(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, execAt(t, m, "cpA"), runeKey(" "))
	return press(t, execAt(t, m, "cpA/closed"), runeKey(" "))
}

func TestExecBadgeAndTabLabel(t *testing.T) {
	beads := append(execFixture(), dbead("lv", contract.KindApproval, "open", "cpA", 5))
	m, _ := execModel(t, beads)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{"lv": {State: session.Alive, Waiting: session.WaitAsk}}})
	if n := m.tabAt(tabExecute).count(); n != 1 {
		t.Errorf("badge = %d, want 1", n)
	}
	if lbl := ansi.Strip(tabLabel(tabExecute, m.tabAt(tabExecute))); lbl != "3 Execute ●1" {
		t.Errorf("label = %q", lbl)
	}
	if bar := lines(m.View())[2]; !strings.Contains(bar, "3 Execute ●1") {
		t.Errorf("tab bar = %q", bar)
	}
	e, _ := execModel(t, nil)
	if n := e.tabAt(tabExecute).count(); n != 0 {
		t.Errorf("empty badge = %d", n)
	}
}

// With live rows and nothing waiting or Actionable the label is a green ●N equal to the Live header's N.
func TestExecLiveBadge(t *testing.T) {
	beads := []bd.Bead{
		dbead("cpA", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("sg", contract.KindSurgery, "in_progress", "cpA", 2),
	}
	m, _ := execModel(t, beads)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{"sg": {State: session.Alive}}})
	hdr := regexp.MustCompile(`Live \((\d+)\)`).FindStringSubmatch(ansi.Strip(m.View()))
	if hdr == nil || hdr[1] == "0" {
		t.Fatalf("no live rows in view:\n%s", ansi.Strip(m.View()))
	}
	want := "3 Execute ●" + hdr[1]
	if lbl := ansi.Strip(tabLabel(tabExecute, m.tabAt(tabExecute))); lbl != want {
		t.Errorf("label = %q, want %q", lbl, want)
	}
	f := press(t, execAt(t, m, liveKey), runeKey(" ")) // folding Live leaves the badge alone
	if lbl := ansi.Strip(tabLabel(tabExecute, f.tabAt(tabExecute))); lbl != want {
		t.Errorf("folded label = %q, want %q", lbl, want)
	}
}

// Rows carry marks and, after title and id, the relation hint.
func TestExecRowHints(t *testing.T) {
	m, _ := execModel(t, execFixture())
	v := ansi.Strip(m.View())
	for _, want := range []string{
		"Actionable (6)", "t ap · ap", "in → t cpA", "gates → t cs", "holds land of → t cpA", "waits on land of → t cpA",
		"No Initiative", "▸", "t cs",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	e, _ := execModel(t, nil)
	if v := ansi.Strip(e.View()); !strings.Contains(v, "nothing waits on you") {
		t.Errorf("empty view lacks the placeholder:\n%s", v)
	}
}

func TestExecSelectionFollowsKeyAcrossRefresh(t *testing.T) {
	m, _ := execModel(t, execFixture())
	m = execAt(t, m, "sp")
	m, _ = send(t, m, loadedMsg{beads: append([]bd.Bead{dbead("zz", contract.KindSpec, "open", "", 0)}, execFixture()...), live: map[string]bool{}, sessions: execSessions()})
	if got := execSel(m); got != "sp" {
		t.Errorf("selection = %q, want sp", got)
	}
	if got := m.selection(); got == nil || got.ID != "sp" {
		t.Errorf("selected bead = %+v", got)
	}
}

func TestExecEnter(t *testing.T) {
	m, calls := execModel(t, execFixture())
	m = unfolded(t, m)
	for _, c := range []struct{ key, say string }{
		{"blk", "blk is blocked by t bl (bl) — not launching"},
		{"pz", "pz is paused (paused:manual) — not launching"},
		{"i:i1", "nothing to launch or run here — space folds"},
		{actKey, "nothing to launch or run here — space folds"},
		{"cpA/closed", "nothing to launch or run here — space folds"},
		{"cpA", "cpA:"},
	} {
		n, cmd := send(t, execAt(t, m, c.key), enterKey())
		if cmd != nil || len(n.modals) != 0 || !strings.Contains(statusOf(n), c.say) {
			t.Errorf("%s: cmd=%v modals=%d status=%q, want %q", c.key, cmd != nil, len(n.modals), statusOf(n), c.say)
		}
	}
	for _, k := range []string{"act:ap", "act:pg", "act:sg", "act:tb", "side:tb", "ap", "sp"} { // launch, resume an idle claim, run
		if n, cmd := send(t, execAt(t, m, k), enterKey()); cmd == nil || len(n.modals) != 0 {
			t.Errorf("%s: cmd=%v modals=%d status=%q, want a started action", k, cmd != nil, len(n.modals), statusOf(n))
		}
	}
	n, cmd := send(t, execAt(t, m, "sp2"), enterKey())
	if cmd != nil || len(n.modals) != 1 || n.modals[0].title != "Run blocked bead now?" {
		t.Errorf("blocked AFK: cmd=%v modals=%+v", cmd != nil, n.modals)
	}
	if len(*calls) != 0 && strings.Contains(strings.Join(*calls, ";"), "switchSession") {
		t.Errorf("unexpected attach: %v", *calls)
	}
}

func TestExecBg(t *testing.T) {
	m, _ := execModel(t, append(execFixture(), dbead("lv", contract.KindApproval, "open", "cpA", 5)))
	m = unfolded(t, m)
	for _, c := range []struct{ key, say string }{
		{"sp", "b is HITL-only — enter runs it"},
		{"i:i1", "nothing to launch or run here — space folds"},
		{"blk", "blk is blocked by t bl (bl) — not launching"},
		{"lv", "already running — enter attaches"},
	} {
		n, cmd := send(t, execAt(t, m, c.key), runeKey("b"))
		if cmd != nil || len(n.modals) != 0 || !strings.Contains(statusOf(n), c.say) {
			t.Errorf("%s: cmd=%v modals=%d status=%q, want %q", c.key, cmd != nil, len(n.modals), statusOf(n), c.say)
		}
	}
	if n, cmd := send(t, execAt(t, m, "act:ap"), runeKey("b")); cmd == nil {
		t.Errorf("b on a ready HITL row: status=%q", statusOf(n))
	}
}

func TestExecHints(t *testing.T) {
	beads := append(execFixture(), dbead("lv", contract.KindApproval, "open", "cpA", 5))
	m, _ := execModel(t, beads)
	m = unfolded(t, m)
	for _, c := range []struct{ key, want string }{
		{"act:ap", "enter launch · b bg · d detail · p pause · t transcript · +/- prio · J/K section"},
		{"lv", "enter attach · d detail · p pause · x drop · t transcript · +/- prio · J/K section"},
		{"blk", "d detail · p pause · t transcript · +/- prio · J/K section"},
		{"sp", "enter run · d detail · p pause · t transcript · +/- prio · J/K section · r retrospective"},
		{"cpA", "space fold · d detail · p pause · t transcript · +/- prio · J/K section · r retrospective"},
		{actKey, "space fold · d detail · J/K section"},
		{"i:i1", "space fold · d detail · J/K section"},
		{"cpA/closed", "space fold · d detail · J/K section"},
		{"cl", "d detail · t transcript · J/K section"},
	} {
		n := execAt(t, m, c.key)
		if got := hintsOf(n); got != c.want {
			t.Errorf("%s: hints %q, want %q", c.key, got, c.want)
		}
		l := lines(n.View())
		if bar := strings.TrimRight(l[len(l)-1], " "); !strings.HasSuffix(bar, "? keys") {
			t.Errorf("%s: bar %q does not end with ? keys", c.key, bar)
		}
	}
}

func TestExecBindingCoverage(t *testing.T) {
	m, _ := execModel(t, execFixture())
	m = unfolded(t, m)
	for _, k := range []string{actKey, "act:ap", "i:i1", "cpA", "ap", "blk", "sp", "sp2", "cpA/closed", "cl", "side:tb"} {
		checkCoverage(t, "Execute "+k, execAt(t, m, k), true, "3", "d", "J", "K", "ctrl+down", "ctrl+up") // d detail's pane is a later Build
	}
}

func TestExecSharedKeys(t *testing.T) {
	m, _ := execModel(t, execFixture())
	if n, cmd := send(t, execAt(t, m, "i:i1"), runeKey("x")); cmd != nil || len(n.modals) != 0 {
		t.Errorf("x on a header: cmd=%v modals=%d", cmd != nil, len(n.modals))
	}
}

// Execute marks the selection's visible open blockers; hidden ones go to the footer.
func TestExecBlockerHighlightAndFooter(t *testing.T) {
	forceANSI256(t)
	m, _ := execModel(t, execFixture())
	m = execAt(t, unfolded(t, m), "blk") // blocked by bl, a visible member
	v := m.View()
	n := 0
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, blockerMk.Render("!")) {
			n++
			if !strings.Contains(ansi.Strip(l), " bl ") {
				t.Errorf("gutter on wrong row: %q", l)
			}
		}
	}
	if n != 1 || len(footerRows(v)) > 0 {
		t.Errorf("want one ! row and no footer, got %d:\n%s", n, ansi.Strip(v))
	}
	if !strings.Contains(v, tinted(pinkFg, 1).Render(prioText(3))+sep()+pinkFg.Render("t bl")) {
		t.Errorf("blocker title not pink:\n%s", v)
	}
	// fold cpA: bl leaves the list and lands in the footer
	m = execAt(t, press(t, execAt(t, m, "cpA"), runeKey(" ")), "side:ta") // ta waits on cpA, drawn
	if !strings.Contains(m.View(), blockerMk.Render("!")) {
		t.Errorf("visible cpA not marked")
	}
	m = execAt(t, m, "sp2") // blocked by sp, drawn
	if len(footerRows(m.View())) > 0 {
		t.Errorf("drawn blocker in footer")
	}
	m = execAt(t, m, "side:pg") // unblocked
	if len(footerRows(m.View())) > 0 || strings.Contains(m.View(), blockerMk.Render("!")) {
		t.Errorf("unblocked selection got a mark")
	}
}

// A blocker with no Execute row lands in the footer, directly above the drawer rule.
func TestExecBlockerFooterAboveRule(t *testing.T) {
	forceANSI256(t)
	beads := append(execFixture(), dbead("ghost", contract.KindBuild, "open", "other", 1))
	beads[3].BlockedBy = []string{"ghost"} // ap
	m, _ := execModel(t, beads)
	m = press(t, execAt(t, unfolded(t, m), "ap"), runeKey("d"))
	got := lines(ansi.Strip(m.View()))
	for i := len(got) - 1; i > 0; i-- {
		if strings.HasPrefix(got[i], "────") {
			if !strings.HasPrefix(got[i-1], "!") || !strings.Contains(got[i-1], "· ghost") {
				t.Errorf("row above rule = %q, want footer", got[i-1])
			}
			return
		}
	}
	t.Errorf("no rule:\n%s", strings.Join(got, "\n"))
}

func TestExecGapUnderActionable(t *testing.T) {
	m, _ := execModel(t, execFixture())
	ls := strings.Split(ansi.Strip(m.View()), "\n")
	var next string // the first section after Actionable
	for _, r := range execList(execFixture(), nil)[1:] {
		if r.kind == eSec {
			next = r.title
			break
		}
	}
	for i, l := range ls {
		if strings.Contains(l, next) {
			if i == 0 || strings.TrimSpace(ls[i-1]) != "" || strings.TrimSpace(ls[i-2]) == "" {
				t.Errorf("want one blank line above %q:\n%s", next, strings.Join(ls[:i+1], "\n"))
			}
			return
		}
	}
	t.Fatalf("no %q line", next)
}

// initiativeOf is the bead's own Initiative, else its Checkpoint's: a Task adopted
// into X that holds a Checkpoint in Y resolves to X.
func TestInitiativeOf(t *testing.T) {
	g := graphData{beads: []bd.Bead{
		dbead("x", contract.KindInitiative, "open", "", 1),
		dbead("y", contract.KindInitiative, "open", "", 1),
		dbead("cp", contract.KindCheckpoint, "open", "y", 1, isContainer),
		dbead("own", contract.KindTask, "open", "x", 1),
		dbead("none", contract.KindTask, "open", "", 1),
		dbead("nocp", contract.KindCheckpoint, "open", "", 1, isContainer),
	}}
	for _, c := range []struct{ b, cp, want string }{
		{"own", "cp", "x"},
		{"none", "cp", "y"},
		{"none", "nocp", ""},
		{"cp", "", "y"},
	} {
		var cp *bd.Bead
		if c.cp != "" {
			cp = g.beadByID(c.cp)
		}
		if got := g.initiativeOf(g.beadByID(c.b), cp); got != c.want {
			t.Errorf("initiativeOf(%s, %s) = %q, want %q", c.b, c.cp, got, c.want)
		}
	}
}

// Each leafState lands in its band: waiting first, then ready; launching, live, exited,
// and blocked are out; an own-paused bead closes the list (Spec mc-lu2x.38 D1, mc-qgtw).
func TestExecActionableBands(t *testing.T) {
	cp := dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer)
	var beads []bd.Bead
	beads = append(beads, cp)
	for _, id := range []string{"ready", "turn", "ask", "launch", "live", "exited", "paused"} {
		beads = append(beads, dbead(id, contract.KindApproval, "open", "cp", 1))
	}
	beads = append(beads, dbead("blk", contract.KindApproval, "open", "cp", 1, blockedBy("ready")))
	beads[len(beads)-2].Labels = append(beads[len(beads)-2].Labels, "paused:manual")
	g := graphData{beads: beads, launching: map[string]bool{"launch": true}, sessions: map[string]session.Info{
		"turn":   {State: session.Alive, Waiting: session.WaitTurn},
		"ask":    {State: session.Alive, Waiting: session.WaitAsk},
		"live":   {State: session.Alive},
		"exited": {State: session.Exited},
	}}
	var ids []string
	for _, r := range g.actionable() {
		ids = append(ids, r.b.ID)
	}
	if got := strings.Join(ids, ","); got != "ask,turn,ready,paused" && got != "turn,ask,ready,paused" {
		t.Errorf("actionable = %s", got)
	}
	if g.execBadge() != 2 {
		t.Errorf("badge = %d, want 2", g.execBadge())
	}
}

// Every open unblocked Task is Actionable wherever it sits (Spec mc-b5ce); only a Task
// blocking a listed Checkpoint carries the "holds land of" hint.
func TestExecActionableListsEveryTask(t *testing.T) {
	tk := contract.KindTask
	beads := []bd.Bead{
		dbead("cpOpen", contract.KindCheckpoint, "open", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"tb", "tb2"} }),
		dbead("cpOpen2", contract.KindCheckpoint, "open", "", 2, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"tb"} }),
		dbead("cpGone", contract.KindCheckpoint, "closed", "", 1, isContainer),
		dbead("i1", contract.KindInitiative, "open", "", 1),
		dbead("ip", contract.KindInitiative, "open", "", 2, labeled("paused:manual")),
		dbead("tb", tk, "open", "", 3),
		dbead("tb2", tk, "open", "", 3),
		dbead("after", tk, "open", "", 3, blockedBy("cpGone")),
		dbead("afterOpen", tk, "open", "", 3, blockedBy("cpOpen")),
		dbead("free", tk, "open", "", 3),
		dbead("kid", tk, "open", "i1", 3),
		dbead("blk", tk, "open", "", 3, blockedBy("free")),
		dbead("dr", tk, "deferred", "", 3),
		dbead("own", tk, "open", "", 3, labeled("paused:manual")),
		dbead("anc", tk, "open", "ip", 3),
		dbead("live", tk, "open", "", 3),
	}
	g := graphData{beads: beads, sessions: map[string]session.Info{"live": {State: session.Alive}}}
	var got []string
	hint := map[string]string{}
	for _, r := range g.actionable() {
		got = append(got, r.b.ID)
		if r.cp != nil {
			hint[r.b.ID] = r.rel + " " + r.cp.ID
		}
	}
	slices.Sort(got)
	if want := "after,free,kid,own,tb,tb2"; strings.Join(got, ",") != want {
		t.Errorf("actionable = %v, want %s", got, want)
	}
	if hint["tb"] != "before land cpOpen" || hint["tb2"] != "before land cpOpen" || hint["free"] != "" || hint["after"] != "" {
		t.Errorf("hints = %v", hint)
	}
	var live []string
	for _, r := range g.liveRows(g.actionable()) {
		live = append(live, r.b.ID)
	}
	if strings.Join(live, ",") != "live" {
		t.Errorf("live = %v, want [live]", live)
	}
}
