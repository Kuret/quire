package download_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/imageproc"
)

// Nothing here touches a network: the Fetcher is a stub and every page image
// is drawn in process (PLAN §1.3/§1.4).

func synthJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{220, 220, 220, 255}
			if (x+y)%48 < 6 {
				c = color.RGBA{30, 30, 30, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stubFetcher serves a fixed body, records how many fetches ran at once, and
// can be made to fail specific URLs.
type stubFetcher struct {
	body  []byte
	delay time.Duration

	failURL  string
	failN    int32 // number of times failURL fails before succeeding; -1 = always
	failSeen atomic.Int32

	calls atomic.Int64

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (s *stubFetcher) Get(ctx context.Context, url string) (io.ReadCloser, error) {
	s.calls.Add(1)

	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()

	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.failURL != "" && url == s.failURL {
		if s.failN < 0 || s.failSeen.Add(1) <= s.failN {
			return nil, fmt.Errorf("stub: %s is unreachable", url)
		}
	}
	return io.NopCloser(bytes.NewReader(s.body)), nil
}

func (s *stubFetcher) peakInFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight
}

func chapters(n, pages int) []download.Chapter {
	chs := make([]download.Chapter, n)
	for i := range chs {
		chs[i] = download.Chapter{
			ID:     fmt.Sprintf("ch-%d", i+1),
			Title:  fmt.Sprintf("Chapter %d", i+1),
			Number: fmt.Sprintf("%d", i+1),
		}
		for p := range pages {
			chs[i].PageURLs = append(chs[i].PageURLs, fmt.Sprintf("https://example.invalid/%d/%d.jpg", i+1, p))
		}
	}
	return chs
}

func TestRunDownloadsEveryPage(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1400, 2000)}
	q := download.New(f, download.Options{MinFreeBytes: -1})

	out, stats, err := q.Run(t.Context(), dir, chapters(3, 5))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d chapters, want 3", len(out))
	}
	if stats.PagesDone != 15 || stats.PagesFetched != 15 {
		t.Errorf("stats = %+v, want 15 done and 15 fetched", stats.Progress)
	}
	if stats.BytesStored <= 0 || stats.BytesFetched <= 0 {
		t.Errorf("byte accounting is empty: %+v", stats.Progress)
	}

	for _, ch := range out {
		if len(ch.Pages) != 5 {
			t.Fatalf("chapter %s has %d pages", ch.ID, len(ch.Pages))
		}
		for i, p := range ch.Pages {
			if p.Index != i {
				t.Errorf("chapter %s page %d has Index %d", ch.ID, i, p.Index)
			}
			st, err := os.Stat(p.Path)
			if err != nil {
				t.Fatalf("page missing: %v", err)
			}
			if st.Size() == 0 {
				t.Errorf("%s is empty", p.Path)
			}
			// Pages are normalised at save time, not read time.
			f, err := os.Open(p.Path)
			if err != nil {
				t.Fatal(err)
			}
			cfg, format, err := image.DecodeConfig(f)
			f.Close()
			if err != nil {
				t.Fatalf("decode saved page: %v", err)
			}
			if format != "jpeg" {
				t.Errorf("saved page format = %q, want jpeg", format)
			}
			if cfg.Width > imageproc.PanelWidth || cfg.Height > imageproc.PanelHeight {
				t.Errorf("saved page is %dx%d, larger than the panel grid", cfg.Width, cfg.Height)
			}
		}
	}

	// No temp files survive a clean run.
	assertNoTempFiles(t, dir)

	t.Logf("15 pages, %d bytes stored (%.0f KiB/page), %d fetched, peak in flight %d, %s",
		stats.BytesStored, float64(stats.BytesStored)/15/1024, stats.BytesFetched, stats.MaxInFlight, stats.Elapsed)
}

