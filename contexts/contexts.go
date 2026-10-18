// Package contexts reads a Workspace's declared Contexts ([context.<name>] path =
// "<subtree>" or repo = "<child repo dir>" in .mugicrew/config.toml) and words the focus line for a labelled
// container. A leaf, so loop and the launchers can both use it (ADR 0157).
package contexts

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Contexts maps each declared context name to its subtree, or to its child repo's
// dir for a repo = entry; nil means none, today's behaviour. The read (both layers,
// local winning as a whole table) lives in cfglayer.
func Contexts(root string) map[string]string {
	table := cfglayer.Contexts(root)
	if len(table) == 0 {
		return nil
	}
	out := make(map[string]string, len(table))
	for name, c := range table {
		if c.Repo != "" && !git.IsRepoRoot(filepath.Join(root, c.Repo)) {
			continue // unusable: held like an undeclared context
		}
		out[name] = cmp.Or(c.Repo, c.Path)
	}
	return out
}

// repos is the repo = table; a seam so a test can see who reads config.
var repos = cfglayer.Repos

// RepoOf is the child repo a bead's context: label names, "" unless it carries one
// that names a repo = entry. It reads no config for an unlabelled bead.
func RepoOf(b bd.Bead) git.Repo {
	r, _ := RepoDirOf(b)
	return r
}

// NestedAttrs is the context=/repo=/into= a nested land's or Rollback's events carry (Spec FR 30).
func NestedAttrs(b bd.Bead, r git.Repo, dir string) []any {
	return []any{"context", bd.ContextOf(b), "repo", dir, "into", r.Mainline()}
}

// RepoDirOf is RepoOf plus the entry's dir as declared (relative to the Workspace
// root), for the nested cut that materializes at <host>/<dir>.
func RepoDirOf(b bd.Bead) (git.Repo, string) {
	name := bd.ContextOf(b)
	if name == "" || len(repos(".")) == 0 { // config first: a subtree-only Workspace pays no git call
		return "", ""
	}
	root, err := git.RepoRoot()
	if err != nil {
		return "", ""
	}
	dir, ok := repos(root)[name]
	if !ok || !git.IsRepoRoot(filepath.Join(root, dir)) { // a plain dir would address the Workspace repo
		return "", ""
	}
	return git.Repo(filepath.Join(root, dir)), dir
}

// RepoOfID is RepoOf for a container known only by id: it gates on any repo =
// being declared before the show, so a no-context Workspace makes no extra read.
func RepoOfID(id string, show show) git.Repo {
	if id == "" || len(repos(".")) == 0 {
		return ""
	}
	b, err := show(id)
	if err != nil {
		return ""
	}
	return RepoOf(b)
}

// ShowBead reads a container for Owner; a seam so a test scripts the graph.
var ShowBead = bd.Show

// Owner is the bead whose context: label decides b's repo. Leaves don't carry
// context: (ADR 0166), so for an effort routed onto container it is the container's
// bead. b itself when no repo = is declared (a no-context Workspace reads no bead),
// when container names no bead, or when the read fails.
func Owner(b bd.Bead, container string) bd.Bead {
	if container == "" || len(repos(".")) == 0 {
		return b
	}
	_, id := git.ParseBranchName(container)
	if id == "" {
		return b
	}
	if c, err := ShowBead(id); err == nil {
		return c
	}
	return b
}

// WorktreeOn is git.WorktreeOn on b's repo: the child's for a repo = bead, with the
// holder's Path lifted from the child's tree <host>/<dir> to the host tree, which is
// what the temp-shape, session and in-flight checks key on. An unlabelled bead makes
// the package call exactly.
func WorktreeOn(b bd.Bead, branch string) (git.Holder, bool, error) {
	r, dir := RepoDirOf(b)
	if r == "" {
		return git.WorktreeOn(branch)
	}
	h, ok, err := r.WorktreeOn(branch)
	h.Path = hostOf(h.Path, dir)
	return h, ok, err
}

// Busy is git.Busy on b's repo, the holder's Path lifted to the host as WorktreeOn does.
func Busy(b bd.Bead, err error, branch string) (git.Holder, bool) {
	r, dir := RepoDirOf(b)
	h, ok := r.Busy(err, branch)
	h.Path = hostOf(h.Path, dir)
	return h, ok
}

// ChildTree is the child tree materialized in b's host tree, "" for a bead with no
// repo = context.
func ChildTree(b bd.Bead, host string) string {
	if _, dir := RepoDirOf(b); dir != "" {
		return filepath.Join(host, dir)
	}
	return ""
}

// hostOf is the host tree of a child tree <host>/<dir>; a path not shaped so (the
// child's main checkout <root>/<dir> trims to <root>; any other path stays itself).
func hostOf(p, dir string) string {
	if dir == "" {
		return p
	}
	return strings.TrimSuffix(p, string(filepath.Separator)+filepath.Clean(dir))
}

// addStamp writes the repo:<dir> label; a seam so a test sees the write.
var addStamp = bd.StampRepo

