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
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/logging"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/mangakakalot"
	"github.com/rickl/quire/backend/theme/mangathemesia"
)

// logDir is where the rotating log lives, once it is known. Empty means
// logging to a file is not available, and the in-app viewer says so.
var logDir string

// version is the build stamp; -ldflags "-X main.version=..." can override it.
var version = "dev"

// uptimePath is /proc/uptime, overridable in tests.
var uptimePath = "/proc/uptime"

func main() {
	// AppLoad gives the backend a pipe to xochitl's stderr, so structured
	// logging here lands in the xochitl journal.
	// Logging goes to stderr *and* to a rotating file. AppLoad pipes stderr to
	// xochitl's journal, which is what anyone with SSH reads; the file is what
	// the person holding the tablet can read, from inside the app (PLAN §6 M7).
	// If the file cannot be opened that is not a reason to refuse to start —
	// it costs diagnosis, not function.
	var logFile *logging.Writer
	logOut := io.Writer(os.Stderr)
	if dir, err := dataDir(); err == nil {
		if w, err := logging.NewWriter(filepath.Join(dir, "logs")); err == nil {
			logFile = w
			logDir = filepath.Join(dir, "logs")
			logOut = io.MultiWriter(os.Stderr, w)
		}
	}
	log := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log = log.With("app", "quired", "version", version)
	slog.SetDefault(log)
	if logFile != nil {
		defer logFile.Close()
		log.Info("logging to file", "path", logFile.Path(), "maxBytes", logging.MaxBytes)
	}

	if len(os.Args) < 2 {
		log.Error("no socket path given", "usage", "quired <appload-socket>")
		os.Exit(2)
	}
	socket := os.Args[1]

	// Page resizing allocates in ~170 MB steps, and unbounded Go GC pacing on
	// a 2 GB device shared with xochitl ends in an OOM kill rather than a slow
	// download (docs/DEVICE-NOTES.md §10.4).
	memLimit := download.SetMemoryLimit()

	log.Info("starting", "socket", socket, "go", runtime.Version(), "arch", runtime.GOARCH,
		"pid", os.Getpid(), "memLimitMiB", memLimit>>20)

	svc, session, err := newService(log)
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
		// Left open on purpose: an error exit is an abnormal end, and the next
		// session should say so.
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
	// A clean exit clears the marker; anything else — a crash, the OOM killer,
	// a power cut — leaves it, and the next launch tells the user quietly.
	if err := session.Close(); err != nil {
		log.Warn("could not clear the session marker", "err", err)
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
		// An optional {"log": true} asks for the tail as well. It rides on
		// Ping rather than taking a message type of its own: the log viewer is
		// a page of the settings screen, and it already pings.
		var req struct {
			Log   bool `json:"log"`
			Lines int  `json:"lines"`
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &req)
		}
		if req.Log {
			return sendStatusWithLog(conn, log, svc, req.Lines)
		}
		return sendStatus(conn, log, svc)

	case appload.MessageSystemNewCoordinator:
		log.Info("frontend attached")
		// Push, do not wait to be asked.
		//
		// AppLoad *drops* messages aimed at a backend whose socket is not up
		// yet — it logs "No active socket for ID:quire" and discards the frame.
		// So a request sent from QML's Component.onCompleted races this very
		// event and is sometimes thrown away, leaving the UI empty forever
		// because nothing asks again. The frontend arriving is the earliest
		// moment a send can possibly succeed, which makes it the right moment
		// to send everything the shell needs to draw itself.
		if err := sendStatus(conn, log, svc); err != nil {
			return err
		}
		if svc == nil {
			return nil
		}
		return svc.FrontendAttached(conn)

	case appload.MessageSystemLostCoordinator:
		log.Info("frontend detached")
		// Downloads only while foregrounded (PLAN §6 M7). Stopping here is
		// safe because cancel keeps the fetched pages, so reopening Quire
		// resumes rather than starting over.
		if svc != nil {
			svc.FrontendDetached(log)
		}
		return nil

	default:
		log.Warn("unimplemented message type", "type", msgType, "name", appload.Name(msgType))
		return sendError(conn, "not_implemented",
			fmt.Sprintf("%s is not implemented yet", appload.Name(msgType)))
	}
}

// sendStatus answers a Ping, and is also what a freshly attached frontend gets
// unprompted. It carries the startup notice, so there is exactly one message
// the shell has to receive before it can draw itself correctly.
func sendStatus(conn *appload.Conn, log *slog.Logger, svc *service.Service) error {
	return send(conn, log, svc, nil)
}

// sendStatusWithLog is sendStatus plus the tail of the log file, for the in-app
// viewer.
func sendStatusWithLog(conn *appload.Conn, log *slog.Logger, svc *service.Service, lines int) error {
	if logDir == "" {
		return send(conn, log, svc, []string{
			"Quire is not writing a log file on this device, so there is nothing to show."})
	}
	tail, err := logging.Tail(logDir, lines)
	if err != nil {
		log.Warn("could not read the log", "err", err)
		return send(conn, log, svc, []string{"Quire could not read its own log file: " + err.Error()})
	}
	if len(tail) == 0 {
		tail = []string{"The log is empty."}
	}
	return send(conn, log, svc, tail)
}

