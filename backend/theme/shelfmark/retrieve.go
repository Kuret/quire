package shelfmark

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
)

// How long Retrieve is prepared to wait, and how often it asks.
//
// Every number here came from the instance measured on 2026-09-20, and the
// bias is deliberately towards patience: a timeout that fires while Shelfmark
// is still working destroys a download that would have succeeded, and the user
// is left with nothing to show for several minutes of waiting. A spinner that
// runs slightly too long costs nothing by comparison.
const (
	// pollInterval is how often /api/status is asked.
	//
	// 5s. The fetch layer's own politeness floor is 2 seconds per host, so
	// anything under that is not actually faster; 5 keeps a ten-minute
	// download to around 120 requests, which is nothing for a service on the
	// user's own network, while still refreshing the progress note often
	// enough that the screen looks alive. One second was rejected as
	// hammering a box in the user's living room for no information.
	pollInterval = 5 * time.Second

	// retrieveTimeout bounds the whole wait: POST, source search, transfer.
	//
	// 15 minutes, which is three times the instance's own
	// `release_search_timeout` of 300 seconds (its /api/config, 2026-09-20).
	// The multiplier is the point: 300s is the instance's budget for searching
	// *one* source, and the owner's note is that it tries several in turn
	// before one works, so 300s is a floor on the worst case and not a bound
	// on it. A single /api/releases call was already measured at 36 seconds.
	//
	// It is a backstop, not a schedule. The caller's context is the real
	// control and is checked first on every pass, so a user who taps cancel
	// does not wait for this.
	retrieveTimeout = 15 * time.Minute
)

// jsonPoster is the one capability shelfmark needs that theme.Fetcher does not
// offer: a POST with an application/json body.
//
// It is asserted rather than added to theme.Fetcher because that interface is
// implemented by the real client, the fixture fetcher and half a dozen test
// doubles across the tree, and widening it would make every one of them carry
// a method that only this theme will ever call. fetch.Client implements this,
// and so does themetest.Fetcher; a fetcher that does not gets a clear error
// rather than a silent fallback, because there is no other way to start a
// download and pretending otherwise would fail later and less legibly.
type jsonPoster interface {
	PostJSON(ctx context.Context, p *fetch.Policy, rawurl string, body []byte) (*fetch.Response, error)
}

