package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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

// result is one doctor row. pre and fix are set on every row that can warn or
// FAIL; ok and skip rows leave them zero (ADR 0302).
type result struct {
	status       status
	name, detail string
	pre          string // what the row tests, one sentence
	fix          fix
}

// quiet is an ok or skip row: one roll-call line, no precondition or fix.
func quiet(st status, name, detail string) result {
	return result{status: st, name: name, detail: detail}
}

// problem is a warn or FAIL row with its precondition and fix.
func problem(st status, name, detail, pre string, f fix) result {
	return result{st, name, detail, pre, f}
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

// doctorUsage is what `mugicrew doctor` prints for an argument it does not know.
const doctorUsage = "usage: mugicrew doctor [fix]\n  mugicrew doctor      check the environment and Workspace are ready (read-only)\n  mugicrew doctor fix  apply doctor's safe fixes, then re-check\n"

// runDoctorCmd dispatches `mugicrew doctor [args]`: bare is the check, `fix` the
// fix runner, anything else usage and exit 1.
func runDoctorCmd(args []string, w io.Writer) int {
	switch {
	case len(args) == 0:
		return runDoctor(w)
	case len(args) == 1 && args[0] == "fix":
		return runDoctorFix(w)
	}
	fmt.Fprint(w, doctorUsage)
	return 1
}

// doctorExec runs a safe fix's argv, no shell and stdin at /dev/null, returning
// its combined output; a var so tests stub it.
var doctorExec = func(argv []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runDoctorFix is `mugicrew doctor fix`: run every check once, apply each
// warn/FAIL row's safe fix in roll-call order (a failure does not stop the
// next), list the manual rows left, then re-check. It returns 1 if the
// re-check has a FAIL. It touches only what the fixes touch: no event log, no
// bead graph (ADR 0301 §5).
func runDoctorFix(w io.Writer) int {
	s := styleFor(w)
	rows := doctorRows()
	fmt.Fprintf(w, "%s\n", s.bold("Applying safe fixes"))
	applied := 0
	for _, r := range rows {
		if !needsFix(r) || r.fix.safe == nil {
			continue
		}
		applied++
		cmdline := strings.Join(r.fix.safe, " ")
		out, err := doctorExec(r.fix.safe)
		if err == nil {
			fmt.Fprintf(w, "applied  %s: %s\n", r.name, cmdline)
			if line := firstLine(out); line != "" {
				fmt.Fprintf(w, "  %s\n", line)
			}
			continue
		}
		fmt.Fprintf(w, "failed   %s: %s\n", r.name, cmdline)
		if out != "" {
			fmt.Fprintf(w, "  %s\n", strings.ReplaceAll(out, "\n", "\n  "))
		}
		fmt.Fprintln(w, "  now manual:")
		for _, st := range r.fix.steps {
			for _, l := range s.wrap(st, "    • ", "      ") {
				fmt.Fprintln(w, l)
			}
		}
		if r.fix.more != "" {
			fmt.Fprintf(w, "    %s\n", s.dim("More: "+r.fix.more))
		}
	}
	if applied == 0 {
		fmt.Fprintln(w, "nothing safe to apply")
	}
	first := true
	for _, r := range rows {
		if !needsFix(r) || r.fix.safe != nil {
			continue
		}
		if first {
			fmt.Fprintf(w, "\n%s\n", s.bold("Left for you (manual)"))
			first = false
		}
		fmt.Fprintln(w, s.rollCall(r))
	}
	fmt.Fprintf(w, "\n%s\n", s.bold("Re-check"))
	return runDoctor(w)
}

// firstLine is the first non-blank line of out.
func firstLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// runDoctor is the bare `mugicrew doctor`: the readiness check (GLOSSARY
// doctor), which fixes nothing. It runs the whole ordered list even after a
// failure and returns the exit code, 1 if any check failed.
func runDoctor(w io.Writer) int {
	rows := doctorRows()
	fmt.Fprint(w, styleFor(w).report(rows))
	for _, r := range rows {
		if r.status == stFail {
			return 1
		}
	}
	return 0
}

// doctorRows runs every check, even after a failure, and collects their rows.
func doctorRows() []result {
	root, err := git.RepoRoot()
	if err != nil {
		root = ""
	}
	var rows []result
	for _, c := range doctorChecks {
		rows = append(rows, c(root)...)
	}
	return rows
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
	checkWorkspaceSetUp,
	checkContexts,
	checkGate,
	checkSequences,
	checkGitlab,
	checkJira,
	checkProfiles,
	checkMDRenderer,
	checkBrowser,
}

func checkGitRepo(root string) []result {
	if root == "" {
		return []result{problem(stFail, "git repo", "not inside a git repo", "the current directory is inside a git repo",
			manual("", "run from inside the Workspace"))}
	}
	return []result{quiet(stOK, "git repo", root)}
}

func checkBeadGraph(root string) []result {
	if root == "" {
		return []result{quiet(stSkip, "bead graph", "no git repo")}
	}
	prefix, err := doctorBD(root, "config", "get", "issue_prefix")
	var where struct {
		Path string `json:"path"`
	}
	out, werr := doctorBD(root, "where", "--json")
	if err != nil || prefix == "" || werr != nil || json.Unmarshal([]byte(out), &where) != nil || where.Path == "" {
		return []result{problem(stFail, "bead graph", "not initialized or not resolvable", "a Bead Graph resolves for this clone",
			manual("", "run `mugicrew init`"))}
	}
	return []result{quiet(stOK, "bead graph", "issue prefix "+prefix+" · "+graphSource(root, where.Path)+" · "+where.Path)}
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
		return []result{quiet(stSkip, "bd store", "no git repo")}
	}
	var ctx struct {
		DoltMode string `json:"dolt_mode"`
	}
	out, err := doctorBD(root, "context", "--json")
	if err != nil || json.Unmarshal([]byte(out), &ctx) != nil {
		return []result{quiet(stSkip, "bd store", "mode unknown")}
	}
	switch ctx.DoltMode {
	case "server":
		return []result{quiet(stOK, "bd store", "server")}
	case "embedded":
		detail := fmt.Sprintf("embedded, AFK pool %d", loop.PoolSetting(root).N)
		return []result{problem(stFail, "bd store", detail, "bd runs in server mode; the TUI and loop need it",
			manual("", "on a fresh clone run `mugicrew init`", `otherwise see README "Bd store mode" to migrate`))}
	}
	return []result{quiet(stSkip, "bd store", "mode unknown")}
}

// trackedBeadsSteps is the untrack guidance shared by doctor and init's guard
// (ADR 0244), so the two texts cannot drift.
var trackedBeadsSteps = []string{
	"on a branch run `git rm -r --cached .beads`",
	"merge it through the Workspace's usual MR or land flow",
	"re-run `mugicrew init`",
	"every other clone re-runs `mugicrew init` after it pulls (the pull deletes their .beads files)",
	"optional, in the same MR: the bd block in CLAUDE.md/AGENTS.md and bd's root .gitignore entries",
}

// trackedBeadsResult is the beads tracked row for paths; doctor and init build it
// here so their fix cannot drift.
func trackedBeadsResult(paths []string) result {
	return problem(stFail, "beads tracked", strings.Join(paths, ", "), "git's index tracks no path under .beads/",
		manual("", trackedBeadsSteps...))
}

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
		return []result{quiet(stSkip, "beads", "no git repo")}
	}
	paths, err := trackedBeads(root)
	switch {
	case err != nil:
		return []result{quiet(stSkip, "beads", "cannot list tracked paths: "+err.Error())}
	case len(paths) > 0:
		return []result{trackedBeadsResult(paths)}
	}
	return []result{quiet(stOK, "beads untracked", "no tracked .beads/ paths")}
}

