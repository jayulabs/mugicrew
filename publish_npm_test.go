package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const publishScript = ".github/publish-npm.sh"

var publishOrder = []string{"mugicrew-linux-x64", "mugicrew-linux-arm64", "mugicrew-darwin-arm64", "mugicrew-darwin-x64", "mugicrew"}

// runPublish drives the publish script with a stub npm on PATH: view answers from the
// published set, publish appends its arguments to a log. It skips where bash or the
// script is absent (the Nix check sandbox's src excludes the workflow tree).
func runPublish(t *testing.T, event string, published []string, extra ...string) (out string, code int, calls []string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if _, err := os.Stat(publishScript); err != nil {
		t.Skip("no " + publishScript + " in this tree")
	}
	script, err := filepath.Abs(publishScript)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	repoDir := dir
	if len(extra) > 0 {
		repoDir = extra[len(extra)-1] // the temp repo the diff runs in
		extra = extra[:len(extra)-1]
	}
	log := filepath.Join(dir, "publish.log")
	set := ""
	for _, p := range published {
		set += "@jayulabs/" + p + "@1.2.3\n"
	}
	setFile := filepath.Join(dir, "published.txt")
	if err := os.WriteFile(setFile, []byte(set), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := fmt.Sprintf(`#!/bin/sh
case "$1" in
view) if grep -qxF "$2" %q; then echo 1.2.3; else exit 1; fi ;;
publish) echo "$*" >> %q ;;
esac
`, setFile, log)
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{script, "dist", "1.2.3", event}, extra...)
	cmd := exec.Command(bash, args...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	b, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if l, err := os.ReadFile(log); err == nil {
		calls = strings.Split(strings.TrimSpace(string(l)), "\n")
	}
	return string(b), code, calls
}

func publishCall(p string) string {
	return "publish dist/jayulabs-" + p + "-1.2.3.tgz --access public"
}

func TestPublishAllPublishedPushFails(t *testing.T) {
	out, code, calls := runPublish(t, "push", publishOrder)
	if code == 0 {
		t.Error("exit 0, want non-zero")
	}
	for _, want := range []string{"::error::", "1.2.3", "docs/release.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if len(calls) != 0 {
		t.Errorf("publish calls %v, want none", calls)
	}
}

func TestPublishAllPublishedDispatchWarns(t *testing.T) {
	out, code, calls := runPublish(t, "workflow_dispatch", publishOrder)
	if code != 0 {
		t.Errorf("exit %d, want 0:\n%s", code, out)
	}
	if !strings.Contains(out, "::warning::") || strings.Contains(out, "::error::") {
		t.Errorf("want only a ::warning:: annotation:\n%s", out)
	}
	if len(calls) != 0 {
		t.Errorf("publish calls %v, want none", calls)
	}
}

func TestPublishNonePublishedPublishesAllInOrder(t *testing.T) {
	out, code, calls := runPublish(t, "push", nil)
	if code != 0 {
		t.Errorf("exit %d, want 0:\n%s", code, out)
	}
	var want []string
	for _, p := range publishOrder {
		want = append(want, publishCall(p))
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("publish calls %v, want %v", calls, want)
	}
	if strings.Contains(out, "::") {
		t.Errorf("unexpected annotation:\n%s", out)
	}
}

func TestPublishSomePublishedPublishesMissingInOrder(t *testing.T) {
	out, code, calls := runPublish(t, "push", []string{"mugicrew-linux-arm64", "mugicrew-darwin-x64"})
	if code != 0 {
		t.Errorf("exit %d, want 0:\n%s", code, out)
	}
	want := []string{publishCall("mugicrew-linux-x64"), publishCall("mugicrew-darwin-arm64"), publishCall("mugicrew")}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("publish calls %v, want %v", calls, want)
	}
	if !strings.Contains(out, "already published, skipping") || strings.Contains(out, "::") {
		t.Errorf("want skip logs and no annotation:\n%s", out)
	}
}

