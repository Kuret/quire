//go:build quiretest

// This file exists only under the `quiretest` build tag. Nothing in the
// Makefile's production targets passes it, so the symbol below is not merely
// unreachable in a shipped binary — it is not compiled into one at all.
//
// Why a build tag rather than the usual export_test.go trick: the SSRF guard's
// loopback exemption is already reachable from fetch's own tests via
// AllowLoopback in export_test.go, and that is enough for tests *inside* this
// package. The fixture-server integration test (PLAN §7.4, backend/fixtures)
// runs the real client against a real socket on 127.0.0.1 from a *different*
// package, and an unexported field cannot be reached from there.
//
// The three properties that make this safe, in the order they matter:
//
//  1. **No configuration surface.** Options.allowLoopback is unexported and
//     has no JSON tag, no schema entry and no flag. A source entry, an
//     imported sources file or a goja script cannot set it however it is
//     built. That was true before this file and is unchanged by it.
//  2. **Not in the binary.** Without -tags quiretest the function does not
//     exist, so there is no symbol for a future caller to find by grep or by
//     autocomplete, and no code path to audit.
//  3. **Loud if misused.** The name says what it is, and the only callers are
//     in _test.go files that are themselves tagged.
//
// `make test` passes the tag so these tests run in the normal check; a plain
// `go test ./...` compiles the production shape instead, which is a useful
// second thing to be able to do.

package fetch

// AllowLoopbackForTests returns o with the SSRF guard's loopback rejection
// lifted, so a test can point a Client at an httptest server on 127.0.0.1.
//
// It relaxes *only* the loopback check. Private and link-local ranges, the
// scheme check, the credentials check and the registrable-domain boundary on
// redirects are all untouched, and every other §7.4 invariant applies exactly
// as it does in production.
func AllowLoopbackForTests(o Options) Options {
	o.allowLoopback = true
	return o
}
