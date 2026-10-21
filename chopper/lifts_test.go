package chopper

import (
	"strings"
	"testing"
)

func TestCheckupPromptLifts(t *testing.T) {
	got := strings.Join(strings.Fields(checkupPrompt("mc-x")), " ")
	for _, want := range []string{"history, not liveness", "stays open"} {
		if !strings.Contains(got, want) {
			t.Errorf("checkup prompt lacks %q", want)
		}
	}
}
