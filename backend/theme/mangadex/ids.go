package mangadex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rickl/quire/backend/theme"
)

// MangaDex identifies everything by UUID, where the other themes use a path.
// theme.SeriesStub.ID and theme.Chapter.ID are documented as site-relative so
// that a source can be re-pointed at a mirror without orphaning what is
// already downloaded, so the UUIDs are stored in that shape: "/manga/<uuid>"
// and "/chapter/<uuid>".
//
// The prefix earns its keep twice over. It keeps the IDs self-describing in a
// state file, and it means a chapter UUID handed to Series(), or a series UUID
// handed to Pages(), is a clear error rather than a 404 from the API.
const (
	seriesPrefix  = "/manga/"
	chapterPrefix = "/chapter/"
)

func seriesID(uuid string) string  { return seriesPrefix + uuid }
func chapterID(uuid string) string { return chapterPrefix + uuid }

// parseID accepts the stored form, a bare UUID, or a full API URL — the last
// because the probe's stage 5 and the browse UI both have a habit of passing
// around whatever they last saw.
func parseID(id, prefix string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("mangadex: empty id")
	}
	if i := strings.Index(id, prefix); i >= 0 {
		id = id[i+len(prefix):]
	}
	id = strings.Trim(id, "/")
	if i := strings.IndexAny(id, "/?#"); i >= 0 {
		id = id[:i]
	}
	if !looksLikeUUID(id) {
		return "", fmt.Errorf("mangadex: %q is not a %s id", id, strings.Trim(prefix, "/"))
	}
	return strings.ToLower(id), nil
}

// looksLikeUUID checks the shape only. It is a cheap guard against a path
// segment arriving where an id belongs, not a validity check — the API is
// entitled to be the judge of whether a well-formed UUID exists.
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// unmarshal is json.Unmarshal that tolerates an absent attributes block, which
// is what a relationship carries when the request did not ask to include it.
func unmarshal(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("no attributes")
	}
	return json.Unmarshal(raw, out)
}

// chapterTitle composes the display title.
//
// theme.Chapter has no volume field, and MangaDex is the only theme so far
// that reliably knows the volume, so it goes in the title where the user can
// see it. The forms, in the order they are tried:
//
//	Vol. 3 Chapter 12: The Long Walk
//	Chapter 12
//	Oneshot                     (no chapter number at all)
//	Oneshot: The Long Walk
func chapterTitle(a chapterAttributes) string {
	var b strings.Builder
	if v := strings.TrimSpace(a.Volume); v != "" {
		fmt.Fprintf(&b, "Vol. %s ", v)
	}
	if c := strings.TrimSpace(a.Chapter); c != "" {
		fmt.Fprintf(&b, "Chapter %s", c)
	} else {
		b.WriteString("Oneshot")
	}
	if t := strings.TrimSpace(a.Title); t != "" {
		b.WriteString(": ")
		b.WriteString(t)
	}
	return b.String()
}

// parseTime reads the API's timestamps. MangaDex sends RFC 3339 with a numeric
// offset; a value we cannot parse becomes the zero time, which theme.Chapter
// already documents as "the site gave none".
func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// firstName is the Name of the first relationship of a type, or "".
func firstName(e entity, kind string) string {
	if names := e.names(kind); len(names) > 0 {
		return names[0]
	}
	return ""
}

// mergeAlts flattens the altTitles list — a list of single-entry maps, not one
// map — into a language-keyed map, keeping the first entry for each language.
func mergeAlts(alts []map[string]string) map[string]string {
	merged := map[string]string{}
	for _, m := range alts {
		for k, v := range m {
			if _, seen := merged[k]; !seen {
				merged[k] = v
			}
		}
	}
	return merged
}

// bestTitle chooses what to show a user whose source is configured for lang.
//
// The order matters more than it looks. MangaDex has no canonical title: a
// series carries whatever titles people entered, the main `title` map often
// holds only a romanisation of the original, and the English title lives in
// `altTitles`. Preferring the *language* over the *field* is what makes a
// series called "Tooi Tou no Kiroku" show up as "Record of the Distant Tower"
// for an English source — and, for a Portuguese one, as neither.
//
// Within a language the main title still wins over an alternative; only when
// the language is absent from both does it fall through to the next one.
func bestTitle(a mangaAttributes, lang string) string {
	alts := mergeAlts(a.AltTitles)
	for _, l := range []string{lang, "en", romanised(a.OriginalLanguage), a.OriginalLanguage} {
		if l == "" {
			continue
		}
		if v := pickLang(a.Title, l); v != "" {
			return v
		}
		if v := pickLang(alts, l); v != "" {
			return v
		}
	}
	if v := pickAny(a.Title); v != "" {
		return v
	}
	return pickAny(alts)
}

// altTitles flattens altTitles into a deduplicated list, dropping whichever
// one was promoted to the main title so it is not shown twice.
func altTitles(alts []map[string]string, main string) []string {
	var out []string
	seen := map[string]bool{strings.ToLower(main): true}
	for _, m := range alts {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := strings.TrimSpace(m[k])
			if v == "" || seen[strings.ToLower(v)] {
				continue
			}
			seen[strings.ToLower(v)] = true
			out = append(out, v)
		}
	}
	return out
}

// genres reads tag names in the preferred language. MangaDex groups its tags
// (genre, theme, format, content); all four are useful as browse facets and
// are kept, in the order the API listed them.
func genres(tags []entity, prefer []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tag := range tags {
		var a tagAttributes
		if unmarshal(tag.Attributes, &a) != nil {
			continue
		}
		n := pick(a.Name, append(prefer, "en")...)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// status maps MangaDex's vocabulary onto theme's. The two happen to agree
// word for word, which is worth mapping explicitly rather than passing through
// — theme.Series documents Status as normalised, and an unrecognised value
// must become StatusUnknown rather than leak the site's own wording into the
// UI.
func status(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ongoing":
		return theme.StatusOngoing
	case "completed":
		return theme.StatusCompleted
	case "hiatus":
		return theme.StatusHiatus
	case "cancelled", "canceled":
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}
