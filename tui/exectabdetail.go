package tui

// The Execute tab's d detail pane (Spec mc-lu2x.21 FR38-FR42): the same split-height
// layout as Defog's, with per-row-kind text.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
)

func (t execTab) detailVP() viewport.Model {
	return drawerVP(t.open, t.w, t.h, t.detailText(), t.off)
}

// specOfCP is the Spec a Checkpoint serves, from the snapshot (bd.SpecOf): its caused-by
// Spec, or through a Rollback's related target the rolled-back Checkpoint's. Nil when
// the cause is neither, or the walk leaves the snapshot or runs past four hops.
func (g graphData) specOfCP(cp *bd.Bead) *bd.Bead {
	for range 4 {
		if cp.CausedBy == "" || g.beadIndex(cp.CausedBy) < 0 {
			return nil
		}
		p := g.beadByID(cp.CausedBy)
		switch p.Kind {
		case contract.KindSpec:
			return p
		case contract.KindRollback:
			if len(p.Related) == 0 || g.beadIndex(p.Related[0]) < 0 {
				return nil
			}
			cp = g.beadByID(p.Related[0])
		default:
			return nil
		}
	}
	return nil
}

// cpOfSpec is the live Checkpoint cut for spec, if any.
func (g graphData) cpOfSpec(spec *bd.Bead) *bd.Bead {
	cps, _ := execListed(g.beads)
	for _, cp := range cps {
		if s := g.specOfCP(cp); s != nil && s.ID == spec.ID {
			return cp
		}
	}
	return nil
}

// detailText is the selected row's detail.
func (t execTab) detailText() string {
	r, ok := t.selRow()
	if !ok {
		return ""
	}
	return t.rowText(r)
}

