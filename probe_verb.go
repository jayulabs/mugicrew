package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// The Forge reads `probe` makes beyond getMR; seams for the verb's tests.
var (
	probeFindOpenMR   = forge.FindOpenMR
	probeGetPipeline  = forge.GetPipeline
	probeMainPipeline = forge.MainPipeline

	probeShow           = bd.Show
	probeRollbackTarget = bd.RollbackTarget
	probeRepoOf         = contexts.RepoOf
	probeRepos          = contexts.Repos
	probeFetch          = git.Repo.FetchTracking
	probeForgeRemote    = git.Repo.ForgeRemote
	probeLandedOn       = git.Repo.LandedBaseOn
	probeForeignScan    = git.Repo.ForeignScan
	probeMainline       = git.Repo.Mainline
	probeRepoProject    = func(r git.Repo) (forge.Project, error) { return git.SameHostProject(r, git.Repo.ForgeProject) }
)

const probeUsage = "usage: mugicrew probe mr (<ref> | --branch <b>) | pipeline <ref> (<id> | --main) | landed <id> | foreign [--context <c>]"

// runProbe is `mugicrew probe <subverb>` (ADR 0336): a read-only read of Forge state
// that prints one word and returns nil whenever the read succeeded, whatever the
// word, so a Hold Task's Check tells a fail (still running) from an error.
func runProbe(argv []string, w io.Writer) error {
	if len(argv) == 0 {
		return errors.New(probeUsage)
	}
	var word string
	var err error
	switch argv[0] {
	case "mr":
		word, err = probeMR(argv[1:])
	case "pipeline":
		word, err = probePipeline(argv[1:])
	case "landed":
		word, err = probeLanded(argv[1:])
	case "foreign":
		word, err = probeForeign(argv[1:])
	default:
		return errors.New(probeUsage)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, word)
	return err
}

// probeForgeProject is the Workspace's Forge project, refused under land_strategy=local.
func probeForgeProject() (forge.Project, error) {
	name, err := git.LandStrategyName()
	if err != nil {
		return forge.Project{}, fmt.Errorf("probe: %w", err)
	}
	if name != git.LandStrategyGitlab {
		return forge.Project{}, fmt.Errorf("probe: no Forge: this Workspace is land_strategy=%s; probe mr and probe pipeline need gitlab", name)
	}
	fp, err := forgeProject()
	if err != nil {
		return forge.Project{}, fmt.Errorf("probe: %w; check the Forge remote", err)
	}
	return fp, nil
}

func probeMR(argv []string) (string, error) {
	fs := flag.NewFlagSet("probe mr", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	branch := fs.String("branch", "", "")
	var ref string
	err := fs.Parse(argv)
	if err == nil && fs.NArg() > 0 && *branch == "" { // the ref may sit before the flags
		ref = fs.Arg(0)
		err = fs.Parse(fs.Args()[1:])
	}
	if err != nil || fs.NArg() > 0 || (ref == "") == (*branch == "") {
		return "", errors.New(probeUsage)
	}
	fp, err := probeForgeProject()
	if err != nil {
		return "", err
	}
	if *branch != "" {
		if fp, err = probeBranchProject(*branch, fp); err != nil {
			return "", err
		}
		mr, err := probeFindOpenMR(fp, *branch)
		if err != nil {
			return "", fmt.Errorf("probe mr --branch %s: %w", *branch, err)
		}
		if mr == nil {
			return "none", nil
		}
		return mr.State, nil
	}
	proj, iid, err := forge.ParseMRRef(ref, fp)
	if err != nil {
		return "", fmt.Errorf("probe mr: %w", err)
	}
	mr, err := getMR(proj, iid)
	if err != nil {
		return "", fmt.Errorf("probe mr %s: %w", ref, err)
	}
	return mr.State, nil
}

// probeBranchProject is the Forge project of the bead a <kind>/<id> branch names: a
// nested container's own project (as Jinbe resolves it), else the Workspace's ws.
func probeBranchProject(branch string, ws forge.Project) (forge.Project, error) {
	_, id := git.ParseBranchName(branch)
	if id == "" {
		return ws, nil
	}
	b, err := probeShow(id)
	if err != nil {
		return forge.Project{}, fmt.Errorf("probe mr --branch %s: %w", branch, err)
	}
	r := probeRepoOf(b)
	if r == "" {
		return ws, nil
	}
	proj, err := probeRepoProject(r)
	if err != nil {
		return forge.Project{}, fmt.Errorf("probe mr --branch %s: %w", branch, err)
	}
	return proj, nil
}

func probePipeline(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New(probeUsage)
	}
	ref, rest := argv[0], argv[1:]
	if len(rest) != 1 {
		return "", errors.New(probeUsage)
	}
	id := 0
	if rest[0] != "--main" {
		var err error
		if id, err = strconv.Atoi(rest[0]); err != nil || id < 1 {
			return "", errors.New(probeUsage)
		}
	}
	fp, err := probeForgeProject()
	if err != nil {
		return "", err
	}
	proj, _, err := forge.ParseMRRef(ref, fp)
	if err != nil {
		return "", fmt.Errorf("probe pipeline: %w", err)
	}
	var pl *forge.Pipeline
	if id == 0 {
		if pl, err = probeMainPipeline(proj); err == nil && pl == nil {
			err = errors.New("no pipeline on the mainline")
		}
	} else {
		pl, err = probeGetPipeline(proj, id)
	}
	if err != nil {
		return "", fmt.Errorf("probe pipeline %s: %w", ref, err)
	}
	return pl.Status, nil
}

