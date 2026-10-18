package luffy

import (
	"errors"
	"fmt"
	"jayulabs.org/mugicrew/cfglayer"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
)

// TestMain keeps stderr quiet (slog's default handler); use eventlogtest.Capture
// to assert events.
func TestMain(m *testing.M) {
	eventlogtest.Discard()
	discoveredFrom = func(string) ([]bd.Bead, error) { return nil, nil }
	initiativesSetting = func() cfglayer.Initiatives { return cfglayer.Initiatives{} }
	initiativesByRef = func(string) ([]bd.Bead, error) { return nil, nil }
	os.Exit(m.Run())
}

// The Chart reconcileSignal seeds carries the shared placeholder body the prompts
// reference, and is a Draft by the Signal (ADR 0059 §7).
func TestChartSuccessorSeedsPlaceholderBody(t *testing.T) {
	c := chartSuccessor(bd.Bead{ID: "s", Title: "a signal"})
	if c.Kind != contract.KindChart || !c.Container {
		t.Errorf("Chart successor must be a kind:chart container, got kind=%q container=%v", c.Kind, c.Container)
	}
	if c.Title != "a signal" {
		t.Errorf("Chart title = %q, want the Signal's title verbatim", c.Title)
	}
	if c.Description != contract.ChartPlaceholderBody {
		t.Errorf("Chart seed body = %q, want the shared placeholder constant %q", c.Description, contract.ChartPlaceholderBody)
	}
	// ADR 0059 §7, Spec mc-r832 D4: the Chart is a Draft by the Signal until
	// triage's hitl-run publishes it together with its Survey.
	if c.DraftedBy != "s" {
		t.Errorf("Chart successor DraftedBy = %q, want the Signal's id", c.DraftedBy)
	}
}

// fakeSignalGraph is a stateful stand-in for the bd/git legs reconcileSignal and
// SeedSpec drive (ADR 0035 §10): the Signal's caused-by successors by kind, the
// trailers on mainline, and every write — so leg choice, the refusal to mix legs,
// and crash-rerun idempotence are checkable with no live bd or git. There is no
// close seam: reconcile never closes the Signal (ADR 0096), the publish does.
type fakeSignalGraph struct {
	succ     map[contract.Kind]string
	bodies   map[string]bd.Successor
	trailers map[string]bool   // legacy "Spec: <id>" commits already on mainline
	pins     map[string]string // Spec id → its body (carries the Anchor: pin once written)
	spawns   int
	anchors  int      // Anchor: pins written
	cuts     []string // branches cut, in order
	failAt   string   // "anchor": crash there once
	seq      []string // carry calls, in order: "carry <pred> <succ>"
	carryErr error    // carry: the error bd.Carry returns
	// surveys: each Chart's kind:survey children in any status (bd children lists
	// closed and Drafts alike), as spawnSurvey reads and writes them.
	surveys map[string][]bd.Successor
}

// drop deletes the Signal's successor Draft, and the Chart's Survey with it (both
// carry the Signal's drafted-by), as a session drop's DeleteDrafts does.
func (g *fakeSignalGraph) drop(k contract.Kind) {
	delete(g.surveys, g.succ[k])
	delete(g.succ, k)
}

func stubSignalSeams(t *testing.T) *fakeSignalGraph {
	t.Helper()
	g := &fakeSignalGraph{succ: map[contract.Kind]string{}, bodies: map[string]bd.Successor{}, trailers: map[string]bool{}, pins: map[string]string{}, surveys: map[string][]bd.Successor{}}
	swap(t, &successorOf, func(_ string, k contract.Kind) (string, bool, error) { id, ok := g.succ[k]; return id, ok, nil })
	swap(t, &spawnSuccessor, func(_ string, s bd.Successor) (string, bool, error) {
		if id, ok := g.succ[s.Kind]; ok {
			return id, false, nil
		}
		g.spawns++
		id := "ws-" + string(s.Kind)
		if g.spawns > 1 {
			id += fmt.Sprint(g.spawns) // a re-spawn after a drop gets a new id
		}
		g.succ[s.Kind], g.bodies[id] = id, s
		return id, true, nil
	})
	swap(t, &spawnSurvey, func(chartID, title, drafter string) (string, bool, error) {
		if len(g.surveys[chartID]) > 0 {
			return "ws-survey", false, nil
		}
		g.surveys[chartID] = append(g.surveys[chartID], bd.Successor{Kind: contract.KindSurvey, Title: title, Description: contract.SurveyBody, DraftedBy: drafter})
		return "ws-survey", true, nil
	})
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyLocal, nil })
	swap(t, &repoLanded, func(_ git.Repo, tr string) (string, error) {
		if g.trailers[tr] {
			return "landedsha", nil
		}
		return "", git.ErrNotLanded
	})
	swap(t, &repoTip, func(git.Repo) (string, error) {
		if g.failAt == "anchor" {
			g.failAt = ""
			return "", errors.New("crash")
		}
		return "tipsha", nil
	})
	swap(t, &showSpec, func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Description: g.pins[id]}, nil })
	swap(t, &updateBody, func(id string, a []string) error {
		g.anchors++
		g.pins[id] = a[1]
		return nil
	})
	swap(t, &cutBranch, func(b, _ string) (bool, error) {
		if slices.Contains(g.cuts, b) {
			return false, nil
		}
		g.cuts = append(g.cuts, b)
		return true, nil
	})
	swap(t, &carry, func(pred, succ string) ([]string, []bd.CarrySkip, error) {
		g.seq = append(g.seq, "carry "+pred+" "+succ)
		return nil, nil, g.carryErr
	})
	return g
}

// swap points a package seam at v for the rest of the test.
func swap[T any](t *testing.T, seam *T, v T) {
	old := *seam
	t.Cleanup(func() { *seam = old })
	*seam = v
}

func openSignal() bd.Bead {
	return bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Status: "open", Title: "sharp fix"}
}

