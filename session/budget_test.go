package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/crewdocs"
	"jayulabs.org/mugicrew/luffy"
	"jayulabs.org/mugicrew/nami"
	"jayulabs.org/mugicrew/sanji"
	"jayulabs.org/mugicrew/zoro"
)

type harnessCtx struct{ h contract.Harness }

func (c harnessCtx) Harness() contract.Harness { return c.h }

// Spec D6.3: every HITL kind's real tmux new-session command — the
// crewmate's Launch through the claude backend (preamble, kind prompt, closing
// line, --add-dir), wrapped by paneArgs/launchArgs — stays ≤ 14 KiB, 2 KiB under
// tmux's ~16 KiB "command too long". Long paths and a long title stand in for a
// worst realistic launch.
func TestHITLLaunchByteBudget(t *testing.T) {
	// Zoro's pivot Launch resolves its Checkpoint and Spec through bd.
	bin := t.TempDir()
	fake := "#!/bin/sh\nprintf '%s' '[{\"id\":\"ws-rwch\",\"labels\":[\"kind:checkpoint\"]},{\"id\":\"ws-te9o\",\"labels\":[\"kind:spec\"]}]'\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir, err := crewdocs.Dir()
	if err != nil {
		t.Fatal(err)
	}
	home := "/home/" + strings.Repeat("u", 32)
	longCache := strings.NewReplacer(filepath.Dir(dir), home+"/.cache/mugicrew/crewdocs")
	wt := home + "/src/" + strings.Repeat("w", 40) + "/.git/mugicrew/worktrees/ws-rwch.12"
	title := strings.Repeat("A long bead title naming what the session settles ", 3)
	ctx := contract.WithClosingLine(harnessCtx{claudecode.New("claude-opus-5-5")})

	for _, c := range []struct {
		crew contract.Crewmate
		kind contract.Kind
	}{
		{nami.Nami{}, contract.KindSignal},
		{nami.Nami{}, contract.KindGrilling},
		{zoro.Zoro{}, contract.KindPivot},
		{sanji.Sanji{}, contract.KindPrototype},
		{luffy.Luffy{}, contract.KindApproval},
	} {
		b := bd.Bead{ID: "ws-rwch.12", Parent: "ws-rwch", Kind: c.kind, Title: title}
		lc, err := c.crew.Launch(ctx, contract.Bead{ID: b.ID, Kind: c.kind})
		if err != nil {
			t.Fatalf("%s Launch: %v", c.kind, err)
		}
		ac := lc.(contract.AgentCommand)
		argv := make([]string, len(ac.Argv))
		for i, a := range ac.Argv {
			argv[i] = longCache.Replace(a)
		}
		tags := sessionTags{b, ac.SessionID, wt}
		args := launchArgs(tmuxName(b.ID), wt, home+"/.cache/mugicrew/env-1234567890", paneArgs(home+"/.nix-profile/bin/mugicrew", tags, argv), tags)
		n := len("tmux -S " + home + "/.cache/mugicrew/tmux-sock")
		for _, a := range args {
			n += len(a) + 1
		}
		t.Logf("%s: %d bytes", c.kind, n)
		if n > 14*1024 {
			t.Errorf("%s launch command is %d bytes, budget 14 KiB", c.kind, n)
		}
	}
}
