package service_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// headeredVolumeTheme is volumeTheme plus theme.SourceHeaders, standing in for
// a theme like globalcomix whose source needs a static header attached to its
// own requests — including the page-image fetches sourceFetcher.Get performs.
type headeredVolumeTheme struct {
	volumeTheme
	headers http.Header
}

func (h headeredVolumeTheme) SourceHeaders(*theme.Source) http.Header { return h.headers }

// cookieVolumeTheme is volumeTheme plus theme.CookieUser, standing in for a
// theme whose source carries a server-issued session cookie into the
// page-image fetches that follow a grant.
type cookieVolumeTheme struct {
	volumeTheme
	use bool
}

func (c cookieVolumeTheme) UsesCookies(*theme.Source) bool { return c.use }

// TestDownloadedPagesCarrySourceHeaders is the mutation (a) test for
// download.go's sourceFetcher.Get: reverting its policy construction from
// theme.PolicyFor(f.th, f.src) back to the bare f.src.Policy() it used to call
// makes this fail, because a bare Source.Policy() never carries a theme's
// SourceHeaders — theme.PolicyFor is the only place that side interface is
// consulted.
func TestDownloadedPagesCarrySourceHeaders(t *testing.T) {
	f := runVolumeDownload(t, func(f *themetest.Fetcher) theme.Theme {
		return headeredVolumeTheme{
			volumeTheme: volumeTheme{madara.NewWithClock(f, func() time.Time { return fixedNow })},
			headers:     http.Header{"X-Reading-Grant": []string{"granted-token"}},
		}
	})

	got := imageReferers(t, f)
	if len(got) == 0 {
		t.Fatal("no page images were fetched")
	}
	for path, req := range got {
		if req.Policy == nil || req.Policy.Headers.Get("X-Reading-Grant") != "granted-token" {
			t.Errorf("%s fetched with policy %+v, want X-Reading-Grant: granted-token", path, req.Policy)
		}
	}
}

// TestDownloadedPagesCarryCookieJar is the same mutation (a) proof for
// theme.CookieUser: without going through theme.PolicyFor, a theme that opts
// into cookies never gets a jar on the policy the page-image queue fetches
// with.
func TestDownloadedPagesCarryCookieJar(t *testing.T) {
	f := runVolumeDownload(t, func(f *themetest.Fetcher) theme.Theme {
		return cookieVolumeTheme{
			volumeTheme: volumeTheme{madara.NewWithClock(f, func() time.Time { return fixedNow })},
			use:         true,
		}
	})

	got := imageReferers(t, f)
	if len(got) == 0 {
		t.Fatal("no page images were fetched")
	}
	for path, req := range got {
		if req.Policy == nil || req.Policy.Cookies == nil {
			t.Errorf("%s fetched with policy %+v, want a cookie jar", path, req.Policy)
		}
	}
}
