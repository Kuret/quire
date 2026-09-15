//go:build linux

package appload

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// sendZeroLengthPacket writes a genuine zero-length SEQPACKET datagram, the way
// AppLoad's sendMessageTo() does when it send()s an empty payload
// unconditionally. It goes through sendmsg rather than net.Conn.Write because
// Write is not a reliable way to demand a zero-length record.
func sendZeroLengthPacket(t *testing.T, c *Conn) {
	t.Helper()
	uc, ok := c.c.(*net.UnixConn)
	if !ok {
		t.Fatalf("not a unix conn: %T", c.c)
	}
	rc, err := uc.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var sysErr error
	if err := rc.Write(func(fd uintptr) bool {
		sysErr = unix.Sendmsg(int(fd), nil, nil, nil, 0)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if sysErr != nil {
		t.Fatalf("sendmsg: %v", sysErr)
	}
}

// TestZeroLengthPayloadIsNotEOF is the regression test for the bug that killed
// the app on the first Ping: the host's trailing empty payload packet was read
// as the next header, reported as io.EOF, and the backend exited "cleanly".
//
// The peer here does exactly what AppLoad does — header with messageLength 0,
// then a separate zero-length packet.
func TestZeroLengthPayloadIsNotEOF(t *testing.T) {
	server, client := unixpacketPair(t)

	// A Ping exactly as AppLoad puts it on the wire.
	if err := client.Send(MessagePing, nil); err != nil {
		t.Fatal(err)
	}
	sendZeroLengthPacket(t, client)

	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, payload, err := server.Recv()
	if err != nil {
		t.Fatalf("Recv: %v, want the Ping (this is the exact failure that exited the backend)", err)
	}
	if typ != MessagePing {
		t.Fatalf("type = %d, want Ping", typ)
	}
	if len(payload) != 0 {
		t.Errorf("payload = %q, want empty", payload)
	}

	// The connection must still be usable: the stray packet must have been
	// consumed, not left to be misread as the next header.
	if err := client.Send(MessagePong, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, payload, err = server.Recv()
	if err != nil {
		t.Fatalf("second Recv: %v", err)
	}
	if typ != MessagePong || string(payload) != `{"ok":true}` {
		t.Fatalf("second Recv: type=%d payload=%q", typ, payload)
	}
}

// TestManyZeroLengthPayloadsInARow makes sure the consume/skip logic stays in
// step over a run of payload-less messages rather than drifting by one packet.
func TestManyZeroLengthPayloadsInARow(t *testing.T) {
	server, client := unixpacketPair(t)

	const n = 20
	go func() {
		for i := 0; i < n; i++ {
			if err := client.Send(MessagePing, nil); err != nil {
				return
			}
			sendZeroLengthPacket(t, client)
		}
	}()

	for i := 0; i < n; i++ {
		server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
		typ, _, err := server.Recv()
		if err != nil {
			t.Fatalf("Recv %d: %v", i, err)
		}
		if typ != MessagePing {
			t.Fatalf("Recv %d: type = %d, want Ping (framing drifted)", i, typ)
		}
	}
}

// TestEmptyMessageThenCloseIsEOF is the case that must NOT be confused with the
// one above: a payload-less message followed by an actual close. The message
// must be delivered, and only then io.EOF.
func TestEmptyMessageThenCloseIsEOF(t *testing.T) {
	server, client := unixpacketPair(t)

	if err := client.Send(MessagePing, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, _, err := server.Recv()
	if err != nil {
		t.Fatalf("Recv: %v, want the Ping", err)
	}
	if typ != MessagePing {
		t.Fatalf("type = %d, want Ping", typ)
	}

	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, _, err := server.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// TestCloseWithNothingQueuedIsEOF is the plain shutdown path.
func TestCloseWithNothingQueuedIsEOF(t *testing.T) {
	server, client := unixpacketPair(t)

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, _, err := server.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// TestZeroLengthPacketAloneIsSkipped covers the backstop in recvHeader: a stray
// empty packet with no header in front of it is discarded, and the next real
// message still arrives.
func TestZeroLengthPacketAloneIsSkipped(t *testing.T) {
	server, client := unixpacketPair(t)

	sendZeroLengthPacket(t, client)
	if err := client.Send(MessagePong, []byte("hi")); err != nil {
		t.Fatal(err)
	}

	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, payload, err := server.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if typ != MessagePong || string(payload) != "hi" {
		t.Fatalf("type=%d payload=%q", typ, payload)
	}
}

// TestZeroLengthRecordIsDistinguishableFromEOF pins the syscall-level fact the
// whole fix rests on, independently of the framing above: readPacket reports a
// zero-length record as (0, true, nil) and end of stream as io.EOF, where
// net.Conn.Read collapses both into io.EOF.
//
// It is also the test that would have caught the MSG_EOR approach: MSG_EOR is
// never set on this socket type on the device, so an implementation relying on
// it reports the zero-length record as EOF and fails here.
func TestZeroLengthRecordIsDistinguishableFromEOF(t *testing.T) {
	server, client := unixpacketPair(t)

	sendZeroLengthPacket(t, client)
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))

	buf := make([]byte, 8)
	n, record, err := server.pr.readPacket(buf)
	if err != nil {
		t.Fatalf("readPacket: %v", err)
	}
	if n != 0 || !record {
		t.Fatalf("n=%d record=%v, want a 0-byte record", n, record)
	}

	client.Close()
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, record, err = server.pr.readPacket(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after close: n=%d record=%v err=%v, want io.EOF", n, record, err)
	}
}
