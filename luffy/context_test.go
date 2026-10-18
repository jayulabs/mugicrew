package luffy

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

func declare(t *testing.T, m map[string]string) {
	swap(t, &declaredContexts, func() map[string]string { return m })
}

func TestValidateContextRejectsUndeclared(t *testing.T) {
	declare(t, map[string]string{"api": "apps/api/"})
	if err := ValidateContext("web"); err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("ValidateContext(web) = %v, want an undeclared-context error", err)
	}
	if err := ValidateContext("api"); err != nil {
		t.Fatalf("ValidateContext(api): %v", err)
	}
	declare(t, nil)
	if err := ValidateContext("api"); err == nil {
		t.Fatal("ValidateContext with no declared contexts must refuse a name")
	}
}

func TestReconcileContextStampsChartAndFastLaneSpec(t *testing.T) {
	declare(t, map[string]string{"api": "apps/api/"})
	g := stubSignalSeams(t)
	if err := reconcileSignal(openSignal(), "api"); err != nil {
		t.Fatal(err)
	}
	if got := g.bodies["ws-chart"].Context; got != "api" {
		t.Errorf("Chart context = %q, want api", got)
	}
	g = stubSignalSeams(t)
	if err := SeedSpec(openSignal(), "", "body", "api"); err != nil {
		t.Fatal(err)
	}
	if got := g.bodies["ws-spec"].Context; got != "api" {
		t.Errorf("Spec context = %q, want api", got)
	}
}

func TestTriagePromptContextQuestion(t *testing.T) {
	base := triagePrompt("ws-sig", "", "", nil, nil)
	if got := triagePrompt("ws-sig", "", "", map[string]string{}, nil); got != base {
		t.Error("empty contexts must leave the prompt byte-identical")
	}
	got := triagePrompt("ws-sig", "", "", map[string]string{"web": "apps/web/", "api": "apps/api"}, nil)
	rest, ok := strings.CutPrefix(got, base)
	if !ok {
		t.Fatal("the context question must be appended, not woven in")
	}
	for _, want := range []string{"api (apps/api/)", "web (apps/web/)", "--context <name>", "none"} {
		if !strings.Contains(rest, want) {
			t.Errorf("question missing %q:\n%s", want, rest)
		}
	}
	if strings.Index(rest, "api (") > strings.Index(rest, "web (") {
		t.Error("entries must be sorted")
	}
}

func TestSignalContextLabelPreBindsSuccessor(t *testing.T) {
	declare(t, map[string]string{"api": "apps/api/", "web": "apps/web/"})
	cases := []struct {
		name, label, flag, want string
		refuse                  bool
	}{
		{"loose, flag passes through", "", "api", "api", false},
		{"label alone", "api", "", "api", false},
		{"label matches flag", "api", "api", "api", false},
		{"label differs from flag", "api", "web", "", true},
		{"undeclared label", "typo", "", "", true},
	}
	paths := map[string]func(bd.Bead, string) (error, string){
		"accept": func(s bd.Bead, f string) (error, string) { return reconcileSignal(s, f), "ws-chart" },
		"fast":   func(s bd.Bead, f string) (error, string) { return SeedSpec(s, "", "body", f), "ws-spec" },
	}
	for path, run := range paths {
		for _, c := range cases {
			t.Run(path+"/"+c.name, func(t *testing.T) {
				g := stubSignalSeams(t)
				sig := openSignal()
				if c.label != "" {
					sig.Labels = []string{"context:" + c.label}
				}
				err, id := run(sig, c.flag)
				if c.refuse {
					if err == nil || !strings.Contains(err.Error(), c.label) || len(g.bodies) != 0 {
						t.Fatalf("err = %v, spawned %d; want a refusal naming the label, no spawn", err, len(g.bodies))
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if got := g.bodies[id].Context; got != c.want {
					t.Errorf("context = %q, want %q", got, c.want)
				}
			})
		}
	}
}

func TestTriagePromptRepoContextRules(t *testing.T) {
	ctxs := map[string]string{"api": "proj-a/", "docs": "docs/"}
	swap(t, &repoContexts, func() map[string]string { return map[string]string{"api": "proj-a"} })
	got := triagePrompt("ws-sig", "", "", ctxs, nil)
	for _, want := range []string{"Never none", "Cross-repo split", "api (proj-a/)", "[<context>]", "ADR 0085"} {
		if !strings.Contains(got, want) {
			t.Errorf("repo rules missing %q", want)
		}
	}
	if strings.Contains(got, "Repo contexts: api (proj-a/), docs") {
		t.Error("path = context listed as a repo context")
	}
	swap(t, &repoContexts, func() map[string]string { return nil })
	got = triagePrompt("ws-sig", "", "", ctxs, nil)
	if strings.Contains(got, "Never none") || strings.Contains(got, "Cross-repo split") {
		t.Error("path-only Workspace must get neither rule")
	}
}

func TestTriagePromptPreBoundOnlyWhenLabelled(t *testing.T) {
	ctxs := map[string]string{"api": "proj-a/"}
	if strings.Contains(triagePrompt("ws-sig", "", "", ctxs, nil), "Pre-bound member") {
		t.Error("loose Signal got the pre-bound paragraph")
	}
	got := triagePrompt("ws-sig", "", "api", ctxs, nil)
	if !strings.Contains(got, "Pre-bound member") || !strings.Contains(got, "context:api") {
		t.Error("labelled Signal missing the pre-bound paragraph")
	}
}
