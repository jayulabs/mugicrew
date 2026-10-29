package jinbe

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// Seams over the hold clearing step's bd reads and close and the tmux session table.
var (
	holdTargets     = bd.Blocking
	holdSessions    = session.Sessions
	holdClose       = bd.ForceClose
	holdGetPipeline = forge.GetPipeline
)

// pipelineSuccess is a Forge pipeline status that clears a hold.
const pipelineSuccess = "success"

// holdKeyRe is a recognised Hold Errand key line: Closed MR: !N, Merged MR: !N,
// Pipeline: <id> or Foreign: <sha> (ADR 0393).
var holdKeyRe = regexp.MustCompile(`^(Closed MR: !|Merged MR: !|Pipeline: |Foreign: )(\S+)$`)

// holdKey is a parsed key line: its variant prefix and number (the MR iid or pipeline id;
// 0 for a Foreign sha or a non-numeric id), the line itself verbatim.
type holdKey struct {
	line, variant string
	n             int
}

const (
	keyClosedMR = "Closed MR: !"
	keyMergedMR = "Merged MR: !"
	keyPipeline = "Pipeline: "
	keyForeign  = "Foreign: "
)

// holdKeyLine is body's first fact line that is a recognised key line.
func holdKeyLine(body string) (holdKey, bool) {
	for _, ln := range strings.Split(body, "\n") {
		m := holdKeyRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		k := holdKey{line: ln, variant: m[1]}
		if k.variant != keyForeign {
			k.n, _ = strconv.Atoi(m[2])
		}
		if k.variant == keyClosedMR || k.variant == keyMergedMR {
			if k.n == 0 { // digits only for an MR iid
				continue
			}
		}
		return k, true
	}
	return holdKey{}, false
}

// holdBlocked is the one bead errand blocks, read from bd (which finds a closed one a
// tick's open list lacks). ok is false for zero or several targets.
func holdBlocked(errand bd.Bead) (bd.Bead, bool, error) {
	found, err := holdTargets(errand.ID)
	if err != nil || len(found) != 1 {
		return bd.Bead{}, false, err
	}
	return found[0], true, nil
}

// holdMootReason is the close reason for a hold whose blocked bead closed by any path
// but withdraw (its sweep stays the only closer there), or "" while the hold stands.
func holdMootReason(blocked bd.Bead) string {
	if blocked.Status != "closed" {
		return ""
	}
	if strings.HasPrefix(blocked.CloseReason, "withdrawn") || strings.HasPrefix(blocked.CloseReason, "cancelled:") {
		return ""
	}
	return fmt.Sprintf("obsolete: %s closed", blocked.ID)
}

// holdRollback is a Rollback hold's resolved context, read once per check: whether its
// target Checkpoint is recorded, that target, and the bead whose repo stamp resolves the
// Forge project (the Rollback's own when no target is recorded).
type holdRollback struct {
	target   string
	found    bool
	repoBead bd.Bead
}

func resolveHoldRollback(rb bd.Bead) (holdRollback, error) {
	target, found, err := rollbackTarget(rb.ID)
	if err != nil || !found {
		return holdRollback{repoBead: rb}, err
	}
	cp, err := showBead(target)
	if err != nil {
		return holdRollback{}, err
	}
	return holdRollback{target, true, decidingBead(rb, cp)}, nil
}

// holdLandedTrailer is the trailer whose presence on mainline clears a hold on blocked
// (a Checkpoint's own, a Rollback's Reverts-Checkpoint: <target>) and the bead whose
// repo stamp resolves the Forge project; ok is false for a hold with no landed read:
// a Foreign hold, a Chart Pipeline hold, a Rollback-blocking Closed MR hold.
func holdLandedTrailer(key holdKey, blocked bd.Bead, rb holdRollback) (trailer string, repoBead bd.Bead, ok bool) {
	if key.variant == keyForeign {
		return "", bd.Bead{}, false
	}
	switch blocked.Kind {
	case contract.KindCheckpoint:
		return checkpointTrailer(blocked.ID), blocked, true
	case contract.KindChart:
		if key.variant == keyClosedMR || key.variant == keyMergedMR {
			return git.TrailerFor(string(blocked.Kind), blocked.ID), blocked, true
		}
	case contract.KindRollback:
		if key.variant == keyClosedMR || !rb.found {
			return "", bd.Bead{}, false
		}
		return git.RevertsTrailer(string(contract.KindCheckpoint), rb.target), rb.repoBead, true
	}
	return "", bd.Bead{}, false
}

