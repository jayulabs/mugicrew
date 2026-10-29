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
	fail     map[string]error // "verb id" (or "verb *") → injected error
	calls    []string
	minted   int
}

// New seeds a graph with beads and makes it bd's core until the test ends. It pins the bd
// actor to Actor, so bd.Mine() and the fake's claims agree.
func New(t testing.TB, beads ...bd.Bead) *Fake {
	f := &Fake{t: t, beads: map[string]*bd.Bead{}, comments: map[string][]bd.Comment{}, fail: map[string]error{}}
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
		es = append(es, [2]string{id, "blocks"})
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
	if dir == bd.Down {
		if b, ok := f.beads[id]; ok {
			for _, e := range edgesFrom(b) {
				add(f.beads[e[0]], e[1])
			}
		}
		return rows, nil
	}
	for _, oid := range sortedKeys(f.beads) {
		for _, e := range edgesFrom(f.beads[oid]) {
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

// AddEdge records from → to of type typ; the contract suite builds its graphs with it.
func (f *Fake) AddEdge(from, to, typ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("AddEdge", from, to); err != nil {
		return err
	}
	return f.addEdge(from, to, typ)
}

// addEdge is AddEdge's write, under f.mu.
func (f *Fake) addEdge(from, to, typ string) error {
	b, ok := f.beads[from]
	if !ok {
		return notFound(from)
	}
	if _, ok := f.beads[to]; !ok {
		return notFound(to)
	}
	switch typ {
	case "blocks", "conditional-blocks":
		b.BlockedBy = append(b.BlockedBy, to)
	case "related":
		b.Related = append(b.Related, to)
	case "caused-by":
		b.CausedBy = to
	case "discovered-from":
		b.DiscoveredFrom = append(b.DiscoveredFrom, to)
	case "parent-child":
		b.Parent = to
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
	f.minted++
	b := bd.Bead{ID: "bdct-" + strconv.Itoa(f.minted), Status: "open", Title: s.Title, Description: s.Description,
		Parent: s.Parent, Labels: slices.Clone(s.Labels), ExternalRef: s.ExternalRef, Container: s.Type == "epic"}
	if s.Deferred {
		b.Status = "deferred"
	}
	for _, l := range b.Labels {
		if k, ok := strings.CutPrefix(l, "kind:"); ok {
			b.Kind = contract.Kind(k)
		}
	}
	f.beads[b.ID] = &b
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

func (f *Fake) Close(ids []string, reason string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Close", ids...); err != nil {
		return err
	}
	return f.closeIDs(ids, reason)
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

// Supersede closes id as replaced by with.
func (f *Fake) Supersede(id, with string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Supersede", id, with); err != nil {
		return err
	}
	if _, ok := f.beads[with]; !ok {
		return notFound(with)
	}
	return f.closeIDs([]string{id}, "Superseded by "+with)
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
