package service

import (
	"fmt"
	"time"

	"github.com/rickl/quire/backend/seriescache"
	"github.com/rickl/quire/backend/theme"
)

// PLAN §12.12: opening a series answers instantly from what Quire already
// knows about it — a cached live fetch, or failing that whatever is saved or
// downloaded — while a fresh fetch runs behind it, so a tablet with no
// connection still shows the chapters it has rather than a spinner that never
// resolves.

// seriesChapterRow is one row of MessageSeriesDetailResult's "chapters" array.
// See runSeriesDetail's own history for why it is trimmed to this shape: a
// long series can carry hundreds of chapters and the socket's real ceiling is
// a few hundred KB (PLAN §3.1).
type seriesChapterRow struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Number    float64 `json:"number"`
	Published string  `json:"published,omitempty"`
	Scanlator string  `json:"scanlator,omitempty"`

	// DocumentUUID is set when this chapter's volume is already on the
	// tablet. It is what turns the row's button from "Download" into "Read"
	// (PLAN §6 M6) — without it, a volume downloaded last week is
	// indistinguishable from one never fetched.
	DocumentUUID string `json:"documentUuid,omitempty"`
	VolumeLabel  string `json:"volumeLabel,omitempty"`

	// Saved is true when this chapter is saved in Quire — independently of
	// DocumentUUID, since a chapter can be both saved and in the library at
	// once, each an independent copy (see download.go's runDownload).
	Saved bool `json:"saved"`
}

// buildSeriesDetailPayload assembles MessageSeriesDetailResult's payload from
// a series and its chapters, whatever supplied them — a live fetch, the
// cache, or a synthesis from what is on the tablet. cached, fetchedAt and note
// are PLAN §12.12's additions: cached says this did not just come off the
// network, fetchedAt is when the underlying data was actually fetched (zero
// for a synthesis, which was never fetched at all), and note is the sentence
// shown above the chapter list explaining what is being shown and why.
func (s *Service) buildSeriesDetailPayload(sourceID, kind string, series theme.Series, chapters []theme.Chapter,
	private, cached bool, fetchedAt time.Time, note string) map[string]any {

	stored := s.storedVolumes(sourceID, series.ID, series.Title, chapters)
	saved := s.storedSaved(sourceID, series.ID)

	rows := make([]seriesChapterRow, 0, len(chapters))
	for _, c := range chapters {
		r := seriesChapterRow{ID: c.ID, Title: c.Title, Number: c.Number, Scanlator: c.Scanlator,
			Saved: saved[c.ID]}
		if !c.Published.IsZero() {
			r.Published = c.Published.UTC().Format("2006-01-02")
		}
		if rec, ok := stored[c.ID]; ok {
			r.DocumentUUID, r.VolumeLabel = rec.DocumentUUID, rec.Volume
		}
		rows = append(rows, r)
	}

	payload := map[string]any{
		"sourceId": sourceID,
		// What this series' chapters are (see kind.go) — in particular,
		// "book" says a source has no page images at all, which is what
		// tells the chapter list to hide Try.
		"kind":     kind,
		"series":   series,
		"chapters": rows,
		"volumes":  volumeRows(series.Title, chapters, stored, saved),
		// Private says whether this source's chapters may ever go to the
		// reMarkable library (PrivateSourceLibraryRefusal).
		"private": private,
	}
	if cached {
		payload["cached"] = true
		if !fetchedAt.IsZero() {
			payload["fetchedAt"] = fetchedAt.UTC().Format(time.RFC3339)
		}
	}
	if note != "" {
		payload["note"] = note
	}
	return payload
}

// seriesCacheProtect builds the Protect callback the cache's eviction uses:
// a series with saved chapters, a library record or a watch must never be
// dropped just to make room for casual browsing (PLAN §12.12) — those are
// exactly the series whose offline reply matters.
func (s *Service) seriesCacheProtect() seriescache.Protect {
	return func(sourceID, seriesID string) bool {
		if len(s.savedRecordsFor(sourceID, seriesID)) > 0 {
			return true
		}
		if len(s.recordsFor(sourceID, seriesID)) > 0 {
			return true
		}
		if s.store != nil && s.store.IsWatched(sourceID, seriesID) {
			return true
		}
		return false
	}
}