// gitRepo makes a temp repo whose commits give a real diff: base holds a.go and site/x.html;
// each element of changes then makes one commit touching the named files. It returns the
// repo dir, the first commit's SHA and the last's.
func gitRepo(t *testing.T, changes ...[]string) (dir, first, last string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir = t.TempDir()
	git := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	commit := func(files []string, n int) {
		for _, f := range files {
			p := filepath.Join(dir, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(fmt.Sprint(n, f)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		git("add", "-A")
		git("commit", "-q", "-m", fmt.Sprint("c", n))
	}
	commit([]string{"a.go", "site/x.html"}, 0)
	first = git("rev-parse", "HEAD")
	for i, c := range changes {
		commit(c, i+1)
	}
	return dir, first, git("rev-parse", "HEAD")
}

const zeroSHA = "0000000000000000000000000000000000000000"

func TestPublishAllPublishedSiteOnlyPushWarns(t *testing.T) {
	dir, before, sha := gitRepo(t, []string{"site/x.html", "site/new/y.css"})
	out, code, calls := runPublish(t, "push", publishOrder, before, sha, dir)
	if code != 0 {
		t.Errorf("exit %d, want 0:\n%s", code, out)
	}
	if !strings.Contains(out, "::warning::") || strings.Contains(out, "::error::") {
		t.Errorf("want only a ::warning:: annotation:\n%s", out)
	}
	if len(calls) != 0 {
		t.Errorf("publish calls %v, want none", calls)
	}
}

func TestPublishAllPublishedMixedPushFails(t *testing.T) {
	dir, before, sha := gitRepo(t, []string{"site/x.html", "a.go"})
	out, code, calls := runPublish(t, "push", publishOrder, before, sha, dir)
	if code != 1 || !strings.Contains(out, "::error::") {
		t.Errorf("exit %d, want 1 with ::error::\n%s", code, out)
	}
	if len(calls) != 0 {
		t.Errorf("publish calls %v, want none", calls)
	}
}

func TestPublishAllPublishedDocsOnlyPushFails(t *testing.T) {
	dir, before, sha := gitRepo(t, []string{"docs/release.md"})
	out, code, _ := runPublish(t, "push", publishOrder, before, sha, dir)
	if code != 1 || !strings.Contains(out, "::error::") {
		t.Errorf("exit %d, want 1 with ::error::\n%s", code, out)
	}
}

func TestPublishAllPublishedZeroBeforeFails(t *testing.T) {
	dir, _, sha := gitRepo(t, []string{"site/x.html"})
	out, code, _ := runPublish(t, "push", publishOrder, zeroSHA, sha, dir)
	if code != 1 || !strings.Contains(out, "::error::") {
		t.Errorf("exit %d, want 1 with ::error::\n%s", code, out)
	}
}

func TestPublishAllPublishedFailedDiffFails(t *testing.T) {
	dir, _, sha := gitRepo(t, []string{"site/x.html"})
	out, code, _ := runPublish(t, "push", publishOrder, strings.Repeat("1", 40), sha, dir)
	if code != 1 || !strings.Contains(out, "::error::") {
		t.Errorf("exit %d, want 1 with ::error::\n%s", code, out)
	}
}

func TestPublishAllPublishedEmptyDiffFails(t *testing.T) {
	dir, _, sha := gitRepo(t, []string{"site/x.html"})
	out, code, _ := runPublish(t, "push", publishOrder, sha, sha, dir)
	if code != 1 || !strings.Contains(out, "::error::") {
		t.Errorf("exit %d, want 1 with ::error::\n%s", code, out)
	}
}

func TestPublishAllPublishedDispatchWithRangeWarns(t *testing.T) {
	dir, before, sha := gitRepo(t, []string{"a.go"})
	out, code, _ := runPublish(t, "workflow_dispatch", publishOrder, before, sha, dir)
	if code != 0 || !strings.Contains(out, "::warning::") {
		t.Errorf("exit %d, want 0 with ::warning::\n%s", code, out)
	}
}
