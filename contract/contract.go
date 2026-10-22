// Package contract is the type foundation every crewmate builds on: the bead
// routing key, the Crewmate interface (Run for AFK, Launch for HITL), and the
// LaunchCommand sum type. Spec §4; OE ADR 0007.
package contract

import (
	"cmp"
	"crypto/rand"
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"

	"jayulabs.org/mugicrew/crewdocs"
	"jayulabs.org/mugicrew/git"
)

// Kind is a bead's single routing key, carried as a kind:<value> label
// (ADR 0002). It is the key the registry resolves an owner from.
type Kind string

// The intake and charting kinds (spec §4.2; chart-conflict added by ADR 0011):
// Signal is Luffy's to triage (ADR 0129), the rest Nami's. Plus kinds other
// crewmates own: Spec (Zoro's) — the terminal artifact a drained Chart
// spawns in succession (§8.4); Prototype (Sanji's), an Inbox HITL kind (§9.2);
// and Task (Brook's), a HITL walk-through of external-world steps, a direct
// Initiative child when ordered against a member, else outside (ADR 0352;
// ADR 0115; ADR 0013 had removed it from the defog set and reserved the name).
const (
	KindSignal        Kind = "signal"
	KindGrilling      Kind = "grilling"
	KindResearch      Kind = "research"
	KindChart         Kind = "chart"
	KindSurvey        Kind = "survey"         // Nami-owned HITL leaf: a Chart's charting session, seeded as its first child (Spec mc-r832 D1)
	KindChartConflict Kind = "chart-conflict" // Nami-owned AFK leaf: a Chart-land conflict, member of the Chart, carrying the Base:/Tip: pins (ADR 0011, ADR 0080)
	KindSpec          Kind = "spec"
	KindCheckpoint    Kind = "checkpoint" // Zoro-cut container, handled by Jinbe (registry row); BranchName/TrailerFor name the kind from this one const (FR27)
	KindPrototype     Kind = "prototype"  // Sanji's defog kind; named here for the TUI's HITL filter (§9.2)
	KindTask          Kind = "task"       // Brook-owned HITL leaf: a human-launched guide through external-world steps (ADR 0113/0115); not a defog kind (ADR 0013)
	KindPivot         Kind = "pivot"      // Zoro-owned HITL leaf: a Spec flaw a Checkpoint effort raised; its Checkpoint blocks on it (ADR 0043)
	KindRollback      Kind = "rollback"   // human-filed, Jinbe-owned AFK leaf undoing a landed Checkpoint; a successor Checkpoint is caused-by it (ADR 0047/0048)
	KindSurgery       Kind = "surgery"    // human-filed, Franky-owned HITL leaf: dictated code on a Checkpoint: a fresh one it files, or an existing one it joins (ADR 0091/0092)
	KindInitiative    Kind = "initiative" // Luffy-owned HITL container above the spine: branchless, no land; member chains are its children (ADR 0128, 0135)
	KindDiscussion    Kind = "discussion" // Robin-owned HITL leaf: a human-started read-only session that explains the Workspace and drafts Signals (ADR 0194)
	KindCheckup       Kind = "checkup"    // Chopper-owned HITL leaf: a human-started session that diagnoses and repairs one broken bead (ADR 0272)
)

// The Checkpoint tail kinds, in spine order: a landed Spec's Checkpoint
// drives build → review → critique → conflict → approval. Franky owns
// build/critique/conflict (AFK), Usopp review (AFK), Luffy approval (HITL); their
// registry rows land ahead of the owning packages (the loop skips a kind whose
// owner has no crew() entry). "build" is also the wire label the slicing fork lays
// and Zoro's BuildChildren reads back — one const now names both.
const (
	KindBuild    Kind = "build"
	KindReview   Kind = "review"
	KindCritique Kind = "critique"
	KindConflict Kind = "conflict"
	KindApproval Kind = "approval"

	// KindForgeReview is Usopp's AFK leaf that judges a container's Merge Request notes into Critiques or grillings; a Checkpoint member while open (ADR 0317, ADR 0318).
	KindForgeReview Kind = "forge-review"
)

// KindLanding is the Jinbe-owned AFK leaf that lands its parent container: a child
// pinned to the approved commit, so the container never lands itself (ADR 0145/0147).
const KindLanding Kind = "landing"

// KindMRWatch is the Jinbe-owned AFK leaf that reads its parent container's open Merge
// Request every paced tick, so feedback left between Landings spawns a Forge Review
// (ADR 0185 §8). Like a Landing it is never a member and never undrains its parent.
const KindMRWatch Kind = "mr-watch"

