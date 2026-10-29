package tui

import (
	"reflect"
	"strings"
	"unsafe"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

// The description textarea is driven through its own cursor movement: bubbles v0.21 has no
// call that places the cursor at a drawn row, nor one that scrolls it, so a drawn row is
// reached by walking CursorDown from the top and a scroll is the viewport's offset plus
// the cursor riding into view.

// taOffset is the textarea's viewport, whose YOffset is the first drawn visual row.
// ponytail: bubbles keeps it private; read through reflect, and TestTextareaViewportField pins the layout.
func taOffset(ta *textarea.Model) *viewport.Model {
	v := reflect.ValueOf(ta).Elem().FieldByName("viewport")
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Interface().(*viewport.Model)
}

func taAtTop(ta textarea.Model) bool { return ta.Line() == 0 && ta.LineInfo().RowOffset == 0 }

func taAtBottom(ta textarea.Model) bool {
	li := ta.LineInfo()
	return ta.Line() == ta.LineCount()-1 && li.RowOffset == li.Height-1
}

// taStep moves the cursor one visual row (up or down) and reports whether it moved.
func taStep(ta *textarea.Model, down bool) bool {
	l, li, c := ta.Line(), ta.LineInfo(), ta.LineInfo().StartColumn+ta.LineInfo().ColumnOffset
	if down {
		ta.CursorDown()
	} else {
		ta.CursorUp()
	}
	nl, nli := ta.Line(), ta.LineInfo()
	return nl != l || nli.RowOffset != li.RowOffset || nli.StartColumn+nli.ColumnOffset != c
}

// taRows is the cursor's visual row and the count of visual rows, soft wraps included.
// It walks every visual row, so a wheel event on a very long description costs O(rows);
// wheel events are not merged here, which is acceptable at description sizes.
func taRows(ta textarea.Model) (cur, total int) {
	for !taAtTop(ta) && taStep(&ta, false) {
		cur++
	}
	total = 1
	for !taAtBottom(ta) && taStep(&ta, true) {
		total++
	}
	return cur, total
}

// taClick puts the cursor on drawn row r (counted from the top of the view) at cell col,
// allowing for the scroll offset. A row past the text goes to the end of the text, a cell
// past a row's end to that row's end.
func taClick(ta textarea.Model, r, col int) textarea.Model {
	vis := taOffset(&ta).YOffset + r
	for !taAtTop(ta) && taStep(&ta, false) {
	}
	ta.CursorStart()
	for i := 0; i < vis; i++ {
		if !taStep(&ta, true) {
			break
		}
	}
	if taAtBottom(ta) && vis > 0 {
		if _, total := taRows(ta); vis >= total {
			ta.CursorEnd()
			return ta
		}
	}
	li := ta.LineInfo()
	rs := []rune(taLine(ta))
	end := li.StartColumn + li.Width
	if li.RowOffset < li.Height-1 {
		end-- // a soft-wrapped row ends on its trailing space
	}
	c, w := li.StartColumn, 0
	for c < end && c < len(rs) {
		w += ansi.StringWidth(string(rs[c]))
		if w > col {
			break
		}
		c++
	}
	ta.SetCursor(min(c, end))
	return ta
}

// taLine is the logical line the cursor is on.
func taLine(ta textarea.Model) string { return strings.Split(ta.Value(), "\n")[ta.Line()] }

// taScroll scrolls the view by delta visual rows, no further than the text allows, and
// moves the cursor the fewest rows that keep it in view. A text that fits does not scroll.
func taScroll(ta textarea.Model, delta int) textarea.Model {
	cur, total := taRows(ta)
	h := ta.Height()
	if total <= h {
		return ta
	}
	vp := taOffset(&ta)
	off := min(max(vp.YOffset+delta, 0), total-h)
	vp.YOffset = off
	for ; cur < off; cur++ {
		taStep(&ta, true)
	}
	for ; cur > off+h-1; cur-- {
		taStep(&ta, false)
	}
	return ta
}
