package annex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
)

// TestSystemTypesMatchAppLoad pins the numbering. The claim in the package
// comment is that the protocol above the transport is unchanged; if these ever
// drift, that claim is false and a backend swapping transports would
// mysteriously stop pushing on attach.
func TestSystemTypesMatchAppLoad(t *testing.T) {
	cases := []struct {
		name          string
		annex, upload int32
	}{
		{"Terminate", SystemTerminate, appload.MessageSystemTerminate},
		{"NewCoordinator", SystemNewCoordinator, appload.MessageSystemNewCoordinator},
		{"LostCoordinator", SystemLostCoordinator, appload.MessageSystemLostCoordinator},
	}
	for _, c := range cases {
		if c.annex != c.upload {
			t.Errorf("%s: annex has %d, appload has %d", c.name, c.annex, c.upload)
		}
	}
	if MaxMessageLength != appload.MaxMessageLength {
		t.Errorf("MaxMessageLength: annex %d, appload %d", MaxMessageLength, appload.MaxMessageLength)
	}
}

// TestConnSatisfiesSender is the compile-time promise that makes M3 a
// transport swap: the service's Sender is Send(int32, []byte) error and
// nothing more.
func TestConnSatisfiesSender(t *testing.T) {
	var _ interface {
		Send(int32, []byte) error
	} = (*Conn)(nil)
}

// client is a frontend, as the QML side will behave.
type client struct {
	t     *testing.T
	base  string
	token string
	id    string
	http  *http.Client
}

