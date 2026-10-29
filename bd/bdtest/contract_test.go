package bdtest

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
)

// TestFakeContract runs the contract suite against the fake; its leg always runs.
func TestFakeContract(t *testing.T) {
	var f *Fake
	Contract(t, func(t *testing.T) bd.Core { f = New(t); return f },
		func(t *testing.T, id, status, assignee string) {
			f.Mutate(id, func(b *bd.Bead) { b.Status, b.Assignee = status, assignee })
		})
}
