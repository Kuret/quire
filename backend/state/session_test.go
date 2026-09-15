package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/state"
)

// A session that ends properly leaves nothing behind, so the next launch has
// nothing to report.
func TestACleanSessionLeavesNoMarker(t *testing.T) {
	dir := t.TempDir()

	session, crashed, err := state.OpenSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if crashed {
		t.Error("a first run reported a previous crash")
	}
	if _, err := os.Stat(filepath.Join(dir, state.SessionFileName)); err != nil {
		t.Errorf("no marker while the session is open: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	_, crashed, err = state.OpenSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if crashed {
		t.Error("a clean previous session was reported as a crash")
	}
}

// The case that matters: the backend is killed — the OOM killer on a tablet
// sharing ~2 GB with xochitl is the realistic one — so Close never runs and the
// marker survives. SIGKILL cannot be caught, so the next launch is the only
// chance to say anything.
func TestAKilledSessionIsNoticedNextLaunch(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := state.OpenSession(dir); err != nil {
		t.Fatal(err)
	}
	// No Close: this is the process dying.

	session, crashed, err := state.OpenSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !crashed {
		t.Fatal("a session that never closed was not noticed")
	}

	// And it is reported once, not for ever after.
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, crashed, err = state.OpenSession(dir); err != nil {
		t.Fatal(err)
	} else if crashed {
		t.Error("the crash was reported a second time")
	}
}

func TestClosingTwiceIsSafe(t *testing.T) {
	session, _, err := state.OpenSession(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := session.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}
}

// The marker lives beside sources.json, and creating it must not disturb it.
func TestOpenSessionCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	session, _, err := state.OpenSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(session.Path()) != dir {
		t.Errorf("marker at %q, want it in %q", session.Path(), dir)
	}
}
