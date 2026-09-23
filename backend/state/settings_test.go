package state_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

// PLAN §7.1 type 75: every screen starts as a grid of covers.
func TestViewDefaultsToGridOnEveryScreen(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	set := s.Settings()
	for _, screen := range state.Screens {
		if got := set.View(screen); got != state.ViewGrid {
			t.Errorf("%s defaults to %q, want %q", screen, got, state.ViewGrid)
		}
	}
	if len(state.Screens) != 3 {
		t.Errorf("Screens has %d entries, want search, downloaded and watching", len(state.Screens))
	}
	if got := set.Views(); len(got) != len(state.Screens) {
		t.Errorf("Views() = %v, want one entry per screen", got)
	}
}

// A view has to survive a reload, and each screen has to be its own setting:
// choosing a list to read search results must not rearrange the library.
func TestViewRoundTripsPerScreen(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetView(state.ScreenSearch, state.ViewList); err != nil {
		t.Fatal(err)
	}
	if got := s.Settings().View(state.ScreenSearch); got != state.ViewList {
		t.Fatalf("search view = %q, want %q", got, state.ViewList)
	}
	if got := s.Settings().View(state.ScreenDownloaded); got != state.ViewGrid {
		t.Errorf("setting search moved downloaded to %q; the setting is per screen", got)
	}

	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Settings().View(state.ScreenSearch); got != state.ViewList {
		t.Errorf("after a reload the search view is %q; it has to be on disk to be a setting at all", got)
	}
	if got := reopened.Settings().View(state.ScreenWatching); got != state.ViewGrid {
		t.Errorf("watching came back as %q, want the default %q", got, state.ViewGrid)
	}

	// Grid is stored explicitly rather than by dropping the key, for the same
	// reason an off robots switch is: "back to grid" and "never chosen" are not
	// the same thing if the default ever changes.
	if err := reopened.SetView(state.ScreenSearch, state.ViewGrid); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"searchView": "grid"`) {
		t.Errorf("stored file does not record the explicit grid:\n%s", b)
	}
}

// A screen or a view nobody defined is an error, never a stored value: it would
// otherwise travel in an exported envelope that no Quire can draw.
func TestSetViewRefusesWhatItCannotDraw(t *testing.T) {
	for _, tc := range []struct {
		name         string
		screen, view string
		want         error
	}{
		{"unknown screen", "settings", state.ViewList, state.ErrUnknownScreen},
		{"unknown view", state.ScreenSearch, "carousel", state.ErrUnknownView},
		{"empty screen", "", state.ViewGrid, state.ErrUnknownScreen},
		{"empty view", state.ScreenSearch, "", state.ErrUnknownView},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := state.Open(dir, registry(t))
			if err != nil {
				t.Fatal(err)
			}
			err = s.SetView(tc.screen, tc.view)
			if !errors.Is(err, tc.want) {
				t.Fatalf("SetView(%q, %q) = %v, want %v", tc.screen, tc.view, err, tc.want)
			}
			// Refused means nothing was written: not the bad pair, and not a
			// file the store did not have before.
			if s.Settings().SearchView != nil {
				t.Error("a refused view was stored anyway")
			}
			if _, err := os.Stat(filepath.Join(dir, state.FileName)); !os.IsNotExist(err) {
				t.Errorf("a refused view saved the store: %v", err)
			}
		})
	}
}

// A file that already holds something this build cannot draw — hand-edited, or
// written by a newer Quire that knows a third layout — must read as the default
// and must not be rewritten on the way through.
func TestUnrecognisedStoredViewReadsAsTheDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, state.FileName)
	stored := `{"version":` + strconv.Itoa(state.CurrentVersion) + `,"sources":[],` +
		`"settings":{"searchView":"carousel","downloadedView":"list"}}`
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("a settings value this build does not know stopped the store from opening: %v", err)
	}
	if got := s.Settings().View(state.ScreenSearch); got != state.ViewGrid {
		t.Errorf("an unreadable stored view read as %q, want the default %q", got, state.ViewGrid)
	}
	// The screens either side of it are untouched by the bad one.
	if got := s.Settings().View(state.ScreenDownloaded); got != state.ViewList {
		t.Errorf("downloaded read as %q, want %q", got, state.ViewList)
	}

	// The value itself is left alone. Rewriting it would mean a round trip
	// through this build silently destroyed what the file meant elsewhere.
	if err := s.SetView(state.ScreenWatching, state.ViewList); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"searchView": "carousel"`) {
		t.Errorf("saving rewrote a value it could not read:\n%s", b)
	}
}

