package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
)

// contextCandidate is one Context init can offer at a terminal: a dir, its kind
// and the reason it was found (Spec D4).
type contextCandidate struct {
	Dir    string
	Repo   bool
	Reason string
}

// repoCandidateDepth is how deep below the root an ignored nested repo is looked for.
const repoCandidateDepth = 3

// manifestNames are the tracked files that mark a package dir as a path candidate.
var manifestNames = []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "pom.xml", "build.gradle", "build.gradle.kts", "mix.exs", "flake.nix"}

func isManifest(base string) bool {
	return slices.Contains(manifestNames, base) || strings.HasSuffix(base, ".csproj")
}

// discoverContexts lists the path candidates (outermost dirs below root with a
// tracked manifest) and the repo candidates (ignored dirs, to depth 3, that are
// their own git repo roots), leaving out any dir already in declared.
func discoverContexts(root string, declared map[string]bool) []contextCandidate {
	var out []contextCandidate
	if files, err := exec.Command("git", "-C", root, "ls-files", "-z").Output(); err == nil {
		reason := map[string]string{}
		for f := range strings.SplitSeq(string(files), "\x00") {
			if dir := path.Dir(f); dir != "." && isManifest(path.Base(f)) {
				if _, ok := reason[dir]; !ok {
					reason[dir] = "tracked " + path.Base(f)
				}
			}
		}
		for _, dir := range slices.Sorted(maps.Keys(reason)) {
			outer := true
			for p := path.Dir(dir); p != "."; p = path.Dir(p) {
				if _, ok := reason[p]; ok {
					outer = false
					break
				}
			}
			if outer && !declared[dir] {
				out = append(out, contextCandidate{Dir: dir, Reason: reason[dir]})
			}
		}
	}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if d.Name() == ".git" || d.Name() == "node_modules" || strings.Count(rel, string(filepath.Separator)) >= repoCandidateDepth {
			return filepath.SkipDir
		}
		if git.IsRepoRoot(p) {
			if exec.Command("git", "-C", root, "check-ignore", "-q", rel).Run() == nil && !declared[rel] {
				out = append(out, contextCandidate{Dir: rel, Repo: true, Reason: "ignored nested git repo"})
			}
			return filepath.SkipDir
		}
		return nil
	})
	return out
}

// deriveContextName cuts a dir's basename to the name charset: lower case, runs
// of other characters to one '-', no leading or trailing '-'.
func deriveContextName(dir string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(path.Base(dir)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func kindWord(repo bool) string {
	if repo {
		return "repo"
	}
	return "path"
}

const badName = "a context name matches ^[a-z0-9][a-z0-9-]*$"

// promptContexts is the terminal's Context stage: on a re-run each declared
// Context with Keep? [Y/n], then the numbered undeclared candidates, then the
// free loop. It returns the set to declare; ok is false when the input ended
// while an answer was unusable.
func promptContexts(root string, cur map[string]cfglayer.Context, pr *wsPrompter) (want []declaredContext, ok bool) {
	names, dirs := map[string]bool{}, map[string]bool{}
	add := func(c declaredContext) {
		want = append(want, c)
		names[c.Name], dirs[c.Dir] = true, true
	}
	declaredDirs := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(cur)) {
		c := cur[name]
		d := declaredContext{Name: name, Dir: c.Path, Repo: c.Repo != ""}
		if d.Repo {
			d.Dir = c.Repo
		}
		declaredDirs[d.Dir] = true
		keep, ok := pr.askBool(fmt.Sprintf("Keep %s (%s %s)?", name, kindWord(d.Repo), d.Dir), true, nil)
		if !ok {
			return nil, false
		}
		if keep {
			add(d)
		}
	}

	freeName := func(v string) string {
		if !contextName.MatchString(v) {
			return badName
		}
		if names[v] {
			return fmt.Sprintf("context name %q is taken", v)
		}
		return ""
	}
	if cands := discoverContexts(root, declaredDirs); len(cands) > 0 {
		fmt.Fprintln(pr.w, "Context candidates:")
		for i, c := range cands {
			fmt.Fprintf(pr.w, "  %d. %s  %s  %s  (%s)\n", i+1, deriveContextName(c.Dir), c.Dir, kindWord(c.Repo), c.Reason)
		}
		var picked []int
		_, ok := pr.ask("Declare which? (numbers, blank for none)", "", func(v string) string {
			picked = picked[:0]
			for f := range strings.FieldsSeq(strings.ReplaceAll(v, ",", " ")) {
				n, err := strconv.Atoi(f)
				if err != nil || n < 1 || n > len(cands) || slices.Contains(picked, n) {
					return fmt.Sprintf("answer numbers from 1 to %d, each once", len(cands))
				}
				picked = append(picked, n)
			}
			return ""
		})
		if !ok {
			return nil, false
		}
		slices.Sort(picked)
		for _, n := range picked {
			c := cands[n-1]
			name := deriveContextName(c.Dir)
			if freeName(name) != "" {
				if name, ok = pr.ask("Name for "+c.Dir, "", freeName); !ok {
					return nil, false
				}
			}
			add(declaredContext{Name: name, Dir: c.Dir, Repo: c.Repo})
		}
	}

	for {
		name, ok := pr.ask("Another Context: name (blank to finish)", "", func(v string) string {
			if v == "" {
				return ""
			}
			return freeName(v)
		})
		if !ok {
			return nil, false
		}
		if name == "" {
			return want, true
		}
		c := declaredContext{Name: name}
		if c.Dir, ok = pr.ask("  dir (relative to the Workspace root)", "", func(v string) string {
			if rule := checkContext(root, declaredContext{Name: name, Dir: v}); rule != "" {
				return rule
			}
			if dirs[v] {
				return fmt.Sprintf("context dir %q is taken", v)
			}
			return ""
		}); !ok {
			return nil, false
		}
		def := "path"
		if git.IsRepoRoot(filepath.Join(root, c.Dir)) {
			def = "repo"
		}
		kind, ok := pr.ask("  kind (path|repo)", def, func(v string) string {
			if v != "path" && v != "repo" {
				return "answer path or repo"
			}
			return checkContext(root, declaredContext{Name: name, Dir: c.Dir, Repo: v == "repo"})
		})
		if !ok {
			return nil, false
		}
		c.Repo = kind == "repo"
		add(c)
	}
}
