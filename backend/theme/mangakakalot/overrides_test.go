package mangakakalot_test

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangakakalot"
)

// PLAN §7.2: unknown keys are a validation error, not silently ignored.
func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := mangakakalot.New(nil)

	tests := []struct {
		name      string
		overrides map[string]any
		wantErr   string
	}{
		{name: "no overrides", overrides: nil},
		{name: "seriesSubPath", overrides: map[string]any{"seriesSubPath": "comic"}},
		{name: "searchPath", overrides: map[string]any{"searchPath": "search/story"}},
		{name: "browsePath", overrides: map[string]any{"browsePath": "manga-list/hot-manga"}},
		{name: "pageSource dom", overrides: map[string]any{"pageSource": "dom"}},
		{name: "pageSource script", overrides: map[string]any{"pageSource": "script"}},

		{
			// The likely mistake is pasting another theme's overrides in. The
			// keys mean roughly the same thing, which is exactly why silently
			// ignoring them would leave a source that 404s everything.
			name:      "madara's mangaSubPath is rejected here",
			overrides: map[string]any{"mangaSubPath": "manga"},
			wantErr:   `unknown override key "mangaSubPath"`,
		},
		{
			name:      "mangathemesia's pageSource value is rejected here",
			overrides: map[string]any{"pageSource": "ts_reader"},
			wantErr:   "want one of dom, script",
		},
		{
			name:      "the error names the accepted keys",
			overrides: map[string]any{"nope": 1},
			wantErr:   "accepted keys are browsePath, pageSource, searchPath, seriesSubPath",
		},
		{
			name:      "a value of the wrong type",
			overrides: map[string]any{"seriesSubPath": 7},
			wantErr:   "seriesSubPath",
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
	for _, doc := range mangakakalot.New(nil).OverrideKeys() {
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
	if err := reg.Register(mangakakalot.New(nil)); err != nil {
		t.Fatal(err)
	}
	src := &theme.Source{
		ID: "s", Theme: mangakakalot.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{"pageImage": ".container-chapter-reader img"},
	}
	if err := reg.Validate(src); err == nil || !strings.Contains(err.Error(), "selectors are only valid") {
		t.Fatalf("Validate = %v, want a rejection of selectors on a themed source", err)
	}
}
