// Package bdtest is an in-memory Bead Graph behind bd.Core. New(t) installs it as bd's
// process default for the test's life, so every bd free function, and every package var
// that still points at one (session's showBead = bd.Show), reads the seeded graph with no
// var stubbed. While it is installed the bd CLI is sealed: a helper not yet on the core
// fails its test instead of reaching the live graph (ADR 0380, mugicrew-the-bd-core-types-
// its-write-class…, and the test-seam ADR). Its methods are safe across goroutines.
package bdtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// Actor is the bd actor every fake claim is taken as.
const Actor = "bdtest"

// Fake is a seeded graph.
type Fake struct {
	t        testing.TB
	mu       sync.Mutex
	beads    map[string]*bd.Bead
	comments map[string][]bd.Comment
	children map[string]int      // children minted per parent, for dotted ids
	supers   map[string][]string // supersedes edges, from → to: bd.Bead models no such edge
	fail     map[string]error    // "verb id" (or "verb *") → injected error
	calls    []string
	minted   int
}

// New seeds a graph with beads and makes it bd's core until the test ends. It pins the bd
// actor to Actor, so bd.Mine() and the fake's claims agree.
func New(t testing.TB, beads ...bd.Bead) *Fake {
	f := &Fake{t: t, beads: map[string]*bd.Bead{}, comments: map[string][]bd.Comment{}, supers: map[string][]string{}, children: map[string]int{}, fail: map[string]error{}}
	f.Seed(beads...)
	t.Cleanup(bd.Use(f))
	t.Cleanup(bd.PinActor(Actor))
	return f
}

// Seed adds (or replaces) beads, mid-test too.
func (f *Fake) Seed(beads ...bd.Bead) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range beads {
		b = clone(b)
		f.beads[b.ID] = &b
	}
}

// Claimed is b as this process holds it: in_progress, assigned to Actor.
func Claimed(b bd.Bead) bd.Bead {
	b.Status, b.Assignee = "in_progress", Actor
	return b
}

// Draft is b as creator's Draft: deferred, labelled drafted-by:<creator>.
func Draft(creator string, b bd.Bead) bd.Bead {
	b.Status = "deferred"
	b.Labels = append(slices.Clone(b.Labels), "drafted-by:"+creator)
	return b
}

// Mutate edits a bead in place, as another writer would mid-test.
func (f *Fake) Mutate(id string, edit func(*bd.Bead)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	edit(f.must(id))
}

// FailOn makes every verb call on id ("*" for any id) return err until Heal. verb is a Core
// method name: "Show", "List", "Ready", …
func (f *Fake) FailOn(verb, id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[verb+" "+id] = err
}

// Heal clears FailOn(verb, id).
func (f *Fake) Heal(verb, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.fail, verb+" "+id)
}

// Bead is id's current state; the test fails if it is absent.
func (f *Fake) Bead(id string) bd.Bead {
	f.mu.Lock()
	defer f.mu.Unlock()
	return clone(*f.must(id))
}

// Calls is every Core call so far as "Verb id,id", in order; a last resort for tests whose
// subject is ordering.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// CallIndex is the position in Calls of the first call satisfying match, or -1.
func (f *Fake) CallIndex(match func(call string) bool) int {
	return slices.IndexFunc(f.Calls(), match)
}

// EdgeBeforeClose reports whether the AddEdge from→to call precedes the first
// Close or Batch call touching id; both calls must exist.
func (f *Fake) EdgeBeforeClose(from, to, id string) bool {
	edge := f.CallIndex(func(c string) bool { return c == "AddEdge "+from+","+to })
	closed := f.CallIndex(func(c string) bool {
		return (strings.HasPrefix(c, "Close ") || strings.HasPrefix(c, "Batch ")) && strings.Contains(c, id)
	})
	return edge >= 0 && closed >= 0 && edge < closed
}

