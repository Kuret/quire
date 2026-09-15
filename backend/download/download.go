// Package download is the page queue: it fetches a volume's page images with
// bounded concurrency, normalises each one to the panel grid as it is saved,
// and hands back the assemble.Chapter values the PDF assembler consumes.
//
// # What this package guarantees
//
//   - **Resumability.** Wifi drops and device sleep are expected (PLAN §6 M7).
//     Every page is written to a temp file and renamed into place only once it
//     is complete, so a page file that exists is a page file that is whole.
//     Resuming is therefore just "skip the pages already on disk" — there is no
//     separate state file to fall out of step with the filesystem.
//
//   - **Two concurrency knobs, not one.** Fetch fan-out (Concurrency) and
//     encode workers (EncodeWorkers) are bounded separately: a fetch is
//     latency-bound and free locally, an encode pegs a core. See
//     DefaultEncodeWorkers.
//
//   - **Byte accounting.** Stored bytes are counted as they land. Crossing
//     WarnBytes calls OnWarn once; crossing MaxBytes fails the run. Before each
//     page the free space on the target filesystem is checked against
//     MinFreeBytes, so a download stops rather than filling the partition.
//
//     Note the shape of the device's disks (docs/DEVICE-NOTES.md §3.3): /home
//     has ~29 GB free but / has ~47 MB. Callers must pass a directory under
//     /home; the free-space check catches the mistake but is not a substitute
//     for the caller getting it right.
//
// # The Fetcher dependency
//
// This package deliberately does not import backend/fetch. It declares the
// narrow Fetcher interface below and takes it as a dependency, so the queue can
// be tested with an in-process stub and never needs a network. The polite,
// guarded HTTP client (PLAN §7.4: rate limits, robots, backoff, SSRF guard) is
// the fetch layer's job and stays there — retries here are only the coarse
// "the wifi came back" kind.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/imageproc"
)

// Fetcher retrieves a page image. It is owned by this package: whatever
// implements it is responsible for the fetch-layer invariants of PLAN §7.4.
type Fetcher interface {
	Get(ctx context.Context, url string) (io.ReadCloser, error)
}

// DefaultConcurrency is the fetch fan-out PLAN §6 M4 prescribes: six page
// requests in flight.
const DefaultConcurrency = 6

// DefaultEncodeWorkers is how many pages are decoded, resized and re-encoded
// at once.
//
// Fetching and encoding are bounded separately because they are bounded by
// different things. A fetch is latency-bound and costs nothing locally; an
// encode pegs a core for the best part of a second on this hardware
// (docs/DEVICE-NOTES.md §10). Running six encoders on a four-core i.MX8MM that
// is also running xochitl buys no throughput and costs interactive latency in
// the reader, so the default leaves cores free.
const DefaultEncodeWorkers = 2

// DefaultMinFreeBytes is how much room the target filesystem must keep. A
// volume is a few hundred MB; leaving 512 MiB means a download stops well
// before anything else on the device notices.
const DefaultMinFreeBytes = 512 << 20

// Errors a caller is expected to distinguish.
var (
	// ErrBudgetExceeded reports the configured total-bytes cap being hit.
	ErrBudgetExceeded = errors.New("download: total byte budget exceeded")

	// ErrDiskFull reports the target filesystem falling below MinFreeBytes.
	ErrDiskFull = errors.New("download: not enough free space")

	// ErrNoPages reports a chapter with no page URLs.
	ErrNoPages = errors.New("download: chapter has no pages")
)

// Chapter is one chapter's worth of page URLs, in reading order.
type Chapter struct {
	ID       string
	Title    string
	Number   string
	Volume   string
	PageURLs []string
}

// Progress is reported after each page completes.
type Progress struct {
	PagesDone        int   // pages on disk, including ones skipped as already present
	PagesTotal       int   // pages in this run
	PagesFetched     int   // pages actually fetched over the network this run
	PagesRequantised int   // pages re-encoded at the lower quality to fit the budget
	BytesStored      int64 // bytes written to disk this run
	BytesFetched     int64 // bytes read from the fetcher this run
}

