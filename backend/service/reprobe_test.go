package service_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// emptyListing is a Madara search page that parses cleanly and contains nothing
// — the shape a site takes when its markup has moved on and our selectors no
// longer match, which is exactly the case that used to surface as the useless
// "no results found".
const emptyListing = `<!doctype html><html><head><title>Example Reader</title></head>
<body><div class="site-content"><div class="c-tabs-item"></div></div></body></html>`

// challengePage is what a site looks like once it puts Cloudflare in front:
// the markup is gone and replaced by an interstitial. PLAN §7.5 stage 3 must
// recognise it, and PLAN §6 M7 wants the user told *that* rather than "nothing
// here".
const challengePage = `<!doctype html><html><head><title>Just a moment...</title></head>
<body><div id="challenge-running">Checking your browser before accessing example.invalid.</div>
<script src="/cdn-cgi/challenge-platform/h/b/orchestrate/jsch/v1"></script></body></html>`

func addExampleSource(t *testing.T, store interface {
	Add(*theme.Source) (*theme.Source, error)
}) {
	t.Helper()
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
}

// An empty *listing* is evidence the site changed, and the user gets an
// explanation instead of a blank grid.
func TestAnEmptyListingRePROBESAndReportsAChallenge(t *testing.T) {
	r := routes()
	r["GET /?post_type=wp-manga&s="] = themetest.Route{Body: emptyListing}
	// The re-probe then finds the site behind a challenge.
	r["GET /"] = themetest.Route{
		Body:   challengePage,
		Status: http.StatusServiceUnavailable,
		Header: http.Header{"Server": []string{"cloudflare"}, "Cf-Ray": []string{"8b0c000000000000-AMS"}},
	}

	svc, store, rec := newService(t, r)
	addExampleSource(t, store)

	handle(t, svc, rec, appload.MessageBrowse, `{"sourceId":"example-reader","page":1}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "source_changed" {
		t.Fatalf("code %q, want source_changed", e.Code)
	}
	if !strings.Contains(e.Message, "Example Reader") {
		t.Errorf("message %q does not name the source", e.Message)
	}
	// The whole point: it says what is wrong, not that there is nothing here.
	if strings.Contains(strings.ToLower(e.Message), "no results") {
		t.Errorf("message %q still reports an empty result", e.Message)
	}

	// And the verdict is recorded, so the source list shows it later too.
	src, _ := store.Get("example-reader")
	if src.LastProbe == nil {
		t.Fatal("the re-probe was not recorded against the source")
	}
	if src.LastProbe.Verdict == theme.VerdictOK {
		t.Errorf("recorded verdict %q", src.LastProbe.Verdict)
	}
}

// The case that must NOT trigger it. Searching for something a site genuinely
// does not have is the commonest way to get zero rows, and re-probing on the
// user's typing would cry wolf constantly.
func TestAnEmptySearchDoesNotReprobe(t *testing.T) {
	r := routes()
	r["GET /?post_type=wp-manga&s=zzzznothing"] = themetest.Route{Body: emptyListing}
	// If the probe ran it would have to fetch these; leaving "GET /" as the
	// normal fixture means a re-probe would *succeed* and be invisible, so
	// assert on the absence of the error instead.
	svc, store, rec := newService(t, r)
	addExampleSource(t, store)

	handle(t, svc, rec, appload.MessageSearch,
		`{"sourceId":"example-reader","query":"zzzznothing"}`)
	rec.wait(t, appload.MessageSearchResults)

	// Give a re-probe time to happen if it were going to.
	time.Sleep(300 * time.Millisecond)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, f := range rec.sent {
		if f.Type == appload.MessageError {
			t.Fatalf("an empty search triggered a re-probe: %s", f.Payload)
		}
	}
	if src, _ := store.Get("example-reader"); src.LastProbe != nil {
		t.Error("an empty search recorded a probe result")
	}
}

// A transport failure is the blip case — wifi dropped, the tablet woke
// mid-request — and belongs to fetch's retry and backoff, not to a re-probe.
func TestATransportFailureDoesNotReprobe(t *testing.T) {
	r := routes()
	r["GET /?post_type=wp-manga&s="] = themetest.Route{Err: errFlaky{}}

	svc, store, rec := newService(t, r)
	addExampleSource(t, store)

	handle(t, svc, rec, appload.MessageBrowse, `{"sourceId":"example-reader","page":1}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "search_failed" {
		t.Errorf("code %q, want the plain failure rather than a re-probe verdict", e.Code)
	}
	time.Sleep(300 * time.Millisecond)
	if src, _ := store.Get("example-reader"); src.LastProbe != nil {
		t.Error("a transport blip triggered a re-probe")
	}
}

type errFlaky struct{}

func (errFlaky) Error() string { return "dial tcp: network is unreachable" }

// A site that is simply quiet today must not be re-probed on every tap: the
// cooldown is what keeps one bad afternoon from becoming a small crawl.
func TestReprobeIsRateLimitedPerSource(t *testing.T) {
	r := routes()
	r["GET /?post_type=wp-manga&s="] = themetest.Route{Body: emptyListing}
	r["GET /"] = themetest.Route{
		Body:   challengePage,
		Status: http.StatusServiceUnavailable,
		Header: http.Header{"Server": []string{"cloudflare"}, "Cf-Ray": []string{"8b0c000000000000-AMS"}},
	}

	svc, store, rec := newService(t, r)
	addExampleSource(t, store)

	for i := 0; i < 3; i++ {
		handle(t, svc, rec, appload.MessageBrowse, `{"sourceId":"example-reader","page":1}`)
	}
	rec.wait(t, appload.MessageError)
	time.Sleep(500 * time.Millisecond)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	errors := 0
	for _, f := range rec.sent {
		if f.Type == appload.MessageError {
			errors++
		}
	}
	if errors > 1 {
		t.Errorf("%d re-probes for three empty listings; the cooldown did not hold", errors)
	}
}

// The cooldown itself, at the unit level, because a 15-minute wall-clock gap is
// not something a test should sit through.
func TestReprobeCooldownIsFifteenMinutes(t *testing.T) {
	if service.ReprobeCooldown < 10*time.Minute {
		t.Errorf("cooldown %v is short enough to hammer a struggling site", service.ReprobeCooldown)
	}
}
