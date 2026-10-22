package bd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// TaskFiling is one Task to file through NewTask: what the author wrote (Title, Body),
// the filing crewmate's bead, and the beads it is ordered against.
type TaskFiling struct {
	Title, Body, DraftedBy string
	After                  []string // the Task is blocked by each: after a land, or after another Task
	Before                 []string // each is blocked by the Task: before a land
	Outside                bool     // keep the Task outside any Initiative
}

// NewTask files a Task as DraftedBy's Draft in one atomic create (ADR 0350): a direct
// child of the Initiative of the member it is ordered against, or born outside with a
// stated reason (ADR 0348). It returns the new id and the placement line, which is also
// the body's first fact line. A refused order edge returns bd's error and files nothing:
// there is no unordered retry.
func NewTask(f TaskFiling) (id, line string, err error) {
	parent, line, err := taskPlacement(f)
	if err != nil {
		return "", "", err
	}
	deps := []string{"discovered-from:" + f.DraftedBy}
	for _, a := range f.After {
		deps = append(deps, "blocked-by:"+a)
	}
	for _, b := range f.Before {
		deps = append(deps, "blocks:"+b)
	}
	s := Successor{Kind: contract.KindTask, DraftedBy: f.DraftedBy, Parent: parent, Title: stripTitle(f.Title, "kind", string(contract.KindTask)), Description: withFactLine(f.Body, line)}
	id, err = run(successorCreateArgs("", s, deps...)...)
	if err != nil {
		return "", "", err
	}
	return id, line, nil
}

// taskPlacement is the Initiative a Task joins ("" when born outside) and the placement
// line saying where it went. Each order target climbs to the Initiative its member sits
// in; exactly one open Initiative across them joins (a closed one is not a second
// Initiative), else the first reason below applies.
func taskPlacement(f TaskFiling) (parent, line string, err error) {
	outside := func(reason string) (string, string, error) { return "", "Initiative: outside (" + reason + ")", nil }
	if f.Outside {
		return outside("kept outside")
	}
	targets := slices.Concat(f.After, f.Before)
	if len(targets) == 0 {
		return outside("unordered")
	}
	var inits, open []Bead
	var bare string // the title of the first target with no Initiative
	for _, id := range targets {
		init, orphan, err := initiativeOfTarget(id)
		if err != nil {
			return "", "", err
		}
		switch {
		case init.ID == "":
			if bare == "" {
				bare = orphan
			}
		case !slices.ContainsFunc(inits, func(b Bead) bool { return b.ID == init.ID }):
			inits = append(inits, init)
			if init.Unclosed() {
				open = append(open, init)
			}
		}
	}
	if len(open) > 1 {
		return outside(fmt.Sprintf("ordered against members of %s and %s", open[0].Title, open[1].Title))
	}
	var init Bead
	switch {
	case len(open) == 1:
		init = open[0]
	case len(inits) > 0:
		init = inits[0]
	}
	parent, line = InitiativePlacement(init, bare)
	return parent, line, nil
}

// InitiativePlacement is the one place a climbed Initiative becomes a filing's parent and
// placement line: init joins when open, else the bead is born outside with the reason. A
// zero init means the climb found none; orphan is the title to name then.
func InitiativePlacement(init Bead, orphan string) (parent, line string) {
	switch {
	case init.ID == "":
		return "", "Initiative: outside (" + orphan + " has no Initiative)"
	case !init.Unclosed():
		return "", "Initiative: outside (" + init.Title + " is closed)"
	}
	return init.ID, "Initiative: " + init.Title
}

// initiativeOfTarget is the Initiative of the member id, any status: its parent when that
// is a kind:initiative. A Rollback climbs through the Checkpoint it undoes first. With no
// Initiative it returns the title to name in the reason.
func initiativeOfTarget(id string) (init Bead, title string, err error) {
	b, err := Show(id)
	if err != nil {
		return Bead{}, "", fmt.Errorf("order target %s: %w", id, err)
	}
	member := b
	if b.Kind == contract.KindRollback {
		cp, ok, err := RollbackTarget(b.ID)
		if err != nil {
			return Bead{}, "", err
		}
		if !ok {
			return Bead{}, b.Title, nil
		}
		if member, err = Show(cp); err != nil {
			return Bead{}, "", fmt.Errorf("order target %s: %w", cp, err)
		}
	}
	p, err := ParentInitiative(member, Show)
	if err != nil {
		return Bead{}, "", err
	}
	if p.ID == "" {
		return Bead{}, b.Title, nil
	}
	return p, "", nil
}

// ParentInitiative is the Initiative member is a direct child of, any status, or a zero Bead
// when its parent is absent or not a kind:initiative. show reads the parent.
func ParentInitiative(member Bead, show func(string) (Bead, error)) (Bead, error) {
	if member.Parent == "" {
		return Bead{}, nil
	}
	p, err := show(member.Parent)
	if err != nil || p.Kind != contract.KindInitiative {
		return Bead{}, err
	}
	return p, nil
}

// factLineRe is a whole "Key: value" fact line as TaskBody writes them.
var factLineRe = regexp.MustCompile(`^[A-Z][A-Za-z() -]{0,30}: \S`)

// withFactLine inserts fact as the body's first fact line: right after its lead (the
// first line, plus any more up to a blank line or a fact line), before ## Steps. A body
// whose first line is already a fact line has no lead.
func withFactLine(body, fact string) string {
	lines := strings.Split(body, "\n")
	end := slices.Index(lines, "## Steps")
	if end < 0 {
		end = len(lines)
	}
	i := 0
	if end > 0 && !factLineRe.MatchString(lines[0]) {
		for i = 1; i < end && lines[i] != "" && !factLineRe.MatchString(lines[i]); i++ {
		}
	}
	ins := []string{fact}
	if i == end && end < len(lines) { // lands flush against ## Steps: keep the blank line before it
		ins = append(ins, "")
	}
	return strings.Join(slices.Insert(lines, i, ins...), "\n")
}