// KindLatch is the Brook-owned AFK leaf that blocks beads until a Jira ticket reaches Done.
const KindLatch Kind = "latch"

// Kinds is every Kind const above (both blocks). StripKindPrefix is built from
// it, so a new Kind must be added here to join the title backstop (ADR 0097).
var Kinds = []Kind{
	KindSignal, KindGrilling, KindResearch, KindChart, KindSurvey, KindChartConflict, KindSpec,
	KindCheckpoint, KindPrototype, KindTask, KindPivot, KindRollback, KindSurgery, KindInitiative, KindDiscussion, KindCheckup,
	KindBuild, KindReview, KindForgeReview, KindCritique, KindConflict, KindApproval, KindLanding, KindMRWatch, KindLatch,
}

// kindPrefix matches one leading "<kind>:" (case-insensitive, whitespace around
// the name), longest name first so chart-conflict wins over chart.
var kindPrefix = func() *regexp.Regexp {
	names := make([]string, len(Kinds))
	for i, k := range Kinds {
		names[i] = regexp.QuoteMeta(string(k))
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return regexp.MustCompile(`(?i)^\s*(?:` + strings.Join(names, "|") + `)\s*:\s*`)
}()

// PacedLeaf reports whether this kind is a Landing or MR Watch: a leaf Jinbe's paced
// tick works that is never a member's work, so it neither drains nor fogs its container.
func (k Kind) PacedLeaf() bool { return k == KindLanding || k == KindMRWatch }

// AdmitsLandOnlyHold reports whether an open blocker of this kind holds only a
// Checkpoint's land, not its work (ADR 0269): the offshoot-shaped kinds. A pivot or
// any other kind freezes the whole Checkpoint. Shared by loop.Readmit and the TUI.
func (k Kind) AdmitsLandOnlyHold() bool {
	switch k {
	case KindTask, KindSignal, KindChart, KindSpec, KindCheckpoint, KindInitiative:
		return true
	}
	return false
}

// StripKindPrefix removes every leading kind prefix from title ("Spec: Chart:
// foo" → "foo"); the colon is required. A title that would strip to nothing is
// returned unchanged, as is one with no prefix (ADR 0097, rule S17).
func StripKindPrefix(title string) string {
	s := title
	for kindPrefix.MatchString(s) {
		s = kindPrefix.ReplaceAllString(s, "")
	}
	if strings.TrimSpace(s) == "" {
		return title
	}
	return s
}

// LabelDefogged is a Chart's explicit completion signal (spec §8.3): the frontier
// reached the destination (no open children AND no un-promoted fog). Named once
// here so the loop's dispatch gate and Nami's reconcile can't drift apart.
const LabelDefogged = "chart:defogged"

// WayfinderMapFields are the sections a Chart body carries (spec §8.1). Named once
// here so nami's charting/speccing/promote-or-stamp prompts and ChartPlaceholderBody's
// parenthetical all compose from one string and cannot drift across the package
// boundary (nami can't be imported here, so this is the source, not a mirror).
const WayfinderMapFields = "Destination / Notes / Decisions-so-far / Not-yet-specified / Out-of-scope"

// ChartPlaceholderBody is the seed body reconcileSignal gives a freshly cut Chart,
// which the charting session replaces. It is no longer a loop gate: the Chart is a
// Draft until the session publishes it, and Drafts are the one gate (ADR 0059 §7).
// Its field list composes from WayfinderMapFields.
const ChartPlaceholderBody = "Chart body — to be written by the charting session as a wayfinder map (" + WayfinderMapFields + ")."

// SurveyBody is every Survey's body (Spec mc-r832 D1): a pointer to its parent
// Chart, so the bead holds no content to keep in sync.
const SurveyBody = "Survey of its parent Chart: a HITL charting session that maps the Chart's fog with a human. The map lives on the Chart (crew doc chart-body.md), not here; this bead holds no content and is closed by mugicrew when the session exits cleanly."

// DraftedBy is the label that marks a Draft and names its creator — the effort's
// or session's own bead (ADR 0059 §2). Publish and cleanup select on it, so two
// efforts on one container never touch each other's Drafts.
func DraftedBy(creator string) string { return "drafted-by:" + creator }

// DraftInstruction is the one Draft block every bead-creating prompt embeds
// (Spec D11), phrased once here so the prompts cannot drift; creator is the
// prompt's own bead id.
func DraftInstruction(creator string) string {
	return fmt.Sprintf(`Drafts: every bead you create is a Draft until your wrapper publishes it.
- Create each one with `+"`-s deferred`"+` and the label %[1]s in the same -l list as its kind label (`+"`-l kind:<kind>,%[1]s`"+`), keeping --no-inherit-labels.
- Add every edge between your Drafts, and from them to existing beads, before you exit.
- Create every Draft before you close your own bead.
- Never publish: do not set a Draft's status to open and do not remove its %[1]s label.
- A bead that must wait on a Jira ticket: run `+"`mugicrew latch jira <KEY> --blocks <bead> --drafted-by %[2]s`"+` (it files the latch as your Draft).`, DraftedBy(creator), creator)
}

// Render executes an embedded prompt template over data. The templates are
// compiled in and render-tested, so an exec error is a programming error and
// panics, as template.Must does for a parse error.
func Render(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		panic(err)
	}
	return b.String()
}

