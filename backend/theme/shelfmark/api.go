package shelfmark

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The response shapes, as observed against a live instance on 2026-09-20.
//
// Only the fields Quire reads are declared. The instance sends a good deal
// more — `column_config` describes how its own web UI lays the release table
// out, `display_fields` are pre-rendered badges, `extra` is a per-source
// grab-bag — and none of it is Quire's business. Decoding less than is sent is
// also what keeps a new field on the instance from being a parse error here.

// apiError is the field every endpoint can carry, and the reason every
// response goes through errorIn before it is used.
//
// An unknown path on this instance answers `{"error":"Resource not found"}`
// with **HTTP 200** (measured 2026-09-20). The status code therefore says
// nothing about whether the endpoint exists, so the body is the only thing
// that can be believed. A theme that trusted the status here would parse the
// error object as an empty result and report "no books found" for a typo in
// the base URL.
type apiError struct {
	Error string `json:"error"`
}

// book is one book, as returned by /api/metadata/search, /api/metadata/book
// and as the `book` member of /api/releases.
type book struct {
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	Authors     []string `json:"authors"`
	CoverURL    string   `json:"cover_url"`
	Provider    string   `json:"provider"`
	ProviderID  string   `json:"provider_id"`
	PublishYear int      `json:"publish_year"`
	Publisher   string   `json:"publisher"`
	ISBN10      string   `json:"isbn_10"`
	ISBN13      string   `json:"isbn_13"`
	Description string   `json:"description"`
	Genres      []string `json:"genres"`
	SourceURL   string   `json:"source_url"`
	Language    string   `json:"language"`

	// series_name, series_position and series_count exist on the wire and were
	// null on every one of the 40 books in the search capture, so nothing here
	// reads them. They are named in this comment rather than declared as dead
	// fields so the next person does not have to re-run the query to find out.
}

// searchResponse is /api/metadata/search.
type searchResponse struct {
	apiError
	Books   []book `json:"books"`
	HasMore bool   `json:"has_more"`
	Page    int    `json:"page"`

	// TotalFound is sent but was 0 on a page that carried 40 books
	// (2026-09-20), so it is decoded and reported as-is and never used to
	// decide anything — in particular, never as "are there more".  HasMore is
	// what answers that.
	TotalFound int `json:"total_found"`
}

// bookResponse is /api/metadata/book/{provider}/{id}: one book, unwrapped.
type bookResponse struct {
	apiError
	book
}

// release is one downloadable file: a format from a source, for a book.
type release struct {
	Title    string `json:"title"`
	Format   string `json:"format"`
	Size     string `json:"size"`
	Language string `json:"language"`
	Source   string `json:"source"`
	SourceID string `json:"source_id"`

	DownloadURL string `json:"download_url"`
	InfoURL     string `json:"info_url"`
	Indexer     string `json:"indexer"`
	Protocol    string `json:"protocol"`

	// ContentType arrives with an emoji prefix and a non-breaking space
	// ("\U0001F4D5 book (fiction)"). See cleanContentType: the reMarkable
	// renders a glyph it has no font for as a tofu box, so the prefix is
	// stripped rather than passed through.
	ContentType string `json:"content_type"`

	// SizeBytes was null on all 50 releases of the 2026-09-20 capture, so Size
	// — a display string like "0.4MB" — is the only size there is, and
	// sizeInBytes parses it. Declared anyway: if an instance ever fills it in,
	// it is the better number and the code prefers it.
	SizeBytes *int64 `json:"size_bytes"`

	// raw is the object exactly as it arrived. /api/releases/download takes a
	// release *back*, and it takes the whole thing: re-encoding our own struct
	// would silently drop `extra`, which carries the per-source information
	// the instance needs to actually fetch the file. Round-tripping the
	// original bytes is the only way to be sure nothing is lost.
	raw json.RawMessage
}

