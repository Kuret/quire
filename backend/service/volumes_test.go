package service

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/theme"
)

// The source's own volume labels are the grouping, whenever it has them. PLAN
// §6 M4's runs of ten were always the fallback; until the ordering contract
// landed they were the only mechanism, because theme.Chapter had no label to
// group on.
func TestGroupUsesTheSourcesVolumeLabels(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Title: "Chapter 1", Number: 1, Volume: "1"},
		{ID: "c2", Title: "Chapter 2", Number: 2, Volume: "1"},
		{ID: "c3", Title: "Chapter 3", Number: 3, Volume: "2"},
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingVolume, 0)
	if len(plans) != 2 {
		t.Fatalf("%d volumes, want 2 — one per source label", len(plans))
	}
	if plans[0].Label != "1" || plans[1].Label != "2" {
		t.Errorf("labels %q and %q, want the source's", plans[0].Label, plans[1].Label)
	}
	if !plans[0].SourceLabelled {
		t.Error("a source-labelled volume must say so, or the user is told a number we invented")
	}
	if len(plans[0].Chapters) != 2 || len(plans[1].Chapters) != 1 {
		t.Errorf("chapters split %d/%d", len(plans[0].Chapters), len(plans[1].Chapters))
	}
}

// With no labels at all, runs of ten — and the volume must not claim to be the
// source's "Volume 1".
func TestGroupFallsBackToRunsWhenTheSourceHasNoVolumes(t *testing.T) {
	var chapters []theme.Chapter
	for i := 1; i <= 12; i++ {
		chapters = append(chapters, theme.Chapter{ID: string(rune('a' + i)), Number: float64(i)})
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingVolume, 0)
	if len(plans) != 2 {
		t.Fatalf("%d volumes, want 2 runs of %d", len(plans), assemble.DefaultChaptersPerVolume)
	}
	if plans[0].SourceLabelled {
		t.Error("a counted run must not be presented as the source's own volume")
	}
	if len(plans[0].Chapters) != assemble.DefaultChaptersPerVolume {
		t.Errorf("first run holds %d chapters", len(plans[0].Chapters))
	}
}

// The important one. A volume is a *run* of chapters, so building one out of a
// list the theme admits it could not order produces a silently scrambled book.
func TestGroupRefusesToBuildAVolumeFromAnUnorderedList(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Title: "Interlude", Number: -1, OrderUnknown: true},
		{ID: "c2", Title: "Finale", Number: -1, OrderUnknown: true},
		{ID: "c3", Title: "Prologue", Number: -1, OrderUnknown: true},
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingChapter, 0)
	if len(plans) != len(chapters) {
		t.Fatalf("%d volumes, want one per chapter", len(plans))
	}
	seen := map[string]bool{}
	for _, p := range plans {
		if !p.PerChapter {
			t.Errorf("volume %q is not marked as the per-chapter fallback", p.Label)
		}
		if !p.OrderUnknown {
			t.Errorf("volume %q does not record that the order was unknowable, so the user is never told", p.Label)
		}
		if len(p.Chapters) != 1 {
			t.Errorf("volume %q holds %d chapters", p.Label, len(p.Chapters))
		}
		if seen[p.Label] {
			t.Errorf("label %q is reused; it is the store key and the PDF filename", p.Label)
		}
		seen[p.Label] = true
	}
}

// The fallback names itself after the chapter. Calling a single chapter
// "Vol 12" would claim a structure the source never gave.
func TestAPerChapterFileIsNamedAfterItsChapter(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Title: "Interlude", Number: -1, OrderUnknown: true},
		{ID: "c2", Title: "Finale", Number: -1, OrderUnknown: true},
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingChapter, 0)
	name := documentName(plans[0], &assemble.Manifest{})
	if !strings.Contains(name, "Interlude") {
		t.Errorf("name %q does not name the chapter", name)
	}
	if strings.Contains(name, "Vol ") {
		t.Errorf("name %q calls a lone chapter a volume", name)
	}
	if !strings.HasSuffix(name, ".pdf") {
		t.Errorf("name %q", name)
	}
}

