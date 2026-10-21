package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// integrateGit, integrateReplace and unpause are Integrate's git and bd seams,
// vars so its exit mapping and pause lift are testable without a repo or a bd.
var (
	integrateGit     = git.Repo.Integrate
	integrateReplace = git.Repo.IntegrateReplace
	unpause          = bd.Unpause
	fetchMainline    = git.Repo.FetchMainline
)

// Integrate's exit codes, which the backstop and the CLI pass through.
const (
	ExitIntegrated = 0 // integrated, or nothing ahead
	ExitTransient  = 1
	ExitConflict   = 2 // rebase, held
	ExitLanded     = 3 // the container already landed
	ExitNoCutPoint = 4 // no cut ref and no fork point after a rewrite; non-transient
	ExitOffPin     = 5 // a replace refused: the container was rewritten off the bead's pinned Tip
)

// Integrate is mugicrew integrate for bead b, run in its effort tree (Spec
// D3, ADR 0102 §3, ADR 0104 §1/§2/§9): it squashes the effort onto the
// container route names (replace mode, a replace-CAS at its pinned Tip, for a
// conflict or chart-conflict) and returns the exit code, 0 integrated or nothing
// ahead, 2 a conflict (rebase, held), 3 the container already landed, 4 no cut
// point survived a rewrite, 5 a replace refused off its pinned tip, 1 transient.
// A 0 lifts paused:integrate-conflict. It never closes b.
func Integrate(b bd.Bead, by eventlog.IntegrateBy, w io.Writer) int {
	return IntegrateIn(Child{}, b, by, w)
}

// IntegrateIn is Integrate for a nested leaf: the squash runs in c's child tree and
// lands on the child's container; the land lock and the events stay the
// Workspace's (the zero Child is Integrate itself).
func IntegrateIn(c Child, b bd.Bead, by eventlog.IntegrateBy, w io.Writer) int {
	d, err := route.Route(b)
	if err == nil && d.Container == "" {
		err = fmt.Errorf("integrate: %s bead %s integrates onto no container", b.Kind, b.ID)
	}
	if err == nil && d.Mode == route.Replace && d.Tip == "" {
		// A Conflict filed before per-effort branches carries no Tip: pin.
		err = fmt.Errorf("integrate: %s bead %s carries no Tip: pin to replace %s at; nothing was integrated", b.Kind, b.ID, d.Container)
		fmt.Fprintf(w, "%v.\nA bead filed before per-effort branches has none: add a line %q to its body, the container tip it rewrites "+
			"(git rev-parse %s, if the container hasn't moved since), then re-run mugicrew integrate.\n", err, git.TipPin("<sha>"), d.Container)
		return ExitTransient
	}
	if err != nil {
		fmt.Fprintln(w, err)
		return ExitTransient
	}
	var sha string
	var retries int
	if d.Mode == route.Replace {
		// Under gitlab a rewrite checks Forge main first, so its landed check sees a
		// container already merged there (ADR 0263); a failed fetch publishes nothing.
		if name, nerr := git.LandStrategyName(); nerr != nil {
			fmt.Fprintln(w, nerr)
			return ExitTransient
		} else if name == git.LandStrategyGitlab {
			if _, ferr := fetchMainline(c.Repo); ferr != nil {
				fmt.Fprintf(w, "integrate: fetching Forge main failed, nothing published: %v\n", ferr)
				return ExitTransient
			}
		}
		sha, retries, err = integrateReplace(c.Repo, d.Container, d.Tip)
	} else {
		sha, retries, err = integrateGit(c.Repo, d.Container, b.ID, git.IntegrateSubject(string(b.Kind), b.ID, b.Title, subjectContext(b)), git.TrailerFor(string(b.Kind), b.ID))
	}
	eventlog.RecordIntegrate(b, d.Container, sha, retries, by, err, c.attrs()...)
	var conflict *git.IntegrateConflict
	switch {
	case errors.As(err, &conflict) && conflict.Reason == git.ConflictRebase:
		fmt.Fprintf(w, "%v; the rebase is stopped in this tree.\n"+
			"Resolve the conflicts, git add them, git rebase --continue, then re-run mugicrew integrate.\n"+
			"If you can't resolve them: git rebase --abort, note why on bead %s, leave it open and exit.\n", err, b.ID)
		return ExitConflict
	case conflict != nil && conflict.Reason == git.ConflictLanded:
		fmt.Fprintln(w, err)
		return ExitLanded
	case conflict != nil:
		fmt.Fprintln(w, err)
		return ExitConflict
	case errors.Is(err, git.ErrNoCutPoint):
		fmt.Fprintln(w, err)
		return ExitNoCutPoint
	case errors.Is(err, git.ErrOffPin):
		fmt.Fprintln(w, err)
		return ExitOffPin
	case err != nil:
		fmt.Fprintln(w, err)
		return ExitTransient
	}
	lifted := false
	if bd.PausedBy(b, bd.PausedIntegrateConflict) {
		if err := unpause(b.ID, bd.PausedIntegrateConflict); err != nil {
			fmt.Fprintf(w, "integrated, but lifting paused:%s failed: %v\n", bd.PausedIntegrateConflict, err)
		} else {
			lifted = true
			slog.Info("effort.resumed", append(eventlog.BeadAttrs(b), "cause", bd.PausedIntegrateConflict)...)
		}
	}
	switch {
	case sha == "" && lifted && by == eventlog.ByHuman:
		// the trailer was already on the container: lifting the pause was the last step
		fmt.Fprintf(w, "nothing to integrate onto %s; bead still open: bd close %s\n", d.Container, b.ID)
	case sha == "":
		fmt.Fprintf(w, "nothing to integrate onto %s\n", d.Container)
	case by == eventlog.ByHuman:
		fmt.Fprintf(w, "integrated; bead still open: bd close %s\n", b.ID)
	default:
		fmt.Fprintf(w, "integrated %s onto %s\n", sha, d.Container)
	}
	return ExitIntegrated
}