// Bounded concurrency: the queue must run several fetches at once, and never
// more than configured.
func TestRunBoundsConcurrency(t *testing.T) {
	for _, limit := range []int{1, 2, 6, 12} {
		t.Run(fmt.Sprintf("limit-%d", limit), func(t *testing.T) {
			dir := t.TempDir()
			f := &stubFetcher{body: synthJPEG(t, 800, 1066), delay: 5 * time.Millisecond}
			q := download.New(f, download.Options{Concurrency: limit, MinFreeBytes: -1})

			_, stats, err := q.Run(t.Context(), dir, chapters(2, 20))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := f.peakInFlight(); got > limit {
				t.Errorf("peak in flight = %d, exceeds the limit of %d", got, limit)
			}
			if stats.MaxInFlight > limit {
				t.Errorf("stats.MaxInFlight = %d, exceeds the limit of %d", stats.MaxInFlight, limit)
			}
			// With 40 slow pages and a pool of n, the pool should actually
			// fill; a queue that serialises would peak at 1.
			if limit > 1 && f.peakInFlight() < 2 {
				t.Errorf("peak in flight = %d: the queue is not running fetches concurrently", f.peakInFlight())
			}
			t.Logf("limit %2d: peak in flight %2d, 40 pages in %s", limit, f.peakInFlight(), stats.Elapsed)
		})
	}
}

// Resume: a second Run keeps the pages already on disk and refetches only what
// is missing.
func TestRunResumes(t *testing.T) {
	dir := t.TempDir()
	body := synthJPEG(t, 1200, 1600)
	chs := chapters(2, 6)

	// First run fails partway: one page is permanently unreachable.
	f := &stubFetcher{body: body, failURL: chs[1].PageURLs[3], failN: -1}
	q := download.New(f, download.Options{Concurrency: 2, MinFreeBytes: -1, Retries: 1, RetryDelay: time.Millisecond})
	if _, _, err := q.Run(t.Context(), dir, chs); err == nil {
		t.Fatal("expected the first run to fail")
	}

	onDisk := countFiles(t, dir)
	if onDisk == 0 {
		t.Fatal("the failed run saved nothing; there is nothing to resume from")
	}
	if onDisk == 12 {
		t.Fatal("the failed run saved everything; the failure was not reached")
	}
	assertNoTempFiles(t, dir)

	// Second run, with the source healthy again.
	f2 := &stubFetcher{body: body}
	q2 := download.New(f2, download.Options{Concurrency: 2, MinFreeBytes: -1})
	out, stats, err := q2.Run(t.Context(), dir, chs)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if stats.PagesDone != 12 {
		t.Errorf("PagesDone = %d, want 12", stats.PagesDone)
	}
	if stats.PagesFetched >= 12 {
		t.Errorf("PagesFetched = %d: the resume refetched pages already on disk", stats.PagesFetched)
	}
	if stats.PagesFetched != 12-onDisk {
		t.Errorf("PagesFetched = %d, want %d (12 minus the %d already saved)", stats.PagesFetched, 12-onDisk, onDisk)
	}
	for _, ch := range out {
		for _, p := range ch.Pages {
			if st, err := os.Stat(p.Path); err != nil || st.Size() == 0 {
				t.Errorf("page %s missing after resume: %v", p.Path, err)
			}
		}
	}
	t.Logf("first run saved %d/12 pages; resume fetched the remaining %d", onDisk, stats.PagesFetched)
}

