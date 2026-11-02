package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/internal/isolate"
	"jayulabs.org/mugicrew/release"
)

// seedRelease writes a release cache saying latest is out, under cache (the
// dir os.UserCacheDir resolves), where release.CachePath reads it.
func seedRelease(t *testing.T, cache, latest string) {
	t.Helper()
	dir := filepath.Join(cache, "mugicrew")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"checked_at": time.Now(), "latest": latest})
	if err := os.WriteFile(filepath.Join(dir, "release.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// noBdPath is a PATH holding git (the child's `ps` runs in a git repo) and no bd.
func noBdPath(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	if err := os.Symlink(git, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// noticeRun re-execs this binary as `mugicrew ps` in a fresh repo, an npm build
// at 1.0.0 whose cache already says 2.0.0 is out, and returns its stderr. env
// overrides the defaults.
func noticeRun(t *testing.T, env ...string) string {
	t.Helper()
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	iso, _, cache := isolate.Env(t)
	seedRelease(t, cache, "2.0.0")
	self, _ := os.Executable()
	cmd := exec.Command(self, "ps")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), iso...)
	cmd.Env = append(cmd.Env, "MUGICREW_TEST_MAIN=1", "MUGICREW_TEST_VERSION=1.0.0", "MUGICREW_TEST_TTY=1",
		"PATH="+noBdPath(t), "MUGICREW_EFFORT=", "MUGICREW_RELEASE_NOTICE=", "BEADS_DIR="+t.TempDir())
	cmd.Env = append(cmd.Env, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Run()
	return stderr.String()
}

func noticeLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "mugicrew upgrade") {
			n++
		}
	}
	return n
}

func TestReleaseNoticePrintsOneLine(t *testing.T) {
	got := noticeRun(t)
	if noticeLines(got) != 1 || !strings.Contains(got, "2.0.0") {
		t.Errorf("stderr %q; want exactly one notice naming 2.0.0", got)
	}
}

func TestReleaseNoticeSilentWhen(t *testing.T) {
	for name, env := range map[string][]string{
		"not a TTY":       {"MUGICREW_TEST_TTY="},
		"effort set":      {"MUGICREW_EFFORT=mc-1"},
		"env opt-out 0":   {"MUGICREW_RELEASE_NOTICE=0"},
		"env opt-out off": {"MUGICREW_RELEASE_NOTICE=off"},
		"from source":     {"MUGICREW_TEST_SOURCE_ROOT=/src"},
		"unstamped":       {"MUGICREW_TEST_VERSION="},
	} {
		if got := noticeRun(t, env...); noticeLines(got) != 0 {
			t.Errorf("%s: stderr %q; want no notice", name, got)
		}
	}
}

func TestReleaseNoticeConfigOptOut(t *testing.T) {
	repo := t.TempDir()
	exec.Command("git", "-C", repo, "init", "-q").Run()
	os.MkdirAll(filepath.Join(repo, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(repo, ".mugicrew", "config.toml"), []byte("[release]\nnotice = false\n"), 0o644)
	iso, _, cache := isolate.Env(t)
	seedRelease(t, cache, "2.0.0")
	self, _ := os.Executable()
	cmd := exec.Command(self, "ps")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), iso...)
	cmd.Env = append(cmd.Env, "MUGICREW_TEST_MAIN=1", "MUGICREW_TEST_VERSION=1.0.0", "MUGICREW_TEST_TTY=1",
		"PATH="+noBdPath(t), "MUGICREW_EFFORT=", "MUGICREW_RELEASE_NOTICE=", "BEADS_DIR="+t.TempDir())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Run()
	if noticeLines(stderr.String()) != 0 {
		t.Errorf("stderr %q; want no notice under [release] notice = false", stderr.String())
	}
}

func TestReleaseNoticeSkipsVersionAndHelp(t *testing.T) {
	self, _ := os.Executable()
	for _, arg := range []string{"--version", "--help"} {
		cmd := exec.Command(self, arg)
		cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1", "MUGICREW_TEST_VERSION=1.0.0", "MUGICREW_TEST_TTY=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Run()
		if stderr.Len() != 0 {
			t.Errorf("%s: stderr %q", arg, stderr.String())
		}
	}
}

