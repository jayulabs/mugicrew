package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStartPprofWritesProfile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cpu.prof")
	t.Setenv("MUGICREW_PPROF", p)
	var warn bytes.Buffer
	stop := startPprof(&warn)
	stop()
	if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
		t.Fatalf("profile missing or empty: %v", err)
	}
	if warn.Len() != 0 {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
}

func TestStartPprofUnsetWritesNothing(t *testing.T) {
	t.Setenv("MUGICREW_PPROF", "")
	var warn bytes.Buffer
	startPprof(&warn)()
	if warn.Len() != 0 {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
}

func TestStartPprofUnwritablePathWarns(t *testing.T) {
	t.Setenv("MUGICREW_PPROF", filepath.Join(t.TempDir(), "no", "such", "dir", "cpu.prof"))
	var warn bytes.Buffer
	startPprof(&warn)()
	if warn.Len() == 0 {
		t.Fatal("want a warning on create failure")
	}
}
