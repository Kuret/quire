package library

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ConfPath is xochitl's settings file on the device.
const ConfPath = "/home/root/.config/remarkable/xochitl.conf"

// WebInterfaceKey is the setting that decides whether xochitl serves the web
// interface at all. It is **false** on a factory device.
const WebInterfaceKey = "WebInterfaceEnabled"

// ErrWebInterfaceDisabled means xochitl's web interface is switched off, so
// there is nothing listening on Host and there is nothing Quire can do about
// it from here.
//
// Quire deliberately does not flip the setting itself. The key only takes
// effect when xochitl restarts, and a restart throws away the page the user
// was on — the exact thing PLAN Goal 5 exists to protect. So this is a
// first-run setup step the user performs once, and our job is to say so in
// words rather than to fail with a connection error.
var ErrWebInterfaceDisabled = errors.New(WebInterfaceRemedy)

// WebInterfaceRemedy is the plain-language instruction shown to the user. It
// is the error text as well, because PLAN §2 puts the wording in the backend:
// the frontend is a dumb view and must not compose this itself.
const WebInterfaceRemedy = "Quire saves comics through the reMarkable's own USB web interface, " +
	"and it is switched off. On the tablet open Settings → Storage and turn on " +
	"“USB web interface”, then restart the tablet. Quire only needs this done once."

// ErrConfMissing means xochitl.conf was not found. On the device that is a
// broken install; off the device it simply means we are not on a reMarkable.
var ErrConfMissing = errors.New("library: xochitl.conf not found")

// WebInterfaceEnabled reports whether xochitl.conf has WebInterfaceEnabled=true.
//
// The file is a Qt INI without section headers for these keys, so it is parsed
// as plain key=value rather than with an INI library: one dependency fewer for
// a file we read one line out of.
func WebInterfaceEnabled(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("%w: %s", ErrConfMissing, path)
	}
	if err != nil {
		return false, fmt.Errorf("library: read %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != WebInterfaceKey {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(value), "true"), nil
	}
	if err := sc.Err(); err != nil {
		return false, fmt.Errorf("library: read %s: %w", path, err)
	}
	// The key is absent on a device that has never had the interface on. That
	// is the same situation as an explicit false, and the same remedy.
	return false, nil
}