// ADR 0035 §10: --spec seeds one kind:spec leaf successor with the body verbatim
// and the defaulted title; reconcile then anchors "Spec: <id>" once, carries,
// emits one triage.fastlane, and never takes the Chart leg. It leaves the Signal
// open: the session's clean-exit publish closes it (ADR 0096).
func TestFastLaneSeedsAnchorsAndLeavesSignalForPublish(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	sig := openSignal()
	if err := SeedSpec(sig, "", "the spec body", ""); err != nil {
		t.Fatalf("SeedSpec: %v", err)
	}
	if err := reconcileSignal(sig, ""); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	s := g.bodies["ws-spec"]
	if g.spawns != 1 || s.Kind != contract.KindSpec || s.Container || s.Description != "the spec body" || s.Title != "sharp fix" || s.DraftedBy != "ws-sig" {
		t.Errorf("spawns=%d spec=%+v, want one leaf kind:spec Draft by the Signal with the body and defaulted title", g.spawns, s)
	}
	if sha, ok := git.ParseAnchor(g.pins["ws-spec"]); g.anchors != 1 || !ok || sha != "tipsha" || len(g.trailers) != 0 {
		t.Errorf("anchors=%d pins=%v trailers=%v, want one Anchor: pin at the tip and no Spec: commit", g.anchors, g.pins, g.trailers)
	}
	if want := []string{"carry ws-sig ws-spec"}; !slices.Equal(g.seq, want) {
		t.Errorf("seq = %v, want %v", g.seq, want)
	}
	if _, chart := g.succ[contract.KindChart]; chart || len(g.cuts) != 0 {
		t.Errorf("fast lane must not take the Chart leg (chart=%v cuts=%v)", chart, g.cuts)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "triage.fastlane" || (*evs)[0].Attrs["spec"] != "ws-spec" || (*evs)[0].Attrs["anchor"] != "tipsha" {
		t.Errorf("events = %+v, want one triage.fastlane naming the spec and anchor", *evs)
	}
}

// A re-run over a pre-change Spec (legacy "Spec: <id>" commit, no pin) logs the
// trailer sha and writes no pin.
func TestFastLaneLegacyRerunLogsTrailerSha(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec], g.trailers["Spec: ws-spec"] = "ws-spec", true
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	if g.anchors != 0 || len(*evs) != 1 || (*evs)[0].Attrs["anchor"] != "landedsha" {
		t.Errorf("anchors=%d events=%+v, want no pin and one triage.fastlane with anchor=landedsha", g.anchors, *evs)
	}
}

// Spec D6: every fast-lane failure — anchor read, anchor write, carry — logs one
// triage.failed at Error with spec and err.
func TestFastLaneFailureLogsTriageFailed(t *testing.T) {
	for _, crash := range []string{"read", "anchor", "carry"} {
		evs := eventlogtest.Capture(t)
		g := stubSignalSeams(t)
		g.succ[contract.KindSpec] = "ws-spec"
		switch crash {
		case "read":
			repoLanded = func(git.Repo, string) (string, error) { return "", errors.New("git broke") }
		case "carry":
			g.carryErr = errors.New("bd down")
		default:
			g.failAt = crash
		}
		if err := reconcileSignal(openSignal(), ""); err == nil {
			t.Fatalf("crash at %s: want an error", crash)
		}
		triage := slices.DeleteFunc(slices.Clone(*evs), func(e eventlogtest.Event) bool { return !strings.HasPrefix(e.Name, "triage.") })
		if len(triage) != 1 || triage[0].Name != "triage.failed" || triage[0].Level != slog.LevelError ||
			triage[0].Attrs["spec"] != "ws-spec" || triage[0].Attrs["err"] == nil {
			t.Errorf("crash at %s: events = %+v, want one triage.failed with spec and err", crash, *evs)
		}
	}
}

// An explicit --title wins over the default.
func TestSeedSpecExplicitTitle(t *testing.T) {
	g := stubSignalSeams(t)
	if err := SeedSpec(openSignal(), "custom", "body", ""); err != nil {
		t.Fatalf("SeedSpec: %v", err)
	}
	if got := g.bodies["ws-spec"].Title; got != "custom" {
		t.Errorf("title = %q, want the explicit one", got)
	}
}

// A crash at any step converges on re-run — with or without --spec: no second
// Spec, no second anchor, no Chart; the carry re-runs idempotently and nothing
// closes the Signal (the publish does, ADR 0096).
func TestFastLaneRerunAfterCrashConverges(t *testing.T) {
	for _, crash := range []string{"anchor", "carry"} {
		for _, reseed := range []bool{true, false} {
			g := stubSignalSeams(t)
			if crash == "carry" {
				g.carryErr = errors.New("bd down")
			} else {
				g.failAt = crash
			}
			sig := openSignal()
			if err := SeedSpec(sig, "", "body", ""); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
			if err := reconcileSignal(sig, ""); err == nil {
				t.Fatalf("crash at %s: want an error", crash)
			}
			g.carryErr = nil
			if reseed {
				if err := SeedSpec(sig, "", "body", ""); err != nil {
					t.Fatalf("re-SeedSpec: %v", err)
				}
			}
			if err := reconcileSignal(sig, ""); err != nil {
				t.Fatalf("re-run after %s crash: %v", crash, err)
			}
			// A third run once the publish closed the Signal is a pure no-op.
			g.seq = nil
			sig.Status = "closed"
			if err := reconcileSignal(sig, ""); err != nil {
				t.Fatalf("re-run on closed Signal: %v", err)
			}
			if g.spawns != 1 || g.anchors != 1 || len(g.seq) != 0 || len(g.cuts) != 0 {
				t.Errorf("crash=%s reseed=%v: spawns=%d anchors=%d seq=%v cuts=%v, want 1/1/none/none",
					crash, reseed, g.spawns, g.anchors, g.seq, g.cuts)
			}
			if _, chart := g.succ[contract.KindChart]; chart {
				t.Errorf("crash=%s reseed=%v: a re-run spawned a Chart", crash, reseed)
			}
		}
	}
}

