package bd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

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
// whole on any failing line. A var so a test can fake it.
var runBatch = func(script string) error {
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
	out, err := run("list", "--label", contract.DraftedBy(creator), "--status", "deferred", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
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
	script := publishScript(drafts, edges, closing)
	if script == "" {
		return nil, nil
	}
	if err := runBatch(script); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, _ = run(append(append([]string{"label", "remove"}, ids...), contract.DraftedBy(creator))...)
	}
	return ids, nil
}

// publishScript is Publish's batch, pure (bar the title.stripped event) so its
// order is testable without a db. A kind-prefixed Draft title is stripped in its
// open line, never rejected (ADR 0097).
func publishScript(drafts []Bead, edges []Edge, closing *Closing) string {
	var b strings.Builder
	for _, e := range edges {
		fmt.Fprintln(&b, strings.TrimSpace("dep add "+e.From+" "+e.To+" "+e.Type))
	}
	for _, d := range drafts {
		line := "update " + d.ID + " status=open"
		if t := stripTitle(d.Title, "id", d.ID); t != d.Title {
			line += " title=" + batchQuote(t)
		}
		fmt.Fprintln(&b, line)
	}
	if closing != nil {
		line := "close " + closing.ID
		if r := batchQuote(closing.Reason); r != `""` {
			line += " " + r
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
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
func DeleteDrafts(creator string) ([]string, []BranchOutcome, error) {
	drafts, err := Drafts(creator)
	if err != nil || len(drafts) == 0 {
		return nil, nil, err
	}
	ids := IDs(drafts)
	if _, err := run(append(append([]string{"delete"}, ids...), "--force")...); err != nil {
		return nil, nil, err
	}
	return ids, deleteDraftBranches(drafts), nil
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
		if _, err := git.Head("refs/heads/" + branch); err != nil {
			continue
		}
		err := errors.New("has work")
		if git.IsAncestor("refs/heads/"+branch, git.Mainline()) {
			err = git.DeleteBranch(branch)
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
