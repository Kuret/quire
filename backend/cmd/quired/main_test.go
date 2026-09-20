package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/shelfmark"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReadUptime(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    float64
		wantErr bool
	}{
		// Shape observed in /proc/uptime on the device: two floats.
		{"device shape", "12345.67 98765.43\n", 12345.67, false},
		{"no trailing newline", "1.00 2.00", 1, false},
		{"single field", "42.5", 42.5, false},
		{"just booted", "0.01 0.00\n", 0.01, false},
		{"integer", "7 8\n", 7, false},
		{"empty file", "", 0, true},
		{"whitespace only", "   \n", 0, true},
		{"not a number", "abc def\n", 0, true},
		{"negative", "-1.0 0.0\n", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "uptime")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := readUptime(path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadUptimeMissingFile(t *testing.T) {
	if _, err := readUptime(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("want an error for a missing file")
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0s"},
		{0.4, "0s"},
		{9, "9s"},
		{59.9, "1m 00s"},
		{60, "1m 00s"},
		{125, "2m 05s"},
		{3600, "1h 00m 00s"},
		{3725, "1h 02m 05s"},
		{86400, "1d 0h 00m 00s"},
		{183845, "2d 3h 04m 05s"},
	}
	for _, tc := range tests {
		if got := formatUptime(tc.in); got != tc.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPingAnswersPongWithUptime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uptime")
	if err := os.WriteFile(path, []byte("3725.00 1.00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := uptimePath
	uptimePath = path
	t.Cleanup(func() { uptimePath = old })

	a, b := net.Pipe()
	server, client := appload.NewConn(a), appload.NewConn(b)
	t.Cleanup(func() { server.Close(); client.Close() })

	done := make(chan error, 1)
	go func() { done <- serve(server, discardLogger(), nil) }()

	if err := client.Send(appload.MessagePing, nil); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := client.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if typ != appload.MessagePong {
		t.Fatalf("type = %d, want Pong", typ)
	}
	var s status
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatalf("Pong payload is not JSON: %v (%q)", err, payload)
	}
	if !s.OK {
		t.Error("ok = false")
	}
	if s.UptimeSeconds != 3725 {
		t.Errorf("uptimeSeconds = %v, want 3725", s.UptimeSeconds)
	}
	if s.Uptime != "1h 02m 05s" {
		t.Errorf("uptime = %q", s.Uptime)
	}
	if s.UptimeError != "" {
		t.Errorf("uptimeError = %q", s.UptimeError)
	}
	if s.GoVersion == "" || s.Arch == "" || s.PID == 0 {
		t.Errorf("status is missing process identity: %+v", s)
	}

	// Terminate must end the loop cleanly.
	if err := client.Send(appload.MessageSystemTerminate, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve returned %v, want nil after terminate", err)
	}
}

func TestUnimplementedTypeGetsAnError(t *testing.T) {
	a, b := net.Pipe()
	server, client := appload.NewConn(a), appload.NewConn(b)
	t.Cleanup(func() { server.Close(); client.Close() })

	done := make(chan error, 1)
	go func() { done <- serve(server, discardLogger(), nil) }()

	if err := client.Send(appload.MessageSearch, []byte(`{"sourceId":"x","query":"y"}`)); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := client.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if typ != appload.MessageError {
		t.Fatalf("type = %d, want Error", typ)
	}
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "not_implemented" {
		t.Errorf("code = %q", e.Code)
	}

	client.Close()
	if err := <-done; err != nil {
		t.Errorf("serve returned %v, want nil after peer close", err)
	}
}

// A frontend detaching stays silent: there is nobody to tell.
func TestLostCoordinatorIsSilent(t *testing.T) {
	a, b := net.Pipe()
	server, client := appload.NewConn(a), appload.NewConn(b)
	t.Cleanup(func() { server.Close(); client.Close() })

	done := make(chan error, 1)
	go func() { done <- serve(server, discardLogger(), nil) }()

	if err := client.Send(appload.MessageSystemLostCoordinator, nil); err != nil {
		t.Fatal(err)
	}
	// Nothing should have been written back; a following Ping must be the
	// first thing we read.
	if err := client.Send(appload.MessagePing, nil); err != nil {
		t.Fatal(err)
	}
	typ, _, err := client.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if typ != appload.MessagePong {
		t.Fatalf("type = %d, want Pong (a coordinator message got a reply)", typ)
	}

	client.Close()
	<-done
}

// A frontend *attaching* is answered unprompted, which is the whole fix for
// the startup race: AppLoad discards messages aimed at a backend whose socket
// is not up yet, so a request sent from QML's Component.onCompleted is
// sometimes thrown away and the UI waits forever for a reply that will never
// come. The frontend arriving is the earliest moment a send can succeed.
func TestAttachIsAnsweredUnprompted(t *testing.T) {
	a, b := net.Pipe()
	server, client := appload.NewConn(a), appload.NewConn(b)
	t.Cleanup(func() { server.Close(); client.Close() })

	done := make(chan error, 1)
	go func() { done <- serve(server, discardLogger(), nil) }()

	if err := client.Send(appload.MessageSystemNewCoordinator, nil); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := client.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if typ != appload.MessagePong {
		t.Fatalf("type = %d, want Pong pushed on attach", typ)
	}
	var st status
	if err := json.Unmarshal(payload, &st); err != nil {
		t.Fatalf("pushed status is not JSON: %v (%q)", err, payload)
	}
	if !st.OK {
		t.Error("ok = false")
	}

	client.Close()
	<-done
}

func TestStatusReportsUptimeError(t *testing.T) {
	old := uptimePath
	uptimePath = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() { uptimePath = old })

	s := newStatus()
	if s.UptimeError == "" {
		t.Error("want uptimeError set when /proc/uptime is unreadable")
	}
	if s.Uptime != "unknown" {
		t.Errorf("uptime = %q, want %q", s.Uptime, "unknown")
	}
}

// capture is a conn that keeps what was sent and never expects a Recv.
type capture struct {
	msgType int32
	payload []byte
}

func (c *capture) Send(msgType int32, payload []byte) error {
	c.msgType = msgType
	c.payload = append([]byte(nil), payload...)
	return nil
}
func (c *capture) Recv() (int32, []byte, error) { return 0, nil, io.EOF }
func (c *capture) Close() error                 { return nil }

// The per-screen views ride on the status (PLAN §7.1 type 75), because a screen
// that draws a grid and then rearranges itself into a list once a second round
// trip lands is worse than one that waits.
func TestStatusCarriesThePerScreenViews(t *testing.T) {
	store, err := state.Open(t.TempDir(), theme.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetView(state.ScreenSearch, state.ViewList); err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{Store: store})
	t.Cleanup(svc.Close)

	c := &capture{}
	if err := send(c, discardLogger(), svc, nil); err != nil {
		t.Fatal(err)
	}
	if c.msgType != appload.MessagePong {
		t.Fatalf("type = %d, want Pong", c.msgType)
	}
	var s status
	if err := json.Unmarshal(c.payload, &s); err != nil {
		t.Fatalf("Pong payload is not JSON: %v (%q)", err, c.payload)
	}
	if s.Views[state.ScreenSearch] != state.ViewList {
		t.Errorf("status says search is %q, want %q", s.Views[state.ScreenSearch], state.ViewList)
	}
	if s.Views[state.ScreenDownloaded] != state.ViewGrid {
		t.Errorf("status says downloaded is %q, want the default %q",
			s.Views[state.ScreenDownloaded], state.ViewGrid)
	}
	if len(s.Views) != len(state.Screens) {
		t.Errorf("status carries %d screens, want %d: %v", len(s.Views), len(state.Screens), s.Views)
	}
}

// Every theme in the repository has to be *registered* to exist as far as the
// app is concerned: the probe picks a theme by fingerprinting against the
// registry, so one that is not in it can never be chosen, however well its own
// package is tested. shelfmark was written a whole milestone before anything
// registered it (2026-09-20), which is what this test is here to stop
// happening again.
func TestEveryShippedThemeIsRegistered(t *testing.T) {
	reg := themeRegistry(fetch.NewClient(fetch.Options{Version: "test", Logger: discardLogger()}))

	for _, id := range []string{
		"madara", "mangathemesia", "mangakakalot", "mangadex", "weebcentral",
		"webtoons", "fanfox", "comick", "generic",
		// The file theme. A book source is unusable without it registered, and
		// the symptom — "Quire doesn't recognise this site's layout" — names
		// every other theme and gives no hint that this one exists.
		shelfmark.ID,
	} {
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("the %s theme is not registered, so no source can ever use it", id)
		}
	}
}
