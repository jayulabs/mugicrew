package jinbe

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

const disc1 = "d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1"

func note(id int, author int, body string) forge.Note {
	n := forge.Note{ID: id, Body: body}
	n.Author.ID, n.Author.Username = author, "user"+body[:1]
	return n
}

func thread(id string, notes ...forge.Note) forge.Discussion {
	return forge.Discussion{ID: id, Resolvable: true, Notes: notes}
}

// A tick over an opened MR with the given discussions: what was filed, and how the Landing ended.
func threadTick(t *testing.T, cpKind contract.Kind, r *jinbeSeamRec) (err error, l bd.Bead) {
	t.Helper()
	r.head = "cafef00d"
	r.landErr = &git.LandPending{MR: 7}
	if r.mr == nil {
		r.mr = &forge.MR{IID: 7, State: "opened", WebURL: "https://gl/mr/7"}
	}
	stubJinbeSeams(t, r)
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-gone" { // a stale entry of a closed Landing (the prune tests)
			return bd.Bead{ID: id, Status: "closed"}, nil
		}
		return bd.Bead{ID: "ws-cp", Kind: cpKind, Title: "Ship", Status: "open"}, nil
	}
	l = openLanding("cafef00d")
	l.Description += "\nMR: !7\n"
	return reconcileLanding(l), l
}

func TestActionableThreadFilesABeadAndObsoletesTheLanding(t *testing.T) {
	diff := thread(disc1, note(11, 5, "rename this\nit is unclear"), note(12, 6, "agreed"))
	diff.Notes[0].Position = &forge.Position{NewPath: "a.go", NewLine: 9, HeadSHA: "h1"}
	for _, c := range []struct {
		kind contract.Kind
		want contract.Kind
		tip  bool
	}{{contract.KindCheckpoint, contract.KindCritique, true}, {contract.KindChart, contract.KindGrilling, false}} {
		notesSeen.Delete("ws-land") // one Landing id serves both cases; each is a first tick
		r := &jinbeSeamRec{discussions: []forge.Discussion{diff}}
		if err, _ := threadTick(t, c.kind, r); err != nil {
			t.Fatal(err)
		}
		if len(r.threadSpawned) != 1 || r.threadSpawned[0].Kind != c.want || r.threadSpawned[0].DiscoveredFrom != "ws-land" {
			t.Fatalf("%s: spawned %+v, want one %s discovered from the Landing", c.kind, r.threadSpawned, c.want)
		}
		s := r.threadSpawned[0]
		if s.Title != "rename this" || s.DraftedBy != "" {
			t.Errorf("%s: title %q drafter %q, want the first line, published", c.kind, s.Title, s.DraftedBy)
		}
		for _, want := range []string{"Thread: !7 " + disc1 + "\nThread note: 12\nNote: https://gl/mr/7#note_11\nPosition: a.go:9 @ h1\n", "> **@userr**: rename this\n> it is unclear\n> **@usera**: agreed\n"} {
			if !strings.Contains(s.Description, want) {
				t.Errorf("%s: body %q lacks %q", c.kind, s.Description, want)
			}
		}
		if got := strings.Contains(s.Description, "Tip: cafef00d\n"); got != c.tip {
			t.Errorf("%s: Tip: present = %v, want %v", c.kind, got, c.tip)
		}
		if len(r.closed) != 1 || r.closed[0] != "ws-land" || r.reasons[0] != "obsolete: review comment" || len(r.landed) != 0 || len(r.descAsserts) != 0 {
			t.Errorf("%s: closed %v %v landed %v asserts %v, want only the Landing closed obsolete", c.kind, r.closed, r.reasons, r.landed, r.descAsserts)
		}
	}
}

