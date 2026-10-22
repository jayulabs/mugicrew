package jinbe

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
)

// The container body lines the TUI's d reads for pending close-outs (ADR 0320 (a-forge-review-surfaces…)), and
// the filed bead's notes line that records a settled discussion.
const (
	closeOutsDuePrefix     = contract.CloseOutsDuePrefix
	closeOutsFailingPrefix = contract.CloseOutsFailingPrefix
	closedOutPrefix        = "Closed out: "
)

// Seams of the close-out pass.
var (
	hasCommit    = git.Repo.HasCommit
	commitSubj   = git.Repo.CommitSubject
	fetchCommit  = git.Repo.FetchCommit
	isReachable  = git.Repo.IsAncestor
	signalsFrom  = bd.SignalsFrom
	carriersFrom = bd.CarriersFrom
	pivotByID    = bd.Show
	appendNotes  = func(id, line string) error { return updateBody(id, []string{"--append-notes", line}) }
	graphID      = git.GraphID
)

// closeOutTarget is what the Forge holds of a container: head is the commit a fix must be
// reachable from (the MR's pushed head, or once merged the commit it landed: the squash's
// Tip: when it has one). Under a merged target a bead whose commit head lacks is stranded,
// carried by the stranded-commits Signal landing filed. final marks a swept container's
// late pass (ADR 0343 (an-mr-watch-closes-once-nothing-is-owed…)): no push can come, so every closed bead is due at
// once, and it is never merged. watch is set on a late pass over a closed container.
type closeOutTarget struct {
	head    string
	merged  bool
	final   bool
	landing bd.Bead
	watch   bd.Bead // the MR Watch carrying the close-out lines once the container is closed; zero while it is open
}

// sourcesOf reads b's Source lines for MR iid, from its body and its notes (a fold
// appends to the notes), as the discussions they name, once each, in order.
func sourcesOf(b bd.Bead, iid int) []string {
	var out []string
	for _, src := range contract.SourcesOf(b.Description + "\n" + b.Notes) {
		if src.IID == iid && !slices.Contains(out, src.Discussion) {
			out = append(out, src.Discussion)
		}
	}
	return out
}

// closedOut is whether b's notes record discussion d as settled.
func closedOut(b bd.Bead, d string) bool {
	return bd.HasBodyLine(b.Notes, closedOutPrefix+d)
}

// closeOutResult is a pass's tally for the container's d: closed beads waiting on a push,
// (bead, discussion) close-outs that failed with the first error, and a Forge 429 to hold.
type closeOutResult struct {
	awaiting, failing int
	err               error
	rateLimited       error
	lastErr           map[string]string // bead id → its last failure this pass
}

// fail records one failed close-out as forge_review.close_out_failed (Warn).
func (r *closeOutResult) fail(cp, b bd.Bead, discussion string, err error) {
	r.failing++
	if r.err == nil {
		r.err = err
	}
	if r.lastErr == nil {
		r.lastErr = map[string]string{}
	}
	r.lastErr[b.ID] = oneLine(err.Error())
	if errors.Is(err, forge.ErrRateLimited) && r.rateLimited == nil {
		r.rateLimited = err
	}
	eventlog.RecordForgeReview(b, eventlog.ForgeReviewCloseOutFailed,
		"discussion", discussion, "err", err.Error())
}

// dueBead is a closed filed bead whose close-out is due: sha8 is its integrate commit
// ("" with no fix), signal the stranded-commits Signal's title when the merged commit
// lacks that commit, swept the parsed reason of a Critique a pivot sweep closed.
type dueBead struct {
	b      bd.Bead
	sha8   string
	signal string
	swept  *bd.SweptReason
}

// closeOutArm closes out the container cp's filed beads against its MR mr, as target says
// the Forge holds it (Spec D5, ADR 0319 (a-filed-comment-is-closed-out…)): a bead closed with no integrate commit is due at
// once, one with a commit once target.head reaches it, and under a merged target one the
// head lacks is stranded. Each due (bead, discussion) gets one reply, in ADR 0200's
// template, and a discussion is resolved once every bead fed by its notes has its reply.
// The reply marker is the idempotency key. A Forge 429 is returned to be held; any other
// Forge failure is recorded (close_out_failed) and retried on the next pass, and under a
// merged target (the final pass) is also returned, so the container does not close past it.
// Once the container is closed (target.watch) nothing holds it, so the pass records its
// lines on the Watch (ADR 0345 (an-outliving-mr-watch-carries-its-close-out-lines…)) and returns only a 429.
func closeOutArm(cp bd.Bead, lg legs, mr *forge.MR, branch string, target closeOutTarget) error {
	_, err := closeOutPass(cp, lg, mr, branch, target)
	return err
}

