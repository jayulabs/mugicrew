package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub scripts SQL: out keyed by a substring of the statement; every statement is kept.
func stub(t *testing.T, out map[string]string, err error) *[]string {
	t.Helper()
	orig := SQL
	t.Cleanup(func() { SQL = orig })
	var seen []string
	SQL = func(_, q string) (string, error) {
		seen = append(seen, q)
		if err != nil {
			return "", err
		}
		for k, v := range out {
			if strings.Contains(q, k) {
				return v, nil
			}
		}
		return "", nil
	}
	return &seen
}

func TestRemote(t *testing.T) {
	stub(t, map[string]string{"dolt_remotes": "url\nfile:///mnt/bk\n"}, nil)
	if url, ok, err := Remote("b"); err != nil || !ok || url != "file:///mnt/bk" {
		t.Errorf("present: %q %v %v", url, ok, err)
	}
	stub(t, map[string]string{"dolt_remotes": "url\n"}, nil)
	if _, ok, err := Remote("b"); err != nil || ok {
		t.Errorf("absent: ok %v err %v", ok, err)
	}
	stub(t, nil, errors.New("down"))
	if _, _, err := Remote("b"); err == nil {
		t.Error("a runner error must surface")
	}
}

func TestUnpushed(t *testing.T) {
	stub(t, map[string]string{"dolt_log": "count(*),min(date)\n0,\n"}, nil)
	if n, old, err := Unpushed("b"); err != nil || n != 0 || !old.IsZero() {
		t.Errorf("zero: %d %v %v", n, old, err)
	}
	stub(t, map[string]string{"dolt_log": "count(*),min(date)\n3,2026-10-28 01:02:03.5\n"}, nil)
	n, old, err := Unpushed("b")
	if err != nil || n != 3 || !old.Equal(time.Date(2026, 10, 28, 1, 2, 3, 500_000_000, time.UTC)) {
		t.Errorf("N: %d %v %v", n, old, err)
	}
}

func TestPushCall(t *testing.T) {
	for _, c := range []struct{ user, want string }{
		{"", "CALL DOLT_PUSH('backup', 'main')"},
		{"bob", "CALL DOLT_PUSH('--user', 'bob', 'backup', 'main')"},
		{"o'neil", "CALL DOLT_PUSH('--user', 'o''neil', 'backup', 'main')"},
	} {
		seen := stub(t, nil, nil)
		if err := Push("b", c.user); err != nil {
			t.Fatal(err)
		}
		if got := (*seen)[0]; got != c.want || strings.Contains(got, "force") {
			t.Errorf("user %q: %q, want %q", c.user, got, c.want)
		}
	}
	stub(t, nil, errors.New("rejected"))
	if err := Push("b", ""); err == nil || err.Error() != "rejected" {
		t.Errorf("err = %v", err)
	}
}

func TestStale(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		oldest time.Time
		want   bool
	}{
		{"just under 24h", now.Add(-24*time.Hour + time.Minute), false},
		{"just over 24h", now.Add(-24*time.Hour - time.Minute), true},
		{"zero unpushed", time.Time{}, false},
	} {
		if got := Stale(tc.oldest, now); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestSameFilesystem(t *testing.T) {
	beads := t.TempDir()
	if !SameFilesystem("file://"+beads, beads) {
		t.Error("same dir not same filesystem")
	}
	if !SameFilesystem("file://"+filepath.Join(beads, "not", "yet"), beads) {
		t.Error("uncreated child should resolve via nearest parent")
	}
	if SameFilesystem("https://dolt.example/db", beads) {
		t.Error("non-file URL counted as same filesystem")
	}
	if fi, err := os.Stat("/dev/shm"); err == nil && fi.IsDir() {
		if SameFilesystem("file:///dev/shm", beads) == SameFilesystem("file://"+beads, beads) {
			t.Skip("/dev/shm shares a device with the temp dir")
		}
	}
}

func TestDoltSQLKeepsExecErrorWhenDoltPrintsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dolt-server.port"), []byte("3307\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"dolt_database":"db"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	_, err := doltSQL(dir, "select 1")
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Fatalf("want a non-empty error, got %v", err)
	}
	if !strings.Contains(err.Error(), "dolt") {
		t.Fatalf("error should name the exec failure, got %q", err)
	}
}
