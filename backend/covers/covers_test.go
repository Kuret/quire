package covers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

const coverURL = "https://example.invalid/wp-content/uploads/2026/01/lantern-keeper.png"

func source() *theme.Source {
	return &theme.Source{
		ID: "example-reader", Name: "Example Reader", Lang: "en",
		Theme: madara.ID, BaseURL: "https://example.invalid",
		AddedAt: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
	}
}

// bigPNG is a deliberately oversized original: the point of the cache is that
// what lands on disk is much smaller than what the site serves.
func bigPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1200, 1800))
	for y := 0; y < 1800; y++ {
		for x := 0; x < 1200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestCoverIsDownscaledOnceAndReused(t *testing.T) {
	original := bigPNG(t)
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: original},
	})
	c := covers.New(t.TempDir(), f)

	path, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, fetch.Referrer{})
	if err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the cached cover is not a JPEG: %v", err)
	}
	if cfg.Width > covers.ThumbWidth || cfg.Height > covers.ThumbHeight {
		t.Errorf("thumbnail is %dx%d, larger than the %dx%d ceiling",
			cfg.Width, cfg.Height, covers.ThumbWidth, covers.ThumbHeight)
	}
	// PLAN §6 M3: downscale hard. The pixel count is the honest measure — a
	// synthetic gradient compresses unrepresentatively well as PNG — and a flat
	// byte ceiling keeps a screenful comfortably inside the device's memory.
	if got, want := cfg.Width*cfg.Height, 1200*1800; got >= want/8 {
		t.Errorf("thumbnail is %d pixels against an original of %d; that is not downscaled hard", got, want)
	}
	if len(b) > 60<<10 {
		t.Errorf("thumbnail is %d bytes; a screenful of these has to fit in memory", len(b))
	}

	// A second request must not fetch again: the fixture fetcher records every
	// call, so this is checkable rather than a matter of faith.
	before := len(f.Calls())
	again, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, fetch.Referrer{})
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Errorf("path changed between calls: %q then %q", path, again)
	}
	if got := len(f.Calls()); got != before {
		t.Errorf("a cached cover was fetched again (%d calls, was %d)", got, before)
	}
}

// A source's covers live in one directory, so forgetting the source drops them
// all and nothing else.
func TestForgetDropsOnlyThatSource(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: bigPNG(t)},
	})
	dir := t.TempDir()
	c := covers.New(dir, f)
	if _, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, fetch.Referrer{}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "another-source", "keep.jpg")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := c.Forget("example-reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "example-reader")); !os.IsNotExist(err) {
		t.Error("the source's covers were not dropped")
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another source's covers were dropped too: %v", err)
	}
}

// A source ID is schema-constrained already; the cache checks again, because
// the cost of being wrong is writing outside its own directory.
func TestOddSourceIDStaysInsideTheCache(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: bigPNG(t)},
	})
	dir := t.TempDir()
	c := covers.New(dir, f)

	src := source()
	src.ID = "../../escape"
	path, err := c.Path(context.Background(), madara.New(nil), src, coverURL, fetch.Referrer{})
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rel) >= 2 && rel[:2] == ".." {
		t.Errorf("cover written outside the cache: %s", path)
	}
}

// PLAN §7.6, measured on comick's CDN on 2026-09-16: 403 with no `Referer`,
// 200 `image/webp` with one. The cache cannot know which page a cover URL came
// from, so the value arrives as a parameter and has to reach the request
// unchanged.
func TestCoverReferrerReachesTheRequest(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: bigPNG(t)},
	})
	c := covers.New(t.TempDir(), f)

	from, err := fetch.PageReferrer("https://example.invalid/comic/the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, from); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d requests, want 1", len(calls))
	}
	if calls[0].Referrer.String() != from.String() {
		t.Errorf("the cover was fetched with referrer %q, want %q", calls[0].Referrer.String(), from.String())
	}
}

// headeredTheme is madara.Theme plus theme.SourceHeaders, standing in for a
// theme like globalcomix whose source needs a static header attached to its
// own requests — including the cover fetch Cache.Path performs.
type headeredTheme struct {
	*madara.Theme
	headers http.Header
}

func (h headeredTheme) SourceHeaders(*theme.Source) http.Header { return h.headers }

// cookieTheme is madara.Theme plus theme.CookieUser, standing in for a theme
// whose source carries a server-issued session cookie into the cover fetches
// that follow a grant.
type cookieTheme struct {
	*madara.Theme
	use bool
}

func (c cookieTheme) UsesCookies(*theme.Source) bool { return c.use }

