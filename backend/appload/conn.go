// Package appload implements the AppLoad host protocol: the length-prefixed
// framing spoken over the unix socket AppLoad hands to a backend as argv[1].
//
// Protocol facts were read off the AppLoad host (src/protocol.h and the symbols
// exported by the installed appload.so), not copied from it:
//
//	struct PacketHeader { int type; int messageLength; };
//	#define MAX_MESSAGE_LENGTH 10485760
//
// Correction to PLAN.md §7.1 / §6 M1: the plan describes the header as two u32
// fields. It is two *signed* native-endian int32s. Negative type values are
// reserved for system messages (see messages.go), so treating the type as
// unsigned would misread every system message. Length is likewise signed and a
// negative length is a protocol error, not a huge unsigned value.
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

// Dial connects to the unix socket at path (AppLoad passes it as argv[1]).
func Dial(path string) (*Conn, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("appload: dial %q: %w", path, err)
	}
	return NewConn(c), nil
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.c.Close() }

// Send writes one framed message. A nil or empty payload is sent as a
// zero-length message, which is what the payload-less types in §7.1 use.
func (c *Conn) Send(msgType int32, payload []byte) error {
	if len(payload) > MaxMessageLength {
		return fmt.Errorf("appload: send type %d: %w (%d bytes)", msgType, ErrMessageTooLarge, len(payload))
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()

	need := headerSize + len(payload)
	if cap(c.wbuf) < need {
		c.wbuf = make([]byte, need)
	}
	buf := c.wbuf[:need]
	byteOrder.PutUint32(buf[0:4], uint32(msgType))
	byteOrder.PutUint32(buf[4:8], uint32(int32(len(payload))))
	copy(buf[headerSize:], payload)

	if _, err := c.c.Write(buf); err != nil {
		return fmt.Errorf("appload: send type %d: %w", msgType, err)
	}
	return nil
}

// SendString is a convenience wrapper for text and JSON payloads.
func (c *Conn) SendString(msgType int32, payload string) error {
	return c.Send(msgType, []byte(payload))
}

// Recv reads one framed message, blocking until a whole frame has arrived.
//
// It returns io.EOF, unwrapped, when the peer closed cleanly between frames —
// the normal shutdown path. A close *part way* through a frame is reported as
// io.ErrUnexpectedEOF wrapped with which part was truncated.
//
// Recv returns ErrTerminated (along with the message type) when the host sends
// MessageSystemTerminate, so the caller's loop exits without special-casing.
func (c *Conn) Recv() (int32, []byte, error) {
	if _, err := io.ReadFull(c.c, c.hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			// Zero bytes read: clean close between frames.
			return 0, nil, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, fmt.Errorf("appload: truncated header: %w", io.ErrUnexpectedEOF)
		}
		return 0, nil, fmt.Errorf("appload: read header: %w", err)
	}

	msgType := int32(byteOrder.Uint32(c.hdr[0:4]))
	length := int32(byteOrder.Uint32(c.hdr[4:8]))

	// Validate before allocating. This ordering is the whole point.
	switch {
	case length < 0:
		return msgType, nil, fmt.Errorf("appload: type %d: %w (%d)", msgType, ErrNegativeLength, length)
	case length > MaxMessageLength:
		return msgType, nil, fmt.Errorf("appload: type %d: %w (%d bytes)", msgType, ErrMessageTooLarge, length)
	}

	var payload []byte
	if length > 0 {
		payload = make([]byte, length)
		if _, err := io.ReadFull(c.c, payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return msgType, nil, fmt.Errorf("appload: truncated body for type %d (want %d bytes): %w", msgType, length, io.ErrUnexpectedEOF)
			}
			return msgType, nil, fmt.Errorf("appload: read body for type %d: %w", msgType, err)
		}
	}

	if msgType == MessageSystemTerminate {
		return msgType, payload, ErrTerminated
	}
	return msgType, payload, nil
}
