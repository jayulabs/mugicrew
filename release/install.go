package release

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"jayulabs.org/mugicrew/selfexe"
)

// Mode is how the running binary was installed. Only NpmGlobal is upgraded in
// place; every other mode prints what the human should run.
type Mode int

const (
	// Unknown: not an npm global install's platform binary, or a check failed.
	Unknown Mode = iota
	// NpmGlobal: <P>/lib/node_modules/@jayulabs/mugicrew/node_modules/@jayulabs/
	// mugicrew-<plat>/bin/mugicrew, with <P>/bin/mugicrew linking to the package's
	// bin/mugicrew.js.
	NpmGlobal
	// NpxTemp: under npm's <cache>/_npx/<hash>/; npx fetches the release itself.
	NpxTemp
	// PnpmGlobal: a .pnpm store dir that is not under a node_modules.
	PnpmGlobal
	// YarnGlobal: the node_modules sits in `yarn global dir`.
	YarnGlobal
	// ProjectLocal: under a node_modules whose project's package.json depends on
	// the package.
	ProjectLocal
)

// Install is the result of classifying the running binary's real path.
type Install struct {
	Mode   Mode
	Exe    string // the resolved binary path
	Prefix string // NpmGlobal: <P>
	Root   string // ProjectLocal: the project root
	PM     string // ProjectLocal: npm | pnpm | yarn
	Dev    bool   // ProjectLocal: the package sits in devDependencies
	Why    string // Unknown: why it didn't match
}

