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
