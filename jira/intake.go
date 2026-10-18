package jira

import (
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
)

// The bd legs of intake, indirected so tests file no bead.
var (
	NewSignalWithRef = bd.NewSignalWithRef
	CarrierOf        = bd.CarrierOf
	Supersede        = bd.Supersede
)

// IntakeResult is what Intake filed. Key is the resolved key (it differs from
// the typed one when Jira renamed the item); Carrier is "" unless the new
// Signal was superseded onto a live carrier of the same ref.
type IntakeResult struct {
	ID, Key, Carrier string
}

// Open is the read both Jira verbs open with: the Workspace's bare-host [jira]
// site at root, then the item through acli. verb prefixes its errors.
func Open(verb, root, typed string) (Item, string, error) {
	site, siteFile := cfglayer.JiraSite(root)
	if !ValidSite(site) {
		return Item{}, "", fmt.Errorf("%s %s: [jira] site must be a bare host such as acme.atlassian.net; set it in %s", verb, typed, siteFile)
	}
	it, err := View(typed, site)
	if err != nil {
		return Item{}, "", fmt.Errorf("%s %s: %w", verb, typed, err)
	}
	return it, site, nil
}

// Intake files Jira work item typed as a Signal titled <KEY>: <summary>
// carrying external ref jira-<KEY> and a body that only points at the item (ADR
// 0205). A failure before filing returns a zero result; filed-then-failed
// (the supersede) returns the result with ID set and the error.
func Intake(root, typed string) (IntakeResult, error) {
	it, site, err := Open("intake jira", root, typed)
	if err != nil {
		return IntakeResult{}, err
	}
	body := fmt.Sprintf("Jira work item %[1]s: https://%[2]s/browse/%[1]s\n\nTriage reads this work item from Jira; this body only points at it:\n\n    acli jira workitem view %[1]s\n", it.Key, site)
	ref := "jira-" + it.Key
	carrier, found, err := CarrierOf(ref) // before filing: the new Signal carries the ref too
	if err != nil {
		return IntakeResult{Key: it.Key}, fmt.Errorf("intake jira %s: %w", typed, err)
	}
	id, err := NewSignalWithRef(it.Key+": "+it.Summary, body, ref)
	if err != nil {
		return IntakeResult{Key: it.Key}, fmt.Errorf("intake jira %s: %w", typed, err)
	}
	res := IntakeResult{ID: id, Key: it.Key}
	if found { // a re-intake is kept as lineage, never a second effort (ADR 0006)
		if err := Supersede(id, carrier.ID); err != nil {
			return res, fmt.Errorf("intake jira %s: filed %s but could not supersede it onto %s: %w", typed, id, carrier.ID, err)
		}
		res.Carrier = carrier.ID
	}
	return res, nil
}
