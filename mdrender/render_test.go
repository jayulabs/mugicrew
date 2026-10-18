package mdrender

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const body = "# Title\n\n## What to build\n\nThe tracer bullet with `inline code` and **strong** and *em*, a long enough line to wrap at the narrower widths used below.\n\n- first item that is long enough to wrap onto a second line at narrow widths\n  - nested item\n    1. deep ordered\n- second\n\n```go\nfunc veryLongFunctionName(argumentOne, argumentTwo, argumentThree int) {}\n\tx := 1\n```\n\n> quoted text\n\n---\n\n[docs](https://example.com/x) and <https://auto.example>\n"

func TestRenderContract(t *testing.T) {
	for _, w := range []int{1, 10, 38, 78, 118} {
		lines := strings.Split(Render(body, w), "\n")
		if lines[0] != "" || lines[len(lines)-1] != "" {
			t.Errorf("w=%d: first/last line not blank", w)
		}
		for _, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("w=%d: line width %d: %q", w, got, l)
			}
			if w >= 3 && ansi.Strip(l) != "" && !strings.HasPrefix(l, " ") {
				t.Errorf("w=%d: no margin: %q", w, l)
			}
		}
	}
}

func plain(src string, w int) string { return ansi.Strip(Render(src, w)) }

func TestHeadings(t *testing.T) {
	out := Render("# One\n\n## Two\n\ntext", 40)
	if strings.Contains(ansi.Strip(out), "#") {
		t.Errorf("# kept: %q", out)
	}
	if !strings.Contains(out, "\x1b[4m") {
		t.Errorf("h1 not underlined: %q", out)
	}
	if !strings.Contains(plain("text\n\n## Two", 40), "\n\n Two") {
		t.Errorf("no blank line above heading: %q", plain("text\n\n## Two", 40))
	}
}

func TestListHangingIndentAndSpacing(t *testing.T) {
	got := plain("- aaa bbb ccc ddd eee\n- two", 14)
	want := "\n • aaa bbb\n   ccc ddd\n   eee\n • two\n"
	if got != want {
		t.Errorf("tight:\n%q\nwant\n%q", got, want)
	}
	got = plain("- one\n\n- two", 20)
	if want := "\n • one\n\n • two\n"; got != want {
		t.Errorf("loose: %q want %q", got, want)
	}
	got = plain("2. a\n3. b", 20)
	if want := "\n 2. a\n 3. b\n"; got != want {
		t.Errorf("ordered: %q want %q", got, want)
	}
}

func TestCodeBlockHardBreaksKeepingIndent(t *testing.T) {
	got := plain("```\nabcdefghijklmnop\n\tx\n```", 12)
	want := "\n   abcdefgh\n   ijklmnop\n       x\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestLinksAndNoOSC8(t *testing.T) {
	out := Render("[docs](https://e.com/x) <https://a.example> [https://same](https://same)", 80)
	if got := ansi.Strip(out); !strings.Contains(got, "docs (https://e.com/x)") || !strings.Contains(got, "https://a.example") {
		t.Errorf("links: %q", got)
	}
	if strings.Contains(out, "\x1b]8;") {
		t.Errorf("OSC 8 present")
	}
}

func TestUnknownConstructKeepsText(t *testing.T) {
	if got := plain("![alt text](pic.png)", 40); !strings.Contains(got, "alt text") {
		t.Errorf("image text lost: %q", got)
	}
}

func TestWideRunesStayWithinWidth(t *testing.T) {
	for _, l := range strings.Split(Render("日本語のテキスト😀😀😀 mixed 日本語", 9), "\n") {
		if ansi.StringWidth(l) > 9 {
			t.Errorf("overflow: %q", l)
		}
	}
}

func TestBlockquoteAndRule(t *testing.T) {
	got := plain("> hi\n\n---", 10)
	if !strings.Contains(got, "│ hi") || !strings.Contains(got, "────") {
		t.Errorf("got %q", got)
	}
}

const table = "| Name | Qty | Note |\n|:-----|:---:|-----:|\n| apple | 3 | ok |\n| fig | 12 | later |\n"

