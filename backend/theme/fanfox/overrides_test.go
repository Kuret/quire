package fanfox_test

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/fanfox"
)

// PLAN §7.2: "unknown keys are a validation error, not silently ignored."
func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := fanfox.New(nil)

	tests := []struct {
		name      string
		overrides map[string]any
		wantErr   string
	}{
		{name: "no overrides", overrides: nil},
		{name: "browseOrder", overrides: map[string]any{"browseOrder": "latest"}},
		{name: "dateFormat", overrides: map[string]any{"dateFormat": "MMMM d, yyyy"}},
		{
			name:      "another theme's key is rejected here",
			overrides: map[string]any{"browseSort": "Popularity"},
			wantErr:   "accepted keys are browseOrder, dateFormat",
		},
		{
			name:      "an order the directory does not offer",
			overrides: map[string]any{"browseOrder": "alphabetical"},
			wantErr:   "want one of",
		},
		{
			name:      "a value of the wrong type",
			overrides: map[string]any{"dateFormat": 7},
			wantErr:   "dateFormat",
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

func TestOverrideKeysAreDocumented(t *testing.T) {
	for _, doc := range fanfox.New(nil).OverrideKeys() {
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
	if err := reg.Register(fanfox.New(nil)); err != nil {
		t.Fatal(err)
	}
	src := &theme.Source{
		ID: "s", Theme: fanfox.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{"pageImage": "#viewer img"},
	}
	if err := reg.Validate(src); err == nil || !strings.Contains(err.Error(), "selectors are only valid") {
		t.Fatalf("Validate = %v, want a rejection of selectors on a themed source", err)
	}
}