// enter logs the call and returns an injected fault, under f.mu.
func (f *Fake) enter(verb string, ids ...string) error {
	f.calls = append(f.calls, strings.TrimSpace(verb+" "+strings.Join(ids, ",")))
	if err := f.fail[verb+" *"]; err != nil {
		return err
	}
	for _, id := range ids {
		if err := f.fail[verb+" "+id]; err != nil {
			return err
		}
	}
	return nil
}

func (f *Fake) must(id string) *bd.Bead {
	b, ok := f.beads[id]
	if !ok {
		f.t.Fatalf("bdtest: no bead %q seeded", id)
	}
	return b
}

// notFound is bd 1.3.0's refusal for an unknown id; bd.IsNotFound reads "not found".
func notFound(id string) error {
	return fmt.Errorf("bd show %s --json: exit status 1: Issue %s not found", id, id)
}

// noIssueMatching is bd 1.3.0's refusal of an update or unclaim of an unknown id, worded
// differently from show's; bd.IsNotFound reads it too.
func noIssueMatching(id string) error {
	return fmt.Errorf("bd update %s: exit status 1: Error resolving %s: no issue found matching %q", id, id, id)
}

func (f *Fake) Show(id string) (bd.Bead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Show", id); err != nil {
		return bd.Bead{}, err
	}
	b, ok := f.beads[id]
	if !ok {
		return bd.Bead{}, notFound(id)
	}
	return clone(*b), nil
}

// List models bd list: the default status set is every non-closed status, an explicit Limit
// truncates, and no Limit is unlimited.
func (f *Fake) List(q bd.Query) ([]bd.Bead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("List"); err != nil {
		return nil, err
	}
	var out []bd.Bead
	for _, id := range sortedKeys(f.beads) {
		b := f.beads[id]
		switch {
		case len(q.Status) > 0 && !slices.Contains(q.Status, b.Status):
		case len(q.Status) == 0 && !q.All && b.Status == "closed":
		case len(q.IDs) > 0 && !slices.Contains(q.IDs, b.ID):
		case q.Parent != "" && b.Parent != q.Parent:
		case q.Label != "" && !b.HasLabel(q.Label):
		case q.LabelPattern != "" && !slices.ContainsFunc(b.Labels, func(l string) bool { return globMatch(q.LabelPattern, l) }):
		case q.ExternalRef != "" && b.ExternalRef != q.ExternalRef:
		default:
			out = append(out, clone(*b))
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// globMatch is bd's label pattern: * matches any run of characters.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// blockedBy is whether b waits on an unclosed blocker. A blocker absent from the graph does not block.
func (f *Fake) blockedBy(b *bd.Bead) bool {
	for _, id := range b.BlockedBy {
		if o, ok := f.beads[id]; ok && o.Status != "closed" {
			return true
		}
	}
	return false
}

// Ready is bd's readiness rule: an open bead with no unclosed blocker, itself or on any
// ancestor (bd ready hides a blocked container's members), uncapped like the core's bd ready -n 0.
func (f *Fake) Ready() ([]bd.Bead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Ready"); err != nil {
		return nil, err
	}
	var out []bd.Bead
	for _, id := range sortedKeys(f.beads) {
		b := f.beads[id]
		if b.Status != "open" {
			continue
		}
		hidden := false
		seen := map[string]bool{}
		for c := b; c != nil && !seen[c.ID]; c = f.beads[c.Parent] {
			seen[c.ID] = true
			if f.blockedBy(c) {
				hidden = true
				break
			}
		}
		if !hidden {
			out = append(out, clone(*b))
		}
	}
	slices.SortStableFunc(out, func(a, b bd.Bead) int { return a.Priority - b.Priority })
	return out, nil
}

// edgesFrom is b's outgoing edges as (target, type) pairs.
func edgesFrom(b *bd.Bead) [][2]string {
	var es [][2]string
	if b.Parent != "" {
		es = append(es, [2]string{b.Parent, "parent-child"})
	}
	for _, id := range b.BlockedBy {
		if slices.Contains(b.CondBlockedBy, id) {
			es = append(es, [2]string{id, "conditional-blocks"})
		} else {
			es = append(es, [2]string{id, "blocks"})
		}
	}
	for _, id := range b.Related {
		es = append(es, [2]string{id, "related"})
	}
	if b.CausedBy != "" {
		es = append(es, [2]string{b.CausedBy, "caused-by"})
	}
	for _, id := range b.DiscoveredFrom {
		es = append(es, [2]string{id, "discovered-from"})
	}
	return es
}

// Edges lists the beads at the far end of id's edges: what id depends on (Down) or what
// depends on id (Up), each with its dependency type.
func (f *Fake) Edges(id string, dir bd.Dir, typ string) ([]bd.EdgeRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Edges", id); err != nil {
		return nil, err
	}
	var rows []bd.EdgeRow
	add := func(far *bd.Bead, t string) {
		if far != nil && (typ == "" || typ == t) {
			rows = append(rows, bd.EdgeRow{Bead: clone(*far), Type: t})
		}
	}
	from := func(oid string) [][2]string {
		es := edgesFrom(f.beads[oid])
		for _, to := range f.supers[oid] {
			es = append(es, [2]string{to, "supersedes"})
		}
		return es
	}
	if dir == bd.Down {
		if _, ok := f.beads[id]; ok {
			for _, e := range from(id) {
				add(f.beads[e[0]], e[1])
			}
		}
		return rows, nil
	}
	for _, oid := range sortedKeys(f.beads) {
		for _, e := range from(oid) {
			if e[0] == id {
				add(f.beads[oid], e[1])
			}
		}
	}
	return rows, nil
}

func (f *Fake) Comments(id string) ([]bd.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Comments", id); err != nil {
		return nil, err
	}
	if _, ok := f.beads[id]; !ok {
		return nil, notFound(id)
	}
	return slices.Clone(f.comments[id]), nil
}

