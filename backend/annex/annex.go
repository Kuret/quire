// Package annex implements the Annex backend transport: the AppLoad message
// protocol tunnelled over loopback HTTP.
//
// # Why not a unix socket
//
// AppLoad's transport is a SOCK_SEQPACKET unix socket that the *host* creates
// and hands to the backend as argv[1]. Reproducing that under Annex would mean
// a compiled xovi extension — something has to call socketpair(), fork the
// backend and relay bytes into QML, and QML can do none of those. A compiled
// extension means a C cross-toolchain and CI to keep it alive, for every
// developer, forever.
//
// Annex has no compiled artefact anywhere, and this package is why. systemd
// starts the backend, the backend binds a loopback port, and QML talks to it
// with XMLHttpRequest — which it is already doing to read app manifests. An
// app that needs no backend of its own (a monitor for some HTTP API, say)
// writes plain QML and skips all of this rather than shipping a relay process
// that exists only to satisfy the transport.
//
// # What is preserved
//
// Everything above the transport. A message is still (int32 type, []byte
// payload); the system types still have AppLoad's numbering; Conn still offers
// exactly Send and Recv, with the same semantics and the same clean-shutdown
// signal. A backend written against appload.Conn compiles against annex.Conn
// with the import changed and nothing else, which is the entire point: the
// protocol is documented and reusable, and it did not dissolve into per-app
// REST when the transport under it changed.
//
// # The wire
//
//	POST /msg      X-Annex-Type: <int32>   body: the payload bytes
//	               → 204. Blocks while the backend is busy; that is backpressure.
//	GET  /events?wait=<ms>
//	               → 200 {"messages":[{"type":41,"data":"…"}]}
//	               Long-poll: returns as soon as anything is queued, or empty
//	               at the deadline. Not SSE — every response is complete, so
//	               the QML side is an ordinary XHR it re-issues in onload.
//	POST /detach   → 204. Clean teardown; the timeout is only the backstop.
//
// Every request carries X-Annex-Token and X-Annex-Client. A frame's `data` is
// a string, with "b64":true and base64 when the payload is not valid UTF-8 —
// so the common case (JSON) stays readable under curl, and binary is still
// carried honestly rather than mangled.
//
// # Long-poll rather than fixed-interval polling
//
// Both are "polling" from the frontend's side; the difference is only whether
// the server answers immediately with nothing. Holding the request costs one
// idle connection and delivers download progress at once, where a 1s poll
// spends 25 requests per idle 25 seconds to do the same job slower. If the
// long poll ever misbehaves on the device, Wait=0 turns this into exactly the
// fixed-interval poll, with no change on either side.
package annex

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// System message types. These are AppLoad's numbering, deliberately: the
// protocol above the transport is unchanged, and a backend that switched
// transports should not have to relearn what -2 means.
//
// TestSystemTypesMatchAppLoad pins them to backend/appload.
const (
	// SystemTerminate is not sent over this transport. Under AppLoad the host
	// sends it; under Annex the backend is a systemd service and the
	// equivalent is SIGTERM, which the caller turns into Close. It is defined
	// so the numbering stays a complete description of the protocol.
	SystemTerminate int32 = -1
	// SystemNewCoordinator is delivered by Recv when a frontend attaches.
	SystemNewCoordinator int32 = -2
	// SystemLostCoordinator is delivered by Recv when it goes away.
	SystemLostCoordinator int32 = -3
)

// MaxMessageLength is AppLoad's MAX_MESSAGE_LENGTH, kept as the cap here too.
// Nothing about HTTP requires it; the point is that a message which was legal
// under one transport is legal under the other.
const MaxMessageLength = 10485760

// Defaults for Options.
const (
	// DefaultWait is how long GET /events holds a request with nothing to say.
	DefaultWait = 20 * time.Second
	// DefaultDetachAfter must exceed DefaultWait: a frontend sitting in a long
	// poll is silent by design, and treating that as a departure would detach
	// a perfectly healthy app every 20 seconds.
	DefaultDetachAfter = 45 * time.Second
	// DefaultQueue bounds the outbound backlog. See enqueue for what happens
	// when it fills.
	DefaultQueue = 512
)

// ErrClosed is returned by Send after Close.
var ErrClosed = errors.New("annex: connection closed")

// ErrMessageTooLarge is returned when a payload exceeds MaxMessageLength.
var ErrMessageTooLarge = errors.New("annex: message length exceeds 10 MiB cap")