// Refusal to mix legs: --spec on a Signal that already has a Chart successor fails
// loudly and writes nothing.
func TestSeedSpecRefusesChartedSignal(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindChart] = "ws-chart"
	if err := SeedSpec(openSignal(), "", "body", ""); err == nil {
		t.Fatal("--spec on a charted Signal must error")
	}
	if g.spawns != 0 || g.anchors != 0 || len(g.seq) != 0 {
		t.Errorf("refusal must change nothing: spawns=%d anchors=%d seq=%v", g.spawns, g.anchors, g.seq)
	}
}

// --spec on a Jira Signal refuses before any write, so no Draft Spec is left to publish.
func TestSeedSpecRefusesJiraSignal(t *testing.T) {
	g := stubSignalSeams(t)
	err := SeedSpec(jiraSignal(), "", "body", "")
	if err == nil || !strings.Contains(err.Error(), "INITIATIVE") {
		t.Fatalf("err = %v, want a refusal naming INITIATIVE", err)
	}
	if g.spawns != 0 || g.anchors != 0 || len(g.seq) != 0 {
		t.Errorf("refusal must change nothing: spawns=%d anchors=%d seq=%v", g.spawns, g.anchors, g.seq)
	}
}

// The fast-lane Spec (ADR 0035 §10, Spec D2) is a leaf kind:spec carrying
// Luffy's body verbatim; the title defaults to exactly the Signal's title.
func TestSpecFastLaneSuccessorShape(t *testing.T) {
	sig := bd.Bead{ID: "s", Title: "a signal"}
	s := specFastLaneSuccessor(sig, "", "the body")
	if s.Kind != contract.KindSpec || s.Container {
		t.Errorf("fast-lane Spec must be a kind:spec leaf, got kind=%q container=%v", s.Kind, s.Container)
	}
	if s.Title != "a signal" || s.Description != "the body" {
		t.Errorf("got title=%q body=%q, want the defaulted title and the body verbatim", s.Title, s.Description)
	}
	if s := specFastLaneSuccessor(sig, "custom", "b"); s.Title != "custom" {
		t.Errorf("explicit title overridden: %q", s.Title)
	}
}

// A closed Signal with no Spec (REJECT/DUPLICATE) never takes the fast lane; one
// already fast-laned and published (its Spec exists) re-runs as a no-op.
func TestSeedSpecClosedSignal(t *testing.T) {
	g := stubSignalSeams(t)
	sig := openSignal()
	sig.Status = "closed"
	if err := SeedSpec(sig, "", "body", ""); err == nil || g.spawns != 0 {
		t.Errorf("closed un-seeded Signal: err=%v spawns=%d, want an error and no spawn", err, g.spawns)
	}
	g.succ[contract.KindSpec] = "ws-spec"
	if err := SeedSpec(sig, "", "body", ""); err != nil || g.spawns != 0 {
		t.Errorf("closed fast-laned Signal: err=%v spawns=%d, want a no-op", err, g.spawns)
	}
}

// --spec is valid only on a Signal, and needs a body.
func TestSeedSpecUsageErrors(t *testing.T) {
	g := stubSignalSeams(t)
	if err := SeedSpec(bd.Bead{ID: "mc-c", Kind: contract.KindChart}, "", "body", ""); err == nil {
		t.Error("--spec on a non-Signal must be a usage error")
	}
	if err := SeedSpec(openSignal(), "", "  \n", ""); err == nil {
		t.Error("--spec with an empty body must error")
	}
	if g.spawns != 0 {
		t.Errorf("usage errors must spawn nothing, spawned %d", g.spawns)
	}
}

// A bare reconcile on a Signal with no successor takes the Chart leg: one Chart,
// its branch cut, the carry, no anchor — and the Signal left open for the publish.
func TestBareReconcileTakesChartLeg(t *testing.T) {
	g := stubSignalSeams(t)
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	if _, chart := g.succ[contract.KindChart]; !chart || !slices.Equal(g.cuts, []string{"chart/ws-chart"}) || g.anchors != 0 ||
		!slices.Equal(g.seq, []string{"carry ws-sig ws-chart"}) {
		t.Errorf("chart=%v cuts=%v anchors=%d seq=%v, want the Chart leg", chart, g.cuts, g.anchors, g.seq)
	}
	if _, spec := g.succ[contract.KindSpec]; spec {
		t.Error("the Chart leg must not spawn a Spec")
	}
}

// Spec mc-r832 D4, FR1/FR4: ACCEPT's reconcile seeds the Chart's Survey under it,
// titled with the Signal's title, the shared pointer body, a Draft by the Signal
// so triage's clean exit publishes it with the Chart (and a drop deletes both).
func TestAcceptSeedsSurveyUnderChart(t *testing.T) {
	g := stubSignalSeams(t)
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	want := []bd.Successor{{Kind: contract.KindSurvey, Title: "sharp fix", Description: contract.SurveyBody, DraftedBy: "ws-sig"}}
	if got := g.surveys["ws-chart"]; !slices.Equal(got, want) {
		t.Errorf("surveys under ws-chart = %+v, want %+v", got, want)
	}
}

