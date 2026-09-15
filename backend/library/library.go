// Package library puts a finished volume into the reMarkable's own library,
// through xochitl's USB web interface.
//
// # Why the web interface and not the filesystem
//
// The document directory is right there and writing to it looks easy. It is
// also useless while xochitl is running: xochitl does not watch that directory
// (its inotify watches are on other inodes entirely — docs/DEVICE-NOTES.md §5),
// so a file written behind its back is simply not noticed. The only way to make
// it notice is `systemctl restart xochitl`, and a restart throws away the page
// the user was on. PLAN Goal 5 exists to protect exactly that. So Quire uploads
// instead, and xochitl does all the bookkeeping — .content, .metadata, page
// UUIDs, the rendered thumbnail — correctly, by itself, with no restart.
//
// # The two device facts this package is built around
//
//  1. **xochitl listens on 10.11.99.1:80 and nowhere else.** Not on loopback.
//     Unplugging USB strips that address from `usb1`, at which point the
//     request is no longer local, gets routed to the default gateway and leaks
//     onto the LAN. Quire fixes that itself by adding the address to `lo`; see
//     alias.go. It is not reboot-persistent, so it is re-checked before every
//     upload batch rather than assumed after startup.
//
//  2. **The upload target folder is global mutable server state.** There is no
//     folder parameter on POST /upload. The document lands in whichever folder
//     was last fetched with GET /documents/<guid>, and that state persists
//     across TCP connections and across client processes — measured, see
//     docs/DEVICE-NOTES.md §5 (Q1c). Anyone else talking to the interface,
//     including the user's own browser over USB, can move it under us. So every
//     upload is GET-then-POST under one mutex, and the result is verified by
//     reading back where the document actually landed.
//
// # What this package cannot do
//
// There is no folder-create route. The whole HTTP surface is /documents/,
// /download/ and /upload — read out of the xochitl binary and confirmed by
// probing POST, PUT, PATCH and MKCOL on /documents/, all of which just return
// the listing. Folders can therefore only be created by writing CollectionType
// records to disk, which brings the restart back. Resolve reports which folders
// are missing; creating them is the user's to do, once, on the tablet.
package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// MaxUploadBytes is the hard limit xochitl's /upload enforces on the **whole
// multipart body** — not on the PDF inside it.
//
// Measured by binary search on the device, 2026-09-15:
//
//	body =  99,999,242 bytes -> 201 Upload successful
//	body = 100,000,242 bytes -> connection reset by peer
//	body = 100,663,538 bytes -> connection reset (413 on an earlier run)
//
// So it is exactly a decimal 100 MB. The failure mode is worse than a clean
// 413: past the limit xochitl usually resets the connection *mid-upload*, so
// the whole transfer is wasted and what surfaces is a confusing transport error
// rather than a refusal. That is why Upload measures the body and refuses
// before sending a byte.
const MaxUploadBytes = 100_000_000

// UploadBudgetBytes is what Quire aims a volume at, leaving margin below
// MaxUploadBytes for multipart framing and for the fact that a volume's size is
// only known exactly once it is assembled. PLAN §6 M4 splits a volume that
// would exceed it.
const UploadBudgetBytes = 90_000_000

// ErrTooLarge means the body is over MaxUploadBytes and was not sent.
var ErrTooLarge = errors.New("library: the volume is too large for the reMarkable to accept")

// DefaultTimeout bounds a single request. An upload of a 100 MB volume over
// loopback is fast, but xochitl indexes and renders a thumbnail before it
// answers, so this is generous rather than tight.
const DefaultTimeout = 2 * time.Minute

// Entry is one row of GET /documents/.
//
// Note VisibleName *and* VissibleName: the web API really does return both
// spellings, and the on-disk .metadata uses a third form, lowercase
// visibleName. They are not unified here — see docs/DEVICE-NOTES.md §5. Read
// VisibleName; VissibleName is kept only so the shape is documented and a
// future firmware that drops one of them is a visible change rather than a
// silent empty string.
type Entry struct {
	ID           string `json:"ID"`
	Parent       string `json:"Parent"`
	Type         string `json:"Type"`
	VisibleName  string `json:"VisibleName"`
	VissibleName string `json:"VissibleName,omitempty"`

	FileType       string `json:"fileType,omitempty"`
	Bookmarked     bool   `json:"Bookmarked,omitempty"`
	CurrentPage    int    `json:"CurrentPage,omitempty"`
	ModifiedClient string `json:"ModifiedClient,omitempty"`
}

