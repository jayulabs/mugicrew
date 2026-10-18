package tui

// The Defog tab's d detail pane (FR27, FR32-35): plain text in the lower half,
// scrolled with pgup/pgdn, as on Ideate.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/registry"
)

func (t defogTab) detailVP() viewport.Model {
	return drawerVP(t.open, t.w, t.h, t.detailText(), t.off)
}

var leafWords = map[lstate]string{
	sReady: "ready", sBlocked: "blocked", sPaused: "paused", sLaunching: "launching",
	sLive: "live", sTurn: "your turn", sAsk: "needs approval", sDone: "done — /exit", sExited: "exited",
}

// chartSections splits a Chart body by '## ' heading: the lines under each (blank
// ones too), keyed by the lower-cased heading with '-' read as a space.
func chartSections(body string) (secs map[string][]string, order []string) {
	lines := strings.Split(body, "\n")
	secs = map[string][]string{}
	for _, s := range contract.ChartSections(lines) {
		order = append(order, s.Name)
		secs[strings.ReplaceAll(strings.ToLower(s.Name), "-", " ")] = lines[s.Head+1 : s.End]
	}
	return
}

// nonBlank counts the lines with text.
func nonBlank(lines []string) (n int) {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return
}

// detailText is the selected row's detail.
func (t defogTab) detailText() string {
	r, ok := t.selRow()
	if !ok {
		return ""
	}
	return t.rowText(r)
}

// rowText is r's detail; the Initiative focus calls it on rows the tab does not list.
func (t defogTab) rowText(r dRow) string {
	g := t.g
	field := func(h *strings.Builder, k, v string) {
		h.WriteString(labelField(k, v) + "\n")
	}
	switch r.kind {
	case dSec:
		if r.key == liveKey {
			return liveHeaderText(r.title)
		}
		if r.key == chartsKey {
			return r.title + "\n\n" + chartsDetail + "\n\nenter does nothing here (space folds)."
		}
		if r.key == finKey {
			return r.title + "\n\nCharts closed within the last 24h, newest first.\n\nenter does nothing here (space folds)."
		}
		if r.key == treeKey {
			return r.title + "\n\nThe Initiative groups.\n\nenter does nothing here (space folds)."
		}
		return r.title + "\n\nThe HITL beads waiting on you.\n\nenter does nothing here (space folds)."
	case dHead:
		if r.init == nil {
			return r.title + "\n\nThe Charts that belong to no Initiative.\n\nenter does nothing here (space folds)."
		}
		return g.sbrn(r.init, r.title, true, 0) + "\n\nenter does nothing on an Initiative row (space folds).\n\n" + g.beadBody(r.init, true)
	case dPlace: // no bead: a launch can park the cursor here
		switch {
		case r.title != "":
			return r.title
		case r.key == liveNone:
			return liveEmpty
		}
		return "nothing waits on you"
	case dSum:
		var h strings.Builder
		fmt.Fprintf(&h, "%d closed under %s\n\nenter does nothing here (space folds).\n", len(r.members), r.chart.ID)
		for _, m := range r.members {
			fmt.Fprintf(&h, "\n%s %s — %s", softSt.Render(m.ID), m.Title, closeReason(m))
		}
		return h.String()
	}
	b := r.b
	var h strings.Builder
	title := leafTitle(b, false)
	if b.Kind == contract.KindChart {
		title = chartName(b.Title)
	}
	h.WriteString(g.sbrn(b, title, true, 0) + "\n\n")
	if b.Kind == contract.KindChart {
		hitl, afk, _ := chartKids(g.beads, b)
		secs, order := chartSections(b.Description)
		field(&h, "counts", fmt.Sprintf("fog %d · HITL %d · AFK %d", nonBlank(secs["not yet specified"]), len(hitl), len(afk)))
		if r.kind == dCW || r.kind == dACW {
			w := g.chartWord(b)
			field(&h, "chart word", w.label())
			field(&h, "why", w.Why)
			if len(w.Also) > 0 {
				field(&h, "also", strings.Join(w.Also, ", "))
			}
		}
		if r.kind == dFin {
			word, _, why := g.chartOutcome(b)
			field(&h, "outcome", word)
			field(&h, "why", why)
		}
		field(&h, "enter", t.enterField(r))
		if l := g.pauseLine(b); l != "" {
			field(&h, "paused", l)
		}
		parts := []string{strings.TrimRight(h.String(), "\n")}
		var rest []string
		for _, name := range order {
			norm := strings.ReplaceAll(strings.ToLower(name), "-", " ")
			if body := secs[norm]; norm == "destination" || norm == "not yet specified" {
				parts = append(parts, labelSt.Render(name)+"\n"+mdBody(strings.Join(body, "\n"), g.w))
			} else {
				n := nonBlank(body)
				rest = append(rest, fmt.Sprintf("%s: %d %s", name, n, plural(n, "line")))
			}
		}
		parts = append(parts, strings.Join(rest, "\n"))
		if body := g.beadBody(b, false); body != "" {
			parts = append(parts, body)
		}
		return strings.Join(parts, "\n\n")
	}
	mode := "AFK"
	if isHITL(b.Kind) {
		mode = "HITL"
	}
	owner := "—"
	if e, err := registry.Lookup(b.Kind); err == nil {
		owner = e.Owner
	}
	field(&h, "owner", owner+" · "+mode)
	switch {
	case b.Status == "closed": // no state field: the close line lives in beadBody
	case r.leaf():
		field(&h, "state", leafWords[g.leafState(b)])
	default:
		field(&h, "liveness", livenessLine(g.liveOf(b), b))
	}
	if v, ok := g.sessionLine(b); ok {
		field(&h, "session", v)
	}
	if bl := g.heldBy(b); len(bl) > 0 {
		var ids []string
		for _, o := range bl {
			ids = append(ids, o.Title+" ("+o.ID+")")
		}
		field(&h, "blocked by", strings.Join(ids, ", "))
	}
	if anc := g.inheritedPause(b); anc != "" {
		field(&h, "paused", "manual via "+anc)
	}
	field(&h, "enter", t.enterField(r))
	parts := []string{strings.TrimRight(h.String(), "\n")}
	if body := g.beadBody(b, true); body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}

func closeReason(b *bd.Bead) string {
	if b.CloseReason == "" {
		return bd.DefaultCloseReason
	}
	return b.CloseReason
}

func plural(n int, w string) string {
	if n == 1 {
		return w
	}
	return w + "s"
}

// enterField is the detail's enter line (graphData.rowEnterField).
func (t defogTab) enterField(r dRow) string {
	if r.kind == dFin {
		return "refuses — " + finRefuse
	}
	return t.g.rowEnterField(r.b, r.leaf())
}