// AddEdge records from → to of type typ. It runs bd dep add's CLI pre-check on dotted IDs, then
// the storage rules addEdge shares with Supersede and Batch.
func (f *Fake) AddEdge(from, to, typ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("AddEdge", from, to); err != nil {
		return err
	}
	// bd's CLI pre-check (cmd/bd/dep.go isDisallowedHierarchicalDependency): any edge from a dotted
	// descendant onto its dotted ancestor, bar a parent-child onto the immediate parent. Only dep add
	// runs it; create --deps does not.
	if strings.HasPrefix(from, to+".") && !(typ == "parent-child" && !strings.Contains(strings.TrimPrefix(from, to+"."), ".")) {
		return fmt.Errorf("bd dep add %s %s -t %s: exit status 1: cannot add dependency: %s is already a child of %s. Children inherit dependency on parent completion via hierarchy. Adding an explicit dependency would create a deadlock", from, to, typ, from, to)
	}
	return f.addEdge(from, to, typ)
}

// held is the type of the edge from → to, or "" when the pair holds none; one ordered pair
// holds one edge, whatever its type.
func (f *Fake) held(from, to string) string {
	for _, e := range f.edgesOf(from) {
		if e[0] == to {
			return e[1]
		}
	}
	return ""
}

// edgesOf is id's outgoing edges, supersedes included.
func (f *Fake) edgesOf(id string) [][2]string {
	b, ok := f.beads[id]
	if !ok {
		return nil
	}
	es := edgesFrom(b)
	for _, to := range f.supers[id] {
		es = append(es, [2]string{to, "supersedes"})
	}
	return es
}

// isAncestor is whether anc is a proper ancestor of id, through parent edges.
func (f *Fake) isAncestor(anc, id string) bool {
	seen := map[string]bool{}
	for b := f.beads[id]; b != nil && b.Parent != "" && !seen[b.ID]; b = f.beads[b.Parent] {
		seen[b.ID] = true
		if b.Parent == anc {
			return true
		}
	}
	return false
}