// ClosingLine is the one standard closing instruction every HITL prompt carries
// (ADR 0067 §1, Spec D8), phrased once here. The bars and bold set it apart in
// the harness's markdown rendering; /exit is literal until a second harness
// supplies its own finish wording.
func ClosingLine() string {
	return "When your session's work is done (you closed your bead, or you finished your outcome and left the close to mugicrew), print exactly these four lines and stop:\n" + closingBar + "\n" +
		"✅ **" + ClosingPhrase + "** You may close this chat.\n" +
		"`/exit` or hit Ctrl+C twice to close.\n" + closingBar
}

// ClosingPhrase is the closing line's own words: Auto-exit arms on a turn that
// shows it, matched as a substring so a changed bar or wrapping still matches
// (Spec D1, ADR 0111).
const ClosingPhrase = "The work for this session is finished."

const closingBar = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

// WithClosingLine wraps ctx so its harness appends ClosingLine() to every
// HITL agent prompt — the one splice point both Launch invokers (the TUI's
// launchFor and `mugicrew signal triage`) apply, so a new HITL role inherits it
// without a template copy. The wrapper hides any extra interface ctx implements
// (e.g. Publisher, AFK-only), so wrap Launch ctxs only.
func WithClosingLine(ctx Ctx) Ctx { return closingCtx{ctx} }

type closingCtx struct{ Ctx }

func (c closingCtx) Harness() Harness { return closingHarness{c.Ctx.Harness()} }

type closingHarness struct{ Harness }

func (h closingHarness) LaunchCommand(inv Invocation) (LaunchCommand, error) {
	inv.Prompt += "\n\n" + ClosingLine()
	return h.Harness.LaunchCommand(inv)
}

// Publisher is the publish handle an AFK effort's Ctx carries (ADR 0059 §4, Spec
// D6): a Go flow hands its finishing own-bead close, and any final edges, to the
// dispatch tail's publish batch instead of writing them itself, so the close can't
// land ahead of its Drafts. A Ctx outside a dispatched effort has none (a type
// assertion fails); the flow then closes its bead itself.
type Publisher interface {
	CloseOnPublish(reason string)
	EdgeOnPublish(e Edge)
}

// Edge is a final edge a publish adds in its batch: From depends on To, Type a
// `bd dep add` type ("" is bd's default, blocks).
type Edge struct{ From, To, Type string }

// Bead is the minimal bead a crewmate is invoked with. The fuller record lands
// with the bd shell-out layer.
// ponytail: ID + Kind are all the contract and registry need today.
type Bead struct {
	ID   string
	Kind Kind
}

// Ctx carries everything a crewmate needs — bd access and the harness — so a
// crewmate stays harness-blind (spec §4): Run/Launch reach the backend through
// Harness(), never construct one. The bd accessor lands with the bd layer;
// the concrete Ctx is assembled by the loop, which places
// the backend in it (spec §5.2).
type Ctx interface {
	Harness() Harness
}

// Invocation is one agent-spawn request (spec §5.1). The crewmate authors
// Prompt — the whole domain lives there (bar the HITL ClosingLine that
// WithClosingLine splices on), the harness is dumb; BeadID names the
// session (--name), the natural anchor. SessionID is an optional pre-assigned
// session id (the event log's session.started carries it before the run, ADR
// 0029 §4); the backend mints one when it is empty.
type Invocation struct {
	Prompt    string
	BeadID    string
	SessionID string
	ReadOnly  bool // drop the edit tools from the session (registry Entry.ReadOnly, ADR 0191)
}

// preamble is the Crewmate preamble (CREWMATE.md): static text, no fmt verbs,
// one {{CREWDOCS}} token (ADR 0088 §2).
//
//go:embed CREWMATE.md
var preamble string

