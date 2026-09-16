package service_test

// The per-source grouping control, PLAN §6 M4 (reversed 2026-09-16). The same
// round trip as the strip-splitting control in stripsplit_test.go, for the same
// reasons: the UI sends the schema's spelling as data, the backend resolves
// "unset" so the control is never blank, and a bad value is refused in words.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/theme"
)

type groupingSources struct {
	Sources []struct {
		ID        string `json:"id"`
		Grouping  string `json:"grouping"`
		GroupSize int    `json:"groupSize"`
	} `json:"sources"`
}

// A source with nothing set must report the default rather than an empty
// string, or the row's button would read "Saving: " and say nothing.
func TestSourceListResolvesGroupingToTheDefault(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store) // no grouping set at all

	handle(t, svc, rec, appload.MessageListSources, `{}`)
	var msg groupingSources
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Sources) != 1 {
		t.Fatalf("%d sources, want 1", len(msg.Sources))
	}
	if msg.Sources[0].Grouping != theme.GroupingChapter {
		t.Errorf("a source with nothing set reports grouping %q, want %q",
			msg.Sources[0].Grouping, theme.GroupingChapter)
	}
	if msg.Sources[0].GroupSize != theme.DefaultGroupSize {
		t.Errorf("groupSize %d, want %d", msg.Sources[0].GroupSize, theme.DefaultGroupSize)
	}
}

// The round trip the control makes: stored, and echoed back as a source list so
// the row redraws with what was saved rather than with what the UI hoped for.
func TestSetSourceGroupingStoresAndEchoes(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageSetSourceGrouping,
		`{"sourceId":"example-reader","grouping":"volume"}`)

	var msg groupingSources
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Sources) != 1 || msg.Sources[0].Grouping != theme.GroupingVolume {
		t.Fatalf("the reply reports %+v, want grouping volume", msg.Sources)
	}

	src, ok := store.Get("example-reader")
	if !ok {
		t.Fatal("the source vanished")
	}
	if src.Grouping != theme.GroupingVolume {
		t.Errorf("stored grouping = %q, want volume", src.Grouping)
	}
}

// A bad value stays out of the store, and the refusal says what is allowed
// rather than answering with a code.
func TestSetSourceGroupingRefusesAnUnknownMode(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageSetSourceGrouping,
		`{"sourceId":"example-reader","grouping":"sometimes"}`)

	var errMsg struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &errMsg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errMsg.Message, "chapter") {
		t.Errorf("the refusal does not say what is allowed: %q", errMsg.Message)
	}
	if src, _ := store.Get("example-reader"); src.Grouping != "" {
		t.Errorf("a refused grouping was stored anyway: %q", src.Grouping)
	}
}

// Choosing "volume" actually changes what is downloaded, not just what is
// stored. This is the setting's whole purpose: a reader who preferred one
// document per volume keeps it after the default changed.
func TestGroupingVolumeChangesWhatIsDownloaded(t *testing.T) {
	svc, store, _, fake, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageSetSourceGrouping,
		`{"sourceId":"example-reader","grouping":"volume"}`)
	rec.wait(t, appload.MessageSources)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")

	if len(fake.uploaded) != 1 {
		t.Fatalf("%d documents uploaded, want 1 volume", len(fake.uploaded))
	}
	if name, _ := done["title"].(string); strings.Contains(name, "Ch ") {
		t.Errorf("grouping=volume still produced a per-chapter document: %q", name)
	}
	if pages, _ := done["pagesTotal"].(float64); pages <= 5 {
		t.Errorf("the download covered %v pages; a volume is more than one chapter", done["pagesTotal"])
	}
}
