// Package logging writes Quire's structured log to a size-capped file on the
// device, and can read the tail of it back for the in-app viewer.
//
// PLAN §6 M7 calls SSH-free debugging "worth the hour", and this session is the
// evidence: every bug so far — the 100 MB upload cap, the startup race, the
// OOM — was diagnosed by reading journalctl over SSH, which is not something
// the person using the tablet can do. A log the app can show itself turns "it
// stopped working" into something a user can quote.
//
// # Two caps, both hard
//
// The device has ~29 GB free on /home, which is plenty of room for a runaway
// log to become its own bug on a machine that is awkward to clean out. So the
// file rotates at MaxBytes and exactly one previous file is kept: the worst
// case is bounded at 2 × MaxBytes, permanently, with no cleanup job to forget
// to run.
//
// The second cap is on reading. The tail goes over an AppLoad socket, which is
// SOCK_SEQPACKET — a whole message must fit one datagram, and §3.1 measured
// SO_SNDBUF rejecting 4 MiB on this device. Tail therefore bounds what it
// returns by lines *and* by bytes, and returns the most recent lines, which are
// the ones anyone diagnosing a problem actually wants.
package logging

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileName is the current log; FileName + ".1" is the previous one.
const FileName = "quire.log"

// MaxBytes is the size at which the log rotates. Two files are kept, so the
// worst case on disk is twice this.
const MaxBytes int64 = 1 << 20

// MaxTailLines and MaxTailBytes bound what the in-app viewer is given.
const (
	MaxTailLines = 400
	MaxTailBytes = 128 << 10
)

// Writer is an io.Writer that rotates at MaxBytes. It is safe for concurrent
// use, which it has to be: slog handlers write from every goroutine.
type Writer struct {
	dir      string
	maxBytes int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// NewWriter opens (or creates) the log in dir.
func NewWriter(dir string) (*Writer, error) {
	return NewWriterWithLimit(dir, MaxBytes)
}

// NewWriterWithLimit is NewWriter with the rotation threshold overridden, for
// tests that would otherwise have to write a megabyte to prove anything.
func NewWriterWithLimit(dir string, maxBytes int64) (*Writer, error) {
	if maxBytes <= 0 {
		maxBytes = MaxBytes
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}
	w := &Writer{dir: dir, maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	path := filepath.Join(w.dir, FileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logging: %w", err)
	}
	w.f, w.size = f, st.Size()
	return nil
}

// Path is the current log file.
func (w *Writer) Path() string { return filepath.Join(w.dir, FileName) }

// Write implements io.Writer, rotating first when the record would take the
// file past the limit.
//
// Rotation happens *before* the write rather than after, so the cap is never
// exceeded even briefly — a log line can be long, and "we go over and then tidy
// up" is how a size cap turns out not to be one.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f == nil {
		return 0, fmt.Errorf("logging: the log is closed")
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate moves the current file aside, replacing whatever was there. Exactly
// one generation is kept: two files of a known size is a bound, and a numbered
// series of them is a directory that grows.
func (w *Writer) rotate() error {
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	current := filepath.Join(w.dir, FileName)
	if err := os.Rename(current, current+".1"); err != nil {
		// A failed rotation must not take logging down with it: start a new
		// file over the old one rather than losing the ability to log at all.
		_ = os.Remove(current)
	}
	return w.open()
}

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Tail returns up to maxLines of the most recent log, oldest first, bounded by
// MaxTailBytes.
//
// It reads the rotated file too, so a tail taken just after a rotation is not
// nearly empty — which is exactly when something interesting has just happened.
func Tail(dir string, maxLines int) ([]string, error) {
	if maxLines <= 0 || maxLines > MaxTailLines {
		maxLines = MaxTailLines
	}

	var lines []string
	for _, name := range []string{FileName + ".1", FileName} {
		got, err := readLines(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		lines = append(lines, got...)
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	// Byte budget, newest first: a single enormous line must not crowd out
	// everything else, and the socket has a hard datagram limit (§3.1).
	total := 0
	cut := 0
	for i := len(lines) - 1; i >= 0; i-- {
		total += len(lines[i]) + 1
		if total > MaxTailBytes {
			cut = i + 1
			break
		}
	}
	return lines[cut:], nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	// A log line can be long — a URL plus an error — and the default 64 KiB
	// scanner limit would turn one into a read failure.
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if line := strings.TrimRight(sc.Text(), "\r"); line != "" {
			out = append(out, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("logging: reading %s: %w", path, err)
	}
	return out, nil
}

// Handler builds the slog handler Quire logs through: text, to both the given
// writer and the file, so the journal keeps working for anyone with SSH while
// the file serves the person holding the tablet.
func Handler(w io.Writer, file io.Writer, level slog.Level) slog.Handler {
	return slog.NewTextHandler(io.MultiWriter(w, file), &slog.HandlerOptions{Level: level})
}
