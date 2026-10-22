// Package registry is the static kind → {owner, mode, container} map — the
// total owner-from-kind function and the AFK/HITL source. Nothing is derived or
// stored elsewhere (spec §4.2), with one live exception: ModeOf derives an
// Approval's mode from the Workspace auto-approve snapshot and the bead's
// auto-approve:failed label — AFK while the snapshot is on and the label is
// absent (ADR 0346, mugicrew-an-approval-is-afk-while…; fallback to HITL per
// ADR 0341, mugicrew-a-failing-afk-approval-falls-back…).
package registry

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"jayulabs.org/mugicrew/contract"
)

// Mode picks a bead's dispatch path: afk work runs via Crewmate.Run (the loop);
// hitl work is surfaced and opened via Crewmate.Launch (the TUI / triage).
type Mode string

const (
	AFK  Mode = "afk"
	HITL Mode = "hitl"
)

// Entry is the routing facts for a kind (spec §4.2).
type Entry struct {
	Owner      string // the crewmate that owns the kind
	Mode       Mode   // afk → Run, hitl → surface + Launch
	Container  bool   // container kind (bd epic) vs leaf (bd task)
	ReadOnly   bool   // launch and resume drop the edit tools (ADR 0191); true only for discussion
	Mechanical bool   // runs no harness session (Run/Launch never reach ctx.Harness); shown as a — row in Settings Kind profiles and can't be edited
}

// table IS the owner-from-kind function. Luffy's intake Signal, Nami's, Zoro's,
// and the Checkpoint tail (Jinbe/Sanji/Franky/Usopp/Luffy) kinds are wired; other
// crewmates' kinds stay omitted (YAGNI). A tail kind's owner has no crew() entry yet, so the loop skips
// it and the bead sits open until its package lands.
var table = map[contract.Kind]Entry{
	contract.KindSignal:        {Owner: "luffy", Mode: HITL, Container: false}, // ADR 0129: Luffy triages; HITL, so the loop never dispatches a Signal (ADR 0006)
	contract.KindGrilling:      {Owner: "nami", Mode: HITL, Container: false},
	contract.KindResearch:      {Owner: "nami", Mode: AFK, Container: false},
	contract.KindChart:         {Owner: "nami", Mode: AFK, Container: true},
	contract.KindSurvey:        {Owner: "nami", Mode: HITL, Container: false}, // the Chart's charting leaf: a human launches it, never the loop
	contract.KindChartConflict: {Owner: "nami", Mode: AFK, Container: false},  // ADR 0011/0030, 0079/0080: Chart-land conflict, resolved by Nami's AFK arm
	contract.KindSpec:          {Owner: "zoro", Mode: AFK, Container: false},  // FR1: an open Spec is an AFK leaf Zoro reconciles to a Checkpoint skeleton
	contract.KindPivot:         {Owner: "zoro", Mode: HITL, Container: false}, // ADR 0043 (amends ADR 0005 §6): Inbox-surfaced, never dispatched AFK (FR26)

	// Checkpoint tail, landed ahead of the owning packages — the loop
	// skips a kind whose owner has no crew() entry, so a laid tail bead sits open.
	contract.KindCheckpoint:  {Owner: "jinbe", Mode: AFK, Container: true, Mechanical: true}, // the Checkpoint handler (container)
	contract.KindPrototype:   {Owner: "sanji", Mode: HITL, Container: false},                 // Inbox HITL defog kind (§9.2)
	contract.KindBuild:       {Owner: "franky", Mode: AFK, Container: false},
	contract.KindCritique:    {Owner: "franky", Mode: AFK, Container: false},
	contract.KindConflict:    {Owner: "franky", Mode: AFK, Container: false},
	contract.KindReview:      {Owner: "usopp", Mode: AFK, Container: false},
	contract.KindForgeReview: {Owner: "usopp", Mode: AFK, Container: false},  // copies Review's row: Usopp's Forge Review pass over a container's MR notes
	contract.KindApproval:    {Owner: "luffy", Mode: HITL, Container: false}, // HITL is the setting-off default; ModeOf derives AFK live (ADR 0346)

	contract.KindRollback:   {Owner: "jinbe", Mode: AFK, Container: false, Mechanical: true}, // ADR 0047: human-filed, Jinbe's first leaf kind (amends ADR 0002)
	contract.KindLanding:    {Owner: "jinbe", Mode: AFK, Container: false, Mechanical: true}, // ADR 0145/0147: lands its parent container; copies Rollback's row
	contract.KindMRWatch:    {Owner: "jinbe", Mode: AFK, Container: false, Mechanical: true}, // Spec D4: Jinbe's every-tick MR read; copies Landing's row
	contract.KindLatch:      {Owner: "brook", Mode: AFK, Container: false, Mechanical: true}, // Spec mc-8ymw.9 D1: Brook's in-process Jira check; copies Landing's row
	contract.KindSurgery:    {Owner: "franky", Mode: HITL, Container: false},                 // ADR 0092: HITL, so an open Surgery is never an AFK loop or run-now candidate
	contract.KindTask:       {Owner: "brook", Mode: HITL, Container: false},                  // ADR 0115: only a human launches a Task, so the loop never does
	contract.KindDiscussion: {Owner: "robin", Mode: HITL, Container: false, ReadOnly: true},  // ADR 0194: a human-started Robin session, so the loop never dispatches it
	contract.KindCheckup:    {Owner: "chopper", Mode: HITL, Container: false},                // ADR 0272: a human-started Chopper session (bd edits, so not ReadOnly), so the loop never dispatches it
	contract.KindInitiative: {Owner: "luffy", Mode: HITL, Container: true},                   // ADR 0128/0135: branchless container with no effort, so never a loop candidate nor a pool slot
}

