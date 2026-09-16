# DEVICE-NOTES.md

Facts observed directly on the target device. Everything here was run, not
recalled. Commands are recorded so they can be re-run after any OS change.

Last verified: 2026-09-15.

---

## 1. Target device identity

Recorded verbatim — the installer compares against these (PLAN §6 M0.5).

```
$ cat /etc/version
20260210094933

$ cat /etc/os-release
ID=codex
NAME="Codex Linux"
VERSION="5.5.125 (scarthgap)"
VERSION_ID=5.5.125
VERSION_CODENAME="scarthgap"
PRETTY_NAME="Codex Linux 5.5.125 (scarthgap)"
CPE_NAME="cpe:/o:openembedded:codex:5.5.125"
BUILD_MODE_RM="public"
IMG_VERSION="3.25.1.1"

$ uname -m
aarch64

$ tr -d '\0' < /proc/device-tree/model
reMarkable Ferrari
```

`IMG_VERSION=3.25.1.1` is the plan's pinned target (§7.8). **`/etc/version`
(`20260210094933`) is the build stamp the installer should gate on** — it is
more specific than `IMG_VERSION`, which omits the build.

`strings /usr/bin/xochitl` additionally yields `3.25.1` and `3.25.1.1289`.

### Automatic updates

```
$ systemctl is-enabled update-engine   → disabled
$ systemctl is-active  update-engine   → inactive
```

Confirmed off, as §6 M0.4 requires. `/usr/share/remarkable/update.conf` does
not exist on this build — do not use its absence as a signal either way; check
the `update-engine` unit instead.

---

## 2. M0 hard gate — AppLoad compatibility  ✅ PASSED (with a correction)

**This gate failed on first attempt and the fix is a plan correction.** See
PLAN §3.1.

`remagic` pins **AppLoad v0.5.3**. On this device (3.25.1.1) v0.5.3 does *not*
work. qt-resource-rebuilder loads fine, then AppLoad aborts xochitl:

```
[qmldiff]: Set system version to 3.25.1.1
[qmldiff]: Hashtab loaded! Cached 19825 entries
thread '<unnamed>' panicked at src/util/common_util.rs:121:60:
called `Result::unwrap()` on an `Err` value: Couldn't resolve the hashed
identifier 17477757197668945522 required by AppLoad hooks in main UI
xochitl.service: Main process exited, code=dumped, status=6/ABRT
```

xochitl aborting triggers a **whole-device reboot**, which wipes the volatile
`/etc` drop-in — so the symptom presents as "xovi silently never started",
not as a crash. Read the *previous* boot's journal (`journalctl -b -1`) to see
it at all.

### Which AppLoad version to use

`AFFECT [[<hash>]]` at the top of each release's embedded QMLDiff names the
main-UI QML file it patches. Checked every hashed identifier in each release
against this device's own hashtab (big-endian u64 in
`exthome/qt-resource-rebuilder/hashtab`):

| AppLoad release | hashed ids referenced | unresolvable on 3.25.1.1 |
|---|---|---|
| **v0.4.2** | 32 | **0 — works** ✅ |
| v0.5.0 | 32 | 2 |
| v0.5.1 | 36 | 2 |
| v0.5.2 | 35 | 1 |
| v0.5.3 (remagic default) | 35 | 1 |

**Use AppLoad v0.4.2 on 3.25.1.1.** Installed and confirmed: hooks resolve,
`Loaded external AppLoad hooks in main UI`, xochitl stable, `NRestarts=0`.

Re-run this check after any AppLoad or OS change:

```sh
# on host, against a pulled copy of the device hashtab
python3 - <<'PY'
import re,struct,subprocess
d=open('hashtab','rb').read()
s=subprocess.run(['strings','appload.so'],capture_output=True,text=True).stdout
ids={int(p) for m in re.findall(r'\[\[\"?([0-9.]+)\]\]',s)
             for p in m.split('.') if p.isdigit() and int(p)>100000}
print(sorted(h for h in ids if struct.pack('>Q',h) not in d) or "all resolve")
PY
```

### Installed extension set

`/home/root/xovi/extensions.d/`: `appload.so` (v0.4.2), `qt-resource-rebuilder.so`,
`xovi-message-broker.so`. v0.5.3 kept at `/home/root/appload-v0.5.3-backup/`.

---

## 3. Boot persistence — two traps

### 3.1 `.wants` entries must be symlinks

remagic's `scripts/99-advanced-autostart.sh` *copies* the unit into
`multi-user.target.wants/`. systemd refuses a regular file there:

```
multi-user.target: Wants dependency dropin
/etc/systemd/system/multi-user.target.wants/xovi-boot.service
is not a symlink, ignoring.
```

It must be a **symlink** to `/etc/systemd/system/xovi-boot.service`.

### 3.2 `/etc` is a tmpfs overlay — `systemctl enable` does not persist

```
overlay on /etc type overlay (rw,lowerdir=/etc,upperdir=/var/volatile/etc,...)
tmpfs on /var/volatile type tmpfs
/dev/mmcblk0p3 on / type ext4 (ro,relatime)
```

Anything written to `/etc` at runtime lands in tmpfs and is gone on reboot.
To persist, write *under* the overlay via a bind mount of `/`:

```sh
mount -o remount,rw /
BIND=$(mktemp -d); mount --bind / "$BIND"
ln -s /etc/systemd/system/xovi-boot.service \
      "$BIND/etc/systemd/system/multi-user.target.wants/xovi-boot.service"
sync; umount "$BIND"; rmdir "$BIND"
```

`mount -o remount,ro /` afterwards reports **busy** (the `/etc` overlay holds
`lowerdir=/etc` on it) — this is expected and harmless; a reboot restores `ro`
from fstab. **Never `umount -R /etc`** — it rips out the `/etc/dropbear` bind
mount holding the SSH host key and wedges SSH.

> ⚠️ **Rootfs is nearly full: `/` is 506.4M with ~47.8M free (90%).** Anything
> persisted to the rootfs must be tiny. Quire itself lives on `/home` — keep it
> that way; only the `.qmd` and unit files ever touch `/`.

