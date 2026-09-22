package globalcomix

import (
	"fmt"
	"strconv"
	"strings"
)

// IDs are opaque, site-relative strings (PLAN §7.2), exactly like mangadex's
// "/manga/{uuid}" and "/chapter/{uuid}": stable across a base-URL change, and
// self-describing enough that a bad ID from a stale state file is an error
// rather than a request to the wrong endpoint.
const (
	seriesPrefix  = "/comics/"
	releasePrefix = "/releases/"
)

// seriesID formats a comic's numeric ID as the opaque handle Search and Series
// hand back.
func seriesID(id int) string { return seriesPrefix + strconv.Itoa(id) }

// parseSeriesID recovers the numeric comic ID from an opaque handle.
func parseSeriesID(id string) (int, error) {
	rest, ok := strings.CutPrefix(id, seriesPrefix)
	if !ok || rest == "" {
		return 0, fmt.Errorf("globalcomix: %q is not a series ID this theme produced", id)
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, fmt.Errorf("globalcomix: series ID %q: %w", id, err)
	}
	return n, nil
}

// releaseID formats a release's key — a UUID, not the numeric ID — as the
// opaque handle Chapters hands back.
//
// The key rather than the numeric ID because it is what the reading endpoints
// (readV3, and the reader-CDN's own URL template) actually take; the numeric
// ID is not accepted there at all (confirmed live: /v1/readV3/{numericID}
// answers ROUTE_NOT_FOUND, /v1/readV3/{key} answers 200).
func releaseID(key string) string { return releasePrefix + key }

// parseReleaseID recovers the release key from an opaque handle.
func parseReleaseID(id string) (string, error) {
	rest, ok := strings.CutPrefix(id, releasePrefix)
	if !ok || rest == "" {
		return "", fmt.Errorf("globalcomix: %q is not a chapter ID this theme produced", id)
	}
	return rest, nil
}