// releasesResponse is /api/releases.
type releasesResponse struct {
	apiError
	Book            book              `json:"book"`
	Releases        []json.RawMessage `json:"releases"`
	SourcesSearched []string          `json:"sources_searched"`
}

// statusResponse is /api/status. Every bucket is keyed by download id.
type statusResponse struct {
	apiError
	Complete    map[string]statusEntry `json:"complete"`
	Downloading map[string]statusEntry `json:"downloading"`
	Queued      map[string]statusEntry `json:"queued"`
	Locating    map[string]statusEntry `json:"locating"`
	Resolving   map[string]statusEntry `json:"resolving"`
	Error       map[string]statusEntry `json:"error"`
	Cancelled   map[string]statusEntry `json:"cancelled"`
}

// statusEntry is one download's record.
type statusEntry struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Format        string  `json:"format"`
	Progress      float64 `json:"progress"`
	Status        string  `json:"status"`
	StatusMessage string  `json:"status_message"`

	// DownloadPath is where the instance put the file, e.g.
	// "/books/An Example Book - Example Author (1970)_1.epub". Its basename is
	// the only place the instance's own name for the file is visible to a
	// caller that is not reading a Content-Disposition header, and
	// FileTheme.Retrieve has to return that name.
	DownloadPath string `json:"download_path"`
}

// downloadAck is the reply to POST /api/releases/download.
//
// The instance's own record for a download is keyed by the release's
// source_id, and that is what the poll looks for (see Retrieve). This struct
// exists because the acknowledgement was not among the captured fixtures: if
// it turns out to name an id of its own, that id is the authoritative one and
// is preferred; if it is silent, nothing is lost. `id` is the field every
// other record in this API uses for the same thing, so it is the only one
// guessed at, and the fallback is what is actually verified.
type downloadAck struct {
	apiError
	ID string `json:"id"`
}

// decodeRelease reads one release, keeping the original bytes.
func decodeRelease(raw json.RawMessage) (release, error) {
	var r release
	if err := json.Unmarshal(raw, &r); err != nil {
		return release{}, err
	}
	r.raw = raw
	return r, nil
}

// CleanContentType strips the emoji and the non-breaking space the instance
// prefixes every content_type with.
//
// This is not tidiness. Quire's device is a reMarkable: its fonts have no
// emoji coverage, so "\U0001F4D5 book (fiction)" renders as a tofu box
// followed by the words. Dropping every rune outside the Basic Multilingual
// plane's ordinary text and normalising the NBSP leaves "book (fiction)",
// which is the part that carries the meaning.
//
// It is exported because content_type is not the only string the instance
// decorates this way, and because it is the one piece of this theme worth
// testing directly: everything downstream reads only the parenthesised
// qualifier, so a title assertion cannot tell whether the emoji was stripped
// or merely never looked at.
func CleanContentType(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteRune(' ')
		case r >= 0x1f000: // emoji and pictographs
			// dropped
		case r >= 0x2190 && r <= 0x2bff: // arrows, symbols, dingbats
			// dropped
		case r == '️' || r == '︎': // variation selectors
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// sizeInBytes turns a display size ("0.4MB", "1.7 MB", "612KB") into a byte
// count, or -1 when it cannot.
//
// It exists only to order the release list deterministically; nothing is
// displayed from it, and nothing depends on the units being decimal rather
// than binary. An instance that starts sending size_bytes makes it redundant.
func sizeInBytes(s string) int64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
	if s == "" {
		return -1
	}
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == ',' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	num := strings.ReplaceAll(s[:i], ",", ".")
	unit := strings.ToUpper(strings.TrimSpace(s[i:]))
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v < 0 {
		return -1
	}
	mult := map[string]float64{
		"":    1,
		"B":   1,
		"KB":  1 << 10,
		"KIB": 1 << 10,
		"MB":  1 << 20,
		"MIB": 1 << 20,
		"GB":  1 << 30,
		"GIB": 1 << 30,
	}[unit]
	if mult == 0 {
		return -1
	}
	return int64(v * mult)
}