// StampCut stamps repo:<dir> on target, the container about to be cut from src, when
// branch does not yet exist in r and src carries no stamp to have copied (a re-cut, or
// a successor that copied one, writes nothing). It runs BEFORE the cut, so a crash or
// a failed write leaves no branch and the re-run stamps again; stamping after left a
// created branch whose re-run never stamped. The dir is the live repo = of src's
// context (ADR 0167). A target already stamped writes nothing, nor does a subtree or
// unlabelled src, whose dir is "".
func StampCut(src bd.Bead, target string, r git.Repo, branch string) error {
	if bd.RepoStampOf(src) != "" || r.HasBranch(branch) {
		return nil
	}
	_, dir := RepoDirOf(src)
	if dir == "" {
		return nil
	}
	// a stamp written before a failed cut stays on target; a second would hold it (bd adds, never replaces)
	if t, err := ShowBead(target); err == nil && bd.RepoStampOf(t) != "" {
		return nil
	}
	return addStamp(target, dir)
}

func init() { git.DeclaredRepos = Repos }

// Repos is the live repo = table, context name to dir; nil when the Workspace root
// can't be resolved.
func Repos() map[string]string {
	if len(repos(".")) == 0 { // config first: no repo = declared, no git call
		return nil
	}
	root, err := git.RepoRoot()
	if err != nil {
		return nil
	}
	live := maps.Clone(repos(root))
	maps.DeleteFunc(live, func(_, dir string) bool { return !git.IsRepoRoot(filepath.Join(root, dir)) })
	return live
}

// Drift is b's repo: stamp and the live repo = dir its context now names ("" for
// none), drifted when they differ. A bead with no context or no stamp never drifts.
func Drift(b bd.Bead, live map[string]string) (stamp, dir string, drifted bool) {
	stamp = bd.RepoStampOf(b)
	if stamp == "" || bd.ContextOf(b) == "" {
		return stamp, "", false
	}
	dir = live[bd.ContextOf(b)]
	return stamp, dir, strings.TrimSuffix(stamp, "/") != strings.TrimSuffix(dir, "/")
}

// CutKind reports whether b is a container cut on its own <kind>/<id> branch: a
// Checkpoint or a Chart.
func CutKind(b bd.Bead) bool {
	return b.Kind == contract.KindCheckpoint || b.Kind == contract.KindChart
}

// PauseNote is bd.PauseNote, except a repo-stamp drift on a paused:context bead names
// both dirs and the two fixes (ADR 0167).
func PauseNote(b bd.Bead, cause string) string {
	if cause == bd.PausedContext && bd.RepoStampOf(b) != "" { // ADR 0157 zero-cost guard
		if stamp, dir, drifted := Drift(b, Repos()); drifted {
			if dir == "" { // switched to path =, or undeclared
				return fmt.Sprintf("context (cut in repo %q but it now declares no repo =; revert it, or replace %s's repo:%s label)", stamp, b.ID, stamp)
			}
			return fmt.Sprintf("context (cut in repo %q but repo = now %q — revert repo =, or replace %s's repo:%s label with repo:%s)", stamp, dir, b.ID, stamp, dir)
		}
	}
	if cause == bd.PausedContext && CutKind(b) && bd.RepoStampOf(b) == "" && bd.ContextOf(b) != "" { // label-only gate
		if dir := Repos()[bd.ContextOf(b)]; dir != "" {
			return fmt.Sprintf("context (cut in the Workspace repo before it was labelled context:%s — remove the label, or cut %s in %s and add repo:%s)",
				bd.ContextOf(b), git.BranchName(string(b.Kind), b.ID), dir, dir)
		}
	}
	return bd.PauseNote(b, cause)
}

// RepoLine is the addendum for a repo context: the dir is its own git repo with the
// branch checked out, run git there, commit ADRs and GLOSSARY there, and its trunk.
// "" for any other container.
func RepoLine(container bd.Bead) string {
	r, dir := RepoDirOf(container)
	if r == "" {
		return ""
	}
	dir = strings.TrimSuffix(dir, "/")
	return fmt.Sprintf("Repo context: %s/ is its own git repo, where your branch is checked out. Run git there with git -C %s, and commit ADR and GLOSSARY edits in it, not in the Workspace repo. Its trunk is %s.",
		dir, dir, r.Mainline())
}

// FocusLine is the line appended to a crewmate prompt under container: "" when it
// carries no declared context, else the context's name and path and what to read first.
func FocusLine(contexts map[string]string, container bd.Bead) string {
	name := bd.ContextOf(container)
	path, ok := contexts[name]
	if name == "" || !ok {
		return ""
	}
	path = strings.TrimSuffix(path, "/")
	return fmt.Sprintf("Context focus: this work targets the %q context (%s/). Read %s/GLOSSARY.md and %s/docs/adr/ first. Also read %s/.mugicrew/CREW.md if present, after the Workspace Crew brief and in addition to it. Your cwd stays at the repo root.",
		name, path, path, path, path)
}