// closeOutPass is closeOutArm that also hands back the pass's tally.
func closeOutPass(cp bd.Bead, lg legs, mr *forge.MR, branch string, target closeOutTarget) (closeOutResult, error) {
	var res closeOutResult
	kids, err := childrenOfKind(cp.ID, contract.FiledKind(cp.Kind))
	if err != nil {
		return res, err
	}
	filed := slices.DeleteFunc(slices.Clone(kids), func(k bd.Bead) bool { return len(sourcesOf(k, mr.IID)) == 0 })
	var due []dueBead
	for _, b := range filed {
		if b.Unclosed() || !slices.ContainsFunc(sourcesOf(b, mr.IID), func(d string) bool { return !closedOut(b, d) }) {
			continue
		}
		x, wait, err := dueOf(cp, b, lg, branch, target)
		switch {
		case err != nil:
			res.fail(cp, b, "", err)
		case wait:
			res.awaiting++
		default:
			due = append(due, x)
		}
	}
	if len(due) > 0 {
		closeOutDue(cp, lg, mr, filed, due, &res)
	}
	switch {
	case target.watch.ID != "":
		recordCloseOuts(target.watch, res)
	case !target.merged && !target.final:
		recordCloseOuts(cp, res)
	case res.rateLimited == nil:
		return res, res.err // the final pass: nothing reads the container again, so a failure holds it open to retry
	}
	return res, res.rateLimited
}

// dueOf decides a closed bead b: wait is true while its integrate commit is not yet on
// the Forge.
func dueOf(cp, b bd.Bead, lg legs, branch string, target closeOutTarget) (x dueBead, wait bool, _ error) {
	if strings.HasPrefix(b.CloseReason, bd.ContainerLandedReason) { // its container landed: due at once, whatever the head
		x = dueBead{b: b}
		sigs, err := signalsFrom(b.ID)
		if err != nil {
			return x, false, err
		}
		carried := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(b.CloseReason, bd.ContainerLandedReason), "; carried by"))
		for _, s := range sigs { // the stranded-commits Signal the close reason names, not an offshoot filed meanwhile
			if s.ID == carried {
				x.signal = s.Title
				break
			}
		}
		return x, false, nil
	}
	if sw, ok := bd.ParseSweptReason(b.CloseReason); ok { // swept: due at once, no sha, whatever the head reaches
		return dueBead{b: b, swept: &sw}, false, nil
	}
	var sha string
	if !target.final || hasBranch(lg.repo, branch) { // a swept branch may be gone: closed with no fix
		var err error
		if sha, err = newestByTrailer(lg.repo, branch, git.TrailerFor(string(b.Kind), b.ID)); err != nil {
			return x, false, err
		}
	}
	x = dueBead{b: b, sha8: sha[:min(8, len(sha))]}
	if target.final { // nothing more will be pushed: due at once
		return x, false, nil
	}
	if sha == "" { // closed with no fix: due at its close
		return x, false, nil
	}
	if reached, err := reaches(lg.repo, sha, target.head); err != nil || reached {
		return x, false, err
	}
	if !target.merged {
		return x, true, nil
	}
	sigs, err := signalsFrom(target.landing.ID)
	if err != nil {
		return x, false, err
	}
	for _, s := range sigs {
		if s.Title == nami.StalePinTitle(cp) {
			x.signal = s.Title
		}
	}
	return x, false, nil // with no stranded-commits Signal the merge carried it in some other shape
}

// reaches is whether commit sha is reachable from head, which is fetched first when the
// clone lacks it (another clone pushed it). An empty head reaches nothing.
func reaches(repo git.Repo, sha, head string) (bool, error) {
	if head == "" {
		return false, nil
	}
	if !hasCommit(repo, head) {
		if err := fetchCommit(repo, head); err != nil {
			return false, err
		}
	}
	return isReachable(repo, sha, head), nil
}

