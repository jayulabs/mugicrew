package bd

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/git"
)

func TestDoltGitURL(t *testing.T) {
	for in, want := range map[string]string{
		"git@gitlab.com:ns/p.git":            "git+ssh://git@gitlab.com/ns/p.git",
		"ssh://git@gitlab.com/ns/p.git":      "git+ssh://git@gitlab.com/ns/p.git",
		"https://gitlab.com/ns/p.git":        "git+https://gitlab.com/ns/p.git",
		"git+https://gitlab.com/ns/p.git":    "git+https://gitlab.com/ns/p.git",
		"ssh://git@gitlab.com:2222/ns/p.git": "git+ssh://git@gitlab.com:2222/ns/p.git",
	} {
		if got := DoltGitURL(in); got != want {
			t.Errorf("DoltGitURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeRefForge fakes bd's CLI and the Forge for the graph ref: remote is the
// dolt_remotes row ("" none), remoteRoot the fetched root ("" no ref on the Forge).
type fakeRefForge struct {
	remote, remoteRoot string
	info               bool
	calls              [][]string
	envs               [][]string     // env of each call, parallel to calls (ls-remote included)
	fail               map[string]int // substring of a call → how many times it fails
	forgeURL           string
}

func (f *fakeRefForge) install(t *testing.T) {
	t.Helper()
	orun, oenv, ourl, ohas := run, runEnv, forgeURL, forgeHasRef
	t.Cleanup(func() { run, runEnv, forgeURL, forgeHasRef = orun, oenv, ourl, ohas })
	if f.forgeURL == "" {
		f.forgeURL = "git@gitlab.com:ns/p.git"
	}
	forgeURL = func() (string, error) { return f.forgeURL, nil }
	forgeHasRef = func(ref string, env []string) (bool, error) {
		f.calls, f.envs = append(f.calls, []string{"ls-remote", ref}), append(f.envs, env)
		if ref == DoltInfoRef {
			return f.info, nil
		}
		return f.remoteRoot != "", nil
	}
	run = func(args ...string) (string, error) { return runEnv(nil, args...) }
	runEnv = func(env []string, args ...string) (string, error) {
		f.calls, f.envs = append(f.calls, args), append(f.envs, env)
		q := args[len(args)-1]
		for sub, n := range f.fail {
			if n > 0 && strings.Contains(strings.Join(args, " "), sub) {
				f.fail[sub]--
				return "", errors.New("auth: " + sub)
			}
		}
		switch {
		case strings.Contains(q, "FROM dolt_remotes"):
			return "url,ref\n" + f.remote, nil
		case strings.Contains(q, "dolt_log('remotes/"):
			return "commit_hash\n" + f.remoteRoot, nil
		}
		return "", nil
	}
}

func (f *fakeRefForge) did(sub ...string) bool {
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), strings.Join(sub, " ")) {
			return true
		}
	}
	return false
}

const wantURL = "git+ssh://git@gitlab.com/ns/p.git"

func TestEnsureGraphRemote(t *testing.T) {
	ref := "refs/mugicrew/graphs/ann"
	for name, c := range map[string]struct {
		remote  string
		add, rm bool
	}{
		"absent":      {"", true, false},
		"same":        {wantURL + "," + ref, false, false},
		"renamed ref": {wantURL + ",refs/mugicrew/graphs/old", true, true},
		"new forge":   {"git+ssh://git@other/x.git," + ref, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeRefForge{remote: c.remote}
			f.install(t)
			if err := ensureGraphRemote(ref, "git@gitlab.com:ns/p.git", nil); err != nil {
				t.Fatal(err)
			}
			if f.did("DOLT_REMOTE('add','forge-graph','"+wantURL+"','--ref','"+ref+"')") != c.add {
				t.Errorf("add = %v, want %v: %v", !c.add, c.add, f.calls)
			}
			if f.did("DOLT_REMOTE('remove','forge-graph')") != c.rm {
				t.Errorf("remove want %v: %v", c.rm, f.calls)
			}
		})
	}
}

