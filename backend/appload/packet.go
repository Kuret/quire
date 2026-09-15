package appload

import (
	"errors"
	"io"
	"net"
)

// packetReader reads whole SEQPACKET records.
//
// It exists because net.Conn cannot express the distinction this protocol
// depends on: a zero-length datagram and a closed peer both surface through
// net.Conn.Read as a 0-byte read, which the net package reports as io.EOF.
// AppLoad sends an empty payload packet after every empty-payload header, so
// conflating the two makes the backend exit the moment it receives a
// payload-less message such as Ping.
//
// # Why not MSG_EOR
//
// The textbook answer is that recvmsg() sets MSG_EOR in msg_flags at the end of
// a record, so a 0-byte read with MSG_EOR is a zero-length record and 0 without
// it is end of stream. **That does not hold for AF_UNIX SOCK_SEQPACKET on this
// device.** Measured on the reMarkable Paper Pro (OS 3.25.1.1, Linux 6.x
// aarch64), recvmsg never sets MSG_EOR on this socket type at all:
//
//	zero-length datagram  n=0 err=<nil> flags=0x0 EOR=false
//	5-byte datagram       n=5 err=<nil> flags=0x0 EOR=false
//	after peer close      n=0 err=<nil> flags=0x0 EOR=false
//
// So the flag carries no information here and cannot be used.
//
// # What is used instead: EOF is sticky, a datagram is not
//
// Once the peer has closed, every subsequent read returns 0 bytes immediately,
// forever. A zero-length datagram, by contrast, is consumed by the read that
// returns it — so if the socket is still open and nothing else is queued, the
// next read reports EAGAIN rather than 0. A non-blocking MSG_PEEK straight
// after a 0-byte read therefore separates the two:
//
//	peek -> EAGAIN  : the queue is empty and the peer is open  -> zero-length record
//	peek -> n > 0   : another record is waiting behind it      -> zero-length record
//	peek -> n == 0  : sticky zero, the peer is gone            -> end of stream
//
// The one case this cannot separate is two zero-length records back to back,
// which reads as end of stream. The host never emits that — it sends at most
// one empty packet per message — and a zero-length record at header position
// carries no information by construction, so the cost of being wrong is nil.
type packetReader interface {
	// readPacket reads one record into buf, blocking until one arrives.
	//
	// It returns (n, true, nil) for a record of n bytes, where n may legally be
	// 0, and (0, false, io.EOF) at end of stream. A record longer than buf is
	// truncated, as the kernel does, and reported with n == len(buf).
	readPacket(buf []byte) (n int, record bool, err error)

	// discardEmptyPacket consumes one queued zero-length record, waiting briefly
	// for it to arrive. It must never consume a record that has data in it, and
	// must never block indefinitely. Errors are not reported: there is nothing a
	// caller could usefully do about them.
	discardEmptyPacket()
}

// connPacketReader is the portable fallback, used for non-SEQPACKET
// connections: net.Pipe in tests, and any platform without the syscall path.
//
// It cannot tell a zero-length record from EOF — that is exactly the limitation
// the syscall path exists to escape — so it reports a 0-byte read with a nil
// error as a zero-length record and a 0-byte read with io.EOF as end of stream.
// A real net.Conn over a stream socket never produces the former, so on those
// this degrades to the obvious behaviour. Test fakes use it to drive both
// branches without a kernel.
type connPacketReader struct{ c net.Conn }

func (r connPacketReader) readPacket(buf []byte) (int, bool, error) {
	n, err := r.c.Read(buf)
	switch {
	case errors.Is(err, io.EOF):
		return 0, false, io.EOF
	case err != nil:
		return n, false, err
	}
	return n, true, nil
}

// discardEmptyPacket does nothing: a plain net.Conn offers no way to peek at a
// queued record, so there is no safe way to consume one without risking eating
// a real message. recvHeader's stray-record skip covers this case instead.
func (r connPacketReader) discardEmptyPacket() {}
