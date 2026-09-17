package download

// Re-cutting a chapter the source pre-sliced — PLAN §12.3, extended
// 2026-09-17.
//
// The measurement is in imageproc/restitch.go: a source can deliver a
// vertical-scroll comic already cut into fixed-ratio chunks, at arbitrary
// points, and the per-image splitter correctly does nothing to each chunk while
// the chapter as a whole reproduces the site's own bad cuts. This is where the
// chapter-level decision is applied.
//
// # The on-disk shape, and why it is a subdirectory
//
// A re-cut page spans several source images, so it cannot be named in the
// `%04d-%03d-of-%03d` scheme, which is per *source* image: source 3 might yield
// two pages and source 4 none, and existingPages requires every index to have a
// file. The re-cut pages therefore live in their own directory beside the
// sources, with a marker naming the sources they were made from.
//
//   <chapter>/0000.jpg …          the source images, untouched
//   <chapter>/restitched/0000.jpg …   the re-cut pages
//   <chapter>/restitched/quire-restitch.json
//
// **The sources are kept.** Resume is what this protects: a chapter that cannot
// resume restarts from zero on a dropped connection, which on a tablet on wifi
// is the common case rather than the rare one. It costs roughly double the
// cache footprint for a re-stitched chapter; delete reclaims both, because
// reclaim removes the chapter directory whole, and so does the cache control.
//
// **The marker is written last.** A re-cut that dies halfway leaves pages with
// no marker, which the next run treats as absent and redoes — the same rule the
// `-of-%03d` naming exists for. A partial result that looks whole is worse than
// no result.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"

	"github.com/rickl/quire/backend/imageproc"
)

// RestitchDirName is the subdirectory holding a chapter's re-cut pages.
const RestitchDirName = "restitched"

// restitchMarkerName is written last, and only when every page is on disk.
const restitchMarkerName = "quire-restitch.json"

// restitchMarker records what the re-cut pages were made from.
//
// The source *set* rather than a flag: a chapter re-downloaded from a source
// that has changed its slicing must re-cut rather than serve pages made from
// images that are no longer there.
type restitchMarker struct {
	Version int              `json:"version"`
	Sources []restitchSource `json:"sources"`
	Pages   []string         `json:"pages"`
}

type restitchSource struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

const restitchMarkerVersion = 1

// restitchChapters re-cuts whole chapters the source pre-sliced, replacing
// their page list when it does.
//
// It runs after every page of the chapter is on disk, because the decision is
// about the chapter and cannot be taken one image at a time.
func (r *run) restitchChapters(ctx context.Context, chapters []Chapter) error {
	if r.q.opts.SplitStrips == imageproc.SplitNever {
		// "never" means leave this source alone, and that has to keep being
		// true of the chapter-level path as well as the per-image one.
		return nil
	}

	for c := range r.results {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := ""
		if c < len(chapters) {
			id = chapters[c].ID
		}
		if err := r.restitchChapter(ctx, c, id); err != nil {
			return err
		}
	}
	return nil
}

// restitchChapter handles one chapter: reuse, re-cut, or leave alone.
func (r *run) restitchChapter(ctx context.Context, c int, id string) error {
	sources, ok := chapterSources(r.results[c])
	if !ok {
		// A chapter the per-image splitter has already cut is a chapter of
		// strips, which is the case §12.3 was written for and is not this one.
		return nil
	}
	if len(sources) < imageproc.MinSliceCount {
		return nil
	}
	dir := filepath.Join(filepath.Dir(sources[0]), RestitchDirName)

	if pages, ok := reuseRestitched(dir, sources); ok {
		r.results[c] = []pageFiles{{paths: pages}}
		r.pagesFromRestitch.Add(int64(len(pages)))
		return nil
	}

	scan := imageproc.NewStripScan(r.q.opts.Restitch)
	for _, path := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		img, err := r.decodePage(ctx, path)
		if err != nil {
			// A page that will not decode is not a reason to fail a download
			// that has already succeeded: the chapter keeps its source pages.
			return nil
		}
		scan.Add(img)
	}
	if err := scan.Err(); err != nil {
		return nil
	}

	pages, verdict := scan.Plan()
	if r.q.opts.SplitStrips == imageproc.SplitAlways && !verdict.PreSliced {
		// The escape hatch, in the direction detection cannot be argued out of:
		// "always" waives the seam and shape tests, keeping only the minimum
		// image count, because a user looking at a mangled chapter should not
		// have to wait for a release. The per-image splitter is unchanged.
		pages = scan.ForcePlan()
	}
	if len(pages) == 0 {
		return nil
	}

	written, err := r.writeRestitched(ctx, dir, sources, pages, scan.PlanWidth())
	if err != nil {
		return err
	}
	if len(written) == 0 {
		return nil
	}

	r.results[c] = []pageFiles{{paths: written}}
	r.chaptersRestitched.Add(1)
	r.pagesFromRestitch.Add(int64(len(written)))
	return nil
}

