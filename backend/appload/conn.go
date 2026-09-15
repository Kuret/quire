// Package appload implements the AppLoad host protocol: the framing spoken over
// the unix socket AppLoad hands to a backend as argv[1].
//
// Protocol facts were read off the AppLoad host (src/protocol.h,
// src/management.cpp and the symbols exported by the installed appload.so), not
// copied from it:
//
//	struct PacketHeader { int type; int messageLength; };
//	#define MAX_MESSAGE_LENGTH 10485760
//
// Two corrections to PLAN.md §7.1 / §6 M1, both found the hard way:
//
// 1. The plan describes the header as two u32 fields. It is two *signed*
// native-endian int32s. Negative type values are reserved for system messages
// (see messages.go), so treating the type as unsigned would misread every
// system message. Length is likewise signed and a negative length is a protocol
// error, not a huge unsigned value.
//
// 2. The socket is **SOCK_SEQPACKET, not SOCK_STREAM** — AppLoad creates it with
// socket(AF_UNIX, SOCK_SEQPACKET, 0). Go's "unix" network is SOCK_STREAM and
// connecting to it fails with EPROTOTYPE ("protocol wrong type for socket"), so
// Dial must use "unixpacket". SEQPACKET preserves message boundaries, which
// changes the framing in two ways that a stream implementation gets wrong:
//
//   - AppLoad's receive loop does two separate read() calls, one for the
//     8-byte header and one for the payload, and each read() consumes exactly
//     one packet — discarding anything past the buffer length. So the header
//     must be its own packet and the payload a second one. A single combined
//     write would become one packet whose payload AppLoad silently drops.
//   - AppLoad skips its payload read() when messageLength == 0, so a
//     zero-length payload packet must not be sent: it would be consumed as the
//     *next* message's header, read 0 bytes, trip the host's `status < 1`
//     check and tear the connection down.
//
// There is no such thing as a partial packet, so none of the stream-oriented
// io.ReadFull reassembly belongs here; a short header read is a protocol error,
// not something to loop on.
//
// Note on size: the 10 MiB MAX_MESSAGE_LENGTH is the host's cap, but a
// SEQPACKET datagram is additionally bounded by the socket buffer (~200 KiB by
// default on Linux). PLAN §7.1 already forbids bulk transfers over this socket
// — write to disk and send a path — so this is a bound we should never be near.
package appload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// MaxMessageLength is AppLoad's MAX_MESSAGE_LENGTH: 10 MiB.
const MaxMessageLength = 10485760

// headerSize is sizeof(struct PacketHeader): two int32s, no padding.
const headerSize = 8

// ErrMessageTooLarge is returned when a peer announces a payload larger than
// MaxMessageLength. It is reported *before* any buffer is allocated, so a
// hostile or garbled length cannot exhaust memory on the device.
var ErrMessageTooLarge = errors.New("appload: message length exceeds 10 MiB cap")

// ErrNegativeLength is returned when a peer announces a negative payload length.
var ErrNegativeLength = errors.New("appload: negative message length")

// ErrTerminated is returned by Recv when the host sent MessageSystemTerminate.
// A backend should treat it as a clean shutdown request, not an error to report.
var ErrTerminated = errors.New("appload: host requested termination")

// ErrShortHeader is returned when a header packet is not exactly 8 bytes.
// SEQPACKET has no partial packets, so this is a framing error and not
// something a retry can fix.
var ErrShortHeader = errors.New("appload: header packet is not 8 bytes")

// ErrShortPayload is returned when a payload packet is shorter than the length
// its header announced. SEQPACKET truncates silently, so this catches a peer
// that lied about its length or a packet that exceeded the socket buffer.
var ErrShortPayload = errors.New("appload: payload packet shorter than its header announced")

// byteOrder is the wire order of the two header int32s. The header is written
// by the host with a plain struct write, so it is native-endian; both the
// device (aarch64) and the development host are little-endian.
var byteOrder = binary.NativeEndian

// Conn is a framed message connection to the AppLoad host.
//
// Recv must be called from a single goroutine. Send is safe for concurrent use:
// each message is written under a mutex so frames cannot interleave.
type Conn struct {
	c net.Conn

	wmu sync.Mutex
	// wbuf is reused across Send calls to keep header and payload in one write.
	wbuf []byte

	hdr [headerSize]byte
}

// NewConn wraps an established net.Conn in the AppLoad framing.
func NewConn(c net.Conn) *Conn {
	return &Conn{c: c}
}

// Network is the Go network name for AppLoad's socket. AppLoad creates it as
// SOCK_SEQPACKET; Go spells that "unixpacket". Using "unix" (SOCK_STREAM) fails
// at connect time with EPROTOTYPE.
const Network = "unixpacket"

// Dial connects to the unix socket at path (AppLoad passes it as argv[1]).
func Dial(path string) (*Conn, error) {
	c, err := net.Dial(Network, path)
	if err != nil {
		return nil, fmt.Errorf("appload: dial %q: %w", path, err)
	}
	return NewConn(c), nil
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.c.Close() }

