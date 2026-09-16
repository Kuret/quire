package comick_test

// The shapes this backend varies.
//
// On 2026-09-16 `md_titles` came back as an object where the fixture had an
// array and `Series()` failed outright. Because the series screen errors as a
// unit the user saw **no chapters at all** — `Chapters()` was fine throughout.
//
// PLAN §9 is why the fixtures come in pairs rather than being edited in place:
// a test that only ever sees the array proves nothing about the object, and
// the array shape is still the common one and still has to work.

import (
	"context"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/comick"
	"github.com/rickl/quire/backend/theme/themetest"
)

// TestSeriesParsesEitherCardinality is the regression test for the reported
// bug. series-single.html sends the relations as objects rather than arrays,
// in both the forms seen: keyed by position, which is what the live API sends,
// and bare.
func TestSeriesParsesEitherCardinality(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series-single.html"},
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatalf("a payload whose relations are objects must parse: %v", err)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title = %q", got.Title)
	}
	// The object form is a map keyed by position, so both titles must come
	// out, in the order the site wrote them. Reading the object as one title
	// instead would yield one empty title — a quiet wrong answer where the bug
	// at least had the decency to fail loudly.
	if len(got.AltTitles) != 2 {
		t.Fatalf("altTitles = %v, want both entries of the keyed object", got.AltTitles)
	}
	if got.AltTitles[0] != "Kanteru no Banjin" || got.AltTitles[1] != "The Keeper of Lanterns" {
		t.Errorf("altTitles = %v, want the site's own order", got.AltTitles)
	}
	// authors is a bare object rather than a keyed one: the other object form,
	// and one author is the right reading of it.
	if len(got.Authors) != 1 || got.Authors[0] != "IWANO Miyuki" {
		t.Errorf("authors = %v, want the bare object read as one author", got.Authors)
	}
	// md_comic_md_genres is keyed the same way, and the md_genres inside the
	// join row is an array — the to-one relation varying the other way.
	if len(got.Genres) != 1 || got.Genres[0] != "Drama" {
		t.Errorf("genres = %v, want the join object and its array-wrapped genre", got.Genres)
	}
	// artists is null rather than an empty array.
	if len(got.Artists) != 0 {
		t.Errorf("artists = %v, want none: the payload's field is null", got.Artists)
	}
	if got.Status != theme.StatusCompleted {
		t.Errorf("status = %q", got.Status)
	}
}

// TestSearchParsesEitherCardinality covers the envelope rather than a relation:
// a search precise enough to match one series is ordinary, and is where the
// missing array wrapper would first show.
func TestSearchParsesEitherCardinality(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search-single.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "lantern keeper", 1)
	if err != nil {
		t.Fatalf("an envelope whose data is a keyed object must parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want the keyed object read as one: %+v", len(got), got)
	}
	if got[0].ID != seriesID {
		t.Errorf("id = %q, want %q", got[0].ID, seriesID)
	}
}

// TestChaptersParseEitherCardinality covers the chapter list's envelope, its
// one to-many relation, and the two fields most likely to arrive unquoted.
func TestChaptersParseEitherCardinality(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/comics/the-lantern-keeper/chapter-list": jsonRoute("chapter-list-single.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatalf("a single-chapter list must parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d chapters, want 1: %+v", len(got), got)
	}
	c := got[0]
	// chap and vol arrive as numbers and must come out as the text the rest of
	// the theme reads, unchanged.
	if c.Number != 1 {
		t.Errorf("number = %v, want 1 from the unquoted chap", c.Number)
	}
	if c.Volume != "1" {
		t.Errorf("volume = %q, want \"1\" from the unquoted vol", c.Volume)
	}
	// group_name is a bare string rather than an array of one.
	if c.Scanlator != "Lamplight" {
		t.Errorf("scanlator = %q, want the bare string read as one group", c.Scanlator)
	}
	// The ID is built from the chapter number, so an unquoted one must not
	// change the handle a stored library entry was written with.
	if !strings.HasSuffix(c.ID, "SI7oIMm-chapter-1-en") {
		t.Errorf("id = %q, want the number rendered as it arrived", c.ID)
	}
}

// A field that cannot reasonably be two shapes keeps its plain type, so the
// failure names it. That was the one good thing about the original bug: the log
// line said which field and which shape, and this pins that it still does.
func TestAnImpossibleShapeFailsNamingTheField(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {Body: seriesPageWith(`"title": {"en": "The Lantern Keeper"}`)},
	})
	th := comick.NewWithClock(f, clock)

	_, err := th.Series(context.Background(), site(), seriesID)
	if err == nil {
		t.Fatal("an object where the title belongs must fail rather than be guessed at")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("the error must name the field it could not read, got: %v", err)
	}
}

// seriesPageWith builds a minimal series page whose payload carries one
// replaced field. Synthetic, on example.invalid, like every fixture here.
func seriesPageWith(field string) string {
	return `<!DOCTYPE html><html><body>` +
		`<script type="application/json" id="comic-data">{` + field + `,"slug":"the-lantern-keeper"}</script>` +
		`</body></html>`
}
