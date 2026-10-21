package luffy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// leg prefixes every `pivot raise` error and note.
const leg = "pivot raise"

// fallback is what every refusal of `pivot raise` tells Brook to do.
const fallback = "fall back to the offshoot Signal"

// heldUp lists the beads a Task blocks (its up blocks edges); a seam so the probe's
// tests script the Task's graph.
var heldUp = func(task string) ([]bd.Bead, error) { return bd.LiveHeldReads(nil).Up(task) }

// heldReads are bd.HeldReads over this package's seams: the same show, SpecOf and
// landed-by-trailer reads the reroute's CheckHost makes.
func heldReads() bd.HeldReads {
	return bd.HeldReads{
		Up:     heldUp,
		Show:   show,
		SpecOf: specOf,
		Landed: func(cp bd.Bead) (bool, error) {
			return HostLanded(cp, git.TrailerFor(string(contract.KindCheckpoint), cp.ID))
		},
	}
}

// RaiseRequest is a flagged `pivot raise`: the --pivot and --critique bodies and the
// --merge-into targets with their evidence, as given.
type RaiseRequest struct {
	Pivots    []string
	Critiques []string
	Merges    []RaiseMerge
}

// None is whether no body flag was given: a probe.
func (r RaiseRequest) None() bool {
	return len(r.Pivots)+len(r.Critiques)+len(r.Merges) == 0
}

// raiseTarget is what `pivot raise` resolves before it names or writes anything.
type raiseTarget struct {
	task bd.Bead
	host bd.Bead
	spec string
}

// raiseTask is the Task a raise is for: an open kind:task. Every refusal says to fall
// back to the offshoot Signal.
func raiseTask(taskID string) (bd.Bead, error) {
	task, err := show(taskID)
	if err != nil {
		return bd.Bead{}, err
	}
	switch {
	case task.Kind != contract.KindTask:
		return bd.Bead{}, refuseRaise(leg, "%s is kind %q, not a task", taskID, task.Kind)
	case !task.Open():
		return bd.Bead{}, refuseRaise(leg, "task %s is not open (status %q)", taskID, task.Status)
	}
	return task, nil
}

// resolveRaise is the held-Checkpoint resolver and the Landing refusal over an open Task
// (Spec FR 2–5). Its order: the resolver, then the Landing refusal.
func resolveRaise(task bd.Bead) (raiseTarget, error) {
	host, spec, err := bd.HeldCheckpoint(task.ID, heldReads())
	var several *bd.SeveralHeldError
	var noSpec *bd.NoSpecError
	switch {
	case errors.Is(err, bd.ErrNoHeldCheckpoint):
		return raiseTarget{}, refuseRaise(leg, "task %s %s (none is open, or its land is already on mainline)", task.ID, err)
	case errors.As(err, &several), errors.As(err, &noSpec):
		return raiseTarget{}, refuseRaise(leg, "task %s %s", task.ID, err)
	case err != nil:
		return raiseTarget{}, err
	}
	landings, err := childrenOfKind(host.ID, contract.KindLanding)
	if err != nil {
		return raiseTarget{}, err
	}
	if slices.ContainsFunc(landings, bd.Bead.Unclosed) {
		return raiseTarget{}, refuseRaise(leg, "checkpoint %s is landing; a raise never freezes code that is landing", host.ID)
	}
	return raiseTarget{task: task, host: host, spec: spec}, nil
}

// ProbeRaise is a flagless `mugicrew pivot raise <task>` (Spec FR 2–8): when the Task
// has raise Drafts it reports them and their Checkpoint; otherwise it names the one
// open, unlanded Checkpoint the Task holds, its Spec and its branch, with the MR warning
// and Surgery note ADR 0324 §3–4 give a reroute, or returns the reason a raise is
// refused. It writes nothing.
func ProbeRaise(taskID string, w io.Writer) error {
	task, err := raiseTask(taskID)
	if err != nil {
		return err
	}
	if drafts, err := raiseDrafts(task); err != nil {
		return err
	} else if len(drafts) > 0 {
		return reportRaise(task, drafts, w)
	}
	t, err := resolveRaise(task)
	if err != nil {
		return err
	}
	host := t.host
	fmt.Fprintf(w, "checkpoint: %s\nspec: %s\nbranch: %s\n", host.ID, t.spec, git.BranchName(string(contract.KindCheckpoint), host.ID))
	if r := repoOf(host); r != "" {
		fmt.Fprintf(w, "repo: %s (read the branch with git -C %s)\n", r, r)
	}
	for _, n := range hostNotes(host) {
		fmt.Fprintln(w, n)
	}
	return nil
}

// raiseDrafts is the Task's raise Drafts: its pivot Drafts carrying raise_checkpoint and
// its Critique Drafts.
func raiseDrafts(task bd.Bead) ([]bd.Bead, error) {
	drafts, err := draftsOf(task.ID)
	if err != nil {
		return nil, err
	}
	pivots, critiques := RaiseDrafts(task, drafts)
	return append(pivots, critiques...), nil
}

