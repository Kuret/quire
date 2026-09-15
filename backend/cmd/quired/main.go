// Command quired is the Quire backend daemon.
//
// AppLoad starts it with argv[1] set to the path of a unix socket it has
// already created. quired connects, then serves the PLAN §7.1 message loop
// until the host sends MessageSystemTerminate or closes the socket.
//
// M1 wires up Ping/Pong only. Everything else is answered with MessageError so
// a frontend never hangs waiting for a reply that is not coming yet.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangathemesia"
)

// version is the build stamp; -ldflags "-X main.version=..." can override it.
var version = "dev"

// uptimePath is /proc/uptime, overridable in tests.
var uptimePath = "/proc/uptime"

func main() {
	// AppLoad gives the backend a pipe to xochitl's stderr, so structured
	// logging here lands in the xochitl journal.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log = log.With("app", "quired", "version", version)
	slog.SetDefault(log)

	if len(os.Args) < 2 {
		log.Error("no socket path given", "usage", "quired <appload-socket>")
		os.Exit(2)
	}
	socket := os.Args[1]

	log.Info("starting", "socket", socket, "go", runtime.Version(), "arch", runtime.GOARCH, "pid", os.Getpid())

	svc, err := newService(log)
	if err != nil {
		// Without a store there is nowhere to keep the user's sources, and
		// pretending otherwise would lose whatever they add.
		log.Error("could not start", "err", err)
		os.Exit(1)
	}

	conn, err := appload.Dial(socket)
	if err != nil {
		log.Error("connect failed", "err", err)
		os.Exit(1)
	}
	defer conn.Close()
	log.Info("connected")

	if err := serve(conn, log, svc); err != nil {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
	log.Info("exiting cleanly")
}

// serve runs the message loop. It returns nil for the two clean shutdown
// paths — host terminate and EOF — and an error for anything else.
func serve(conn *appload.Conn, log *slog.Logger, svc *service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		msgType, payload, err := conn.Recv()
		switch {
		case errors.Is(err, appload.ErrTerminated):
			log.Info("host requested termination")
			return nil
		case errors.Is(err, io.EOF):
			log.Info("host closed the socket")
			return nil
		case err != nil:
			return err
		}

		log.Debug("received", "type", msgType, "name", appload.Name(msgType), "bytes", len(payload))

		if err := handle(ctx, conn, log, svc, msgType, payload); err != nil {
			// A write failure means the socket is gone; stop.
			return err
		}
	}
}

func handle(ctx context.Context, conn *appload.Conn, log *slog.Logger, svc *service.Service, msgType int32, payload []byte) error {
	// Everything M3 added lives in the service. Ping and the host's own
	// messages stay here, where M1 put them.
	if svc != nil {
		handled, err := svc.Handle(ctx, conn, msgType, payload)
		if handled {
			return err
		}
	}

	switch msgType {
	case appload.MessagePing:
		status, err := json.Marshal(newStatus())
		if err != nil {
			// Cannot happen with a fixed struct, but never send a half frame.
			log.Error("marshal status", "err", err)
			return sendError(conn, "internal", "could not build status")
		}
		return conn.Send(appload.MessagePong, status)

	case appload.MessageSystemNewCoordinator:
		log.Info("frontend attached")
		return nil

	case appload.MessageSystemLostCoordinator:
		log.Info("frontend detached")
		return nil

	default:
		log.Warn("unimplemented message type", "type", msgType, "name", appload.Name(msgType))
		return sendError(conn, "not_implemented",
			fmt.Sprintf("%s is not implemented yet", appload.Name(msgType)))
	}
}

func sendError(conn *appload.Conn, code, message string) error {
	// PLAN §7.1: Error is JSON {code, message}.
	b, err := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message})
	if err != nil {
		return err
	}
	return conn.Send(appload.MessageError, b)
}

// status is the Pong payload: enough to prove a real Go process on the real
// device answered, not a QML-side stub.
type status struct {
	OK            bool    `json:"ok"`
	Version       string  `json:"version"`
	GoVersion     string  `json:"goVersion"`
	Arch          string  `json:"arch"`
	OS            string  `json:"os"`
	PID           int     `json:"pid"`
	UptimeSeconds float64 `json:"uptimeSeconds"`
	Uptime        string  `json:"uptime"`
	UptimeError   string  `json:"uptimeError,omitempty"`
	Now           string  `json:"now"`
}

func newStatus() status {
	s := status{
		OK:        true,
		Version:   version,
		GoVersion: runtime.Version(),
		Arch:      runtime.GOARCH,
		OS:        runtime.GOOS,
		PID:       os.Getpid(),
		Now:       time.Now().Format(time.RFC3339),
	}
	secs, err := readUptime(uptimePath)
	if err != nil {
		s.UptimeError = err.Error()
		s.Uptime = "unknown"
		return s
	}
	s.UptimeSeconds = secs
	s.Uptime = formatUptime(secs)
	return s
}

// readUptime parses the first field of /proc/uptime: seconds since boot as a
// float. The second field is idle time across all cores; ignore it.
func readUptime(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	field, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	if field == "" {
		return 0, fmt.Errorf("%s: empty", path)
	}
	secs, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: parse %q: %w", path, field, err)
	}
	if secs < 0 {
		return 0, fmt.Errorf("%s: negative uptime %v", path, secs)
	}
	return secs, nil
}

// formatUptime renders seconds as "2d 3h 04m 05s", dropping leading zero units.
func formatUptime(secs float64) string {
	d := time.Duration(secs * float64(time.Second)).Round(time.Second)

	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	d -= time.Duration(mins) * time.Minute
	sec := int(d / time.Second)

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %02dm %02ds", days, hours, mins, sec)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm %02ds", hours, mins, sec)
	case mins > 0:
		return fmt.Sprintf("%dm %02ds", mins, sec)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// dataDirEnv overrides where Quire keeps its state. The emulator and the tests
// use it; on the device the default is right.
const dataDirEnv = "QUIRE_DATA_DIR"

// dataDir is <app root>/data: AppLoad launches backend/entry from inside the
// app directory, which is under /home/root and is writable (docs/DEVICE-NOTES.md).
// Keeping state beside the app means an uninstall takes the state with it, and
// nothing of Quire's ever lands in the user's document tree.
func dataDir() (string, error) {
	if dir := os.Getenv(dataDirEnv); dir != "" {
		return dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the backend binary: %w", err)
	}
	return filepath.Join(filepath.Dir(filepath.Dir(exe)), "data"), nil
}

// newService wires the backend together: one guarded HTTP client, every theme,
// the source store and the cover cache.
//
// The single fetch.Client is the point. PLAN §7.4's invariants — the rate
// limits, robots, the SSRF guard, the byte budget — live in it, so every
// request Quire makes has to go through here. A second client anywhere would be
// a second network path with none of that.
func newService(log *slog.Logger) (*service.Service, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	client := fetch.NewClient(fetch.Options{Version: version})

	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(client))
	reg.MustRegister(mangathemesia.New(client))
	reg.MustRegister(generic.New(client))

	store, err := state.Open(filepath.Join(dir, "state"), reg)
	if err != nil {
		return nil, err
	}
	log.Info("state opened", "path", store.Path(), "sources", len(store.List()))

	return service.New(service.Options{
		Store:    store,
		Registry: reg,
		Fetcher:  client,
		Covers:   covers.New(filepath.Join(dir, "covers"), client),
		Log:      log,
	}), nil
}
