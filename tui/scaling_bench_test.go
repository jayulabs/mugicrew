package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/session"
)

// The scaling gate (docs/tui-scaling-baseline.md; Spec FR30): Graph and Logs allocs/op at
// a synthetic 10k beads / 100k events stay within 1.25× of today's store (3.5k beads /
// 25k events). Allocations are deterministic where wall time on the dev box is not.
//
//	CGO_ENABLED=0 go test ./tui -run '^$' -bench BenchmarkScaling -benchtime 3x

const scaleGate = 1.25

type scaleCase struct {
	name           string
	charts, events int
}

var scales = []scaleCase{{"today", 380, 25_000}, {"synthetic", 1030, 100_000}}

// scaleFix is a store: a fixed open set (one Initiative, five open Charts) and charts
// parentless closed Charts of nine closed Builds each, the history that only grows. The
// default closed set is what the TUI loads without a claim.
type scaleFix struct {
	all, dflt []bd.Bead
	log       string
}

var (
	scaleMu    sync.Mutex
	scaleFixes = map[string]*scaleFix{}
)

func scaleFixture(tb testing.TB, sc scaleCase) *scaleFix {
	scaleMu.Lock()
	defer scaleMu.Unlock()
	if f := scaleFixes[sc.name]; f != nil {
		return f
	}
	f := &scaleFix{}
	add := func(b bd.Bead, dflt bool) {
		b.Created = epoch
		b.Title = b.ID
		f.all = append(f.all, b)
		if dflt {
			f.dflt = append(f.dflt, b)
		}
	}
	add(bd.Bead{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open"}, true)
	for c := range 5 {
		ch := fmt.Sprintf("ini.c%d", c)
		add(bd.Bead{ID: ch, Kind: contract.KindChart, Container: true, Status: "open", Parent: "ini"}, true)
		for k := range 8 {
			st := "closed"
			if k < 2 {
				st = "open"
			}
			add(bd.Bead{ID: fmt.Sprintf("%s.b%d", ch, k), Kind: contract.KindBuild, Status: st, Parent: ch}, true) // member of an open Chart
		}
	}
	for c := range sc.charts {
		ch := fmt.Sprintf("old%d", c)
		add(bd.Bead{ID: ch, Kind: contract.KindChart, Container: true, Status: "closed"}, false)
		for k := range 9 {
			add(bd.Bead{ID: fmt.Sprintf("%s.b%d", ch, k), Kind: contract.KindBuild, Status: "closed", Parent: ch}, false)
		}
	}
	var sb strings.Builder
	t0 := epoch.Add(24 * time.Hour)
	msgs := []string{"effort.started", "effort.exited", "bd.read", "land.checkpoint", "tmux.poll"}
	for i := range sc.events {
		fmt.Fprintf(&sb, `{"time":%q,"level":"INFO","msg":%q,"pid":"1","bead":"old%d","note":"landed checkpoint %d and moved on"}`+"\n",
			t0.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano), msgs[i%len(msgs)], i%sc.charts, i)
	}
	f.log = filepath.Join(os.TempDir(), fmt.Sprintf("mugicrew-scale-%d-%s.jsonl", os.Getpid(), sc.name))
	if err := os.WriteFile(f.log, []byte(sb.String()), 0o644); err != nil {
		tb.Fatal(err)
	}
	scaleFixes[sc.name] = f
	return f
}

func loadedOf(beads []bd.Bead) loadedMsg {
	open := map[string]bool{}
	var closed []bd.Bead
	for _, b := range beads {
		if b.Status != "closed" {
			open[b.ID] = true
		} else {
			closed = append(closed, b)
		}
	}
	return loadedMsg{beads: beads, closed: closed, openIDs: open, slowTier: true, live: map[string]bool{}, sessions: map[string]session.Info{}, started: time.Now()}
}

// scaleDrain runs cmd and its follow-ups through Update until the Logs filter scan is done.
func scaleDrain(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = scaleDrain(m, c)
		}
		return m
	}
	n, next := m.Update(msg)
	m = n.(Model)
	if m.tabAt(tabLogs).(logsTab).scan.searching {
		return scaleDrain(m, next)
	}
	return m
}

// scaleModel is a 200×50 TUI on the fixture's default load and its event log, on tabKey.
func scaleModel(f *scaleFix, tabKey string, keys ...tea.Msg) Model {
	mirrorBeads = f.dflt
	m, _ := New(nil, nil).WithLog(eventlog.New(f.log, func(string) {})).Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	mm := m.(Model)
	mm.loading = false
	n, _ := mm.Update(loadedOf(f.dflt))
	mm = n.(Model)
	mm = scaleDrain(mm, mm.loadLogs())
	if tabKey != "" {
		n, _ = mm.Update(runeKey(tabKey))
		mm = n.(Model)
	}
	for _, k := range keys {
		n, cmd := mm.Update(k)
		mm = scaleDrain(n.(Model), cmd)
	}
	return mm
}

type scalePath struct {
	name string
	op   func(f *scaleFix) func()
}

func keyOp(tab string, setup ...tea.Msg) func(f *scaleFix) func() {
	return func(f *scaleFix) func() {
		m := scaleModel(f, tab, setup...)
		return func() {
			n, cmd := m.Update(runeKey("j"))
			m = scaleDrain(n.(Model), cmd)
			_ = m.View()
		}
	}
}

var scalePaths = []scalePath{
	{"LogsKeyWords", keyOp("$", runeKey("/"), runeKey("l"), runeKey("a"), runeKey("n"), runeKey("d"), tea.KeyMsg{Type: tea.KeyEnter})},
	{"LogsKey", keyOp("$")},
	{"LogsFrame", func(f *scaleFix) func() {
		m := scaleModel(f, "$")
		return func() { _ = m.View() }
	}},
	{"GraphKeyAny", func(f *scaleFix) func() {
		m := scaleModel(f, "4")
		n, _ := m.Update(loadedOf(f.all)) // s any shows every closed bead: they are loaded
		m = n.(Model)
		g := m.tabAt(tabGraph).(graphTab)
		g.q.status = statusAny
		m.tabs[tabGraph] = g.rebuild()
		return func() {
			n, _ := m.Update(runeKey("j"))
			m = n.(Model)
			_ = m.View()
		}
	}},
	{"GraphKey", keyOp("4")},
	{"FoldLoad", func(f *scaleFix) func() {
		m, msg := scaleModel(f, "4"), loadedOf(f.dflt)
		return func() { m.Update(msg) }
	}},
}

func BenchmarkScaling(b *testing.B) {
	defer func() {
		for _, f := range scaleFixes {
			os.Remove(f.log)
		}
	}()
	for _, p := range scalePaths {
		b.Run(p.name, func(b *testing.B) {
			allocs := map[string]float64{}
			for _, sc := range scales {
				op := p.op(scaleFixture(b, sc))
				op()
				allocs[sc.name] = testing.AllocsPerRun(3, op)
				b.Run(sc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						op()
					}
				})
			}
			today, syn := allocs["today"], allocs["synthetic"]
			b.Logf("allocs/op today %.0f → synthetic %.0f (×%.2f)", today, syn, syn/today)
			if syn > scaleGate*today {
				b.Errorf("allocs/op grew ×%.2f from today's store to the synthetic one; gate ×%.2f", syn/today, scaleGate)
			}
		})
	}
}