// Send writes one framed message as one or two SEQPACKET packets: the 8-byte
// header always, then the payload as a *separate* packet if and only if it is
// non-empty. A nil or empty payload is sent as a header-only message, which is
// what the payload-less types in §7.1 use.
//
// Both properties are load-bearing against AppLoad's receive loop — see the
// package comment. Do not "optimise" this into a single write.
func (c *Conn) Send(msgType int32, payload []byte) error {
	if len(payload) > MaxMessageLength {
		return fmt.Errorf("appload: send type %d: %w (%d bytes)", msgType, ErrMessageTooLarge, len(payload))
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()

	if cap(c.wbuf) < headerSize {
		c.wbuf = make([]byte, headerSize)
	}
	hdr := c.wbuf[:headerSize]
	byteOrder.PutUint32(hdr[0:4], uint32(msgType))
	byteOrder.PutUint32(hdr[4:8], uint32(int32(len(payload))))

	if _, err := c.c.Write(hdr); err != nil {
		return fmt.Errorf("appload: send header for type %d: %w", msgType, err)
	}
	if len(payload) == 0 {
		return nil
	}
	if _, err := c.c.Write(payload); err != nil {
		return fmt.Errorf("appload: send payload for type %d: %w", msgType, err)
	}
	return nil
}

// SendString is a convenience wrapper for text and JSON payloads.
func (c *Conn) SendString(msgType int32, payload string) error {
	return c.Send(msgType, []byte(payload))
}

// Recv reads one framed message: one header packet, plus one payload packet if
// the header announced a non-empty payload.
//
// It returns io.EOF, unwrapped, when the peer closed cleanly between messages —
// the normal shutdown path. A close between the header and its payload is
// reported as io.ErrUnexpectedEOF; a packet that is present but short is
// ErrShortHeader or ErrShortPayload, because SEQPACKET truncates silently and
// there is no more of it coming.
//
// Recv returns ErrTerminated (along with the message type) when the host sends
// MessageSystemTerminate, so the caller's loop exits without special-casing.
func (c *Conn) Recv() (int32, []byte, error) {
	msgType, length, err := c.recvHeader()
	if err != nil {
		return msgType, nil, err
	}

	// Validate before allocating. This ordering is the whole point: a hostile
	// or garbled length must not be able to OOM the device.
	switch {
	case length < 0:
		return msgType, nil, fmt.Errorf("appload: type %d: %w (%d)", msgType, ErrNegativeLength, length)
	case length > MaxMessageLength:
		return msgType, nil, fmt.Errorf("appload: type %d: %w (%d bytes)", msgType, ErrMessageTooLarge, length)
	}

	var payload []byte
	if length > 0 {
		// Exactly length bytes: on SEQPACKET a short buffer silently discards
		// the rest of the packet, so never read into a larger one.
		payload = make([]byte, length)
		n, err := c.c.Read(payload)
		switch {
		case errors.Is(err, io.EOF):
			return msgType, nil, fmt.Errorf("appload: peer closed before the payload of type %d: %w", msgType, io.ErrUnexpectedEOF)
		case err != nil:
			return msgType, nil, fmt.Errorf("appload: read payload for type %d: %w", msgType, err)
		case n != int(length):
			return msgType, nil, fmt.Errorf("appload: type %d: %w (got %d, want %d)", msgType, ErrShortPayload, n, length)
		}
	}

	if msgType == MessageSystemTerminate {
		return msgType, payload, ErrTerminated
	}
	return msgType, payload, nil
}

// recvHeader reads one header packet.
//
// It tolerates stray zero-length packets. AppLoad's own send path is asymmetric
// to its receive path: sendMessageTo() always send()s the payload *unconditionally*,
// even when the payload is empty, while its read loop skips the payload read()
// when messageLength == 0. So a host-sent message with an empty payload leaves a
// zero-length packet queued where we expect the next header. Every current host
// call site happens to pass a non-empty string, but that is luck, not contract.
//
// The deliberate choice: skip such packets rather than erroring. Discarding an
// empty packet can only ever lose a message that carried no information, whereas
// erroring would tear down a connection over a quirk of the host we do not
// control. A zero-length read is distinguishable from a close because Go reports
// a peer close as io.EOF, not as a 0-byte packet.
func (c *Conn) recvHeader() (msgType, length int32, err error) {
	for {
		n, err := c.c.Read(c.hdr[:])
		switch {
		case errors.Is(err, io.EOF):
			// Clean close between messages.
			return 0, 0, io.EOF
		case err != nil:
			return 0, 0, fmt.Errorf("appload: read header: %w", err)
		case n == 0:
			continue // stray empty packet; see the doc comment
		case n != headerSize:
			return 0, 0, fmt.Errorf("appload: %w (got %d)", ErrShortHeader, n)
		}
		return int32(byteOrder.Uint32(c.hdr[0:4])), int32(byteOrder.Uint32(c.hdr[4:8])), nil
	}
}
