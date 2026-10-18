package jinbe

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// threadLine is the identity line of a filed review-thread bead: the MR iid and the
// full discussion id, which survives a force-push.
func threadLine(iid int, discussion string) string {
	return fmt.Sprintf("Thread: !%d %s", iid, discussion)
}

// threadBead renders the title and body of the bead a review thread files, one shape
// for both kinds: the key lines first, then every non-system note quoted with "> " so
// a human line such as "Thread: …" can never be the first match for a key. tip is the
// Landing's Commit: pin, stamped as Tip: for a Critique only ("" omits it).
func threadBead(mr *forge.MR, d forge.Discussion, tip string) (title, body string) {
	first := d.Notes[0]
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nThread note: %d\nNote: %s#note_%d\n", threadLine(mr.IID, d.ID), d.Notes[len(d.Notes)-1].ID, mr.WebURL, first.ID)
	if pos := first.Position; pos != nil {
		path, line := pos.NewPath, pos.NewLine
		if line == 0 { // removed line: GitLab still sets new_path, but only old_line
			path, line = cmp.Or(pos.OldPath, pos.NewPath), pos.OldLine
		}
		fmt.Fprintf(&b, "Position: %s:%d @ %s\n", path, line, pos.HeadSHA)
	}
	if tip != "" {
		b.WriteString(git.TipPin(tip) + "\n")
	}
	b.WriteString("\n")
	for _, n := range d.Notes {
		if n.System {
			continue
		}
		for i, line := range strings.Split(strings.TrimRight(strings.ReplaceAll(n.Body, "\r", ""), "\n"), "\n") {
			if i == 0 {
				line = fmt.Sprintf("**@%s**: %s", n.Author.Username, line)
			}
			b.WriteString("> " + line + "\n")
		}
	}
	for _, line := range strings.Split(first.Body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line, b.String()
		}
	}
	return fmt.Sprintf("review thread on !%d", mr.IID), b.String()
}

// botCache remembers GitLab's bot flag per author id: a user's type does not change.
var botCache sync.Map

func isBot(p forge.Project, id int) (bool, error) {
	if v, ok := botCache.Load(id); ok {
		return v.(bool), nil
	}
	bot, err := forgeIsBot(p, id)
	if err == nil {
		botCache.Store(id, bot)
	}
	return bot, err
}

// graphName is forge.CurrentUser, cached per process: the account does not change.
var graphName struct {
	sync.Mutex
	name string
}

func currentUser() (string, error) {
	graphName.Lock()
	defer graphName.Unlock()
	if graphName.name == "" {
		name, err := forgeUser()
		if err != nil {
			return "", err
		}
		graphName.name = name
	}
	return graphName.name, nil
}

const replyMarker = "<!-- mugicrew-reply: "

// replyMarkerLine is the hidden last line of Jinbe's reply: the Qualified Bead Id the
// reply answers (ADR 0200).
func replyMarkerLine(beadID, graphID, name string) string {
	return fmt.Sprintf("%s%s@%s %s -->", replyMarker, beadID, graphID, name)
}

// replyMarked is whether the note carries any mugicrew-reply marker line (never a human
// reply), and forB whether one is for exactly this Qualified Bead Id (id and graph
// hash; the trailing name is ignored).
func replyMarked(body, beadID, graphID string) (marked, forB bool) {
	for _, line := range strings.Split(body, "\n") {
		inner, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), replyMarker)
		if !ok {
			continue
		}
		inner, ok = strings.CutSuffix(inner, " -->")
		if !ok {
			continue
		}
		marked = true
		if q, _, _ := strings.Cut(inner, " "); q == beadID+"@"+graphID {
			forB = true
		}
	}
	return marked, forB
}

