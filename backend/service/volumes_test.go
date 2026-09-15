package service

import (
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
	plans := groupVolumes("Snotgirl", chapters)
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
	plans := groupVolumes("Snotgirl", chapters)
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
	plans := groupVolumes("Snotgirl", chapters)
	if len(plans) != len(chapters) {
		t.Fatalf("%d volumes, want one per chapter", len(plans))
	}
	seen := map[string]bool{}
	for _, p := range plans {
		if !p.PerChapter {
			t.Errorf("volume %q is not marked as the per-chapter fallback", p.Label)
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
	plans := groupVolumes("Snotgirl", chapters)
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
	})
	if got := documentName(plans[0], &assemble.Manifest{}); got != "Snotgirl — Vol 3.pdf" {
		t.Errorf("name %q", got)
	}
}

// A single-chapter list has no order to get wrong, so it must not be dragged
// through the fallback.
func TestASingleChapterIsNotTreatedAsUnordered(t *testing.T) {
	plans := groupVolumes("Snotgirl", []theme.Chapter{{ID: "c1", Title: "Only", Number: 1}})
	if len(plans) != 1 {
		t.Fatalf("%d volumes", len(plans))
	}
	if plans[0].PerChapter {
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
		vol, ok := volumeContaining("Snotgirl", chapters, c.ID)
		if !ok {
			t.Fatalf("%s is in no volume", c.ID)
		}
		if vol.Label != c.Volume {
			t.Errorf("%s landed in volume %q, want %q", c.ID, vol.Label, c.Volume)
		}
	}
	if _, ok := volumeContaining("Snotgirl", chapters, "gone"); ok {
		t.Error("a chapter that is not in the list must not resolve")
	}
}