// holdLanded is the landed read: one mainline fetch the way Jinbe does (FetchMainline
// then LandedBase), true once the trailer is on mainline. A trailer not there, or a
// foreign land, leaves the hold undecided.
func holdLanded(key holdKey, blocked bd.Bead, rb holdRollback) (bool, error) {
	trailer, repoBead, ok := holdLandedTrailer(key, blocked, rb)
	if !ok {
		return false, nil
	}
	r, _ := repoDirOf(repoBead)
	if _, err := fetchForge(r); err != nil {
		return false, err
	}
	read := landedBase
	if r != "" {
		read = r.LandedBase
	}
	_, err := read(trailer)
	switch foreign := (*git.ForeignLanding)(nil); {
	case err == nil:
		return true, nil
	case errors.Is(err, git.ErrNotLanded), errors.As(err, &foreign):
		return false, nil
	}
	return false, err
}

// holdRest is the REST read, the last a hold pays: at most one Forge call. It clears a
// Closed MR hold on a Checkpoint or Chart once GetMR reads opened, a Checkpoint Pipeline hold once
// GetPipeline reads success, and a Rollback Pipeline hold once its revert MR (the iid
// recorded on the Rollback, never the Errand's link line) reads merged or its head
// pipeline success; that MR reading closed makes the hold obsolete. The head pipeline's
// id is not compared with the key's: a Rollback's head pipeline reading success means the
// failure the hold recorded is superseded, whichever pipeline ran last. reason is ""
// while undecided. Merged MR, Foreign and Chart holds make no read.
func holdRest(key holdKey, blocked bd.Bead, rb holdRollback) (reason string, err error) {
	cleared := "cleared: " + key.line
	switch {
	case (blocked.Kind == contract.KindCheckpoint || blocked.Kind == contract.KindChart) && key.variant == keyClosedMR:
		proj, err := holdProject(blocked)
		if err != nil {
			return "", err
		}
		mr, err := forgeMR(proj, key.n)
		if err != nil {
			return "", err
		}
		if mr.State == forge.StateOpened {
			return cleared, nil
		}
	case blocked.Kind == contract.KindCheckpoint && key.variant == keyPipeline:
		if key.n == 0 {
			return "", nil
		}
		proj, err := holdProject(blocked)
		if err != nil {
			return "", err
		}
		pl, err := holdGetPipeline(proj, key.n)
		if err != nil {
			return "", err
		}
		if pl.Status == pipelineSuccess {
			return cleared, nil
		}
	case blocked.Kind == contract.KindRollback && key.variant == keyPipeline:
		iid := git.RecordedMR(blocked.Description)
		if iid == 0 {
			return "", nil
		}
		proj, err := holdProject(rb.repoBead)
		if err != nil {
			return "", err
		}
		mr, err := forgeMR(proj, iid)
		if err != nil {
			return "", err
		}
		switch {
		case mr.State == forge.StateMerged, mr.HeadPipeline != nil && mr.HeadPipeline.Status == pipelineSuccess:
			return cleared, nil
		case mr.State == forge.StateClosed:
			return "obsolete: revert merge request closed", nil
		}
	}
	return "", nil
}

// holdProject is the Forge project of bead's repo stamp.
func holdProject(b bd.Bead) (forge.Project, error) {
	r, _ := repoDirOf(b)
	return resolveProject(r)
}

