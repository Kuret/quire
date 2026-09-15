package appload

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// pipe returns two Conns joined by net.Pipe. No network, no device.
func pipe(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	a, b := net.Pipe()
	ca, cb := NewConn(a), NewConn(b)
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return ca, cb
}

// header builds a raw header packet with an arbitrary (possibly invalid)
// length, so tests can feed the reader things a well-behaved Send would never
// produce.
func header(msgType, length int32) []byte {
	var hdr [headerSize]byte
	byteOrder.PutUint32(hdr[0:4], uint32(msgType))
	byteOrder.PutUint32(hdr[4:8], uint32(length))
	return hdr[:]
}

func TestRoundTrip(t *testing.T) {
	big := bytes.Repeat([]byte("q"), MaxMessageLength)

	tests := []struct {
		name    string
		msgType int32
		payload []byte
	}{
		// Every type in PLAN §7.1, round-tripped.
		{"ping empty", MessagePing, nil},
		{"pong json", MessagePong, []byte(`{"uptimeSeconds":1234.5}`)},
		{"list sources", MessageListSources, nil},
		{"sources", MessageSources, []byte(`[]`)},
		{"probe source", MessageProbeSource, []byte(`{"url":"https://example.invalid"}`)},
		{"probe progress", MessageProbeProgress, []byte(`{"stage":"reachability"}`)},
		{"probe verdict", MessageProbeVerdict, []byte(`{"verdict":"ok","theme":"madara","detail":""}`)},
		{"confirm add source", MessageConfirmAddSource, []byte(`{"url":"u","theme":"t","name":"n","lang":"en"}`)},
		{"search", MessageSearch, []byte(`{"sourceId":"s","query":"q"}`)},
		{"search results", MessageSearchResults, []byte(`[{"id":"1"}]`)},
		{"series detail", MessageSeriesDetail, []byte(`{"sourceId":"s","seriesId":"x"}`)},
		{"series detail result", MessageSeriesDetailResult, []byte(`{"title":"t"}`)},
		{"enqueue download", MessageEnqueueDownload, []byte(`{"sourceId":"s","seriesId":"x","volumeId":"v"}`)},
		{"download progress", MessageDownloadProgress, []byte(`{"done":3,"total":40}`)},
		{"open in reader", MessageOpenInReader, []byte(`{"documentUuid":"abc"}`)},
		{"error", MessageError, []byte(`{"code":"unreachable","message":"no route"}`)},

		// Boundaries.
		{"zero-length payload", MessagePing, []byte{}},
		{"single byte", MessagePong, []byte{0x00}},
		{"binary payload with nuls", MessagePong, []byte{0, 1, 2, 0, 255, 0}},
		{"max-size payload", MessagePong, big},

		// System types. Terminate is covered separately because Recv
		// deliberately returns ErrTerminated for it.
		{"system new coordinator", MessageSystemNewCoordinator, nil},
		{"system lost coordinator", MessageSystemLostCoordinator, nil},
		{"unknown negative type", -99, []byte("x")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pipe(t)
			errc := make(chan error, 1)
			go func() { errc <- a.Send(tc.msgType, tc.payload) }()

			gotType, gotPayload, err := b.Recv()
			if err != nil {
				t.Fatalf("Recv: %v", err)
			}
			if err := <-errc; err != nil {
				t.Fatalf("Send: %v", err)
			}
			if gotType != tc.msgType {
				t.Errorf("type = %d, want %d", gotType, tc.msgType)
			}
			if !bytes.Equal(gotPayload, tc.payload) && !(len(gotPayload) == 0 && len(tc.payload) == 0) {
				t.Errorf("payload len = %d, want %d", len(gotPayload), len(tc.payload))
			}
		})
	}
}

func TestRecvTerminate(t *testing.T) {
	a, b := pipe(t)
	go a.Send(MessageSystemTerminate, nil)

	gotType, _, err := b.Recv()
	if !errors.Is(err, ErrTerminated) {
		t.Fatalf("err = %v, want ErrTerminated", err)
	}
	if gotType != MessageSystemTerminate {
		t.Errorf("type = %d, want %d", gotType, MessageSystemTerminate)
	}
}

func TestSendOverCapRejected(t *testing.T) {
	a, _ := pipe(t)
	// One byte over the cap. Send must refuse rather than write a frame the
	// host will reject mid-stream.
	err := a.Send(MessagePong, make([]byte, MaxMessageLength+1))
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge", err)
	}
}

// packetConn is a fake SOCK_SEQPACKET connection: each Read yields exactly one
// queued packet, truncated to the caller's buffer as the kernel would, and each
// Write is recorded as one packet. It is what makes the framing bugs visible —
// net.Pipe is stream-like and would hide them.
type packetConn struct {
	net.Conn
	in  [][]byte // packets still to be delivered
	out [][]byte // packets written
}

