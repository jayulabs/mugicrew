// Package bd is the single shell-out choke point for bd CLI access (spec §3.1).
// Today it files a Signal; reads, edges, and spawnSuccessorIfAbsent
// (§4.3) grow here. Footguns live in this package, never at call
// sites.
package bd

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// run shells out to the bd CLI and returns trimmed stdout. Every bd call routes
// through here, so the §3.1 footguns are encapsulated in one place. A var so a
// test can fake the CLI and assert the argv a read sends.
var run = func(args ...string) (string, error) { return runIn("", args...) }

// runIn is run from dir ("" for this process's cwd). bd -C ignores an ambient
// BEADS_DIR, so a call that must resolve from a dir sets the cwd instead.
func runIn(dir string, args ...string) (string, error) {
	return retrying(func() (string, error) { return runOnce(dir, args...) })
}

// retrying runs once up to maxAttempts times, backing off between retryable failures.
func retrying(once func() (string, error)) (string, error) {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var out string
		if out, err = once(); err == nil {
			return out, nil
		}
		if attempt == maxAttempts-1 || !retryable(err) {
			break
		}
		sleep(backoff(attempt))
	}
	return "", err
}

// runOnce is one bd exec; a var so a test fakes the process under the retry.
var runOnce = func(dir string, args ...string) (string, error) { return execBd(dir, args...) }

// sqlRows runs q against the graph and returns its CSV rows below the header.
func sqlRows(q string) ([][]string, error) {
	out, err := run("sql", "--csv", q)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[1:], nil
}

func execBd(dir string, args ...string) (string, error) {
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // a loop's Ctrl-C drains; it must not kill an effort's bd write (ADR 0050)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("bd %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("bd %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	git.SetBdReads(readGraphRoot, func() (string, error) {
		out, err := run("config", "get", "issue_prefix")
		if err != nil {
			return "", fmt.Errorf("bd: read issue_prefix: %w", err)
		}
		return out, nil
	})
}

// graphRootSQL is the Bead Graph's identity: its Dolt root commit hash, which
// survives a re-clone and differs between graphs sharing an issue_prefix
// (docs/research/stable-per-graph-identity-for-the-graph-trailer.md).
const graphRootSQL = "SELECT commit_hash FROM dolt_commit_ancestors WHERE parent_hash IS NULL"

// readGraphRoot is the Bead Graph's Dolt root commit hash, exactly one.
func readGraphRoot() (string, error) {
	out, err := run("sql", "--csv", graphRootSQL)
	if err != nil {
		return "", fmt.Errorf("bd: read Bead Graph root: %w", err)
	}
	rows := strings.Fields(out) // header, then one hash per root commit
	if len(rows) != 2 {
		return "", fmt.Errorf("bd: returned %d Dolt root commits, want exactly 1: %q", max(len(rows)-1, 0), out)
	}
	return rows[1], nil
}

// newSignalArgs is the pure argv for filing a Signal, split out so the §3.1
// footgun (--no-inherit-labels on every runtime create) is checkable without a
// DB. A Signal is a chain head — no --parent — so it has nothing to inherit
// from; the flag is set unconditionally anyway, so the one-kind-routing
// invariant holds by construction across every create, not by remembering which
// ones have a parent.
func newSignalArgs(title, description string) []string {
	return unparentedArgs(Successor{Kind: contract.KindSignal}, title, description)
}

// unparentedArgs is the argv for an unparented bead of kind: a Signal (chain head),
// a Rollback (leaf, ADR 0047 §3) or an Initiative (epic, s.Container), --no-inherit-labels set as on every create.
// The title is kind-prefix stripped (ADR 0097); with no parent, the event names the kind.
func unparentedArgs(s Successor, title, description string) []string {
	typ := "task"
	if s.Container {
		typ = "epic"
	}
	args := append([]string{"create", "--type", typ}, labelArgs(s)...)
	return append(append(args, externalRefArgs(s)...),
		"--no-inherit-labels",
		"--title", stripTitle(title, "kind", string(s.Kind)),
		"--description", description,
		"--silent", // print only the new ID
	)
}

// NewSignal files a Signal as a chain head (spec §7.1): a kind:signal task with
// no parent. Returns the new bead ID.
func NewSignal(title, description string) (string, error) {
	return run(newSignalArgs(title, description)...)
}

// NewOffshoot files an offshoot Signal as draftedBy's Draft in one atomic create (ADR 0267):
// discovered-from cause, plus edge ("blocked-by:<id>" or "blocks:<id>") when ordered, and
// born a direct child of the Initiative parent when set (ADR 0348 §6, §7).
func NewOffshoot(title, body, draftedBy, cause, edge, parent string) (string, error) {
	deps := "discovered-from:" + cause
	if edge != "" {
		deps += "," + edge
	}
	if parent == "" {
		args := unparentedArgs(Successor{Kind: contract.KindSignal, DraftedBy: draftedBy}, title, body)
		return run(append(args, "--deps", deps)...)
	}
	s := Successor{Kind: contract.KindSignal, DraftedBy: draftedBy, Parent: parent, Title: stripTitle(title, "kind", string(contract.KindSignal)), Description: body}
	return run(successorCreateArgs("", s, deps)...)
}

// NewSignalWithRef is NewSignal carrying an external ref (a jira-<KEY> intake).
func NewSignalWithRef(title, description, ref string) (string, error) {
	return run(unparentedArgs(Successor{Kind: contract.KindSignal, ExternalRef: ref}, title, description)...)
}

