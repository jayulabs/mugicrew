package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/release"
	"jayulabs.org/mugicrew/selfexe"
)

// afterVerb runs once the verb is done, before the process exits: the Release
// notice's print. A no-op unless startReleaseNotice armed it.
var afterVerb = func() {}

// exit runs afterVerb, then exits: main's os.Exit, so a verb that exits itself
// still gets its Release notice.
func exit(code int) {
	afterVerb()
	os.Exit(code)
}

// stderrIsTTY is a var so tests stub the terminal check.
var stderrIsTTY = func() bool { return term.IsTerminal(os.Stderr.Fd()) }

// noticeSkipVerbs never check or notice: the hidden verbs, the loop and its
// re-exec markers, and upgrade, which says its own news.
var noticeSkipVerbs = map[string]bool{
	"hitl-hook": true, "hitl-run": true, "hitl-follow": true, "run": true,
	"reconcile": true, "integrate": true, "doc-pane": true, "bead-detail": true,
	"loop": true, "upgrade": true,
}

// releaseNoticeOff reports an env or config opt-out. The env var wins over the
// config key, which reads through cfglayer (local layer first).
func releaseNoticeOff() bool {
	switch strings.ToLower(os.Getenv("MUGICREW_RELEASE_NOTICE")) {
	case "0", "off", "false":
		return true
	}
	if root, err := git.RepoRoot(); err == nil {
		return !cfglayer.ReleaseNotice(root)
	}
	return false
}

// noticeEnabled is the eligibility every Release notice shares: an npm build, not
// a crewmate session, not opted out.
func noticeEnabled() bool {
	return release.Eligible(version, sourceRoot) && os.Getenv("MUGICREW_EFFORT") == "" && !releaseNoticeOff()
}

// unattendedUpgradeOptedIn reads the opt-in: MUGICREW_UNATTENDED_UPGRADE wins
// over the personal-layer key, 1/on/true on and 0/off/false off.
func unattendedUpgradeOptedIn() bool {
	switch strings.ToLower(os.Getenv("MUGICREW_UNATTENDED_UPGRADE")) {
	case "1", "on", "true":
		return true
	case "0", "off", "false":
		return false
	}
	return cfglayer.UnattendedUpgrade()
}

// unattendedUpgradeEnabled is the loop's gate for an unattended Upgrade: the
// check is not opted out, the build is an npm build whose startup binary is an
// NpmGlobal install, and the human opted in. MUGICREW_EFFORT plays no part.
func unattendedUpgradeEnabled() bool {
	return !releaseNoticeOff() && release.Eligible(version, sourceRoot) &&
		release.Classify().Mode == release.NpmGlobal && unattendedUpgradeOptedIn()
}

// tuiReleaseCheck is the TUI's Release notice check: the same gate and cache as the
// CLI's, run as a tea.Cmd; the newer release's version, or "" for nothing to show.
func tuiReleaseCheck() string {
	if !noticeEnabled() {
		return ""
	}
	latest, err := release.Check(false)
	if err != nil || !release.Newer(latest, version) {
		return ""
	}
	return latest
}

// startReleaseNotice arms the Release notice for a human-facing verb of an
// npm-installed build. It starts the registry check in a goroutine and returns the
// print to run after the verb, which reads only the cache and never waits on the
// goroutine, so a cold cache costs no latency and prints nothing on the run that
// filled it. The bare TUI shows its notice in the tab bar, not here.
func startReleaseNotice(args []string) func() {
	none := func() {}
	if len(args) == 0 || args[0] == "--watch" || noticeSkipVerbs[args[0]] || !noticeEnabled() {
		return none
	}
	go release.Check(false)
	return func() {
		if !stderrIsTTY() {
			return
		}
		if latest := release.Cached(); release.Newer(latest, version) {
			fmt.Fprintf(os.Stderr, "mugicrew %s is out (you run %s); run mugicrew upgrade\n", latest, version)
		}
	}
}

// installedUpgrader is the loop's Upgrader (Spec mc-d0jkl §4): the cached release
// check, the exact-version npm plan mugicrew upgrade uses, and the new binary's
// doctor, all behind the loop's narrow seam.
//
// release.Check caches only a success, so a failing registry read would repeat
// every tick; the upgrader remembers its last failure and asks again only after
// release.TTL, keeping the loop to one registry read a day (Spec FR 10).
type installedUpgrader struct {
	failedAt time.Time
}

func (u *installedUpgrader) Check() (target, blockedBy string, ok bool) {
	if !u.failedAt.IsZero() {
		if age := release.Now().Sub(u.failedAt); age >= 0 && age < release.TTL {
			return "", "", false
		}
	}
	if _, err := release.Check(false); err != nil {
		u.failedAt = release.Now()
		return "", "", false
	}
	u.failedAt = time.Time{}
	info, known := release.Facts()
	if !known { // an older binary's cache: wait for the next refresh
		return "", "", false
	}
	target, blockedBy = release.Target(version, info.Latest, info.Versions, info.HumanStep)
	return target, blockedBy, true
}

// noPlanCode is Install's code when release.Plan yields nothing to run: npm never ran.
const noPlanCode = -1

func (*installedUpgrader) Install(target string) int {
	plan := release.Plan(release.Classify(), target)
	if plan.Run == nil {
		slog.Error("upgrade.no-plan", "to", target, "why", strings.TrimSpace(plan.Text))
		return noPlanCode
	}
	slog.Info("upgrade.installing", "from", version, "to", target, "argv", plan.Run)
	_, code := release.RunCaptured(plan.Run)
	return code
}

func (*installedUpgrader) Doctor() (code, fixes int) {
	self, err := selfexe.Path()
	if err != nil {
		return 1, 0
	}
	out, code := release.RunCaptured([]string{self, "doctor"})
	return code, release.FixCount(out)
}
