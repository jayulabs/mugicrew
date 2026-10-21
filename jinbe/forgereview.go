package jinbe

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// settleWait is how old the newest candidate note must be before a Forge Review reads
// it: the mark is then the max id and Notes count: agrees with it, and a note still being
// inserted is left for a later tick (Spec D2, the mark race guard).
const settleWait = 30 * time.Second

// tagInjection matches a note body's own <note>/<discussion> tags, which would forge a
// snapshot tag Usopp's ledger check reads (a JUDGE id that is no note).
var tagInjection = regexp.MustCompile(`(?i)<(/?)(note|discussion)\b`)

// skippedAt remembers, per container, the user notes count of an all-skipped read made
// while no closed Forge Review existed to carry the mark; a restart forgets it and
// merely reads once more.
var skippedAt sync.Map

// forgeReviewArm is the one function that reads a container's recorded MR for unacked
// notes (Spec D4): mirrorMR calls it for a pending Landing, the MR Watch between
// Landings. mr is the caller's read of the MR. It does nothing while a Forge Review is
// open, and skips the discussions read while mr's user notes count equals the newest
// successfully closed Forge Review's Notes: count (with none, it always reads).
// Otherwise it reads the discussions, applies the mechanical pre-filter and spawns a
// Forge Review over what remains, DiscoveredFrom landing when one is pending (nil for
// none). Every note skipped advances the newest closed Forge Review's mark instead. A
// spawn is the only graph write besides that advance; an error is the caller's to hold.
func forgeReviewArm(cp bd.Bead, landing *bd.Bead, lg legs, mr *forge.MR) error {
	reviews, err := childrenOfKind(cp.ID, contract.KindForgeReview)
	if err != nil {
		return err
	}
	if err := awardDue(reviews, lg, mr.IID); err != nil {
		return err // a 429: the caller holds it
	}
	if slices.ContainsFunc(reviews, bd.Bead.Unclosed) {
		return nil // at most one open per container; it holds the land and reads this round
	}
	last, hasLast := newestClosedReview(reviews)
	mark := 0
	if !hasLast {
		if n, ok := skippedAt.Load(cp.ID); ok && n.(int) == mr.UserNotesCount {
			return nil // an all-skipped read with no Forge Review to carry its mark: nothing new since
		}
	} else {
		count, err := strconv.Atoi(forgeReviewLine(last, "Notes count:"))
		if err == nil && count == mr.UserNotesCount {
			return nil // a quiet tick: no new note
		}
		mark, _ = strconv.Atoi(forgeReviewLine(last, "Read through note:"))
	}
	discussions, err := forgeDiscussions(lg.proj, mr.IID)
	if err != nil {
		return err
	}
	snap := readSnapshot(discussions, mark)
	if snap.through == 0 && mark == 0 {
		return nil // an MR with no notes at all
	}
	if len(snap.judge) == 0 {
		if !hasLast {
			skippedAt.Store(cp.ID, mr.UserNotesCount)
		}
		return advanceMark(last, hasLast, max(snap.through, mark), mr.UserNotesCount)
	}
	if clock().Sub(snap.newest) < settleWait {
		return nil
	}
	body := snap.body(mr, lg.mrPath, mark, hasLast)
	s := bd.Successor{Kind: contract.KindForgeReview,
		Title:       fmt.Sprintf("Read %s notes through %d", git.MRRef(lg.mrPath, mr.IID), snap.through),
		Description: body}
	if landing != nil {
		s.DiscoveredFrom = landing.ID
	}
	id, created, err := spawnChild(cp.ID, s)
	if err != nil || !created {
		return err
	}
	eventlog.RecordForgeReview(bd.Bead{ID: id, Kind: contract.KindForgeReview, Parent: cp.ID}, eventlog.ForgeReviewSpawned,
		append([]any{"mr", git.MRRef(lg.mrPath, mr.IID), "from", fromLabel(mark, hasLast), "notes", len(snap.judge)}, lg.attrs...)...)
	return nil
}