func TestGraphPush(t *testing.T) {
	defer git.SetGraphID("ours")()
	pushed := func(f *fakeRefForge) bool { return f.did("dolt push --remote forge-graph --force") }

	t.Run("no remote ref pushes", func(t *testing.T) {
		f := &fakeRefForge{}
		f.install(t)
		r, err := GraphPush("ann")
		if err != nil || !pushed(f) || r.Ref != "refs/mugicrew/graphs/ann" || r.Warning != "" {
			t.Fatalf("%+v %v %v", r, err, f.calls)
		}
		if f.did("DOLT_FETCH") {
			t.Error("fetched a ref that is not there")
		}
	})
	t.Run("same root force pushes", func(t *testing.T) {
		f := &fakeRefForge{remoteRoot: "ours"}
		f.install(t)
		if _, err := GraphPush("ann"); err != nil || !pushed(f) {
			t.Fatalf("%v %v", err, f.calls)
		}
	})
	t.Run("foreign root refuses without pushing", func(t *testing.T) {
		f := &fakeRefForge{remoteRoot: "theirs"}
		f.install(t)
		_, err := GraphPush("ann")
		var fg *ForeignGraphRef
		if !errors.As(err, &fg) || fg.Ours != "ours" || fg.Theirs != "theirs" || fg.Ref != "refs/mugicrew/graphs/ann" {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(err.Error(), "ours") || !strings.Contains(err.Error(), "theirs") {
			t.Errorf("message names neither id: %v", err)
		}
		if pushed(f) || f.did("dolt push") {
			t.Errorf("pushed anyway: %v", f.calls)
		}
	})
	t.Run("dolt_remote_info warns and still pushes", func(t *testing.T) {
		f := &fakeRefForge{info: true}
		f.install(t)
		r, err := GraphPush("ann")
		if err != nil || !pushed(f) || !strings.Contains(r.Warning, "DOLT_REMOTE_INFO_BRANCH") {
			t.Fatalf("%+v %v", r, err)
		}
	})
}

const helperEnv = "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=credential.https://gitlab.com.helper GIT_CONFIG_VALUE_0=!glab auth git-credential"

func (f *fakeRefForge) count(sub string) (n int) {
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), sub) {
			n++
		}
	}
	return n
}

func TestGraphPushInjectsHelperIntoEveryCall(t *testing.T) {
	defer git.SetGraphID("ours")()
	f := &fakeRefForge{remoteRoot: "ours", info: true, forgeURL: "https://gitlab.com/ns/p.git"}
	f.install(t)
	if _, err := GraphPush("ann"); err != nil {
		t.Fatal(err)
	}
	for i, env := range f.envs {
		if got := strings.Join(env, " "); got != helperEnv {
			t.Errorf("call %v env = %q, want %q", f.calls[i], got, helperEnv)
		}
	}
	if !f.did("ls-remote") || !f.did("DOLT_FETCH") || !f.did("DOLT_REMOTE") || !f.did("dolt push") {
		t.Errorf("legs missing: %v", f.calls)
	}
}

func TestGraphPushRestartsServerOnceOnFetchOrPushFailure(t *testing.T) {
	defer git.SetGraphID("ours")()
	for _, leg := range []string{"DOLT_FETCH", "dolt push"} {
		t.Run(leg, func(t *testing.T) {
			f := &fakeRefForge{remoteRoot: "ours", forgeURL: "https://gitlab.com/ns/p.git", fail: map[string]int{leg: 1}}
			f.install(t)
			if _, err := GraphPush("ann"); err != nil {
				t.Fatal(err)
			}
			if f.count("dolt stop") != 1 || f.count("dolt start") != 1 || f.count("dolt push") != 1+btoi(leg == "dolt push") {
				t.Errorf("calls: %v", f.calls)
			}
			for i, c := range f.calls {
				if strings.Contains(strings.Join(c, " "), "dolt start") && strings.Join(f.envs[i], " ") != helperEnv {
					t.Errorf("dolt start not injected: %v", f.envs[i])
				}
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestGraphPushSecondFailureIsReturned(t *testing.T) {
	defer git.SetGraphID("ours")()
	f := &fakeRefForge{remoteRoot: "ours", forgeURL: "https://gitlab.com/ns/p.git", fail: map[string]int{"DOLT_FETCH": 2}}
	f.install(t)
	if _, err := GraphPush("ann"); err == nil || !strings.Contains(err.Error(), "DOLT_FETCH") {
		t.Fatalf("err = %v", err)
	}
	if f.count("dolt stop") != 1 || f.count("DOLT_FETCH") != 2 {
		t.Errorf("calls: %v", f.calls)
	}
}

func TestGraphPushForeignRefDoesNotRestart(t *testing.T) {
	defer git.SetGraphID("ours")()
	f := &fakeRefForge{remoteRoot: "theirs", forgeURL: "https://gitlab.com/ns/p.git"}
	f.install(t)
	var foreign *ForeignGraphRef
	if _, err := GraphPush("ann"); !errors.As(err, &foreign) {
		t.Fatalf("err = %v, want *ForeignGraphRef", err)
	}
	if f.did("dolt stop") {
		t.Errorf("restarted: %v", f.calls)
	}
}

func TestGraphPushSSHForgeGetsNoEnvAndNoRestart(t *testing.T) {
	defer git.SetGraphID("ours")()
	f := &fakeRefForge{remoteRoot: "ours", fail: map[string]int{"DOLT_FETCH": 1}}
	f.install(t)
	if _, err := GraphPush("ann"); err == nil {
		t.Fatal("want the fetch failure")
	}
	for _, env := range f.envs {
		if env != nil {
			t.Errorf("env on ssh forge: %v", env)
		}
	}
	if f.did("dolt stop") {
		t.Errorf("restarted: %v", f.calls)
	}
}
