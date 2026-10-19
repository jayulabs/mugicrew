package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

)

// fixWS is a healthy doctor Workspace whose [jira] site is acme, with acli on
// *active (changed by the stubbed switch), and doctorExec recording every argv.
func fixWS(t *testing.T, active string) (ran *[][]string, now *string) {
	t.Helper()
	dir := doctorWS(t)
	writeCfg(t, filepath.Join(dir, ".mugicrew", "config.toml"), "land_strategy = \"local\"\n[initiatives]\nrequired = false\n[jira]\nsite = \"acme.atlassian.net\"\n")
	oldSite, oldExec := doctorACLISite, doctorExec
	t.Cleanup(func() { doctorACLISite, doctorExec = oldSite, oldExec })
	doctorACLISite = func() (string, error) { return active, nil }
	ran, now = new([][]string), &active
	doctorExec = func(argv []string) (string, error) {
		*ran = append(*ran, argv)
		return "switched", nil
	}
	return ran, now
}

func fixRun(t *testing.T) (string, int) {
	t.Helper()
	var b bytes.Buffer
	code := runDoctorFix(&b)
	return b.String(), code
}

var switchArgv = []string{"acli", "jira", "auth", "switch", "--site", "acme.atlassian.net"}

func TestDoctorACLIOtherSiteIsSafe(t *testing.T) {
	fixWS(t, "other.atlassian.net")
	out, _ := doctorRun(t)
	for _, w := range []string{"Safe — doctor fix runs:", "acli jira auth switch --site acme.atlassian.net", "More: docs/install.md#acli", "1 safe fix: run mugicrew doctor fix"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
}

func TestDoctorFixAppliedClearsFail(t *testing.T) {
	ran, now := fixWS(t, "other.atlassian.net")
	doctorExec = func(argv []string) (string, error) {
		*ran = append(*ran, argv)
		*now = "acme.atlassian.net"
		return "Switched to acme.atlassian.net\nextra", nil
	}
	out, code := fixRun(t)
	if code != 0 || !reflect.DeepEqual(*ran, [][]string{switchArgv}) {
		t.Errorf("code %d ran %v", code, *ran)
	}
	for _, w := range []string{"Applying safe fixes", "applied  acli site: acli jira auth switch --site acme.atlassian.net", "Switched to acme.atlassian.net", "Re-check", "ok  acli site: acme.atlassian.net"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
	if strings.Contains(out, "Left for you") {
		t.Errorf("manual list printed with nothing manual: %q", out)
	}
}

func TestDoctorFixAppliedLeavesFail(t *testing.T) {
	ran, _ := fixWS(t, "other.atlassian.net") // the switch exits 0 but acli stays put
	out, code := fixRun(t)
	if code != 1 || !reflect.DeepEqual(*ran, [][]string{switchArgv}) {
		t.Errorf("code %d ran %v out %q", code, *ran, out)
	}
	if !strings.Contains(out, "applied  acli site") || !strings.Contains(out, "FAIL  acli site") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorFixFailedPrintsFallbackAndCarriesOn(t *testing.T) {
	ran, _ := fixWS(t, "other.atlassian.net")
	// A second safe row after the acli one, to show the runner carries on.
	oldChecks := doctorChecks
	t.Cleanup(func() { doctorChecks = oldChecks })
	doctorChecks = append(append([]check{}, oldChecks...), func(string) []result {
		return []result{problem(stWarn, "second", "d", "p", fix{safe: []string{"second", "--go"}, steps: []string{"by hand"}})}
	})
	doctorExec = func(argv []string) (string, error) {
		*ran = append(*ran, argv)
		if argv[0] == "acli" {
			return "account with site acme.atlassian.net not found", errors.New("exit status 1")
		}
		return "fine", nil
	}
	out, code := fixRun(t)
	want := [][]string{switchArgv, {"second", "--go"}}
	if code != 1 || !reflect.DeepEqual(*ran, want) {
		t.Errorf("code %d ran %v", code, *ran)
	}
	for _, w := range []string{"failed   acli site: acli jira auth switch --site acme.atlassian.net", "account with site acme.atlassian.net not found", "now manual:", "acli jira auth login", "applied  second: second --go"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
}

func TestDoctorFixNothingSafe(t *testing.T) {
	ran, _ := fixWS(t, "acme.atlassian.net")
	oldChecks := doctorChecks
	t.Cleanup(func() { doctorChecks = oldChecks })
	doctorChecks = append(append([]check{}, oldChecks...), func(string) []result {
		return []result{problem(stFail, "hand", "broken", "p", manual("", "do it"))}
	})
	out, code := fixRun(t)
	if code != 1 || len(*ran) != 0 {
		t.Errorf("code %d ran %v", code, *ran)
	}
	for _, w := range []string{"Applying safe fixes", "nothing safe to apply", "Left for you (manual)", "FAIL  hand: broken", "Re-check"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
}

func TestDoctorFixWarnOnlyRecheckExitsZero(t *testing.T) {
	ran, _ := fixWS(t, "acme.atlassian.net")
	t.Setenv("BROWSER", "")
	out, code := fixRun(t)
	if code != 0 || len(*ran) != 0 {
		t.Errorf("code %d ran %v out %q", code, *ran, out)
	}
	if !strings.Contains(out, "Left for you (manual)") || !strings.Contains(out, "warn  browser") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorFixWritesNoEventLog(t *testing.T) {
	fixWS(t, "other.atlassian.net")
	fixRun(t)
	if _, err := os.Stat(".git/mugicrew"); !os.IsNotExist(err) {
		t.Errorf("doctor fix created the event-log dir: %v", err)
	}
}

func TestDoctorDispatch(t *testing.T) {
	var b bytes.Buffer
	if code := runDoctorCmd([]string{"bogus"}, &b); code != 1 || !strings.Contains(b.String(), "mugicrew doctor fix") {
		t.Errorf("bogus: code %d out %q", code, b.String())
	}
	b.Reset()
	if code := runDoctorCmd([]string{"fix", "x"}, &b); code != 1 || !strings.Contains(b.String(), "usage") {
		t.Errorf("fix x: code %d out %q", code, b.String())
	}
}

// doctor fix is no graph-required verb: in a repo with no bead graph it still runs.
func TestDoctorFixRunsWithNoBeadGraph(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(dir)
	self, _ := os.Executable()
	cmd := exec.Command(self, "doctor", "fix")
	cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1", "BEADS_DIR=")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "Applying safe fixes") || !strings.Contains(string(out), "Re-check") {
		t.Errorf("output %q, want the fix runner to run with no graph", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "mugicrew")); !os.IsNotExist(err) {
		t.Errorf("event-log dir created: %v", err)
	}
}

func TestDoctorBogusPrintsUsage(t *testing.T) {
	t.Chdir(t.TempDir())
	self, _ := os.Executable()
	cmd := exec.Command(self, "doctor", "bogus")
	cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "mugicrew doctor fix") {
		t.Errorf("doctor bogus = %v, %s; want exit 1 and usage", err, out)
	}
}

