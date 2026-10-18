package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

func closedWith(reason string) func(*bd.Bead) { return func(b *bd.Bead) { b.CloseReason = reason } }
func madeAt(t time.Time) func(*bd.Bead)       { return func(b *bd.Bead) { b.Created = t } }
func closedOn(t time.Time) func(*bd.Bead)     { return func(b *bd.Bead) { b.Closed = t } }
func pinned(sha string) func(*bd.Bead)        { return func(b *bd.Bead) { b.Description = "Commit: " + sha } }

// rollupOf is cp's Rollup word over its kids (cp is the Checkpoint "cp"), with tips.
func rollupOf(tips map[string]string, kids ...bd.Bead) rollupResult {
	beads := append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer)}, kids...)
	g := graphData{beads: beads, cpTips: tips}
	return g.rollup(g.beadByID("cp"))
}

func TestRollupWords(t *testing.T) {
	const tip = "aaaa1111"
	at := map[string]string{"cp": tip}
	k := func(id string, kind contract.Kind, status string, mut ...func(*bd.Bead)) bd.Bead {
		return dbead(id, kind, status, "cp", 1, mut...)
	}
	sig := func(id string) bd.Bead { return dbead(id, contract.KindSignal, "open", "", 1) }
	cases := []struct {
		name  string
		tips  map[string]string
		kids  []bd.Bead
		extra []bd.Bead // non-children
		block []string
		word  string
		rank  int
		also  string
	}{
		{"landed while open", at, []bd.Bead{k("l", contract.KindLanding, "closed", closedWith("landed: abc123"))}, nil, nil, "landed", 1, ""},
		{"land failed", at, []bd.Bead{k("l", contract.KindLanding, "open", labeled("paused:failing"))}, nil, nil, "land failed", 2, "landing"},
		{"land paused", at, []bd.Bead{k("l", contract.KindLanding, "open", labeled("paused:manual"))}, nil, nil, "land paused", 2, "landing"},
		{"landing", at, []bd.Bead{k("l", contract.KindLanding, "in_progress")}, nil, nil, "landing", 3, ""},
		{"conflict", at, []bd.Bead{k("c", contract.KindConflict, "open")}, nil, nil, "conflict", 4, ""},
		{"blocked on Signal", at, []bd.Bead{k("m", contract.KindBuild, "closed")}, []bd.Bead{sig("s1")}, []string{"s1"}, "blocked on Signal", 5, "review due"},
		{"blocked on Signal +1", at, nil, []bd.Bead{sig("s1"), sig("s2")}, []string{"s1", "s2"}, "blocked on Signal +1", 5, ""},
		{"fixing critique", at, []bd.Bead{k("c", contract.KindCritique, "open")}, nil, nil, "fixing", 6, ""},
		{"fixing surgery", at, []bd.Bead{k("c", contract.KindSurgery, "open")}, nil, nil, "fixing", 6, ""},
		{"awaiting approval", at, []bd.Bead{k("a", contract.KindApproval, "open", pinned(tip))}, nil, nil, "awaiting approval", 7, ""},
		{"in review", at, []bd.Bead{k("r", contract.KindReview, "open")}, nil, nil, "in review", 8, ""},
		{"in build", at, []bd.Bead{k("b", contract.KindBuild, "open")}, nil, nil, "in build", 9, ""},
		{"approved", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip))}, nil, nil, "approved", 10, ""},
		{"review due", at, []bd.Bead{k("m", contract.KindBuild, "closed")}, nil, nil, "review due", 11, ""},
		{"cut", at, nil, nil, nil, "cut", 12, ""},
		{"cut with only a task", at, []bd.Bead{k("t", contract.KindTask, "closed")}, nil, nil, "cut", 12, ""},
		// precedence pairs from the prototype replay
		{"fixing beats in review", at, []bd.Bead{k("c", contract.KindCritique, "open"), k("r", contract.KindReview, "open")}, nil, nil, "fixing", 6, "in review"},
		{"landing beats approved", at, []bd.Bead{k("l", contract.KindLanding, "open"), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip))}, nil, nil, "landing", 3, ""},
		{"conflict beats a stale Approval", at, []bd.Bead{k("c", contract.KindConflict, "open"), k("a", contract.KindApproval, "open", pinned("old"))}, nil, nil, "conflict", 4, "awaiting approval"},
		{"blocked plus conflict gives also", at, []bd.Bead{k("c", contract.KindConflict, "open")}, []bd.Bead{sig("s1")}, []string{"s1"}, "conflict", 4, "blocked on Signal"},
		// the tip map
		{"approved needs the pin at the tip", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned("old"))}, nil, nil, "review due", 11, ""},
		{"empty tips give no approved", nil, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip))}, nil, nil, "review due", 11, ""},
		{"at-tip open Approval vetoes review due", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "open", pinned(tip))}, nil, nil, "awaiting approval", 7, ""},
		{"at-tip rework Approval is not approved and gets a fresh review", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("rework: mc-1"), pinned(tip))}, nil, nil, "review due", 11, ""},
		{"at-tip Approval obsoleted is not approved", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("Approval obsoleted: tip moved"), pinned(tip))}, nil, nil, "review due", 11, ""},
		{"at-tip empty-reason Approval is not approved", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", pinned(tip))}, nil, nil, "review due", 11, ""},
		{"an earlier Landing does not block approved", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip), closedOn(epoch.Add(2*time.Hour))), k("l", contract.KindLanding, "closed", closedWith("obsolete: conflict"), madeAt(epoch.Add(time.Hour)), closedOn(epoch.Add(time.Hour)))}, nil, nil, "approved", 10, ""},
		{"a Landing spawned after the Approval blocks approved", at, []bd.Bead{k("m", contract.KindBuild, "closed"), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip)), k("l", contract.KindLanding, "closed", closedWith("obsolete: conflict"))}, nil, nil, "idle ⚠", 13, ""},
		{"open Pivot blocker denies review due", at, []bd.Bead{k("m", contract.KindBuild, "closed")}, []bd.Bead{dbead("pv", contract.KindPivot, "open", "", 1)}, []string{"pv"}, "blocked on Pivot", 5, ""},
		// a blocker absent from the snapshot counts as closed; one without a kind label must not panic
		{"blocker absent from the snapshot", at, nil, nil, []string{"ext-9"}, "cut", 12, ""},
		{"blocker with no kind label", at, nil, []bd.Bead{dbead("nk", "", "open", "", 1)}, []string{"nk"}, "blocked on Bead", 5, ""},
		// a Chart's Landing never counts
		{"Chart Landing is not a child", at, nil, []bd.Bead{dbead("chart", contract.KindChart, "open", "", 1), dbead("cl", contract.KindLanding, "open", "chart", 1)}, nil, "cut", 12, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beads := append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = c.block })}, c.kids...)
			beads = append(beads, c.extra...)
			g := graphData{beads: beads, cpTips: c.tips}
			r := g.rollup(g.beadByID("cp"))
			if r.Word != c.word || r.Rank != c.rank || strings.Join(r.Also, ",") != c.also {
				t.Errorf("rollup = %q rank %d also %v; want %q rank %d also %q", r.Word, r.Rank, r.Also, c.word, c.rank, c.also)
			}
		})
	}
}

