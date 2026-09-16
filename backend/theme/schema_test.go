package theme_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/imageproc"
	"github.com/rickl/quire/backend/theme"
)

// schemaPath is the committed schema. The Go struct and the schema are two
// spellings of the same contract — the device writes one and the UI and any
// exported config read the other — so they have to be checked against each
// other rather than merely kept in step by hand.
const schemaPath = "../../schema/source.schema.json"

func loadSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	abs, err := filepath.Abs(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(abs)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	defer f.Close()

	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("source.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("source.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return s
}

// validate marshals v, re-reads it as generic JSON (which is what the schema
// validator needs) and checks it.
func validate(t *testing.T, s *jsonschema.Schema, v any) error {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return s.Validate(doc)
}

// TestSourceRoundTripsAgainstSchema marshals Go-built Sources and validates
// them against the committed schema. PLAN §7.2 says a configured source is
// "validated against schema/source.schema.json"; this makes the struct's JSON
// tags answerable to that statement.
func TestSourceRoundTripsAgainstSchema(t *testing.T) {
	s := loadSchema(t)
	at := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	enabled := false

	tests := []struct {
		name string
		src  theme.Source
	}{
		{
			name: "the minimum a source can be",
			src: theme.Source{
				ID: "user-added-01", Name: "Example", Lang: "en",
				Theme: "madara", BaseURL: "https://example.invalid", AddedAt: at,
			},
		},
		{
			name: "PLAN §7.2's own example, field for field",
			src: theme.Source{
				ID: "user-added-01", Name: "Example", Lang: "en",
				Theme: "madara", BaseURL: "https://example.invalid",
				Overrides: map[string]any{
					"dateFormat":      "MMMM d, yyyy",
					"mangaSubPath":    "manga",
					"useAjaxChapters": true,
				},
				RateLimit: &fetch.RateLimit{RequestsPerMinute: 30, Concurrency: 2},
				AddedAt:   at,
				LastProbe: &theme.ProbeResult{Verdict: theme.VerdictOK, At: at},
			},
		},
		{
			name: "every field populated",
			src: theme.Source{
				ID: "user-added-02", Name: "Second Example", Lang: "pt-BR",
				Theme: "mangathemesia", BaseURL: "http://example.invalid/reader",
				Overrides:    map[string]any{"seriesSubPath": "series"},
				AllowedHosts: []string{"cdn.example.invalid"},
				RateLimit:    &fetch.RateLimit{RequestsPerMinute: 6, Concurrency: 1},
				SplitStrips:  "never",
				Enabled:      &enabled,
				AddedAt:      at,
				LastProbe: &theme.ProbeResult{
					Verdict:     theme.VerdictPartial,
					At:          at,
					Detail:      "Search works, but the chapter list came back empty.",
					ThemeScores: map[string]int{"madara": 12, "mangathemesia": 88},
				},
			},
		},
		{
			name: "the generic escape hatch carries selectors and a script",
			src: theme.Source{
				ID: "user-added-03", Name: "One-off", Lang: "en",
				Theme: theme.GenericID, BaseURL: "https://example.invalid",
				Selectors: map[string]string{"chapterItem": "#chapters li"},
				Script:    "function pages(doc) { return [] }",
				AddedAt:   at,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validate(t, s, tc.src); err != nil {
				t.Fatalf("Go-marshalled source does not satisfy the schema:\n%v", err)
			}
		})
	}
}

// TestSchemaRejectsWhatGoValidationRejects checks the two documents agree on
// the *negative* cases too. A schema that accepts what Registry.Validate
// refuses would let an imported source look fine and then fail at browse time.
func TestSchemaRejectsWhatGoValidationRejects(t *testing.T) {
	s := loadSchema(t)

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "selectors on a non-generic theme",
			raw: `{"id":"a","name":"A","lang":"en","theme":"madara",
			       "baseUrl":"https://example.invalid","addedAt":"2026-09-15T00:00:00Z",
			       "selectors":{"x":"y"}}`,
		},
		{
			name: "a script on a non-generic theme",
			raw: `{"id":"a","name":"A","lang":"en","theme":"madara",
			       "baseUrl":"https://example.invalid","addedAt":"2026-09-15T00:00:00Z",
			       "script":"1"}`,
		},
		{
			name: "an unknown top-level property",
			raw: `{"id":"a","name":"A","lang":"en","theme":"madara",
			       "baseUrl":"https://example.invalid","addedAt":"2026-09-15T00:00:00Z",
			       "userAgent":"Mozilla/5.0"}`,
		},
		{
			name: "a non-http scheme in baseUrl",
			raw: `{"id":"a","name":"A","lang":"en","theme":"madara",
			       "baseUrl":"file:///etc/passwd","addedAt":"2026-09-15T00:00:00Z"}`,
		},
		{
			name: "a splitStrips value outside the PLAN §12.3 enum",
			raw: `{"id":"a","name":"A","lang":"en","theme":"madara",
			       "baseUrl":"https://example.invalid","addedAt":"2026-09-15T00:00:00Z",
			       "splitStrips":"sometimes"}`,
		},
		{
			name: "a verdict outside the PLAN §7.5 enum",
			raw: `{"id":"a","name":"A","lang":"en","theme":"madara",
			       "baseUrl":"https://example.invalid","addedAt":"2026-09-15T00:00:00Z",
			       "lastProbe":{"verdict":"probably_fine","at":"2026-09-15T00:00:00Z"}}`,
		},
		{
			name: "a missing required field",
			raw:  `{"id":"a","name":"A","lang":"en","theme":"madara"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := jsonschema.UnmarshalJSON(strings.NewReader(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(doc); err == nil {
				t.Fatal("schema accepted a source it should reject")
			}
		})
	}
}

// TestSourceOmitsEmptyFields keeps an exported config readable: a source the
// user never customised should not export a wall of nulls and empty objects.
func TestSourceOmitsEmptyFields(t *testing.T) {
	src := theme.Source{
		ID: "user-added-01", Name: "Example", Lang: "en",
		Theme: "madara", BaseURL: "https://example.invalid",
		AddedAt: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"overrides", "selectors", "script", "allowedHosts", "rateLimit", "splitStrips", "enabled", "lastProbe"} {
		if strings.Contains(string(b), `"`+unwanted+`"`) {
			t.Errorf("a bare source exported %q; it should be omitted:\n%s", unwanted, b)
		}
	}
}

// TestEnabledDefaultsToTrue pins the schema's default. Enabled is a pointer so
// "absent" and "explicitly false" survive an export/import round trip, which
// is only useful if absent still means enabled.
func TestEnabledDefaultsToTrue(t *testing.T) {
	var src theme.Source
	if err := json.Unmarshal([]byte(`{"id":"a"}`), &src); err != nil {
		t.Fatal(err)
	}
	if !src.IsEnabled() {
		t.Error("a source with no enabled field is disabled; the schema default is true")
	}

	if err := json.Unmarshal([]byte(`{"id":"a","enabled":false}`), &src); err != nil {
		t.Fatal(err)
	}
	if src.IsEnabled() {
		t.Error("enabled:false was not honoured")
	}
}

// PLAN §7.2, 2026-09-15: a theme declares the hosts it legitimately needs, and
// stage 6 seeds them onto the source. Whatever a theme declares therefore has
// to be a value the schema accepts — otherwise the probe would happily produce
// a source entry that fails validation on the way to disk.
func TestSchemaAcceptsWhatThemesDeclare(t *testing.T) {
	s := loadSchema(t)

	for _, hosts := range [][]string{
		{"cdn.example.invalid"},
		{"*.mangadex.network"},
		{"example.invalid", "*.cdn-example.invalid"},
	} {
		src := theme.Source{
			ID: "user-added-01", Name: "Example", Lang: "en",
			Theme: "madara", BaseURL: "https://example.invalid",
			AllowedHosts: hosts,
			AddedAt:      time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
		}
		if err := validate(t, s, src); err != nil {
			t.Errorf("allowedHosts %v is rejected by the schema: %v", hosts, err)
		}
	}

	// And the pattern is still a pattern: a wildcard is the one metacharacter
	// allowed, in the one position it means something.
	for _, bad := range []string{"*", "cdn.*.invalid", "*cdn.invalid", "HTTPS://cdn.invalid", "cdn invalid"} {
		src := theme.Source{
			ID: "user-added-01", Name: "Example", Lang: "en",
			Theme: "madara", BaseURL: "https://example.invalid",
			AllowedHosts: []string{bad},
			AddedAt:      time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
		}
		if err := validate(t, s, src); err == nil {
			t.Errorf("allowedHosts entry %q was accepted by the schema", bad)
		}
	}
}

// TestSplitStripsEnumAgreesEverywhere pins the three places the PLAN §12.3
// override is spelled out: the committed schema, Source's own validation, and
// imageproc.ParseSplitMode, which is where the value is finally turned into
// behaviour.
//
// The reason this test exists rather than an import: making backend/theme
// depend on the image pipeline for an enum would be the wrong edge, but the
// alternative — three hand-copied lists — is exactly the drift §9 warns about,
// and a schema field whose value is silently ignored is the specific failure
// worth pinning. So the schema is read as data and both implementations are
// held to whatever it says.
func TestSplitStripsEnumAgreesEverywhere(t *testing.T) {
	values := schemaEnum(t, "splitStrips")
	if len(values) == 0 {
		t.Fatal("the schema declares no splitStrips enum; this test is checking nothing")
	}

	s := loadSchema(t)
	reg := theme.NewRegistry()
	reg.MustRegister(&fake{id: "madara"})

	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			if _, err := imageproc.ParseSplitMode(v); err != nil {
				t.Errorf("the schema offers %q but imageproc cannot parse it: %v", v, err)
			}
			src := theme.Source{
				ID: "a", Name: "A", Lang: "en", Theme: "madara",
				BaseURL: "https://example.invalid", AddedAt: time.Now().UTC(),
				SplitStrips: v,
			}
			if err := validate(t, s, src); err != nil {
				t.Errorf("the schema rejects its own enum value %q: %v", v, err)
			}
			if err := reg.Validate(&src); err != nil {
				t.Errorf("Registry.Validate rejects the schema's enum value %q: %v", v, err)
			}
		})
	}

	// The empty string is the stored form of "unset" and must mean the default
	// everywhere, not "invalid".
	if mode, err := imageproc.ParseSplitMode(""); err != nil || mode != imageproc.SplitAuto {
		t.Errorf(`ParseSplitMode("") = %v, %v; want the default, auto`, mode, err)
	}

	// And a value none of them offers is refused by both, so a hand-edited
	// source cannot smuggle one past the Go side.
	bad := theme.Source{
		ID: "a", Name: "A", Lang: "en", Theme: "madara",
		BaseURL: "https://example.invalid", AddedAt: time.Now().UTC(),
		SplitStrips: "sometimes",
	}
	if err := reg.Validate(&bad); err == nil {
		t.Error("Registry.Validate accepted splitStrips=sometimes")
	}
	if _, err := imageproc.ParseSplitMode("sometimes"); err == nil {
		t.Error("imageproc parsed splitStrips=sometimes")
	}
}

// schemaEnum reads one property's enum straight out of the committed schema, so
// the test is driven by the document rather than by a copy of it.
func schemaEnum(t *testing.T, property string) []string {
	t.Helper()
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Properties[property].Enum
}