func TestTableFitsDrawnWithRulesBoldHeaderAndAlignment(t *testing.T) {
	out := Render(table, 40)
	if !strings.Contains(out, "\x1b[1m") {
		t.Errorf("header not bold: %q", out)
	}
	want := []string{
		"┌───────┬─────┬───────┐",
		"│ Name  │ Qty │  Note │",
		"├───────┼─────┼───────┤",
		"│ apple │  3  │    ok │",
		"│ fig   │ 12  │ later │",
		"└───────┴─────┴───────┘",
	}
	got := strings.Split(strings.Trim(ansi.Strip(out), "\n"), "\n")
	for i := range got {
		got[i] = strings.TrimPrefix(got[i], " ")
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTaskItemsAndStrikethrough(t *testing.T) {
	out := Render("- [ ] todo\n- [x] done\n\n~~gone~~", 40)
	p := ansi.Strip(out)
	if !strings.Contains(p, "☐ todo") || !strings.Contains(p, "☑ done") || strings.Contains(p, "[") {
		t.Errorf("tasks: %q", p)
	}
	if !strings.Contains(out, "\x1b[9m") || !strings.Contains(out, "gone\x1b[29m") {
		t.Errorf("no SGR 9: %q", out)
	}
}

func TestWidthContractWithWideTable(t *testing.T) {
	wide := "| " + strings.Repeat("column ", 30) + " | b |\n|---|---|\n| 1 | 2 |\n\n" + table + "\n- [x] a ~~b~~\n"
	for _, w := range []int{1, 10, 38, 78, 118} {
		for _, l := range strings.Split(Render(wide, w), "\n") {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("w=%d: width %d: %q", w, got, l)
			}
		}
	}
}

func TestH4ToH6BoldMuted(t *testing.T) {
	out := Render("#### h4\n\n##### h5\n\n###### h6", 30)
	for _, l := range []string{"h4", "h5", "h6"} {
		if !strings.Contains(out, "\x1b[1m\x1b[38;5;244m"+l) {
			t.Errorf("%s not bold muted: %q", l, out)
		}
	}
	if strings.Contains(out, "38;5;212m") {
		t.Errorf("accent colour on h4–h6: %q", out)
	}
}

func TestHTMLCommentKeepsClosingLineMuted(t *testing.T) {
	out := Render("<!--\nhidden comment\n-->\n\nafter", 30)
	if !strings.Contains(ansi.Strip(out), "-->") {
		t.Fatalf("closing line dropped: %q", ansi.Strip(out))
	}
	if !strings.Contains(out, "\x1b[38;5;244m-->") {
		t.Errorf("closing line not muted: %q", out)
	}
}

func TestCodeSpanIsLiteral(t *testing.T) {
	const lit = `a.b \ &amp;`
	if got := plain("`"+lit+"`", 40); !strings.Contains(got, lit) {
		t.Errorf("code span altered: %q", got)
	}
}

func TestNestedConstructsNeverPanicOrOverflow(t *testing.T) {
	srcs := []string{
		"> ---",
		"- a\n\n  ---",
		"- a\n  - b\n    - > ---",
		strings.Repeat(">", 19) + " ---",
		strings.Repeat("> ", 19) + "text\n\n" + strings.Repeat("> ", 19) + "```\ncode\n```",
		"- > | a | b |\n  |---|---|\n  | 1 | 2 |",
	}
	for _, src := range srcs {
		for _, w := range []int{1, 3, 8, 38} {
			for _, l := range strings.Split(Render(src, w), "\n") {
				if got := ansi.StringWidth(l); got > w {
					t.Errorf("w=%d %q: line width %d: %q", w, src, got, l)
				}
			}
		}
	}
}

// boxLines returns the ANSI-stripped, margin-trimmed lines of Render(src, w).
func boxLines(src string, w int) []string {
	ls := strings.Split(strings.Trim(plain(src, w), "\n"), "\n")
	for i := range ls {
		ls[i] = strings.TrimPrefix(ls[i], " ")
	}
	return ls
}

func wantLines(t *testing.T, src string, w int, want ...string) {
	t.Helper()
	got := boxLines(src, w)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range strings.Split(Render(src, w), "\n") {
		if ansi.StringWidth(l) > w {
			t.Errorf("w=%d: width %d: %q", w, ansi.StringWidth(l), l)
		}
	}
}

func TestNarrowedBoxProportionalWrappedRulesAndWrappedHeader(t *testing.T) {
	src := "| Change item | Why |\n|---|---|\n| alpha beta gamma delta | one two |\n| x | three four five six |\n"
	wantLines(t, src, 22,
		"┌─────────┬────────┐",
		"│ Change  │ Why    │",
		"│ item    │        │",
		"├─────────┼────────┤",
		"│ alpha   │ one    │",
		"│ beta    │ two    │",
		"│ gamma   │        │",
		"│ delta   │        │",
		"├─────────┼────────┤",
		"│ x       │ three  │",
		"│         │ four   │",
		"│         │ five   │",
		"│         │ six    │",
		"└─────────┴────────┘",
	)
}

func TestPathCodeSpanWrapsAtSeparatorsBeforeHardBreak(t *testing.T) {
	src := "| Where | N |\n|---|---|\n| `apps/mugicrew/mdrender/render.go` | 1 |\n"
	wantLines(t, src, 24,
		"┌────────────────┬───┐",
		"│ Where          │ N │",
		"├────────────────┼───┤",
		"│ apps/mugicrew/ │ 1 │",
		"│ mdrender/      │   │",
		"│ render.go      │   │",
		"└────────────────┴───┘",
	)
}

func TestHardBrokenColumnAtFour(t *testing.T) {
	src := "| a | b |\n|---|---|\n| abcdefghijkl | xy |\n"
	wantLines(t, src, 15,
		"┌──────┬────┐",
		"│ a    │ b  │",
		"├──────┼────┤",
		"│ abcd │ xy │",
		"│ efgh │    │",
		"│ ijkl │    │",
		"└──────┴────┘",
	)
}

func TestAlignmentOnWrappedLines(t *testing.T) {
	src := "| r | c |\n|--:|:-:|\n| aa bb cc | dd ee ff |\n"
	wantLines(t, src, 17,
		"┌──────┬──────┐",
		"│    r │  c   │",
		"├──────┼──────┤",
		"│   aa │  dd  │",
		"│   bb │  ee  │",
		"│   cc │  ff  │",
		"└──────┴──────┘",
	)
}

func TestStyleReopensAfterWrapWithoutBorderInsideSGR(t *testing.T) {
	out := Render("| h | i |\n|---|---|\n| `aaa bbb` | **cc dd** |\n", 15)
	if strings.Count(out, "\x1b[38;5;180m") != 2 || strings.Count(out, "\x1b[1m") != 4 { // header cells + 2 lines of body bold
		t.Errorf("styles not re-opened per line: %q", out)
	}
	bar := "\x1b[38;5;244m│\x1b[39m"
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, bar) {
			continue // a rule line
		}
		for _, seg := range strings.Split(l, bar) {
			if strings.Contains(seg, "\x1b[") && !strings.Contains(seg, "\x1b[0m") {
				t.Errorf("cell left a style open before its border: %q", l)
			}
		}
	}
}

