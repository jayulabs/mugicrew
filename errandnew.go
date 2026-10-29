package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"jayulabs.org/mugicrew/bd"
)

// newErrand is the bd leg of `errand new`; a var so tests fake it.
var newErrand = bd.NewErrand

// beadList is a repeatable string flag.
type beadList []string

func (l *beadList) String() string     { return fmt.Sprint([]string(*l)) }
func (l *beadList) Set(v string) error { *l = append(*l, v); return nil }

// runErrandNew files an Errand in the Initiative of the member it is ordered against, or
// born outside with the reason (ADR 0350). It prints the new id, then the placement
// line; a refused order edge is an error and files nothing.
func runErrandNew(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew errand new --drafted-by <bead> [--after <bead>]... [--before <bead>]... [--outside] --title T --description D"
	fs := flag.NewFlagSet("errand new", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	by := fs.String("drafted-by", "", "the filing crewmate's bead")
	var after, before beadList
	fs.Var(&after, "after", "a bead the Errand is blocked by (repeatable)")
	fs.Var(&before, "before", "a bead the Errand blocks (repeatable)")
	outside := fs.Bool("outside", false, "keep the Errand outside any Initiative")
	title := fs.String("title", "", "")
	desc := fs.String("description", "", "")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *by == "" || *title == "" || *desc == "" {
		return errors.New(usage)
	}
	id, line, err := newErrand(bd.ErrandFiling{Title: *title, Body: *desc, DraftedBy: *by, After: after, Before: before, Outside: *outside})
	if err != nil {
		return fmt.Errorf("errand new: %w", err)
	}
	fmt.Fprintf(w, "%s\n%s\n", id, line)
	return nil
}

// runRetiredTask is `mugicrew task ...`, kept only as a refusal (ADR 0354): the kind is an
// Errand now, and the old verb is no alias. It files nothing and names the new form.
func runRetiredTask([]string) error {
	return errors.New("mugicrew task is retired: the kind is an Errand now; file one with mugicrew errand new --drafted-by <bead> --title T --description D")
}

// relabelLegacyTasks is the bd leg of `errand relabel`; a var so tests fake it.
var relabelLegacyTasks = bd.RelabelLegacyTasks

// runErrandRelabel is `mugicrew errand relabel`, the safe fix behind doctor's task
// beads row (ADR 0355). It is internal: help does not list it.
func runErrandRelabel(argv []string, w io.Writer) error {
	if len(argv) != 0 {
		return errors.New("usage: mugicrew errand relabel")
	}
	n, err := relabelLegacyTasks("")
	if err != nil {
		return fmt.Errorf("errand relabel: %w", err)
	}
	fmt.Fprintf(w, "relabelled %d bead(s) kind:task -> kind:errand\n", n)
	return nil
}
