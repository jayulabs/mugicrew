package luffy

import (
	"jayulabs.org/mugicrew/cfglayer"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func stubRequire(t *testing.T, on bool, parent string, inis ...bd.Bead) {
	t.Helper()
	swap(t, &initiativesSetting, func() cfglayer.Initiatives { return cfglayer.Initiatives{Required: on} })
	swap(t, &show, func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindSignal, Parent: parent}, nil
	})
	all := append([]bd.Bead{{ID: "ws-chart", Kind: contract.KindChart}}, inis...)
	swap(t, &listAll, func() ([]bd.Bead, error) { return all, nil })
}

func launchPrompt(t *testing.T) string {
	t.Helper()
	h := &stubHarness{}
	if _, err := (Luffy{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	return h.inv.Prompt
}

func TestTriagePromptRequireInitiatives(t *testing.T) {
	open := bd.Bead{ID: "ws-b", Kind: contract.KindInitiative, Status: "open", Title: "B", Description: "## Done when\n\nB done.\n"}
	closed := bd.Bead{ID: "ws-c", Kind: contract.KindInitiative, Status: "closed", Title: "C"}
	open2 := bd.Bead{ID: "ws-a", Kind: contract.KindInitiative, Status: "open", Title: "A", Description: "## Done when\n\nA done."}

	stubRequire(t, false, "")
	off := launchPrompt(t)
	if !strings.HasSuffix(off, triagePrompt("ws-sig", "", "", declaredContexts(), nil)) || strings.Contains(off, "Require Initiatives") {
		t.Error("setting off must render today's prompt")
	}

	stubRequire(t, true, "ws-ini", open)
	if launchPrompt(t) != off {
		t.Error("a parented Signal gets no section")
	}

	stubRequire(t, true, "", open, closed, open2)
	got := launchPrompt(t)
	rest, ok := strings.CutPrefix(got, off)
	if !ok {
		t.Fatal("the section must be appended")
	}
	for _, want := range []string{"## Require Initiatives", "ws-a — A", "A done.", "ws-b — B", "B done.", "mugicrew initiative adopt ws-sig <initiativeID>", "REJECT; (d) DUPLICATE"} {
		if !strings.Contains(rest, want) {
			t.Errorf("section missing %q:\n%s", want, rest)
		}
	}
	if strings.Contains(rest, "ws-c") || strings.Contains(rest, "ws-chart") {
		t.Error("only open Initiatives are listed")
	}
	if strings.Index(rest, "ws-a —") > strings.Index(rest, "ws-b —") {
		t.Error("Initiatives sorted by ID")
	}
}

func TestTriagePromptRequireComposesWithAdoptionAndJira(t *testing.T) {
	p := triagePrompt("ws-sig", "jira-X-1", "", nil, []bd.Bead{{ID: "ws-o"}}, requireSection("ws-sig", nil, false))
	a, r, j := strings.Index(p, "## Adoption offer"), strings.Index(p, "## Require Initiatives"), strings.Index(p, "## A Jira Signal")
	if a < 0 || r < a || j < r {
		t.Errorf("want adoption < require < jira, got %d %d %d", a, r, j)
	}
}

func TestReconcileRequireInitiativesRefusesUnparented(t *testing.T) {
	stubInitiative(t)
	for _, leg := range []string{"accept", "fastlane"} {
		g := stubSignalSeams(t)
		swap(t, &initiativesSetting, func() cfglayer.Initiatives { return cfglayer.Initiatives{Required: true} })
		run := func(sig bd.Bead) error {
			if leg == "accept" {
				return reconcileSignal(sig, "")
			}
			return SeedSpec(sig, "", "body", "")
		}
		err := run(openSignal())
		if err == nil || !strings.Contains(err.Error(), "mugicrew initiative adopt") || !strings.Contains(err.Error(), openSignal().ID) {
			t.Fatalf("%s: unparented must be refused naming adopt, got %v", leg, err)
		}
		if g.spawns != 0 || g.anchors != 0 || len(g.cuts) != 0 || len(g.seq) != 0 {
			t.Errorf("%s: refusal must write nothing: %+v", leg, g)
		}
		member := openSignal()
		member.Parent = "ws-init"
		if err := run(member); err != nil {
			t.Errorf("%s: parented Signal must pass: %v", leg, err)
		}
	}
}

func TestReconcileRequireInitiativesRerunConverges(t *testing.T) {
	stubInitiative(t)
	g := stubSignalSeams(t)
	swap(t, &initiativesSetting, func() cfglayer.Initiatives { return cfglayer.Initiatives{Required: true} })
	g.succ[contract.KindChart] = "ws-chart"
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Errorf("charted before the switch must converge: %v", err)
	}
	g = stubSignalSeams(t)
	swap(t, &initiativesSetting, func() cfglayer.Initiatives { return cfglayer.Initiatives{Required: true} })
	g.succ[contract.KindSpec] = "ws-spec"
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Errorf("fast-laned before the switch must converge: %v", err)
	}
	if err := SeedSpec(openSignal(), "", "body", ""); err != nil {
		t.Errorf("SeedSpec re-run with a Spec must pass: %v", err)
	}
}

