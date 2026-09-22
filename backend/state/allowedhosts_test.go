package state_test

// AllowHost and RevokeHost are the allowedHosts editor: the only way to widen
// or narrow a source's allowedHosts after it has been added, mirroring
// SetProxy/ClearProxyAndRevoke's shape for the same kind of thing — a
// permission the user granted about one source, changeable and revocable
// after the fact.

import (
	"errors"
	"testing"

	"github.com/rickl/quire/backend/state"
)

func TestAllowHostAppendsLowercasedAndPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Example Reader", "https://api.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.AllowHost(added.ID, "CDN.Example.Invalid"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(added.ID)
	if len(got.AllowedHosts) != 1 || got.AllowedHosts[0] != "cdn.example.invalid" {
		t.Fatalf("AllowedHosts = %+v, want [cdn.example.invalid] (lower-cased)", got.AllowedHosts)
	}

	// A second, different host appends rather than replacing.
	if err := s.AllowHost(added.ID, "other.example.invalid"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(added.ID)
	if len(got.AllowedHosts) != 2 {
		t.Fatalf("AllowedHosts = %+v, want two entries", got.AllowedHosts)
	}

	// Survives a reload, which is also validate() accepting what was written.
	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("an allowed host did not survive a reload: %v", err)
	}
	reloaded, _ := again.Get(added.ID)
	if len(reloaded.AllowedHosts) != 2 {
		t.Fatalf("after reload: AllowedHosts = %+v", reloaded.AllowedHosts)
	}
}

// TestAllowHostReachesOnlyTheNamedSource is the mutation this feature exists
// to fail: allowing a host for one source must never widen another's guard.
// state.Store holds several sources; only the one named gets the entry.
func TestAllowHostReachesOnlyTheNamedSource(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Add(source("A", "https://a.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Add(source("B", "https://b.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}

	// Asked for B, deliberately not the first source added: a mutation that
	// dropped the id match entirely and just acted on the first source in the
	// list would still pass if this named A.
	if err := s.AllowHost(b.ID, "cdn.example.invalid"); err != nil {
		t.Fatal(err)
	}

	gotB, _ := s.Get(b.ID)
	if len(gotB.AllowedHosts) != 1 || gotB.AllowedHosts[0] != "cdn.example.invalid" {
		t.Fatalf("the named source B = %+v, want the host allowed", gotB.AllowedHosts)
	}
	gotA, _ := s.Get(a.ID)
	if len(gotA.AllowedHosts) != 0 {
		t.Fatalf("source A, which never asked for it, got AllowedHosts = %+v", gotA.AllowedHosts)
	}
}

func TestAllowHostRefusesADuplicate(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Example Reader", "https://api.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AllowHost(added.ID, "cdn.example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := s.AllowHost(added.ID, "cdn.example.invalid"); !errors.Is(err, state.ErrHostAlreadyAllowed) {
		t.Errorf("AllowHost of a duplicate = %v, want ErrHostAlreadyAllowed", err)
	}
	got, _ := s.Get(added.ID)
	if len(got.AllowedHosts) != 1 {
		t.Fatalf("AllowedHosts = %+v, want no duplicate stored", got.AllowedHosts)
	}
}

func TestAllowHostOnAMissingSource(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AllowHost("nope", "cdn.example.invalid"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("AllowHost on an unknown source = %v, want ErrNotFound", err)
	}
}

func TestRevokeHostRemovesOneAndLeavesTheRestPersisted(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Example Reader", "https://api.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AllowHost(added.ID, "cdn.example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := s.AllowHost(added.ID, "other.example.invalid"); err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeHost(added.ID, "cdn.example.invalid"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(added.ID)
	if len(got.AllowedHosts) != 1 || got.AllowedHosts[0] != "other.example.invalid" {
		t.Fatalf("AllowedHosts after revoke = %+v, want [other.example.invalid]", got.AllowedHosts)
	}

	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("a revoked host did not survive a reload: %v", err)
	}
	reloaded, _ := again.Get(added.ID)
	if len(reloaded.AllowedHosts) != 1 || reloaded.AllowedHosts[0] != "other.example.invalid" {
		t.Fatalf("after reload: AllowedHosts = %+v", reloaded.AllowedHosts)
	}
}

// TestRevokeHostOfSomethingNeverAllowedIsNotAnError: the end state the caller
// wants — this host not allowed — is already true, the way clearing an unset
// proxy is not an error either.
func TestRevokeHostOfSomethingNeverAllowedIsNotAnError(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Example Reader", "https://api.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeHost(added.ID, "cdn.example.invalid"); err != nil {
		t.Errorf("RevokeHost of an absent host = %v, want nil", err)
	}
}

func TestRevokeHostOnAMissingSource(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeHost("nope", "cdn.example.invalid"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("RevokeHost on an unknown source = %v, want ErrNotFound", err)
	}
}
