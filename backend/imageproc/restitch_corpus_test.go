package imageproc_test

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rickl/quire/backend/imageproc"
)

// Detection is tested here and nowhere else, against real chapters.
//
// # Why not synthetic
//
// Synthetic fixtures produced two false results while this was being written: a
// hand-made "ordinary manga" whose pages were one formula with a per-page offset
// correlated at 11 of 11 seams and was accepted, and a hand-made "sliced strip"
// whose texture changed too fast row to row correlated at 0 of 9 and was
// refused. Both were artefacts of the fixture. Real manga measures 6–8% and the
// real webtoon 70%, and no amount of care with a generator establishes that.
//
// # The corpus
//
// Copied off the user's device into the scratchpad, because real page images are
// not something to commit:
//
//	sinners-ch29/      28 images, the comick webtoon — the true positive
//	jjk-ch1/           52 images, Jujutsu Kaisen ch.1 — real manga, as delivered
//	jjk-ch1-uniform/   49 images, the same with the three odd-sized pages removed
//
// The third is the one that matters: real manga art at perfectly uniform
// dimensions, which is the norm for scanlations, so the count and shape rules
// cannot refuse it and the decision lands squarely on the two measured signals.
//
// **These tests skip when the corpus is absent**, which is every machine but the
// one that has it. That is a real limitation and is recorded in PLAN §12.3 along
// with the numbers, so a reader is not left believing `make check` has verified
// detection.
// corpusRoot locates the images. It is not a constant and it is not a path
// under any temporary directory, both on purpose.
//
// The corpus cannot live in the repository: it is copyrighted work from the
// sources a user configured, and PLAN §1.3 keeps that out of git. So it lives
// beside the checkout and is found by convention, with QUIRE_CORPUS_DIR as the
// override.
//
// The failure this shape prevents is specific. A path under a session's scratch
// directory works perfectly on the machine that wrote it and then disappears —
// after which these tests skip, for ever, and `make check` stays green while
// the one guarantee they provide has quietly stopped being checked. A test that
// silently stops testing is worse than one that was never written, because the
// green tick still reads as evidence.
func corpusRoot() string {
	if dir := os.Getenv("QUIRE_CORPUS_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "quire-detection-corpus")
}

func corpusScan(t *testing.T, name string) (*imageproc.StripScan, int) {
	t.Helper()
	root := corpusRoot()
	if root == "" {
		t.Skip("no home directory, so no corpus; set QUIRE_CORPUS_DIR")
	}
	entries, err := os.ReadDir(filepath.Join(root, name))
	if err != nil {
		t.Skipf("corpus %q is not on this machine (%v); "+
			"put it in %s or set QUIRE_CORPUS_DIR", name, err, root)
	}
	corpusDir := root
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			paths = append(paths, filepath.Join(corpusDir, name, e.Name()))
		}
	}
	sort.Strings(paths)

	scan := imageproc.NewStripScan(imageproc.RestitchOptions{})
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		scan.Add(img)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return scan, len(paths)
}

// The true positive: the chapter the feature exists for.
func TestTheSlicedWebtoonIsRecognised(t *testing.T) {
	scan, n := corpusScan(t, "sinners-ch29")
	v := scan.Verdict()
	t.Logf("sinners-ch29: %d images, padding %.0f%%, %d of %d seams continue — %s",
		n, v.Padding*100, v.Continuing, v.Seams, v.Reason)

	if !v.PreSliced {
		t.Fatalf("the chapter this exists for was refused: %s", v.Reason)
	}
	// The margins that made the thresholds, so a change that narrows them fails
	// here rather than on someone's tablet.
	if v.Padding < 0.4 {
		t.Errorf("padding measured %.2f, well under the 0.58 this was built on", v.Padding)
	}
	if got := float64(v.Continuing) / float64(v.Seams); got < 0.6 {
		t.Errorf("continuing fraction %.2f, well under the 0.70 this was built on", got)
	}
}

// The one that matters: real manga art at uniform dimensions, where nothing but
// the two measured signals can refuse it.
//
// A single false positive here means the thresholds are wrong. An unsplit
// webtoon is a page the user can see and complain about; a wrongly re-cut manga
// is a whole volume of sliced art they will not notice until they read it.
func TestRealMangaAtUniformSizeIsLeftAlone(t *testing.T) {
	scan, n := corpusScan(t, "jjk-ch1-uniform")
	v := scan.Verdict()
	t.Logf("jjk-ch1-uniform: %d images, aspect %.3f..%.3f, padding %.0f%%, %d of %d seams continue — %s",
		n, v.MinAspect, v.MaxAspect, v.Padding*100, v.Continuing, v.Seams, v.Reason)

	if v.PreSliced {
		t.Fatalf("FALSE POSITIVE on real manga: %s", v.Reason)
	}
	// It must be refused by the *evidence*, not by an accident of shape: these
	// pages are all one size, so the count and spread rules cannot save us.
	if v.MaxAspect/v.MinAspect > 1.12 {
		t.Errorf("the aspect rule refused it (%.3f..%.3f); this case is meant to reach the "+
			"measured signals", v.MinAspect, v.MaxAspect)
	}
	if v.Seams < 8 {
		t.Errorf("only %d seams; this case is meant to have enough evidence to judge", v.Seams)
	}
	if v.Padding > 0.15 || float64(v.Continuing)/float64(v.Seams) > 0.5 {
		t.Errorf("padding %.2f and %d/%d continuing — closer to the thresholds than measured",
			v.Padding, v.Continuing, v.Seams)
	}
}

// The same chapter as delivered. Worth its own case because its three
// odd-sized pages are *not* enough to refuse it: 1455×1940 and 797×1062 are
// both 4:3, so the aspect rule passes and the measured signals do the work here
// too.
func TestRealMangaAsDeliveredIsLeftAlone(t *testing.T) {
	scan, n := corpusScan(t, "jjk-ch1")
	v := scan.Verdict()
	t.Logf("jjk-ch1: %d images, aspect %.3f..%.3f, padding %.0f%%, %d of %d seams continue — %s",
		n, v.MinAspect, v.MaxAspect, v.Padding*100, v.Continuing, v.Seams, v.Reason)

	if v.PreSliced {
		t.Fatalf("FALSE POSITIVE on real manga: %s", v.Reason)
	}
	if v.MaxAspect/v.MinAspect > 1.12 {
		t.Errorf("this chapter was expected to pass the aspect rule (%.3f..%.3f) and be refused "+
			"on evidence", v.MinAspect, v.MaxAspect)
	}
}
