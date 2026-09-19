package annex

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

// Header names. X-Annex-Client identifies one frontend instance, so the
// backend can tell a reattach from a second poll by the same app.
const (
	HeaderToken  = "X-Annex-Token"
	HeaderClient = "X-Annex-Client"
	HeaderType   = "X-Annex-Type"
)

// guard authenticates a request and records that the frontend is alive.
func (c *Conn) guard(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A browser never has business here, and the one plausible way a page
		// reaches a loopback port is a form or a simple cross-origin request
		// from something the user happened to open. Refuse anything carrying
		// an Origin outright rather than relying on the token alone; it costs
		// nothing and removes the whole class.
		if r.Header.Get("Origin") != "" {
			http.Error(w, "annex: cross-origin requests are not served", http.StatusForbidden)
			return
		}
		got := r.Header.Get(HeaderToken)
		// Constant time, so a wrong token cannot be found a byte at a time.
		if subtle.ConstantTimeCompare([]byte(got), []byte(c.token)) != 1 {
			http.Error(w, "annex: bad or missing token", http.StatusUnauthorized)
			return
		}
		client := r.Header.Get(HeaderClient)
		if client == "" {
			http.Error(w, "annex: missing "+HeaderClient, http.StatusBadRequest)
			return
		}
		c.seen(client)
		next(w, r, client)
	}
}

// seen records a request from a frontend, emitting SystemNewCoordinator the
// first time a given client id appears.
//
// A *different* client id while one is attached means the app was unloaded and
// reloaded without a clean detach — the QML side crashed, or xochitl restarted
// the view. That is a departure followed by an arrival, and it is reported as
// both, in that order, so a backend's attach handler runs exactly once per
// frontend and its detach handler is never simply skipped.
func (c *Conn) seen(client string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.lastSeen = time.Now()
	if c.client == client {
		return
	}
	if c.client != "" {
		c.log.Info("frontend replaced without detaching", "old", c.client, "new", client)
		c.detachLocked()
	}
	c.client = client
	c.log.Info("frontend attached", "client", client)
	c.deliverLocked(frame{Type: SystemNewCoordinator})
}

// deliverLocked hands one message to Recv. It must not block while holding the
// lock: the receive loop calls Send, and a full inbound channel would deadlock
// the two against each other. A dropped system message is logged loudly
// because it is not recoverable by reattaching.
func (c *Conn) deliverLocked(f frame) {
	select {
	case c.in <- f:
	default:
		c.log.Error("inbound queue full, dropped a message",
			"type", f.Type, "bytes", len(f.Data))
	}
}

// handleMsg is one frontend→backend message.
func (c *Conn) handleMsg(w http.ResponseWriter, r *http.Request, client string) {
	raw := r.Header.Get(HeaderType)
	t, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		http.Error(w, "annex: bad "+HeaderType+": "+raw, http.StatusBadRequest)
		return
	}
	// Bounded read: a length the peer lied about must not be able to exhaust
	// memory on a device that shares 2 GB with xochitl.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxMessageLength))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "annex: "+ErrMessageTooLarge.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "annex: read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		http.Error(w, "annex: backend is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Blocking here is deliberate backpressure: the frontend's XHR waits while
	// the backend is busy, which is what the socket did too. It is bounded by
	// the client's own timeout rather than left forever, so a wedged backend
	// surfaces as a failed request instead of a hung app.
	select {
	case c.in <- frame{Type: int32(t), Data: body}:
		w.WriteHeader(http.StatusNoContent)
	case <-r.Context().Done():
		// The frontend gave up or went away; nothing to answer.
	}
}

// wireFrame is one message as the frontend sees it.
type wireFrame struct {
	Type int32  `json:"type"`
	Data string `json:"data,omitempty"`
	// B64 marks Data as base64 rather than text. Quire's payloads are all
	// JSON, so this never fires there — but the transport carries []byte and
	// silently mangling a non-UTF-8 payload would be a bug that only shows up
	// in whichever app first sends one.
	B64 bool `json:"b64,omitempty"`
}

