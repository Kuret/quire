package service

import (
	"path/filepath"
	"strings"
)

// bookReadableFormats are the file extensions backend/bookrender's mutool
// driver can actually open (books-contract.md §B, Storage). Anything else —
// azw3 is the example the contract names — is not something Quire's own
// reader can show, whatever destination the request asked for.
var bookReadableFormats = map[string]bool{
	"epub": true,
	"fb2":  true,
	"mobi": true,
	"pdf":  true,
	"xps":  true,
	"cbz":  true,
	"txt":  true,
}

// bookFormat returns name's extension, lower-cased and without the leading
// dot — the shape shelf.Record.Format and the readable-format check both
// use.
func bookFormat(name string) string {
	ext := filepath.Ext(name)
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}

// bookFormatReadable reports whether backend/bookrender can open a file of
// this format.
func bookFormatReadable(format string) bool {
	return bookReadableFormats[format]
}
