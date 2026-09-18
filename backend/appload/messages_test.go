package appload

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestMessageTypeValues pins PLAN §7.1 as literal numbers. If someone
// renumbers a constant, this fails before a frontend silently misroutes.
func TestMessageTypeValues(t *testing.T) {
	want := map[string]int32{
		"SystemTerminate":       -1,
		"SystemNewCoordinator":  -2,
		"SystemLostCoordinator": -3,
		"Ping":                  1,
		"Pong":                  2,
		"ListSources":           10,
		"Sources":               11,
		"ProbeSource":           12,
		"ProbeProgress":         13,
		"ProbeVerdict":          14,
		"ConfirmAddSource":      15,
		"ProbeAnswer":           16,
		"SetSourceEnabled":      17,
		"RemoveSource":          18,
		"RenameSource":          19,
		"Search":                20,
		"SearchResults":         21,
		"Browse":                22,
		"RequestCover":          23,
		"CoverReady":            24,
		"SetSourceSplitStrips":  25,
		"SeriesDetail":          30,
		"SeriesDetailResult":    31,
		"EnqueueDownload":       40,
		"DownloadProgress":      41,
		"CancelDownload":        42,
		"EnqueueDownloads":      43,
		"QueueResult":           45,
		"OpenInReader":          50,
		"DeleteDownload":        51,
		"DownloadDeleted":       52,
		"DeleteConfirm":         53,
		"SortDocuments":         54,
		"DocumentsSorted":       55,
		"CheckDocuments":        56,
		"DocumentsChecked":      57,
		"DeleteSeries":          58,
		"DeleteSeriesConfirm":   59,
		"WatchSeries":           60,
		"UnwatchSeries":         61,
		"CheckWatched":          62,
		"WatchList":             63,
		"WatchUpdate":           64,
		"ListDownloaded":        65,
		"DownloadedList":        66,
		"DeleteFolder":          67,
		"FolderDeleted":         68,
		"Resume":                69,
		"SetConsultRobots":      70,
		"GetCacheSize":          71,
		"CacheStatus":           72,
		"ClearCache":            73,
		"CacheConfirm":          74,
		"Error":                 90,
	}
	if len(messageNames) != len(want) {
		t.Fatalf("messageNames has %d entries, want %d", len(messageNames), len(want))
	}
	for value, name := range messageNames {
		w, ok := want[name]
		if !ok {
			t.Errorf("unexpected message name %q", name)
			continue
		}
		if value != w {
			t.Errorf("%s = %d, want %d", name, value, w)
		}
	}
	if MessageReserved != 0 {
		t.Errorf("type 0 must stay reserved")
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		in   int32
		want string
	}{
		{MessagePing, "Ping"},
		{MessageError, "Error"},
		{MessageSystemTerminate, "SystemTerminate"},
		{MessageSystemLostCoordinator, "SystemLostCoordinator"},
		{MessageReserved, "Reserved"},
		{7, "Unknown(7)"},
		{-99, "Unknown(-99)"},
		{2147483647, "Unknown(2147483647)"},
		{-2147483648, "Unknown(-2147483648)"},
	}
	for _, tc := range tests {
		if got := Name(tc.in); got != tc.want {
			t.Errorf("Name(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

var jsVar = regexp.MustCompile(`(?m)^var\s+(\w+)\s*=\s*(-?\d+)\s*$`)

// TestQMLMessagesMatchGo is the anti-drift guard between the Go table and the
// QML mirror. Both sides must define exactly the same names and values.
func TestQMLMessagesMatchGo(t *testing.T) {
	const path = "../../ui/Messages.js"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	js := map[string]int32{}
	for _, m := range jsVar.FindAllStringSubmatch(string(src), -1) {
		v, err := strconv.ParseInt(m[2], 10, 32)
		if err != nil {
			t.Fatalf("%s: %s = %q: %v", path, m[1], m[2], err)
		}
		js[m[1]] = int32(v)
	}
	if len(js) == 0 {
		t.Fatalf("%s: parsed no constants", path)
	}

	goSide := map[string]int32{}
	for value, name := range messageNames {
		goSide[name] = value
	}

	for name, value := range goSide {
		jsValue, ok := js[name]
		if !ok {
			t.Errorf("%s is missing from %s", name, path)
			continue
		}
		if jsValue != value {
			t.Errorf("%s: Go = %d, QML = %d", name, value, jsValue)
		}
	}
	for name := range js {
		if _, ok := goSide[name]; !ok {
			t.Errorf("%s defines %s, which Go does not", path, name)
		}
	}
}
