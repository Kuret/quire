// Command quired is the Quire backend daemon.
//
// Under Annex, systemd starts it as annex-app@quire. It binds a loopback port,
// publishes the port and a token to /home/root/annex/run/quire.json, and
// serves the PLAN §7.1 message loop until it is stopped.
//
// Under AppLoad it was started with argv[1] set to the path of a unix socket
// the host had already created. That still works: pass a socket path and it
// dials, exactly as before. The message protocol is identical either way — see
// backend/annex for why the transport moved and what was preserved.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rickl/quire/backend/annex"
	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/bookreader"
	"github.com/rickl/quire/backend/bookrender"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/logging"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/seriescache"
	"github.com/rickl/quire/backend/shelf"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/asurascans"
	"github.com/rickl/quire/backend/theme/comick"
	"github.com/rickl/quire/backend/theme/doujinreader"
	"github.com/rickl/quire/backend/theme/fanfox"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/globalcomix"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/mangakakalot"
	"github.com/rickl/quire/backend/theme/mangathemesia"
	"github.com/rickl/quire/backend/theme/shelfmark"
	"github.com/rickl/quire/backend/theme/webtoons"
	"github.com/rickl/quire/backend/theme/weebcentral"
	"github.com/rickl/quire/backend/tryreader"
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

	// Page resizing allocates in ~170 MB steps, and unbounded Go GC pacing on
	// a 2 GB device shared with xochitl ends in an OOM kill rather than a slow
	// download (docs/DEVICE-NOTES.md §10.4).
	memLimit := download.SetMemoryLimit()

	log.Info("starting", "go", runtime.Version(), "arch", runtime.GOARCH,
		"pid", os.Getpid(), "memLimitMiB", memLimit>>20)

	svc, session, err := newService(log)
	if err != nil {
		// Without a store there is nowhere to keep the user's sources, and
		// pretending otherwise would lose whatever they add.
		log.Error("could not start", "err", err)
		os.Exit(1)
	}

	c, err := connect(log)
	if err != nil {
		log.Error("connect failed", "err", err)
		os.Exit(1)
	}
	defer c.Close()

	// Under systemd, stopping the service is SIGTERM. Closing the transport
	// makes Recv return io.EOF, which is the same clean-shutdown path AppLoad
	// took when the host closed the socket — so the loop below is unchanged
	// and an ordinary `systemctl stop` is not reported as a crash.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig, ok := <-stop
		if !ok {
			return
		}
		log.Info("shutting down", "signal", sig.String())
		c.Close()
	}()
	defer signal.Stop(stop)

	serveErr := serve(c, log, svc)

	// Stop the service's background work and wait for it, whichever way the
	// loop ended. A watched-series check or an automatic re-probe outlives the
	// message that started it by design, so the message loop returning is not
	// evidence that anything has stopped writing to the store; only this is.
	// Exiting under it would leave the state directory mid-write on a device
	// that is about to lose the process (docs/DEVICE-NOTES.md §10.4).
	if svc != nil {
		svc.Close()
		log.Debug("background work stopped")
	}

	if serveErr != nil {
		// Left open on purpose: an error exit is an abnormal end, and the next
		// session should say so.
		log.Error("serve failed", "err", serveErr)
		os.Exit(1)
	}
	// A clean exit clears the marker; anything else — a crash, the OOM killer,
	// a power cut — leaves it, and the next launch tells the user quietly. The
	// marker is cleared only after Close: it claims an orderly end, and that is
	// not true while a re-probe is still running.
	if err := session.Close(); err != nil {
		log.Warn("could not clear the session marker", "err", err)
	}
	log.Info("exiting cleanly")
}

// conn is the transport quired speaks over.
//
// Both backend/annex (loopback HTTP, started by systemd) and backend/appload
// (a unix socket handed over by the AppLoad host) satisfy it, because the
// protocol above the transport is the same one. It is also a superset of
// service.Sender, so the service layer needs no knowledge of which is in use.
type conn interface {
	Send(msgType int32, payload []byte) error
	Recv() (int32, []byte, error)
	Close() error
}

// annexAppID names the endpoint file the frontend reads. It must match the
// manifest id and the systemd instance name.
const annexAppID = "quire"

// connect picks a transport.
//
// A socket path in argv[1] means AppLoad started us and this is the old world;
// anything else is Annex. Keeping both is nearly free — they differ in one
// constructor — and it means the device can be moved back to AppLoad without
// rebuilding, which matters while only one of the two has been proven on
// hardware.
func connect(log *slog.Logger) (conn, error) {
	if len(os.Args) >= 2 && os.Args[1] != "" {
		socket := os.Args[1]
		log.Info("using the AppLoad transport", "socket", socket)
		c, err := appload.Dial(socket)
		if err != nil {
			return nil, err
		}
		log.Info("connected")
		return c, nil
	}

	log.Info("using the Annex transport", "app", annexAppID)
	return annex.Listen(annex.Options{AppID: annexAppID, Log: log})
}

