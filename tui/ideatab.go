package tui

// The Ideate tab (Spec mc-lu2x.6): open and in-triage Signals grouped under their
// Initiative, a detail pane and a ●N badge of triage sessions waiting on the human.
// Its action keys (enter, n, e, z, E, +/-, a/A) go through the shared launch, action
// and form paths over the selected Signal (ideaKey).

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// commonDir is the repo's git common dir, the one place every worktree shares; a seam
// for tests.
var commonDir = git.CommonDir

// foldsPath is the named fold file under the common dir ("" when it can't be resolved).
func foldsPath(name string) string {
	common, err := commonDir()
	if err != nil {
		return ""
	}
	return filepath.Join(common, "mugicrew", name)
}

// foldsFile is the one fold file Ideate, Defog and Execute share, a section per tab.
const foldsFile = "folds.json"

// loadFolds reads the tab's section of the fold file; a missing, unreadable or
// unparsable file is no folds.
func loadFolds(path, tab string) map[string]bool {
	flip := map[string]bool{}
	for k, v := range loadSection(path, tab) {
		if v {
			flip[k] = true
		}
	}
	return flip
}

// loadFlatFolds reads a flat fold file (Graph's), keeping false values: Graph stores
// absolute states.
func loadFlatFolds(path string) map[string]bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]bool
	json.Unmarshal(b, &m) // garbage leaves m nil or partial
	return m
}

// loadSection reads one tab's section of the shared fold file.
func loadSection(path, tab string) map[string]bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]map[string]bool
	json.Unmarshal(b, &m)
	return m[tab]
}

