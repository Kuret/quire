package fetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// windows1251Privet is "Привет" ("hello") encoded as windows-1251. Real bytes
// in a real non-UTF-8 encoding, not a hand-rolled stand-in — the fixture the
// task calls for.
var windows1251Privet = []byte{0xCF, 0xF0, 0xE8, 0xE2, 0xE5, 0xF2}

// utf8Privet is the same word, correctly encoded as UTF-8, for comparing the
// decoded result against.
const utf8Privet = "Привет"

// TestDecodeCharsetFromContentTypeHeader is the highest-precedence source:
// the HTTP header wins even when the page carries no meta tag of its own.
//
// Mutation (b) from the task — skip decoding entirely — fails this test:
// windows1251Privet is not valid UTF-8, so an undecoded body would not equal
// utf8Privet.
func TestDecodeCharsetFromContentTypeHeader(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		w.Write([]byte("<html><body><p>"))
		w.Write(windows1251Privet)
		w.Write([]byte("</p></body></html>"))
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Body), utf8Privet) {
		t.Fatalf("body = %q, want it to contain %q", resp.Body, utf8Privet)
	}
}

// TestDecodeCharsetFromMetaTagWhenHeaderIsSilent covers the second source of
// truth: no header charset parameter at all, so the decoder must fall back to
// the document's own <meta charset>.
func TestDecodeCharsetFromMetaTagWhenHeaderIsSilent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately no charset parameter here.
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><meta charset="windows-1251"></head><body><p>`))
		w.Write(windows1251Privet)
		w.Write([]byte("</p></body></html>"))
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Body), utf8Privet) {
		t.Fatalf("body = %q, want it to contain %q", resp.Body, utf8Privet)
	}
}

// TestHeaderCharsetOutranksConflictingMetaTag pins the precedence order
// itself, not just that decoding happens at all.
//
// Mutation (a) from the task — ignore the HTTP header and trust only the meta
// tag — fails this test: the header says windows-1251 and is correct, the
// meta tag says utf-8 and is a lie, and a decoder that read only the meta tag
// would decode windows1251Privet's raw bytes as (invalid) UTF-8, producing
// replacement characters instead of "Привет".
func TestHeaderCharsetOutranksConflictingMetaTag(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		w.Write([]byte(`<html><head><meta charset="utf-8"></head><body><p>`))
		w.Write(windows1251Privet)
		w.Write([]byte("</p></body></html>"))
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Body), utf8Privet) {
		t.Fatalf("body = %q, want the header's windows-1251 to win and contain %q", resp.Body, utf8Privet)
	}
}

// TestUTF8BodyWithExplicitCharsetIsByteIdentical is the common case the task
// says must not regress: a page that is already UTF-8 and says so.
func TestUTF8BodyWithExplicitCharsetIsByteIdentical(t *testing.T) {
	t.Parallel()
	const html = `<html><head><meta charset="utf-8"></head><body><p>Héllo Wörld</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != html {
		t.Fatalf("body = %q, want byte-identical %q", resp.Body, html)
	}
}

// TestUTF8BodyWithNoDeclarationIsByteIdentical is the other half of the
// common case: a page that declares nothing but is valid UTF-8. This must be
// sniffed as UTF-8 and left untouched, not defaulted to windows-1252.
func TestUTF8BodyWithNoDeclarationIsByteIdentical(t *testing.T) {
	t.Parallel()
	const html = `<html><body><p>Héllo Wörld</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // no charset parameter
		w.Write([]byte(html))
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != html {
		t.Fatalf("body = %q, want byte-identical %q", resp.Body, html)
	}
}

// TestBinaryBodyIsNeverCharsetDecoded proves the gate: a response declared as
// binary must pass through untouched even though its bytes are full of
// values a naive charset guess could mangle. Without the Content-Type gate,
// DetermineEncoding's windows-1252 fallback would rewrite these bytes.
func TestBinaryBodyIsNeverCharsetDecoded(t *testing.T) {
	t.Parallel()
	// High-bit bytes that are not valid UTF-8 and are not a real image, only
	// declared as one.
	binary := []byte{0x89, 0xFF, 0xFE, 0x00, 0x01, 0xC0, 0xA0, 0x00}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(binary)
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != string(binary) {
		t.Fatalf("body = %v, want untouched %v", resp.Body, binary)
	}
}

// TestInvalidBytesDoNotPanicOrTruncate covers a page that lies about its
// encoding: declared UTF-8, but containing a byte that is not valid UTF-8 on
// its own. Decoding must not panic and must not drop the rest of the
// document — only the bad byte should come out looking wrong, as U+FFFD.
func TestInvalidBytesDoNotPanicOrTruncate(t *testing.T) {
	t.Parallel()
	before := "<html><body><p>before-"
	after := "-after</p></body></html>"
	var body []byte
	body = append(body, before...)
	body = append(body, 0xFF) // not valid UTF-8 anywhere
	body = append(body, after...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(body)
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	resp, err := c.Get(context.Background(), p, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	got := string(resp.Body)
	if !strings.Contains(got, before) || !strings.Contains(got, after) {
		t.Fatalf("body = %q, want both %q and %q preserved around the bad byte", got, before, after)
	}
	if !strings.Contains(got, "�") {
		t.Fatalf("body = %q, want the invalid byte replaced with U+FFFD", got)
	}
}
