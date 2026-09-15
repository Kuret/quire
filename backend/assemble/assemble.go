// Package assemble turns a volume's downloaded page images into one PDF the
// stock xochitl reader opens at full bleed.
//
// Geometry is measured, not guessed (docs/DEVICE-NOTES.md §4): every page gets
// MediaBox [0 0 514 685], which is the box xochitl's own renderer emits for a
// full-bleed native page, so the reader cannot letterbox it. Page images are
// already 1620 × 2160 (or a smaller image of the same 3:4 aspect) — see
// backend/imageproc, which does the resizing at save time.
//
// # Two invariants
//
//  1. **A partial download must never produce a PDF** (PLAN §6 M4). Assembly
//     writes into a hidden .partial directory beside the destination and does
//     a single os.Rename at the end. A crash, a kill or a cancelled context
//     therefore leaves nothing at the destination path — at worst a leftover
//     under .partial, which CleanPartial removes and which nothing else reads.
//
//  2. **One PDF per volume** (volume.go), with the chapter → page-offset map
//     persisted as a manifest sidecar (manifest.go). The manifest is renamed
//     into place *after* the PDF, so its presence means "complete".
//
// pdfcpu is pure Go: the device binary is built CGO_ENABLED=0 and must stay
// static.
//
// Note on pdfcpu: its import position "full" is *not* what we want, despite
// the name. With Pos == types.Full pdfcpu sets each MediaBox to the image's
// pixel dimensions ([0 0 1620 2160]), ignoring PageDim. Pos == types.Center
// with Scale 1 keeps PageDim as the MediaBox and scales the image to fill it,
// which is the full-bleed 514 × 685 page we need.
package assemble

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/rickl/quire/backend/imageproc"
)

// The panel's page box in PDF points. See docs/DEVICE-NOTES.md §4.
const (
	MediaBoxWidthPt  = 514.0
	MediaBoxHeightPt = 685.0
)

// PartialDirName is the hidden working directory created beside the
// destination. It is a sibling so the final rename stays within one
// filesystem, which is what makes it atomic.
const PartialDirName = ".partial"

// ErrPageTooLarge reports a page image over the per-page byte budget. PLAN §8
// lists oversized images as a core-UX risk ("erode reader performance"); this
// fails the whole volume loudly rather than shipping a PDF the stock reader
// crawls through.
var ErrPageTooLarge = errors.New("assemble: page image exceeds the per-page byte budget")

// ErrNoPages reports a volume with nothing to assemble.
var ErrNoPages = errors.New("assemble: volume has no pages")

// Options tunes assembly. The zero value is usable; DefaultOptions documents
// the defaults it implies.
type Options struct {
	// MaxPageBytes rejects any page image larger than this. Zero means
	// imageproc.DefaultMaxPageBytes. Negative disables the check.
	MaxPageBytes int64

	// MaxTotalBytes rejects a volume whose page images sum above this, before
	// any work is done. Zero disables the check.
	MaxTotalBytes int64
}

// DefaultOptions returns the defaults an empty Options implies.
func DefaultOptions() Options {
	return Options{MaxPageBytes: imageproc.DefaultMaxPageBytes}
}

func (o Options) maxPageBytes() int64 {
	switch {
	case o.MaxPageBytes == 0:
		return imageproc.DefaultMaxPageBytes
	case o.MaxPageBytes < 0:
		return 0
	default:
		return o.MaxPageBytes
	}
}