// Options configures a Queue. The zero value is usable and implies the
// defaults documented on each field.
type Options struct {
	// Concurrency is the number of page fetches in flight. Zero means
	// DefaultConcurrency.
	Concurrency int

	// EncodeWorkers is the number of pages being decoded, resized and encoded
	// at once. Zero means DefaultEncodeWorkers.
	EncodeWorkers int

	// Image controls normalisation. The zero value means
	// imageproc.DefaultOptions.
	Image imageproc.Options

	// WarnBytes triggers OnWarn once, when stored bytes first cross it. Zero
	// disables the warning.
	WarnBytes int64

	// MaxBytes fails the run with ErrBudgetExceeded. Zero disables the cap.
	MaxBytes int64

	// MinFreeBytes is the free space the target filesystem must retain. Zero
	// means DefaultMinFreeBytes; negative disables the check.
	MinFreeBytes int64

	// Retries is the number of extra attempts per page. Zero means 2.
	// Transport-level backoff belongs to the fetch layer; this is the coarse
	// retry that covers a link going away mid-volume.
	Retries int

	// RetryDelay is the pause between attempts. Zero means 2s.
	RetryDelay time.Duration

	// OnProgress, if set, is called after each page. It is called from worker
	// goroutines but serialised, so it need not be safe for concurrent use —
	// it must, however, not block for long.
	OnProgress func(Progress)

	// OnWarn, if set, is called once when stored bytes cross WarnBytes.
	OnWarn func(bytesStored, threshold int64)

	// OnRequantise, if set, is called for each page that busted the per-page
	// byte budget and was re-encoded at the lower quality. A source that
	// triggers it on every page is systematically oversized and should be
	// visible rather than silently degraded.
	OnRequantise func(url string, res imageproc.Result)

	// FreeSpace reports free bytes on the filesystem holding a directory.
	// Defaults to a statfs call; overridable for tests.
	FreeSpace func(dir string) (int64, error)

	// Sleep is the retry delay, overridable for tests.
	Sleep func(ctx context.Context, d time.Duration)
}

func (o *Options) applyDefaults() {
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}
	if o.EncodeWorkers <= 0 {
		o.EncodeWorkers = DefaultEncodeWorkers
	}
	if o.Image.MaxWidth == 0 && o.Image.MaxHeight == 0 {
		o.Image = imageproc.DefaultOptions()
	}
	if o.MinFreeBytes == 0 {
		o.MinFreeBytes = DefaultMinFreeBytes
	} else if o.MinFreeBytes < 0 {
		o.MinFreeBytes = 0
	}
	if o.Retries == 0 {
		o.Retries = 2
	} else if o.Retries < 0 {
		o.Retries = 0
	}
	if o.RetryDelay == 0 {
		o.RetryDelay = 2 * time.Second
	}
	if o.FreeSpace == nil {
		o.FreeSpace = freeSpace
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
}

// Queue downloads pages. It is safe to reuse across runs; each Run has its own
// accounting.
type Queue struct {
	fetcher Fetcher
	opts    Options
}

// New returns a queue that fetches through f.
func New(f Fetcher, opts Options) *Queue {
	opts.applyDefaults()
	return &Queue{fetcher: f, opts: opts}
}

// Concurrency reports the configured number of in-flight fetches.
func (q *Queue) Concurrency() int { return q.opts.Concurrency }

// EncodeWorkers reports the configured number of concurrent encodes.
func (q *Queue) EncodeWorkers() int { return q.opts.EncodeWorkers }

// Stats summarises a run.
type Stats struct {
	Progress
	// MaxInFlight is the highest number of fetches observed running at once.
	// It exists so "start at 6 and measure" can be measured rather than
	// asserted.
	MaxInFlight int

	// MaxEncoding is the highest number of concurrent encodes observed.
	MaxEncoding int

	Elapsed time.Duration
}

// Run downloads every page of every chapter into dir and returns the chapters
// in assemble.Chapter form, ready to hand to assemble.Assemble.
//
// Pages already present from an earlier run are kept, not refetched. On error
// the pages that did complete stay on disk, so a later Run resumes rather than
// starting over.
func (q *Queue) Run(ctx context.Context, dir string, chapters []Chapter) ([]assemble.Chapter, Stats, error) {
	start := time.Now()
	var stats Stats

	jobs, out, err := q.planJobs(dir, chapters)
	if err != nil {
		return nil, stats, err
	}
	stats.PagesTotal = len(jobs)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, stats, fmt.Errorf("download: %w", err)
	}

	r := &run{
		q:       q,
		dir:     dir,
		total:   len(jobs),
		warned:  q.opts.WarnBytes <= 0,
		encoder: make(chan struct{}, q.opts.EncodeWorkers),
	}

	err = r.execute(ctx, jobs)

	stats.Progress = r.snapshot()
	stats.MaxInFlight = int(r.maxInFlight.Load())
	stats.MaxEncoding = int(r.maxEncoding.Load())
	stats.Elapsed = time.Since(start)
	if err != nil {
		return nil, stats, err
	}
	return out, stats, nil
}

