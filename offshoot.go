package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// newOffshoot is the bd leg of `signal offshoot`; a var so tests fake it.
var newOffshoot = bd.NewOffshoot

// runSignalOffshoot files an offshoot Signal ordered against its cause's
// container, or unordered with the reason (ADR 0267). It prints the new id,
// then the body's Order: line.
func runSignalOffshoot(argv []string, w io.Writer) error {
	const usage = "usage: mugicrew signal offshoot --from <cause> --drafted-by <bead> --order follow-up|prerequisite|none --title T --description D"
	fs := flag.NewFlagSet("signal offshoot", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main prints the error once
	from := fs.String("from", "", "the bead that found the work")
	by := fs.String("drafted-by", "", "the filing crewmate's bead")
	order := fs.String("order", "", "follow-up|prerequisite|none")
	title := fs.String("title", "", "")
	desc := fs.String("description", "", "")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *from == "" || *by == "" || *title == "" || *desc == "" ||
		*order != "follow-up" && *order != "prerequisite" && *order != "none" {
		return errors.New(usage)
	}
	cause, err := showBead(*from)
	if err != nil {
		return fmt.Errorf("signal offshoot: cause %s: %w", *from, err)
	}

	line, edge := "Order: none", ""
	if *order != "none" {
		var container bd.Bead
		if cause.Parent != "" {
			if p, err := showBead(cause.Parent); err != nil {
				return fmt.Errorf("signal offshoot: container of %s: %w", *from, err)
			} else if p.Kind == contract.KindCheckpoint || p.Kind == contract.KindChart {
				container = p
			}
		}
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

	body := *desc + "\n\n"
	id, err := newOffshoot(*title, body+line, *by, *from, edge)
	if err != nil && edge != "" {
		reason := bd.CarrySkipReason(err)
		if reason == "" {
			return fmt.Errorf("signal offshoot: %w", err)
		}
		line = fmt.Sprintf("Order: unordered (%s)", reason)
		id, err = newOffshoot(*title, body+line, *by, *from, "")
	}
	if err != nil {
		return fmt.Errorf("signal offshoot: %w", err)
	}
	fmt.Fprintf(w, "%s\n%s\n", id, line)
	return nil
}
