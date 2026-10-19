package withdraw

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
)

type closeCall struct {
	ids    []string
	reason string
}

var (
	cas    []string // the CAS claims and their reverts, in order
	casErr map[string]error
)

// graph stubs the snapshot and the close, and returns what was closed.
func graph(t *testing.T, bs ...bd.Bead) *[]closeCall {
	t.Helper()
	var calls []closeCall
	ol, oc, oc2, ou, oi := listAll, forceClose, casClaim, unclaim, takeInventory
	t.Cleanup(func() { listAll, forceClose, casClaim, unclaim, takeInventory = ol, oc, oc2, ou, oi })
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) { return Inventory{}, nil }
	t.Setenv("BEADS_DIR", t.TempDir()) // the real liveness holds
	casClaim = func(id string) error { cas = append(cas, "claim "+id); return casErr[id] }
	unclaim = func(id string) error { cas = append(cas, "unclaim "+id); return nil }
	cas, casErr = nil, map[string]error{}
	listAll = func() ([]bd.Bead, error) { return bs, nil }
	oq := required
	t.Cleanup(func() { required = oq })
	required = func() bool { return false }
	forceClose = func(ids []string, reason string) error {
		calls = append(calls, closeCall{ids, reason})
		return nil
	}
	return &calls
}

func b(id string, kind contract.Kind, parent string, blockedBy ...string) bd.Bead {
	return bd.Bead{ID: id, Kind: kind, Status: "open", Parent: parent, BlockedBy: blockedBy}
}

func fixture() []bd.Bead {
	spec := b("spec", contract.KindSpec, "in")
	cp := b("cp", contract.KindCheckpoint, "in")
	cp.CausedBy = "spec"
	return []bd.Bead{
		b("in", contract.KindInitiative, ""), spec, cp,
		b("b1", contract.KindBuild, "cp"), b("b2", contract.KindBuild, "cp", "b1"),
		b("s2", contract.KindSignal, "in"),
	}
}

func busy(ids ...string) Opts {
	return Opts{InFlight: func(x bd.Bead) bool { return slices.Contains(ids, x.ID) }}
}

// A Checkpoint whose Spec is still open withdraws both, leaves first, head last;
// the Initiative's other chain and the Initiative stay.
func TestWithdrawClosesChain(t *testing.T) {
	calls := graph(t, fixture()...)
	evs := eventlogtest.Capture(t)
	res, err := Withdraw("cp", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"spec", "b1", "b2", "cp"}
	if !slices.Equal(res.Closed, want) || len(*calls) != 1 || !slices.Equal((*calls)[0].ids, want) || (*calls)[0].reason != "withdrawn" {
		t.Errorf("res %v calls %v", res, *calls)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.done"}) {
		t.Errorf("events = %v", got)
	}
	e := (*evs)[0]
	if e.Attrs["bead"] != "cp" || e.Attrs["reason"] != "withdrawn" {
		t.Errorf("attrs = %v", e.Attrs)
	}
}

// Withdrawing an Initiative's last chain leaves it drained; cancel closes the
// rest, Initiative last, with its reason, and logs on the Initiative.
func TestCancelClosesMembersThenInitiative(t *testing.T) {
	calls := graph(t, fixture()...)
	evs := eventlogtest.Capture(t)
	if _, err := Cancel("in", Opts{}); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if !slices.Equal(c.ids, []string{"spec", "b1", "b2", "cp", "s2", "in"}) || c.reason != "cancelled: in" {
		t.Errorf("close = %v", c)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"cancel.done"}) {
		t.Errorf("events = %v", got)
	}
}

