package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/selfexe"
)

func mkNpmGlobal(t *testing.T) (prefix, exe string) {
	t.Helper()
	prefix, _ = filepath.EvalSymlinks(t.TempDir())
	pkg := filepath.Join(prefix, "lib", "node_modules", "@jayulabs", "mugicrew")
	exe = filepath.Join(pkg, "node_modules", "@jayulabs", "mugicrew-"+"linux-x64", "bin", "mugicrew")
	for _, d := range []string{filepath.Dir(exe), filepath.Join(pkg, "bin"), filepath.Join(prefix, "bin")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(exe, nil, 0o755)
	os.WriteFile(filepath.Join(pkg, "bin", "mugicrew.js"), nil, 0o755)
	os.Symlink(filepath.Join("..", "lib", "node_modules", "@jayulabs", "mugicrew", "bin", "mugicrew.js"), filepath.Join(prefix, "bin", "mugicrew"))
	return prefix, exe
}

func TestClassifyNpmGlobal(t *testing.T) {
	stubYarn(t, "", errors.New("no yarn"))
	prefix, exe := mkNpmGlobal(t)
	in := classifyExe(exe, "linux-x64")
	if in.Mode != NpmGlobal || in.Prefix != prefix || in.Exe != exe {
		t.Errorf("got %+v; want NpmGlobal with Prefix %s", in, prefix)
	}
}

func TestClassifyUnknown(t *testing.T) {
	stubYarn(t, "", errors.New("no yarn"))
	prefix, exe := mkNpmGlobal(t)
	os.Remove(filepath.Join(prefix, "bin", "mugicrew"))
	os.Symlink(exe, filepath.Join(prefix, "bin", "mugicrew")) // hand-made link to the binary
	outside := filepath.Join(t.TempDir(), "bin", "mugicrew")
	for name, in := range map[string]Install{
		"hand-made symlink": classifyExe(exe, "linux-x64"),
		"outside":           classifyExe(outside, "linux-x64"),
		"no platform":       classifyExe(exe, ""),
	} {
		if in.Mode != Unknown || in.Why == "" || in.Prefix != "" {
			t.Errorf("%s: got %+v; want Unknown with a Why", name, in)
		}
	}
}

func TestPlan(t *testing.T) {
	g := Plan(Install{Mode: NpmGlobal, Prefix: "/opt/np"}, "1.2.3")
	if strings.Join(g.Run, " ") != "npm i -g --prefix /opt/np @jayulabs/mugicrew@1.2.3" || g.Fail || g.Text != "" {
		t.Errorf("NpmGlobal: %+v", g)
	}
	u := Plan(Install{Exe: "/x/mugicrew", Why: "because"}, "1.2.3")
	want := `mugicrew: cannot tell how this mugicrew was installed; nothing changed
  binary: /x/mugicrew
  why:    because
To upgrade yourself, run one of:
  npm i -g @jayulabs/mugicrew@1.2.3       (a global install)
  npm i -D @jayulabs/mugicrew@1.2.3       (a project-local install, in the project root)
`
	if u.Run != nil || !u.Fail || u.Text != want {
		t.Errorf("Unknown: %+v", u)
	}
}

func stubYarn(t *testing.T, dir string, err error) {
	old := YarnGlobalDir
	YarnGlobalDir = func() (string, error) { return dir, err }
	t.Cleanup(func() { YarnGlobalDir = old })
}

func mkExe(t *testing.T, root string, parts ...string) string {
	t.Helper()
	exe := filepath.Join(append([]string{root}, parts...)...)
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, nil, 0o755)
	return exe
}