// closeOutDue replies on and resolves the discussions of the due beads, one discussion at
// a time.
func closeOutDue(cp bd.Bead, lg legs, mr *forge.MR, filed []bd.Bead, due []dueBead, res *closeOutResult) {
	gid, err := graphID()
	var name string
	if err == nil {
		name, err = currentUser()
	}
	var discussions []forge.Discussion
	if err == nil {
		discussions, err = forgeDiscussions(lg.proj, mr.IID)
	}
	if err != nil {
		for _, x := range due {
			res.fail(cp, x.b, "", err)
		}
		return
	}
	var order []string // the discussions the due beads name, once each
	for _, x := range due {
		for _, d := range sourcesOf(x.b, mr.IID) {
			if !closedOut(x.b, d) && !slices.Contains(order, d) {
				order = append(order, d)
			}
		}
	}
	for _, id := range order {
		i := slices.IndexFunc(discussions, func(d forge.Discussion) bool { return d.ID == id })
		if i < 0 { // the thread is gone from the Forge: nothing to answer, so settle it
			for _, x := range due {
				if slices.Contains(sourcesOf(x.b, mr.IID), id) && !closedOut(x.b, id) {
					if err := appendNotes(x.b.ID, closedOutPrefix+id); err != nil {
						slog.Warn("forge_review.ledger_write_failed", append(eventlog.BeadAttrs(x.b), "err", err)...)
					}
				}
			}
			continue
		}
		var fed []bd.Bead
		for _, f := range filed {
			if slices.Contains(sourcesOf(f, mr.IID), id) {
				fed = append(fed, f)
			}
		}
		here := slices.DeleteFunc(slices.Clone(due), func(x dueBead) bool {
			return !slices.Contains(sourcesOf(x.b, mr.IID), id) || closedOut(x.b, id)
		})
		closeOutDiscussion(cp, lg, mr, discussions[i], fed, here, gid, name, res)
	}
}

// closeOutDiscussion posts the due beads' replies on d, then resolves it once every bead
// in fed has its reply and no human has written since. A bead is recorded settled on d
// only when its reply stands and the thread is resolved, or a human has since replied
// (then the Forge Review reads that reply).
func closeOutDiscussion(cp bd.Bead, lg legs, mr *forge.MR, d forge.Discussion, fed []bd.Bead, due []dueBead, gid, name string, res *closeOutResult) {
	has := map[string]bool{}
	for _, f := range fed {
		for _, n := range d.Notes {
			if _, forF := replyMarked(n.Body, f.ID, gid); forF {
				has[f.ID] = true
			}
		}
	}
	posted := map[string]bool{}
	for _, x := range due {
		if has[x.b.ID] {
			continue
		}
		var body string
		if x.signal != "" {
			body = renderStranded(x.b, gid, name, x.signal)
		} else if x.swept != nil {
			carriers, err := successorTitles(x.b, x.swept.Successor)
			if err != nil {
				res.fail(cp, x.b, d.ID, err)
				return // no reply with a wrong carrier
			}
			v, why := sweptReply(*x.swept, len(carriers) > 0)
			if v != verdictDeferred {
				carriers = nil
			}
			body = renderReply(x.b, gid, name, v, "", why, carriers)
		} else {
			v, why := closeVerdict(x.b, x.sha8)
			if why == "" && x.sha8 != "" {
				if subj, err := commitSubj(lg.repo, x.sha8); err == nil {
					why = subj
				}
			}
			var carriers []string
			if v == verdictDeferred {
				var err error
				if carriers, err = carrierTitles(x.b); err != nil {
					res.fail(cp, x.b, d.ID, err)
					return // no reply with a wrong carrier
				}
			}
			body = renderReply(x.b, gid, name, v, x.sha8, why, carriers)
		}
		if err := forgeReply(lg.proj, mr.IID, d.ID, body); err != nil {
			res.fail(cp, x.b, d.ID, err)
			return // no resolve without the reply
		}
		has[x.b.ID], posted[x.b.ID] = true, true
	}
	// a plain comment turns into a resolvable thread once replied to, and an unresolved
	// one blocks merging: it resolves after any reply, whatever its siblings and humans say
	plainReplied := d.IndividualNote && slices.ContainsFunc(fed, func(f bd.Bead) bool { return has[f.ID] })
	if plainReplied || !slices.ContainsFunc(fed, func(f bd.Bead) bool { return !has[f.ID] }) { // every bead fed by d is out
		resolved := d.Resolved
		if !resolved && (plainReplied || len(posted) > 0 || !humanSince(d.Notes, fed, gid)) {
			if err := forgeResolve(lg.proj, mr.IID, d.ID); err != nil {
				for _, x := range due {
					res.fail(cp, x.b, d.ID, err)
				}
				return
			}
			resolved = true
		}
		for _, x := range due {
			if posted[x.b.ID] || resolved != d.Resolved {
				eventlog.RecordForgeReview(x.b, eventlog.ForgeReviewClosedOut,
					"mr", git.MRRef(lg.mrPath, mr.IID), "discussion", d.ID, "resolved", resolved)
			}
			if err := appendNotes(x.b.ID, closedOutPrefix+d.ID); err != nil {
				slog.Warn("forge_review.ledger_write_failed", append(eventlog.BeadAttrs(x.b), "err", err)...)
			}
		}
		return
	}
	for _, x := range due {
		if posted[x.b.ID] {
			eventlog.RecordForgeReview(x.b, eventlog.ForgeReviewClosedOut,
				"mr", git.MRRef(lg.mrPath, mr.IID), "discussion", d.ID, "resolved", false)
		}
	}
}