// job is one page to fetch.
type job struct {
	url  string
	path string
	// skip is set when the page is already on disk from an earlier run.
	skip bool
}

// planJobs decides the on-disk layout and works out what is left to do.
func (q *Queue) planJobs(dir string, chapters []Chapter) ([]job, []assemble.Chapter, error) {
	var (
		jobs []job
		out  []assemble.Chapter
	)
	seen := map[string]bool{}
	for _, ch := range chapters {
		if len(ch.PageURLs) == 0 {
			return nil, nil, fmt.Errorf("%w: %s", ErrNoPages, ch.ID)
		}
		sub := slug(ch.ID)
		if seen[sub] {
			return nil, nil, fmt.Errorf("download: two chapters map to the directory %q; chapter IDs must be unique", sub)
		}
		seen[sub] = true

		ac := assemble.Chapter{ID: ch.ID, Title: ch.Title, Number: ch.Number, Volume: ch.Volume}
		for i, u := range ch.PageURLs {
			path := filepath.Join(dir, sub, fmt.Sprintf("%04d.jpg", i))
			st, err := os.Stat(path)
			jobs = append(jobs, job{url: u, path: path, skip: err == nil && st.Size() > 0})
			ac.Pages = append(ac.Pages, assemble.Page{Path: path, Index: i})
		}
		out = append(out, ac)
	}
	return jobs, out, nil
}

// run holds one Run's mutable state.
type run struct {
	q     *Queue
	dir   string
	total int

	mu     sync.Mutex
	warned bool

	pagesDone        atomic.Int64
	pagesFetched     atomic.Int64
	pagesRequantised atomic.Int64
	bytesStored      atomic.Int64
	bytesFetched     atomic.Int64

	inFlight    atomic.Int64
	maxInFlight atomic.Int64

	// encoder bounds the CPU-heavy half independently of the fetch fan-out.
	encoder     chan struct{}
	encoding    atomic.Int64
	maxEncoding atomic.Int64
}

func (r *run) snapshot() Progress {
	return Progress{
		PagesDone:        int(r.pagesDone.Load()),
		PagesTotal:       r.total,
		PagesFetched:     int(r.pagesFetched.Load()),
		PagesRequantised: int(r.pagesRequantised.Load()),
		BytesStored:      r.bytesStored.Load(),
		BytesFetched:     r.bytesFetched.Load(),
	}
}

