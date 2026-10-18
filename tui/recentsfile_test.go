package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func recentsFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "mugicrew", "recents.json")
}

func seedRecents(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRecentsTouch(t *testing.T) {
	all := func(string) bool { return true }
	var many []string
	for i := range 25 {
		many = append(many, fmt.Sprint("b", i))
	}
	for _, c := range []struct {
		name  string
		seed  string
		id    string
		known func(string) bool
		want  []string
	}{
		{"empty file", "", "a", all, []string{"a"}},
		{"new id goes first", `["b","c"]`, "a", all, []string{"a", "b", "c"}},
		{"dedupe moves to top", `["a","b","c"]`, "c", all, []string{"c", "a", "b"}},
		{"garbage reads empty", `{not json`, "a", all, []string{"a"}},
		{"prune gone ids", `["a","gone","b"]`, "c", func(id string) bool { return id != "gone" }, []string{"c", "a", "b"}},
		{"nil known prunes nothing", `["a","gone"]`, "c", nil, []string{"c", "a", "gone"}},
		{"touched id is never pruned", `["a"]`, "new", func(id string) bool { return id == "a" }, []string{"new", "a"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := recentsFile(t)
			if c.seed != "" {
				seedRecents(t, p, c.seed)
			}
			got, err := touchRecent(p, c.id, c.known)
			if err != nil || !slices.Equal(got, c.want) {
				t.Fatalf("touch = %v, %v; want %v", got, err, c.want)
			}
			if disk := loadRecents(p); !slices.Equal(disk, c.want) {
				t.Errorf("on disk %v, want %v", disk, c.want)
			}
		})
	}
	t.Run("cap 20", func(t *testing.T) {
		p := recentsFile(t)
		b := `["` + many[0]
		for _, id := range many[1:] {
			b += `","` + id
		}
		seedRecents(t, p, b+`"]`)
		got, _ := touchRecent(p, "new", all)
		if len(got) != 20 || got[0] != "new" || got[19] != "b18" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("two TUIs merge", func(t *testing.T) {
		p := recentsFile(t)
		touchRecent(p, "a", all) // TUI one
		touchRecent(p, "b", all) // TUI two, whose own list never saw a
		got, _ := touchRecent(p, "c", all)
		if want := []string{"c", "b", "a"}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("missing file loads empty", func(t *testing.T) {
		if got := loadRecents(recentsFile(t)); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("unresolved path", func(t *testing.T) {
		if got := loadRecents(""); len(got) != 0 {
			t.Errorf("load %v", got)
		}
		if _, err := touchRecent("", "a", all); err == nil {
			t.Error("save with no path did not error")
		}
	})
}
