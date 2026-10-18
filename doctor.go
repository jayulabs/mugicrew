package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jira"
	"jayulabs.org/mugicrew/loop"
)

// status is a doctor check's verdict; its string is the printed word.
type status string

const (
	stOK   status = "ok"
	stWarn status = "warn"
	stFail status = "FAIL"
	stSkip status = "skip"
)

// result is one printed doctor line.
type result struct {
	status       status
	name, detail string
	hint         string
}

// check is a name plus a function yielding one or more results. root is "" when
// the cwd is in no git repo.
type check func(root string) []result

// doctorBD runs bd with args in dir, inheriting the environment so BEADS_DIR
// applies; a var so tests stub it.
var doctorBD = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// runDoctor is `mugicrew doctor`: the read-only readiness check (GLOSSARY
// doctor). It runs the whole ordered list even after a failure and returns the
// exit code, 1 if any check failed.
func runDoctor(w io.Writer) int {
	root, err := git.RepoRoot()
	if err != nil {
		root = ""
	}
	code := 0
	for _, c := range doctorChecks {
		for _, r := range c(root) {
			line := fmt.Sprintf("%s  %s: %s", r.status, r.name, r.detail)
			if r.hint != "" {
				line += " — " + r.hint
			}
			fmt.Fprintln(w, line)
			if r.status == stFail {
				code = 1
			}
		}
	}
	return code
}

// doctorChecks is the fixed output order: tools, git repo, bead graph, bd store, (config,
// contexts, gate), agent profiles, preview renderer.
var doctorChecks = []check{
	checkTools,
	checkBinary,
	checkGitRepo,
	checkBeadGraph,
	checkBdStore,
	checkTrackedBeads,
	checkConfigLayers,
	checkContexts,
	checkGate,
	checkSequences,
	checkGraphRef,
	checkGitlab,
	checkJira,
	checkProfiles,
	checkMDRenderer,
	checkBrowser,
}

func checkGitRepo(root string) []result {
	if root == "" {
		return []result{{stFail, "git repo", "not inside a git repo", "run from inside the Workspace"}}
	}
	return []result{{stOK, "git repo", root, ""}}
}

func checkBeadGraph(root string) []result {
	if root == "" {
		return []result{{stSkip, "bead graph", "no git repo", ""}}
	}
	prefix, err := doctorBD(root, "config", "get", "issue_prefix")
	var where struct {
		Path string `json:"path"`
	}
	out, werr := doctorBD(root, "where", "--json")
	if err != nil || prefix == "" || werr != nil || json.Unmarshal([]byte(out), &where) != nil || where.Path == "" {
		return []result{{stFail, "bead graph", "not initialized or not resolvable", "run mugicrew init"}}
	}
	return []result{{stOK, "bead graph", "issue prefix " + prefix + " · " + graphSource(root, where.Path) + " · " + where.Path, ""}}
}

// graphSource says how bd found the graph at path, first match wins. Doctor runs
// before mugicrew exports its own BEADS_DIR, so a BEADS_DIR here is the user's.
func graphSource(root, path string) string {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	same := func(a, b string) bool { return resolve(a) == resolve(b) }
	if env := os.Getenv("BEADS_DIR"); env != "" && same(env, path) {
		return "BEADS_DIR"
	}
	if same(filepath.Join(root, ".beads"), path) {
		return "in-tree"
	}
	if _, err := os.Stat(filepath.Join(root, ".beads", "redirect")); err == nil {
		return "redirect"
	}
	return "routed"
}

// checkBdStore reports the bd store mode (ADR 0153) and fails when it is
// embedded, whatever the AFK pool size. An unreadable mode skips: the bead graph
// row is the single report of an unresolvable graph.
func checkBdStore(root string) []result {
	if root == "" {
		return []result{{stSkip, "bd store", "no git repo", ""}}
	}
	var ctx struct {
		DoltMode string `json:"dolt_mode"`
	}
	out, err := doctorBD(root, "context", "--json")
	if err != nil || json.Unmarshal([]byte(out), &ctx) != nil {
		return []result{{stSkip, "bd store", "mode unknown", ""}}
	}
	switch ctx.DoltMode {
	case "server":
		return []result{{stOK, "bd store", "server", ""}}
	case "embedded":
		detail := fmt.Sprintf("embedded, AFK pool %d", loop.PoolSetting(root).N)
		return []result{{stFail, "bd store", detail, `the TUI and loop need server mode: on a fresh clone run mugicrew init; else see README "Bd store mode" to migrate`}}
	}
	return []result{{stSkip, "bd store", "mode unknown", ""}}
}

