package loop

import (
	"errors"
	"sync"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Seams under the live adapter's reads, so the loop's tests run with no live bd/git. The
// adapter itself is a var (newLiveView) so a test can swap in an in-memory View.
var (
	listChildren = bd.ListChildren
	landedBase   = git.LandedBase
	headOf       = git.Head
	openBlockers = bd.OpenBlockers
)

// LiveReads are the raw bd/git reads under the live adapter. NewLiveViewOver takes them so a
// test can run the real adapter over stubs; NewLiveView fills them with the package's own.
type LiveReads struct {
	ListChildren func(id string) ([]bd.Bead, error)
	LandedBase   func(trailer string) (string, error)
	Head         func(ref string) (string, error)
	OpenBlockers func(id string) ([]bd.Bead, error)
	Show         func(id string) (bd.Bead, error)
	EffortRefs   func() (*git.EffortSet, error)
}

// seamReads reads through the package seam vars at call time, so a test swapping one still
// reaches the adapter.
func seamReads() LiveReads {
	return LiveReads{
		ListChildren: func(id string) ([]bd.Bead, error) { return listChildren(id) },
		LandedBase:   func(t string) (string, error) { return landedBase(t) },
		Head:         func(r string) (string, error) { return headOf(r) },
		OpenBlockers: func(id string) ([]bd.Bead, error) { return openBlockers(id) },
		Show:         func(id string) (bd.Bead, error) { return showBead(id) },
		EffortRefs:   func() (*git.EffortSet, error) { return effortRefsAcross() },
	}
}

// NewLiveViewOver is the live View over the given reads, for a test that checks it against
// another adapter.
func NewLiveViewOver(r LiveReads, efforts func() *git.EffortSet, seed ...[]bd.Bead) View {
	v := newLive(efforts, seed...)
	v.reads = r
	return v
}

// newLiveView builds the tick's live View over bd/git. seed pre-loads beads the caller has
// already read, so Bead(id) answers them without a bd show.
var newLiveView = func(efforts func() *git.EffortSet, seed ...[]bd.Bead) View {
	return newLive(efforts, seed...)
}

// NewLiveView is the live View for a caller outside the loop (the TUI's run-now press),
// built once per press with its own memoised effort set.
func NewLiveView(efforts func() *git.EffortSet, seed ...[]bd.Bead) View {
	return newLive(efforts, seed...)
}

// liveView is the View over bd/git, built once per loop tick (or per press). It memoises
// Children, Landed, CheckpointTip and Blockers by id, and the effort set, for its lifetime.
type liveView struct {
	effortsFn func() *git.EffortSet
	reads     LiveReads

	mu       sync.Mutex
	efforts  *git.EffortSet
	children map[string][]bd.Bead
	landed   map[string]Trailer
	tips     map[string]string
	beads    map[string]bd.Bead
	blockers map[string][]Blocker
}

func newLive(efforts func() *git.EffortSet, seed ...[]bd.Bead) *liveView {
	v := &liveView{
		effortsFn: efforts,
		reads:     seamReads(),
		children:  map[string][]bd.Bead{},
		landed:    map[string]Trailer{},
		tips:      map[string]string{},
		beads:     map[string]bd.Bead{},
		blockers:  map[string][]Blocker{},
	}
	for _, bs := range seed {
		for _, b := range bs {
			v.beads[b.ID] = b
		}
	}
	return v
}

func (v *liveView) Children(id string) ([]bd.Bead, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if kids, ok := v.children[id]; ok {
		return kids, nil
	}
	kids, err := v.reads.ListChildren(id)
	if err != nil {
		return nil, err
	}
	v.children[id] = kids
	return kids, nil
}

// Efforts is the supplied set, called once; a nil set (a failed memo read) is read afresh
// here so the failure surfaces as an error rather than an empty set.
func (v *liveView) Efforts() (*git.EffortSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.efforts != nil {
		return v.efforts, nil
	}
	var set *git.EffortSet
	if v.effortsFn != nil {
		set = v.effortsFn()
	}
	if set == nil {
		var err error
		if set, err = v.reads.EffortRefs(); err != nil {
			return nil, err
		}
		if set == nil {
			set = git.NewEffortSet()
		}
	}
	v.efforts = set
	return set, nil
}

// Landed reads b's land trailer (Chart or Checkpoint) through the strategy seam; a
// foreign land comes back as data (ADR 0187), any other error as an error.
func (v *liveView) Landed(b bd.Bead) (Trailer, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.beads[b.ID] = b
	if t, ok := v.landed[b.ID]; ok {
		return t, nil
	}
	sha, landed, err := contexts.Landed(b, git.TrailerFor(string(b.Kind), b.ID), v.reads.LandedBase)
	var foreign *git.ForeignLanding
	switch {
	case errors.As(err, &foreign):
		err = nil
	case err != nil:
		return Trailer{}, err
	}
	t := Trailer{Sha: sha, Landed: landed, Foreign: foreign}
	v.landed[b.ID] = t
	return t, nil
}

// CheckpointTip is the checkpoint/<id> tip in the Checkpoint's repo; a failed head read
// is "" (it pins nothing), as ApprovedAtHead reads it.
func (v *liveView) CheckpointTip(id string) (string, error) {
	v.mu.Lock()
	cp, known := v.beads[id]
	tip, done := v.tips[id]
	v.mu.Unlock()
	if done {
		return tip, nil
	}
	if !known {
		var err error
		if cp, err = v.Bead(id); err != nil {
			return "", err
		}
	}
	tip, err := contexts.Head(cp, git.BranchName(string(contract.KindCheckpoint), id), v.reads.Head)
	if err != nil {
		tip = ""
	}
	v.mu.Lock()
	v.tips[id] = tip
	v.mu.Unlock()
	return tip, nil
}

func (v *liveView) Blockers(b bd.Bead) ([]Blocker, error) {
	v.mu.Lock()
	out, ok := v.blockers[b.ID]
	v.mu.Unlock()
	if ok {
		return out, nil
	}
	open, err := v.reads.OpenBlockers(b.ID)
	if err != nil {
		return nil, err
	}
	out = make([]Blocker, len(open))
	for i, x := range open {
		out[i] = Blocker{Bead: x, Edge: EdgeBlocks}
	}
	v.mu.Lock()
	v.blockers[b.ID] = out
	v.mu.Unlock()
	return out, nil
}

func (v *liveView) Bead(id string) (bd.Bead, error) {
	v.mu.Lock()
	b, ok := v.beads[id]
	v.mu.Unlock()
	if ok {
		return b, nil
	}
	b, err := v.reads.Show(id)
	if err != nil {
		return bd.Bead{}, err
	}
	v.mu.Lock()
	v.beads[id] = b
	v.mu.Unlock()
	return b, nil
}
