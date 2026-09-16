package weebcentral_test

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/weebcentral"
)

// PLAN §7.2: "unknown keys are a validation error, not silently ignored." The
// failure this guards against is a typo'd key that leaves the theme running on
// its defaults while the user believes they configured something.
func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := weebcentral.New(nil)

	tests := []struct {
		name      string
		overrides map[string]any
		wantErr   string
	}{
		{name: "no overrides", overrides: nil},
		{name: "browseSort", overrides: map[string]any{"browseSort": "Recently Added"}},
		{name: "includeAdultContent", overrides: map[string]any{"includeAdultContent": true}},
		{
			// Every other theme spells its knobs differently on purpose; a key
			// borrowed from one of them is a mistake, not a synonym.
			name:      "madara's mangaSubPath is rejected here",
			overrides: map[string]any{"mangaSubPath": "manga"},
			wantErr:   "accepted keys are browseSort, includeAdultContent",
		},
		{
			name:      "a sort the site does not offer",
			overrides: map[string]any{"browseSort": "Chronological"},
			wantErr:   "want one of",
		},
		{
			name:      "a value of the wrong type",
			overrides: map[string]any{"includeAdultContent": "yes"},
			wantErr:   "includeAdultContent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := th.ValidateOverrides(tc.overrides)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateOverrides(%v) = %v, want no error", tc.overrides, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateOverrides(%v) = nil, want an error mentioning %q", tc.overrides, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// Every key the theme accepts says what it is for, so the "add a source" UI
// can show the user something better than a name.
func TestOverrideKeysAreDocumented(t *testing.T) {
	for _, doc := range weebcentral.New(nil).OverrideKeys() {
		if strings.TrimSpace(doc.Why) == "" {
			t.Errorf("override %q has no explanation", doc.Key)
		}
		if doc.Default == nil {
			t.Errorf("override %q has no default", doc.Key)
		}
	}
}

// selectors belong to the generic escape hatch alone (PLAN §6 M2).
func TestSelectorsAreRejectedOnAThemedSource(t *testing.T) {
	reg := theme.NewRegistry()
	if err := reg.Register(weebcentral.New(nil)); err != nil {
		t.Fatal(err)
	}
	src := &theme.Source{
		ID: "s", Theme: weebcentral.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{"pageImage": "#chapter-images img"},
	}
	if err := reg.Validate(src); err == nil || !strings.Contains(err.Error(), "selectors are only valid") {
		t.Fatalf("Validate = %v, want a rejection of selectors on a themed source", err)
	}
}
