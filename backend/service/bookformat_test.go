package service

import "testing"

func TestBookFormatAndReadable(t *testing.T) {
	cases := []struct {
		name     string
		format   string
		readable bool
	}{
		{"An Example Book (1970).epub", "epub", true},
		{"a.PDF", "pdf", true},
		{"a.Cbz", "cbz", true},
		{"a.fb2", "fb2", true},
		{"a.mobi", "mobi", true},
		{"a.xps", "xps", true},
		{"a.txt", "txt", true},
		{"a.azw3", "azw3", false},
		{"a.mobi.azw3", "azw3", false},
		{"noextension", "", false},
	}
	for _, c := range cases {
		got := bookFormat(c.name)
		if got != c.format {
			t.Errorf("bookFormat(%q) = %q, want %q", c.name, got, c.format)
		}
		if bookFormatReadable(got) != c.readable {
			t.Errorf("bookFormatReadable(%q) = %v, want %v", got, bookFormatReadable(got), c.readable)
		}
	}
}
