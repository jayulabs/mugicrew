// Package claudecode is the one concrete Harness backend: it spawns Claude Code
// (spec §5.3). Run execs it headless and blocks; LaunchCommand formats the argv
// a human runs in their own TTY. Both are bd-blind and content-blind (spec
// §5.4) — this package only shapes a command line and parses one JSON blob.
package claudecode

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/crewdocs"
	"jayulabs.org/mugicrew/eventlog"
)

// Backend is the claudecode Harness. Model is an optional --model pass-through
// (empty = defer to Claude Code's native resolution: --model flag > ANTHROPIC_
// MODEL > settings.json > default, spec §5.2). Env is its Agent Profile's
// expanded env, which Run hands the child through its process environment,
// never argv (ADR 0125).
type Backend struct {
	Model string
	Env   map[string]string
}

var _ contract.Harness = (*Backend)(nil)

// New constructs the backend. The loop constructs it and places it in
// ctx (spec §5.2: "constructed in main, placed in ctx" — construction and
// placement are one step, and ctx is assembled there); swapping harness = a new
// impl + that one line.
func New(model string) *Backend { return &Backend{Model: model} }

// execCommand is exec.Command, indirected so Run is testable without a real
// claude on PATH. ponytail: test seam only.
var execCommand = exec.Command

// runArgs is the pure AFK argv (spec §5.3), split out so the flag contract is
// checkable without exec. inv.SessionID is already assigned (Run mints it).
func (b *Backend) runArgs(inv contract.Invocation) []string {
	args := []string{
		"-p", inv.Prompt,
		"--output-format", "json",
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Read,Grep,Glob,Bash,Write,Edit",
	}
	if inv.SessionID != "" {
		args = append(args, "--session-id", inv.SessionID)
	}
	if b.Model != "" {
		args = append(args, "--model", b.Model)
	}
	if inv.BeadID != "" {
		args = append(args, "--name", inv.BeadID)
	}
	return args
}

// Stderr and result caps (ADR 0029 §4): the tail keeps an event record small.
const (
	stderrTail = 4 << 10
	resultCap  = 1 << 10
)

// Run execs claude headless and blocks till it exits (spec §5.3). The session id
// is pre-assigned (--session-id) so it is known even for a kill. The Result
// carries the session's evidence — exit or signal, the best-effort parse of
// stdout's result JSON (which claude writes even on a non-zero exit), and the
// stderr tail — on failure as well as success (research §5). Non-zero exit (incl.
// 143 SIGTERM / 137 killed) or exit 0 with is_error → error. It writes nothing to
// bd and performs no state transition (§5.4).
func (b *Backend) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	if inv.SessionID == "" {
		inv.SessionID = contract.NewSessionID()
	}
	cmd := execCommand("claude", b.runArgs(inv)...)
	if len(b.Env) > 0 { // on top of the inherited env: exec keeps a duplicate key's last value
		cmd.Env = os.Environ()
		for k, v := range b.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // a loop's Ctrl-C drains; it must not kill the session (ADR 0050)
	// Own the stderr capture (the stdlib only keeps it on a non-zero exit), bounded
	// to twice the tail so a chatty stderr can't grow without limit.
	stderr := &tailWriter{n: 2 * stderrTail}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	res, perr := parseResult(out, err != nil)
	res.SessionID = inv.SessionID // the id session.started logged, whatever stdout says
	res.Stderr = tail(stderr.b, stderrTail)
	res.Exit, res.Signal = eventlog.Exit(err) // -1 when it never started
	switch {
	case err != nil: // the stderr tail rides on the Result (its own event attr), not the error
		err = fmt.Errorf("claude -p: %w", err)
	case perr != nil:
		return res, perr
	case res.Summary.IsError:
		err = fmt.Errorf("claude -p: is_error (%s): %s", cmp.Or(res.Summary.TerminalReason, res.Summary.Subtype), res.Summary.Result)
	}
	if err != nil && isPlanLimit(res.Summary) { // a Plan usage limit hit, whatever the exit code
		err = &contract.LimitError{Limit: resetTime(inv.SessionID, res.Summary), Err: err}
	}
	return res, err
}