func checkConfigLayers(root string) []result {
	if root == "" {
		return []result{quiet(stSkip, "config layers", "no git repo")}
	}
	var out []result
	for _, l := range []struct{ name, path string }{{"shared", cfglayer.SharedPath(root)}, {"local", cfglayer.LocalPath(root)}} {
		name := "config " + l.name
		switch cfglayer.Decode(l.path, &struct{}{}) {
		case cfglayer.Unusable:
			out = append(out, problem(stFail, name, l.path, "the config layer decodes as TOML with the expected types",
				manual("", "fix the TOML syntax or types", "until then the layer is being ignored")))
		case cfglayer.Set:
			out = append(out, quiet(stOK, name, l.path))
		default:
			out = append(out, quiet(stOK, name, "not present"))
		}
	}
	return out
}

// checkWorkspaceSetUp warns, never FAILs, when the shared layer lacks the keys
// `mugicrew init workspace` writes. An unusable shared layer already FAILs in
// the config layers row, so this row skips.
func checkWorkspaceSetUp(root string) []result {
	if root == "" {
		return []result{quiet(stSkip, "workspace setup", "no git repo")}
	}
	switch set, st := cfglayer.WorkspaceSetUp(root); {
	case st == cfglayer.Unusable:
		return []result{quiet(stSkip, "workspace setup", "shared config is unusable")}
	case set:
		return []result{quiet(stOK, "workspace setup", "land_strategy and [initiatives] required are set")}
	}
	return []result{problem(stWarn, "workspace setup", "the Workspace is not set up; run mugicrew init workspace",
		"the shared config sets land_strategy and [initiatives] required",
		manual("first-human-in-a-workspace", "run mugicrew init workspace", "until then the Workspace runs on defaults"))}
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
		return []result{quiet(stSkip, "contexts", "no git repo")}
	}
	table := declaredContexts(root)
	if len(table) == 0 {
		return []result{quiet(stOK, "contexts", "none declared")}
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
			out = append(out, problem(stFail, name, "sets both path and repo; dropped as undeclared", "the context sets exactly one of path or repo",
				manual("", "keep one of path or repo")))
		case c.Path != "" && (err != nil || !fi.IsDir()):
			out = append(out, problem(stFail, name, "path "+c.Path+" is not a directory under the Workspace root", "the context path is a directory under the Workspace root",
				manual("", "fix path", "or create the directory")))
		case c.Repo != "" && !git.IsRepoRoot(filepath.Join(root, c.Repo)):
			out = append(out, problem(stFail, name, "repo "+c.Repo+" is not a git repo", "the context repo is a git repo under the Workspace root",
				manual("", "`git init` it", "or fix repo")))
		default:
			out = append(out, quiet(stOK, name, cmp.Or(c.Path, c.Repo)))
		}
	}
	return out
}