// carrierTitles are the titles of what tracks the Deferred ask of b (ADR 0339 (a-close-out-reply-reads-its-verdict…)):
// the pivot named by a pivoted: reason's first id, read at any status, else the Signals,
// Specs and Charts discovered-from b, at any status. A pivot that resolves to nothing names none.
func carrierTitles(b bd.Bead) ([]string, error) {
	_, _, ids, _ := parseVerdict(b.CloseReason)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(b.CloseReason)), "pivoted:") {
		id := strings.FieldsFunc(ids, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
		if len(id) == 0 {
			return nil, nil
		}
		p, err := pivotByID(id[0])
		if bd.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []string{p.Title}, nil
	}
	sigs, err := carriersFrom(b.ID)
	if err != nil {
		return nil, err
	}
	titles := make([]string, len(sigs))
	for i, s := range sigs {
		titles[i] = s.Title
	}
	return titles, nil
}

// successorTitles is the title of the swept reason's successor when it is discovered-from
// b at any status (Drafts included), else none: an unrelated Signal, Spec or Chart
// discovered-from b is not what carries the ask (ADR 0344 (a-swept-critique-is-deferred-only…)).
func successorTitles(b bd.Bead, successor string) ([]string, error) {
	cs, err := carriersFrom(b.ID)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if c.ID == successor {
			return []string{c.Title}, nil
		}
	}
	return nil, nil
}

// humanSince is whether a non-system note that carries no reply marker follows the last
// reply any bead of fed has in notes.
func humanSince(notes []forge.Note, fed []bd.Bead, gid string) bool {
	last := -1
	for i, n := range notes {
		for _, f := range fed {
			if _, forF := replyMarked(n.Body, f.ID, gid); forF {
				last = i
			}
		}
	}
	for _, n := range notes[last+1:] {
		if !n.System && !anyReplyMarker(n.Body) {
			return true
		}
	}
	return false
}

// recordCloseOuts writes the close-out lines the TUI's d shows on b, the container or its
// closed container's MR Watch: how many
// closed beads await a push, and how many close-outs fail, with the first error. A line
// with a zero count is dropped. A failed write only warns; the next pass rewrites it.
func recordCloseOuts(b bd.Bead, r closeOutResult) {
	due, fail := "", ""
	if r.awaiting > 0 {
		due = fmt.Sprintf("%s%d", closeOutsDuePrefix, r.awaiting)
	}
	if r.failing > 0 {
		fail = fmt.Sprintf("%s%d: %s", closeOutsFailingPrefix, r.failing, oneLine(r.err.Error()))
	}
	have := func(prefix string) string {
		v, _ := git.ParseLine(b.Description, strings.TrimSpace(prefix))
		if v == "" {
			return ""
		}
		return prefix + v
	}
	if have(closeOutsDuePrefix) == due && have(closeOutsFailingPrefix) == fail {
		return
	}
	writeMarker(b, func(body string) string {
		for _, l := range [][2]string{{closeOutsDuePrefix, due}, {closeOutsFailingPrefix, fail}} {
			if l[1] == "" {
				body = bd.RemoveBodyLine(body, l[0])
			} else {
				body = setBodyLine(body, l[0], l[1])
			}
		}
		return body
	})
}