// A properly grouped volume is named for the series and the volume, because
// the library is flat and Comics holds every series side by side.
func TestAVolumeIsNamedSeriesAndVolume(t *testing.T) {
	plans := groupVolumes("Snotgirl", []theme.Chapter{
		{ID: "c1", Number: 1, Volume: "3"},
		{ID: "c2", Number: 2, Volume: "3"},
	}, theme.GroupingVolume, 0)
	if got := documentName(plans[0], &assemble.Manifest{}); got != "Snotgirl — Vol 3.pdf" {
		t.Errorf("name %q", got)
	}
}

// A single-chapter list has no order to get wrong, so it must not be dragged
// through the fallback.
//
// The flag this asserts on moved: per chapter is the default now (PLAN §6 M4,
// reversed 2026-09-16), so PerChapter no longer means "something went wrong"
// and OrderUnknown is the field that does. The claim is unchanged.
func TestASingleChapterIsNotTreatedAsUnordered(t *testing.T) {
	plans := groupVolumes("Snotgirl", []theme.Chapter{{ID: "c1", Title: "Only", Number: 1}}, theme.GroupingChapter, 0)
	if len(plans) != 1 {
		t.Fatalf("%d volumes", len(plans))
	}
	if plans[0].OrderUnknown {
		t.Error("a one-chapter series was reported as unorderable")
	}
}

// volumeContaining and storedVolumes must agree, because one decides what is
// downloaded and the other decides which rows offer to read it.
func TestVolumeContainingFindsEveryChapter(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Number: 1, Volume: "1"},
		{ID: "c2", Number: 2, Volume: "1"},
		{ID: "c3", Number: 3, Volume: "2"},
	}
	for _, c := range chapters {
		vol, ok := volumeContaining("Snotgirl", chapters, c.ID, theme.GroupingVolume, 0)
		if !ok {
			t.Fatalf("%s is in no volume", c.ID)
		}
		if vol.Label != c.Volume {
			t.Errorf("%s landed in volume %q, want %q", c.ID, vol.Label, c.Volume)
		}
	}
	if _, ok := volumeContaining("Snotgirl", chapters, "gone", theme.GroupingVolume, 0); ok {
		t.Error("a chapter that is not in the list must not resolve")
	}
}

// The reversal itself, and the one that matters most: a source that publishes
// volume labels still gets one PDF per chapter unless the user asked otherwise.
//
// PLAN §6 M4, reversed 2026-09-16. The original design read a volume label as
// an instruction. It is information: the reason is sampling, because the first
// thing anyone does with an unfamiliar series is read a few pages to decide
// whether they want the rest, and a 7–10 minute wait before anything is
// readable makes that impossible.
func TestTheDefaultIsOnePDFPerChapterEvenWithVolumeLabels(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Title: "Chapter 1", Number: 1, Volume: "1"},
		{ID: "c2", Title: "Chapter 2", Number: 2, Volume: "1"},
		{ID: "c3", Title: "Chapter 3", Number: 3, Volume: "2"},
	}
	// An untouched source: no grouping set at all, which is what every source
	// added before this setting existed looks like.
	var src theme.Source
	plans := groupVolumes("Snotgirl", chapters, src.Group(), src.Size())

	if len(plans) != len(chapters) {
		t.Fatalf("%d documents for %d chapters; the default is one each", len(plans), len(chapters))
	}
	for i, p := range plans {
		if len(p.Chapters) != 1 {
			t.Errorf("document %d holds %d chapters", i, len(p.Chapters))
		}
		if !p.PerChapter {
			t.Errorf("document %d is not marked per-chapter", i)
		}
		// Nothing went wrong here: the user is not owed an explanation, and a
		// note on every download is a note nobody reads.
		if p.OrderUnknown {
			t.Errorf("document %d claims the order was unknowable", i)
		}
		if p.SourceLabelled {
			t.Errorf("document %d claims to be the source's own volume", i)
		}
	}
	// The label is the store key, so it has to be the chapter's.
	if plans[0].Label != "1" || plans[2].Label != "3" {
		t.Errorf("labels %q…%q, want the chapter numbers", plans[0].Label, plans[2].Label)
	}
}