### 3.3 Disk

```
/dev/mapper/home-encrypted-disk   46.3G  16.4G used  29.4G avail  36%  /home
/dev/root                        506.4M 419.5M used   47.8M avail  90%  /
```

---

## 4. Qt and panel

```
$ ls -l /usr/lib/libQt6Core.so.6
→ libQt6Core.so.6.8.2
```

**Qt 6.8.2.** Resolves PLAN §3.2 "Qt version is 6.x" — it is 6.8.2. Qt Quick,
QuickControls2 (Basic/Fusion), QtQml, Labs modules all present.

### Panel geometry — `/dev/fb0` does NOT exist

The Paper Pro has no legacy framebuffer node; `fbset` fails. Display is DRM:

```
$ ls /sys/class/drm/       → card0  card0-LVDS-1  version
$ cat /sys/class/drm/card0-LVDS-1/modes   → 405x1084
```

`405x1084` is the LVDS timing mode, **not** the logical panel size — the panel
packs 4 greyscale pixels per LVDS pixel (405 × 4 = 1620). Consistent with the
expected 1620×2160, but **not yet proof**.

### ✅ Q3 ANSWERED — use `MediaBox [0 0 514 685]`

Settled the way the plan prescribes: had xochitl render one of its own native
notebooks to PDF and read the MediaBox out of the result.

```sh
# with WebInterfaceEnabled=true, from the host over USB
curl -o out.pdf "http://10.11.99.1/download/<notebook-uuid>/placeholder"
# → %PDF-1.7,  /MediaBox [0 0 514 685]
```

| | value |
|---|---|
| **MediaBox xochitl itself emits** | **`[0 0 514 685]`** (514 × 685 pt) |
| aspect | 0.7504 (3:4) |
| implied pixel size | **1620 × 2160** |
| implied DPI | **≈ 227** (`1620 × 72 / 514 = 226.9`; `2160 × 72 / 685 = 227.0`) |

**M4 should emit `MediaBox [0 0 514 685]` and resize pages to 1620 × 2160.**
That is not a guess about what the reader "probably" wants — it is the exact
box xochitl's own renderer produces for a full-bleed native page, so it cannot
letterbox.

> PLAN §3.2 assumed "~229 DPI". The measured figure is **227**. The difference
> is immaterial for image sizing but matters if anything ever computes a
> physical size — use 227.

A second `MediaBox [0 0 100 100]` also appears in the same file; that is a
placeholder/template page, not the content geometry. Take the 514 × 685 one.

> The `.content` default below advertises 1404×1872 on **every** document on
> this device (all 44 of them, including PDFs and epubs). That is a fixed rM2-era
> constant xochitl writes regardless of the actual panel — **it is not the panel
> size and not a per-document value.** Do not read anything into it.

---

## 5. M0.5 Q1 — library writes while xochitl runs  ✅ ANSWERED: YES

The USB web interface is **off by default**:
`/home/root/.config/remarkable/xochitl.conf` → `WebInterfaceEnabled=false`.
Quire must require the user to enable it (Settings → Storage), or set the key
and restart xochitl. Backup of the original conf:
`/home/root/xochitl.conf.quire-backup`.

### Which binds answer (tested from an on-device Go binary)

Once enabled, xochitl listens on **`10.11.99.1:80` only** — the usb-gadget
interface address (note: the interface is **`usb1`**, not `usb0`):

| host tried from on-device | result |
|---|---|
| `127.0.0.1` | connection refused |
| `localhost` / `[::1]` | connection refused |
| `192.168.68.70` (wlan0) | connection refused |
| **`10.11.99.1`** | **HTTP 200** ✅ |

So there is no loopback bind, but the usb-gadget address *is* a local address
and an on-device process reaches it fine.

### Upload works with xochitl running

```
POST http://10.11.99.1/upload
  Content-Type: multipart/form-data; field name "file"
  Origin: http://10.11.99.1
  Referer: http://10.11.99.1/
→ HTTP 201  {"status":"Upload successful"}
```

**xochitl indexed it immediately — no restart.** Within the same second it
created `.content`, `.metadata`, `.pagedata`, `.local`, the `<uuid>/` dir and
a rendered `<uuid>.thumbnails/<pageUuid>.png`. This resolves the §3.2
assumption "xochitl indexes an uploaded document without a restart" — it does.

Probe source: `backend/library/internal/spike/` (see git history).

### ✅ Q1b ANSWERED — unplugging breaks it, and a loopback alias fixes it

**The problem.** Sampled across two real unplug cycles:

| USB state | `usb1` address | socket bound | on-device HTTP |
|---|---|---|---|
| plugged | `10.11.99.1/27` | yes | **OK** |
| **unplugged** | **`NONE`** | **yes** | **FAIL** |

The listening socket *survives* — only the address is stripped. And with the
address gone the destination stops being local, so the request falls back to
the default route and **leaks onto the LAN**:

```
unplugged: route=[10.11.99.1 via 192.168.68.1 dev wlan0  src 192.168.68.70]
plugged:   route=[local 10.11.99.1 dev lo  src 10.11.99.1]
```

That second line is the whole mechanism: on-device access works because the
address is *local*, so traffic is routed via **`lo`**, not over USB. USB
carrier is irrelevant except that it is what causes the address to be assigned.

**The fix — give the kernel the local route ourselves:**

```sh
ip addr add 10.11.99.1/32 dev lo      # idempotent; needs CAP_NET_ADMIN (we are root)
```

Tested directly, by removing the address from `usb1` over wifi to simulate an
unplug:

```
usb1 address removed      → socket bound: 1, HTTP_FAIL
ip addr add .../32 dev lo → route: local 10.11.99.1 dev lo
                          → HTTP_OK   ← endpoint reachable with no USB at all
```

**It is also safe to leave in place while USB is connected** — verified with
both addresses present simultaneously (`usb1 10.11.99.1/27` + `lo
10.11.99.1/32`): on-device HTTP `OK`, **USB SSH from the host still works, and
the USB web interface still answers `HTTP 200`**. Nothing about normal USB
behaviour changed.

