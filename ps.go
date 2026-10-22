package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/tui"
)

// inProgress and probe are the ps verb's bd read and process-table probe, vars so
// the verb is testable with no bd and no live processes.
var (
	inProgress = bd.InProgress
	probe      = liveness.Alive
)

// psRow is one active AFK effort in the effort liveness view (ADR 0032 §2).
type psRow struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Assignee  string `json:"assignee"`
	Command   string `json:"command"`
	StartedAt string `json:"started_at"` // RFC 3339; empty if bd has no claim time
	Age       string `json:"age"`        // age-since-claim — never a liveness signal (ADR 0025)
	Liveness  string `json:"liveness"`   // alive | dead
}

// runPs is `mugicrew ps [--json]`: a one-shot snapshot of every active AFK effort
// and whether anything is running for it (ADR 0032 §1). Observe-only — it never
// writes to bd, and a dead effort is rendered, not an error (ADR 0031).
func runPs(argv []string, w io.Writer) error {
	asJSON := len(argv) == 1 && argv[0] == "--json"
	if len(argv) > 0 && !asJSON {
		return errors.New("usage: mugicrew ps [--json]")
	}
	inheritGraph()
	loop.StampAutoApprove()
	all, err := inProgress()
	if err != nil {
		return err
	}
	var efforts []bd.Bead
	var ids []string
	for _, b := range all {
		if loop.IsActiveAFKEffort(b) {
			efforts = append(efforts, b)
			ids = append(ids, b.ID)
		}
	}
	alive := probe(ids)
	rows := []psRow{}
	for _, b := range efforts {
		r := psRow{ID: b.ID, Title: b.Title, Kind: string(b.Kind), Assignee: b.Assignee,
			Command: effortCommand(b), Age: tui.HumanAge(b.Started), Liveness: "dead"}
		if !b.Started.IsZero() {
			r.StartedAt = b.Started.UTC().Format(time.RFC3339)
		}
		if alive[b.ID] {
			r.Liveness = "alive"
		}
		rows = append(rows, r)
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tLIVENESS\tCLAIMED_AGO\tASSIGNEE\tCOMMAND\tTITLE")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Kind, r.Liveness, r.Age, r.Assignee, r.Command, r.Title)
	}
	return tw.Flush()
}

// effortCommand constructs what dispatch spawns for the bead: worktree-routed
// kinds re-enter via the reconcile subprocess, the rest run claude directly with
// --name <id>. Built from the bead, not read from the process — nothing is captured.
// An unroutable bead (e.g. a Build missing its parent edge) can't have been
// spawned as either, so it shows "?" rather than a misleading command.
func effortCommand(b bd.Bead) string {
	d, err := route.Route(b)
	if err != nil {
		return "?"
	}
	if d.Placement != route.RepoRoot {
		return "mugicrew reconcile " + b.ID
	}
	return "claude -p … --name " + b.ID
}
