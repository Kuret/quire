package service_test

import (
	"strconv"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

func TestSavePositionIsRememberedAndClamped(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	before, ok := h.shelfStore.Get(key)
	if !ok {
		t.Fatal("chapter was not saved")
	}
	pageCount := len(before.Pages)
	if pageCount < 2 {
		t.Fatalf("fixture only has %d page(s); the clamp needs at least 2", pageCount)
	}

	handle(t, h.svc, rec, appload.MessageSavePosition,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`","position":1}`)

	// No reply is sent; poll the store directly rather than the socket.
	waitFor(t, func() bool {
		got, _ := h.shelfStore.Get(key)
		return got.Position == 1
	})

	// A position past the last page clamps to the last page rather than
	// being stored as-is or refused.
	handle(t, h.svc, rec, appload.MessageSavePosition,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+
			`","position":`+strconv.Itoa(pageCount+50)+`}`)
	waitFor(t, func() bool {
		got, _ := h.shelfStore.Get(key)
		return got.Position == pageCount-1
	})
}
