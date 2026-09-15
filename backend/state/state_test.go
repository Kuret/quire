package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func registry(t *testing.T) *theme.Registry {
	t.Helper()
	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(themetest.New(t, nil)))
	return reg
}

func source(name, url string) *theme.Source {
	return &theme.Source{
		Name: name, Lang: "en", Theme: madara.ID, BaseURL: url,
		AddedAt: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
	}
}

func TestAddListAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	// PLAN §1.3: the shipped configuration is empty, and that is not an error.
	if got := s.List(); len(got) != 0 {
		t.Fatalf("a fresh store has %d sources, want none", len(got))
	}

	added, err := s.Add(source("Example Reader", "https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if added.ID != "example-reader" {
		t.Errorf("id = %q, want one derived from the name", added.ID)
	}

	// A second, differently-named source at another address.
	if _, err := s.Add(source("Example Reader", "https://other.invalid")); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 2 || got[1].ID != "example-reader-2" {
		t.Fatalf("ids = %v, want a suffixed second", ids(got))
	}

	// Reopening reads exactly what was written.
	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := again.List(); len(got) != 2 {
		t.Fatalf("after reload: %v", ids(got))
	}
}

func TestAddRefusesTheSameSiteTwice(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(source("Example Reader", "https://example.invalid")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(source("Another Name", "https://EXAMPLE.invalid")); err == nil {
		t.Fatal("adding the same base URL twice must be refused")
	}
}

func TestAddValidatesAgainstTheRegistry(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	bad := source("Example Reader", "https://example.invalid")
	bad.Overrides = map[string]any{"noSuchKey": true}
	if _, err := s.Add(bad); err == nil {
		t.Fatal("an unknown override key must be a validation error (PLAN §7.2)")
	}
}

func TestToggleProbeAndRemove(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(source("Example Reader", "https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if !added.IsEnabled() {
		t.Error("a new source defaults to enabled")
	}

	if err := s.SetEnabled(added.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProbe(added.ID, &theme.ProbeResult{Verdict: theme.VerdictPartial, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(added.ID)
	if !ok {
		t.Fatal("source vanished across a reload")
	}
	if got.IsEnabled() {
		t.Error("the toggle did not survive a reload")
	}
	if got.LastProbe == nil || got.LastProbe.Verdict != theme.VerdictPartial {
		t.Errorf("lastProbe = %+v, want the stored partial", got.LastProbe)
	}

	if err := reloaded.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Remove(added.ID); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("removing twice: %v, want ErrNotFound", err)
	}
}

// A caller that mutates what List handed it must not change what is stored.
func TestListReturnsCopies(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(source("Example Reader", "https://example.invalid")); err != nil {
		t.Fatal(err)
	}
	got := s.List()
	got[0].Name = "Vandalised"
	if s.List()[0].Name != "Example Reader" {
		t.Error("List handed out the stored entry itself")
	}
}

// A hand-edited or imported file with a bad entry fails at open, not later.
func TestOpenRejectsAnInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, state.FileName)
	if err := os.WriteFile(path, []byte(`{"version":1,"sources":[{"id":"x","name":"X","lang":"en","theme":"nosuchtheme","baseUrl":"https://example.invalid","addedAt":"2026-03-04T12:00:00Z"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Open(dir, registry(t)); err == nil {
		t.Fatal("a source naming an unknown theme must be refused at open")
	}
}

func ids(srcs []*theme.Source) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.ID)
	}
	return out
}
