package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/themetest"
)

// migrationRegistry has the themes a stored file might name, including one with
// a real AllowedHosts declaration (mangadex serves page images from
// *.mangadex.network) and one with none.
func migrationRegistry(t *testing.T) *theme.Registry {
	t.Helper()
	f := themetest.New(t, nil)
	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(f))
	reg.MustRegister(mangadex.New(f))
	return reg
}

// writeStore puts a raw file on disk. Tests here work at the JSON level on
// purpose: the whole point of a migration is what happens to a file this
// build did not write.
func writeStore(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, state.FileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readStore(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Migration #1, on the case that motivated it: a MangaDex source stored before
// themes could declare AllowedHosts passes search, series and chapters and then
// fails at download with an SSRF refusal, because nothing says the page-image
// CDN is allowed.
func TestMigrationSeedsAllowedHostsOnce(t *testing.T) {
	dir := t.TempDir()
	path := writeStore(t, dir, `{
	  "version": 1,
	  "sources": [
	    {
	      "id": "mangadex",
	      "name": "MangaDex",
	      "theme": "mangadex",
	      "baseUrl": "https://api.mangadex.org",
	      "lang": "en",
	      "addedAt": "2026-03-04T12:00:00Z"
	    }
	  ]
	}`)

	store, err := state.Open(dir, migrationRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	src, ok := store.Get("mangadex")
	if !ok {
		t.Fatal("the source did not survive the migration")
	}
	if len(src.AllowedHosts) == 0 {
		t.Fatal("allowedHosts is still empty; the download would still be refused")
	}

	// And it was written back, at the new version, so it runs once.
	on := readStore(t, path)
	if on["version"] != float64(state.CurrentVersion) {
		t.Errorf("file version %v, want %d", on["version"], state.CurrentVersion)
	}
}

// The distinction the whole mechanism exists for: an empty list is a legitimate
// state once the migration has run, and must not be re-seeded. A user who
// cleared it meant it.
func TestAnEmptyListIsNotReseededAfterTheMigration(t *testing.T) {
	dir := t.TempDir()
	path := writeStore(t, dir, `{
	  "version": 1,
	  "sources": [
	    {"id":"mangadex","name":"MangaDex","theme":"mangadex",
	     "baseUrl":"https://api.mangadex.org","lang":"en",
	     "addedAt":"2026-03-04T12:00:00Z"}
	  ]
	}`)

	// First load migrates and seeds.
	if _, err := state.Open(dir, migrationRegistry(t)); err != nil {
		t.Fatal(err)
	}

	// Now the user deliberately clears the list.
	on := readStore(t, path)
	sources := on["sources"].([]any)
	sources[0].(map[string]any)["allowedHosts"] = []any{}
	cleared, err := json.Marshal(on)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, cleared, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := state.Open(dir, migrationRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	src, _ := store.Get("mangadex")
	if len(src.AllowedHosts) != 0 {
		t.Errorf("allowedHosts was re-seeded to %v, silently undoing the user", src.AllowedHosts)
	}
}

// A file already at the current version must not be rewritten. A needless write
// on every launch is a needless chance to lose the file to a power cut.
func TestACurrentFileIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	path := writeStore(t, dir, `{"version":`+itoa(state.CurrentVersion)+`,"sources":[]}`)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Open(dir, migrationRegistry(t)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Error("an up-to-date store was rewritten on load")
	}
}

// A file from the future is refused rather than guessed at: a downgrade that
// silently drops fields it does not understand would mangle the user's
// configuration and there is no cloud copy to restore from.
func TestAFutureVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeStore(t, dir, `{"version":999,"sources":[]}`)

	_, err := state.Open(dir, migrationRegistry(t))
	if err == nil {
		t.Fatal("a newer format was accepted")
	}
	for _, want := range []string{"newer version", "999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A theme that declares no hosts, and a theme Quire no longer ships, must both
// pass through the migration without inventing anything or failing it.
func TestMigrationToleratesThemesWithNothingToSeed(t *testing.T) {
	dir := t.TempDir()
	writeStore(t, dir, `{
	  "version": 1,
	  "sources": [
	    {"id":"plain","name":"Plain","theme":"madara",
	     "baseUrl":"https://example.invalid","lang":"en",
	     "addedAt":"2026-03-04T12:00:00Z"}
	  ]
	}`)

	store, err := state.Open(dir, migrationRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	src, ok := store.Get("plain")
	if !ok {
		t.Fatal("the source did not survive")
	}
	if len(src.AllowedHosts) != 0 {
		t.Errorf("hosts %v were invented for a theme that declares none", src.AllowedHosts)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
