package appload

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
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

// frame builds a raw wire frame with an arbitrary (possibly invalid) length,
// so tests can feed the reader things a well-behaved Send would never produce.
func frame(msgType, length int32, body []byte) []byte {
	var hdr [headerSize]byte
	byteOrder.PutUint32(hdr[0:4], uint32(msgType))
	byteOrder.PutUint32(hdr[4:8], uint32(length))
	return append(hdr[:], body...)
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

// readerConn wraps a plain io.Reader so tests can drive Recv from a byte slice.
type readerConn struct {
	net.Conn
	r io.Reader
}

func (rc *readerConn) Read(p []byte) (int, error)  { return rc.r.Read(p) }
func (rc *readerConn) Write(p []byte) (int, error) { return len(p), nil }
func (rc *readerConn) Close() error                { return nil }

func fromBytes(b []byte) *Conn { return NewConn(&readerConn{r: bytes.NewReader(b)}) }

func TestRecvMalformed(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantErr  error
		wantType int32
	}{
		{
			name:    "clean EOF between frames",
			input:   nil,
			wantErr: io.EOF,
		},
		{
			name:    "truncated header",
			input:   []byte{1, 0, 0, 0, 5},
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:    "header only, one byte",
			input:   []byte{1},
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:     "truncated body",
			input:    frame(MessagePong, 16, []byte("only-four")),
			wantErr:  io.ErrUnexpectedEOF,
			wantType: MessagePong,
		},
		{
			name:     "EOF mid-stream after a complete frame",
			input:    append(frame(MessagePing, 0, nil), frame(MessagePong, 8, []byte{1, 2})...),
			wantErr:  io.ErrUnexpectedEOF,
			wantType: MessagePong,
		},
		{
			name:     "length one over the cap",
			input:    frame(MessagePong, MaxMessageLength+1, nil),
			wantErr:  ErrMessageTooLarge,
			wantType: MessagePong,
		},
		{
			name:     "length at max int32",
			input:    frame(MessagePong, 0x7fffffff, nil),
			wantErr:  ErrMessageTooLarge,
			wantType: MessagePong,
		},
		{
			name:     "negative length",
			input:    frame(MessagePong, -1, nil),
			wantErr:  ErrNegativeLength,
			wantType: MessagePong,
		},
		{
			name:     "negative length, most negative int32",
			input:    frame(MessagePing, -2147483648, nil),
			wantErr:  ErrNegativeLength,
			wantType: MessagePing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fromBytes(tc.input)
			var (
				gotType int32
				err     error
			)
			// "EOF mid-stream" needs the first, valid frame consumed first.
			if tc.name == "EOF mid-stream after a complete frame" {
				if _, _, e := c.Recv(); e != nil {
					t.Fatalf("first Recv: %v", e)
				}
			}
			gotType, _, err = c.Recv()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantType != 0 && gotType != tc.wantType {
				t.Errorf("type = %d, want %d", gotType, tc.wantType)
			}
		})
	}
}

// TestOverCapDoesNotAllocate is the load-bearing one: a hostile length must be
// rejected before the payload buffer is made, or the device OOMs.
func TestOverCapDoesNotAllocate(t *testing.T) {
	c := fromBytes(frame(MessagePong, 0x7fffffff, nil))
	allocs := testing.AllocsPerRun(50, func() {
		c2 := fromBytes(frame(MessagePong, 0x7fffffff, nil))
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
	var buf bytes.Buffer
	c := NewConn(&writerConn{w: &buf})
	if err := c.Send(-1, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if len(got) != headerSize+2 {
		t.Fatalf("frame len = %d, want %d", len(got), headerSize+2)
	}
	if binary.NativeEndian.Uint32(got[0:4]) != uint32(0xffffffff) {
		t.Errorf("type bytes = % x, want ff ff ff ff", got[0:4])
	}
	if int32(binary.NativeEndian.Uint32(got[4:8])) != 2 {
		t.Errorf("length bytes = % x, want 2", got[4:8])
	}
	if string(got[8:]) != "hi" {
		t.Errorf("body = %q", got[8:])
	}
}

type writerConn struct {
	net.Conn
	w io.Writer
}

func (wc *writerConn) Write(p []byte) (int, error) { return wc.w.Write(p) }
func (wc *writerConn) Close() error                { return nil }

// TestPartialWritesAreReassembled drives Recv through a reader that returns one
// byte at a time, exercising the io.ReadFull loops.
func TestPartialWritesAreReassembled(t *testing.T) {
	payload := bytes.Repeat([]byte("abcd"), 500)
	c := NewConn(&readerConn{r: iotest1{r: bytes.NewReader(frame(MessagePong, int32(len(payload)), payload))}})
	typ, got, err := c.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if typ != MessagePong || !bytes.Equal(got, payload) {
		t.Errorf("type=%d len=%d", typ, len(got))
	}
}

// iotest1 returns at most one byte per Read.
type iotest1 struct{ r io.Reader }

func (o iotest1) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
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
