package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/eventlog"
)

// statsFixture is an event log of four session ends, 1h, 2d, 10d and 40d old.
func statsFixture(t *testing.T) string {
	t.Helper()
	now := time.Now()
	ev := func(ago time.Duration, msg, bead, attrs string) string {
		return fmt.Sprintf(`{"time":%q,"level":"INFO","msg":%q,"bead":%q,%s}`+"\n", now.Add(-ago).Format(time.RFC3339Nano), msg, bead, attrs)
	}
	p := filepath.Join(t.TempDir(), "events.jsonl")
	os.WriteFile(p, []byte(
		ev(time.Hour, "session.ended", "mc-a", `"crewmate":"franky","dur_ms":60000,"api_ms":30000,"cost_usd":1.5,"models":{"opus":{"cost_usd":1.5,"in":10,"out":20}}`)+
			ev(48*time.Hour, "session.failed", "mc-b", `"crewmate":"chopper-the-reindeer-doctor","dur_ms":60000,"api_ms":30000,"cost_usd":3,"models":{"haiku":{"cost_usd":3,"in":100,"out":900}}`)+
			ev(240*time.Hour, "session.ended", "mc-c", `"crewmate":"franky","dur_ms":1000,"models":{"opus":{"in":1,"out":1}}`)+
			ev(960*time.Hour, "session.ended", "mc-d", `"crewmate":"nami","dur_ms":1000,"cost_usd":7`)), 0o644)
	return p
}

// statsModel is a model with the fixture log loaded, on Graph.
func statsModel(t *testing.T) Model {
	t.Helper()
	m := New(nil, nil).WithLog(eventlog.New(statsFixture(t), nil))
	next, _ := m.Update(m.loadLogs()())
	m = next.(Model)
	m.w, m.h = 100, 24
	return m
}

// keyNamed is the key k as bubbletea delivers it: a named key, else runes.
func keyNamed(k string) tea.KeyMsg {
	for _, t := range []tea.KeyType{tea.KeyLeft, tea.KeyRight, tea.KeyEnter, tea.KeyTab, tea.KeyEsc, tea.KeySpace, tea.KeyUp, tea.KeyDown} {
		if t.String() == k {
			return tea.KeyMsg{Type: t}
		}
	}
	return runeKey(k)
}

// plain is v without styling, each run of spaces one space, so rows read as cells.
func plain(v string) string { return strings.Join(strings.Fields(ansi.Strip(v)), " ") }

// statsLines is the plain Stats body lines (the tab bar dropped): the Plan usage band, then the header.
func statsLines(m Model) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n")[1:] {
		out = append(out, strings.Join(strings.Fields(l), " "))
	}
	return out
}

func hasLine(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// FR1–3: # jumps to Stats from any tab, the arrows cycle through it after Logs,
// and the tab bar shows its # key.
func TestStatsTabReachable(t *testing.T) {
	m := statsModel(t)
	if !strings.Contains(ansi.Strip(strings.Split(m.View(), "\n")[0]), "#") {
		t.Errorf("tab bar lacks #: %q", ansi.Strip(m.View()))
	}
	for _, from := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs} {
		m.tab = from
		if got, _ := send(t, m, runeKey("#")); got.tab != tabStats {
			t.Errorf("# from %v landed on %v", from, got.tab)
		}
	}
	m.tab = tabLogs
	m, _ = send(t, m, keyNamed("right"))
	if m.tab != tabStats || !strings.Contains(plain(m.View()), "last 7d") {
		t.Fatalf("right from Logs: tab %v, view:\n%s", m.tab, m.View())
	}
	if next, _ := send(t, m, keyNamed("right")); next.tab != tabSettings {
		t.Errorf("right from Stats landed on %v, want Settings", next.tab)
	}
	m.tab = tabSettings
	if next, _ := send(t, m, keyNamed("left")); next.tab != tabStats {
		t.Errorf("left from Settings landed on %v, want Stats", next.tab)
	}
}

