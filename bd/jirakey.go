package bd

import (
	"strings"

	"jayulabs.org/mugicrew/contract"
)

// JiraRefPrefix marks a bead that mirrors a Jira work item (mugicrew intake jira).
const JiraRefPrefix = "jira-"

// JiraPrefixOf is the "<KEY>: " a Land MR title takes when container (a Chart or
// Checkpoint) traces to a Jira work item, else "". Computed per Landing, never stored.
func JiraPrefixOf(container Bead) (string, error) { return jiraPrefixOf(container, CausedBy, Show) }

// jiraPrefixOf walks container, then its caused-by predecessors (Checkpoint to Spec
// to Chart to Signal, as far as each edge exists), then its parent if that is a
// kind:initiative; the first jira- external ref wins. Reads are injected for table tests.
func jiraPrefixOf(container Bead, causedBy func(string, contract.Kind) (string, bool, error), show func(string) (Bead, error)) (string, error) {
	key := func(b Bead) (string, bool) {
		k, ok := strings.CutPrefix(b.ExternalRef, JiraRefPrefix)
		if !ok {
			return "", false
		}
		return k + ": ", true
	}
	seen := map[string]bool{}
	for b := container; !seen[b.ID]; {
		seen[b.ID] = true
		if k, ok := key(b); ok {
			return k, nil
		}
		next := ""
		for _, kind := range []contract.Kind{contract.KindSpec, contract.KindChart, contract.KindSignal} {
			id, ok, err := causedBy(b.ID, kind)
			if err != nil {
				return "", err
			}
			if ok {
				next = id
				break
			}
		}
		if next == "" {
			break
		}
		var err error
		if b, err = show(next); err != nil {
			return "", err
		}
	}
	if container.Parent == "" {
		return "", nil
	}
	p, err := show(container.Parent)
	if err != nil || p.Kind != contract.KindInitiative {
		return "", err
	}
	if k, ok := key(p); ok {
		return k, nil
	}
	return "", nil
}