// trackedBeadsFix is the untrack guidance shared by doctor and init's guard
// (ADR 0244), so the two texts cannot drift.
const trackedBeadsFix = "on a branch run `git rm -r --cached .beads`, merge it through the Workspace's usual MR or land flow, then re-run `mugicrew init`; " +
	"every other clone re-runs `mugicrew init` after it pulls (the pull deletes their .beads files). " +
	"Optional cleanup in the same MR: the bd block in CLAUDE.md/AGENTS.md and bd's root .gitignore entries"

// trackedBeads lists the paths under .beads/ that git's index tracks in root.
func trackedBeads(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", ".beads")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 }), nil
}

// checkTrackedBeads FAILs when the index tracks any path under .beads/.
func checkTrackedBeads(root string) []result {
	if root == "" {
		return []result{{stSkip, "beads", "no git repo", ""}}
	}
	paths, err := trackedBeads(root)
	switch {
	case err != nil:
		return []result{{stSkip, "beads", "cannot list tracked paths: " + err.Error(), ""}}
	case len(paths) > 0:
		return []result{{stFail, "beads tracked", strings.Join(paths, ", "), trackedBeadsFix}}
	}
	return []result{{stOK, "beads untracked", "no tracked .beads/ paths", ""}}
}

func checkConfigLayers(root string) []result {
	if root == "" {
		return []result{{stSkip, "config layers", "no git repo", ""}}
	}
	var out []result
	for _, l := range []struct{ name, path string }{{"shared", cfglayer.SharedPath(root)}, {"local", cfglayer.LocalPath(root)}} {
		name := "config " + l.name
		switch cfglayer.Decode(l.path, &struct{}{}) {
		case cfglayer.Unusable:
			out = append(out, result{stFail, name, l.path, "fix the TOML syntax or types; the layer is being ignored"})
		case cfglayer.Set:
			out = append(out, result{stOK, name, l.path, ""})
		default:
			out = append(out, result{stOK, name, "not present", ""})
		}
	}
	return out
}

// declaredContexts is the raw [context.*] table, as cfglayer.Contexts picks it
// (local layer wins as a whole) but without dropping entries that set both keys.
func declaredContexts(root string) map[string]cfglayer.Context {
	for _, path := range []string{cfglayer.LocalPath(root), cfglayer.SharedPath(root)} {
		var l struct{ Context map[string]cfglayer.Context }
		if cfglayer.Decode(path, &l) == cfglayer.Set && len(l.Context) > 0 {
			return l.Context
		}
	}
	return nil
}