type eventsBody struct {
	Messages []wireFrame `json:"messages"`
}

// handleEvents is the long poll.
func (c *Conn) handleEvents(w http.ResponseWriter, r *http.Request, client string) {
	wait := c.opts.Wait
	if raw := r.URL.Query().Get("wait"); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil || ms < 0 {
			http.Error(w, "annex: bad wait: "+raw, http.StatusBadRequest)
			return
		}
		if d := time.Duration(ms) * time.Millisecond; d <= c.opts.Wait {
			wait = d
		}
	}

	// A frontend parked in a long poll sends nothing for Wait seconds. Without
	// this count the detach watcher would see the silence and declare it gone,
	// every single time.
	c.mu.Lock()
	c.polls++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.polls--
		c.lastSeen = time.Now()
		c.mu.Unlock()
	}()

	deadline := time.NewTimer(wait)
	defer deadline.Stop()

	for {
		if msgs, ok := c.drain(); ok {
			writeJSON(w, eventsBody{Messages: msgs})
			return
		}
		select {
		case <-c.notify:
			// Something arrived, or Close woke us. Loop and look.
		case <-deadline.C:
			writeJSON(w, eventsBody{Messages: []wireFrame{}})
			return
		case <-r.Context().Done():
			return
		}
	}
}

// drain takes everything queued, or reports that there was nothing.
func (c *Conn) drain() ([]wireFrame, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		// Answer the poll rather than holding it: the frontend should see the
		// backend stop, not time out against a socket that is already gone.
		return []wireFrame{}, true
	}
	if len(c.out) == 0 {
		return nil, false
	}
	msgs := make([]wireFrame, 0, len(c.out))
	for _, f := range c.out {
		msgs = append(msgs, encode(f))
	}
	c.out = nil
	return msgs, true
}

// encode renders one frame for the wire, keeping text readable.
func encode(f frame) wireFrame {
	if len(f.Data) == 0 {
		return wireFrame{Type: f.Type}
	}
	if utf8.Valid(f.Data) {
		return wireFrame{Type: f.Type, Data: string(f.Data)}
	}
	return wireFrame{Type: f.Type, Data: base64.StdEncoding.EncodeToString(f.Data), B64: true}
}

// handleDetach is the frontend saying goodbye on its way out.
func (c *Conn) handleDetach(w http.ResponseWriter, r *http.Request, client string) {
	c.mu.Lock()
	if c.client == client {
		c.detachLocked()
	}
	c.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// detachLocked reports the frontend gone and throws away anything queued for
// it.
//
// Dropping the queue is the right call, not a shortcut. Whatever is in it was
// aimed at a screen that no longer exists, and a reattaching frontend is sent
// the whole world again by the backend's attach handler — so delivering the
// backlog would replay old progress over fresh state. It also means a backend
// that keeps working while nothing is attached cannot slowly fill memory with
// undeliverable messages.
func (c *Conn) detachLocked() {
	if c.client == "" {
		return
	}
	c.log.Info("frontend detached", "client", c.client, "queuedDropped", len(c.out))
	c.client = ""
	c.out = nil
	c.deliverLocked(frame{Type: SystemLostCoordinator})
}

// watchDetach notices a frontend that went away without saying so — the app
// was force-closed, xochitl restarted, the device suspended. The clean path is
// POST /detach; this is the backstop, and it is why DetachAfter must be longer
// than Wait.
func (c *Conn) watchDetach() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for range tick.C {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		if c.client != "" && c.polls == 0 && time.Since(c.lastSeen) > c.opts.DetachAfter {
			c.log.Info("frontend timed out", "silentFor", time.Since(c.lastSeen).Round(time.Second))
			c.detachLocked()
		}
		c.mu.Unlock()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// Nothing here should ever be cached, and the frontend re-issues this
	// request in a tight loop.
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The response is already begun; there is nowhere to report this but
		// the log, and the frontend will see a truncated body and retry.
		return
	}
}
