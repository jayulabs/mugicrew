package bd

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Build-time check (Spec Further Notes): bd 1.3.0 accepts
// `bd create -s deferred --parent <Draft>`, so a Draft Chart takes Draft defog
// children before either is published.

// Edge is a final edge Publish adds in its batch (contract.Edge).
type Edge = contract.Edge

// Closing is the finishing bead Publish closes in its batch — the creator's own
// bead, handed to the publish so it can't close ahead of its Drafts (Spec D6).
type Closing struct{ ID, Reason string }

// runBatch pipes a script into one `bd batch` — one transaction, rolled back
// whole on any failing line — replaying a clean conflict under the shared retry
// policy (Spec D2, ADR 0153). A failed batch commits nothing, so the same script
// is re-piped byte for byte. A var so a test can fake it.
var runBatch = func(script string) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err = runBatchOnce(script); err == nil {
			return nil
		}
		if attempt == maxAttempts-1 || !retryable(err) {
			break
		}
		sleep(backoff(attempt))
	}
	return err
}

// runBatchOnce is one bd batch exec; a var so a test fakes the process under the retry.
var runBatchOnce = func(script string) error {
	if sealed {
		return errSealed("batch")
	}
	if err := errUnpinned("batch"); err != nil {
		return err
	}
	cmd := exec.Command("bd", "batch")
	cmd.Stdin = strings.NewReader(script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // as run: a loop's Ctrl-C must not kill the write (ADR 0050)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd batch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Drafts lists the creator's Drafts.
func Drafts(creator string) ([]Bead, error) {
	return core.List(Query{Label: contract.DraftedBy(creator), Status: []string{"deferred"}})
}

// Publish makes the creator's Drafts normal beads in one bd batch (ADR 0059 §4,
// Spec D4): the final edges, every Draft to open, then the finishing close. Any
// failure rolls the whole batch back. Removing the drafted-by labels afterwards
// is best-effort — a leftover label is harmless. Nothing to do is a no-op.
// Returns the published ids: nil only when nothing was done, empty when the batch
// landed just the edges and close.
func Publish(creator string, edges []Edge, closing *Closing) ([]string, error) {
	drafts, err := Drafts(creator)
	if err != nil {
		return nil, err
	}
	ids := IDs(drafts)
	ops := publishOps(drafts, edges, closing)
	if len(ops) == 0 {
		return nil, nil
	}
	if err := core.Batch(ops); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_ = core.RemoveLabel(ids, contract.DraftedBy(creator))
	}
	return ids, nil
}

// publishOps is Publish's batch, pure (bar the title.stripped event) so its order is
// testable without a db: the final edges, every Draft to open, then the finishing close. A
// kind-prefixed Draft title is stripped in its open op, never rejected (ADR 0097).
func publishOps(drafts []Bead, edges []Edge, closing *Closing) []Op {
	var ops []Op
	for _, e := range edges {
		ops = append(ops, EdgeOp(e.From, e.To, e.Type))
	}
	for _, d := range drafts {
		title := ""
		if t := stripTitle(d.Title, "id", d.ID); t != d.Title {
			title = t
		}
		ops = append(ops, OpenOp(d.ID, title))
	}
	if closing != nil {
		ops = append(ops, CloseOp(closing.ID, closing.Reason))
	}
	return ops
}

// batchQuote quotes a bd batch value, flattened to one line: batch is line-oriented.
func batchQuote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// BranchOutcome is what DeleteDrafts did with a deleted Draft's container
// branch: deleted (nil Err), or kept with why. Callers record it (bd is event-blind).
type BranchOutcome struct {
	Branch string
	Err    error
}

// DeleteDrafts deletes the creator's Drafts — a failed effort's or dropped
// session's unpublished work (ADR 0059 §6). None is a no-op. Returns the ids,
// and the outcomes of then deleting each deleted container Draft's branch, which never fail the Draft delete.
// s is the Serializer the caller runs under (ADR 0070): the delete is a read-then-write.
func DeleteDrafts(creator string, s Serializer) ([]string, []BranchOutcome, error) {
	drafts, err := Drafts(creator)
	if err != nil || len(drafts) == 0 {
		return nil, nil, err
	}
	ids := IDs(drafts)
	if err := core.Delete(ids, s); err != nil {
		return nil, nil, err
	}
	return ids, deleteDraftBranches(drafts), nil
}

// draftRepo is the child repo d's context: label names, "" for none or a subtree.
// ponytail: bd can't import contexts (it imports bd), so this reads the repo = table directly.
var draftRepo = func(d Bead) git.Repo {
	name := ContextOf(d)
	if name == "" {
		return ""
	}
	if len(cfglayer.Repos(".")) == 0 { // config first: a subtree-only Workspace pays no git call
		return ""
	}
	root, err := git.RepoRoot()
	if err != nil {
		return ""
	}
	if dir, ok := cfglayer.Repos(root)[name]; ok {
		return git.Repo(filepath.Join(root, dir))
	}
	return ""
}

// deleteDraftBranches deletes each Chart or Checkpoint Draft's branch whose tip
// is on mainline (as CutBranch left it); force is safe behind that gate. A
// branch never cut is skipped silently; one with work, or one git refuses (a
// worktree holds it), is kept.
func deleteDraftBranches(drafts []Bead) (outs []BranchOutcome) {
	for _, d := range drafts {
		if d.Kind != contract.KindChart && d.Kind != contract.KindCheckpoint {
			continue
		}
		branch := git.BranchName(string(d.Kind), d.ID)
		head, isAncestor, mainline, del := git.Head, git.IsAncestor, git.Mainline, git.DeleteBranch
		if r := draftRepo(d); r != "" { // a nested Draft's branch was cut in the child
			head, isAncestor, mainline, del = r.Head, r.IsAncestor, r.Mainline, r.DeleteBranch
		}
		if _, err := head("refs/heads/" + branch); err != nil {
			continue
		}
		err := errors.New("has work")
		if isAncestor("refs/heads/"+branch, mainline()) {
			err = del(branch)
		}
		outs = append(outs, BranchOutcome{branch, err})
	}
	return outs
}

// CloseOnPublish closes id with reason, together with its Drafts and final edges
// (Spec D6): a dispatched effort hands them to its tail's publish batch; outside
// one (a hand-run reconcile) there is no tail, so the flow publishes itself.
func CloseOnPublish(ctx contract.Ctx, id, reason string, edges []Edge) error {
	return onPublish(ctx, id, edges, &Closing{ID: id, Reason: reason})
}

// EdgesOnPublish is CloseOnPublish with no close (ADR 0079 §5): creator's Drafts
// and final edges publish, creator stays open.
func EdgesOnPublish(ctx contract.Ctx, creator string, edges []Edge) error {
	return onPublish(ctx, creator, edges, nil)
}

func onPublish(ctx contract.Ctx, creator string, edges []Edge, closing *Closing) error {
	if p, ok := ctx.(contract.Publisher); ok {
		for _, e := range edges {
			p.EdgeOnPublish(e)
		}
		if closing != nil {
			p.CloseOnPublish(closing.Reason)
		}
		return nil
	}
	_, err := Publish(creator, edges, closing)
	return err
}