// reaches is whether to leads back to from along blocks, conditional-blocks and parent-child
// edges, the only ones bd's cycle check walks.
func (f *Fake) reaches(start, goal string) bool {
	seen := map[string]bool{}
	var walk func(id string) bool
	walk = func(id string) bool {
		if id == goal {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, e := range f.edgesOf(id) {
			if (e[1] == "blocks" || e[1] == "conditional-blocks" || e[1] == "parent-child") && walk(e[0]) {
				return true
			}
		}
		return false
	}
	return walk(start)
}

// addEdge is AddEdge's write, under f.mu, with bd's storage rules: a same-type re-add is a no-op,
// a pair holds one edge, and a blocking edge may not tie a bead to its ancestor, its descendant
// or a cycle.
func (f *Fake) addEdge(from, to, typ string) error {
	b, ok := f.beads[from]
	if !ok {
		return notFound(from)
	}
	if _, ok := f.beads[to]; !ok {
		return notFound(to)
	}
	refuse := func(format string, a ...any) error {
		return fmt.Errorf("bd dep add %s %s -t %s: exit status 1: "+format, append([]any{from, to, typ}, a...)...)
	}
	if typ == "blocks" || typ == "conditional-blocks" {
		switch {
		case f.isAncestor(to, from):
			return refuse("%s cannot be blocked by its ancestor %s: %s cannot close until its descendants finish, so the gate would never clear", from, to, to)
		case f.isAncestor(from, to):
			return refuse("%s cannot be blocked by its descendant %s: blocked status cascades to descendants, so %s would inherit the block and never close", from, to, to)
		}
	}
	switch held := f.held(from, to); {
	case held == typ:
		return nil
	case held != "":
		return refuse("dependency %s -> %s already exists with type %q (requested %q); remove it first with 'bd dep remove' then re-add", from, to, held, typ)
	}
	if (typ == "blocks" || typ == "conditional-blocks" || typ == "parent-child") && f.reaches(to, from) {
		return refuse("adding dependency would create a cycle")
	}
	switch typ {
	case "blocks":
		b.BlockedBy = append(b.BlockedBy, to)
	case "conditional-blocks":
		b.BlockedBy = append(b.BlockedBy, to)
		b.CondBlockedBy = append(b.CondBlockedBy, to)
	case "related":
		b.Related = append(b.Related, to)
	case "caused-by":
		b.CausedBy = to
	case "discovered-from":
		b.DiscoveredFrom = append(b.DiscoveredFrom, to)
	case "parent-child":
		b.Parent = to
	case "supersedes":
		f.supers[from] = append(f.supers[from], to)
	default:
		return fmt.Errorf("bdtest: edge type %q not modelled", typ)
	}
	return nil
}

// Create mints a bead; the contract suite seeds its graphs with it.
func (f *Fake) Create(s bd.CreateSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Create"); err != nil {
		return "", err
	}
	if s.Serializer != nil { // a create without one is a plain single insert
		if err := f.serialized("Create "+s.Title, s.Serializer); err != nil {
			return "", err
		}
	}
	for _, d := range s.Deps {
		if d.From != "" {
			return "", fmt.Errorf("bdtest: create dep %s -> %s: From must be empty", d.From, d.To)
		}
		if _, ok := f.beads[d.To]; !ok {
			return "", notFound(d.To)
		}
	}
	minted, kids := f.minted, f.children[s.Parent]
	f.minted++
	id := "bdct-" + strconv.Itoa(f.minted)
	if s.Parent != "" { // bd mints a child's id under its parent's: parent.N
		for { // skip suffixes a seeded child already holds; never overwrite one
			f.children[s.Parent]++
			id = s.Parent + "." + strconv.Itoa(f.children[s.Parent])
			if _, taken := f.beads[id]; !taken {
				break
			}
		}
	}
	b := bd.Bead{ID: id, Status: "open", Title: s.Title, Description: s.Description,
		Parent: s.Parent, Labels: slices.Clone(s.Labels), ExternalRef: s.ExternalRef, Container: s.Type == "epic"}
	b.Priority = 2 // bd's default when a create names none
	if s.Priority != nil {
		b.Priority = *s.Priority
	}
	if s.Deferred {
		b.Status = "deferred"
	}
	for _, l := range b.Labels {
		if k, ok := strings.CutPrefix(l, "kind:"); ok {
			b.Kind = contract.Kind(k)
		}
	}
	f.beads[b.ID] = &b
	for _, d := range s.Deps { // the edge rules AddEdge runs, bar the dotted-ID pre-check (create --deps skips it)
		typ := d.Type
		if typ == "" {
			typ = "blocks"
		}
		if err := f.addEdge(b.ID, d.To, typ); err != nil { // a refused Dep leaves no bead
			delete(f.beads, b.ID)
			f.minted = minted
			if s.Parent != "" {
				f.children[s.Parent] = kids
			}
			return "", err
		}
	}
	return b.ID, nil
}

