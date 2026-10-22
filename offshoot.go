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
// container, or unordered with the reason (ADR 0267). An ordered follow-up or
// prerequisite joins its cause's open Initiative (ADR 0348 §6, §7); every other
// offshoot is born outside it. It prints the new id,
// then the body's Order: line, then the Initiative: placement line.
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
	parent, place := "", "Initiative: outside (unordered)"
	if *order != "none" {
		var container, init bd.Bead
		if cause.Parent != "" {
			if p, err := showBead(cause.Parent); err != nil {
				return fmt.Errorf("signal offshoot: container of %s: %w", *from, err)
			} else if p.Kind == contract.KindCheckpoint || p.Kind == contract.KindChart {
				container = p
			} else if p.Kind == contract.KindInitiative && cause.Kind == contract.KindTask && cause.Unclosed() {
				container, init = cause, p
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
		if edge != "" {
			if init.ID == "" && container.Parent != "" {
				// any status: a closed Initiative is not "no Initiative"
				if init, err = bd.ParentInitiative(container, showBead); err != nil {
					return fmt.Errorf("signal offshoot: initiative %s: %w", container.Parent, err)
				}
			}
			parent, place = bd.InitiativePlacement(init, container.Title)
		}
	}

	body := *desc + "\n\n"
	id, err := newOffshoot(*title, body+line, *by, *from, edge, parent)
	if err != nil && edge != "" {
		reason := bd.CarrySkipReason(err)
		if reason == "" {
			return fmt.Errorf("signal offshoot: %w", err)
		}
		line, place = fmt.Sprintf("Order: unordered (%s)", reason), "Initiative: outside (unordered)"
		id, err = newOffshoot(*title, body+line, *by, *from, "", "")
	}
	if err != nil {
		return fmt.Errorf("signal offshoot: %w", err)
	}
	fmt.Fprintf(w, "%s\n%s\n%s\n", id, line, place)
	return nil
}
