package mangadex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
)

// The response envelopes, transcribed from the documented shapes at
// https://api.mangadex.org/docs/ and confirmed against live responses on
// 2026-09-15. Only the fields Quire uses are declared: an API that adds a
// field should not break a decoder, and a field we do not read is a field we
// cannot be wrong about.

// entity is the shape every object on this API shares: a UUID, a type tag, a
// bag of attributes, and a list of related entities. Relationships carry their
// own attributes only when the request asked for them with includes[].
type entity struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Attributes    json.RawMessage `json:"attributes"`
	Relationships []entity        `json:"relationships"`
}

// collection is the paginated envelope. total is the number of matches, not
// the number returned, which is what makes offset paging possible.
type collection struct {
	Result   string   `json:"result"`
	Response string   `json:"response"`
	Data     []entity `json:"data"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
	Total    int      `json:"total"`
}

// single is the envelope for one object.
type single struct {
	Result   string `json:"result"`
	Response string `json:"response"`
	Data     entity `json:"data"`
}

// mangaAttributes is the series record. Title, altTitles and description are
// all keyed by language, which is the whole reason this theme has a language
// preference order rather than a field read.
type mangaAttributes struct {
	Title                  map[string]string   `json:"title"`
	AltTitles              []map[string]string `json:"altTitles"`
	Description            map[string]string   `json:"description"`
	OriginalLanguage       string              `json:"originalLanguage"`
	Status                 string              `json:"status"`
	ContentRating          string              `json:"contentRating"`
	Year                   int                 `json:"year"`
	AvailableTranslatedLan []string            `json:"availableTranslatedLanguages"`
	Tags                   []entity            `json:"tags"`
}

// tagAttributes is a genre. Tag names are language-keyed too.
type tagAttributes struct {
	Name  map[string]string `json:"name"`
	Group string            `json:"group"`
}

// chapterAttributes is one translated chapter.
//
// Two fields decide whether Quire can actually read it, and both are easy to
// miss until a download fails:
//
//   - ExternalURL, when set, means the chapter is hosted on the publisher's
//     own site. MangaDex lists it but serves no images for it, and Pages()
//     would come back empty.
//   - Pages is the image count. Zero and an ExternalURL travel together.
type chapterAttributes struct {
	Volume             string `json:"volume"`
	Chapter            string `json:"chapter"`
	Title              string `json:"title"`
	TranslatedLanguage string `json:"translatedLanguage"`
	ExternalURL        string `json:"externalUrl"`
	IsUnavailable      bool   `json:"isUnavailable"`
	PublishAt          string `json:"publishAt"`
	Pages              int    `json:"pages"`
}

// namedAttributes covers author, artist and scanlation_group, which all carry
// a plain name.
type namedAttributes struct {
	Name string `json:"name"`
}

// coverAttributes is a cover_art relationship. The filename is half a URL; the
// other half is the uploads host and the manga's own UUID.
type coverAttributes struct {
	FileName string `json:"fileName"`
	Locale   string `json:"locale"`
}

// atHomeResponse is /at-home/server/{id}: a base URL valid for a short while,
// a hash, and the filenames at two qualities.
//
// This is the one endpoint api.mangadex.org disallows in robots.txt, and the
// reason Quire needed the discovery/retrieval distinction at all (PLAN §7.4).
type atHomeResponse struct {
	Result  string `json:"result"`
	BaseURL string `json:"baseUrl"`
	Chapter struct {
		Hash      string   `json:"hash"`
		Data      []string `json:"data"`
		DataSaver []string `json:"dataSaver"`
	} `json:"chapter"`
}

// getJSON performs a discovery GET and decodes the body.
func (t *Theme) getJSON(ctx context.Context, s *theme.Source, u string, out any) error {
	p, err := s.Policy()
	if err != nil {
		return err
	}
	res, err := t.f.Get(ctx, p, u)
	if err != nil {
		return fmt.Errorf("mangadex: %s: %w", redact(u), err)
	}
	return decode(res, u, out)
}

// getJSONRetrieval performs a *retrieval* GET and decodes the body.
//
// It exists for exactly one caller — Pages(), asking for a chapter the user
// opened — and is deliberately not a general-purpose escape hatch. See the
// comment at its call site for why that call is retrieval and a search is not.
func (t *Theme) getJSONRetrieval(ctx context.Context, s *theme.Source, u string, out any) error {
	p, err := s.Policy()
	if err != nil {
		return err
	}
	res, err := t.f.GetRetrieval(ctx, p, u)
	if err != nil {
		return fmt.Errorf("mangadex: %s: %w", redact(u), err)
	}
	return decode(res, u, out)
}

func decode(res *fetch.Response, u string, out any) error {
	if res.StatusCode != http.StatusOK {
		// 429 has already been through the client's Retry-After handling and
		// its backoff; if it reaches here the server is still saying no, and
		// saying so plainly beats a decode error about a JSON error envelope.
		if res.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("mangadex: %s: rate limited by the server after %d attempt(s)", redact(u), res.Attempts)
		}
		return fmt.Errorf("mangadex: %s: HTTP %d", redact(u), res.StatusCode)
	}
	if err := json.Unmarshal(res.Body, out); err != nil {
		return fmt.Errorf("mangadex: %s: decode response: %w", redact(u), err)
	}
	return nil
}

// redact keeps a query string out of an error message. Search terms are the
// user's own words and do not belong in a log line.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}

// find returns the first relationship of the given type.
func (e entity) find(kind string) (entity, bool) {
	for _, r := range e.Relationships {
		if r.Type == kind {
			return r, true
		}
	}
	return entity{}, false
}

// names collects the Name of every relationship of the given type, in the
// order the API listed them and without duplicates.
func (e entity) names(kind string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range e.Relationships {
		if r.Type != kind || len(r.Attributes) == 0 {
			continue
		}
		var a namedAttributes
		if json.Unmarshal(r.Attributes, &a) != nil {
			continue
		}
		n := strings.TrimSpace(a.Name)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// pick chooses a value from a language-keyed map.
//
// MangaDex has no "the" title: a series carries whatever languages someone
// entered, and the key set differs from series to series. The order is the
// source's own language, then English as the lingua franca of the site, then
// a romanised form of the original (`ja-ro`, `ko-ro`, `zh-ro`), then the
// original itself, and only then whatever is left.
func pick(m map[string]string, prefer ...string) string {
	if len(m) == 0 {
		return ""
	}
	for _, lang := range prefer {
		if v := pickLang(m, lang); v != "" {
			return v
		}
	}
	return pickAny(m)
}

// pickLang is pick without the fallback: it answers "" when the map has
// nothing in this language. bestTitle needs that distinction, because "the
// English title is missing" and "here is the Japanese one instead" are
// different answers and only the first should move on to the altTitles.
func pickLang(m map[string]string, lang string) string {
	if lang == "" {
		return ""
	}
	if v := strings.TrimSpace(m[lang]); v != "" {
		return v
	}
	// "pt-br" satisfies a preference for "pt", and vice versa. Matched on the
	// base tag in sorted key order, so a map with both "pt" and "pt-br" always
	// resolves the same way.
	keys := sortedKeys(m)
	for _, k := range keys {
		if baseLang(k) == baseLang(lang) {
			if v := strings.TrimSpace(m[k]); v != "" {
				return v
			}
		}
	}
	return ""
}

// pickAny is the last resort. It sorts rather than ranging over the map: Go
// randomises map iteration, and a title that changes between runs is a bug
// that surfaces as a flaky test months later.
func pickAny(m map[string]string) string {
	for _, k := range sortedKeys(m) {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// baseLang narrows "pt-br" to "pt". MangaDex's codes are BCP-47-shaped but
// include forms like "ja-ro" (romanised Japanese) that are not translations
// into another language, which is why this is only ever used for *matching*
// and never to rewrite a code we send to the API.
func baseLang(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "-_"); i > 0 {
		return code[:i]
	}
	return code
}

// romanised is the preference slot between "English" and "the original
// script": MangaDex spells these as the original language plus "-ro".
func romanised(original string) string {
	if original == "" {
		return ""
	}
	return original + "-ro"
}