// Spec mc-r832 D4, FR5/FR6: a crash re-run, a manual re-run once the Survey has
// closed, and a re-run on the published (closed) Signal spawn no second Survey;
// a dropped triage takes the Survey with the Chart, so a re-triage seeds afresh.
func TestAcceptRerunSeedsNoSecondSurvey(t *testing.T) {
	g := stubSignalSeams(t)
	g.carryErr = errors.New("bd down") // crash after the Chart and its Survey exist
	sig := openSignal()
	if err := reconcileSignal(sig, ""); err == nil {
		t.Fatal("carry crash: want an error")
	}
	g.carryErr = nil
	if err := reconcileSignal(sig, ""); err != nil {
		t.Fatalf("re-run after crash: %v", err)
	}
	sig.Status = "closed" // published; the Survey's charting session has since closed it too
	if err := reconcileSignal(sig, ""); err != nil {
		t.Fatalf("re-run on closed Signal: %v", err)
	}
	if n := len(g.surveys["ws-chart"]); n != 1 {
		t.Errorf("Surveys under ws-chart = %d after re-runs, want 1", n)
	}
	g.drop(contract.KindChart)
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatalf("re-triage after drop: %v", err)
	}
	if len(g.surveys["ws-chart"]) != 0 || len(g.surveys["ws-chart2"]) != 1 {
		t.Errorf("surveys = %+v, want the dropped Chart's gone and one under the new Chart", g.surveys)
	}
}

// FR18 / Spec D1: a Signal with both a Spec and a Chart successor is a loud
// error that mutates nothing — no anchor read or write, no spawn, cut, or carry.
func TestReconcileSignalBothSuccessorsErrorsWithoutMutation(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec], g.succ[contract.KindChart] = "ws-spec", "ws-chart"
	repoLanded = func(git.Repo, string) (string, error) {
		t.Error("landed base read with both successors")
		return "", git.ErrNotLanded
	}
	if err := reconcileSignal(openSignal(), ""); err == nil {
		t.Fatal("both successors present must error")
	}
	if g.spawns != 0 || g.anchors != 0 || len(g.cuts) != 0 || len(g.seq) != 0 {
		t.Errorf("spawns=%d anchors=%d cuts=%v seq=%v, want nothing written", g.spawns, g.anchors, g.cuts, g.seq)
	}
}

// A Chart successor present means the Chart leg: no "Spec:" anchor is written.
func TestReconcileSignalChartedWritesNoAnchor(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindChart] = "ws-chart"
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	if g.anchors != 0 || g.spawns != 0 || len(g.cuts) != 1 {
		t.Errorf("anchors=%d spawns=%d cuts=%v, want the Chart leg with no anchor", g.anchors, g.spawns, g.cuts)
	}
}

// A failed LandedBase read is not absence: the leg errors and writes no anchor,
// so a transient git failure can't stamp a second "Spec: <id>" commit.
func TestFastLaneFailedAnchorReadWritesNothing(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec] = "ws-spec"
	repoLanded = func(git.Repo, string) (string, error) { return "", errors.New("git log: boom") }
	if err := reconcileSignal(openSignal(), ""); err == nil {
		t.Fatal("a failed anchor read must error")
	}
	if g.anchors != 0 || len(g.seq) != 0 {
		t.Errorf("anchors=%d seq=%v, want nothing written", g.anchors, g.seq)
	}
}

// Spec D1 (ADR 0096), Signal→Chart and Signal→Spec: reconcile of an open
// Signal carries its Ordering edges to the successor and never closes it — the
// clean-exit publish does; a carry error fails the reconcile; a re-run on the open
// Signal re-carries without a second successor; a closed Signal carries nothing.
func TestReconcileSignalCarriesAndLeavesCloseToPublish(t *testing.T) {
	for _, leg := range []contract.Kind{contract.KindChart, contract.KindSpec} {
		succ := "ws-" + string(leg)
		g := stubSignalSeams(t)
		if leg == contract.KindSpec {
			if err := SeedSpec(openSignal(), "", "body", ""); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
		}
		if err := reconcileSignal(openSignal(), ""); err != nil {
			t.Fatalf("%s leg: reconcileSignal: %v", leg, err)
		}
		if want := []string{"carry ws-sig " + succ}; !slices.Equal(g.seq, want) {
			t.Errorf("%s leg: seq = %v, want %v", leg, g.seq, want)
		}

		g.seq = nil
		if err := reconcileSignal(openSignal(), ""); err != nil {
			t.Fatalf("%s leg: re-run: %v", leg, err)
		}
		if want := []string{"carry ws-sig " + succ}; !slices.Equal(g.seq, want) || g.spawns != 1 || len(g.cuts) > 1 || g.anchors > 1 {
			t.Errorf("%s leg: re-run seq=%v spawns=%d cuts=%v anchors=%d, want a re-carry and no second successor, branch or anchor",
				leg, g.seq, g.spawns, g.cuts, g.anchors)
		}

		g.seq, g.carryErr = nil, errors.New("bd down")
		if err := reconcileSignal(openSignal(), ""); err == nil {
			t.Errorf("%s leg: a carry error must fail the reconcile", leg)
		}

		g.seq, g.carryErr = nil, nil
		sig := openSignal()
		sig.Status = "closed"
		if err := reconcileSignal(sig, ""); err != nil {
			t.Fatalf("%s leg: re-entrant reconcileSignal: %v", leg, err)
		}
		if len(g.seq) != 0 {
			t.Errorf("%s leg: a closed Signal must not carry again, got %v", leg, g.seq)
		}
	}
}

