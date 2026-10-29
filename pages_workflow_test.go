package main

import (
	"os"
	"strings"
	"testing"
)

func readWorkflow(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no " + path + " in this tree")
	}
	return string(b)
}

// block returns the lines of the top-level-indented YAML block that starts at the first line
// equal to header, up to the next line indented no deeper than it.
func block(t *testing.T, doc, header string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if l != header {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		var out []string
		for _, n := range lines[i+1:] {
			if strings.TrimSpace(n) != "" && len(n)-len(strings.TrimLeft(n, " ")) <= indent {
				break
			}
			out = append(out, n)
		}
		return strings.Join(out, "\n")
	}
	t.Fatalf("no %q block", header)
	return ""
}

func TestPublishWorkflowIgnoresSitePaths(t *testing.T) {
	doc := readWorkflow(t, ".github/workflows/publish.yml")
	push := block(t, doc, "  push:")
	if !strings.Contains(push, "paths-ignore:") || !strings.Contains(push, "'site/**'") {
		t.Errorf("publish.yml push trigger lacks paths-ignore: ['site/**']:\n%s", push)
	}
	if !strings.Contains(block(t, doc, "on:"), "workflow_dispatch:") {
		t.Error("publish.yml lost workflow_dispatch")
	}
}

func TestPagesWorkflowShape(t *testing.T) {
	doc := readWorkflow(t, ".github/workflows/pages.yml")
	push := block(t, doc, "  push:")
	if !strings.Contains(push, "paths:") || !strings.Contains(push, "'site/**'") {
		t.Errorf("pages.yml push trigger lacks paths with site/**:\n%s", push)
	}
	perms := block(t, doc, "permissions:")
	for _, want := range []string{"pages: write", "id-token: write", "contents: read"} {
		if !strings.Contains(perms, want) {
			t.Errorf("pages.yml permissions lack %q", want)
		}
	}
	for _, want := range []string{"workflow_dispatch:", "github-pages", "concurrency:", "npm ci", "npm test", "actions/upload-pages-artifact@v4", "actions/deploy-pages@v4"} {
		if !strings.Contains(doc, want) {
			t.Errorf("pages.yml lacks %q", want)
		}
	}
}