// Retrieve implements theme.FileTheme: it asks the instance to fetch one
// release and waits until the finished file can be collected.
//
// The sequence is the instance's, verified 2026-09-20:
//
//  1. GET /api/releases for the book, to find the chosen release *as the
//     instance describes it today*. The whole object has to go back in step 2
//     and it is not something Quire can reconstruct — `extra` carries
//     per-source detail the instance needs to do the fetch — so it is read
//     fresh rather than cached from whenever Chapters last ran.
//  2. POST that object to /api/releases/download.
//  3. GET /api/status until the download's id turns up under `complete`.
//  4. Hand back /api/localdownload?id=… .
//
// What comes back is a **URL**, not bytes, and that is the whole design: the
// caller fetches it through the ordinary guarded client, so the SSRF policy,
// the rate limiter, the size cap and the honest User-Agent apply to the
// transfer exactly as they do to everything else. See theme.FileTheme.
func (t *Theme) Retrieve(ctx context.Context, s *theme.Source, chapterID string, progress func(note string)) (string, string, error) {
	note := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}

	ref, err := parseReleaseID(chapterID)
	if err != nil {
		return "", "", err
	}
	poster, ok := t.f.(jsonPoster)
	if !ok {
		return "", "", fmt.Errorf("shelfmark: this fetcher cannot POST JSON, so a download cannot be started")
	}
	p, err := s.Policy()
	if err != nil {
		return "", "", err
	}

	// Step 1. Slow — see ReleaseList — so the user is told what is happening
	// before it starts rather than after it finishes.
	note("asking Shelfmark which sources have this book")
	res, err := t.releases(ctx, s, ref.Book())
	if err != nil {
		return "", "", err
	}
	chosen, err := findRelease(res, ref)
	if err != nil {
		return "", "", err
	}
	format := normaliseFormat(chosen.Format)

	// Step 2. The release object goes back exactly as it arrived.
	note("asking Shelfmark to fetch %s", releaseTitle(chosen, format))
	startURL := t.endpoint(s, pathStartDownload, nil)
	resp, err := poster.PostJSON(ctx, p, startURL, chosen.raw)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("shelfmark: %s: HTTP %d", startURL, resp.StatusCode)
	}
	var ack downloadAck
	if err := decodeInto(startURL, resp.Body, &ack); err != nil {
		return "", "", err
	}

	// The download's id. The instance keys its own /api/status records by the
	// release's source_id — the id under `complete` is a 32-character md5, the
	// same shape and, on the instance, the same value as source_id — so that
	// is what is polled for.
	//
	// **The acknowledgement carries no id.** Measured 2026-09-20 by posting a
	// real release to a live instance (testdata/download-ack.json):
	//
	//	{"priority":0,"status":"queued"}
	//
	// and the record then appeared under /api/status keyed by the release's
	// source_id. So the fallback below is not a fallback in practice, it is
	// the path. The ack.ID branch is kept because believing an id the server
	// volunteers can only be more authoritative than inferring one — but it is
	// unreachable against this build, and a future version that starts sending
	// one should not need a code change to be obeyed.
	downloadID := strings.TrimSpace(ack.ID)
	if downloadID == "" {
		downloadID = ref.SourceID
	}

	// Steps 3 and 4.
	entry, err := t.await(ctx, s, downloadID, note)
	if err != nil {
		return "", "", err
	}

	v := url.Values{}
	v.Set("id", downloadID)
	fileURL := t.endpoint(s, pathLocalDownload, v)
	note("Shelfmark has the file")
	return fileURL, filename(entry, chosen, format), nil
}

// await polls /api/status until the download finishes, fails, or runs out of
// patience.
func (t *Theme) await(ctx context.Context, s *theme.Source, id string, note func(string, ...any)) (statusEntry, error) {
	deadline := t.now().Add(retrieveTimeout)
	statusURL := t.endpoint(s, pathStatus, nil)
	last := ""

	for {
		// The caller's context first, so a cancelled download stops at the top
		// of the loop rather than after another request.
		if err := ctx.Err(); err != nil {
			return statusEntry{}, err
		}

		var st statusResponse
		if err := t.getJSON(ctx, s, statusURL, &st); err != nil {
			return statusEntry{}, err
		}

		if e, ok := st.Complete[id]; ok {
			return e, nil
		}
		if e, ok := st.Error[id]; ok {
			return statusEntry{}, fmt.Errorf("shelfmark: download %s failed: %s", id, describe(e, "the instance reported an error"))
		}
		if e, ok := st.Cancelled[id]; ok {
			return statusEntry{}, fmt.Errorf("shelfmark: download %s was cancelled: %s", id, describe(e, "cancelled on the instance"))
		}

		// Not finished. Say where it is, but only when that has changed:
		// repeating "downloading — 31%" every five seconds is noise, and the
		// caller may well be writing each note to a log.
		if msg, ok := pending(st, id); ok {
			if msg != last {
				note("%s", msg)
				last = msg
			}
		} else if last == "" {
			// The id is in no bucket at all. That is normal for the first
			// second or two after the POST — the record is created when the
			// instance starts work — so it is reported as waiting rather than
			// treated as a failure.
			note("waiting for Shelfmark to pick the download up")
			last = "waiting"
		}

		if !t.now().Before(deadline) {
			return statusEntry{}, fmt.Errorf(
				"shelfmark: gave up waiting for download %s after %s; the instance may still be working, "+
					"in which case the file will be in its library", id, retrieveTimeout)
		}
		if err := t.sleep(ctx, pollInterval); err != nil {
			return statusEntry{}, err
		}
	}
}

