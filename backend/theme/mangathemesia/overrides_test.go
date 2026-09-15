package mangathemesia_test

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangathemesia"
)

// PLAN §7.2: unknown keys are a validation error, not silently ignored.
func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := mangathemesia.New(nil)

	tests := []struct {
		name      string
		overrides map[string]any
		wantErr   string
	}{
		{name: "no overrides", overrides: nil},
		{name: "seriesSubPath", overrides: map[string]any{"seriesSubPath": "komik"}},
		{name: "dateFormat", overrides: map[string]any{"dateFormat": "d MMMM yyyy"}},
		{name: "pageSource ts_reader", overrides: map[string]any{"pageSource": "ts_reader"}},
		{name: "pageSource dom", overrides: map[string]any{"pageSource": "dom"}},

		{
			// The single most likely mistake: pasting madara's overrides into
			// a mangathemesia source. It must be rejected loudly, because the
			// two keys mean the same thing and silently ignoring it would
			// leave the user with a source that 404s everything.
			name:      "madara's mangaSubPath is rejected here",
			overrides: map[string]any{"mangaSubPath": "manga"},
			wantErr:   `unknown override key "mangaSubPath"`,
		},
		{
			name:      "madara's ajaxStyle is rejected here",
			overrides: map[string]any{"ajaxStyle": "legacy"},
			wantErr:   "unknown override key",
		},
		{
			name:      "the error names the accepted keys",
			overrides: map[string]any{"nope": 1},
			wantErr:   "accepted keys are dateFormat, pageSource, seriesSubPath",
		},
		{
			name:      "a value outside the enum",
			overrides: map[string]any{"pageSource": "json"},
			wantErr:   "want one of ts_reader, dom",
		},
		{
			name:      "a wrong type",
			overrides: map[string]any{"seriesSubPath": true},
			wantErr:   "want a string",
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

func TestEveryOverrideKeyIsDocumented(t *testing.T) {
	for _, doc := range mangathemesia.New(nil).OverrideKeys() {
		if doc.Why == "" {
			t.Errorf("override %q has no explanation", doc.Key)
		}
		if doc.Default == nil {
			t.Errorf("override %q has no default", doc.Key)
		}
	}
}

func TestSelectorsAreRejectedForANonGenericTheme(t *testing.T) {
	reg := theme.NewRegistry()
	if err := reg.Register(mangathemesia.New(nil)); err != nil {
		t.Fatal(err)
	}
	src := &theme.Source{
		ID: "s", Theme: mangathemesia.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{"pageImage": "#readerarea img"},
	}
	if err := reg.Validate(src); err == nil || !strings.Contains(err.Error(), "selectors are only valid") {
		t.Fatalf("Validate = %v, want a rejection of selectors on a themed source", err)
	}
}
