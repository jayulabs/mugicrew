package chopper

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
)

// List is `mugicrew recover` with no bead, a pure view (ADR 0033): no write, no
// event. It prints one row per candidate (a mugicrew bead in_progress or paused
// abandoned or recovering, one whose effort tree no live holder runs in, one
// whose tree is stopped mid-rebase) with its verdict, leftovers and way out, then
// under their own heading the detached temp trees no bead links to, which recover
// never repairs.
func List(w io.Writer) error {
	ids := map[string]bool{} // candidate → it is one by more than its tree
	in, err := inProgress()
	if err != nil {
		return err
	}
	ps, err := pausedList()
	if err != nil {
		return err
	}
	for _, b := range append(in, ps...) {
		if _, err := registry.Lookup(b.Kind); err == nil && (b.Status == "in_progress" || abandoned(b) || bd.PausedBy(b, bd.PausedCheckup)) {
			ids[b.ID] = true
		}
	}
	trees, err := worktrees()
	if err != nil {
		return err
	}
	rebases, err := rebasing()
	if err != nil {
		return err
	}
	hosts, err := nestedHosts()
	if err != nil {
		return err
	}
	var unowned []string
	for _, t := range trees {
		branch := t.Branch
		if branch == "" {
			branch = cmp.Or(rebases[t.Path], hosts[t.Path])
		}
		if id, ok := strings.CutPrefix(branch, git.EffortBranch("")); ok {
			if _, ok := ids[id]; !ok {
				ids[id] = false // a candidate by its tree alone: listed unless its holder reads alive
			}
		} else if t.Branch == "" && git.IsTempWorktree(t.Path) {
			unowned = append(unowned, t.Path)
		}
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := 0
	row := func(cols ...string) {
		if rows == 0 {
			fmt.Fprintln(tw, "ID\tVERDICT\tLEFTOVERS\tWAY OUT")
		}
		fmt.Fprintln(tw, strings.Join(cols, "\t"))
		rows++
	}
	self := lineage(os.Getpid())
	var hs hostState // one snapshot of the host per listing, read when a first row needs it
	var wErr error
	seen := false
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		inv, err := Take(id)
		if err != nil {
			row(id, "refused", "?", "can't take its inventory: "+err.Error())
			continue
		}
		if !inv.stranded() {
			continue
		}
		if !seen {
			hs, wErr = look(Opts{})
			seen = true
		}
		verdict, out := listVerdict(hs, wErr, inv, self)
		if verdict == Alive && !ids[id] {
			continue
		}
		row(id, verdict, Leftovers(inv), out)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if rows == 0 && len(unowned) == 0 {
		_, err := fmt.Fprintln(w, "nothing stranded")
		return err
	}
	if len(unowned) > 0 {
		if rows > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "UNOWNED (detached temp trees no bead links to; recover never repairs them)")
		for _, p := range unowned {
			fmt.Fprintln(w, p)
		}
	}
	return nil
}

// listVerdict is inv's row verdict (alive, dead, ambiguous or refused) and its
// way out: the recover to run when licensed, else the refusal's reason.
func listVerdict(hs hostState, werr error, inv Inventory, self []int) (string, string) {
	id := inv.Bead.ID
	v, err := "", werr
	if err == nil {
		v, err = judgeIn(hs, inv, Opts{}, self, false)
	}
	if err != nil {
		var rf *Refused
		if errors.As(err, &rf) && rf.Verdict != "" {
			v = rf.Verdict
		} else {
			v = "refused"
		}
		return v, strings.TrimPrefix(err.Error(), id+": ")
	}
	if v == AmbiguousResolved { // the loop lock is free and no TUI is alive
		v = Ambiguous
	}
	out := "mugicrew recover " + id
	if inv.Files > 0 {
		out += " --discard"
	}
	if bd.PausedBy(inv.Bead, bd.PausedRecovering) {
		out = "re-run " + out + " (a recover stopped partway)"
	}
	return v, out
}

// Leftovers is inv's claim, tree (dirty count, mid-rebase), Draft count and pauses.
func Leftovers(inv Inventory) string {
	var l []string
	if inv.Bead.Status == "in_progress" {
		l = append(l, "claimed")
	}
	if inv.Tree != "" {
		t := "tree " + inv.Tree
		if inv.Files > 0 {
			t += fmt.Sprintf(" (%d dirty)", inv.Files)
		}
		if inv.Rebasing {
			t += " mid-rebase"
		}
		l = append(l, t)
	}
	if len(inv.Drafts) > 0 {
		l = append(l, fmt.Sprintf("%d Drafts", len(inv.Drafts)))
	}
	if cs := bd.PausedCauses(inv.Bead); len(cs) > 0 {
		l = append(l, "paused "+strings.Join(cs, ","))
	}
	if len(l) == 0 {
		return "-"
	}
	return strings.Join(l, "; ")
}

// List's reads, vars so its tests run with no bd and no git.
var (
	inProgress = bd.InProgress
	pausedList = bd.PausedList
	worktrees  = git.Worktrees
	rebasing   = git.Rebasing
	// nestedHosts is each detached host tree a declared child holds an effort/<id> in
	// (Spec D8), host path to branch; nil, with no git call, when no repo is declared.
	nestedHosts = func() (map[string]string, error) {
		repos := cfglayer.Repos(".")
		if len(repos) == 0 {
			return nil, nil
		}
		root, err := git.RepoRoot()
		if err != nil {
			return nil, err
		}
		hosts := map[string]string{}
		for _, dir := range slices.Compact(slices.Sorted(maps.Values(repos))) {
			r := git.Repo(filepath.Join(root, dir))
			if !git.IsRepoRoot(string(r)) {
				continue
			}
			ts, err := r.Worktrees()
			if err != nil {
				return nil, err
			}
			rb, err := r.Rebasing()
			if err != nil {
				return nil, err
			}
			for _, t := range ts {
				if b := cmp.Or(t.Branch, rb[t.Path]); strings.HasPrefix(b, git.EffortBranch("")) {
					hosts[strings.TrimSuffix(t.Path, string(filepath.Separator)+filepath.Clean(dir))] = b
				}
			}
		}
		return hosts, nil
	}
)