// saveFolds replaces the tab's section of the fold file with flip, leaving the other
// tabs' sections as they are; the last writer wins.
func saveFolds(path, tab string, flip map[string]bool) error {
	if path == "" {
		return errors.New("common dir unresolved")
	}
	var all map[string]map[string]bool
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &all) // garbage is replaced
	}
	if all == nil {
		all = map[string]map[string]bool{}
	}
	if flip == nil {
		flip = map[string]bool{}
	}
	all[tab] = flip
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// saveFlatFolds writes a flat fold file (Graph's) through a temp file renamed over path.
func saveFlatFolds(path string, flip map[string]bool) error {
	if flip == nil {
		flip = map[string]bool{}
	}
	b, err := json.Marshal(flip)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeFileAtomic writes b to path through a temp file renamed over it.
func writeFileAtomic(path string, b []byte) error {
	if path == "" {
		return errors.New("common dir unresolved")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // a no-op once renamed
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// newIdea is an Ideate tab with the folds the human saved on this machine.
func newIdea() ideaTab {
	path := foldsPath(foldsFile)
	return ideaTab{path: path, flip: loadFolds(path, "ideate")}
}

// save writes the folds minus a closed or missing bead's; the fold still applies this
// session.
func (t ideaTab) save() error {
	return saveFolds(t.path, "ideate", pruneFolds(unpeeked(t.flip, t.peek), t.beads, actKey, liveKey, treeKey))
}

// pruneFolds is flip minus the keys whose bead (after cutting an i: prefix and a
// trailing /closed) is missing from beads or closed; the keys in keep, i: (No
// Initiative) and the Backlog key stay. The tabs' save rule; flip itself is never pruned.
func pruneFolds(flip map[string]bool, beads []bd.Bead, keep ...string) map[string]bool {
	saved := maps.Clone(flip)
	for k := range saved {
		id := strings.TrimSuffix(strings.TrimPrefix(k, "i:"), "/closed")
		if id == "" || k == backlogKey || slices.Contains(keep, k) {
			continue
		}
		if j := beadIndex(beads, id); j < 0 || beads[j].Status == "closed" {
			delete(saved, k)
		}
	}
	return saved
}

// ideaRow is one list line: the '+ New Signal' row (always rs[0]), a group header
// (an Initiative, No Initiative or Backlog) or a Signal.
type ideaRow struct {
	key     string   // selection and fold key: "i:<initID>", "i:" for No Initiative, backlogKey, a Signal id, "" create
	b       *bd.Bead // a Signal row
	init    *bd.Bead // an Initiative header's Initiative
	head    string   // a header's title; "" on Signal and create rows
	create  bool
	backlog bool   // the Backlog header
	n       int    // a header's member count
	hint    string // a Backlog Signal's Initiative title, "" when loose
	drained bool   // a drained Initiative's header or its hint row
	cue     bool   // the hint row under a drained header, key "h:<initID>"
	cut     bool   // an Initiative header whose cut session exists (alive or exited)
	sec     bool   // a section header, key actKey, liveKey or treeKey
	place   bool   // an empty section's placeholder, key actKey/none or liveKey/none
	act     bool   // an Actionable or Live row, key "act:<id>" or "live:<id>": a Signal's twin, or a combined Initiative row
	live    bool   // a Live row (act too), key "live:<id>"
	title   string // the section header's text, or a placeholder's
	depth   int    // nesting: a top-level section header is 0; line() indents 2·depth columns
}

func (r ideaRow) header() bool { return r.head != "" }

// selectable is whether the cursor may rest on the row: not on a placeholder.
func (r ideaRow) selectable() bool { return !r.place }

// shadeIn reduces the row for the ancestor shade: an Actionable row or a Signal stands for
// its bead, an Initiative header for its Initiative (the twin of its combined row).
func (r ideaRow) shadeIn() shadeIn {
	switch {
	case r.cue:
		return shadeIn{}
	case r.header():
		s := shadeIn{grp: r.key, head: true}
		if r.init != nil {
			s.id = r.init.ID
		}
		return s
	case r.init != nil:
		return shadeIn{id: r.init.ID, act: r.act, live: r.live}
	case r.b != nil:
		return shadeIn{id: r.b.ID, act: r.act, live: r.live}
	}
	return shadeIn{}
}

// shaded is the row indexes of rows the ancestor shade paints for selection sel.
func (t ideaTab) shaded(f ideaFrame, sel int) map[int]bool {
	// No Initiative and Backlog start folded, so unfold them for the twin lookup.
	open := map[string]bool{"i:": true, backlogKey: true}
	full := slices.Concat(f.exp[:1], t.gate.ideaActionable(t.flip), f.live, ideaTree(f.exp[1:], open))
	set := shadeSet(t.gate.parentOf, f.rows, sel, full, ideaRow.shadeIn)
	// shadeSet tracks only the nearest header, so a member nested in a Backlog group
	// also lights the Backlog header, which is a section above its group (ADR 0179 §5).
	bi := slices.IndexFunc(f.rows, func(r ideaRow) bool { return r.key == backlogKey })
	if bi < 0 || sel < 0 || sel >= len(f.rows) {
		return set
	}
	s := f.rows[sel]
	nested := func(rs []ideaRow, i int) bool {
		j := slices.IndexFunc(rs, func(r ideaRow) bool { return r.key == backlogKey })
		return j >= 0 && i > j && rs[i].depth > 1
	}
	switch {
	case s.header() || s.cue || s.place || s.sec:
	case !s.act:
		if sel > bi && nested(f.rows, sel) {
			set[bi] = true
		}
	default:
		id := s.shadeIn().id
		if i := slices.IndexFunc(full, func(r ideaRow) bool { return !r.act && !r.header() && r.shadeIn().id == id }); i >= 0 && nested(full, i) {
			set[bi] = true
		}
	}
	return set
}

// foldable is whether space folds the row: a header, a drained hint row (its header) or
// the Actionable section.
func (r ideaRow) foldable() bool { return r.header() || r.cue || r.sec }

// launchable is a row enter launches or attaches the Initiative's cut session from.
func (r ideaRow) launchable() bool { return r.drained || r.cut }

// cueText is the drained hint row's wording; bodylessCueText is its bodyless-Initiative
// form (ADR 0182 §5).
const (
	cueText         = "all members closed · enter: Luffy cuts the next phase, or closes it"
	bodylessCueText = "no body yet · enter: Luffy settles its body with you, then cuts phase 1"
)

// bodyless is an unclosed Initiative whose description has no Done when: computed from the
// snapshot at render, no subprocess (ADR 0101; ADR 0182 §5).
func bodyless(b *bd.Bead) bool {
	return b != nil && b.Kind == contract.KindInitiative && b.Unclosed() && !bd.HasDoneWhen(b.Description)
}

// noRef is an unclosed Initiative with an empty external ref while Enforce external ID is on:
// a render-time marker, never stored, cleared by bd update <id> --external-ref R.
func (g graphData) noRef(b *bd.Bead) bool {
	return g.extRef && b != nil && b.Kind == contract.KindInitiative && b.Unclosed() && strings.TrimSpace(b.ExternalRef) == ""
}

// backlogKey is the Backlog header's key: it contains a NUL, so it is never a Signal ID.
const backlogKey = "\x00backlog"

// isBacklog is a listed Signal that is not in triage (in_progress, which ideaList
// lists only with a session) and sits at P3 or P4. It decides the flat Backlog Signals;
// a parked Initiative's members are placed by backlogInit.
func isBacklog(b bd.Bead) bool { return b.Status != "in_progress" && b.Priority >= 3 }

// backlogInit is whether the listed Initiative's whole group sits in the Backlog (ADR 0314,
// ADR 0315): it is unclosed at P3 or P4, has no cut session that is alive or launching/held
// (an exited session counts as none), and has no listed member in triage. Over the
// snapshot, sessions and held set, never over folds. triage is from triageInits.
func backlogInit(in bd.Bead, sessions map[string]session.Info, held func(string) bool, triage map[string]bool) bool {
	if in.Kind != contract.KindInitiative || in.Status == "closed" || in.Priority < 3 {
		return false
	}
	return sessions[in.ID].State != session.Alive && (held == nil || !held(in.ID)) && !triage[in.ID]
}

// triageInits are the Initiatives with a listed member in triage (in_progress), built once
// per pass so backlogInit stays constant-time per Initiative.
func triageInits(beads []bd.Bead, sessions map[string]session.Info, held func(string) bool) map[string]bool {
	out := map[string]bool{}
	for _, b := range beads {
		if b.Parent != "" && b.Status == "in_progress" && ideaListed(b, sessions, held) {
			out[b.Parent] = true
		}
	}
	return out
}

// listsInit is the one rule for which Initiatives Ideate lists (ADR 0177 §1): one with a
// listed member, a drained one, or an unclosed one with a cut session.
func listsInit(in bd.Bead, s session.Info, drained, hasMember bool) bool {
	return hasMember || drained || hasCutSession(in, s)
}

// hasCutSession is whether an unclosed Initiative has a cut session, alive or exited.
func hasCutSession(in bd.Bead, s session.Info) bool {
	return in.Unclosed() && s.Has()
}

// parkedInits are the listed Initiatives whose whole group sits in the Backlog.
func (g graphData) parkedInits(sessions map[string]session.Info, held func(string) bool) (out []bd.Bead) {
	triage := triageInits(g.beads, sessions, held)
	hasMember := map[string]bool{}
	for _, b := range g.beads {
		if b.Parent != "" && ideaListed(b, sessions, held) {
			hasMember[b.Parent] = true
		}
	}
	for _, in := range g.beads {
		if !backlogInit(in, sessions, held, triage) {
			continue
		}
		drained := bd.StateOf(in, g.beads) == bd.InitiativeDrained
		if listsInit(in, sessions[in.ID], drained, hasMember[in.ID]) {
			out = append(out, in)
		}
	}
	return
}

const noInitiative = "No Initiative"

// ideaFolded: every header starts folded but the Actionable and Live sections (ADR 0179,
// amending ADR 0178 §5); flip holds the keys the human has flipped from that default.
func ideaFolded(flip map[string]bool, key string) bool {
	return flip[key] != !isSectionKey(key)
}

// groupKey is the header key a listed Signal sits under: i:<parent> for a member of an
// unclosed P3/P4 Initiative (which has its own group, in the Initiatives section or the
// Backlog), else the Backlog key for a P3/P4 Signal, else i:<parent> under a P0–P2
// Initiative, else i: (No Initiative).
func groupKey(g graphData, b bd.Bead) string {
	j := g.beadIndex(b.Parent)
	init := j >= 0 && g.beads[j].Kind == contract.KindInitiative
	lowInit := init && g.beads[j].Unclosed() && g.beads[j].Priority >= 3
	switch {
	case init && (!isBacklog(b) || lowInit):
		return "i:" + b.Parent
	case isBacklog(b):
		return backlogKey
	}
	return "i:"
}

// ideaListed is whether ideaList lists b (before any fold): a non-event Signal that is
// open, or in_progress with a session (alive or exited). Status is tested explicitly
// because Bead.Open() counts a deferred Draft as open.
// A claimed Signal whose launch is held or in flight (held) is listed before its session shows.
func ideaListed(b bd.Bead, sessions map[string]session.Info, held func(string) bool) bool {
	return b.Kind == contract.KindSignal && !b.Event && (b.Status == "open" || b.Status == "in_progress" && (sessions[b.ID].Has() || held != nil && held(b.ID)))
}

// ideaList is the Ideate list: the create row, then each Initiative with members
// (beadCmp) and its Signals (beadCmp), then 'No Initiative', then the Backlog group;
// a folded group is its header alone. The Backlog holds the parked P3/P4 Initiatives
// (backlogInit) with their whole groups in beadCmp order, then the other P3/P4 Signals
// not in triage (isBacklog), listed only there. A Signal is kept when ideaListed.
func ideaList(beads []bd.Bead, sessions map[string]session.Info, flip map[string]bool) []ideaRow {
	return ideaListHeld(graphData{beads: beads}, sessions, nil, flip)
}

// onIdeaListHeld is nil outside tests: a test sets it to count ideaListHeld builds.
var onIdeaListHeld func()

func ideaListHeld(g graphData, sessions map[string]session.Info, held func(string) bool, flip map[string]bool) []ideaRow {
	if onIdeaListHeld != nil {
		onIdeaListHeld()
	}
	beads := g.beads
	inits := map[string]*bd.Bead{}
	for i, b := range beads {
		if b.Kind == contract.KindInitiative {
			inits[b.ID] = &beads[i]
		}
	}
	members := map[string][]*bd.Bead{}
	drained := map[string]bool{}
	for _, b := range beads {
		if b.Kind == contract.KindInitiative && b.Unclosed() && bd.StateOf(b, beads) == bd.InitiativeDrained {
			drained[b.ID] = true
		}
	}
	var loose, backlog []*bd.Bead
	for i, b := range beads {
		if !ideaListed(b, sessions, held) {
			continue
		}
		switch groupKey(g, b) {
		case backlogKey:
			backlog = append(backlog, &beads[i])
		case "i:":
			loose = append(loose, &beads[i])
		default:
			members[b.Parent] = append(members[b.Parent], &beads[i])
		}
	}
	byCmp := func(a, b *bd.Bead) int { return beadCmp(*a, *b) }
	var ids []string
	for id := range members {
		ids = append(ids, id)
	}
	for id := range drained {
		ids = append(ids, id) // a drained Initiative has no unclosed member, so no overlap
	}
	for id, in := range inits {
		if !drained[id] && len(members[id]) == 0 && hasCutSession(*in, sessions[id]) {
			ids = append(ids, id) // a cut session keeps its Initiative listed
		}
	}
	slices.SortStableFunc(ids, func(a, b string) int { return beadCmp(*inits[a], *inits[b]) })
	rows := []ideaRow{{create: true}}
	group := func(h ideaRow, ms []*bd.Bead) {
		if len(ms) == 0 {
			return
		}
		h.n = len(ms)
		if h.sec {
			h.depth = 0
		} else {
			h.depth = 1
		}
		rows = append(rows, h)
		if ideaFolded(flip, h.key) {
			return
		}
		slices.SortStableFunc(ms, byCmp)
		for _, b := range ms {
			rows = append(rows, ideaRow{key: b.ID, b: b, depth: h.depth + 1})
		}
	}
	// emitInit emits one Initiative's rows at depth 1: its header, then its members, the
	// drained hint row, or (a cut session with no members) the bare header.
	emitInit := func(id string) {
		h := ideaRow{key: "i:" + id, init: inits[id], head: inits[id].Title, depth: 1}
		if !drained[id] {
			h.cut = inits[id].Status != "closed" && sessions[id].Has()
			if h.cut && len(members[id]) == 0 {
				rows = append(rows, h) // no Signals, no hint row: nothing to fold
				return
			}
			group(h, members[id])
			return
		}
		h.drained = true
		rows = append(rows, h)
		if !ideaFolded(flip, h.key) {
			rows = append(rows, ideaRow{key: "h:" + id, init: h.init, drained: true, cue: true, depth: 2})
		}
	}
	var parked []string
	triage := triageInits(beads, sessions, held)
	for _, id := range ids {
		if backlogInit(*inits[id], sessions, held, triage) {
			parked = append(parked, id)
			continue
		}
		emitInit(id)
	}
	group(ideaRow{key: "i:", head: noInitiative, sec: true}, loose)
	if n := len(backlog) + len(parked); n > 0 {
		for _, id := range parked {
			n += len(members[id]) // a drained hint row is not counted
		}
		rows = append(rows, ideaRow{key: backlogKey, head: "Backlog", backlog: true, sec: true, n: n})
		if !ideaFolded(flip, backlogKey) {
			for _, id := range parked {
				emitInit(id)
			}
			slices.SortStableFunc(backlog, byCmp)
			for _, b := range backlog {
				r := ideaRow{key: b.ID, b: b, depth: 1}
				if p, ok := inits[b.Parent]; ok {
					r.hint = p.Title
				}
				rows = append(rows, r)
			}
		}
	}
	return rows
}

// ideaData is the slice of the shell's data Ideate reads, handed over before every
// update and view.
type ideaData struct {
	beads    []bd.Bead
	sessions map[string]session.Info
	gate     graphData // the shell's launch and action gates
	waiting  int       // sessions waiting on the human (listed in-triage Signals, cut sessions): the badge
	w, h     int       // the body's size
}

// ideaTab's state: the selected row by Signal id ("" is the create row), the
// detail pane and its scroll offset.
type ideaTab struct {
	ideaData
	sel  string
	open bool
	off  int
	flip map[string]bool // header keys flipped from ideaFolded's default, kept in path
	peek map[string]bool // keys a Recents jump flipped, with their pre-jump value: never saved
	path string          // the fold file
}

func (t ideaTab) afterLaunch(id string) component {
	t.sel = afterLaunch(t.sel, id, rowKeys(t.gate.ideaAct(), func(r ideaRow) string { return r.key }))
	return t
}

func (t ideaTab) count() int { return t.waiting }
func (t ideaTab) actN() int  { return len(t.gate.ideaAct()) }

// rows is the create row, the Actionable and Live sections, then the Initiatives, No
// Initiative and Backlog sections.
func (t ideaTab) rows() []ideaRow { return t.frame().rows }

// ideaFrame is what one Ideas-tab frame (or key) derives from the current snapshot: the
// fully expanded tree (fold-independent), the Live section built from it, and the rows
// shown under t.flip. Entry points build it once and pass it down; nothing keeps it.
type ideaFrame struct{ exp, live, rows []ideaRow }

func (t ideaTab) frame() ideaFrame {
	exp := t.gate.ideaExpanded()
	live := t.gate.ideaLive(exp, t.flip)
	tree := ideaListHeld(t.gate, t.sessions, t.gate.isHeld, t.flip)
	return ideaFrame{exp, live, slices.Concat(tree[:1], t.gate.ideaActionable(t.flip), live, ideaTree(tree[1:], t.flip))}
}

// ideaExpanded is ideaList with every header unfolded.
func (g graphData) ideaExpanded() []ideaRow {
	all := expandAll(func(f map[string]bool) []ideaRow { return ideaListHeld(g, g.sessions, g.isHeld, f) },
		func(f map[string]bool, r ideaRow) bool { return r.header() && ideaFolded(f, r.key) }, func(r ideaRow) string { return r.key })
	return ideaListHeld(g, g.sessions, g.isHeld, all)
}

// ideaTree turns ideaList's rows after the create row into the three tree sections: the
// Initiatives section over the Initiative groups, then No Initiative and Backlog, each
// present even when ideaList listed nothing for it.
func ideaTree(tree []ideaRow, flip map[string]bool) []ideaRow {
	at := slices.IndexFunc(tree, func(r ideaRow) bool { return r.sec })
	if at < 0 {
		at = len(tree)
	}
	n := 0
	for _, r := range tree[:at] {
		if r.header() {
			n++
		}
	}
	rows := section(tree[:at], flip, ideaRow{sec: true, key: treeKey, title: fmt.Sprintf("Initiatives (%d)", n)}, "no Initiatives", n > 0)
	rest := tree[at:]
	for _, h := range []ideaRow{{key: "i:", head: noInitiative}, {key: backlogKey, head: "Backlog", backlog: true}} {
		h.sec = true
		var body []ideaRow
		if i := slices.IndexFunc(rest, func(r ideaRow) bool { return r.key == h.key }); i >= 0 {
			h, body = rest[i], rest[i+1:]
			if j := slices.IndexFunc(body, func(r ideaRow) bool { return r.sec }); j >= 0 {
				body = body[:j]
			}
		}
		place := "no freestanding beads"
		if h.backlog {
			place = "nothing in the Backlog"
		}
		rows = append(rows, section(body, flip, h, place, h.n > 0)...)
	}
	return rows
}

// section is header h, then its body unless folded, or the placeholder when the body is empty.
func section(body []ideaRow, flip map[string]bool, h ideaRow, place string, full bool) []ideaRow {
	rows := []ideaRow{h}
	switch {
	case ideaFolded(flip, h.key):
	case !full:
		rows = append(rows, ideaRow{place: true, key: h.key + "/none", title: place, depth: 1})
	default:
		rows = append(rows, body...)
	}
	return rows
}

// ideaBands are the human's queue: the waiting band (in-triage Signals and cut sessions
// waiting on the human), then the frontier (open ready Signals above the Backlog, and
// drained Initiatives with no live cut session), then the paused band (rows carrying their
// own pause, otherwise ready: open, unblocked, not Live, not launching or held), each in
// beadCmp order (ADR 0180 §1). A P3 or P4 Initiative is placed only in the waiting band,
// while its cut session waits on the human; a Signal whose Initiative's group sits in the
// Backlog (backlogInit) is placed nowhere (ADR 0314 §2).
// Never a function of folds. It doesn't use band(): Initiative rows take their state from
// the cut session, not leafState.
func (g graphData) ideaBands() (wait, front, paused []ideaRow) {
	triage := triageInits(g.beads, g.sessions, g.isHeld)
	for i := range g.beads {
		b := &g.beads[i]
		switch {
		case ideaListed(*b, g.sessions, g.isHeld) && !isBacklog(*b):
			if j := g.beadIndex(b.Parent); j >= 0 && backlogInit(g.beads[j], g.sessions, g.isHeld, triage) {
				continue // its group is in the Backlog
			}
			r := ideaRow{key: "act:" + b.ID, b: b, act: true, depth: 1}
			switch g.leafState(b) {
			case sTurn, sAsk:
				if b.Status == "in_progress" { // an open Signal is not in triage
					wait = append(wait, r)
				}
			case sReady:
				front = append(front, r)
			case sPaused:
				if bd.Paused(*b) { // inherited-only pause stays out
					paused = append(paused, r)
				}
			}
		case b.Kind == contract.KindInitiative && b.Unclosed() && !g.launching[b.ID] && (g.launched[b.ID].IsZero() || g.sessions[b.ID].State == session.Alive): // a held launch is Live until its session shows alive
			s := g.sessions[b.ID]
			drained := bd.StateOf(*b, g.beads) == bd.InitiativeDrained
			r := ideaRow{key: "act:" + b.ID, init: b, act: true, drained: drained, cut: s.Has(), depth: 1}
			switch {
			case humanWait(s, false) != "":
				wait = append(wait, r)
			case b.Priority >= 3: // parked: neither frontier nor paused (ADR 0314 §2)
			case s.State != session.Alive && drained:
				front = append(front, r)
			case g.ownPausedReady(b):
				paused = append(paused, r)
			}
		}
	}
	seen := map[string]bool{}
	for _, r := range slices.Concat(wait, front, paused) {
		if r.b != nil {
			seen[r.b.ID] = true
		}
	}
	for _, b := range g.homePaused(tabIdeate, seen) { // Spec mc-f22wp FR 5; the Backlog rule (ADR 0314 §2) still holds
		if isBacklog(*b) {
			continue
		}
		if j := g.beadIndex(b.Parent); j >= 0 && backlogInit(g.beads[j], g.sessions, g.isHeld, triage) {
			continue
		}
		paused = append(paused, ideaRow{key: "act:" + b.ID, b: b, act: true, depth: 1})
	}
	bead := func(r ideaRow) bd.Bead {
		if r.b != nil {
			return *r.b
		}
		return *r.init
	}
	cmp := func(a, b ideaRow) int { return beadCmp(bead(a), bead(b)) }
	slices.SortStableFunc(wait, cmp)
	slices.SortStableFunc(front, cmp)
	slices.SortStableFunc(paused, cmp)
	return
}

// ideaAct is the three bands, concatenated.
func (g graphData) ideaAct() []ideaRow {
	wait, front, paused := g.ideaBands()
	return slices.Concat(wait, front, paused)
}

// ideaActionable is the Actionable header, then its rows or the placeholder while unfolded.
func (g graphData) ideaActionable(flip map[string]bool) []ideaRow {
	act := g.ideaAct()
	rows := []ideaRow{{sec: true, key: actKey, title: fmt.Sprintf("Actionable (%d)", len(act))}}
	if ideaFolded(flip, actKey) {
		return rows
	}
	if len(act) == 0 {
		return append(rows, ideaRow{place: true, key: actKey + "/none", depth: 1})
	}
	return append(rows, act...)
}

// ideaLive is the Live header, then its rows or the placeholder while unfolded: every
// Live effort among the expanded tree's Signals and Initiatives, in beadCmp order.
func (g graphData) ideaLive(exp []ideaRow, flip map[string]bool) []ideaRow {
	var cands []*bd.Bead
	for _, r := range exp {
		if b := cmp.Or(r.b, r.init); b != nil && !r.cue {
			cands = append(cands, b)
		}
	}
	act := idSet(g.ideaAct(), func(r ideaRow) *bd.Bead { return cmp.Or(r.b, r.init) })
	var live []ideaRow
	for _, b := range g.liveBeads(cands, act) {
		r := ideaRow{key: livePrefix + b.ID, act: true, live: true, depth: 1}
		if b.Kind == contract.KindInitiative {
			r.init, r.drained, r.cut = b, bd.StateOf(*b, g.beads) == bd.InitiativeDrained && b.Unclosed(), g.sessions[b.ID].Has()
		} else {
			r.b = b
			if j := g.beadIndex(b.Parent); j >= 0 && g.beads[j].Kind == contract.KindInitiative && isBacklog(*b) {
				r.hint = g.beads[j].Title
			}
		}
		live = append(live, r)
	}
	rows := []ideaRow{{sec: true, key: liveKey, title: fmt.Sprintf("Live (%d)", len(live))}}
	if ideaFolded(flip, liveKey) {
		return rows
	}
	if len(live) == 0 {
		return append(rows, ideaRow{place: true, key: liveNone, depth: 1})
	}
	return append(rows, live...)
}

// selIndex is the selected row's index. A selected Signal that folded away, or moved
// into a folded group on refresh, falls back to its group's header, or to its section's
// (the Backlog for a backlogged group) when that header is not shown either; a header
// i:<id> or hint row h:<id> likewise. One that left the list (closed, session gone) or is
// absent falls back to the create row.
func (t ideaTab) selIndex(rows []ideaRow) int {
	sel := t.sel
	if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == t.sel }); i >= 0 {
		return i
	}
	if id, ok := strings.CutPrefix(t.sel, "h:"); ok { // a vanished hint row falls back to its header
		if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.header() && r.key == "i:"+id }); i >= 0 {
			return i
		}
		return t.sectionFallback(rows, id)
	}
	if id, ok := strings.CutPrefix(t.sel, "i:"); ok && id != "" { // a header folded away under its section
		return t.sectionFallback(rows, id)
	}
	if o := otherSection(t.sel); o != "" { // moved between Actionable and Live
		if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == o }); i >= 0 {
			return i
		}
	}
	id, ok := strings.CutPrefix(t.sel, "act:")
	if !ok {
		id, ok = strings.CutPrefix(t.sel, livePrefix)
	}
	if ok { // left Actionable or Live: the tree twin, else the group header
		if i := slices.IndexFunc(rows, func(r ideaRow) bool { return !r.act && r.key == id }); i >= 0 {
			return i
		}
		if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.header() && r.key == "i:"+id }); i >= 0 {
			return i
		}
		if j := t.gate.beadIndex(id); j >= 0 && t.beads[j].Kind == contract.KindInitiative {
			return t.sectionFallback(rows, id) // an Initiative's combined row moved into a folded Backlog
		}
		sel = id
	}
	if j := t.gate.beadIndex(sel); j >= 0 && ideaListed(t.beads[j], t.sessions, t.gate.isHeld) {
		g := groupKey(t.gate, t.beads[j])
		if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.header() && r.key == g }); i >= 0 {
			return i
		}
		if id, ok := strings.CutPrefix(g, "i:"); ok && id != "" {
			return t.sectionFallback(rows, id)
		}
	}
	return t.treeFallback(rows)
}

