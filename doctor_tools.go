package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
)

// doctorToolVersion runs `bin args...` and returns its output; a var so tests
// stub it with real version outputs.
var doctorToolVersion = func(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// doctorExe is the running binary's path; a var so tests stub it.
var doctorExe = os.Executable

// prereq is one row of the prerequisite table that feeds doctor's tool lines
// (and install.md's short list). Hints are verbatim from docs/research/
// mugicrew-prerequisite-tool-versions-and-install-commands.md; one too long for a
// line links its install.md anchor.
type prereq struct {
	name, bin    string
	gated        func(root string) bool // nil: core, always checked
	verArgs      []string               // nil: presence only
	field        int                    // which whitespace field of the output is the version
	floor        string                 // FAIL below ("" none)
	warnFrom     string                 // WARN at or above ("" none)
	linux, macos string                 // install hint
	why          string                 // appended to the absent hint
}

func gitlabGate(string) bool {
	s, err := git.LandStrategyName()
	return err == nil && s == git.LandStrategyGitlab
}

func jiraGate(root string) bool {
	site, _ := cfglayer.JiraSite(root)
	return site != ""
}

var prereqs = []prereq{
	{name: "bd", bin: "bd", verArgs: []string{"version"}, field: 2, floor: "1.3.0",
		linux: "brew install beads or npm install -g @beads/bd", macos: "brew install beads"},
	{name: "dolt", bin: "dolt", verArgs: []string{"version"}, field: 2, warnFrom: "2.3.0",
		linux: "pinned 2.2.0 tarball, see docs/install.md#dolt", macos: "pinned 2.2.0 tarball, see docs/install.md#dolt"},
	{name: "git", bin: "git", linux: "apt install git (dnf/yum install git)", macos: "xcode-select --install or brew install git"},
	{name: "tmux", bin: "tmux", verArgs: []string{"-V"}, field: 1, floor: "3.2",
		linux: "apt install tmux (dnf/yum install tmux)", macos: "brew install tmux", why: "; HITL sessions cannot open without it"},
	{name: "claude", bin: harnesses[builtinHarness].binary, verArgs: []string{"--version"}, field: 0, floor: "2.1.78",
		linux: "curl -fsSL https://claude.ai/install.sh | bash", macos: "curl -fsSL https://claude.ai/install.sh | bash",
		why: "; no crewmate session can start without it"},
	{name: "glab", bin: "glab", gated: gitlabGate, linux: "tested 1.107.0; brew install glab", macos: "tested 1.107.0; brew install glab"},
	{name: "acli", bin: "acli", gated: jiraGate, linux: "tested 1.3.39; see docs/install.md#acli", macos: "tested 1.3.39; brew tap atlassian/homebrew-acli && brew install acli"},
}

// verParts reads the leading dotted numbers of s ("3.7c" is [3 7]).
func verParts(s string) []int {
	var p []int
	for _, f := range strings.Split(s, ".") {
		n := 0
		for n < len(f) && f[n] >= '0' && f[n] <= '9' {
			n++
		}
		if n == 0 {
			break
		}
		v, _ := strconv.Atoi(f[:n])
		p = append(p, v)
		if n < len(f) {
			break
		}
	}
	return p
}

// verLess is a < b over their dotted numbers, missing parts counting as 0.
func verLess(a, b string) bool {
	x, y := verParts(a), verParts(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var u, v int
		if i < len(x) {
			u = x[i]
		}
		if i < len(y) {
			v = y[i]
		}
		if u != v {
			return u < v
		}
	}
	return false
}

func (p prereq) install() string {
	if runtime.GOOS == "darwin" {
		return p.macos
	}
	return p.linux
}

// check runs one table row; nil when its feature is not configured.
func (p prereq) check(root string) []result {
	if p.gated != nil && !p.gated(root) {
		return nil
	}
	if _, err := lookPath(p.bin); err != nil {
		hint := "install " + p.install()
		if p.floor != "" {
			hint = "need ≥ " + p.floor + ": " + hint
		}
		return []result{{stFail, p.name, "not on PATH", hint + p.why}}
	}
	if p.verArgs == nil {
		return []result{{stOK, p.name, "on PATH", ""}}
	}
	out, err := doctorToolVersion(p.bin, p.verArgs...)
	f := strings.Fields(out)
	if err != nil || p.field >= len(f) || len(verParts(f[p.field])) == 0 {
		return []result{{stWarn, p.name, "on PATH, version unreadable", "run " + p.bin + " " + strings.Join(p.verArgs, " ")}}
	}
	v := f[p.field]
	switch {
	case p.floor != "" && verLess(v, p.floor):
		return []result{{stFail, p.name, v + " is below " + p.floor, "install " + p.install()}}
	case p.warnFrom != "" && !verLess(v, p.warnFrom):
		return []result{{stWarn, p.name, v + " is outside the measured-clean range (bd pins 2.2.0; 2.3.x regressed)", "install " + p.install()}}
	}
	return []result{{stOK, p.name, v, ""}}
}

// checkTools is the table's rows in order, gated ones only when configured.
func checkTools(root string) []result {
	var rs []result
	for _, p := range prereqs {
		rs = append(rs, p.check(root)...)
	}
	return rs
}

// resolved is p with symlinks followed, or p itself when it cannot be.
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// checkBinary always prints the running binary; WARN when the first mugicrew on
// PATH is another file.
func checkBinary(string) []result {
	exe, err := doctorExe()
	if err != nil {
		return []result{{stWarn, "binary", "cannot resolve the running executable: " + err.Error(), ""}}
	}
	detail := fmt.Sprintf("%s (version %s)", exe, versionString())
	if onPath, err := lookPath("mugicrew"); err == nil && resolved(onPath) != resolved(exe) {
		return []result{{stWarn, "binary", detail, "the first mugicrew on PATH is " + onPath + ", a different file"}}
	}
	return []result{{stOK, "binary", detail, ""}}
}