// Spec FR 18–19: a re-run after a drop deleted the Draft, on the still-open
// Signal, spawns a successor with a new id — on the Chart leg with a new
// chart/<newID>, on the fast lane (re-seeded) with a fresh Spec: <newID> anchor.
func TestReconcileSignalRerunAfterDropSpawnsFreshSuccessor(t *testing.T) {
	t.Run("chart", func(t *testing.T) {
		g := stubSignalSeams(t)
		if err := reconcileSignal(openSignal(), ""); err != nil {
			t.Fatalf("reconcileSignal: %v", err)
		}
		g.drop(contract.KindChart)
		if err := reconcileSignal(openSignal(), ""); err != nil {
			t.Fatalf("re-run after drop: %v", err)
		}
		if g.spawns != 2 || !slices.Equal(g.cuts, []string{"chart/ws-chart", "chart/ws-chart2"}) ||
			!slices.Equal(g.seq, []string{"carry ws-sig ws-chart", "carry ws-sig ws-chart2"}) {
			t.Errorf("spawns=%d cuts=%v seq=%v, want a new Chart, branch and carry", g.spawns, g.cuts, g.seq)
		}
	})
	t.Run("fast lane", func(t *testing.T) {
		evs := eventlogtest.Capture(t)
		g := stubSignalSeams(t)
		for range 2 {
			if err := SeedSpec(openSignal(), "", "body", ""); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
			if err := reconcileSignal(openSignal(), ""); err != nil {
				t.Fatalf("reconcileSignal: %v", err)
			}
			g.drop(contract.KindSpec)
		}
		if g.spawns != 2 || g.anchors != 2 || g.pins["ws-spec"] == "" || g.pins["ws-spec2"] == "" {
			t.Errorf("spawns=%d anchors=%d pins=%v, want a new Spec and a fresh pin (the old one inert)", g.spawns, g.anchors, g.pins)
		}
		if len(*evs) != 2 || (*evs)[1].Name != "triage.fastlane" || (*evs)[1].Attrs["spec"] != "ws-spec2" || (*evs)[1].Attrs["anchor"] != "tipsha" {
			t.Errorf("events = %+v, want a triage.fastlane per run, the last naming the new Spec and anchor", *evs)
		}
	})
}