func mkProject(t *testing.T, root, field, lock string) {
	t.Helper()
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"`+field+`":{"@jayulabs/mugicrew":"^1.0.0"}}`), 0o644)
	if lock != "" {
		os.WriteFile(filepath.Join(root, lock), nil, 0o644)
	}
}

func TestClassifyLayouts(t *testing.T) {
	const plat = "linux-x64"
	pk := filepath.Join("@jayulabs", "mugicrew-"+plat, "bin", "mugicrew")
	tmp := func() string { d, _ := filepath.EvalSymlinks(t.TempDir()); return d }
	cases := []struct {
		name  string
		setup func() (exe, root, yarnDir string)
		mode  Mode
		pm    string
		dev   bool
	}{
		{"npm local hoisted", func() (string, string, string) {
			r := tmp()
			mkProject(t, r, "devDependencies", "package-lock.json")
			return mkExe(t, r, "node_modules", pk), r, ""
		}, ProjectLocal, "npm", true},
		{"npm local in dir named lib", func() (string, string, string) {
			r := filepath.Join(tmp(), "lib")
			mkProject(t, r, "dependencies", "")
			return mkExe(t, r, "node_modules", pk), r, ""
		}, ProjectLocal, "npm", false},
		{"npx", func() (string, string, string) {
			return mkExe(t, tmp(), "_npx", "e4c31b0c06bb25ca", "node_modules", pk), "", ""
		}, NpxTemp, "", false},
		{"pnpm local", func() (string, string, string) {
			r := tmp()
			mkProject(t, r, "devDependencies", "pnpm-lock.yaml")
			return mkExe(t, r, "node_modules", ".pnpm", "@jayulabs+mugicrew-"+plat+"@1.0.0", "node_modules", pk), r, ""
		}, ProjectLocal, "pnpm", true},
		{"pnpm global", func() (string, string, string) {
			r := tmp()
			mkProject(t, filepath.Join(r, "global", "5"), "dependencies", "pnpm-lock.yaml")
			return mkExe(t, r, "global", "5", ".pnpm", "@jayulabs+mugicrew-"+plat+"@1.0.0", "node_modules", pk), "", ""
		}, PnpmGlobal, "", false},
		{"yarn local", func() (string, string, string) {
			r := tmp()
			mkProject(t, r, "devDependencies", "yarn.lock")
			return mkExe(t, r, "node_modules", pk), r, ""
		}, ProjectLocal, "yarn", true},
		{"yarn global", func() (string, string, string) {
			r := filepath.Join(tmp(), "yglobal")
			mkProject(t, r, "dependencies", "yarn.lock")
			return mkExe(t, r, "node_modules", pk), "", r
		}, YarnGlobal, "", false},
		{"yarn absent falls through", func() (string, string, string) {
			r := filepath.Join(tmp(), "yglobal")
			mkProject(t, r, "dependencies", "yarn.lock")
			return mkExe(t, r, "node_modules", pk), r, ""
		}, ProjectLocal, "yarn", false},
	}
	for _, c := range cases {
		exe, root, yarnDir := c.setup()
		if yarnDir != "" {
			stubYarn(t, yarnDir, nil)
		} else {
			stubYarn(t, "", errors.New("no yarn"))
		}
		in := classifyExe(exe, plat)
		if in.Mode != c.mode || in.Root != root || in.PM != c.pm || in.Dev != c.dev {
			t.Errorf("%s: got %+v; want mode %d root %s pm %s dev %v", c.name, in, c.mode, root, c.pm, c.dev)
		}
	}
}

func TestPlanPrintModes(t *testing.T) {
	for _, c := range []struct {
		in   Install
		want string
	}{
		{Install{Mode: NpxTemp}, "mugicrew: running via npx; nothing to upgrade, npx fetches the release itself\n"},
		{Install{Mode: PnpmGlobal}, "mugicrew: this is a pnpm global install; nothing changed\nTo upgrade, run:\n  pnpm add -g @jayulabs/mugicrew@2.0.0\n"},
		{Install{Mode: YarnGlobal}, "mugicrew: this is a yarn global install; nothing changed\nTo upgrade, run:\n  yarn global add @jayulabs/mugicrew@2.0.0\n"},
	} {
		if a := Plan(c.in, "2.0.0"); a.Run != nil || a.Fail || a.Text != c.want {
			t.Errorf("%v: %+v", c.in.Mode, a)
		}
	}
	for _, c := range []struct {
		pm  string
		dev bool
		cmd string
	}{
		{"npm", false, "npm i"}, {"npm", true, "npm i -D"},
		{"pnpm", false, "pnpm add"}, {"pnpm", true, "pnpm add -D"},
		{"yarn", false, "yarn add"}, {"yarn", true, "yarn add -D"},
	} {
		want := "mugicrew: this is a project-local install; nothing changed\nTo upgrade, run in /proj:\n  " + c.cmd + " @jayulabs/mugicrew@2.0.0\nThen land the package.json and lockfile change.\n"
		a := Plan(Install{Mode: ProjectLocal, Root: "/proj", PM: c.pm, Dev: c.dev}, "2.0.0")
		if a.Run != nil || a.Fail || a.Text != want {
			t.Errorf("%s dev=%v: %+v", c.pm, c.dev, a)
		}
	}
}

func TestFixCount(t *testing.T) {
	for _, c := range []struct {
		out  string
		want int
	}{
		{"ok a\nall 3 checks ok\n", 0},
		{"warn a\n\nTo fix\n\n1. warn a\n   Expected: x.\n\n2. FAIL b\n\n1 FAIL\n", 2},
	} {
		if got := FixCount(c.out); got != c.want {
			t.Errorf("FixCount(%q) = %d, want %d", c.out, got, c.want)
		}
	}
}

// A binary swapped under a running process makes os.Executable name a ghost
// path; Classify reads the startup path, so the process still reads NpmGlobal.
func TestClassifyReadsStartupPath(t *testing.T) {
	if platform() != "linux-x64" {
		t.Skip("layout fixture is linux-x64")
	}
	stubYarn(t, "", errors.New("no yarn"))
	_, exe := mkNpmGlobal(t)
	old := selfexe.Path
	selfexe.Path = func() (string, error) { return exe, nil }
	t.Cleanup(func() { selfexe.Path = old })
	if in := Classify(); in.Mode != NpmGlobal {
		t.Errorf("got %+v; want NpmGlobal from the startup path", in)
	}
}