func (pc *packetConn) Read(p []byte) (int, error) {
	if len(pc.in) == 0 {
		return 0, io.EOF
	}
	pkt := pc.in[0]
	pc.in = pc.in[1:]
	return copy(p, pkt), nil // SEQPACKET discards the remainder silently
}

func (pc *packetConn) Write(p []byte) (int, error) {
	pc.out = append(pc.out, append([]byte(nil), p...))
	return len(p), nil
}

func (pc *packetConn) Close() error { return nil }

func fromPackets(packets ...[]byte) *Conn { return NewConn(&packetConn{in: packets}) }

func TestRecvMalformed(t *testing.T) {
	tests := []struct {
		name      string
		input     [][]byte
		skipFirst bool // consume one valid message before the assertion
		wantErr   error
		wantType  int32
	}{
		{
			name:    "clean EOF between messages",
			input:   nil,
			wantErr: io.EOF,
		},
		{
			name:    "short header packet",
			input:   [][]byte{{1, 0, 0, 0, 5}},
			wantErr: ErrShortHeader,
		},
		{
			name:    "header packet of one byte",
			input:   [][]byte{{1}},
			wantErr: ErrShortHeader,
		},
		{
			// SEQPACKET has no partial packets, so a payload packet shorter
			// than its header claimed is a lie, not a partial read to loop on.
			name:     "short payload packet",
			input:    [][]byte{header(MessagePong, 16), []byte("only-nine")},
			wantErr:  ErrShortPayload,
			wantType: MessagePong,
		},
		{
			name:     "peer closed between header and payload",
			input:    [][]byte{header(MessagePong, 8)},
			wantErr:  io.ErrUnexpectedEOF,
			wantType: MessagePong,
		},
		{
			name:      "EOF after a complete message",
			input:     [][]byte{header(MessagePing, 0), header(MessagePong, 8)},
			skipFirst: true,
			wantErr:   io.ErrUnexpectedEOF,
			wantType:  MessagePong,
		},
		{
			name:     "length one over the cap",
			input:    [][]byte{header(MessagePong, MaxMessageLength+1)},
			wantErr:  ErrMessageTooLarge,
			wantType: MessagePong,
		},
		{
			name:     "length at max int32",
			input:    [][]byte{header(MessagePong, 0x7fffffff)},
			wantErr:  ErrMessageTooLarge,
			wantType: MessagePong,
		},
		{
			name:     "negative length",
			input:    [][]byte{header(MessagePong, -1)},
			wantErr:  ErrNegativeLength,
			wantType: MessagePong,
		},
		{
			name:     "negative length, most negative int32",
			input:    [][]byte{header(MessagePing, -2147483648)},
			wantErr:  ErrNegativeLength,
			wantType: MessagePing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fromPackets(tc.input...)
			if tc.skipFirst {
				if _, _, e := c.Recv(); e != nil {
					t.Fatalf("first Recv: %v", e)
				}
			}
			gotType, _, err := c.Recv()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantType != 0 && gotType != tc.wantType {
				t.Errorf("type = %d, want %d", gotType, tc.wantType)
			}
		})
	}
}

// TestStrayEmptyPacketIsSkipped pins the deliberate choice documented on
// recvHeader: AppLoad's sendMessageTo always sends a payload packet even when
// the payload is empty, while its read loop skips the payload read when the
// length is 0. Such a stray empty packet must be discarded, not treated as a
// short header, or the connection dies on the host's own asymmetry.
func TestStrayEmptyPacketIsSkipped(t *testing.T) {
	c := fromPackets(
		header(MessagePong, 0),
		[]byte{}, // the host's unconditional empty payload send
		header(MessagePong, 2),
		[]byte("hi"),
	)

	typ, payload, err := c.Recv()
	if err != nil || typ != MessagePong || len(payload) != 0 {
		t.Fatalf("first Recv: type=%d payload=%q err=%v", typ, payload, err)
	}
	typ, payload, err = c.Recv()
	if err != nil {
		t.Fatalf("second Recv: %v", err)
	}
	if typ != MessagePong || string(payload) != "hi" {
		t.Fatalf("second Recv: type=%d payload=%q", typ, payload)
	}
	if _, _, err := c.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Recv: %v, want io.EOF", err)
	}
}

