package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitSpy puts a git on PATH that logs "<argv>|<GIT_CONFIG_*>" per call, then runs
// the real git. It returns the log's path.
func gitSpy(t *testing.T) (logPath string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath = filepath.Join(dir, "env.log")
	script := "#!/bin/sh\necho \"$*|${GIT_CONFIG_COUNT-}|${GIT_CONFIG_KEY_0-}|${GIT_CONFIG_VALUE_0-}|${GIT_CONFIG_KEY_1-}\" >> " + logPath + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return logPath
}

// spied returns the env part of every logged call whose argv contains sub.
func spied(t *testing.T, logPath, sub string) (envs []string) {
	t.Helper()
	b, _ := os.ReadFile(logPath)
	for _, l := range strings.Split(string(b), "\n") {
		if argv, env, ok := strings.Cut(l, "|"); ok && strings.Contains(" "+argv+" ", " "+sub+" ") {
			envs = append(envs, env)
		}
	}
	return envs
}

func wantEnv(t *testing.T, logPath, sub, host string) {
	t.Helper()
	want := "1|credential.https://" + host + ".helper|!glab auth git-credential|"
	if host == "" {
		want = "|||"
	}
	envs := spied(t, logPath, sub)
	if len(envs) == 0 {
		t.Errorf("no %s call logged", sub)
	}
	for _, e := range envs {
		if e != want {
			t.Errorf("%s env = %q, want %q", sub, e, want)
		}
	}
}

// httpsRemote respells r's origin as an https URL on host, still rewritten to bare.
func httpsRemote(t *testing.T, r Repo, bare, host, path string) {
	t.Helper()
	url := "https://" + host + "/" + path + ".git"
	for _, a := range [][]string{{"remote", "set-url", "origin", url}, {"config", "url." + bare + ".insteadOf", url}} {
		if _, err := r.run(a...); err != nil {
			t.Fatal(err)
		}
	}
}

// Every network call to an https Forge remote carries glab's helper for that
// remote's host; a local call carries none (Spec FR 2-4, 6, 7, 11).
func TestForgeCallsCarryGlabHelper(t *testing.T) {
	bare, _ := gitlabForge(t)
	httpsRemote(t, "", bare, "gl.example.com", "ns/p")
	mustGit(t, "push", bare, "main")
	mustGit(t, "branch", "chart/x")
	spy := gitSpy(t)

	if _, err := Land("chart/x", "Chart: x", landGitlab); err == nil {
		t.Fatal("want a pending Landing")
	}
	if _, err := FetchMainline("origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := ForgeHasRef("refs/heads/chart/x"); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"fetch", "ls-remote", "push"} {
		wantEnv(t, spy, sub, "gl.example.com")
	}
	wantEnv(t, spy, "rev-parse", "")
}

// The lineage Land's MR-branch push carries the helper too.
func TestLandLineageGitlabPushCarriesGlabHelper(t *testing.T) {
	bare, _, _ := lineageGitlab(t)
	httpsRemote(t, "", bare, "gl.example.com", "ns/p")
	spy := gitSpy(t)
	if _, err := LandLineage("lin"); err != nil {
		t.Fatal(err)
	}
	wantEnv(t, spy, "push", "gl.example.com")
	wantEnv(t, spy, "fetch", "gl.example.com")
}

// A child's calls get its own Forge host, not the Workspace's.
func TestChildForgeCallsCarryTheChildsHost(t *testing.T) {
	child, wsBare, childBare := wiredChild(t)
	httpsRemote(t, "", wsBare, "ws.example.com", "ns/p")
	httpsRemote(t, child, childBare, "child.example.com", "ns/child")
	child.run("push", childBare, "main")
	spy := gitSpy(t)
	if _, err := child.FetchMainline(); err != nil {
		t.Fatal(err)
	}
	wantEnv(t, spy, "fetch", "child.example.com")
}

// An ssh remote gets no entries.
func TestSSHForgeCallsCarryNoHelper(t *testing.T) {
	bare, _ := gitlabForge(t)
	mustGit(t, "push", bare, "main")
	spy := gitSpy(t)
	if _, err := FetchMainline("origin"); err != nil {
		t.Fatal(err)
	}
	wantEnv(t, spy, "fetch", "")
}