// holdReadFailed logs a failed read of e's hold once for its check: a Forge 429 like
// holdRateLimited's, any other error as a failed check. The hold is retried next period.
func holdReadFailed(e bd.Bead, err error) {
	event := "hold.check_failed"
	if errors.Is(err, forge.ErrRateLimited) {
		event = "hold.rate_limited"
	}
	slog.Warn(event, append(eventlog.BeadAttrs(e), "err", err.Error())...)
}

type holdDecision struct {
	errand, blocked bd.Bead
	reason          string
}

// checkHold is one Errand's check: its decision, if a fact Go can read says it is moot.
// An error is logged and the hold left to the next period.
func checkHold(e bd.Bead, key holdKey) (holdDecision, bool) {
	blocked, ok, err := holdBlocked(e)
	if err != nil {
		holdReadFailed(e, err)
		return holdDecision{}, false
	}
	if !ok {
		return holdDecision{}, false
	}
	switch blocked.Kind {
	case contract.KindCheckpoint, contract.KindChart, contract.KindRollback:
	default:
		return holdDecision{}, false
	}
	if reason := holdMootReason(blocked); reason != "" {
		return holdDecision{e, blocked, reason}, true
	}
	var rb holdRollback
	if blocked.Kind == contract.KindRollback && key.variant != keyClosedMR && key.variant != keyForeign {
		if rb, err = resolveHoldRollback(blocked); err != nil {
			holdReadFailed(e, err)
			return holdDecision{}, false
		}
	}
	landed, err := holdLanded(key, blocked, rb)
	if err != nil {
		holdReadFailed(e, err)
		return holdDecision{}, false
	}
	if landed {
		return holdDecision{e, blocked, "cleared: " + key.line}, true
	}
	reason, err := holdRest(key, blocked, rb)
	if err != nil {
		holdReadFailed(e, err)
		return holdDecision{}, false
	}
	return holdDecision{e, blocked, reason}, reason != ""
}

// ClearHolds is the loop's hold clearing step (ADR 0393): housekeeping each tick that
// closes a Hold Errand once a fact Go can read says it is moot. due says whether an
// Errand's pacing allows a check and checked stamps one once its reads have ended, pass
// or fail. It runs no session, claims nothing and never fails the tick: an error is
// logged and the hold retried next period. A hold whose guide session is live, or whose
// liveness cannot be read, is never closed.
func ClearHolds(open []bd.Bead, due func(id string) bool, checked func(id string)) {
	var decided []holdDecision
	for _, e := range open {
		if e.Kind != contract.KindErrand || (e.Status != "open" && e.Status != "in_progress") {
			continue
		}
		key, ok := holdKeyLine(e.Description)
		if !ok || !due(e.ID) {
			continue
		}
		d, ok := checkHold(e, key)
		checked(e.ID)
		if ok {
			decided = append(decided, d)
		}
	}
	if len(decided) == 0 {
		return
	}
	table, err := holdSessions()
	if err != nil {
		slog.Warn("hold.liveness_failed", "err", err.Error())
		return
	}
	for _, d := range decided {
		if table[d.errand.ID].State == session.Alive {
			continue
		}
		if table, err = holdSessions(); err != nil { // re-read just before each close: a guide may have gone live since
			slog.Warn("hold.liveness_failed", "err", err.Error())
			return
		}
		if table[d.errand.ID].State == session.Alive {
			continue
		}
		if err := holdClose(d.errand.ID, d.reason); err != nil {
			slog.Warn("hold.close_failed", append(eventlog.BeadAttrs(d.errand), "blocked", d.blocked.ID, "err", err.Error())...)
			continue
		}
		event := "hold.obsolete"
		if strings.HasPrefix(d.reason, "cleared: ") {
			event = "hold.cleared"
		}
		slog.Info(event, append(eventlog.BeadAttrs(d.errand), "blocked", d.blocked.ID, "reason", d.reason)...)
	}
}