func send(conn *appload.Conn, log *slog.Logger, svc *service.Service, logTail []string) error {
	st := newStatus()
	st.LogTail = logTail
	if svc != nil {
		st.Notice = svc.StartupNotice()
	}
	body, err := json.Marshal(st)
	if err != nil {
		// Cannot happen with a fixed struct, but never send a half frame.
		log.Error("marshal status", "err", err)
		return sendError(conn, "internal", "could not build status")
	}
	return conn.Send(appload.MessagePong, body)
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

	// Notice is a one-off sentence for the user, in plain language, or empty.
	// It rides here rather than in a message type of its own: PLAN §7.1's
	// table is not worth growing for a line of text that is only ever sent
	// alongside the status.
	Notice string `json:"notice,omitempty"`

	// LogTail is the most recent log lines, sent only when asked for. Bounded
	// by backend/logging: the socket is SOCK_SEQPACKET and a whole message has
	// to fit one datagram (§3.1).
	LogTail []string `json:"logTail,omitempty"`
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

// deviceDataDir is where Quire keeps state on the tablet.
//
// It is deliberately NOT inside the app directory. It used to be
// (<app root>/data), on the reasoning that an uninstall should take the state
// with it — but build/install-device.sh does `rm -rf "$APP_DIR"` before
// unpacking, so every *reinstall* silently destroyed the user's configured
// sources, their library records, the cover cache and any part-finished
// download. That is exactly what happened on 2026-09-15: a redeploy wiped a
// source the user had just added, and it looked like the app had forgotten.
//
// User data does not live where the installer writes. An uninstall leaving
// configuration behind is normal and recoverable; an upgrade eating it is not.
//
// Hardcoded rather than derived from $HOME or the XDG variables because
// **xochitl's environment sets none of them** — a backend launched by AppLoad
// inherits only USER. Deriving a path from an unset variable would have put the
// state somewhere surprising, or in "/.local/share/quire".
const deviceDataDir = "/home/root/.local/share/quire"

// dataDir resolves where state lives: the override first (the emulator and the
// tests use it), then the device path, then a host fallback for development.
func dataDir() (string, error) {
	if dir := os.Getenv(dataDirEnv); dir != "" {
		return dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the backend binary: %w", err)
	}
	// On the device the binary is unpacked under /home/root/xovi/exthome/appload.
	if strings.HasPrefix(exe, "/home/root/") {
		return deviceDataDir, nil
	}
	// Host: keep it beside the bundle so a developer's tree stays self-contained
	// and several checkouts do not share one state file.
	return filepath.Join(filepath.Dir(filepath.Dir(exe)), "data"), nil
}

// newService wires the backend together: one guarded HTTP client, every theme,
// the source store and the cover cache.
//
// The single fetch.Client is the point. PLAN §7.4's invariants — the rate
// limits, robots, the SSRF guard, the byte budget — live in it, so every
// request Quire makes has to go through here. A second client anywhere would be
// a second network path with none of that.
func newService(log *slog.Logger) (*service.Service, *state.Session, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, nil, err
	}
	stateDir := filepath.Join(dir, "state")
	session, previousCrashed, err := state.OpenSession(stateDir)
	if err != nil {
		// Not fatal: losing the ability to notice a crash next time is not a
		// reason to refuse to start.
		log.Warn("could not mark the session as open", "err", err)
	}
	if previousCrashed {
		log.Warn("the previous session did not end cleanly", "marker", session.Path())
	}

	// ConsultRobots is left at its default (off, PLAN §7.4) until the store is
	// open and can say what the user set; the client is needed first because
	// the themes are built over it.
	client := fetch.NewClient(fetch.Options{Version: version, Logger: log})

	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(client))
	reg.MustRegister(mangathemesia.New(client))
	reg.MustRegister(mangakakalot.New(client))
	reg.MustRegister(mangadex.New(client))
	reg.MustRegister(generic.New(client))

	store, err := state.Open(stateDir, reg)
	if err != nil {
		return nil, nil, err
	}
	log.Info("state opened", "path", store.Path(), "sources", len(store.List()))

	// The one global robots.txt switch (PLAN §7.4). Logged at startup as well
	// as per suppressed check, so a log that begins mid-session still says
	// which way it was set.
	client.SetConsultRobots(store.Settings().RobotsConsulted())
	log.Info("robots.txt setting", "consulted", client.ConsultRobots())

	// The library store holds the document UUIDs, and losing it means losing
	// the "Read" button for everything already downloaded, so a broken file is
	// a startup failure rather than something to shrug at.
	libStore, err := library.OpenStore(stateDir)
	if err != nil {
		return nil, nil, err
	}
	log.Info("library store opened", "path", libStore.Path(), "volumes", len(libStore.List()))

	lib := library.New(library.Options{Log: log})

	// The loopback alias is added here as well as before every upload. Doing
	// it at startup means the endpoint is reachable untethered from the first
	// moment, and doing it again later covers the fact that it does not
	// survive a reboot (docs/DEVICE-NOTES.md §5).
	if err := lib.EnsureReachable(context.Background()); err != nil {
		// Not fatal: browsing, searching and covers all work without the
		// library, and the download path repeats this check and reports the
		// same sentence to the user when they ask for something.
		log.Warn("the reMarkable library is not reachable yet", "err", err)
	}

	return service.New(service.Options{
		Store:        store,
		Registry:     reg,
		Fetcher:      client,
		Covers:       covers.New(filepath.Join(dir, "covers"), client),
		Log:          log,
		Library:      lib,
		LibraryStore: libStore,
		DownloadDir:  filepath.Join(dir, "downloads"),

		PreviousSessionCrashed: previousCrashed,
	}), session, nil
}
