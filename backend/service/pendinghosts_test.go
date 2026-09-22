package service_test

// The allowedHosts editor's service half: recording a pending host
// (Service.RecordOffDomainHost, the sink fetch.Client.SetOffDomainHook is
// given in production — see main.go), offering it on the source list
// (sendSources' pendingHosts field) and the two messages that act on it,
// MessageAllowSourceHost and MessageRevokeSourceHost.

import (
	"encoding/json"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

type hostsSourcesReply struct {
	Sources []struct {
		ID           string   `json:"id"`
		AllowedHosts []string `json:"allowedHosts"`
		PendingHosts []struct {
			Host    string `json:"host"`
			Purpose string `json:"purpose"`
		} `json:"pendingHosts"`
	} `json:"sources"`
}

func decodeHostsReply(t *testing.T, b []byte) hostsSourcesReply {
	t.Helper()
	var reply hostsSourcesReply
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

// TestRecordOffDomainHostOffersItOnTheSourceList is the round trip: a fetch
// refused for being off-domain shows up, unprompted, the next time the
// source list is sent — never as an interruption, exactly PLAN's
// record-and-offer design.
func TestRecordOffDomainHostOffersItOnTheSourceList(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	svc.RecordOffDomainHost("example-reader", "CDN.Example.Invalid", fetch.KindDiscovery)

	handle(t, svc, rec, appload.MessageListSources, `{}`)
	reply := decodeHostsReply(t, rec.wait(t, appload.MessageSources))
	if len(reply.Sources) != 1 {
		t.Fatalf("sources = %+v", reply.Sources)
	}
	pending := reply.Sources[0].PendingHosts
	if len(pending) != 1 {
		t.Fatalf("pendingHosts = %+v, want one entry", pending)
	}
	// Lower-cased, matching how AllowHost stores a granted one.
	if pending[0].Host != "cdn.example.invalid" {
		t.Errorf("host = %q, want lower-cased", pending[0].Host)
	}
	if pending[0].Purpose != "cover" {
		t.Errorf("purpose = %q, want cover (KindDiscovery)", pending[0].Purpose)
	}
}

// TestRecordOffDomainHostRetrievalIsAPage: the download queue's page fetch is
// KindRetrieval, and the owner reviewing the list needs to be able to tell
// "a page" from "a cover" — a bare hostname beside an Allow button is exactly
// the reflexive yes this design exists to avoid.
func TestRecordOffDomainHostRetrievalIsAPage(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	svc.RecordOffDomainHost("example-reader", "pages.example.invalid", fetch.KindRetrieval)

	handle(t, svc, rec, appload.MessageListSources, `{}`)
	reply := decodeHostsReply(t, rec.wait(t, appload.MessageSources))
	if len(reply.Sources[0].PendingHosts) != 1 || reply.Sources[0].PendingHosts[0].Purpose != "page" {
		t.Fatalf("pendingHosts = %+v, want purpose page", reply.Sources[0].PendingHosts)
	}
}

// TestRecordOffDomainHostReachesOnlyTheNamedSource: two sources, one refusal
// against one of them, and the other's row must stay empty. This is the
// mutation named directly in the design: recording against the wrong source
// would offer a host to a source that never asked for it.
func TestRecordOffDomainHostReachesOnlyTheNamedSource(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")
	if _, err := store.Add(&theme.Source{
		Name: "Another", Lang: "en", Theme: madara.ID,
		BaseURL: "https://other.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	svc.RecordOffDomainHost("example-reader", "cdn.example.invalid", fetch.KindDiscovery)

	handle(t, svc, rec, appload.MessageListSources, `{}`)
	reply := decodeHostsReply(t, rec.wait(t, appload.MessageSources))
	for _, src := range reply.Sources {
		switch src.ID {
		case "example-reader":
			if len(src.PendingHosts) != 1 {
				t.Errorf("example-reader pendingHosts = %+v, want one", src.PendingHosts)
			}
		default:
			if len(src.PendingHosts) != 0 {
				t.Errorf("%s, which never asked for it, has pendingHosts = %+v", src.ID, src.PendingHosts)
			}
		}
	}
}

// TestPendingHostCapDiscardsBeyondTheBound is mutation (c): a source spraying
// more distinct off-domain hosts than the cap allows must not grow its
// pending list past the bound, and the hosts already pending must survive —
// see pendingHosts.record's discard rule.
func TestPendingHostCapDiscardsBeyondTheBound(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	const tryCount = 20 // comfortably past pendingHostCap (8)
	for i := 0; i < tryCount; i++ {
		svc.RecordOffDomainHost("example-reader", hostN(i), fetch.KindDiscovery)
	}

	handle(t, svc, rec, appload.MessageListSources, `{}`)
	reply := decodeHostsReply(t, rec.wait(t, appload.MessageSources))
	pending := reply.Sources[0].PendingHosts
	if len(pending) != 8 {
		t.Fatalf("pendingHosts has %d entries, want the cap (8): %+v", len(pending), pending)
	}
	// The discard rule keeps what arrived first rather than the most recent.
	if pending[0].Host != hostN(0) {
		t.Errorf("first pending host = %q, want %q (earliest survives)", pending[0].Host, hostN(0))
	}
}

// TestPendingHostSameHostDoesNotConsumeCapSlots: a source that keeps failing
// on the *same* host is not spraying distinct names, and re-recording it must
// not silently vanish once seven other distinct hosts already filled the
// list around it.
func TestPendingHostSameHostDoesNotConsumeCapSlots(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addProxySource(t, store, "https://example.invalid")

	svc.RecordOffDomainHost("example-reader", "cdn.example.invalid", fetch.KindDiscovery)
	svc.RecordOffDomainHost("example-reader", "cdn.example.invalid", fetch.KindDiscovery)
	svc.RecordOffDomainHost("example-reader", "cdn.example.invalid", fetch.KindDiscovery)

	handle(t, svc, rec, appload.MessageListSources, `{}`)
	reply := decodeHostsReply(t, rec.wait(t, appload.MessageSources))
	if len(reply.Sources[0].PendingHosts) != 1 {
		t.Fatalf("pendingHosts = %+v, want exactly one entry for a repeated host",
			reply.Sources[0].PendingHosts)
	}
}

func hostN(i int) string {
	return string(rune('a'+i)) + ".example.invalid"
}