// A transient failure is retried; a page that comes back succeeds.
func TestRunRetriesTransientFailure(t *testing.T) {
	dir := t.TempDir()
	chs := chapters(1, 4)
	f := &stubFetcher{body: synthJPEG(t, 900, 1200), failURL: chs[0].PageURLs[2], failN: 2}

	q := download.New(f, download.Options{Concurrency: 2, MinFreeBytes: -1, Retries: 3, RetryDelay: time.Millisecond})
	_, stats, err := q.Run(t.Context(), dir, chs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.PagesDone != 4 {
		t.Errorf("PagesDone = %d, want 4", stats.PagesDone)
	}
	if f.calls.Load() != 6 { // 4 pages + 2 failed attempts
		t.Errorf("fetcher called %d times, want 6", f.calls.Load())
	}
}

func TestRunWarnsAtThreshold(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1620, 2160)}

	// Size the threshold off one real page, so the warning lands mid-run.
	var probe bytes.Buffer
	res, err := imageproc.Normalise(&probe, bytes.NewReader(f.body), imageproc.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	threshold := res.Bytes * 3

	var (
		mu       sync.Mutex
		warnings []int64
	)
	q := download.New(f, download.Options{
		Concurrency:  1,
		MinFreeBytes: -1,
		WarnBytes:    threshold,
		OnWarn: func(stored, th int64) {
			mu.Lock()
			defer mu.Unlock()
			if th != threshold {
				t.Errorf("OnWarn threshold = %d, want %d", th, threshold)
			}
			warnings = append(warnings, stored)
		},
	})

	_, stats, err := q.Run(t.Context(), dir, chapters(1, 8))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(warnings) != 1 {
		t.Fatalf("OnWarn called %d times, want exactly 1", len(warnings))
	}
	if warnings[0] < threshold {
		t.Errorf("warned at %d bytes, below the %d threshold", warnings[0], threshold)
	}
	if stats.BytesStored < threshold {
		t.Errorf("BytesStored = %d, should have passed the threshold", stats.BytesStored)
	}
	t.Logf("warned at %d bytes (threshold %d), finished at %d", warnings[0], threshold, stats.BytesStored)
}

func TestRunStopsAtByteCap(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1620, 2160)}

	var probe bytes.Buffer
	res, err := imageproc.Normalise(&probe, bytes.NewReader(f.body), imageproc.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	budget := res.Bytes * 2

	q := download.New(f, download.Options{Concurrency: 1, MinFreeBytes: -1, MaxBytes: budget})
	_, stats, err := q.Run(t.Context(), dir, chapters(1, 20))
	if !errors.Is(err, download.ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	if stats.PagesFetched >= 20 {
		t.Errorf("fetched %d pages: the cap did not stop the run", stats.PagesFetched)
	}
	// Whatever landed is intact and resumable — no half pages.
	assertNoTempFiles(t, dir)
	t.Logf("cap %d bytes stopped the run after %d pages (%d bytes)", budget, stats.PagesFetched, stats.BytesStored)
}

func TestRunRefusesWhenDiskIsFull(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 800, 1066)}

	q := download.New(f, download.Options{
		Concurrency:  1,
		MinFreeBytes: 512 << 20,
		FreeSpace:    func(string) (int64, error) { return 1 << 20, nil },
	})
	if _, _, err := q.Run(t.Context(), dir, chapters(1, 3)); !errors.Is(err, download.ErrDiskFull) {
		t.Fatalf("err = %v, want ErrDiskFull", err)
	}
	if n := countFiles(t, dir); n != 0 {
		t.Errorf("%d pages written despite a full disk", n)
	}
}

// Free space is read off the real filesystem on unix, so the default is not a
// stub that happens to pass.
func TestFreeSpaceIsReal(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 400, 533)}
	q := download.New(f, download.Options{Concurrency: 1})
	if _, _, err := q.Run(t.Context(), dir, chapters(1, 1)); err != nil {
		t.Fatalf("Run with the default free-space check: %v", err)
	}
}

func TestRunRejectsOversizedImage(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1620, 2160)}

	opts := download.Options{Concurrency: 1, MinFreeBytes: -1, Retries: 5, RetryDelay: time.Hour}
	opts.Image = imageproc.DefaultOptions()
	opts.Image.MaxBytes = 2048

	q := download.New(f, opts)
	// An oversized page must fail immediately rather than burn the retries —
	// refetching cannot make an image smaller.
	done := make(chan error, 1)
	go func() {
		_, _, err := q.Run(t.Context(), dir, chapters(1, 2))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, imageproc.ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("an oversized page was retried instead of failing")
	}
	assertNoTempFiles(t, dir)
}

