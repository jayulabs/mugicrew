package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/term"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
)

// Seams for init's tests: bd and the terminal are never live.
var (
	initBD    = bd.RunIn
	initIsTTY = func() bool { return term.IsTerminal(os.Stdin.Fd()) }
)

const infoBranchKey = "DOLT_REMOTE_INFO_BRANCH"

// runInit is `mugicrew init` (ADR 0243, ADR 0244): bootstrap one human's
// server-mode bd store in this clone. It runs no git write and never commits.
// The fixed order is guard, bd init, .env, bd dolt stop. It returns the exit code.
func runInit(args []string, in io.Reader, w io.Writer) int {
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
		fmt.Fprintf(w, "mugicrew init: the index tracks .beads/ paths: %s\nfix: %s\n", strings.Join(paths, ", "), trackedBeadsFix)
		return 1
	}

	dir, gerr := graphDir(root)
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
		if dir, err = graphDir(root); err != nil {
			fmt.Fprintln(w, "mugicrew init: bd init ran but no Bead Graph resolves:", err)
			return 1
		}
	}

	changed, err := ensureInfoBranchKey(filepath.Join(dir, ".env"))
	if err != nil {
		fmt.Fprintln(w, "mugicrew init:", err)
		return 1
	}
	if changed { // the server reads .env at start: the next bd call restarts it
		if _, err := initBD(root, "dolt", "stop"); err != nil {
			fmt.Fprintln(w, "mugicrew init:", err)
			return 1
		}
	}

	if fresh {
		fmt.Fprintf(w, "initialized a server-mode bd store, prefix %s\n", *prefix)
	} else {
		fmt.Fprintln(w, "a Bead Graph already resolves; the store was left as is")
	}
	fmt.Fprintln(w, "run mugicrew doctor next")
	return 0
}

// ensureInfoBranchKey adds `DOLT_REMOTE_INFO_BRANCH=` to the .env at path only
// when no line sets the key, keeping every other line; it reports whether it wrote.
func ensureInfoBranchKey(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, _, ok := strings.Cut(strings.TrimSpace(l), "="); ok && strings.TrimPrefix(k, "export ") == infoBranchKey {
			return false, nil
		}
	}
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return true, os.WriteFile(path, []byte(s+infoBranchKey+"=\n"), 0o600)
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