func TestTriagePromptAsksForRefOnlyUnderExternalRef(t *testing.T) {
	if strings.Contains(requireSection("ws-sig", nil, false), "--ref") {
		t.Error("ref line without external_ref")
	}
	if got := requireSection("ws-sig", nil, true); !strings.Contains(got, "INITIATIVE needs a ref") || !strings.Contains(got, "--ref <R>") {
		t.Errorf("ref line missing:\n%s", got)
	}
}

// ref resolution: --ref > the Signal's own (jira- always, any under the setting); the
// Initiative carries it trimmed.
func TestSeedInitiativeRefResolution(t *testing.T) {
	plain := func() bd.Bead { s := openSignal(); s.ExternalRef = "gh-7"; return s }
	for _, tc := range []struct {
		name  string
		sig   bd.Bead
		flag  string
		on    bool
		want  string
		fails bool
	}{
		{"flag wins", plain(), "  PROJ-1 ", true, "PROJ-1", false},
		{"flag off-setting", plain(), "PROJ-1", false, "PROJ-1", false},
		{"signal ref under setting", plain(), "", true, "gh-7", false},
		{"non-jira signal ref not carried off-setting", plain(), "", false, "", false},
		{"jira ref carried off-setting", jiraSignal(), "", false, "jira-ABC-1", false},
		{"no ref under setting", openSignal(), "", true, "", true},
		{"blank ref under setting", openSignal(), "   ", true, "", true},
	} {
		g := stubSignalSeams(t)
		swap(t, &initiativesSetting, func() cfglayer.Initiatives { return cfglayer.Initiatives{Required: true, ExternalRef: tc.on} })
		err := SeedInitiative(tc.sig, "", "body", tc.flag)
		if (err != nil) != tc.fails || g.spawns != btoi(!tc.fails) {
			t.Errorf("%s: err=%v spawns=%d", tc.name, err, g.spawns)
		}
		if got := g.bodies["ws-initiative"].ExternalRef; got != tc.want {
			t.Errorf("%s: ref = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// a ref another Initiative carries (open or closed) is refused naming it, before any
// write; the Signal's own spawned Initiative is not a clash, so a re-run passes.
func TestSeedInitiativeRefUniqueness(t *testing.T) {
	g := stubSignalSeams(t)
	var asked string
	swap(t, &initiativesByRef, func(r string) ([]bd.Bead, error) {
		asked = r
		return []bd.Bead{{ID: "ws-old", Kind: contract.KindInitiative, Status: "closed"}}, nil
	})
	err := SeedInitiative(openSignal(), "", "body", "PROJ-1")
	if err == nil || !strings.Contains(err.Error(), "ws-old") || g.spawns != 0 || asked != "PROJ-1" {
		t.Fatalf("err=%v spawns=%d asked=%q, want a refusal naming ws-old", err, g.spawns, asked)
	}
	g.succ[contract.KindInitiative] = "ws-old" // the Signal's own Initiative
	if err := SeedInitiative(openSignal(), "", "body", "PROJ-1"); err != nil {
		t.Errorf("re-run: %v", err)
	}
}
