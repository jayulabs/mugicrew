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

func TestReplyRenderCarriesTheWholeReasonCutAt2000(t *testing.T) {
	b := bd.Bead{ID: "ws-x", Title: "T", CloseReason: "first\nsecond " + strings.Repeat("é", 2000)}
	got := renderReply(b, "h", "n", "12345678")
	want := "🤖 mugicrew: addressed by **T** (ws-x@n) in 12345678: first\nsecond " + strings.Repeat("é", 1987) + "…\n" + marker("ws-x", "h", "n")
	if got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
	b.CloseReason = strings.Repeat("a", 2000)
	if got := renderReply(b, "h", "n", ""); !strings.Contains(got, "(ws-x@n): "+b.CloseReason+"\n") {
		t.Errorf("a 2000-char reason must stay whole: %q", got)
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