// sectionFallback is where the cursor lands when the header of the group of Initiative id
// is not shown: the Backlog header for a backlogged group (ADR 0315 §9), else treeFallback.
func (t ideaTab) sectionFallback(rows []ideaRow, id string) int {
	if j := t.gate.beadIndex(id); j >= 0 && backlogInit(t.beads[j], t.sessions, t.gate.isHeld, triageInits(t.beads, t.sessions, t.gate.isHeld)) {
		if i := slices.IndexFunc(rows, func(r ideaRow) bool { return r.header() && r.key == backlogKey }); i >= 0 {
			return i
		}
	}
	return t.treeFallback(rows)
}

// treeFallback is where the cursor lands when its row is gone: the Initiatives header if
// that section is folded (the row may be folded away under it), else the create row.
func (t ideaTab) treeFallback(rows []ideaRow) int {
	if t.flip[treeKey] {
		return max(slices.IndexFunc(rows, func(r ideaRow) bool { return r.key == treeKey }), 0)
	}
	return 0
}

// cursor is the row under the cursor.
func (t ideaTab) cursor() ideaRow { return t.cursorIn(t.rows()) }

func (t ideaTab) cursorIn(rows []ideaRow) ideaRow { return rows[t.selIndex(rows)] }

// onCreate is whether the cursor is on the '+ New Signal' row.

