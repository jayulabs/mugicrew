package claudecode

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/crewdocs"
)

// crewDocsParent is the --add-dir every HITL argv carries (Spec D5,
// D6.6): the crewdocs/ parent of this build's hash dir, per the probe.
func crewDocsParent(t *testing.T) string {
	t.Helper()
	d, err := crewdocs.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if p := filepath.Dir(d); filepath.Base(p) == "crewdocs" {
		return p
	}
	t.Fatalf("crew docs dir %q is not a hash dir under crewdocs/", d)
	return ""
}

func has(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// The AFK argv is the ticket's Run acceptance criterion (spec §5.3): headless
// -p, JSON output, acceptEdits, the scoped allow-list, BeadID → --name.
func TestRunArgsCarryHeadlessFlags(t *testing.T) {
	args := New("").runArgs(contract.Invocation{Prompt: "do the thing", BeadID: "ws-42"})

	for _, want := range []string{
		"-p", "do the thing",
		"--output-format", "json",
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Read,Grep,Glob,Bash,Write,Edit",
		"--name", "ws-42",
	} {
		if !has(args, want) {
			t.Errorf("runArgs missing %q; got %v", want, args)
		}
	}
	if has(args, "--model") {
		t.Errorf("no model configured → no --model; got %v", args)
	}
	if has(args, "--dangerously-skip-permissions") {
		t.Error("scoped allow-list is chosen over --dangerously-skip-permissions (§5.3)")
	}
}

func TestRunArgsModelPassThrough(t *testing.T) {
	args := New("claude-opus-4-8").runArgs(contract.Invocation{Prompt: "p", BeadID: "mc-1"})
	if !has(args, "--model") || !has(args, "claude-opus-4-8") {
		t.Errorf("configured model must pass through as --model X; got %v", args)
	}
}

// A profile's Run (ADR 0125): --model in argv, and the profile env
// in the child's environment on top of the inherited one, never in argv.
func TestRunCarriesProfileModelAndEnv(t *testing.T) {
	t.Setenv("MC_INHERITED", "kept")
	var argv []string
	orig := execCommand
	execCommand = func(_ string, args ...string) *exec.Cmd {
		argv = args
		return exec.Command("sh", "-c", `printf '{"result":"%s|%s|%s"}' "$ANTHROPIC_BASE_URL" "$ANTHROPIC_AUTH_TOKEN" "$MC_INHERITED"`)
	}
	t.Cleanup(func() { execCommand = orig })

	b := &Backend{Model: "deepseek-chat", Env: map[string]string{
		"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-secret"}}
	res, err := b.Run(nil, contract.Invocation{Prompt: "p", BeadID: "mc-9"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "https://api.deepseek.com/anthropic|sk-secret|kept"; res.Output != want {
		t.Errorf("child env read %q, want %q", res.Output, want)
	}
	if i := slices.Index(argv, "--model"); i < 0 || argv[i+1] != "deepseek-chat" {
		t.Errorf("argv must carry --model deepseek-chat; got %v", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "sk-secret") || strings.Contains(a, "api.deepseek.com") {
			t.Errorf("profile env leaked into argv: %v", argv)
		}
	}
}

// .result → Output, .session_id → SessionID (spec §5.3).
func TestParseResultMapsFields(t *testing.T) {
	got, err := parseResult([]byte(`{"result":"the answer","session_id":"sess-abc","type":"result"}`), false)
	if err != nil {
		t.Fatalf("parseResult: %v", err)
	}
	if got.Output != "the answer" || got.SessionID != "sess-abc" {
		t.Errorf("parseResult = %+v, want Output=the answer SessionID=sess-abc", got)
	}
}

func TestParseResultRejectsGarbage(t *testing.T) {
	if _, err := parseResult([]byte("not json"), false); err == nil {
		t.Error("non-JSON output must be a loud error")
	}
}

// Result blobs from real headless runs (Claude Code 2.1.289), trimmed to the
// fields that matter plus some neighbours. The runs gave only costs for the
// two-model run, so its other token counts are filled in; crashZeroed and
// unpriced are synthetic (the docs' shapes).
const (
	haikuOK = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"duration_ms":1580,"duration_api_ms":1434,
		"total_cost_usd":0.0159696,"result":"OK","session_id":"s",
		"usage":{"input_tokens":10,"output_tokens":54,"output_tokens_details":{"thinking_tokens":47}},
		"modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":10,"outputTokens":54,"cacheReadInputTokens":13796,
		"cacheCreationInputTokens":7155,"webSearchRequests":0,"costUSD":0.0159696,"contextWindow":200000,
		"thinkingTokens":47,"canonicalModel":"claude-haiku-4-5","provider":"firstParty","costBasis":"list"}}}`
	twoModels = `{"type":"result","subtype":"success","is_error":false,"duration_ms":1199,"duration_api_ms":11003,
		"total_cost_usd":0.0647016,"result":"DONE","subagent_stats":{"started_in_background":1},
		"usage":{"output_tokens":36,"cache_read_input_tokens":21614},
		"modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":30,"outputTokens":762,"cacheReadInputTokens":41000,"cacheCreationInputTokens":8000,"costUSD":0.0247451,"costBasis":"list"},
		"claude-sonnet-5-5":{"inputTokens":12,"outputTokens":300,"cacheReadInputTokens":9000,"cacheCreationInputTokens":6000,"costUSD":0.0399565,"costBasis":"list"}}}`
	maxTurns = `{"type":"result","subtype":"error_max_turns","is_error":true,"terminal_reason":"max_turns","num_turns":2,
		"errors":["Reached maximum number of turns (1)"],"result":null,"duration_api_ms":3100,"total_cost_usd":0.0149216,
		"modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":10,"outputTokens":269,"cacheReadInputTokens":14926,"cacheCreationInputTokens":6040,"costUSD":0.0149216,"costBasis":"list"}}}`
	crashZeroed = `{"type":"result","subtype":"error_during_execution","is_error":true,"duration_api_ms":0,"total_cost_usd":0,"modelUsage":{}}`
	unpriced    = `{"type":"result","subtype":"success","is_error":false,"duration_api_ms":900,"total_cost_usd":0.01,
		"modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":1,"outputTokens":2,"costUSD":0.01,"costBasis":"list"},
		"my-proxy-model":{"inputTokens":5,"outputTokens":6,"costUSD":0,"costBasis":"unknown"}}}`
)

// The Session cost is normalised in the harness: unknown (no result, or a failed
// run's zeroed total) is !Known; costBasis unknown flags the cost unpriced.
func TestRunCost(t *testing.T) {
	haiku := "claude-haiku-4-5-20251001"
	for _, tc := range []struct {
		name, script string
		known        bool
		usd          float64
		apiMS        int64
		models       map[string]contract.ModelCost
		unpriced     bool
	}{
		{"single-model success", `printf '%s' '` + haikuOK + `'`, true, 0.0159696, 1434,
			map[string]contract.ModelCost{haiku: {USD: 0.0159696, In: 10, Out: 54, CacheRead: 13796, CacheWrite: 7155}}, false},
		{"two models with a subagent", `printf '%s' '` + twoModels + `'`, true, 0.0647016, 11003,
			map[string]contract.ModelCost{
				haiku:               {USD: 0.0247451, In: 30, Out: 762, CacheRead: 41000, CacheWrite: 8000},
				"claude-sonnet-5-5": {USD: 0.0399565, In: 12, Out: 300, CacheRead: 9000, CacheWrite: 6000}}, false},
		{"error_max_turns exit 1 carries cost", `printf '%s' '` + maxTurns + `'; exit 1`, true, 0.0149216, 3100,
			map[string]contract.ModelCost{haiku: {USD: 0.0149216, In: 10, Out: 269, CacheRead: 14926, CacheWrite: 6040}}, false},
		{"empty stdout (killed) is unknown", `exit 143`, false, 0, 0, nil, false},
		{"failed run with total 0 is unknown", `printf '%s' '` + crashZeroed + `'; exit 1`, false, 0, 0, nil, false},
		{"is_error with total 0 on exit 0 is unknown", `printf '%s' '` + crashZeroed + `'`, false, 0, 0, nil, false},
		{"no cost field is unknown", `printf '%s' '{"result":"hi"}'`, false, 0, 0, nil, false},
		{"costBasis unknown is unpriced", `printf '%s' '` + unpriced + `'`, true, 0.01, 900,
			map[string]contract.ModelCost{haiku: {USD: 0.01, In: 1, Out: 2}, "my-proxy-model": {In: 5, Out: 6}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubExec(t, tc.script)
			res, _ := New("").Run(nil, contract.Invocation{Prompt: "p"})
			want := contract.Cost{Known: tc.known, USD: tc.usd, APIms: tc.apiMS, Models: tc.models, Unpriced: tc.unpriced}
			if !reflect.DeepEqual(res.Cost, want) {
				t.Errorf("Cost = %+v, want %+v", res.Cost, want)
			}
			var sum float64
			for _, m := range res.Cost.Models {
				sum += m.USD
			}
			if tc.known && !tc.unpriced && math.Abs(sum-res.Cost.USD) > 1e-9 {
				t.Errorf("total %v != Σ per-model %v", res.Cost.USD, sum)
			}
		})
	}
}

// stubExec points execCommand at `sh -c <script>`, ignoring the real argv, so
// Run's exec+map and exit-code paths run without a claude on PATH.
func stubExec(t *testing.T, script string) {
	t.Helper()
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("sh", "-c", script)
	}
	t.Cleanup(func() { execCommand = orig })
}

func TestRunParsesSuccessfulExit(t *testing.T) {
	stubExec(t, `printf '%s' '{"result":"hi","session_id":"s1"}'`)
	got, err := New("").Run(nil, contract.Invocation{Prompt: "p", BeadID: "mc-9", SessionID: "pre"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Output != "hi" || got.SessionID != "pre" || got.Exit != 0 {
		t.Errorf("Run = %+v, want Output=hi, the pre-assigned SessionID, exit 0", got)
	}
}

func TestRunNonZeroExitIsError(t *testing.T) {
	stubExec(t, `echo boom >&2; exit 143`) // 143 = SIGTERM, an expected kill path
	if _, err := New("").Run(nil, contract.Invocation{Prompt: "p"}); err == nil {
		t.Fatal("non-zero exit must be an error")
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Run pre-assigns a UUIDv4 via --session-id (research §5.1) and keeps --name.
func TestRunPassesPreassignedSessionID(t *testing.T) {
	var argv []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		argv = args
		return exec.Command("sh", "-c", `printf '{"result":"ok"}'`)
	}
	t.Cleanup(func() { execCommand = orig })

	res, err := New("").Run(nil, contract.Invocation{Prompt: "p", BeadID: "mc-9"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	i := slices.Index(argv, "--session-id")
	if i < 0 || i+1 >= len(argv) || !uuidV4.MatchString(argv[i+1]) {
		t.Fatalf("argv must carry --session-id <uuidv4>; got %v", argv)
	}
	if res.SessionID != argv[i+1] || !has(argv, "mc-9") {
		t.Errorf("SessionID = %q, argv %v; want the pre-assigned id and --name kept", res.SessionID, argv)
	}
	// A caller-assigned id is passed through, not re-minted.
	New("").Run(nil, contract.Invocation{Prompt: "p", SessionID: "given-id"})
	if !has(argv, "given-id") {
		t.Errorf("caller's SessionID must pass through; got %v", argv)
	}
}

// withoutWaitingHooks checks a HITL argv carries the waiting hooks (Spec D5)
// and returns it with the --settings pair cut out: the JSON parses, hooks are
// on, each edge event runs `<abs path> hitl-hook`, and no bead id rides along.
func withoutWaitingHooks(t *testing.T, argv []string, bead string) []string {
	t.Helper()
	i := slices.Index(argv, "--settings")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("Argv = %v, want --settings <hooks json>", argv)
	}
	js := argv[i+1]
	var s struct {
		DisableAllHooks *bool `json:"disableAllHooks"`
		Hooks           map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal([]byte(js), &s); err != nil {
		t.Fatalf("--settings %q: %v", js, err)
	}
	if s.DisableAllHooks == nil || *s.DisableAllHooks {
		t.Errorf("--settings %s: want disableAllHooks false", js)
	}
	for _, ev := range []string{"Stop", "StopFailure", "PermissionRequest", "Notification", "UserPromptSubmit", "PostToolUse", "PostToolUseFailure"} {
		m := s.Hooks[ev]
		if len(m) != 1 || len(m[0].Hooks) != 1 || m[0].Hooks[0].Type != "command" ||
			!strings.HasPrefix(m[0].Hooks[0].Command, "'/") || !strings.HasSuffix(m[0].Hooks[0].Command, " hitl-hook") {
			t.Errorf("--settings hooks[%s] = %+v, want one command hook '<abs path>' hitl-hook", ev, m)
		}
	}
	if bead != "" && strings.Contains(js, bead) {
		t.Errorf("--settings %s carries bead id %s; identity comes from hitl-run's env var", js, bead)
	}
	return slices.Delete(slices.Clone(argv), i, i+2)
}

// HITL argv (spec §5.3, FR30): ["claude","<prompt>","--session-id",<uuidv4>,"--name",<bead>],
// agent variety, no headless flags — the human's terminal supplies the TTY.
func TestLaunchCommandArgv(t *testing.T) {
	lc, err := New("").LaunchCommand(contract.Invocation{Prompt: "triage this", BeadID: "mc-7"})
	if err != nil {
		t.Fatalf("LaunchCommand: %v", err)
	}
	ac, ok := lc.(contract.AgentCommand)
	if !ok {
		t.Fatalf("LaunchCommand must return the agent variety, got %T", lc)
	}
	argv := withoutWaitingHooks(t, ac.Argv, "mc-7")
	if len(argv) != 8 || !uuidV4.MatchString(argv[3]) {
		t.Fatalf("Argv = %v, want [claude <prompt> --session-id <uuidv4> --name <bead> --add-dir <crewdocs> --settings <hooks>]", ac.Argv)
	}
	want := []string{"claude", "triage this", "--session-id", argv[3], "--name", "mc-7", "--add-dir", crewDocsParent(t)}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("Argv = %v, want %v", argv, want)
	}
	if ac.SessionID != ac.Argv[3] {
		t.Errorf("SessionID = %q, want the argv id %q", ac.SessionID, ac.Argv[3])
	}
}

// ReadOnly drops the edit tools from launch argv (ADR 0191); without it the flag is absent.
func TestLaunchCommandReadOnly(t *testing.T) {
	for _, ro := range []bool{true, false} {
		lc, err := New("").LaunchCommand(contract.Invocation{Prompt: "p", BeadID: "mc-7", ReadOnly: ro})
		if err != nil {
			t.Fatal(err)
		}
		argv := lc.(contract.AgentCommand).Argv
		i := slices.Index(argv, "--disallowedTools")
		if ro && (i < 0 || argv[i+1] != "Edit,Write,NotebookEdit") || !ro && i >= 0 {
			t.Errorf("ReadOnly=%v: argv %v", ro, argv)
		}
	}
}

func TestLaunchCommandHonoursSessionIDAndModel(t *testing.T) {
	lc, err := New("claude-opus-4-8").LaunchCommand(contract.Invocation{Prompt: "p", BeadID: "mc-7", SessionID: "given-id"})
	ac, ok := lc.(contract.AgentCommand)
	if err != nil || !ok {
		t.Fatalf("LaunchCommand = %T, %v; want the agent variety", lc, err)
	}
	want := []string{"claude", "p", "--session-id", "given-id", "--model", "claude-opus-4-8", "--name", "mc-7", "--add-dir", crewDocsParent(t)}
	if !reflect.DeepEqual(withoutWaitingHooks(t, ac.Argv, "mc-7"), want) || ac.SessionID != "given-id" {
		t.Errorf("Argv = %v SessionID = %q, want %v and given-id", ac.Argv, ac.SessionID, want)
	}
}

// The failure shapes from the research (§2): the evidence comes back with the error.
func TestRunReturnsEvidenceOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		exit         int
		signal       string
		sum          contract.Summary
		stderr       string
	}{
		{"bad model", `printf '%s' '{"is_error":true,"terminal_reason":"api_error","result":"There is an issue with the selected model","session_id":"x"}'; echo '[claude-code:unrecognized_model] {"model":"nope"}' >&2; exit 1`,
			1, "", contract.Summary{IsError: true, TerminalReason: "api_error", Result: "There is an issue with the selected model"}, `[claude-code:unrecognized_model] {"model":"nope"}`},
		{"max turns", `printf '%s' '{"is_error":true,"subtype":"error_max_turns","terminal_reason":"max_turns","errors":["Reached maximum number of turns (1)"]}'; exit 1`,
			1, "", contract.Summary{IsError: true, Subtype: "error_max_turns", TerminalReason: "max_turns", Errors: []string{"Reached maximum number of turns (1)"}}, ""},
		{"killed 137, empty output", `exit 137`, 137, "", contract.Summary{}, ""},
		{"killed by signal", `kill -KILL $$`, -1, "killed", contract.Summary{}, ""},
		{"exit 0 is_error", `printf '%s' '{"is_error":true,"subtype":"success","result":"API Error: overloaded"}'`,
			0, "", contract.Summary{IsError: true, Subtype: "success", Result: "API Error: overloaded"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubExec(t, tc.script)
			res, err := New("").Run(nil, contract.Invocation{Prompt: "p", SessionID: "pre-id"})
			if err == nil {
				t.Fatal("a failed session must be an error")
			}
			if res.SessionID != "pre-id" || res.Exit != tc.exit || res.Signal != tc.signal {
				t.Errorf("session/exit/signal = %q/%d/%q, want pre-id/%d/%q", res.SessionID, res.Exit, res.Signal, tc.exit, tc.signal)
			}
			if !reflect.DeepEqual(res.Summary, tc.sum) {
				t.Errorf("Summary = %+v, want %+v", res.Summary, tc.sum)
			}
			if strings.TrimSpace(res.Stderr) != tc.stderr {
				t.Errorf("Stderr = %q, want %q", res.Stderr, tc.stderr)
			}
		})
	}
}

// Parsing is best-effort: a mistyped field is skipped, the rest still land.
func TestParseToleratesOddFields(t *testing.T) {
	stubExec(t, `printf '%s' '{"errors":[{"code":1}],"subtype":"error_during_execution","is_error":true}'; exit 1`)
	res, _ := New("").Run(nil, contract.Invocation{Prompt: "p"})
	if res.Summary.Subtype != "error_during_execution" || !res.Summary.IsError {
		t.Errorf("Summary = %+v, want subtype and is_error despite the odd errors[]", res.Summary)
	}
}

// Run applies the cap: a chatty stderr comes back as its last 4 KiB of whole lines.
func TestRunStderrTailCapped(t *testing.T) {
	stubExec(t, `for i in $(seq 200); do echo "line $i of a chatty stderr, padded to be long enough"; done >&2; exit 1`)
	res, _ := New("").Run(nil, contract.Invocation{Prompt: "p"})
	if len(res.Stderr) > 4096 || !strings.HasPrefix(res.Stderr, "line ") || !strings.HasSuffix(res.Stderr, "line 200 of a chatty stderr, padded to be long enough\n") {
		t.Errorf("Stderr = %d bytes, starting %q", len(res.Stderr), res.Stderr[:min(12, len(res.Stderr))])
	}
}

func TestSummaryResultTruncatedTo1KiB(t *testing.T) {
	stubExec(t, `printf '{"result":"%s"}' "$(printf '%03000d' 0 | tr 0 a)"`)
	res, err := New("").Run(nil, contract.Invocation{Prompt: "p"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Output) != 3000 || len(res.Summary.Result) != 1024 {
		t.Errorf("Output %d bytes, Summary.Result %d; want the full 3000 and 1024", len(res.Output), len(res.Summary.Result))
	}
}

// The stderr tail is the last 4 KiB, cut forward to a line boundary.
func TestStderrTailCutAtLineBoundary(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n" // 100 bytes
	got := tail([]byte(strings.Repeat(line, 50)+"last\n"), 4096)
	if len(got) > 4096 || !strings.HasPrefix(got, "xxx") || !strings.HasSuffix(got, "last\n") {
		t.Fatalf("tail = %d bytes %q…", len(got), got[:min(20, len(got))])
	}
	if strings.Count(got, "\n") != 41 || len(got) != 40*100+5 {
		t.Errorf("tail must hold only whole lines: %d bytes, %d lines", len(got), strings.Count(got, "\n"))
	}
	if got := tail([]byte("short\n"), 4096); got != "short\n" {
		t.Errorf("under the cap is kept whole, got %q", got)
	}
	if got := tail([]byte(strings.Repeat("y", 5000)), 4096); len(got) != 4096 {
		t.Errorf("one line longer than the cap keeps its last 4 KiB, got %d", len(got))
	}
}

// ResumeCommand (ADR 0039 §1, §3): `claude --resume <uuid>` carrying the id and
// re-passing --add-dir (a launch flag, not session state — Spec D5), and
// a plain error once the transcript under ~/.claude/projects/*/ is gone.
func TestResumeCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if _, err := New("m").ResumeCommand("sess-1", false); err == nil || !strings.Contains(err.Error(), "sess-1") {
		t.Errorf("missing transcript: err = %v, want an error naming the session", err)
	}
	proj := home + "/.claude/projects/-tmp-wt"
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proj+"/sess-1.jsonl", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ac, err := New("").ResumeCommand("sess-1", false)
	if err != nil {
		t.Fatalf("ResumeCommand: %v", err)
	}
	if !slices.Equal(withoutWaitingHooks(t, ac.Argv, ""), []string{"claude", "--resume", "sess-1", "--add-dir", crewDocsParent(t)}) || ac.SessionID != "sess-1" {
		t.Errorf("ResumeCommand = %+v, want claude --resume sess-1 --add-dir <crewdocs> --settings <hooks>", ac)
	}
	if _, err := New("").ResumeCommand("", false); err == nil {
		t.Error("an empty session id must be an error, not a glob over every transcript")
	}
}

// A read-only resume keeps the edit tools off, as launch does (ADR 0191).
func TestResumeCommandReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	proj := home + "/.claude/projects/-tmp-wt"
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proj+"/sess-1.jsonl", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ro := range []bool{true, false} {
		ac, err := New("").ResumeCommand("sess-1", ro)
		if err != nil {
			t.Fatalf("ResumeCommand: %v", err)
		}
		if got := slices.Contains(ac.Argv, "Edit,Write,NotebookEdit") && slices.Contains(ac.Argv, "--disallowedTools"); got != ro {
			t.Errorf("readOnly=%v: argv %v", ro, ac.Argv)
		}
	}
}

// A profile's HITL launch and resume (ADR 0125): both carry
// --model (resume again, since claude's --model binds only the session it
// launched) and the profile Env on the command, never in argv or --settings.
func TestLaunchAndResumeCarryProfileModelAndEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	proj := home + "/.claude/projects/-tmp-wt"
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proj+"/sess-1.jsonl", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-secret"}
	b := &Backend{Model: "deepseek-chat", Env: env}
	lc, err := b.LaunchCommand(contract.Invocation{Prompt: "p", BeadID: "mc-7"})
	if err != nil {
		t.Fatalf("LaunchCommand: %v", err)
	}
	rc, err := b.ResumeCommand("sess-1", false)
	if err != nil {
		t.Fatalf("ResumeCommand: %v", err)
	}
	for name, ac := range map[string]contract.AgentCommand{"launch": lc.(contract.AgentCommand), "resume": rc} {
		if i := slices.Index(ac.Argv, "--model"); i < 0 || ac.Argv[i+1] != "deepseek-chat" {
			t.Errorf("%s argv must carry --model deepseek-chat; got %v", name, ac.Argv)
		}
		if !reflect.DeepEqual(ac.Env, env) {
			t.Errorf("%s Env = %v, want the profile env %v", name, ac.Env, env)
		}
		for _, a := range ac.Argv { // the --settings JSON is an argv word too
			if strings.Contains(a, "sk-secret") || strings.Contains(a, "api.deepseek.com") {
				t.Errorf("%s: profile env leaked into argv: %q", name, a)
			}
		}
	}
}

// MeterCost reads a HITL session's cost from its transcript's last cost-state
// line, as the delta against the one before it, and is unknown (never 0, never an
// error) when the run appended none or its shape changed (Spec FR12–17).
func TestMeterCost(t *testing.T) {
	const (
		user  = `{"type":"user","message":{"content":"/exit"}}`
		cs1   = `{"type":"cost-state","totalCostUSD":1.5,"totalAPIDuration":3000,"modelUsage":{"claude-opus-5-5":{"inputTokens":10,"outputTokens":200,"cacheReadInputTokens":4000,"cacheCreationInputTokens":300,"costUSD":1.25},"claude-haiku-4-5":{"inputTokens":5,"outputTokens":20,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0.25}},"hasUnknownModelCost":false}`
		cs2   = `{"type":"cost-state","totalCostUSD":2,"totalAPIDuration":4000,"modelUsage":{"claude-opus-5-5":{"inputTokens":15,"outputTokens":300,"cacheReadInputTokens":5000,"cacheCreationInputTokens":400,"costUSD":1.75},"claude-haiku-4-5":{"inputTokens":5,"outputTokens":20,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0.25}},"hasUnknownModelCost":false}`
		unpr  = `{"type":"cost-state","totalCostUSD":0.5,"totalAPIDuration":10,"modelUsage":{"x-model":{"inputTokens":1,"outputTokens":2,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0}},"hasUnknownModelCost":true}`
		shape = `{"type":"cost-state","totalCostUSD":"1.5","totalAPIDuration":3000}`
	)
	opus1 := contract.ModelCost{USD: 1.25, In: 10, Out: 200, CacheRead: 4000, CacheWrite: 300}
	haiku := contract.ModelCost{USD: 0.25, In: 5, Out: 20}
	for _, tc := range []struct {
		name          string
		before, after []string // transcript lines before the run (nil = no file) and after it
		want          contract.Cost
	}{
		{"one record", nil, []string{user, cs1},
			contract.Cost{Known: true, USD: 1.5, APIms: 3000, Models: map[string]contract.ModelCost{"claude-opus-5-5": opus1, "claude-haiku-4-5": haiku}}},
		{"user line after the record", nil, []string{cs1, user},
			contract.Cost{Known: true, USD: 1.5, APIms: 3000, Models: map[string]contract.ModelCost{"claude-opus-5-5": opus1, "claude-haiku-4-5": haiku}}},
		{"resume records the delta", []string{cs1, user}, []string{cs1, user, cs2},
			contract.Cost{Known: true, USD: 0.5, APIms: 1000, Models: map[string]contract.ModelCost{
				"claude-opus-5-5": {USD: 0.5, In: 5, Out: 100, CacheRead: 1000, CacheWrite: 100}}}},
		{"handoff duplicate cost-state is the source's spend", nil, []string{cs1, cs1, `{"type":"continued-in","timestamp":"t","sessionId":"sess-1","continuedInSessionId":"dest-9"}`},
			contract.Cost{Known: true, USD: 1.5, APIms: 3000, Models: map[string]contract.ModelCost{"claude-opus-5-5": opus1, "claude-haiku-4-5": haiku}}},
		{"resume with no new record", []string{cs1}, []string{cs1, user}, contract.Cost{}},
		{"unknown-shaped baseline", []string{shape}, []string{shape, cs1}, contract.Cost{}},
		{"no file", nil, nil, contract.Cost{}},
		{"garbage", nil, []string{"not json", `{"type":"cost-state",`}, contract.Cost{}},
		{"changed shape", nil, []string{shape}, contract.Cost{}},
		{"unknown model cost", nil, []string{unpr},
			contract.Cost{Known: true, USD: 0.5, APIms: 10, Unpriced: true, Models: map[string]contract.ModelCost{"x-model": {In: 1, Out: 2}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			path := dir + "/projects/-tmp-wt/sess-1.jsonl"
			write := func(lines []string) {
				if lines == nil {
					return
				}
				if err := os.MkdirAll(dir+"/projects/-tmp-wt", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(tc.before)
			read := MeterCost("sess-1")
			write(tc.after)
			m := read()
			got := m.Cost
			if got.Known != tc.want.Known || math.Abs(got.USD-tc.want.USD) > 1e-9 || got.APIms != tc.want.APIms || got.Unpriced != tc.want.Unpriced {
				t.Errorf("cost = %+v, want %+v", got, tc.want)
			}
			if len(got.Models) != len(tc.want.Models) {
				t.Fatalf("models = %+v, want %+v", got.Models, tc.want.Models)
			}
			for id, w := range tc.want.Models {
				g := got.Models[id]
				if math.Abs(g.USD-w.USD) > 1e-9 || g.In != w.In || g.Out != w.Out || g.CacheRead != w.CacheRead || g.CacheWrite != w.CacheWrite {
					t.Errorf("models[%s] = %+v, want %+v", id, g, w)
				}
			}
		})
	}
}

// A handoff writes a duplicate of the first cost-state: the source's spend is the
// first one (the baseline predates it), and the transcript's continued-in record
// names the destination (Spec mc-11b8 D1, D2).
func TestMeterCostHandoff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := dir + "/projects/-tmp-wt/sess-1.jsonl"
	if err := os.MkdirAll(dir+"/projects/-tmp-wt", 0o755); err != nil {
		t.Fatal(err)
	}
	read := MeterCost("sess-1")
	cs := `{"type":"cost-state","totalCostUSD":1.5,"totalAPIDuration":3000}`
	ci := `{"type":"continued-in","timestamp":"t","sessionId":"sess-1","continuedInSessionId":"dest-9"}`
	if err := os.WriteFile(path, []byte(cs+"\n"+cs+"\n"+ci+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := read()
	if !m.Known || m.USD != 1.5 || m.APIms != 3000 || m.ContinuedIn != "dest-9" || m.Last.USD != 1.5 {
		t.Errorf("metered = %+v, want 1.5 USD, 3000 ms, dest-9, last 1.5", m)
	}
}

// A resumed run's transcript still holds the earlier run's continued-in; only a
// continued-in this run wrote (a new cost-state, then the record last) counts.
func TestMeterCostResumeIgnoresOldHandoff(t *testing.T) {
	cs1 := `{"type":"cost-state","totalCostUSD":1.5,"totalAPIDuration":3000}`
	cs2 := `{"type":"cost-state","totalCostUSD":2,"totalAPIDuration":4000}`
	ci := `{"type":"continued-in","timestamp":"t","sessionId":"sess-1","continuedInSessionId":"dest-9"}`
	user := `{"type":"user","message":{"content":"hi"}}`
	for _, tc := range []struct {
		name  string
		after []string
		known bool
		usd   float64
	}{
		{"resumed and ended", []string{cs1, cs1, ci, user, cs2}, true, 0.5},
		{"resume killed", []string{cs1, cs1, ci}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			path := dir + "/projects/-tmp-wt/sess-1.jsonl"
			if err := os.MkdirAll(dir+"/projects/-tmp-wt", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Join([]string{cs1, cs1, ci}, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			read := MeterCost("sess-1")
			if err := os.WriteFile(path, []byte(strings.Join(tc.after, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			m := read()
			if m.ContinuedIn != "" || m.Known != tc.known || math.Abs(m.USD-tc.usd) > 1e-9 {
				t.Errorf("metered = %+v, want no continued-in, known=%v usd=%v", m, tc.known, tc.usd)
			}
		})
	}
}
