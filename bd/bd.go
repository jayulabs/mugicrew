// Package bd is the single shell-out choke point for bd CLI access (spec §3.1).
// Today it files a Signal; reads, edges, and spawnSuccessorIfAbsent
// (§4.3) grow here. Footguns live in this package, never at call
// sites.
package bd

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"syscall"

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

// newSignalArgs is the pure argv for filing a Signal, split out so the §3.1
// footgun (--no-inherit-labels on every runtime create) is checkable without a
// DB. A Signal is a chain head — no --parent — so it has nothing to inherit
// from; the flag is set unconditionally anyway, so the one-kind-routing
// invariant holds by construction across every create, not by remembering which
// ones have a parent.
func newSignalArgs(title, description string) []string {
	return unparentedArgs(contract.KindSignal, title, description)
}

// unparentedArgs is the argv for an unparented bead of kind: a Signal (chain head)
// or a Rollback (leaf, ADR 0047 §3), --no-inherit-labels set as on every create.
// The title is kind-prefix stripped (ADR 0097); with no parent, the event names the kind.
func unparentedArgs(kind contract.Kind, title, description string) []string {
	return []string{
		"create",
		"--type", "task",
		"--labels", "kind:" + string(kind),
		"--no-inherit-labels",
		"--title", stripTitle(title, "kind", string(kind)),
		"--description", description,
		"--silent", // print only the new ID
	}
}

// NewSignal files a Signal as a chain head (spec §7.1): a kind:signal task with
// no parent. Returns the new bead ID.
func NewSignal(title, description string) (string, error) {
	return run(newSignalArgs(title, description)...)
}

// NewRollback files a human Rollback of a landed Checkpoint (ADR 0047 §3): an
// unparented kind:rollback task with one related edge to targetID. Returns the
// new bead ID; on an edge failure the ID too, so the caller can name the orphan.
func NewRollback(title, body, targetID string) (string, error) {
	id, err := run(unparentedArgs(contract.KindRollback, title, body)...)
	if err != nil {
		return "", err
	}
	_, err = run("dep", "add", id, targetID, "-t", "related")
	return id, err
}

// cutBranch is the cut-at-SHA helper Jinbe's handOff uses; a var so a test fakes it.
var cutBranch = git.CutBranch

// NewSurgery files a human Surgery (Spec D2, ADR 0091): an unparented
// kind:checkpoint container with no caused-by edge (so SpecOf is not-found), its
// checkpoint/<id> branch cut at main HEAD's SHA with no anchor, then the
// kind:surgery leaf as its child, claimed so it starts idle and never enters bd
// ready. A human create, not a Draft. On a mid-sequence failure the ids already
// created come back too, so the caller can name the orphan (mirrors NewRollback).
func NewSurgery(title, problem string) (surgeryID, checkpointID string, err error) {
	cp, err := run("create", "--type", "epic", "--labels", "kind:"+string(contract.KindCheckpoint),
		"--no-inherit-labels", "--title", stripTitle(title, "kind", string(contract.KindCheckpoint)), "--description", "", "--silent")
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
// container, with their bodies — the closed-Approval read Jinbe's land gate needs:
// a closed Approval IS the approved state (Luffy closes it on the human
// OK; declining leaves it open), so its Commit: pin is what git.ApprovedAtHead compares
// to the branch tip. Returning ALL (not the first) matters: after a
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

// SurgeryOf is the Surgery behind a surgery Checkpoint: its kind:surgery child,
// closed included, since an ended Surgery is still the intent review reads (Spec
// D6). Membership is the only edge read. A Checkpoint with no Surgery
// returns ("", false, nil).
func SurgeryOf(checkpointID string) (string, bool, error) {
	kids, err := childBeads(checkpointID)
	if err != nil {
		return "", false, err
	}
	for _, k := range kids {
		if k.Kind == contract.KindSurgery {
			return k.ID, true, nil
		}
	}
	return "", false, nil
}

// Pause causes (ADR 0055 §2): each is its own additive paused:<cause> label, so
// a bead may carry several (ADR 0070 §3). Only foreign-hold clears itself; failing
// is lifted by the human or a successful run-now (ADR 0078); manual is the human's;
// abandoned (a force stop or quit left an effort behind) only the human lifts (ADR 0082).
const (
	PausedForeignHold = "foreign-hold"
	PausedFailing     = "failing"
	PausedManual      = "manual"
	PausedAbandoned   = "abandoned"
	// PausedIntegrateConflict: the backstop's integrate hit a conflict (rebase, held,
	// landed); only a successful mugicrew integrate lifts it (ADR 0102 §6, ADR 0104).
	PausedIntegrateConflict = "integrate-conflict"
)

const pausedPrefix = "paused:"

// PauseRank is every known cause, highest first (ADR 0078, 0082, 0104 §4): it
// ranks PausedCauses and is the list the TUI's pause colours must cover.
var PauseRank = []string{PausedManual, PausedAbandoned, PausedIntegrateConflict, PausedFailing, PausedForeignHold}

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
// paused:manual, "" when none. Only manual cascades and caused-by is never walked.
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
		if PausedBy(anc, PausedManual) {
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
