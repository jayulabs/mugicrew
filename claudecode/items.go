package claudecode

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Item is one thing a transcript shows, in file order (timestamps are labels,
// never a sort key: they are not monotonic). Kind is prompt, human, interrupt,
// text, tool, result, apierr or continued (Text is the destination session id).
type Item struct {
	Kind  string
	TS    time.Time // line timestamp, local
	Text  string    // human/prompt/text/result body; apierr text
	Name  string    // tool name
	ID    string    // tool_use id, or tool_result's tool_use_id
	IsErr bool      // tool_result is_error (absent = false)
	Full  string    // tool: the whole input (first itemKeys hit, else JSON)
}

// itemKeys are the tool_use input fields that say what a call acts on, best first.
var itemKeys = []string{"command", "file_path", "notebook_path", "pattern", "url", "query", "path", "description"}

// ItemParser turns transcript bytes into Items incrementally: Write takes whatever
// was read so far and returns the items of its completed lines; a torn trailing
// line waits for its newline. The format is undocumented, so unknown or malformed
// lines are skipped silently.
type ItemParser struct {
	partial   []byte
	sawPrompt bool
	seen      map[string]bool // uuids of lines already parsed: a handoff destination repeats them
	cost      bool            // a cost-state line, with no continued-in after it
}

func (p *ItemParser) Write(b []byte) []Item {
	p.partial = append(p.partial, b...)
	end := bytes.LastIndexByte(p.partial, '\n') + 1
	var out []Item
	for _, l := range bytes.Split(p.partial[:end], []byte("\n")) {
		out = append(out, p.line(l)...)
	}
	p.partial = append(p.partial[:0], p.partial[end:]...)
	return out
}

func (p *ItemParser) line(raw []byte) []Item {
	var l struct {
		Type, Timestamp   string
		UUID              string
		ContinuedIn       string `json:"continuedInSessionId"`
		IsMeta            bool
		IsApiErrorMessage bool
		Message           struct{ Content json.RawMessage }
	}
	if json.Unmarshal(raw, &l) != nil {
		return nil
	}
	switch l.Type {
	case "cost-state":
		p.cost = true
		return nil
	case "continued-in":
		p.cost = false
		if l.ContinuedIn == "" {
			return nil
		}
		return []Item{{Kind: "continued", Text: l.ContinuedIn}}
	}
	if (l.Type != "user" && l.Type != "assistant") || l.IsMeta {
		return nil
	}
	if l.UUID != "" {
		if p.seen[l.UUID] {
			return nil
		}
		if p.seen == nil {
			p.seen = map[string]bool{}
		}
		p.seen[l.UUID] = true
	}
	ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	ts = ts.Local()
	var s string
	if json.Unmarshal(l.Message.Content, &s) == nil {
		if strings.HasPrefix(s, "<") {
			return nil
		}
		k := "human"
		if !p.sawPrompt {
			k, p.sawPrompt = "prompt", true
		}
		return []Item{{Kind: k, TS: ts, Text: s}}
	}
	var bs []struct {
		Type, Text, Name, ID string
		ToolUseID            string `json:"tool_use_id"`
		Input                map[string]any
		Content              json.RawMessage
		IsError              bool `json:"is_error"`
	}
	if json.Unmarshal(l.Message.Content, &bs) != nil {
		return nil
	}
	var out []Item
	for _, b := range bs {
		switch {
		case l.IsApiErrorMessage && l.Type == "assistant":
			out = append(out, Item{Kind: "apierr", TS: ts, Text: b.Text})
		case b.Type == "text" && l.Type == "assistant":
			out = append(out, Item{Kind: "text", TS: ts, Text: strings.TrimSpace(b.Text)})
		case b.Type == "text" && strings.HasPrefix(b.Text, "[Request interrupted"):
			out = append(out, Item{Kind: "interrupt", TS: ts, Text: b.Text})
		case b.Type == "tool_use":
			full := ""
			for _, k := range itemKeys {
				if v, ok := b.Input[k].(string); ok && v != "" {
					full = v
					break
				}
			}
			if full == "" {
				j, _ := json.Marshal(b.Input)
				full = string(j)
			}
			out = append(out, Item{Kind: "tool", TS: ts, Name: b.Name, ID: b.ID, Full: full})
		case b.Type == "tool_result":
			out = append(out, Item{Kind: "result", TS: ts, Text: strings.TrimRight(resultText(b.Content), "\n"), ID: b.ToolUseID, IsErr: b.IsError})
		}
	}
	return out
}

// retryPolls is how many polls a handoff destination's file is waited for (5 s at
// the 250 ms follow poll) before the view ends at the marker alone.
const retryPolls = 20

// Tail reads a session's transcript incrementally and, at a continued-in record,
// goes on into the destination session (found with the same glob), so one Poll loop
// serves a one-shot read and a live follow. A destination whose id was already
// visited is not followed again (chains end, cycles cannot loop), and a line whose
// uuid was already parsed is skipped.
type Tail struct {
	p       ItemParser
	id      string
	off     int64
	started bool // the first file was read
	whole   bool // the file is finished: its last line needs no newline
	handed  bool // a continued-in was met
	next    string
	tries   int
	visited map[string]bool
}

func NewTail(sessionID string) *Tail {
	return &Tail{id: sessionID, visited: map[string]bool{sessionID: true}}
}

// Poll returns the items appended since the last call, crossing into a destination
// found along the way.
func (t *Tail) Poll() []Item {
	var out []Item
	for {
		if t.next != "" {
			if _, err := transcript(t.next); err != nil {
				t.tries++
				return out
			}
			t.id, t.off, t.next, t.tries = t.next, 0, "", 0
			t.visited[t.id] = true
			t.p.partial, t.p.cost = nil, false
		}
		path, err := transcript(t.id)
		if err != nil {
			return out
		}
		f, err := os.Open(path)
		if err != nil {
			return out
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return out
		}
		t.started = true
		t.off = min(t.off, fi.Size())
		fresh := make([]byte, fi.Size()-t.off)
		n, _ := f.ReadAt(fresh, t.off)
		f.Close()
		fresh = fresh[:n]
		if t.whole {
			fresh = append(fresh[:len(fresh):len(fresh)], '\n')
		}
		t.off += int64(n)
		for _, it := range t.p.Write(fresh) {
			if it.Kind == "continued" {
				if t.visited[it.Text] {
					t.p.cost = true // a visited destination is the end of the follow
					continue
				}
				t.next, t.handed = it.Text, true
			}
			out = append(out, it)
		}
		if t.next == "" {
			return out
		}
	}
}

// Started reports whether the session's own transcript has been read yet.
func (t *Tail) Started() bool { return t.started }

// Handed reports whether the conversation moved to another session, so the
// recorded session's end no longer ends a follow.
func (t *Tail) Handed() bool { return t.handed }

// Ended reports a follow that went through a handoff and is over: the destination
// wrote its cost-state with no continued-in after it, or never appeared.
func (t *Tail) Ended() bool {
	return t.handed && ((t.next == "" && t.p.cost) || t.tries >= retryPolls)
}

// Items reads a session's whole transcript, following continued-in handoffs; ok is
// false when the file is gone (pruned) or not yet written.
func Items(sessionID string) (items []Item, ok bool) {
	t := NewTail(sessionID)
	t.whole = true
	items = t.Poll()
	return items, t.Started()
}

// resultText is a tool_result's text: its content is a string or text blocks.
func resultText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var bs []struct{ Type, Text string }
	if json.Unmarshal(c, &bs) == nil {
		for _, b := range bs {
			if b.Type == "text" {
				return b.Text
			}
		}
	}
	return ""
}
