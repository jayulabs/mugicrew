package robin

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
)

// Entry is one usable declared Context: its name, its dir relative to the
// Workspace root, and whether that dir is its own git repo (repo =) or a subtree
// (path =).
type Entry struct {
	Name, Path string
	Repo       bool
}

// workspaceContexts is the gathering seam, swapped in tests.
var workspaceContexts = gatherContexts

// gatherContexts reads the declared [context.*] table and drops a repo = entry
// whose dir isn't a git repo root, as contexts.Contexts does for the launchers.
func gatherContexts() []Entry {
	var out []Entry
	for name, c := range cfglayer.Contexts(".") {
		if c.Repo != "" && !git.IsRepoRoot(filepath.Join(".", c.Repo)) {
			continue // unusable: held like an undeclared context
		}
		if c.Repo == "" && c.Path == "" {
			continue
		}
		out = append(out, Entry{Name: name, Path: strings.TrimSuffix(c.Repo+c.Path, "/"), Repo: c.Repo != ""})
	}
	return out
}

// layout is the "## Workspace layout" section: names and paths only, no doc
// content. "" when no Context is usable. Pure; appended after the rendered
// prompt, never through its template.
func layout(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	var b strings.Builder
	b.WriteString("## Workspace layout\n\n")
	b.WriteString("The Workspace repo itself: its root GLOSSARY-MAP.md, GLOSSARY.md, docs/adr/ and .mugicrew/CREW.md.\n\n")
	b.WriteString("Declared Contexts (names and paths only; for a Context at <path>, its domain sources are <path>/GLOSSARY.md, <path>/docs/adr/ and <path>/.mugicrew/CREW.md, read if present):\n\n")
	for _, e := range entries {
		p := strings.TrimSuffix(e.Path, "/")
		if e.Repo {
			fmt.Fprintf(&b, "- %s: nested repo at %s/ (its own git repo: git -C %s)\n", e.Name, p, p)
		} else {
			fmt.Fprintf(&b, "- %s: subtree at %s/\n", e.Name, p)
		}
	}
	b.WriteString("\nIn your opening reply, offer to discuss the Workspace repo and each Context listed here.")
	return b.String()
}