// NewInvocation is the one way a crewmate builds an Invocation: it opens prompt
// with the Crewmate preamble, its {{CREWDOCS}} filled with the materialized
// Crew docs dir, so no kind's prompt can forget it (ADR 0086). Each
// {{MAINLINE}}, where a prompt's git command names the trunk ref, takes the
// Workspace's detected mainline (Spec slice 3).
func NewInvocation(beadID, prompt string) (Invocation, error) {
	dir, err := crewdocs.Dir()
	if err != nil {
		return Invocation{}, fmt.Errorf("crew docs: %w", err)
	}
	prompt = strings.ReplaceAll(prompt, "{{MAINLINE}}", git.Mainline())
	return Invocation{Prompt: strings.ReplaceAll(preamble, "{{CREWDOCS}}", dir) + "\n\n---\n\n" + prompt, BeadID: beadID}, nil
}

// NewSessionID is a random UUIDv4 (RFC 9562), the form claude's --session-id
// requires: pre-assigning it means the id is known even if the session is killed
// before it prints anything.
func NewSessionID() string {
	var u [16]byte
	rand.Read(u[:])         // never fails (crypto/rand, go1.24+)
	u[6] = u[6]&0x0f | 0x40 // version 4
	u[8] = u[8]&0x3f | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// Result is what an AFK run returns (spec §5.1): the agent's final text and its
// session id (for debugging / deferred resume), plus the session's evidence —
// exit, signal, result summary, stderr tail — returned on failure as well as
// success so the caller can record it (ADR 0029 §4).
type Result struct {
	Output    string
	SessionID string
	Exit      int    // process exit code; -1 when killed by a signal or never started
	Signal    string // the killing signal, when there was one
	Summary   Summary
	Stderr    string // the last 4 KiB of stderr, cut at a line boundary
	Cost      Cost   // top-level attrs, not part of Summary (ADR 0029 amendment)
}

// Cost is the Session cost of one agent session, shaped like the end-event attr contract (ADR
// 0029 amendment): an estimate, not billing data. !Known means unknown,
// which is never recorded as 0. The harness normalises it, the session end-event
// writers render it with Attrs.
type Cost struct {
	Known    bool
	USD      float64              // the whole session tree, subagents included
	APIms    int64                // busy API time
	Models   map[string]ModelCost // keyed by full model id
	Unpriced bool                 // some model had no price, so USD undercounts
}

// Metered is a HITL meter's result: the run's Cost, plus where the session went if
// its transcript ends in a continued-in record (ContinuedIn, "" = nowhere) and its
// last cost-state as cumulative totals (Last; the baseline of the remainder the
// destination adds).
type Metered struct {
	Cost
	ContinuedIn string
	Last        Cost
}

// UsageWindow is one Plan usage rate-limit window: Pct used (0–100) and when it
// resets (zero = unknown).
type UsageWindow struct {
	Pct      float64
	ResetsAt time.Time
}

// PlanUsage is the account-wide Plan usage, as Claude Code's /usage shows it: the
// subscription's 5-hour and weekly windows (nil = window absent). Not a Harness
// method (Spec §3).
type PlanUsage struct {
	FiveHour, Weekly *UsageWindow
}

// ModelCost is one models entry; its JSON is the attr shape.
type ModelCost struct {
	USD        float64 `json:"cost_usd"`
	In         int64   `json:"in"`
	Out        int64   `json:"out"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
}

// Sub is m − p, field by field.
func (m ModelCost) Sub(p ModelCost) ModelCost {
	return ModelCost{USD: m.USD - p.USD, In: m.In - p.In, Out: m.Out - p.Out,
		CacheRead: m.CacheRead - p.CacheRead, CacheWrite: m.CacheWrite - p.CacheWrite}
}

// Attrs are the cost's top-level event attrs: cost_usd, api_ms, models, and
// cost_unpriced only when true. Unknown → none at all.
func (c Cost) Attrs() []any {
	if !c.Known {
		return nil
	}
	a := []any{"cost_usd", c.USD, "api_ms", c.APIms}
	if len(c.Models) > 0 {
		a = append(a, "models", c.Models)
	}
	if c.Unpriced {
		a = append(a, "cost_unpriced", true)
	}
	return a
}

// Summary is the best-effort parse of the agent's result JSON (ADR 0029 §4);
// missing fields stay zero. Result is truncated to 1 KiB.
type Summary struct {
	Subtype        string   `json:"subtype"`
	TerminalReason string   `json:"terminal_reason"`
	IsError        bool     `json:"is_error"`
	Errors         []string `json:"errors"`
	Result         string   `json:"result"`
	APIError       string   `json:"api_error"`
	APIErrorStatus int      `json:"api_error_status"`
}

// Limit is a Plan usage limit hit's reset: when the window resets and where that
// time came from. Source is "message" (transcript or result prose), "usage"
// (ReadUsage) or "fallback" (now + 1h, no reset time found).
type Limit struct {
	ResetsAt time.Time
	Source   string
}

// LimitError is the error a Harness's Run returns when the session ended on a
// Plan usage limit hit; Err is the ordinary error it would have returned.
type LimitError struct {
	Limit
	Err error
}

func (e *LimitError) Error() string { return e.Err.Error() }
func (e *LimitError) Unwrap() error { return e.Err }

// Harness is the single choke point for spawning an agent (spec §5). Run is the
// AFK path (exec headless, block till done); LaunchCommand is the HITL path
// (format the command the human runs, return it). It is bd-blind AND
// content-blind on both paths (spec §5.4): it invokes an agent and returns —
// writing nothing to bd and performing no state transition. ResumeCommand is the
// HITL resume of an exited session by its id (ADR 0039 §1–3): a harness that
// can't resume it returns a plain error, which the TUI shows as the status.
type Harness interface {
	Run(ctx Ctx, inv Invocation) (Result, error)
	LaunchCommand(inv Invocation) (LaunchCommand, error)
	ResumeCommand(sessionID string, readOnly bool) (AgentCommand, error)
}

// Crewmate owns >=1 kind and exposes the kind's two dispatch shapes (spec §4).
// A crewmate owning several kinds switches on bead.Kind inside each method.
type Crewmate interface {
	// Run does AFK work (agentic runs via the harness on ctx) then mutates the
	// graph itself — close + spawn, per ADR 0001. Invoked by the loop.
	Run(ctx Ctx, bead Bead) error

	// Launch returns the command a human opens for a HITL kind. Invoked by the
	// TUI or the `signal triage` verb on pick-up — never by the loop.
	Launch(ctx Ctx, bead Bead) (LaunchCommand, error)
}

// LaunchCommand is the thing a human runs for a HITL bead (spec §4.1). It is a
// sealed sum type; its one variety is an agent session (the checklist variety
// was retired, ADR 0080/0101 §2).
type LaunchCommand interface {
	isLaunchCommand()
}

// AgentCommand is an agent invocation the human runs in their own terminal; the
// harness produces it (spec §5.3). Argv is the command line; Env is its Agent
// Profile's expanded env, which the launcher hands the agent through its process
// environment, never argv (ADR 0125).
type AgentCommand struct {
	Argv      []string
	SessionID string            // the pre-assigned agent session id, "" if the argv carries none
	Env       map[string]string // KEY=VAL on top of the launcher's environment
	Profile   string            // the Agent Profile it runs on, "" = the built-in; set by the launcher, not the harness
}

func (AgentCommand) isLaunchCommand() {}

// mugicrew integrate's exit codes. They live here, not in session, so prompt
// packages that session's tests import can splice IntegrateExits; session
// re-exports them as its Exit* constants.
const (
	ExitIntegrated = 0 // integrated, or nothing ahead
	ExitTransient  = 1
	ExitConflict   = 2 // rebase, held
	ExitLanded     = 3 // the container already landed
	ExitNoCutPoint = 4 // no cut ref and no fork point after a rewrite; non-transient
	ExitOffPin     = 5 // a replace refused: the container was rewritten off the bead's pinned Tip
)

// IntegrateExits is the one exit block every prompt that runs mugicrew integrate
// embeds (ADR 0328 §4), rendered from the Exit* constants so the text cannot
// drift from the binary.
func IntegrateExits() string {
	return fmt.Sprintf("- Exit %d: integrated, or nothing ahead.\n"+
		"- Exit %d with a rebase stopped in your tree: resolve the conflicts, `git add` them, `git rebase --continue`, then re-run `mugicrew integrate`.\n"+
		"- Exit %[2]d with no rebase stopped: the container is held.\n"+
		"- Exit %d: the container already landed.\n"+
		"- Exit %d or %d: the container was rewritten past your cut or is off its pin. Never repair or rebase branches by hand.\n"+
		"- Any exit but %[1]d or a resolved %[2]d (%[6]d is transient): note the output on your bead, leave it open and stop.",
		ExitIntegrated, ExitConflict, ExitLanded, ExitNoCutPoint, ExitOffPin, ExitTransient)
}
