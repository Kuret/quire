package covers_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickl/quire/backend/covers"
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

	path, err := c.Path(context.Background(), source(), coverURL)
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
	again, err := c.Path(context.Background(), source(), coverURL)
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
	if _, err := c.Path(context.Background(), source(), coverURL); err != nil {
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
	path, err := c.Path(context.Background(), src, coverURL)
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