**Consequences for M5 — this rescues the preferred design.**

- The upload endpoint works **untethered**, so Goal 1 ("no computer involved")
  holds and the direct-write fallback stays a fallback.
- xochitl keeps doing all the bookkeeping — `.content`, `.metadata`, page
  UUIDs, thumbnail rendering. We never hand-roll that.
- **No `systemctl restart xochitl`.** Which matters, because xochitl does *not*
  watch its own document directory: its inotify watches are on inodes 728 / 120
  / 41, and `/home/root/.local/share/remarkable/xochitl` is inode 146. Files
  written directly behind its back are simply not noticed — a restart would be
  the only way, and a restart costs the user their place.
- The alias is **not persistent across reboot**. Quire's backend must add it at
  startup; make it idempotent and tolerate `EEXIST`.
- Still gated on `WebInterfaceEnabled=true`, which is off by default. Flipping
  it requires an xochitl restart to take effect, so it is a **first-run setup
  step**, not something to toggle per download.

### ✅ Q1c ANSWERED — the upload folder is sticky global server state

Measured 2026-09-15 with `POST /upload` from an on-device Go binary, each
request on a **brand new TCP connection** (`DisableKeepAlives`), and in one case
from a **different process** than the `GET`:

| what was done | where the document landed |
|---|---|
| `GET /documents/<Comics>` on conn A, `POST /upload` on conn B | **Comics** |
| `POST /upload` alone, no `GET` at all, new process | **Comics** — still |
| `GET /documents/` (root), then `POST /upload` | **root** |

So the answer to "does the last-listed folder persist across connections" is
**yes, and then some**. It is not per-connection and not per-client: it is one
mutable variable inside xochitl that any client can move, and it survives until
somebody moves it again. `GET /documents/` resets it to the root.

Consequences, both implemented in `backend/library`:

- **`GET /documents/<guid>` immediately before every `POST /upload`.** Not once
  per batch — the user's own browser on the USB web interface is another client
  and can retarget it between two of our uploads.
- **Serialise.** Every operation that touches the interface goes through one
  mutex, and the upload's result is verified by reading the folder back rather
  than assumed.

#### Does the upload mutex become a bottleneck with one PDF per chapter?

**No — measured, host, 2026-09-16.** PLAN §6 M4 was reversed that day and the
default became one PDF per chapter, which is roughly ten times as many uploads.
The worry was that a many-chapter download would spend most of its time queued
behind the single library mutex.

Two reasons it does not, and the first one is structural:

1. **There is only one download worker** (`service.enqueueDownload`), so there
   is never a second download waiting on the mutex. The lock serialises the
   `GET`/`POST`/`GET` *inside* one upload, and nothing else contends for it.
2. **The lock is held for milliseconds.** Against a local server, with a
   `Comics` folder already holding **2000 documents** so both listings are
   realistically large:

   | document body | time per upload (GET + POST + GET, all under the mutex) |
   |---|---|
   | 0 MB (pure overhead) | **12 ms** |
   | 6 MB — a typical chapter at M4's ~307 KiB/page | **18 ms** |
   | 60 MB — a whole volume | **81 ms** |

   Per chapter costs *more round trips* (three HTTP calls per document instead
   of three per volume) but the same bytes, so a 10-chapter series pays about
   **180 ms** of upload against a download that M4 measured in tens of seconds
   even on the host. These are host numbers over loopback and the device is
   slower, but the ratio is what matters and it is three orders of magnitude.

The second-order effect is the one to keep an eye on: **every upload lists the
target folder twice**, and per chapter puts ten times as many documents in the
flat `Comics` folder, so those listings grow. The 2000-document row above is
what that looks like, and it costs ~2 ms over an empty folder.

### ❌ There is no folder-create route

The whole HTTP surface is three routes. `strings` on `/usr/bin/xochitl` yields
exactly `/documents/`, `/download/` and `/upload`, and the shipped web app
(`/assets/index.js`) references only those. `POST`, `PUT`, `PATCH` and `MKCOL`
on `/documents/` all answer `200` with the listing — the handler ignores the
method.

Confirmed the other way too: a `CollectionType` record written directly to
`/home/root/.local/share/remarkable/xochitl/` with a valid random UUID **never
appears** in `GET /documents/`, with or without a `touch` on the directory.
That is the §5 "xochitl does not watch its document directory" finding again.

So `Comics/<series>` cannot be created while xochitl is running. Quire resolves
as far down the path as it can and uploads into the deepest folder that exists,
reporting the missing one to the user in words.

### The uploaded filename becomes `visibleName`, `.pdf` and all

`POST /upload`'s multipart `filename=` is used verbatim, except that xochitl
**appends `.pdf` when it is missing** — `quire-m5-test-noext` became
`quire-m5-test-noext.pdf`. Spaces, dots and parentheses all survive. There is no
way to get a name without the extension through this endpoint.

### `VissibleName` — both spellings, in different places

The **web API** listing returns *both* `VisibleName` and `VissibleName` (sic).
The **on-disk `.metadata`** uses lowercase `visibleName`. Do not unify them.

---

## 6. Document format, observed (PLAN §7.7)

From the spike upload, `aab1cdbf-5f71-4ab2-8da0-91d6603b721e`.

### `<uuid>.metadata`

```json
{
    "createdTime": "1789497798057",
    "lastModified": "1789497798044",
    "lastOpened": "0",
    "lastOpenedPage": 0,
    "new": false,
    "parent": "",
    "pinned": false,
    "source": "",
    "type": "DocumentType",
    "visibleName": "quire-spike.pdf"
}
```

- `createdTime` / `lastModified` / `lastOpened` are **strings** of epoch millis.
  `lastOpenedPage` is a bare int. Do not "tidy" these types.
- `parent`: `""` = root. Otherwise the **UUID of a `CollectionType`** — *or* one
  of two literal strings. See the correction below.
- `type`: `DocumentType` | `CollectionType`.

#### Correction — `parent` also takes the literal strings `root` and `trash`

