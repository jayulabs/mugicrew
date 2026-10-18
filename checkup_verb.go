package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
	"jayulabs.org/mugicrew/tui"
)

// checkupIO is runCheckup's seams: the reads and writes of the launch, a var so
// main's tests script them.
type checkupIO struct {
	Show    func(id string) (bd.Bead, error)
	Refusal func(target bd.Bead) error
	OpenOn  func(target string) (bd.Bead, bool, error)
	New     func(target bd.Bead, problem string) (string, error)
	Fence   func(id, cause string) error
	Launch  func(checkupID string) error
}

var checkupSeams = checkupIO{
	Show:    bd.Show,
	Refusal: chopper.CheckupRefusal,
	OpenOn:  bd.OpenCheckupOn,
	New:     bd.NewCheckup,
	Fence:   bd.Pause,
	Launch:  launchCheckup,
}

const checkupUsage = "usage: mugicrew checkup <bead> [--problem \"<one line>\"]"

// runCheckup is `mugicrew checkup <bead> [--problem P]` (ADR 0274 §1): it reads the
// target, refuses or finds-or-files its Checkup, fences the target with
// paused:checkup, then starts (or resumes) the HITL session and attaches the
// terminal. Every refusal is exit 1; there is no --force.
func runCheckup(argv []string, errw io.Writer) int {
	fs := flag.NewFlagSet("checkup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	problem := fs.String("problem", "", "the symptom, one line")
	var id string
	err := fs.Parse(argv)
	if err == nil && fs.NArg() > 0 { // the id may sit before the flags
		id = fs.Arg(0)
		err = fs.Parse(fs.Args()[1:])
	}
	if err != nil || id == "" || fs.NArg() > 0 {
		fmt.Fprintln(errw, checkupUsage)
		return 1
	}
	if err := checkup(checkupSeams, id, *problem); err != nil {
		fmt.Fprintln(errw, err)
		return 1
	}
	return 0
}

func checkup(x checkupIO, id, problem string) error {
	target, err := x.Show(id)
	if err != nil {
		return err
	}
	ckID, err := chopper.FileCheckup(chopper.CheckupFiling{Refusal: x.Refusal, OpenOn: x.OpenOn, New: x.New, Fence: x.Fence}, target, problem)
	if err != nil {
		return err
	}
	return x.Launch(ckID)
}

// launchCheckup starts the Checkup's HITL session on the repo's tmux server, or
// resumes an exited one, and attaches this terminal to it: by switch-client from
// inside the private server, else by an attach-session as bare mugicrew does.
func launchCheckup(id string) error {
	b, err := bd.Show(id)
	if err != nil {
		return err
	}
	all, err := session.Sessions()
	if err != nil {
		return err
	}
	ctx := newCtx()
	switch s := all[id]; s.State {
	case session.Alive:
	case session.Exited:
		pctx, err := ctx.ForProfile(s.Profile)
		if err := tui.RefuseProfile(b, s.Profile, "resume", err); err != nil {
			return err
		}
		if err := session.Resume(b, s, pctx.Harness()); err != nil {
			return err
		}
	default:
		if err := startCheckup(ctx, b); err != nil {
			return err
		}
	}
	if inside, err := session.Inside(); err == nil && inside {
		return session.SwitchTo(id)
	}
	return session.Attach(id)
}

// startCheckup claims b (unless it is an idle in_progress Checkup being rejoined)
// and starts its session; a failed start releases the claim (ADR 0034).
func startCheckup(ctx appCtx, b bd.Bead) error {
	pctx, profile, err := ctx.ForKind(b.Kind)
	if err := tui.RefuseProfile(b, profile, "hitl", err); err != nil {
		return err
	}
	lc, err := chopper.Chopper{}.Launch(contract.WithClosingLine(pctx), contract.Bead{ID: b.ID, Kind: b.Kind})
	if err != nil {
		return err
	}
	ac, ok := lc.(contract.AgentCommand)
	if !ok {
		return fmt.Errorf("checkup: expected an agent command, got %T", lc)
	}
	ac.Profile = profile
	claim := b.Status != "in_progress"
	if claim {
		if err := bd.Claim(b.ID); err != nil {
			var held *bd.HeldError
			if errors.As(err, &held) {
				return fmt.Errorf("%s is claimed by %s — not launched", b.ID, held.Holder)
			}
			return fmt.Errorf("claim: %w", err)
		}
	}
	if err := session.Start(b, ac); err != nil {
		if claim {
			_ = bd.Unclaim(b.ID) // ponytail: a failed release strands the claim; bd unclaim by hand
		}
		return err
	}
	return nil
}
