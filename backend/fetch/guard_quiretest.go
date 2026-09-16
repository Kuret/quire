//go:build quiretest

// This file exists only under the `quiretest` build tag, for the same reasons
// and on the same terms as loopback_quiretest.go — read that file first; this
// one only says what is different.
//
// What is different: the SSRF guard is testable from inside this package via
// export_test.go's NewGuard, but PLAN §7.5 stage 1 hands a URL to the guard
// from *another* package, and that hand-off is the thing worth testing. Before
// this file, backend/probe/prober had no way to reach a real Guard, so its
// tests used a hand-written stand-in that answered the way the guard was
// believed to answer. That is a second source of truth about a security
// boundary, and a second source of truth can agree with itself forever while
// the first one changes underneath it.
//
// The three properties that make this safe are unchanged:
//
//  1. **No configuration surface.** Guard.resolve is unexported, has no JSON
//     tag, no schema entry and no flag. Nothing a user writes — a source entry,
//     an imported sources file, a goja script — can reach it.
//  2. **Not in the binary.** Without -tags quiretest this function does not
//     exist. There is no symbol to find and no path to audit.
//  3. **Loud if misused.** The name says what it is, and its only callers are
//     in _test.go files that carry the tag themselves.
//
// Note what it does *not* relax: this Guard applies every §7.4 rule exactly as
// production does — scheme, credentials, blocked address ranges, the
// registrable-domain boundary. Only the source of DNS answers is substituted,
// so a test can refuse "localhost" without a resolver rather than asserting
// against a model of one.

package fetch

import (
	"context"
	"net"
)

// NewGuardForTests builds a real Guard whose DNS answers come from resolve, so
// a test in another package can exercise the production guard without a
// network.
func NewGuardForTests(resolve func(ctx context.Context, host string) ([]net.IP, error)) *Guard {
	return &Guard{resolve: resolve}
}