// Options configures Listen.
type Options struct {
	// AppID is the manifest id. It names the endpoint file, so it must match
	// what the frontend looks for.
	AppID string

	// RunDir is where the endpoint file is written. Empty means $ANNEX_RUN,
	// then /home/root/annex/run.
	RunDir string

	// Wait, DetachAfter and Queue default as documented above.
	Wait        time.Duration
	DetachAfter time.Duration
	Queue       int

	Log *slog.Logger
}

// frame is one message in flight.
type frame struct {
	Type int32
	Data []byte
}

// Conn is a framed message connection to an Annex frontend.
//
// It is the same shape as appload.Conn: Recv from a single goroutine, Send
// from any. Unlike appload.Conn it is a server — it exists before a frontend
// does, and outlives any particular one.
type Conn struct {
	opts Options
	log  *slog.Logger

	srv      *http.Server
	ln       net.Listener
	token    string
	endpoint string // path of the endpoint file, removed on Close

	in chan frame

	mu     sync.Mutex
	out    []frame
	notify chan struct{} // cap 1; signalled when out becomes non-empty
	closed bool

	// client is the attached frontend's id, empty when none is attached.
	// polls counts its outstanding long polls: a frontend sitting in one is
	// silent but present, which is why lastSeen alone cannot decide this.
	client   string
	polls    int
	lastSeen time.Time

	dropped int // outbound frames discarded because the queue was full
}

