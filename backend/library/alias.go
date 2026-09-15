package library

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// AliasAddr is the address the loopback alias carries, in CIDR form.
//
// A /32 is deliberate: it makes 10.11.99.1 a *local* address without claiming
// the rest of the usb-gadget subnet, so the address can sit on `lo` and on
// `usb1` (which carries it as a /27) at the same time. Verified on hardware —
// USB SSH and the USB web interface both keep working with both present
// (docs/DEVICE-NOTES.md §5).
const AliasAddr = Host + "/32"

// AliasDevice is the interface the alias goes on.
const AliasDevice = "lo"

// ErrAliasUnsupported is returned by the default alias adder on a platform
// that has no `ip` command — the host test machine and the AppLoad PC
// emulator. It is not a failure: there is no xochitl there either.
var ErrAliasUnsupported = fmt.Errorf("library: adding a loopback alias is only supported on Linux")

// ErrAliasForbidden means the alias could not be added for want of
// CAP_NET_ADMIN. On the device the backend runs as root, so this means
// something is wrong rather than something is unsupported.
var ErrAliasForbidden = fmt.Errorf("library: not permitted to add the loopback alias (needs root)")

// addAlias adds AliasAddr to AliasDevice, idempotently.
//
// Why this exists at all: xochitl binds its web interface to 10.11.99.1 only,
// and unplugging USB strips that address from `usb1`. The listening socket
// survives, but the destination stops being local, so the request is routed to
// the default gateway and leaks onto the LAN instead of reaching xochitl. On-
// device access never used USB in the first place — it worked because the
// address was local and traffic went via `lo`. So Quire supplies that route
// itself (docs/DEVICE-NOTES.md §5, PLAN §11 Q1b).
//
// It is *not* reboot-persistent, which is why callers re-check rather than
// doing this once at startup and trusting it.
func addAlias() error {
	if runtime.GOOS != "linux" {
		return ErrAliasUnsupported
	}
	out, err := exec.Command("ip", "addr", "add", AliasAddr, "dev", AliasDevice).CombinedOutput()
	if err == nil {
		return nil
	}
	text := strings.ToLower(strings.TrimSpace(string(out)))
	switch {
	case strings.Contains(text, "file exists"):
		// Already there. This is the common case after the first call.
		return nil
	case strings.Contains(text, "operation not permitted"), strings.Contains(text, "permission denied"):
		return ErrAliasForbidden
	case errors.Is(err, exec.ErrNotFound):
		return ErrAliasUnsupported
	default:
		return fmt.Errorf("library: ip addr add %s dev %s: %w: %s", AliasAddr, AliasDevice, err, text)
	}
}