func checkContexts(root string) []result {
	if root == "" {
		return []result{{stSkip, "contexts", "no git repo", ""}}
	}
	table := declaredContexts(root)
	if len(table) == 0 {
		return []result{{stOK, "contexts", "none declared", ""}}
	}
	names := make([]string, 0, len(table))
	for n := range table {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []result
	for _, n := range names {
		c, name := table[n], "context "+n
		switch fi, err := os.Stat(filepath.Join(root, c.Path)); {
		case c.Path != "" && c.Repo != "":
			out = append(out, result{stFail, name, "sets both path and repo; dropped as undeclared", "keep one of path or repo"})
		case c.Path != "" && (err != nil || !fi.IsDir()):
			out = append(out, result{stFail, name, "path " + c.Path + " is not a directory under the Workspace root", "fix path or create the directory"})
		case c.Repo != "" && !git.IsRepoRoot(filepath.Join(root, c.Repo)):
			out = append(out, result{stFail, name, "repo " + c.Repo + " is not a git repo", "git init it or fix repo"})
		default:
			out = append(out, result{stOK, name, cmp.Or(c.Path, c.Repo), ""})
		}
	}
	return out
}

func checkGate(root string) []result {
	if root == "" {
		return []result{{stSkip, "nix", "no git repo", ""}}
	}
	var set []string
	if cfglayer.Gate(root) != "" {
		set = append(set, "top-level gate")
	}
	table := declaredContexts(root)
	names := make([]string, 0, len(table))
	for n, c := range table {
		if c.Gate != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		set = append(set, "context "+n+" gate")
	}
	if len(set) == 0 {
		return nil
	}
	if _, err := lookPath("nix"); err != nil {
		return []result{{stFail, "nix", "not on PATH but set: " + strings.Join(set, ", "), "install nix; every gated land will fail without it"}}
	}
	return []result{{stOK, "nix", "on PATH", ""}}
}

// checkSequences runs Land's rule reader and overlap rule for the Workspace repo and
// each repo = context, so a broken sequences rule fails here before a Land does.
// Silent when only the built-in ADR rule applies.
func checkSequences(root string) []result {
	if root == "" {
		return []result{{stSkip, "sequences", "no git repo", ""}}
	}
	repos := map[string]string{"workspace": root}
	for n, dir := range cfglayer.Repos(root) {
		repos["context "+n] = filepath.Join(root, dir)
	}
	names := make([]string, 0, len(repos))
	for n := range repos {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []result
	for _, n := range names {
		name := "sequences " + n
		rules, err := cfglayer.SequenceRules(root, git.Repo(repos[n]))
		if err != nil {
			out = append(out, result{stFail, name, err.Error(), "fix the sequences rule; every land of this repo is refused"})
			continue
		}
		if len(rules) == 1 {
			continue
		}
		overlaps, err := git.Repo(repos[n]).TreeOverlaps(rules)
		if err != nil {
			out = append(out, result{stSkip, name, err.Error(), ""})
			continue
		}
		for _, o := range overlaps {
			out = append(out, result{stFail, name, o.Error(), "narrow a template so no file matches two"})
		}
		if len(overlaps) == 0 {
			out = append(out, result{stOK, name, fmt.Sprintf("%d rules, no overlap", len(rules)), ""})
		}
	}
	return out
}

func checkProfiles(root string) []result {
	if root == "" {
		return []result{{stSkip, "agent profiles", "no git repo", ""}}
	}
	common, _ := git.CommonDirOf(root)
	reasons := profileReasons(agentprofile.Load(root, common))
	names := make([]string, 0, len(reasons))
	for n := range reasons {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []result
	for _, n := range names {
		out = append(out, result{stWarn, "agent profile " + n, reasons[n], "fix the profile or its kind mapping"})
	}
	return out
}

// doctorForgeHasRef is the Forge ref listing; a var so tests stub it.
var doctorForgeHasRef = git.ForgeHasRef

// checkGraphRef warns under land_strategy=gitlab when the Forge carries bd's
// shared __dolt_remote_info__ branch (bd server started without
// DOLT_REMOTE_INFO_BRANCH=). Skipped, with no line, under any other strategy.
func checkGraphRef(root string) []result {
	if root == "" {
		return nil
	}
	if name, err := git.LandStrategyName(); err != nil || name != git.LandStrategyGitlab {
		return nil
	}
	has, err := doctorForgeHasRef(bd.DoltInfoRef)
	switch {
	case err != nil:
		return []result{{stWarn, "graph ref", "cannot list the Forge: " + err.Error(), ""}}
	case has:
		return []result{{stWarn, "graph ref", bd.DoltInfoRef + " on the Forge", "set DOLT_REMOTE_INFO_BRANCH= in .beads/.env (run mugicrew init), then restart the bd server; delete the branch by hand if wanted (mugicrew never does)"}}
	}
	return []result{{stOK, "graph ref", "no " + bd.DoltInfoRef + " on the Forge", ""}}
}

// Seams for checkGitlab's network calls; vars so tests stub them (nothing here fetches or moves main).
var (
	doctorGitlabProjects = git.GitlabProjects
	doctorForgeCheck     = forge.CheckAuth
	doctorForgeUser      = forge.CurrentUser
	doctorCredentialFill = git.CredentialFillEnv
	doctorForgeURL       = git.ForgeURL
	doctorForgeSettings  = func(p forge.Project) error {
		s, err := forge.ReadSettings(p)
		if err != nil {
			return err
		}
		return forge.ValidateSettings(s)
	}
)

// checkGitlab is doctor's read-only GitLab group, under land_strategy=gitlab only (no
// lines otherwise): the glab user, then one FAIL per project whose squash settings would
// refuse a land. It never calls git.PreflightGitlab, which fetches and moves main.
func checkGitlab(root string) []result {
	if root == "" {
		return nil
	}
	if !gitlabGate(root) {
		return nil
	}
	const authHint = "run one interactive web glab auth login (docs/install.md, GitLab section)"
	// The fill does not need glab or the project list (FR 29), so it prints whatever they say.
	creds := []result{credentialResult()}
	projs, err := doctorGitlabProjects()
	if err != nil {
		return append([]result{{stFail, "gitlab projects", err.Error(), "fix the Forge remote or the declared repos"}}, creds...)
	}
	if _, err := lookPath("glab"); err != nil {
		return creds // the tools row already reports a missing glab
	}
	proj := projs[0].Project
	if err := doctorForgeCheck(proj.Host); err != nil {
		return append([]result{{stFail, "glab user", "not authenticated for " + proj.Host + ": " + err.Error(), authHint}}, creds...)
	}
	user, err := doctorForgeUser(proj)
	if err != nil {
		return append([]result{{stFail, "glab user", err.Error(), authHint}}, creds...)
	}
	out := append([]result{{stOK, "glab user", user, ""}}, creds...)
	for _, e := range projs {
		name := "squash settings " + e.Project.ID
		if e.Context != "" {
			name += " (context " + e.Context + ")"
		}
		if err := doctorForgeSettings(e.Project); err != nil {
			out = append(out, result{stFail, name, err.Error(), ""})
		} else {
			out = append(out, result{stOK, name, "ok", ""})
		}
	}
	return out
}

// credentialResult is the git credential line: git credential fill under the injected glab
// helper every Forge call carries (ADR 0257), run from outside the repo so a repo-local
// helper cannot answer. OK, FAIL, or skip for an ssh remote. Never touches bd's sql-server.
func credentialResult() result {
	const name = "git credential"
	url, err := doctorForgeURL()
	if err != nil {
		return result{stFail, name, err.Error(), "fix the Forge remote"}
	}
	switch err := doctorCredentialFill(url, os.TempDir(), git.GlabHelperEnv(url)); {
	case errors.Is(err, git.ErrCredentialNA):
		return result{stSkip, name, "ssh remote", ""}
	case err != nil:
		host := url
		if p, perr := forge.ParseRemote(url); perr == nil {
			host = p.Host
		}
		return result{stFail, name, err.Error(), "glab cannot answer git for " + host + ": set glab's user (glab config set user <name> --host " + host + ") or re-run glab auth login (docs/install.md, GitLab section)"}
	}
	return result{stOK, name, "credential available", ""}
}

// checkMDRenderer follows MUGICREW_MD_RENDERER like the HITL pane wording
// (default the built-in mugicrew doc-pane); a set renderer's first word is looked up.
func checkMDRenderer(string) []result {
	r := strings.TrimSpace(os.Getenv("MUGICREW_MD_RENDERER"))
	if r == "" {
		return []result{{stOK, "md renderer", "built-in (mugicrew doc-pane)", ""}}
	}
	cmd := strings.Fields(r)[0]
	if _, err := lookPath(cmd); err != nil {
		return []result{{stWarn, "md renderer", cmd + " not on PATH", "Spec, Decision Packet and Initiative previews show inline only; set MUGICREW_MD_RENDERER to another renderer"}}
	}
	return []result{{stOK, "md renderer", cmd + " on PATH", ""}}
}

// checkBrowser follows $BROWSER like the Sanji prototype hand-over does; only
// the command's first word is looked up.
func checkBrowser(string) []result {
	const hint = "prototype hand-over prints the target only; set BROWSER"
	f := strings.Fields(os.Getenv("BROWSER"))
	if len(f) == 0 {
		return []result{{stWarn, "browser", "BROWSER unset", hint}}
	}
	if _, err := lookPath(f[0]); err != nil {
		return []result{{stWarn, "browser", f[0] + " not on PATH", hint}}
	}
	return []result{{stOK, "browser", f[0] + " on PATH", ""}}
}

// checkJira is the Jira group, silent without [jira]: intake would read the
// site acli is logged in to, so a malformed [jira] site, a missing login or a
// different active site FAILs here before an intake does.
func checkJira(root string) []result {
	if root == "" {
		return nil
	}
	if !jiraGate(root) {
		return nil
	}
	site, file := cfglayer.JiraSite(root)
	if !jira.ValidSite(site) {
		return []result{{stFail, "jira site", site, "[jira] site must be a bare host such as acme.atlassian.net; set it in " + file}}
	}
	active, err := doctorACLISite()
	switch {
	case errors.Is(err, jira.ErrNotLoggedIn):
		return []result{{stFail, "acli site", "not logged in", "acli jira auth login"}}
	case errors.Is(err, jira.ErrNotFound):
		return nil // the tools row already reports a missing acli
	case err != nil:
		return []result{{stFail, "acli site", err.Error(), "acli jira auth login"}}
	case active != "" && active != site:
		return []result{{stFail, "acli site", fmt.Sprintf("acli is on %s, [jira] site is %s", active, site), "acli jira auth switch --site " + site}}
	}
	return []result{{stOK, "acli site", site, ""}}
}

// doctorACLISite reads acli's active site; a var so tests stub it.
var doctorACLISite = jira.ActiveSite
