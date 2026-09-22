package asurascans

import (
	"fmt"
	"strconv"
	"strings"
)

// As in every other theme, identifiers are stored site-relative, so a source
// re-pointed at a mirror later keeps working. Here the relative form doubles
// as a real, resolvable page path: "/comics/{slug}" 302s to the site's own
// "/comics/{slug}-{buildTag}" on the live site (observed 2026-09-21) — the
// suffix is a deployment-wide tag, not part of any one series' identity, so
// storing IDs without it is both simpler and more stable across a rebuild.

// seriesID builds a series' stored ID from its API slug.
func seriesID(slug string) string { return "/comics/" + slug }

// seriesSlug recovers the API slug from a series ID. A bare slug with no
// leading path is accepted too, since that is what a caller reconstructing an
// ID from stored state, or a user copying just the slug, tends to have.
func seriesSlug(id string) (string, error) {
	id = strings.Trim(strings.TrimSpace(id), "/")
	segs := strings.Split(id, "/")
	switch {
	case len(segs) >= 2 && segs[0] == "comics" && segs[1] != "":
		return segs[1], nil
	case len(segs) == 1 && segs[0] != "":
		return segs[0], nil
	default:
		return "", fmt.Errorf("%s: %q is not a series id", ID, id)
	}
}

// formatChapterNumber renders a chapter number the way the API's own path
// segment expects it: "85" for a whole chapter, "10.5" for a half one, never
// trailing zeros the API was not given.
func formatChapterNumber(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// chapterTitle is the display title for a chapter identified only by number,
// matching the site's own wording ("Chapter 85") — there is nothing else to
// draw one from, since the API's chapter rows carry no separate title field.
func chapterTitle(n float64) string {
	return "Chapter " + formatChapterNumber(n)
}

// chapterID builds a chapter's stored ID from its series slug and number.
func chapterID(slug string, number float64) string {
	return "/comics/" + slug + "/chapter/" + formatChapterNumber(number)
}

// parseChapterID splits a chapter ID back into the series slug and the
// number-string the API's page endpoint takes as a path segment.
func parseChapterID(id string) (slug, number string, err error) {
	id = strings.Trim(strings.TrimSpace(id), "/")
	segs := strings.Split(id, "/")
	if len(segs) == 4 && segs[0] == "comics" && segs[2] == "chapter" && segs[1] != "" && segs[3] != "" {
		return segs[1], segs[3], nil
	}
	return "", "", fmt.Errorf("%s: %q is not a chapter id", ID, id)
}
