package main

import (
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

const lifecycleDoc = "docs/bead-lifecycle.md"

// closeReasonHeader is the exact header cell that marks a closure table.
const closeReasonHeader = "Close reason (exact)"

var (
	lifecyclePlaceholder = regexp.MustCompile(`<[^>]*>`)
	lifecycleSpan        = regexp.MustCompile("`([^`]*)`")
)

// closeFragment is one literal piece of a backticked close reason the doc quotes.
type closeFragment struct{ section, span, text string }

// splitRow splits a Markdown table row into trimmed cells; `\|` is a literal pipe.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// closeFragments walks md's tables and returns the literal fragments of every
// backticked span in the "Close reason (exact)" column, each tagged with the
// level-2 section it sits under. Tables without that column are ignored.
func closeFragments(md string) (frags []closeFragment, tables int) {
	section, col, inFence := "", -1, false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(line[3:])
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			col = -1
			continue
		}
		cells := splitRow(line)
		if col < 0 {
			// A header row (or a column-less table's row: no match, col stays -1).
			for i, c := range cells {
				if c == closeReasonHeader {
					col = i
					tables++
				}
			}
			continue
		}
		if strings.Trim(strings.Join(cells, ""), "-: ") == "" || col >= len(cells) {
			continue // separator or short row
		}
		for _, m := range lifecycleSpan.FindAllStringSubmatch(cells[col], -1) {
			for _, f := range lifecyclePlaceholder.Split(m[1], -1) {
				if f = strings.TrimSpace(f); f != "" {
					frags = append(frags, closeFragment{section, m[1], f})
				}
			}
		}
	}
	return frags, tables
}

// kindSections returns every level-2 heading except Legend and Gaps.
func kindSections(md string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if inFence || !strings.HasPrefix(line, "## ") {
			continue
		}
		if h := strings.TrimSpace(line[3:]); h != "Legend" && h != "Gaps" {
			out = append(out, h)
		}
	}
	return out
}

func TestKindSectionsExtraction(t *testing.T) {
	md := "## Legend\n\n## signal\n\n### Sub\n\n## Gaps\n\n## build\n```\n## fenced\n```\n"
	if got, want := kindSections(md), []string{"signal", "build"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kindSections = %v, want %v", got, want)
	}
}

func TestCloseFragmentsExtraction(t *testing.T) {
	cases := []struct {
		name, md string
		want     []string
		tables   int
	}{
		{"column among others",
			"## signal\n\n| Outcome | Close reason (exact) | Source |\n| --- | --- | --- |\n| a | `rerouted: x` and `plain` | `skipped` |\n",
			[]string{"rerouted: x", "plain"}, 1},
		{"several placeholders",
			"## spec\n\n| Close reason (exact) |\n| --- |\n| `a <x> b <y>c` |\n",
			[]string{"a", "b", "c"}, 1},
		{"placeholder only",
			"## spec\n\n| Close reason (exact) |\n| --- |\n| `<why>` |\n",
			nil, 1},
		{"table without the column",
			"## spec\n\n| Outcome | Reason |\n| --- | --- |\n| a | `ignored` |\n",
			nil, 0},
		{"subsection table",
			"## chart\n\n### Amendment\n\n| Outcome | Close reason (exact) |\n| --- | --- |\n| a | `withdrawn: <t>` |\n",
			[]string{"withdrawn:"}, 1},
		{"escaped pipe",
			"## chart\n\n| Close reason (exact) |\n| --- |\n| `a\\|b` |\n",
			[]string{"a|b"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frags, tables := closeFragments(c.md)
			var got []string
			for _, f := range frags {
				got = append(got, f.text)
			}
			if !reflect.DeepEqual(got, c.want) || tables != c.tables {
				t.Fatalf("got %q in %d tables, want %q in %d", got, tables, c.want, c.tables)
			}
		})
	}
	frags, _ := closeFragments("## signal\n\n| Close reason (exact) |\n| --- |\n| `x <y>` |\n")
	if len(frags) != 1 || frags[0].section != "signal" || frags[0].span != "x <y>" {
		t.Fatalf("fragment context lost: %+v", frags)
	}
}

// lifecycleCorpus is every non-test Go file, role prompt and crew doc.
func lifecycleCorpus(t *testing.T) string {
	t.Helper()
	root := os.DirFS(".")
	var sb strings.Builder
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !scannedFile(path) {
			return err
		}
		b, err := fs.ReadFile(root, path)
		sb.Write(b)
		sb.WriteByte('\n')
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// The bead lifecycle reference must name exactly the contract kinds, and every
// literal close reason it quotes must still exist in the code, prompts or crew docs.
func TestBeadLifecycleReferenceMatchesCode(t *testing.T) {
	b, err := os.ReadFile(lifecycleDoc)
	if err != nil {
		t.Fatal(err)
	}
	md := string(b)

	want := map[string]bool{}
	for _, k := range contract.Kinds {
		want[string(k)] = true
	}
	have := map[string]bool{}
	for _, h := range kindSections(md) {
		have[h] = true
	}
	var missing, extra []string
	for k := range want {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	for h := range have {
		if !want[h] {
			extra = append(extra, h)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	for _, k := range missing {
		t.Errorf("%s has no ## %s section for contract kind %q", lifecycleDoc, k, k)
	}
	for _, h := range extra {
		t.Errorf("%s has ## %s, which is not a contract.Kinds value", lifecycleDoc, h)
	}

	frags, tables := closeFragments(md)
	if tables == 0 || len(frags) < 20 {
		t.Fatalf("walk found %d %q tables and %d fragments; did the column header change?", tables, closeReasonHeader, len(frags))
	}
	corpus := lifecycleCorpus(t)
	for _, f := range frags {
		if !strings.Contains(corpus, f.text) {
			t.Errorf("## %s: close reason span `%s`: fragment %q not found in the Go source, prompts or crew docs", f.section, f.span, f.text)
		}
	}
}
