//go:build !unix

package download

import "errors"

// freeSpace has no portable implementation off unix. The caller treats an
// error as "free space unknown" and carries on, which is the right behaviour
// on a development host: the device is Linux, and that is where the check
// matters.
func freeSpace(string) (int64, error) {
	return 0, errors.New("download: free space is not available on this platform")
}

// FreeSpace is freeSpace, exported for callers outside this package. See
// freespace_unix.go's FreeSpace for what it is for; off unix it always
// reports unknown, and the caller treats that as "omit the free-space
// clause" rather than an error.
func FreeSpace(dir string) (int64, error) { return freeSpace(dir) }