// selected is the cursor's Signal, nil on the create row and every header.
func (t ideaTab) selected() *bd.Bead { return t.cursor().b }

var (
	keyTriage = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "triage"))
	keyNewRow = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "new"))
	keyCut    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "cut"))
	keyAttach = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "attach"))
	keyAdoptA = key.NewBinding(key.WithKeys("a", "A"), key.WithHelp("a/A", "adopt/release"))
)

// bindings are the hints (placement and drop rank: ADR 0229): only the verbs the selection
// offers (z on an open, unclaimed Signal, A in an Initiative).
func (t ideaTab) bindings() []key.Binding { return t.bindingsAt(t.cursor()) }

func (t ideaTab) bindingsAt(c ideaRow) []key.Binding {
	b := c.b
	var bs []key.Binding
	enter := keyCut
	if c.init != nil && t.sessions[c.init.ID].Has() {
		enter = keyAttach
	}
	switch {
	case c.sec:
		bs = append(bs, keyFold)
	case c.place:
	case c.act && c.init != nil: // the combined Initiative row: the drained hint row's hints
		bs = append(bs, enter)
		if !t.open {
			bs = append(bs, keyDetail)
		}
	case c.drained && c.header():
		bs = append(bs, enter, keyFold)
		if !t.open {
			bs = append(bs, keyDetail)
		}
	case c.cue:
		bs = append(bs, enter)
		if !t.open {
			bs = append(bs, keyDetail)
		}
	case c.cut && c.header():
		bs = append(bs, enter)
		if c.n > 0 {
			bs = append(bs, keyFold)
		}
		if !t.open {
			bs = append(bs, keyDetail)
		}
	case c.header():
		bs = append(bs, keyFold)
		if c.init != nil && !t.open {
			bs = append(bs, keyDetail)
		}
	case b == nil:
		bs = append(bs, keyNewRow)
	default:
		if t.gate.launchWhy(b) == "" {
			bs = append(bs, keyTriage)
			if t.sessions[b.ID].State != session.Alive {
				bs = append(bs, keyBg)
			}
		}
		if !t.open {
			bs = append(bs, keyDetail)
		}
	}
	if t.open {
		bs = append(bs, keyShut, keyScroll)
	}
	if b == nil {
		if c.init != nil { // an Initiative row: +/- bumps the Initiative, X cancels it
			bs = append(bs, keyPrio)
			if isOpenInitiative(c.init) {
				bs = append(bs, keyCancelKey)
			}
		}
		return append(bs, keySection)
	}
	if rejectable(*b) {
		bs = append(bs, keyReject)
	}
	if b.Unclosed() {
		bs = append(bs, verbKey("p", pauseLabel(b)))
	}
	if l := t.gate.xHint(b); l != "" {
		bs = append(bs, verbKey("x", l))
	}
	if t.gate.xOffers(b) {
		bs = append(bs, xBinding(b))
	}
	bs = append(bs, keyTranscript)
	bs = append(bs, keyEdit, keyPrio)
	adopt, release := canAdopt(t.beads, b), canRelease(t.beads, b)
	switch {
	case adopt && release:
		bs = append(bs, keyAdoptA)
	case adopt:
		bs = append(bs, keyAdopt)
	case release:
		bs = append(bs, keyRelease)
	}
	return append(bs, keySection)
}

