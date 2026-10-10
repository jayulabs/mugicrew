package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// integrateGit, integrateReplace and unpause are Integrate's git and bd seams,
// vars so its exit mapping and pause lift are testable without a repo or a bd.
var (
	integrateGit     = git.Integrate
	integrateReplace = git.IntegrateReplace
	unpause          = bd.Unpause
)

// Integrate's exit codes, which the backstop and the CLI pass through.
const (
	ExitIntegrated = 0 // integrated, or nothing ahead
	ExitTransient  = 1
	ExitConflict   = 2 // rebase, held, landed
)

// Integrate is mugicrew integrate for bead b, run in its effort tree (Spec
// D3, ADR 0102 §3, ADR 0104 §1/§2/§9): it squashes the effort onto the
// container route names (replace mode, a replace-CAS at its pinned Tip, for a
// conflict or chart-conflict) and returns the exit code, 0 integrated or nothing
// ahead, 2 a conflict (rebase, held, landed), 1 transient. A 0 lifts
// paused:integrate-conflict. It never closes b.
func Integrate(b bd.Bead, by eventlog.IntegrateBy, w io.Writer) int {
	d, err := route.Route(b)
	if err == nil && d.Container == "" {
		err = fmt.Errorf("integrate: %s bead %s integrates onto no container", b.Kind, b.ID)
	}
	if err == nil && d.Mode == route.Replace && d.Tip == "" {
		// A Conflict filed before per-effort branches carries no Tip: pin.
		err = fmt.Errorf("integrate: %s bead %s carries no Tip: pin to replace %s at; nothing was integrated", b.Kind, b.ID, d.Container)
		fmt.Fprintf(w, "%v.\nA bead filed before per-effort branches has none: add a line %q to its body, the container tip it rewrites "+
			"(git rev-parse %s, if the container hasn't moved since), then re-run mugicrew integrate.\n", err, git.TipPin("<sha>"), d.Container)
		return ExitTransient
	}
	if err != nil {
		fmt.Fprintln(w, err)
		return ExitTransient
	}
	var sha string
	var retries int
	if d.Mode == route.Replace {
		sha, retries, err = integrateReplace(d.Container, d.Tip)
	} else {
		sha, retries, err = integrateGit(d.Container, b.ID, git.TrailerFor(string(b.Kind), b.ID))
	}
	eventlog.RecordIntegrate(b, d.Container, sha, retries, by, err)
	var conflict *git.IntegrateConflict
	switch {
	case errors.As(err, &conflict) && conflict.Reason == git.ConflictRebase:
		fmt.Fprintf(w, "%v; the rebase is stopped in this tree.\n"+
			"Resolve the conflicts, git add them, git rebase --continue, then re-run mugicrew integrate.\n"+
			"If you can't resolve them: git rebase --abort, note why on bead %s, leave it open and exit.\n", err, b.ID)
		return ExitConflict
	case conflict != nil:
		fmt.Fprintln(w, err)
		return ExitConflict
	case err != nil:
		fmt.Fprintln(w, err)
		return ExitTransient
	}
	lifted := false
	if bd.PausedBy(b, bd.PausedIntegrateConflict) {
		if err := unpause(b.ID, bd.PausedIntegrateConflict); err != nil {
			fmt.Fprintf(w, "integrated, but lifting paused:%s failed: %v\n", bd.PausedIntegrateConflict, err)
		} else {
			lifted = true
			slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedIntegrateConflict)...)
		}
	}
	switch {
	case sha == "" && lifted && by == eventlog.ByHuman:
		// the trailer was already on the container: lifting the pause was the last step
		fmt.Fprintf(w, "nothing to integrate onto %s; bead still open: bd close %s\n", d.Container, b.ID)
	case sha == "":
		fmt.Fprintf(w, "nothing to integrate onto %s\n", d.Container)
	case by == eventlog.ByHuman:
		fmt.Fprintf(w, "integrated; bead still open: bd close %s\n", b.ID)
	default:
		fmt.Fprintf(w, "integrated %s onto %s\n", sha, d.Container)
	}
	return ExitIntegrated
}