// parseResult maps claude's --output-format json blob to a Result: .result →
// Output, .session_id → SessionID (spec §5.3), and the failure fields into
// Summary, and total_cost_usd/duration_api_ms/modelUsage into Cost. Best-effort:
// a mistyped field is skipped and the rest still land; only output that is not
// JSON at all (empty stdout from a killed session included) is an error, with
// the Cost unknown. failed is a non-zero exit: a failed run (failed or
// is_error) reporting total_cost_usd 0 is a crash's zeroed result, so unknown, not free.
func parseResult(out []byte, failed bool) (contract.Result, error) {
	var j struct {
		contract.Summary
		SessionID    string   `json:"session_id"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
		APIms        int64    `json:"duration_api_ms"`
		ModelUsage   map[string]struct {
			CostUSD                  float64 `json:"costUSD"`
			InputTokens              int64   `json:"inputTokens"`
			OutputTokens             int64   `json:"outputTokens"`
			CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
			CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
			CostBasis                string  `json:"costBasis"`
		} `json:"modelUsage"`
	}
	err := json.Unmarshal(out, &j)
	var te *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &te) {
		return contract.Result{}, fmt.Errorf("claude -p: parse json output: %w", err)
	}
	res := contract.Result{Output: j.Result, SessionID: j.SessionID, Summary: j.Summary}
	if t := j.TotalCostUSD; t != nil && (*t != 0 || !(failed || j.IsError)) {
		res.Cost = contract.Cost{Known: true, USD: *t, APIms: j.APIms, Models: map[string]contract.ModelCost{}}
		for id, m := range j.ModelUsage {
			res.Cost.Models[id] = contract.ModelCost{USD: m.CostUSD, In: m.InputTokens, Out: m.OutputTokens,
				CacheRead: m.CacheReadInputTokens, CacheWrite: m.CacheCreationInputTokens}
			res.Cost.Unpriced = res.Cost.Unpriced || m.CostBasis == "unknown"
		}
	}
	if len(res.Summary.Result) > resultCap {
		res.Summary.Result = strings.ToValidUTF8(res.Summary.Result[:resultCap], "") // drop a split rune
	}
	return res, nil
}

// tail is the last n bytes of b, cut forward to the first line boundary so no
// partial line leads it. A single line longer than n keeps its last n bytes.
func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	cut := len(b) - n
	if b[cut-1] != '\n' {
		if i := bytes.IndexByte(b[cut:], '\n'); i >= 0 && cut+i+1 < len(b) {
			cut += i + 1
		}
	}
	return string(b[cut:])
}

// tailWriter keeps only the last n bytes written to it.
type tailWriter struct {
	n int
	b []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	if len(w.b) > w.n {
		w.b = append(w.b[:0], w.b[len(w.b)-w.n:]...)
	}
	return len(p), nil
}

// A ReadOnly invocation (a discussion, ADR 0191) drops the edit tools. Accepted
// ceiling: this is a guardrail, not a sandbox. Bash, write-capable user MCP
// tools and WebFetch can still write; the human approving Bash prompts is the
// backstop. The flag rides argv, never the Agent Profile.
//
// LaunchCommand returns the agent command a human runs in their own terminal
// (spec §5.3): `claude "<prompt>" --session-id <uuid> [--model X] [--name <bead>]
// --add-dir <crewdocs> --settings <waiting hooks>`, with the profile Env, default manual permissions, TTY supplied by the human's
// shell. The session id
// is pre-assigned (and returned) so the TUI can record it (research §4). The
// prompt carries the workflow. It returns — it does not exec, write bd, or
// transition state (§5.4).
func (b *Backend) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	id := cmp.Or(inv.SessionID, contract.NewSessionID())
	argv := []string{"claude", inv.Prompt, "--session-id", id}
	if b.Model != "" {
		argv = append(argv, "--model", b.Model)
	}
	if inv.BeadID != "" {
		argv = append(argv, "--name", inv.BeadID)
	}
	dir, err := crewDocsDir()
	if err != nil {
		return nil, err
	}
	hooks, err := waitingHooks()
	if err != nil {
		return nil, err
	}
	argv = append(argv, "--add-dir", dir, "--settings", hooks)
	if inv.ReadOnly {
		argv = append(argv, "--disallowedTools", "Edit,Write,NotebookEdit")
	}
	return contract.AgentCommand{Argv: argv, SessionID: id, Env: b.Env}, nil
}

// waitingHooks is the --settings JSON HITL launch and resume pass (ADR 0099,
// Spec D5): one sync command hook per waiting edge, each running this binary's
// hidden hitl-hook, which finds its pane from hitl-run's env var, so no bead id
// rides here. Claude Code merges it with the user's and Workspace's hooks and no
// file is written; a policy that silences hooks just means never waiting.
func waitingHooks() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("hitl-hook path: %w", err)
	}
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcher struct {
		Matcher string `json:"matcher,omitempty"`
		Hooks   []hook `json:"hooks"`
	}
	cmd := []hook{{"command", "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' hitl-hook"}}
	hooks := map[string][]matcher{
		// Only the MCP elicitation types are an ask; hitl-hook filters them too.
		"Notification": {{Matcher: "elicitation_dialog|elicitation_url_dialog", Hooks: cmd}},
	}
	for _, ev := range []string{"Stop", "StopFailure", "PermissionRequest", "UserPromptSubmit", "PostToolUse", "PostToolUseFailure"} {
		hooks[ev] = []matcher{{Hooks: cmd}}
	}
	js, err := json.Marshal(struct {
		DisableAllHooks bool                 `json:"disableAllHooks"`
		Hooks           map[string][]matcher `json:"hooks"`
	}{false, hooks})
	return string(js), err
}

