package session

import (
	"os/exec"
	"slices"
	"strings"
)

// chords are the prefix2 spellings tried in order (ADR 0321). Only these are
// ever written or compared, never C-@: on tmux 3.5+ it is not the Ctrl-Space byte.
var chords = []string{"C-a", "C-Space"}

// nestedIn reports whether a launcher with $TMUX tmuxEnv is nested in another
// tmux: it has one that does not name the private server on sock (ADR 0321).
func nestedIn(tmuxEnv, sock string) bool {
	return tmuxEnv != "" && !insideOf(tmuxEnv, sock)
}

// chooseChord is the first of chords that neither the outer server (prefix,
// prefix2; "" when unknown) nor the private server's prefix table (bound) holds,
// or "" for none. A private prefix2 other than None is already set: no chord.
func chooseChord(outerPrefix, outerPrefix2, privatePrefix2 string, bound []string) string {
	if privatePrefix2 != "None" {
		return ""
	}
	for _, k := range chords {
		if k != outerPrefix && k != outerPrefix2 && !slices.Contains(bound, k) {
			return k
		}
	}
	return ""
}

// parsePrefixKeys is the keys of `list-keys -T prefix` output: the token after
// -T prefix on each line, since -r may come first. A missing table reads as none.
func parsePrefixKeys(out string) []string {
	var keys []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if i := slices.Index(f, "-T"); i >= 0 && i+2 < len(f) && f[i+1] == "prefix" {
			keys = append(keys, f[i+2])
		}
	}
	return keys
}

// outerOption is the outer server's global option name, "" when unknown: a plain
// tmux that keeps the launcher's $TMUX and passes no -S, so it is not tmuxCmd.
func outerOption(name string) string {
	out, err := exec.Command("tmux", "show", "-gv", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// setNestedPrefix2 gives the private server on sock a second prefix when the
// launcher is nested (ADR 0321): the chosen chord as server-wide prefix2, bound to
// send-prefix -2. Best effort and silent: any error leaves the launch to go on.
func setNestedPrefix2(sock string) {
	cur, err := tmuxCmd(sock, "show", "-gv", "prefix2").Output()
	if err != nil || strings.TrimSpace(string(cur)) != "None" {
		return
	}
	// list-keys exits 1 when the table doesn't exist: nothing is bound.
	keys, _ := tmuxCmd(sock, "list-keys", "-T", "prefix").Output()
	k := chooseChord(outerOption("prefix"), outerOption("prefix2"), "None", parsePrefixKeys(string(keys)))
	if k == "" {
		return
	}
	tmuxCmd(sock, "set", "-g", "prefix2", k, ";", "bind", "-T", "prefix", k, "send-prefix", "-2").Run()
}