// NewDiscussion files a Robin discussion (ADR 0194 §5): an open, unparented
// kind:discussion task, a human action so not a Draft. Returns the new bead ID.
func NewDiscussion(title, description string) (string, error) {
	return run(unparentedArgs(Successor{Kind: contract.KindDiscussion}, title, description)...)
}

// NewInitiative starts an Initiative from the Initiatives tab: an open, unparented
// kind:initiative epic with no predecessor and its seed text verbatim as the body.
// Priority is the caller's follow-up update, as for a Signal.
func NewInitiative(title, body, ref string) (string, error) {
	id, err := run(unparentedArgs(Successor{Kind: contract.KindInitiative, Container: true, ExternalRef: ref}, title, body)...)
	if err == nil {
		slog.Info("initiative.new", "bead", id, "kind", "initiative")
	}
	return id, err
}

// NewRollback files a human Rollback of a landed Checkpoint (ADR 0047 §3): an
// unparented kind:rollback task with one related edge to target. Returns the
// new bead ID; on an edge failure the ID too, so the caller can name the orphan.
//
// A nested target's context: and repo: labels are copied onto the Rollback, so the
// tick scan can hold it paused:context on drift (ADR 0158, 0167). An unstamped
// target adds no label. The caller passes the target it already loaded, so no read
// here can fail the human's action.
func NewRollback(title, body string, target Bead) (string, error) {
	return newRelated(Successor{Kind: contract.KindRollback}, title, body, target)
}

// newRelated files an unparented s bead carrying target's context and repo stamp,
// then a related edge to target; on an edge failure the id comes back too.
func newRelated(s Successor, title, body string, target Bead) (string, error) {
	if dir := RepoStampOf(target); dir != "" && ContextOf(target) != "" {
		s.Context, s.Repo = ContextOf(target), dir
	}
	id, err := run(unparentedArgs(s, title, body)...)
	if err != nil {
		return "", err
	}
	_, err = run("dep", "add", id, target.ID, "-t", "related")
	return id, err
}

// NewCheckup files a human Checkup on target (ADR 0272 §2), shaped like
// NewRollback: an unparented kind:checkup task, not a Draft, whose body names the
// target and carries problem when given, then a related edge to target.
func NewCheckup(target Bead, problem string) (string, error) {
	body := fmt.Sprintf("Target: %s (%s)", target.ID, target.Title)
	if problem != "" {
		body += "\n\nProblem: " + problem
	}
	return newRelated(Successor{Kind: contract.KindCheckup}, "Repair "+target.Title, body, target)
}

// OpenCheckupOn is target's open Checkup, if any (ADR 0272 §2: at most one): an
// unclosed kind:checkup bead with a related edge to target.
func OpenCheckupOn(target string) (Bead, bool, error) {
	open, err := OpenSet()
	if err != nil {
		return Bead{}, false, err
	}
	b, ok := openCheckupIn(open, target)
	return b, ok, nil
}

func openCheckupIn(beads []Bead, target string) (Bead, bool) {
	for _, b := range beads {
		if b.Kind == contract.KindCheckup && b.Open() && slices.Contains(b.Related, target) {
			return b, true
		}
	}
	return Bead{}, false
}

// cutBranch is the cut-at-SHA helper Jinbe's handOff uses; a var so a test fakes it.
var cutBranch = git.CutBranch

// NewSurgery files a human Surgery (Spec D2, ADR 0091): an unparented
// kind:checkpoint container with no caused-by edge (so SpecOf is not-found), its
// checkpoint/<id> branch cut at main HEAD's SHA with no anchor, then the
// kind:surgery leaf as its child, claimed so it starts idle and never enters bd
// ready. A human create, not a Draft. On a mid-sequence failure the ids already
// created come back too, so the caller can name the orphan (mirrors NewRollback).
// A non-empty initiative must be an open Initiative and files the Checkpoint as
// its member (--parent, still no caused-by edge); required (Require Initiatives)
// refuses an empty one. Both are checked before any create.
func NewSurgery(title, problem, initiative string, required bool) (surgeryID, checkpointID string, err error) {
	if initiative == "" && required {
		return "", "", errors.New("surgery new: Require Initiatives is on — name an open Initiative (--initiative <id>)")
	}
	if initiative != "" {
		if err := requireOpenInitiative("surgery new", initiative); err != nil {
			return "", "", err
		}
	}
	args := []string{"create", "--type", "epic", "--labels", "kind:" + string(contract.KindCheckpoint), "--no-inherit-labels"}
	if initiative != "" {
		args = append(args, "--parent", initiative)
	}
	cp, err := run(append(args, "--title", stripTitle(title, "kind", string(contract.KindCheckpoint)), "--description", "", "--silent")...)
	if err != nil {
		return "", "", err
	}
	head, err := git.Head(git.Mainline())
	if err != nil {
		return "", cp, err
	}
	if _, err := cutBranch(git.BranchName(string(contract.KindCheckpoint), cp), head); err != nil {
		return "", cp, err
	}
	sg, err := SpawnChild(cp, Successor{Kind: contract.KindSurgery, Title: title, Description: problem})
	if err != nil {
		return "", cp, err
	}
	return sg, cp, Claim(sg)
}

// CarrierOf returns the live bead (open or Draft; `bd list` omits closed and
// superseded) carrying external ref ref: the kind:initiative bead first, else the
// kind:signal bead, never by result order (ADR 0006). ok is false when none does.
func CarrierOf(ref string) (b Bead, ok bool, err error) {
	out, err := run("list", "--external-ref", ref, "--json")
	if err != nil {
		return Bead{}, false, err
	}
	beads, err := parseBeads([]byte(out))
	if err != nil {
		return Bead{}, false, err
	}
	for _, kind := range []contract.Kind{contract.KindInitiative, contract.KindSignal} {
		for _, c := range beads {
			if c.Kind == kind && c.ExternalRef == ref {
				return c, true, nil
			}
		}
	}
	return Bead{}, false, nil
}