// crewDocsDir is the --add-dir HITL launch and resume pass so a manual-mode
// human isn't asked to approve each Crew doc Read (Spec D5). It names
// the crewdocs/ parent, not this build's <hash> leaf: probed (claude -p
// --permission-mode default) — the parent suppresses the prompt for
// reads in its hash subdirs, so a session resumed after the loop moved onto a
// newer build still reads its original hash dir. It is a launch flag, not
// session state, hence resume passes it again. Only a path; content-blind.
func crewDocsDir() (string, error) {
	d, err := crewdocs.Dir()
	if err != nil {
		return "", fmt.Errorf("crew docs: %w", err)
	}
	return filepath.Dir(d), nil
}

// ResumeCommand returns `claude --resume <uuid> [--model X] --add-dir <crewdocs>
// --settings <waiting hooks>`, with the profile Env, for an
// exited session (ADR 0039 §1): the caller runs it in the session's kept
// worktree through hitl-run. It errors when the transcript is gone — claude prunes ~/.claude/projects/*/
// <uuid>.jsonl after about 30 days (§3) — so the path knowledge stays here.
func (b *Backend) ResumeCommand(sessionID string, readOnly bool) (contract.AgentCommand, error) {
	if sessionID == "" {
		return contract.AgentCommand{}, errors.New("no claude session id to resume")
	}
	if _, err := transcript(sessionID); err != nil {
		return contract.AgentCommand{}, err
	}
	dir, err := crewDocsDir()
	if err != nil {
		return contract.AgentCommand{}, err
	}
	hooks, err := waitingHooks()
	if err != nil {
		return contract.AgentCommand{}, err
	}
	argv := []string{"claude", "--resume", sessionID}
	if b.Model != "" { // --model binds only the session it launched, so resume passes it again
		argv = append(argv, "--model", b.Model)
	}
	argv = append(argv, "--add-dir", dir, "--settings", hooks)
	if readOnly { // a resumed discussion stays read-only, as at launch (ADR 0191)
		argv = append(argv, "--disallowedTools", "Edit,Write,NotebookEdit")
	}
	return contract.AgentCommand{Argv: argv, SessionID: sessionID, Env: b.Env}, nil
}