func (f *Fake) AddLabels(id string, labels ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("AddLabels", id); err != nil {
		return err
	}
	b, ok := f.beads[id]
	if !ok {
		return notFound(id)
	}
	for _, l := range labels {
		if !b.HasLabel(l) {
			b.Labels = append(b.Labels, l)
		}
	}
	return nil
}

func (f *Fake) Close(ids []string, reason string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Close", ids...); err != nil {
		return err
	}
	if !force { // bd refuses a plain close of a bead with an open blocker or child; supersede does not
		for _, id := range ids {
			if b, ok := f.beads[id]; ok && b.Status != "closed" {
				// bd 1.3.0 counts any child not closed (open, in_progress, deferred)
				if n := f.openChildren(id); n > 0 {
					return fmt.Errorf("cannot close %s: %d open child issue(s); close children first or use --force to override", id, n)
				}
				for _, blk := range b.BlockedBy {
					if o, ok := f.beads[blk]; ok && o.Status != "closed" {
						return fmt.Errorf("cannot close blocked issue: %s is blocked by [%s] (use --force to override)", id, blk)
					}
				}
			}
		}
	}
	return f.closeIDs(ids, reason)
}

// openChildren counts id's children that are not closed, under f.mu.
func (f *Fake) openChildren(id string) int {
	n := 0
	for _, c := range f.beads {
		if c.Parent == id && c.Status != "closed" {
			n++
		}
	}
	return n
}

// closeIDs is Close's write, under f.mu: all or none.
func (f *Fake) closeIDs(ids []string, reason string) error {
	for _, id := range ids {
		if _, ok := f.beads[id]; !ok {
			return notFound(id)
		}
	}
	for _, id := range ids {
		b := f.beads[id]
		b.Status, b.CloseReason = "closed", reason // the assignee stays, as bd keeps it
	}
	return nil
}

func (f *Fake) Claim(id string, g bd.IfStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Claim", id); err != nil {
		return err
	}
	b, ok := f.beads[id]
	if !ok {
		return noIssueMatching(id)
	}
	if b.Status != string(g) {
		msg := fmt.Sprintf("status mismatch: %s has status %q, expected %q", id, b.Status, g)
		switch {
		case b.Assignee != "" && b.Assignee != Actor: // held by another actor: bd's reassign refusal
			return &bd.HeldError{Holder: b.Assignee, Err: fmt.Errorf("cannot reassign %s: held by %q", id, b.Assignee)}
		case b.Status == "in_progress": // this actor's own claim: the caller's snapshot was stale
			return fmt.Errorf("%w: %s", bd.ErrClaimedSince, msg)
		}
		return errors.New(msg)
	}
	b.Status, b.Assignee = "in_progress", Actor
	return nil
}