// TestOverCapDoesNotAllocate is the load-bearing one: a hostile length must be
// rejected before the payload buffer is made, or the device OOMs.
func TestOverCapDoesNotAllocate(t *testing.T) {
	c := fromPackets(header(MessagePong, 0x7fffffff))
	allocs := testing.AllocsPerRun(50, func() {
		c2 := fromPackets(header(MessagePong, 0x7fffffff))
		_, payload, err := c2.Recv()
		if payload != nil || !errors.Is(err, ErrMessageTooLarge) {
			t.Fatalf("payload=%v err=%v", payload, err)
		}
	})
	// The error formatting itself allocates; what matters is that the count is
	// small and constant, i.e. no ~2 GiB buffer was made.
	if allocs > 16 {
		t.Errorf("allocs per Recv = %v, want a small constant", allocs)
	}
	if _, _, err := c.Recv(); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

// TestWireLayout pins the exact bytes on the wire: two little-endian signed
// int32s, no padding. If this fails the host will not understand us.
func TestWireLayout(t *testing.T) {
	pc := &packetConn{}
	c := NewConn(pc)
	if err := c.Send(-1, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if len(pc.out) != 2 {
		t.Fatalf("wrote %d packets, want 2", len(pc.out))
	}
	hdr := pc.out[0]
	if len(hdr) != headerSize {
		t.Fatalf("header packet len = %d, want %d", len(hdr), headerSize)
	}
	if binary.NativeEndian.Uint32(hdr[0:4]) != uint32(0xffffffff) {
		t.Errorf("type bytes = % x, want ff ff ff ff", hdr[0:4])
	}
	if int32(binary.NativeEndian.Uint32(hdr[4:8])) != 2 {
		t.Errorf("length bytes = % x, want 2", hdr[4:8])
	}
	if string(pc.out[1]) != "hi" {
		t.Errorf("payload packet = %q", pc.out[1])
	}
}

// TestSendPacketBoundaries is the regression test for the SEQPACKET framing
// bugs. AppLoad reads the header with one read() and the payload with a second,
// and skips the second entirely when messageLength == 0. So:
//   - header and payload must be two distinct packets, never one combined write
//   - an empty payload must send exactly one packet
func TestSendPacketBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    []int // expected length of each packet written
	}{
		{"nil payload sends header only", nil, []int{headerSize}},
		{"empty payload sends header only", []byte{}, []int{headerSize}},
		{"one byte payload is its own packet", []byte("x"), []int{headerSize, 1}},
		{"json payload is its own packet", []byte(`{"ok":true}`), []int{headerSize, 11}},
		{"large payload is its own packet", bytes.Repeat([]byte("q"), 4096), []int{headerSize, 4096}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pc := &packetConn{}
			if err := NewConn(pc).Send(MessagePong, tc.payload); err != nil {
				t.Fatal(err)
			}
			if len(pc.out) != len(tc.want) {
				t.Fatalf("wrote %d packets, want %d", len(pc.out), len(tc.want))
			}
			for i, want := range tc.want {
				if len(pc.out[i]) != want {
					t.Errorf("packet %d len = %d, want %d", i, len(pc.out[i]), want)
				}
			}
			// Whatever the payload, the announced length must match it.
			if got := int32(binary.NativeEndian.Uint32(pc.out[0][4:8])); got != int32(len(tc.payload)) {
				t.Errorf("announced length = %d, want %d", got, len(tc.payload))
			}
		})
	}
}

// unixpacketPair returns two Conns joined by a real SOCK_SEQPACKET unix socket
// — the same socket type AppLoad creates. This is the only test that exercises
// true kernel packet semantics; net.Pipe and packetConn are approximations.
//
// Skipped where the platform has no AF_UNIX SOCK_SEQPACKET (notably macOS), so
// it runs on the device's OS and in Linux CI but not on a dev Mac.
func unixpacketPair(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	// Keep the path short: sun_path is ~108 bytes.
	dir, err := os.MkdirTemp("", "ql")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")

	l, err := net.Listen(Network, sock)
	if err != nil {
		t.Skipf("%s unavailable on %s: %v", Network, runtime.GOOS, err)
	}
	t.Cleanup(func() { l.Close() })

	type result struct {
		c   net.Conn
		err error
	}
	accepted := make(chan result, 1)
	go func() {
		c, err := l.Accept()
		accepted <- result{c, err}
	}()

	client, err := net.Dial(Network, sock)
	if err != nil {
		t.Fatalf("dial %s: %v", Network, err)
	}
	r := <-accepted
	if r.err != nil {
		t.Fatalf("accept: %v", r.err)
	}

	a, b := NewConn(r.c), NewConn(client)
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// TestUnixpacketRoundTrip runs Send/Recv across a real SEQPACKET socket.
func TestUnixpacketRoundTrip(t *testing.T) {
	server, client := unixpacketPair(t)

	tests := []struct {
		name    string
		msgType int32
		payload []byte
	}{
		{"ping, no payload", MessagePing, nil},
		{"ping, empty payload", MessagePing, []byte{}},
		{"pong json", MessagePong, []byte(`{"ok":true,"uptimeSeconds":1703.6}`)},
		{"binary with nuls", MessagePong, []byte{0, 1, 2, 0, 255}},
		{"system new coordinator", MessageSystemNewCoordinator, nil},
		{"error json", MessageError, []byte(`{"code":"x","message":"y"}`)},
		{"64 KiB payload", MessagePong, bytes.Repeat([]byte("q"), 64*1024)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := client.Send(tc.msgType, tc.payload); err != nil {
				t.Fatalf("Send: %v", err)
			}
			server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
			gotType, gotPayload, err := server.Recv()
			if err != nil {
				t.Fatalf("Recv: %v", err)
			}
			if gotType != tc.msgType {
				t.Errorf("type = %d, want %d", gotType, tc.msgType)
			}
			if !bytes.Equal(gotPayload, tc.payload) && !(len(gotPayload) == 0 && len(tc.payload) == 0) {
				t.Errorf("payload = %q, want %q", gotPayload, tc.payload)
			}
		})
	}
}