func TestThreadsThatAreNotActionableFileNothing(t *testing.T) {
	sys := thread("s", note(1, 5, "added 1 commit"))
	sys.Notes[0].System = true
	resolved := thread("r", note(2, 5, "done"))
	resolved.Resolved = true
	plain := thread("p", note(3, 5, "fyi"))
	plain.Resolvable, plain.IndividualNote = false, true
	for name, d := range map[string]forge.Discussion{
		"system note": sys, "resolved": resolved, "plain comment": plain,
		"bot": thread("b", note(4, 99, "automated")),
	} {
		t.Run(name, func(t *testing.T) {
			r := &jinbeSeamRec{discussions: []forge.Discussion{d}, bots: map[int]bool{99: true}}
			if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil {
				t.Fatal(err)
			}
			if len(r.threadSpawned) != 0 || len(r.closed) != 0 {
				t.Errorf("spawned %v closed %v, want nothing", r.threadSpawned, r.closed)
			}
			if len(r.descAsserts) != 1 {
				t.Errorf("the remaining arms must run: asserts %v", r.descAsserts)
			}
		})
	}
}

func TestAnUnclosedBeadSuppressesRefiling(t *testing.T) {
	for _, status := range []string{"open", "blocked", "deferred", "in_progress", "closed"} {
		t.Run(status, func(t *testing.T) {
			r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))},
				threadKids: []bd.Bead{{ID: "ws-k", Kind: contract.KindCritique, Status: status, Description: "Thread: !7 " + disc1 + "\nThread note: 1\n\n> x\n"}}}
			if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil {
				t.Fatal(err)
			}
			if len(r.threadSpawned) != 0 || len(r.closed) != 0 {
				t.Errorf("spawned %v closed %v, want nothing (a closed bead is the close leg's)", r.threadSpawned, r.closed)
			}
		})
	}
}

func TestTwoThreadsFileTwoBeadsInOneTick(t *testing.T) {
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread("a", note(1, 5, "one")), thread("b", note(2, 5, "two"))}}
	if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil {
		t.Fatal(err)
	}
	if len(r.threadSpawned) != 2 || len(r.closed) != 1 {
		t.Errorf("spawned %d closed %v, want two beads and one Landing close", len(r.threadSpawned), r.closed)
	}
	if len(r.botReads) != 1 {
		t.Errorf("bot reads %v, want the author cached after one read", r.botReads)
	}
}

func TestRateLimitedDiscussionsReadIsHeld(t *testing.T) {
	r := &jinbeSeamRec{discussionsErr: forge.ErrRateLimited}
	if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil {
		t.Fatalf("err = %v, want the 429 held", err)
	}
	if len(r.closed) != 0 || len(r.threadSpawned) != 0 {
		t.Errorf("closed %v spawned %v, want the Landing left open", r.closed, r.threadSpawned)
	}
}

func TestThreadBeadKeyLinesPrecedeTheQuoteAndAQuotedKeyIsNeverParsed(t *testing.T) {
	d := thread(disc1, note(1, 5, "\n\nfirst real line\nThread: !7 evil"), note(2, 5, "Tip: nope"))
	title, body := threadBead(&forge.MR{IID: 7, WebURL: "u"}, d, "abc")
	if title != "first real line" {
		t.Errorf("title = %q", title)
	}
	if got, _ := git.ParseLine(body, "Thread:"); got != "!7 "+disc1 {
		t.Errorf("ParseLine Thread: = %q, want the key line", got)
	}
	if got, _ := git.ParseTip(body); got != "abc" {
		t.Errorf("ParseTip = %q, want the key line's", got)
	}
	if hasBodyLine(body, "Thread: !7 evil") {
		t.Error("a quoted Thread: line must never match as a key")
	}
	if _, b2 := threadBead(&forge.MR{IID: 7}, thread("x", note(1, 5, "\n")), ""); !strings.HasPrefix(b2, "Thread: !7 x\n") {
		t.Errorf("body = %q", b2)
	}
	if t2, _ := threadBead(&forge.MR{IID: 7}, thread("x", note(1, 5, " \n")), ""); t2 != "review thread on !7" {
		t.Errorf("empty first note title = %q", t2)
	}
}