// renderReply is Jinbe's reply on a thread whose bead b closed (ADR 0200 verbatim);
// sha8 "" omits " in <sha8>". The reason is the close reason's first line, cut at 200
// characters with "…".
func renderReply(b bd.Bead, graphID, name, sha8 string) string {
	reason, _, _ := strings.Cut(b.CloseReason, "\n")
	if r := []rune(reason); len(r) > 200 {
		reason = string(r[:200]) + "…"
	}
	in := ""
	if sha8 != "" {
		in = " in " + sha8
	}
	return fmt.Sprintf("🤖 mugicrew: addressed by **%s** (%s@%s)%s: %s\n%s", b.Title, b.ID, name, in, reason, replyMarkerLine(b.ID, graphID, name))
}

// closeLegAction is what a thread whose beads are all closed is due (Spec D4), given
// threadAnchor's reading of it and whether the thread is resolved. A new reply is the
// reopen leg's, so it is due nothing here.
func closeLegAction(marker, newReply, resolved bool) (reply, resolve bool) {
	if newReply {
		return false, false
	}
	return !marker, !resolved
}

// threadAnchor reads a thread against its newest closed member b: whether any note
// carries b's marker, and whether a new reply (non-system, unmarked) follows the anchor:
// the last note marked for b, else b's Thread note:, else (deleted) the starter note.
func threadAnchor(notes []forge.Note, b bd.Bead, graphID string) (marker, newReply bool) {
	anchorNote, _ := strconv.Atoi(threadNote(b.Description))
	anchor := 0
	for i, n := range notes {
		if _, forB := replyMarked(n.Body, b.ID, graphID); forB {
			anchor, marker = i, true
		} else if !marker && n.ID == anchorNote {
			anchor = i
		}
	}
	for _, n := range notes[anchor+1:] {
		if marked, _ := replyMarked(n.Body, b.ID, graphID); !n.System && !marked {
			return marker, true
		}
	}
	return marker, false
}

func newestMember(members []bd.Bead) bd.Bead {
	return slices.MaxFunc(members, func(x, y bd.Bead) int { return x.Created.Compare(y.Created) })
}

func threadNote(body string) string {
	v, _ := git.ParseLine(body, "Thread note:")
	return v
}

// closeLeg posts the reply and resolves the thread d, whose newest closed bead is b,
// as closeLegAction says of marker and newReply. A 403/404 on either is recorded and
// skipped; the rest, a 429 included, is returned.
func closeLeg(l bd.Bead, lg legs, mr *forge.MR, d forge.Discussion, b bd.Bead, branch, graphID string, marker, newReply bool) error {
	reply, resolve := closeLegAction(marker, newReply, d.Resolved)
	if reply {
		name, err := currentUser()
		if err != nil {
			return err
		}
		sha, err := newestByTrailer(lg.repo, branch, git.TrailerFor(string(b.Kind), b.ID))
		if err != nil {
			return err
		}
		if err := forgeReply(lg.proj, mr.IID, d.ID, renderReply(b, graphID, name, sha[:min(8, len(sha))])); err != nil {
			return skipForbidden(l, lg, mr, d, err) // skipped: no resolve without the reply
		}
		landOutcome(lg, l, eventlog.LandThreadReplied, mr.IID, "discussion", d.ID)
	}
	if resolve {
		if err := forgeResolve(lg.proj, mr.IID, d.ID); err != nil {
			return skipForbidden(l, lg, mr, d, err)
		}
		landOutcome(lg, l, eventlog.LandThreadResolved, mr.IID, "discussion", d.ID)
	}
	return nil
}

// skipForbidden records a 403/404 on a thread's reply or resolve and moves on (retried on
// the next ungated read); any other error is returned.
func skipForbidden(l bd.Bead, lg legs, mr *forge.MR, d forge.Discussion, err error) error {
	if !errors.Is(err, forge.ErrForbidden) && !errors.Is(err, forge.ErrNotFound) {
		return err
	}
	landOutcome(lg, l, eventlog.LandThreadSkipped, mr.IID, "discussion", d.ID, "err", err.Error())
	return nil
}

