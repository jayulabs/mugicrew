package bd

import (
	"fmt"
	"strconv"
	"strings"

	"jayulabs.org/mugicrew/git"
)

// Core is the bead-level primitive core: the one test seam over the bd CLI (ADR 0380,
// mugicrew-the-bd-core-types-its-write-class…, and the test-seam ADR). Its signature is
// its ADR 0070 (mugicrew-one-bd-write-rule) write class:
//   - a read takes no class;
//   - a commutative or idempotent write takes none either (AddEdge, AddLabels, AddComment, Close);
//   - a single insert takes none (Create), or the Serializer of its read-then-create (ADR 0413);
//   - a CAS write takes its guard (Claim, Unclaim);
//   - a read-then-write takes the Serializer it runs under (SetField, Delete, Reopen);
//   - Batch is atomic.
//
// The process default is the unexported cli; bdtest installs an in-memory one through Use.
type Core interface {
	Show(id string) (Bead, error)
	List(q Query) ([]Bead, error)
	Ready() ([]Bead, error)
	Edges(id string, dir Dir, typ string) ([]EdgeRow, error)
	Comments(id string) ([]Comment, error)

	AddEdge(from, to, typ string) error
	RemoveEdge(from, to string) error
	Supersede(id, with string) error
	Create(spec CreateSpec) (string, error)
	AddLabels(id string, labels ...string) error
	RemoveLabel(ids []string, label string) error
	Claim(id string, g IfStatus) error
	Unclaim(id string, g IfAssignee) error
	Close(ids []string, reason string, force bool) error
	Reopen(id string, s Serializer) error
	Delete(ids []string, s Serializer) error
	AddComment(id, text string) error
	SetField(id string, f Field, value string, s Serializer) error
	Batch(ops []Op) error
}

// Query is a list read. The zero Query is bd's default status set (every non-closed
// status) with no cap: Limit 0 is unlimited (-n 0), a positive Limit is passed through.
type Query struct {
	Status       []string // empty: bd's default non-closed set
	All          bool     // every status, closed included
	IDs          []string
	Parent       string
	Label        string
	LabelPattern string
	ExternalRef  string
	Limit        int
}

// Dir is the direction of an Edges read: Down lists what id depends on, Up what depends on id.
type Dir string

const (
	Down Dir = "down"
	Up   Dir = "up"
)

// EdgeRow is the bead at the far end of an edge plus the edge's dependency type.
type EdgeRow struct {
	Bead
	Type string
}

// IfStatus is a CAS guard on a bead's current status.
type IfStatus string

// IfAssignee is a CAS guard on a bead's current assignee.
type IfAssignee string

// Mine is the guard "still claimed by this process's bd actor".
func Mine() IfAssignee { return IfAssignee(actor()) }

// Field is a whole-field write: last-write-wins unless serialized.
type Field string

const (
	Description Field = "description"
	Metadata    Field = "metadata"
	ParentField Field = "parent"
	TitleField  Field = "title"
	// Priority has no production caller: the TUI's +/- edit and Luffy's triage run
	// bd update --priority directly, and the Forge Review tail no longer writes it
	// (ADR 0410 (mugicrew-effective-priority-is-the-most-urgent…)); only bdtest's contract
	// uses it. A successor's birth level rides CreateSpec.Priority instead.
	Priority Field = "priority"
)

// CreateSpec is one bead Create inserts.
type CreateSpec struct {
	Type        string // bd issue type: task, epic
	Title       string
	Description string
	Parent      string
	Labels      []string
	Deps        []Edge // edges from the new bead: From is empty, To and Type name the target; Type "" is blocks
	ExternalRef string
	Deferred    bool       // born a Draft
	NoInherit   bool       // --no-inherit-labels
	Priority    *int       // bd priority 0–4 at birth; nil is unset, so bd's default (P2) applies, apart from a real P0
	Serializer  Serializer // a read-then-create's exclusion (ADR 0070 §1); nil is a plain single insert
}

// depArg renders a Dep as one bd create --deps entry. bd's --deps reads blocks:X reversed (X
// blocks the new bead), so a blocks Dep is blocked-by:<to>; every other type is <type>:<to>.
func depArg(d Edge) (string, error) {
	if d.From != "" {
		return "", fmt.Errorf("create dep %s -> %s: From must be empty, the edge starts at the new bead", d.From, d.To)
	}
	switch d.Type {
	case "", "blocks":
		return "blocked-by:" + d.To, nil
	}
	return d.Type + ":" + d.To, nil
}