// Cancel with a released chain closes the rest and the Initiative, releases the
// head through bd.ReleaseChain before the close, and records it on cancel.done.
func TestCancelReleasesChosenChain(t *testing.T) {
	calls := graph(t, fixture()...)
	rel := stubRelease(t)
	evs := eventlogtest.Capture(t)
	res, err := Cancel("in", Opts{Release: []string{"cp"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"s2", "in"}
	if !slices.Equal((*calls)[0].ids, want) || !slices.Equal(*rel, []string{"cp"}) || !slices.Equal(res.Released, []string{"cp"}) {
		t.Errorf("closed %v released %v res %v", *calls, *rel, res)
	}
	if got := (*evs)[len(*evs)-1]; got.Name != "cancel.done" || !slices.Equal(got.Attrs["released"].([]string), []string{"cp"}) {
		t.Errorf("events = %v", *evs)
	}
}

// A released chain ordered after a closing chain refuses and writes nothing.
func TestCancelRefusesReleasedChainOrderedAfterClosing(t *testing.T) {
	g := append(fixture(), b("s3", contract.KindSignal, "in", "s2"))
	calls := graph(t, g...)
	rel := stubRelease(t)
	_, err := Cancel("in", Opts{Release: []string{"s3"}})
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != OrderedAfter || !slices.Equal(re.Refusals[0].Beads, []string{"s3"}) {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 || len(*rel) != 0 {
		t.Errorf("a refusal wrote: closed %v released %v", *calls, *rel)
	}
}

// Under Require Initiatives every unclosed member is locked with the reason and
// a --release naming one refuses.
func TestCancelLockedUnderRequire(t *testing.T) {
	calls := graph(t, fixture()...)
	rel := stubRelease(t)
	or := required
	t.Cleanup(func() { required = or })
	required = func() bool { return true }
	p, err := plan("in", fixture(), Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Locked) != 3 || p.Locked[0].Why != bd.ReleaseRequiredWhy(p.Locked[0].Head) {
		t.Errorf("locked = %v", p.Locked)
	}
	_, err = Cancel("in", Opts{Release: []string{"cp"}})
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != ReleaseLocked || re.Refusals[0].Fix != bd.ReleaseRequiredWhy("cp") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 || len(*rel) != 0 {
		t.Errorf("a refusal wrote: closed %v released %v", *calls, *rel)
	}
}

// --release names an unclosed member chain head of the Initiative, nothing else.
func TestReleaseRejectsNonMember(t *testing.T) {
	graph(t, fixture()...)
	stubRelease(t)
	for _, h := range []string{"b1", "nope", "in"} {
		if _, err := Cancel("in", Opts{Release: []string{h}}); err == nil || errors.As(err, new(*RefusedError)) {
			t.Errorf("release %s: err = %v, want a plain error", h, err)
		}
	}
	if _, err := Withdraw("cp", Opts{Release: []string{"cp"}}); err == nil {
		t.Error("withdraw accepted --release")
	}
}

// stubRelease stubs bd.ReleaseChain and returns the heads released.
func stubRelease(t *testing.T) *[]string {
	t.Helper()
	var got []string
	or, oq := releaseChain, required
	t.Cleanup(func() { releaseChain, required = or, oq })
	releaseChain = func(h string) error { got = append(got, h); return nil }
	required = func() bool { return false }
	return &got
}

// A refusal writes nothing, names each blocker with its fix, and logs one
// <verb>.refused; an ordering refusal fires from a bead outside any Initiative.
func TestRefusals(t *testing.T) {
	g := append(fixture(), b("far", contract.KindSignal, "", "b1"))
	for _, c := range []struct {
		name   string
		o      Opts
		kind   RefusalKind
		blamed []string
	}{
		{"in flight", busy("b2"), InFlight, []string{"b2"}},
		{"ordered after", Opts{}, OrderedAfter, []string{"far"}},
	} {
		calls := graph(t, g...)
		evs := eventlogtest.Capture(t)
		_, err := Withdraw("cp", c.o)
		var re *RefusedError
		if !errors.As(err, &re) || re.Refusals[0].Kind != c.kind || !slices.Equal(re.Refusals[0].Beads, c.blamed) || re.Refusals[0].Fix == "" {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if len(*calls) != 0 {
			t.Errorf("%s: a refusal closed %v", c.name, *calls)
		}
		if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.refused"}) {
			t.Errorf("%s: events = %v", c.name, got)
		}
	}
}

// A Spec's not-yet-carried dependent refuses its withdrawal.
func TestRefusesSpecDependent(t *testing.T) {
	g := []bd.Bead{b("spec", contract.KindSpec, ""), b("dep", contract.KindSignal, "", "spec")}
	graph(t, g...)
	if _, err := Withdraw("spec", Opts{}); !errors.As(err, new(*RefusedError)) {
		t.Errorf("err = %v, want a refusal", err)
	}
}

// Anything but a chain head is a plain error, nothing written, no event.
func TestRejectsNonHead(t *testing.T) {
	calls := graph(t, fixture()...)
	evs := eventlogtest.Capture(t)
	for _, id := range []string{"b1", "spec", "in", "nope"} {
		_, err := Withdraw(id, Opts{})
		if err == nil || errors.As(err, new(*RefusedError)) {
			t.Errorf("withdraw %s = %v, want a plain error", id, err)
		}
	}
	if _, err := Cancel("cp", Opts{}); err == nil {
		t.Error("cancel on a Checkpoint: want an error")
	}
	if len(*calls) != 0 || len(*evs) != 0 {
		t.Errorf("wrote %v, events %v", *calls, eventlogtest.Names(*evs))
	}
}

// PlanOf is read-only: it reports refusals and records nothing.
func TestPlanWritesNothing(t *testing.T) {
	calls := graph(t, fixture()...)
	evs := eventlogtest.Capture(t)
	p, err := PlanOf("cp", busy("b1"))
	if err != nil || p.Kind != KindWithdraw || len(p.Refusals) != 1 {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if len(*calls) != 0 || len(*evs) != 0 {
		t.Errorf("wrote %v, events %v", *calls, eventlogtest.Names(*evs))
	}
}

// A failed close is withdraw.failed on the head, step close, and returns the error.
func TestCloseFailure(t *testing.T) {
	graph(t, fixture()...)
	boom := errors.New("boom")
	forceClose = func([]string, string) error { return boom }
	evs := eventlogtest.Capture(t)
	if _, err := Withdraw("cp", Opts{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.failed"}) || (*evs)[0].Attrs["step"] != "close" {
		t.Errorf("events = %v", *evs)
	}
}

func TestReadFailure(t *testing.T) {
	graph(t)
	listAll = func() ([]bd.Bead, error) { return nil, errors.New("down") }
	evs := eventlogtest.Capture(t)
	if _, err := Cancel("in", Opts{}); err == nil {
		t.Fatal("want an error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"cancel.failed"}) || (*evs)[0].Attrs["step"] != "read" {
		t.Errorf("events = %v", *evs)
	}
}

// A hold on a set member refuses as in flight on that bead and writes nothing.
func TestHeldMemberRefuses(t *testing.T) {
	calls := graph(t, fixture()...)
	h, err := liveness.Acquire("b2")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	evs := eventlogtest.Capture(t)
	_, err = Withdraw("cp", Opts{})
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != InFlight || !slices.Equal(re.Refusals[0].Beads, []string{"b2"}) {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 || len(cas) != 0 {
		t.Errorf("wrote %v, cas %v", *calls, cas)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.refused"}) {
		t.Errorf("events = %v", got)
	}
	free(t, "spec", "b1", "cp") // the refused verb's holds are gone
}

func free(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		h, err := liveness.Acquire(id)
		if err != nil {
			t.Errorf("%s still held: %v", id, err)
			continue
		}
		h.Release()
	}
}

// While the verb is closing, a claim's Acquire on a set member fails; after, it succeeds.
func TestFenceHoldsDuringClose(t *testing.T) {
	graph(t, fixture()...)
	forceClose = func(ids []string, _ string) error {
		for _, id := range ids {
			if h, err := liveness.Acquire(id); err == nil {
				h.Release()
				t.Errorf("%s acquirable under the fence", id)
			}
		}
		return nil
	}
	if _, err := Withdraw("cp", Opts{}); err != nil {
		t.Fatal(err)
	}
	free(t, "spec", "b1", "b2", "cp")
}

// A CAS mismatch on the second HITL bead reverts the first to open and refuses.
func TestCASMismatchReverts(t *testing.T) {
	calls := graph(t, fixture()...)
	casErr["in"] = fmt.Errorf("status mismatch: %w", bd.ErrClaimedSince)
	_, err := Cancel("in", Opts{})
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != InFlight || !slices.Equal(re.Refusals[0].Beads, []string{"in"}) {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Join(cas, ","); got != "claim s2,claim in,unclaim s2" {
		t.Errorf("cas = %s", got)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
	free(t, "spec", "b1", "b2", "cp", "s2", "in")
}

// A CAS failure that is no mismatch (bd itself broke) is withdraw.failed step
// fence, not an in-flight refusal; the claims taken so far revert.
func TestCASFaultFailsFence(t *testing.T) {
	calls := graph(t, fixture()...)
	evs := eventlogtest.Capture(t)
	casErr["in"] = errors.New("dolt down")
	_, err := Cancel("in", Opts{})
	if err == nil || errors.As(err, new(*RefusedError)) {
		t.Fatalf("err = %v", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"cancel.failed"}) || (*evs)[0].Attrs["step"] != "fence" {
		t.Errorf("events = %v", got)
	}
	if got := strings.Join(cas, ","); got != "claim s2,claim in,unclaim s2" || len(*calls) != 0 {
		t.Errorf("cas = %s, closed %v", got, *calls)
	}
}

// A close that failed after closing a HITL bead must not unclaim it: bd unclaim
// resets status to open.
func TestCloseFailureSparesClosedHITL(t *testing.T) {
	graph(t, fixture()...)
	base := listAll
	n := 0
	listAll = func() ([]bd.Bead, error) {
		g, _ := base()
		g = slices.Clone(g)
		if n++; n > 2 { // plan, re-plan; then the reads after the failed close
			for i := range g {
				if g[i].ID == "s2" {
					g[i].Status = "closed"
				}
			}
		}
		return g, nil
	}
	forceClose = func([]string, string) error { return errors.New("boom") }
	if _, err := Cancel("in", Opts{}); err == nil {
		t.Fatal("want an error")
	}
	if got := strings.Join(cas, ","); got != "claim s2,claim in,unclaim in" {
		t.Errorf("cas = %s", got)
	}
}

// A failed close reverts the CAS claims and frees the holds, step close.
func TestCloseFailureReverts(t *testing.T) {
	graph(t, fixture()...)
	forceClose = func([]string, string) error { return errors.New("boom") }
	if _, err := Cancel("in", Opts{}); err == nil {
		t.Fatal("want an error")
	}
	if got := strings.Join(cas, ","); got != "claim s2,claim in,unclaim s2,unclaim in" {
		t.Errorf("cas = %s", got)
	}
	free(t, "spec", "b1", "b2", "cp", "s2", "in")
}

// A refusal found only on the re-read under the fence reverts and frees too.
func TestRefusalUnderFence(t *testing.T) {
	calls := graph(t, fixture()...)
	n := 0
	base := listAll
	listAll = func() ([]bd.Bead, error) {
		n++
		g, _ := base()
		if n >= 2 {
			g = append(slices.Clone(g), b("far", contract.KindSignal, "", "b1"))
		}
		return g, nil
	}
	if _, err := Cancel("in", Opts{}); !errors.As(err, new(*RefusedError)) {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 || !strings.Contains(strings.Join(cas, ","), "unclaim s2") {
		t.Errorf("closed %v, cas %v", *calls, cas)
	}
	free(t, "spec", "b1", "b2", "cp", "s2", "in")
}

// A lossy inventory without Discard stops the verb before it closes anything and
// logs nothing; with Discard the close goes ahead. A lossless chain needs no flag.
func TestDiscardGate(t *testing.T) {
	lossy := Inventory{Beads: []BeadLeftovers{{ID: "b1", Branch: "effort/b1", Unintegrated: 2}}}
	calls := graph(t, fixture()...)
	evs := eventlogtest.Capture(t)
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) { return lossy, nil }
	_, err := Withdraw("cp", Opts{})
	var de *DiscardRequiredError
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "effort/b1") || !strings.Contains(err.Error(), "--discard") {
		t.Fatalf("err = %v, want a discard-required error naming the inventory", err)
	}
	if len(*calls) != 0 || len(*evs) != 0 {
		t.Errorf("wrote %v, events %v", *calls, eventlogtest.Names(*evs))
	}
	if _, err := Withdraw("cp", Opts{Discard: true}); err != nil || len(*calls) != 1 {
		t.Errorf("with Discard: err = %v, calls %v", err, *calls)
	}
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) { return Inventory{}, nil }
	if _, err := Cancel("in", Opts{}); err != nil {
		t.Errorf("lossless cancel: %v", err)
	}
}

// A failed inventory read (a session table that can't be read) fails the verb with
// nothing written; PlanOf carries the inventory.
func TestInventoryFailureFailsTheVerb(t *testing.T) {
	calls := graph(t, fixture()...)
	boom := errors.New("tmux down")
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) { return Inventory{}, boom }
	evs := eventlogtest.Capture(t)
	if _, err := Withdraw("cp", Opts{Discard: true}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.failed"}) || (*evs)[0].Attrs["step"] != "inventory" {
		t.Errorf("events = %v", *evs)
	}
	if _, err := PlanOf("cp", Opts{}); !errors.Is(err, boom) {
		t.Errorf("PlanOf err = %v", err)
	}
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) {
		return Inventory{Containers: []ContainerBranch{{ID: "cp", Ahead: 1}}}, nil
	}
	if p, err := PlanOf("cp", Opts{}); err != nil || len(p.Inventory.Containers) != 1 {
		t.Errorf("plan inventory = %+v, %v", p.Inventory, err)
	}
}

// A fence failure that is not contention (no lock dir) is a failed step fence, not an
// in-flight refusal.
func TestFenceNonContentionErrorFails(t *testing.T) {
	calls := graph(t, fixture()...)
	t.Setenv("BEADS_DIR", "")
	evs := eventlogtest.Capture(t)
	_, err := Withdraw("cp", Opts{})
	if err == nil || errors.As(err, new(*RefusedError)) {
		t.Fatalf("err = %v, want a plain failure", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.failed"}) || (*evs)[0].Attrs["step"] != "fence" {
		t.Errorf("events = %v", *evs)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
}

// Cleanup runs while the fence is still held (close → cleanup → release).
func TestFenceHeldDuringCleanup(t *testing.T) {
	graph(t, fixture()...)
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) {
		return Inventory{Beads: []BeadLeftovers{{ID: "b1", Drafts: []string{"d"}}}}, nil
	}
	od := dropDrafts
	t.Cleanup(func() { dropDrafts = od })
	dropDrafts = func(bd.Bead, ...any) error {
		if h, err := liveness.Acquire("b1"); err == nil {
			h.Release()
			t.Error("b1 acquirable during cleanup")
		}
		return nil
	}
	if _, err := Withdraw("cp", Opts{}); err != nil {
		t.Fatal(err)
	}
	free(t, "b1")
}

// A target that is no chain head emits no verb.failed (it is a usage error), and an
// unreadable land strategy does fail the step.
func TestStrategyErrorEmitsFailed(t *testing.T) {
	forgeWorld(t, git.LandStrategyGitlab)
	strategyName = func() (string, error) { return "", errors.New("no config") }
	rb := b("rb", contract.KindRollback, "")
	rb.Description = "MR: !9"
	graph(t, rb)
	evs := eventlogtest.Capture(t)
	if _, err := Withdraw("rb", Opts{}); err == nil {
		t.Fatal("want an error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"withdraw.failed"}) || (*evs)[0].Attrs["step"] != "plan" {
		t.Errorf("events = %v", *evs)
	}
}

// The in-flight test is re-read under the fence: an effort that started after the
// first read refuses the verb.
func TestInFlightRereadUnderFence(t *testing.T) {
	calls := graph(t, fixture()...)
	o := Opts{Reread: func() (func(bd.Bead) bool, error) { return busy("b2").InFlight, nil }}
	_, err := Withdraw("cp", o)
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != InFlight || !slices.Equal(re.Refusals[0].Beads, []string{"b2"}) {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
	free(t, "spec", "b1", "b2", "cp")
}