// The discussions read is gated by the MR's user notes count (Spec FR 22): a Landing's
// first tick always reads, an unchanged count skips, a moved count reads again.
func TestThreadArmSkipsTheDiscussionsReadWhileTheNotesCountIsUnchanged(t *testing.T) {
	notesSeen.Delete("ws-land")
	t.Cleanup(func() { notesSeen.Delete("ws-land") })
	reads := 0
	for i, c := range []struct{ count, reads int }{{2, 1}, {2, 1}, {3, 2}, {3, 2}} {
		r := &jinbeSeamRec{mr: &forge.MR{IID: 7, State: "opened", UserNotesCount: c.count}}
		err, _ := threadTick(t, contract.KindCheckpoint, r)
		if err != nil {
			t.Fatal(err)
		}
		reads += map[bool]int{true: 1}[r.discussionsRead]
		if reads != c.reads {
			t.Errorf("tick %d (count %d): %d discussions reads, want %d", i, c.count, reads, c.reads)
		}
	}
}

func TestNotesSeenPrunesClosedLandings(t *testing.T) {
	notesSeen.Store("ws-gone", 1)
	notesSeen.Delete("ws-land")
	t.Cleanup(func() { notesSeen.Delete("ws-gone"); notesSeen.Delete("ws-land") })
	r := &jinbeSeamRec{}
	if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil {
		t.Fatal(err)
	}
	if _, ok := notesSeen.Load("ws-gone"); ok {
		t.Error("closed Landing still in notesSeen")
	}
}

func marker(id, hash, name string) string {
	return "<!-- mugicrew-reply: " + id + "@" + hash + " " + name + " -->"
}

func closedBead(note int, reason string) bd.Bead {
	return bd.Bead{ID: "ws-fix", Kind: contract.KindCritique, Status: "closed", Title: "Fix it", CloseReason: reason,
		Description: "Thread: !7 " + disc1 + "\nThread note: " + strconv.Itoa(note) + "\n\n> x\n"}
}

const wantReply = "reply " + disc1 + " 🤖 mugicrew: addressed by **Fix it** (ws-fix@jinbe) in abcdef12: renamed\n<!-- mugicrew-reply: ws-fix@g00d jinbe -->"

func TestCloseLeg(t *testing.T) {
	human := note(1, 5, "rename")
	other := note(2, 7, marker("ws-fix", "other", "jinbe"))
	for name, c := range map[string]struct {
		notes    []forge.Note
		resolved bool
		sha      string
		want     []string
	}{
		"no new reply yields reply then resolve": {[]forge.Note{human}, false, "abcdef123456", []string{wantReply, "resolve " + disc1}},
		"marker present yields resolve only":     {[]forge.Note{human, note(2, 7, marker("ws-fix", "g00d", "old"))}, false, "abcdef123456", []string{"resolve " + disc1}},
		"already resolved yields reply only":     {[]forge.Note{human}, true, "abcdef123456", []string{wantReply}},
		"marker and resolved is a no-op":         {[]forge.Note{human, note(2, 7, marker("ws-fix", "g00d", "jinbe"))}, true, "abcdef123456", nil},
		"another graph's marker is no marker":    {[]forge.Note{human, other}, false, "abcdef123456", []string{wantReply, "resolve " + disc1}},
		"no integrate commit omits in sha8":      {[]forge.Note{human}, true, "", []string{strings.Replace(wantReply, " in abcdef12", "", 1)}},
	} {
		t.Run(name, func(t *testing.T) {
			d := thread(disc1, c.notes...)
			d.Resolved = c.resolved
			r := &jinbeSeamRec{discussions: []forge.Discussion{d}, integrateSHA: c.sha,
				threadKids: []bd.Bead{closedBead(1, "renamed\nmore")}}
			if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil || !slices.Equal(r.sends, c.want) || len(r.threadSpawned) != 0 || len(r.descAsserts) != 1 || len(r.closed) != 0 {
				t.Errorf("err %v sends %q spawned %v asserts %v closed %v, want %q and the remaining arms run", err, r.sends, r.threadSpawned, r.descAsserts, r.closed, c.want)
			}
		})
	}
}

func TestCloseLegUsesTheNewestClosedMember(t *testing.T) {
	old, newer := closedBead(1, "old"), closedBead(1, "newer")
	old.ID, old.Created = "ws-old", time.Unix(1, 0)
	newer.Created = time.Unix(2, 0)
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}, threadKids: []bd.Bead{newer, old}}
	if err, _ := threadTick(t, contract.KindCheckpoint, r); err != nil || len(r.sends) != 2 || !strings.Contains(r.sends[0], "(ws-fix@jinbe)") {
		t.Errorf("err %v sends %q, want B = the newest member", err, r.sends)
	}
}

