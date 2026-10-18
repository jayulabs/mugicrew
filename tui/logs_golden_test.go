package tui

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLogsGolden pins the Logs render byte-for-byte (UPDATE_GOLDEN=1 re-records),
// over several selections and heights, filtered and not, drawer open and shut.
func TestLogsGolden(t *testing.T) {
	defer func(l *time.Location) { time.Local = l }(time.Local)
	time.Local = time.UTC // the stamp renders in Local; pin it so the golden holds on any host
	var evs = stubEvents()
	levels := []string{"INFO", "WARN", "ERROR", "DEBUG", "INFO"}
	beads := []string{"run", "ch", "", "old", "paused"}
	for i := range 60 {
		evs = append(evs, ev(10+i, levels[i%5], fmt.Sprintf("msg.%d", i), beads[i%5], "ch"))
	}
	base := logsTab{logsData: logsData{events: evs, beads: stubBeads(), skipped: 1}}
	filters := map[string]func(l logsTab) logsTab{
		"plain":  func(l logsTab) logsTab { return l },
		"debug":  func(l logsTab) logsTab { l.debug = true; return l },
		"errs":   func(l logsTab) logsTab { l.errs = true; return l },
		"words":  func(l logsTab) logsTab { l.words = "msg.1"; return l },
		"typing": func(l logsTab) logsTab { l.typing, l.edit = true, lineAt("run"); return l },
		"scope":  func(l logsTab) logsTab { l.scope, l.kids = "ch", true; return l },
		"none":   func(l logsTab) logsTab { l.words = "zzz"; return l },
	}
	var got []string
	for _, name := range []string{"plain", "debug", "errs", "words", "typing", "scope", "none"} {
		l := filters[name](base)
		if l.active() { // a finished filter scan: the matches, newest first
			match := l.matcher()
			for i := len(evs) - 1; i >= 0; i-- {
				if match(evs[i]) {
					l.scan.found = append(l.scan.found, evs[i])
				}
				if b := l.base(); b.matcher()(evs[i]) {
					l.scan.tally.Lines++
					if evs[i].Level == "DEBUG" {
						l.scan.tally.Debug++
					}
				}
			}
		}
		rows := l.rows()
		for i := 0; i < len(rows) || i == 0; i += 7 {
			s := l
			if len(rows) > 0 {
				s.sel = l.ev(rows[i]).Key()
			}
			for _, h := range []int{6, 20} {
				for _, open := range []bool{false, true} {
					s.open = open
					f := s.view(100, h)
					got = append(got, fmt.Sprintf("%s/%d/%d/%v %x", name, i, h, open, sha256.Sum256([]byte(f))))
				}
			}
		}
	}
	text := strings.Join(got, "\n") + "\n"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile("testdata/logs.golden", []byte(text), 0o644)
	}
	want, err := os.ReadFile("testdata/logs.golden")
	if err != nil {
		t.Fatal(err)
	}
	if text != string(want) {
		t.Errorf("Logs render differs from golden")
	}
	if len(got) < 80 {
		t.Errorf("only %d frames", len(got))
	}
}

// Rows are computed once per View.
func TestLogsViewComputesRowsOnce(t *testing.T) {
	l := logsTab{logsData: logsData{events: stubEvents(), beads: stubBeads()}}
	l.words = "effort"
	rowScans = 0
	l.view(100, 20)
	if rowScans > 2 { // the filtered rows and the unfiltered total
		t.Errorf("rowsBy ran %d times in one view", rowScans)
	}
}