// AddComment appends a comment by Actor.
func (f *Fake) AddComment(id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("AddComment", id); err != nil {
		return err
	}
	if _, ok := f.beads[id]; !ok {
		return notFound(id)
	}
	f.comments[id] = append(f.comments[id], bd.Comment{Author: Actor, Text: text})
	return nil
}

// RemoveEdge drops from's edge to to, whatever its type.
func (f *Fake) RemoveEdge(from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("RemoveEdge", from, to); err != nil {
		return err
	}
	return f.removeEdge(from, to)
}

func (f *Fake) removeEdge(from, to string) error {
	b, ok := f.beads[from]
	if !ok {
		return notFound(from)
	}
	drop := func(id string) bool { return id == to }
	b.BlockedBy = slices.DeleteFunc(b.BlockedBy, drop)
	b.CondBlockedBy = slices.DeleteFunc(b.CondBlockedBy, drop)
	f.supers[from] = slices.DeleteFunc(f.supers[from], drop)
	b.Related = slices.DeleteFunc(b.Related, drop)
	b.DiscoveredFrom = slices.DeleteFunc(b.DiscoveredFrom, drop)
	if b.CausedBy == to {
		b.CausedBy = ""
	}
	if b.Parent == to {
		b.Parent = ""
	}
	return nil
}

// Supersede follows bd supersede: the supersedes edge is added first, and a refusal (the pair
// already holds an edge of another type) leaves old untouched; a repeat adds nothing. An open
// id is then closed with an empty reason (bd writes none); an id already closed keeps its
// status and reason.
func (f *Fake) Supersede(id, with string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Supersede", id, with); err != nil {
		return err
	}
	if _, ok := f.beads[with]; !ok {
		return notFound(with)
	}
	b, ok := f.beads[id]
	if !ok {
		return notFound(id)
	}
	if err := f.addEdge(id, with, "supersedes"); err != nil {
		return fmt.Errorf("failed to add supersede link: %w", err)
	}
	if b.Status != "closed" {
		b.Status, b.CloseReason = "closed", ""
	}
	return nil
}

// RemoveLabel drops label from every id; an id lacking it is untouched, an unknown id fails.
func (f *Fake) RemoveLabel(ids []string, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("RemoveLabel", ids...); err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := f.beads[id]; !ok {
			return notFound(id)
		}
	}
	for _, id := range ids {
		b := f.beads[id]
		b.Labels = slices.DeleteFunc(b.Labels, func(l string) bool { return l == label })
	}
	return nil
}

// Unclaim follows bd 1.3.0's check order exactly; every refusal writes nothing, and a closed
// bead is never reopened.
func (f *Fake) Unclaim(id string, g bd.IfAssignee) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Unclaim", id); err != nil {
		return err
	}
	b, ok := f.beads[id]
	switch {
	case !ok:
		return noIssueMatching(id)
	case g == "":
		return fmt.Errorf("bd unclaim %s: an empty --if-assignee guard", id)
	case b.Status == "closed":
		return fmt.Errorf("cannot unclaim closed issue %s", id)
	case b.Assignee != string(g):
		return fmt.Errorf("assignee mismatch: %s is held by %q, expected %q", id, b.Assignee, g)
	case b.Status != "open" && b.Status != "in_progress":
		return fmt.Errorf("failed to unclaim issue %s: no matching row", id)
	}
	b.Status, b.Assignee = "open", ""
	return nil
}

