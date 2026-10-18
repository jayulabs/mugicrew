package jira

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// acliReply is one canned acli call.
type acliReply struct {
	out, err string
	code     int
}

// fakeAcli puts a /bin/sh script named acli first on PATH that logs its argv and
// replies per call: view for "workitem view", status for "auth status".
func fakeAcli(t *testing.T, view, status acliReply) (argvLog string) {
	t.Helper()
	dir := t.TempDir()
	for n, r := range map[string]acliReply{"view": view, "status": status} {
		os.WriteFile(filepath.Join(dir, n+".out"), []byte(r.out), 0o644)
		os.WriteFile(filepath.Join(dir, n+".err"), []byte(r.err), 0o644)
		os.WriteFile(filepath.Join(dir, n+".code"), []byte(strconv.Itoa(r.code)), 0o644)
	}
	script := "#!/bin/sh\nD=" + dir + "\necho \"$*\" >> $D/argv.log\nn=view; [ \"$2\" = auth ] && n=status\ncat $D/$n.out; cat $D/$n.err >&2; exit $(cat $D/$n.code)\n"
	os.WriteFile(filepath.Join(dir, "acli"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return filepath.Join(dir, "argv.log")
}

func TestViewReadsKeyAndSummaryInOneCall(t *testing.T) {
	log := fakeAcli(t, acliReply{out: `{"key":"ABC-2","self":"https://internal/x","fields":{"summary":"Fix the thing","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}}`}, acliReply{})
	it, err := View("ABC-1", "acme.atlassian.net")
	if err != nil || it != (Item{Key: "ABC-2", Summary: "Fix the thing", Status: "In Progress", Category: "indeterminate"}) {
		t.Fatalf("View = %+v, %v", it, err)
	}
	b, _ := os.ReadFile(log)
	if want := "jira workitem view ABC-1 --fields summary,status --json\n"; string(b) != want {
		t.Errorf("argv log = %q, want %q", b, want)
	}
}

func TestItemDone(t *testing.T) {
	for cat, want := range map[string]bool{"done": true, "DONE": true, "indeterminate": false, "new": false} {
		if got := (Item{Category: cat}).Done(); got != want {
			t.Errorf("Done(%q) = %v, want %v", cat, got, want)
		}
	}
}

func TestViewFailureIsDiagnosedInOneLine(t *testing.T) {
	const site = "acme.atlassian.net"
	denied := acliReply{err: "✗ Error: Issue does not exist or you do not have permission to see it\n", code: 1}
	authOK := func(host string) acliReply {
		return acliReply{out: "✓ Authenticated\n  " + host + "\n  Email: a@b.c\n"}
	}
	const unexpected = "acli gave unexpected output for ABC-1; it may be too old (check 'acli --version' and update)"
	notFound := `acli could not read ABC-1 ("Issue does not exist or you do not have permission to see it"); check the key and your access on acme.atlassian.net`
	for name, tc := range map[string]struct {
		view, status acliReply
		want         string
	}{
		"empty summary":      {acliReply{out: `{"key":"ABC-1","fields":{"summary":"","status":{"statusCategory":{"key":"new"}}}}`}, acliReply{}, unexpected},
		"null":               {acliReply{out: "null"}, acliReply{}, unexpected},
		"no status category": {acliReply{out: `{"key":"ABC-1","fields":{"summary":"x","status":{"name":"Open"}}}`}, acliReply{}, unexpected},
		"no status name":     {acliReply{out: `{"key":"ABC-1","fields":{"summary":"x","status":{"statusCategory":{"key":"done"}}}}`}, acliReply{}, unexpected},
		"help text exit 0":   {acliReply{out: "usage: acli"}, acliReply{}, unexpected},
		"logged out": {denied, acliReply{code: 1},
			"acli is not logged in to Jira; run 'acli jira auth login'"},
		"site mismatch": {denied, authOK("Site: other.atlassian.net"),
			"acli is on other.atlassian.net, Workspace config wants acme.atlassian.net; run 'acli jira auth switch --site acme.atlassian.net'"},
		"site match":   {denied, authOK("Site: " + site), notFound},
		"no Site line": {denied, authOK("Org: x"), notFound},
	} {
		fakeAcli(t, tc.view, tc.status)
		if _, err := View("ABC-1", site); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestViewWithoutAcliOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := View("ABC-1", "acme.atlassian.net")
	if want := "acli not found on PATH; install the Atlassian CLI, then 'acli jira auth login'"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestActiveSite(t *testing.T) {
	for _, c := range []struct {
		name string
		r    acliReply
		want string
		err  error
	}{
		{"site line", acliReply{out: "✓ Authenticated\n  Site: acme.atlassian.net\n"}, "acme.atlassian.net", nil},
		{"no site line", acliReply{out: "✓ Authenticated\n"}, "", nil},
		{"non-zero exit", acliReply{err: "no login", code: 1}, "", ErrNotLoggedIn},
	} {
		fakeAcli(t, acliReply{}, c.r)
		if got, err := ActiveSite(); got != c.want || err != c.err {
			t.Errorf("%s: ActiveSite = %q, %v", c.name, got, err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := ActiveSite(); err != ErrNotFound {
		t.Errorf("missing acli: err = %v", err)
	}
}
