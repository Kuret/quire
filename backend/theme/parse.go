package theme

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Parsing helpers shared by the themes. They live here rather than being
// copied into each theme because every one of them encodes a fact about how
// *WordPress-generated markup in general* behaves, not about any one family:
// lazy-loaded images, chapter numbers embedded in free text, and dates written
// in whatever format the site's locale settings produce.

// imageAttrs are the attributes that may carry an image URL, in priority
// order. Lazy-loading plugins move the real URL out of src and leave a
// placeholder behind, so reading src first would yield a page of grey squares.
var imageAttrs = []string{
	"data-src",
	"data-lazy-src",
	"data-cfsrc",
	"data-original",
	"data-srcset",
	"srcset",
	"src",
}

// ImageURL extracts the best image URL from a node, handling the lazy-load
// attributes and picking the first candidate from a srcset.
func ImageURL(sel *goquery.Selection) string {
	for _, attr := range imageAttrs {
		v, ok := sel.Attr(attr)
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" || strings.HasPrefix(v, "data:") {
			continue // a base64 placeholder is not the image
		}
		if attr == "srcset" || attr == "data-srcset" {
			v = firstFromSrcset(v)
		}
		if v != "" {
			return v
		}
	}
	return ""
}

// firstFromSrcset takes the first candidate of a srcset list, dropping its
// descriptor. Sites list their smallest rendition first often enough that
// "first" is a reasonable default, and M4 resizes everything anyway.
func firstFromSrcset(v string) string {
	first, _, _ := strings.Cut(v, ",")
	fields := strings.Fields(strings.TrimSpace(first))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// chapterNumberRE finds the first decimal number in a chapter title. Anchoring
// on "chapter"/"ch." first and falling back to any number is what keeps
// "Volume 3 Chapter 12.5" from being read as chapter 3.
var (
	chapterLabelRE  = regexp.MustCompile(`(?i)\b(?:chapter|chap|ch|episode|ep)\.?\s*#?\s*(\d+(?:[.,]\d+)?)`)
	anyNumberRE     = regexp.MustCompile(`(\d+(?:[.,]\d+)?)`)
	whitespaceRunRE = regexp.MustCompile(`\s+`)
)

// ChapterNumber parses the chapter number out of a title, returning -1 when
// there is none. -1 rather than 0 because chapter 0 is a real thing (prologues
// are routinely numbered 0) and the caller must be able to tell them apart.
func ChapterNumber(title string) float64 {
	title = strings.TrimSpace(title)
	if m := chapterLabelRE.FindStringSubmatch(title); m != nil {
		return parseDecimal(m[1])
	}
	if m := anyNumberRE.FindStringSubmatch(title); m != nil {
		return parseDecimal(m[1])
	}
	return -1
}

func parseDecimal(s string) float64 {
	f, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil {
		return -1
	}
	return f
}

// Collapse trims a string and squeezes internal whitespace runs to one space.
// HTML text nodes are full of newlines and indentation that would otherwise
// end up in a title.
func Collapse(s string) string {
	return strings.TrimSpace(whitespaceRunRE.ReplaceAllString(strings.TrimSpace(s), " "))
}

// Text is Collapse applied to a selection's text.
func Text(sel *goquery.Selection) string { return Collapse(sel.Text()) }

// patternToLayout converts the CLDR-ish date pattern used in the `dateFormat`
// override (PLAN §7.2's example uses "MMMM d, yyyy") into a Go layout.
//
// A CLDR-ish pattern is what the override carries because that is what the
// sites themselves are configured with — a WordPress date format setting is
// closer to this than to Go's reference-time layout, so a user copying their
// site's setting across gets something that works. Longest tokens first, so
// "MMMM" is not consumed as two "MM"s.
var patternTokens = []struct{ pattern, layout string }{
	{"yyyy", "2006"},
	{"yy", "06"},
	{"MMMM", "January"},
	{"MMM", "Jan"},
	{"MM", "01"},
	{"M", "1"},
	{"dd", "02"},
	{"d", "2"},
	{"HH", "15"},
	{"hh", "03"},
	{"h", "3"},
	{"mm", "04"},
	{"ss", "05"},
	{"a", "PM"},
}

// DateLayout converts a dateFormat override into a Go time layout.
func DateLayout(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); {
		matched := false
		for _, tok := range patternTokens {
			if strings.HasPrefix(pattern[i:], tok.pattern) {
				b.WriteString(tok.layout)
				i += len(tok.pattern)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(pattern[i])
			i++
		}
	}
	return b.String()
}

// relativeDateRE matches the "3 days ago" style both families fall back to for
// recent chapters, which no date layout can parse.
var relativeDateRE = regexp.MustCompile(`(?i)^(\d+)\s+(second|minute|hour|day|week|month|year)s?\s+ago$`)

// ParseDate parses a chapter date using the source's dateFormat, falling back
// to relative dates and to a handful of unambiguous absolute formats.
//
// A date we cannot parse yields the zero time rather than an error: a chapter
// with an unreadable date is still a chapter worth downloading, and refusing
// the whole list over a locale mismatch would be the wrong trade.
func ParseDate(s, pattern string, now time.Time) time.Time {
	s = Collapse(s)
	if s == "" {
		return time.Time{}
	}
	if m := relativeDateRE.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}
		}
		switch strings.ToLower(m[2]) {
		case "second":
			return now.Add(-time.Duration(n) * time.Second)
		case "minute":
			return now.Add(-time.Duration(n) * time.Minute)
		case "hour":
			return now.Add(-time.Duration(n) * time.Hour)
		case "day":
			return now.AddDate(0, 0, -n)
		case "week":
			return now.AddDate(0, 0, -7*n)
		case "month":
			return now.AddDate(0, -n, 0)
		case "year":
			return now.AddDate(-n, 0, 0)
		}
	}
	layouts := []string{}
	if pattern != "" {
		layouts = append(layouts, DateLayout(pattern))
	}
	layouts = append(layouts, time.RFC3339, "2006-01-02", "January 2, 2006", "Jan 2, 2006", "02/01/2006")
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