// TestCoverFetchCarriesSourceHeaders is the mutation (a) test for
// covers.go's Cache.Path: reverting its policy construction from
// theme.PolicyFor(th, src) back to the bare src.Policy() it used to call
// makes this fail, because a bare Source.Policy() never carries a theme's
// SourceHeaders — theme.PolicyFor is the only place that side interface is
// consulted.
func TestCoverFetchCarriesSourceHeaders(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: bigPNG(t)},
	})
	c := covers.New(t.TempDir(), f)
	th := headeredTheme{Theme: madara.New(f), headers: http.Header{"X-Reading-Grant": []string{"granted-token"}}}

	if _, err := c.Path(context.Background(), th, source(), coverURL, fetch.Referrer{}); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d requests, want 1", len(calls))
	}
	if calls[0].Policy == nil || calls[0].Policy.Headers.Get("X-Reading-Grant") != "granted-token" {
		t.Errorf("the cover was fetched with policy %+v, want X-Reading-Grant: granted-token", calls[0].Policy)
	}
}

// TestCoverFetchCarriesCookieJar is the same mutation (a) proof for
// theme.CookieUser: without going through theme.PolicyFor, a theme that opts
// into cookies never gets a jar on the policy the cover cache fetches with.
func TestCoverFetchCarriesCookieJar(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: bigPNG(t)},
	})
	c := covers.New(t.TempDir(), f)
	th := cookieTheme{Theme: madara.New(f), use: true}

	if _, err := c.Path(context.Background(), th, source(), coverURL, fetch.Referrer{}); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d requests, want 1", len(calls))
	}
	if calls[0].Policy == nil || calls[0].Policy.Cookies == nil {
		t.Errorf("the cover was fetched with policy %+v, want a cookie jar", calls[0].Policy)
	}
}

// The other half of the same rule, and the reason mangadex is untouched: a
// source whose cover host asks for nothing gets **no header at all**, asserted
// by absence rather than by an empty string.
func TestNoCoverReferrerSendsNoHeader(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: bigPNG(t)},
	})
	c := covers.New(t.TempDir(), f)

	if _, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, fetch.Referrer{}); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d requests, want 1", len(calls))
	}
	if !calls[0].Referrer.IsZero() {
		t.Errorf("a cover with no page behind it was fetched with referrer %q", calls[0].Referrer.String())
	}
}

// The panel on this device is colour (it reports "reMarkable Ferrari", the
// Paper Pro), so a cover keeps its colour. It used to be flattened to grey.
// The panel on this device is colour — it reports "reMarkable Ferrari", the
// Paper Pro, and the stock UI ships colour pens on it — so a cover keeps its
// colour. It used to be flattened to grey here.
func TestACoverKeepsItsColour(t *testing.T) {
	// A deep red source image. Red is chosen because greyscale conversion is
	// luminance-weighted: a mid red collapses to a mid grey, so a flattened
	// cover would still *look* plausible and only the channels give it away.
	src := image.NewRGBA(image.Rect(0, 0, 600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 600; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, src); err != nil {
		t.Fatal(err)
	}

	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: raw.String()},
	})
	c := covers.New(t.TempDir(), f)

	path, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, fetch.Referrer{})
	if err != nil {
		t.Fatal(err)
	}
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	img, _, err := image.Decode(fh)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	r, g, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	if r == g && g == bl {
		t.Fatalf("the cover came back grey (%d,%d,%d); colour was flattened", r>>8, g>>8, bl>>8)
	}
	if r <= g || r <= bl {
		t.Errorf("the red channel did not dominate: (%d,%d,%d)", r>>8, g>>8, bl>>8)
	}
}

// Turning colour on must not leave every already-cached cover grey for ever.
//
// The cache key was the URL alone, so the bytes on disk were reused whatever
// they had been rendered with. This pins that how a cover was rendered is part
// of the key, by asserting the file is *not* at the path the bare URL gives.
func TestTheRenderVersionIsPartOfTheKey(t *testing.T) {
	original := bigPNG(t)
	f := themetest.New(t, map[string]themetest.Route{
		"GET /wp-content/uploads/2026/01/lantern-keeper.png": {Body: original},
	})
	dir := t.TempDir()
	c := covers.New(dir, f)

	path, err := c.Path(context.Background(), madara.New(nil), source(), coverURL, fetch.Referrer{})
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte(coverURL))
	bare := hex.EncodeToString(sum[:8]) + ".jpg"
	if filepath.Base(path) == bare {
		t.Fatal("the cover is keyed by the bare URL, so a render change would reuse the old bytes for ever")
	}
}
