// Package covers is the on-disk cover thumbnail cache for M3's series grid.
//
// PLAN §7.1: images are never sent over the AppLoad socket. The backend fetches
// a cover, downscales it hard, writes it under the cache directory and sends
// the *path*; QML loads it from disk. PLAN §6 M3 adds the memory rule — never
// hold more than a screenful — which falls out of this design: the backend
// holds one image at a time, and QML's Image cache holds what the view is
// showing.
package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rickl/quire/backend/imageproc"
	"github.com/rickl/quire/backend/theme"
)

// Thumbnail size. The grid shows six columns at most on a 1620px panel, so a
// 300px-wide thumbnail is already generous; the 2:3 ratio is the shape almost
// every cover is published at. Greyscale because the panel is.
const (
	ThumbWidth   = 300
	ThumbHeight  = 450
	ThumbQuality = 70
)

// MaxSourceBytes is how much of an original cover we will read. A cover that
// wants more than this is not a cover.
const MaxSourceBytes = 4 << 20

// Cache turns a remote cover URL into a local thumbnail path.
type Cache struct {
	dir string
	f   theme.Fetcher

	// inflight collapses concurrent requests for the same cover, which the
	// grid produces by scrolling: without it a flick down and back refetches
	// everything it already has in hand.
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// New builds a Cache writing under dir.
func New(dir string, f theme.Fetcher) *Cache {
	return &Cache{dir: dir, f: f, inflight: map[string]chan struct{}{}}
}

// Dir is the cache directory.
func (c *Cache) Dir() string { return c.dir }

// Path returns the local path of the thumbnail for rawurl, fetching and
// downscaling it if it is not cached yet.
//
// The cover is fetched through the source's own policy, so it is rate-limited,
// robots-checked and SSRF-guarded like every other request (PLAN §7.4). A cover
// on an image CDN needs that CDN in the source's allowedHosts, which is exactly
// the check we want rather than a hole to route around.
func (c *Cache) Path(ctx context.Context, src *theme.Source, rawurl string) (string, error) {
	if rawurl == "" {
		return "", fmt.Errorf("covers: no cover URL")
	}
	path := c.pathFor(src.ID, rawurl)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// One fetch per cover, however many callers ask.
	c.mu.Lock()
	wait, busy := c.inflight[path]
	if busy {
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("covers: %s could not be fetched", rawurl)
	}
	done := make(chan struct{})
	c.inflight[path] = done
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, path)
		c.mu.Unlock()
		close(done)
	}()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("covers: %w", err)
	}

	pol, err := src.Policy()
	if err != nil {
		return "", fmt.Errorf("covers: %w", err)
	}
	resp, err := c.f.Get(ctx, pol, rawurl)
	if err != nil {
		return "", fmt.Errorf("covers: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("covers: %s answered %d", rawurl, resp.StatusCode)
	}
	if len(resp.Body) > MaxSourceBytes {
		return "", fmt.Errorf("covers: %s is %d bytes, more than a cover should be", rawurl, len(resp.Body))
	}

	var buf bytes.Buffer
	opts := imageproc.DefaultOptions()
	opts.MaxWidth, opts.MaxHeight = ThumbWidth, ThumbHeight
	opts.Quality = ThumbQuality
	opts.Grayscale = true
	if _, err := imageproc.Normalise(&buf, bytes.NewReader(resp.Body), opts); err != nil {
		return "", fmt.Errorf("covers: %w", err)
	}

	// Written via a temp file so a half-written thumbnail is never left behind
	// for QML to load as a grey rectangle.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("covers: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("covers: %w", err)
	}
	return path, nil
}

// pathFor is deterministic, so a restart reuses what is already on disk, and
// per-source, so removing a source can drop its covers in one directory.
func (c *Cache) pathFor(sourceID, rawurl string) string {
	sum := sha256.Sum256([]byte(rawurl))
	return filepath.Join(c.dir, safe(sourceID), hex.EncodeToString(sum[:8])+".jpg")
}

// Forget deletes one source's cached covers.
func (c *Cache) Forget(sourceID string) error {
	return os.RemoveAll(filepath.Join(c.dir, safe(sourceID)))
}

// safe keeps a source ID from escaping the cache directory. IDs are already
// constrained by schema/source.schema.json; this is the second lock, because
// the cost of being wrong is writing outside the cache.
func safe(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}
