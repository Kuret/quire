package fetch_test

// SetOffDomainHook is the record-and-offer sink's whole connection to the
// fetch layer: the callback a service installs so a fetch refused for being
// off a source's registrable domain can be noted rather than only failed.
// These tests pin the one property the design leans on — the hook fires for
// exactly fetch.GuardError.OffDomain and nothing else — because a hook that
// also fired for a private or loopback refusal would be offering to widen
// exactly the boundary the guard exists to hold, however the source that
// carried it was described.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// offDomainCall is one invocation the hook recorded.
type offDomainCall struct {
	sourceID, host string
	kind           fetch.Kind
}

// offDomainRecorder collects hook calls under a mutex — Client can call it
// from more than one request's goroutine.
type offDomainRecorder struct {
	mu    sync.Mutex
	calls []offDomainCall
}

func (r *offDomainRecorder) hook(sourceID, host string, kind fetch.Kind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, offDomainCall{sourceID, host, kind})
}

func (r *offDomainRecorder) snapshot() []offDomainCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]offDomainCall(nil), r.calls...)
}

// TestOffDomainHookFiresForARefusedOffDomainHost is the case the whole
// feature is for: a source's own fetch reaches for a CDN outside its
// registrable domain that was never declared, and the hook has to be told
// which source asked and which host it was refused.
func TestOffDomainHookFiresForARefusedOffDomainHost(t *testing.T) {
	t.Parallel()
	rec := &offDomainRecorder{}
	c := fetch.NewClient(fetch.WithStubResolver(fetch.Options{}, stubResolve(map[string][]string{
		"cdn.otherhost.invalid": {"93.184.216.40"},
	})))
	c.SetOffDomainHook(rec.hook)

	base, err := url.Parse("https://api.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	pol := &fetch.Policy{SourceID: "src1", BaseURL: base}

	_, err = c.Get(context.Background(), pol, "https://cdn.otherhost.invalid/cover.png")
	if err == nil {
		t.Fatal("an undeclared off-domain host was permitted")
	}

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("hook called %d times, want 1: %+v", len(calls), calls)
	}
	if calls[0].sourceID != "src1" {
		t.Errorf("sourceID = %q, want src1", calls[0].sourceID)
	}
	if calls[0].host != "cdn.otherhost.invalid" {
		t.Errorf("host = %q, want cdn.otherhost.invalid", calls[0].host)
	}
	if calls[0].kind != fetch.KindDiscovery {
		t.Errorf("kind = %v, want KindDiscovery (Get is a discovery request)", calls[0].kind)
	}
}

// TestOffDomainHookDoesNotFireForAPrivateAddressRefusal is the mutation this
// design names by name: a host that is refused for resolving into a private
// range, on the source's *own* registrable domain, must never reach the
// hook. checkDomain never runs the address rules and never sets OffDomain for
// this refusal — proving the hook honours that rather than firing on every
// GuardError is the whole point of this test existing.
func TestOffDomainHookDoesNotFireForAPrivateAddressRefusal(t *testing.T) {
	t.Parallel()
	rec := &offDomainRecorder{}
	c := fetch.NewClient(fetch.WithStubResolver(fetch.Options{}, stubResolve(map[string][]string{
		"api.example.invalid": {"192.168.1.10"}, // the source's own host, privately addressed
	})))
	c.SetOffDomainHook(rec.hook)

	base, err := url.Parse("https://api.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	pol := &fetch.Policy{SourceID: "src1", BaseURL: base}

	// Same host as BaseURL: checkDomain passes trivially (host == base), so
	// any refusal here can only come from checkAddress, which never sets
	// OffDomain.
	_, err = c.Get(context.Background(), pol, "https://api.example.invalid/")
	if err == nil {
		t.Fatal("a private address was permitted")
	}
	var ge *fetch.GuardError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want a *fetch.GuardError", err)
	}
	if ge.OffDomain {
		t.Fatalf("a same-domain private-address refusal was reported as OffDomain: %v", ge)
	}

	if calls := rec.snapshot(); len(calls) != 0 {
		t.Fatalf("hook fired for a private-address refusal: %+v; "+
			"offering that host would be offering to defeat the SSRF boundary", calls)
	}
}

// TestOffDomainHookFiresOnARefusedRedirectHop covers the other call site: a
// redirect target refused for being off-domain, checked deep inside
// http.Client's own CheckRedirect rather than in the initial CheckURL call in
// Client.do. Both paths have to report through the same hook, or a source
// whose off-domain CDN is only reached via a redirect would never get
// offered at all.
func TestOffDomainHookFiresOnARefusedRedirectHop(t *testing.T) {
	t.Parallel()
	rec := &offDomainRecorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://cdn.otherhost.invalid/page.png", http.StatusFound)
	}))
	defer srv.Close()

	opts := fetch.AllowLoopback(fetch.WithStubResolver(fetch.Options{}, stubResolve(map[string][]string{
		"cdn.otherhost.invalid": {"93.184.216.40"},
	})))
	opts.Version = "test"
	c := fetch.NewClient(opts)
	c.SetOffDomainHook(rec.hook)

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	pol := &fetch.Policy{SourceID: "src1", BaseURL: base}

	_, err = c.GetRetrievalFrom(context.Background(), pol, srv.URL+"/start", fetch.Referrer{})
	if err == nil {
		t.Fatal("a redirect to an undeclared off-domain host was permitted")
	}

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("hook called %d times, want 1: %+v", len(calls), calls)
	}
	if calls[0].host != "cdn.otherhost.invalid" {
		t.Errorf("host = %q, want cdn.otherhost.invalid", calls[0].host)
	}
	if calls[0].kind != fetch.KindRetrieval {
		t.Errorf("kind = %v, want KindRetrieval (GetRetrievalFrom is retrieval)", calls[0].kind)
	}
}
