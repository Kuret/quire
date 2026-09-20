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

// TestConfirmSelfHostedViaProxyNeedsTheProxy. The confirmation that records no
// address stands on the proxy being there; without one it is the bare "let this
// one through" flag that theme.SelfHosted exists to refuse.
func TestConfirmSelfHostedViaProxyNeedsTheProxy(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Zima", "http://zima.example:8084"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ConfirmSelfHostedViaProxy(added.ID); !errors.Is(err, state.ErrNoProxyToConfirm) {
		t.Fatalf("ConfirmSelfHostedViaProxy with no proxy = %v, want ErrNoProxyToConfirm", err)
	}
	if got, _ := s.Get(added.ID); got.SelfHosted != nil {
		t.Fatal("a confirmation was recorded with nothing to stand on")
	}

	// With the proxy on it, the same call records what was agreed.
	if err := s.SetProxy(added.ID, "http://localhost:1055"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmSelfHostedViaProxy(added.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(added.ID)
	if got.SelfHosted == nil || !got.SelfHosted.ViaProxy {
		t.Fatalf("stored confirmation = %+v", got.SelfHosted)
	}
	if got.SelfHosted.ConfirmedAddr != "" {
		t.Errorf("ConfirmedAddr = %q; nothing resolved, so nothing may be recorded", got.SelfHosted.ConfirmedAddr)
	}
	if got.SelfHosted.ConfirmedAt.IsZero() {
		t.Error("ConfirmedAt is zero; the record should say when consent was given")
	}

	// And taking the proxy away takes the ground out from under it, so the
	// store refuses rather than leaving a confirmation standing on nothing.
	if err := s.SetProxy(added.ID, ""); err == nil {
		t.Error("clearing the proxy left a proxy-confirmation behind it")
	}
	if still, _ := s.Get(added.ID); still.Proxy != "http://localhost:1055" {
		t.Errorf("proxy = %q, want the refused change rolled back", still.Proxy)
	}
}

// TestClearProxyAndRevokeTakesBothWithIt: a ViaProxy confirmation stands on
// the proxy, so clearing the proxy has to take the confirmation with it in the
// same call, and the result has to be one theme.Source.validate() accepts on
// its own — which a reload proves, the way it does for every other confirmed
// shape in this package.
func TestClearProxyAndRevokeTakesBothWithIt(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Zima", "http://zima.example:8084"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxy(added.ID, "http://localhost:1055"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmSelfHostedViaProxy(added.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearProxyAndRevoke(added.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(added.ID)
	if got.Proxy != "" {
		t.Errorf("proxy after ClearProxyAndRevoke = %q, want empty", got.Proxy)
	}
	if got.SelfHosted != nil {
		t.Errorf("SelfHosted after ClearProxyAndRevoke = %+v, want revoked", got.SelfHosted)
	}

	// Surviving a reload is what proves validate() accepts the shape that was
	// written, not just that this process's in-memory copy looks right.
	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("a cleared proxy did not survive a reload: %v", err)
	}
	reloaded, _ := again.Get(added.ID)
	if reloaded.Proxy != "" || reloaded.SelfHosted != nil {
		t.Fatalf("after reload: proxy=%q selfHosted=%+v", reloaded.Proxy, reloaded.SelfHosted)
	}
}

// TestClearProxyAndRevokeLeavesAnAddressConfirmationAlone: a source confirmed
// by a resolved address does not lose that confirmation just because it also
// happened to carry a proxy — its evidence is the address, not the proxy.
func TestClearProxyAndRevokeLeavesAnAddressConfirmationAlone(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Shelfmark", "http://shelfmark.internal.invalid:8084"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxy(added.ID, "http://localhost:1055"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmSelfHosted(added.ID, "100.100.0.1"); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearProxyAndRevoke(added.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(added.ID)
	if got.Proxy != "" {
		t.Errorf("proxy after ClearProxyAndRevoke = %q, want empty", got.Proxy)
	}
	if got.SelfHosted == nil || got.SelfHosted.ConfirmedAddr != "100.100.0.1" {
		t.Fatalf("an address confirmation was disturbed by clearing the proxy: %+v", got.SelfHosted)
	}
}

func TestClearProxyAndRevokeOnAMissingSource(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClearProxyAndRevoke("nope"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("ClearProxyAndRevoke on an unknown source = %v, want ErrNotFound", err)
	}
}
