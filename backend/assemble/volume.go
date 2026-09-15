package assemble

import (
	"fmt"
	"regexp"
	"strings"
)

// Page is one downloaded, already normalised page image on disk. Pages are
// normalised by backend/imageproc at save time; the assembler does not resize.
type Page struct {
	// Path is the local file. It must exist when Assemble runs.
	Path string

	// Index is the page's 0-based position within its chapter. It exists so a
	// caller that downloads pages concurrently can still hand them over in a
	// defined order, and so an out-of-order slice is a detectable bug rather
	// than a silently shuffled volume.
	Index int
}

// Chapter is one chapter's worth of pages, plus the volume it belongs to.
type Chapter struct {
	ID     string // stable source-side identifier
	Title  string // display title, e.g. "Chapter 12"
	Number string // source-side chapter number as text ("12", "12.5"); may be empty
	Volume string // source-side volume label; empty when the source has none
	Pages  []Page
}

// Volume is a group of chapters that becomes exactly one PDF.
//
// PLAN §6 M4 is emphatic that this is the unit: one PDF per volume, never one
// per chapter. Chapter PDFs clutter the stock library and make xochitl's
// reading position meaningless, because position is per document.
type Volume struct {
	// Series is the series title; it names the library folder in M5.
	Series string

	// Label is the source-side volume label when there is one ("3"), or the
	// synthesised label for a count-grouped volume ("1" for chapters 1–10).
	Label string

	// Title is the display title of the resulting document.
	Title string

	// Chapters in reading order.
	Chapters []Chapter
}

// DefaultChaptersPerVolume is the fallback grouping size for sources with no
// volume structure, per PLAN §6 M4.
const DefaultChaptersPerVolume = 10

// GroupIntoVolumes splits chapters, in the order given, into volumes.
//
// Chapters carrying a Volume label group by that label; runs of chapters with
// no label group by chaptersPerVolume. The two rules are applied over a single
// pass so a series that gains volume labels partway through — common, since
// labels usually appear only once a print edition exists — does not reorder or
// merge across the boundary. Reading order is always preserved.
//
// chaptersPerVolume <= 0 means DefaultChaptersPerVolume.
func GroupIntoVolumes(series string, chapters []Chapter, chaptersPerVolume int) []Volume {
	if chaptersPerVolume <= 0 {
		chaptersPerVolume = DefaultChaptersPerVolume
	}

	var vols []Volume
	unlabelledSeq := 0

	for i := 0; i < len(chapters); {
		ch := chapters[i]

		if ch.Volume != "" {
			// Take the whole run carrying this label.
			j := i
			for j < len(chapters) && chapters[j].Volume == ch.Volume {
				j++
			}
			vols = append(vols, Volume{
				Series:   series,
				Label:    ch.Volume,
				Title:    fmt.Sprintf("%s — Volume %s", series, ch.Volume),
				Chapters: chapters[i:j:j],
			})
			i = j
			continue
		}

		// Unlabelled: take up to chaptersPerVolume, stopping at the first
		// labelled chapter.
		j := i
		for j < len(chapters) && chapters[j].Volume == "" && j-i < chaptersPerVolume {
			j++
		}
		unlabelledSeq++
		label := fmt.Sprintf("%d", unlabelledSeq)
		vols = append(vols, Volume{
			Series:   series,
			Label:    label,
			Title:    volumeTitleForRange(series, chapters[i:j]),
			Chapters: chapters[i:j:j],
		})
		i = j
	}

	return vols
}

// volumeTitleForRange names a count-grouped volume after the chapters in it,
// because "Volume 2" is meaningless when the source has no volumes — the
// reader is looking for a chapter number.
func volumeTitleForRange(series string, chs []Chapter) string {
	if len(chs) == 0 {
		return series
	}
	first, last := chs[0].Number, chs[len(chs)-1].Number
	if first == "" {
		first = chs[0].Title
	}
	if last == "" {
		last = chs[len(chs)-1].Title
	}
	if first == last {
		return fmt.Sprintf("%s — %s", series, first)
	}
	return fmt.Sprintf("%s — %s–%s", series, first, last)
}

// PageCount is the number of pages the assembled PDF will have.
func (v Volume) PageCount() int {
	n := 0
	for _, ch := range v.Chapters {
		n += len(ch.Pages)
	}
	return n
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Slug is the base filename for the volume's PDF and manifest. It is derived,
// not random, so a resumed or repeated assembly overwrites its own output
// rather than accumulating duplicates in the library.
func (v Volume) Slug() string {
	s := strings.TrimSpace(v.Series)
	if s == "" {
		s = "series"
	}
	slug := strings.Trim(unsafeName.ReplaceAllString(s, "-"), "-.")
	if slug == "" {
		slug = "series"
	}
	if len(slug) > 64 {
		slug = strings.Trim(slug[:64], "-.")
	}
	label := strings.Trim(unsafeName.ReplaceAllString(v.Label, "-"), "-.")
	if label == "" {
		label = "1"
	}
	return fmt.Sprintf("%s-v%s", slug, label)
}
