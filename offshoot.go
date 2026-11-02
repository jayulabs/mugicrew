package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"jayulabs.org/mugicrew/bd"
)

// newOffshoot is the bd leg of `signal offshoot`; a var so tests fake it.
var newOffshoot = bd.NewOffshoot

// runSignalOffshoot files an offshoot Signal ordered against its cause's
// container, or unordered with the reason (ADR 0267). Whatever the order, it
// climbs to its cause's Initiative and is born that open Initiative's direct
// member unless --outside opts out (ADR 0371). It prints the new id, then the
// body's Order: line, then the Initiative: line. --dry-run stops before any
// create and prints those two lines, then the climbed Initiative's Done when
// (open or closed); it cannot know a pair bd would refuse, so it shows the
// order as if the edge applies.
func runSignalOffshoot(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew signal offshoot --from <cause> --drafted-by <bead> --order follow-up|prerequisite|none [--outside <why>] [--dry-run] --title T --description D"
	fs := flag.NewFlagSet("signal offshoot", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	from := fs.String("from", "", "the bead that found the work")
	by := fs.String("drafted-by", "", "the filing crewmate's bead")
	order := fs.String("order", "", "follow-up|prerequisite|none")
	outside := fs.String("outside", "", "why the work is outside the cause's Initiative")
	dry := fs.Bool("dry-run", false, "print the Order: and Initiative: lines and the Initiative's Done when; file nothing")
	title := fs.String("title", "", "")
	desc := fs.String("description", "", "")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *from == "" || *by == "" || *title == "" || *desc == "" ||
		*order != "follow-up" && *order != "prerequisite" && *order != "none" {
		return errors.New(usage)
	}
	outsideSet := false
	fs.Visit(func(f *flag.Flag) { outsideSet = outsideSet || f.Name == "outside" })
	if outsideSet && *outside == "" {
		return errors.New(usage)
	}
	cause, err := showBead(*from)
	if err != nil {
		return fmt.Errorf("signal offshoot: cause %s: %w", *from, err)
	}

	parent, place, container, init, err := bd.OffshootPlacement(cause, showBead)
	if err != nil {
		return fmt.Errorf("signal offshoot: %w", err)
	}
	if parent != "" && outsideSet {
		parent, place = "", "Initiative: outside (filer: "+*outside+")"
	}

	line, edge := "Order: none", ""
	if *order != "none" {
		switch {
		case container.ID == "":
			line = fmt.Sprintf("Order: unordered (%s has no container)", cause.Title)
		case container.Status == "closed":
			line = fmt.Sprintf("Order: unordered (%s is closed)", container.Title)
		case *order == "follow-up":
			line, edge = "Order: follow-up of "+container.Title, "blocked-by:"+container.ID
		default:
			line, edge = "Order: prerequisite of "+container.Title, "blocks:"+container.ID
		}
	}

	if *dry {
		fmt.Fprintf(w, "%s\n%s\n", line, place)
		if init.ID != "" {
			fmt.Fprintf(w, "\nDone when (%s):\n%s\n", init.Title, bd.DoneWhen(init.Description))
		}
		return nil
	}
	body := *desc + "\n\n"
	id, err := newOffshoot(*title, body+line+"\n"+place, *by, *from, edge, parent)
	if err != nil && edge != "" {
		reason := bd.CarrySkipReason(err)
		if reason == "" {
			return fmt.Errorf("signal offshoot: %w", err)
		}
		line = fmt.Sprintf("Order: unordered (%s)", reason)
		id, err = newOffshoot(*title, body+line+"\n"+place, *by, *from, "", parent)
	}
	if err != nil {
		return fmt.Errorf("signal offshoot: %w", err)
	}
	fmt.Fprintf(w, "%s\n%s\n%s\n", id, line, place)
	return nil
}