// awardDue awards 👀 on every note named in a file or fold ledger line of each closed
// Forge Review lacking Awarded: (Spec D5). It is graph-driven, not gated by the notes
// count. A 404 is a re-award and counts as done. Once every award succeeds the Forge
// Review gets Awarded: <n>; a failure emits forge_review.award_failed and is retried next
// tick, so one never blocks the rest of the arm. A Forge 429 is returned for the caller
// to hold, and stops the awards.
func awardDue(reviews []bd.Bead, lg legs, iid int) error {
	for i, r := range reviews {
		if r.Unclosed() || forgeReviewLine(r, "Awarded:") != "" {
			continue
		}
		var ids []int
		for _, line := range strings.Split(r.Notes, "\n") {
			note, verdict, ok := contract.LedgerLine(line)
			id, err := strconv.Atoi(note)
			if ok && verdict != "skip" && err == nil && !slices.Contains(ids, id) { // a skip earns no award
				ids = append(ids, id)
			}
		}
		failed := false
		for _, id := range ids {
			err := forgeAward(lg.proj, iid, id, "eyes")
			if errors.Is(err, forge.ErrRateLimited) {
				return err
			}
			if err != nil && !errors.Is(err, forge.ErrNotFound) {
				eventlog.RecordForgeReview(r, eventlog.ForgeReviewAwardFailed, append([]any{"note", id, "err", err}, lg.attrs...)...)
				failed = true
			}
		}
		if failed || len(ids) == 0 {
			continue // nothing to award leaves nothing to stop
		}
		body := setBodyLine(r.Description, "Awarded: ", "Awarded: "+strconv.Itoa(len(ids)))
		if err := updateBody(r.ID, []string{"--description", body}); err != nil {
			slog.Warn("forge_review.award_write_failed", append(eventlog.BeadAttrs(r), "err", err)...)
			continue
		}
		reviews[i].Description = body // so the arm's later mark write keeps the line
	}
	return nil
}

func fromLabel(mark int, has bool) string {
	if !has {
		return "none"
	}
	return strconv.Itoa(mark)
}

// newestClosedReview is the newest successfully closed Forge Review (one carrying a mark).
func newestClosedReview(reviews []bd.Bead) (bd.Bead, bool) {
	var best bd.Bead
	found := false
	for _, r := range reviews {
		if r.Unclosed() || forgeReviewLine(r, "Read through note:") == "" {
			continue
		}
		if !found || r.Created.After(best.Created) {
			best, found = r, true
		}
	}
	return best, found
}

func forgeReviewLine(b bd.Bead, key string) string {
	v, _ := git.ParseLine(b.Description, key)
	return v
}

// advanceMark records an all-skipped read on the newest closed Forge Review as its
// Read through note: / Notes count: pair, so the next tick's gate holds. With no closed
// Forge Review there is nothing to advance and the next tick reads again.
func advanceMark(last bd.Bead, ok bool, through, count int) error {
	if !ok {
		return nil
	}
	body := setBodyLine(last.Description, "Read through note: ", "Read through note: "+strconv.Itoa(through))
	body = setBodyLine(body, "Notes count: ", "Notes count: "+strconv.Itoa(count))
	if body == last.Description {
		return nil
	}
	if err := updateBody(last.ID, []string{"--description", body}); err != nil {
		slog.Warn("forge_review.mark_write_failed", append(eventlog.BeadAttrs(last), "err", err)...)
		return nil // the next tick reads ungated and retries
	}
	slog.Debug("forge_review.skipped", append(eventlog.BeadAttrs(last), "through", through)...)
	return nil
}

// snapshot is one discussions read, filtered: the discussions that hold a note to
// judge, with the per-note status.
type snapshot struct {
	discussions []snapDiscussion
	judge       []int     // ids of the notes to judge
	through     int       // the highest unacked non-system note id, skipped ones included
	newest      time.Time // creation of the newest note to judge
}