// execute runs the bounded worker pool. The first hard failure cancels the
// rest: a page that cannot be had is not going to be had by fetching the next
// two hundred, and everything already written stays on disk for the resume.
func (r *run) execute(ctx context.Context, jobs []job) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan job)
	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		runErr  error
	)
	fail := func(err error) {
		errOnce.Do(func() {
			runErr = err
			cancel()
		})
	}

	for range r.q.opts.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				if ctx.Err() != nil {
					return
				}
				if err := r.do(ctx, j); err != nil {
					fail(err)
					return
				}
			}
		}()
	}

	for _, j := range jobs {
		select {
		case ch <- j:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(ch)
	wg.Wait()

	if runErr != nil {
		return runErr
	}
	return ctx.Err()
}

// do handles one page: the accounting, the guards, the fetch and the atomic
// save.
func (r *run) do(ctx context.Context, j job) error {
	if j.skip {
		r.pagesDone.Add(1)
		r.report()
		return nil
	}

	if err := r.checkSpace(); err != nil {
		return err
	}
	if max := r.q.opts.MaxBytes; max > 0 && r.bytesStored.Load() >= max {
		return fmt.Errorf("%w: %d bytes", ErrBudgetExceeded, r.bytesStored.Load())
	}

	n := r.inFlight.Add(1)
	observeMax(&r.maxInFlight, n)
	defer r.inFlight.Add(-1)

	var lastErr error
	for attempt := 0; attempt <= r.q.opts.Retries; attempt++ {
		if attempt > 0 {
			r.q.opts.Sleep(ctx, r.q.opts.RetryDelay)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		stored, fetched, err := r.fetchPage(ctx, j)
		if err == nil {
			r.bytesStored.Add(stored)
			r.bytesFetched.Add(fetched)
			r.pagesFetched.Add(1)
			r.pagesDone.Add(1)
			r.checkWarn()
			r.report()
			if max := r.q.opts.MaxBytes; max > 0 && r.bytesStored.Load() > max {
				return fmt.Errorf("%w: %d > %d bytes", ErrBudgetExceeded, r.bytesStored.Load(), max)
			}
			return nil
		}
		// A cancelled context or a rejected image is not worth retrying.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, imageproc.ErrTooLarge) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("download: %s: %w", j.url, lastErr)
}

// fetchPage fetches one page and writes it, normalised, via temp-and-rename.
// It returns the stored and fetched byte counts.
func (r *run) fetchPage(ctx context.Context, j job) (stored, fetched int64, err error) {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return 0, 0, fmt.Errorf("download: %w", err)
	}

	body, err := r.q.fetcher.Get(ctx, j.url)
	if err != nil {
		return 0, 0, err
	}
	defer body.Close()
	counted := &countingReader{r: body}

	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".page-*.tmp")
	if err != nil {
		return 0, 0, fmt.Errorf("download: %w", err)
	}
	name := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			os.Remove(name)
		}
	}()

	res, err := r.normalise(ctx, tmp, counted)
	if err != nil {
		return 0, counted.n, err
	}
	if res.Requantised {
		r.pagesRequantised.Add(1)
		if r.q.opts.OnRequantise != nil {
			r.mu.Lock()
			r.q.opts.OnRequantise(j.url, res)
			r.mu.Unlock()
		}
	}
	if err := tmp.Sync(); err != nil {
		return 0, counted.n, fmt.Errorf("download: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, counted.n, fmt.Errorf("download: close: %w", err)
	}
	if err := os.Rename(name, j.path); err != nil {
		return 0, counted.n, fmt.Errorf("download: rename: %w", err)
	}
	committed = true
	return res.Bytes, counted.n, nil
}

// normalise runs the CPU-heavy half under the encode semaphore, so a large
// fetch fan-out cannot put more decoders on the CPU than configured.
func (r *run) normalise(ctx context.Context, dst io.Writer, src io.Reader) (imageproc.Result, error) {
	select {
	case r.encoder <- struct{}{}:
	case <-ctx.Done():
		return imageproc.Result{}, ctx.Err()
	}
	defer func() { <-r.encoder }()

	observeMax(&r.maxEncoding, r.encoding.Add(1))
	defer r.encoding.Add(-1)

	return imageproc.Normalise(dst, src, r.q.opts.Image)
}

// observeMax raises m to n if n is larger.
func observeMax(m *atomic.Int64, n int64) {
	for {
		cur := m.Load()
		if n <= cur || m.CompareAndSwap(cur, n) {
			return
		}
	}
}

func (r *run) checkSpace() error {
	if r.q.opts.MinFreeBytes <= 0 {
		return nil
	}
	free, err := r.q.opts.FreeSpace(r.dir)
	if err != nil {
		// An unreadable filesystem is not a reason to stop downloading; it is
		// a reason to stop *claiming* to know the free space.
		return nil
	}
	if free < r.q.opts.MinFreeBytes {
		return fmt.Errorf("%w: %d bytes free on the filesystem holding %s, need %d",
			ErrDiskFull, free, r.dir, r.q.opts.MinFreeBytes)
	}
	return nil
}

func (r *run) checkWarn() {
	if r.q.opts.WarnBytes <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.warned || r.bytesStored.Load() < r.q.opts.WarnBytes {
		return
	}
	r.warned = true
	if r.q.opts.OnWarn != nil {
		r.q.opts.OnWarn(r.bytesStored.Load(), r.q.opts.WarnBytes)
	}
}

func (r *run) report() {
	if r.q.opts.OnProgress == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.q.opts.OnProgress(r.snapshot())
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// slug turns a source-side chapter ID into a directory name. Source IDs are
// untrusted strings that routinely contain slashes and query fragments.
func slug(id string) string {
	s := strings.Trim(unsafeName.ReplaceAllString(id, "-"), "-.")
	if s == "" {
		s = "chapter"
	}
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-.")
	}
	return s
}