func checkGate(root string) []result {
	if root == "" {
		return []result{quiet(stSkip, "nix", "no git repo")}
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
		return []result{problem(stFail, "nix", "not on PATH but set: "+strings.Join(set, ", "), "nix is on PATH when a gate is set",
			manual("", "install nix", "every gated land will fail without it"))}
	}
	return []result{quiet(stOK, "nix", "on PATH")}
}

// checkSequences runs Land's rule reader and overlap rule for the Workspace repo and
// each repo = context, so a broken sequences rule fails here before a Land does.
// Silent when only the built-in ADR rule applies.
func checkSequences(root string) []result {
	if root == "" {
		return []result{quiet(stSkip, "sequences", "no git repo")}
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
			out = append(out, problem(stFail, name, err.Error(), "the sequences rule reads and validates",
				manual("", "fix the sequences rule", "every land of this repo is refused until then")))
			continue
		}
		if len(rules) == 1 {
			continue
		}
		overlaps, err := git.Repo(repos[n]).TreeOverlaps(rules)
		if err != nil {
			out = append(out, quiet(stSkip, name, err.Error()))
			continue
		}
		for _, o := range overlaps {
			out = append(out, problem(stFail, name, o.Error(), "no file matches two sequences templates",
				manual("", "narrow a template so no file matches two")))
		}
		if len(overlaps) == 0 {
			out = append(out, quiet(stOK, name, fmt.Sprintf("%d rules, no overlap", len(rules))))
		}
	}
	return out
}