// FullLine is FocusLine plus, for a repo context, its RepoLine addendum.
func FullLine(cs map[string]string, container bd.Bead) string {
	line := FocusLine(cs, container)
	if line == "" {
		return ""
	}
	if repo := RepoLine(container); repo != "" {
		line += " " + repo
	}
	return line
}

// OutOfScope keeps the paths outside subtree, minus the fixed exempt set (ADR 0159):
// GLOSSARY*.md, anything under a docs/adr/ segment, .mugicrew/config.toml and the Workspace root
// .mugicrew/CREW.md.
// Lockfiles and flake files stay.
func OutOfScope(paths []string, subtree string) []string {
	subtree = strings.TrimSuffix(subtree, "/")
	var out []string
	for _, p := range paths {
		switch base := path.Base(p); {
		case p == subtree || strings.HasPrefix(p, subtree+"/"):
		case base == "GLOSSARY.md" || base == "GLOSSARY-MAP.md" || p == ".mugicrew/config.toml" || p == ".mugicrew/CREW.md":
		case strings.HasPrefix(p, "docs/adr/") || strings.Contains(p, "/docs/adr/"):
		default:
			out = append(out, p)
		}
	}
	return out
}

// show reads one bead; the callers pass their bd.Show seam.
type show = func(string) (bd.Bead, error)

// Focus appends container's focus line to a rendered prompt, or returns it as is
// when the Workspace declares no contexts or the container carries none.
func Focus(prompt string, container bd.Bead) string {
	return withFocus(prompt, Contexts("."), container)
}

// FocusByID is Focus for a container not in hand: it gates on the declared contexts
// first, so a no-context Workspace never calls show (ADR 0157).
func FocusByID(prompt, containerID string, show show) string {
	cs := Contexts(".")
	if len(cs) == 0 || containerID == "" {
		return prompt
	}
	return focusByID(prompt, cs, containerID, show)
}

// FocusUnder is FocusByID for a leaf: the container is the parent of bead id.
func FocusUnder(prompt, id string, show show) string {
	cs := Contexts(".")
	if len(cs) == 0 {
		return prompt
	}
	b, err := show(id)
	if err != nil {
		return prompt
	}
	return focusByID(prompt, cs, b.Parent, show)
}

func focusByID(prompt string, cs map[string]string, containerID string, show show) string {
	if containerID == "" {
		return prompt
	}
	c, err := show(containerID)
	if err != nil {
		return prompt // focus is advisory; a failed read launches unfocused
	}
	return withFocus(prompt, cs, c)
}

func withFocus(prompt string, cs map[string]string, container bd.Bead) string {
	if line := FullLine(cs, container); line != "" {
		return prompt + "\n\n" + line
	}
	return prompt
}

// LandedBase reads trailer on b's mainline: the child's for a repo context, else ws
// (the cwd's, through the caller's own seam). A landed nested bead is thus seen as
// landed by the loop, Nami, Jinbe and the TUI alike (Spec FR 35).
func LandedBase(b bd.Bead, trailer string, ws func(string) (string, error)) (string, error) {
	if r := RepoOf(b); r != "" {
		return r.LandedBase(trailer)
	}
	return ws(trailer)
}

// Landed is LandedBase where only git.ErrNotLanded is a miss: landed reports the
// trailer on mainline, a miss is (_, false, nil), and any other error (a foreign
// land, ADR 0187 §4, or a failed read) is returned, never "not landed".
func Landed(b bd.Bead, trailer string, ws func(string) (string, error)) (sha string, landed bool, err error) {
	sha, err = LandedBase(b, trailer, ws)
	if errors.Is(err, git.ErrNotLanded) {
		return "", false, nil
	}
	return sha, err == nil, err
}

// Head is ref's tip in b's repo: the child's for a repo context, else ws.
func Head(b bd.Bead, ref string, ws func(string) (string, error)) (string, error) {
	if r := RepoOf(b); r != "" {
		return r.Head(ref)
	}
	return ws(ref)
}

// AncestorIn is r's IsAncestor, or ws when r is empty (a Workspace bead).
func AncestorIn(r git.Repo, ws func(a, b string) bool) func(a, b string) bool {
	if r == "" {
		return ws
	}
	return r.IsAncestor
}

// AtHead is git.ApprovedAtHead for b's repo: ws unchanged for a Workspace bead, else
// the same pin-vs-tip compare read in the child.
func AtHead(b bd.Bead, ws func(body, branch string) bool) func(body, branch string) bool {
	r := RepoOf(b)
	if r == "" {
		return ws
	}
	return func(body, branch string) bool {
		pin, ok := git.ParseCommit(body)
		if !ok {
			return false
		}
		head, err := r.Head(branch)
		return err == nil && pin == head
	}
}

// LandedTip is the Tip: a land squash pinned, read in b's repo: the child's for a
// repo context, else ws.
func LandedTip(b bd.Bead, sha string, ws func(string) (string, bool)) (string, bool) {
	if r := RepoOf(b); r != "" {
		return r.LandedTip(sha)
	}
	return ws(sha)
}