func TestLinkSuffixInCellWraps(t *testing.T) {
	src := "| l | n |\n|---|---|\n| [doc](http://x.io/a-b) | 1 |\n"
	got := strings.Join(boxLines(src, 24), "\n")
	if !strings.Contains(got, "doc (http://") || !strings.Contains(got, "x.io/a-b)") {
		t.Errorf("link suffix lost or unwrapped:\n%s", got)
	}
}

func TestRaggedRowPadded(t *testing.T) {
	wantLines(t, "| a | b |\n|---|---|\n| 1 |\n", 40,
		"┌───┬───┐",
		"│ a │ b │",
		"├───┼───┤",
		"│ 1 │   │",
		"└───┴───┘",
	)
}

func TestTableWidthSweepAllTiers(t *testing.T) {
	srcs := []string{six, table, "- item\n\n  | h1 | h2 |\n  |---|---|\n  | aaaaaaaaaaaaaaa | bbbbbbbbb-bbbbbbbbb |\n  | c | d |\n",
		"> | h1 | h2 |\n> |---|---|\n> | aaaaaaaaaaaaaaa | bbbbbbbbb-bbbbbbbbb |\n> | c | d |\n"}
	for _, src := range srcs {
		for w := 1; w <= 118; w++ {
			for _, l := range strings.Split(Render(src, w), "\n") {
				if got := ansi.StringWidth(l); got > w {
					t.Fatalf("w=%d: width %d: %q", w, got, l)
				}
			}
		}
	}
}