func checkProfiles(root string) []result {
	if root == "" {
		return []result{quiet(stSkip, "agent profiles", "no git repo")}
	}
	cat := cfglayer.Profiles(root)
	const pre = "the agent profile can run its harness"
	fix := manual("", "fix the profile or its kind mapping")
	var out []result
	for _, n := range slices.Sorted(maps.Keys(cat.Winners)) {
		e := cat.Winners[n]
		name := "agent profile " + n
		if _, err := checkProfile(root, e); err != nil {
			out = append(out, problem(stWarn, name, err.Error(), pre, fix))
		} else {
			out = append(out, quiet(stOK, name, "from "+displayPath(root, e.Path)))
		}
	}
	local, shared := loop.KindProfiles(root)
	mapped := map[string]bool{}
	for _, m := range []map[string]string{local, shared} {
		for _, n := range m {
			if _, ok := cat.Winners[n]; !ok && n != "" {
				mapped[n] = true
			}
		}
	}
	for _, n := range slices.Sorted(maps.Keys(mapped)) {
		out = append(out, problem(stWarn, "agent profile "+n, "not declared (no file)", pre, fix))
	}
	for _, d := range cat.Unusable {
		if w, ok := cat.Winners[d.Name]; ok && d.Name != "" && w.Path == d.Path && w.Err != nil {
			continue
		}
		name := "agent profiles"
		if d.Name != "" {
			name = "agent profile " + d.Name
		}
		out = append(out, problem(stWarn, name, fmt.Sprintf("%s (from %s)", d.Err, displayPath(root, d.Path)), pre, fix))
	}
	if len(cat.Winners) == 0 && len(cat.Unusable) == 0 {
		paths := cfglayer.ProfilePaths(root)
		for i, p := range paths {
			paths[i] = displayPath(root, p)
		}
		out = append(out, quiet(stOK, "agent profiles", "none declared (read "+strings.Join(paths, ", ")+")"))
	}
	return out
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
	const authPre = "glab is authenticated for the Forge host"
	authFix := manual("gitlab", "run one interactive web `glab auth login`")
	// The fill does not need glab or the project list (FR 29), so it prints whatever they say;
	// it is safe to fix only when the glab user row read an account (FR 27).
	projs, err := doctorGitlabProjects()
	if err != nil {
		return append([]result{problem(stFail, "gitlab projects", err.Error(), "the Forge remote and declared repos resolve to GitLab projects",
			manual("", "fix the Forge remote or the declared repos"))}, credentialResult(""))
	}
	if _, err := lookPath("glab"); err != nil {
		return []result{credentialResult("")} // the tools row already reports a missing glab
	}
	proj := projs[0].Project
	if err := doctorForgeCheck(proj.Host); err != nil {
		return []result{problem(stFail, "glab user", "not authenticated for "+proj.Host+": "+err.Error(), authPre, authFix), credentialResult("")}
	}
	user, err := doctorForgeUser(proj)
	if err != nil {
		return []result{problem(stFail, "glab user", err.Error(), authPre, authFix), credentialResult("")}
	}
	out := []result{quiet(stOK, "glab user", user), credentialResult(user)}
	for _, e := range projs {
		name := "squash settings " + e.Project.ID
		if e.Context != "" {
			name += " (context " + e.Context + ")"
		}
		if err := doctorForgeSettings(e.Project); err != nil {
			detail, steps := err.Error(), []string{"fix the project's squash settings as the detail says"}
			var ref *forge.ProjectRefusal
			if errors.As(err, &ref) {
				detail = ref.Wrong()
				steps = []string{ref.Fix}
				if ref.Field == "squash_commit_template" {
					steps = []string{"put %{description} on its own line of the template"}
				}
			}
			out = append(out, problem(stFail, name, detail, "the project's squash settings let a land through", manual("", steps...)))
		} else {
			out = append(out, quiet(stOK, name, "ok"))
		}
	}
	return out
}

