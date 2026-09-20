package state_test

import (
	"errors"
	"testing"

	"github.com/rickl/quire/backend/state"
)

// SetProxy is the only writer of theme.Source.Proxy, the way ConfirmSelfHosted
// is the only writer of the confirmation beside it. Both are things the *user*
// asserted about one source, and a single writer is what makes "nothing else
// sets this" checkable.

func TestSetProxyRecordsAndClears(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Zima", "http://zima.example:8084"))
	if err != nil {
		t.Fatal(err)
	}
	if added.Proxy != "" {
		t.Fatalf("a freshly added source already has a proxy: %q", added.Proxy)
	}

	if err := s.SetProxy(added.ID, "http://localhost:1055"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(added.ID)
	if !ok || got.Proxy != "http://localhost:1055" {
		t.Fatalf("stored proxy = %q, want the one that was set", got.Proxy)
	}

	// It survives a reload, which is also the store validating it on the way
	// back in.
	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("a stored proxy did not survive a reload: %v", err)
	}
	if reloaded, _ := again.Get(added.ID); reloaded.Proxy != "http://localhost:1055" {
		t.Errorf("proxy after reload = %q", reloaded.Proxy)
	}

	// A permission the user cannot take back is not one they are in charge of.
	if err := s.SetProxy(added.ID, ""); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := s.Get(added.ID); cleared.Proxy != "" {
		t.Errorf("proxy after clearing = %q, want empty", cleared.Proxy)
	}
}

// TestSetProxyRefusesWhatItCannotUse: validated at the point it is set, so the
// user is told by the field they typed into rather than by a chapter that will
// not download an hour later.
func TestSetProxyRefusesWhatItCannotUse(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Zima", "http://zima.example:8084"))
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{
		"localhost:1055",
		"socks4://127.0.0.1:1080",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"http://localhost:1055/proxy",
		"http://",
	} {
		if err := s.SetProxy(added.ID, raw); !errors.Is(err, state.ErrBadProxy) {
			t.Errorf("SetProxy(%q) = %v, want ErrBadProxy", raw, err)
		}
		if got, _ := s.Get(added.ID); got.Proxy != "" {
			t.Fatalf("a refused proxy %q was stored anyway as %q", raw, got.Proxy)
		}
	}
}

func TestSetProxyOnAMissingSource(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxy("nope", "http://localhost:1055"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("SetProxy on an unknown source = %v, want ErrNotFound", err)
	}
}