// synthesizeSeriesDetail builds a series and its chapters from what is
// already on the tablet — saved chapters and library records — for a series
// that has never been fetched successfully while the cache existed (so there
// is nothing in seriescache) and cannot be fetched now (so there is nothing
// else to show). ok is false when there is nothing at all to synthesise from,
// in which case the caller falls back to today's plain error.
func (s *Service) synthesizeSeriesDetail(sourceID, seriesID string) (theme.Series, []theme.Chapter, bool) {
	savedRecs := s.savedRecordsFor(sourceID, seriesID)
	libRecs := s.recordsFor(sourceID, seriesID)
	if len(savedRecs) == 0 && len(libRecs) == 0 {
		return theme.Series{}, nil, false
	}

	title := ""
	if len(libRecs) > 0 {
		title = downloadedTitle(libRecs)
	}
	if title == "" || title == "Untitled series" {
		if t := savedSeriesTitle(savedRecs); t != "" {
			title = t
		}
	}
	if title == "" {
		title = "Untitled series"
	}

	chapters := map[string]theme.Chapter{}
	for _, rec := range savedRecs {
		if rec.Chapter == "" || rec.IsBook() {
			continue
		}
		chapters[rec.Chapter] = theme.Chapter{
			ID:     rec.Chapter,
			Title:  chapterTitleOr(rec.ChapterTitle, rec.Chapter),
			Number: numberOr(rec.Number),
		}
	}
	for _, rec := range libRecs {
		for _, id := range rec.Chapters {
			if id == "" {
				continue
			}
			if _, ok := chapters[id]; ok {
				continue
			}
			chapters[id] = theme.Chapter{
				ID:     id,
				Title:  fmt.Sprintf("Chapter %s", id),
				Number: -1,
			}
		}
	}

	out := make([]theme.Chapter, 0, len(chapters))
	for _, c := range chapters {
		out = append(out, c)
	}
	sortSynthesisedChapters(out)

	return theme.Series{ID: seriesID, Title: title}, out, true
}

func chapterTitleOr(title, fallback string) string {
	if title != "" {
		return title
	}
	return fmt.Sprintf("Chapter %s", fallback)
}

// numberOr reports a shelf record's chapter number, or -1 (the theme
// convention for "unknown") for the zero value — which shelf.Record's own
// omitempty tag cannot tell apart from a genuine chapter zero, but a
// synthesised offline listing errs toward "unknown" rather than asserting a
// number that might not be the one the source actually used.
func numberOr(n float64) float64 {
	if n == 0 {
		return -1
	}
	return n
}

// sortSynthesisedChapters orders by number, ascending, with unknown numbers
// (-1) trailing in the order they were encountered — there is no reading
// order to recover from records alone, so this is the best guess available
// rather than a claim of correctness.
func sortSynthesisedChapters(chapters []theme.Chapter) {
	// A stable sort so ties (including every -1) keep the order the maps
	// happened to range in — deterministic within one run, which is all a
	// synthesis needs.
	for i := 1; i < len(chapters); i++ {
		for j := i; j > 0 && chapterLess(chapters[j], chapters[j-1]); j-- {
			chapters[j], chapters[j-1] = chapters[j-1], chapters[j]
		}
	}
}

func chapterLess(a, b theme.Chapter) bool {
	if a.Number < 0 || b.Number < 0 {
		return false
	}
	return a.Number < b.Number
}

// cachedNote is the sentence shown above the chapter list when it came
// straight from the cache while a fresh fetch is still in flight.
func cachedNote(age time.Duration) string {
	return fmt.Sprintf("Showing the chapter list from %s while Quire checks for new chapters.", humanAge(age))
}

// synthesisedNote is cachedNote's counterpart when there was no cached fetch
// at all and the listing was built from what is on the tablet instead.
const synthesisedNote = "Showing only the chapters on this reMarkable while Quire checks the site."

// cachedFailureNote is shown when a live fetch that followed an immediate
// cached reply then failed: the cached reply stands, but the sentence above
// it now says why nothing newer arrived.
func cachedFailureNote(sourceName string, age time.Duration) string {
	return fmt.Sprintf("Couldn't reach %s, so this is the chapter list from %s.", sourceName, humanAge(age))
}

// synthesisedFailureNote is cachedFailureNote's counterpart for a synthesised
// reply.
func synthesisedFailureNote(sourceName string) string {
	return fmt.Sprintf("Couldn't reach %s, so only the chapters on this reMarkable are shown.", sourceName)
}

// humanAge renders a duration as the humanised age PLAN §12.12 asks for:
// "a moment ago", "2 hours ago", "3 days ago". Composed here rather than in
// QML because PLAN §2 puts every user-facing sentence in the backend.
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "a moment ago"
	case d < time.Hour:
		return agePhrase(roundDiv(d, time.Minute), "minute")
	case d < 24*time.Hour:
		return agePhrase(roundDiv(d, time.Hour), "hour")
	default:
		return agePhrase(roundDiv(d, 24*time.Hour), "day")
	}
}

// roundDiv divides d by unit, rounding to the nearest whole unit and never
// reporting less than one — the bracket above has already decided d is old
// enough to be counted in this unit, so "0 minutes ago" would be a lie about
// which bracket this is.
func roundDiv(d, unit time.Duration) int {
	n := int((d + unit/2) / unit)
	if n < 1 {
		n = 1
	}
	return n
}

func agePhrase(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