func dial(t *testing.T, c *Conn, id string) *client {
	t.Helper()
	return &client{
		t:     t,
		base:  fmt.Sprintf("http://127.0.0.1:%d", c.Port()),
		token: c.Token(),
		id:    id,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (cl *client) do(method, path string, body []byte, hdr map[string]string) *http.Response {
	cl.t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, cl.base+path, r)
	if err != nil {
		cl.t.Fatal(err)
	}
	req.Header.Set(HeaderToken, cl.token)
	req.Header.Set(HeaderClient, cl.id)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := cl.http.Do(req)
	if err != nil {
		cl.t.Fatal(err)
	}
	return resp
}

func (cl *client) send(msgType int32, payload string) {
	cl.t.Helper()
	resp := cl.do("POST", "/msg", []byte(payload), map[string]string{
		HeaderType: strconv.Itoa(int(msgType)),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		cl.t.Fatalf("POST /msg: %s: %s", resp.Status, b)
	}
}

func (cl *client) poll(waitMS int) []wireFrame {
	cl.t.Helper()
	resp := cl.do("GET", "/events?wait="+strconv.Itoa(waitMS), nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		cl.t.Fatalf("GET /events: %s: %s", resp.Status, b)
	}
	var out eventsBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		cl.t.Fatalf("decode events: %v", err)
	}
	return out.Messages
}

// pollQuietly is for polls a test parks on purpose and does not intend to
// read: it reports nothing, so a request torn down by Close at cleanup time
// cannot call t.Fatalf from a goroutine after the test has finished.
func (cl *client) pollQuietly(waitMS int) {
	resp, err := func() (*http.Response, error) {
		req, err := http.NewRequest("GET", cl.base+"/events?wait="+strconv.Itoa(waitMS), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set(HeaderToken, cl.token)
		req.Header.Set(HeaderClient, cl.id)
		return cl.http.Do(req)
	}()
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func (cl *client) detach() {
	cl.t.Helper()
	resp := cl.do("POST", "/detach", nil, nil)
	resp.Body.Close()
}

func listen(t *testing.T, opts Options) *Conn {
	t.Helper()
	if opts.AppID == "" {
		opts.AppID = "test"
	}
	if opts.RunDir == "" {
		opts.RunDir = t.TempDir()
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	c, err := Listen(opts)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// recvWithin fails rather than hanging the suite.
func recvWithin(t *testing.T, c *Conn, d time.Duration) (int32, []byte, error) {
	t.Helper()
	type result struct {
		t   int32
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		mt, b, err := c.Recv()
		ch <- result{mt, b, err}
	}()
	select {
	case r := <-ch:
		return r.t, r.b, r.err
	case <-time.After(d):
		t.Fatalf("Recv did not return within %s", d)
		return 0, nil, nil
	}
}

func TestEndpointFile(t *testing.T) {
	run := t.TempDir()
	c := listen(t, Options{AppID: "quire", RunDir: run})

	path := filepath.Join(run, "quire.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("endpoint file: %v", err)
	}
	var ep endpointFile
	if err := json.Unmarshal(b, &ep); err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if ep.App != "quire" {
		t.Errorf("app = %q, want quire", ep.App)
	}
	if ep.Port != c.Port() {
		t.Errorf("port = %d, want %d", ep.Port, c.Port())
	}
	if ep.Token != c.Token() || len(ep.Token) != 64 {
		t.Errorf("token = %q (len %d)", ep.Token, len(ep.Token))
	}
	if ep.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", ep.PID, os.Getpid())
	}

	// 0600: the token is the only thing keeping a stray process from talking
	// to the backend by accident, so it must not be world-readable.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("endpoint mode = %o, want 600", perm)
	}

	// Removed on Close, before the port stops answering: a stale endpoint
	// file makes a frontend wait out a connection error instead of reporting
	// that the backend is not running.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("endpoint file still present after Close: %v", err)
	}
}

func TestPortIsLoopbackOnly(t *testing.T) {
	c := listen(t, Options{})
	// The listener address itself is the guarantee; asserting on it is what
	// stops someone "fixing" a connection problem by binding 0.0.0.0 and
	// quietly exposing an app backend over the USB cable.
	if got := c.ln.Addr().String(); !bytes.HasPrefix([]byte(got), []byte("127.0.0.1:")) {
		t.Errorf("listening on %q, want 127.0.0.1", got)
	}
}

func TestAttachThenMessageRoundTrip(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")

	// The first request of any kind is the attach — here, a poll.
	polled := make(chan struct{})
	go func() { defer close(polled); cl.pollQuietly(200) }()

	mt, payload, err := recvWithin(t, c, 2*time.Second)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if mt != SystemNewCoordinator {
		t.Fatalf("first message = %d, want SystemNewCoordinator", mt)
	}
	// Let that poll finish before sending anything, or it collects the
	// message the rest of the test is about.
	<-polled
	if len(payload) != 0 {
		t.Errorf("attach payload = %q, want empty", payload)
	}

	cl.send(42, `{"hello":"world"}`)
	mt, payload, err = recvWithin(t, c, 2*time.Second)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if mt != 42 || string(payload) != `{"hello":"world"}` {
		t.Errorf("got (%d, %q)", mt, payload)
	}

	if err := c.Send(41, []byte(`{"progress":3}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msgs := cl.poll(2000)
	if len(msgs) != 1 || msgs[0].Type != 41 || msgs[0].Data != `{"progress":3}` {
		t.Fatalf("poll returned %+v", msgs)
	}
	if msgs[0].B64 {
		t.Error("JSON payload was base64'd; it should stay readable")
	}
}

// TestLongPollWakesOnSend is the claim that makes long-polling worth it: a
// message queued while the frontend is parked is delivered at once, not at the
// deadline.
func TestLongPollWakesOnSend(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "") // attach
	recvWithin(t, c, 2*time.Second)

	done := make(chan []wireFrame, 1)
	start := time.Now()
	go func() { done <- cl.poll(10000) }()

	// Let the poll park, then answer it.
	time.Sleep(150 * time.Millisecond)
	if err := c.Send(2, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case msgs := <-done:
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("poll took %s; it should return as soon as a message is queued", elapsed)
		}
		if len(msgs) != 1 || msgs[0].Type != 2 {
			t.Fatalf("poll returned %+v", msgs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long poll did not return after a Send")
	}
}

func TestLongPollReturnsEmptyAtDeadline(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second)

	start := time.Now()
	msgs := cl.poll(300)
	if len(msgs) != 0 {
		t.Errorf("expected no messages, got %+v", msgs)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("returned after %s; it should have waited out the deadline", elapsed)
	}
}

func TestCleanDetach(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	if mt, _, _ := recvWithin(t, c, 2*time.Second); mt != SystemNewCoordinator {
		t.Fatalf("want attach, got %d", mt)
	}
	// Drop the app's own message.
	recvWithin(t, c, 2*time.Second)

	cl.detach()
	mt, _, err := recvWithin(t, c, 2*time.Second)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if mt != SystemLostCoordinator {
		t.Errorf("want SystemLostCoordinator, got %d", mt)
	}
}

// TestDetachDropsQueue: whatever was queued was aimed at a screen that no
// longer exists, and the backend re-pushes the world on reattach.
func TestDetachDropsQueue(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second)
	recvWithin(t, c, 2*time.Second)

	for i := range 5 {
		if err := c.Send(int32(100+i), []byte("stale")); err != nil {
			t.Fatal(err)
		}
	}
	cl.detach()
	recvWithin(t, c, 2*time.Second) // LostCoordinator

	next := dial(t, c, "frontend-2")
	if msgs := next.poll(200); len(msgs) != 0 {
		t.Errorf("reattached frontend was handed a stale backlog: %+v", msgs)
	}
}

// TestReplacedFrontendDetachesFirst: an app reloaded without a clean detach
// must still produce one, or a backend's detach handler silently never runs.
func TestReplacedFrontendDetachesFirst(t *testing.T) {
	c := listen(t, Options{})
	first := dial(t, c, "frontend-1")
	first.send(1, "")
	recvWithin(t, c, 2*time.Second) // attach 1
	recvWithin(t, c, 2*time.Second) // the message

	second := dial(t, c, "frontend-2")
	second.send(1, "")

	mt, _, _ := recvWithin(t, c, 2*time.Second)
	if mt != SystemLostCoordinator {
		t.Fatalf("first event = %d, want SystemLostCoordinator", mt)
	}
	mt, _, _ = recvWithin(t, c, 2*time.Second)
	if mt != SystemNewCoordinator {
		t.Fatalf("second event = %d, want SystemNewCoordinator", mt)
	}
}

// TestDetachTimeout is the backstop for a frontend that vanished. DetachAfter
// must exceed Wait or a parked long poll would be mistaken for a departure;
// this checks the timeout fires when there is genuinely nobody there.
func TestDetachTimeout(t *testing.T) {
	c := listen(t, Options{Wait: 50 * time.Millisecond, DetachAfter: 300 * time.Millisecond})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second)
	recvWithin(t, c, 2*time.Second)

	// Say nothing at all.
	mt, _, err := recvWithin(t, c, 3*time.Second)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if mt != SystemLostCoordinator {
		t.Errorf("want SystemLostCoordinator, got %d", mt)
	}
}

// TestParkedPollIsNotADeparture is the other half: silence *while polling* is
// how a healthy idle app looks.
func TestParkedPollIsNotADeparture(t *testing.T) {
	c := listen(t, Options{Wait: 600 * time.Millisecond, DetachAfter: 200 * time.Millisecond})
	cl := dial(t, c, "frontend-1")
	parked := make(chan struct{})
	go func() { defer close(parked); cl.pollQuietly(600) }()
	t.Cleanup(func() { <-parked })

	if mt, _, _ := recvWithin(t, c, 2*time.Second); mt != SystemNewCoordinator {
		t.Fatal("no attach")
	}

	// DetachAfter would have fired three times over by now if an outstanding
	// poll did not count as presence.
	select {
	case f := <-c.in:
		t.Fatalf("frontend was detached while it was polling: type %d", f.Type)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestRejectsBadToken(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.token = "0000000000000000000000000000000000000000000000000000000000000000"

	resp := cl.do("GET", "/events", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token got %s, want 401", resp.Status)
	}
}

func TestRejectsMissingClient(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "")
	resp := cl.do("GET", "/events", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing client got %s, want 400", resp.Status)
	}
}

// TestRejectsCrossOrigin: a browser has no business here, and refusing the
// whole class costs nothing.
func TestRejectsCrossOrigin(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	resp := cl.do("GET", "/events", nil, map[string]string{"Origin": "http://10.11.99.1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin got %s, want 403", resp.Status)
	}
}

func TestRejectsOversizedMessage(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")

	resp := cl.do("POST", "/msg", bytes.Repeat([]byte("x"), MaxMessageLength+1),
		map[string]string{HeaderType: "1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized got %s, want 413", resp.Status)
	}
}

func TestSendRejectsOversized(t *testing.T) {
	c := listen(t, Options{})
	err := c.Send(1, bytes.Repeat([]byte("x"), MaxMessageLength+1))
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Errorf("Send oversized: %v, want ErrMessageTooLarge", err)
	}
}

func TestBinaryPayloadIsBase64(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second)
	recvWithin(t, c, 2*time.Second)

	raw := []byte{0xff, 0xfe, 0x00, 0x01}
	if err := c.Send(7, raw); err != nil {
		t.Fatal(err)
	}
	msgs := cl.poll(2000)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if !msgs[0].B64 {
		t.Fatalf("binary payload was not marked b64: %+v", msgs[0])
	}
	got, err := base64.StdEncoding.DecodeString(msgs[0].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("round trip changed the bytes: %v", got)
	}
}

func TestEmptyPayloadRoundTrip(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second)

	mt, payload, err := recvWithin(t, c, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if mt != 1 || len(payload) != 0 {
		t.Errorf("got (%d, %q), want (1, empty)", mt, payload)
	}
}

// TestQueueDropsOldest documents the behaviour rather than merely exercising
// it: a full queue means the frontend stopped collecting, and blocking Send
// there would wedge a download worker behind a UI that is never coming back.
func TestQueueDropsOldest(t *testing.T) {
	c := listen(t, Options{Queue: 4})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second)
	recvWithin(t, c, 2*time.Second)

	for i := range 10 {
		if err := c.Send(int32(i), nil); err != nil {
			t.Fatal(err)
		}
	}
	msgs := cl.poll(500)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want the queue bound of 4", len(msgs))
	}
	// The newest four survive, in order.
	for i, m := range msgs {
		if want := int32(6 + i); m.Type != want {
			t.Errorf("msgs[%d].Type = %d, want %d", i, m.Type, want)
		}
	}
}

func TestSendQueuesBeforeAnyFrontend(t *testing.T) {
	c := listen(t, Options{})
	// A backend should not have to care whether a screen is showing it.
	if err := c.Send(11, []byte(`["sources"]`)); err != nil {
		t.Fatalf("Send with nobody attached: %v", err)
	}
	cl := dial(t, c, "frontend-1")
	msgs := cl.poll(500)
	if len(msgs) != 1 || msgs[0].Type != 11 {
		t.Errorf("queued message was not delivered on attach: %+v", msgs)
	}
}

// TestCloseEndsRecv: Close must produce the same clean-shutdown signal
// appload.Conn gives when the host closes the socket, or quired's serve loop
// reports a crash on every ordinary systemctl stop.
func TestCloseEndsRecv(t *testing.T) {
	c := listen(t, Options{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		c.Close()
	}()
	_, _, err := recvWithin(t, c, 2*time.Second)
	if !errors.Is(err, io.EOF) {
		t.Errorf("Recv after Close: %v, want io.EOF", err)
	}
}

func TestSendAfterCloseFails(t *testing.T) {
	c := listen(t, Options{})
	c.Close()
	if err := c.Send(1, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after Close: %v, want ErrClosed", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c := listen(t, Options{})
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestListenNeedsAppID(t *testing.T) {
	if _, err := Listen(Options{RunDir: t.TempDir()}); err == nil {
		t.Error("Listen with no AppID should fail")
	}
}

// TestWireContractIsLiteral pins the exact strings the QML client depends on.
//
// Every other test in this file round-trips through wireFrame and eventsBody,
// so renaming a JSON field would change both sides at once and stay green —
// while silently breaking lib/Backend.qml, which reads these names by hand and
// has no compiler to catch it. This test reads the raw bytes instead.
//
// If you change anything here, change lib/Backend.qml in the same commit.
func TestWireContractIsLiteral(t *testing.T) {
	if HeaderToken != "X-Annex-Token" || HeaderClient != "X-Annex-Client" || HeaderType != "X-Annex-Type" {
		t.Fatalf("header names changed: %q %q %q", HeaderToken, HeaderClient, HeaderType)
	}

	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")
	cl.send(1, "")
	recvWithin(t, c, 2*time.Second) // attach
	recvWithin(t, c, 2*time.Second) // the message

	if err := c.Send(41, []byte(`{"pct":50}`)); err != nil {
		t.Fatal(err)
	}

	resp := cl.do("GET", "/events?wait=2000", nil, nil)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	// Parsed as a bare map, so the field *names* are what is being checked.
	var body map[string][]map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("events body is not the shape QML parses: %v\n%s", err, raw)
	}
	msgs, ok := body["messages"]
	if !ok {
		t.Fatalf(`no "messages" key; Backend.qml reads body.messages: %s`, raw)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages: %s", len(msgs), raw)
	}
	m := msgs[0]

	// QML compares m.type to an integer message id.
	typ, ok := m["type"].(float64)
	if !ok || int32(typ) != 41 {
		t.Errorf(`"type" is %#v, want the number 41`, m["type"])
	}
	// QML passes m.data to JSON.parse, so it must be a string, not an object.
	data, ok := m["data"].(string)
	if !ok || data != `{"pct":50}` {
		t.Errorf(`"data" is %#v, want the string {"pct":50}`, m["data"])
	}
	// b64 is absent rather than false for text, which is what lets
	// `m.b64 === true` be the whole check on the QML side.
	if _, present := m["b64"]; present {
		t.Errorf(`"b64" should be omitted for a UTF-8 payload, got %#v`, m["b64"])
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// TestEmptyEventsBodyIsAnArray: Backend.qml iterates body.messages without a
// null check beyond `body && body.messages`, so an idle poll must answer with
// an empty array rather than null or an absent key.
func TestEmptyEventsBodyIsAnArray(t *testing.T) {
	c := listen(t, Options{})
	cl := dial(t, c, "frontend-1")

	resp := cl.do("GET", "/events?wait=100", nil, nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	msgs, ok := body["messages"]
	if !ok {
		t.Fatalf(`no "messages" key: %s`, raw)
	}
	if _, isSlice := msgs.([]any); !isSlice {
		t.Errorf(`"messages" is %#v, want an array`, msgs)
	}
}
