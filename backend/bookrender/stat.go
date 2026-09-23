package bookrender

import "os"

// statExists is the real filesystem check ComposeCSS uses when no
// FontExistsFunc is given.
func statExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
