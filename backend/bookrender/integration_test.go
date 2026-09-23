package bookrender

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealMutool exercises render.js end to end against a real mutool build
// and a real epub — skipped unless QUIRE_MUTOOL names a host mutool binary
// (see build/mupdf.sh's `host` target, or build it directly from the MuPDF
// source per books-contract.md §A) and QUIRE_MUTOOL_EPUB names an epub to
// open (e.g. ~/projects/quire-spike/disciple.epub). Both stay unset in CI and
// on a plain `go test ./...`, which is why the whole suite is offline by
// default.
func TestRealMutool(t *testing.T) {
	mutool := os.Getenv("QUIRE_MUTOOL")
	epub := os.Getenv("QUIRE_MUTOOL_EPUB")
	if mutool == "" || epub == "" {
		t.Skip("QUIRE_MUTOOL and QUIRE_MUTOOL_EPUB not set; skipping the real-mutool integration test")
	}

	dir := t.TempDir()
	script, err := WriteScript(dir)
	if err != nil {
		t.Fatalf("WriteScript: %v", err)
	}

	r := New(Options{MutoolPath: mutool, ScriptPath: script})
	defer r.Close()
	ctx := context.Background()

	openRes, err := r.Open(ctx, epub)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if openRes.FixedLayout {
		t.Errorf("an epub should not be fixed layout")
	}
	if openRes.Title == "" {
		t.Errorf("expected a non-empty title")
	}

	for _, tc := range []struct {
		name     string
		settings Settings
	}{
		{"defaults", Defaults()},
		{"garamond-relaxed-left", Settings{Font: FontGaramond, Size: 6, Margins: MarginsWide, Spacing: SpacingRelaxed, Align: AlignLeft}},
		{"noto-narrow", Settings{Font: FontNoto, Size: 2, Margins: MarginsNarrow, Spacing: SpacingNormal, Align: AlignBook}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			css, effectiveFont := ComposeCSS(tc.settings, nil)
			t.Logf("effective font for %s: %s", tc.name, effectiveFont)

			pages, err := r.Layout(ctx, LayoutParams{W: 509.3, H: 679.2, Em: EmForSize(tc.settings.Size), CSS: css})
			if err != nil {
				t.Fatalf("Layout: %v", err)
			}
			if pages <= 0 {
				t.Fatalf("Layout reported %d pages", pages)
			}

			// Page 0 of disciple.epub is a cover image with no extractable
			// text; page 5 is well into the body text, same as the manual
			// verification this test mirrors.
			const textPage = 5
			out := filepath.Join(dir, tc.name+"-page.png")
			if err := r.Render(ctx, textPage, out, 0); err != nil {
				t.Fatalf("Render: %v", err)
			}
			info, err := os.Stat(out)
			if err != nil || info.Size() == 0 {
				t.Fatalf("Render did not produce a usable PNG: %v", err)
			}

			text, err := r.Text(ctx, textPage)
			if err != nil {
				t.Fatalf("Text: %v", err)
			}
			if text == "" {
				t.Errorf("Text returned nothing for page %d", textPage)
			}
		})
	}

	toc, err := r.Outline(ctx)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(toc) == 0 {
		t.Errorf("expected a non-empty outline for disciple.epub")
	}
}

// TestRealMutoolMemoryCap makes sure the ulimit wrapper does not itself break
// a normal open — a wrapper that silently ate the child's stdin/stdout would
// be worse than no memory cap at all.
func TestRealMutoolMemoryCap(t *testing.T) {
	mutool := os.Getenv("QUIRE_MUTOOL")
	epub := os.Getenv("QUIRE_MUTOOL_EPUB")
	if mutool == "" || epub == "" {
		t.Skip("QUIRE_MUTOOL and QUIRE_MUTOOL_EPUB not set")
	}
	dir := t.TempDir()
	script, err := WriteScript(dir)
	if err != nil {
		t.Fatalf("WriteScript: %v", err)
	}
	// A generous cap: this is about proving the wrapper works, not about
	// finding the real device's limit.
	r := New(Options{MutoolPath: mutool, ScriptPath: script, MemoryCapKB: 2_000_000,
		OpenTimeout: 20 * time.Second})
	defer r.Close()
	if _, err := r.Open(context.Background(), epub); err != nil {
		t.Fatalf("Open under a memory cap: %v", err)
	}
}
