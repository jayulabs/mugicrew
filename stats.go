package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/stats"
)

// statsLog is the event log path the stats verb reads, a var so tests point it
// at a temp file.
var statsLog = eventlog.Resolve

// statsLineage reads the bead lineage the stats verb scopes a bead by, a var so
// tests inject a fixture graph.
var statsLineage = bd.LoadLineage

const statsUsage = "usage: mugicrew stats [<bead>] [--spine] [--since <dur|date>] [--by crewmate|model|profile|kind|day] [--kind <kind>] [--json]"

// exitCode is an error main exits with code (default 1): 2 is --spine without a
// bead or a bead that is not on a spine; other usage errors exit 1.
type exitCode struct {
	code int
	error
}

func (e exitCode) Unwrap() error { return e.error }

// runStats is `mugicrew stats [<bead>] [--spine] [--since <dur|date>] [--by
// crewmate|model|profile|kind|day] [--kind <kind>] [--json]`: Session cost and
// Agent time per bead (or per --by key), optionally of one bead kind, summed
// from the event log's session end events (ADR 0061). A bead scopes to its whole
// parent subtree, closed beads included, read from bd (ADR 0286), so a Chart counts
// its grandchildren; --spine sums the whole spine the bead sits on, one row per
// stage in spine order with a KIND column (ADR 0283, ADR 0284). No bead reads no bd.
// Observe-only (ADR 0033): it reads the retained log and writes nothing.
func runStats(argv []string, w io.Writer) error {
	var bead, by, kind string
	var since time.Time
	asJSON, spine := false, false
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; {
		case a == "--json":
			asJSON = true
		case a == "--spine":
			spine = true
		case a == "--since" && i+1 < len(argv):
			i++
			if d, err := time.ParseDuration(argv[i]); err == nil {
				since = time.Now().Add(-d)
			} else if t, err := time.ParseInLocation("2006-01-02", argv[i], time.Local); err == nil {
				since = t
			} else {
				return errors.New(statsUsage)
			}
		case a == "--by" && by == "" && i+1 < len(argv):
			i++
			switch by = argv[i]; by {
			case "crewmate", "model", "profile", "kind", "day":
			default:
				return errors.New(statsUsage)
			}
		case a == "--kind" && kind == "" && i+1 < len(argv) && argv[i+1] != "":
			// Not checked against the registry: an unknown kind matches nothing.
			i++
			kind = argv[i]
		case bead == "" && len(a) > 0 && a[0] != '-':
			bead = a
		default:
			return errors.New(statsUsage)
		}
	}
	if spine && bead == "" {
		return exitCode{2, errors.New(statsUsage)}
	}
	path, err := statsLog()
	if err != nil {
		return err
	}
	evs, _, err := eventlog.Read(path)
	if err != nil {
		return err
	}

	var scope map[string]string
	var stages []bd.Stage // --spine: one row per stage, in spine order
	if bead != "" {
		lin, err := statsLineage()
		if err != nil {
			return err
		}
		scope = map[string]string{}
		if spine {
			if stages, err = lin.Spine(bead); err != nil {
				return exitCode{2, err}
			}
			scope = bd.SpineScope(stages)
		} else {
			for _, id := range lin.Subtree(bead) {
				scope[id] = id
			}
		}
	}
	total, rows := stats.Sum(evs, stats.Filter{Scope: scope, Since: since, Kind: kind, By: by})
	var kinds []string // per row, under --spine without --by
	if spine && by == "" {
		have := map[string]stats.Row{}
		for _, r := range rows {
			have[r.Key] = r
		}
		rows = nil
		for _, st := range stages {
			r, ok := have[st.ID]
			if !ok {
				r = stats.Row{Key: st.ID}
			}
			rows = append(rows, r)
			kinds = append(kinds, st.Kind)
		}
	}

	if asJSON {
		var jrows any = rows
		if kinds != nil {
			type kindRow struct {
				stats.Row
				Kind string `json:"kind"`
			}
			krows := []kindRow{}
			for i, r := range rows {
				krows = append(krows, kindRow{r, kinds[i]})
			}
			jrows = krows
		} else if by == "model" {
			mrows := []stats.ModelRow{}
			for _, r := range rows {
				mrows = append(mrows, stats.ModelRow{Key: r.Key, Cost: r.Cost})
			}
			jrows = mrows
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Rows  any       `json:"rows"`
			Total stats.Row `json:"total"`
		}{jrows, total})
	}
	keyCol := "BEAD"
	if by != "" {
		keyCol = strings.ToUpper(by)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	kindHdr := ""
	if kinds != nil {
		kindHdr = "\tKIND"
	}
	fmt.Fprintln(tw, keyCol+kindHdr+"\tSESSIONS\tFAILED\tWALL\tAPI\tCOST\tAVG_COST\tAVG_API\tFAIL%\tIN\tOUT\tCACHE_R\tCACHE_W")
	for i, r := range append(rows, total) {
		c := r.Cells(by == "model" && r.Key != total.Key)
		key := r.Key
		if kinds != nil {
			key += "\t"
			if i < len(kinds) {
				key += kinds[i]
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", key, c[0], c[1], c[2], c[3],
			r.USD(), c[4], c[5], c[6], stats.Tokens(r.In), stats.Tokens(r.Out), stats.Tokens(r.CacheRead), stats.Tokens(r.CacheWrite))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if total.CostPartial > 0 {
		_, err = fmt.Fprintf(w, "+ %d sessions without a cost\n", total.CostPartial)
	}
	return err
}