// Batch applies every op to a copy of the graph and swaps it in only if all succeed.
func (f *Fake) Batch(ops []bd.Op) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(ops))
	for _, o := range ops {
		ids = append(ids, o.ID)
	}
	if err := f.enter("Batch", ids...); err != nil {
		return err
	}
	saved := make(map[string]*bd.Bead, len(f.beads))
	for id, b := range f.beads {
		c := clone(*b)
		saved[id] = &c
	}
	for i, o := range ops {
		var err error
		switch o.Verb {
		case "dep add":
			err = f.addEdge(o.ID, o.To, o.Type)
		case "open":
			b, ok := f.beads[o.ID]
			if !ok {
				err = notFound(o.ID)
				break
			}
			b.Status = "open"
			if o.Title != "" {
				b.Title = o.Title
			}
		case "close":
			err = f.closeIDs([]string{o.ID}, o.Why)
		default:
			err = fmt.Errorf("bdtest: batch verb %q not modelled", o.Verb)
		}
		if err != nil {
			f.beads = saved
			return fmt.Errorf("bd batch: line %d: %w", i+1, err)
		}
	}
	return nil
}

// serialized checks the ADR 0070 (mugicrew-one-bd-write-rule) Serializer a read-then-write
// runs under, under f.mu. A violation fails the test and refuses the write.
func (f *Fake) serialized(verb string, s bd.Serializer) error {
	var why string
	switch s := s.(type) {
	case nil:
		why = "no Serializer"
	case bd.ClaimSer:
		if b, ok := f.beads[s.ID]; !ok || b.Status != "in_progress" || b.Assignee != Actor {
			why = "the claim on " + s.ID + " is not held by " + Actor
		}
	case bd.LockSer:
		if !s.Live() {
			why = "the " + s.Name + " lock token is no longer live"
		}
	case bd.HoldSer:
		if !s.Live() {
			why = "the " + s.Name + " hold token is no longer live"
		}
	}
	if why == "" {
		return nil
	}
	f.t.Errorf("ADR 0070 violation: %s runs under %s", verb, why)
	return fmt.Errorf("bdtest: ADR 0070 violation: %s runs under %s", verb, why)
}

// SetField is a whole-field write; it needs a live Serializer.
func (f *Fake) SetField(id string, fl bd.Field, v string, s bd.Serializer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("SetField", id); err != nil {
		return err
	}
	if err := f.serialized("SetField "+id, s); err != nil {
		return err
	}
	b, ok := f.beads[id]
	if !ok {
		return notFound(id)
	}
	switch fl {
	case bd.Description:
		b.Description = v
	case bd.TitleField:
		b.Title = v
	case bd.ParentField:
		b.Parent = v
	case bd.Priority:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("bdtest: priority %q: %w", v, err)
		}
		b.Priority = n
	case bd.Metadata:
		var m bd.Meta
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return fmt.Errorf("bdtest: metadata %q: %w", v, err)
		}
		b.Metadata = m
	default:
		return fmt.Errorf("bdtest: field %q not modelled", fl)
	}
	return nil
}

// Delete removes beads for good; it needs a live Serializer.
func (f *Fake) Delete(ids []string, s bd.Serializer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Delete", ids...); err != nil {
		return err
	}
	if err := f.serialized("Delete "+strings.Join(ids, ","), s); err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := f.beads[id]; !ok {
			return notFound(id)
		}
	}
	for _, id := range ids {
		delete(f.beads, id)
		delete(f.comments, id)
	}
	return nil
}

// Reopen makes a closed bead open again; it needs a live Serializer.
func (f *Fake) Reopen(id string, s bd.Serializer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Reopen", id); err != nil {
		return err
	}
	if err := f.serialized("Reopen "+id, s); err != nil {
		return err
	}
	b, ok := f.beads[id]
	if !ok {
		return notFound(id)
	}
	if b.Status == "closed" {
		b.Status, b.CloseReason, b.Closed = "open", "", time.Time{}
	}
	return nil
}

func clone(b bd.Bead) bd.Bead {
	b.Labels = slices.Clone(b.Labels)
	b.BlockedBy = slices.Clone(b.BlockedBy)
	b.CondBlockedBy = slices.Clone(b.CondBlockedBy)
	b.Related = slices.Clone(b.Related)
	b.DiscoveredFrom = slices.Clone(b.DiscoveredFrom)
	return b
}

func sortedKeys(m map[string]*bd.Bead) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}
