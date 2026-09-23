package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBookMutoolPathNotFound: without a "mutool" beside the running
// executable (the ordinary case for `go test`'s own binary), bookMutoolPath
// must fail rather than guess some other path.
func TestBookMutoolPathNotFound(t *testing.T) {
	if _, err := bookMutoolPath(); err == nil {
		t.Fatal("expected an error: go test's binary has no mutool beside it")
	}
}

// TestBookMutoolPathFound: a "mutool" placed next to the running executable
// is what bookMutoolPath resolves to.
func TestBookMutoolPathFound(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	fake := filepath.Join(filepath.Dir(exe), "mutool")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Skipf("cannot write beside the test binary: %v", err)
	}
	t.Cleanup(func() { os.Remove(fake) })

	got, err := bookMutoolPath()
	if err != nil {
		t.Fatalf("bookMutoolPath: %v", err)
	}
	if got != fake {
		t.Errorf("bookMutoolPath() = %q, want %q", got, fake)
	}
}
