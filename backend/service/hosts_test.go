package service_test

// MessageAllowSourceHost and MessageRevokeSourceHost: the allowedHosts editor
// the owner acts through from the source list. Allowing widens exactly one
// source's allowedHosts by exactly one host and clears it from that source's
// pending list; revoking narrows it back. Neither ever reaches a source that
// did not ask for it.

import (
	"encoding/json"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

// TestAllowSourceHostGrantsAndClearsPending is the round trip the Allow
// button drives: a pending host, once allowed, moves from pendingHosts to
// allowedHosts on the same row.
func TestAllowSourceHostGrantsAndClearsPending(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")
	svc.RecordOffDomainHost("example-reader", "cdn.example.invalid", fetch.KindDiscovery)

	handle(t, svc, rec, appload.MessageAllowSourceHost,
		`{"sourceId":"example-reader","host":"cdn.example.invalid"}`)

	reply := decodeHostsReply(t, waitLast(t, rec, appload.MessageSources))
	if len(reply.Sources) != 1 {
		t.Fatalf("sources = %+v", reply.Sources)
	}
	if got := reply.Sources[0].AllowedHosts; len(got) != 1 || got[0] != "cdn.example.invalid" {
		t.Fatalf("allowedHosts = %+v, want [cdn.example.invalid]", got)
	}
	if got := reply.Sources[0].PendingHosts; len(got) != 0 {
		t.Fatalf("pendingHosts after allowing = %+v, want empty", got)
	}

	// And it is really on the store, not only in the reply.
	stored, ok := store.Get("example-reader")
	if !ok || len(stored.AllowedHosts) != 1 || stored.AllowedHosts[0] != "cdn.example.invalid" {
		t.Fatalf("stored AllowedHosts = %+v", stored)
	}
}

// TestAllowSourceHostNeverGrantsToTheWrongSource is mutation (b): allowing a
// host for one source must never widen another's guard, however the two are
// listed side by side in the same message.
func TestAllowSourceHostNeverGrantsToTheWrongSource(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")
	second, err := store.Add(&theme.Source{
		Name: "Another", Lang: "en", Theme: madara.ID,
		BaseURL: "https://another.invalid", AddedAt: fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageAllowSourceHost,
		`{"sourceId":"example-reader","host":"cdn.example.invalid"}`)
	rec.wait(t, appload.MessageSources)

	other, ok := store.Get(second.ID)
	if !ok {
		t.Fatal("second source not found")
	}
	if len(other.AllowedHosts) != 0 {
		t.Fatalf("a host allowed for example-reader reached another's AllowedHosts: %+v", other.AllowedHosts)
	}
}

// TestAllowSourceHostOnAnUnknownSourceErrorsCleanly matches
// TestSetSourceProxyOnAnUnknownSourceErrorsCleanly's shape.
func TestAllowSourceHostOnAnUnknownSourceErrorsCleanly(t *testing.T) {
	svc, _, rec := newService(t, routes())

	handle(t, svc, rec, appload.MessageAllowSourceHost,
		`{"sourceId":"nope","host":"cdn.example.invalid"}`)

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

// TestRevokeSourceHostRemovesAGrantedHost is the editor's other half: a
// mistaken or no-longer-wanted grant can be taken back, which did not exist
// before this feature at all — allowedHosts could only be seeded at add time.
func TestRevokeSourceHostRemovesAGrantedHost(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")
	if err := store.AllowHost("example-reader", "cdn.example.invalid"); err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageRevokeSourceHost,
		`{"sourceId":"example-reader","host":"cdn.example.invalid"}`)

	reply := decodeHostsReply(t, waitLast(t, rec, appload.MessageSources))
	if got := reply.Sources[0].AllowedHosts; len(got) != 0 {
		t.Fatalf("allowedHosts after revoke = %+v, want empty", got)
	}
	stored, _ := store.Get("example-reader")
	if len(stored.AllowedHosts) != 0 {
		t.Fatalf("stored AllowedHosts after revoke = %+v", stored.AllowedHosts)
	}
}

func TestRevokeSourceHostOnAnUnknownSourceErrorsCleanly(t *testing.T) {
	svc, _, rec := newService(t, routes())

	handle(t, svc, rec, appload.MessageRevokeSourceHost,
		`{"sourceId":"nope","host":"cdn.example.invalid"}`)

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
