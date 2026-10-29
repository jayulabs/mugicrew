package tui

import (
	"fmt"
	"strconv"
	"strings"

	"jayulabs.org/mugicrew/git"
)

// mrFacts is an MR Watch's display-only Forge lines (ADR 0385), parsed through the keys Jinbe
// writes them with. A field is ok only when its line is present and parses.
type mrFacts struct {
	head                   string
	pipeStatus, pipeRanOn  string
	pipeID                 int
	pipeNone, pipeOK       bool
	threadsOpen, threadsOf int
	threadsOK              bool
	mergeStatus            string
}

func parseMRFacts(body string) mrFacts {
	var f mrFacts
	f.head, _ = git.ParseLine(body, git.MRHeadKey)
	if raw, ok := git.ParseLine(body, git.MRPipelineKey); ok {
		if raw == "none" {
			f.pipeNone, f.pipeOK = true, true
		} else if p := strings.Fields(raw); len(p) == 3 {
			if id, err := strconv.Atoi(p[1]); err == nil {
				f.pipeStatus, f.pipeID, f.pipeRanOn, f.pipeOK = p[0], id, p[2], true
			}
		}
	}
	if raw, ok := git.ParseLine(body, git.MRThreadsKey); ok {
		o, r, found := strings.Cut(raw, "/")
		oi, e1 := strconv.Atoi(strings.TrimSpace(o))
		ri, e2 := strconv.Atoi(strings.TrimSpace(r))
		if found && e1 == nil && e2 == nil {
			f.threadsOpen, f.threadsOf, f.threadsOK = oi, ri, true
		}
	}
	f.mergeStatus, _ = git.ParseLine(body, git.MRMergeStatusKey)
	return f
}

// allGreen is Spec D6: a passing pipeline on the MR's current head, no open thread, and a
// settled can_be_merged. Anything missing or unparseable is not all green; approval,
// need_rebase and draft are not consulted.
func (f mrFacts) allGreen() bool {
	return f.pipeOK && !f.pipeNone && f.pipeStatus == "success" &&
		f.head != "" && f.pipeRanOn == f.head &&
		f.threadsOK && f.threadsOpen == 0 &&
		f.mergeStatus == "can_be_merged"
}

// fields are d's rows for the Watch's lines, in order; a missing line gives no row.
func (f mrFacts) fields() [][2]string {
	var out [][2]string
	switch {
	case f.pipeOK && f.pipeNone:
		out = append(out, [2]string{"pipeline", "none"})
	case f.pipeOK:
		at := sha8(f.pipeRanOn)
		if f.pipeRanOn == "-" {
			at = "- (train)"
		}
		v := fmt.Sprintf("%s #%d at %s", f.pipeStatus, f.pipeID, at)
		if f.head != "" {
			v += " · head " + sha8(f.head)
		}
		out = append(out, [2]string{"pipeline", v})
	}
	if f.threadsOK {
		out = append(out, [2]string{"threads", fmt.Sprintf("%d open of %d", f.threadsOpen, f.threadsOf)})
	}
	if f.mergeStatus != "" {
		v := f.mergeStatus
		if v == "cannot_be_merged" {
			v += " (conflict)"
		}
		out = append(out, [2]string{"merge status", v})
	}
	return out
}
