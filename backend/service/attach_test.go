package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

// The startup race, from the backend's side.
//
// AppLoad discards a message aimed at a backend whose socket is not up yet, so
// a ListSources sent from QML's Component.onCompleted is sometimes thrown away
// and the UI waits forever. The user saw that twice as "my sources were gone",
// with the state file perfectly intact. The fix is that the backend does not
// wait to be asked.
func TestAttachPushesTheSourceListUnprompted(t *testing.T) {
	svc, store, rec := newService(t, routes())
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	// No ListSources is sent. This is the whole point.
	if err := svc.FrontendAttached(rec); err != nil {
		t.Fatal(err)
	}

	var list struct {
		Sources []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != 1 || list.Sources[0].Name != "Example Reader" {
		t.Fatalf("pushed %+v", list.Sources)
	}
}

// PLAN §1.3 ships with no sources, so an empty push is the normal first run —
// and it still has to arrive, or the empty state never renders either.
func TestAttachPushesAnEmptyListToo(t *testing.T) {
	svc, _, rec := newService(t, routes())
	if err := svc.FrontendAttached(rec); err != nil {
		t.Fatal(err)
	}

	var list struct {
		Sources []any `json:"sources"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != 0 {
		t.Errorf("pushed %d sources, want none", len(list.Sources))
	}
}

// Nothing to report is the usual case, and must produce no notice at all: a
// line on the first screen every launch would be noise.
func TestNoNoticeAfterACleanShutdown(t *testing.T) {
	svc, _, _ := newService(t, routes())
	if got := svc.StartupNotice(); got != "" {
		t.Errorf("notice %q, want none", got)
	}
}

func TestANoticeAfterAnAbnormalExit(t *testing.T) {
	svc := service.New(service.Options{PreviousSessionCrashed: true})
	got := svc.StartupNotice()
	if got != service.AbnormalExitNotice {
		t.Fatalf("notice %q", got)
	}
	// It has to reassure as well as report: the user's fear was that their
	// sources had been lost, and they had not been.
	for _, want := range []string{"unexpectedly", "Nothing was lost"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q does not say %q", got, want)
		}
	}
}
