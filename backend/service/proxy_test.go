package service_test

// Editing a source's proxy after it was added — PLAN §7.1 type 76. Before
// this, a wrong proxy meant deleting the source and adding it again; these
// tests are the two edited paths (setting/changing a proxy, and clearing one)
// and the one they close together: clearing a proxy a self-hosted
// confirmation stands on must take the confirmation with it, in the same
// operation the store applies, without disturbing a confirmation that stands
// on an address instead.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

// waitLast is rec.wait but for the *last* frame of a type sent so far, so a
// test that sends several messages of the same type in a row (setting a
// proxy, then changing it, then clearing it) can see each reply rather than
// always the first. setSourceProxy answers synchronously, so the reply is
// already in rec.sent by the time handle returns; wait is only the fallback
// for the (unused here) case where it is not yet.
func waitLast(t *testing.T, rec *recorder, msgType int32) []byte {
	t.Helper()
	rec.wait(t, msgType) // fails loudly if none has arrived at all; also gives async replies time to land
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var last []byte
	for _, f := range rec.sent {
		if f.Type == msgType {
			last = f.Payload
		}
	}
	return last
}

func addProxySource(t *testing.T, store interface {
	Add(*theme.Source) (*theme.Source, error)
}, baseURL string) {
	t.Helper()
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: baseURL, AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
}

// sourcesReply is the shape TestListSources and friends decode; declared once
// here for the proxy fields this file cares about.
type proxySourcesReply struct {
	Sources []struct {
		ID                 string `json:"id"`
		Proxy              string `json:"proxy"`
		SelfHostedViaProxy bool   `json:"selfHostedViaProxy"`
	} `json:"sources"`
}

func decodeSourcesReply(t *testing.T, b []byte) proxySourcesReply {
	t.Helper()
	var reply proxySourcesReply
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

// TestSetSourceProxySetsChangesAndClears is the round trip the UI's Proxy
// action makes: a proxy can be set on a source that had none, changed, and
// cleared, and each step is both echoed in the reply and on the stored source.
func TestSetSourceProxySetsChangesAndClears(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":"http://localhost:1055"}`)
	reply := decodeSourcesReply(t, waitLast(t, rec, appload.MessageSources))
	if len(reply.Sources) != 1 || reply.Sources[0].Proxy != "http://localhost:1055" {
		t.Fatalf("after setting: %+v", reply.Sources)
	}
	if got, _ := store.Get("example-reader"); got.Proxy != "http://localhost:1055" {
		t.Errorf("stored proxy = %q", got.Proxy)
	}

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":"socks5://localhost:1080"}`)
	reply = decodeSourcesReply(t, waitLast(t, rec, appload.MessageSources))
	if len(reply.Sources) != 1 || reply.Sources[0].Proxy != "socks5://localhost:1080" {
		t.Fatalf("after changing: %+v", reply.Sources)
	}

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":""}`)
	reply = decodeSourcesReply(t, waitLast(t, rec, appload.MessageSources))
	if len(reply.Sources) != 1 || reply.Sources[0].Proxy != "" {
		t.Fatalf("after clearing: %+v", reply.Sources)
	}
	if got, _ := store.Get("example-reader"); got.Proxy != "" {
		t.Errorf("stored proxy after clearing = %q, want empty", got.Proxy)
	}
}

// TestSetSourceProxyRefusesAnInvalidOneInWords keeps a bad proxy from ever
// being stored, and answers in a sentence the user can act on, the way an
// invalid splitStrips value does.
func TestSetSourceProxyRefusesAnInvalidOneInWords(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":"ftp://localhost:21"}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "bad_proxy" {
		t.Errorf("code = %q, want bad_proxy", e.Code)
	}
	if !strings.Contains(e.Message, "proxy") {
		t.Errorf("message %q does not say what is wrong", e.Message)
	}
	if got, _ := store.Get("example-reader"); got.Proxy != "" {
		t.Errorf("a refused proxy was stored anyway: %q", got.Proxy)
	}
}

// TestSetSourceProxyClearingAViaProxyConfirmationRevokesIt is the gap this
// feature closes: RevokeSelfHosted had no caller anywhere in the app before
// this, and clearing the proxy underneath a ViaProxy confirmation is the one
// honest place to call it, because without the proxy the confirmation claims a
// route that no longer exists.
func TestSetSourceProxyClearingAViaProxyConfirmationRevokesIt(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":"http://localhost:1055"}`)
	rec.wait(t, appload.MessageSources)

	if err := store.ConfirmSelfHostedViaProxy("example-reader"); err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":""}`)
	reply := decodeSourcesReply(t, waitLast(t, rec, appload.MessageSources))
	if len(reply.Sources) != 1 {
		t.Fatalf("sources = %+v", reply.Sources)
	}
	if reply.Sources[0].Proxy != "" {
		t.Errorf("proxy = %q, want cleared", reply.Sources[0].Proxy)
	}
	if reply.Sources[0].SelfHostedViaProxy {
		t.Errorf("selfHostedViaProxy is still true after the proxy was cleared")
	}
	got, _ := store.Get("example-reader")
	if got.SelfHosted != nil {
		t.Errorf("SelfHosted = %+v, want revoked", got.SelfHosted)
	}
}

// TestSetSourceProxyClearingLeavesAnAddressConfirmationAlone is the other
// half: a confirmation given by resolved address does not depend on the proxy,
// so clearing the proxy must not touch it.
func TestSetSourceProxyClearingLeavesAnAddressConfirmationAlone(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "http://example.invalid:8084")

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":"http://localhost:1055"}`)
	rec.wait(t, appload.MessageSources)

	if err := store.ConfirmSelfHosted("example-reader", "100.100.0.1"); err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"example-reader","proxy":""}`)
	reply := decodeSourcesReply(t, waitLast(t, rec, appload.MessageSources))
	if len(reply.Sources) != 1 || reply.Sources[0].Proxy != "" {
		t.Fatalf("sources = %+v", reply.Sources)
	}
	if reply.Sources[0].SelfHostedViaProxy {
		t.Errorf("selfHostedViaProxy reported true for an address confirmation")
	}
	got, _ := store.Get("example-reader")
	if got.SelfHosted == nil || got.SelfHosted.ConfirmedAddr != "100.100.0.1" {
		t.Fatalf("an address confirmation was disturbed by clearing the proxy: %+v", got.SelfHosted)
	}
}

// TestSetSourceProxyOnAnUnknownSourceErrorsCleanly answers plainly rather than
// leaving the UI waiting for a MessageSources that will never come.
func TestSetSourceProxyOnAnUnknownSourceErrorsCleanly(t *testing.T) {
	svc, _, rec := newService(t, routes())

	handle(t, svc, rec, appload.MessageSetSourceProxy,
		`{"sourceId":"nope","proxy":"http://localhost:1055"}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "not_found" {
		t.Errorf("code = %q, want not_found", e.Code)
	}
}
