package library

// Host is the only address xochitl's web interface answers on.
//
// Not loopback — there is no loopback bind. This is the usb-gadget address,
// carried by interface `usb1`, and an on-device process reaches it because the
// address is *local*, not because USB is plugged in. That distinction is the
// whole of alias.go (docs/DEVICE-NOTES.md §5).
const Host = "10.11.99.1"

// BaseURL is Host as an origin. The interface is a browser app and checks
// Origin and Referer, so both are sent on every request.
const BaseURL = "http://" + Host