// Assemble writes vol as a single PDF in dir, plus its manifest sidecar, and
// returns the manifest.
//
// It is atomic from the caller's point of view: on any error — including a
// cancelled ctx — nothing exists at the destination paths that was not there
// before.
func Assemble(ctx context.Context, dir string, vol Volume, opts Options) (*Manifest, error) {
	pageCount := vol.PageCount()
	if pageCount == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoPages, vol.Slug())
	}

	slug := vol.Slug()
	paths, offsets, err := plan(vol, opts)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}
	partial := filepath.Join(dir, PartialDirName)
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}

	tmp, err := os.CreateTemp(partial, slug+".*.pdf")
	if err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			os.Remove(tmpName)
		}
	}()

	readers, closeAll := lazyReaders(paths)
	defer closeAll()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := api.ImportImages(nil, tmp, readers, importConfig(), pdfConfig()); err != nil {
		return nil, fmt.Errorf("assemble: %s: %w", slug, err)
	}
	closeAll()

	// fsync before the rename: a rename is atomic with respect to other
	// processes, but on a device that loses power mid-write it must also be
	// durable, or the library gains a zero-length PDF on the next boot.
	if err := tmp.Sync(); err != nil {
		return nil, fmt.Errorf("assemble: sync: %w", err)
	}
	st, err := tmp.Stat()
	if err != nil {
		return nil, fmt.Errorf("assemble: stat: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("assemble: close: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m := &Manifest{
		Version:     ManifestVersion,
		Series:      vol.Series,
		Volume:      vol.Label,
		Title:       vol.Title,
		PDF:         slug + ".pdf",
		PageCount:   pageCount,
		Bytes:       st.Size(),
		WidthPt:     MediaBoxWidthPt,
		HeightPt:    MediaBoxHeightPt,
		AssembledAt: time.Now().UTC(),
		Chapters:    offsets,
	}

	// The PDF lands first, then the manifest. The manifest is the completeness
	// marker, so the only window is "PDF present, manifest absent", which
	// IsAssembled reads as not assembled.
	if err := os.Rename(tmpName, PDFPath(dir, slug)); err != nil {
		return nil, fmt.Errorf("assemble: rename: %w", err)
	}
	committed = true

	if err := writeManifest(partial, dir, slug, m); err != nil {
		os.Remove(PDFPath(dir, slug))
		return nil, err
	}
	syncDir(dir)
	return m, nil
}

// plan validates every page up front and computes the chapter offsets. Doing
// it before any writing means a volume with a missing or oversized page fails
// having produced nothing at all.
func plan(vol Volume, opts Options) ([]string, []ChapterOffset, error) {
	maxPage := opts.maxPageBytes()

	var (
		paths   []string
		offsets []ChapterOffset
		total   int64
		offset  int
	)
	for _, ch := range vol.Chapters {
		for i, p := range ch.Pages {
			if p.Index != i {
				return nil, nil, fmt.Errorf("assemble: chapter %s: page %d out of order (Index %d)", ch.ID, i, p.Index)
			}
			st, err := os.Stat(p.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("assemble: chapter %s page %d: %w", ch.ID, i, err)
			}
			if st.Size() == 0 {
				return nil, nil, fmt.Errorf("assemble: chapter %s page %d: %s is empty", ch.ID, i, p.Path)
			}
			if maxPage > 0 && st.Size() > maxPage {
				return nil, nil, fmt.Errorf("%w: chapter %s page %d (%s): %d > %d bytes",
					ErrPageTooLarge, ch.ID, i, p.Path, st.Size(), maxPage)
			}
			total += st.Size()
			paths = append(paths, p.Path)
		}
		offsets = append(offsets, ChapterOffset{
			ID:         ch.ID,
			Title:      ch.Title,
			Number:     ch.Number,
			PageOffset: offset,
			PageCount:  len(ch.Pages),
		})
		offset += len(ch.Pages)
	}

	if opts.MaxTotalBytes > 0 && total > opts.MaxTotalBytes {
		return nil, nil, fmt.Errorf("%w: volume total %d > %d bytes", ErrPageTooLarge, total, opts.MaxTotalBytes)
	}
	return paths, offsets, nil
}

// importConfig is the pdfcpu import configuration for a full-bleed panel page.
// See the package comment for why Pos is Center rather than Full.
func importConfig() *pdfcpu.Import {
	return &pdfcpu.Import{
		PageDim:  &types.Dim{Width: MediaBoxWidthPt, Height: MediaBoxHeightPt},
		UserDim:  true,
		Pos:      types.Center,
		Scale:    1,
		InpUnit:  types.POINTS,
		PageSize: "",
	}
}

func pdfConfig() *model.Configuration {
	// Never touch $HOME: the device has no pdfcpu config and writing one from
	// a backend launched by AppLoad would be a surprise.
	api.DisableConfigDir()
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}

// writeManifest writes the sidecar via the same temp-then-rename dance.
func writeManifest(partial, dir, slug string, m *Manifest) error {
	b, err := marshalManifest(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(partial, slug+".*.json")
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("assemble: manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("assemble: manifest sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("assemble: manifest close: %w", err)
	}
	if err := os.Rename(name, ManifestPath(dir, slug)); err != nil {
		return fmt.Errorf("assemble: manifest rename: %w", err)
	}
	return nil
}

// CleanPartial removes leftovers from a killed assembly. Safe to call at
// startup: nothing under .partial is ever read back.
func CleanPartial(dir string) error {
	err := os.RemoveAll(filepath.Join(dir, PartialDirName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("assemble: clean partial: %w", err)
	}
	return nil
}

// lazyReaders opens each page only when pdfcpu first reads it, and closes it
// at EOF. A volume can be hundreds of pages; opening them all up front would
// hold hundreds of descriptors for the whole run.
func lazyReaders(paths []string) ([]io.Reader, func()) {
	ls := make([]*lazyFile, len(paths))
	rs := make([]io.Reader, len(paths))
	for i, p := range paths {
		ls[i] = &lazyFile{path: p}
		rs[i] = ls[i]
	}
	return rs, func() {
		for _, l := range ls {
			l.Close()
		}
	}
}

type lazyFile struct {
	path string
	f    *os.File
	err  error
}

func (l *lazyFile) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if l.f == nil {
		f, err := os.Open(l.path)
		if err != nil {
			l.err = err
			return 0, err
		}
		l.f = f
	}
	n, err := l.f.Read(p)
	if err != nil {
		l.Close()
		l.err = err
	}
	return n, err
}

func (l *lazyFile) Close() {
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

// syncDir flushes a directory entry so a rename survives power loss. Best
// effort: some filesystems refuse to open a directory for sync.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