// ADR 0129 (Spec D2, D4): Launch(signal) → one agent session anchored to the
// Signal whose prompt offers the four outcomes, the accept and fast-lane verbs,
// the priority call, and leaves the fog to Nami.
func TestLaunchSignalComposesTriageSession(t *testing.T) {
	h := &stubHarness{}
	lc, err := Luffy{}.Launch(stubCtx{h}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, ok := lc.(contract.AgentCommand); !ok || h.inv.BeadID != "ws-sig" {
		t.Fatalf("Launch = %T anchored to %q, want an agent session on the Signal", lc, h.inv.BeadID)
	}
	p := h.inv.Prompt
	for _, want := range []string{
		"You are Luffy",
		"### ACCEPT", "### FAST LANE", "### REJECT", "### DUPLICATE",
		"mugicrew reconcile ws-sig\n", `mugicrew reconcile ws-sig --spec <body-file> --title "<what is built>"`,
		"bd close ws-sig --reason", "bd supersede ws-sig --with",
		`--title "<Destination name>"`, "--priority <P0-P4>", "bd update <specID> --priority <P0-P4>",
		"No kind prefix", "Do not seed defog beads", "Inbox",
		contract.DraftInstruction("ws-sig"),
	} {
		if !strings.Contains(p, want) {
			t.Errorf("triage prompt missing %q", want)
		}
	}
	// The fast-laned Spec takes the standard template, so Zoro slices it like any Spec.
	_, fastLane, _ := strings.Cut(p, "### FAST LANE")
	fastLane, _, _ = strings.Cut(fastLane, "\n### ")
	if !slices.Contains(promptguard.CrewDocRefs(fastLane), "spec-template.md") {
		t.Errorf("FAST LANE must read the spec-template Crew doc:\n%s", fastLane)
	}
	// Luffy maps no fog: no defog-child create, no Not-yet-specified to write.
	_, accept, _ := strings.Cut(p, "### ACCEPT")
	accept, _, _ = strings.Cut(accept, "\n### ")
	if strings.Contains(accept, "bd create") || strings.Contains(p, "kind:<grilling|research|prototype>") {
		t.Errorf("triage prompt must not seed defog beads:\n%s", p)
	}
}

// Spec mc-r832 D4, FR4/FR7/FR8: ACCEPT's steps 1-4 say reconcile seeds the Survey
// beside the Chart (both Drafts, published together, never a second), find it,
// retitle it to the Destination leaving its body alone, and send the human to it.
func TestTriageAcceptSeedsAndTendsSurvey(t *testing.T) {
	_, accept, _ := strings.Cut(triagePrompt("ws-sig", "", "", nil, nil), "### ACCEPT")
	accept, _, _ = strings.Cut(accept, "\n### ")
	accept = strings.Join(strings.Fields(accept), " ")
	for _, want := range []string{
		"This spawns the Signal's one Chart (kind:chart, in succession to the Signal) as a Draft, seeds a Survey under it (kind:survey, the Chart's HITL charting leaf, also a Draft by the Signal), and cuts the chart/<id> branch; it does NOT close the Signal.",
		"It is idempotent — safe to re-run; it never makes a second Chart or Survey. Both stay Drafts until this session exits cleanly, when they are published together.",
		"bd list --parent <chartID> -l kind:survey --all --json",
		`Retitle the Survey to the same Destination name: bd update <surveyID> --title "<Destination name>"`, "Leave its body alone.",
		"Stop. Do not seed defog beads and do not sketch the fog: mapping it is Nami's. Tell the human the Chart's Survey now waits in the Inbox for Nami's charting session.",
	} {
		if !strings.Contains(accept, want) {
			t.Errorf("ACCEPT missing %q:\n%s", want, accept)
		}
	}
}

// ADR 0096: no reconcile closes the Signal; mugicrew does at the clean-exit publish.
func TestTriagePromptSaysSignalClosesAtPublish(t *testing.T) {
	got := strings.Join(strings.Fields(triagePrompt("ws-sig", "", "", nil, nil)), " ")
	if strings.Count(got, "does NOT close the Signal") != 3 || !strings.Contains(got, "mugicrew closes it when this session exits cleanly") {
		t.Errorf("triage prompt must say, on every reconcile leg, that mugicrew closes the Signal at publish:\n%s", got)
	}
}

// Spec D6 (ADR 0088) over the triage prompt as rendered: no skill call, no ADR
// citation, every crew-doc reference resolves, and it renders cleanly.
func TestTriagePromptSkillFree(t *testing.T) {
	p := triagePrompt("mc-x", "", "", nil, nil)
	promptguard.Check(t, "triage", p)
	refs := promptguard.CrewDocRefs(p)
	for _, d := range []string{"grilling.md", "domain-modeling.md", "chart-body.md", "spec-template.md"} {
		if !slices.Contains(refs, d) {
			t.Errorf("triage prompt reads no crew doc %s (refs %q)", d, refs)
		}
	}
	if strings.Contains(p, "%!") {
		t.Errorf("triage prompt has a stray %%:\n%s", p)
	}
}

// Reconcile has only a Signal arm.
func TestReconcileNonSignalErrors(t *testing.T) {
	if err := (Luffy{}).Reconcile(nil, bd.Bead{ID: "c", Kind: contract.KindChart}); err == nil {
		t.Error("reconcile of a non-Signal kind must error")
	}
}

// stubInitiative stands in for bd.InitiativeOf: a bead parented under ws-init is
// in an unclosed Initiative; any other parent (none, or a closed Initiative)
// yields none.
func stubInitiative(t *testing.T) {
	swap(t, &initiativeOf, func(b bd.Bead) (string, error) {
		if b.Parent == "ws-init" {
			return "ws-init", nil
		}
		return "", nil
	})
}

// Spec D2, FR11: the accept leg's Chart and the fast lane's Spec are born inside
// the Signal's Initiative, and outside one when the Signal has none.
func TestSignalSuccessorsJoinTheSignalsInitiative(t *testing.T) {
	for _, parent := range []string{"ws-init", "ws-closed-init", ""} {
		want := ""
		if parent == "ws-init" {
			want = parent
		}
		t.Run("accept/"+parent, func(t *testing.T) {
			g := stubSignalSeams(t)
			stubInitiative(t)
			sig := openSignal()
			sig.Parent = parent
			if err := reconcileSignal(sig, ""); err != nil {
				t.Fatalf("reconcileSignal: %v", err)
			}
			if got := g.bodies["ws-chart"].Parent; got != want {
				t.Errorf("Chart Parent = %q, want %q", got, want)
			}
		})
		t.Run("fastlane/"+parent, func(t *testing.T) {
			g := stubSignalSeams(t)
			stubInitiative(t)
			sig := openSignal()
			sig.Parent = parent
			if err := SeedSpec(sig, "", "the spec body", ""); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
			if got := g.bodies["ws-spec"].Parent; got != want {
				t.Errorf("Spec Parent = %q, want %q", got, want)
			}
		})
	}
}

// Spec D3, FR6: reconcile --initiative spawns the Signal's one caused-by
// Initiative — a container Draft by the Signal, never parented — carries the
// Signal's Ordering edges to it and logs triage.initiative; a re-run spawns
// nothing more, and none of it takes the Chart or fast-lane leg.
func TestSeedInitiativeSpawnsOneAndIsIdempotent(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	stubInitiative(t)
	sig := openSignal()
	sig.Parent = "ws-init" // a member Signal triaged INITIATIVE still births a top-level Initiative
	for range 2 {
		if err := SeedInitiative(sig, "the effort", "## Done when\n", ""); err != nil {
			t.Fatalf("SeedInitiative: %v", err)
		}
	}
	ini := g.bodies["ws-initiative"]
	if g.spawns != 1 || ini.Kind != contract.KindInitiative || !ini.Container || ini.Title != "the effort" ||
		ini.Description != "## Done when\n" || ini.DraftedBy != "ws-sig" || ini.Parent != "" {
		t.Errorf("spawns=%d initiative=%+v, want one unparented kind:initiative container Draft by the Signal", g.spawns, ini)
	}
	if want := []string{"carry ws-sig ws-initiative", "carry ws-sig ws-initiative"}; !slices.Equal(g.seq, want) {
		t.Errorf("seq = %v, want %v", g.seq, want)
	}
	if len(g.cuts) != 0 || g.anchors != 0 {
		t.Errorf("cuts=%v anchors=%d, want no branch and no anchor", g.cuts, g.anchors)
	}
	if len(*evs) != 2 || (*evs)[0].Name != "triage.initiative" || (*evs)[0].Attrs["initiative"] != "ws-initiative" {
		t.Errorf("events = %+v, want triage.initiative naming the Initiative per run", *evs)
	}
}

// The title defaults to the Signal's.
func TestSeedInitiativeDefaultTitle(t *testing.T) {
	g := stubSignalSeams(t)
	if err := SeedInitiative(openSignal(), "", "body", ""); err != nil {
		t.Fatalf("SeedInitiative: %v", err)
	}
	if got := g.bodies["ws-initiative"].Title; got != "sharp fix" {
		t.Errorf("title = %q, want the Signal's", got)
	}
}

// Spec D3: --initiative refuses a non-Signal, an empty body, a closed Signal, and
// a Signal beside a Chart or Spec successor, writing nothing; a closed Signal
// whose Initiative is already published re-runs as a no-op.
func TestSeedInitiativeRefusals(t *testing.T) {
	closed := openSignal()
	closed.Status = "closed"
	for name, tc := range map[string]struct {
		sig  bd.Bead
		body string
		succ contract.Kind
	}{
		"non-signal":    {bd.Bead{ID: "c", Kind: contract.KindChart}, "body", ""},
		"empty body":    {openSignal(), " \n", ""},
		"closed signal": {closed, "body", ""},
		"beside chart":  {openSignal(), "body", contract.KindChart},
		"beside spec":   {openSignal(), "body", contract.KindSpec},
	} {
		g := stubSignalSeams(t)
		if tc.succ != "" {
			g.succ[tc.succ] = "ws-" + string(tc.succ)
		}
		if err := SeedInitiative(tc.sig, "", tc.body, ""); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if g.spawns != 0 || len(g.seq) != 0 {
			t.Errorf("%s: spawns=%d seq=%v, want nothing written", name, g.spawns, g.seq)
		}
	}
	g := stubSignalSeams(t)
	g.succ[contract.KindInitiative] = "ws-initiative"
	if err := SeedInitiative(closed, "", "body", ""); err != nil || g.spawns != 0 || len(g.seq) != 0 {
		t.Errorf("closed Signal with its Initiative: err=%v spawns=%d seq=%v, want a no-op", err, g.spawns, g.seq)
	}
}

// Spec D3: the accept and fast-lane legs refuse a Signal that already has an
// Initiative successor, writing nothing.
func TestAcceptAndFastLaneRefuseBesideInitiative(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindInitiative] = "ws-initiative"
	if err := reconcileSignal(openSignal(), ""); err == nil {
		t.Error("accept beside an Initiative must error")
	}
	if err := SeedSpec(openSignal(), "", "body", ""); err == nil {
		t.Error("fast lane beside an Initiative must error")
	}
	if g.spawns != 0 || len(g.cuts) != 0 || len(g.seq) != 0 || g.anchors != 0 {
		t.Errorf("spawns=%d cuts=%v seq=%v anchors=%d, want nothing written", g.spawns, g.cuts, g.seq, g.anchors)
	}
}

