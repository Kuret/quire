package assemble

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ManifestVersion is bumped when the on-disk shape changes incompatibly.
const ManifestVersion = 1

// ManifestSuffix is appended to the volume slug for the sidecar file.
const ManifestSuffix = ".quire.json"

// ChapterOffset maps one chapter to its position inside the volume PDF.
//
// This is a first-class, persisted result of assembly rather than a side
// effect: M6 ("open at the right page") consumes exactly this, and it is the
// only thing that makes one-PDF-per-volume usable — without it, "read chapter
// 14" has no answer.
type ChapterOffset struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Number     string `json:"number,omitempty"`
	PageOffset int    `json:"pageOffset"` // 0-based index of the chapter's first page
	PageCount  int    `json:"pageCount"`
}

// Manifest describes one assembled volume PDF. It is written *after* the PDF
// is renamed into place, so its presence is the marker that the PDF is
// complete: a PDF with no manifest is a leftover and must be reassembled.
type Manifest struct {
	Version     int             `json:"version"`
	Series      string          `json:"series"`
	Volume      string          `json:"volume"`
	Title       string          `json:"title"`
	PDF         string          `json:"pdf"` // base name, in the same directory
	PageCount   int             `json:"pageCount"`
	Bytes       int64           `json:"bytes"`
	WidthPt     float64         `json:"widthPt"`
	HeightPt    float64         `json:"heightPt"`
	AssembledAt time.Time       `json:"assembledAt"`
	Chapters    []ChapterOffset `json:"chapters"`
}

// PageFor returns the 0-based page index of a chapter's first page.
func (m *Manifest) PageFor(chapterID string) (int, bool) {
	for _, c := range m.Chapters {
		if c.ID == chapterID {
			return c.PageOffset, true
		}
	}
	return 0, false
}

// marshalManifest renders a manifest as indented JSON with a trailing
// newline, so a sidecar is readable in a terminal on the device.
func marshalManifest(m *Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("assemble: manifest: %w", err)
	}
	return append(b, '\n'), nil
}

// ManifestPath is where the sidecar for slug lives.
func ManifestPath(dir, slug string) string {
	return filepath.Join(dir, slug+ManifestSuffix)
}

// PDFPath is where the assembled PDF for slug lives.
func PDFPath(dir, slug string) string {
	return filepath.Join(dir, slug+".pdf")
}

// LoadManifest reads a volume's manifest. It reports os.ErrNotExist when the
// volume has not been assembled.
func LoadManifest(dir, slug string) (*Manifest, error) {
	b, err := os.ReadFile(ManifestPath(dir, slug))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("assemble: manifest %s: %w", slug, err)
	}
	if m.Version != ManifestVersion {
		return nil, fmt.Errorf("assemble: manifest %s: version %d, want %d", slug, m.Version, ManifestVersion)
	}
	return &m, nil
}

// IsAssembled reports whether dir holds a complete volume for slug: a manifest
// and the PDF it names.
func IsAssembled(dir, slug string) bool {
	m, err := LoadManifest(dir, slug)
	if err != nil {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, m.PDF))
	return err == nil && st.Size() == m.Bytes
}