Found while listing every folder on the device (2026-09-15). Records that came
down from the cloud are written as **single-line JSON** and use different root
and trash markers than the pretty-printed ones xochitl writes locally:

```json
{"visibleName":"Comics","type":"CollectionType","parent":"root","createdTime":"1774903705599", ... ,"synced":true,"deleted":false}
```

- `"parent": "root"` — top level. Same meaning as `""`.
- `"parent": "trash"` — in the bin.
- Synced records also carry `version`, `synced`, `modified`, `deleted` and
  `metadatamodified`, which locally-created ones do not.

**The web API normalises all of this away**: `GET /documents/` reports that same
folder with `"Parent": ""`, and does not list trashed items at all. So anything
reading the web API can treat `""` as the only root value; anything reading
`.metadata` off disk cannot.

### A real folder (`CollectionType`)

```json
{
    "createdTime": "1774828000585",
    "lastModified": "1774828000584",
    "new": false,
    "parent": "5548bc8d-b693-4204-9915-0fa69090315b",
    "pinned": false,
    "source": "",
    "type": "CollectionType",
    "visibleName": "Scott Pilgrim"
}
```

Folders nest by the same `parent` field, and carry **no** `lastOpened` /
`lastOpenedPage`. This confirms the §3.2 parent-folder assumption: `Comics/` →
`Comics/<series>/` is just two `CollectionType` records.

### `<uuid>.content` (abridged)

```json
{
    "coverPageNumber": 0,
    "customZoomPageHeight": 1872,
    "customZoomPageWidth": 1404,
    "fileType": "pdf",
    "formatVersion": 1,
    "orientation": "portrait",
    "originalPageCount": 1,
    "pageCount": 1,
    "pages": ["ab56ab9f-543a-49bd-b5f0-3bdae8a145fd"],
    "redirectionPageMap": [0],
    "sizeInBytes": "2805",
    "tags": [],
    "zoomMode": "bestFit"
}
```

- `pages` is a list of **per-page UUIDs**; the thumbnail PNG is named after the
  *page* UUID, not the document UUID.
- `sizeInBytes` is a **string**.
- `customZoomPageWidth/Height` = 1404×1872 is an rM2-shaped default written by
  xochitl itself — see §4, do not read it as the panel size.

### `<uuid>.local`

```json
{ "contentFormatVersion": 1 }
```

### Files created per document

```
<uuid>.pdf  <uuid>.metadata  <uuid>.content  <uuid>.local  <uuid>.pagedata
<uuid>/                      (empty for a fresh upload)
<uuid>.thumbnails/<pageUuid>.png
```

Document root: `/home/root/.local/share/remarkable/xochitl/`.

---

## 6.5 AppLoad app layout and wire protocol (M1)

Read off the AppLoad host (`src/protocol.h`, the symbols in the installed
`appload.so`) and confirmed by a working install on this device.

### App directory

`/home/root/xovi/exthome/appload/<id>/`:

```
manifest.json
icon.png
resources.rcc      <- REQUIRED. AppLoad calls QResource::registerResource()
                      on it. There is no loose-file fallback: without it the
                      app does not load at all.
backend/entry      <- executable; started with argv[1] = path to a unix socket
                      AppLoad has already created
```

`manifest.json`: `id` (must equal the QML `AppLoad.applicationID`), `name`,
`loadsBackend`, `entry` (path of the root QML *inside* the rcc, e.g.
`/ui/Main.qml`), `supportsScaling`, `canHaveMultipleFrontends`; optional
`aspectRatio`, `width`.

Build the rcc with **`rcc --binary --format-version 3`**. The device runs Qt
6.8.2 (§4); a host `rcc` from a newer Qt may default to a newer format. Qt
6.11's `rcc` happens to default to 3 as well, but pin it rather than depend on
that.

AppLoad scans app roots at **xochitl start**, not on launcher open. After an
install, restart xochitl and look for:

```
[AppLoad]: Loaded app root /home/root/xovi/services/xochitl.service//exthome/appload//quire
```

That line is emitted when the manifest parses. Confirmed on 2026-09-15.

### The socket is SOCK_SEQPACKET, not SOCK_STREAM

`rm-appload/src/management.cpp:137`:

```c
int sockFD = socket(AF_UNIX, SOCK_SEQPACKET, 0);
```

Go's `"unix"` network is SOCK_STREAM, so connecting to it fails and the backend
dies before doing anything:

```
quired ... level=ERROR msg="connect failed"
  err="dial unix /tmp/quire.sock: connect: protocol wrong type for socket"
[AppLoad]: Process for "quire" finished with exit code 1
```

**Use `net.Dial("unixpacket", path)`** — that is Go's SOCK_SEQPACKET.

**macOS has no AF_UNIX SOCK_SEQPACKET** (`socket: protocol not supported`), so
a test that binds a real one must skip on darwin and be run on the device or in
Linux CI. `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c`, copy the binary
over, run it in `/tmp` — works, and is how the framing below was verified.

### Wire protocol — PLAN §7.1 is wrong in two ways

```c
struct PacketHeader { int type; int messageLength; };
#define MAX_MESSAGE_LENGTH 10485760   /* 10 MiB */
```

**1. The header is two native-endian *signed* int32s**, not the u32s PLAN §6 M1
/ §7.1 describe. Negative `type` values are reserved for system messages, so
reading the type as unsigned misreads every one of them:

| type | meaning |
|---|---|
| -1 | `MESSAGE_SYSTEM_TERMINATE` — shut the backend down |
| -2 | `MESSAGE_SYSTEM_NEW_COORDINATOR` — a frontend attached |
| -3 | `MESSAGE_SYSTEM_LOST_COORDINATOR` — the frontend detached |

`messageLength` is signed too: a negative length is a protocol error, not a
huge unsigned value. The host compares it against the cap *signed* and never
checks for negatives, so do that check yourself. Validate the length **before**
allocating — a garbled length otherwise OOMs the device.

**2. It is not a byte stream, so "length-prefixed framing" is the wrong mental
model.** Each `read()`/`send()` moves exactly one packet, and a read into a
too-small buffer silently discards the rest of that packet. AppLoad's receive
loop (`_listeningThread`, ~line 185) does:

