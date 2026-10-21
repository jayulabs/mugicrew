package robin

import (
	"strings"
	"testing"
)

func TestRetrospectiveHorizonSource(t *testing.T) {
	if !strings.Contains(strings.Join(strings.Fields(retrospectiveTmpl), " "), "rotated session log, then the current one") {
		t.Error("retrospective prompt lacks the horizon source")
	}
}