// Classify classifies the running binary from the real path of its own
// executable, never from env vars or npm's configured prefix. A var so the verb
// and the TUI stub it.
var Classify = func() Install {
	exe, err := selfexe.Path()
	if err != nil {
		return Install{Why: "cannot find the running binary: " + err.Error()}
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return classifyExe(exe, platform())
}

// platform maps GOOS/GOARCH to npm's platform-package suffix ("" when none).
func platform() string {
	os := map[string]string{"linux": "linux", "darwin": "darwin"}[runtime.GOOS]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if os == "" || arch == "" {
		return ""
	}
	return os + "-" + arch
}

// YarnGlobalDir returns `yarn global dir`, the only subprocess classification
// runs. A var so tests stub it; an error means the yarn rule doesn't match.
var YarnGlobalDir = func() (string, error) {
	out, err := exec.Command("yarn", "global", "dir").Output()
	return strings.TrimSpace(string(out)), err
}

func classifyExe(exe, plat string) Install {
	in := Install{Exe: exe}
	if plat == "" {
		in.Why = "no npm platform package for " + runtime.GOOS + "/" + runtime.GOARCH
		return in
	}
	sep := string(filepath.Separator)
	pkgDir := filepath.Join("@jayulabs", "mugicrew-"+plat)
	nmTail := sep + filepath.Join("node_modules", pkgDir, "bin", "mugicrew")
	if !strings.HasSuffix(exe, nmTail) {
		in.Why = "the path is not under node_modules/@jayulabs/mugicrew-" + plat + "/bin/mugicrew"
		return in
	}
	nm := strings.TrimSuffix(exe, sep+filepath.Join(pkgDir, "bin", "mugicrew")) // the node_modules dir
	if strings.Contains(exe, sep+"_npx"+sep) {
		in.Mode = NpxTemp
		return in
	}
	pkg := filepath.Join("lib", "node_modules", "@jayulabs", "mugicrew")
	tail := sep + filepath.Join(pkg, "node_modules", pkgDir, "bin", "mugicrew")
	if strings.HasSuffix(exe, tail) {
		prefix := strings.TrimSuffix(exe, tail)
		if prefix == "" {
			prefix = sep
		}
		want := filepath.Join(prefix, pkg, "bin", "mugicrew.js")
		if got, err := filepath.EvalSymlinks(filepath.Join(prefix, "bin", "mugicrew")); err == nil && got == want {
			in.Mode, in.Prefix = NpmGlobal, prefix
			return in
		}
	}
	pnpm := strings.Contains(exe, sep+".pnpm"+sep+"@jayulabs+mugicrew-"+plat+"@")
	if pnpm {
		store := exe[:strings.Index(exe, sep+".pnpm"+sep)]
		if filepath.Base(store) != "node_modules" {
			in.Mode = PnpmGlobal
			return in
		}
	}
	if dir, err := YarnGlobalDir(); err == nil && dir != "" {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		if filepath.Dir(nm) == dir {
			in.Mode = YarnGlobal
			return in
		}
	}
	for d := nm; ; d = filepath.Dir(d) {
		if filepath.Base(d) == "node_modules" {
			root := filepath.Dir(d)
			if dev, ok := dependsOnPkg(filepath.Join(root, "package.json")); ok {
				in.Mode, in.Root, in.Dev = ProjectLocal, root, dev
				in.PM = lockfilePM(root, pnpm)
				return in
			}
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	in.Why = "no enclosing package.json lists " + pkgName + " in dependencies or devDependencies"
	return in
}

// dependsOnPkg reports whether the package.json at path names pkgName, and if so
// whether only as a devDependency.
func dependsOnPkg(path string) (dev, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var p struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false, false
	}
	if _, ok := p.Dependencies[pkgName]; ok {
		return false, true
	}
	_, ok = p.DevDependencies[pkgName]
	return ok, ok
}

// lockfilePM names the package manager that owns root: pnpm when the path went
// through .pnpm, else by lockfile, defaulting to npm.
func lockfilePM(root string, pnpm bool) string {
	if pnpm {
		return "pnpm"
	}
	for _, l := range []struct{ file, pm string }{{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"package-lock.json", "npm"}} {
		if _, err := os.Stat(filepath.Join(root, l.file)); err == nil {
			return l.pm
		}
	}
	return "npm"
}

// Action is an upgrade plan: a command to run, or text to print.
type Action struct {
	Run  []string // argv to run; nil for a print plan
	Text string   // print plan: what to tell the human
	Fail bool     // a print plan that exits non-zero (Unknown)
}

const pkgName = "@jayulabs/mugicrew"

// Plan is side-effect free: the command that moves install onto latest, or the
// text that tells the human what to do.
func Plan(in Install, latest string) Action {
	spec := pkgName + "@" + latest
	switch in.Mode {
	case NpmGlobal:
		return Action{Run: []string{"npm", "i", "-g", "--prefix", in.Prefix, spec}}
	case NpxTemp:
		return Action{Text: "mugicrew: running via npx; nothing to upgrade, npx fetches the release itself\n"}
	case PnpmGlobal:
		return Action{Text: "mugicrew: this is a pnpm global install; nothing changed\nTo upgrade, run:\n  pnpm add -g " + spec + "\n"}
	case YarnGlobal:
		return Action{Text: "mugicrew: this is a yarn global install; nothing changed\nTo upgrade, run:\n  yarn global add " + spec + "\n"}
	case ProjectLocal:
		cmd := map[string]string{"npm": "npm i", "pnpm": "pnpm add", "yarn": "yarn add"}[in.PM]
		if in.Dev {
			cmd += " -D"
		}
		return Action{Text: fmt.Sprintf("mugicrew: this is a project-local install; nothing changed\nTo upgrade, run in %s:\n  %s %s\nThen land the package.json and lockfile change.\n", in.Root, cmd, spec)}
	}
	return Action{Fail: true, Text: fmt.Sprintf(`mugicrew: cannot tell how this mugicrew was installed; nothing changed
  binary: %s
  why:    %s
To upgrade yourself, run one of:
  npm i -g %s@%s       (a global install)
  npm i -D %s@%s       (a project-local install, in the project root)
`, in.Exe, in.Why, pkgName, latest, pkgName, latest)}
}

// RunNpm runs argv with output streamed and stdin from the terminal, returning
// its exit code. A var so tests stub it.
var RunNpm = stream

// RunDoctor runs the binary at exe with doctor, streaming its output and
// returning its exit code. A var so tests stub it.
var RunDoctor = func(exe string) int { return stream([]string{exe, "doctor"}) }

// RunCaptured runs argv with its output held back, returning it with the exit
// code: the TUI's seam, where streaming would draw over the screen. A var so
// tests stub it.
var RunCaptured = func(argv []string) (string, int) {
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() > 0 {
		return string(out), ee.ExitCode()
	}
	return string(out) + err.Error() + "\n", 1
}

var fixItem = regexp.MustCompile(`(?m)^\d+\. `)

// FixCount is the number of To-fix items in a doctor report.
func FixCount(report string) int {
	_, fix, ok := strings.Cut(report, "To fix")
	if !ok {
		return 0
	}
	return len(fixItem.FindAllString(fix, -1))
}

func stream(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() > 0 {
		return ee.ExitCode()
	}
	fmt.Fprintln(os.Stderr, "mugicrew:", err)
	return 1
}