// Document and Collection are the two values Entry.Type takes. A folder is
// just a document with no content.
const (
	Collection = "CollectionType"
	Document   = "DocumentType"
)

// RootID is the parent of a top-level entry, and the folder GET /documents/
// selects.
const RootID = ""

// Options configures a Library.
type Options struct {
	// BaseURL overrides the endpoint. Tests point this at an httptest server.
	BaseURL string

	// ConfPath overrides where xochitl.conf is read from.
	ConfPath string

	// HTTPClient overrides the transport.
	HTTPClient *http.Client

	Log *slog.Logger

	// AddAlias overrides the loopback-alias step. Tests supply a no-op; the
	// zero value runs `ip addr add`.
	AddAlias func() error
}

// Library talks to xochitl's web interface.
//
// It is safe for concurrent use, and deliberately not concurrent internally:
// see the package comment on why every upload is serialised.
type Library struct {
	base     string
	confPath string
	http     *http.Client
	log      *slog.Logger
	addAlias func() error

	// mu serialises everything that touches the interface's selected-folder
	// state, which is to say everything.
	mu sync.Mutex
}

// New builds a Library. The zero Options is the device configuration.
func New(opts Options) *Library {
	l := &Library{
		base:     strings.TrimSuffix(opts.BaseURL, "/"),
		confPath: opts.ConfPath,
		http:     opts.HTTPClient,
		log:      opts.Log,
		addAlias: opts.AddAlias,
	}
	if l.base == "" {
		l.base = BaseURL
	}
	if l.confPath == "" {
		l.confPath = ConfPath
	}
	if l.log == nil {
		l.log = slog.Default()
	}
	if l.addAlias == nil {
		l.addAlias = addAlias
	}
	if l.http == nil {
		l.http = &http.Client{
			Timeout: DefaultTimeout,
			Transport: &http.Transport{
				// One request per connection. The selected-folder state lives
				// in the server, not in the connection, so pooling buys
				// nothing and a stale pooled connection after a USB unplug
				// costs a retry.
				DisableKeepAlives: true,
				DialContext:       (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			},
		}
	}
	return l
}

// EnsureReachable makes the endpoint reachable and says plainly why it is not
// when it cannot.
//
// It is cheap and idempotent, and it is called before every upload batch rather
// than once at startup: the loopback alias does not survive a reboot, and the
// user can switch the web interface off at any time.
func (l *Library) EnsureReachable(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ensureReachableLocked(ctx)
}

func (l *Library) ensureReachableLocked(ctx context.Context) error {
	switch err := l.addAlias(); {
	case err == nil:
		l.log.Debug("loopback alias present", "addr", AliasAddr, "dev", AliasDevice)
	case errors.Is(err, ErrAliasUnsupported):
		// Not a reMarkable. Fall through: if something answers on l.base the
		// caller is entitled to use it, and if nothing does they get the
		// connection error below.
		l.log.Debug("skipping the loopback alias", "reason", err)
	default:
		return err
	}

	enabled, err := WebInterfaceEnabled(l.confPath)
	switch {
	case errors.Is(err, ErrConfMissing):
		// No xochitl here. Same reasoning as the alias.
		l.log.Debug("no xochitl.conf", "path", l.confPath)
	case err != nil:
		return err
	case !enabled:
		return ErrWebInterfaceDisabled
	}

	if _, err := l.listLocked(ctx, RootID); err != nil {
		return err
	}
	return nil
}

// List returns the contents of a folder. RootID lists the top level.
//
// Calling this has the side effect of selecting that folder as the upload
// target, for every client, until someone selects another one. That is why it
// takes the mutex, and why Upload does its own listing rather than trusting a
// listing a caller did earlier.
func (l *Library) List(ctx context.Context, folderID string) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.listLocked(ctx, folderID)
}