// extra is moving, then every action key the selection does not offer, which
// answers with a status line; shown in ? alone.
func (t ideaTab) extra() []key.Binding {
	var out []key.Binding
	rows := t.rows()
	c := t.cursorIn(rows)
	if len(rows) >= 2 {
		out = append(out, keyMove)
	}
	shown := map[string]bool{}
	for _, b := range t.bindingsAt(c) {
		for _, k := range b.Keys() {
			shown[k] = true
		}
	}
	if c.cue {
		out = append(out, keyFold) // folds the header; not hinted on the row itself
	}
	if c.b == nil {
		return append(out, keyBg)
	}
	for _, b := range []key.Binding{keyTriage, keyBg, keyReject, keyPauseKey, keyAdopt, keyRelease} {
		if !shown[b.Keys()[0]] {
			out = append(out, b)
		}
	}
	return out
}

// ideaKey routes the action keys through the shared paths over the selected Signal;
// ok is false for any other key. The create row takes enter and n alone, and u, b, t
// and x are Graph's; p toggles the Signal's manual pause.
func (m Model) ideaKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	t := m.tabAt(m.tab).(ideaTab)
	c := t.cursorIn(t.rows())
	b := c.b
	if m.focus.id != "" { // as selection(): the shared action paths act on the focus's cursor row
		b = m.focusBead()
	}
	switch {
	case key.Matches(k, keyFold) && c.foldable():
		next, cmd := m.tabKey(k)
		nm := next.(Model)
		nt := nm.tabAt(nm.tab).(ideaTab)
		if err := nt.save(); err != nil {
			nm.setStatus("folds won't survive a restart: " + err.Error())
		}
		return nm, cmd, true
	case b == nil && key.Matches(k, keyLaunch) && c.create:
		return m.openNewSignal()
	case b == nil && (key.Matches(k, keyLaunch) || key.Matches(k, keyBg)) && c.launchable():
		next, cmd := m.launchAs(m.gateData(), *c.init, key.Matches(k, keyBg))
		return next, cmd, true
	case b == nil && key.Matches(k, keyLaunch) && c.header():
		if c.init != nil {
			m.setStatus("members still open · cut from Graph")
		} else {
			m.setStatus("space folds a group")
		}
		return m, nil, true
	case b == nil && key.Matches(k, keyBg):
		m.setStatus("nothing to launch here")
		return m, nil, true
	case b == nil && c.init != nil && key.Matches(k, keyPrio): // Initiative rows carry no Signal
		next, cmd := m.bumpPriority(*c.init, prioDir(k))
		return next, cmd, true
	case b == nil && c.init != nil && key.Matches(k, keyWithdrawKey):
		return m.actionKey(k)
	case b == nil:
		return m, nil, false
	case key.Matches(k, keyLaunch), key.Matches(k, keyBg), key.Matches(k, keyPrio), key.Matches(k, keyPauseKey):
		return m.actionKey(k)
	case key.Matches(k, keyDropKey) && m.gateData().xHint(b) != "": // unhinted x does nothing
		return m.actionKey(k)
	case key.Matches(k, keyWithdrawKey):
		return m.actionKey(k)
	case key.Matches(k, keyEdit), key.Matches(k, keyReject), key.Matches(k, keyAdopt), key.Matches(k, keyRelease):
		return m.graphKey(k)
	}
	return m, nil, false
}