// FR20–24: 7d by none opens on TOTAL only; w and b cycle the header and the rows;
// a Partial cost is marked + with the footer's no-cost count.
func TestStatsWindowAndGrouping(t *testing.T) {
	m, _ := send(t, statsModel(t), runeKey("#"))
	ls := statsLines(m)
	if ls[1] != "last 7d · by none" {
		t.Errorf("header %q", ls[1])
	}
	if !hasLine(ls, "KEY SESSIONS FAILED AGENT API COST TOKENS") || !hasLine(ls, "TOTAL 2 1 2m0s 1m0s $4.50 1.0k") {
		t.Errorf("7d table:\n%s", strings.Join(ls, "\n"))
	}
	if hasLine(ls, "franky") || strings.Contains(m.View(), "without a cost") {
		t.Errorf("none shows TOTAL only, no footer:\n%s", strings.Join(ls, "\n"))
	}

	m, _ = send(t, m, runeKey("b"))
	ls = statsLines(m)
	if ls[1] != "last 7d · by crewmate" || !hasLine(ls, "chopper-the-reindeer-doctor 1 1 1m0s 30s $3.00 1.0k") || !hasLine(ls, "franky 1 0 1m0s 30s $1.50 30") {
		t.Errorf("by crewmate:\n%s", strings.Join(ls, "\n"))
	}

	for _, want := range []struct{ head, total string }{
		{"last 30d · by crewmate", "TOTAL 3 1 2m1s 1m0s $4.50+ 1.0k"},
		{"all time · by crewmate", "TOTAL 4 1 2m2s 1m0s $11.50+ 1.0k"},
		{"last 24h · by crewmate", "TOTAL 1 0 1m0s 30s $1.50 30"},
		{"last 7d · by crewmate", "TOTAL 2 1 2m0s 1m0s $4.50 1.0k"},
	} {
		m, _ = send(t, m, runeKey("w"))
		ls = statsLines(m)
		if ls[1] != want.head || !hasLine(ls, want.total) {
			t.Errorf("want %q / %q:\n%s", want.head, want.total, strings.Join(ls, "\n"))
		}
	}
	m, _ = send(t, m, runeKey("w"))
	if v := plain(m.View()); !strings.Contains(v, "+1 sessions without a cost") || !strings.Contains(v, "franky 2 0 1m1s 30s $1.50+ 32") {
		t.Errorf("30d: want the Partial cost marked and the footer:\n%s", m.View())
	}

	m, _ = send(t, m, runeKey("b"))
	ls = statsLines(m)
	if ls[1] != "last 30d · by model" || !hasLine(ls, "haiku – – – – $3.00 1.0k") || !hasLine(ls, "opus – – – – $1.50+ 32") {
		t.Errorf("by model: want – for the unattributable cells:\n%s", strings.Join(ls, "\n"))
	}
	m, _ = send(t, m, runeKey("b")) // day
	m, _ = send(t, m, runeKey("b")) // none again
	if ls = statsLines(m); ls[1] != "last 30d · by none" || hasLine(ls, "opus") {
		t.Errorf("b wraps back to none:\n%s", strings.Join(ls, "\n"))
	}
}

// FR26–27: day rows are newest first; TOTAL is the same under every grouping.
func TestStatsDayRowsAndTotal(t *testing.T) {
	m, _ := send(t, statsModel(t), runeKey("#"))
	m, _ = send(t, m, runeKey("w"))
	m, _ = send(t, m, runeKey("w")) // all
	var totals []string
	for range 4 {
		m, _ = send(t, m, runeKey("b"))
		for _, l := range statsLines(m) {
			if strings.HasPrefix(l, "TOTAL ") {
				totals = append(totals, l)
			}
		}
	}
	if len(totals) != 4 || totals[0] != "TOTAL 4 1 2m2s 1m0s $11.50+ 1.0k" || totals[1] != totals[0] || totals[2] != totals[0] || totals[3] != totals[0] {
		t.Errorf("TOTAL rows across groupings: %q", totals)
	}

	m, _ = send(t, m, runeKey("b"))
	m, _ = send(t, m, runeKey("b"))
	m, _ = send(t, m, runeKey("b")) // day
	var days []string
	for _, l := range statsLines(m) {
		if len(l) > 10 && l[4] == '-' && l[7] == '-' {
			days = append(days, l[:10])
		}
	}
	now := time.Now()
	want := []string{}
	for _, ago := range []time.Duration{time.Hour, 48 * time.Hour, 240 * time.Hour, 960 * time.Hour} {
		want = append(want, now.Add(-ago).Format("2006-01-02"))
	}
	if strings.Join(days, " ") != strings.Join(want, " ") {
		t.Errorf("day rows %v, want newest first %v", days, want)
	}
}

