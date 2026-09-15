package library_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/library"
)

// TestDeviceUploadAVolume is PLAN §6 M5's acceptance, run **on the device**
// against the real xochitl. It is skipped everywhere else: there is no way to
// prove "xochitl indexed it and rendered a thumbnail" against a fake.
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./backend/library
//	scp library.test root@10.11.99.1:/home/root/quire-m5/
//	ssh root@10.11.99.1 'cd /home/root/quire-m5 && QUIRE_DEVICE_LIBRARY=1 \
//	    ./library.test -test.run TestDeviceUploadAVolume -test.v'
//
// It assembles a real multi-page PDF, uploads it, and reads back where it
// landed. The document it leaves behind is named quire-m5-test-* on purpose:
// it is disposable, and it is the only thing a human has to look at to
// finish the acceptance — does it appear in the right place, with a real
// thumbnail, and does it open in the stock reader.
func TestDeviceUploadAVolume(t *testing.T) {
	if os.Getenv("QUIRE_DEVICE_LIBRARY") == "" {
		t.Skip("set QUIRE_DEVICE_LIBRARY=1 and run this on the reMarkable")
	}

	ctx := context.Background()
	lib := library.New(library.Options{})

	if err := lib.EnsureReachable(ctx); err != nil {
		t.Fatalf("the library is not reachable: %v", err)
	}
	t.Log("endpoint reachable, loopback alias in place")

	dir := t.TempDir()
	if home := os.Getenv("QUIRE_DEVICE_DIR"); home != "" {
		dir = home
	}

	// A handful of real page images, at the panel's aspect, assembled by the
	// same code the download path uses.
	var pages []assemble.Page
	for i := 0; i < 4; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%04d.jpg", i))
		if err := os.WriteFile(path, pageJPEG(t, i), 0o644); err != nil {
			t.Fatal(err)
		}
		pages = append(pages, assemble.Page{Path: path, Index: i})
	}

	stamp := time.Now().Format("150405")
	vol := assemble.Volume{
		Series: "quire-m5-test",
		Label:  stamp,
		Title:  "quire-m5-test volume " + stamp,
		Chapters: []assemble.Chapter{
			{ID: "quire-m5-test-chapter", Title: "Chapter 1", Number: "1", Pages: pages},
		},
	}
	manifest, err := assemble.Assemble(ctx, dir, vol, assemble.DefaultOptions())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Logf("assembled %d pages, %d bytes", manifest.PageCount, manifest.Bytes)

	place, err := lib.Resolve(ctx, library.ComicsFolder, vol.Series)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Logf("placement: folder=%q path=%v missing=%v", place.FolderID, place.Path, place.Missing)
	if len(place.Path) == 0 {
		t.Fatalf("no %s folder on this device; make one and re-run", library.ComicsFolder)
	}
	if !place.Complete() {
		t.Logf("remedy: %s", place.Remedy())
	}

	f, err := os.Open(assemble.PDFPath(dir, vol.Slug()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	res, err := lib.Upload(ctx, place.FolderID, vol.Title+".pdf", f)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	t.Logf("uploaded: uuid=%s folder=%s name=%q", res.DocumentUUID, res.FolderUUID, res.VisibleName)

	if res.DocumentUUID == "" {
		t.Fatal("no document UUID came back")
	}
	if res.FolderUUID != place.FolderID {
		t.Errorf("landed in %q, asked for %q", res.FolderUUID, place.FolderID)
	}
	if !strings.Contains(res.VisibleName, "quire-m5-test") {
		t.Errorf("visible name %q", res.VisibleName)
	}

	// The UUID has to outlive the process; that is the whole point of it.
	store, err := library.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := library.Key{Source: "device-test", Series: vol.Series, Volume: vol.Label}
	if err := store.Put(library.Record{
		Key: key, DocumentUUID: res.DocumentUUID, FolderUUID: res.FolderUUID,
		FolderPath: place.Path, VisibleName: res.VisibleName, Pages: manifest.PageCount,
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := library.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(key)
	if !ok || got.DocumentUUID != res.DocumentUUID {
		t.Fatalf("the UUID did not survive a reopen: %+v", got)
	}

	t.Logf("ACCEPTANCE: look for %q in My Files → %s, check the thumbnail, open it, then delete it",
		res.VisibleName, strings.Join(place.Path, " → "))
}

func pageJPEG(t *testing.T, n int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 1024, 1365))
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			v := uint8((x/32 + y/32 + n*4) % 2 * 200)
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
