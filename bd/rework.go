package bd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// critiqueFile is one --critique file: its first "# Title" line and the rest.
type critiqueFile struct{ title, body string }

// parseCritiqueFile reads path as "# Title" then a non-empty body (Spec FR7).
func parseCritiqueFile(path string) (critiqueFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return critiqueFile{}, err
	}
	first, rest, _ := strings.Cut(strings.TrimSpace(string(raw)), "\n")
	title := strings.TrimSpace(strings.TrimPrefix(first, "#"))
	if !strings.HasPrefix(first, "# ") || title == "" {
		return critiqueFile{}, fmt.Errorf("approval rework: %s: the first line must be \"# <Title>\"", path)
	}
	if body := strings.TrimSpace(rest); body != "" {
		return critiqueFile{title, body}, nil
	}
	return critiqueFile{}, fmt.Errorf("approval rework: %s: empty body", path)
}

// ReworkApproval is `mugicrew approval rework <approval> ...` (Spec FR6–14, ADR
// 0203 §2). It validates every file before any read, and refuses unless approval
// is an open or in_progress kind:approval child of a Checkpoint or a Chart.
//
// Checkpoint: --critique files only. It adopts the Approval's Critique Drafts
// whose title matches a file, creates the missing ones, deletes the unnamed ones,
// then publishes them (and any Signal Drafts of the Approval) and closes the
// Approval "rework: <ids>" in one batch.
//
// Chart: --grilling and --research files only (at least one in total). The same
// adopt/create/prune runs over grilling and research Drafts, children of the
// Chart; chart:defogged comes off before the publish, so the Chart owes defog
// again. A re-run after a crash before the publish adopts and finishes.
// Returns the new bead ids.
func ReworkApproval(approval string, critiques, grillings, research []string) ([]string, error) {
	type want struct {
		kind  contract.Kind
		files []string
	}
	type item struct {
		kind contract.Kind
		critiqueFile
	}
	var items []item
	seen := map[string]bool{}
	for _, w := range []want{{contract.KindCritique, critiques}, {contract.KindGrilling, grillings}, {contract.KindResearch, research}} {
		for _, f := range w.files {
			c, err := parseCritiqueFile(f)
			if err != nil {
				return nil, err
			}
			if seen[c.title] {
				return nil, fmt.Errorf("approval rework: duplicate title %q", c.title)
			}
			seen[c.title] = true
			items = append(items, item{w.kind, c})
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("approval rework: at least one --critique file (a Checkpoint) or --grilling/--research file (a Chart) is required")
	}
	all, err := ListAll()
	if err != nil {
		return nil, err
	}
	in, _ := index(all)
	ap, ok := in[approval]
	switch {
	case !ok:
		return nil, fmt.Errorf("approval rework: no bead %s", approval)
	case ap.Kind != contract.KindApproval:
		return nil, fmt.Errorf("approval rework: %s is not a kind:approval bead", approval)
	case in[ap.Parent].Kind != contract.KindCheckpoint && in[ap.Parent].Kind != contract.KindChart:
		return nil, fmt.Errorf("approval rework: %s is not a child of a Checkpoint or a Chart", approval)
	case ap.Status == "closed":
		return nil, fmt.Errorf("approval rework: %s is already closed (%s)", approval, ap.CloseReason)
	case ap.Status != "open" && ap.Status != "in_progress":
		return nil, fmt.Errorf("approval rework: %s is %s, not open or in_progress", approval, ap.Status)
	}
	isChart := in[ap.Parent].Kind == contract.KindChart
	for _, it := range items {
		switch {
		case isChart && it.kind == contract.KindCritique:
			return nil, fmt.Errorf("approval rework: --critique applies to a Checkpoint's Approval; %s belongs to a Chart (use --grilling/--research)", approval)
		case !isChart && it.kind != contract.KindCritique:
			return nil, fmt.Errorf("approval rework: --grilling and --research apply to a Chart's Approval; %s belongs to a Checkpoint (use --critique)", approval)
		}
	}
	drafts, err := Drafts(approval)
	if err != nil {
		return nil, err
	}
	kindOf := map[string]contract.Kind{}
	for _, it := range items {
		kindOf[it.title] = it.kind
	}
	var stray []string
	have := map[string]string{}
	for _, d := range drafts {
		switch d.Kind {
		case contract.KindCritique, contract.KindGrilling, contract.KindResearch:
		default:
			continue
		}
		if k, named := kindOf[d.Title]; named && k == d.Kind && have[d.Title] == "" {
			have[d.Title] = d.ID
		} else {
			stray = append(stray, d.ID)
		}
	}
	if len(stray) > 0 {
		if _, err := run(append(append([]string{"delete"}, stray...), "--force")...); err != nil {
			return nil, err
		}
	}
	ids := make([]string, len(items))
	for i, it := range items {
		if ids[i] = have[it.title]; ids[i] != "" {
			continue
		}
		if ids[i], err = SpawnChild(ap.Parent, Successor{Kind: it.kind, Title: it.title, Description: it.body, DraftedBy: approval}); err != nil {
			return nil, err
		}
	}
	if isChart && in[ap.Parent].HasLabel(contract.LabelDefogged) {
		// Ahead of the publish: the open Approval keeps the Chart undrained, so a
		// crash between the two leaves a re-run to adopt and finish.
		if _, err := run("label", "remove", ap.Parent, contract.LabelDefogged); err != nil {
			return nil, err
		}
	}
	if _, err := Publish(approval, nil, &Closing{ID: approval, Reason: "rework: " + strings.Join(ids, ", ")}); err != nil {
		return nil, err
	}
	slog.Info("approval.rework", "bead", approval, "beads", strings.Join(ids, ","))
	return ids, nil
}
