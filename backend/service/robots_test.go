package service_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// switchableFetcher is the fixture fetcher plus the live switch the real client
// has, so a test can see whether the handler reached *both* halves.
type switchableFetcher struct {
	*themetest.Fetcher

	mu       sync.Mutex
	consult  bool
	setCalls int
}

func (f *switchableFetcher) SetConsultRobots(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consult = on
	f.setCalls++
}

func (f *switchableFetcher) ConsultRobots() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.consult
}

func (f *switchableFetcher) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setCalls
}

func newRobotsService(t *testing.T) (*service.Service, *state.Store, *switchableFetcher) {
	t.Helper()
	f := &switchableFetcher{Fetcher: themetest.New(t, routes())}
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    f,
		Covers:     covers.New(dir+"/covers", f),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: allowGuard{},
	})
	return svc, store, f
}

// The setting has to reach the store *and* the client. Persisting alone gives a
// toggle that does nothing until the next restart; applying alone gives one
// that forgets what it was told. Either on its own is a setting that lies.
func TestConsultRobotsReachesBothTheStoreAndTheClient(t *testing.T) {
	svc, store, f := newRobotsService(t)
	rec := &recorder{}

	// PLAN §7.4: off by default, and "never set" reads as off.
	if store.Settings().RobotsConsulted() {
		t.Fatal("robots.txt is consulted by default; PLAN §7.4 says it is not")
	}
	if svc.ConsultRobots() {
		t.Fatal("the service reports robots.txt as consulted before anything set it")
	}

	handle(t, svc, rec, appload.MessageSetConsultRobots, `{"consultRobots":true}`)

	if !store.Settings().RobotsConsulted() {
		t.Error("the setting did not reach the store, so it would be lost on restart")
	}
	if !f.ConsultRobots() {
		t.Error("the setting did not reach the client, so it would do nothing until restart")
	}
	if !svc.ConsultRobots() {
		t.Error("the service does not report the setting it just stored")
	}

	handle(t, svc, rec, appload.MessageSetConsultRobots, `{"consultRobots":false}`)

	if store.Settings().RobotsConsulted() {
		t.Error("turning it off did not reach the store")
	}
	if f.ConsultRobots() {
		t.Error("turning it off did not reach the client")
	}
	if f.calls() != 2 {
		t.Errorf("the client was switched %d times, want 2", f.calls())
	}
}

// "Off" and "never set" have to stay distinguishable in the stored file, so a
// future change of default cannot silently rewrite what an existing file meant.
func TestConsultRobotsOffIsStoredExplicitly(t *testing.T) {
	svc, store, _ := newRobotsService(t)
	rec := &recorder{}

	if store.Settings().ConsultRobots != nil {
		t.Fatal("a fresh store already has an opinion about robots.txt")
	}
	handle(t, svc, rec, appload.MessageSetConsultRobots, `{"consultRobots":false}`)

	got := store.Settings().ConsultRobots
	if got == nil {
		t.Fatal("setting it to off left the store with no opinion, which is not the same thing")
	}
	if *got {
		t.Fatal("setting it to off stored on")
	}
}

// A malformed payload is answered, not ignored, and changes nothing.
func TestConsultRobotsRejectsRubbish(t *testing.T) {
	svc, store, f := newRobotsService(t)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageSetConsultRobots, `not json`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "bad_request" {
		t.Errorf("code = %q, want bad_request", e.Code)
	}
	if store.Settings().ConsultRobots != nil || f.calls() != 0 {
		t.Error("a rubbish payload changed the setting")
	}
}
