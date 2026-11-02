package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/release"
)

type upgradeRig struct {
	npm, doctor []string
	npmCode     int
	doctorCode  int
	lock        string
}

func newUpgradeRig(t *testing.T, install release.Install) *upgradeRig {
	r := &upgradeRig{lock: filepath.Join(t.TempDir(), "loop.lock")}
	oldV, oldS, oldF, oldC, oldN, oldD, oldL := version, sourceRoot, release.Fetch, release.Classify, release.RunNpm, release.RunDoctor, upgradeLockPath
	t.Cleanup(func() {
		version, sourceRoot, release.Fetch, release.Classify, release.RunNpm, release.RunDoctor, upgradeLockPath = oldV, oldS, oldF, oldC, oldN, oldD, oldL
	})
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	version, sourceRoot = "1.0.0", ""
	release.Fetch = func() (release.Info, error) { return release.Info{Latest: "1.1.0"}, nil }
	release.Classify = func() release.Install { return install }
	release.RunNpm = func(argv []string) int { r.npm = argv; return r.npmCode }
	release.RunDoctor = func(exe string) int { r.doctor = append(r.doctor, exe); return r.doctorCode }
	upgradeLockPath = func() (string, error) { return r.lock, nil }
	return r
}

var npmGlobal = release.Install{Mode: release.NpmGlobal, Exe: "/p/mugicrew", Prefix: "/p"}

func upgrade(t *testing.T) (code int, out, errs string) {
	var o, e bytes.Buffer
	code = runUpgrade(nil, &o, &e)
	return code, o.String(), e.String()
}

func TestUpgradeRunsNpmThenDoctorAtExe(t *testing.T) {
	r := newUpgradeRig(t, npmGlobal)
	r.doctorCode = 3
	code, _, _ := upgrade(t)
	if strings.Join(r.npm, " ") != "npm i -g --prefix /p @jayulabs/mugicrew@1.1.0" {
		t.Errorf("npm argv %v", r.npm)
	}
	if len(r.doctor) != 1 || r.doctor[0] != "/p/mugicrew" || code != 3 {
		t.Errorf("doctor %v, exit %d; want doctor at /p/mugicrew and its code 3", r.doctor, code)
	}
}

func TestUpgradeNpmFailurePropagatesAndSkipsDoctor(t *testing.T) {
	r := newUpgradeRig(t, npmGlobal)
	r.npmCode = 243
	if code, _, _ := upgrade(t); code != 243 || r.doctor != nil {
		t.Errorf("exit %d, doctor %v; want 243 and no doctor", code, r.doctor)
	}
}

func TestUpgradeAlreadyLatestRunsNothing(t *testing.T) {
	r := newUpgradeRig(t, npmGlobal)
	release.Fetch = func() (release.Info, error) { return release.Info{Latest: "1.0.0"}, nil }
	code, out, _ := upgrade(t)
	if code != 0 || r.npm != nil || r.doctor != nil || !strings.Contains(out, "already the latest") {
		t.Errorf("exit %d out %q npm %v", code, out, r.npm)
	}
}

func TestUpgradeUnknownFailsAndRunsNothing(t *testing.T) {
	r := newUpgradeRig(t, release.Install{Exe: "/x/mugicrew", Why: "odd"})
	code, _, errs := upgrade(t)
	if code == 0 || r.npm != nil || !strings.Contains(errs, "/x/mugicrew") || !strings.Contains(errs, "odd") {
		t.Errorf("exit %d err %q npm %v", code, errs, r.npm)
	}
}

func TestUpgradeRefusesUnderHeldLoopLock(t *testing.T) {
	r := newUpgradeRig(t, npmGlobal)
	l, err := looplock.Acquire(r.lock, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	code, _, errs := upgrade(t)
	if code == 0 || r.npm != nil || !strings.Contains(errs, "mugicrew loop stop") {
		t.Errorf("exit %d err %q npm %v", code, errs, r.npm)
	}
}

func TestUpgradeRefusesFromSourceBuild(t *testing.T) {
	r := newUpgradeRig(t, npmGlobal)
	sourceRoot = "/src"
	code, _, errs := upgrade(t)
	if code == 0 || r.npm != nil || !strings.Contains(errs, "Watched loop") {
		t.Errorf("exit %d err %q", code, errs)
	}
}

func TestUpgradeRegistryFailureExitsNonZero(t *testing.T) {
	r := newUpgradeRig(t, npmGlobal)
	release.Fetch = func() (release.Info, error) { return release.Info{}, errors.New("offline") }
	if code, _, _ := upgrade(t); code == 0 || r.npm != nil {
		t.Errorf("exit %d npm %v", code, r.npm)
	}
}

func TestUsageListsUpgrade(t *testing.T) {
	if !strings.Contains(usage, "mugicrew upgrade") {
		t.Error("usage lacks upgrade")
	}
}

func TestUpgradePrintPlanRunsNothingAndExitsZero(t *testing.T) {
	for _, in := range []release.Install{
		{Mode: release.NpxTemp},
		{Mode: release.PnpmGlobal},
		{Mode: release.YarnGlobal},
		{Mode: release.ProjectLocal, Root: "/proj", PM: "pnpm", Dev: true},
	} {
		r := newUpgradeRig(t, in)
		r.lock = "/nonexistent/dir/loop.lock"
		code, out, _ := upgrade(t)
		if code != 0 || r.npm != nil || r.doctor != nil || out == "" {
			t.Errorf("%v: exit %d out %q npm %v doctor %v", in.Mode, code, out, r.npm, r.doctor)
		}
	}
}
