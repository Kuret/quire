package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/state"
)

// ConfirmSelfHosted is the only way the exemption of PLAN §7.4 ever gets
// recorded, so these tests are about what it refuses and about the fact that
// nothing else writes it. The guard-side scoping lives in
// backend/fetch/selfhosted_test.go.

func TestConfirmSelfHostedRecordsWhatWasAgreed(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Shelfmark", "http://shelfmark.internal.invalid:8084"))
	if err != nil {
		t.Fatal(err)
	}

	// Until the user says so, there is nothing there.
	if added.SelfHosted != nil {
		t.Fatalf("a freshly added source is already confirmed: %+v", added.SelfHosted)
	}

	if err := s.ConfirmSelfHosted(added.ID, "100.100.0.1"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(added.ID)
	if !ok || got.SelfHosted == nil {
		t.Fatal("the confirmation was not stored")
	}
	if got.SelfHosted.ConfirmedAddr != "100.100.0.1" {
		t.Errorf("ConfirmedAddr = %q, want the address the caller resolved", got.SelfHosted.ConfirmedAddr)
	}
	if got.SelfHosted.ConfirmedAt.IsZero() {
		t.Error("ConfirmedAt is zero; the record should say when consent was given")
	}

	// What Get hands back is a copy, confirmation and all. Sharing the record
	// would let any caller holding a source rewrite the address the user
	// agreed to — in memory, in the store, without a save and without a trace.
	got.SelfHosted.ConfirmedAddr = "192.168.1.11"
	if still, _ := s.Get(added.ID); still.SelfHosted.ConfirmedAddr != "100.100.0.1" {
		t.Fatalf("ConfirmedAddr = %q after a caller edited its copy, want the stored one",
			still.SelfHosted.ConfirmedAddr)
	}

	// It survives a reload, which is also the store validating it on the way
	// back in.
	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _ := again.Get(added.ID)
	if reloaded.SelfHosted == nil || reloaded.SelfHosted.ConfirmedAddr != "100.100.0.1" {
		t.Fatalf("after reload: %+v", reloaded.SelfHosted)
	}

	// And it can be taken back. A permission that cannot be withdrawn is not
	// one the user is in charge of.
	if err := again.RevokeSelfHosted(added.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := again.Get(added.ID); after.SelfHosted != nil {
		t.Fatalf("after revoke: %+v", after.SelfHosted)
	}
}

func TestConfirmSelfHostedRefusesAddressesItCannotMean(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Shelfmark", "http://shelfmark.internal.invalid:8084"))
	if err != nil {
		t.Fatal(err)
	}

	for _, addr := range []string{
		"",                           // nothing at all
		"shelfmark.internal.invalid", // a name where an address belongs
		"93.184.216.34",              // public: needs no confirmation
		"127.0.0.1",                  // the device itself
		"169.254.169.254",            // the cloud metadata service
		"224.0.0.1",                  // multicast
		"192.168.1.10, 10.0.0.5",     // two, so neither is what was agreed
	} {
		if err := s.ConfirmSelfHosted(added.ID, addr); !errors.Is(err, state.ErrBadSelfHosted) {
			t.Errorf("ConfirmSelfHosted(%q) = %v, want ErrBadSelfHosted", addr, err)
		}
	}
	if got, _ := s.Get(added.ID); got.SelfHosted != nil {
		t.Fatalf("a refused confirmation was stored anyway: %+v", got.SelfHosted)
	}
}

// TestStoredConfirmationMustBeComplete is the hand-edited-file case: the
// exemption must not be reachable by adding one plausible-looking object to
// sources.json. A store that cannot be trusted is refused on the way in rather
// than loaded with the bad entry dropped, because "your sources file is wrong"
// is a better outcome than a silently different security posture.
func TestStoredConfirmationMustBeComplete(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "no address recorded", json: `{"confirmedAt":"2026-09-20T09:30:00Z"}`},
		{name: "a name instead of an address", json: `{"confirmedAddr":"shelfmark.internal.invalid","confirmedAt":"2026-09-20T09:30:00Z"}`},
		{name: "an address no confirmation can cover", json: `{"confirmedAddr":"169.254.169.254","confirmedAt":"2026-09-20T09:30:00Z"}`},
		{name: "no date", json: `{"confirmedAddr":"192.168.1.10"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"version":4,"sources":[{"id":"shelfmark","name":"Shelfmark","lang":"en",
			  "theme":"madara","baseUrl":"http://shelfmark.internal.invalid:8084",
			  "addedAt":"2026-09-20T00:00:00Z","selfHosted":` + tt.json + `}]}`
			if err := os.WriteFile(filepath.Join(dir, state.FileName), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := state.Open(dir, registry(t))
			if err == nil {
				t.Fatal("the store loaded a confirmation that records nothing")
			}
			if !strings.Contains(err.Error(), "selfHosted") {
				t.Fatalf("err = %v, want it to name the field at fault", err)
			}
		})
	}
}
