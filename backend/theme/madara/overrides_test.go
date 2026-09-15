package madara_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

func decodeSource(s string) (*theme.Source, error) {
	var src theme.Source
	if err := json.Unmarshal([]byte(s), &src); err != nil {
		return nil, err
	}
	return &src, nil
}

// TestOverridesRejectUnknownKeys covers PLAN §7.2: "unknown keys are a
// validation error, not silently ignored". A typo'd key that does nothing and
// is never reported is the single most confusing failure mode a config-driven
// theme can have.
func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := madara.New(nil)

	tests := []struct {
		name      string
		overrides map[string]any
		wantErr   string // substring, "" means accept
	}{
		{name: "no overrides", overrides: nil},
		{name: "empty overrides", overrides: map[string]any{}},
		{
			name:      "the keys PLAN §7.2's example uses",
			overrides: map[string]any{"dateFormat": "MMMM d, yyyy", "mangaSubPath": "manga", "useAjaxChapters": true},
		},
		{name: "ajaxStyle current", overrides: map[string]any{"ajaxStyle": "current"}},
		{name: "ajaxStyle legacy", overrides: map[string]any{"ajaxStyle": "legacy"}},
		{name: "searchPostType", overrides: map[string]any{"searchPostType": "manga"}},

		{
			name:      "a typo is rejected, not ignored",
			overrides: map[string]any{"mangaSubpath": "series"}, // lowercase p
			wantErr:   `unknown override key "mangaSubpath"`,
		},
		{
			name:      "a key from another theme is rejected",
			overrides: map[string]any{"seriesSubPath": "manga"},
			wantErr:   "unknown override key",
		},
		{
			name:      "a plausible but undeclared key is rejected",
			overrides: map[string]any{"userAgent": "something else"},
			wantErr:   "unknown override key",
		},
		{
			name:      "the error names the accepted keys",
			overrides: map[string]any{"nope": 1},
			wantErr:   "accepted keys are ajaxStyle, dateFormat, mangaSubPath, searchPostType, useAjaxChapters",
		},
		{
			name:      "a wrong type is rejected",
			overrides: map[string]any{"useAjaxChapters": "yes"},
			wantErr:   "want a boolean",
		},
		{
			name:      "a wrong type on a string key is rejected",
			overrides: map[string]any{"mangaSubPath": 3},
			wantErr:   "want a string",
		},
		{
			name:      "a value outside an enum is rejected",
			overrides: map[string]any{"ajaxStyle": "modern"},
			wantErr:   "want one of current, legacy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := th.ValidateOverrides(tc.overrides)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateOverrides(%v) = %v, want nil", tc.overrides, err)
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

// TestEveryOverrideKeyIsDocumented keeps docs/THEME-NOTES.md honest: a key
// without a Why is a key whose reason for existing has already been forgotten.
func TestEveryOverrideKeyIsDocumented(t *testing.T) {
	for _, doc := range madara.New(nil).OverrideKeys() {
		if doc.Why == "" {
			t.Errorf("override %q has no explanation", doc.Key)
		}
		if doc.Default == nil {
			t.Errorf("override %q has no default", doc.Key)
		}
	}
}

// TestSelectorsAreRejectedForANonGenericTheme covers the schema's allOf clause
// restated in Go, because an imported source never met the schema.
func TestSelectorsAreRejectedForANonGenericTheme(t *testing.T) {
	reg := theme.NewRegistry()
	if err := reg.Register(madara.New(nil)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		src     *theme.Source
		wantErr string
	}{
		{
			name: "selectors on a madara source",
			src: &theme.Source{
				ID: "s", Theme: madara.ID, BaseURL: "https://example.invalid",
				Selectors: map[string]string{"chapterItem": ".x"},
			},
			wantErr: "selectors are only valid when theme is \"generic\"",
		},
		{
			name: "script on a madara source",
			src: &theme.Source{
				ID: "s", Theme: madara.ID, BaseURL: "https://example.invalid",
				Script: "function pages() { return [] }",
			},
			wantErr: "script is only valid when theme is \"generic\"",
		},
		{
			name: "a clean madara source is accepted",
			src: &theme.Source{
				ID: "s", Theme: madara.ID, BaseURL: "https://example.invalid",
			},
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
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}