// threadArm is mirrorMR's first opened arm (Spec D6): each review thread of the MR
// (resolvable, human-started) is keyed by its Thread: line among the container's
// children of its kind. No bead: an unresolved human thread is filed as a published
// child, a Critique under a Checkpoint or a grilling under a Chart, DiscoveredFrom the
// Landing; filing anything records review_comment and closes the Landing obsolete, the
// MR left open. An unclosed bead: a no-op. All closed: the close leg replies on the
// thread and resolves it. A 429 is returned, held by reconcileLanding.
func threadArm(l, cp bd.Bead, lg legs, mr *forge.MR, branch string) (bool, error) {
	if seen, ok := notesSeen.Load(l.ID); ok && seen == mr.UserNotesCount {
		return false, nil // a quiet tick: no new comment, so no new thread (Spec FR 22)
	}
	done, err := fileThreads(l, cp, lg, mr, branch)
	if !done && err == nil { // record only a complete read, so a failed filing is retried
		if _, seen := notesSeen.Swap(l.ID, mr.UserNotesCount); !seen {
			pruneClosed(&notesSeen)
		}
	}
	return done, err
}

// notesSeen is the MR's user notes count at each Landing's last discussions read, in
// process memory: a first tick (or a loop restart) reads ungated. Edits and Forge-side
// resolves don't move the count and need no action.
var notesSeen sync.Map

// fileThreads files the MR's new threads and closes the Landing for them. A step that
// fails after a thread was filed still records and closes for those, joining the error.
func fileThreads(l, cp bd.Bead, lg legs, mr *forge.MR, branch string) (bool, error) {
	filed, err := fileNew(l, cp, lg, mr, branch)
	if len(filed) == 0 {
		return err != nil, err
	}
	landOutcome(lg, l, eventlog.LandReviewComment, mr.IID, "discussions", strings.Join(filed, ","))
	return true, errors.Join(err, closeIfOpen(l, "obsolete: review comment"))
}

// fileNew walks the MR's threads: the close leg for those whose beads closed, a new bead
// for the rest. It returns the discussion ids it filed, with the error that stopped it.
func fileNew(l, cp bd.Bead, lg legs, mr *forge.MR, branch string) (filed []string, err error) {
	discussions, err := forgeDiscussions(lg.proj, mr.IID)
	if err != nil {
		return nil, err
	}
	kind, tip := contract.KindGrilling, ""
	if cp.Kind == contract.KindCheckpoint {
		kind = contract.KindCritique
		tip, _ = git.ParseCommit(l.Description)
	}
	var kids []bd.Bead
	var read bool
	for _, d := range discussions {
		if !d.Resolvable || d.IndividualNote || len(d.Notes) == 0 || d.Notes[0].System {
			continue
		}
		if !read { // only a candidate thread costs the bd read
			if kids, err = childrenOfKind(cp.ID, kind); err != nil {
				return filed, err
			}
			read = true
		}
		key := threadLine(mr.IID, d.ID)
		members := slices.DeleteFunc(slices.Clone(kids), func(k bd.Bead) bool { return !hasBodyLine(k.Description, key) })
		if len(members) > 0 {
			if slices.ContainsFunc(members, bd.Bead.Unclosed) {
				continue
			}
			b := newestMember(members)
			graphID, err := git.GraphID()
			if err != nil {
				return filed, err
			}
			marker, newReply := threadAnchor(d.Notes, b, graphID)
			if !newReply || d.Resolved {
				if err := closeLeg(l, lg, mr, d, b, branch, graphID, marker, newReply); err != nil {
					return filed, err
				}
				continue
			}
			// "that didn't fix it": a fresh bead below, no reply or resolve for b
		}
		if d.Resolved {
			continue
		}
		if bot, err := isBot(lg.proj, d.Notes[0].Author.ID); err != nil {
			return filed, err
		} else if bot {
			continue
		}
		title, body := threadBead(mr, d, tip)
		if _, err := spawnThreadBead(cp.ID, bd.Successor{Kind: kind, Title: title, Description: body, DiscoveredFrom: l.ID}); err != nil {
			return filed, err
		}
		filed = append(filed, d.ID)
	}
	return filed, nil
}
