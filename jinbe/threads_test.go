package jinbe

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
)

const disc1 = "d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1"

func note(id int, author int, body string) forge.Note {
	n := forge.Note{ID: id, Body: body, CreatedAt: testNow.Add(-time.Hour)}
	n.Author.ID, n.Author.Username = author, "user"+body[:1]
	return n
}

func thread(id string, notes ...forge.Note) forge.Discussion {
	return forge.Discussion{ID: id, Resolvable: true, Notes: notes}
}

func marker(id, hash, name string) string {
	return "<!-- mugicrew-reply: " + id + "@" + hash + " " + name + " -->"
}

func closedBead(note int, reason string) bd.Bead {
	return bd.Bead{ID: "ws-fix", Kind: contract.KindCritique, Status: "closed", Title: "Fix it", CloseReason: reason,
		Description: "Source: !7 " + disc1 + " " + strconv.Itoa(note) + "\n\n> x\n"}
}

func TestParseVerdict(t *testing.T) {
	for _, c := range []struct {
		reason, rationale, rest string
		v                       verdict
		ok                      bool
	}{
		{"accepted: done", "done", "", verdictAccepted, true},
		{"  REJECTED: not needed", "not needed", "", verdictRejected, true},
		{"\n\tAnswered:  because", "because", "", verdictAnswered, true},
		{"Deferred: later", "later", "", verdictDeferred, true},
		{"pivoted: mc-a, mc-b", pivotRationale, "mc-a, mc-b", verdictDeferred, true},
		{"PIVOTED: mc-a", pivotRationale, "mc-a", verdictDeferred, true},
		{"accepted:", "", "", verdictAccepted, true},
		{"wontfix: x", "wontfix: x", "", verdictClosed, false},
		{"fixed: y", "fixed: y", "", verdictClosed, false},
		{"Closed", "", "", verdictClosed, false},
		{"", "", "", verdictClosed, false},
		{"  plain words ", "plain words", "", verdictClosed, false},
	} {
		v, why, rest, ok := parseVerdict(c.reason)
		if v != c.v || why != c.rationale || rest != c.rest || ok != c.ok {
			t.Errorf("%q = %v %q %q %v, want %v %q %q %v", c.reason, v, why, rest, ok, c.v, c.rationale, c.rest, c.ok)
		}
	}
}

func TestCloseVerdictFallback(t *testing.T) {
	for _, c := range []struct {
		name string
		b    bd.Bead
		sha8 string
		v    verdict
		why  string
	}{
		{"commit", bd.Bead{CloseReason: "Closed"}, "12345678", verdictAccepted, ""},
		{"asks", bd.Bead{Title: "Reviewer asks why X", CloseReason: "Closed"}, "", verdictAnswered, ""},
		{"asks in body", bd.Bead{Title: "T", Description: "Reviewer asks why", CloseReason: "because"}, "", verdictAnswered, "because"},
		{"neither", bd.Bead{Title: "T"}, "", verdictClosed, ""},
		{"commit beats asks", bd.Bead{Title: "Reviewer asks", CloseReason: "wontfix: x"}, "12345678", verdictAccepted, "wontfix: x"},
		{"prefix wins", bd.Bead{CloseReason: "rejected: no"}, "12345678", verdictRejected, "no"},
		{"pivot", bd.Bead{CloseReason: "pivoted: mc-a"}, "", verdictDeferred, pivotRationale},
	} {
		if v, why := closeVerdict(c.b, c.sha8); v != c.v || why != c.why {
			t.Errorf("%s = %v %q, want %v %q", c.name, v, why, c.v, c.why)
		}
	}
}

