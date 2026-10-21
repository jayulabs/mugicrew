// Package cfglayer reads one .mugicrew config layer: the leaf shared by loop's
// settings and the contexts reader (ADR 0157: contexts can't live in loop).
package cfglayer

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/git"
)

// State is what reading one layer found.
type State int

const (
	Missing State = iota
	Set
	Unusable
)

// SharedPath is root's checked-in layer.
func SharedPath(root string) string { return filepath.Join(root, ".mugicrew", "config.toml") }

// LocalPath is the machine-local override, <git common dir of root>/mugicrew/
// config.toml; "" (a missing layer) when the common dir can't be resolved.
func LocalPath(root string) string {
	common := gitCommonDir(root)
	if common == "" {
		return ""
	}
	return filepath.Join(common, "mugicrew", "config.toml")
}

// gitCommonDir is root's git common dir, "" when unresolvable.
func gitCommonDir(root string) string {
	common, ok := commonDir(root)
	if !ok {
		var err error
		if common, err = git.CommonDirOf(root); err != nil {
			return ""
		}
	}
	return common
}

// commonDir is the git common dir of the repo containing root, read off the
// filesystem so a config read costs no git call (ADR 0157): it walks up to the
// nearest .git, a dir (the main tree) or a gitdir: file (a linked worktree, whose
// commondir file names the main one). !ok when it finds neither (the caller asks git).
func commonDir(root string) (string, bool) {
	dir, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	for ; ; dir = filepath.Dir(dir) {
		dotgit := filepath.Join(dir, ".git")
		fi, err := os.Stat(dotgit)
		if err == nil && fi.IsDir() {
			return dotgit, true
		}
		if err == nil {
			b, err := os.ReadFile(dotgit)
			gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
			if err != nil || !ok {
				return "", false
			}
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(dir, gitdir)
			}
			if rel, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
				if rel := strings.TrimSpace(string(rel)); filepath.IsAbs(rel) {
					gitdir = rel
				} else {
					gitdir = filepath.Join(gitdir, rel)
				}
			}
			return gitdir, true
		}
		if filepath.Dir(dir) == dir {
			return "", false
		}
	}
}

// Decode decodes one config file into cfg: Set when it decoded, Missing when there
// is no file, Unusable (warned) when it is malformed or mistyped.
func Decode(path string, cfg any) State {
	if path == "" {
		return Missing
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Missing
		}
		slog.Warn("config.unusable", "path", path, "err", err.Error())
		return Unusable
	}
	return Set
}

// Context is one [context.<name>] entry: a subtree (Path) or a nested child repo
// (Repo), never both; Gate is the context's own land-gate flake attribute.
type Context struct {
	Path string
	Repo string
	Gate string
}

type contextLayer struct {
	Context map[string]Context
}

// Contexts is the declared [context.*] table; nil means none. Both layers are
// re-read every call, unknown keys are ignored and the local layer wins as a whole
// table; a missing or unusable layer reads as none and falls through. An entry
// carrying both repo and path is warned and dropped: undeclared (ADR 0166).
func Contexts(root string) map[string]Context {
	for _, path := range []string{LocalPath(root), SharedPath(root)} {
		var l contextLayer
		if Decode(path, &l) != Set || len(l.Context) == 0 {
			continue
		}
		for name, c := range l.Context {
			if c.Repo != "" && c.Path != "" {
				slog.Warn("config.unusable", "path", path, "err", "context."+name+": repo and path are exclusive")
				delete(l.Context, name)
			}
		}
		return l.Context
	}
	return nil
}

// Repos maps each repo = context name to its dir; nil when none is declared.
func Repos(root string) map[string]string {
	var out map[string]string
	for name, c := range Contexts(root) {
		if c.Repo != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[name] = c.Repo
		}
	}
	return out
}

type gateLayer struct {
	Gate string
}

// Gate is the top-level gate = "<attr>", the flake attribute a Workspace-root land
// builds; "" (no key) lands ungated. The local layer wins when it sets one.
func Gate(root string) string {
	for _, path := range []string{LocalPath(root), SharedPath(root)} {
		var l gateLayer
		if Decode(path, &l) == Set && l.Gate != "" {
			return l.Gate
		}
	}
	return ""
}