// rowText is r's detail; the Initiative focus builds the same text for its Execute rows.
func (t execTab) rowText(r eRow) string {
	g := t.g
	switch r.kind {
	case eSec:
		if r.key == actKey {
			return r.title + "\n\nThe HITL beads waiting on you.\n\nenter does nothing here (space folds)."
		}
		if r.key == liveKey {
			return liveHeaderText(r.title)
		}
		if r.key == mrsKey {
			return r.title + "\n\n" + mrsDetail + "\n\nenter does nothing here (space folds)."
		}
		if r.key == specsKey {
			return r.title + "\n\n" + specsDetail + "\n\nenter does nothing here (space folds)."
		}
		if r.key == cpsKey {
			return r.title + "\n\n" + cpsDetail + "\n\nenter does nothing here (space folds)."
		}
		if r.key == landedKey {
			return r.title + "\n\nCheckpoints whose Landing closed landed: within the last 24h.\n\nenter does nothing here (space folds)."
		}
		if r.key == treeKey {
			return r.title + "\n\nThe Initiative groups.\n\nenter does nothing here (space folds)."
		}
		if r.init == nil {
			return r.title + "\n\nThe Checkpoints and Specs with no Initiative parent, and the Tasks and Pivots that wait on them.\n\nenter does nothing here (space folds)."
		}
		return g.sbrn(r.init, r.title, true, 0) + "\n\nenter does nothing on an Initiative row (space folds).\n\n" + g.beadBody(r.init, true)
	case ePlace: // no bead: a launch can park the cursor here
		return r.title
	case eSum:
		var h strings.Builder
		fmt.Fprintf(&h, "%d closed under %s\n\nenter does nothing here (space folds).\n", len(r.members), r.cp.ID)
		for _, m := range r.members {
			fmt.Fprintf(&h, "\n%s %s — %s", softSt.Render(m.ID), m.Title, closeReason(m))
		}
		return h.String()
	}
	b := r.b
	var h strings.Builder
	field := func(k, v string) { h.WriteString(labelField(k, v) + "\n") }
	title := leafTitle(b, false)
	if b.Kind == contract.KindCheckpoint {
		title = b.Title
	}
	h.WriteString(g.sbrn(b, title, true, 0) + "\n\n")
	owner := "—"
	if e, err := registry.Lookup(b.Kind); err == nil {
		owner = e.Owner
	}
	field("owner", owner)
	for _, f := range forgeReviewFields(b) {
		field(f[0], f[1])
	}
	if r.kind == eMR {
		if url := git.RecordedMRLink(b.Description); url != "" { // read through the helper only: never derived from the iid, path or host
			field("MR link", url)
		}
	}
	kind := r.kind
	if r.cpAct() || kind == eLive || kind == eCPS || kind == eSPS || kind == eLanded { // a Live or Checkpoints row details like its tree row
		switch b.Kind {
		case contract.KindCheckpoint:
			kind = eCP
		case contract.KindSpec:
			kind = eSpec
		}
	}
	switch kind {
	case eCP:
		field("status", b.Status)
		if r.kind == eLanded { // closed: no Rollup, the Landing is the why
			l := r.members[0]
			field("why", fmt.Sprintf("Landing %s closed %s", l.ID, l.CloseReason))
		} else {
			ru := g.rollup(b)
			field("rollup", ru.label())
			field("why", ru.Why)
			if len(ru.Also) > 0 {
				field("also", strings.Join(ru.Also, ", "))
			}
		}
		open, closed := g.cpKids(b)
		total := map[contract.Kind]int{}
		done := map[contract.Kind]int{}
		for _, m := range open {
			total[m.Kind]++
		}
		for _, m := range closed {
			total[m.Kind]++
			done[m.Kind]++
		}
		var kinds []string
		for k := range total {
			kinds = append(kinds, string(k))
		}
		slices.Sort(kinds)
		var parts []string
		for _, k := range kinds {
			parts = append(parts, fmt.Sprintf("%s %d/%d", k, done[contract.Kind(k)], total[contract.Kind(k)]))
		}
		field("members", fmt.Sprintf("%d/%d closed", len(closed), len(open)+len(closed))+rest(parts))
		ws, rels := g.waiters(b)
		for _, rel := range []string{"before land", "after land", "gates"} {
			var ids []string
			for i, w := range ws {
				if rels[i] == rel {
					ids = append(ids, w.Title+" ("+w.ID+")")
				}
			}
			if len(ids) > 0 {
				field(rel, strings.Join(ids, ", "))
			}
		}
		if s := g.specOfCP(b); s != nil {
			field("spec", s.Title+" ("+s.ID+")")
		} else {
			field("spec", "none")
		}
	case eSpec:
		if cp := g.cpOfSpec(b); cp != nil {
			field("stage", "Checkpoint cut")
			field("checkpoint", cp.Title+" ("+cp.ID+")")
		} else {
			field("stage", "specified — no Checkpoint cut")
		}
	default:
		if b.Unclosed() {
			field("state", leafWords[g.leafState(b)])
		}
		if v, ok := g.sessionLine(b); ok {
			field("session", v)
		}
		if r.cp != nil {
			field("checkpoint", r.cp.Title+" ("+r.cp.ID+") — "+r.rel)
		}
	}
	if bl := g.openBlockers(b); len(bl) > 0 {
		var ids []string
		for _, o := range bl {
			ids = append(ids, o.Title+" ("+o.ID+")")
		}
		field("blocked by", strings.Join(ids, ", "))
	}
	if l := g.pauseLine(b); l != "" {
		field("paused", l)
	}
	field("enter", t.enterField(r))
	out := []string{strings.TrimRight(h.String(), "\n")}
	if body := g.beadBody(b, true); body != "" {
		out = append(out, body)
	}
	return strings.Join(out, "\n\n")
}

func rest(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " · ") + ")"
}

// enterField is the detail's enter line, the one rowEnterField gives the keypress.
func (t execTab) enterField(r eRow) string {
	if r.kind == eLanded {
		return "refuses — " + landedRefuse
	}
	return t.g.rowEnterField(r.b, r.leaf())
}
