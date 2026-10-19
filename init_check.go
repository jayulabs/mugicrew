package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jira"
)

// Format checks for Workspace init's answers (Spec D5). Each returns the rule
// that was broken, "" when the answer is fine, so the flag path can refuse
// naming the flag and the prompt loop can re-ask with the same words. They
// check format only; live checks stay in doctor.

func checkLandStrategy(v string) string {
	if v != git.LandStrategyLocal && v != git.LandStrategyGitlab {
		return fmt.Sprintf("land strategy is %s or %s", git.LandStrategyLocal, git.LandStrategyGitlab)
	}
	return ""
}

func checkJiraSite(v string) string {
	if v != "" && !jira.ValidSite(v) {
		return "the Jira site is a bare host such as acme.atlassian.net, not a URL"
	}
	return ""
}

// checkExternalRef: enforcing an external ID is only valid when Initiatives are required.
func checkExternalRef(required, external bool) string {
	if external && !required {
		return "external_ref requires --require-initiatives true"
	}
	return ""
}

func parseBool(v string) (bool, string) {
	switch v {
	case "true":
		return true, ""
	case "false":
		return false, ""
	}
	return false, "the answer is true or false"
}

var contextName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// declaredContext is one parsed --context spec: a path or repo Context at Dir.
type declaredContext struct {
	Name, Dir string
	Repo      bool
}

// parseContextSpec reads name=dir (path), name=repo:dir (repo) or name=path:dir
// (path, for a dir that starts with repo:). Rule is "" when the spec is well formed.
func parseContextSpec(spec string) (c declaredContext, rule string) {
	name, rest, ok := strings.Cut(spec, "=")
	if !ok {
		return c, "a Context is name=dir, name=repo:dir or name=path:dir"
	}
	c.Name = name
	switch {
	case strings.HasPrefix(rest, "repo:"):
		c.Repo, c.Dir = true, strings.TrimPrefix(rest, "repo:")
	case strings.HasPrefix(rest, "path:"):
		c.Dir = strings.TrimPrefix(rest, "path:")
	default:
		c.Dir = rest
	}
	return c, ""
}

// checkContext is the format rules for one Context against the Workspace at root:
// the name charset; a dir that is relative, cleaned, inside root, not root, and
// existing; and for a repo dir, a git repo root the Workspace's .gitignore matches.
func checkContext(root string, c declaredContext) string {
	if !contextName.MatchString(c.Name) {
		return "a context name matches ^[a-z0-9][a-z0-9-]*$"
	}
	switch {
	case filepath.IsAbs(c.Dir):
		return "a context dir is relative to the Workspace root"
	case c.Dir == "." || c.Dir == "":
		return "a context dir is below the Workspace root, not the root itself"
	case c.Dir == ".." || strings.HasPrefix(c.Dir, "../"):
		return "a context dir is inside the Workspace"
	case filepath.Clean(c.Dir) != c.Dir:
		return fmt.Sprintf("a context dir is cleaned (%s)", filepath.Clean(c.Dir))
	}
	abs := filepath.Join(root, c.Dir)
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "a context dir must exist"
	}
	if c.Repo {
		if !git.IsRepoRoot(abs) {
			return "a repo context dir is a git repo root"
		}
		if err := git.RequireIgnored(abs); err != nil {
			return "a repo context dir is ignored by the Workspace's .gitignore (init never adds the entry)"
		}
	}
	return ""
}

// checkContextSet is the whole-set rule: no name or dir repeats.
func checkContextSet(cs []declaredContext) string {
	names, dirs := map[string]bool{}, map[string]bool{}
	for _, c := range cs {
		if names[c.Name] {
			return fmt.Sprintf("context name %q is declared twice", c.Name)
		}
		if dirs[c.Dir] {
			return fmt.Sprintf("context dir %q is declared twice", c.Dir)
		}
		names[c.Name], dirs[c.Dir] = true, true
	}
	return ""
}
