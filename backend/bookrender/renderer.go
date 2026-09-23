// Package bookrender drives one `mutool run render.js` child process per open
// book, turning it into page images for Quire's own reader (PLAN's Books
// design, see /Users/rick/.claude/projects/-Users-rick/memory/quire-mupdf-spike.md
// for the measurements this is built against).
//
// # Why a long-lived child, not one invocation per page
//
// Opening and laying out an epub costs 0.4-1.4 s on the device (the spike
// memory's measured numbers); a page after that is 25-260 ms. Paying the
// open+layout cost on every single page turn would make the reader
// unusable, so one process is kept running for as long as one book is open,
// fed one request per line on stdin and answering one line on stdout — see
// render.js's own comment for the wire shape.
//
// # Untrusted input, parsed by C
//
// The file this process opens is whatever the user pointed a book source at;
// MuPDF's parsers are C, and a malformed epub or a hostile PDF is exactly the
// kind of input C parsers have historically not survived well. Renderer
// therefore treats every response as spoken by a process that might vanish
// mid-sentence: a request has a timeout (LayoutTimeout, RenderTimeout — the
// wire contract's 30 s and 10 s), and a process that times out or exits is
// killed and restarted exactly once, transparently — reopened on the last
// path and re-laid-out with the last settings before the request that
// triggered the restart is retried. A second failure in a row is not masked
// again: it surfaces to the caller as an error, which the service layer turns
// into book_render_failed or book_unreadable for the user.
package bookrender

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/creack/pty"
)

//go:embed render.js
var scriptSource []byte

// ScriptFileName is the name WriteScript gives the embedded driver on disk.
const ScriptFileName = "render.js"

// WriteScript writes the embedded render.js into dir (creating it if
// needed) and returns its path. It is written fresh on every call — a build
// that changed render.js must not leave an older copy on disk for a device
// that never re-downloaded it, which is also why this is not skipped when a
// file is already there.
func WriteScript(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("bookrender: %w", err)
	}
	path := filepath.Join(dir, ScriptFileName)
	if err := os.WriteFile(path, scriptSource, 0o644); err != nil {
		return "", fmt.Errorf("bookrender: %w", err)
	}
	return path, nil
}

// Default timeouts, per the wire contract (books-contract.md §B, Renderer).
const (
	DefaultLayoutTimeout  = 30 * time.Second
	DefaultRenderTimeout  = 10 * time.Second
	DefaultOpenTimeout    = 10 * time.Second
	DefaultTextTimeout    = 10 * time.Second
	DefaultOutlineTimeout = 10 * time.Second
)

// errCrashed and errTimeout classify a failed request: both mean "the
// process is no longer trustworthy and must be restarted", as opposed to a
// context cancellation, which is the caller changing its mind and never
// triggers a restart.
var (
	errCrashed = errors.New("bookrender: the renderer process died")
	errTimeout = errors.New("bookrender: the renderer did not answer in time")
)

// Options configures a Renderer.
type Options struct {
	// MutoolPath is the mutool executable to run. Required unless spawn is
	// set (tests only).
	MutoolPath string
	// ScriptPath is where render.js lives on disk (see WriteScript).
	ScriptPath string
	// MemoryCapKB, when positive, runs the child under `ulimit -v` (busybox
	// sh on the device) so a hostile or corrupt file cannot exhaust the
	// device's memory rather than merely failing to open. Zero disables the
	// wrapper — used by tests, where there is no real mutool to cap.
	MemoryCapKB int

	LayoutTimeout  time.Duration
	RenderTimeout  time.Duration
	OpenTimeout    time.Duration
	TextTimeout    time.Duration
	OutlineTimeout time.Duration

	Log *slog.Logger

	// spawn overrides how the child process is built, for tests: a fake
	// process implementing the render.js protocol without a real mutool
	// binary. nil (the only way production ever sets this) builds the real
	// command from MutoolPath/ScriptPath/MemoryCapKB.
	spawn func() *exec.Cmd
}