// InitiativesByRef returns every kind:initiative bead, in any status, carrying
// external ref ref exactly (case-sensitive, as bd matches). Not CarrierOf, which
// skips closed beads; Enforce external ID's uniqueness spans closed Initiatives too.
func InitiativesByRef(ref string) ([]Bead, error) {
	out, err := run("list", "--external-ref", ref, "--all", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	beads, err := parseBeads([]byte(out))
	if err != nil {
		return nil, err
	}
	var hits []Bead
	for _, b := range beads {
		if b.Kind == contract.KindInitiative && b.ExternalRef == ref {
			hits = append(hits, b)
		}
	}
	return hits, nil
}

// LatchOf returns the live (open or Draft) kind:latch bead carrying external ref ref.
func LatchOf(ref string) (b Bead, ok bool, err error) {
	out, err := run("list", "--external-ref", ref, "--json")
	if err != nil {
		return Bead{}, false, err
	}
	beads, err := parseBeads([]byte(out))
	if err != nil {
		return Bead{}, false, err
	}
	for _, c := range beads {
		if c.Kind == contract.KindLatch && c.ExternalRef == ref {
			return c, true, nil
		}
	}
	return Bead{}, false, nil
}

// NewLatch files an unparented kind:latch carrying ref; a non-empty draftedBy
// files it as that bead's Draft.
func NewLatch(title, body, ref, draftedBy string) (string, error) {
	return run(unparentedArgs(Successor{Kind: contract.KindLatch, ExternalRef: ref, DraftedBy: draftedBy}, title, body)...)
}

// ListAll returns every bead including closed lineage (`bd list --all`, spec
// §9.1: the Graph tab shows all kinds and states). `-n 0` lifts the default
// 50-row cap so the whole graph comes back.
func ListAll() ([]Bead, error) {
	out, err := run("list", "--all", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// defaultIDsSQL is the TUI's default id set as one flat UNION (no CTEs: Dolt
// fails them): open beads, closed members/Landings/Approvals of open Charts,
// Checkpoints and Initiatives, closed Initiatives and their members, finished
// Charts within 24 h with their children and caused-by Specs, and landed:
// Landings within 24 h with their closed Checkpoints. It reads bd's internal
// tables, so column names are the table names (depends_on_issue_id), not the
// list JSON's. The first arm is status!='closed' rather than OpenSet's four statuses so
// any other non-closed status (bd's pinned, hooked) still loads. The window clock is Dolt's; tabs re-apply their own over it.
const defaultIDsSQL = `select id from issues where status!='closed'
union select d.issue_id from dependencies d join issues p on p.id=d.depends_on_issue_id join labels l on l.issue_id=p.id
 where d.type='parent-child' and ((p.status!='closed' and l.label in ('kind:chart','kind:checkpoint','kind:initiative'))
  or (p.status='closed' and l.label='kind:initiative')
  or (p.status='closed' and l.label='kind:chart' and p.issue_type!='event' and p.closed_at>=utc_timestamp()-interval 24 hour))
union select p.id from issues p join labels l on l.issue_id=p.id where p.status='closed' and (l.label='kind:initiative'
  or (l.label='kind:chart' and p.issue_type!='event' and p.closed_at>=utc_timestamp()-interval 24 hour))
union select d.issue_id from dependencies d join issues p on p.id=d.depends_on_issue_id join labels l on l.issue_id=p.id
 where d.type='caused-by' and p.status='closed' and l.label='kind:chart' and p.issue_type!='event' and p.closed_at>=utc_timestamp()-interval 24 hour
union select i.id from issues i join labels l on l.issue_id=i.id where i.status='closed' and l.label='kind:landing'
  and i.close_reason like 'landed: %' and i.closed_at>=utc_timestamp()-interval 24 hour
union select d.depends_on_issue_id from dependencies d join issues i on i.id=d.issue_id join labels l on l.issue_id=i.id
  join issues p on p.id=d.depends_on_issue_id join labels lp on lp.issue_id=p.id
 where d.type='parent-child' and i.status='closed' and l.label='kind:landing' and i.close_reason like 'landed: %'
  and i.closed_at>=utc_timestamp()-interval 24 hour and lp.label='kind:checkpoint' and p.status='closed'
union select p.id from dependencies d join issues w on w.id=d.issue_id join labels l on l.issue_id=w.id join issues p on p.id=d.depends_on_issue_id
 where d.type='parent-child' and l.label='kind:mr-watch' and w.status!='closed' and p.status='closed'`

// DefaultIDs is the ids of the default set (ADR 0232): the first of the slow
// tier's two bd calls, ListByID the second.
func DefaultIDs() ([]string, error) {
	rows, err := sqlRows(defaultIDsSQL)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r[0])
	}
	return ids, nil
}

// ListByID fetches the beads with the given ids in the list row shape. Always
// --all (without it closed ids silently return nothing), never --brief
// (Approvals' commit and detail panes read Description); an id bd does not
// have is absent from the result (ADR 0232).
func ListByID(ids []string) ([]Bead, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := run("list", "--all", "-n", "0", "--id", strings.Join(ids, ","), "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// SearchClosed finds the closed beads whose title and id contain every word of
// words: `bd search` on the longest word (a multi-word bd search is a phrase
// match, and bd query cannot match inside an id), the remaining words matched
// client-side, then the hits fetched in full by id since search rows lack parent
// and dependencies (ADR 0232). Longest-first is a heuristic: it keeps the first
// read small.
func SearchClosed(words string) ([]Bead, error) {
	ws := strings.Fields(strings.ToLower(words))
	if len(ws) == 0 {
		return nil, nil
	}
	longest := slices.MaxFunc(ws, func(a, b string) int { return len(a) - len(b) })
	out, err := run("search", longest, "-s", "closed", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	rows, err := parseBeads([]byte(out))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, b := range rows {
		text := strings.ToLower(b.Title + " " + b.ID)
		if b.Status == "closed" && !slices.ContainsFunc(ws, func(w string) bool { return !strings.Contains(text, w) }) {
			ids = append(ids, b.ID)
		}
	}
	return ListByID(ids)
}

// ListWithAncestors fetches id and its ancestors: one ListByID with id plus its
// dotted prefixes, then parent followed for any still-missing ancestor, since a
// parent need not be the dotted prefix (ADR 0232). Not `bd show`: its dependency
// rows parse with empty BlockedBy/Related/CausedBy. A bead bd lacks is absent.
func ListWithAncestors(id string) ([]Bead, error) {
	want := []string{id}
	for p := id; strings.Contains(p, "."); {
		p = p[:strings.LastIndex(p, ".")]
		want = append(want, p)
	}
	asked := map[string]bool{}
	var out []Bead
	for len(want) > 0 {
		for _, w := range want {
			asked[w] = true
		}
		got, err := ListByID(want)
		if err != nil {
			return nil, err
		}
		out, want = append(out, got...), nil
		for _, b := range got {
			if b.Parent != "" && !asked[b.Parent] {
				asked[b.Parent] = true
				want = append(want, b.Parent)
			}
		}
	}
	return out, nil
}

// ListChildren fetches the beads whose parent is id, closed ones included.
func ListChildren(id string) ([]Bead, error) {
	out, err := run("list", "--all", "-n", "0", "--parent", id, "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// DependentIDs returns the ids of the beads that point at id by any edge (blocks,
// caused-by, successor…): `bd dep list --direction=up`. The rows are thin, so the
// caller fetches full rows with ListByID.
func DependentIDs(id string) ([]string, error) {
	rows, err := depTargets(id, "up", "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, b := range rows {
		ids[i] = b.ID
	}
	return ids, nil
}

// OpenSet returns the beads that are not closed (`bd list --status
// open,in_progress,blocked,deferred`) — the TUI's fast-tier read (ADR 0197).
// Not OpenList, which omits blocked and feeds the loop's frontier.
func OpenSet() ([]Bead, error) {
	out, err := run("list", "--status", "open,in_progress,blocked,deferred", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// BuildChildren counts a Checkpoint's kind:build members across all statuses —
// the artifact-existence check reconcileSpec gates the Spec close on (FR22, ADR
// 0015): the slicing fork lays the Builds itself, so a fork that returned nil
// while laying zero is not done. Reuses childBeads (includes closed) so a Build
// already built still counts. slicing.md lays `kind:build` and this is where
// Zoro's side reads it back — one const (contract.KindBuild) now names both.
func BuildChildren(id string) (int, error) {
	kids, err := childBeads(id)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range kids {
		if k.Kind == contract.KindBuild {
			n++
		}
	}
	return n, nil
}

// OpenChildrenOfKind counts a container's still-open children of one kind — the
// open-tail-of-kind gate a Checkpoint reconcile reads, combining
// OpenChildren's open filter with BuildChildren's kind filter. countOpenKind is
// split out so the filter is checkable without a DB.
func OpenChildrenOfKind(id string, kind contract.Kind) (int, error) {
	kids, err := childBeads(id)
	if err != nil {
		return 0, err
	}
	return countOpenKind(kids, kind), nil
}

func countOpenKind(kids []Bead, kind contract.Kind) int {
	n := 0
	for _, k := range kids {
		if k.Open() && k.Kind == kind {
			n++
		}
	}
	return n
}

// OpenChildrenOfKindBeads returns a container's still-open children of one kind
// WITH their bodies — the read the stale-Approval obsoletion needs:
// approveIfClean must inspect each open Approval's Commit: pin to obsolete the ones
// an advanced HEAD has stranded. Mirrors OpenChildrenOfKind (the count twin) and
// ClosedChildrenOfKindBeads (the closed twin); openChildrenOfKind is split out so the
// filter is checkable without a DB.
func OpenChildrenOfKindBeads(id string, kind contract.Kind) ([]Bead, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, err
	}
	return openChildrenOfKind(kids, kind), nil
}

func openChildrenOfKind(kids []Bead, kind contract.Kind) []Bead {
	var out []Bead
	for _, k := range kids {
		if k.Open() && k.Kind == kind {
			out = append(out, k)
		}
	}
	return out
}

// ClosedChildrenOfKindBeads returns ALL closed children of the given kind under a
// container, with their bodies — the closed-Approval read Jinbe's land gate needs.
// Closed is not approved: callers keep only Bead.ApprovedClosed (Luffy closes
// approved on the human OK; declining leaves it open; rework and Usopp's
// obsoletion close otherwise), then compare the Commit: pin git.ApprovedAtHead
// reads to the branch tip. Returning ALL (not the first) matters: after a
// stale-Approval self-heal a Checkpoint has >=2 closed Approvals — the stale
// obsoleted one (created first) and the real at-head one — and a first-match read hits
// the stale pin and strands the land. Mirrors OpenChildrenOfKindBeads (the open twin);
// closedChildrenOfKind is split out so the filter is checkable without a DB.
func ClosedChildrenOfKindBeads(id string, kind contract.Kind) ([]Bead, error) {
	kids, err := childBeads(id)
	if err != nil {
		return nil, err
	}
	return closedChildrenOfKind(kids, kind), nil
}

func closedChildrenOfKind(kids []Bead, kind contract.Kind) []Bead {
	var out []Bead
	for _, k := range kids {
		if k.Kind == kind && !k.Open() {
			out = append(out, k)
		}
	}
	return out
}

// SurgeriesOf is every kind:surgery child of a Checkpoint, closed and abandoned
// included, in filing order. bd's children order is not pinned, so it sorts by
// creation time (stable, so equal stamps keep bd's order). Other kinds are ignored.
func SurgeriesOf(checkpointID string) ([]Bead, error) {
	kids, err := childBeads(checkpointID)
	if err != nil {
		return nil, err
	}
	var out []Bead
	for _, k := range kids {
		if k.Kind == contract.KindSurgery {
			out = append(out, k)
		}
	}
	slices.SortStableFunc(out, func(a, b Bead) int { return a.Created.Compare(b.Created) })
	return out, nil
}

// Pause causes (ADR 0055 §2): each is its own additive paused:<cause> label, so
// a bead may carry several (ADR 0070 §3). Only foreign-hold and profile clear
// themselves; failing is lifted by the human or a successful run-now (ADR 0078);
// manual is the human's; abandoned (a force stop or quit left an effort behind) only the human lifts (ADR 0082).
const (
	PausedForeignHold = "foreign-hold"
	PausedFailing     = "failing"
	PausedManual      = "manual"
	PausedAbandoned   = "abandoned"
	// PausedProfile: the bead's kind resolves to an unrunnable Agent Profile; the
	// loop lifts it once the profile runs again (ADR 0123 §3).
	PausedProfile = "profile"
	// PausedContext: the container's context: label names an undeclared context; the
	// loop lifts it once the label or the config is fixed (ADR 0158). Cascades like manual.
	PausedContext = "context"
	// PausedIntegrateConflict: the backstop's integrate hit a conflict (rebase, held,
	// landed); only a successful mugicrew integrate lifts it (ADR 0102 §6, ADR 0104).
	PausedIntegrateConflict = "integrate-conflict"
	// PausedNotBuilt: a Build's backstop integrated but found no integrate trailer
	// for it on its Checkpoint; a re-run that integrates lifts it (ADR 0248).
	PausedNotBuilt = "not-built"
	// PausedRecovering: recover's fence while it repairs a stranded bead; left
	// behind only by a repair that failed partway, lifted by re-running it (ADR 0127 §6).
	PausedRecovering = "recovering"
	// PausedCheckup: a Checkup's fence on its target while the human repairs it with
	// Chopper; lifted by the Checkup's close, drop, or recover (ADR 0272 §3).
	PausedCheckup = "checkup"
)

const pausedPrefix = "paused:"

// PauseRank is every known cause, highest first (ADR 0078, 0082, 0104 §4, 0123
// §3, 0127 §6, 0248): it ranks PausedCauses and is the list the TUI's pause colours must cover.
var PauseRank = []string{PausedManual, PausedAbandoned, PausedRecovering, PausedCheckup, PausedIntegrateConflict, PausedNotBuilt, PausedFailing, PausedContext, PausedProfile, PausedForeignHold}

// PauseNote is the pause line's words for cause on b: the cause itself, except
// paused:context, which names the undeclared context and the three fixes (ADR 0158).
func PauseNote(b Bead, cause string) string {
	if cause != PausedContext {
		return cause
	}
	return fmt.Sprintf("context (%q undeclared — relabel it, remove the label, or declare it in .mugicrew/config.toml)", ContextOf(b))
}

// PausedCauses are the bead's pause causes off its own labels, ranked by
// PauseRank, unknown causes alongside foreign-hold in label order, nil when
// unpaused — no extra bd call on the hot path.
func PausedCauses(b Bead) []string {
	var out []string
	for _, l := range b.Labels {
		if cause, ok := strings.CutPrefix(l, pausedPrefix); ok {
			out = append(out, cause)
		}
	}
	rank := func(c string) int {
		if i := slices.Index(PauseRank, c); i >= 0 {
			return i
		}
		return len(PauseRank) - 1 // unknown ties with the last, kept in label order
	}
	slices.SortStableFunc(out, func(a, b string) int { return rank(a) - rank(b) })
	return out
}

// Paused reports whether the bead carries any paused:* label.
func Paused(b Bead) bool {
	return slices.ContainsFunc(b.Labels, func(l string) bool { return strings.HasPrefix(l, pausedPrefix) })
}

// PausedBy reports whether the bead carries paused:<cause>, whatever else it carries.
func PausedBy(b Bead, cause string) bool { return b.HasLabel(pausedPrefix + cause) }

// PausedAncestor is the inherited-pause predicate (ADR 0081, Spec B), one
// for loop candidacy and TUI run-now parity: it walks b's membership chain
// (Bead.Parent) upward, at any depth, and returns the nearest ancestor carrying
// paused:manual or paused:context, "" when none. Only those cascade and caused-by is never walked.
// lookup resolves an id to its bead, and its error is returned; a zero Bead (no
// parent) ends the walk, as does a parent cycle.
func PausedAncestor(b Bead, lookup func(id string) (Bead, error)) (string, error) {
	seen := map[string]bool{b.ID: true}
	for id := b.Parent; id != "" && !seen[id]; {
		seen[id] = true
		anc, err := lookup(id)
		if err != nil {
			return "", err
		}
		if PausedBy(anc, PausedManual) || PausedBy(anc, PausedContext) {
			return id, nil
		}
		id = anc.Parent
	}
	return "", nil
}

// PausedList lists every paused bead, closed included, with its parent — the one
// extra read a loop tick makes so candidacy can see a paused ancestor (ADR 0081
// §7). Closed is in so a manual-paused ancestor is always in the list.
// ponytail: --all goes past Spec C — every closed paused bead is read each tick,
// and a closed manual ancestor still cascades — kept for parity with the TUI,
// whose ListAll snapshot sees closed ancestors too; drop --all if the read bites.
func PausedList() ([]Bead, error) {
	out, err := run("list", "--all", "--json", "-n", "0", "--label-pattern", pausedPrefix+"*")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

// Pause adds paused:<cause> (ADR 0070 §3): one commutative label add, so a loop
// foreign-hold racing a manual pause leaves both labels, a valid state.
func Pause(id, cause string) error {
	_, err := run("update", id, "--add-label", pausedPrefix+cause)
	return err
}

// AddLabel adds label to id: one commutative label add (ADR 0070 §3), as Pause
// is, for a marker a human removes with bd label remove.
func AddLabel(id, label string) error {
	_, err := run("update", id, "--add-label", label)
	return err
}

// NotBuiltNote is the body of id's newest "Not built:" comment, prefix and
// surrounding space trimmed; "" when none, or when the read fails (the pause it
// decorates must not wait on it).
func NotBuiltNote(id string) string {
	cs, err := Comments(id)
	if err != nil {
		return ""
	}
	return notBuiltFrom(cs)
}

// RationaleNote is the body of id's newest "Rationale:" comment, prefix and
// surrounding space trimmed; "" when none, or when the read fails (the close it
// decorates must not wait on it). Mirrors NotBuiltNote.
func RationaleNote(id string) string {
	cs, err := Comments(id)
	if err != nil {
		return ""
	}
	return rationaleFrom(cs)
}

// Comment is one `bd comments --json` element.
type Comment struct {
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	Created time.Time `json:"created_at"`
}

// Comments is id's comments, oldest first (bd's order).
func Comments(id string) ([]Comment, error) {
	out, err := run("comments", id, "--json")
	if err != nil {
		return nil, err
	}
	return parseComments([]byte(out))
}

// AddComment appends text as a comment on id.
func AddComment(id, text string) error {
	_, err := run("comments", "add", id, text)
	return err
}

func parseComments(js []byte) ([]Comment, error) {
	var cs []Comment
	if err := json.Unmarshal(js, &cs); err != nil {
		return nil, fmt.Errorf("bd comments: %w", err)
	}
	return cs, nil
}

func notBuiltFrom(cs []Comment) string { return newestNote(cs, "Not built:") }

func rationaleFrom(cs []Comment) string { return newestNote(cs, "Rationale:") }

// newestNote is the trimmed body of the newest comment starting with prefix.
func newestNote(cs []Comment, prefix string) string {
	for i := len(cs) - 1; i >= 0; i-- {
		if note, ok := strings.CutPrefix(strings.TrimSpace(cs[i].Text), prefix); ok {
			return strings.TrimSpace(note)
		}
	}
	return ""
}

// Reopen reopens a closed bead (`bd reopen`): the integrate backstop's undo of a
// close whose work never reached its container (Spec D6).
func Reopen(id string) error {
	_, err := run("reopen", id)
	return err
}

// Unpause removes only its own paused:<cause>; any other cause stays.
func Unpause(id, cause string) error {
	_, err := run("label", "remove", id, pausedPrefix+cause)
	return err
}

// Update runs `bd update <id> <args...>` — the TUI's edit escape hatch (spec
// §9.3: set any field via bd). The caller supplies the flags (--status,
// --priority, --assignee, --title, …); bd update covers close/reopen/unclaim
// through --status and --assignee, so no separate verbs are needed here. A
// --title X or --title=X value is kind-prefix stripped (ADR 0097); every other
// arg passes through untouched.
func Update(id string, args []string) error {
	args = slices.Clone(args)
	for i, a := range args {
		if a == "--title" && i+1 < len(args) {
			args[i+1] = stripTitle(args[i+1], "id", id)
		} else if v, ok := strings.CutPrefix(a, "--title="); ok {
			args[i] = "--title=" + stripTitle(v, "id", id)
		}
	}
	_, err := run(append([]string{"update", id}, args...)...)
	return err
}

// JoinRefusal is a refusal to file a Surgery onto an existing Checkpoint (Spec D2,
// ADR 0162 §3/§7). Error() is the bare text; the CLI prefixes "surgery new: ",
// so the TUI renders the same words.
type JoinRefusal string

func (r JoinRefusal) Error() string { return string(r) }

// The refusal texts after the Checkpoint id; Tag and Resumes read them back, so the
// wording lives in one place.
const (
	joinLanding    = " is landing — no surgery while a Landing is live"
	joinPivot      = " is frozen at a pivot gate — resolve the pivot first"
	joinConflict   = " has an open Conflict Critique rewriting its branch — wait for it to publish"
	joinBaseTip    = " has an open Base+Tip Critique rewriting its branch — wait for it to publish"
	joinHasSurgery = " already has open Surgery "
	joinClosed     = " is closed — file on a new Checkpoint instead"
)

// Tag is the refusal as a short label, "" for a refusal with none.
func (r JoinRefusal) Tag() string {
	switch s := string(r); {
	case strings.HasSuffix(s, joinLanding):
		return "landing"
	case strings.HasSuffix(s, joinPivot):
		return "pivot gate"
	case strings.HasSuffix(s, joinConflict), strings.HasSuffix(s, joinBaseTip):
		return "branch rewriting"
	}
	return ""
}

// Resumes reports whether r is the open-Surgery refusal, which carries the resume hint.
func (r JoinRefusal) Resumes() bool { return strings.Contains(string(r), joinHasSurgery) }

// JoinVerdict is the pure eligibility check behind NewSurgeryOn: the first
// applicable refusal for cp given its children and its open pivot gates, in the
// ADR 0162 order, or "" when a Surgery may join. resume is the caller's key hint
// for resuming the open Surgery ("l to resume" in the CLI, "enter to resume" in
// the TUI). The TUI runs the same function over its snapshot, so the two cannot
// drift.
func JoinVerdict(cp Bead, kids, pivots []Bead, resume string) JoinRefusal {
	for _, k := range kids {
		if k.Open() && k.Kind == contract.KindLanding {
			return JoinRefusal(cp.ID + joinLanding)
		}
	}
	if len(pivots) > 0 {
		return JoinRefusal(cp.ID + joinPivot)
	}
	for _, k := range kids {
		if !k.Open() {
			continue
		}
		_, based := git.ParseBase(k.Description)
		_, tipped := git.ParseTip(k.Description)
		switch {
		case k.Kind == contract.KindConflict:
			return JoinRefusal(cp.ID + joinConflict)
		case k.Kind == contract.KindCritique && based && tipped:
			return JoinRefusal(cp.ID + joinBaseTip)
		}
	}
	for _, k := range kids {
		if k.Open() && k.Kind == contract.KindSurgery {
			return JoinRefusal(cp.ID + joinHasSurgery + k.ID + " — " + resume)
		}
	}
	if !cp.Open() {
		return JoinRefusal(cp.ID + joinClosed)
	}
	return ""
}

// NewSurgeryOn files a Surgery as a child of the existing Checkpoint cpID and
// claims it (Spec D2): no Checkpoint create, no branch cut. Eligibility is
// re-checked against bd here (JoinVerdict), so a Landing or pivot newer than any
// TUI snapshot still refuses; a refusal is a JoinRefusal.
func NewSurgeryOn(cpID, title, problem string) (string, error) {
	cp, err := Show(cpID)
	if err != nil {
		if IsNotFound(err) {
			return "", JoinRefusal("no such bead " + cpID)
		}
		return "", err
	}
	if cp.Kind != contract.KindCheckpoint {
		return "", JoinRefusal(cpID + " is not a Checkpoint")
	}
	kids, err := childBeads(cpID)
	if err != nil {
		return "", err
	}
	pivots, err := OpenPivots(cpID)
	if err != nil {
		return "", err
	}
	if r := JoinVerdict(cp, kids, pivots, "l to resume"); r != "" {
		return "", r
	}
	sg, err := SpawnChild(cpID, Successor{Kind: contract.KindSurgery, Title: title, Description: problem})
	if err != nil {
		return "", err
	}
	return sg, Claim(sg)
}

// SpawnBlockingTask is SpawnBlockingTaskIfAbsent behind a seam for tests.
var SpawnBlockingTask = SpawnBlockingTaskIfAbsent

// HoldForeign is the answer to a trailer another Bead Graph landed (ADR 0187 §3):
// file one Brook-owned HITL Task blocking the container, idempotent on (container,
// foreign sha), and return the refusal so a Landing stays open. It is the one home
// of this land policy: Jinbe, Nami and the loop all call it; tests swap SpawnBlockingTask.
func HoldForeign(cp Bead, foreign *git.ForeignLanding) error {
	where, probe := "Mainline", "mugicrew probe foreign"
	if c := ContextOf(cp); RepoStampOf(cp) != "" && c != "" {
		where, probe = "Mainline of context "+c, "mugicrew probe foreign --context "+c
	}
	branch := git.BranchName(string(cp.Kind), cp.ID)
	body := TaskBody(
		fmt.Sprintf("%s holds %q at %s under another Bead Graph.", where, foreign.Trailer, foreign.Sha),
		[]string{"Foreign: " + foreign.Sha, "Graph (theirs): " + foreign.Theirs, "Graph (ours): " + foreign.Ours},
		[]TaskStep{{
			Title:   fmt.Sprintf("Close the merge request for %s.", branch),
			Markers: []string{"irreversible"},
			Do:      fmt.Sprintf("close the Forge merge request whose source branch is %s.", branch),
			Check:   fmt.Sprintf("`mugicrew probe mr --branch %s` prints `none`.", branch),
		}, {
			Title: "Stop the loop.",
			Do:    "run `mugicrew loop stop`.",
			Ask:   "is the loop stopped?",
		}, {
			Title: "Give this graph a prefix distinct from the other graph's.",
			Do: "after this Task closes, run `bd rename-prefix` to a prefix distinct from the other graph's (not a per-bead rename), " +
				"re-cut each open container branch under its new id, and restart the loop. " +
				"`" + probe + "` may be run first to confirm the foreign land.",
			Ask: "will you run the rename, the branch re-cuts and the loop restart after this Task closes?",
		}}, "")
	if _, _, err := SpawnBlockingTask(cp.ID, "Foreign: "+foreign.Sha+"\n", cp.Title+": mainline holds a foreign land of this bead", body); err != nil {
		return err
	}
	return foreign
}

// AnchorSeams are the caller's seams for Anchor.
type AnchorSeams struct {
	Strategy  func() (string, error)                 // the Workspace's land strategy name
	Show      func(string) (Bead, error)             // reads the Spec bead
	Update    func(id string, args []string) error   // bd.Update's shape
	Fetch     func() (string, error)                 // the Workspace's Forge main fetch
	RepoFetch func(git.Repo) (string, error)         // a child's Forge main fetch (Repo.FetchMainline)
	Landed    func(git.Repo, string) (string, error) // the legacy "Spec: <id>" landed-base read (Repo.LandedBase; "" = the Workspace)
	Tip       func(git.Repo) (string, error)         // r's local mainline tip, lock- and fetch-free (Repo.ResolveProbeBase)
}

// Anchor anchors a direct Spec in repo r ("" = the Workspace) under every land
// strategy: an Anchor: pin on the Spec bead. An existing pin is returned as is (no tip
// read, no Forge call). Else a legacy "Spec: <id>" trailer commit in r wins, returning
// its sha and writing no pin (only git.ErrNotLanded falls through). Else the tip is
// recorded as the pin: the fetched Forge tip under gitlab, Tip(r) under local. No
// commit lands on any mainline and no land lock is taken. Every dependency is the
// caller's seam; Luffy and Zoro share it so the two cannot drift.
func Anchor(s AnchorSeams, r git.Repo, specID, trailer string) (string, error) {
	name, err := s.Strategy()
	if err != nil {
		return "", err
	}
	tip := func() (string, error) { return s.Tip(r) }
	if name == git.LandStrategyGitlab {
		tip = s.Fetch
		if r != "" {
			tip = func() (string, error) { return s.RepoFetch(r) }
		}
	}
	return pinAnchor(s, r, tip, specID, trailer)
}

// pinAnchor is a direct Spec's anchor: its Anchor: pin when it has one, else a legacy
// trailer's sha, else tip's sha recorded as the pin through s.Update.
func pinAnchor(s AnchorSeams, r git.Repo, tip func() (string, error), specID, trailer string) (string, error) {
	b, err := s.Show(specID)
	if err != nil {
		return "", err
	}
	if sha, ok := git.ParseAnchor(b.Description); ok {
		return sha, nil
	}
	sha, err := s.Landed(r, trailer)
	if err == nil {
		return sha, nil
	}
	if !errors.Is(err, git.ErrNotLanded) {
		return "", err
	}
	if sha, err = tip(); err != nil {
		return "", err
	}
	_, err = SetLineIfUnset(s.Update, specID, b.Description, "Anchor:", git.AnchorPin(sha))
	return sha, err
}

// SetLineIfUnset appends line to the bead's body through update (bd.Update's shape,
// so callers keep their seam) unless body already carries a key line: the one
// set-if-unset body pin writer. It returns the body as it now stands.
func SetLineIfUnset(update func(id string, args []string) error, id, body, key, line string) (string, error) {
	if _, ok := git.ParseLine(body, key); ok {
		return body, nil
	}
	next := AppendBodyLine(body, line)
	return next, update(id, []string{"--description", next})
}

// AppendBodyLine is body with line appended: the one body-line appender.
func AppendBodyLine(body, line string) string {
	return strings.TrimRight(body, "\n") + "\n" + line + "\n"
}

// SetBodyLine is body with every line starting prefix replaced by line, appended when
// none was there.
func SetBodyLine(body, prefix, line string) string {
	return AppendBodyLine(RemoveBodyLine(body, prefix), line)
}

// RemoveBodyLine is body with every line starting prefix dropped, the rest untouched.
func RemoveBodyLine(body, prefix string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	lines = slices.DeleteFunc(lines, func(s string) bool { return strings.HasPrefix(s, prefix) })
	return strings.Join(lines, "\n") + "\n"
}

// HasBodyLine is whether the body holds line as a whole line, wherever it sits.
func HasBodyLine(body, line string) bool {
	return strings.Contains("\n"+body+"\n", "\n"+line+"\n")
}

// EditBody re-reads the bead through show and writes edit of its current body through
// update: for a write that must not undo what earlier arms of the tick wrote to a body
// the caller holds stale.
func EditBody(show func(string) (Bead, error), update func(id string, args []string) error, id string, edit func(body string) string) error {
	b, err := show(id)
	if err != nil {
		return err
	}
	return update(id, []string{"--description", edit(b.Description)})
}

// kindsSQL is every bead's kind label. One query reads the kind of beads no list holds
// (the Logs f filter names closed history, ADR 0232).
const kindsSQL = "SELECT issue_id, label FROM labels WHERE label LIKE 'kind:%'"

// Kinds maps every bead id to its kind, read with one bd sql over the labels table.
func Kinds() (map[string]contract.Kind, error) {
	out, err := run("sql", "--csv", kindsSQL)
	if err != nil {
		return nil, fmt.Errorf("bd: read kinds: %w", err)
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("bd: read kinds: %w", err)
	}
	ks := make(map[string]contract.Kind, len(rows))
	for _, r := range rows[min(1, len(rows)):] { // skip the header
		if len(r) == 2 {
			ks[r[0]] = contract.Kind(strings.TrimPrefix(r[1], "kind:"))
		}
	}
	return ks, nil
}

// RunIn is one bd call from dir, the choke point for verbs outside this package
// (mugicrew init) that need bd's raw argv.
func RunIn(dir string, args ...string) (string, error) { return runIn(dir, args...) }

// TaskChildren counts a Spec's kind:task dependents over discovered-from in every
// status (deferred, open, closed) — the Task-existence check reconcileSpec gates
// the slicing re-fork on. Not an open-only scan: a closed Task still counts, or
// the loop would re-fork over a Spec whose Tasks are done.
func TaskChildren(specID string) (int, error) {
	rows, err := depTargets(specID, "up", "discovered-from")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if r.Kind == contract.KindTask {
			n++
		}
	}
	return n, nil
}
