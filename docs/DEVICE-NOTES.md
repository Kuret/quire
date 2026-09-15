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

> ⚠️ **Q1b OPEN — does the `10.11.99.1` bind survive USB unplug?** Goal 1 is
> "no computer involved", so this matters. A sampler is recording
> `carrier` / `addr` / bind presence to `/tmp/usbwatch.log` on the device.
> Until answered, treat the upload path as *proven only while tethered*, and
> keep the direct-write fallback (§6 M5) a first-class path, not a contingency.

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
- `parent`: `""` = root. Otherwise the **UUID of a `CollectionType`**.
- `type`: `DocumentType` | `CollectionType`.

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

### Wire protocol — PLAN §7.1 is wrong about the header type

```c
struct PacketHeader { int type; int messageLength; };
#define MAX_MESSAGE_LENGTH 10485760   /* 10 MiB */
```

**Two native-endian *signed* int32s**, not the u32s PLAN §6 M1 / §7.1 describe.
Negative `type` values are reserved for system messages, so reading the type as
unsigned misreads every one of them:

| type | meaning |
|---|---|
| -1 | `MESSAGE_SYSTEM_TERMINATE` — shut the backend down |
| -2 | `MESSAGE_SYSTEM_NEW_COORDINATOR` — a frontend attached |
| -3 | `MESSAGE_SYSTEM_LOST_COORDINATOR` — the frontend detached |

`messageLength` is signed too: a negative length is a protocol error, not a
huge unsigned value. Validate the length against the 10 MiB cap **before**
allocating — a garbled length otherwise OOMs the device. See
`backend/appload/conn.go`.

### QML side

The type is `net.asivery.AppLoad 1.0` / `AppLoad`. Names verified against the
installed `appload.so`: `applicationID`, `messageReceived`, **`sendMessage`**
(one `s` in the middle — the upstream README's `sendMesssage` is a typo),
`terminate`, `unloading`. The root element should declare `signal close` and a
`function unloading()` that calls `appload.terminate()`, or the backend
outlives the frontend.

### Verified end to end on device

`quired` cross-compiled for `linux/arm64` (CGO off, static), started with a
unix socket path as argv[1], answered `Ping`(1) with `Pong`(2):

```
{"ok":true,"version":"6b3aa69","goVersion":"go1.25.14","arch":"arm64",
 "os":"linux","pid":9998,"uptimeSeconds":1703.6,"uptime":"28m 24s", ...}
```

matching `/proc/uptime` (`1703.61 6468.05`) at the same instant, and exited
cleanly on `MESSAGE_SYSTEM_TERMINATE`.

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