// TestUnixpacketPacketCounts asserts the boundaries the host depends on,
// measured by the kernel rather than by a fake: one packet for a header-only
// message, two for a message with a payload.
func TestUnixpacketPacketCounts(t *testing.T) {
	server, client := unixpacketPair(t)

	// A message with a payload must arrive as two packets. Reading the first
	// 8-byte packet must NOT also consume the payload.
	if err := client.Send(MessagePong, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	hdr := make([]byte, headerSize)
	n, err := server.c.Read(hdr)
	if err != nil || n != headerSize {
		t.Fatalf("header read: n=%d err=%v", n, err)
	}
	if got := int32(byteOrder.Uint32(hdr[4:8])); got != 5 {
		t.Fatalf("announced length = %d, want 5", got)
	}
	body := make([]byte, 5)
	n, err = server.c.Read(body)
	if err != nil || n != 5 || string(body) != "hello" {
		t.Fatalf("payload read: n=%d body=%q err=%v", n, body, err)
	}

	// A header-only message must send exactly one packet: nothing else may be
	// waiting behind it, or the host would read it as the next header.
	if err := client.Send(MessagePing, nil); err != nil {
		t.Fatal(err)
	}
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err = server.c.Read(hdr)
	if err != nil || n != headerSize {
		t.Fatalf("ping header read: n=%d err=%v", n, err)
	}
	// Nothing more should be queued.
	server.c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	spare := make([]byte, headerSize)
	if n, err := server.c.Read(spare); err == nil {
		t.Fatalf("a header-only Send left %d extra bytes queued: % x", n, spare[:n])
	} else if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("unexpected error draining: %v", err)
	}
}

// TestUnixpacketOversizedPacketIsCaught: SEQPACKET truncates silently, so a
// payload larger than the socket buffer must surface as an error rather than as
// quietly corrupted data. Whether it fails on the send or the receive side
// depends on the socket buffer size, so accept either — what must not happen is
// a short payload being handed back as if it were whole.
func TestUnixpacketOversizedPacketIsCaught(t *testing.T) {
	server, client := unixpacketPair(t)

	// 4 MiB is far over any default SO_SNDBUF but under the 10 MiB cap, so the
	// kernel rejects the datagram outright (EMSGSIZE) rather than blocking.
	payload := bytes.Repeat([]byte("q"), 4<<20)
	if err := client.Send(MessagePong, payload); err != nil {
		t.Logf("Send refused the oversized packet, as it should: %v", err)
		return
	}

	// If the kernel did accept it, the payload must come back whole — a
	// silently truncated packet handed back as if complete is the failure mode
	// this test exists to rule out.
	server.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, got, err := server.Recv()
	if err != nil {
		t.Logf("Recv rejected the oversized packet, as it should: %v", err)
		return
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Recv returned %d bytes with no error, want %d", len(got), len(payload))
	}
}

func TestConcurrentSendsDoNotInterleave(t *testing.T) {
	a, b := pipe(t)
	const n = 20
	payload := bytes.Repeat([]byte("z"), 4096)
	for i := 0; i < n; i++ {
		go func() {
			if err := a.Send(MessagePong, payload); err != nil {
				t.Errorf("Send: %v", err)
			}
		}()
	}
	b.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < n; i++ {
		typ, got, err := b.Recv()
		if err != nil {
			t.Fatalf("Recv %d: %v", i, err)
		}
		if typ != MessagePong || !bytes.Equal(got, payload) {
			t.Fatalf("frame %d corrupted: type=%d len=%d", i, typ, len(got))
		}
	}
}