```c
int status = read(clientFD, &header, sizeof(header));   /* packet 1 */
if(status < 1) break;                                   /* 0 bytes kills the connection */
if(header.messageLength > 0)
    status = read(clientFD, inboundBuffer, header.messageLength);  /* packet 2 */
```

So, when sending to the host:

- **Header and payload must be two separate packets.** One combined write is
  one packet, and the host's first `read()` would copy only 8 bytes of it and
  throw the payload away.
- **An empty payload must send the header packet only.** The host skips its
  second `read()` when `messageLength == 0`, so a stray zero-length packet
  would be consumed as the *next* message's header, return 0 bytes, trip
  `status < 1` and tear the connection down.

The host's own send path (`sendMessageTo`, ~line 37) is **asymmetric** to that
— it `send()`s the payload unconditionally, even when empty:

```c
send(sock, &header, sizeof(header), 0);
send(sock, bytes.constData(), bytes.length(), 0);   /* even when length == 0 */
```

So every empty-payload message *from* the host is two packets, and a receiver
must always consume the second. Getting this wrong killed the app on the first
`Ping`: the stray packet was read as the next header, reported as EOF, and the
backend exited "cleanly" while the user watched the app vanish.

### A zero-length datagram vs a closed peer — MSG_EOR does NOT work here

Through Go's `net.Conn` these are indistinguishable: both are a 0-byte read,
which `net` reports as `io.EOF`.

The textbook fix is `recvmsg()` + `MSG_EOR` in `msg_flags`. **It does not work
on this device.** Measured directly (AF_UNIX SOCK_SEQPACKET, OS 3.25.1.1,
aarch64):

```
zero-length datagram   n=0 err=<nil> flags=0x0 EOR=false TRUNC=false
5-byte datagram        n=5 err=<nil> flags=0x0 EOR=false TRUNC=false
after peer close       n=0 err=<nil> flags=0x0 EOR=false TRUNC=false
```

`MSG_EOR` is never set on this socket type at all, not even for a non-empty
datagram, so the flag carries no information. Do not build on it.

**What does work: end of stream is sticky, a datagram is not.** Once the peer
closes, every read returns 0 immediately, forever. A zero-length datagram is
consumed by the read that returns it. So a non-blocking `MSG_PEEK` straight
after a 0-byte read separates them:

| peek result | meaning |
|---|---|
| `EAGAIN` | queue empty, peer open → it was a zero-length record |
| `n > 0` | another record behind it → it was a zero-length record |
| `n == 0` | sticky zero → end of stream |

Two zero-length records back to back still read as end of stream; the host never
emits that, and such a record carries no information anyway. See `packet.go` and
`packet_linux.go`.

Peek *before* consuming the empty payload packet, too — if the host ever stopped
sending one, the next thing queued would be a real header, and swallowing it
would desynchronise the connection permanently.

There is no such thing as a partial packet, so stream-style `io.ReadFull`
reassembly is wrong here: a header packet that is not exactly 8 bytes, or a
payload packet shorter than its header announced, is a framing error, not
something to loop for more data on.

A datagram is additionally bounded by `SO_SNDBUF`, well under the 10 MiB cap: a
4 MiB payload fails with `EMSGSIZE` on this device, 64 KiB round-trips fine.
PLAN §7.1 already forbids bulk transfers over this socket; this is why.

### QML side