// Spec D3, D4, FR2–FR8: the triage prompt offers INITIATIVE as a proposal awaiting
// the human's yes, with its reconcile verb, the three-section body, the priority
// call, and phase 1's member Signal filed as a Draft child of the Initiative.
func TestTriagePromptOffersInitiative(t *testing.T) {
	p := triagePrompt("ws-sig", "", "", nil, nil)
	_, ini, ok := strings.Cut(p, "### INITIATIVE")
	if !ok {
		t.Fatalf("triage prompt has no ### INITIATIVE outcome:\n%s", p)
	}
	ini, _, _ = strings.Cut(ini, "\n### ")
	for _, want := range []string{
		"yes",
		`mugicrew reconcile ws-sig --initiative <body-file> --title "<effort>"`,
		"## Done when", "## Phases", "## Notes",
		"bd update <initiativeID> --priority <P0-P4>",
		"crew doc signal-filing.md",
		"Initiative member form",
	} {
		if !strings.Contains(ini, want) {
			t.Errorf("INITIATIVE section missing %q:\n%s", want, ini)
		}
	}
	if !strings.Contains(p, "ACCEPT, FAST LANE, INITIATIVE, REJECT, or DUPLICATE") {
		t.Errorf("triage prompt must close on all five outcomes:\n%s", p)
	}
}

// Spec D3, D5, FR9: with the human's yes, INITIATIVE triage may adopt existing
// open chains as the first phases instead of filing a new member Signal.
func TestTriagePromptAdoptsChainsOnYes(t *testing.T) {
	_, ini, _ := strings.Cut(triagePrompt("ws-sig", "", "", nil, nil), "### INITIATIVE")
	ini, _, _ = strings.Cut(ini, "\n### ")
	for _, want := range []string{"mugicrew initiative adopt <head> <initiativeID>", "instead of filing", "yes"} {
		if !strings.Contains(ini, want) {
			t.Errorf("INITIATIVE section missing %q:\n%s", want, ini)
		}
	}
}

// A Signal labelled for a nested context cuts its Chart in the child from the child's
// trunk, and its fast-lane Anchor pin is read at the child's tip (Spec FR11, D3).
func TestNestedSignalChartAndAnchorGoToTheChild(t *testing.T) {
	g := stubSignalSeams(t)
	// git.RepoRoot below needs a repo: a scratch one, so the test needs no checkout.
	t.Chdir(t.TempDir())
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// A real unlabelled Signal; only its successors carry context:kid (no repoOf stub).
	if err := os.MkdirAll(".mugicrew", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".mugicrew/config.toml", []byte("[context.kid]\nrepo = \"child\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := git.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(child, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	swap(t, &successorBead, func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Labels: []string{"context:kid"}}, nil
	})
	var cuts, anchors []string
	swap(t, &repoCut, func(r git.Repo, name, base string) (bool, error) {
		cuts = append(cuts, string(r)+" "+name+" "+base)
		return true, nil
	})
	swap(t, &repoTip, func(r git.Repo) (string, error) {
		anchors = append(anchors, string(r))
		return "childsha", nil
	})
	var stamped []string
	swap(t, &stampCut, func(_ bd.Bead, id string, _ git.Repo, _ string) error {
		stamped = append(stamped, id)
		return nil
	})
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-chart"}; !slices.Equal(stamped, want) {
		t.Errorf("stamped = %v, want %v", stamped, want)
	}
	if want := []string{child + " chart/ws-chart main"}; !slices.Equal(cuts, want) || len(g.cuts) != 0 {
		t.Errorf("child cuts = %v, workspace cuts = %v, want %v and none", cuts, g.cuts, want)
	}
	g.drop(contract.KindChart)
	g.succ[contract.KindSpec] = "ws-spec"
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{child}; !slices.Equal(anchors, want) || g.anchors != 1 {
		t.Errorf("child tips = %v, pins written = %d, want %v and one pin", anchors, g.anchors, want)
	}
}