// Listen binds a loopback port, publishes the endpoint file and starts
// serving. The returned Conn is usable immediately; messages sent before a
// frontend attaches are queued, not lost.
func Listen(opts Options) (*Conn, error) {
	if opts.AppID == "" {
		return nil, errors.New("annex: Listen needs an AppID")
	}
	if opts.Wait <= 0 {
		opts.Wait = DefaultWait
	}
	if opts.DetachAfter <= 0 {
		opts.DetachAfter = DefaultDetachAfter
	}
	if opts.Queue <= 0 {
		opts.Queue = DefaultQueue
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.RunDir == "" {
		opts.RunDir = runDir()
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}

	// Port 0: the kernel assigns. This is the whole of Annex's port
	// allocation story — there is no registry, no range and nothing in the
	// manifest, so two apps cannot collide and an app cannot squat a port
	// something else wanted. The cost is that the port is not knowable in
	// advance, which is what the endpoint file is for.
	//
	// 127.0.0.1 and not 0.0.0.0: the tablet already exposes a web interface on
	// 10.11.99.1 over the cable, and an app backend must not become a second
	// listener reachable from it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("annex: listen on loopback: %w", err)
	}

	c := &Conn{
		opts:   opts,
		log:    opts.Log.With("transport", "annex", "app", opts.AppID),
		ln:     ln,
		token:  token,
		in:     make(chan frame, 64),
		notify: make(chan struct{}, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /msg", c.guard(c.handleMsg))
	mux.HandleFunc("GET /events", c.guard(c.handleEvents))
	mux.HandleFunc("POST /detach", c.guard(c.handleDetach))
	c.srv = &http.Server{
		Handler: mux,
		// A long poll legitimately holds a request for Wait; anything past
		// that plus a margin is stuck.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      opts.Wait + 30*time.Second,
	}

	port := ln.Addr().(*net.TCPAddr).Port
	if err := c.writeEndpoint(port); err != nil {
		ln.Close()
		return nil, err
	}

	go func() {
		if err := c.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.log.Error("http server stopped", "err", err)
		}
	}()
	go c.watchDetach()

	c.log.Info("listening", "port", port, "endpoint", c.endpoint)
	return c, nil
}

// Port is the assigned loopback port. Useful in tests and in logs.
func (c *Conn) Port() int { return c.ln.Addr().(*net.TCPAddr).Port }

// Token is the shared secret from the endpoint file.
func (c *Conn) Token() string { return c.token }

// Send queues one message for the frontend.
//
// It never blocks and never fails because nobody is listening: a backend
// should not have to care whether the screen is showing its app. Messages
// queued with no frontend attached are dropped at attach time — see detach.
func (c *Conn) Send(msgType int32, payload []byte) error {
	if len(payload) > MaxMessageLength {
		return fmt.Errorf("annex: send type %d: %w (%d bytes)", msgType, ErrMessageTooLarge, len(payload))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.enqueue(frame{Type: msgType, Data: payload})
	return nil
}

// SendString is a convenience wrapper, matching appload.Conn.
func (c *Conn) SendString(msgType int32, payload string) error {
	return c.Send(msgType, []byte(payload))
}

// enqueue appends one frame, dropping the oldest if the queue is full.
//
// Dropping the oldest rather than blocking is the lesser of two bad options. A
// full queue means the frontend has stopped collecting — it crashed, or the
// device suspended mid-download — and a Send that blocked there would wedge
// the download worker behind a UI that is never coming back. Dropping the
// newest instead would keep a stale backlog and discard the current state,
// which is exactly backwards.
//
// It is survivable because every screen Quire draws can be rebuilt from an
// attach push: a frontend that missed messages reattaches and is sent the
// world again. It is still logged, because if this fires regularly the queue
// is too small or something upstream is sending far too much.
func (c *Conn) enqueue(f frame) {
	if len(c.out) >= c.opts.Queue {
		c.out = c.out[1:]
		c.dropped++
		if c.dropped == 1 || c.dropped%100 == 0 {
			c.log.Warn("outbound queue full, dropping oldest",
				"queue", c.opts.Queue, "droppedTotal", c.dropped)
		}
	}
	c.out = append(c.out, f)
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// Recv returns the next message from the frontend, blocking until one arrives.
//
// It returns io.EOF, unwrapped, after Close — the same clean-shutdown signal
// appload.Conn gives when the host closes the socket, so a serve loop written
// for one works unchanged on the other.
//
// System messages (SystemNewCoordinator, SystemLostCoordinator) are delivered
// inline, in order, exactly as AppLoad delivers them.
func (c *Conn) Recv() (int32, []byte, error) {
	f, ok := <-c.in
	if !ok {
		return 0, nil, io.EOF
	}
	return f.Type, f.Data, nil
}

// Close stops serving, removes the endpoint file and makes Recv return io.EOF.
//
// The endpoint file goes first and deliberately: while it exists it is an
// advertisement for a port that is about to stop answering, and a frontend
// that reads a stale one waits out a connection error instead of saying the
// backend is not running.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.out = nil
	c.mu.Unlock()

	if c.endpoint != "" {
		if err := os.Remove(c.endpoint); err != nil && !os.IsNotExist(err) {
			c.log.Warn("could not remove the endpoint file", "path", c.endpoint, "err", err)
		}
	}
	err := c.srv.Close()
	close(c.in)
	select {
	case c.notify <- struct{}{}: // wake any parked long poll
	default:
	}
	c.log.Info("closed")
	return err
}

// runDir resolves where the endpoint file lives.
//
// Hardcoded rather than derived from $HOME or the XDG variables for the same
// reason backend/cmd/quired does it: a process started by systemd on this
// device inherits almost nothing, and deriving a path from an unset variable
// puts the file somewhere nobody will look.
func runDir() string {
	if d := os.Getenv("ANNEX_RUN"); d != "" {
		return d
	}
	return "/home/root/annex/run"
}

// newToken mints the per-process shared secret.
//
// What it is for, precisely: it stops anything that cannot read the endpoint
// file from talking to the backend. That covers the accident this design
// otherwise invites — a second app, a stray script, or a web page the user has
// open reaching a loopback port that answers to anyone who guesses it.
//
// What it is NOT: a security boundary against a hostile process on the tablet.
// Everything here runs as root, so any such process can simply read the file.
// Saying otherwise would be a lie in the one place a reader would rely on it.
// The real perimeter is that the port is bound to 127.0.0.1 and is therefore
// unreachable over the cable or the network at all.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("annex: generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// endpointFile is the JSON the frontend reads to find the backend.
type endpointFile struct {
	App   string `json:"app"`
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
	Since string `json:"since"`
}

// writeEndpoint publishes the port and token, atomically and 0600.
func (c *Conn) writeEndpoint(port int) error {
	if err := os.MkdirAll(c.opts.RunDir, 0o700); err != nil {
		return fmt.Errorf("annex: create %s: %w", c.opts.RunDir, err)
	}
	path := filepath.Join(c.opts.RunDir, c.opts.AppID+".json")

	body, err := json.Marshal(endpointFile{
		App:   c.opts.AppID,
		Port:  port,
		Token: c.token,
		PID:   os.Getpid(),
		Since: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("annex: marshal endpoint: %w", err)
	}

	// Temp file then rename: the frontend polls for this file and must never
	// read a half-written one. A truncating write would hand it an empty file
	// often enough to matter, and the failure looks like "the backend is not
	// running" rather than like a race.
	tmp := path + ".new"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("annex: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("annex: publish %s: %w", path, err)
	}
	c.endpoint = path
	return nil
}
