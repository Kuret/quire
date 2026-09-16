package state_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/rickl/quire/backend/state"
)

// PLAN §7.4: the robots.txt consultation is off unless the user turns it on.
// A store nobody has configured must not claim otherwise.
func TestRobotsSettingDefaultsToOff(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Settings().RobotsConsulted() {
		t.Fatal("a fresh store consults robots.txt; PLAN §7.4 says the setting is off by default")
	}
}

func TestRobotsSettingRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConsultRobots(true); err != nil {
		t.Fatal(err)
	}
	if !s.Settings().RobotsConsulted() {
		t.Fatal("the setting did not take")
	}

	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Settings().RobotsConsulted() {
		t.Fatal("the setting did not survive a reload; it has to be on disk to be a setting at all")
	}

	// Off is stored as an explicit false rather than by dropping the key, so
	// "the user turned this off" and "the user never touched it" stay
	// distinguishable if the default ever changes.
	if err := reopened.SetConsultRobots(false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"consultRobots": false`) {
		t.Errorf("stored file does not record the explicit off:\n%s", b)
	}
}

// The Go struct and schema/settings.schema.json are two spellings of one
// contract; neither is allowed to drift from the other.
func TestSettingsMatchTheSchema(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(mustOpen(t, "../../schema/settings.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("settings.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("settings.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	on := true
	for _, tc := range []struct {
		name string
		in   state.Settings
	}{
		{"defaults", state.Settings{}},
		{"robots on", state.Settings{ConsultRobots: &on}},
	} {
		b, err := json.Marshal(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(v); err != nil {
			t.Errorf("%s: %s does not validate: %v", tc.name, b, err)
		}
	}

	// additionalProperties: false is the point of the schema — a key nobody
	// declared is a mistake, not a setting.
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"ignoreRobots": true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(v); err == nil {
		t.Error("an undeclared settings key validated; the schema accepts anything")
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