func (t ideaTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	rows := t.rows()
	c := t.cursorIn(rows)
	switch {
	case key.Matches(k, keyFold) && c.foldable():
		if c.cue {
			c.key = "i:" + c.init.ID // the hint row folds its header, then the cursor follows
		}
		t.flip = maps.Clone(t.flip) // tab state is a value type
		if t.flip == nil {
			t.flip = map[string]bool{}
		}
		if t.flip[c.key] = !t.flip[c.key]; !t.flip[c.key] {
			delete(t.flip, c.key) // back at the default
		}
		t.peek = unpeek(t.peek, c.key)
		t.sel, t.off = c.key, 0
	case key.Matches(k, keyMove):
		if len(rows) < 2 {
			return t, nil
		}
		d := 1
		if s := k.String(); s == "k" || s == "up" {
			d = -1
		}
		t.sel, t.off = rows[stepTo(rows, t.selIndex(rows), d, ideaRow.selectable)].key, 0
	case key.Matches(k, keySection):
		d := 1
		if s := k.String(); s == "K" || s == "ctrl+up" {
			d = -1
		}
		t.sel, t.off = rows[jumpTo(rows, t.selIndex(rows), d, func(r ideaRow) bool { return r.sec })].key, 0
	case t.open && key.Matches(k, keyShut):
		t.open = false
	case !t.open && shownOf(c) != nil && key.Matches(k, keyDetail):
		t.open = true
	case t.open && key.Matches(k, keyScroll):
		vp := t.detailVP(c)
		switch k.String() {
		case "pgup":
			vp.PageUp()
		case "pgdown":
			vp.PageDown()
		case "ctrl+u":
			vp.HalfPageUp()
		default:
			vp.HalfPageDown()
		}
		t.off = vp.YOffset
	}
	return t, nil
}

