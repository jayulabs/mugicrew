package bd

import (
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// RunSession is the shared tail of every bead-anchored AFK handler: it runs inv
// through ctx's harness, then re-reads the Invocation's bead with show (the
// caller's own reader, so its test seam keeps working; the pre-dispatch snapshot
// is stale). open=false with err=nil means the bead is closed — by the agent, a
// prior dispatch or a concurrent confirm sweep (FR49) — and the handler is done.
// The order is fixed: a failed Run whose bead reads closed is done; a failed Run
// whose bead reads open, or whose read fails, returns the Run's error unchanged;
// after a successful Run a read error is an error, never "closed". line is the
// last non-blank trimmed line of the output ("" when the bead is closed), res the whole Result. It closes,
// spawns and publishes nothing: every graph transition stays the caller's, inside
// the publish boundary (ADR 0059 (drafts-initialization-safe-graph-mutations)).
func RunSession(ctx contract.Ctx, inv contract.Invocation, show func(string) (Bead, error)) (fresh Bead, line string, res contract.Result, open bool, err error) {
	res, runErr := ctx.Harness().Run(ctx, inv)
	fresh, readErr := show(inv.BeadID)
	if runErr != nil {
		if readErr == nil && !fresh.Open() {
			return fresh, "", res, false, nil
		}
		return fresh, "", res, true, runErr
	}
	if readErr != nil {
		return fresh, "", res, true, readErr
	}
	if !fresh.Open() {
		return fresh, "", res, false, nil
	}
	return fresh, lastNonBlankLine(res.Output), res, true, nil
}

// lastNonBlankLine is the last non-blank line of s, trimmed: headless output
// carries the whole assistant message, so the outcome sits behind any prose.
func lastNonBlankLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// Seams over the two bd legs RaiseIfPivoted drives, so it is testable without a
// live bd (the closeOnPublish idiom).
var (
	pivotsDiscoveredFrom = PivotsDiscoveredFrom
	raisePivot           = RaisePivot
)

// RaiseIfPivoted finds the pivots discovered-from b (the agent created them, or
// merged into an existing same-FR pivot) and, when any exist, raises them against
// b's Checkpoint (its parent) and specID, reporting raised. Franky's and Usopp's
// Review tails call it after RunSession.
func RaiseIfPivoted(ctx contract.Ctx, b Bead, specID string) (raised bool, err error) {
	pivots, err := pivotsDiscoveredFrom(b.ID)
	if err != nil || len(pivots) == 0 {
		return false, err
	}
	if err := raisePivot(ctx, b, b.Parent, specID, pivots); err != nil {
		return false, err
	}
	return true, nil
}