// pending finds id in one of the in-progress buckets and describes it.
//
// The buckets are asked in the order the instance moves a download through
// them, so the note the user sees follows the work: queued, then locating a
// source, then resolving it, then downloading.
func pending(st statusResponse, id string) (string, bool) {
	for _, b := range []struct {
		name    string
		entries map[string]statusEntry
	}{
		{"queued", st.Queued},
		{"looking for a source", st.Locating},
		{"resolving the source", st.Resolving},
		{"downloading", st.Downloading},
	} {
		e, ok := b.entries[id]
		if !ok {
			continue
		}
		msg := describe(e, b.name)
		if e.Progress > 0 {
			msg = fmt.Sprintf("%s — %.0f%%", msg, e.Progress)
		}
		return msg, true
	}
	return "", false
}

// describe prefers the instance's own status_message, which is written for a
// person, over the bucket name, which is not.
func describe(e statusEntry, fallback string) string {
	if msg := strings.TrimSpace(e.StatusMessage); msg != "" {
		return msg
	}
	if s := strings.TrimSpace(e.Status); s != "" {
		return s
	}
	return fallback
}

// findRelease picks the chosen release out of a fresh /api/releases response.
//
// It matches on source *and* source_id, which is what the id encodes and what
// was unique across all 50 releases of the 2026-09-20 capture. A release that
// is no longer offered is a real and ordinary outcome — sources come and go
// between the list and the tap — so it is reported as that rather than as a
// parse failure.
func findRelease(res *releasesResponse, ref releaseRef) (release, error) {
	for _, raw := range res.Releases {
		r, err := decodeRelease(raw)
		if err != nil {
			continue
		}
		if r.Source == ref.Source && r.SourceID == ref.SourceID {
			return r, nil
		}
	}
	return release{}, fmt.Errorf(
		"shelfmark: %s is no longer among the releases for this book; it may have been withdrawn, "+
			"or the source may be unreachable — search the book again", ref.SourceID)
}

// filename is the name to give the finished document.
//
// The instance's own name for the file is the best one available: it is what
// /api/localdownload's Content-Disposition will say, and it already carries
// author and year ("An Example Book - Example Author (1970)_1.epub"). Since
// Retrieve hands back a URL rather than the response, that header is never
// seen here, so the name is taken from the status record's download_path
// instead, which is the same string.
//
// When the record carries no path — an instance that does not report one — the
// release's own title is used with the format as an extension. Either way the
// result is sanitised: it becomes a file name on the device, and a title with
// a slash in it must not become a directory.
func filename(e statusEntry, r release, format string) string {
	if p := strings.TrimSpace(e.DownloadPath); p != "" {
		if base := sanitiseName(path.Base(p)); base != "" && base != "." {
			return base
		}
	}
	name := sanitiseName(strings.TrimSpace(r.Title))
	if name == "" {
		name = sanitiseName(strings.TrimSpace(e.Title))
	}
	if name == "" {
		name = "book"
	}
	if format != "" && !strings.HasSuffix(strings.ToLower(name), "."+format) {
		name += "." + format
	}
	return name
}

// sanitiseName makes a string safe to use as a single file name.
func sanitiseName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			// control characters: dropped
		case r == '/' || r == '\\' || r == ':':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.Trim(out, ".")
	return strings.TrimSpace(out)
}

// compile-time proof that the theme satisfies the interfaces it claims.
var (
	_ theme.Theme               = (*Theme)(nil)
	_ theme.FileTheme           = (*Theme)(nil)
	_ theme.DiscoveryClassifier = (*Theme)(nil)
	// Confirm is the strong check PLAN §7.5 stage 5 runs before it believes the
	// fingerprint (2026-09-20). Asserted here so that renaming it would break
	// the build rather than silently leave the instance unverified.
	_ theme.Confirmer = (*Theme)(nil)
)