// Lookup resolves a kind to its routing entry. An unknown kind is a loud error
// — a defensive assertion; the loop's scope fence (spec §6.1) means it should
// never fire in the MVP.
func Lookup(kind contract.Kind) (Entry, error) {
	e, ok := table[kind]
	if !ok {
		return Entry{}, fmt.Errorf("registry: unknown kind %q has no owner", kind)
	}
	return e, nil
}

// AutoApproveFailedLabel is the sticky fallback marker on an Approval whose AFK
// session failed three times in a row (ADR 0341): it makes the Approval HITL again.
const AutoApproveFailedLabel = "auto-approve:failed"

// autoApprove is the Workspace auto-approve snapshot, stamped by the loop at the
// start of every tick (as git.LandStrategyName is wired from loop config), so
// registry never imports cfglayer. Off until set.
var autoApprove atomic.Bool

// SetAutoApprove stamps the auto-approve snapshot ModeOf reads.
func SetAutoApprove(on bool) { autoApprove.Store(on) }

// ResetAutoApprove turns the snapshot off; tests call it in Cleanup.
func ResetAutoApprove() { autoApprove.Store(false) }

// ModeOf is a bead's dispatch mode: Lookup's static Mode, except an Approval is
// AFK while the auto-approve snapshot is on and labels lack auto-approve:failed
// (ADR 0346). An unknown kind reads HITL, so nothing unowned is dispatched.
func ModeOf(kind contract.Kind, labels []string) Mode {
	e, err := Lookup(kind)
	if err != nil {
		return HITL
	}
	if kind == contract.KindApproval && autoApprove.Load() && !slices.Contains(labels, AutoApproveFailedLabel) {
		return AFK
	}
	return e.Mode
}

// IsMode reports whether a bead's ModeOf is mode; an unknown kind is in no mode
// (not even HITL), so callers need no separate Lookup guard.
func IsMode(kind contract.Kind, labels []string, mode Mode) bool {
	if _, err := Lookup(kind); err != nil {
		return false
	}
	return ModeOf(kind, labels) == mode
}

// SessionKinds is every non-Mechanical kind, sorted: the kinds whose sessions
// launch a harness, so the Settings Kind profiles rows (ADR 0136). A new
// registry row is listed unless it is deliberately marked Mechanical.
// Computed once; the slice is shared, so callers must not modify it.
var SessionKinds = sync.OnceValue(func() []contract.Kind {
	var ks []contract.Kind
	for k, e := range table {
		if !e.Mechanical {
			ks = append(ks, k)
		}
	}
	slices.Sort(ks)
	return ks
})

// AllKinds is every registry kind, sorted: the Settings Kind profiles section
// draws a — row for each Mechanical one (ADR 0136 §1). Computed once; the slice
// is shared, so callers must not modify it.
var AllKinds = sync.OnceValue(func() []contract.Kind {
	ks := make([]contract.Kind, 0, len(table))
	for k := range table {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
})
