package service

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// viewRecorder is the smallest Sender that can answer "what came back", kept
// here rather than borrowed from service_test.go because handleSetView is
// unexported: the switch in service.go that would let an external test reach it
// through Handle is wired separately.
type viewRecorder struct {
	mu   sync.Mutex
	sent []sentFrame
}

type sentFrame struct {
	msgType int32
	payload []byte
}

func (r *viewRecorder) Send(msgType int32, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentFrame{msgType, append([]byte(nil), payload...)})
	return nil
}

// errorCode returns the code of the single error frame sent, or "" if none was.
func (r *viewRecorder) errorCode(t *testing.T) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	code := ""
	for _, f := range r.sent {
		if f.msgType != appload.MessageError {
			continue
		}
		var e struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(f.payload, &e); err != nil {
			t.Fatalf("error frame is not readable JSON: %v", err)
		}
		if code != "" {
			t.Fatalf("more than one error frame: %q then %q", code, e.Code)
		}
		code = e.Code
	}
	return code
}

func newViewService(t *testing.T) (*Service, *state.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.Open(dir, theme.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	svc := New(Options{Store: store})
	t.Cleanup(svc.Close)
	return svc, store
}

// The handler's whole job: the choice reaches the store, so it is still true
// after a restart, and it reaches only the screen it was about.
func TestSetViewReachesTheStore(t *testing.T) {
	svc, store := newViewService(t)
	rec := &viewRecorder{}

	if _, err := svc.handleSetView(rec, []byte(`{"screen":"search","view":"list"}`)); err != nil {
		t.Fatal(err)
	}
	if code := rec.errorCode(t); code != "" {
		t.Fatalf("a good request was answered with %q", code)
	}
	if got := store.Settings().View(state.ScreenSearch); got != state.ViewList {
		t.Errorf("search view = %q, want %q", got, state.ViewList)
	}
	if got := store.Settings().View(state.ScreenDownloaded); got != state.ViewGrid {
		t.Errorf("setting search moved downloaded to %q", got)
	}
}

// There is no reply of its own: the value rides on the status, so the frontend
// draws what the store says rather than what it hoped.
func TestSetViewRidesOnTheStatusRatherThanReplying(t *testing.T) {
	svc, _ := newViewService(t)
	rec := &viewRecorder{}

	if _, err := svc.handleSetView(rec, []byte(`{"screen":"watching","view":"list"}`)); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	sent := len(rec.sent)
	rec.mu.Unlock()
	if sent != 0 {
		t.Errorf("handleSetView sent %d frames, want none: the value rides on the status", sent)
	}

	views := svc.Views()
	if views[state.ScreenWatching] != state.ViewList {
		t.Errorf("the status says watching is %q, want %q", views[state.ScreenWatching], state.ViewList)
	}
	if views[state.ScreenSearch] != state.ViewGrid {
		t.Errorf("the status says search is %q, want the default %q", views[state.ScreenSearch], state.ViewGrid)
	}
	if len(views) != len(state.Screens) {
		t.Errorf("the status carries %d screens, want %d", len(views), len(state.Screens))
	}
}

// A request for something that does not exist is answered, not ignored, and
// changes nothing.
func TestSetViewRefusesWhatItCannotDraw(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"unknown screen", `{"screen":"settings","view":"list"}`},
		{"unknown view", `{"screen":"search","view":"carousel"}`},
		{"no screen", `{"view":"list"}`},
		{"no view", `{"screen":"search"}`},
		{"rubbish", `not json`},
		{"empty", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newViewService(t)
			rec := &viewRecorder{}

			handled, err := svc.handleSetView(rec, []byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if !handled {
				t.Fatal("a bad request was left unhandled, so nothing answers it")
			}
			if code := rec.errorCode(t); code != "bad_request" {
				t.Errorf("code = %q, want bad_request", code)
			}
			if got := store.Settings(); got.SearchView != nil ||
				got.DownloadedView != nil || got.WatchingView != nil {
				t.Errorf("a refused request stored something: %+v", got)
			}
		})
	}
}

// The message the switch in service.go dispatches on is the one the frontend
// sends; a handler wired to the wrong number is silently dead.
func TestViewMessageIsSetView(t *testing.T) {
	if viewMessage != appload.MessageSetView {
		t.Errorf("viewMessage = %d, want MessageSetView (%d)", viewMessage, appload.MessageSetView)
	}
}