func (t ideaTab) detailVP(c ideaRow) viewport.Model {
	return drawerVP(t.open, t.w, t.h, t.detailText(c), t.off)
}

// shownOf is the bead the detail pane renders for cursor row c: its Signal or Initiative header.
func shownOf(c ideaRow) *bd.Bead {
	if c.b != nil {
		return c.b
	}
	return c.init
}

// detailText is the cursor row c's detail.
func (t ideaTab) detailText(c ideaRow) string {
	if c.sec && c.key == liveKey {
		return liveHeaderText(c.title)
	}
	switch c.key {
	case treeKey:
		return c.title + "\n\nThe Initiative groups.\n\nenter does nothing here (space folds)."
	case "i:":
		return c.head + "\n\nThe Signals that belong to no Initiative.\n\nenter does nothing here (space folds)."
	case backlogKey:
		return c.head + "\n\nThe parked P3 and P4 Initiatives with their whole groups, then the other P3 and P4 Signals not in triage.\n\nenter does nothing here (space folds)."
	}
	if c.sec {
		return c.title + "\n\nThe Signals and Initiatives waiting on you.\n\nenter does nothing here (space folds)."
	}
	s := t.beadText(shownOf(c))
	if c.launchable() { // Graph's preview, so it agrees with enter (ADR 0146 §3)
		if p := (graphTab{graphData: t.gate}).launchPreview(shownOf(c)); p != "" {
			s += "\n\n" + p
		}
	}
	return s
}

// beadText is b's SBRN, its parent and labels fields, then the shared bead body.
func (t ideaTab) beadText(b *bd.Bead) string {
	if b == nil {
		return softSt.Render("+ New Signal")
	}
	parent := "—"
	for _, p := range t.beads {
		if p.ID == b.Parent {
			parent = p.Title
		}
	}
	labels := "—"
	if len(b.Labels) > 0 {
		labels = strings.Join(b.Labels, ", ")
	}
	return t.gate.sbrn(b, b.Title, true, 0) + "\n\n" + labelField("parent", parent) + "\n" + labelField("labels", labels) + "\n\n" +
		t.gate.beadBody(b, true)
}

// line is a row's left part and right-edge suffix, in Defog's anatomy.
// gutter is a row's blocker mark; a folded header hides its group's listed Signals.
func (t ideaTab) gutter(r ideaRow, blockers map[string]bool) string {
	var hidden []bd.Bead
	if r.header() && ideaFolded(t.flip, r.key) {
		hidden = t.listedUnder(r.key)
		if r.key == backlogKey { // the nested Initiatives are hidden beads too
			hidden = slices.Concat(hidden, t.gate.parkedInits(t.sessions, t.gate.isHeld))
		}
	}
	id := ""
	if r.b != nil {
		id = r.b.ID
	}
	return blockerGutter(blockers, id, hidden)
}

// listedUnder are the beads a folded header hides: the listed Signals under the header
// key; for the Backlog, also each nested Initiative's whole subgraph by Parent (the
// Initiatives themselves, which a header's own trailer does not count, are parkedInits).
func (t ideaTab) listedUnder(key string) (out []bd.Bead) {
	for _, b := range t.beads {
		if ideaListed(b, t.sessions, t.gate.isHeld) && groupKey(t.gate, b) == key {
			out = append(out, b)
		}
	}
	if key == backlogKey {
		for _, in := range t.gate.parkedInits(t.sessions, t.gate.isHeld) {
			out = append(out, subgraph(t.gate, in.ID)...)
		}
	}
	return
}

