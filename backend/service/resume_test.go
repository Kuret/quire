package service_test

import (
	"encoding/json"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
)

// handoff reports a successful open, carrying where the frontend was.
//
// Its own helper rather than a shared fixture, because every case below needs a
// *fresh* service: a position that survives from the case before it is not a
// position anyone recorded.
func handoff(t *testing.T, svc *service.Service, rec *recorder, seriesID, cameFrom string, page int) {
	t.Helper()
	handle(t, svc, rec, appload.MessageOpenInReader, `{
		"documentUuid": "doc-1",
		"resume": {"sourceId": "example-reader", "sourceName": "Example Reader",
		           "seriesId": "`+seriesID+`", "title": "The Lantern Keeper",
		           "page": `+itoa(page)+`, "cameFrom": "`+cameFrom+`"}}`)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

type resumeMsg struct {
	Screen     string `json:"screen"`
	SourceID   string `json:"sourceId"`
	SourceName string `json:"sourceName"`
	SeriesID   string `json:"seriesId"`
	Title      string `json:"title"`
	Page       int    `json:"page"`
	CameFrom   string `json:"cameFrom"`
}

// The feature: the frontend closed itself on a handoff, and the next one is put
// back where it was.
func TestAttachRestoresTheScreenTheHandoffClosed(t *testing.T) {
	svc, store, rec := newService(t, nil)
	addSource(t, store)

	handoff(t, svc, rec, "/manga/the-lantern-keeper/", "downloaded", 3)

	fresh := &recorder{}
	if err := svc.FrontendAttached(fresh); err != nil {
		t.Fatal(err)
	}

	var got resumeMsg
	if err := json.Unmarshal(fresh.wait(t, appload.MessageResume), &got); err != nil {
		t.Fatal(err)
	}
	if got.Screen != "series" {
		t.Errorf("screen %q, want series", got.Screen)
	}
	if got.SeriesID != "/manga/the-lantern-keeper/" {
		t.Errorf("series %q", got.SeriesID)
	}
	if got.Page != 3 {
		t.Errorf("page %d, want the page they were on", got.Page)
	}
	if got.CameFrom != "downloaded" {
		t.Errorf("Back would go to %q", got.CameFrom)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title %q", got.Title)
	}
}

// Once. A position is a place the user left, not a screen the app opens on
// from then on: a second attach is a new session.
func TestAPositionIsRestoredOnlyOnce(t *testing.T) {
	svc, store, rec := newService(t, nil)
	addSource(t, store)

	handoff(t, svc, rec, "/manga/the-lantern-keeper/", "sources", 1)

	first := &recorder{}
	if err := svc.FrontendAttached(first); err != nil {
		t.Fatal(err)
	}
	first.wait(t, appload.MessageResume)

	second := &recorder{}
	if err := svc.FrontendAttached(second); err != nil {
		t.Fatal(err)
	}
	if hasFrame(second, appload.MessageResume) {
		t.Error("the same position was restored twice")
	}
}

// A source removed while the user was reading: there is nothing to browse, so
// there is nowhere to go back to. The sources screen, not an error.
func TestAPositionOnARemovedSourceIsDropped(t *testing.T) {
	svc, store, rec := newService(t, nil)
	addSource(t, store)

	handoff(t, svc, rec, "/manga/the-lantern-keeper/", "watching", 2)

	if err := store.Remove("example-reader"); err != nil {
		t.Fatal(err)
	}

	fresh := &recorder{}
	if err := svc.FrontendAttached(fresh); err != nil {
		t.Fatal(err)
	}
	if hasFrame(fresh, appload.MessageResume) {
		t.Error("a position on a removed source was restored")
	}
}

// Renamed rather than replayed: the header would otherwise show the name the
// source had when the user tapped Read.
func TestARestoredPositionCarriesTheCurrentSourceName(t *testing.T) {
	svc, store, rec := newService(t, nil)
	addSource(t, store)

	handoff(t, svc, rec, "/manga/the-lantern-keeper/", "sources", 1)

	if err := store.Rename("example-reader", "Renamed Reader"); err != nil {
		t.Fatal(err)
	}

	fresh := &recorder{}
	if err := svc.FrontendAttached(fresh); err != nil {
		t.Fatal(err)
	}
	var got resumeMsg
	if err := json.Unmarshal(fresh.wait(t, appload.MessageResume), &got); err != nil {
		t.Fatal(err)
	}
	if got.SourceName != "Renamed Reader" {
		t.Errorf("source name %q, want the current one", got.SourceName)
	}
}

// An origin this backend does not know becomes the sources screen. It ends up
// behind a Back button the user presses without looking, so it is never passed
// through unchecked.
func TestAnUnknownOriginBecomesTheSourcesScreen(t *testing.T) {
	svc, store, rec := newService(t, nil)
	addSource(t, store)

	handoff(t, svc, rec, "/manga/the-lantern-keeper/", "settings", 1)

	fresh := &recorder{}
	if err := svc.FrontendAttached(fresh); err != nil {
		t.Fatal(err)
	}
	var got resumeMsg
	if err := json.Unmarshal(fresh.wait(t, appload.MessageResume), &got); err != nil {
		t.Fatal(err)
	}
	if got.CameFrom != "sources" {
		t.Errorf("Back would go to %q, want sources", got.CameFrom)
	}
}

// A handoff that did not open anything records nothing: the frontend did not
// close, so there is no position to come back to.
func TestAFailedHandoffRemembersNothing(t *testing.T) {
	svc, store, rec := newService(t, nil)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageOpenInReader, `{
		"documentUuid": "doc-1", "missing": true,
		"resume": {"sourceId": "example-reader", "seriesId": "/manga/the-lantern-keeper/",
		           "page": 2, "cameFrom": "sources"}}`)

	fresh := &recorder{}
	if err := svc.FrontendAttached(fresh); err != nil {
		t.Fatal(err)
	}
	if hasFrame(fresh, appload.MessageResume) {
		t.Error("a handoff that failed left a position behind")
	}
}

// Nothing remembered, nothing pushed. Most attaches are ordinary launches and
// must not be told to go anywhere.
func TestAnOrdinaryAttachRestoresNothing(t *testing.T) {
	svc, store, _ := newService(t, nil)
	addSource(t, store)

	fresh := &recorder{}
	if err := svc.FrontendAttached(fresh); err != nil {
		t.Fatal(err)
	}
	if hasFrame(fresh, appload.MessageResume) {
		t.Error("an ordinary attach was told to restore a screen")
	}
}
