//go:build linux

package appload

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// emptyPacketWait is how long discardEmptyPacket waits for the host's trailing
// empty payload packet to show up.
//
// AppLoad emits the header and the payload from two back-to-back send() calls
// in one function, so in practice the second packet is already queued by the
// time we look and this timeout is never reached. It exists only so that a host
// which does not send one cannot wedge the backend; the cost of the wait is
// paid once per payload-less message, and never by a message that has a payload.
const emptyPacketWait = 50 * time.Millisecond

// newPacketReader returns a recvmsg-based reader for real SOCK_SEQPACKET unix
// sockets, and the portable fallback for anything else (net.Pipe, test fakes).
func newPacketReader(c net.Conn) packetReader {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return connPacketReader{c: c}
	}
	rc, err := uc.SyscallConn()
	if err != nil {
		return connPacketReader{c: c}
	}
	return &seqpacketReader{rc: rc}
}

// seqpacketReader reads records with recvmsg(2) so it can peek at the queue.
//
// Blocking reads go through syscall.RawConn.Read, which keeps them on Go's
// netpoller: returning false from the callback parks the goroutine until the fd
// is readable, and read deadlines set on the net.Conn still apply. Non-blocking
// probes go through RawConn.Control, which runs the callback once without any
// readiness handling — exactly what a MSG_DONTWAIT call wants.
type seqpacketReader struct{ rc syscall.RawConn }

func (r *seqpacketReader) readPacket(buf []byte) (n int, record bool, err error) {
	var sysErr error
	ctlErr := r.rc.Read(func(fd uintptr) bool {
		n, _, _, _, sysErr = unix.Recvmsg(int(fd), buf, nil, 0)
		if isWouldBlock(sysErr) || errors.Is(sysErr, unix.EINTR) {
			return false // park until readable, then retry
		}
		if n == 0 && sysErr == nil {
			// Ambiguous: zero-length record, or end of stream. Peek at what is
			// behind it — see the packetReader doc comment for why this works
			// and why MSG_EOR does not.
			record = r.peekSaysOpen(fd)
		} else {
			record = true
		}
		return true
	})
	switch {
	case ctlErr != nil:
		// Deadline exceeded, or the conn was closed underneath us.
		return 0, false, ctlErr
	case sysErr != nil:
		return 0, false, fmt.Errorf("appload: recvmsg: %w", sysErr)
	case n == 0 && !record:
		return 0, false, io.EOF
	}
	return n, record, nil
}

// peekSaysOpen reports whether the socket is still open, given that a read just
// returned 0 bytes. Must be called with fd borrowed from a RawConn callback.
func (r *seqpacketReader) peekSaysOpen(fd uintptr) bool {
	var probe [1]byte
	n, _, _, _, err := unix.Recvmsg(int(fd), probe[:], nil, unix.MSG_PEEK|unix.MSG_DONTWAIT)
	switch {
	case isWouldBlock(err):
		return true // open, nothing queued: the 0 bytes were a real record
	case err != nil:
		return false
	}
	// n > 0 means another record is already waiting, so the 0 bytes were a
	// record too. n == 0 is the sticky zero of a closed peer.
	return n > 0
}

// discardEmptyPacket consumes the zero-length packet AppLoad sends after an
// empty-payload header, and nothing else.
//
// It peeks before consuming so that a record carrying data can never be eaten:
// if the host ever omitted its empty packet, the next thing in the queue would
// be a real header, and swallowing it would desynchronise the stream for good.
func (r *seqpacketReader) discardEmptyPacket() {
	deadline := time.Now().Add(emptyPacketWait)
	for {
		var (
			peekN   int
			peekErr error
		)
		if err := r.rc.Control(func(fd uintptr) {
			var probe [1]byte
			peekN, _, _, _, peekErr = unix.Recvmsg(int(fd), probe[:], nil, unix.MSG_PEEK|unix.MSG_DONTWAIT)
		}); err != nil {
			return
		}

		switch {
		case isWouldBlock(peekErr):
			// Not queued yet. This is the only path that waits, and only up to
			// emptyPacketWait; recvHeader's skip is the backstop if we give up.
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(time.Millisecond)
			continue
		case peekErr != nil:
			return
		case peekN > 0:
			// A real record is queued: the host sent no empty packet. Leave it.
			return
		}

		// peekN == 0: a zero-length record, or a closed peer. Consuming is
		// correct in the first case and harmless in the second, since a closed
		// peer keeps reporting 0 and recvHeader will report io.EOF next.
		_ = r.rc.Control(func(fd uintptr) {
			var probe [1]byte
			_, _, _, _, _ = unix.Recvmsg(int(fd), probe[:], nil, unix.MSG_DONTWAIT)
		})
		return
	}
}

func isWouldBlock(err error) bool {
	return errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK)
}