func TestRollupIdle(t *testing.T) {
	// members exist but no rule matched: a rework-closed at-tip Approval is not approved
	// and no longer vetoes review due.
	r := rollupOf(map[string]string{"cp": "x"},
		dbead("m", contract.KindBuild, "closed", "cp", 1),
		dbead("a", contract.KindApproval, "closed", "cp", 1, closedWith("rework: mc-1"), pinned("x")))
	if r.Word != "review due" {
		t.Errorf("word = %q, want review due", r.Word)
	}
}

// The tree row and its Live twin carry no Rollup bracket; the Checkpoints section row still shows the word.
func TestCheckpointTreeRowHasNoRollupBracket(t *testing.T) {
	beads := []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("c", contract.KindConflict, "open", "cp", 1),
	}
	tab := execTab{g: graphData{beads: beads}}
	tab.flip = tab.expanded()
	seen := map[int]bool{}
	for _, r := range tab.rows() {
		if r.b == nil || r.b.ID != "cp" {
			continue
		}
		seen[r.kind] = true
		l, _ := tab.line(r, 10, nil, "")
		l = ansi.Strip(l)
		switch r.kind {
		case eCP:
			twin := r
			twin.kind = eLive
			tl, _ := tab.line(twin, 10, nil, "")
			if strings.Contains(l, "[conflict]") || strings.Contains(ansi.Strip(tl), "[conflict]") {
				t.Errorf("tree row %q / live twin %q, want no [conflict] bracket", l, ansi.Strip(tl))
			}
		case eCPS:
			if !strings.Contains(l, "conflict") {
				t.Errorf("section row = %q, want conflict", l)
			}
		}
	}
	if !seen[eCP] || !seen[eCPS] {
		t.Fatalf("rows seen = %v, want a tree row and a section row", seen)
	}
}