func TestRenderReplyHeaders(t *testing.T) {
	b := bd.Bead{ID: "ws-x", Title: "T"}
	m := marker("ws-x", "h", "n")
	q := "**T** (ws-x@n)"
	for _, c := range []struct {
		name     string
		v        verdict
		sha8     string
		why      string
		carriers []string
		want     string
	}{
		{"accepted", verdictAccepted, "12345678", "done", nil, "🤖 mugicrew — ✅ Accepted · fixed in 12345678 by " + q + "\n\n**Rationale:** done\n" + m},
		{"accepted no commit", verdictAccepted, "", "already", nil, "🤖 mugicrew — ✅ Accepted · by " + q + "\n\n**Rationale:** already\n" + m},
		{"rejected", verdictRejected, "12345678", "no", nil, "🤖 mugicrew — ❌ Rejected · by " + q + "\n\n**Rationale:** no\n" + m},
		{"answered", verdictAnswered, "12345678", "a", nil, "🤖 mugicrew — 💬 Answered · by " + q + "\n\n**Rationale:** a\n" + m},
		{"deferred", verdictDeferred, "12345678", "later", []string{"C1", "C2"}, "🤖 mugicrew — ⏭️ Deferred · " + q + " · tracked by **C1**, **C2**\n\n**Rationale:** later\n" + m},
		{"deferred none", verdictDeferred, "", "later", nil, "🤖 mugicrew — ⏭️ Deferred · " + q + "\n\n**Rationale:** later\n" + m},
		{"closed", verdictClosed, "12345678", "r", nil, "🤖 mugicrew — ☑️ Closed · " + q + "\n\n**Rationale:** r\n" + m},
		{"no rationale", verdictAccepted, "12345678", "  ", nil, "🤖 mugicrew — ✅ Accepted · fixed in 12345678 by " + q + "\n" + m},
	} {
		if got := renderReply(b, "h", "n", c.v, c.sha8, c.why, c.carriers); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestReplyRationaleCutAt2000(t *testing.T) {
	b := bd.Bead{ID: "ws-x", Title: "T"}
	got := renderReply(b, "h", "n", verdictAnswered, "", "first\nsecond "+strings.Repeat("é", 2000), nil)
	want := "🤖 mugicrew — 💬 Answered · by **T** (ws-x@n)\n\n**Rationale:** first\nsecond " + strings.Repeat("é", 1987) + "…\n" + marker("ws-x", "h", "n")
	if got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
	whole := strings.Repeat("a", 2000)
	if got := renderReply(b, "h", "n", verdictAnswered, "", whole, nil); !strings.Contains(got, "**Rationale:** "+whole+"\n") {
		t.Errorf("a 2000-char rationale must stay whole: %q", got)
	}
}

func TestStrandedReply(t *testing.T) {
	b := bd.Bead{ID: "ws-x", Title: "T"}
	want := "🤖 mugicrew: not shipped with this MR: carried by **Ship: commits after the landed Tip: are not on mainline**\n" + marker("ws-x", "h", "n")
	if got := renderStranded(b, "h", "n", "Ship: commits after the landed Tip: are not on mainline"); got != want {
		t.Errorf("reply = %q, want %q", got, want)
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

func TestSweptReply(t *testing.T) {
	for _, c := range []struct {
		escalated, carried bool
		v                  verdict
		want               string
	}{
		{false, true, verdictDeferred, "The Spec was revised and this Merge Request was replaced; this ask moves to the revised Spec."},
		{true, true, verdictDeferred, "The Spec was sent back to charting and this Merge Request was replaced; this ask moves to the new Chart."},
		{false, false, verdictClosed, "The Spec was revised and this Merge Request was replaced; this ask was not carried into the revised Spec."},
		{true, false, verdictClosed, "The Spec was sent back to charting and this Merge Request was replaced; this ask was not carried forward."},
	} {
		if v, got := sweptReply(bd.SweptReason{Escalated: c.escalated}, c.carried); v != c.v || got != c.want {
			t.Errorf("escalated %v carried %v = %v %q, want %v %q", c.escalated, c.carried, v, got, c.v, c.want)
		}
	}
}