// credentialResult is the git credential line: git credential fill under the injected glab
// helper every Forge call carries (ADR 0300), run from outside the repo so a repo-local
// helper cannot answer. OK, FAIL, or skip for an ssh remote. Never touches bd's sql-server.
// user is the account the glab user row read, "" when it did not; a FAIL is a safe fix only
// for a known user with no token variable overriding glab's config (ADR 0247 §2).
func credentialResult(user string) result {
	const name = "git credential"
	const credPre = "glab's credential helper answers git for the Forge host"
	url, err := doctorForgeURL()
	if err != nil {
		return problem(stFail, name, err.Error(), credPre, manual("", "fix the Forge remote"))
	}
	switch err := doctorCredentialFill(url, os.TempDir(), git.GlabHelperEnv(url)); {
	case errors.Is(err, git.ErrCredentialNA):
		return quiet(stSkip, name, "ssh remote")
	case err != nil:
		host := url
		if p, perr := forge.ParseRemote(url); perr == nil {
			host = p.Host
		}
		f := manual("gitlab", "set glab's user, or re-run `glab auth login`")
		if user != "" && !glabTokenSet() {
			f.safe = []string{"glab", "config", "set", "user", user, "--host", host}
		}
		return problem(stFail, name, err.Error(), credPre, f)
	}
	return quiet(stOK, name, "credential available")
}

// glabTokenSet is whether a token variable is set that glab prefers over its config's user.
func glabTokenSet() bool {
	for _, v := range []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"} {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

// checkMDRenderer follows MUGICREW_MD_RENDERER like the HITL pane wording
// (default the built-in mugicrew doc-pane); a set renderer's first word is looked up.
func checkMDRenderer(string) []result {
	r := strings.TrimSpace(os.Getenv("MUGICREW_MD_RENDERER"))
	if r == "" {
		return []result{quiet(stOK, "md renderer", "built-in (mugicrew doc-pane)")}
	}
	cmd := strings.Fields(r)[0]
	if _, err := lookPath(cmd); err != nil {
		return []result{problem(stWarn, "md renderer", cmd+" not on PATH", "the renderer named by MUGICREW_MD_RENDERER is on PATH",
			manual("", "set MUGICREW_MD_RENDERER to another renderer", "until then Spec, Decision Packet and Initiative previews show inline only"))}
	}
	return []result{quiet(stOK, "md renderer", cmd+" on PATH")}
}

// checkBrowser follows $BROWSER like the Sanji prototype hand-over does; only
// the command's first word is looked up.
func checkBrowser(string) []result {
	const pre = "BROWSER names a browser on PATH"
	fix := manual("", "set BROWSER", "until then the prototype hand-over prints the target only")
	f := strings.Fields(os.Getenv("BROWSER"))
	if len(f) == 0 {
		return []result{problem(stWarn, "browser", "BROWSER unset", pre, fix)}
	}
	if _, err := lookPath(f[0]); err != nil {
		return []result{problem(stWarn, "browser", f[0]+" not on PATH", pre, fix)}
	}
	return []result{quiet(stOK, "browser", f[0]+" on PATH")}
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
		return []result{problem(stFail, "jira site", site, "[jira] site is a bare host such as acme.atlassian.net",
			manual("", "set `[jira] site` in "+file+" to a bare host such as acme.atlassian.net"))}
	}
	active, err := doctorACLISite()
	switch {
	case errors.Is(err, jira.ErrNotLoggedIn):
		return []result{problem(stFail, "acli site", "not logged in", aclipre(site), aclilogin)}
	case errors.Is(err, jira.ErrNotFound):
		return nil // the tools row already reports a missing acli
	case err != nil:
		return []result{problem(stFail, "acli site", err.Error(), aclipre(site), aclilogin)}
	case active != "" && active != site:
		return []result{problem(stFail, "acli site", fmt.Sprintf("acli is on %s, [jira] site is %s", active, site), aclipre(site),
			fix{safe: []string{"acli", "jira", "auth", "switch", "--site", site}, steps: []string{"run `acli jira auth login`"}, more: "docs/install.md#acli"})}
	}
	return []result{quiet(stOK, "acli site", site)}
}

// aclipre is the acli site rows' precondition.
func aclipre(site string) string { return "acli's active site is [jira] site (" + site + ")" }

// aclilogin is the fix for acli with no usable login.
var aclilogin = manual("jira", "run `acli jira auth login`")

// doctorACLISite reads acli's active site; a var so tests stub it.
var doctorACLISite = jira.ActiveSite