func TestRollupQualifiers(t *testing.T) {
	const tip = "aaaa1111bbbb"
	at := map[string]string{"cp": tip}
	k := func(id string, kind contract.Kind, status string, mut ...func(*bd.Bead)) bd.Bead {
		return dbead(id, kind, status, "cp", 1, mut...)
	}
	rev := func(id string) bd.Bead { return k(id, contract.KindReview, "closed") }
	cases := []struct {
		name string
		tips map[string]string
		kids []bd.Bead
		want string // qualifiers joined with ·
	}{
		{"round hidden at 1", at, []bd.Bead{rev("r1"), k("b", contract.KindBuild, "open")}, ""},
		{"round shown at 2", at, []bd.Bead{rev("r1"), rev("r2"), k("b", contract.KindBuild, "open")}, "round 2"},
		{"round absent on landed", at, []bd.Bead{rev("r1"), rev("r2"), k("l", contract.KindLanding, "closed", closedWith("landed: abc"))}, ""},
		{"re-review from obsolete Landing", at, []bd.Bead{k("l", contract.KindLanding, "closed", closedWith("obsolete: conflict")), k("b", contract.KindBuild, "open")}, "re-review"},
		{"re-review from non-approved Approval", at, []bd.Bead{k("a", contract.KindApproval, "closed", closedWith("rejected: no"), pinned("old")), k("b", contract.KindBuild, "open")}, "re-review"},
		{"an earlier obsolete Landing still gives re-review under approved", at, []bd.Bead{k("l", contract.KindLanding, "closed", closedWith("obsolete: conflict"), madeAt(epoch.Add(time.Hour))), k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip), closedOn(epoch.Add(2*time.Hour)))}, "re-review"},
		{"approved Approval is not re-review", at, []bd.Bead{k("a", contract.KindApproval, "closed", closedWith("approved"), pinned(tip)), k("b", contract.KindBuild, "open")}, ""},
		{"round then re-review then stale pin", at, []bd.Bead{rev("r1"), rev("r2"), k("l", contract.KindLanding, "closed", closedWith("obsolete: build failed")), k("a", contract.KindApproval, "open", pinned("deadbeef00"))}, "round 2 · re-review · stale pin"},
		{"approve… Approval is spent, not approved", at, []bd.Bead{k("a", contract.KindApproval, "closed", closedWith("approve: ok"), pinned(tip)), k("b", contract.KindBuild, "open")}, "re-review"},
		{"Approval obsoleted is spent, not approved", at, []bd.Bead{k("a", contract.KindApproval, "closed", closedWith("Approval obsoleted: tip moved"), pinned(tip)), k("b", contract.KindBuild, "open")}, "re-review"},
		{"pin at tip is not stale", at, []bd.Bead{k("a", contract.KindApproval, "open", pinned(tip))}, ""},
		{"empty tip map is not stale", nil, []bd.Bead{k("a", contract.KindApproval, "open", pinned("deadbeef00"))}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := strings.Join(rollupOf(c.tips, c.kids...).Qualifiers, " · "); got != c.want {
				t.Errorf("qualifiers = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRollupWhy(t *testing.T) {
	const tip = "aaaa1111bbbb"
	k := func(id string, kind contract.Kind, status string, mut ...func(*bd.Bead)) bd.Bead {
		return dbead(id, kind, status, "cp", 1, mut...)
	}
	cases := []struct {
		name string
		kids []bd.Bead
		want []string
	}{
		{"driver bead", []bd.Bead{k("c1", contract.KindConflict, "open")}, []string{"c1", "conflict", "open"}},
		{"paused label", []bd.Bead{k("l1", contract.KindLanding, "open", labeled("paused:failing"))}, []string{"l1", "paused:failing"}},
		{"re-review cause", []bd.Bead{k("l0", contract.KindLanding, "closed", closedWith("obsolete: conflict")), k("b", contract.KindBuild, "open")}, []string{"after Landing obsolete: conflict"}},
		{"stale pin shas", []bd.Bead{k("a", contract.KindApproval, "open", pinned("deadbeef0123"))}, []string{"pin deadbeef ≠ tip aaaa1111"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := rollupOf(map[string]string{"cp": tip}, c.kids...).Why
			for _, w := range c.want {
				if !strings.Contains(why, w) {
					t.Errorf("why = %q, lacks %q", why, w)
				}
			}
		})
	}
}

// d on a Checkpoint shows rollup, why and also right after status, identically for the
// tree row and its Live twin.
func TestCheckpointDetailRollupLines(t *testing.T) {
	beads := []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer, func(b *bd.Bead) { b.BlockedBy = []string{"s1"} }),
		dbead("c", contract.KindConflict, "open", "cp", 1),
		dbead("s1", contract.KindSignal, "open", "", 1),
	}
	tab := execTab{g: graphData{beads: beads}}
	tab.flip = tab.expanded()
	var texts []string
	for _, r := range tab.rows() {
		if r.kind != eCP {
			continue
		}
		texts = append(texts, ansi.Strip(tab.rowText(r)))
		twin := r
		twin.kind = eLive
		texts = append(texts, ansi.Strip(tab.rowText(twin)))
	}
	if len(texts) != 2 || texts[0] != texts[1] {
		t.Fatalf("want identical tree and Live details, got %q", texts)
	}
	d := texts[0]
	iStatus, iRollup, iWhy, iAlso, iMembers := strings.Index(d, "status"), strings.Index(d, "rollup      conflict"), strings.Index(d, "why"), strings.Index(d, "also        blocked on Signal"), strings.Index(d, "members")
	if !(iStatus >= 0 && iStatus < iRollup && iRollup < iWhy && iWhy < iAlso && iAlso < iMembers) {
		t.Errorf("want status < rollup < why < also < members:\n%s", d)
	}
}

