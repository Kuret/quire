package fetch

import (
	"mime"
	"net/http"
	"strings"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
)

// decodeToUTF8 converts an HTML response body to UTF-8, honouring — in this
// order — the HTTP Content-Type header's charset parameter, the document's
// own <meta charset> / <meta http-equiv> declaration, and a sniffed fallback.
// golang.org/x/net/html/charset.DetermineEncoding already implements exactly
// that precedence (WHATWG's "determining the character encoding" algorithm),
// so this wraps it rather than re-implementing it.
//
// This is the one place it runs: every theme and the probe read
// fetch.Response.Body, so decoding it here — before Response is even built —
// reaches every caller instead of some of them.
//
// It only runs on bodies that look like HTML (looksLikeHTML). A page fetched
// with no declared type is still worth sniffing, but a page image or a
// GetFileRetrieval epub is not text at all, and running charset detection
// over binary bytes would corrupt them: DetermineEncoding's own fallback,
// absent better evidence, is windows-1252, which is not a no-op on arbitrary
// bytes the way it is on ASCII text.
//
// A body that is already valid UTF-8 is returned byte-for-byte unchanged.
// DetermineEncoding recognises valid UTF-8 containing non-ASCII bytes and
// reports the no-op encoding for it (handled below), and an all-ASCII body
// decodes as identical under windows-1252 too, since windows-1252 agrees
// with ASCII below 0x80. So the common case — a page that is already UTF-8,
// or that declares nothing and is valid UTF-8 — never has a byte moved.
//
// A byte that is invalid in whatever encoding was chosen is not an error and
// does not truncate the document: golang.org/x/text's decoders replace an
// untranscodable byte with U+FFFD (the Unicode replacement rune) and keep
// going, so a page that lies about its encoding, or simply contains a bad
// byte, still parses as far as it can — only the offending characters come
// out looking wrong, which is the honest outcome for data that was already
// wrong before Quire touched it.
func decodeToUTF8(header http.Header, body []byte) []byte {
	contentType := header.Get("Content-Type")
	if !looksLikeHTML(contentType, body) {
		return body
	}
	enc, _, _ := charset.DetermineEncoding(body, contentType)
	if enc == encoding.Nop {
		return body
	}
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		// Transcoding failed outright, rather than merely substituting
		// replacement runes for bad bytes. Returning the original bytes
		// keeps the document whole; a garbled-but-complete page beats a
		// dropped one.
		return body
	}
	return decoded
}

// looksLikeHTML trusts a declared Content-Type when there is one — the same
// convention prober.looksLikeImage uses for image bytes, and for the same
// reason: a host that labels its own bytes is telling us something, and
// sniffing is the fallback for silence, not the rule. Real HTML servers
// rarely omit Content-Type; real binary responses Quire fetches (an epub, a
// page image) reliably declare their own type correctly, so gating on it
// here is what keeps this decoder off their bytes.
func looksLikeHTML(contentType string, body []byte) bool {
	if contentType != "" {
		if mt, _, err := mime.ParseMediaType(contentType); err == nil {
			return mt == "text/html" || mt == "application/xhtml+xml"
		}
		// An unparseable Content-Type is not a signal either way; fall
		// through to sniffing rather than guess from a malformed header.
	}
	return strings.HasPrefix(http.DetectContentType(body), "text/html")
}
