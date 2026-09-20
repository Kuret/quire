package fetch

import (
	"net/http"
	"testing"
	"time"
)

// The half of the slow path that cannot be seen from outside: the transport.
//
// http.Client.Timeout bounds the *whole* request, and raising it alone would
// have left this change as a comment — Transport.ResponseHeaderTimeout is 30
// seconds and it measures exactly the wait that matters, the one before the
// first byte. shelfmark's release search sends nothing for 36 seconds
// (2026-09-20) because the server is searching, so it died there.
func TestTheSlowTransportWaitsForAServerThatIsWorking(t *testing.T) {
	c := NewClient(Options{Version: "test"})

	ordinary, ok := c.hc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the ordinary transport is %T, not one this test can read", c.hc.Transport)
	}
	slow, ok := c.slowHC.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the slow transport is %T, not one this test can read", c.slowHC.Transport)
	}
	if ordinary == slow {
		t.Fatal("both clients share one transport, so the slow one carries the 30s header timeout")
	}

	if c.hc.Timeout != DefaultRequestTimeout {
		t.Errorf("the ordinary client's timeout is %v, want %v", c.hc.Timeout, DefaultRequestTimeout)
	}
	if ordinary.ResponseHeaderTimeout != defaultResponseHeaderTimeout {
		t.Errorf("the ordinary header timeout is %v, want %v",
			ordinary.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	}

	// The number that decides whether a real instance works: its own
	// release_search_timeout is 300 seconds, so anything at or under that fires
	// while the far end is still legitimately searching.
	const instanceSearchBudget = 300 * time.Second
	if c.slowHC.Timeout <= instanceSearchBudget {
		t.Errorf("the slow client's timeout is %v, which fires inside the instance's own %v search budget",
			c.slowHC.Timeout, instanceSearchBudget)
	}
	if slow.ResponseHeaderTimeout <= instanceSearchBudget {
		t.Errorf("the slow transport waits %v for a first byte, which fires inside the instance's own %v search budget",
			slow.ResponseHeaderTimeout, instanceSearchBudget)
	}

	// The pool settings are the ordinary ones. A slow request is not a licence
	// to hold more sockets open on a 2 GB device.
	if slow.MaxIdleConnsPerHost != ordinary.MaxIdleConnsPerHost ||
		slow.MaxConnsPerHost != ordinary.MaxConnsPerHost {
		t.Error("the slow transport has different connection caps from the ordinary one")
	}
}

// An injected transport (tests, and nothing in production) is used by both
// clients, so a test double does not silently lose the slow path.
func TestAnInjectedTransportServesBothClients(t *testing.T) {
	rt := http.DefaultTransport
	c := NewClient(Options{Version: "test", Transport: rt})
	if c.hc.Transport != rt || c.slowHC.Transport != rt {
		t.Error("an injected transport did not reach both clients")
	}
}