func TestTUIReleaseCheck(t *testing.T) {
	_, cache := isolate.Dirs(t)
	oldV, oldS := version, sourceRoot
	t.Cleanup(func() { version, sourceRoot = oldV, oldS })
	for _, c := range []struct {
		name, running, src, cached, effort, off, want string
	}{
		{"newer", "1.0.0", "", "2.0.0", "", "", "2.0.0"},
		{"equal", "2.0.0", "", "2.0.0", "", "", ""},
		{"older", "3.0.0", "", "2.0.0", "", "", ""},
		{"from source", "1.0.0", "/src", "2.0.0", "", "", ""},
		{"opted out", "1.0.0", "", "2.0.0", "", "off", ""},
		{"crewmate", "1.0.0", "", "2.0.0", "mc-1", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			version, sourceRoot = c.running, c.src
			seedRelease(t, cache, c.cached)
			t.Setenv("MUGICREW_EFFORT", c.effort)
			t.Setenv("MUGICREW_RELEASE_NOTICE", c.off)
			if got := tuiReleaseCheck(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestUnattendedUpgradeGate(t *testing.T) {
	cfg, _ := isolate.Dirs(t)
	t.Setenv("MUGICREW_EFFORT", "")
	t.Setenv("MUGICREW_RELEASE_NOTICE", "")
	t.Setenv("MUGICREW_UNATTENDED_UPGRADE", "")
	oldV, oldS, oldC := version, sourceRoot, release.Classify
	t.Cleanup(func() { version, sourceRoot, release.Classify = oldV, oldS, oldC })
	version, sourceRoot = "1.0.0", ""
	mode := release.NpmGlobal
	release.Classify = func() release.Install { return release.Install{Mode: mode} }
	setKey := func(body string) {
		os.MkdirAll(filepath.Join(cfg, "mugicrew"), 0o755)
		os.WriteFile(filepath.Join(cfg, "mugicrew", "config.toml"), []byte(body), 0o644)
	}
	on, off := "[upgrade]\nunattended = true\n", "[upgrade]\nunattended = false\n"
	for _, c := range []struct {
		name, key, env, notice string
		mode                   release.Mode
		src                    string
		want                   bool
	}{
		{"unset", "", "", "", release.NpmGlobal, "", false},
		{"key on", on, "", "", release.NpmGlobal, "", true},
		{"env on without key", "", "1", "", release.NpmGlobal, "", true},
		{"env true", "", "TRUE", "", release.NpmGlobal, "", true},
		{"env on", "", "on", "", release.NpmGlobal, "", true},
		{"env off beats key", on, "off", "", release.NpmGlobal, "", false},
		{"env 0 beats key", on, "0", "", release.NpmGlobal, "", false},
		{"env false beats key", on, "false", "", release.NpmGlobal, "", false},
		{"env on beats key off", off, "1", "", release.NpmGlobal, "", true},
		{"notice opt-out", on, "", "0", release.NpmGlobal, "", false},
		{"notice opt-out beats env on", "", "1", "off", release.NpmGlobal, "", false},
		{"from source", on, "", "", release.NpmGlobal, "/src", false},
		{"project local", on, "", "", release.ProjectLocal, "", false},
		{"pnpm global", on, "", "", release.PnpmGlobal, "", false},
		{"yarn global", on, "", "", release.YarnGlobal, "", false},
		{"npx", on, "", "", release.NpxTemp, "", false},
		{"unknown", on, "", "", release.Unknown, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			setKey(c.key)
			t.Setenv("MUGICREW_UNATTENDED_UPGRADE", c.env)
			t.Setenv("MUGICREW_RELEASE_NOTICE", c.notice)
			mode, sourceRoot = c.mode, c.src
			if got := unattendedUpgradeEnabled(); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestInstalledUpgraderBacksOffAfterFailedRead(t *testing.T) {
	oldF, oldN := release.Fetch, release.Now
	t.Cleanup(func() { release.Fetch, release.Now = oldF, oldN })
	isolate.Dirs(t)
	now := time.Now()
	release.Now = func() time.Time { return now }
	calls := 0
	release.Fetch = func() (release.Info, error) { calls++; return release.Info{}, os.ErrDeadlineExceeded }
	u := &installedUpgrader{}
	for i := 0; i < 5; i++ {
		if _, _, ok := u.Check(); ok {
			t.Fatal("ok on a failed read")
		}
		now = now.Add(15 * time.Second)
	}
	if calls != 1 {
		t.Fatalf("Fetch called %d times inside the TTL, want 1", calls)
	}
	now = now.Add(release.TTL)
	u.Check()
	if calls != 2 {
		t.Fatalf("Fetch called %d times after the TTL, want 2", calls)
	}
}