// Op is one line of an atomic Batch.
type Op struct {
	Verb                     string // "dep add", "open", "close"
	ID, To, Type, Title, Why string
}

func EdgeOp(from, to, typ string) Op { return Op{Verb: "dep add", ID: from, To: to, Type: typ} }
func OpenOp(id, title string) Op     { return Op{Verb: "open", ID: id, Title: title} }
func CloseOp(id, reason string) Op   { return Op{Verb: "close", ID: id, Why: reason} }

// Serializer names what makes a read-then-write exclusive (ADR 0070 §1). The real core
// ignores it; the bdtest fake checks it. It is sealed to three kinds (ADR 0380).
type Serializer interface{ serializer() }

// ClaimSer is "under the claim on ID": the predecessor's, container's or creator's.
type ClaimSer struct{ ID string }

func (ClaimSer) serializer() {}

// UnderClaim is the serializer "under the claim on id".
func UnderClaim(id string) Serializer { return ClaimSer{id} }

// LockSer is a lock token, live only inside the With… call that minted it.
type LockSer struct {
	Name string
	live *bool
}

func (LockSer) serializer() {}

// Live is whether the token's lock is still held.
func (l LockSer) Live() bool { return l.live != nil && *l.live }

// WithLandLock runs fn under git's land lock, handing it the lock as a Serializer.
func WithLandLock(fn func(land Serializer) error) error {
	return git.WithLandLock(func() error {
		live := true
		defer func() { live = false }()
		return fn(LockSer{"land", &live})
	})
}

// HoldSer is an effort's dispatcher hold (ADR 0154, mugicrew-the-dispatcher-holds…): live
// until that hold is released.
type HoldSer struct {
	Name string
	live func() bool
}

func (HoldSer) serializer() {}

// Live is whether the hold is still held.
func (h HoldSer) Live() bool { return h.live != nil && h.live() }

// UnderHold is the serializer "under the hold name", live while live reports true.
func UnderHold(name string, live func() bool) Serializer { return HoldSer{name, live} }

// core is the process default; Use swaps it.
var core Core = cli{}

// sealed refuses every bd CLI exec while a non-cli core is the default: a helper not yet on
// the core must fail its test, never reach the live graph.
var sealed bool

// Use makes c the process default until restore runs. Any core but the cli seals the CLI.
func Use(c Core) (restore func()) {
	old, oldSealed := core, sealed
	core, sealed = c, c != Core(cli{})
	return func() { core, sealed = old, oldSealed }
}

// errSealed is the refusal of a bd exec while the CLI is sealed.
func errSealed(argv ...string) error {
	return fmt.Errorf("bd %s: refused: the bd CLI is sealed while a non-cli Core is installed (bdtest); route this helper through bd.Core", strings.Join(argv, " "))
}

// cli is the real core: today's bd CLI calls behind the typed methods.
type cli struct{}

func (cli) Show(id string) (Bead, error) {
	out, err := run("show", id, "--json")
	if err != nil {
		return Bead{}, err
	}
	return parseShow([]byte(out))
}