type snapDiscussion struct {
	d     forge.Discussion
	judge map[int]bool
}

// readSnapshot applies the mechanical pre-filter (Spec D2) to discussions' notes above
// mark: system notes, notes carrying Jinbe's reply marker and notes on resolved threads
// are skipped, except an unmarked note newer than a Jinbe marker in its discussion.
// Bots are never skipped.
func readSnapshot(discussions []forge.Discussion, mark int) snapshot {
	var s snapshot
	for _, d := range discussions {
		sd := snapDiscussion{d: d, judge: map[int]bool{}}
		markerSeen := false
		for _, n := range d.Notes {
			marked := anyReplyMarker(n.Body)
			if n.System {
				continue
			}
			if marked {
				markerSeen = true
			}
			if n.ID <= mark {
				continue
			}
			s.through = max(s.through, n.ID)
			if marked || (d.Resolved && !markerSeen) {
				continue
			}
			sd.judge[n.ID] = true
			s.judge = append(s.judge, n.ID)
			if n.CreatedAt.After(s.newest) {
				s.newest = n.CreatedAt
			}
		}
		if len(sd.judge) > 0 {
			s.discussions = append(s.discussions, sd)
		}
	}
	return s
}

// body renders the Forge Review body (Spec D2): the key lines, then the snapshot in
// tags, never markdown headings. Notes appear as they read now.
func (s snapshot) body(mr *forge.MR, mrPath string, mark int, hasLast bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nRead from note: %s\nRead through note: %d\nNotes count: %d\n\n", git.MRLine(mrPath, mr.IID), fromLabel(mark, hasLast), s.through, mr.UserNotesCount)
	fmt.Fprintf(&b, "MR title: %s\nMR author: %s\nMR link: %s\n\n", oneLine(mr.Title), mr.Author.Username, mr.WebURL)
	for _, sd := range s.discussions {
		fmt.Fprintf(&b, "<discussion id=\"%s\" kind=\"%s\">\n", sd.d.ID, strings.ReplaceAll(discussionKind(sd.d), `"`, "'"))
		for _, n := range sd.d.Notes {
			if n.System {
				continue
			}
			status := "context, read earlier"
			if sd.judge[n.ID] {
				status = "JUDGE"
			}
			by := n.Author.Username
			if isBot(n) {
				by += " bot"
			}
			if mr.Author.Username != "" && n.Author.Username == mr.Author.Username {
				by += " MR author"
			}
			fmt.Fprintf(&b, "<note id=\"%d\" by=\"%s\" at=\"%s\" status=\"%s\">\n%s\n</note>\n", n.ID, by, n.CreatedAt.UTC().Format(time.RFC3339), status, noteBody(n.Body))
		}
		b.WriteString("</discussion>\n")
	}
	return b.String()
}

func isBot(n forge.Note) bool {
	return n.Author.Bot || strings.HasSuffix(strings.ToLower(n.Author.Username), "bot")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func noteBody(s string) string {
	return tagInjection.ReplaceAllString(strings.TrimRight(strings.ReplaceAll(s, "\r", ""), "\n"), "&lt;$1$2")
}

func discussionKind(d forge.Discussion) string {
	if d.IndividualNote {
		return "plain comment"
	}
	if len(d.Notes) > 0 && d.Notes[0].Position != nil {
		pos := d.Notes[0].Position
		path, line := cmp.Or(pos.NewPath, pos.OldPath), pos.NewLine
		if line == 0 { // removed line: only old_line is set
			line = pos.OldLine
		}
		if pos.HeadSHA != "" {
			return fmt.Sprintf("diff thread on %s:%d @ %s", path, line, pos.HeadSHA)
		}
		return fmt.Sprintf("diff thread on %s:%d", path, line)
	}
	return "thread"
}