// books-contract.md §B: the reader defaults to font book, size 4, margins
// normal, spacing book, align book on a store nobody has configured.
func TestReaderSettingsDefaultToDefaults(t *testing.T) {
	s, err := state.Open(t.TempDir(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Settings().Reader()
	want := state.DefaultReaderSettings()
	if got != want {
		t.Errorf("Reader() = %+v, want the defaults %+v", got, want)
	}
}

func TestReaderSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	chosen := state.ReaderSettings{
		Font: state.ReaderFontGaramond, Size: 7,
		Margins: state.ReaderMarginsWide, Spacing: state.ReaderSpacingRelaxed,
		Align: state.ReaderAlignLeft,
	}
	if err := s.SetReader(chosen); err != nil {
		t.Fatal(err)
	}
	if got := s.Settings().Reader(); got != chosen {
		t.Fatalf("Reader() = %+v, want %+v", got, chosen)
	}

	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Settings().Reader(); got != chosen {
		t.Errorf("after a reload Reader() = %+v, want %+v", got, chosen)
	}
}

// A reader setting SetReaderSettings would refuse must not be written at
// all — the same rule SetView applies to an unrecognised view.
func TestSetReaderRefusesAnInvalidCombination(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   state.ReaderSettings
	}{
		{"bad font", state.ReaderSettings{Font: "helvetica", Size: 4, Margins: "normal", Spacing: "book", Align: "book"}},
		{"bad margins", state.ReaderSettings{Font: "book", Size: 4, Margins: "huge", Spacing: "book", Align: "book"}},
		{"bad spacing", state.ReaderSettings{Font: "book", Size: 4, Margins: "normal", Spacing: "cramped", Align: "book"}},
		{"bad align", state.ReaderSettings{Font: "book", Size: 4, Margins: "normal", Spacing: "book", Align: "center"}},
		{"size too small", state.ReaderSettings{Font: "book", Size: 0, Margins: "normal", Spacing: "book", Align: "book"}},
		{"size too large", state.ReaderSettings{Font: "book", Size: 10, Margins: "normal", Spacing: "book", Align: "book"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := state.Open(dir, registry(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetReader(tc.in); err == nil {
				t.Fatalf("SetReader(%+v) should have been refused", tc.in)
			}
			if got := s.Settings().Reader(); got != state.DefaultReaderSettings() {
				t.Errorf("a refused reader setting was stored anyway: %+v", got)
			}
			if _, err := os.Stat(filepath.Join(dir, state.FileName)); !os.IsNotExist(err) {
				t.Errorf("a refused reader setting saved the store: %v", err)
			}
		})
	}
}

// A stored reader value this build cannot recognise (a hand-edited file, or
// one written by a newer Quire) must read as the default rather than fail to
// open the store at all.
func TestUnrecognisedStoredReaderReadsAsTheDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, state.FileName)
	stored := `{"version":` + strconv.Itoa(state.CurrentVersion) + `,"sources":[],` +
		`"settings":{"reader":{"font":"comic-sans","size":4,"margins":"normal","spacing":"book","align":"book"}}}`
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("a reader setting this build does not know stopped the store from opening: %v", err)
	}
	if got := s.Settings().Reader(); got != state.DefaultReaderSettings() {
		t.Errorf("an unreadable stored reader setting read as %+v, want the defaults", got)
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
	grid, list := state.ViewGrid, state.ViewList
	for _, tc := range []struct {
		name string
		in   state.Settings
	}{
		{"defaults", state.Settings{}},
		{"robots on", state.Settings{ConsultRobots: &on}},
		{"one screen set", state.Settings{SearchView: &list}},
		{"every screen set", state.Settings{
			SearchView: &list, DownloadedView: &grid, WatchingView: &grid}},
		{"reader settings", state.Settings{StoredReader: &state.ReaderSettings{
			Font: state.ReaderFontNoto, Size: 9, Margins: state.ReaderMarginsNarrow,
			Spacing: state.ReaderSpacingRelaxed, Align: state.ReaderAlignLeft,
		}}},
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

	// The view enum is the half of the contract SetView enforces in Go; if the
	// schema does not also hold it, an imported file could carry anything.
	for _, bad := range []string{
		`{"searchView": "carousel"}`,
		`{"downloadedView": ""}`,
		`{"watchingView": true}`,
		`{"detailView": "grid"}`,
		`{"reader": {"font": "comic-sans", "size": 4, "margins": "normal", "spacing": "book", "align": "book"}}`,
		`{"reader": {"font": "book", "size": 0, "margins": "normal", "spacing": "book", "align": "book"}}`,
		`{"reader": {"font": "book", "size": 4, "margins": "huge", "spacing": "book", "align": "book"}}`,
		`{"reader": {"font": "book", "size": 4, "margins": "normal", "spacing": "book", "align": "book", "extra": true}}`,
	} {
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(bad))
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(v); err == nil {
			t.Errorf("%s validated; the schema does not pin the view values", bad)
		}
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
