package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"jayulabs.org/mugicrew/bd"
)

// newTask is the bd leg of `task new`; a var so tests fake it.
var newTask = bd.NewTask

// beadList is a repeatable string flag.
type beadList []string

func (l *beadList) String() string     { return fmt.Sprint([]string(*l)) }
func (l *beadList) Set(v string) error { *l = append(*l, v); return nil }

// runTaskNew files a Task in the Initiative of the member it is ordered against, or
// born outside with the reason (ADR 0350). It prints the new id, then the placement
// line; a refused order edge is an error and files nothing.
func runTaskNew(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew task new --drafted-by <bead> [--after <bead>]... [--before <bead>]... [--outside] --title T --description D"
	fs := flag.NewFlagSet("task new", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	by := fs.String("drafted-by", "", "the filing crewmate's bead")
	var after, before beadList
	fs.Var(&after, "after", "a bead the Task is blocked by (repeatable)")
	fs.Var(&before, "before", "a bead the Task blocks (repeatable)")
	outside := fs.Bool("outside", false, "keep the Task outside any Initiative")
	title := fs.String("title", "", "")
	desc := fs.String("description", "", "")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *by == "" || *title == "" || *desc == "" {
		return errors.New(usage)
	}
	id, line, err := newTask(bd.TaskFiling{Title: *title, Body: *desc, DraftedBy: *by, After: after, Before: before, Outside: *outside})
	if err != nil {
		return fmt.Errorf("task new: %w", err)
	}
	fmt.Fprintf(w, "%s\n%s\n", id, line)
	return nil
}