// reportRaise lists the raise Drafts a flagless run finds and the Checkpoint they name.
func reportRaise(task bd.Bead, drafts []bd.Bead, w io.Writer) error {
	var pivots, critiques []bd.Bead
	for _, d := range drafts {
		if d.Kind == contract.KindPivot {
			pivots = append(pivots, d)
		} else {
			critiques = append(critiques, d)
		}
	}
	hosts, err := RaiseHosts(pivots, critiques)
	if err != nil {
		return fmt.Errorf("pivot raise: task %s: %w", task.ID, err)
	}
	fmt.Fprintf(w, "task %s already has raise Drafts on checkpoint %s; drop the session to change the raise\n", task.ID, strings.Join(hosts, ", "))
	for _, d := range drafts {
		fmt.Fprintf(w, "%s: %s %s\n", d.Kind, d.ID, d.Title)
	}
	return nil
}

// checkRaiseShape is the three shapes Spec FR 10 accepts: one --critique alone; one
// --pivot with one --critique; one --merge-into with one --critique. Anything else, and
// an empty body, is refused before any write.
func checkRaiseShape(r RaiseRequest) error {
	switch {
	case len(r.Pivots)+len(r.Merges) > 1:
		return errors.New("one raise files one pivot or one --merge-into, not several")
	case len(r.Critiques) > 1:
		return errors.New("one raise files one --critique")
	case len(r.Critiques) == 0:
		return errors.New("a --pivot or --merge-into needs a --critique carrying the Ask")
	}
	for _, b := range append(slices.Clone(r.Pivots), r.Critiques...) {
		if strings.TrimSpace(b) == "" {
			return errors.New("empty pivot or Critique body")
		}
	}
	return checkRerouteBodies(nil, r.Merges)
}

// RaiseFromTask is the flagged `mugicrew pivot raise <task>` (Spec FR 1–15): it files the
// raise onto the Task's one held Checkpoint as Drafts by the Task — a kind:pivot
// (discovered-from the Task, the Checkpoint in its metadata, no gate edge), a Critique
// parented to the Checkpoint and discovered-from the Task, an Ask Critique also
// discovered-from its pivot or merge target — and records --merge-into on the Task's
// metadata, writing nothing to the target. (Spec decision 3 has the verb write no edge
// for a merge; the Ask's discovered-from edge to the merge target is written here, as the
// Signal reroute does, and a drop deletes it with the Draft.) Every refusal runs before any write. The
// clean-exit publish hands the gate and opens the Drafts; nothing blocks the Task on
// them and nothing closes it.
func RaiseFromTask(taskID string, r RaiseRequest) error {
	if r.None() {
		return errors.New(leg + ": nothing to raise; pass --critique, --pivot or --merge-into")
	}
	task, err := raiseTask(taskID)
	if err != nil {
		return err
	}
	if existing, err := raiseDrafts(task); err != nil {
		return err
	} else if len(existing) > 0 {
		return fmt.Errorf("%s: task %s already has raise Drafts; drop the session to change the raise", leg, task.ID)
	}
	if err := checkRaiseShape(r); err != nil {
		return fmt.Errorf("%s: %w", leg, err)
	}
	t, err := resolveRaise(task)
	if err != nil {
		return err
	}
	host := t.host
	if err := checkMerges(host.ID, r.Merges); err != nil {
		return fmt.Errorf("%s: %w", leg, err)
	}
	for _, n := range hostNotes(host) {
		notify(leg, "%s", n)
	}
	fail := func(err error) error {
		return eventlog.Record("task.raise", "task.raise.failed", err, eventlog.BeadAttrs(task)...)
	}
	// before any Draft, so a crash cannot lose them; always written, so a re-run with no
	// --merge-into replaces any leftover merges instead of publishing them
	if len(r.Merges) > 0 {
		err = setMetadata(task.ID, RaiseMergesMeta, r.Merges)
	} else {
		err = clearMetadata(task.ID, RaiseMergesMeta)
	}
	if err != nil {
		return fail(err)
	}
	from := "discovered-from:" + task.ID
	var pivots, critiques []string
	askDeps := []string{from}
	for _, body := range r.Pivots {
		meta, err := json.Marshal(map[string]string{RaiseCheckpointMeta: host.ID})
		if err != nil {
			return err
		}
		id, err := createRaiseDraft(bd.Successor{Kind: contract.KindPivot, Title: draftTitle(body, task.Title), Description: body, DraftedBy: task.ID, Metadata: string(meta)}, from)
		if err != nil {
			return fail(err)
		}
		pivots = append(pivots, id)
		askDeps = append(askDeps, "discovered-from:"+id)
	}
	for _, m := range r.Merges {
		askDeps = append(askDeps, "discovered-from:"+m.Pivot)
	}
	for _, body := range r.Critiques {
		id, err := createRaiseDraft(bd.Successor{Kind: contract.KindCritique, Title: draftTitle(body, task.Title), Description: body, DraftedBy: task.ID, Parent: host.ID}, askDeps...)
		if err != nil {
			return fail(err)
		}
		critiques = append(critiques, id)
	}
	return eventlog.Record("task.raise", "task.raise.failed", nil,
		append(eventlog.BeadAttrs(task), "checkpoint", host.ID, "pivots", pivots, "critiques", critiques, "merges", mergeTargets(r.Merges))...)
}

// refuseRaise is a probe refusal: its reason, then the fallback.
func refuseRaise(leg, format string, a ...any) error {
	return fmt.Errorf("%s: %s; %s", leg, strings.TrimSpace(fmt.Sprintf(format, a...)), fallback)
}