func TestCloseLegErrors(t *testing.T) {
	for name, c := range map[string]struct {
		rec  jinbeSeamRec
		want []string
		err  error
	}{
		"403 on resolve is skipped":          {jinbeSeamRec{resolveErr: forge.ErrForbidden}, []string{wantReply, "resolve " + disc1}, nil},
		"404 on reply skips the thread":      {jinbeSeamRec{replyErr: forge.ErrNotFound}, []string{wantReply}, nil},
		"429 is held, the Landing left open": {jinbeSeamRec{replyErr: forge.ErrRateLimited}, []string{wantReply}, nil},
		"other errors are returned":          {jinbeSeamRec{resolveErr: errors.New("boom")}, []string{wantReply, "resolve " + disc1}, errors.New("boom")},
	} {
		t.Run(name, func(t *testing.T) {
			r := c.rec
			r.discussions, r.integrateSHA = []forge.Discussion{thread(disc1, note(1, 5, "x"))}, "abcdef123456"
			r.threadKids = []bd.Bead{closedBead(1, "renamed")}
			err, _ := threadTick(t, contract.KindCheckpoint, &r)
			if (err == nil) != (c.err == nil) || !slices.Equal(r.sends, c.want) {
				t.Errorf("err = %v want %v, sends %q want %q", err, c.err, r.sends, c.want)
			}
		})
	}
}

func TestReplyRenderCutsTheReasonAt200(t *testing.T) {
	b := bd.Bead{ID: "ws-x", Title: "T", CloseReason: strings.Repeat("é", 201) + "\nsecond"}
	got := renderReply(b, "h", "n", "")
	if want := "🤖 mugicrew: addressed by **T** (ws-x@n): " + strings.Repeat("é", 200) + "…\n" + marker("ws-x", "h", "n"); got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
	b.CloseReason = strings.Repeat("a", 200)
	if got := renderReply(b, "h", "n", "12345678"); !strings.Contains(got, "in 12345678: "+b.CloseReason+"\n") {
		t.Errorf("a 200-char reason must stay whole: %q", got)
	}
}

func TestReplyMarkerMatch(t *testing.T) {
	for _, c := range []struct {
		body         string
		marked, forB bool
	}{
		{marker("b", "h", "x"), true, true},
		{"text\n" + marker("b", "h", "other") + "\nmore", true, true},
		{marker("b", "h2", "x"), true, false},
		{marker("b2", "h", "x"), true, false},
		{"> " + marker("b", "h", "x"), false, false},
		{"see " + marker("b", "h", "x"), false, false},
		{"addressed by b@x", false, false},
	} {
		if m, f := replyMarked(c.body, "b", "h"); m != c.marked || f != c.forB {
			t.Errorf("%q = %v %v, want %v %v", c.body, m, f, c.marked, c.forB)
		}
	}
}

func TestAnchorRule(t *testing.T) {
	b := closedBead(1, "r")
	ns := func(bodies ...string) []forge.Note {
		var out []forge.Note
		for i, body := range bodies {
			out = append(out, note(i+1, 5, body))
		}
		return out
	}
	mk := marker("ws-fix", "g00d", "jinbe")
	for name, c := range map[string]struct {
		notes          []forge.Note
		reply, resolve bool
	}{
		"anchor is the thread note, nothing after": {ns("a"), true, true},
		"a system note after is not a reply":       {append(ns("a"), forge.Note{ID: 9, System: true, Body: "x"}), true, true},
		"a marked note after another graph's":      {ns("a", marker("ws-fix", "zzz", "n")), true, true},
		"a human note after the thread note":       {ns("a", "b"), false, false},
		"the last marker is the anchor":            {ns("a", mk, "b"), false, false},
		"human note before the marker is old":      {ns("a", "b", mk), false, true},
	} {
		m, n := threadAnchor(c.notes, b, "g00d")
		if r, s := closeLegAction(m, n, false); r != c.reply || s != c.resolve {
			t.Errorf("%s: got %v %v, want %v %v", name, r, s, c.reply, c.resolve)
		}
	}
	// the anchor note deleted: the starter note stands in, so it alone is no reply
	late := []forge.Note{note(5, 5, "late")}
	if m, n := threadAnchor(late, b, "g00d"); n || m {
		t.Errorf("deleted anchor alone: got marker %v newReply %v", m, n)
	}
	if _, n := threadAnchor(append(late, note(6, 5, "more")), b, "g00d"); !n {
		t.Error("deleted anchor + a human reply: want a new reply")
	}
}

