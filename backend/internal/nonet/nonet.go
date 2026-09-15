// Package nonet makes "this test touched the live network" a test failure
// rather than a slow test and a flaky CI.
//
// PLAN §6 M2 is explicit: fixtures are recorded once, committed, and tested
// offline forever — "never hit the live network in unit tests". That is easy
// to honour by accident today and easy to break by accident tomorrow, when
// someone adds a theme and reaches for a real URL "just to check". This turns
// that into a red test on the spot.
//
// It works by replacing net.DefaultResolver's dial function, which is the
// chokepoint every hostname lookup in the standard library goes through.
// Requests to an httptest server are unaffected: those use a literal
// 127.0.0.1, which needs no resolver.
package nonet

import (
	"context"
	"fmt"
	"net"
	"testing"
)

// Forbid installs a resolver that fails instead of resolving, and restores the
// previous one when the test (or TestMain, via t.Cleanup on a fake T) is done.
//
// Call it from TestMain so it covers every test in the package:
//
//	func TestMain(m *testing.M) { nonet.ForbidMain(); os.Exit(m.Run()) }
func Forbid(t *testing.T) {
	t.Helper()
	restore := install(func(host string) error {
		t.Errorf("test attempted a DNS lookup for %q; unit tests must run offline (PLAN §6 M2)", host)
		return fmt.Errorf("nonet: DNS lookup for %q refused", host)
	})
	t.Cleanup(restore)
}

// ForbidMain is the TestMain form. It cannot fail a specific test, so it
// returns an error to the caller instead, which surfaces as a lookup failure
// with an unmistakable message.
func ForbidMain() {
	install(func(host string) error {
		return fmt.Errorf("nonet: DNS lookup for %q refused; unit tests must run offline (PLAN §6 M2)", host)
	})
}

func install(onLookup func(host string) error) (restore func()) {
	prevDial := net.DefaultResolver.Dial
	prevPreferGo := net.DefaultResolver.PreferGo

	net.DefaultResolver.PreferGo = true
	net.DefaultResolver.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, onLookup(address)
	}
	return func() {
		net.DefaultResolver.Dial = prevDial
		net.DefaultResolver.PreferGo = prevPreferGo
	}
}
