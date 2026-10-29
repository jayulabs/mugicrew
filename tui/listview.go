package tui

import "github.com/charmbracelet/bubbles/viewport"

// A drawer list's viewport (ADR 0374): Following centres the selected row as followOffset
// does and keeps no state; the wheel puts the list in Scrolled, which holds the identity of
// the top visible row so refreshes and resizes keep it on top; a key snaps it back.

// offsetFn is a list window's first line for n lines in a window lh tall, selected at sel.
type offsetFn func(n, lh, sel int) int

// anchorSpan caps how many earlier rows a Scrolled list remembers to fall back on.
const anchorSpan = 256

// listView is one list's viewport mode. The zero value is Following.
type listView struct {
	scrolled bool     // the wheel scrolled it: the anchor row stays on top
	anchor   string   // Scrolled: the key of the top visible line
	prev     []string // Scrolled: the keys of the lines above the anchor, nearest first
	off      int      // the offset the wheel or the snap-back last settled on
	resume   bool     // a key snapped it back: Following from off while the selection is in view
}

// offsets is the window offset lv gives a list whose lines are keyed keys; nil is today's
// centred follow. While Scrolled the first of the anchor and the rows before it that still
// exists is held on top, and with none left the list follows the selection again.
func (v listView) offsets(keys []string) offsetFn {
	if !v.scrolled && !v.resume {
		return nil
	}
	held := v.held(keys)
	return func(n, lh, sel int) int {
		top := max(n-lh, 0)
		switch {
		case held >= 0:
			return min(held, top)
		case v.resume && sel >= min(v.off, top) && sel < min(v.off, top)+lh:
			return min(v.off, top)
		}
		return followOffset(n, lh, sel)
	}
}

// held is the index of the first of the anchor and the rows before it that keys still has, -1
// when the list is not Scrolled or none survives.
func (v listView) held(keys []string) int {
	if !v.scrolled {
		return -1
	}
	at := make(map[string]int, len(keys))
	for i, k := range keys {
		at[k] = i
	}
	for _, k := range append([]string{v.anchor}, v.prev...) {
		if i, ok := at[k]; ok {
			return i
		}
	}
	return -1
}

// scrolledTo is a Scrolled listView whose top line is keys[top].
func scrolledTo(keys []string, top int) listView {
	prev := make([]string, 0, min(top, anchorSpan))
	for k := top - 1; k >= 0 && len(prev) < anchorSpan; k-- {
		prev = append(prev, keys[k])
	}
	return listView{scrolled: true, anchor: keys[top], prev: prev, off: top}
}

// anchorKeys names every line: a gap (an empty key) takes the key of the row below it, so
// the wheel can rest on it.
func anchorKeys(keys []string) []string {
	next := "~end"
	for i := len(keys) - 1; i >= 0; i-- {
		if keys[i] == "" {
			keys[i] = "~" + next
		} else {
			next = keys[i]
		}
	}
	return keys
}

// wheelDrawer is the drawer viewport's offset after the wheel scrolled it by delta lines, as
// pgup/pgdown scroll it.
func wheelDrawer(vp viewport.Model, delta int) int {
	return min(max(vp.YOffset+delta, 0), max(vp.TotalLineCount()-vp.Height, 0))
}
