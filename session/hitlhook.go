package session

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// paneVar is the pane's address, <socket>,<pane id>, that hitl-run exports to its
// agent and hitl-hook reads (Spec D3): hitl-run strips TMUX/TMUX_PANE from
// the agent's env, so this is how a hook finds its session.
const paneVar = "MUGICREW_HITL_PANE"

// InPane reports whether this process runs under a HITL pane's agent (paneVar set).
func InPane() bool { return os.Getenv(paneVar) != "" }

// HitlHook is the hidden `mugicrew hitl-hook` a Claude Code hook runs (ADR 0099):
// it reads the hook event from stdin and the pane address from paneVar, and sets or
// unsets the waiting bit on the pane's session per waitEdge, and arms or disarms its
// Auto-exit per autoExitEdge (ADR 0111), autoExit being the auto_exit setting of a
// cwd. It never fails and writes nothing to stdout, so it never blocks Stop or a
// prompt and never adds to Claude's context: a missing var, bad JSON or a failed
// tmux call is a no-op (a failed read just makes its write a failing one too).
func HitlHook(stdin io.Reader, autoExit func(cwd string) time.Duration) {
	sock, pane, ok := strings.Cut(os.Getenv(paneVar), ",")
	if !ok || sock == "" || pane == "" {
		return
	}
	event, err := io.ReadAll(stdin)
	if err != nil {
		return
	}
	// ponytail: read-then-set, so two hooks racing (a tool run's clear against a Stop)
	// can drop an edge; make the clear an if-shell -F inside tmux if that shows up.
	// hitl-run's re-read at the deadline is Auto-exit's backstop (Spec D3).
	armed, cur := readAutoExit(sock, pane)
	var args []string
	if next := waitEdge(event, cur); next == NotWaiting && cur != NotWaiting {
		args = append(args, ";", "set-option", "-u", "-t", pane, "@waiting")
	} else if next != cur {
		args = append(args, ";", "set-option", "-t", pane, "@waiting", string(next))
	}
	if d, change := autoExitEdge(event, autoExit); change && d > 0 {
		// Re-arming restarts the full duration (FR6), even over a running countdown.
		args = append(args, ";", "set-option", "-t", pane, "@autofinish", strconv.FormatInt(time.Now().Add(d).Unix(), 10))
	} else if change && !armed.IsZero() {
		args = append(args, ";", "set-option", "-u", "-t", pane, "@autofinish")
	}
	if len(args) > 0 {
		tmuxCmd(sock, args[1:]...).Run()
	}
}

// autoExitEdge is the Auto-exit edge of hook event (its JSON) (Spec D2,
// ADR 0111): a Stop whose last_assistant_message shows contract.ClosingPhrase and
// left no background_tasks or session_crons arms for setting(the event's cwd),
// read only then, so an off (0) setting disarms; every other hooked edge
// (UserPromptSubmit, PermissionRequest, an elicitation Notification, StopFailure, a
// non-arming Stop, a tool run) disarms (d = 0). Anything else, bad JSON included,
// keeps (change false).
func autoExitEdge(event []byte, setting func(cwd string) time.Duration) (d time.Duration, change bool) {
	var e struct {
		Name    string            `json:"hook_event_name"`
		Type    string            `json:"notification_type"`
		Cwd     string            `json:"cwd"`
		Message string            `json:"last_assistant_message"`
		Tasks   []json.RawMessage `json:"background_tasks"`
		Crons   []json.RawMessage `json:"session_crons"`
	}
	if json.Unmarshal(event, &e) != nil {
		return 0, false
	}
	switch e.Name {
	case "Stop":
		if strings.Contains(e.Message, contract.ClosingPhrase) && len(e.Tasks) == 0 && len(e.Crons) == 0 {
			return setting(e.Cwd), true
		}
		return 0, true
	case "Notification":
		return 0, e.Type == "elicitation_dialog" || e.Type == "elicitation_url_dialog"
	case "StopFailure", "UserPromptSubmit", "PermissionRequest", "PostToolUse", "PostToolUseFailure":
		return 0, true
	}
	return 0, false
}

// waitEdge is the waiting bit after hook event (its JSON) given the current bit
// cur (ADR 0099 §4): Stop/StopFailure set turn; PermissionRequest (any agent) and
// an MCP elicitation Notification set ask; UserPromptSubmit clears either; a tool
// run (PostToolUse/PostToolUseFailure) clears only ask, since a background
// subagent's tools run after Stop. Anything else, bad JSON included, keeps cur.
func waitEdge(event []byte, cur Waiting) Waiting {
	var e struct {
		Name string `json:"hook_event_name"`
		Type string `json:"notification_type"`
	}
	if json.Unmarshal(event, &e) != nil {
		return cur
	}
	switch e.Name {
	case "Stop", "StopFailure":
		return WaitTurn
	case "PermissionRequest":
		return WaitAsk
	case "Notification":
		if e.Type == "elicitation_dialog" || e.Type == "elicitation_url_dialog" {
			return WaitAsk
		}
	case "UserPromptSubmit":
		return NotWaiting
	case "PostToolUse", "PostToolUseFailure":
		if cur == WaitAsk {
			return NotWaiting
		}
	}
	return cur
}