func (o *Options) setDefaults() {
	if o.LayoutTimeout <= 0 {
		o.LayoutTimeout = DefaultLayoutTimeout
	}
	if o.RenderTimeout <= 0 {
		o.RenderTimeout = DefaultRenderTimeout
	}
	if o.OpenTimeout <= 0 {
		o.OpenTimeout = DefaultOpenTimeout
	}
	if o.TextTimeout <= 0 {
		o.TextTimeout = DefaultTextTimeout
	}
	if o.OutlineTimeout <= 0 {
		o.OutlineTimeout = DefaultOutlineTimeout
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
}

// OpenResult is "open"'s answer.
type OpenResult struct {
	FixedLayout bool
	Title       string
}

// LayoutParams is one "layout" request, remembered so a restart can replay
// it.
type LayoutParams struct {
	W, H, Em float64
	CSS      string
}

// TocEntry is one row of "outline"'s answer.
type TocEntry struct {
	Title string `json:"title"`
	Page  int    `json:"page"`
	Level int    `json:"level"`
}

// Renderer drives one child process for one open book at a time. It is not
// safe for concurrent use by more than one caller — the service layer's
// book session already serialises access to it, the same way it serialises
// access to any other single-book-at-a-time resource.
type Renderer struct {
	opts Options

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	// ptmx is the pty master given to the real mutool's stdout, closed
	// alongside the process. nil for the test spawn hook. See startLocked's
	// comment for why a plain pipe does not work.
	ptmx io.Closer

	// lastPath and lastLayout are what recover() replays after a restart.
	// lastLayout is nil until Layout has succeeded at least once (a
	// fixed-layout book, or a book not yet laid out, has nothing to replay).
	lastPath    string
	fixedLayout bool
	lastLayout  *LayoutParams
}

// New builds a Renderer. It does not start a process — the first request
// does that lazily, so constructing one costs nothing when a book session is
// opened but no page has been requested yet (there is always at least an
// Open call in practice, but the laziness keeps the type honest either way).
func New(opts Options) *Renderer {
	opts.setDefaults()
	return &Renderer{opts: opts}
}

// Open starts the child (if needed) and opens path.
func (r *Renderer) Open(ctx context.Context, path string) (OpenResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := r.request(ctx, r.opts.OpenTimeout, map[string]any{"cmd": "open", "path": path})
	if err != nil {
		return OpenResult{}, err
	}
	var res struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		FixedLayout bool   `json:"fixedLayout"`
		Title       string `json:"title"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return OpenResult{}, fmt.Errorf("bookrender: unreadable open response: %w", err)
	}
	if !res.OK {
		return OpenResult{}, fmt.Errorf("%s", res.Error)
	}
	r.lastPath = path
	r.fixedLayout = res.FixedLayout
	r.lastLayout = nil
	return OpenResult{FixedLayout: res.FixedLayout, Title: res.Title}, nil
}

// Layout lays a reflowable book out at the given page size, em and CSS. It
// is a no-op that only reports the page count for a fixed-layout book — see
// render.js's own comment.
func (r *Renderer) Layout(ctx context.Context, p LayoutParams) (pages int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := r.request(ctx, r.opts.LayoutTimeout, map[string]any{
		"cmd": "layout", "w": p.W, "h": p.H, "em": p.Em, "css": p.CSS,
	})
	if err != nil {
		return 0, err
	}
	var res struct {
		Pages int    `json:"pages"`
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("bookrender: unreadable layout response: %w", err)
	}
	if res.Error != "" {
		return 0, fmt.Errorf("%s", res.Error)
	}
	pl := p
	r.lastLayout = &pl
	return res.Pages, nil
}

// Render writes page's pixmap to out as a grayscale PNG. scale of 0 lets
// render.js choose (the layout's own scale for a reflowable book, or
// whatever fits a fixed-layout page into 1620x2160).
func (r *Renderer) Render(ctx context.Context, page int, out string, scale float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req := map[string]any{"cmd": "render", "page": page, "out": out}
	if scale > 0 {
		req["scale"] = scale
	}
	raw, err := r.request(ctx, r.opts.RenderTimeout, req)
	if err != nil {
		return err
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("bookrender: unreadable render response: %w", err)
	}
	if !res.OK {
		return fmt.Errorf("%s", res.Error)
	}
	return nil
}

// Text returns page's first ~200 characters, whitespace collapsed — used to
// anchor a reading position across a re-layout (see the wire contract's
// Position section).
func (r *Renderer) Text(ctx context.Context, page int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := r.request(ctx, r.opts.TextTimeout, map[string]any{"cmd": "text", "page": page})
	if err != nil {
		return "", err
	}
	var res struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("bookrender: unreadable text response: %w", err)
	}
	if res.Error != "" {
		return "", fmt.Errorf("%s", res.Error)
	}
	return res.Text, nil
}

// Outline returns the book's table of contents, flattened with a level per
// entry. An empty slice (no error) means the book has none.
func (r *Renderer) Outline(ctx context.Context) ([]TocEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := r.request(ctx, r.opts.OutlineTimeout, map[string]any{"cmd": "outline"})
	if err != nil {
		return nil, err
	}
	var res struct {
		Toc   []TocEntry `json:"toc"`
		Error string     `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("bookrender: unreadable outline response: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("%s", res.Error)
	}
	return res.Toc, nil
}

// Close kills the child process, if one is running. It never restarts —
// there is nothing left to serve once the session that owns this Renderer is
// done with it.
func (r *Renderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.killLocked()
}

// request sends req and returns its raw response, restarting the process
// and replaying open+layout exactly once if the first attempt finds the
// process dead or unresponsive. The caller must hold r.mu.
func (r *Renderer) request(ctx context.Context, timeout time.Duration, req any) (json.RawMessage, error) {
	if r.cmd == nil {
		if err := r.startLocked(); err != nil {
			return nil, fmt.Errorf("bookrender: starting mutool: %w", err)
		}
	}
	raw, err := r.callLocked(ctx, timeout, req)
	if err == nil {
		return raw, nil
	}
	if !errors.Is(err, errCrashed) && !errors.Is(err, errTimeout) {
		// A context cancellation, or a write/marshal error unrelated to the
		// process's health: nothing here would be fixed by a restart.
		return nil, err
	}

	if recErr := r.recoverLocked(ctx); recErr != nil {
		return nil, fmt.Errorf("bookrender: the renderer died and could not restart: %w", recErr)
	}
	raw2, err2 := r.callLocked(ctx, timeout, req)
	if err2 != nil {
		return nil, fmt.Errorf("bookrender: the renderer died again after restarting: %w", err2)
	}
	return raw2, nil
}

// recoverLocked kills whatever is left of the old process, starts a fresh
// one, and replays the last open (and, for a reflowable book already laid
// out, the last layout) so the retry in request lands on a process in the
// same state the caller thought it was talking to.
func (r *Renderer) recoverLocked(ctx context.Context) error {
	r.killLocked()
	if err := r.startLocked(); err != nil {
		return err
	}
	if r.lastPath == "" {
		return nil
	}
	openReq := map[string]any{"cmd": "open", "path": r.lastPath}
	raw, err := r.callLocked(ctx, r.opts.OpenTimeout, openReq)
	if err != nil {
		return fmt.Errorf("reopening %s: %w", r.lastPath, err)
	}
	var openRes struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		FixedLayout bool   `json:"fixedLayout"`
	}
	if err := json.Unmarshal(raw, &openRes); err != nil || !openRes.OK {
		if err == nil {
			err = fmt.Errorf("%s", openRes.Error)
		}
		return fmt.Errorf("reopening %s: %w", r.lastPath, err)
	}
	r.fixedLayout = openRes.FixedLayout

	if r.lastLayout != nil && !r.fixedLayout {
		l := r.lastLayout
		layoutReq := map[string]any{"cmd": "layout", "w": l.W, "h": l.H, "em": l.Em, "css": l.CSS}
		if _, err := r.callLocked(ctx, r.opts.LayoutTimeout, layoutReq); err != nil {
			return fmt.Errorf("re-laying out %s: %w", r.lastPath, err)
		}
	}
	return nil
}