The type is `net.asivery.AppLoad 1.0` / `AppLoad`. Names verified against the
installed `appload.so`: `applicationID`, `messageReceived`, **`sendMessage`**
(one `s` in the middle — the upstream README's `sendMesssage` is a typo),
`terminate`, `unloading`. The root element should declare `signal close` and a
`function unloading()` that calls `appload.terminate()`, or the backend
outlives the frontend.

### Verified end to end on device

`quired` cross-compiled for `linux/arm64` (CGO off, static), started with a
SOCK_SEQPACKET socket path as argv[1] by a harness impersonating the host,
answered `Ping`(1) with `Pong`(2):

```
{"ok":true,"version":"31f4282","goVersion":"go1.25.14","arch":"arm64",
 "os":"linux","pid":15748,"uptimeSeconds":2581.24,"uptime":"43m 01s", ...}
```

matching `/proc/uptime` (`2581.25 9818.85`) at the same instant, and exited
cleanly on `MESSAGE_SYSTEM_TERMINATE`.

The frontend half is confirmed by the host's own log when the app is launched:

```
[AppLoad]: Loading "qrc:/ILCPSKLRYV/ui/Main.qml"
```

— so `resources.rcc` at format-version 3 registers and deserialises under Qt
6.8.2, and the manifest's `entry` path resolves. AppLoad mounts the resource
tree under a **random per-launch prefix**, so never hard-code `qrc:/ui/...` in
QML; use relative paths.

---

## 7. Host-side access

```sh
ssh root@10.11.99.1     # USB
ssh root@192.168.68.70  # wlan0 — works, key-based
```

Both key-authenticated. Wifi access matters: it is the only way to observe the
device while USB is unplugged (§5 Q1b).

### Cross-compiling for the device

No Docker and no Codex SDK needed for the Go backend — CGO is off, so the
stock host toolchain suffices (Go 1.25.14 via mise):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o quired ./cmd/quired
```

Verified: produces `ELF 64-bit LSB executable, ARM aarch64, statically linked`,
deploys via `scp`, and runs. **M0 acceptance criterion met.**

The Dockerfile in `build/` is only needed for anything linking Qt/C++.

---

## 8. Recovery

- Triple-press power → `xovi-tripletap` toggles xovi off (installed, v1.1.0).
- Remove a bad patch:
  `ssh root@10.11.99.1 'rm -f /home/root/xovi/exthome/qt-resource-rebuilder/quireOpen.qmd && systemctl restart xochitl'`
- Patch appears to do nothing → clear the stale QML cache first:
  `rm -rf /home/root/.cache/remarkable/xochitl/qmlcache`
- `xovi-boot.service` has a crash-loop safety net: 3 fast boots in a row and it
  skips xovi and boots stock. Kill switch: `touch /home/root/.xovi-boot/disable`.

---

## 9. M1 acceptance — PASSED on hardware 2026-09-15

User launched Quire from the AppLoad menu and tapped "Ping backend": the label
rendered the device's real uptime and **the app stayed open**. That closes the
full round trip end to end:

`tap → QML sendMessage(Ping) → SOCK_SEQPACKET → Go backend → /proc/uptime →
Pong → onMessageReceived → label`

Proven by this, beyond the Go tests: `resources.rcc` at format-version 3
deserialises under Qt 6.8.2; AppLoad's random per-launch qrc prefix is handled;
the manifest `entry` path resolves; and the packet framing survives the real
launch path (which the on-device harness could not fully reproduce — AppLoad
`chdir`s into the app directory and unlinks the socket right after accept).

**It took three attempts, and every failure was invisible on the host.**

| # | Bug | Why the host missed it |
|---|---|---|
| 1 | Dialled `unix` (SOCK_STREAM) against AppLoad's `SOCK_SEQPACKET` | macOS has no `AF_UNIX SOCK_SEQPACKET`; tests used `net.Pipe()` |
| 2 | Header+payload written as one packet; stream-style reassembly | `net.Pipe()` is stream-like, so boundaries never mattered |
| 3 | Zero-length payload packet read as `io.EOF` → backend exits mid-session | Needs a real SEQPACKET socket *and* an empty-payload message |

**Lesson for later milestones:** for the AppLoad transport specifically, the
device is not where you *confirm* the work — it is the only place the code is
real. Cross-compile the test binary and run it on the device
(`GOOS=linux GOARCH=arm64 go test -c`, scp, run) rather than trusting host runs.

---

## 10. M4 on device — resize cost, queue throughput, assembly memory

Everything below was measured **on the tablet**, with xochitl running, over
USB (`ssh root@10.11.99.1`). Host figures for this work are misleading: the
i.MX8MM has four Cortex-A53 cores and the resize is CPU-bound, so a
ten-core development machine understates the cost by roughly 3×.

How to reproduce (the harnesses live in the repo, gated so CI never runs them):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o imageproc.test ./backend/imageproc
scp imageproc.test root@10.11.99.1:/home/root/quire-m4/
ssh root@10.11.99.1 'cd /home/root/quire-m4 && ./imageproc.test \
    -test.bench=BenchmarkNormalise -test.benchtime=5x -test.cpu=1 -test.run=XXX'
```

Write only under `/home` (§3.3: `/` has 47 MB free). Use
`COPYFILE_DISABLE=1 tar …` when copying page fixtures from macOS, or the `._`
AppleDouble files arrive too and the assembler rejects them as
`image: unknown format`.

### 10.1 Resize + encode cost per page — single core

`BenchmarkNormalise`, decode → resize onto the panel grid → JPEG q85:

| source | CatmullRom | BiLinear | ApproxBiLinear |
|---|---|---|---|
| 2480 × 3508 (A4 scan — the common case) | **4.15 s** | 3.17 s | 1.67 s |
| 1620 × 2160 (already panel-native) | 1.72 s | 1.71 s | 0.74 s |
| 5000 × 7000 (rare, huge) | 12.34 s | 9.18 s | 4.09 s |

Splitting the A4 case: **decode 0.52 s, encode 0.39 s**, so ~0.9 s is fixed and
the kernel is the whole of the difference.

**CatmullRom stays the default.** ApproxBiLinear is 2.5× faster but samples
only four source pixels per destination pixel regardless of the ratio, so it
aliases screentone — worse on a 227 DPI panel than the softness it saves.
`imageproc.ScalerBiLinear` is the documented middle option at −24%.

**The box pre-pass trick does not apply here.** A cheap pre-pass must leave 2×
for the quality kernel, so it needs a ≥4× total downscale. Comic sources are
1.5–3× the panel, so it never engaged — measured identical timings with and
without, even at 5000 × 7000 (12.34 s vs 12.39 s). The option was removed
rather than left as a knob that does nothing.

**Allocation matters more than it looks.** `Kernel.Scale` rebuilds its weight
tables *and* its `dstW × srcH × 32` byte intermediate — ~170 MB for an A4 page —
on every call. Caching a `NewScaler` per geometry (pages in a volume share one)
took allocation from **235 MB/op to 63 MB/op** with wall clock unchanged.

### 10.2 Queue throughput vs encode workers

`TestDeviceEncodeWorkers`, 12 pages of 2480 × 3508, fetch fan-out 6, stub
fetcher (so this is pure local cost), xochitl running:

| encode workers | wall clock | per page | speedup |
|---|---|---|---|
| 1 | 63.6 s | 5.30 s | 1.00× |
| **2 (default)** | **34.6 s** | **2.89 s** | **1.84×** |
| 3 | 25.6 s | 2.14 s | 2.48× |
| 4 | 35.5 s | 2.96 s | 1.79× |

Four workers is **slower than three**: with all four cores saturated the run
contends with xochitl and the system. Load average reached 6.05.

`DefaultEncodeWorkers = 2` takes 62% of the best throughput and leaves half the
CPU to the reader. Three is the throughput optimum for anyone who wants it.

**Extrapolated, a 200-page volume costs 7–10 minutes of background CPU** at the
default (2.08–2.89 s/page ÷ nothing, two cores busy), on top of download time.
That is tolerable for a background download but it is not free; it is the
number to revisit if a user complains about heat or battery.

### 10.3 Assembly memory and time

`TestDeviceAssembleFromDir`, real page JPEGs, xochitl running:

| volume | assembled in | PDF | peak Go heap | **VmHWM** |
|---|---|---|---|---|
| 200 pages, 10 chapters | 1.10 s | 60.0 MiB (307 KiB/page) | 141 MiB | **164 MiB** |
| 400 pages, 20 chapters | 2.18 s | 120.0 MiB (307 KiB/page) | 279 MiB | **302 MiB** |

**Assembly is not the problem the host run suggested.** pdfcpu holds the
document in memory, but peak RSS lands at ≈2.5× the PDF size and scales
linearly: 164 MB for a typical volume, 302 MB for a large one, against ~1.6 GB
available with xochitl up. No OOM, no swap (2.5 GB of swap exists and was never
touched). A 1000-page volume would be ~750 MB, which is the point at which
smaller volumes or a streaming writer would need discussing — ordinary volumes
are nowhere near it.

Re-verified with **varied** page geometry after §10.4 (240 pages, 64.0 MiB PDF
→ VmHWM **172 MiB**), so this figure was not a victim of the uniform-fixture
mistake below. It cannot be: assembly embeds the page JPEGs as DCTDecode
streams without decoding them, so peak RSS tracks PDF size and nothing else.

Assembly wall time is negligible next to the resize: **1.1 s for 200 pages**.
Bytes on disk are unchanged by assembly — pdfcpu embeds the page JPEGs as
DCTDecode streams rather than re-encoding them, so PDF size ≈ sum of pages.

---

## 11. No right-to-left reading direction — checked, not assumed

Manga reads right-to-left. **The stock reader has no concept of it**, and there
is nothing we can put in a PDF to ask for it. Three places it could have lived,
all checked on 3.25.1.1:

| Mechanism | Result |
|---|---|
| PDF `/ViewerPreferences << /Direction /R2L >>` — the standard hint | `ViewerPreferences`, `Direction`, `R2L` appear **nowhere** in `/usr/bin/xochitl`. Not parsed |
| A `.content` field | No direction key exists across a 44-document real library. Nearest are `orientation`, `textAlignment`, `verticalScroll`, `zoomMode` — none is page direction |
| xochitl's own QML | All 17 `Qt.RightToLeft` uses are UI layout (toolbar side, tag chips, handedness). None touches document reading |

**Why this is a small loss.** In a single-page reader, reading direction does not
change page *order* — page 1 is page 1. It changes which way you swipe, and how
two-page spreads pair. Portrait reading on the Paper Pro is single-page, so only
the gesture differs. Panel order within a page lives in the artwork.

> ⚠️ **Do not compensate by reversing page order.** It would make the reader's
> "next" walk backwards through the story, and it would desynchronise the
> chapter→page-offset map that M6's Read and xochitl's saved position both
> depend on. A backwards gesture is cosmetic; a reversed book is broken.

**Adjacent and actually fixable:** wide double-page spreads currently fit-and-pad
into a 3:4 page, shrinking to a small centred strip — the mirror of the
webtoon-strip limitation in §6 M4. Splitting a spread into two panel-shaped
pages is within our control and would affect most manga volumes.

### 10.4 The OOM kill — what §10.1–10.3 missed

**The backend was OOM-killed on the user's device during a real download.**

```
Out of memory: Killed process 67015 (entry)
  total-vm:3089332kB  anon-rss:1632924kB
```

AppLoad reported `exit code 9 / QProcess::CrashExit` and tore the app down
mid-session.

**Why the earlier measurements missed it: every synthetic page was the same
size.** Fixtures were generated by one function with one geometry, so the
resampler cache saw one geometry and the numbers looked fine. Real sources —
MangaDex among them — vary page to page.

Reproduced and measured with 60 varied-geometry pages (2480×3508 down to
900×1600, plus a spread and jittered sizes), two encode workers, xochitl
running, `TestDeviceVariedGeometryRun`:

| configuration | peak RSS | wall clock |
|---|---|---|
| no memory limit, cache bounded at 8 entries (the shipped bug) | **OOM-killed ~1.73 GB** | — |
| no memory limit, cache bounded at 256 MiB | 1690 MiB — survived, barely | 111 s |
| **no memory limit, cache disabled entirely** | **OOM-killed ~1.72 GB** | — |
| `SetMemoryLimit(512 MiB)`, cache disabled | 646 MiB | 71 s |
| **`SetMemoryLimit(512 MiB)`, cache 256 MiB — shipped** | **637–651 MiB** | **72 s** |
| `SetMemoryLimit(768 MiB)`, cache 256 MiB | 804 MiB | 70 s |

**Two findings, and the second is the important one.**

1. The cache's bound was in the wrong unit. Eight entries × ~170 MB of
   intermediate ≈ 1.36 GB. It is now bounded by retained bytes, counting the
   worst case (`sync.Pool` keeps a per-P copy, so one entry costs up to
   GOMAXPROCS buffers), with LRU eviction.

2. **The cache was not the root cause.** With it disabled the backend still
   died at 1.72 GB. Resizing allocates a `dstW × srcH × 32` byte intermediate —
   ~170 MB for an A4 page — and Go's default pacing, with no limit and
   GOGC=100, chases those allocations upward until the kernel intervenes. The
   fix is a **soft heap limit**: `download.RecommendedMemoryLimit` = 512 MiB,
   applied by `download.New` (or by `main` via `download.SetMemoryLimit`).

The limit is not a tax — it is **36% faster** (72 s vs 111 s), because the
device stops thrashing.

**Headroom at the shipped configuration:** peak RSS **637 MiB** against
~2.0 GB total and ~1.64 GB available with xochitl up, leaving roughly **1 GB**.

**Lesson, and it is the same one §9 records for the AppLoad transport:** a
fixture you generated yourself only tests the shape you imagined. Vary the
input geometry deliberately, and measure peak RSS on the device — `VmHWM` in
`/proc/<pid>/status` — not on the host.

### 10.5 Bounding the resampler's intermediate

§10.4 capped the heap. It could not cap the *allocation*: the resampler's
intermediate is `destinationWidth × sourceHeight × 32` bytes, which scales with
the **input**, so a soft limit only decides how hard the GC works around it.
`imageproc` now box-downscales by an exact integer factor first, whenever that
product would exceed `DefaultMaxResampleBytes` (**256 MiB**).

**Why 256 MiB.** The worst ordinary page lands at 171 MB (a 2480 × 3508 A4 scan
resamples through a 1527 × 3508 intermediate; a 2550 × 3300 letter scan the
same). 256 MiB sits ~50% above that, so the guard never fires on normal manga —
a guard that quietly engaged on everything would be a silent quality
regression — while bounding two concurrent encodes to 512 MiB, which is exactly
the soft heap limit from §10.4.

Measured on device, 15 pages mixing ordinary scans with a 5000 × 7000, a
6000 × 8000, a 6000 × 4000 spread and two 20000 px strips, two encode workers,
xochitl running:

| configuration | pages guarded | peak RSS | wall clock |
|---|---|---|---|
| **guard on (shipped, 256 MiB)** | 2 of 15 | **603 MiB** | 46.05 s |
| guard off | 0 | **1189 MiB** | 46.19 s |

**Half the peak RSS, at no cost in time** — the box pass reads each source pixel
exactly once and disappears into the noise.

**Quality cost is academic at these ratios.** Box-then-kernel against kernel
alone on the 5000 × 7000 page (guard factor 2): mean absolute difference
**2.07/255**, worst pixel 26/255, and the JPEG came out 1.5% smaller. That is a
rounding difference, not a visible softening — unsurprising, since an exact 2×2
box average is what the kernel would largely have computed anyway.

**Correction to the arithmetic that motivated this.** A long strip does *not*
produce a huge intermediate. Fitting 800 × 20000 into a 3:4 page makes the
destination a narrow sliver (86 × 2160), so the intermediate is
86 × 20000 × 32 = 55 MB, not ~1 GB — that figure assumed the destination kept
the full 1620 px panel width. The real offenders are **large** sources where
the destination stays wide: 5000 × 7000 needs 346 MB, 6000 × 8000 needs 518 MB.

**A limit of the mechanism, stated plainly.** The guard can only shrink a
source that is at least twice the destination, because an integer factor below
that would mean upscaling afterwards. An A4 scan is only ~1.6× the panel, so
its 171 MB intermediate is irreducible this way. That is fine at 256 MiB × 2
workers, but it is the reason the guard is a guard and not a general
optimisation — bringing that case down would need banding the resample, with
the seam handling banding implies.

**The webtoon output is still poor, and this does not fix it.** The guard stops
a long strip from being a memory problem; it does nothing about the fact that
fitting 800 × 20000 into a 3:4 page yields a tiny centred sliver with white
margins either side — a page nobody can read. That is the known webtoon
limitation in PLAN §6 M4. Strip-splitting is the fix if the user asks for it,
and it wants real banding rather than this guard, so the two concerns are kept
separate in the code.

**Superseded 2026-09-16 by strip splitting (PLAN §12.3), and the correction is
worth reading — see §12.** The prediction above was that splitting "wants real
banding". It does not. Cutting the strip *before* the fit-and-pad resize means
each piece resamples as an ordinary ~800 × 1067 page, so the intermediate never
gets large in the first place and this guard never fires on a strip at all
(measured: 0 pages guarded across seven strips up to 800 × 20000). The two
concerns stay separate in the code for the reason given, but the guard is not
what makes splitting affordable.

---

## 12. Strip splitting on device (PLAN §12.3)

Measured on the tablet 2026-09-16, xochitl running, over USB, with
`TestDeviceStripSplitRun` in `backend/download`. Seven strips of deliberately
varied geometry (800 × 20000, 1080 × 15000, 800 × 12000, 720 × 9600,
800 × 8000, 900 × 5400, 800 × 4200), one chapter, two encode workers, the
shipped soft heap limit and resample guard.

```
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o download.test ./backend/download
COPYFILE_DISABLE=1 tar -cf - strips download.test | ssh root@10.11.99.1 'tar -C /home/root/quire-strip -xf -'
ssh root@10.11.99.1 'cd /home/root/quire-strip && QUIRE_DEVICE_MEASURE=1 \
    QUIRE_WORK_DIR=/home/root/quire-strip/work \
    QUIRE_SRC_DIR=/home/root/quire-strip/strips \
    ./download.test -test.run=TestDeviceStripSplitRun -test.v -test.timeout=40m'
```

| configuration | pages out | peak RSS (VmHWM) | wall clock | pages guarded |
|---|---|---|---|---|
| `splitStrips: auto` — shipped | **70** | **519 MiB** | 21.4 s | 0 |
| `splitStrips: never` — the pre-§12.3 behaviour | 7 | **536 MiB** | 11.1 s | 0 |

**Splitting does not cost memory; it saves a little.** Ten times the output
pages at a *lower* peak than leaving the strips whole, against ~1.64 GB
available with xochitl up — roughly 1.1 GB of headroom, in line with §10.4's
shipped figure of 637 MiB for ordinary pages.

**Why, and it is the whole reason PLAN §12.3 insists on the ordering.** The
intermediate x/image allocates is `destinationWidth × sourceHeight × 32`. Fit a
whole 800 × 20000 strip into a 3:4 page and the destination is an 86 px sliver,
so the intermediate is 86 × 20000 × 32 = 55 MB — small, but the *decoded strip*
is held across the whole resize either way. Split first and each piece
resamples as an ordinary ~800 × 1067 page, so nothing large is ever resampled
and §10.5's guard never engages (0 of 7, both runs). The decode of a
20000-row strip is the irreducible cost and it is the same in both columns.

**Wall clock roughly doubles, and that is the honest price:** 70 pages are
encoded instead of 7. Per *output* page it is 0.31 s against 1.58 s, because a
split piece is a small image and a whole strip is not. Extrapolating §10.2's
figures, a webtoon episode of ten strips is about 30 s of background CPU.

**The fixtures are synthetic and that is a real limitation.** PLAN §1.3 forbids
committing source content, so the strips are generated: measured *geometry*
from §12.3's table, panel art with authored gutters, speech bubbles and
lettering, encoded at q88 to land at 0.2–0.8 MiB each — the size range a real
strip occupies. That is fair for a memory and throughput measurement, which
depends on pixel dimensions and compressed size rather than on the art. It is
**not** evidence about cut *quality* on real authored gutters, and §9's
circular-fixture warning applies: the first real webtoon episode a user
downloads is the measurement that has not been taken.

**Scratch was removed after the run** (`/home/root/quire-strip`). Write only
under `/home`: `/` has ~47 MB free (§3.3).
