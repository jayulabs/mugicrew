package selfexe

import "testing"

func TestPathIsCapturedAtStartup(t *testing.T) {
	got, err := Path()
	if err != nil || got != startPath || got == "" {
		t.Fatalf("Path() = %q, %v; want the startup path %q", got, err, startPath)
	}
}