// configDir is claude's config dir: $CLAUDE_CONFIG_DIR, else ~/.claude.
func configDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// transcript is the path of a session's transcript, claude's
// <config dir>/projects/*/<uuid>.jsonl; an error when it is gone (pruned).
func transcript(sessionID string) (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sessionID+".jsonl")); len(m) > 0 {
		return m[0], nil
	}
	return "", fmt.Errorf("claude transcript for session %s is gone (pruned?)", sessionID)
}

// costState is one "type":"cost-state" transcript line: claude's running session
// totals, written at exit (hangup included). Undocumented (seen on 2.1.289), so a
// line of another shape has a nil USD and reads as unknown.
type costState struct {
	Type       string   `json:"type"`
	USD        *float64 `json:"totalCostUSD"`
	APIms      float64  `json:"totalAPIDuration"`
	ModelUsage map[string]struct {
		CostUSD                  float64 `json:"costUSD"`
		InputTokens              int64   `json:"inputTokens"`
		OutputTokens             int64   `json:"outputTokens"`
		CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
		CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
	Unpriced bool `json:"hasUnknownModelCost"`
}

// costStates is every cost-state line of the session's transcript, in order, and
// the continued-in destination (a /bg handoff) when that record comes after the
// last cost-state, else ""; none when there is no transcript. An earlier run's
// continued-in, followed by a later cost-state, is not this transcript's ending.
func costStates(sessionID string) ([]costState, string) {
	if sessionID == "" {
		return nil, ""
	}
	path, err := transcript(sessionID)
	if err != nil {
		return nil, ""
	}
	b, _ := os.ReadFile(path)
	var out []costState
	dest := ""
	for _, line := range bytes.Split(b, []byte("\n")) {
		switch {
		case bytes.Contains(line, []byte(`"cost-state"`)):
			var s costState
			if err := json.Unmarshal(line, &s); err != nil {
				s.USD = nil // a mistyped cost-state is counted, but unknown; one that is not JSON is no cost-state
			}
			if s.Type == "cost-state" {
				out = append(out, s)
				dest = ""
			}
		case bytes.Contains(line, []byte(`"continued-in"`)):
			var r struct {
				Type string `json:"type"`
				To   string `json:"continuedInSessionId"`
			}
			if json.Unmarshal(line, &r) == nil && r.Type == "continued-in" {
				dest = r.To
			}
		}
	}
	return out, dest
}

// cumulative is s as a Cost holding its running totals, not a delta.
func (s costState) cumulative() contract.Cost {
	c := contract.Cost{Known: true, USD: *s.USD, APIms: int64(s.APIms), Unpriced: s.Unpriced,
		Models: map[string]contract.ModelCost{}}
	for id, m := range s.ModelUsage {
		c.Models[id] = contract.ModelCost{USD: m.CostUSD, In: m.InputTokens, Out: m.OutputTokens,
			CacheRead: m.CacheReadInputTokens, CacheWrite: m.CacheCreationInputTokens}
	}
	return c
}

// MeterCost reads the HITL session's Session cost from its transcript: call it
// before the agent runs, and the func it returns after the agent exits. That is
// the spend of this run, the last cost-state minus the last one on disk at the
// call (zero with none; a resume adds to the totals, and a /bg handoff writes a
// duplicate of the first), total and per model. No new cost-state since the call
// (no transcript, a SIGKILL), or one of an unknown shape, is an unknown Cost: never
// 0, never an error. The result also names the continued-in destination and the
// last cost-state (Spec mc-11b8 D1, D2).
func MeterCost(sessionID string) func() contract.Metered {
	s0, _ := costStates(sessionID)
	before := len(s0)
	base := costState{USD: new(float64)}
	if before > 0 {
		base = s0[before-1]
	}
	return func() contract.Metered {
		s, dest := costStates(sessionID)
		m := contract.Metered{}
		if len(s) > before { // a handoff writes a fresh cost-state; an older run's record is not this run's
			m.ContinuedIn = dest
		}
		if len(s) > 0 && s[len(s)-1].USD != nil {
			m.Last = s[len(s)-1].cumulative()
		}
		if len(s) <= before || s[len(s)-1].USD == nil || base.USD == nil {
			return m
		}
		m.Cost = Remainder(s[len(s)-1].cumulative(), base.cumulative())
		return m
	}
}
