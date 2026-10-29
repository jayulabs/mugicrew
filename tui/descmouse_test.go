package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
)

// TestTextareaViewportField fails loudly if a bubbles bump drops or retypes the private
// viewport field taOffset reads through reflect.
func TestTextareaViewportField(t *testing.T) {
	f, ok := reflect.TypeOf(textarea.Model{}).FieldByName("viewport")
	if !ok {
		t.Fatal("textarea.Model has no viewport field; taOffset (descmouse.go) must be reworked")
	}
	if f.Type != reflect.TypeOf((*viewport.Model)(nil)) {
		t.Fatalf("textarea viewport is %v, want *viewport.Model; taOffset must be reworked", f.Type)
	}
}
