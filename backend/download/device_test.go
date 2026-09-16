package download_test

import (
	"context"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/imageproc"
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

// fileFetcher serves page bytes from a directory, so a device run can use a
// realistic, *varied* page set copied from the host rather than a single
// synthetic geometry repeated. Nothing here touches a network.
type fileFetcher struct{ paths []string }

func (f *fileFetcher) Get(ctx context.Context, url, referer string) (io.ReadCloser, error) {
	i, err := strconv.Atoi(strings.TrimPrefix(url, "file://"))
	if err != nil || i < 0 || i >= len(f.paths) {
		return nil, fmt.Errorf("no such page %q", url)
	}
	return os.Open(f.paths[i])
}

// TestDeviceVariedGeometryRun is the regression measurement for the OOM kill:
// download + assemble a volume whose pages have *different* geometries, on the
// device, with xochitl running, reporting peak RSS.
//
// The original measurements used one geometry for every page, which is what
// hid the scaler cache's real cost — eight cached geometries pinned eight
// ~170 MB intermediate buffers.
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o download.test ./backend/download
//	COPYFILE_DISABLE=1 tar -cf - src | ssh root@10.11.99.1 'tar -C /home/root/quire-m4 -xf -'
//	ssh root@10.11.99.1 'cd /home/root/quire-m4 && QUIRE_DEVICE_MEASURE=1 \
//	    QUIRE_SRC_DIR=/home/root/quire-m4/src QUIRE_WORK_DIR=/home/root/quire-m4/run \
//	    QUIRE_SCALER_CACHE_MB=256 ./download.test -test.run=TestDeviceVariedGeometryRun -test.v'
//
// COPYFILE_DISABLE matters: macOS tar otherwise ships ._ AppleDouble files,
// which the assembler rightly rejects as "image: unknown format".
func TestDeviceVariedGeometryRun(t *testing.T) {
	if os.Getenv("QUIRE_DEVICE_MEASURE") != "1" {
		t.Skip("set QUIRE_DEVICE_MEASURE=1 to run the on-device measurement")
	}
	srcDir, work := os.Getenv("QUIRE_SRC_DIR"), os.Getenv("QUIRE_WORK_DIR")
	if srcDir == "" || work == "" {
		t.Fatal("QUIRE_SRC_DIR and QUIRE_WORK_DIR are required; both must be under /home")
	}
	if s := os.Getenv("QUIRE_SCALER_CACHE_MB"); s != "" {
		mb, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		imageproc.SetScalerCacheBytes(mb << 20)
		t.Logf("scaler cache budget %d MiB", mb)
	} else {
		t.Logf("scaler cache budget %d MiB (default)", imageproc.DefaultScalerCacheBytes>>20)
	}
	// Apply the same soft memory limit the backend sets, so the measurement is
	// of the shipped configuration. QUIRE_NO_MEMLIMIT=1 reproduces the
	// unbounded behaviour that got the backend OOM-killed.
	noLimit := os.Getenv("QUIRE_NO_MEMLIMIT") == "1"
	if noLimit {
		debug.SetMemoryLimit(math.MaxInt64)
		t.Log("memory limit: none (reproducing the OOM configuration)")
	}

	workers := 2
	if s := os.Getenv("QUIRE_ENCODE_WORKERS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		workers = n
	}

	var paths []string
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") && !strings.HasPrefix(e.Name(), ".") {
			paths = append(paths, filepath.Join(srcDir, e.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatalf("no source pages in %s", srcDir)
	}

	perChapter := 20
	var chs []download.Chapter
	for i := range paths {
		if i%perChapter == 0 {
			n := len(chs) + 1
			chs = append(chs, download.Chapter{ID: fmt.Sprintf("ch-%d", n), Title: fmt.Sprintf("Chapter %d", n), Number: strconv.Itoa(n)})
		}
		c := &chs[len(chs)-1]
		c.PageURLs = append(c.PageURLs, fmt.Sprintf("file://%d", i))
	}

	pagesDir := filepath.Join(work, "pages")
	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	opts := download.Options{
		Concurrency:   6,
		EncodeWorkers: workers,
		MinFreeBytes:  -1,
		NoMemoryLimit: noLimit,
	}
	opts.Image = imageproc.DefaultOptions()
	opts.Image.MaxBytes = 0 // measuring memory, not enforcing the page budget
	if os.Getenv("QUIRE_NO_GUARD") == "1" {
		opts.Image.MaxResampleBytes = -1
		t.Log("resample guard: disabled (reproducing the unbounded intermediate)")
	} else {
		t.Logf("resample guard: %d MiB per resize", opts.Image.MaxResampleBytes>>20)
	}
	q := download.New(&fileFetcher{paths: paths}, opts)
	if !noLimit {
		t.Logf("memory limit: %d MiB (applied by download.New)", debug.SetMemoryLimit(-1)>>20)
	}

	start := time.Now()
	out, stats, err := q.Run(t.Context(), pagesDir, chs)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	dlElapsed := time.Since(start)
	hwmAfterDownload, _ := procMem()

	vol := assemble.GroupIntoVolumes("Varied Geometry", out, len(out))[0]
	start = time.Now()
	m, err := assemble.Assemble(context.Background(), filepath.Join(work, "library"), vol, assemble.DefaultOptions())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	asmElapsed := time.Since(start)
	hwm, rss := procMem()

	t.Logf("cores %d, encode workers %d, %d pages of varied geometry", runtime.NumCPU(), workers, stats.PagesDone)
	t.Logf("download+resize  %s (%s/page), %.1f MiB stored, %.0f KiB/page, %d requantised, %d guarded",
		dlElapsed.Round(time.Millisecond), (dlElapsed / time.Duration(stats.PagesDone)).Round(time.Millisecond),
		float64(stats.BytesStored)/(1<<20), float64(stats.BytesStored)/float64(stats.PagesDone)/1024,
		stats.PagesRequantised, stats.PagesGuarded)
	t.Logf("assemble         %s, %.1f MiB PDF (%.0f KiB/page)",
		asmElapsed.Round(time.Millisecond), float64(m.Bytes)/(1<<20), float64(m.Bytes)/float64(m.PageCount)/1024)
	t.Logf("scaler cache     %.0f MiB retained at the end", float64(imageproc.ScalerCacheBytes())/(1<<20))
	t.Logf("VmHWM after dl   %.0f MiB", float64(hwmAfterDownload)/1024)
	t.Logf("VmHWM / VmRSS    %.0f MiB / %.0f MiB   <-- peak RSS for the whole run", float64(hwm)/1024, float64(rss)/1024)
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

// TestDeviceStripSplitRun measures the strip splitter on the device, which is
// the only place the number means anything: strips are the largest inputs this
// package handles, and the device has already OOM-killed us once over a
// smaller input (docs/DEVICE-NOTES.md §10.4).
//
// What is being checked is that splitting *before* the fit-and-pad resize
// (PLAN §12.3) keeps the peak below the whole-strip path rather than above it.
// Decoding a 20000-row strip is unavoidable; resampling one is not, and each
// piece resizes as an ordinary ~800 × 1067 page instead.
//
// QUIRE_SPLIT=never reruns the same input through the pre-§12.3 behaviour, so
// the two peaks can be compared on the same hardware in the same session.
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o download.test ./backend/download
//	COPYFILE_DISABLE=1 tar -cf - strips | ssh root@10.11.99.1 'tar -C /home/root/quire-strip -xf -'
//	ssh root@10.11.99.1 'cd /home/root/quire-strip && QUIRE_DEVICE_MEASURE=1 \
//	    QUIRE_WORK_DIR=/home/root/quire-strip/work QUIRE_SRC_DIR=/home/root/quire-strip/strips \
//	    ./download.test -test.run=TestDeviceStripSplitRun -test.v -test.timeout=30m'
//
// Write only under /home: / has ~47 MB free (docs/DEVICE-NOTES.md §3.3).
// Recorded results live in docs/DEVICE-NOTES.md §12.
func TestDeviceStripSplitRun(t *testing.T) {
	if os.Getenv("QUIRE_DEVICE_MEASURE") != "1" {
		t.Skip("set QUIRE_DEVICE_MEASURE=1 to run the on-device measurement")
	}
	work := os.Getenv("QUIRE_WORK_DIR")
	if work == "" {
		t.Fatal("QUIRE_WORK_DIR is required; it must be under /home")
	}
	srcDir := os.Getenv("QUIRE_SRC_DIR")
	if srcDir == "" {
		t.Fatal("QUIRE_SRC_DIR is required: a directory of strip images")
	}

	mode := imageproc.SplitAuto
	if s := os.Getenv("QUIRE_SPLIT"); s != "" {
		m, err := imageproc.ParseSplitMode(s)
		if err != nil {
			t.Fatal(err)
		}
		mode = m
	}

	var paths []string
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") && !strings.HasPrefix(e.Name(), ".") {
			paths = append(paths, filepath.Join(srcDir, e.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatalf("no strips in %s", srcDir)
	}

	// One chapter, as a webtoon episode is: every image is a strip, so
	// corroboration is satisfied and the deferral path is exercised for any
	// image below the extreme ratio.
	ch := download.Chapter{ID: "ep-1", Title: "Episode 1", Number: "1"}
	for i := range paths {
		ch.PageURLs = append(ch.PageURLs, fmt.Sprintf("file://%d", i))
	}

	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	opts := download.Options{
		Concurrency:   6,
		EncodeWorkers: 2,
		MinFreeBytes:  -1,
		SplitStrips:   mode,
	}
	opts.Image = imageproc.DefaultOptions()
	opts.Image.MaxBytes = 0 // measuring memory, not enforcing the page budget
	q := download.New(&fileFetcher{paths: paths}, opts)
	t.Logf("splitStrips: %s", mode)
	t.Logf("memory limit: %d MiB, resample guard %d MiB",
		debug.SetMemoryLimit(-1)>>20, opts.Image.MaxResampleBytes>>20)

	for i, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		t.Logf("strip %d: %d×%d  h:w %.1f  %.1f MiB on disk",
			i, cfg.Width, cfg.Height, imageproc.Aspect(cfg.Width, cfg.Height), float64(st.Size())/(1<<20))
	}

	start := time.Now()
	out, stats, err := q.Run(t.Context(), filepath.Join(work, "pages"), []download.Chapter{ch})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	dlElapsed := time.Since(start)
	hwmAfterDownload, _ := procMem()

	vol := assemble.GroupIntoVolumes("Strip", out, len(out))[0]
	start = time.Now()
	m, err := assemble.Assemble(context.Background(), filepath.Join(work, "library"), vol, assemble.DefaultOptions())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	asmElapsed := time.Since(start)
	hwm, rss := procMem()

	t.Logf("source images    %d", stats.PagesTotal)
	t.Logf("split            %d images → %d pages", stats.PagesSplit, stats.PagesFromSplit)
	t.Logf("PDF              %d pages, %.1f MiB (%.0f KiB/page)",
		m.PageCount, float64(m.Bytes)/(1<<20), float64(m.Bytes)/float64(m.PageCount)/1024)
	t.Logf("download         %s", dlElapsed.Round(time.Millisecond))
	t.Logf("assemble         %s", asmElapsed.Round(time.Millisecond))
	t.Logf("pages guarded    %d", stats.PagesGuarded)
	t.Logf("scaler cache     %.0f MiB retained at the end", float64(imageproc.ScalerCacheBytes())/(1<<20))
	t.Logf("VmHWM after dl   %.0f MiB", float64(hwmAfterDownload)/1024)
	t.Logf("VmHWM / VmRSS    %.0f MiB / %.0f MiB   <-- peak RSS for the whole run", float64(hwm)/1024, float64(rss)/1024)
}
