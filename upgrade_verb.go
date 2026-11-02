package main

import (
	"fmt"
	"io"
	"strings"

	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/release"
)

const upgradeUsage = "usage: mugicrew upgrade"

// upgradeLockPath is this Workspace's loop lock; a var so tests point it at a
// temp lock.
var upgradeLockPath = func() (string, error) {
	path, _, err := resolveLoop()
	return path, err
}

// runUpgrade is `mugicrew upgrade`: it moves an npm global install onto the latest
// release, then shows the new binary's doctor. Any other install prints what to
// run and changes nothing. The exit code is npm's on failure, doctor's on success.
func runUpgrade(argv []string, w, errw io.Writer) int {
	if len(argv) != 0 {
		fmt.Fprintln(errw, upgradeUsage)
		return 1
	}
	if sourceRoot != "" {
		fmt.Fprintln(errw, "mugicrew: a from-source build is kept current by a Watched loop; nothing to upgrade")
		return 1
	}
	if !release.Eligible(version, sourceRoot) {
		fmt.Fprintln(errw, "mugicrew: this build carries no release version; only an npm-installed mugicrew upgrades")
		return 1
	}
	latest, err := release.Check(true)
	if err != nil {
		fmt.Fprintln(errw, "mugicrew: cannot read the latest release from the npm registry:", err)
		return 1
	}
	if !release.Newer(latest, version) {
		fmt.Fprintf(w, "mugicrew %s is already the latest release\n", version)
		return 0
	}
	in := release.Classify()
	plan := release.Plan(in, latest)
	if plan.Run == nil {
		out := w
		if plan.Fail {
			out = errw
		}
		fmt.Fprint(out, plan.Text)
		if plan.Fail {
			return 1
		}
		return 0
	}
	path, err := upgradeLockPath()
	if err != nil {
		fmt.Fprintln(errw, err)
		return 1
	}
	if st, err := looplock.Read(path); err == nil && st.State != looplock.Off {
		fmt.Fprintf(errw, "mugicrew: a loop is running (pid %d); run mugicrew loop stop, then upgrade\n", st.Pid)
		return 1
	}
	fmt.Fprintf(w, "upgrading %s to %s: %s\n", version, latest, strings.Join(plan.Run, " "))
	if code := release.RunNpm(plan.Run); code != 0 {
		fmt.Fprintln(errw, "mugicrew: the upgrade failed; nothing else was run")
		return code
	}
	fmt.Fprintf(w, "upgraded to %s; mugicrew doctor:\n", latest)
	return release.RunDoctor(in.Exe)
}