// subjectContext is the scope of b's integrate subject: its own context: label,
// else its container's. A failed lookup is no scope: a subject cosmetic never
// blocks an integrate.
func subjectContext(b bd.Bead) string {
	if c := bd.ContextOf(b); c != "" || b.Parent == "" {
		return c
	}
	parent, err := showBead(b.Parent)
	if err != nil {
		return ""
	}
	return bd.ContextOf(parent)
}

// Child is the nested repo an integrate acts in: the zero value is the cwd's own
// repo. Context is the repo = context naming it.
type Child struct {
	Repo    git.Repo // the effort's tree, <host>/<Dir>
	Main    git.Repo // the child's own checkout <root>/<Dir>, where branches outlive the effort tree; from a host it is the linked worktree, equal to Repo
	Context string
	Dir     string
	Effort  string // the bead id on the child's effort/<id>, set by ProbeChildren
}

// attrs are the context=/repo=/into= attrs a nested integrate's events carry; none
// for the zero Child, so unlabelled events stay byte-identical.
func (c Child) attrs() []any {
	if c.Repo == "" {
		return nil
	}
	return []any{"context", c.Context, "repo", c.Dir, "into", c.Repo.Mainline()}
}

// ProbeChildren is mugicrew integrate's nested lookup (Spec D6): the declared repo =
// dirs present under cwd whose tree is on, or mid-rebase on, an effort/<id>.
func ProbeChildren(cwd string) []Child {
	if len(declaredRepos(cwd)) == 0 { // no repo = declared: no git call
		return nil
	}
	root, err := git.RepoRoot()
	if err != nil { // no root, no child main to point at: never fall back to the host's own tree
		return nil
	}
	var out []Child
	for name, dir := range cfglayer.Repos(root) {
		tree := git.Repo(filepath.Join(cwd, dir))
		if !git.IsRepoRoot(string(tree)) { // a plain dir would make git -C walk up to the host
			continue
		}
		if id, err := tree.CurrentEffort(); err == nil {
			out = append(out, Child{Repo: tree, Main: git.Repo(filepath.Join(root, dir)), Context: name, Dir: dir, Effort: id})
		}
	}
	slices.SortFunc(out, func(a, b Child) int { return strings.Compare(a.Dir, b.Dir) })
	return out
}

// InsideChild reports whether cwd is a declared child's tree, <host>/<dir> for some
// host above it with that dir declared there: integrate refuses to run from one, since
// the land lock, events and config would re-home to the child (Spec FR 28). It reads
// config files only, never git: only a dir with its own .git can host children, so
// the walk's ancestors above the Workspace are one stat each, not a config read.
func InsideChild(cwd string) bool {
	for host := cwd; ; host = filepath.Dir(host) {
		if _, err := os.Stat(filepath.Join(host, ".git")); err != nil {
			if host == filepath.Dir(host) {
				return false
			}
			continue
		}
		for _, dir := range cfglayer.Repos(host) {
			if in := filepath.Join(host, dir); cwd == in || strings.HasPrefix(cwd, in+string(filepath.Separator)) {
				return true
			}
		}
		if host == filepath.Dir(host) {
			return false
		}
	}
}
