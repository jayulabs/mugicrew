// Package registry is the static kind → {owner, mode, container} map — the
// total owner-from-kind function and the AFK/HITL source. Nothing is derived or
// stored elsewhere (spec §4.2).
package registry

import (
	"fmt"

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
	Owner     string // the crewmate that owns the kind
	Mode      Mode   // afk → Run, hitl → surface + Launch
	Container bool   // container kind (bd epic) vs leaf (bd task)
}

// table IS the owner-from-kind function. Nami's, Zoro's, and the Checkpoint tail
// (Jinbe/Sanji/Franky/Usopp/Luffy) kinds are wired; other crewmates' kinds stay
// omitted (YAGNI). A tail kind's owner has no crew() entry yet, so the loop skips
// it and the bead sits open until its package lands.
var table = map[contract.Kind]Entry{
	contract.KindSignal:        {Owner: "nami", Mode: HITL, Container: false},
	contract.KindGrilling:      {Owner: "nami", Mode: HITL, Container: false},
	contract.KindResearch:      {Owner: "nami", Mode: AFK, Container: false},
	contract.KindChart:         {Owner: "nami", Mode: AFK, Container: true},
	contract.KindChartConflict: {Owner: "nami", Mode: AFK, Container: false},  // ADR 0011/0030, 0079/0080: Chart-land conflict, resolved by Nami's AFK arm
	contract.KindSpec:          {Owner: "zoro", Mode: AFK, Container: false},  // FR1: an open Spec is an AFK leaf Zoro reconciles to a Checkpoint skeleton
	contract.KindPivot:         {Owner: "zoro", Mode: HITL, Container: false}, // ADR 0043 (amends ADR 0005 §6): Inbox-surfaced, never dispatched AFK (FR26)

	// Checkpoint tail, landed ahead of the owning packages — the loop
	// skips a kind whose owner has no crew() entry, so a laid tail bead sits open.
	contract.KindCheckpoint: {Owner: "jinbe", Mode: AFK, Container: true},   // the Checkpoint handler (container)
	contract.KindPrototype:  {Owner: "sanji", Mode: HITL, Container: false}, // Inbox HITL defog kind (§9.2)
	contract.KindBuild:      {Owner: "franky", Mode: AFK, Container: false},
	contract.KindCritique:   {Owner: "franky", Mode: AFK, Container: false},
	contract.KindConflict:   {Owner: "franky", Mode: AFK, Container: false},
	contract.KindReview:     {Owner: "usopp", Mode: AFK, Container: false},
	contract.KindApproval:   {Owner: "luffy", Mode: HITL, Container: false},

	contract.KindRollback: {Owner: "jinbe", Mode: AFK, Container: false},   // ADR 0047: human-filed, Jinbe's first leaf kind (amends ADR 0002)
	contract.KindSurgery:  {Owner: "franky", Mode: HITL, Container: false}, // ADR 0092: HITL, so an open Surgery is never an AFK loop or run-now candidate
	contract.KindTask:     {Owner: "brook", Mode: HITL, Container: false},  // ADR 0115: only a human launches a Task, so the loop never does
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
