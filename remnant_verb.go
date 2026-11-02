package main

import (
	"errors"
	"fmt"
	"io"

	"jayulabs.org/mugicrew/remnant"
)

const remnantUsage = "usage: mugicrew remnant <remnant> drop|offshoot|respin"

// remnantSeams is runRemnant's exits, a var so main's tests script them.
var remnantSeams = remnant.Real

// respinSeams is the respin exit's seams, a var for the same reason.
var respinSeams = remnant.RealRespin

// runRemnant is `mugicrew remnant <id> drop|offshoot|respin`: one exit of a Remnant. It
// refuses a bead that is not an open Remnant with no open blocker.
func runRemnant(argv []string, w io.Writer) error {
	if len(argv) != 2 {
		return errors.New(remnantUsage)
	}
	var res remnant.Result
	var rr remnant.RespinResult
	var err error
	switch argv[1] {
	case "drop":
		res, err = remnant.Drop(remnantSeams, argv[0])
	case "offshoot":
		res, err = remnant.Offshoot(remnantSeams, argv[0])
	case "respin":
		rr, err = remnant.Respin(respinSeams, argv[0])
		res = rr.Result
	default:
		return errors.New(remnantUsage)
	}
	for _, id := range res.Swept {
		fmt.Fprintln(w, "carried", id)
	}
	for _, ref := range res.Deleted {
		fmt.Fprintln(w, "deleted", ref)
	}
	for _, id := range res.Errands {
		fmt.Fprintln(w, "closed errand", id)
	}
	if res.Signal != "" {
		fmt.Fprintln(w, "filed", res.Signal)
	}
	if err == nil && rr.Title != "" {
		fmt.Fprintln(w, "respin", rr.Title)
		if rr.Conflict != "" {
			fmt.Fprintln(w, "replay conflict", rr.Conflict)
		}
	}
	return err
}
