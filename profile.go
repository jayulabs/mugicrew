package main

import (
	"fmt"
	"os"
	"os/exec"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
)

// harnesses are the harnesses mugicrew supports, by catalog name (ADR 0125):
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
// always runnable. A name is unrunnable when it is absent from the catalog,
// its harness is unsupported or not on PATH, or its env names an unset ${VAR}
// (ADR 0123 §1); the error is the reason.
func resolveRecorded(root, name string) (agentprofile.Resolution, error) {
	if name == "" {
		return agentprofile.Resolution{Harness: builtinHarness}, nil
	}
	common, _ := git.CommonDirOf(root) // unresolvable: no local layer
	p, ok := agentprofile.Load(root, common)[name]
	if !ok {
		return agentprofile.Resolution{}, fmt.Errorf("absent from the catalog")
	}
	return checkProfile(p)
}

// checkProfile is the per-profile runnable check resolveRecorded and
// ProfileReasons share: harness supported, binary on PATH, ${VAR}s set.
func checkProfile(p agentprofile.Profile) (agentprofile.Resolution, error) {
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

// profileReasons checks an already-loaded catalog in one pass (no Load, no
// RepoRoot): name → unrunnable reason; runnable names are absent.
func profileReasons(catalog map[string]agentprofile.Profile) map[string]string {
	out := map[string]string{}
	for name, p := range catalog {
		if _, err := checkProfile(p); err != nil {
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

// ctxForProfile assembles the ctx for profile name from the current catalog,
// skipping the mapping: a resume's, from its session's recorded @profile (ADR
// 0122 §6). An unrunnable profile is the error.
func ctxForProfile(root, name string) (contract.Ctx, error) {
	r, err := resolveRecorded(root, name)
	if err != nil {
		return nil, err
	}
	return appCtx{harness: harnesses[r.Harness].build(r)}, nil
}