// trailer is a folded header's " · N open · M live" (ADR 0179): an Initiative counts its
// whole open subgraph by Parent, No Initiative its listed Signals, Backlog its flat Signals
// plus each nested Initiative's open subgraph (listedUnder). A drained
// Initiative reads "drained" for the open part and counts its own session as live.
func (t ideaTab) trailer(r ideaRow) string {
	switch {
	case r.drained:
		return t.gate.trailer(slices.Concat([]bd.Bead{*r.init}, subgraph(t.gate, r.init.ID)), bodyless(r.init), true)
	case r.init != nil:
		return t.gate.trailer(subgraph(t.gate, r.init.ID), false, false)
	}
	return t.gate.trailer(t.listedUnder(r.key), false, false)
}

func (t ideaTab) line(r ideaRow, blockers map[string]bool) (string, string) {
	g := t.gate
	gut := t.gutter(r, blockers)
	ind := strings.Repeat("  ", r.depth)
	const slot = "  " // the blank caret slot a row that cannot fold keeps, so siblings share a column
	switch {
	case r.create:
		return gut + "  " + brand.Render("+ New Signal"), ""
	case r.sec:
		fold := "▾ "
		if ideaFolded(t.flip, r.key) {
			fold = "▸ "
		}
		switch {
		case r.key == liveKey: // the count in the live ●'s green
			return gut + ind + dimSt.Render(fold) + liveTitle(r.title), ""
		case r.key == actKey:
			return gut + ind + dimSt.Render(fold) + selMarker.Render(r.title), ""
		case r.header(): // No Initiative and Backlog: the count, and the folded trailer
			trail := ""
			if ideaFolded(t.flip, r.key) {
				trail = t.trailer(r)
			}
			return gut + ind + dimSt.Render(fold) + sectionSt.Render(fmt.Sprintf("%s (%d)", r.head, r.n)) + trail, ""
		}
		return gut + ind + dimSt.Render(fold) + sectionSt.Render(r.title), ""
	case r.place:
		switch {
		case r.title != "":
			return gut + ind + slot + dimSt.Render(r.title), ""
		case r.key == liveNone:
			return gut + ind + slot + dimSt.Render(liveEmpty), ""
		}
		return gut + ind + slot + dimSt.Render("nothing waits on you"), ""
	case r.act && r.init != nil: // the combined Initiative row, a leaf (ADR 0180 §2)
		l := gut + ind + slot + g.sbrn(r.init, r.init.Title, true, 0)
		if r.drained {
			if bodyless(r.init) {
				l += softSt.Render(" · no body yet")
			} else {
				l += softSt.Render(" · all members closed")
			}
		}
		return l, strings.TrimLeft(g.rowSuffix(r.init), " ")
	case r.header():
		fold, n, trail := "▾ ", "", ""
		folded := ideaFolded(t.flip, r.key) && (r.n > 0 || r.drained) // a cut Initiative with no Signals has nothing to fold, so no trailer either (judgement: its row is a leaf)
		if folded {
			fold, trail = "▸ ", t.trailer(r)
		}
		if folded && r.n > 0 {
			n = fmt.Sprintf(" (%d)", r.n)
		}
		title := r.head
		if r.init != nil && r.init.Status == "closed" {
			n = " (closed)" + n // FR15: extras follow the id
		}
		suffix := ""
		if r.launchable() {
			suffix = strings.TrimLeft(g.rowSuffix(r.init), " ")
		}
		if r.init != nil {
			head, tail := g.sbrnParts(r.init, true, 0)
			return gut + ind + dimSt.Render(fold) + head + dimSt.Render(title) + tail + dimSt.Render(n) + trail, suffix
		}
		return gut + ind + dimSt.Render(fold) + dimSt.Render(title+n) + trail, suffix
	case r.cue:
		text := cueText
		if bodyless(r.init) {
			text = bodylessCueText
		}
		return gut + ind + slot + g.stateGlyph(r.init) + " " + softSt.Render(text), ""
	}
	b := r.b
	title := b.Title
	if blockers[b.ID] {
		title = pinkFg.Render(title)
	}
	l := gut + ind + slot + g.sbrn(b, title, true, 0)
	if r.hint != "" {
		l += sep() + softSt.Render(r.hint)
	}
	return l, strings.TrimLeft(g.rowSuffix(b), " ")
}

func (t ideaTab) view(w, h int) string {
	f := t.frame()
	rows := f.rows
	sel := t.selIndex(rows)
	cur := rows[sel]
	blockers := t.gate.blockerSet(cur.b)
	row := func(i int) (string, string) { return t.line(rows[i], blockers) }
	shade := t.shaded(f, sel)
	lines, selRow := make([]string, len(rows)), sel
	ids := make([]string, len(rows))
	liveAt, treeAt, noInitAt, backlogAt := 0, 0, 0, 0 // the section headers that get a blank line above
	for i, r := range rows {
		lines[i] = strconv.Itoa(i)
		if r.b != nil {
			ids[i] = r.b.ID
		}
		switch r.key {
		case liveKey:
			liveAt = i
		case treeKey:
			treeAt = i
		case "i:":
			noInitAt = i
		case backlogKey:
			backlogAt = i
		}
	}
	lines, ids, sel = gapAt(lines, ids, sel, backlogAt) // a blank line above each section header
	lines, ids, sel = gapAt(lines, ids, sel, noInitAt)
	lines, ids, sel = gapAt(lines, ids, sel, treeAt)
	lines, ids, sel = gapAt(lines, ids, sel, liveAt)
	lines, ids, sel = gapAt(lines, ids, sel, 1) // and one under '+ New Signal'
	var footer func(off, lh int) []string
	if b := cur.b; b != nil {
		footer = blockerFooter(t.gate, t.gate.openBlockers(b), ids, w)
	}
	return drawRows(lines, row, shade, sel, selRow, t.open, w, h, t.detailVP(cur), footer)
}

func (t ideaTab) drawerOpen() bool { return t.open }

// drawerBead is the Signal or Initiative the open drawer describes.
func (t ideaTab) drawerBead() *bd.Bead { return shownOf(t.cursor()) }

// isHeld is whether id's launch is in flight or held in Live awaiting its session.
func (g graphData) isHeld(id string) bool { return g.launching[id] || !g.launched[id].IsZero() }
