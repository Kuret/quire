package logging_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/logging"
)

// The cap has to be a cap. /home has ~29 GB free, which is exactly enough room
// for a runaway log to become its own bug on a device that is awkward to clean
// out, so the bound is two files of a known size and nothing else.
func TestTheLogIsBoundedByTwoFiles(t *testing.T) {
	dir := t.TempDir()
	w, err := logging.NewWriterWithLimit(dir, 2048)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Deliberately awkward: lines of wildly different lengths, including one
	// longer than the whole rotation threshold. Uniform input is how a size cap
	// passes a test and fails in the field.
	sizes := []int{10, 700, 3000, 40, 1200, 5, 900}
	for i, n := range sizes {
		line := fmt.Sprintf("%04d %s\n", i, strings.Repeat("x", n))
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	if len(entries) > 2 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("%d log files (%v), want at most 2", len(entries), names)
	}
	// One over-long line can exceed the threshold on its own; the bound that
	// matters is the total, and it must stay near 2x.
	if total > 3*2048 {
		t.Errorf("logs total %d bytes against a 2048-byte rotation threshold", total)
	}
}

// A tail taken just after a rotation must not be nearly empty: that is exactly
// when something interesting has happened.
func TestTailSpansTheRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := logging.NewWriterWithLimit(dir, 512)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if _, err := w.Write([]byte(fmt.Sprintf("line-%02d %s\n", i, strings.Repeat("y", 20)))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	tail, err := logging.Tail(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) < 10 {
		t.Fatalf("tail has %d lines just after a rotation", len(tail))
	}
	// Oldest first, and the newest line must be the last one written.
	if !strings.HasPrefix(tail[len(tail)-1], "line-59") {
		t.Errorf("last tail line is %q, want the most recent write", tail[len(tail)-1])
	}
	for i := 1; i < len(tail); i++ {
		if tail[i-1] >= tail[i] {
			t.Fatalf("tail is not in order: %q then %q", tail[i-1], tail[i])
		}
	}
}

// The tail goes over a SOCK_SEQPACKET socket where a whole message must fit one
// datagram (§3.1), so one enormous line must not crowd out everything else or
// blow the budget.
func TestTailIsBoundedInBytes(t *testing.T) {
	dir := t.TempDir()
	w, err := logging.NewWriterWithLimit(dir, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("short line\n")); err != nil {
			t.Fatal(err)
		}
	}
	// A single line far larger than the tail budget.
	if _, err := w.Write([]byte(strings.Repeat("z", logging.MaxTailBytes*2) + "\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	tail, err := logging.Tail(dir, logging.MaxTailLines)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, l := range tail {
		total += len(l) + 1
	}
	if total > logging.MaxTailBytes+1 {
		t.Errorf("tail is %d bytes, over the %d budget", total, logging.MaxTailBytes)
	}
}

func TestTailOfNothingIsNotAnError(t *testing.T) {
	tail, err := logging.Tail(t.TempDir(), 50)
	if err != nil {
		t.Fatalf("tailing an empty directory failed: %v", err)
	}
	if len(tail) != 0 {
		t.Errorf("tail = %v", tail)
	}
}

// What the viewer actually shows has to be the real log, so the writer has to
// work as an slog destination rather than just as an io.Writer.
func TestSlogWritesThroughTheRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	w, err := logging.NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("volume stored", "document", "9f7aac1f", "pages", 4)
	log.Warn("download failed", "err", "network is unreachable")
	w.Close()

	tail, err := logging.Tail(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(tail, "\n")
	for _, want := range []string{"volume stored", "9f7aac1f", "download failed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log does not contain %q:\n%s", want, joined)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, logging.FileName)); err != nil {
		t.Errorf("no log file: %v", err)
	}
}
