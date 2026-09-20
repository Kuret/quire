package shelfmark

import (
	"fmt"
	"net/url"
	"strings"
)

// Shelfmark identifies nothing with a single opaque token, so both of Quire's
// IDs are composite.
//
// A book is identified by the *metadata provider* it came from plus that
// provider's own id — "openlibrary" + "OL893414W" — because the same book
// exists under several providers and neither half is unique on its own. A
// release is identified by its book plus the source that offered it plus that
// source's id, which is the smallest thing that names one downloadable file:
// on the 2026-09-20 Dune capture, (source, source_id) was unique across all
// 50 releases, and adding the book makes the id self-contained so a release
// can be looked up again without the caller remembering which book it was for.
//
// They are stored in theme.SeriesStub.ID / theme.Chapter.ID's documented
// site-relative path shape, for the reason that field documents: a source
// re-pointed at another instance keeps working, because nothing here is an
// absolute URL. The leading prefix also keeps the ids self-describing in a
// state file, and makes a release id handed to Series(), or a book id handed
// to Retrieve(), a clear error rather than a 404 from the instance.
//
// Every segment is percent-escaped. Provider ids are tidy in practice
// ("OL893414W", an md5), but "in practice" is not a guarantee, and a source_id
// with a slash in it would otherwise silently re-cut the whole id.
const (
	bookPrefix    = "/book/"
	releasePrefix = "/release/"
)

// bookID builds the stored id of a book.
func bookID(provider, providerID string) string {
	return bookPrefix + url.PathEscape(provider) + "/" + url.PathEscape(providerID)
}

// releaseID builds the stored id of one release of a book.
func releaseID(provider, providerID, source, sourceID string) string {
	return releasePrefix +
		url.PathEscape(provider) + "/" +
		url.PathEscape(providerID) + "/" +
		url.PathEscape(source) + "/" +
		url.PathEscape(sourceID)
}

// bookRef is a parsed book id.
type bookRef struct {
	Provider   string
	ProviderID string
}

// String re-encodes the reference, so that parse and build are demonstrably
// each other's inverse.
func (b bookRef) String() string { return bookID(b.Provider, b.ProviderID) }

// releaseRef is a parsed release id.
type releaseRef struct {
	Provider   string
	ProviderID string
	Source     string
	SourceID   string
}

// Book is the book this release belongs to.
func (r releaseRef) Book() bookRef { return bookRef{Provider: r.Provider, ProviderID: r.ProviderID} }

// String re-encodes the reference.
func (r releaseRef) String() string {
	return releaseID(r.Provider, r.ProviderID, r.Source, r.SourceID)
}

// parseBookID reads a stored book id.
func parseBookID(id string) (bookRef, error) {
	parts, err := segments(id, bookPrefix, 2, "book")
	if err != nil {
		return bookRef{}, err
	}
	return bookRef{Provider: parts[0], ProviderID: parts[1]}, nil
}

// parseReleaseID reads a stored release id.
func parseReleaseID(id string) (releaseRef, error) {
	parts, err := segments(id, releasePrefix, 4, "release")
	if err != nil {
		return releaseRef{}, err
	}
	return releaseRef{Provider: parts[0], ProviderID: parts[1], Source: parts[2], SourceID: parts[3]}, nil
}

// segments splits an id into exactly n unescaped, non-empty parts.
//
// Like mangadex's parseID it tolerates the id arriving with something in front
// of the prefix — the browse UI and the probe both have a habit of passing
// around whatever they last saw, which may be a full URL — but it does not
// tolerate the wrong *number* of segments. An id with a missing or extra part
// is a caller bug, and a lenient read of one is how a release ends up being
// fetched for the wrong book.
func segments(id, prefix string, n int, what string) ([]string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("shelfmark: empty %s id", what)
	}
	i := strings.Index(id, prefix)
	if i < 0 {
		return nil, fmt.Errorf("shelfmark: %q is not a %s id (expected a %s… path)", id, what, prefix)
	}
	rest := strings.Trim(id[i+len(prefix):], "/")
	if j := strings.IndexAny(rest, "?#"); j >= 0 {
		rest = rest[:j]
	}
	parts := strings.Split(rest, "/")
	if len(parts) != n {
		return nil, fmt.Errorf("shelfmark: %q is not a %s id: got %d segments, want %d", id, what, len(parts), n)
	}
	out := make([]string, n)
	for k, p := range parts {
		v, err := url.PathUnescape(p)
		if err != nil {
			return nil, fmt.Errorf("shelfmark: %q is not a %s id: %w", id, what, err)
		}
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("shelfmark: %q is not a %s id: segment %d is empty", id, what, k+1)
		}
		out[k] = v
	}
	return out, nil
}
