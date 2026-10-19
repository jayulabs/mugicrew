package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var npmPlatforms = []string{"linux-x64", "linux-arm64", "darwin-arm64", "darwin-x64"}

type npmManifest struct {
	Version              string            `json:"version"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

func readNpmManifest(t *testing.T, root, pkg string) npmManifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "npm", pkg, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m npmManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", pkg, err)
	}
	return m
}

// npmManifestProblems reports, in the publish workflow's terms, every platform manifest
// version and launcher pin that differs from the launcher's own version.
func npmManifestProblems(t *testing.T, root string) []string {
	t.Helper()
	launcher := readNpmManifest(t, root, "mugicrew")
	var problems []string
	for _, p := range npmPlatforms {
		if v := readNpmManifest(t, root, "mugicrew-"+p).Version; v != launcher.Version {
			problems = append(problems, "version mismatch: mugicrew-"+p+" is "+v+", mugicrew is "+launcher.Version)
		}
		if pin := launcher.OptionalDependencies["@jayulabs/mugicrew-"+p]; pin != launcher.Version {
			problems = append(problems, "pin mismatch: optionalDependencies @jayulabs/mugicrew-"+p+" is "+pin+", mugicrew is "+launcher.Version)
		}
	}
	return problems
}

func TestNpmManifestsAgree(t *testing.T) {
	if _, err := os.Stat("npm/mugicrew/package.json"); err != nil {
		t.Skip("no npm manifests in this tree")
	}
	for _, p := range npmManifestProblems(t, ".") {
		t.Error(p)
	}
}

func TestNpmManifestsDisagreementIsNamed(t *testing.T) {
	if _, err := os.Stat("npm/mugicrew/package.json"); err != nil {
		t.Skip("no npm manifests in this tree")
	}
	launcher := readNpmManifest(t, ".", "mugicrew").Version
	const stray = "9.9.9"
	cases := []struct{ name, pkg, old, want string }{}
	for _, p := range npmPlatforms {
		cases = append(cases,
			struct{ name, pkg, old, want string }{"version " + p, "mugicrew-" + p, `"version": "` + launcher + `"`,
				"version mismatch: mugicrew-" + p + " is " + stray + ", mugicrew is " + launcher},
			struct{ name, pkg, old, want string }{"pin " + p, "mugicrew", `"@jayulabs/mugicrew-` + p + `": "` + launcher + `"`,
				"pin mismatch: optionalDependencies @jayulabs/mugicrew-" + p + " is " + stray + ", mugicrew is " + launcher})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for _, pkg := range append([]string{"mugicrew"}, prefixed("mugicrew-", npmPlatforms)...) {
				b, err := os.ReadFile(filepath.Join("npm", pkg, "package.json"))
				if err != nil {
					t.Fatal(err)
				}
				if pkg == c.pkg {
					s := string(b)
					if !strings.Contains(s, c.old) {
						t.Fatalf("%s lacks %s", pkg, c.old)
					}
					b = []byte(strings.Replace(s, c.old, strings.Replace(c.old, launcher, stray, 1), 1))
				}
				dir := filepath.Join(root, "npm", pkg)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "package.json"), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := npmManifestProblems(t, root)
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("problems = %q, want [%q]", got, c.want)
			}
		})
	}
}

func prefixed(pre string, xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = pre + x
	}
	return out
}