func TestRunHonoursCancellation(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 900, 1200), delay: 20 * time.Millisecond}
	q := download.New(f, download.Options{Concurrency: 2, MinFreeBytes: -1})

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, stats, err := q.Run(ctx, dir, chapters(4, 20))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if stats.PagesDone == 80 {
		t.Error("the run finished despite cancellation")
	}
	assertNoTempFiles(t, dir)
}

func TestRunRejectsEmptyChapter(t *testing.T) {
	q := download.New(&stubFetcher{}, download.Options{MinFreeBytes: -1})
	chs := []download.Chapter{{ID: "ch-1"}}
	if _, _, err := q.Run(t.Context(), t.TempDir(), chs); !errors.Is(err, download.ErrNoPages) {
		t.Fatalf("err = %v, want ErrNoPages", err)
	}
}

func TestRunRejectsCollidingChapterIDs(t *testing.T) {
	q := download.New(&stubFetcher{}, download.Options{MinFreeBytes: -1})
	chs := []download.Chapter{
		{ID: "ch/1", PageURLs: []string{"https://example.invalid/a.jpg"}},
		{ID: "ch:1", PageURLs: []string{"https://example.invalid/b.jpg"}},
	}
	if _, _, err := q.Run(t.Context(), t.TempDir(), chs); err == nil {
		t.Fatal("expected an error for two chapter IDs that share a directory name")
	}
}

// The encode half is bounded independently of the fetch fan-out: on a
// four-core device, six fetches must not put six decoders on the CPU.
func TestEncodeWorkersBoundSeparately(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1620, 2160)}
	q := download.New(f, download.Options{Concurrency: 8, EncodeWorkers: 2, MinFreeBytes: -1})

	_, stats, err := q.Run(t.Context(), dir, chapters(1, 16))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.MaxEncoding > 2 {
		t.Errorf("MaxEncoding = %d, exceeds the 2 configured encode workers", stats.MaxEncoding)
	}
	if stats.MaxEncoding < 2 {
		t.Errorf("MaxEncoding = %d: the encode workers never both ran", stats.MaxEncoding)
	}
	if stats.MaxInFlight <= stats.MaxEncoding {
		t.Errorf("MaxInFlight = %d, MaxEncoding = %d: the fetch fan-out is being throttled to the encode limit",
			stats.MaxInFlight, stats.MaxEncoding)
	}
	t.Logf("fetch fan-out peaked at %d, encodes at %d", stats.MaxInFlight, stats.MaxEncoding)
}

// A page over the budget is re-encoded once and counted, not fatal.
func TestRunRequantisesOversizedPage(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1620, 2160)}

	probe := imageproc.DefaultOptions()
	full, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(f.body), probe)
	if err != nil {
		t.Fatal(err)
	}
	probe.Quality = 40
	probe.MaxBytes = 0
	low, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(f.body), probe)
	if err != nil {
		t.Fatal(err)
	}

	opts := download.Options{Concurrency: 2, MinFreeBytes: -1}
	opts.Image = imageproc.DefaultOptions()
	opts.Image.RetryQuality = 40
	opts.Image.MaxBytes = (full.Bytes + low.Bytes) / 2

	var (
		mu       sync.Mutex
		reported []string
	)
	opts.OnRequantise = func(url string, res imageproc.Result) {
		mu.Lock()
		defer mu.Unlock()
		if !res.Requantised || res.FirstBytes <= opts.Image.MaxBytes {
			t.Errorf("OnRequantise got %+v", res)
		}
		reported = append(reported, url)
	}

	q := download.New(f, opts)
	out, stats, err := q.Run(t.Context(), dir, chapters(1, 4))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.PagesRequantised != 4 {
		t.Errorf("PagesRequantised = %d, want 4", stats.PagesRequantised)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 4 {
		t.Errorf("OnRequantise called %d times, want 4", len(reported))
	}
	for _, ch := range out {
		for _, p := range ch.Pages {
			st, err := os.Stat(p.Path)
			if err != nil {
				t.Fatal(err)
			}
			if st.Size() > opts.Image.MaxBytes {
				t.Errorf("%s is %d bytes, over the %d budget", p.Path, st.Size(), opts.Image.MaxBytes)
			}
		}
	}
}