// ContextGate is context name's own gate = "<attr>", "" when it sets none: the top
// level never leaks into a context.
func ContextGate(root, name string) string { return Contexts(root)[name].Gate }

type sequenceLayer struct {
	Sequences any
	Context   map[string]struct {
		Path      string
		Repo      string
		Sequences any
	}
}

// SequenceRules is the numbered-sequence rule set for repo r (the empty Repo is the
// cwd's, i.e. the Workspace repo): the built-in ADR rule, then the shared layer's
// top-level sequences = [...] for the Workspace repo, or [context.<name>] sequences
// for the child repo = <name> that r is. The local layer is never read, and the file
// is read now, not from a landing base. Unlike Decode, a missing file is built-in
// only but a malformed one is an error, as is any unusable rule, naming the rule and
// the file: a Land that can't tell what to enforce must refuse, not skip (ADR 0214).
func SequenceRules(root string, r git.Repo) ([]git.Rule, error) {
	rules := []git.Rule{git.ADRRule}
	path := SharedPath(root)
	var l sequenceLayer
	if _, err := toml.DecodeFile(path, &l); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return rules, nil
		}
		return nil, fmt.Errorf("sequences: %s: %w", path, err)
	}
	for name, c := range l.Context {
		if c.Path != "" && c.Sequences != nil {
			return nil, fmt.Errorf("sequences: context.%s in %s: a path context has no repo of its own, declare sequences at the top level", name, path)
		}
	}
	decl := l.Sequences
	if r != "" {
		if own, _ := sameRepo(root, string(r)); !own {
			decl = nil
			for _, c := range l.Context {
				if same, _ := sameRepo(filepath.Join(root, c.Repo), string(r)); c.Repo != "" && same {
					decl = c.Sequences
					break
				}
			}
		}
	}
	if decl == nil {
		return rules, nil
	}
	list, ok := decl.([]any)
	if !ok {
		return nil, fmt.Errorf("sequences: %s: sequences must be an array of templates", path)
	}
	for _, e := range list {
		tmpl, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("sequences: %s: rule %v is not a template string", path, e)
		}
		rule, err := git.CompileRule(tmpl)
		if err != nil {
			return nil, fmt.Errorf("sequences: %s: %w", path, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// sameRepo is whether dirs a and b belong to one git repo (a linked worktree shares its main's common dir).
func sameRepo(a, b string) (bool, error) {
	ca, err := git.CommonDirOf(a)
	if err != nil {
		return false, err
	}
	cb, err := git.CommonDirOf(b)
	return err == nil && ca == cb, err
}

type jiraLayer struct {
	Jira *struct {
		Site         string
		PollInterval string `toml:"poll_interval"`
	}
}

// DefaultJiraPollInterval is how often an open latch is re-checked; MinJiraPollInterval floors it.
const (
	DefaultJiraPollInterval = 15 * time.Minute
	MinJiraPollInterval     = time.Minute
)

// JiraPollInterval is [jira] poll_interval, a Go duration: how long a latch waits
// between real reads. The first layer with a [jira] table wins as a whole, local
// first. Unset yields the default; an unparseable or sub-minimum value warns
// config.unusable and yields the default too.
func JiraPollInterval(root string) time.Duration {
	for _, p := range []string{LocalPath(root), SharedPath(root)} {
		var l jiraLayer
		if Decode(p, &l) != Set || l.Jira == nil {
			continue
		}
		if l.Jira.PollInterval == "" {
			return DefaultJiraPollInterval
		}
		d, err := time.ParseDuration(l.Jira.PollInterval)
		if err == nil && d < MinJiraPollInterval {
			err = fmt.Errorf("under the %v minimum", MinJiraPollInterval)
		}
		if err != nil {
			slog.Warn("config.unusable", "path", p, "key", "jira.poll_interval", "err", err.Error())
			return DefaultJiraPollInterval
		}
		return d
	}
	return DefaultJiraPollInterval
}

type gitlabLayer struct {
	Gitlab *struct {
		PollInterval        string `toml:"poll_interval"`
		LandingPollInterval string `toml:"landing_poll_interval"`
	}
}

// GitlabPollInterval is [gitlab] poll_interval: JiraPollInterval's rule and bounds
// for a latch watching a merge request.
func GitlabPollInterval(root string) time.Duration {
	for _, p := range []string{LocalPath(root), SharedPath(root)} {
		var l gitlabLayer
		if Decode(p, &l) != Set || l.Gitlab == nil {
			continue
		}
		if l.Gitlab.PollInterval == "" {
			return DefaultJiraPollInterval
		}
		d, err := time.ParseDuration(l.Gitlab.PollInterval)
		if err == nil && d < MinJiraPollInterval {
			err = fmt.Errorf("under the %v minimum", MinJiraPollInterval)
		}
		if err != nil {
			slog.Warn("config.unusable", "path", p, "key", "gitlab.poll_interval", "err", err.Error())
			return DefaultJiraPollInterval
		}
		return d
	}
	return DefaultJiraPollInterval
}

// DefaultLandingPollInterval is how often a pending Landing is re-checked;
// MinLandingPollInterval floors it at the loop tick.
const (
	DefaultLandingPollInterval = time.Minute
	MinLandingPollInterval     = 15 * time.Second
)

// LandingPollInterval is [gitlab] landing_poll_interval, a Go duration: how long a
// Landing waits after its last effort ended before the next. GitlabPollInterval's
// layered lookup, with its own default and floor; an unparseable or sub-minimum
// value warns config.unusable and yields the default.
func LandingPollInterval(root string) time.Duration {
	for _, p := range []string{LocalPath(root), SharedPath(root)} {
		var l gitlabLayer
		if Decode(p, &l) != Set || l.Gitlab == nil {
			continue
		}
		if l.Gitlab.LandingPollInterval == "" {
			return DefaultLandingPollInterval
		}
		d, err := time.ParseDuration(l.Gitlab.LandingPollInterval)
		if err == nil && d < MinLandingPollInterval {
			err = fmt.Errorf("under the %v minimum", MinLandingPollInterval)
		}
		if err != nil {
			slog.Warn("config.unusable", "path", p, "key", "gitlab.landing_poll_interval", "err", err.Error())
			return DefaultLandingPollInterval
		}
		return d
	}
	return DefaultLandingPollInterval
}

// JiraSite is [jira] site, the Jira Cloud host intake builds browse links from;
// "" when unset, with the config file that supplied it (the shared one when unset).
// The first layer with a [jira] table wins as a whole, local first.
func JiraSite(root string) (site, path string) {
	for _, p := range []string{LocalPath(root), SharedPath(root)} {
		var l jiraLayer
		if Decode(p, &l) == Set && l.Jira != nil {
			return l.Jira.Site, p
		}
	}
	return "", SharedPath(root)
}

// Initiatives is the resolved [initiatives] setting (ADR 0224).
type Initiatives struct {
	Required    bool   // [initiatives] required: Require Initiatives
	ExternalRef bool   // [initiatives] external_ref: Enforce external ID; never true unless Required
	Unusable    bool   // the table was malformed, mistyped, or external_ref without required (warned config.unusable)
	Path        string // the shared config file read
}

type initiativesLayer struct {
	Initiatives *struct {
		Required    bool
		ExternalRef bool `toml:"external_ref"`
	}
}

// InitiativesSetting reads [initiatives] from the shared layer only, so no machine
// can quietly opt out of a shared rule. It is re-read on every call. A missing file
// or table is both off; a decode error or external_ref without required warns
// config.unusable and reads as both off.
func InitiativesSetting(root string) Initiatives {
	out := Initiatives{Path: SharedPath(root)}
	var l initiativesLayer
	switch Decode(out.Path, &l) {
	case Unusable:
		out.Unusable = true
	case Set:
		if t := l.Initiatives; t != nil {
			if t.ExternalRef && !t.Required {
				slog.Warn("config.unusable", "path", out.Path, "err", "initiatives: external_ref needs required")
				out.Unusable = true
			} else {
				out.Required, out.ExternalRef = t.Required, t.ExternalRef
			}
		}
	}
	return out
}

// WorkspaceSetUp reads whether the Workspace was set up by `mugicrew init
// workspace`: land_strategy and [initiatives] required both present in the
// shared layer (no marker key). The State is the shared layer's: Missing and Unusable
// are never set up. A Workspace with no shared config stays valid on defaults.
func WorkspaceSetUp(root string) (bool, State) {
	var l OwnedKeys
	st := Decode(SharedPath(root), &l)
	if st != Set {
		return false, st
	}
	return l.LandStrategy != nil && l.Initiatives.Required != nil, st
}

// OwnedKeys is the shared-layer shape `mugicrew init workspace` owns: each
// field is nil when the key is absent. Context is the declared [context.*] tables.
type OwnedKeys struct {
	LandStrategy *string `toml:"land_strategy"`
	Initiatives  struct {
		Required    *bool `toml:"required"`
		ExternalRef *bool `toml:"external_ref"`
	}
	Jira struct {
		Site *string `toml:"site"`
	}
	Context map[string]Context
}

// Defect is one unusable thing Profiles found: a bad [profiles.<name>] table
// (Name set), or a whole file that contributes nothing (Name ""): a dropped OE
// layer, or a config.toml whose profiles is not a table.
type Defect struct {
	Path, Name string
	Err        error
}

// Catalog is the merged Agent Profile set: each declared name's winning table,
// and every defect found, outranked ones included.
type Catalog struct {
	Winners  map[string]agentprofile.Entry
	Unusable []Defect
}

type profileFile struct {
	path   string
	strict bool
}

// profileFiles is the four profile files in precedence order; a path is ""
// when its layer can't be resolved.
func profileFiles(root string) []profileFile {
	oeShared, oeLocal := agentprofile.OEPaths(root, gitCommonDir(root))
	return []profileFile{{LocalPath(root), false}, {oeLocal, true}, {SharedPath(root), false}, {oeShared, true}}
}

// ProfilePaths is the resolvable paths of the four files Profiles reads, in
// precedence order.
func ProfilePaths(root string) []string {
	var out []string
	for _, f := range profileFiles(root) {
		if f.path != "" {
			out = append(out, f.path)
		}
	}
	return out
}

// Profiles reads the four profile files, never cached, and merges them first
// wins: mugicrew local config.toml, OE local, mugicrew shared config.toml, OE
// shared (ADR 0304). Each defect is logged once per read: config.unusable
// {path, key, reason} for a bad table or a non-table profiles, config.unusable
// {path, err} for a malformed config.toml (which contributes no profiles, and
// is not in Unusable), agent-profiles.unusable {file, reason} for a dropped OE
// layer. A missing file is silent.
func Profiles(root string) Catalog {
	var layers []agentprofile.Layer
	var out Catalog
	for _, f := range profileFiles(root) {
		if f.path == "" {
			continue
		}
		read := agentprofile.ReadTables
		if f.strict {
			read = agentprofile.ReadStrict
		}
		l, err := read(f.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case f.strict && err != nil:
			slog.Warn("agent-profiles.unusable", "file", f.path, "reason", err.Error())
			out.Unusable = append(out.Unusable, Defect{Path: f.path, Err: err})
			continue
		case errors.Is(err, agentprofile.ErrProfilesNotTable):
			slog.Warn("config.unusable", "path", f.path, "key", "profiles", "reason", err.Error())
			out.Unusable = append(out.Unusable, Defect{Path: f.path, Err: err})
			continue
		case err != nil:
			slog.Warn("config.unusable", "path", f.path, "err", err.Error())
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(l.Bad)) {
			slog.Warn("config.unusable", "path", f.path, "key", "profiles."+name, "reason", l.Bad[name].Error())
			out.Unusable = append(out.Unusable, Defect{Path: f.path, Name: name, Err: l.Bad[name]})
		}
		layers = append(layers, l)
	}
	out.Winners = agentprofile.Merge(layers...)
	return out
}