func TestRollupRetryingQualifierOrder(t *testing.T) {
	rev := func(id string) bd.Bead { return dbead(id, contract.KindReview, "closed", "cp", 1) }
	g := graphData{
		beads: append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer), dbead("l", contract.KindLanding, "open", "cp", 1),
			dbead("o", contract.KindLanding, "closed", "cp", 1, closedWith("obsolete: conflict"))}, rev("r1"), rev("r2")),
		landEv: map[string]string{"l": "land.failed"},
	}
	if got := strings.Join(g.rollup(g.beadByID("cp")).Qualifiers, " · "); got != "round 2 · re-review · retrying" {
		t.Errorf("qualifiers = %q", got)
	}
}

func TestRollupRetrying(t *testing.T) {
	const tip = "aaaa1111"
	cases := []struct {
		name   string
		status string
		ev     string
		want   string
	}{
		{"land.failed", "open", "land.failed", "landing · retrying"},
		{"gate.infra", "open", "gate.infra", "landing · retrying"},
		{"gate.pending", "in_progress", "gate.pending", "landing · retrying"},
		{"effort.failed", "open", "effort.failed", "landing · retrying"},
		{"success event", "open", "land.landed", "landing"},
		{"effort.paused", "open", "effort.paused", "landing"},
		{"no event", "open", "", "landing"},
		{"snapshot shows the Landing closed", "closed", "land.failed", "cut"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := graphData{
				beads:  []bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer), dbead("l", contract.KindLanding, c.status, "cp", 1)},
				cpTips: map[string]string{"cp": tip},
				landEv: map[string]string{"l": c.ev},
			}
			r := g.rollup(g.beadByID("cp"))
			if got := strings.Join(append([]string{r.Word}, r.Qualifiers...), " · "); got != c.want {
				t.Errorf("rollup = %q, want %q", got, c.want)
			}
		})
	}
}

// Folding a log read indexes the latest landing event per bead; a later success clears retrying.
func TestLogsMsgIndexesLandingEvents(t *testing.T) {
	m := sized(t, 120, 30)
	ev := func(msg string, sec int64) eventlog.Event {
		return eventlog.Event{Msg: msg, Bead: "l", Time: time.Unix(sec, 0), Raw: map[string]any{}}
	}
	m, _ = send(t, m, logsMsg{path: "p", changed: true, events: []eventlog.Event{ev("land.failed", 1)}, digest: []eventlog.Event{ev("land.failed", 1)}})
	if m.landEv["l"] != "land.failed" {
		t.Fatalf("landEv = %v, want l → land.failed", m.landEv)
	}
	m, _ = send(t, m, logsMsg{path: "p", changed: true, events: []eventlog.Event{ev("land.failed", 1), ev("land.landed", 2), {Msg: "hitl.exited", Bead: "l", Time: time.Unix(3, 0), Raw: map[string]any{}}}, digest: []eventlog.Event{ev("land.failed", 1), ev("land.landed", 2), {Msg: "hitl.exited", Bead: "l", Time: time.Unix(3, 0), Raw: map[string]any{}}}})
	if m.landEv["l"] != "land.landed" {
		t.Errorf("landEv = %v, want l → land.landed", m.landEv)
	}
}

func landBody(body string) func(*bd.Bead) { return func(b *bd.Bead) { b.Description = body } }

