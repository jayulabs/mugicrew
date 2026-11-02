package bd

import (
	"slices"
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// RemnantOf is the Remnant filed for the landed container: the kind:remnant bead
// related to it, any status (found by the related edge, so a re-run converges on the
// one Remnant).
func RemnantOf(containerID string) (Bead, bool, error) {
	found, err := depTargets(containerID, "up", "related")
	if err != nil {
		return Bead{}, false, err
	}
	i := slices.IndexFunc(found, func(b Bead) bool { return b.Kind == contract.KindRemnant })
	if i < 0 {
		return Bead{}, false, nil
	}
	return found[i], true, nil
}

// SpawnRemnantIfAbsent files the Remnant of a landed container unless it already has
// one: an unparented kind:remnant leaf related to the container and, when landingID is
// set, discovered-from that Landing, both edges riding the create (ADR 0401).
func SpawnRemnantIfAbsent(container Bead, title, body, landingID string) (Bead, bool, error) {
	if existing, ok, err := RemnantOf(container.ID); err != nil || ok {
		return existing, false, err
	}
	deps := "related:" + container.ID
	if landingID != "" {
		deps += ",discovered-from:" + landingID
	}
	id, err := run(append(unparentedArgs(Successor{Kind: contract.KindRemnant}, title, body), "--deps", deps)...)
	if err != nil {
		return Bead{}, false, err
	}
	b, err := Show(id)
	return b, true, err
}

// AppendLines adds each of lines to the body of id unless appendMissing finds it held,
// so a re-run appends nothing twice. The body has independent writers (a carrying child,
// jinbe's sweep, an exit), so the read-then-write runs under the land lock as its
// Serializer (ADR 0070 §1), the way a Chart body edit does.
func AppendLines(id string, lines ...string) error {
	return WithLandLock(func(land Serializer) error {
		b, err := Show(id)
		if err != nil {
			return err
		}
		body, changed := appendMissing(b.Description, lines)
		if !changed {
			return nil
		}
		return SetDescription(id, body, land)
	})
}

// keyedPrefixes are the Remnant body line prefixes keyed by the child they name: the
// first word after the prefix is the key. For a Child: line that is the child's id; a
// Kept: line leads with its effort/<id> branch, which stands in for the id (every Kept:
// branch is effort/<child id>).
var keyedPrefixes = []string{keptPrefix, childPrefix}

// childKey is the prefix and key (see keyedPrefixes) a keyed line names, ok false for any other line.
func childKey(line string) (prefix, key string, ok bool) {
	for _, p := range keyedPrefixes {
		if rest, found := strings.CutPrefix(line, p); found {
			w := FirstWord(rest)
			return p, w, w != ""
		}
	}
	return "", "", false
}

// appendMissing is body with each of lines appended on its own line unless body already
// holds it. A Kept: or Child: line is skipped when body already holds a line with the
// same prefix naming the same child, whatever the rest says: the first-written line is
// what a crashed run acted on, so it is never replaced.
func appendMissing(body string, lines []string) (string, bool) {
	have := strings.Split(body, "\n")
	changed := false
	for _, l := range lines {
		if slices.Contains(have, l) {
			continue
		}
		if p, c, ok := childKey(l); ok && slices.ContainsFunc(have, func(h string) bool {
			hp, hc, hok := childKey(h)
			return hok && hp == p && hc == c
		}) {
			continue
		}
		body = strings.TrimRight(body, "\n") + "\n" + l
		have = append(have, l)
		changed = true
	}
	return body, changed
}

// RemnantLanded is the landed Checkpoint a Remnant is related to.
func RemnantLanded(remnantID string) (string, bool, error) {
	return kindAlong(remnantID, "down", "related", contract.KindCheckpoint)
}

// RenumberCheckpointsOf is the Checkpoints caused-by the landed container, any status:
// the Renumber Checkpoints its land minted (an ordinary Checkpoint is caused-by a Spec).
func RenumberCheckpointsOf(containerID string) ([]Bead, error) {
	found, err := depTargets(containerID, "up", "caused-by")
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(found, func(b Bead) bool { return b.Kind != contract.KindCheckpoint }), nil
}

// NewRemnantOffshoot files the offshoot Signal an offshoot exit of a Remnant records:
// an open unparented kind:signal discovered-from the Remnant (the exit is a human's
// act, so it is no Draft).
func NewRemnantOffshoot(title, body, remnantID string) (string, error) {
	args := unparentedArgs(Successor{Kind: contract.KindSignal}, title, body)
	return run(append(args, "--deps", "discovered-from:"+remnantID)...)
}

// RemnantOffshootTitled is the Signal titled title already discovered-from the Remnant
// (any status), so a re-run of its offshoot exit reuses the one it filed.
func RemnantOffshootTitled(remnantID, title string) (string, bool, error) {
	found, err := SignalsFrom(remnantID)
	if err != nil {
		return "", false, err
	}
	id, ok := signalTitled(found, stripTitle(title, "kind", string(contract.KindSignal)))
	return id, ok, nil
}

// SupersededBy is the member old already supersedes ("" when none), so a respin re-run
// finds the member it filed for old before its adoption leg.
func SupersededBy(old string) (string, error) {
	found, err := depTargets(old, "down", "supersedes")
	if err != nil || len(found) == 0 {
		return "", err
	}
	return found[0].ID, nil
}
