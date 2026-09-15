package assemble_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/assemble"
)

// TestDeviceAssembleFromDir is a measurement, not an assertion, and it is meant
// to be run **on the device** — assembly holds the whole document in memory and
// the tablet has ~2 GB shared with xochitl, so a host figure proves nothing:
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./backend/assemble
//	scp assemble.test root@10.11.99.1:/home/root/
//	scp -r pages root@10.11.99.1:/home/root/           # normalised page JPEGs
//	ssh root@10.11.99.1 'QUIRE_DEVICE_MEASURE=1 QUIRE_PAGES_DIR=/home/root/pages \
//	    QUIRE_OUT_DIR=/home/root/quire-measure /home/root/assemble.test \
//	    -test.run=TestDeviceAssembleFromDir -test.v'
//
// Write only under /home: / has ~47 MB free (docs/DEVICE-NOTES.md §3.3).
// Recorded results live in docs/DEVICE-NOTES.md §10.
func TestDeviceAssembleFromDir(t *testing.T) {
	if os.Getenv("QUIRE_DEVICE_MEASURE") != "1" {
		t.Skip("set QUIRE_DEVICE_MEASURE=1 to run the on-device measurement")
	}
	pagesDir := os.Getenv("QUIRE_PAGES_DIR")
	outDir := os.Getenv("QUIRE_OUT_DIR")
	if pagesDir == "" || outDir == "" {
		t.Fatal("QUIRE_PAGES_DIR and QUIRE_OUT_DIR are required")
	}
	perChapter := 20
	if s := os.Getenv("QUIRE_PAGES_PER_CHAPTER"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		perChapter = n
	}

	paths := collectPages(t, pagesDir)
	if len(paths) == 0 {
		t.Fatalf("no .jpg pages under %s", pagesDir)
	}

	var (
		chs    []assemble.Chapter
		srcSum int64
	)
	for i, p := range paths {
		if i%perChapter == 0 {
			n := len(chs) + 1
			chs = append(chs, assemble.Chapter{ID: fmt.Sprintf("ch-%d", n), Title: fmt.Sprintf("Chapter %d", n), Number: strconv.Itoa(n)})
		}
		c := &chs[len(chs)-1]
		if st, err := os.Stat(p); err == nil {
			srcSum += st.Size()
		}
		c.Pages = append(c.Pages, assemble.Page{Path: p, Index: len(c.Pages)})
	}

	vol := assemble.GroupIntoVolumes("Device Measure", chs, len(chs))[0]

	peakHeap := sampleHeap(t)
	start := time.Now()
	m, err := assemble.Assemble(context.Background(), outDir, vol, assemble.DefaultOptions())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	t.Logf("pages           %d in %d chapters (%.1f MiB of page JPEGs)", m.PageCount, len(chs), float64(srcSum)/(1<<20))
	t.Logf("assembled in    %s", elapsed.Round(time.Millisecond))
	t.Logf("pdf             %.1f MiB (%.0f KiB/page)", float64(m.Bytes)/(1<<20), float64(m.Bytes)/float64(m.PageCount)/1024)
	t.Logf("peak Go heap    %.0f MiB", float64(peakHeap())/(1<<20))
	if hwm, rss := procMem(); hwm > 0 {
		t.Logf("VmHWM / VmRSS   %.0f MiB / %.0f MiB", float64(hwm)/1024, float64(rss)/1024)
	}
	t.Logf("pdf at          %s", assemble.PDFPath(outDir, vol.Slug()))
}

func collectPages(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jpg") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

// sampleHeap starts a sampler and returns a function reporting the peak
// HeapAlloc seen since it started.
func sampleHeap(t *testing.T) func() uint64 {
	var peak uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ms runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak {
				peak = ms.HeapAlloc
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
		}
	})
	return func() uint64 {
		close(stop)
		<-done
		return peak
	}
}

// procMem reads VmHWM (peak RSS) and VmRSS in kB from /proc/self/status. Both
// are zero off Linux.
func procMem() (hwm, rss int64) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "VmHWM:":
			hwm = v
		case "VmRSS:":
			rss = v
		}
	}
	return hwm, rss
}
