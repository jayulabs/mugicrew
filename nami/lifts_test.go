package nami

import (
	"strings"
	"testing"
)

func TestNamiPromptLifts(t *testing.T) {
	charting := strings.Join(strings.Fields(chartingPrompt("s", "c")), " ")
	if !strings.Contains(charting, "spans more than one Checkpoint") {
		t.Error("charting prompt lacks the multi-Checkpoint hand-back")
	}
	if !strings.Contains(researchPrompt("r"), "Never edit an Initiative's body") {
		t.Error("research prompt lacks the Initiative-body clause")
	}
}
