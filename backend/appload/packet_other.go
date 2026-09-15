//go:build !linux

package appload

import "net"

// newPacketReader uses the portable fallback everywhere but Linux.
//
// Only the device matters for the real protocol, and it is Linux. Other
// platforms exist here so the package builds and its non-socket tests run on a
// development machine; note that macOS has no AF_UNIX SOCK_SEQPACKET at all, so
// there is no real socket to read from there in the first place.
func newPacketReader(c net.Conn) packetReader {
	return connPacketReader{c: c}
}
