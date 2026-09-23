package bookrender

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRenderer(t *testing.T, extraEnv ...string) *Renderer {
	t.Helper()
	opts := Options{
		LayoutTimeout: time.Second,
		RenderTimeout: time.Second,
		OpenTimeout:   time.Second,
		spawn:         fakeSpawn(extraEnv...),
	}
	r := New(opts)
	t.Cleanup(func() { r.Close() })
	return r
}

func TestRendererRoundTrip(t *testing.T) {
	r := newTestRenderer(t)
	ctx := context.Background()

	res, err := r.Open(ctx, "book.epub")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if res.FixedLayout {
		t.Errorf("expected reflowable, got fixedLayout=true")
	}
	if res.Title != "Fake Book" {
		t.Errorf("Title = %q", res.Title)
	}

	pages, err := r.Layout(ctx, LayoutParams{W: 509.3, H: 679.2, Em: 12, CSS: "@page{margin:1em}"})
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if pages != 10 {
		t.Errorf("pages = %d, want 10", pages)
	}

	out := filepath.Join(t.TempDir(), "page.png")
	if err := r.Render(ctx, 3, out, 0); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("render did not write %s: %v", out, err)
	}

	text, err := r.Text(ctx, 3)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != "snippet for page 3" {
		t.Errorf("Text = %q", text)
	}

	toc, err := r.Outline(ctx)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(toc) != 1 || toc[0].Title != "Chapter 1" {
		t.Errorf("Outline = %+v", toc)
	}
}

func TestRendererOpenUnreadable(t *testing.T) {
	r := newTestRenderer(t, envOpenFails+"=1")
	_, err := r.Open(context.Background(), "corrupt.epub")
	if err == nil {
		t.Fatal("expected an error opening an unreadable file")
	}
}

// TestRendererTimeoutKillsChild is the mutation check for "renderer timeout
// kills the child": a render call that never answers must be killed, not
// left running, once its timeout elapses.
func TestRendererTimeoutKillsChild(t *testing.T) {
	r := newTestRenderer(t, envHangOnCmd+"=render")
	ctx := context.Background()

	if _, err := r.Open(ctx, "book.epub"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	start := time.Now()
	err := r.Render(ctx, 0, filepath.Join(t.TempDir(), "p.png"), 0)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected the hung render to time out")
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s to fail, want close to the 1s render timeout", elapsed)
	}

	r.mu.Lock()
	proc := r.cmd
	r.mu.Unlock()
	if proc != nil {
		t.Error("the timed-out process was not cleared — it should have been killed")
	}
}

// TestRendererCrashRestart is the mutation check for "crash restart": a
// process that dies mid-request is restarted, reopened and re-laid-out
// transparently, and the request that triggered the restart still succeeds.
func TestRendererCrashRestart(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "crashed-once")
	r := newTestRenderer(t, envCrashOnCmd+"=render", envCrashMarker+"="+marker)
	ctx := context.Background()

	if _, err := r.Open(ctx, "book.epub"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := r.Layout(ctx, LayoutParams{W: 500, H: 700, Em: 12, CSS: "@page{margin:1em}"}); err != nil {
		t.Fatalf("Layout: %v", err)
	}

	out := filepath.Join(t.TempDir(), "p.png")
	if err := r.Render(ctx, 1, out, 0); err != nil {
		t.Fatalf("Render should have recovered from the crash transparently: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the fake process never actually crashed, so this did not test what it claims to")
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("render did not write %s after recovering: %v", out, err)
	}
}

// TestRendererCrashTwiceSurfacesError: a second failure in a row after the
// one transparent restart must not be masked again.
func TestRendererCrashTwiceSurfacesError(t *testing.T) {
	// No marker file at all: the fake process crashes on every "render",
	// since it can never find (or successfully check) a marker that lets it
	// behave — using a marker path inside a directory that does not exist
	// makes the os.Stat/WriteFile in the fake process fail every time in a
	// way that still exits(1) on crashOn, i.e. it crashes forever.
	r := newTestRenderer(t, envCrashOnCmd+"=render", envCrashMarker+"=/nonexistent/dir/marker")
	ctx := context.Background()

	if _, err := r.Open(ctx, "book.epub"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	err := r.Render(ctx, 1, filepath.Join(t.TempDir(), "p.png"), 0)
	if err == nil {
		t.Fatal("expected a second, unmasked failure")
	}
}

// A layout carrying a font override is several hundred bytes, and `mutool
// run`'s readline() reads 255 at a time: sent as one line, the request split
// in two, failed to parse, and every reply after it answered the wrong
// request. Measured on the device, where the fonts exist; off it the override
// falls back and the request stays short. The fake reads the same way.
func TestRendererLongRequestArrivesWhole(t *testing.T) {
	r := newTestRenderer(t)
	ctx := context.Background()
	if _, err := r.Open(ctx, "book.epub"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	css := strings.Repeat("body, p { font-family: 'Q' !important } /* é… */ ", 40) + "/*LEN*/"
	pages, err := r.Layout(ctx, LayoutParams{W: 509.3, H: 679.2, Em: 12, CSS: css})
	if err != nil {
		t.Fatalf("Layout with a %d-byte stylesheet: %v", len(css), err)
	}
	if pages != len(css) {
		t.Fatalf("the renderer received %d bytes of a %d-byte stylesheet", pages, len(css))
	}

	// The reply after it must answer its own request, not a leftover piece.
	if _, err := r.Outline(ctx); err != nil {
		t.Fatalf("Outline after a long request: %v", err)
	}
}
