package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
)

// PLAN §6 M3: plain language, not error codes. The probe used to tell a user
// "HTTP 444" and leave them there; the download path had the same hole, in the
// place a rate limit is most likely to turn up — part way through a volume,
// after the site has decided Quire is asking too often.
//
// No retry is added anywhere for this. PLAN §7.4's client already honours
// Retry-After and backs off; the user starting the download again when they
// choose is the right control.
func TestDownloadRateLimitSaysWhatToDo(t *testing.T) {
	svc, store, _, _, rec := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) {
			o.Fetcher = &failingFetcher{
				inner: o.Fetcher,
				after: 0,
				// Exactly the shape a theme builds for a refusal.
				err: errors.New("madara: /pages/lantern-keeper/4/001.jpg: HTTP 429"),
			}
		})
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)

	failed := waitForPhase(t, rec, "failed")
	msg, _ := failed["message"].(string)
	low := strings.ToLower(msg)
	if !strings.Contains(low, "asking too often") || !strings.Contains(low, "few minutes") {
		t.Errorf("message %q does not tell the user what a 429 means or what to do", msg)
	}
	if strings.Contains(msg, "429") {
		t.Errorf("message %q still hands the user a bare status code", msg)
	}
}
