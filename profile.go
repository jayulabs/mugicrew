package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/loop"
)

// harnesses are the harnesses mugicrew supports, by harness name (ADR 0125):
// the binary a profile's PATH check looks for and the constructor
// ctx assembly builds the backend with.
var harnesses = map[string]struct {
	binary string
	build  func(r agentprofile.Resolution) contract.Harness
}{
	"claudecode": {"claude", func(r agentprofile.Resolution) contract.Harness {
		return &claudecode.Backend{Model: r.Model, Env: r.Env}
	}},
}

// builtinHarness is what the unnamed built-in profile runs on (ADR 0122 §4).
const builtinHarness = "claudecode"

// lookPath is the PATH lookup of a harness binary; a var so tests stub it.
var lookPath = exec.LookPath

// resolveRecorded resolves a profile by name, skipping the mapping (resume
// reuses a session's recorded @profile, ADR 0122 §6). "" is the built-in,
// always runnable. A name is unrunnable when no file declares it, its table is
// bad, its harness is unsupported or not on PATH, or its env names an unset
// ${VAR} (ADR 0123 §1); the error is the reason, naming the file to fix.
func resolveRecorded(root, name string) (agentprofile.Resolution, error) {
	if name == "" {
		return agentprofile.Resolution{Harness: builtinHarness}, nil
	}
	e, ok := cfglayer.Profiles(root).Winners[name]
	if !ok {
		return agentprofile.Resolution{}, fmt.Errorf("not declared")
	}
	return checkProfile(root, e)
}

// checkProfile is the one reason builder resolveRecorded and ProfileReasons
// share: a bad table, harness supported, binary on PATH, ${VAR}s set. Every
// reason ends (from <file>), Workspace-relative under root else absolute
// (ADR 0306 §3).
func checkProfile(root string, e agentprofile.Entry) (agentprofile.Resolution, error) {
	r, err := runnable(e)
	if err != nil {
		return agentprofile.Resolution{}, &agentprofile.FileError{Path: e.Path, Err: fmt.Errorf("%s (from %s)", err, displayPath(root, e.Path))}
	}
	return r, nil
}

func runnable(e agentprofile.Entry) (agentprofile.Resolution, error) {
	p := e.Profile
	if e.Err != nil {
		return agentprofile.Resolution{}, fmt.Errorf("declared unusable: %s", e.Err)
	}
	h, ok := harnesses[p.Harness]
	if !ok {
		return agentprofile.Resolution{}, fmt.Errorf("unsupported harness %s", p.Harness)
	}
	if _, err := lookPath(h.binary); err != nil {
		return agentprofile.Resolution{}, fmt.Errorf("%s not on PATH", h.binary)
	}
	env, err := agentprofile.Expand(p.Env, os.LookupEnv)
	if err != nil {
		return agentprofile.Resolution{}, err
	}
	return agentprofile.Resolution{Profile: p.Name, Harness: p.Harness, Model: p.Model, Env: env}, nil
}

// displayPath is path relative to root when it lies under it, else absolute.
func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}

// profileReasons checks an already-merged set in one pass (no read, no
// RepoRoot): name → unrunnable reason; runnable names are absent.
func profileReasons(root string, declared map[string]agentprofile.Entry) map[string]string {
	out := map[string]string{}
	for name, e := range declared {
		if _, err := checkProfile(root, e); err != nil {
			out[name] = err.Error()
		}
	}
	return out
}

// ctxForKind assembles the ctx an effort or HITL launch of kind runs with (ADR
// 0125): a backend built for its resolved profile, and the profile
// name ("" = the built-in), named on an error too. An unrunnable profile is the
// error.
func ctxForKind(root string, kind contract.Kind) (contract.Ctx, string, error) {
	name := loop.KindProfile(root, kind).Name
	ctx, err := ctxForProfile(root, name)
	return ctx, name, err
}

// ctxForProfile assembles the ctx for profile name from the current merged profiles,
// skipping the mapping: a resume's, from its session's recorded @profile (ADR
// 0122 §6). An unrunnable profile is the error.
func ctxForProfile(root, name string) (contract.Ctx, error) {
	r, err := resolveRecorded(root, name)
	if err != nil {
		return nil, err
	}
	return appCtx{harness: harnesses[r.Harness].build(r)}, nil
}