func TestRollupMRQualifiers(t *testing.T) {
	const mr = "MR: !42\n"
	cases := []struct {
		name string
		land []func(*bd.Bead)
		want string
	}{
		{"mergeable", []func(*bd.Bead){landBody(mr + "MR status: mergeable")}, "landing · !42 · awaiting merge"},
		{"ci_still_running", []func(*bd.Bead){landBody(mr + "MR status: ci_still_running")}, "landing · !42 · pipeline running"},
		{"need_rebase", []func(*bd.Bead){landBody(mr + "MR status: need_rebase")}, "landing · !42 · needs rebase"},
		{"not_approved", []func(*bd.Bead){landBody(mr + "MR status: not_approved")}, "landing · !42 · needs approval"},
		{"discussions_not_resolved", []func(*bd.Bead){landBody(mr + "MR status: discussions_not_resolved")}, "landing · !42 · threads open"},
		{"draft_status", []func(*bd.Bead){landBody(mr + "MR status: draft_status")}, "landing · !42 · draft"},
		{"ci_must_pass unmapped", []func(*bd.Bead){landBody(mr + "MR status: ci_must_pass")}, "landing · !42"},
		{"requested_changes unmapped", []func(*bd.Bead){landBody(mr + "MR status: requested_changes")}, "landing · !42"},
		{"invented unmapped", []func(*bd.Bead){landBody(mr + "MR status: brand_new")}, "landing · !42"},
		{"no status line", []func(*bd.Bead){landBody(mr)}, "landing · !42"},
		{"no MR line", []func(*bd.Bead){landBody("MR status: mergeable")}, "landing"},
		{"land failed", []func(*bd.Bead){labeled("paused:failing"), landBody(mr + "MR status: need_rebase")}, "land failed · !42"},
		{"land paused", []func(*bd.Bead){labeled("paused:manual"), landBody(mr + "MR status: need_rebase")}, "land paused · !42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := rollupOf(nil, dbead("l", contract.KindLanding, "open", "cp", 1, c.land...))
			if got := r.label(); got != c.want {
				t.Errorf("label = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRollupMRFullOrder(t *testing.T) {
	rev := func(id string) bd.Bead { return dbead(id, contract.KindReview, "closed", "cp", 1) }
	g := graphData{
		beads: append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
			dbead("l", contract.KindLanding, "open", "cp", 1, landBody("MR: !42\nMR status: need_rebase")),
			dbead("o", contract.KindLanding, "closed", "cp", 1, closedWith("obsolete: conflict"))}, rev("r1"), rev("r2")),
		landEv: map[string]string{"l": "land.failed"},
	}
	if got := strings.Join(g.rollup(g.beadByID("cp")).Qualifiers, " · "); got != "round 2 · re-review · !42 · needs rebase · retrying" {
		t.Errorf("qualifiers = %q", got)
	}
}

func TestRollupMRWhy(t *testing.T) {
	body := landBody("MR: !42\nMR status: need_rebase")
	for name, mut := range map[string][]func(*bd.Bead){"landing": {body}, "land paused": {labeled("paused:manual"), body}} {
		t.Run(name, func(t *testing.T) {
			why := rollupOf(nil, dbead("l", contract.KindLanding, "open", "cp", 1, mut...)).Why
			if !strings.Contains(why, "MR !42 detailed_merge_status=need_rebase") || strings.Contains(why, "since") {
				t.Errorf("why = %q", why)
			}
		})
	}
	if why := rollupOf(nil, dbead("l", contract.KindLanding, "open", "cp", 1, landBody("MR: !42"))).Why; !strings.HasSuffix(why, " · MR !42") {
		t.Errorf("why without status = %q", why)
	}
}

func TestRollupNestedMRName(t *testing.T) {
	l := dbead("l", contract.KindLanding, "open", "cp", 1, landBody("MR: grp/proj!42"))
	cp := dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer, labeled("context:child"))
	g := graphData{beads: []bd.Bead{cp, l}}
	r := g.rollup(g.beadByID("cp"))
	if !slices.Contains(r.Qualifiers, "child!42") || !strings.HasSuffix(r.Why, " · MR grp/proj!42") {
		t.Errorf("got %v / %q", r.Qualifiers, r.Why)
	}
}

func TestRollupWorkspaceMRKeepsBareName(t *testing.T) {
	l := dbead("l", contract.KindLanding, "open", "cp", 1, landBody("MR: !42"))
	cp := dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer, labeled("context:mugicrew"))
	g := graphData{beads: []bd.Bead{cp, l}}
	r := g.rollup(g.beadByID("cp"))
	if !slices.Contains(r.Qualifiers, "!42") || !strings.HasSuffix(r.Why, " · MR !42") {
		t.Errorf("got %v / %q", r.Qualifiers, r.Why)
	}
}
