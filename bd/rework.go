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

// ReworkApproval is `mugicrew approval rework <approval> --critique <file>...`
// (Spec FR6–14, ADR 0203 §2): it validates every file before any read, refuses
// unless approval is an open or in_progress kind:approval child of a Checkpoint,
// adopts the Approval's Critique Drafts whose title matches a file, creates the
// missing ones, deletes the unnamed ones, then publishes the Critiques (and any
// Signal Drafts of the Approval) and closes the Approval "rework: <ids>" in one
// batch. A re-run after a crash before the publish adopts and finishes. Returns
// the Critique ids.
func ReworkApproval(approval string, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("approval rework: at least one --critique file is required")
	}
	crits := make([]critiqueFile, len(files))
	seen := map[string]bool{}
	for i, f := range files {
		c, err := parseCritiqueFile(f)
		if err != nil {
			return nil, err
		}
		if seen[c.title] {
			return nil, fmt.Errorf("approval rework: duplicate Critique title %q", c.title)
		}
		seen[c.title], crits[i] = true, c
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
	case in[ap.Parent].Kind != contract.KindCheckpoint:
		return nil, fmt.Errorf("approval rework: %s is not a child of a Checkpoint", approval)
	case ap.Status == "closed":
		return nil, fmt.Errorf("approval rework: %s is already closed (%s)", approval, ap.CloseReason)
	case ap.Status != "open" && ap.Status != "in_progress":
		return nil, fmt.Errorf("approval rework: %s is %s, not open or in_progress", approval, ap.Status)
	}
	drafts, err := Drafts(approval)
	if err != nil {
		return nil, err
	}
	var stray []string
	have := map[string]string{}
	for _, d := range drafts {
		if d.Kind != contract.KindCritique {
			continue
		}
		if seen[d.Title] && have[d.Title] == "" {
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
	ids := make([]string, len(crits))
	for i, c := range crits {
		if ids[i] = have[c.title]; ids[i] != "" {
			continue
		}
		if ids[i], err = SpawnChild(ap.Parent, Successor{Kind: contract.KindCritique, Title: c.title, Description: c.body, DraftedBy: approval}); err != nil {
			return nil, err
		}
	}
	if _, err := Publish(approval, nil, &Closing{ID: approval, Reason: "rework: " + strings.Join(ids, ", ")}); err != nil {
		return nil, err
	}
	slog.Info("approval.rework", "bead", approval, "critiques", strings.Join(ids, ","))
	return ids, nil
}
