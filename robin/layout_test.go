package robin

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
)

func TestLayoutRendering(t *testing.T) {
	tests := []struct {
		name    string
		in      []Entry
		want    []string
		notWant []string
	}{
		{"none", nil, nil, []string{"Workspace layout"}},
		{"subtree", []Entry{{Name: "web", Path: "apps/web"}},
			[]string{"## Workspace layout", "GLOSSARY-MAP.md", "- web: subtree at apps/web/", "<path>/GLOSSARY.md", "<path>/docs/adr/", "<path>/.mugicrew/CREW.md, read if present", "offer to discuss the Workspace repo and each Context"},
			[]string{"git -C"}},
		{"nested repo", []Entry{{Name: "kid", Path: "kid", Repo: true}},
			[]string{"- kid: nested repo", "its own git repo: git -C kid"}, []string{"subtree"}},
		{"trailing slash", []Entry{{Name: "kid", Path: "kid/", Repo: true}},
			[]string{"git -C kid", "at kid/ ("}, []string{"kid//"}},
		{"percent", []Entry{{Name: "p", Path: "a%sb"}}, []string{"subtree at a%sb/"}, nil},
	}
	for _, tc := range tests {
		got := layout(tc.in)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in:\n%s", tc.name, w, got)
			}
		}
		for _, w := range tc.notWant {
			if strings.Contains(got, w) {
				t.Errorf("%s: unexpected %q", tc.name, w)
			}
		}
	}
	if got := layout(nil); got != "" {
		t.Errorf("no Contexts: got %q", got)
	}
}

func TestLayoutSortedByName(t *testing.T) {
	got := layout([]Entry{{Name: "zeta", Path: "z"}, {Name: "alpha", Path: "a"}})
	if strings.Index(got, "- alpha:") > strings.Index(got, "- zeta:") {
		t.Errorf("not sorted by name:\n%s", got)
	}
}

func TestGatherDropsUnusableRepo(t *testing.T) {
	ws, _ := contextstest.Workspace(t)
	cfg := "[context.kid]\nrepo = \"kid\"\n[context.ghost]\nrepo = \"ghost\"\n[context.sub]\npath = \"apps/sub/\"\n"
	if err := os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got := gatherContexts()
	slices.SortFunc(got, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	want := []Entry{{Name: "kid", Path: "kid", Repo: true}, {Name: "sub", Path: "apps/sub"}}
	if !slices.Equal(got, want) {
		t.Errorf("gather = %v, want %v", got, want)
	}
}

func withContexts(t *testing.T, es []Entry) {
	old := workspaceContexts
	workspaceContexts = func() []Entry { return es }
	t.Cleanup(func() { workspaceContexts = old })
}

func TestLaunchCarriesLayoutPlainAndRetrospective(t *testing.T) {
	withContexts(t, []Entry{{Name: "kid", Path: "kid", Repo: true}})
	for _, body := range []string{"plain topic", "why\nTarget: mc-a\nSpine: mc-a mc-b"} {
		h, err := launchBody(contract.KindDiscussion, body, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(h.inv.Prompt, "## Workspace layout") || !strings.Contains(h.inv.Prompt, "git -C kid") {
			t.Errorf("body %q: prompt lacks the layout", body)
		}
	}
}

func TestLaunchLayoutPercentLiteral(t *testing.T) {
	withContexts(t, []Entry{{Name: "p", Path: "a%sb"}})
	h, err := launchBody(contract.KindDiscussion, "plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.inv.Prompt, "subtree at a%sb/") {
		t.Error("percent in a path must come through literally")
	}
}

func TestLaunchNoContextsIsTodaysPrompt(t *testing.T) {
	withContexts(t, nil)
	h, err := launchBody(contract.KindDiscussion, "plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, rest, _ := strings.Cut(h.inv.Prompt, "\n\n---\n\n"); rest != discussionPrompt("ws-disc")+"\n\n"+contract.ClosingLine() {
		t.Error("no Contexts must give exactly the plain discussion prompt")
	}
}

func TestRetrospectiveWithManyContextsUnderBudget(t *testing.T) {
	var es []Entry
	for i := range 20 {
		es = append(es, Entry{Name: "ctx" + string(rune('a'+i)), Path: strings.Repeat("nested/", 4) + "repo" + string(rune('a'+i)), Repo: true})
	}
	withContexts(t, es)
	h, err := launchBody(contract.KindDiscussion, "why\nTarget: mc-a\nSpine: mc-a mc-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(h.inv.Prompt); n > 16<<10 {
		t.Errorf("prompt is %d bytes, over the 16 KiB HITL budget", n)
	}
}
