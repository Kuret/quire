package download_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/rickl/quire/backend/download"
)

// TestDeviceEncodeWorkers is a measurement, not an assertion, and belongs on
// the device: the encode half is CPU-bound, and four Cortex-A53 cores shared
// with xochitl behave nothing like a development host.
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./backend/download
//	scp download.test root@10.11.99.1:/home/root/quire-m4/
//	ssh root@10.11.99.1 'cd /home/root/quire-m4 && QUIRE_DEVICE_MEASURE=1 \
//	    ./download.test -test.run=TestDeviceEncodeWorkers -test.v'
//
// Write only under /home: / has ~47 MB free (docs/DEVICE-NOTES.md §3.3).
// Recorded results live in docs/DEVICE-NOTES.md §10.
func TestDeviceEncodeWorkers(t *testing.T) {
	if os.Getenv("QUIRE_DEVICE_MEASURE") != "1" {
		t.Skip("set QUIRE_DEVICE_MEASURE=1 to run the on-device measurement")
	}
	root := os.Getenv("QUIRE_WORK_DIR")
	if root == "" {
		t.Fatal("QUIRE_WORK_DIR is required; it must be under /home")
	}
	pages := 12
	if s := os.Getenv("QUIRE_PAGES"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		pages = n
	}

	// A realistic source: an A4-shaped scan larger than the panel.
	body := synthJPEG(t, 2480, 3508)
	t.Logf("cores %d, %d pages of 2480x3508 per run", runtime.NumCPU(), pages)

	for _, workers := range []int{1, 2, 3, 4} {
		dir := filepath.Join(root, "encode-"+strconv.Itoa(workers))
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		f := &stubFetcher{body: body}
		q := download.New(f, download.Options{
			Concurrency:   6,
			EncodeWorkers: workers,
			MinFreeBytes:  -1,
		})
		_, stats, err := q.Run(t.Context(), dir, chapters(1, pages))
		if err != nil {
			t.Fatalf("workers %d: %v", workers, err)
		}
		perPage := stats.Elapsed / time.Duration(stats.PagesDone)
		t.Logf("encode workers %d: %d pages in %s (%.2f pages/s, %s/page), peak encoding %d, %.0f KiB/page",
			workers, stats.PagesDone, stats.Elapsed.Round(time.Millisecond),
			float64(stats.PagesDone)/stats.Elapsed.Seconds(), perPage.Round(time.Millisecond),
			stats.MaxEncoding, float64(stats.BytesStored)/float64(stats.PagesDone)/1024)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
}