// startLocked launches the child process and wires its stdin/stdout.
//
// **The real mutool's stdout goes to a pty, not a plain pipe.** Measured
// 2026-09-23 (docs/DEVICE-NOTES.md): mutool's `print`/`write` go through
// libc's buffered stdio, and murun.c never calls setvbuf or fflush — over an
// anonymous pipe that means a response the size of one JSON line sits in the
// C library's buffer until either enough further output accumulates to fill
// it or the process exits, neither of which happens for a request/response
// child that is deliberately kept running between calls. A pty on stdout
// makes libc's own isatty() check pick line buffering instead, which is the
// standard fix for exactly this (the same trick `expect`/`unbuffer` use) and
// needs no change to mutool itself — it stays the unmodified, unpatched
// binary THIRD_PARTY.md promises. Only stdout is a pty; stdin stays a plain
// pipe, so there is no terminal echo of what Renderer writes to conflict
// with what it reads back.
//
// The test spawn hook bypasses this: a fake process written in Go writes to
// its real os.Stdout, which is not line-buffered C stdio and needs no pty to
// behave.
func (r *Renderer) startLocked() error {
	cmd := r.buildCmd()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if r.opts.spawn != nil {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		r.cmd = cmd
		r.stdin = stdin
		r.reader = bufio.NewReader(stdout)
		r.ptmx = nil
		return nil
	}

	ptmx, tty, err := pty.Open()
	if err != nil {
		return fmt.Errorf("opening a pty for mutool's stdout: %w", err)
	}
	// Only stdout goes through the pty (see the comment above), so the usual
	// reason to set raw mode — keeping keystrokes typed at a terminal from
	// being echoed or line-edited — does not apply; nothing ever writes to
	// this pty's input side. The one output transform that does matter,
	// ONLCR turning "\n" into "\r\n", is harmless here: JSON treats \r as
	// insignificant whitespace, so encoding/json.Unmarshal reads a response
	// line with a trailing \r exactly like one without.
	cmd.Stdout = tty
	if err := cmd.Start(); err != nil {
		ptmx.Close()
		tty.Close()
		return err
	}
	// The child inherited its own copy of tty; the parent's is only needed
	// long enough to hand it to Start.
	tty.Close()

	r.cmd = cmd
	r.stdin = stdin
	r.reader = bufio.NewReader(ptmx)
	r.ptmx = ptmx
	return nil
}

