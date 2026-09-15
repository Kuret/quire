package theme_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

// fake is a minimal Theme for the registry's own tests. The theme packages
// test their real behaviour against fixtures; this one only has to have an ID
// and a score.
type fake struct {
	id            string
	score         int
	allowedHosts  []string
	suggestedName string
}

func (f *fake) ID() string                  { return f.id }
func (f *fake) Fingerprint(*probe.Page) int { return f.score }

func (f *fake) AllowedHosts() []string { return f.allowedHosts }

func (f *fake) SuggestedName() string { return f.suggestedName }
func (f *fake) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	return nil, nil
}
func (f *fake) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, nil
}
func (f *fake) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, nil
}
func (f *fake) Pages(context.Context, *theme.Source, string) ([]string, error) { return nil, nil }

// validating wraps fake with an override declaration.
type validating struct {
	*fake
	spec theme.OverrideSpec
}

func (v *validating) ValidateOverrides(raw map[string]any) error { return v.spec.Validate(raw) }
func (v *validating) OverrideKeys() []theme.OverrideDoc          { return v.spec.Docs() }

func TestRegisterAndLookup(t *testing.T) {
	reg := theme.NewRegistry()

	if err := reg.Register(&fake{id: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&fake{id: "beta"}); err != nil {
		t.Fatal(err)
	}

	if _, ok := reg.Lookup("alpha"); !ok {
		t.Error("alpha was not found after registration")
	}
	if _, ok := reg.Lookup("gamma"); ok {
		t.Error("gamma was found but never registered")
	}
	if got := strings.Join(reg.IDs(), ","); got != "alpha,beta" {
		t.Errorf("IDs = %q, want alpha,beta", got)
	}

	// A duplicate must be an error, not a silent overwrite: a shadowed theme
	// shows up months later as a mysteriously wrong fingerprint.
	if err := reg.Register(&fake{id: "alpha"}); err == nil {
		t.Error("registering a duplicate ID was allowed")
	}
	if err := reg.Register(&fake{id: ""}); err == nil {
		t.Error("registering an empty ID was allowed")
	}
}

func TestFingerprintFanOut(t *testing.T) {
	reg := theme.NewRegistry()
	for _, f := range []*fake{
		{id: "alpha", score: 20},
		{id: "beta", score: 91},
		{id: "delta", score: 20},
		{id: "charlie", score: 0},
	} {
		if err := reg.Register(f); err != nil {
			t.Fatal(err)
		}
	}
	// The generic escape hatch must not compete: PLAN §7.5's "unrecognised"
	// verdict has to stay available as an answer.
	if err := reg.Register(&fake{id: theme.GenericID, score: 100}); err != nil {
		t.Fatal(err)
	}

	page := probe.NewPage(mustURL(t, "https://example.invalid/"), nil, 200, nil, []byte("<html></html>"))
	scores := reg.Fingerprint(page)

	if len(scores) != 4 {
		t.Fatalf("got %d scores, want 4 (generic excluded): %+v", len(scores), scores)
	}
	for _, s := range scores {
		if s.ThemeID == theme.GenericID {
			t.Fatal("the generic theme competed in the fingerprint fan-out")
		}
	}
	// Best first, ties broken by ID so the order is reproducible.
	want := []theme.Score{
		{ThemeID: "beta", Score: 91},
		{ThemeID: "alpha", Score: 20},
		{ThemeID: "delta", Score: 20},
		{ThemeID: "charlie", Score: 0},
	}
	for i := range want {
		if scores[i] != want[i] {
			t.Fatalf("scores = %+v, want %+v", scores, want)
		}
	}

	// Every theme is asked, so an "unrecognised" verdict can name what it tried.
	m := theme.ScoreMap(scores)
	if len(m) != 4 || m["beta"] != 91 {
		t.Fatalf("ScoreMap = %v", m)
	}
}

func TestFingerprintScoresAreClamped(t *testing.T) {
	reg := theme.NewRegistry()
	for _, f := range []*fake{{id: "over", score: 500}, {id: "under", score: -20}} {
		if err := reg.Register(f); err != nil {
			t.Fatal(err)
		}
	}
	page := probe.NewPage(mustURL(t, "https://example.invalid/"), nil, 200, nil, nil)
	for _, s := range reg.Fingerprint(page) {
		if s.Score < 0 || s.Score > 100 {
			t.Errorf("%s scored %d, outside the 0..100 the interface promises", s.ThemeID, s.Score)
		}
	}
}

func TestValidate(t *testing.T) {
	reg := theme.NewRegistry()
	strict := &validating{
		fake: &fake{id: "strict"},
		spec: theme.OverrideSpec{ThemeID: "strict", Keys: []theme.OverrideDoc{
			{Key: "known", Kind: theme.KindString, Default: "x", Why: "test"},
		}},
	}
	if err := reg.Register(strict); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&fake{id: "plain"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&fake{id: theme.GenericID}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		src     *theme.Source
		wantErr string
	}{
		{
			name: "a well-formed source",
			src:  &theme.Source{ID: "a", Theme: "strict", BaseURL: "https://example.invalid"},
		},
		{
			name: "a known override key",
			src:  &theme.Source{ID: "a", Theme: "strict", BaseURL: "https://example.invalid", Overrides: map[string]any{"known": "y"}},
		},
		{
			name:    "an unknown override key",
			src:     &theme.Source{ID: "a", Theme: "strict", BaseURL: "https://example.invalid", Overrides: map[string]any{"nope": "y"}},
			wantErr: "unknown override key",
		},
		{
			name:    "overrides on a theme that declares none",
			src:     &theme.Source{ID: "a", Theme: "plain", BaseURL: "https://example.invalid", Overrides: map[string]any{"any": 1}},
			wantErr: "accepts no overrides",
		},
		{
			name:    "an unregistered theme",
			src:     &theme.Source{ID: "a", Theme: "nosuch", BaseURL: "https://example.invalid"},
			wantErr: "unknown theme",
		},
		{
			name:    "no id",
			src:     &theme.Source{Theme: "plain", BaseURL: "https://example.invalid"},
			wantErr: "no id",
		},
		{
			name:    "a relative baseUrl",
			src:     &theme.Source{ID: "a", Theme: "plain", BaseURL: "/manga"},
			wantErr: "absolute http(s) URL",
		},
		{
			name:    "a non-http baseUrl",
			src:     &theme.Source{ID: "a", Theme: "plain", BaseURL: "file:///etc/passwd"},
			wantErr: "absolute http(s) URL",
		},
		{
			name:    "selectors outside the generic theme",
			src:     &theme.Source{ID: "a", Theme: "plain", BaseURL: "https://example.invalid", Selectors: map[string]string{"x": "y"}},
			wantErr: "selectors are only valid",
		},
		{
			name:    "a script outside the generic theme",
			src:     &theme.Source{ID: "a", Theme: "plain", BaseURL: "https://example.invalid", Script: "x"},
			wantErr: "script is only valid",
		},
		{
			name: "selectors on the generic theme are fine",
			src:  &theme.Source{ID: "a", Theme: theme.GenericID, BaseURL: "https://example.invalid", Selectors: map[string]string{"x": "y"}},
		},
		{
			name:    "a nil source",
			src:     nil,
			wantErr: "nil source",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.Validate(tc.src)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate = nil, want an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateReportsUnknownThemeAsASentinel(t *testing.T) {
	reg := theme.NewRegistry()
	if err := reg.Register(&fake{id: "plain"}); err != nil {
		t.Fatal(err)
	}
	err := reg.Validate(&theme.Source{ID: "a", Theme: "nosuch", BaseURL: "https://example.invalid"})
	if !errors.Is(err, theme.ErrUnknownTheme) {
		t.Fatalf("err = %v, want it to wrap ErrUnknownTheme", err)
	}
	// The message must name what *is* available, so the user can fix it.
	if !strings.Contains(err.Error(), "plain") {
		t.Errorf("error %q does not name the registered themes", err)
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