// FR28: a log that grows refreshes the table on the next read.
func TestStatsFollowsTheLog(t *testing.T) {
	m, _ := send(t, statsModel(t), runeKey("#"))
	f, _ := os.OpenFile(m.logPath, os.O_APPEND|os.O_WRONLY, 0)
	fmt.Fprintf(f, `{"time":%q,"level":"INFO","msg":"session.ended","bead":"mc-e","dur_ms":1000,"cost_usd":1}`+"\n", time.Now().Format(time.RFC3339Nano))
	f.Close()
	next, _ := m.Update(m.loadLogs()())
	if ls := statsLines(next.(Model)); !hasLine(ls, "TOTAL 3 1 2m1s 1m0s $5.50 1.0k") {
		t.Errorf("after the log grew:\n%s", strings.Join(ls, "\n"))
	}
}

// FR29: at the 40-col floor KEY is cut with … and every numeric column stays.
func TestStatsNarrowKeepsNumbers(t *testing.T) {
	m, _ := send(t, statsModel(t), runeKey("#"))
	m, _ = send(t, m, runeKey("b"))
	m.w = floorCols
	v := ansi.Strip(m.View())
	for _, l := range strings.Split(v, "\n") {
		if lipgloss.Width(l) > floorCols {
			t.Errorf("line %q is %d wide", l, lipgloss.Width(l))
		}
	}
	ls := statsLines(m)
	if !hasLine(ls, "KEY SESS FAIL AGENT API COST TOK") {
		t.Errorf("narrow header lost a column:\n%s", v)
	}
	found := false
	for _, l := range ls {
		if strings.Contains(l, "…") && strings.HasSuffix(l, "1 1 1m0s 30s $3.00 1.0k") && !strings.Contains(l, "doctor") {
			found = true
		}
	}
	if !found {
		t.Errorf("narrow: want the long crewmate cut with … and its numbers whole:\n%s", v)
	}
}

// FR4, FR31: Stats has no selection; b only cycles the grouping (never launches)
// and every key off the whitelist is a no-op.
func TestStatsKeysScoped(t *testing.T) {
	m := pausedGraph(t).WithLog(eventlog.New(statsFixture(t), nil))
	next, _ := m.Update(m.loadLogs()())
	m = next.(Model)
	m.selInbox = "ext"
	m, _ = send(t, m, runeKey("#"))
	before := m.View()
	for _, k := range []string{"l", "t", "p", "x", "e", "E", "n", "u", "z", "+", "-", "j", "k", "f", "/", "R", "d", "h", " ", "enter", "tab", "esc"} {
		got, cmd := send(t, m, keyNamed(k))
		if cmd != nil || got.View() != before || got.editMode != editNone {
			t.Errorf("%q on Stats acted: cmd=%v edit=%v", k, cmd != nil, got.editMode)
		}
	}
	got, cmd := send(t, m, runeKey("b"))
	if cmd != nil || len(got.launching) > 0 || !strings.Contains(plain(got.View()), "by crewmate") {
		t.Errorf("b on Stats: cmd=%v launching=%v, want only the grouping cycled", cmd != nil, got.launching)
	}
	if strings.Contains(plain(before), "│") {
		t.Errorf("Stats footer shows bead keys:\n%s", before)
	}
}

// FR30: the footer and ? list w, b and f5 on Stats; every tab's ? lists #.
func TestStatsHelp(t *testing.T) {
	m, _ := send(t, statsModel(t), runeKey("#"))
	if f := plain(m.footer()); !strings.Contains(f, "w window") || !strings.Contains(f, "b group") || !strings.Contains(f, "f5 refresh") {
		t.Errorf("Stats footer %q", f)
	}
	for _, tb := range []tab{tabGraph, tabInbox, tabSearch, tabHistory, tabLogs, tabStats} {
		listed := map[string]string{}
		for _, k := range helpKeys(tb, "ctrl+b") {
			listed[k.k] = k.label
		}
		if listed["#"] != "Stats tab" {
			t.Errorf("%v: ? lacks # Stats tab", tb)
		}
		if tb == tabStats && (listed["w"] == "" || listed["b"] == "" || listed["F5"] == "") {
			t.Errorf("Stats ? lacks w, b or F5: %v", listed)
		}
	}
}
