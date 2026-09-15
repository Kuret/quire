package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/assemble"
)

// pagesOnDisk writes n page files of the given size and returns them, because
// splitToBudget measures the real files rather than trusting a caller's
// arithmetic.
func pagesOnDisk(t *testing.T, dir, chapter string, n int, size int) []assemble.Page {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, chapter), 0o755); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, size)
	var pages []assemble.Page
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, chapter, fmt.Sprintf("%04d.jpg", i))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		pages = append(pages, assemble.Page{Path: path, Index: i})
	}
	return pages
}

func volumeOf(t *testing.T, dir string, chapterPages ...int) volumePlan {
	t.Helper()
	var chs []assemble.Chapter
	for i, n := range chapterPages {
		id := fmt.Sprintf("c%d", i+1)
		chs = append(chs, assemble.Chapter{
			ID:     id,
			Title:  fmt.Sprintf("Chapter %d", i+1),
			Number: fmt.Sprintf("%d", i+1),
			Pages:  pagesOnDisk(t, dir, id, n, 1000),
		})
	}
	return volumePlan{Volume: assemble.Volume{
		Series: "Snotgirl", Label: "3", Title: "Snotgirl — Volume 3", Chapters: chs,
	}}
}

// A volume that already fits is left exactly as it was — no parts, no renaming.
func TestAVolumeWithinBudgetIsNotSplit(t *testing.T) {
	vol := volumeOf(t, t.TempDir(), 10, 10)
	parts, err := splitToBudget(vol, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 {
		t.Fatalf("%d parts, want 1", len(parts))
	}
	if parts[0].Label != "3" || parts[0].Parts != 0 {
		t.Errorf("an unsplit volume was relabelled: %q part %d of %d",
			parts[0].Label, parts[0].Part, parts[0].Parts)
	}
}

// Over budget, the seam goes between chapters: a part boundary inside a chapter
// is a seam the reader trips over, and the source already put one here.
func TestAnOversizedVolumeSplitsBetweenChapters(t *testing.T) {
	dir := t.TempDir()
	vol := volumeOf(t, dir, 10, 10, 10) // 30 KB total
	parts, err := splitToBudget(vol, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("%d parts, want 2", len(parts))
	}

	for i, part := range parts {
		if part.Part != i+1 || part.Parts != 2 {
			t.Errorf("part %d says %d of %d", i+1, part.Part, part.Parts)
		}
		for _, ch := range part.Chapters {
			if len(ch.Pages) != 10 {
				t.Errorf("chapter %s was cut: %d pages", ch.ID, len(ch.Pages))
			}
		}
	}
	if got := len(parts[0].Chapters) + len(parts[1].Chapters); got != 3 {
		t.Errorf("%d chapters across the parts, want all 3", got)
	}
	// Reading order across parts must still be the volume's order.
	var order []string
	for _, part := range parts {
		for _, ch := range part.Chapters {
			order = append(order, ch.ID)
		}
	}
	if strings.Join(order, ",") != "c1,c2,c3" {
		t.Errorf("chapters across the parts read %v", order)
	}
}

// The names have to sort and read correctly, because in a flat Comics folder
// they sit next to each other (PLAN §6 M4).
func TestPartsAreNamedSoTheySort(t *testing.T) {
	vol := volumeOf(t, t.TempDir(), 10, 10, 10)
	parts, err := splitToBudget(vol, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Snotgirl — Vol 3 (part 1 of 2).pdf", "Snotgirl — Vol 3 (part 2 of 2).pdf"}
	for i, part := range parts {
		if got := documentName(part, &assemble.Manifest{}); got != want[i] {
			t.Errorf("part %d is called %q, want %q", i+1, got, want[i])
		}
	}
	// The label is the store key and the PDF filename stem; both must differ.
	if parts[0].Label == parts[1].Label || parts[0].Slug() == parts[1].Slug() {
		t.Errorf("parts collide: %q/%q and %q/%q",
			parts[0].Label, parts[1].Label, parts[0].Slug(), parts[1].Slug())
	}
}

// A single chapter bigger than the whole budget cannot be helped by splitting
// between chapters, and refusing would leave the user with nothing.
func TestAChapterLargerThanTheBudgetIsCutAcrossParts(t *testing.T) {
	dir := t.TempDir()
	vol := volumePlan{Volume: assemble.Volume{
		Series: "Snotgirl", Label: "3", Title: "Snotgirl — Volume 3",
		Chapters: []assemble.Chapter{{
			ID: "long", Title: "The Long One", Number: "1",
			Pages: pagesOnDisk(t, dir, "long", 30, 1000),
		}},
	}}
	parts, err := splitToBudget(vol, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("%d parts, want 3", len(parts))
	}
	total := 0
	for _, part := range parts {
		for _, ch := range part.Chapters {
			if ch.ID != "long" {
				t.Errorf("piece has ID %q; the chapter ID is the map key and must survive", ch.ID)
			}
			total += len(ch.Pages)
		}
	}
	if total != 30 {
		t.Errorf("%d pages across the parts, want all 30", total)
	}
	// Every page must still appear exactly once, in order.
	var seen []string
	for _, part := range parts {
		for _, ch := range part.Chapters {
			for _, pg := range ch.Pages {
				seen = append(seen, filepath.Base(pg.Path))
			}
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i-1] >= seen[i] {
			t.Fatalf("pages out of order across the split: %s then %s", seen[i-1], seen[i])
		}
	}

	// assemble rejects a chapter whose pages do not start at 0 and run in
	// order, so each piece has to be renumbered from 0 in its own PDF. Getting
	// this wrong fails the build of every part after the first.
	for _, part := range parts {
		for _, ch := range part.Chapters {
			for i, pg := range ch.Pages {
				if pg.Index != i {
					t.Fatalf("piece page %d has Index %d; a piece must renumber from 0",
						i, pg.Index)
				}
			}
		}
	}
}

// A chapter cut across parts is recorded once, not twice, so the chapter →
// document map has one answer.
func TestChapterIDsDoNotRepeatAcrossAPiecedChapter(t *testing.T) {
	dir := t.TempDir()
	vol := volumePlan{Volume: assemble.Volume{
		Series: "Snotgirl", Label: "3",
		Chapters: []assemble.Chapter{{ID: "long", Pages: pagesOnDisk(t, dir, "long", 4, 1000)}},
	}}
	parts, err := splitToBudget(vol, 2_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if ids := chapterIDs(part.Volume); len(ids) != 1 || ids[0] != "long" {
			t.Errorf("recorded chapter IDs %v", ids)
		}
	}
}

// A page file that is not there is a bug worth surfacing, not a volume to
// silently mis-split.
func TestSplitReportsAMissingPage(t *testing.T) {
	vol := volumeOf(t, t.TempDir(), 2)
	vol.Chapters[0].Pages[0].Path = filepath.Join(t.TempDir(), "gone.jpg")
	if _, err := splitToBudget(vol, 1_000); err == nil {
		t.Fatal("want an error")
	}
}
