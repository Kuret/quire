package service

import (
	"fmt"
	"strings"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
)

// downloadedRow is one line of the downloaded overview.
//
// Every sentence on it is composed here (PLAN §2). The view renders the title,
// the source name and two backend-written lines, and formats nothing.
type downloadedRow struct {
	SourceID   string `json:"sourceId"`
	SourceName string `json:"sourceName"`
	SeriesID   string `json:"seriesId"`
	Title      string `json:"title"`

	// Detail is what the row says about the downloads themselves.
	Detail string `json:"detail"`

	// CoverURL is the cover the series was last listed with, or "" when Quire
	// has never seen one — a series downloaded before the cache existed, or one
	// whose entry has been evicted. Empty is a blank tile, never an error: the
	// row's title, source and detail are the information, and the picture is
	// the nicety.
	CoverURL string `json:"coverUrl,omitempty"`

	// LatestUUID is the document to hand MessageOpenInReader for "read the
	// newest thing I downloaded of this". It is the uuid and nothing else,
	// because that is the entire payload the reader handoff takes.
	//
	// Empty when there is nothing openable. Every record without one is already
	// skipped when the rows are built, so in practice this is only empty for a
	// row that should not exist.
	LatestUUID string `json:"latestUuid,omitempty"`

	// Openable is false when the source has been removed. The downloads are
	// still there and the row is still listed — they are the user's files —
	// but there is no source left to browse, so the row does not pretend to be
	// a way in.
	Openable bool `json:"openable"`

	// Note is why the row cannot be opened, or "".
	Note string `json:"note,omitempty"`
}

// RemovedSourceNote is what a row says when the source it came from is gone.
//
// The downloads survive a source being removed — they are documents on the
// tablet — so the row is listed rather than hidden. What it cannot do is open a
// series on a source that no longer exists, and saying so is better than a tap
// that does nothing.
const RemovedSourceNote = "The source this came from has been removed, so there is nothing to open. " +
	"The downloads are still on your reMarkable."

// NoDownloadsYet is the empty state.
const NoDownloadsYet = "Nothing downloaded yet. A chapter you download will show up here, " +
	"whether or not you are watching the series."

// sendDownloaded answers with every series that has at least one download.
//
// # Why the row is (source, series) and always names its source
//
// The user asked for this because downloads and the watched list are different
// sets: "since we can download stuff without watching them, the watch list on
// its own is not enough". The navigational half of the request decides the
// shape — *"make sure to separate by source if multiple entries from multiple
// sources exist for the same series, otherwise we dont know which one to
// link to"* — and it is applied **always**, not only when two rows would
// collide. A grouping that changes shape depending on what else is in the
// library is one nobody can predict, and a source label that appears only
// sometimes is one nobody can rely on.
//
// Newest first, because the store already sorts records by StoredAt descending
// and what the user did last is what they are most likely looking for. A series
// sorts by its newest record.
func (s *Service) sendDownloaded(out Sender) error {
	rows := s.downloadedRows()
	msg := map[string]any{"series": rows}
	if len(rows) == 0 {
		msg["empty"] = NoDownloadsYet
	}
	return send(out, appload.MessageDownloadedList, msg)
}

// downloadedRows groups the library records into one row per (source, series).
func (s *Service) downloadedRows() []downloadedRow {
	if s.libStore == nil {
		return nil
	}

	type key struct{ source, series string }
	var order []key
	group := map[key][]library.Record{}

	// Store order is newest first, so the order groups are first seen in is the
	// order they should come out in.
	for _, rec := range s.libStore.List() {
		if rec.DocumentUUID == "" {
			continue
		}
		k := key{rec.Source, rec.Series}
		if _, seen := group[k]; !seen {
			order = append(order, k)
		}
		group[k] = append(group[k], rec)
	}

	rows := make([]downloadedRow, 0, len(order))
	for _, k := range order {
		recs := group[k]
		row := downloadedRow{
			SourceID:   k.source,
			SeriesID:   k.series,
			Title:      downloadedTitle(recs),
			Detail:     downloadCount(len(recs)),
			CoverURL:   s.store.CoverURL(k.source, k.series),
			LatestUUID: latestUUID(recs),
		}
		if src, ok := s.store.Get(k.source); ok {
			row.SourceName, row.Openable = src.Name, true
		} else {
			// The source is gone; the files are not. See RemovedSourceNote.
			row.SourceName = k.source
			row.Note = RemovedSourceNote
		}
		rows = append(rows, row)
	}
	return rows
}

// downloadedTitle is the series' name, by the same rule the attach-time filing
// pass uses.
//
// One rule for both, because a record written before SeriesTitle existed is the
// same record in both places and it would be worse to have two answers for it.
// Where even the filename yields nothing, the series id is shown: it is a path
// or a slug and not pretty, but it is the truth about what this row is, and a
// row the user cannot identify is worse than an ugly one. Nothing is skipped —
// these are downloads they made.
func downloadedTitle(recs []library.Record) string {
	for _, rec := range recs {
		if title := seriesTitleOf(rec); title != "" {
			return title
		}
	}
	if len(recs) > 0 && recs[0].Series != "" {
		return recs[0].Series
	}
	return "Untitled series"
}

// latestUUID is the document behind the row's "Read" — the newest download of
// this series.
//
// The records arrive newest first (library.Store sorts by StoredAt
// descending), so the head of the group is the answer, with one correction: a
// volume too big for xochitl's upload cap is stored as several part documents
// written within seconds of each other, and opening part three of a volume the
// user has not started is not "read the newest thing". Where the newest record
// is a later part, the first part of that same volume is opened instead, which
// is the same "first part wins" rule storedVolumes uses for the chapter list.
func latestUUID(recs []library.Record) string {
	if len(recs) == 0 {
		return ""
	}
	best := recs[0]
	if best.Part > 1 {
		base := baseVolumeLabel(best)
		for _, rec := range recs {
			if baseVolumeLabel(rec) == base && rec.Part < best.Part {
				best = rec
			}
		}
	}
	return best.DocumentUUID
}

// baseVolumeLabel is the volume a record's label names, with the part suffix
// taken back off. A record that is not part of a split is its own label.
//
// The suffix is undone with the function that made it, so the two cannot drift
// — see partLabel.
func baseVolumeLabel(rec library.Record) string {
	if rec.Parts < 2 || rec.Part < 1 {
		return rec.Volume
	}
	return strings.TrimSuffix(rec.Volume, partLabel("", rec.Part, rec.Parts))
}

// downloadCount is what the row says about how much is downloaded.
//
// **"Downloads", not "chapters".** A record is one document — a volume, or one
// part of a volume too big for the upload cap — so the count of records is a
// count of files and is true for every record. "Chapters" would not be: a
// volume record holds several, and a record written before chapter ids were
// kept holds none that can be counted. A number that is right for some rows and
// wrong for others is worse than a vaguer one that is always right.
func downloadCount(n int) string {
	if n == 1 {
		return "1 download"
	}
	return fmt.Sprintf("%d downloads", n)
}