func TestReopenLeg(t *testing.T) {
	mk := note(3, 7, marker("ws-fix", "g00d", "jinbe"))
	for name, c := range map[string]struct {
		notes    []forge.Note
		resolved bool
		filed    bool
		wantNote string
	}{
		"unmarked reply on an unresolved thread files": {[]forge.Note{note(1, 5, "a"), note(2, 5, "still broken")}, false, true, "Thread note: 2\n"},
		"the same reply on a resolved thread is none":  {[]forge.Note{note(1, 5, "a"), note(2, 5, "still broken")}, true, false, ""},
		"a Jinbe-marked note is not a new reply":       {[]forge.Note{note(1, 5, "a"), mk}, false, false, ""},
		"a reply after the marker files":               {[]forge.Note{note(1, 5, "a"), mk, note(4, 5, "nope")}, false, true, "Thread note: 4\n"},
		"a deleted anchor makes later notes new":       {[]forge.Note{note(8, 5, "a"), note(9, 5, "b")}, false, true, "Thread note: 9\n"},
		"a deleted anchor alone is not a reply":        {[]forge.Note{note(8, 5, "a")}, false, false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			d := thread(disc1, c.notes...)
			d.Resolved = c.resolved
			r := &jinbeSeamRec{discussions: []forge.Discussion{d}, integrateSHA: "abcdef123456", threadKids: []bd.Bead{closedBead(1, "r")}}
			err, _ := threadTick(t, contract.KindCheckpoint, r)
			if err != nil || (len(r.threadSpawned) == 1) != c.filed {
				t.Fatalf("err %v spawned %v, want filed = %v", err, r.threadSpawned, c.filed)
			}
			if c.filed {
				if !strings.Contains(r.threadSpawned[0].Description, c.wantNote) || slices.ContainsFunc(r.sends, func(s string) bool { return strings.HasPrefix(s, "reply") }) || len(r.closed) != 1 || r.reasons[0] != "obsolete: review comment" {
					t.Errorf("body %q sends %q closed %v %v", r.threadSpawned[0].Description, r.sends, r.closed, r.reasons)
				}
			}
		})
	}
}

// A later thread's failure (a 429 on isBot) still records and closes for the thread already filed.
func TestPartialFilingStillObsoletesTheLanding(t *testing.T) {
	r := &jinbeSeamRec{
		discussions: []forge.Discussion{thread(disc1, note(1, 5, "a")), thread("d2", note(2, 6, "b"))},
		botErrs:     map[int]error{6: forge.ErrRateLimited},
	}
	err, _ := threadTick(t, contract.KindCheckpoint, r)
	if len(r.threadSpawned) != 1 || len(r.closed) != 1 || r.reasons[0] != "obsolete: review comment" {
		t.Errorf("spawned %v closed %v %v, want the first filed and the Landing closed (err %v)", r.threadSpawned, r.closed, r.reasons, err)
	}
}

func TestRemovedLineThreadRecordsTheOldSidePosition(t *testing.T) {
	d := thread(disc1, note(11, 5, "why delete this"))
	d.Notes[0].Position = &forge.Position{NewPath: "a.go", OldPath: "a.go", OldLine: 4, HeadSHA: "h1"}
	_, body := threadBead(&forge.MR{IID: 7}, d, "")
	if want := "Position: a.go:4 @ h1\n"; !strings.Contains(body, want) {
		t.Errorf("body %q lacks %q", body, want)
	}
}
