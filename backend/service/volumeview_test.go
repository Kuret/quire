package service_test

// The chapter screen's volume view — PLAN §6 M4, revised 2026-09-16.
//
// Chapters are the default and always shown. Volumes appear alongside them only
// when the source publishes real labels and the reading order is known, because
// an empty tab is a worse answer than no tab. These tests are about the
// affordance, which the backend decides: the frontend has no way to tell.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

type volumeRowJSON struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Title        string `json:"title"`
	Detail       string `json:"detail"`
	ChapterCount int    `json:"chapterCount"`
	DocumentUUID string `json:"documentUuid"`
}

// volumesOf asks for a series the way the frontend does and returns the volume
// view the backend offered, which may be none.
func volumesOf(t *testing.T, svc *service.Service, seriesID string) []volumeRowJSON {
	t.Helper()
	rec := &recorder{}
	handle(t, svc, rec, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Volumes  []volumeRowJSON `json:"volumes"`
		Chapters []struct {
			ID string `json:"id"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Chapters) == 0 {
		t.Fatal("chapters are the default and are always shown; none arrived")
	}
	return detail.Volumes
}

// A source that publishes no volume labels shows chapters and nothing else.
func TestASourceWithNoLabelsIsOfferedNoVolumeView(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, _ := firstChapter(t, svc, rec)
	if vols := volumesOf(t, svc, seriesID); len(vols) != 0 {
		t.Errorf("a source with no labels was offered %d volume rows: %+v", len(vols), vols)
	}
}

// A source that publishes them is offered the view, and the rows say what they
// contain — which is the thing that tells the user whether a volume is worth
// taking before a journey.
func TestASourceWithLabelsIsOfferedAVolumeView(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, _ := firstChapter(t, svc, rec)
	vols := volumesOf(t, svc, seriesID)
	if len(vols) != 1 {
		t.Fatalf("%d volume rows, want the fixture's one: %+v", len(vols), vols)
	}
	v := vols[0]
	if v.ID == "" {
		t.Error("a volume row names no chapter, so a download request could not say what it wants")
	}
	if v.Title != "Volume 1" {
		t.Errorf("title %q, want the source's own label", v.Title)
	}
	if v.ChapterCount != 4 {
		t.Errorf("chapterCount %d, want the fixture's 4", v.ChapterCount)
	}
	if v.Detail == "" {
		t.Fatal("the row does not say what the volume contains")
	}
	for _, want := range []string{"4", "chapters"} {
		if !strings.Contains(v.Detail, want) {
			t.Errorf("detail %q does not mention %q", v.Detail, want)
		}
	}
	if v.DocumentUUID != "" {
		t.Errorf("nothing has been downloaded, yet the row offers to read %q", v.DocumentUUID)
	}
}

// PLAN §7.2's ordering contract overrides everything. A list we could not order
// must not offer a volume view at all: assembling one would be the scrambled
// book the contract exists to prevent, and offering it is promising one.
func TestAnUnorderableSeriesIsOfferedNoVolumeView(t *testing.T) {
	r := downloadRoutes(t)
	r["POST /manga/the-lantern-keeper/ajax/chapters/"] = themetest.Route{File: "chapters-unordered.html"}
	svc, store, _, _, rec := newDownloadServiceWith(t, r)
	addVolumeSource(t, store)

	seriesID, _ := firstChapter(t, svc, rec)
	if vols := volumesOf(t, svc, seriesID); len(vols) != 0 {
		t.Errorf("a series with no established order offered %d volume rows: %+v", len(vols), vols)
	}
}

// A volume the user has downloaded as a volume offers Read.
func TestADownloadedVolumeRowOffersRead(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")

	vols := volumesOf(t, svc, seriesID)
	if len(vols) != 1 {
		t.Fatalf("%d volume rows", len(vols))
	}
	if vols[0].DocumentUUID == "" {
		t.Error("the volume is on the tablet and its row does not offer to read it")
	}
}

// The chapters of a volume downloaded one at a time are several documents, not
// the volume. The row goes on offering the volume rather than claiming a
// document that would open a single chapter.
func TestAVolumeOfSeparatelyDownloadedChaptersStillOffersTheVolume(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")

	vols := volumesOf(t, svc, seriesID)
	if len(vols) != 1 {
		t.Fatalf("%d volume rows", len(vols))
	}
	if vols[0].DocumentUUID != "" {
		t.Errorf("the row offers to read %q, which holds one chapter and not the volume",
			vols[0].DocumentUUID)
	}
}

// Downloading a volume whose chapters are already on the tablet as individual
// files does not fetch their pages again.
//
// Nothing was added for this: PLAN §6 M4's resume works by skipping page files
// that already exist, and the page directory is keyed on (source, series,
// chapter) with no volume in the path — so pages a per-chapter download left
// behind are exactly the files a later volume download skips. It is pinned here
// because it is the kind of thing a change to the download directory's layout
// would break silently, and the cost of getting it wrong is refetching a
// 300-page volume on a metered connection.
func TestAVolumeReusesPagesAlreadyDownloadedPerChapter(t *testing.T) {
	f := themetest.New(t, perChapterRoutes(t))
	reg := theme.NewRegistry()
	reg.MustRegister(volumeTheme{madara.NewWithClock(f, func() time.Time { return fixedNow })})

	svc, store, _, _, rec := newDownloadServiceWith(t, perChapterRoutes(t),
		func(o *service.Options) {
			o.Registry = reg
			o.Fetcher = f
		})
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)

	// One chapter on its own first, the way sampling a series goes.
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")
	afterChapter := len(imageReferers(t, f))
	if afterChapter == 0 {
		t.Fatal("the per-chapter download fetched no pages")
	}
	firstSlug := chapterSlugOf(t, chapterID)
	before := imageFetchesFor(f, firstSlug)
	if before == 0 {
		t.Fatalf("no page of %s was fetched by the per-chapter download", firstSlug)
	}

	// Now the whole volume, which includes the chapter already on disk. A fresh
	// recorder, so waiting for "done" waits for *this* download rather than
	// matching the finished one still sitting in the old one.
	second := &recorder{}
	handle(t, svc, second, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, second, "done")

	if after := imageFetchesFor(f, firstSlug); after != before {
		t.Errorf("%s's pages were fetched %d times in all, want the %d from the first download; "+
			"the volume refetched pages that were already on disk", firstSlug, after, before)
	}
}

// imageFetchesFor counts page-image requests whose path names this chapter.
func imageFetchesFor(f *themetest.Fetcher, slug string) int {
	n := 0
	for _, c := range f.Calls() {
		if strings.Contains(c.URL, "/lantern-keeper/"+slug+"/") && strings.HasSuffix(c.URL, ".jpg") {
			n++
		}
	}
	return n
}

// chapterSlugOf is the fixture chapter ID's last path segment, which is how
// perChapterRoutes names that chapter's images.
func chapterSlugOf(t *testing.T, chapterID string) string {
	t.Helper()
	trimmed := strings.Trim(chapterID, "/")
	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return trimmed
	}
	return trimmed[i+1:]
}