// buildCmd builds the child command: the real mutool, wrapped in a memory
// cap when one is configured, or the test spawn hook.
func (r *Renderer) buildCmd() *exec.Cmd {
	if r.opts.spawn != nil {
		return r.opts.spawn()
	}
	if r.opts.MemoryCapKB > 0 {
		// busybox sh on the device; /bin/sh on the Mac emulator. `exec`
		// replaces the shell rather than leaving it as a wrapper process, so
		// killing the *Cmd kills mutool itself, not an orphaned shell.
		return exec.Command("/bin/sh", "-c", `ulimit -v "$1"; exec "$2" run "$3"`,
			"sh", strconv.Itoa(r.opts.MemoryCapKB), r.opts.MutoolPath, r.opts.ScriptPath)
	}
	return exec.Command(r.opts.MutoolPath, "run", r.opts.ScriptPath)
}

// killLocked kills the current process and forgets it, tolerating one that
// is already gone.
func (r *Renderer) killLocked() error {
	if r.ptmx != nil {
		_ = r.ptmx.Close()
		r.ptmx = nil
	}
	if r.cmd == nil || r.cmd.Process == nil {
		r.cmd = nil
		return nil
	}
	_ = r.cmd.Process.Kill()
	_ = r.cmd.Wait()
	r.cmd = nil
	r.stdin = nil
	r.reader = nil
	return nil
}

// callLocked writes one request and reads one response line, honouring
// timeout and ctx. A timeout or a read/write failure kills the process
// before returning, so the caller never has to remember to.
func (r *Renderer) callLocked(ctx context.Context, timeout time.Duration, req any) (json.RawMessage, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("bookrender: %w", err)
	}
	b = append(b, '\n')

	if _, err := r.stdin.Write(b); err != nil {
		r.killLocked()
		return nil, fmt.Errorf("%w: %v", errCrashed, err)
	}

	type result struct {
		line []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := r.reader.ReadBytes('\n')
		done <- result{line, err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		r.killLocked()
		return nil, ctx.Err()
	case <-timer.C:
		r.killLocked()
		return nil, errTimeout
	case res := <-done:
		if res.err != nil {
			r.killLocked()
			return nil, fmt.Errorf("%w: %v", errCrashed, res.err)
		}
		return json.RawMessage(res.line), nil
	}
}