// The soft memory limit is the fix for the device OOM, so it must actually be
// applied — and must not override an operator's GOMEMLIMIT.
func TestSetMemoryLimit(t *testing.T) {
	restore := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(restore) })

	t.Setenv("GOMEMLIMIT", "")
	if got := download.SetMemoryLimit(); got != download.RecommendedMemoryLimit {
		t.Errorf("SetMemoryLimit() = %d, want the recommended %d", got, download.RecommendedMemoryLimit)
	}
	if got := debug.SetMemoryLimit(-1); got != download.RecommendedMemoryLimit {
		t.Errorf("runtime limit = %d, want %d", got, download.RecommendedMemoryLimit)
	}

	// With GOMEMLIMIT set, the operator's choice stands. The runtime parsed it
	// at startup, so all this must do is leave it alone.
	t.Setenv("GOMEMLIMIT", "900MiB")
	before := debug.SetMemoryLimit(-1)
	if got := download.SetMemoryLimit(); got != before {
		t.Errorf("SetMemoryLimit() = %d with GOMEMLIMIT set, want the existing %d", got, before)
	}
}

// Building a queue applies the soft limit, because the thing that gets
// OOM-killed is the process that builds one.
func TestNewAppliesMemoryLimit(t *testing.T) {
	restore := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(restore) })
	t.Setenv("GOMEMLIMIT", "")
	debug.SetMemoryLimit(math.MaxInt64)
	download.ResetMemoryLimitOnce()

	download.New(&stubFetcher{}, download.Options{})
	if got := debug.SetMemoryLimit(-1); got > download.RecommendedMemoryLimit {
		t.Errorf("limit after New = %d, want at most %d", got, download.RecommendedMemoryLimit)
	}
}

func TestDefaultConcurrency(t *testing.T) {
	if got := download.New(&stubFetcher{}, download.Options{}).Concurrency(); got != download.DefaultConcurrency {
		t.Errorf("default concurrency = %d, want %d", got, download.DefaultConcurrency)
	}
	if download.DefaultConcurrency != 6 {
		t.Errorf("DefaultConcurrency = %d; PLAN §6 M4 says start at 6", download.DefaultConcurrency)
	}
	if got := download.New(&stubFetcher{}, download.Options{}).EncodeWorkers(); got != download.DefaultEncodeWorkers {
		t.Errorf("default encode workers = %d, want %d", got, download.DefaultEncodeWorkers)
	}
}

// Measures wall-clock throughput across concurrency settings against a fetcher
// with a realistic per-request latency, so "start at 6 and measure" has
// numbers behind it.
func TestReportConcurrencyScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("timing measurement")
	}
	body := synthJPEG(t, 1620, 2160)
	for _, limit := range []int{1, 2, 4, 6, 8, 12} {
		dir := t.TempDir()
		f := &stubFetcher{body: body, delay: 40 * time.Millisecond} // a plausible page RTT
		q := download.New(f, download.Options{Concurrency: limit, MinFreeBytes: -1})
		_, stats, err := q.Run(t.Context(), dir, chapters(2, 24))
		if err != nil {
			t.Fatalf("limit %d: %v", limit, err)
		}
		t.Logf("concurrency %2d: 48 pages in %6s (%5.1f pages/s), peak in flight %2d, %d bytes",
			limit, stats.Elapsed.Round(time.Millisecond),
			float64(stats.PagesDone)/stats.Elapsed.Seconds(), f.peakInFlight(), stats.BytesStored)
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".jpg" {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".tmp" {
			t.Errorf("temp file left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
