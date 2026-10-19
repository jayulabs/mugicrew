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
func runPublish(t *testing.T, event string, published []string) (out string, code int, calls []string) {
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
	cmd := exec.Command(bash, script, "dist", "1.2.3", event)
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
