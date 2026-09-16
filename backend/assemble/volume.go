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
// It is the unit of assembly, not a claim about how many chapters belong in
// one. PLAN §6 M4 used to say one PDF per volume and never one per chapter;
// that was **reversed on 2026-09-16 after living with it**, and the default is
// now one chapter per PDF even where the source publishes volume labels. The
// original reasoning weighed clutter and reading position and missed sampling:
// the first thing anyone does with an unfamiliar series is read a few pages to
// decide whether they want the rest, and a 7–10 minute wait before *anything*
// is readable makes that impossible. Grouping survives as a choice made at
// download time (see GroupingChapter and GroupingVolume), so a Volume here may
// hold one chapter or twenty.
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

// The groupings a download request can ask for (PLAN §6 M4, revised
// 2026-09-16). They live here rather than on a source because the question they
// answer — "am I about to be without a connection?" — is situational, not a
// property of a website. There is no third mode: fixed runs of N ignoring the
// labels only ever applied to sources with no labels, which are exactly the
// ones that now offer no volume view at all.
const (
	// GroupingChapter is one PDF per chapter. It is the default, and it is
	// always available whatever the source publishes.
	GroupingChapter = "chapter"

	// GroupingVolume groups on the source's own volume label, and is offered
	// only when the source publishes real labels and the reading order is
	// known.
	GroupingVolume = "volume"
)

// GroupingValues are the accepted spellings, in the order the UI offers them.
// Empty is also accepted on the wire and means GroupingChapter.
var GroupingValues = []string{GroupingChapter, GroupingVolume}

// DefaultChaptersPerVolume is the run length GroupIntoVolumes falls back to for
// the stretch of a series the source has not labelled.
//
// It is not a grouping in its own right. A volume view is offered only for a
// source that publishes labels, but labels usually appear only once a print
// edition exists, so the most recent chapters of a labelled series routinely
// carry none — this is what those become.
const DefaultChaptersPerVolume = 10

// GroupIntoVolumes splits chapters, in the order given, into volumes.
//
// This is the "volume" grouping mode, which is no longer the default: see
// Volume's doc comment. It is what a reader who wants one document per volume,
// and reading position carried across chapters, gets when they ask for it.
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

// GroupIntoRuns splits chapters, in the order given, into fixed runs of
// chaptersPerVolume, ignoring any volume labels the source publishes.
//
// It is the "count" grouping mode. Ignoring the labels is the whole point of
// asking for it: a source whose volumes are 40 chapters long, or whose labels
// are unreliable, is exactly the case where a reader wants runs of a size they
// chose. The run is titled after the chapters in it for the same reason the
// unlabelled case is — "Volume 2" is meaningless when the number is ours.
//
// chaptersPerVolume <= 0 means DefaultChaptersPerVolume.
func GroupIntoRuns(series string, chapters []Chapter, chaptersPerVolume int) []Volume {
	if chaptersPerVolume <= 0 {
		chaptersPerVolume = DefaultChaptersPerVolume
	}

	var vols []Volume
	for i := 0; i < len(chapters); i += chaptersPerVolume {
		j := min(i+chaptersPerVolume, len(chapters))
		vols = append(vols, Volume{
			Series:   series,
			Label:    fmt.Sprintf("%d", len(vols)+1),
			Title:    volumeTitleForRange(series, chapters[i:j]),
			Chapters: chapters[i:j:j],
		})
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

// ChapterNumberPad is how many digits a chapter number is padded to in a
// document name. Four, because real series run past a thousand chapters
// (One Piece is over 1100) and three would start sorting 1000 before 999.
const ChapterNumberPad = 4

// ChapterDocumentLabel is how one chapter names itself inside a document name:
// "Ch 0012", "Ch 0012.5", or the chapter's own title when it has no number.
//
// The padding is the whole point. PLAN §6 M5 established that Quire cannot
// create folders, so every chapter of every series lands flat in one Comics
// folder, sorted by name — and one PDF per chapter (§6 M4, reversed
// 2026-09-16) means ten times as many of them. Unpadded, "Ch 10" sorts between
// "Ch 1" and "Ch 2" and the folder is unusable. Chapter numbers are not always
// integers, so the fractional part is kept as the source wrote it: "0012.5"
// still sorts between "0012" and "0013", which is where it belongs.
//
// A chapter with no number at all ("Extra", "Omake") keeps its own title. An
// invented number would sort it somewhere false and claim something the source
// did not say.
func ChapterDocumentLabel(c Chapter) string {
	num := strings.TrimSpace(c.Number)
	if num != "" {
		whole, frac, hasFrac := strings.Cut(num, ".")
		if isDigits(whole) && (!hasFrac || isDigits(frac)) {
			for len(whole) < ChapterNumberPad {
				whole = "0" + whole
			}
			if hasFrac {
				return "Ch " + whole + "." + frac
			}
			return "Ch " + whole
		}
		// A number in a shape we did not expect is still the source's own
		// number; pass it through rather than dropping it.
		return "Ch " + num
	}
	if t := strings.TrimSpace(c.Title); t != "" {
		return t
	}
	return strings.TrimSpace(c.ID)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
