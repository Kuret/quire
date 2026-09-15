package assemble_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/rickl/quire/backend/assemble"
)

func chapters(n int, volumeFor func(i int) string) []assemble.Chapter {
	chs := make([]assemble.Chapter, n)
	for i := range chs {
		v := ""
		if volumeFor != nil {
			v = volumeFor(i)
		}
		chs[i] = assemble.Chapter{
			ID:     fmt.Sprintf("ch-%d", i+1),
			Title:  fmt.Sprintf("Chapter %d", i+1),
			Number: strconv.Itoa(i + 1),
			Volume: v,
			Pages:  []assemble.Page{{Path: "p.jpg", Index: 0}},
		}
	}
	return chs
}

func TestGroupIntoVolumesByLabel(t *testing.T) {
	chs := chapters(7, func(i int) string {
		if i < 3 {
			return "1"
		}
		return "2"
	})

	vols := assemble.GroupIntoVolumes("Series", chs, 10)
	if len(vols) != 2 {
		t.Fatalf("got %d volumes, want 2", len(vols))
	}
	if vols[0].Label != "1" || len(vols[0].Chapters) != 3 {
		t.Errorf("volume 1 = %q with %d chapters", vols[0].Label, len(vols[0].Chapters))
	}
	if vols[1].Label != "2" || len(vols[1].Chapters) != 4 {
		t.Errorf("volume 2 = %q with %d chapters", vols[1].Label, len(vols[1].Chapters))
	}
	if vols[0].Title != "Series — Volume 1" {
		t.Errorf("title = %q", vols[0].Title)
	}
}

func TestGroupIntoVolumesByCount(t *testing.T) {
	vols := assemble.GroupIntoVolumes("Series", chapters(25, nil), 0) // default 10

	if len(vols) != 3 {
		t.Fatalf("got %d volumes, want 3", len(vols))
	}
	want := []int{10, 10, 5}
	for i, v := range vols {
		if len(v.Chapters) != want[i] {
			t.Errorf("volume %d has %d chapters, want %d", i+1, len(v.Chapters), want[i])
		}
		if v.Label != strconv.Itoa(i+1) {
			t.Errorf("volume %d label = %q", i+1, v.Label)
		}
	}
	// A count-grouped volume is named after its chapters: "Volume 2" would be
	// meaningless when the source has no volumes.
	if vols[0].Title != "Series — 1–10" {
		t.Errorf("title = %q, want the chapter range", vols[0].Title)
	}
	if vols[2].Title != "Series — 21–25" {
		t.Errorf("title = %q", vols[2].Title)
	}
}

func TestGroupIntoVolumesCustomCount(t *testing.T) {
	vols := assemble.GroupIntoVolumes("Series", chapters(7, nil), 3)
	if len(vols) != 3 {
		t.Fatalf("got %d volumes, want 3", len(vols))
	}
	if len(vols[2].Chapters) != 1 {
		t.Errorf("last volume has %d chapters, want 1", len(vols[2].Chapters))
	}
	if vols[2].Title != "Series — 7" {
		t.Errorf("single-chapter title = %q", vols[2].Title)
	}
}

// A series that gains volume labels partway through — common, since labels
// appear only once a print edition exists — must not merge or reorder across
// the boundary.
func TestGroupIntoVolumesMixedLabelling(t *testing.T) {
	chs := chapters(9, func(i int) string {
		if i < 4 {
			return "1"
		}
		return ""
	})

	vols := assemble.GroupIntoVolumes("Series", chs, 3)
	if len(vols) != 3 {
		t.Fatalf("got %d volumes, want 3: %+v", len(vols), vols)
	}
	if vols[0].Label != "1" || len(vols[0].Chapters) != 4 {
		t.Errorf("labelled run = %q with %d chapters", vols[0].Label, len(vols[0].Chapters))
	}
	if len(vols[1].Chapters) != 3 || len(vols[2].Chapters) != 2 {
		t.Errorf("unlabelled runs = %d, %d chapters", len(vols[1].Chapters), len(vols[2].Chapters))
	}

	// Reading order is preserved end to end.
	var seen []string
	for _, v := range vols {
		for _, c := range v.Chapters {
			seen = append(seen, c.ID)
		}
	}
	for i, id := range seen {
		if want := fmt.Sprintf("ch-%d", i+1); id != want {
			t.Fatalf("position %d = %s, want %s: reading order changed", i, id, want)
		}
	}
}

func TestGroupIntoVolumesEmpty(t *testing.T) {
	if vols := assemble.GroupIntoVolumes("Series", nil, 10); len(vols) != 0 {
		t.Errorf("got %d volumes for no chapters", len(vols))
	}
}

func TestVolumeSlug(t *testing.T) {
	cases := []struct {
		series, label, want string
	}{
		{"Test Series", "1", "Test-Series-v1"},
		{"名探偵 / Case ?", "2", "Case-v2"},
		{"", "", "series-v1"},
		{"../../etc/passwd", "3", "etc-passwd-v3"},
	}
	for _, tc := range cases {
		v := assemble.Volume{Series: tc.series, Label: tc.label}
		if got := v.Slug(); got != tc.want {
			t.Errorf("Slug(%q, %q) = %q, want %q", tc.series, tc.label, got, tc.want)
		}
	}
}

func TestVolumePageCount(t *testing.T) {
	v := assemble.Volume{Chapters: []assemble.Chapter{
		{Pages: make([]assemble.Page, 3)},
		{Pages: make([]assemble.Page, 5)},
	}}
	if got := v.PageCount(); got != 8 {
		t.Errorf("PageCount = %d, want 8", got)
	}
}