// serve runs the message loop. It returns nil for the two clean shutdown
// paths — host terminate and EOF — and an error for anything else.
func serve(conn conn, log *slog.Logger, svc *service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		msgType, payload, err := conn.Recv()
		switch {
		case errors.Is(err, appload.ErrTerminated):
			log.Info("host requested termination")
			return nil
		case errors.Is(err, io.EOF):
			// Under AppLoad this is the host closing the socket; under Annex
			// it is Close, which is what SIGTERM does. Both are the ordinary
			// way this process ends.
			log.Info("transport closed")
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

func handle(ctx context.Context, conn conn, log *slog.Logger, svc *service.Service, msgType int32, payload []byte) error {
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
func sendStatus(conn conn, log *slog.Logger, svc *service.Service) error {
	return send(conn, log, svc, nil)
}

// sendStatusWithLog is sendStatus plus the tail of the log file, for the in-app
// viewer.
func sendStatusWithLog(conn conn, log *slog.Logger, svc *service.Service, lines int) error {
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

func send(conn conn, log *slog.Logger, svc *service.Service, logTail []string) error {
	st := newStatus()
	st.LogTail = logTail
	if svc != nil {
		st.Notice = svc.StartupNotice()
		st.ConsultRobots = svc.ConsultRobots()
		st.Views = svc.Views()
	}
	body, err := json.Marshal(st)
	if err != nil {
		// Cannot happen with a fixed struct, but never send a half frame.
		log.Error("marshal status", "err", err)
		return sendError(conn, "internal", "could not build status")
	}
	return conn.Send(appload.MessagePong, body)
}

func sendError(conn conn, code, message string) error {
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

	// ConsultRobots is PLAN §7.4's one global switch. It rides here for the
	// same reason Notice does: the settings screen has to draw the toggle in
	// the right position the moment it appears, and a second round trip to
	// find out would show it in the wrong one first.
	ConsultRobots bool `json:"consultRobots"`

	// Views is PLAN §7.1 type 75's per-screen layout, {screen: "grid"|"list"}.
	// It rides here for the same reason ConsultRobots does, and with more at
	// stake: a screen that draws a grid and then rearranges itself into a list
	// once a second round trip lands is worse than one that waits.
	//
	// A map here, where the stored settings are explicit fields: this end is a
	// read-only snapshot the frontend looks a screen up in, not the thing that
	// is written back to disk, so there is no key to leak into the envelope.
	Views map[string]string `json:"views,omitempty"`

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

// themeRegistry is every theme this build ships, over the one guarded client.
//
// It is a function of its own so that "is this theme actually shipped?" is a
// question a test can ask. A theme nobody registers can never be fingerprinted,
// never be chosen by the probe and never drive a source — it is present in the
// repository and absent from the product, which is a difference no amount of
// passing tests inside the theme's own package would reveal.
func themeRegistry(client *fetch.Client) *theme.Registry {
	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(client))
	reg.MustRegister(mangathemesia.New(client))
	reg.MustRegister(mangakakalot.New(client))
	reg.MustRegister(mangadex.New(client))
	reg.MustRegister(weebcentral.New(client))
	reg.MustRegister(webtoons.New(client))
	reg.MustRegister(fanfox.New(client))
	reg.MustRegister(comick.New(client))
	// A doujinshi/gallery-site family: one work is N images with no chapter
	// list, so Chapters() answers with a single synthetic chapter holding
	// every page. See the package comment for the live-site comparison that
	// established this as one real family rather than three sites sharing a
	// framework.
	reg.MustRegister(doujinreader.New(client))
	// The one theme whose chapters are finished files rather than page images
	// (theme.FileTheme, 2026-09-20). It is registered exactly like the rest:
	// what makes it different is downstream, in the download path — see
	// service.runFileDownload.
	reg.MustRegister(shelfmark.New(client))
	// Astro-built site driven by the site's own JSON API rather than its markup;
	// its API and CDN hosts are derived at runtime from the source's baseUrl
	// rather than hardcoded, because this site has already moved domain twice.
	reg.MustRegister(asurascans.New(client))
	// Licensed platform, API-driven, and the first theme to use the
	// theme.SourceHeaders and theme.CookieUser side interfaces: its API requires
	// a public client-identifier header, and its reading grant issues a session
	// cookie that the page-image fetches need.
	reg.MustRegister(globalcomix.New(client))
	reg.MustRegister(generic.New(client))
	return reg
}

// bookMutoolPath resolves the mutool executable Quire's own book reader
// runs: next to the running backend executable, exactly where
// build/build-rmpp.sh's bundle puts it (books-contract.md §A, §B). It
// returns an error — never a guessed fallback path — when the executable
// itself cannot be resolved or mutool is not sitting beside it, since a
// silently wrong path would surface much later as a mystifying "could not
// start mutool" instead of this one clear reason.
func bookMutoolPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving the running executable: %w", err)
	}
	path := filepath.Join(filepath.Dir(exe), "mutool")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("no mutool next to %s: %w", exe, err)
	}
	return path, nil
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

	reg := themeRegistry(client)

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

	// The shelf store holds "Saved in Quire"'s chapters — library.Store's
	// equivalent for Quire's own storage, opened the same way and for the
	// same reason: losing it means losing OpenSaved's ability to find pages
	// that are still sitting right there on disk.
	shelfStore, err := shelf.OpenStore(stateDir)
	if err != nil {
		return nil, nil, err
	}
	log.Info("shelf store opened", "path", shelfStore.Path(), "chapters", len(shelfStore.List()))

	// The series detail cache (PLAN §12.12): opened as part of the data dir
	// rather than the state dir, the same as downloads and covers, because it
	// is a cache and not something an export/import needs to carry (see
	// backend/state/covers.go's own file comment for the same reasoning).
	seriesCache, err := seriescache.OpenStore(filepath.Join(dir, "seriescache"))
	if err != nil {
		return nil, nil, err
	}

	lib := library.New(library.Options{Log: log})

	// Quire's own book reader (books-contract.md §B): a mutool executable
	// bundled next to this one (build/build-rmpp.sh's job — see
	// THIRD_PARTY.md), driving the embedded render.js written out to the
	// data dir once at startup. A missing mutool — an older bundle, or the
	// AppLoad PC emulator, which never ships one — is not fatal: bookCache
	// stays nil, and OpenSaved/TryChapter on a book fall back to a plain
	// sentence exactly the way a nil TryCache/Library already do.
	var bookCache *bookreader.Cache
	if mutoolPath, err := bookMutoolPath(); err != nil {
		log.Warn("no bundled mutool found; Quire's own book reader is disabled", "err", err)
	} else if scriptPath, err := bookrender.WriteScript(filepath.Join(dir, "book")); err != nil {
		log.Warn("could not write render.js; Quire's own book reader is disabled", "err", err)
	} else {
		log.Info("mutool found", "path", mutoolPath)
		bookCache = bookreader.New(filepath.Join(dir, "bookcache"), func() bookreader.Renderer {
			return bookrender.New(bookrender.Options{
				MutoolPath: mutoolPath,
				ScriptPath: scriptPath,
				// ~390 MB: generous enough for a legitimate book (the spike's
				// disciple.epub laid out and rendered comfortably well under
				// this) while still well short of the ~2 GB the device shares
				// with xochitl, so a hostile or corrupt file that tries to
				// exhaust memory fails instead of pressuring the rest of the
				// system (books-contract.md §B, Renderer robustness).
				MemoryCapKB: 400_000,
				Log:         log,
			})
		})
	}

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

	svc := service.New(service.Options{
		Store:    store,
		Registry: reg,
		Fetcher:  client,
		Covers:   covers.New(filepath.Join(dir, "covers"), client),
		// Try's own page cache (milestone 1), swept clean on every start —
		// see tryreader.New: unlike covers, nothing under it is worth keeping
		// warm across a restart.
		TryCache:     tryreader.New(filepath.Join(dir, "try"), client),
		Log:          log,
		Library:      lib,
		LibraryStore: libStore,
		DownloadDir:  filepath.Join(dir, "downloads"),

		// SavedDir is a sibling of DownloadDir and of Try's own cache
		// (tryreader.New above): comics and manga downloads land here by
		// default (see backend/service/download.go's package comment),
		// indexed by ShelfStore the way LibraryStore indexes the library.
		SavedDir:   filepath.Join(dir, "saved"),
		ShelfStore: shelfStore,

		SeriesCache: seriesCache,

		BookCache: bookCache,

		PreviousSessionCrashed: previousCrashed,
	})

	// The allowedHosts editor's record-and-offer sink (PLAN). It is wired
	// here, after the service exists, rather than through fetch.Options at
	// NewClient time above: the client has to exist before the themes that
	// are built over it, and the service — the sink's owner — cannot exist
	// before the store and the themes it holds. No request has been made yet,
	// so there is no race to set this after client is otherwise ready.
	client.SetOffDomainHook(svc.RecordOffDomainHost)

	return svc, session, nil
}