// Asking for volumes gets volumes. The setting is the whole reason the reversal
// is safe: a reader who preferred one document per volume keeps it.
func TestGroupingVolumeStillGroupsByTheSourcesVolumes(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Number: 1, Volume: "1"},
		{ID: "c2", Number: 2, Volume: "1"},
		{ID: "c3", Number: 3, Volume: "2"},
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingVolume, theme.DefaultGroupSize)
	if len(plans) != 2 {
		t.Fatalf("%d documents, want one per source label", len(plans))
	}
	if len(plans[0].Chapters) != 2 || !plans[0].SourceLabelled {
		t.Errorf("volume 1 holds %d chapters, sourceLabelled=%v",
			len(plans[0].Chapters), plans[0].SourceLabelled)
	}
	if got := documentName(plans[0], &assemble.Manifest{}); got != "Snotgirl — Vol 1.pdf" {
		t.Errorf("document name %q", got)
	}
}

// A fixed count ignores the labels, which is the point of choosing it: a source
// whose volumes are forty chapters long is exactly why someone would.
func TestGroupingCountIgnoresVolumeLabels(t *testing.T) {
	var chapters []theme.Chapter
	for i := 1; i <= 5; i++ {
		chapters = append(chapters, theme.Chapter{
			ID: fmt.Sprintf("c%d", i), Number: float64(i), Volume: "1",
		})
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingCount, 2)
	if len(plans) != 3 {
		t.Fatalf("%d documents, want 3 runs of 2 (2+2+1)", len(plans))
	}
	sizes := []int{2, 2, 1}
	for i, want := range sizes {
		if len(plans[i].Chapters) != want {
			t.Errorf("run %d holds %d chapters, want %d", i+1, len(plans[i].Chapters), want)
		}
		if plans[i].SourceLabelled {
			t.Error("a run we counted must not be presented as the source's own volume")
		}
	}
}

// Whatever the setting says, a series whose order the theme could not work out
// is still one chapter per file — and still says why. PLAN §7.2's ordering
// contract does not become negotiable because a grouping setting exists.
func TestAnUnknownOrderOverridesTheGroupingSetting(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Title: "Interlude", Number: -1, Volume: "1", OrderUnknown: true},
		{ID: "c2", Title: "Finale", Number: -1, Volume: "1", OrderUnknown: true},
	}
	for _, mode := range []string{theme.GroupingVolume, theme.GroupingCount} {
		plans := groupVolumes("Snotgirl", chapters, mode, 10)
		if len(plans) != len(chapters) {
			t.Fatalf("grouping=%s: %d documents, want one per chapter", mode, len(plans))
		}
		for _, p := range plans {
			if !p.OrderUnknown {
				t.Errorf("grouping=%s: %q does not record the unknown order, so nobody is told", mode, p.Label)
			}
		}
	}
}

// Naming, PLAN §6 M5's flat Comics folder. Ten times as many documents land in
// one folder sorted by name, so "Ch 10" sorting between "Ch 1" and "Ch 2" would
// make it unusable. Chapter numbers are not always integers.
func TestPerChapterDocumentsSortInAFlatFolder(t *testing.T) {
	chapters := []theme.Chapter{
		{ID: "c1", Title: "Chapter 2", Number: 2},
		{ID: "c2", Title: "Chapter 10", Number: 10},
		{ID: "c3", Title: "Chapter 12.5", Number: 12.5},
		{ID: "c4", Title: "Chapter 100", Number: 100},
	}
	plans := groupVolumes("Snotgirl", chapters, theme.GroupingChapter, 0)

	var names []string
	for _, p := range plans {
		names = append(names, documentName(p, &assemble.Manifest{}))
	}
	want := []string{
		"Snotgirl — Ch 0002.pdf",
		"Snotgirl — Ch 0010.pdf",
		"Snotgirl — Ch 0012.5.pdf",
		"Snotgirl — Ch 0100.pdf",
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("document %d is named %q, want %q", i, names[i], want[i])
		}
	}
	// The claim the padding exists for: reading order is name order.
	if !sort.StringsAreSorted(names) {
		t.Errorf("a flat Comics folder would show these out of order: %q", names)
	}
}

// A chapter the source gave no number for keeps its own title. Inventing a
// number would sort it somewhere false and claim something the source did not
// say — "Extra" and "Omake" are real.
func TestAnUnnumberedChapterIsNamedAfterItself(t *testing.T) {
	plans := groupVolumes("Snotgirl", []theme.Chapter{
		{ID: "extra-1", Title: "Extra", Number: -1},
	}, theme.GroupingChapter, 0)
	got := documentName(plans[0], &assemble.Manifest{})
	if got != "Snotgirl — Extra.pdf" {
		t.Errorf("document name %q", got)
	}
}
