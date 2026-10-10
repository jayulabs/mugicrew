package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/stats"
)

// statsLog is the event log path the stats verb reads, a var so tests point it
// at a temp file.
var statsLog = eventlog.Resolve

const statsUsage = "usage: mugicrew stats [<bead>] [--since <dur|date>] [--by crewmate|model|day] [--json]"

// runStats is `mugicrew stats [<bead>] [--since <dur|date>] [--by
// crewmate|model|day] [--json]`: Session cost and Agent time per bead (or per
// --by key) summed from the event log's session end events (ADR 0061).
// Observe-only (ADR 0033): it reads the retained log and writes nothing.
func runStats(argv []string, w io.Writer) error {
	var bead, by string
	var since time.Time
	asJSON := false
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; {
		case a == "--json":
			asJSON = true
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
			case "crewmate", "model", "day":
			default:
				return errors.New(statsUsage)
			}
		case bead == "" && len(a) > 0 && a[0] != '-':
			bead = a
		default:
			return errors.New(statsUsage)
		}
	}
	path, err := statsLog()
	if err != nil {
		return err
	}
	evs, _, err := eventlog.Read(path)
	if err != nil {
		return err
	}

	total, rows := stats.Sum(evs, stats.Filter{Bead: bead, Since: since, By: by})

	if asJSON {
		var jrows any = rows
		if by == "model" {
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
	fmt.Fprintln(tw, keyCol+"\tSESSIONS\tFAILED\tWALL\tAPI\tCOST\tIN\tOUT\tCACHE_R\tCACHE_W")
	for _, r := range append(rows, total) {
		c := r.Cells(by == "model" && r.Key != total.Key)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Key, c[0], c[1], c[2], c[3],
			r.USD(), stats.Tokens(r.In), stats.Tokens(r.Out), stats.Tokens(r.CacheRead), stats.Tokens(r.CacheWrite))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if total.CostPartial > 0 {
		_, err = fmt.Fprintf(w, "+ %d sessions without a cost\n", total.CostPartial)
	}
	return err
}