func (cli) List(q Query) ([]Bead, error) {
	if q.Parent != "" && q.All && q.Limit == 0 && len(q.Status) == 0 && len(q.IDs) == 0 && q.Label == "" && q.LabelPattern == "" && q.ExternalRef == "" {
		out, err := run("children", q.Parent, "--json") // the same rows as list --parent --all -n 0, pinned by the contract suite
		if err != nil {
			return nil, err
		}
		return parseBeads([]byte(out))
	}
	args := []string{"list"}
	if q.All {
		args = append(args, "--all")
	}
	args = append(args, "-n", strconv.Itoa(max(q.Limit, 0)))
	if len(q.IDs) > 0 {
		args = append(args, "--id", strings.Join(q.IDs, ","))
	}
	if q.Parent != "" {
		args = append(args, "--parent", q.Parent)
	}
	if q.Label != "" {
		args = append(args, "--label", q.Label)
	}
	if q.LabelPattern != "" {
		args = append(args, "--label-pattern", q.LabelPattern)
	}
	if q.ExternalRef != "" {
		args = append(args, "--external-ref", q.ExternalRef)
	}
	if len(q.Status) > 0 {
		args = append(args, "--status", strings.Join(q.Status, ","))
	}
	out, err := run(append(args, "--json")...)
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

func (cli) Ready() ([]Bead, error) {
	out, err := run("ready", "-n", "0", "--json")
	if err != nil {
		return nil, err
	}
	return parseBeads([]byte(out))
}

func (cli) Edges(id string, dir Dir, typ string) ([]EdgeRow, error) {
	args := []string{"dep", "list", id, "--direction=" + string(dir), "--json"}
	if typ != "" {
		args = append(args, "-t", typ)
	}
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	beads, types, err := parseBeadRows([]byte(out))
	if err != nil {
		return nil, err
	}
	rows := make([]EdgeRow, len(beads))
	for i, b := range beads {
		rows[i] = EdgeRow{Bead: b, Type: types[i]}
	}
	return rows, nil
}

func (cli) Comments(id string) ([]Comment, error) {
	out, err := run("comments", id, "--json")
	if err != nil {
		return nil, err
	}
	return parseComments([]byte(out))
}

func (cli) AddEdge(from, to, typ string) error {
	_, err := run("dep", "add", from, to, "-t", typ)
	return err
}

func (cli) RemoveEdge(from, to string) error {
	_, err := run("dep", "remove", from, to)
	return err
}

func (cli) Supersede(id, with string) error {
	_, err := run("supersede", id, "--with="+with)
	return err
}

func (cli) Create(s CreateSpec) (string, error) {
	typ := s.Type
	if typ == "" {
		typ = "task"
	}
	args := []string{"create", "--type", typ}
	if len(s.Labels) > 0 {
		args = append(args, "--labels", strings.Join(s.Labels, ","))
	}
	if s.Deferred {
		args = append(args, "-s", "deferred")
	}
	if s.NoInherit {
		args = append(args, "--no-inherit-labels")
	}
	if s.Parent != "" {
		args = append(args, "--parent", s.Parent)
	}
	if len(s.Deps) > 0 {
		deps := make([]string, len(s.Deps))
		for i, d := range s.Deps {
			a, err := depArg(d)
			if err != nil {
				return "", err
			}
			deps[i] = a
		}
		args = append(args, "--deps", strings.Join(deps, ","))
	}
	if s.ExternalRef != "" {
		args = append(args, "--external-ref", s.ExternalRef)
	}
	if s.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*s.Priority))
	}
	return run(append(args, "--title", s.Title, "--description", s.Description, "--silent")...)
}

func (cli) AddLabels(id string, labels ...string) error {
	args := []string{"update", id}
	for _, l := range labels {
		args = append(args, "--add-label", l)
	}
	_, err := run(args...)
	return err
}

func (cli) RemoveLabel(ids []string, label string) error {
	_, err := run(append(append([]string{"label", "remove"}, ids...), label)...)
	return err
}

func (cli) Claim(id string, g IfStatus) error {
	_, err := run("update", id, "--if-status", string(g), "--status", "in_progress", "--assignee", actor())
	return classifyClaim(err)
}

func (cli) Unclaim(id string, g IfAssignee) error {
	_, err := run("unclaim", id, "--if-assignee", string(g))
	return err
}

func (cli) Close(ids []string, reason string, force bool) error {
	args := append([]string{"close"}, ids...)
	if force {
		args = append(args, "--force")
	}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	_, err := run(args...)
	return err
}

func (cli) Reopen(id string, _ Serializer) error {
	_, err := run("reopen", id)
	return err
}

func (cli) Delete(ids []string, _ Serializer) error {
	_, err := run(append(append([]string{"delete"}, ids...), "--force")...)
	return err
}

func (cli) AddComment(id, text string) error {
	_, err := run("comments", "add", id, text)
	return err
}

func (cli) SetField(id string, f Field, value string, _ Serializer) error {
	_, err := run("update", id, "--"+string(f), value)
	return err
}

func (cli) Batch(ops []Op) error {
	var b strings.Builder
	for _, o := range ops {
		switch o.Verb {
		case "dep add":
			b.WriteString(strings.TrimSpace("dep add "+o.ID+" "+o.To+" "+o.Type) + "\n")
		case "open":
			line := "update " + o.ID + " status=open"
			if o.Title != "" {
				line += " title=" + batchQuote(o.Title)
			}
			b.WriteString(line + "\n")
		case "close":
			line := "close " + o.ID
			if r := batchQuote(o.Why); r != `""` {
				line += " " + r
			}
			b.WriteString(line + "\n")
		default:
			return fmt.Errorf("bd batch: verb %q not modelled", o.Verb)
		}
	}
	return runBatch(b.String())
}

// defaultListCap is bd list's own row cap when -n is omitted; helpers that predate the
// core pass it to keep that cap.
const defaultListCap = 50