func (l *Library) listLocked(ctx context.Context, folderID string) ([]Entry, error) {
	u := l.base + "/documents/"
	if folderID != RootID {
		u = l.base + "/documents/" + url.PathEscape(folderID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	l.decorate(req)

	resp, err := l.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("library: the reMarkable's web interface did not answer: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListBytes))
	if err != nil {
		return nil, fmt.Errorf("library: reading the document list: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("library: listing %q answered HTTP %d", folderID, resp.StatusCode)
	}
	var entries []Entry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("library: the document list was not the JSON we expect: %w", err)
	}
	return entries, nil
}

// maxListBytes caps a listing. A library of a few thousand documents is a few
// hundred KB; this is a sanity bound, not a limit anyone should meet.
const maxListBytes = 32 << 20

// Result is what an upload produced.
type Result struct {
	// DocumentUUID is xochitl's handle for the document. It is M6's input —
	// "open this by UUID" — so it is the reason this whole package exists.
	DocumentUUID string

	// FolderUUID is where the document actually landed, read back rather than
	// assumed. RootID means the top level.
	FolderUUID string

	// VisibleName is the name xochitl gave it. Note that xochitl appends
	// ".pdf" if the uploaded filename does not already end in it, so this is
	// not necessarily the name that was asked for.
	VisibleName string
}

// Upload sends a PDF and reports where it landed.
//
// name is the multipart filename and becomes the document's visible name
// verbatim, except that xochitl appends ".pdf" when it is missing — measured,
// not assumed.
//
// folderID must be a folder that exists; use Resolve to get one. An unknown
// GUID does not fail, it silently leaves the target where it was, which is why
// the result is verified by reading the folder back.
func (l *Library) Upload(ctx context.Context, folderID, name string, pdf io.Reader) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureReachableLocked(ctx); err != nil {
		return Result{}, err
	}

	// Snapshot the target folder before selecting it, so the new document can
	// be identified by difference. A series folder can legitimately already
	// hold a document of the same name (a re-download), and comparing IDs
	// rather than names is the only way to tell them apart.
	before, err := l.listLocked(ctx, folderID)
	if err != nil {
		return Result{}, err
	}
	known := make(map[string]bool, len(before))
	for _, e := range before {
		known[e.ID] = true
	}

	if err := l.post(ctx, name, pdf); err != nil {
		return Result{}, err
	}

	after, err := l.listLocked(ctx, folderID)
	if err != nil {
		return Result{}, err
	}
	for _, e := range after {
		if known[e.ID] || e.Type != Document {
			continue
		}
		l.log.Info("uploaded to the reMarkable library",
			"document", e.ID, "folder", folderID, "name", e.VisibleName)
		return Result{DocumentUUID: e.ID, FolderUUID: e.Parent, VisibleName: e.VisibleName}, nil
	}
	return Result{}, fmt.Errorf("library: %s uploaded, but it is not in the folder we selected; "+
		"something else may have changed the reMarkable's upload target", name)
}

func (l *Library) post(ctx context.Context, name string, pdf io.Reader) error {
	// The body is buffered rather than streamed through an io.Pipe. A volume
	// is tens of megabytes and the device has ~2 GB shared with xochitl, so
	// this is not free — but a streamed body cannot be retried, and the
	// alternative to buffering here is buffering in net/http anyway once a
	// Content-Length is needed.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if _, err := io.Copy(part, pdf); err != nil {
		return fmt.Errorf("library: reading the volume to upload: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("library: %w", err)
	}

	// Measured here, on the framed body, because that is what the device
	// counts — and refused here, before a byte goes out, because the
	// alternative is a socket reset most of the way through a 90 MB transfer.
	if int64(buf.Len()) >= MaxUploadBytes {
		return fmt.Errorf("%w: %s is %d bytes to send and the reMarkable refuses anything "+
			"from %d bytes upward", ErrTooLarge, name, buf.Len(), MaxUploadBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+"/upload", &buf)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	l.decorate(req)

	resp, err := l.http.Do(req)
	if err != nil {
		return fmt.Errorf("library: the reMarkable's web interface did not accept the upload: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("library: upload answered HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// decorate sets the headers the interface's own client sends. Origin and
// Referer are not optional: the interface is a browser app and checks them.
func (l *Library) decorate(req *http.Request) {
	req.Header.Set("Origin", l.base)
	req.Header.Set("Referer", l.base+"/")
}
