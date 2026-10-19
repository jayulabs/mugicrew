package cfglayer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const spliceSample = `# mugicrew config for acme
# (hand-edited; keep comments)

land_strategy = "local"  # switch to gitlab later
other = 1

[profiles.deepseek]
model   = "deepseek-v3"     # aligned
env = { ANTHROPIC_BASE_URL = "https://x", KEY = "${DS_KEY}" }
args = [
  "--foo",   # why foo
  "--bar",
]

[kind_profiles]
build = "deepseek"
# interior comment
review = "deepseek"


[initiatives]
required = false
external_ref = false

# the mugicrew context
[context.mugicrew]
path = "apps/mugicrew"
gate = "mugicrew"   # flake attr

[context.lineage]
path = "apps/lineage"
`

// swap is want-by-construction: sample with one exact substring replaced, so a
// golden differs from its input only where the edit is owned.
func swap(t *testing.T, in, old, new string) string {
	t.Helper()
	if strings.Count(in, old) != 1 {
		t.Fatalf("test bug: %q not exactly once in input", old)
	}
	return strings.Replace(in, old, new, 1)
}

func ctx(name string) []string { return []string{"context", name} }

func TestWriteOwnedGoldens(t *testing.T) {
	noNL := strings.TrimSuffix(spliceSample, "\n")
	cases := []struct {
		name  string
		in    string
		edits []Edit
		want  string
	}{
		{"set scalar keeps trailing comment", spliceSample,
			[]Edit{{Op: SetKey, Key: "land_strategy", Value: "gitlab"}},
			swap(t, spliceSample, `"local"  #`, `"gitlab"  #`)},
		{"set bool", spliceSample,
			[]Edit{{Op: SetKey, Table: []string{"initiatives"}, Key: "external_ref", Value: true}},
			swap(t, spliceSample, "external_ref = false", "external_ref = true")},
		{"insert missing key after the table's last key line", spliceSample,
			[]Edit{{Op: SetKey, Table: ctx("lineage"), Key: "repo", Value: "lin"}},
			spliceSample + "repo = \"lin\"\n"},
		{"insert missing key mid-file", spliceSample,
			[]Edit{{Op: SetKey, Table: []string{"initiatives"}, Key: "extra", Value: 3}},
			swap(t, spliceSample, "external_ref = false\n", "external_ref = false\nextra = 3\n")},
		{"top-level key goes above the comment block heading the first table",
			"# only tables\n[a]\nx = 1\n",
			[]Edit{{Op: SetKey, Key: "land_strategy", Value: "local"}},
			"land_strategy = \"local\"\n\n# only tables\n[a]\nx = 1\n"},
		{"top-level key after a detached top comment",
			"# top\n\n[a]\nx = 1\n",
			[]Edit{{Op: SetKey, Key: "land_strategy", Value: "local"}},
			"# top\n\nland_strategy = \"local\"\n\n[a]\nx = 1\n"},
		{"top-level key into a file with no tables", "# just a comment\n",
			[]Edit{{Op: SetKey, Key: "land_strategy", Value: "local"}},
			"# just a comment\nland_strategy = \"local\"\n"},
		{"append [jira] at EOF with a blank line", spliceSample,
			[]Edit{{Op: SetKey, Table: []string{"jira"}, Key: "site", Value: "acme.atlassian.net"}},
			spliceSample + "\n[jira]\nsite = \"acme.atlassian.net\"\n"},
		{"append a second key to the table just appended", spliceSample,
			[]Edit{
				{Op: SetKey, Table: ctx("x"), Key: "path", Value: "x"},
				{Op: SetKey, Table: ctx("x"), Key: "gate", Value: "g"}},
			spliceSample + "\n[context.x]\npath = \"x\"\ngate = \"g\"\n"},
		{"last line without a newline", noNL,
			[]Edit{
				{Op: SetKey, Table: []string{"jira"}, Key: "site", Value: "s"},
				{Op: SetKey, Table: ctx("lineage"), Key: "repo", Value: "r"}},
			noNL + "\nrepo = \"r\"\n\n[jira]\nsite = \"s\"\n"},
		{"set the value on a last line without a newline", "land_strategy = \"local\"",
			[]Edit{{Op: SetKey, Key: "land_strategy", Value: "gitlab"}},
			"land_strategy = \"gitlab\""},
		{"remove a key takes its trailing comment", spliceSample,
			[]Edit{{Op: RemoveKey, Table: ctx("mugicrew"), Key: "gate"}},
			swap(t, spliceSample, "gate = \"mugicrew\"   # flake attr\n", "")},
		{"remove a key takes the block comment directly above it",
			"[a]\nx = 1\n# why y\n# really\ny = 2\n# not mine\n\nz = 3\n",
			[]Edit{{Op: RemoveKey, Table: []string{"a"}, Key: "y"}},
			"[a]\nx = 1\n# not mine\n\nz = 3\n"},
		{"remove a key leaves a comment separated by a blank line",
			"[a]\n# about the section\n\ny = 2\n",
			[]Edit{{Op: RemoveKey, Table: []string{"a"}, Key: "y"}},
			"[a]\n# about the section\n\n"},
		{"remove an absent key is a no-op", spliceSample,
			[]Edit{{Op: RemoveKey, Table: ctx("mugicrew"), Key: "repo"}}, spliceSample},
		{"remove a whole table, with its heading comment, up to the next header", spliceSample,
			[]Edit{{Op: RemoveTable, Table: ctx("mugicrew")}},
			swap(t, spliceSample, "# the mugicrew context\n[context.mugicrew]\npath = \"apps/mugicrew\"\ngate = \"mugicrew\"   # flake attr\n\n", "")},
		{"remove the last table drops the blank line before it", spliceSample,
			[]Edit{{Op: RemoveTable, Table: ctx("lineage")}},
			swap(t, spliceSample, "\n[context.lineage]\npath = \"apps/lineage\"\n", "")},
		{"remove a table keeps the next table's heading comment",
			"[a]\nx = 1\n\n# about b\n[b]\ny = 2\n",
			[]Edit{{Op: RemoveTable, Table: []string{"a"}}},
			"# about b\n[b]\ny = 2\n"},
		{"remove an absent table is a no-op", spliceSample,
			[]Edit{{Op: RemoveTable, Table: []string{"jira"}}}, spliceSample},
		{"CRLF files get CRLF inserts", "[a]\r\nx = 1\r\n",
			[]Edit{{Op: SetKey, Table: []string{"a"}, Key: "y", Value: 2}},
			"[a]\r\nx = 1\r\ny = 2\r\n"},
		{"quoted table key", "",
			[]Edit{{Op: SetKey, Table: ctx("my proj"), Key: "path", Value: "p"}},
			"[context.\"my proj\"]\npath = \"p\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), ".mugicrew", "config.toml")
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(c.in), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := WriteOwned(p, c.edits); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(p)
			if string(got) != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestWriteOwnedReadsBackThroughCfglayer(t *testing.T) {
	root := workspace(t, spliceSample, "")
	err := WriteOwned(SharedPath(root), []Edit{
		{Op: SetKey, Table: ctx("lineage"), Key: "path", Value: "apps/lineage2"},
		{Op: SetKey, Table: ctx("x"), Key: "repo", Value: "x"},
		{Op: RemoveTable, Table: ctx("mugicrew")},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := Contexts(root)
	if len(got) != 2 || got["lineage"].Path != "apps/lineage2" || got["x"].Repo != "x" {
		t.Fatalf("contexts = %+v", got)
	}
}

func TestWriteOwnedFreshFileInEditOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".mugicrew", "config.toml")
	err := WriteOwned(p, []Edit{
		{Op: SetKey, Key: "land_strategy", Value: "gitlab"},
		{Op: SetKey, Table: []string{"initiatives"}, Key: "required", Value: true},
		{Op: SetKey, Table: []string{"initiatives"}, Key: "external_ref", Value: false},
		{Op: SetKey, Table: []string{"jira"}, Key: "site", Value: "acme"},
		{Op: SetKey, Table: ctx("a"), Key: "repo", Value: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "land_strategy = \"gitlab\"\n\n[initiatives]\nrequired = true\nexternal_ref = false\n\n[jira]\nsite = \"acme\"\n\n[context.a]\nrepo = \"a\"\n"
	if got, _ := os.ReadFile(p); string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteOwnedRefusesOtherShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		edit Edit
	}{
		{"top-level dotted key", "context.x.path = \"a\"\n", Edit{Op: SetKey, Table: ctx("x"), Key: "path", Value: "b"}},
		{"dotted key then a duplicate table", "context.x.path = \"a\"\n\n[context.x]\nrepo = \"r\"\n", Edit{Op: SetKey, Table: ctx("x"), Key: "path", Value: "b"}},
		{"inline table key", "[context]\nx = { path = \"a\" }\n", Edit{Op: SetKey, Table: ctx("x"), Key: "path", Value: "b"}},
		{"inline table holding the whole table", "initiatives = { required = true }\n", Edit{Op: SetKey, Table: []string{"initiatives"}, Key: "required", Value: false}},
		{"non-scalar value", "[jira]\nsite = [\"a\"]\n", Edit{Op: SetKey, Table: []string{"jira"}, Key: "site", Value: "b"}},
		{"array table", "[[context.x]]\npath = \"a\"\n", Edit{Op: SetKey, Table: ctx("x"), Key: "path", Value: "b"}},
		{"dotted key blocks removing the table", "context.x.path = \"a\"\n", Edit{Op: RemoveTable, Table: ctx("x")}},
		{"dotted key blocks removing the key", "context.x.path = \"a\"\n", Edit{Op: RemoveKey, Table: ctx("x"), Key: "path"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.toml")
			os.WriteFile(p, []byte(c.in), 0o644)
			err := WriteOwned(p, []Edit{c.edit})
			if !errors.Is(err, ErrEditByHand) || !strings.Contains(err.Error(), "edit by hand") {
				t.Fatalf("err = %v, want edit-by-hand refusal", err)
			}
			if got, _ := os.ReadFile(p); string(got) != c.in {
				t.Fatalf("file changed: %q", got)
			}
		})
	}
}

func TestWriteOwnedReadBackFailureLeavesOriginal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(spliceSample), 0o644)
	// Invalid UTF-8 renders as a \x escape, which TOML rejects.
	err := WriteOwned(p, []Edit{{Op: SetKey, Key: "land_strategy", Value: "\xff"}})
	if err == nil {
		t.Fatal("want read-back failure")
	}
	if got, _ := os.ReadFile(p); string(got) != spliceSample {
		t.Fatalf("original changed: %q", got)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "config.toml.*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestWriteOwnedRemoveTableThenSetKeyReplacesTable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[context.x]\npath = \"a\"\ngate = \"g\"\n"), 0o644)
	err := WriteOwned(p, []Edit{
		{Op: RemoveTable, Table: ctx("x")},
		{Op: SetKey, Table: ctx("x"), Key: "repo", Value: "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "\n[context.x]\nrepo = \"b\"\n" && string(got) != "[context.x]\nrepo = \"b\"\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteOwnedRemoveTableThenSetKeyStillFailsOnStaleKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	// A table the splicer can't fully remove would leave "gate" behind.
	err := readBack2(p, "[context.x]\ngate = \"g\"\nrepo = \"b\"\n", []Edit{
		{Op: RemoveTable, Table: ctx("x")},
		{Op: SetKey, Table: ctx("x"), Key: "repo", Value: "b"},
	})
	if err == nil {
		t.Fatal("want stale-key failure")
	}
}

func readBack2(p, content string, edits []Edit) error {
	os.WriteFile(p, []byte(content), 0o644)
	return readBack(p, edits)
}
