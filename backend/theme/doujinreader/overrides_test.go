package doujinreader_test

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/doujinreader"
)

// PLAN §7.2: unknown keys are a validation error, not silently ignored.
func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := doujinreader.New(nil)

	tests := []struct {
		name      string
		overrides map[string]any
		wantErr   string
	}{
		{name: "no overrides", overrides: nil},
		{name: "readerPath", overrides: map[string]any{"readerPath": "gallery"}},
		{name: "searchPath", overrides: map[string]any{"searchPath": "find"}},
		{
			// The likely mistake is pasting another theme's overrides in.
			name:      "mangakakalot's seriesSubPath is rejected here",
			overrides: map[string]any{"seriesSubPath": "manga"},
			wantErr:   `unknown override key "seriesSubPath"`,
		},
		{
			name:      "the error names the accepted keys",
			overrides: map[string]any{"nope": 1},
			wantErr:   "accepted keys are readerPath, searchPath",
		},
		{
			name:      "a value of the wrong type",
			overrides: map[string]any{"readerPath": 7},
			wantErr:   "readerPath",
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
	for _, doc := range doujinreader.New(nil).OverrideKeys() {
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
	if err := reg.Register(doujinreader.New(nil)); err != nil {
		t.Fatal(err)
	}
	src := &theme.Source{
		ID: "s", Theme: doujinreader.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{"pageImage": "img#fimg"},
	}
	if err := reg.Validate(src); err == nil || !strings.Contains(err.Error(), "selectors are only valid") {
		t.Fatalf("Validate = %v, want a rejection of selectors on a themed source", err)
	}
}
