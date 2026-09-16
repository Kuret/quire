package download

import (
	"path/filepath"
	"testing"
)

// ChapterDir is what deleting a download uses to find the pages this package
// wrote, so it has to agree with the layout planJobs lays down — exactly, and
// for ids that sanitising and truncation would otherwise run together.
func TestChapterDirMatchesTheDirectoryPlanJobsUses(t *testing.T) {
	long := "/manga/a-very-long-series-name-that-runs-past-the-readable-cut/"
	ids := []string{
		"/manga/the-lantern-keeper/chapter-4/",
		long + "v01/c001/1.html",
		long + "v01/c002/1.html",
		"?p=1",
		"#p=1",
		"",
	}
	seen := map[string]string{}
	for _, id := range ids {
		got := ChapterDir("/tmp/series", id)
		if dir := filepath.Dir(got); dir != "/tmp/series" {
			t.Errorf("ChapterDir(%q) landed in %q", id, dir)
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("%q and %q share the directory %q", prev, id, got)
		}
		seen[got] = id
		if got != filepath.Join("/tmp/series", slug(id)) {
			t.Errorf("ChapterDir(%q) = %q, which is not the slug planJobs uses", id, got)
		}
	}
}