// Spec mc-dbxb FR2-FR10: the FAST LANE and INITIATIVE outcomes show their draft in
// the shared sibling doc pane and pass the shown body file to reconcile.
func TestTriageDraftsShowInGlowPane(t *testing.T) {
	p := triagePrompt("ws-sig", "", "", nil, nil)
	if strings.Contains(p, "%!") {
		t.Fatalf("triage prompt has a format artifact:\n%s", p)
	}
	for outcome, verb := range map[string]string{"### FAST LANE": "--spec <body-file>", "### INITIATIVE": "--initiative <body-file>"} {
		_, sec, _ := strings.Cut(p, outcome)
		sec, _, _ = strings.Cut(sec, "\n### ")
		sec = strings.Join(strings.Fields(sec), " ")
		for _, want := range []string{
			`tmux -S "${MUGICREW_HITL_PANE%%,*}" split-window -t "${MUGICREW_HITL_PANE#*,}" -h -l 40% -P -F '#{pane_id}' "${MUGICREW_MD_RENDERER:-mugicrew doc-pane} '<body-file>'"`,
			`tmux -S "${MUGICREW_HITL_PANE%%,*}" respawn-pane -k -t <doc-pane-id> "${MUGICREW_MD_RENDERER:-mugicrew doc-pane} '<body-file>'"`,
			"If MUGICREW_HITL_PANE is empty or unset, run no tmux at all",
			"If the renderer is not on PATH, skip the pane and say so in one line naming the renderer you looked for",
			"pass the body file you showed",
			verb,
		} {
			if !strings.Contains(sec, want) {
				t.Errorf("%s section missing %q", outcome, want)
			}
		}
	}
	if n := strings.Count(p, strings.TrimSpace(paneProcedure)); n != 2 {
		t.Errorf("pane procedure appears %d times, want 2 (FAST LANE, INITIATIVE)", n)
	}
}

func jiraSignal() bd.Bead {
	s := openSignal()
	s.ExternalRef = "jira-ABC-1"
	return s
}

// A Jira Signal never charts or fast-lanes: reconcileSignal refuses on both legs
// with no write, while a ref-less Signal still accepts.
func TestReconcileRefusesJiraSignal(t *testing.T) {
	g := stubSignalSeams(t)
	err := reconcileSignal(jiraSignal(), "")
	if err == nil || !strings.Contains(err.Error(), "INITIATIVE") {
		t.Errorf("Chart leg err = %v, want a refusal naming INITIATIVE", err)
	}
	g.succ[contract.KindSpec] = "ws-spec" // after a seeded Spec
	if err := reconcileSignal(jiraSignal(), ""); err == nil {
		t.Error("fast-lane leg must refuse a Jira Signal")
	}
	if g.spawns != 0 || g.anchors != 0 || len(g.seq) != 0 || len(g.cuts) != 0 {
		t.Errorf("spawns=%d anchors=%d seq=%v cuts=%v, want no write", g.spawns, g.anchors, g.seq, g.cuts)
	}
	delete(g.succ, contract.KindSpec)
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Errorf("ref-less Signal: %v", err)
	}
}

// The Initiative from a Jira Signal carries its ref and title over a passed --title.
func TestSeedInitiativeCarriesJiraRefAndTitle(t *testing.T) {
	g := stubSignalSeams(t)
	if err := SeedInitiative(jiraSignal(), "other", "body", ""); err != nil {
		t.Fatalf("SeedInitiative: %v", err)
	}
	if ini := g.bodies["ws-initiative"]; ini.ExternalRef != "jira-ABC-1" || ini.Title != "sharp fix" {
		t.Errorf("initiative = %+v, want the Signal's ref and title", ini)
	}
}

func TestTriagePromptJiraSectionOnlyForJiraRef(t *testing.T) {
	if strings.Contains(triagePrompt("ws-sig", "", "", nil, nil), "A Jira Signal") {
		t.Error("ref-less Signal must not get the Jira section")
	}
	if strings.Contains(triagePrompt("ws-sig", "other-1", "", nil, nil), "A Jira Signal") {
		t.Error("non-jira ref must not get the Jira section")
	}
	if !strings.Contains(triagePrompt("ws-sig", "jira-ABC-1", "", nil, nil), "## A Jira Signal") {
		t.Error("jira- ref Signal must get the Jira section")
	}
}

// ADR 0212: under gitlab the fast lane pins Anchor: <fetched sha> on the Spec, writes
// no anchor commit, and a re-run with the pin set makes no fetch.
func TestFastLaneGitlabPinsFetchedMainline(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec] = "ws-spec"
	body, fetches := "the spec body\n", 0
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
	swap(t, &showSpec, func(string) (bd.Bead, error) { return bd.Bead{Description: body}, nil })
	swap(t, &fetchForge, func() (string, error) { fetches++; return "forgesha", nil })
	swap(t, &updateBody, func(_ string, a []string) error { body = a[1]; return nil })
	for i := 0; i < 2; i++ {
		if err := reconcileSignal(openSignal(), ""); err != nil {
			t.Fatalf("reconcileSignal: %v", err)
		}
	}
	if sha, ok := git.ParseAnchor(body); !ok || sha != "forgesha" || fetches != 1 || g.anchors != 0 {
		t.Errorf("pin=(%q,%v) fetches=%d anchors=%d, want forgesha, one fetch, no commit", sha, ok, fetches, g.anchors)
	}
	for _, e := range *evs {
		if e.Attrs["anchor"] != "forgesha" {
			t.Errorf("event %+v, want anchor=forgesha", e)
		}
	}
}

func TestFastLaneNestedGitlabPinsChildTip(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec] = "ws-spec"
	body := "the spec body\n"
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
	swap(t, &successorBead, func(id string) (bd.Bead, error) { return bd.Bead{ID: id}, nil })
	swap(t, &repoOf, func(bd.Bead) git.Repo { return "kid" })
	swap(t, &showSpec, func(string) (bd.Bead, error) { return bd.Bead{Description: body}, nil })
	swap(t, &fetchForge, func() (string, error) { t.Fatal("workspace fetch"); return "", nil })
	swap(t, &repoFetch, func(r git.Repo) (string, error) { return "kid-tip", nil })
	swap(t, &updateBody, func(_ string, a []string) error { body = a[1]; return nil })
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatal(err)
	}
	if sha, ok := git.ParseAnchor(body); !ok || sha != "kid-tip" || g.anchors != 0 {
		t.Errorf("pin=(%q,%v) anchors=%d, want kid-tip and no commit", sha, ok, g.anchors)
	}
}
