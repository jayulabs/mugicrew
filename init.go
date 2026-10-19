package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/term"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// Seams for the Forge: glab is never live in tests.
var (
	forgeProject  = git.ForgeProject
	forgeUserName = forge.CurrentUser
)

// Seams for init's tests: bd and the terminal are never live.
var (
	initBD    = bd.RunIn
	initIsTTY = func() bool { return term.IsTerminal(os.Stdin.Fd()) }
)

// runInit is `mugicrew init` (ADR 0243, ADR 0244): bootstrap one human's
// server-mode bd store in this clone. It runs no git write and never commits.
// The fixed order is guard, bd init. It returns the exit code.
func runInit(args []string, in io.Reader, w io.Writer) int {
	if len(args) > 0 && args[0] == "workspace" {
		return runInitWorkspace(args[1:], in, w)
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(w)
	prefix := fs.String("prefix", "", "issue prefix (skips the prompt)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(w, "usage: mugicrew init [--prefix <p>]")
		return 1
	}
	root, err := git.RepoRoot()
	if err != nil {
		fmt.Fprintln(w, "mugicrew init:", err)
		return 1
	}
	if paths, err := trackedBeads(root); err != nil {
		fmt.Fprintln(w, "mugicrew init: cannot list tracked paths:", err)
		return 1
	} else if len(paths) > 0 {
		s := styleFor(w)
		r := trackedBeadsResult(paths)
		fmt.Fprintf(w, "mugicrew init: the index tracks .beads/ paths\n%s\n\n%s\n", s.rollCall(r), s.toFixItem(1, r))
		return 1
	}

	// only checks that a graph resolves; the dir is unused
	_, gerr := graphDir(root)
	fresh := gerr != nil
	if !fresh && *prefix != "" { // a half-moved store resolves too: bd init would be skipped silently
		if got, err := initBD(root, "config", "get", "issue_prefix"); err != nil || got != *prefix {
			fmt.Fprintf(w, "mugicrew init: a Bead Graph already resolves with issue prefix %q (read error: %v), not %q; to rebuild a half-moved store see README \"Bd store mode\": bd init --server --stealth --prefix %s, then mugicrew init\n", got, err, *prefix, *prefix)
			return 1
		}
	}
	if fresh {
		if *prefix == "" {
			if !initIsTTY() {
				fmt.Fprintln(w, "mugicrew init: no terminal to prompt on; pass --prefix <p>")
				return 1
			}
			*prefix = promptPrefix(in, w, glabPrefixDefault())
		}
		if *prefix == "" {
			fmt.Fprintln(w, "mugicrew init: an issue prefix is required; pass --prefix <p>")
			return 1
		}
		if _, err := initBD(root, "init", "--server", "--non-interactive", "--stealth", "--prefix", *prefix); err != nil {
			fmt.Fprintln(w, "mugicrew init:", err)
			return 1
		}
		if _, err := graphDir(root); err != nil {
			fmt.Fprintln(w, "mugicrew init: bd init ran but no Bead Graph resolves:", err)
			return 1
		}
	}

	if fresh {
		fmt.Fprintf(w, "initialized a server-mode bd store, prefix %s\n", *prefix)
	} else {
		fmt.Fprintln(w, "a Bead Graph already resolves; the store was left as is")
	}
	if set, _ := cfglayer.WorkspaceSetUp(root); !set {
		fmt.Fprintln(w, "this Workspace is not set up: the first human runs mugicrew init workspace (docs/install.md#first-human-in-a-workspace)")
	}
	fmt.Fprintln(w, "run mugicrew doctor next")
	return 0
}

func promptPrefix(in io.Reader, w io.Writer, def string) string {
	if def != "" {
		fmt.Fprintf(w, "issue prefix [%s]: ", def)
	} else {
		fmt.Fprint(w, "issue prefix: ")
	}
	line, _ := bufio.NewReader(in).ReadString('\n')
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return def
}

var notPrefixChar = regexp.MustCompile(`[^a-z0-9]+`)

// glabPrefixDefault is the glab username for the Forge host, lowercased and
// reduced to letters, digits and hyphens; "" when glab reports none.
func glabPrefixDefault() string {
	p, err := forgeProject()
	if err != nil {
		return ""
	}
	u, err := forgeUserName(p)
	if err != nil {
		return ""
	}
	return strings.Trim(notPrefixChar.ReplaceAllString(strings.ToLower(u), "-"), "-")
}