// chapterSources is the chapter's source images in order, or ok=false when any
// page is not a single whole image.
func chapterSources(pages []pageFiles) ([]string, bool) {
	out := make([]string, 0, len(pages))
	for _, p := range pages {
		if len(p.paths) != 1 {
			return nil, false
		}
		out = append(out, p.paths[0])
	}
	return out, len(out) > 0
}

// reuseRestitched reports the pages of a previous re-cut, when its marker is
// valid for exactly these sources.
//
// A missing or mismatched marker is not an error: it is the old path, and the
// chapter is re-cut or left alone as if nothing had been there.
func reuseRestitched(dir string, sources []string) ([]string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, restitchMarkerName))
	if err != nil {
		return nil, false
	}
	var m restitchMarker
	if err := json.Unmarshal(b, &m); err != nil || m.Version != restitchMarkerVersion {
		return nil, false
	}
	if !sameSources(m.Sources, sources) {
		return nil, false
	}

	pages := make([]string, 0, len(m.Pages))
	for _, name := range m.Pages {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			// The marker outlived its pages. Treat it as absent.
			return nil, false
		}
		pages = append(pages, p)
	}
	return pages, len(pages) > 0
}

// sameSources compares the recorded source set with what is on disk now.
func sameSources(recorded []restitchSource, sources []string) bool {
	if len(recorded) != len(sources) {
		return false
	}
	for i, path := range sources {
		st, err := os.Stat(path)
		if err != nil {
			return false
		}
		if recorded[i].Name != filepath.Base(path) || recorded[i].Size != st.Size() {
			return false
		}
	}
	return true
}

// writeRestitched renders the pages and commits the marker last.
func (r *run) writeRestitched(ctx context.Context, dir string, sources []string,
	pages []imageproc.Page, width int) ([]string, error) {

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("download: restitch: %w", err)
	}
	// Anything from an interrupted attempt goes: its marker never landed, so
	// these pages are not a result, they are debris.
	clearRestitched(dir)

	var (
		written []string
		names   []string
	)
	err := imageproc.RenderPages(pages, width,
		func(i int) (image.Image, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return r.decodePage(ctx, sources[i])
		},
		func(n int, img image.Image) error {
			name := wholeName(n)
			if err := r.withEncoder(ctx, func() error {
				_, werr := r.writeOne("restitch", dir, name, img)
				return werr
			}); err != nil {
				return err
			}
			written = append(written, filepath.Join(dir, name))
			names = append(names, name)
			return nil
		})
	if err != nil {
		clearRestitched(dir)
		return nil, err
	}

	marker := restitchMarker{Version: restitchMarkerVersion, Pages: names}
	for _, path := range sources {
		st, serr := os.Stat(path)
		if serr != nil {
			clearRestitched(dir)
			return nil, nil
		}
		marker.Sources = append(marker.Sources, restitchSource{Name: filepath.Base(path), Size: st.Size()})
	}
	if err := writeMarker(dir, marker); err != nil {
		// The pages are there but nothing vouches for them, so they are debris
		// too: the next run must not find a result it cannot trust.
		clearRestitched(dir)
		return nil, err
	}
	return written, nil
}

// writeMarker writes the marker through a temp file and a rename, so it is
// either wholly there or not there at all.
func writeMarker(dir string, m restitchMarker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("download: restitch marker: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".restitch-*.tmp")
	if err != nil {
		return fmt.Errorf("download: restitch marker: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("download: restitch marker: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("download: restitch marker: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("download: restitch marker: close: %w", err)
	}
	return os.Rename(name, filepath.Join(dir, restitchMarkerName))
}

// clearRestitched empties the directory of pages and any marker, leaving it for
// the next attempt.
func clearRestitched(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

// decodePage decodes one page file, holding the encode semaphore while it does.
func (r *run) decodePage(ctx context.Context, path string) (image.Image, error) {
	var img image.Image
	err := r.withEncoder(ctx, func() error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		decoded, _, derr := image.Decode(f)
		if derr != nil {
			return derr
		}
		img = decoded
		return nil
	})
	return img, err
}

// sortedNames is used by the tests to assert a directory's contents.
func sortedNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}
