//go:build unix

package download

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// freeSpace reports the bytes available to an unprivileged writer on the
// filesystem holding dir.
//
// Bavail rather than Bfree: the device's /home is ext4-style with reserved
// blocks, and a download that eats into the reserve is exactly the failure
// this check exists to prevent.
func freeSpace(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("download: statfs %s: %w", dir, err)
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize)), nil
}