// probeMainRef is the mainline ref landed and foreign read in repo r: under gitlab the
// Forge's main fetched into its remote-tracking ref (never a fast-forward of local
// main, so a re-run is a no-op); under local, local main with no fetch.
func probeMainRef(r git.Repo) (string, error) {
	name, err := git.LandStrategyName()
	if err != nil {
		return "", fmt.Errorf("probe: %w", err)
	}
	if name != git.LandStrategyGitlab {
		return probeMainline(r), nil
	}
	ref, err := probeFetch(r, probeForgeRemote(r))
	if err != nil {
		return "", fmt.Errorf("probe: fetch the Forge's main: %w", err)
	}
	return ref, nil
}

func probeLanded(argv []string) (string, error) {
	if len(argv) != 1 {
		return "", errors.New(probeUsage)
	}
	b, err := probeShow(argv[0])
	if err != nil {
		return "", fmt.Errorf("probe landed %s: %w", argv[0], err)
	}
	var trailer string
	repoBead := b // the bead whose repo the trailer is read in
	switch b.Kind {
	case contract.KindChart, contract.KindCheckpoint:
		trailer = git.TrailerFor(string(b.Kind), b.ID)
	case contract.KindRollback:
		target, ok, err := probeRollbackTarget(b.ID)
		if err != nil {
			return "", fmt.Errorf("probe landed %s: %w", b.ID, err)
		}
		if !ok {
			return "", fmt.Errorf("probe landed %s: the Rollback has no related Checkpoint", b.ID)
		}
		trailer = git.RevertsTrailer(string(contract.KindCheckpoint), target)
		// a Rollback's repo is its target's unless it carries its own stamp (Jinbe's decidingBead)
		if cp, err := probeShow(target); err == nil && bd.RepoStampOf(b) == "" {
			repoBead = cp
		}
	default:
		return "", fmt.Errorf("probe landed %s: a %s has no land trailer; want a Chart, Checkpoint or Rollback", b.ID, b.Kind)
	}
	r := probeRepoOf(repoBead)
	ref, err := probeMainRef(r)
	if err != nil {
		return "", err
	}
	_, err = probeLandedOn(r, ref, trailer)
	var foreign *git.ForeignLanding
	switch {
	case err == nil:
		return "landed", nil
	case errors.Is(err, git.ErrNotLanded):
		return "absent", nil
	case errors.As(err, &foreign):
		return "foreign", nil
	}
	return "", fmt.Errorf("probe landed %s: %w", b.ID, err)
}

func probeForeign(argv []string) (string, error) {
	fs := flag.NewFlagSet("probe foreign", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	context := fs.String("context", "", "")
	if fs.Parse(argv) != nil || fs.NArg() > 0 {
		return "", errors.New(probeUsage)
	}
	var r git.Repo
	if *context != "" {
		dir, ok := probeRepos()[*context]
		if !ok {
			return "", fmt.Errorf("probe foreign: no repo = entry for context %q", *context)
		}
		root, err := git.RepoRoot()
		if err != nil {
			return "", fmt.Errorf("probe foreign: %w", err)
		}
		r = git.Repo(filepath.Join(root, dir))
	}
	ref, err := probeMainRef(r)
	if err != nil {
		return "", err
	}
	err = probeForeignScan(r, ref)
	var foreign *git.ForeignLanding
	switch {
	case err == nil:
		return "none", nil
	case errors.As(err, &foreign):
		return "foreign", nil
	}
	return "", fmt.Errorf("probe foreign: %w", err)
}