const six = "| a1 | b2 | c3 | d4 | e5 | f6 |\n|---|---|---|---|---|---|\n| xxxxxxxxxxxx | yyyy-yyyy | zz/zz | w | v | u |\n| 1 | 2 | 3 | 4 | 5 | 6 |\n"

func TestStackedRecordsForTableTooWideForAnyBox(t *testing.T) {
	out := plain(six, 20)
	if strings.Contains(out, "|---") || strings.Contains(out, "┌") {
		t.Fatalf("raw or box:\n%s", out)
	}
	for _, want := range []string{"a1: xxxxxxxxxxxx", "f6: u", "────"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "a1:") != 2 || strings.Contains(out, "a1 b2") {
		t.Errorf("header row printed or rows lost:\n%s", out)
	}
}

func TestStackedKeepsTrailingEmptyRowAndEmptyHeader(t *testing.T) {
	src := "| | | | | | |\n|---|---|---|---|---|---|\n| zzzzzz | b | | | | |\n| | | | | | |\n\nafter\n"
	out := plain(src, 20)
	if strings.Count(out, "after") != 1 || strings.Count(out, "─") != 18 || !strings.Contains(out, "zzzzzz") {
		t.Fatalf("lost rows or swallowed text:\n%s", out)
	}
}

func TestStackedWrapsValueWithTwoSpaceContinuation(t *testing.T) {
	src := "| a1 | b2 | c3 | d4 | e5 | f6 |\n|---|---|---|---|---|---|\n| one two three four five six | y | z | w | v | u |\n"
	lines := strings.Split(plain(src, 20), "\n")
	for i, l := range lines {
		// lines carry the 1-cell margin; the continuation adds 2 more
		if strings.HasPrefix(l, " a1: ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "   ") && strings.TrimSpace(lines[i+1]) != "" {
			return
		}
	}
	t.Fatalf("no 2-space continuation after a1:\n%s", strings.Join(lines, "\n"))
}

func TestStackedHeaderOnlyTablePrintsHeaderCells(t *testing.T) {
	out := plain("| a1 | b2 | c3 | d4 | e5 | f6 |\n|---|---|---|---|---|---|\n", 20)
	if !strings.Contains(out, "a1") || !strings.Contains(out, "f6") {
		t.Fatalf("header-only table vanished:\n%s", out)
	}
}

func TestCellLinesEnDashNoBlankLines(t *testing.T) {
	for _, c := range []struct {
		s    string
		w    int
		want []string
	}{
		{"widths T=120–240 measured", 6, nil},
		{"foo–bar–baz–qux–quux", 8, nil},
		{"", 5, []string{""}},
	} {
		got := cellLines(c.s, c.w)
		if c.want != nil {
			if len(got) != len(c.want) || ansi.Strip(got[0]) != c.want[0] {
				t.Errorf("%q: got %q want %q", c.s, got, c.want)
			}
			continue
		}
		for _, l := range got {
			if ansi.Strip(l) == "" {
				t.Errorf("%q w=%d: blank line in %q", c.s, c.w, got)
			}
		}
		if j := strings.ReplaceAll(strings.Join(got, ""), " ", ""); ansi.Strip(j) != strings.ReplaceAll(c.s, " ", "") {
			t.Errorf("%q: content out of order: %q", c.s, got)
		}
	}
}

func TestCarryResetClearsStyles(t *testing.T) {
	got := carry([]string{"\x1b[1mab\x1b[0m", "cd"})
	if strings.Contains(got[1], "\x1b[1m") {
		t.Fatalf("bold carried past reset: %q", got[1])
	}
}

func TestCellLinesPathAtExactFloorBreaksOnlyAtSeparators(t *testing.T) {
	var got []string
	for _, l := range cellLines("apps/mugicrew/mdrender/render.go:196", 13) {
		got = append(got, ansi.Strip(l))
	}
	want := []string{"apps/", "mugicrew/", "mdrender/", "render.go:196"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}
