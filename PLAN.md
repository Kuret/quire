# Quire — PLAN.md

A Komikku-style comic/manga downloader for the reMarkable Paper Pro that files
downloads into the **stock library** and hands off reading to the **stock
xochitl reader**, so reading position, pen annotations and cloud sync stay in
one place.

Name: `quire` (a quire is a gathering of folded sheets in a codex — fits the
Codex / Vellum naming tradition on this platform).

**Revision 2.** Changes from r1: the source engine is now theme-based rather
than per-site recipes (§7.2, §7.3); added the source-probe / theme-detection
pipeline (§7.5); the stock-reader performance question is resolved (§3.1);
the licence position no longer depends on reusing any other project (§1.4);
browser-challenge sites are explicitly out of scope (§7.6).

**Revision 3.** The target device is pinned to **reMarkable OS 3.25.1.1** with
automatic updates disabled. All QML/QMLDiff work targets that version and only
that version (§7.8).

**Revision 4.** Staying on 3.25.1.1 is now an affirmative decision, not just an
inherited constraint (§7.8). Two corrections to r3: `rm-hacks-qmd` is **not**
usable as a reference corpus — its QMD files are hashed and its author does not
document them — and the back-porting concern was overstated, because the hashtab
is locally rebuildable and `qmldiff` supports a full host-side edit/apply loop.
M6 moves from on-device trial-and-error to ordinary host development (§7.8).
Added a hard AppLoad-compatibility gate to M0: **AppLoad's own hooks into
xochitl are the real version dependency**, not our patch (§3.1, §8).

---

## 0. How to use this document

You are a coding agent implementing this project. Read this file fully before
writing code.

**The single most important rule: this platform is undocumented and
version-fragile. Do not trust any path, JSON field name, QML type name, or
endpoint in this document without verifying it on the actual device first.**
Everything here was assembled from community sources and reMarkable's partial
developer portal. §3.1 lists what is confirmed; §3.2 lists what is assumed.
§11 lists open questions that must be answered by experiment before the
dependent milestone starts.

When a stated fact turns out to be wrong, **fix this document in the same
commit as the code change.** This file is the project's memory.

Work milestone by milestone (§6). Each milestone has an explicit acceptance
test. Do not start a milestone before its predecessor's acceptance test passes
on real hardware.

---

## 1. Goals, non-goals, and position

### 1.1 Goals

1. Browse and search comic sources from the tablet, with no computer involved.
2. Sources are **data, not code** — the user adds a site by URL; the app works
   out how to read it.
3. Download a volume and have it appear in the normal reMarkable library, in a
   normal folder, with a normal thumbnail.
4. Tap "Read" in the app and land in the **stock reader** on that document.
5. Reading progress, highlights and pen annotations are owned entirely by
   xochitl. Quire never stores a reading position.
6. Survive a reMarkable OS update with, at worst, a re-run of the installer and
   a regenerated QML patch.

### 1.2 Non-goals

- Writing our own e-ink reader or touching the framebuffer/waveform layer. §3.1
  now confirms the stock reader is fast enough. Settled; do not revisit.
- Supporting rM1 / rM2. Target is aarch64 Paper Pro family only. Keep the
  backend arch-agnostic so this stays possible later, but do not test for it.
- Cloud sync of Quire's own state. Device-local only.
- Any account system, telemetry, or analytics.
- Defeating, solving, or working around browser challenges, bot detection, or
  anti-scraping measures. See §7.6 — sites that gate content this way are
  detected and refused, not circumvented.

### 1.3 Quire ships with no sources

**The repository contains no source URLs, no bundled index, and no default
catalogue.** The shipped configuration has an empty index. The user adds each
site themselves, by pasting a URL.

This is deliberate and not merely cosmetic:

- Much of the material on comic aggregator sites is copyrighted, and whether
  accessing a given site is permissible varies by jurisdiction and by site.
  That determination belongs to the person operating the tool, who knows their
  own situation. Quire does not make it for them, and does not nudge it by
  shipping a list.
- It is also the better architecture. A site list in the repo is a maintenance
  burden that rots; a theme engine that reads any site of a known shape is not.

The README must state plainly that the user is responsible for the legality of
the sources they configure. `schema/example-index/` may contain **only** entries
pointing at public-domain repositories (Standard Ebooks, Project Gutenberg) or
at a server the developer controls. Never a real aggregator.

### 1.4 Licence — decided, and independent of any other project

**Quire is Apache-2.0.** No GPL-licensed code is copied into this repository.

Prior art (§4) is read for *facts*: protocol shapes, file paths, SDK locations,
which QML type does what, how a site family structures its URLs. Facts, APIs
and techniques are not copyrightable; expression is. Implementations are
written from scratch, working from observed HTTP traffic and observed HTML
rather than from someone else's source.

**Provenance discipline**, without exception:

- Never paste code from another project into this one, not even "temporarily".
- When a file is informed by reading prior art, note the *fact* learned in a
  comment — not an attribution, because nothing was copied.
- Scanly is GPL-3.0. Do not vendor its Dockerfile, its scraping layer, or its
  QML keyboard. Write your own; the Dockerfile is ~20 lines, and the fact that
  reMarkable publishes the Codex SDK at a public bucket URL is just a fact.
- If reusing GPL code ever seems genuinely necessary, **stop and ask the
  human.** Relicensing the project is their call, not yours.

Add `LICENSE` (Apache-2.0) in the first commit, before any other file.

---

## 2. Architecture

```
  ┌─────────────────┐     ┌──────────────────┐     ┌────────────────────┐
  │  User adds a    │────▶│  Quire backend   │────▶│   PDF assembler    │
  │  site by URL    │     │  Go, aarch64     │     │  images → 1 file   │
  │  probe → theme  │     │  fetch/cache/    │     │  pdfcpu            │
  └─────────────────┘     │  queue           │     └─────────┬──────────┘
                          └────────┬─────────┘               │
                                   │ AppLoad socket          │ POST /upload
                                   ▼                         ▼
                          ┌──────────────────┐     ┌────────────────────┐
                          │  Quire QML UI    │     │  xochitl library   │
                          │  inside xochitl  │     │  stock, indexed    │
                          └────────┬─────────┘     └─────────┬──────────┘
                                   │ "Read" tap              │
                                   │  (qmd hook)             │
                                   ▼                         ▼
                          ┌─────────────────────────────────────────────┐
                          │  Stock xochitl reader                       │
                          │  owns position, annotations, cloud sync     │
                          └─────────────────────────────────────────────┘
```

| Component | Language | Lives at |
|---|---|---|
| QML frontend | QML / Qt Quick 6 | `/home/root/xovi/exthome/appload/quire/` |
| Backend daemon | Go (static, CGO off) | same directory, launched by AppLoad |
| QML patch | QMLDiff `.qmd` | `/home/root/xovi/exthome/qt-resource-rebuilder/` |
| Theme engine | Go, in backend | — |
| Installer | Go CLI (host side) | developer machine |

The backend is the only thing that touches the network or the filesystem. The
QML frontend is a dumb view that speaks the AppLoad protocol. Keep it that way
— QML is the layer that breaks on OS updates, so it should contain as little
logic as possible.

---

## 3. Platform facts

### 3.1 Confirmed

- OS is **Codex**, a Yocto-based Linux. Main UI app is **xochitl**, proprietary,
  closed source, no compatibility guarantee between versions. Started by
  systemd: `systemctl {start,stop,restart} xochitl`.
- **Target OS version is 3.25.1.1, pinned, with automatic updates disabled.**
  This is a hard target, not a minimum. Every `.qmd` in this repository is
  written against 3.25.1.1 and the installer refuses to run on anything else
  (§7.8). Do not write version-agnostic QML patches "just in case" — they don't
  exist on this platform, and pretending otherwise produces patches that
  silently half-apply.
- **AppLoad's own hooks break on new firmware, independently of anything we
  write.** AppLoad injects into xochitl's main UI, and those injection points
  move between releases. Observed failures: on a 3.26.0.62 pre-release, AppLoad
  aborted with an unresolvable hashed identifier required by its main-UI hooks,
  while qt-resource-rebuilder itself loaded fine (19,913 entries cached); on
  3.28.0.162, qt-resource-rebuilder again loaded fine (20,235 entries) but
  `appload.so` v0.5.3 panicked immediately, with 3.28 support still an open
  upstream pull request. **Consequence: the newest OS is the worst OS to target.
  Our app cannot run at all until upstream ships AppLoad support for a given
  release.** This, not our `.qmd`, is the binding version constraint.
- **CORRECTION (verified on the target device, 2026-09-15).** The rule above is
  right but its shape was wrong. The failure is **not** "newest OS bad, our
  pinned version fine" — it is that *an AppLoad build targets a narrow window of
  xochitl versions in both directions*. On our own 3.25.1.1, `appload.so`
  **v0.5.3 fails exactly as it does on 3.26/3.28**: qt-resource-rebuilder loads
  (19,825 entries cached), then AppLoad panics with `Couldn't resolve the hashed
  identifier 17477757197668945522 required by AppLoad hooks in main UI` and
  xochitl aborts with `SIGABRT`. **`v0.4.2` is the newest release whose hooks all
  resolve on 3.25.1.1** — checked exhaustively, not by trial: every hashed
  identifier in each release was tested against the device's own hashtab
  (v0.4.2: 0 of 32 unresolvable; v0.5.0/v0.5.1: 2; v0.5.2/v0.5.3: 1). The
  procedure is recorded in `docs/DEVICE-NOTES.md` §2 and should be re-run after
  any AppLoad or OS change — it answers the compatibility question in seconds,
  without installing anything.
  **Two consequences for this project.** (a) `remagic` — which §4 recommends as
  the user-facing prerequisite — pins **v0.5.3**, so a stock remagic install is
  *broken* on our target. Our README and installer must pin v0.4.2 explicitly
  and must not assume remagic's default is usable. (b) A failing AppLoad does
  not look like a failure: the xochitl abort triggers a **full device reboot**,
  which wipes the volatile `/etc` drop-in (§3.1, `/etc` is tmpfs-backed), so the
  symptom is "xovi silently never started". Always diagnose from the *previous*
  boot's journal (`journalctl -b -1`), never the current one.
- **The hashtab is generated locally, not downloaded.** `xovi/rebuild_hashtable`
  builds it on-device, and must be re-run after every software update. There is
  no dependency on anyone publishing a hashtab for our version.
- **`qmldiff` is a host-side CLI, not only a runtime library.** It can create a
  hashtab from a QML tree recursively, rewrite diffs into their hashed form in
  place, and apply diffs to a QML tree writing the result to a destination
  directory. This makes host-side patch development possible — see §7.8.
- **Downgrading is effectively one-way.** Moving down a version involves sourcing
  signed firmware and carries document-format risk; the community position is
  that it ranges from difficult to impractical. Treat any upgrade as
  irreversible.
- **xochitl is required at startup on encrypted devices** (which the Paper Pro
  is). Do not disable the service.
- Developer mode is required for SSH on Paper Pro. Enabling it **performs a
  factory reset** and disables most of secure boot (but not disk encryption).
  Path: Settings → General → Paper Tablet → Software → Advanced → Developer Mode.
- Credentials afterwards: Settings → General → Help → About → Copyrights and
  Licenses, under "General Information". User is `root`, password randomly
  generated.
- Leaving developer mode requires recovery mode: hold power 25–30s, release ~2s,
  single press ~2s, release. USB IDs in recovery: `1fc9:0134` (Paper Pro),
  `2edd:0140` (Paper Pro Move).
- Documents live in `/home/root/.local/share/remarkable/xochitl/`. All files for
  one document share a randomly generated UUID prefix, or sit in a subdirectory
  named with that UUID. PDF/EPUB-backed documents additionally store the
  original as `{UUID}.pdf` / `{UUID}.epub`.
- **reMarkable's own docs state xochitl should not be running when accessing or
  changing stored documents.** This is the reason for the upload-endpoint
  approach in M5.
- `/home/root/.config/remarkable/xochitl.conf` contains the root password in
  plaintext (assuming it hasn't been changed on-device).
- **Stock reader performance is adequate.** A 593-page manga PDF has been
  verified fast on a Paper Pro in normal use. The no-custom-reader design is
  validated. *Caveat: the variable is per-page image weight, not page count —
  M4's sizing step is what keeps this true. Do not emit oversized images.*
- **xovi**: `LD_PRELOAD`-based hooking framework. Extensions are `.so` files in
  `/home/root/xovi/extensions.d/`; per-extension state in
  `/home/root/xovi/exthome/<extension-name>/`. Supports aarch64 and arm32.
  Prebuilts for Paper Pro exist in GitHub releases. Doubles as a dynamic
  linker: extensions can import/export symbols from each other, and an import
  from global scope resolves to the *unhooked* version even if another
  extension hooks it.
- xovi strips itself from the inherited `LD_PRELOAD` by default. Set
  `XOVI_INJECT_CHILDREN=1` to let child processes inherit it. Restarting via
  systemd is unaffected (fresh `LD_PRELOAD` per host process).
- **AppLoad**: xovi extension providing windowed and fullscreen apps. Apps are
  directories in `/home/root/xovi/exthome/appload/`. Frontend must be QML.
  Backend optional, any language, started with `argv[1]` set to a temporary
  unix socket path. Close a fullscreen app by dragging from centre-top to
  centre. Long-press an icon in the AppLoad menu to launch as a window.
- **AppLoad wire protocol**: 8-byte header = u32 message type (arbitrary,
  developer-defined) + u32 payload length. Max payload **10485760 bytes
  (10 MiB)**. Bidirectional, same framing both ways. Receiver loops on headers.
  **CORRECTION (verified 2026-09-15 against `rm-appload/src/protocol.h` and the
  installed binary):** both fields are **native-endian *signed* `int32`**, not
  `u32` — `struct PacketHeader { int type; int messageLength; }`. Negative
  types are **reserved by the host**: `-1` terminate, `-2` new coordinator,
  `-3` lost coordinator. Reading them unsigned misparses every system message,
  and turns a negative length — which is a protocol error — into a ~4 GiB
  allocation on a device with 47 MB of free rootfs. §7.1's table inherits this
  correction.
- **CORRECTION — the AppLoad socket is `SOCK_SEQPACKET`, not a byte stream.**
  `management.cpp:137`: `socket(AF_UNIX, SOCK_SEQPACKET, 0)`. Three
  consequences, all of which bit us on real hardware in M1:
  1. **Dial it as `unixpacket`.** Go's `"unix"` network is `SOCK_STREAM` and
     the connect fails outright with `EPROTOTYPE` — *"protocol wrong type for
     socket"*. This is silent from the UI: AppLoad starts the backend, the
     backend exits 1, the frontend loads fine and simply never gets a reply.
  2. **Message boundaries are preserved, so send the header and the payload as
     two separate packets.** AppLoad's read loop does two `read()` calls, and
     each consumes one whole packet, discarding any excess past the buffer. A
     combined write is one packet, so the payload would be silently thrown
     away. Conversely, send **no** payload packet when the payload is empty:
     AppLoad skips its second read at length 0, so a stray empty packet gets
     consumed as the *next* header, reads 0 bytes, and tears down the
     connection. **AppLoad's send path is asymmetric to its own receive path**:
     it emits the payload packet *unconditionally*. So an empty-payload message
     is **two packets inbound but must be one packet outbound**. Implement both
     halves or the stream drifts by one packet and dies on the next message.
  4. **A zero-length datagram and a closed peer are indistinguishable through
     Go's `net.Conn`** — both are a 0-byte read, which `net` converts to
     `io.EOF`. This is not theoretical: it is what made the app vanish the
     instant the user tapped a button whose payload happened to be empty.
     **`MSG_EOR` does not help** — measured on this device, the kernel never
     sets it on `AF_UNIX`/`SOCK_SEQPACKET`, not even for a non-empty datagram,
     so it carries no information. What *does* work: **end of stream is sticky,
     a datagram is not.** After a close every read returns 0 forever; a
     zero-length record is consumed by the read that returns it. So a
     non-blocking `MSG_PEEK` immediately after a 0-byte read separates them —
     `EAGAIN` or `n > 0` means it was a record, `n == 0` means end of stream.
     Peek *before* consuming, so a record carrying real data can never be
     swallowed. The single unresolvable case is two zero-length records
     back-to-back; the host never emits that, and such a record carries no
     information anyway.
  3. Stream-style partial-read reassembly (`io.ReadFull` loops) is simply the
     wrong shape: there is no such thing as a partial packet. A header read
     that does not return exactly 8 bytes is a protocol error.
- **The 10 MiB cap is not the real limit — `SO_SNDBUF` is, and it is far
  lower.** A 4 MiB payload is rejected by the kernel with *"message too long"*
  on this device. `MAX_MESSAGE_LENGTH` is AppLoad's ceiling, not an achievable
  size. This reinforces §7.1's rule that images never travel over the socket;
  treat anything above a few hundred KB as needing a file path instead.
- **AppLoad mounts an app's `resources.rcc` under a random per-launch prefix**
  — observed `qrc:/ILCPSKLRYV/ui/Main.qml`. **Never hard-code `qrc:/ui/...` in
  QML**; use relative paths and let the loader resolve them. `manifest.entry`
  is still written as the in-rcc path (`/ui/Main.qml`).
- **qt-resource-rebuilder**: xovi extension that loads `.qmd` (QMLDiff) and
  `.rcc` files automatically from `$XOVI_EXTHOME/qt-resource-rebuilder/`.
  Default `$XOVI_EXTHOME` is `/home/root/xovi`.
- QML patches are **not version agnostic**. literm's README states moving
  between OS versions means editing values in the `.qmd`. rm-hacks ships one
  directory per firmware version. Plan for this.
- Stale QML cache breaks patch application. Fix:
  `rm -rf /home/root/.cache/remarkable/xochitl/qmlcache` then restart.
- **USB web interface** endpoints (toggle on in Settings → Storage):
  - `GET|POST http://10.11.99.1/documents/` — root folder listing
  - `GET|POST http://10.11.99.1/documents/<guid>` — folder listing
  - `GET http://10.11.99.1/download/<guid>/placeholder` — PDF for a document
  - `POST http://10.11.99.1/upload` — multipart upload into the **last listed
    folder**. Headers observed in the wild: `Origin: http://10.11.99.1`,
    `Referer: http://10.11.99.1/`. Field name `file`.
  - Listing responses use `VissibleName` (sic, double-s). Match the device's
    spelling; do not "fix" it.
- ~~Package manager is **Vellum**~~ **DEAD SCOPE (2026-09-16): the user chose
  not to publish to Vellum.** Quire installs from a clone via `./install.sh`.
  The Vellum notes below are kept only as platform background.
- Package manager is **Vellum** — a static build of Alpine's `apk` wrapped for
  this platform. Commands: `vellum add|del|update|upgrade|search|info`, plus
  `vellum check-os <version>` (pre-flight before an OS upgrade) and
  `vellum reenable` (restore system files after one). Package format is a
  `VELBUILD` file in Alpine aports style. Virtual packages `rmpp`, `rm2`, and a
  dynamically generated `remarkable-os` allow version-pinned dependencies, e.g.
  `depends="qt-resource-rebuilder remarkable-os>=3.24 remarkable-os<3.25"`.
- Vellum's contribution policy **forbids agent-authored pull requests** and
  LLM-written PR descriptions, and forbids `Co-Authored-By` trailers naming an
  assistant. If we ever publish a Vellum package, a human opens that PR and
  writes the description. Do not open it from this project's automation.
- Paper Pro device codenames: `ferrari` (Paper Pro), `chiappa` (Paper Pro
  Move), `tatsu`. SoC: i.MX8MM, aarch64.
- reMarkable ships official Yocto SDKs (cross toolchain + headers) at a public
  bucket. Fetch it in the Dockerfile.
- **Mihon/Tachiyomi ecosystem structure** (relevant to §7.2): a large fraction
  of extensions inherit from shared base classes in `lib-multisrc/` that
  implement common site patterns — 61 such themes covering roughly 1,700
  extension clusters *(re-counted 2026-09-15: **68** themes. See
  `docs/THEME-NOTES.md`)*. A theme is defined once; individual sites are
  instantiated from it with little more than a name and a base URL. Komikku has
  the same arrangement under `komikku/servers/multi/`. **This is the structure
  Quire adopts — the idea, not the code.**

### 3.2 Assumed — verify before depending on it

| Assumption | How to verify | Blocks | Status |
|---|---|---|---|
| `POST /upload` is reachable from on-device localhost, not only the USB interface | M0.5 spike, §11 Q1 | M5 | ⚠️ **Partly wrong — see below.** There is **no loopback bind.** xochitl listens on `10.11.99.1:80` *only* (the usb-gadget address, on interface `usb1`). An on-device process does reach it there, and `POST /upload` returns `201` with xochitl running. But the interface is off by default (`WebInterfaceEnabled=false`) and whether the bind survives USB unplug is **still open (Q1b)** |
| Panel is 1620×2160 portrait, ~229 DPI | `fbset`, or read a stock-converted PDF's MediaBox | M4 | ❌ **Method invalid.** There is no `/dev/fb0` on Paper Pro; `fbset` fails. DRM reports `405x1084` (LVDS timing, 4 px packed per clock → consistent with 1620 wide, not proof). Use the MediaBox method. **Still open (Q3)** |
| Document `.content` JSON has a usable `parent`-folder mechanism via `.metadata` | Create a folder in the UI, inspect the resulting files | M5 | ✅ **Confirmed.** `parent` lives in `.metadata`, not `.content`, and holds the UUID of a `CollectionType` record (`""` = root). Real examples in `docs/DEVICE-NOTES.md` §6 |
| xochitl indexes an uploaded document without a restart | M0.5 spike | M5 | ✅ **Confirmed.** Indexed within the same second, thumbnail rendered, no restart |
| A QML hook can trigger "open document by UUID" | M6 spike, §11 Q2 | M6 | Open |
| Qt version is 6.x on current OS | `ls /usr/lib/libQt*` | M1 | ✅ **Confirmed: Qt 6.8.2** |

---

## 4. Prior art — read for facts, copy nothing

See §1.4. These are reference material, not a parts bin.

| Project | Licence | What to learn from it |
|---|---|---|
| `Err0r-v2/Scanly` | GPL-3.0 | The closest existing thing — manga reader for Paper Pro, Qt6/QML, AppLoad tile. Learn: that the Codex SDK is fetchable from a public bucket; that parallel page fetch + resize-on-save is the right shape; that it patches `linuxfb` for waveform control (the road we are *not* taking, and why). **Do not vendor its code.** |
| `jdkruzr/reTaskable` | check repo | The structural template: QML frontend + typed backend + a `xovi/` directory of hooks, with separate PC/aarch64/armv7 builds. Critically, it has *jump-back* hooks that navigate xochitl to a specific page of a specific document. **That is the M6 primitive** — read that hook to learn which QML type is involved. |
| `asivery/rm-appload` | check repo | AppLoad itself. Its docs specify the protocol precisely — implement from the spec. Ships a **PC emulator**; this is what makes iteration bearable. |
| `asivery/xovi` | check repo | The framework. Read the trampoline/hooking notes for failure modes, incl. the documented caveat about blocking functions and the per-hook mutex. |
| `asivery/rm-literm` | check repo | Smallest real example of a `.qmd` merging a Qt Quick app into the system UI; clearest statement of the version-coupling problem. |
| `rmitchellscott/xovi-qmd-extensions`, `FouzR/xovi-extensions`, `ingatellent/xovi-qmd-extensions` | check repo | Readable per-OS-version `.qmd` corpora. Use to learn QMLDiff syntax and to see how others locate UI elements. Note these track the *latest* firmware, so treat them as a map of where to look, not as drop-in patches. |
| `asivery/rm-hacks-qmd` | — | **Not a reference.** Its QMD files are hashed, the author states they cannot reveal information about their contents, PRs containing unhashed code are auto-closed, and support for the previous version is dropped on each release. Runnable, not readable. Do not budget time trying to learn from it. |
| `asivery/qmldiff` | check repo | **The host-side workflow.** CLI that builds a hashtab from a QML tree, rewrites diffs into hashed form in place, and applies diffs to a tree writing results elsewhere. Read its language documentation before writing any `.qmd`. |
| `keiyoushi/extensions-source` | Apache-2.0 | **The theme taxonomy.** Enumerate `lib-multisrc/` for the authoritative current theme list. Read a theme to understand the *site's* URL and DOM shape, then implement against a live site yourself. Their `CONTRIBUTING.md` / `AGENTS.md` are the current source of truth for that repo — conventions change, don't rely on stale knowledge. |
| Komikku (GNOME) — **`codeberg.org/valos/Komikku`** | GPL-3.0 | Cross-reference `komikku/servers/multi/` against keiyoushi's `lib-multisrc/`: the intersection is the set of themes both ecosystems thought worth implementing, i.e. the high-value set. Taxonomy only. **Mind the identity traps:** `github.com/komikku-app/komikku` is a *different, Android* project (a Mihon fork), and the old `gitlab.com/valos/Komikku` now redirects — the GNOME project is on Codeberg. Its themes are directories, not `.py` files. Done: see `docs/THEME-NOTES.md`. |
| `vellum-dev/vellum-cli`, `rmitchellscott/reManager` | check repo | Packaging/distribution target for M8. |
| `MaximeRivest/remagic` | MIT | One-command bootstrap (xovi + AppLoad + `xovi-tripletap`). Recommend it as the user-facing prerequisite so our installer needn't own that. |

---

## 5. Repository layout

*Corrected 2026-09-15 during M1:* AppLoad requires `manifest.json`, `icon.png`
and a built `resources.rcc` (there is **no loose-file fallback** —
`QResource::registerResource` simply fails), so those plus `application.qrc`
live at the repo root and are listed below. `build/Dockerfile` was **not**
needed and is not present: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` produces a
working static aarch64 binary with the stock host toolchain, no Codex SDK.

```
quire/
├── LICENSE                     Apache-2.0 — first commit
├── PLAN.md                     this file — keep it current
├── README.md                   incl. the §1.3 responsibility statement
├── manifest.json               AppLoad app manifest
├── application.qrc             → resources.rcc (rcc --binary --format-version 3)
├── icon.png                    launcher tile
├── backend/
│   ├── cmd/quired/main.go      entrypoint; argv[1] = AppLoad socket
│   ├── appload/                protocol framing, message types
│   ├── theme/                  theme engine
│   │   ├── theme.go            the Theme interface
│   │   ├── madara/
│   │   ├── mangathemesia/
│   │   └── registry.go
│   ├── probe/                  §7.5 — reachability, challenge, fingerprint
│   ├── fetch/                  HTTP: rate limit, robots, cache, retry, SSRF
│   ├── assemble/               images → PDF (pdfcpu)
│   ├── library/                xochitl integration: upload, folders, UUIDs
│   ├── state/                  local state store
│   └── log/
├── ui/
│   ├── Main.qml
│   ├── AddSource.qml           the probe wizard
│   ├── SourceList.qml
│   ├── SeriesGrid.qml
│   ├── ChapterList.qml
│   └── Settings.qml
├── install.sh                  one-command install from a clone (M8)
├── uninstall.sh                asks before removing state; defaults to keeping
├── prebuilt/
│   └── resources.rcc           committed so Qt is NOT needed to install;
│                               drift-checked without Qt by build/prebuilt.sh
├── schema/
│   ├── source.schema.json      a configured source entry
│   └── example-index/          public-domain / self-hosted entries ONLY
├── build/
│   ├── prebuilt.sh             regenerates and drift-checks prebuilt/
│   ├── build-rmpp.sh
│   ├── build-pc.sh
│   └── install-device.sh
├── docs/
│   └── DEVICE-CHECKLIST.md     manual pre-release pass (M8)
└── docs/
    ├── DEVICE-NOTES.md
    ├── QMD-NOTES.md            QML type names per firmware version
    └── THEME-NOTES.md          fingerprints and quirks per theme
```

---

## 6. Milestones

### M0 — Device and host preparation

1. Back up everything off the tablet **before** enabling developer mode (it
   factory resets). Sync to cloud or pull via the USB web interface.
2. Enable developer mode. Record the root password.
3. Install an SSH key; stop typing the password.
4. **Confirm automatic OS updates are off and the device is on 3.25.1.1.**
   Already done on the target device, but verify rather than assume — an update
   slipping through breaks **AppLoad**, not us — M6 ships no `.qmd` (see the
   box at §6 M6), so the exposure is upstream's hooks, not our patch. reManager exposes the toggle.
5. Record `cat /etc/version` (and whatever else identifies the build) in
   `docs/DEVICE-NOTES.md`, verbatim. The installer compares against this.
6. **Hard gate — AppLoad compatibility.** Install xovi + qt-resource-rebuilder
   + AppLoad (`remagic setup`, or reManager/Vellum) and confirm the AppLoad
   launcher actually appears. Run `xovi/rebuild_hashtable`, then `xovi/debug`
   and read the output: qt-resource-rebuilder loading and caching entries is
   *not* sufficient — AppLoad resolving its own main-UI hooks without panicking
   is the thing being tested (§3.1).
   *If AppLoad does not work on 3.25.1.1*, stop. Do not proceed and do not
   improvise. Check which OS versions AppLoad's most recent **release** (not an
   open PR) supports, and move the target to that version, updating §3.1, §7.8
   and the `VELBUILD` pin together. Upgrading is close to irreversible, so make
   this decision once, deliberately, with the human.
7. Set up `xovi-tripletap` (triple-press power toggles xovi off) — your escape
   hatch when a bad `.qmd` crash-loops the UI. **Do not skip this.**
8. Full backup of `/home/root/.local/share/remarkable/xochitl/` and
   `/home/root/.config/remarkable/xochitl.conf`.
9. Host side: Docker, Go 1.22+, your own Codex SDK Dockerfile building, AppLoad
   PC emulator running.
10. `LICENSE` committed.

**Acceptance:** `ssh root@10.11.99.1 'uname -m'` returns `aarch64` without a
password prompt; `xovi/debug` shows AppLoad starting cleanly with its hooks
resolved and the launcher visible on the tablet; a trivial `GOARCH=arm64` Go
binary cross-compiles, deploys, and runs on the device; a QML resource tree has
been dumped to the host and `qmldiff` runs against it (§7.8).

---

### M0.5 — De-risking spike (before anything else)

Two unverified assumptions carry the design. Answer both now.

**Q1 — Can we add a document to the library while xochitl runs?**

On the device, with xochitl running:

```sh
curl -v http://127.0.0.1/upload \
  -H 'Origin: http://10.11.99.1' -H 'Referer: http://10.11.99.1/' \
  -F 'file=@/tmp/test.pdf;filename=test.pdf;type=application/pdf'
```

Try `127.0.0.1`, `localhost`, the wlan0 address, the usb0 address. Record which
binds work, and the port, in `docs/DEVICE-NOTES.md`. Determine whether the
"last listed folder" behaviour means we must `GET /documents/<guid>`
immediately before uploading — it almost certainly does, and that is global
mutable state, so note that it must be serialised.

*If no local bind works:* fall back to writing document files directly and
`systemctl restart xochitl`, batched once per session. Update Goal 3.

**Q2 — Can we navigate xochitl to a document from an extension?**

Read reTaskable's jump-back hook. Identify the QML type and method it calls to
open a document at a page. Confirm those names exist in the current firmware
(dump Qt resources via qt-resource-rebuilder's tooling). Write the smallest
`.qmd` that opens a known-UUID document on some trigger. Prove it works.

*If unreachable:* fall back to "download and notify" — the app says the file is
in `Comics/<series>` and the user opens it. Goals 4 and 5 still hold; only
convenience is lost. Acceptable for v1.

**Acceptance:** `docs/DEVICE-NOTES.md` contains a tested answer to both, with
the exact commands used.

---

### M1 — Hello world AppLoad app

- QML page with a button and a label.
- Go backend: connect to `argv[1]` socket, answer a ping with device uptime.
- Implement framing properly now in `backend/appload/`: u32 type + u32 length,
  length checked against the 10 MiB cap, partial-read loop, clean shutdown on
  EOF. Table tests. This is the one piece that must never be flaky.
- `build/install-device.sh` deploys to `/home/root/xovi/exthome/appload/quire/`.
- `build/build-pc.sh` runs the same QML against the emulator.

**Acceptance:** tapping the button shows real uptime from the Go process; the
same QML runs in the PC emulator against a host-built backend.

---

### M2 — Theme engine

The core of the project. See §7.2 for the interface and §7.3 for the theme set.

- Define the `Theme` interface. Implement **one** theme end to end (`madara`)
  before generalising. Resist building a framework first.
- A configured source is `{id, name, lang, theme, baseUrl, overrides}` — §7.2.
  No raw selectors in user config for theme-backed sources.
- Keep a `generic` theme carrying raw selectors plus an optional `goja` script
  hook, for one-off sites matching no theme. Escape hatch, not the main path.
- Fixtures: record real HTTP responses once, commit them, test offline forever.
  Never hit the live network in unit tests.
  **CORRECTION (2026-09-15) — this clause contradicts §1.3 and §1.4 and cannot
  be followed literally.** Recording a real aggregator's responses would put its
  domain and its content in this repository, which §1.3 forbids outright. The
  resolution, implemented in M2:
  - Fixtures are **synthetic but faithful** — written by us to reproduce the
    *markup shape* that a published piece of software emits. Madara is a
    distributed WordPress plugin and MangaThemesia a distributed theme; the
    shape of their output is an observable fact about that software, not
    anyone's expression.
  - Every host is `example.invalid` (RFC 2606). No aggregator domain appears
    anywhere in the repo — not in a fixture, filename, test name or comment.
  - Each fixture carries a header stating what it reproduces and why it is
    synthetic.
  - **Be honest about what this proves:** fixtures pin *our parser's* behaviour
    and nothing more. What proves a real site works is §7.5 **stage 5's
    capability check at runtime**, against whatever site the user chose to add.
    Never describe fixture coverage as evidence that any live site parses.
  Tests additionally install a DNS kill-switch, so an unrouted request fails the
  test rather than silently reaching the network.
- Every theme documents its fingerprint and quirks in `docs/THEME-NOTES.md`.

**Acceptance:** two structurally different themes implemented; search → series
→ chapters → page URLs works against recorded fixtures for both; adding a third
site to an existing theme requires only a config entry and no code.

---

### M3 — Add-a-source probe + browse UI

**The probe (§7.5)** is the headline feature. Implement it before the browse UI
— it is what makes "sources are data" actually usable.

- `AddSource.qml`: paste URL → progress through probe stages → verdict.
- Verdicts surface as plain language, not error codes. "This site requires a
  browser challenge that Quire can't pass" is a complete, final answer, not a
  retry prompt.
- Source list with per-source enable toggle and last-probe status.
- Search box (you will need an on-screen keyboard; check whether AppLoad
  provides one before building your own).
- Series grid with covers. Cache on disk, downscale hard, never hold more than
  a screenful in memory.
- Series detail: synopsis, chapter list, per-chapter download state.
- **No animations.** E-ink ghosting.
- Match the stock UI palette. Do not invent a brand.

**Acceptance:** paste a URL for a site of a supported theme → added and
browsable, no manual configuration. Paste a challenge-protected URL → clear
refusal, nothing added. Paste an unsupported shape → clear "unrecognised"
message naming what was tried.

---

### M4 — Download and PDF assembly

- Queue with bounded concurrency; start around 6 concurrent page fetches and
  measure.
- Resize each page to the panel's native resolution (verify: assumed
  1620×2160). JPEG ~q85. On save, not at read time. **This step is what keeps
  §3.1's performance finding true.**
- Assemble with `pdfcpu` — pure Go, no CGO, preserves the static binary.
  MediaBox matching the panel aspect so the stock reader doesn't letterbox.
  **Measured, M4:** the box is `[0 0 514 685]` (§11 Q3). **pdfcpu trap —
  `Pos: types.Full` is not what you want**: it sets each MediaBox to the
  *image pixel* dimensions (1620×2160) and ignores `PageDim`. Use
  `Pos: types.Center, Scale: 1` to get 514×685 full-bleed.
- **Aspect mismatch: fit-and-pad. Never crop, never stretch.** Comic pages are
  frequently not 3:4. Cropping silently deletes artwork and off-centre dialogue
  and cannot be undone; stretching shows on lettering. Padding applies only
  beyond a 1% tolerance. Sources smaller than the panel are **not** upscaled.
  ~~**Known limitation: webtoon strips** pad down to a small centred image.~~
  **Resolved 2026-09-16 — see §12.3.** Strips are now detected *per image* and
  cut at authored gutters. Detection is deliberately biased against acting: an
  ordinary page is never split, and a lone tall page in an ordinary chapter is
  left alone. Measured on device, splitting is also **cheaper** than not
  splitting (519 MiB peak vs 536 MiB), because cutting before the fit-and-pad
  means each piece resamples as an ordinary page rather than the whole strip.
- **Measured on the host (M4, 200 synthetic pages, 10 chapters):** 307 KiB/page
  stored and in the PDF; assembly 899 ms; download at concurrency 6 in 41 s.
  Read back: 200 pages, zero with a MediaBox other than 514×685.
  **Two numbers that need re-measuring *on the device* before they are
  believed** — the backend runs on the tablet, not the host:
  1. **Resize is CPU-bound, not network-bound.** CatmullRom costs ~1.3 s/page
     on a 10-core host. Concurrency scaling was linear only to ~8 *because the
     stub fetcher made the work pure CPU*. On the i.MX8MM this dominates, and
     200 pages could mean a very long, battery-hungry job. A single
     `Concurrency` knob currently bounds fetch **and** encode; on the device
     these want separate limits.
  2. **Assembly peak heap was ~352 MiB for a 200-page/60 MiB volume** (≈6× the
     PDF; pdfcpu holds the whole document in memory). The device has ~2 GB
     *shared with xochitl*. If this bites, the answers are smaller volumes or a
     streaming writer.
- ~~**One PDF per volume, not per chapter.**~~ **REVERSED 2026-09-16, after
  using it: one PDF per chapter, by default, even when the source publishes
  volume labels.**

  *The original reasoning was that chapter PDFs clutter the library and make
  reading position meaningless.* Clutter is real — xochitl has no folder-create
  API (§6 M5), so a 200-chapter series is 200 flat entries. But "reading
  position meaningless" was overstated: position simply becomes per-chapter,
  which is how a great many people read anyway.

  **What the original reasoning missed entirely is sampling.** The plan assumed
  the user had already decided to read something. In practice the first thing
  you do with an unfamiliar series is read a few pages to find out whether you
  want the rest — and a 7–10 minute wait before *anything* is readable makes
  that impossible. Per chapter, the first file arrives in about a minute.

  Two further gains the original weighing did not include: **a failure costs one
  chapter rather than ten**, which matters because downloads do fail; and
  cancelling costs less.

  Grouping remains available as a per-source setting (volume labels, or a fixed
  count) for anyone who wants it. **Volume labels no longer win by default** —
  the source's structure is information, not an instruction.

  Consequences: M6's chapter→(PDF, page offset) map becomes trivial (offset 0),
  which is a simplification rather than a risk; the §6 M4 confirm step becomes
  unnecessary for a single chapter and should not appear (a prompt that always
  says the same thing is one people learn to tap through); and **existing
  volume PDFs already in a user's library must keep resolving for "Read"** —
  changing the default must not orphan what is already downloaded.

  Quire maps chapters → (PDF, page offset) whatever the grouping. Per chapter
  the offset is 0, which must stay a *real* recorded offset rather than an
  assumed one — §6 M6's "Read" resolves through it.

  **Grouping is a choice at download time, not a setting (revised
  2026-09-16).** The per-source `grouping` / `groupSize` settings are **removed**
  — a preference buried in a source's config was the wrong shape, because the
  question it answers is *situational*: "am I about to be without a connection?"
  is not a property of a website.

  Instead:
  1. **Chapters are the default and always available.** One PDF per chapter.
  2. **When a source publishes real volume labels, offer volumes alongside**
     — a separate view in the chapter screen — so the user can take a whole
     volume on the spot when they want one.
  3. **Show the volume affordance only when volumes genuinely exist.** A source
     with no labels shows chapters and nothing else; an empty tab is a worse
     answer than no tab.
  4. `count` (fixed runs of N, labels ignored) is **dropped entirely**. It only
     ever applied to sources with no labels — exactly the sources that now have
     nothing to group by and no volume view to show.

  **An unknowable reading order still overrides everything** (§7.2's contract):
  a list we could not order is never assembled into a multi-chapter PDF, so the
  volume view must not be offered for it either.

  The two groupings remain internally — the *download request* says which it
  wants. In particular `storedVolumes` must keep re-deriving legacy records as
  volumes, or Read silently stops working on anything downloaded before this.
  4. **A hard byte budget, which overrides everything above.** Measured on the device
     2026-09-15: **xochitl's `/upload` rejects any multipart body of
     100,000,000 bytes or more** — a decimal 100 MB cap on the *body*, not the
     PDF. Worse, past that it frequently **resets the connection mid-upload**
     instead of answering `413`, so the whole transfer is wasted and the error
     is a confusing transport failure rather than a clear refusal.
     At M4's measured ~307 KiB/page that is about **325 pages**, and a
     10-chapter MangaDex volume can exceed it comfortably — this is not a
     corner case, it is the first real download we attempted.
     **So a volume must split when it would exceed the budget** (default ~90 MB,
     leaving margin for multipart overhead and per-page variance), into
     `Vol 3 (part 1 of 2)` and so on. The chapter→(PDF, page offset) map must
     follow the split, since it is M6's input.
     **Check the assembled size before uploading** and fail with a plain-language
     message rather than discovering it as a reset socket 90 MB in.
  **When `OrderIsKnown` is false, do not build a multi-chapter volume at all —
  fall back to one PDF per chapter**, and tell the user why in plain language.
  A volume is a *run* of chapters, so assembling one from a list whose order we
  admit we could not determine produces a silently scrambled book — the failure
  mode §7.2's contract exists to prevent, reintroduced at the next layer up.
  One-PDF-per-chapter is ugly and clutters the library, but page order *within*
  a chapter comes from `Pages()` and is authoritative, so every file is at least
  correct. Refusing outright would be worse still: the user gets nothing when we
  could have given them something true.
- A partial download must never produce a PDF. Assemble to temp, atomic move.
- Track total bytes; warn at a configurable threshold; never fill the
  partition.

**Acceptance:** a full volume downloads, assembles, and opens in a desktop PDF
viewer with correct page count, order and dimensions; total file size is sane
for the page count.

---

### M5 — Library integration

**M0.5 settled this: use the upload endpoint.** Two facts from §11 Q1/Q1b make
it the clear choice, and both must be implemented:

1. **Add the loopback alias first** — `ip addr add 10.11.99.1/32 dev lo`,
   idempotent, at backend startup. Without it the endpoint is unreachable
   whenever USB is unplugged, which is most of the time. It is safe to leave in
   place while tethered and does not survive reboot.
2. **Require `WebInterfaceEnabled=true`** as a first-run setup step (it is off
   by default and needs an xochitl restart to take effect). Detect it and say
   so plainly rather than failing obscurely.

The direct-write path stays documented as a fallback but should not be built
unless the above breaks: xochitl does not watch its document directory, so
direct writes need a full restart, and a restart costs the user their reading
position — the exact thing Goal 5 exists to protect.

**Preferred (upload endpoint):** ~~ensure/create `Comics` and a per-series
subfolder.~~ **CORRECTION 2026-09-15 — Quire cannot create folders at all.**
Established, not assumed: `strings /usr/bin/xochitl` exposes exactly three
routes — `/documents/`, `/download/`, `/upload`; the shipped web app references
only those; `POST`/`PUT`/`PATCH`/`MKCOL` on `/documents/` all return `200` with
the listing (the method is ignored); and a `CollectionType` written directly to
disk never appears in `GET /documents/`, because xochitl does not watch that
directory (§11 Q1b). A folder can therefore only be made **by the user on the
tablet**, or by a direct write plus a restart — which this milestone rules out.

> ### ⚠️ CORRECTION 2026-09-17 — the correction above was right about HTTP and wrong about QML.
>
> **Quire creates folders and moves documents into them.** Proven on hardware,
> OS 3.25.1.1, over five probe rounds:
>
> ```qml
> import com.remarkable
> var id = Library.createCollectionWrapper(parentIdString, name)  // parent FIRST
> var ex = NavigationManager.treeExplorerForNavigation
> ex.selection.clear(); ex.selection.add(documentIdString)
> ex.selectionMove(folderIdString)                                // id STRING
> ```
>
> Everything in the 2026-09-15 correction about the *web interface* still holds
> — there is no folder-create route and there never was. What was wrong was the
> conclusion drawn from it, which quietly became "Quire cannot create folders at
> all" and stood for two days while the reader handoff, the trash and the
> empty-trash were all being driven through exactly the API that could.
>
> **Both arguments are id strings, and both slots have a trap.** An Entry object
> in the parent slot is *accepted and ignored*: the folder lands at the root with
> no error at all. `selectionMove` is the opposite — an Entry object throws
> `Passing incompatible arguments`. So a created folder's parent is read back
> with `Library.parentIdForId` before anything is moved into it, and a move is
> confirmed by reading the document's parent, never by the call returning.
>
> **`parentIdForId` does answer for collections**, and an empty result means the
> root. An earlier round could not tell that apart from "does not work on
> folders", which is why both it and `entryForId(id).parentId` are read.
>
> **Nothing in that QML enumerates a folder's children.** Ten candidates were
> tried; none exist. So "is there already a folder for this series?" can only be
> asked over HTTP, and the feature is split across the socket: the backend
> decides *where*, the frontend does it, and it reports back what actually
> moved.
>
> **What each round got wrong, because the shape repeats:**
> 1. the write step carried its own list of method names and reported a dead end
>    its own read step had already disproved on the same screen;
> 2. the bodies referenced a parameter the caller did not pass — and the
>    off-device harness had *restated* the parameter list, so it tested a world
>    where the bug did not exist;
> 3. the key dump was filtered through a regex that hid `createCollectionWrapper`;
> 4. only the first creator that existed was ever called, so the `…Wrapper`
>    overload was never reached;
> 5. the arguments were passed in the wrong order for three rounds, and every
>    "success" was a folder named after its own parent — visible only by reading
>    `.metadata` on the device.
>
> The lesson that generalises: *a measured list beats a list you invented*, and a
> check that restates what it is checking is not a check. **If you are reading
> the 2026-09-15 correction and about to conclude that folders are impossible:
> they are not. Downloads land in `Comics/<Series>` today.**

> ### ⚠️ FOOTNOTE 2026-09-17 — "xochitl does not watch that directory" is true, and not the end of it.
>
> The 2026-09-15 correction says a `CollectionType` written straight to disk never
> appears, because xochitl does not watch its document directory. The *watching*
> part is correct. What it misses is that **the running library can be told**:
>
> ```qml
> Library.requestLoadEntry(uuid);
> Library.entryAdded(uuid);
> ```
>
> Found by reading rm-librarian's source (`createFolderOnDisk` → `notifyLibrary`),
> not by probing — it writes the `.metadata` itself and then notifies, and so
> creates folders without a restart. We have no measurement of our own yet; the
> librarian probe is what will provide one.
>
> It changes nothing Quire does today — `createCollectionWrapper` works and is
> measured — but it removes "a restart is unavoidable" from the list of things
> anyone should believe when planning around this API. §6 M5's original fallback
> ("write it and `systemctl restart xochitl`, batched once per session") was more
> expensive than it needed to be.

> ### ⚠️ FOOTNOTE 2026-09-17 — why Quire does not depend on rm-librarian.
>
> The rm-librarian xovi extension was researched, probed on hardware, and then
> **rejected**. This is the evidence, kept because it cost real device time and
> because the next person to find the extension will otherwise repeat every one
> of these experiments to arrive back here.
>
> It does work. `net.asivery.XoviMessageBroker` resolves inside an AppLoad app
> under AppLoad v0.4.2 on OS 3.25.1.1, `sendSimpleSignal` answers in-process,
> and xochitl's own private QML keeps working alongside it. `lookupEntry(Comics)`
> returned the same UUID measured independently over SSH, and an absent name
> returned a well-formed `ERROR: not found:`, so success and refusal are
> distinguishable.
>
> **What it could not do is the half Quire needed most.**
>
> - **`createFolder` returns `ok`, not the id of the folder it made.** Sorting a
>   download needs that id.
> - **`ensureFolder` returns a UUID and is idempotent by bare name, but takes no
>   parent argument.** `ensureFolder:Name,<uuid>` produced a folder literally
>   named `Name,<uuid>` at the root — the comma is part of the name.
> - **The only way to express a parent is a path, and the path form creates its
>   ancestors by name instead of matching the folders already there.**
>   `ensureFolder:Comics/<Series>` built a *second* `CollectionType` called
>   "Comics" at the root and filed the series under it. The device briefly had
>   two identically-named Comics folders side by side. Shipping that form would
>   have built the user a silent duplicate library.
> - **The broker's reply FIFO carries no request id and desynchronises under
>   consecutive requests** — reproduced. Every answer has to be matched to its
>   question by position, which is a correctness problem, not a latency one.
>
> So folder creation had to stay on `Library.createCollectionWrapper(parentId,
> name)` regardless: one call, an explicit parent, the new UUID returned, all
> measured. That left move and delete as the only gains, for an extension that
> is version-pinned to the OS exactly as AppLoad is. **A second pinned
> dependency for one improvement is not a trade worth making** — and the
> improvement turned out not to need it: `LibraryController.deleteEntries` is in
> xochitl's own binary (present on 3.25 and 3.27), which is what librarian was
> calling on our behalf, and Quire already imports `LibraryController`.
>
> **The precondition, measured, is what makes the delete two steps:** a live
> entry cannot be deleted. `deleteEntry` on one returned `ok` and did nothing,
> with all three files still on disk. Only an entry already in the Trash is
> removed — and then only that one: a second document sitting in the Trash
> beside it survived. That is what lets the confirmation stop promising to empty
> the user's whole Trash.
>
> **What Quire uses instead, measured 2026-09-17.**
> `LibraryController.deleteEntries([entry.id])` deletes one document, after it
> has been trashed:
>
> - `deleteEntries` is visible from QML (`Library.deleteEntries` is not).
> - **`[uuid]` works and `[entry.id]` works — on 3.25 they are the same
>   string.** The code uses `Library.entryForId(uuid).id` anyway, because
>   librarian's 3.28 work had to introduce exactly that mapping when the
>   controller stopped accepting raw UUIDs. It is free on 3.25 and 3.27 and is
>   already the 3.28 form.
> - **`[entry]` — the entry object itself — is accepted and ignored.** No throw,
>   no complaint, nothing deleted. That is the fifth API on this surface to fail
>   while looking successful.
> - **A control folder left in the Trash survived two deletions beside it.**
>   That is the measurement the confirmation sentence now asserts: deleting a
>   download no longer empties the user's Trash, and `removeAllTrashed()` is
>   gone from the frontend.
>
> **The lesson, and it now has five independent instances: on this surface a
> return value is never evidence.** `ok` means the call was accepted. A UUID
> means an id exists, not that it is the id of the thing you asked for —
> `ensureFolder` returned a perfectly good one for a folder in the wrong place
> that it had just invented. `deleteEntries` returns nothing at all and will not
> tell you it ignored its argument. The only check that has ever caught one of
> these is reading the state back afterwards: the parent, the existence, the
> `.metadata` on disk.


**Resolved design — flat, not nested:** *(superseded 2026-09-17; kept because
the upload path below is unchanged, and because the reasoning about what to do
when a folder is missing still governs every fallback.)*
- ~~**`Comics` is a one-time user setup step**~~ **Quire creates `Comics`
  itself** when it is missing, with the same call. `ComicsRemedy` stays for the
  case where *creating* it fails — the words are still the right words, they are
  just no longer the first thing the user sees.
- **Upload into `Comics` exactly as before** — `GET /documents/<guid>` then
  `POST /upload`, unchanged — and *then* move the document into its series
  folder. Sorting after the upload rather than before it keeps the download path
  free of a round trip through the frontend, and leaves the HTTP behaviour
  intact if the QML door ever closes.
- **A per-series folder is created when there is none**, named after the series
  and nothing else: the user reads that name. The target is chosen in order of
  how much it is worth trusting — the folder id recorded for this (source,
  series) if it still resolves, then a folder of that name inside `Comics`, then
  create. The id is recorded per (source, series) so a renamed folder still
  works and two series of the same title from different sources stay apart.
- **Failing to sort is not failing to download.** No bridge, a folder at the
  wrong parent, a move that changes nothing: the volume stays in `Comics`, which
  is where every download went before this, and the download is reported as the
  success it is. Only a move whose parent reads back correctly is recorded.
- **Downloads that finished while the frontend was away are filed on attach**
  (added 2026-09-17). Sorting is something only the frontend can do, so a
  download that completed with nobody listening was never sorted and no later
  event went looking for it. Attach is when the capability comes back.

  **No timer and no polling**, for the same reason the cache control has no
  schedule: this is tidying, and a task that rearranges the user's library on a
  clock is a task that moves their documents while they are reading.

  Two conditions, both required, and the second is the one that makes it safe:

  1. **Never sorted** — no series folder recorded against the record. A record
     Quire has already filed is off limits for good: a document of ours sitting
     in `Comics` *now* is one the **user** moved back, and filing it again on
     every attach would be Quire overruling them, quietly and repeatedly.
  2. **Still in `Comics` right now**, from one listing of `Comics` per attach
     rather than from `FolderUUID`. The record says where the volume landed at
     upload time, not where it is.

  Grouped by (source, series), one group at a time, stopping early if the
  frontend goes away. Silent: INFO in the log, nothing on screen.

  > **The subtle way this could eat itself.** A frontend whose bridge failed to
  > load answers `moved: []`. If that were recorded as a folder, condition 1
  > would disqualify the record **for good on the strength of a failure**, and
  > the volume would sit in `Comics` forever with Quire believing it was filed.
  > The not-moved case therefore records nothing, and a test holds it there.

  A download already in flight sorts itself when it finishes, so the two paths
  are kept apart by a registry of sorts in flight keyed by document uuid: the
  attach pass skips anything already asked about.

#### Noticing a document the user deleted on the tablet

**Asked 2026-09-17: "if i delete a manga from the regular filesystem, will quire
pick this up and clear its own leftover cache files?"** It did not, twice over.
Quire only noticed when the user tapped Read, and even then it forgot the record
and left the pages — which orphans them for good, because once the record is
gone no later delete can name those chapters. It is the same orphan class the
cache control exists to mop up.

Both halves are fixed. The Read path reclaims as it forgets, and **attach asks**:
the backend sends the recorded document uuids, the frontend answers which no
longer resolve via `Library.entryForId`, and the records that went take their
pages with them. `entryForId` is the only question asked — **a document the user
filed into a folder of their own still resolves and is not deleted**, so folder
membership is never consulted.

> ### ⚠️ A frontend that cannot check must produce no deletions at all.
>
> The reply carries `checked` as **data**. A frontend with no bridge cannot ask
> xochitl anything, and its empty list of missing documents means *"I do not
> know"* — never *"none of them exist"*. An empty list from a frontend that
> never looked must be impossible to confuse with an empty list from one that
> looked and found everything present.
>
> This is stated this bluntly because the failure is not a stale button. It is
> **every record dropped and the entire page cache deleted**, on the user's
> device, silently, because a QML file did not load. It is the bridge-not-loaded
> trap from the sorting pass with an outcome that is not comparable.
>
> Three rules, each with a test that fails when the rule is removed:
>
> 1. **Act only on `checked: true`.**
> 2. **Act only on ids that were asked about.** A reply that widens the list is
>    an answer to a different question.
> 3. **Refuse a total wipe** of more than **three** records. Everything missing
>    at once is likelier to be a bug than a user who emptied their library
>    between two launches, and from here the two are indistinguishable. Refusing
>    costs a tap — Read still cleans up any one of them individually — while
>    obeying a bug costs the cache. Three, because at that size "I deleted the
>    couple of things I had" is an ordinary afternoon.
>
> **The first of those tests was worthless when written** and had to be
> rewritten: it sent `checked: false` with an *empty* missing list, which
> deletes nothing whatever the flag says, and it passed with the guard removed.
> The dangerous reply is one that names documents *while admitting it could not
> check them* — a frontend that failed part way, a malformed message — and that
> is what the test sends now.

  **The folder name comes from `library.Record.SeriesTitle`** (added the same
  day), which the download writes from the same variable it passes to the sort.
  Both paths naming the series from one string is what stops a re-sort and a
  fresh download making *two* folders for it. The old behaviour — splitting the
  document's filename at assemble's em dash — remains as the fallback for
  records written before the field, and is wrong for exactly the series whose
  title contains an em dash of its own.
- If `Comics` is missing, upload into the deepest folder that does exist (root
  at worst) and tell the user where to create it. **Placing the volume slightly
  wrong beats refusing the download** — the bytes are fetched, and a file in the
  wrong folder is recoverable by dragging it; a refused download is not.
- **Names always end in `.pdf`** — xochitl appends it to the multipart filename
  when missing. "Correct name" in the acceptance test includes the extension.

`GET /documents/<folder-guid>` immediately before `POST /upload`; serialise this.
**§11 Q1c is answered and it is worse than "per-connection":** the upload target
is **one mutable global inside xochitl**, not connection state. Measured — an
upload with *no* preceding GET still landed in the previously-listed folder, and
a GET on one connection steered an upload on another, from a different process.
So *any* client can move it, including the user's own browser on the USB web UI.
Read the landing folder back rather than trusting it.

**Fallback (direct write):** UUIDv4; write `{uuid}.pdf`, `.metadata`,
`.content`, `.pagedata` skeletons; `systemctl restart xochitl`, batched once
per session.

Either way, record the resulting document UUID in state keyed by
(source, series, volume). That UUID is M6's handle.

**Acceptance:** a downloaded volume appears in `My Files → Comics → <series>`
with correct name and a real thumbnail; opens in the stock reader; UUID
persisted.

---

### M6 — Native reader handoff

> ## ✅ PROVEN ON HARDWARE 2026-09-15 — **M6 needs no `.qmd` at all.**
>
> This milestone was written around a QMLDiff patch, and §8 rated that the
> project's biggest standing risk. It is not needed. An AppLoad application's
> QML runs **inside xochitl's own QML engine**, so it can import xochitl's
> singletons directly. A throwaway probe app confirmed every step on the device:
>
> ```
> import device.global        OK -> object     Global.documentViewLoader  OK -> object
> loader.item                 OK -> object     import com.remarkable      OK -> object
> Library.entryForId          OK -> object     LibraryController          OK -> object
> openDocument fn             OK -> function
> OPEN: openDocument called, no exception
> ```
>
> It genuinely opened the target document — that document's `lastOpened`
> advanced and no new document was created. The whole handoff is:
>
> ```qml
> import device.global
> import com.remarkable
>
> LibraryController.setLastOpenedPage(uuid, page)   // see the page-offset trap below
> Global.documentViewLoader.item.openDocument(Library.entryForId(uuid))
> ```
>
> **Consequences, which reach well beyond M6:**
> - Delete from §8: *"bad `.qmd` crash-loops the UI"* (High during M6) and
>   *"OS update breaks the `.qmd`"*. Neither can happen if there is no `.qmd`.
> - `xovi/versions/3.25.1.1/quireOpen.qmd` (§5) is not built. The installer
>   needs **no hard version gate** and no refusal-to-install.
> - Most of §7.8's host-side qmldiff loop is no longer on the critical path. Keep
>   the tooling (`tools/rccdump`, `docs/QMD-NOTES.md`) — it is what *found* this
>   answer, and it is how a future port starts if the door ever closes.
> - **It changes what the OS pin is for.** §7.8 partly justified 3.25.1.1 as a
>   frozen target for the patch. With no patch, the only remaining reason is
>   **AppLoad compatibility** — which §3.1 always said was the real binding
>   constraint. An OS upgrade becomes "confirm AppLoad has a *released* build,
>   re-test two calls", not a re-excavation.
>
> **Still true, and still a trap:** `MainView.qml:88` honours a `page` argument
> only when a search-highlight object is also present, so page offset is
> silently dropped. Call `LibraryController.setLastOpenedPage(id, page)` first —
> which is what xochitl itself does at `Navigator.qml:857-860`.
>
> **The `.qmd` route stays documented, not deleted**, in case a future OS closes
> this door. Everything below is that fallback, no longer the plan of record.

> ### ⚠️ FOOTNOTE 2026-09-18 — on 3.27 the reader opens *behind* the app.
>
> The handoff itself is unchanged and still works — the log says "opened in the
> stock reader" — but on OS 3.27 with **AppLoad v0.5.3** the stock reader comes
> up *behind* the Quire window, and the user has to quit or minimise Quire to
> read the chapter they just tapped Read on. On 3.25 with AppLoad v0.4.2 it came
> to the front.
>
> It is not a xochitl change: AppLoad v0.5.3 renders its windows above the
> document view (upstream *"Always render windows on top of document"*, April
> 2026). **So this is a difference between the two AppLoad versions we support,
> not between the two OS versions**, even though that is how it was met.
>
> **The fix is to close the frontend after a successful handoff.** `close()`
> unloads the frontend only; AppLoad's README is explicit that a backend keeps
> running unless the app kills it, and `appload.terminate()` is what would kill
> it. **Terminate must never be called here** — a download in flight has to
> survive being handed off to the reader, which is something the user relies on.
> Only on success: a failed open leaves the app up, because the sentence saying
> why is on that screen.
>
> **It is done on both OS versions, deliberately.** Closing is an improvement on
> 3.25 too — the user tapped Read, so the reader is what they want in front —
> and the alternative is two behaviours keyed off a version Quire cannot detect
> cleanly. One behaviour that is right on both beats a fork on a fact we would
> have to guess.

Keep the `.qmd` **as small as physically possible**. Every line breaks on the
next OS update.

- One hook exposing one callable: open document by UUID (page offset too, if
  it's free; ship without it rather than adding a second hook).
- "Read" in the chapter list → message to backend → hook fires with the stored
  UUID.
- Written against **3.25.1.1 only**, living at
  `xovi/versions/3.25.1.1/quireOpen.qmd`. The installer reads the device's
  version and **refuses to install** on anything else — hard failure with a
  clear message, never a best-effort attempt. A mismatched patch is the fastest
  route to a crash loop.
- **Expect to back-port, not copy.** The reference `.qmd` corpus is maintained
  against newer firmware — see §7.8 for why the QML surface we need is
  specifically one that changed between 3.25 and 3.27.
- Document every QML type and property in `docs/QMD-NOTES.md` with the firmware
  version, so the next port is a diff, not an excavation.

**Recovery procedure — put this in the README:** triple-press power to disable
xovi, then
`ssh root@10.11.99.1 'rm -f /home/root/xovi/exthome/qt-resource-rebuilder/quireOpen.qmd && systemctl restart xochitl'`.
If patches silently fail to apply, clear
`/home/root/.cache/remarkable/xochitl/qmlcache` first.

**Acceptance:** tap "Read" → stock reader opens that volume. Read a few pages,
back out, reopen from the stock library → resumes at the right page. Annotate
with the pen → persists and syncs. Quire stores no page number anywhere.

---

### M7 — Resilience and state

- State store: single-file, crash-safe (BoltDB, or JSON with atomic rename).
  Schema-versioned with forward migrations from day one.
  **Migration #1 is already identified — use it as the worked example.** Sources
  stored before the §7.2 `AllowedHosts` change have an empty `allowedHosts`, so a
  MangaDex source added earlier would pass search/series/chapters and then fail
  at download with an SSRF refusal. Seed from the theme's declaration on load.
  **Do not implement this as "seed whenever the list is empty"** — that silently
  undoes a user who deliberately cleared it. A versioned migration that runs once
  is the distinction, which is precisely why the store is versioned from day one.
  *(No released version exists yet, so nothing in the wild is affected; this is a
  free chance to exercise the migration path with a real case rather than a
  synthetic one.)*
- Handle: user deletes the document in xochitl (dangling UUID — detect, offer
  re-download); theme implementation changes; site layout changes mid-series;
  wifi drops mid-download; device sleeps mid-download.
- **Re-probe on failure.** When a source starts returning empty results, run the
  probe again — a site that switched themes or added a challenge should be
  reported as such, not as "no results found".
- Rate limiting and backoff enforced in `fetch/`, non-bypassable by config.
  Honour `robots.txt` and `Retry-After`. Truthful `User-Agent` naming Quire and
  its version with a project URL.
- Never hold wifi awake for background work. (One community extension,
  `webserver-remote`, is known for crash-looping on xochitl restart and
  draining battery by keeping wifi up — don't repeat that.) Downloads only
  while foregrounded.
- Structured logging to a rotating file, with an in-app log viewer. SSH-free
  debugging is worth the hour.

**Acceptance:** kill wifi mid-volume, sleep, wake, resume — no corrupt PDF, no
orphaned temp files, no duplicate library entry. Point a source at a URL that
starts challenging → next sync reports it accurately.

---

### M8 — Packaging and distribution

- `install-device.sh` for developers.
- A `VELBUILD` with
  `depends="xovi rm-appload qt-resource-rebuilder remarkable-os>=3.25 remarkable-os<3.26"`,
  matching the `.qmd` target exactly. This is how `vellum check-os` warns a user
  before an OS upgrade that Quire will break. Tighten to an exact-version
  dependency if Vellum's constraint syntax allows it.
- Post-OS-update guidance in the README: `vellum reenable`, then reinstall.
- **A human opens the Vellum package PR and writes its description** (§3.1).

**Acceptance:** a clean Paper Pro with xovi+AppLoad present goes from zero to
working Quire via one documented command.

---

## 7. Specifications

### 7.1 AppLoad message types

Go constants mirrored in QML. Reserve 0.

Both header fields are **signed `int32`**, native-endian — see the correction in
§3.1. Types below are ours; negative types are reserved by AppLoad itself.

| ID | Direction | Name | Payload |
|---|---|---|---|
| 1 | UI→BE | Ping | — |
| 2 | BE→UI | Pong | JSON status |
| 10 | UI→BE | ListSources | — |
| 11 | BE→UI | Sources | JSON array |
| 12 | UI→BE | ProbeSource | JSON `{url}` |
| 13 | BE→UI | ProbeProgress | JSON, streamed, one per stage |
| 14 | BE→UI | ProbeVerdict | JSON `{verdict, theme, detail}` |
| 15 | UI→BE | ConfirmAddSource | JSON `{url, theme, name, lang}` |
| 20 | UI→BE | Search | JSON `{sourceId, query}` |
| 21 | BE→UI | SearchResults | JSON array |
| 30 | UI→BE | SeriesDetail | JSON `{sourceId, seriesId}` |
| 31 | BE→UI | SeriesDetailResult | JSON |
| 40 | UI→BE | EnqueueDownload | JSON `{sourceId, seriesId, volumeId}` |
| 41 | BE→UI | DownloadProgress | JSON, streamed |
| 50 | UI→BE | OpenInReader | JSON `{documentUuid}` |
| 90 | BE→UI | Error | JSON `{code, message}` |

**Added during M3** (mirrored in `ui/Messages.js`, drift-tested):

| ID | Direction | Name | Payload |
|---|---|---|---|
| 16 | UI→BE | ProbeAnswer | JSON — answers a question raised on the progress stream (e.g. an off-domain redirect) |
| 17 | UI→BE | SetSourceEnabled | JSON `{sourceId, enabled}` |
| 18 | UI→BE | RemoveSource | JSON `{sourceId}` |
| 22 | UI→BE | Browse | Search with an empty query — see the stage-5 correction in §7.5 |
| 23 | UI→BE | RequestCover | JSON `{sourceId, seriesId}` |
| 24 | BE→UI | CoverReady | JSON `{seriesId, path}` — a **path**, never bytes (§7.1 above) |

**Added 2026-09-15 after first real use:**

| ID | Direction | Name | Payload |
|---|---|---|---|
| 19 | UI→BE | RenameSource | JSON `{sourceId, name}` — see the stage-6 correction in §7.5 |
| 42 | UI→BE | CancelDownload | JSON `{sourceId, seriesId, volumeId}` |

**Cancelling a download is not optional.** A volume is 7–10 minutes of CPU and
up to 90 MB of traffic on a battery-powered device; starting one by mistake and
being unable to stop it is the kind of thing that makes an app feel broken.
Requirements:
- Cancel must be **prompt** — it cannot wait for the current page to finish if
  that page is a slow fetch. Plumb `context.Context` through the queue properly
  rather than polling a flag between pages.
- It must leave **no partial PDF** anywhere the library can see (§6 M4 already
  requires assemble-to-temp-and-rename; cancellation must honour the same rule).
- Already-fetched pages **stay on disk**. §6 M4's resume works by skipping pages
  that exist, so a cancelled download that is restarted later should not re-fetch
  what it already has. Cancel means "stop", not "discard".
- The UI must return the row to its pre-download state, not leave it mid-progress.

All payloads JSON. Images are **never** sent over the socket — write to disk,
send a path. The 10 MiB cap and the per-hook mutex make large transfers a bad
idea — and the *real* ceiling is lower still: the socket is `SOCK_SEQPACKET`,
so a whole message must fit one datagram and `SO_SNDBUF` rejects 4 MiB with
"message too long" on this device (§3.1). Budget for a few hundred KB, not
megabytes.

### 7.2 Theme engine

A **theme** is a Go implementation of one site *family's* shape. A **source** is
a user-supplied instantiation of a theme.

```go
type Theme interface {
    ID() string
    // Fingerprint scores a probed page 0..100 for "is this my shape?"
    Fingerprint(p *probe.Page) int
    Search(ctx context.Context, s *Source, q string, page int) ([]SeriesStub, error)
    Series(ctx context.Context, s *Source, id string) (*Series, error)
    // Chapters MUST return ascending reading order — earliest chapter first.
    // See the ordering contract below; this is not optional.
    Chapters(ctx context.Context, s *Source, id string) ([]Chapter, error)
    Pages(ctx context.Context, s *Source, chapterID string) ([]string, error)
}
```

A configured source, stored on device, validated against
`schema/source.schema.json`:

```json
{
  "id": "user-added-01",
  "name": "Example",
  "lang": "en",
  "theme": "madara",
  "baseUrl": "https://example.invalid",
  "overrides": {
    "dateFormat": "MMMM d, yyyy",
    "mangaSubPath": "manga",
    "useAjaxChapters": true
  },
  "rateLimit": { "requestsPerMinute": 30, "concurrency": 2 },
  "addedAt": "2026-09-15T00:00:00Z",
  "lastProbe": { "verdict": "ok", "at": "2026-09-15T00:00:00Z" }
}
```

`overrides` is a per-theme, schema-declared map. Themes declare which keys they
accept and sensible defaults; unknown keys are a validation error, not silently
ignored.

**SUGGESTED NAME (added 2026-09-15) — a theme may supply the display name it
would like a source of that shape to carry**, used in preference to the page
`<title>`. See the stage-6 correction in §7.5 for why: an API root's title is
accurate and useless. Optional; `""` falls back to the title, then the host.

**ORDERING CONTRACT (decided 2026-09-15, forced by a real bug in M5) —
`Chapters` returns ascending reading order, earliest first.** M5 assembled a
test volume titled *"The Lantern Keeper — 4–1"*: madara's markup lists chapters
newest-first, MangaDex sorts oldest-first, and nothing in this interface said
which was right. Since §6 M4 groups runs of chapters into one volume PDF, a
descending source produces a **volume that reads backwards** — pages in reverse
order inside a file whose reading position xochitl then owns. Silent, and
ruinous.

Ordering is the **theme's** job, because only the theme knows what its chapter
identifiers mean — string sorting `"10"` against `"9"`, or guessing at
`"12.5"`, `"Extra"` and `"Vol. 2 Ch. 3"` generically, is how this goes wrong
a second time. Every theme normalises to ascending and **every theme has a test
asserting it**. Requirements that follow:
- `Chapter` needs enough structure to order by — a comparable number and,
  where the source has one, a volume label. Grouping "runs of 10" is a fallback
  for sources with no volume structure (§6 M4), not the primary mechanism.
- Where a source's order is genuinely unknowable, say so rather than guessing:
  a wrong order is worse than an admitted one.

**INTERFACE CHANGE (decided 2026-09-15, forced by MangaDex) — a theme must be
able to declare the hosts it legitimately needs.** MangaDex serves page images
from `*.mangadex.network`, not from `mangadex.org`. §7.4's SSRF guard rejects
redirects leaving the source's registrable domain unless `allowedHosts` permits
it, so **M4's download would be refused** even though search, series and
chapters all succeed. Splitting the image CDN off the main domain is normal
practice, so this will recur.

The `Theme` interface gains a method declaring the host patterns that theme
requires (e.g. `AllowedHosts() []string`), and §7.5 **stage 6 seeds the stored
source's `allowedHosts` from it** at accept time. Rationale for putting it in
the theme rather than in user config:

- The theme is the thing that *knows* its own CDN. Making the user discover
  `mangadex.network` by reading an SSRF rejection is a terrible first run.
- It stays **auditable in code** and reviewable in a diff, rather than being an
  arbitrary host list a user pastes in — which is exactly what §7.4 was
  guarding against.

**This does not soften the guard.** The private/loopback/link-local, scheme and
credential checks remain absolute and are not overridable by a theme, a source,
or anything else. A theme may only widen the *registrable-domain* boundary, and
only to hosts it names in its own source. Persist the seeded list on the source
so it is visible to the user and survives a theme changing its mind.

Sources may be exported/imported as JSON so a user can move their setup between
devices. Quire provides no discovery mechanism, no directory, and no bundled
index (§1.3).

### 7.3 Theme priority list

Build in this order. Stop and reassess after tier 1 — two themes may cover most
of what you personally need.

**Before implementing, enumerate `lib-multisrc/` in `keiyoushi/extensions-source`
for the authoritative current list and rough per-theme site counts.** The list
below reflects the general landscape and may be stale; the directory listing is
ground truth. Cross-check against Komikku's `servers/multi/` — themes present in
both are the safest bets.

**Tier 1 — build these first; they carry the most sites**

| Theme | Shape | Notes |
|---|---|---|
| `madara` | WordPress plugin, `wp-manga` post type | **Note:** upstream has since split into `madara` and `madaralegacy`. We keep **one** theme with an `ajaxStyle` override rather than two, because the shapes differ in exactly one endpoint — reasoning in `docs/THEME-NOTES.md`. Largest family by a wide margin. Chapter list often behind an `admin-ajax.php` POST rather than in the initial HTML. Per-site variation in path segments (`manga` / `series` / `comics`) — hence `overrides`. |
| `mangathemesia` | WordPress theme, formerly WPMangaStream | Second largest. Page list typically embedded in an inline `ts_reader.run({...})` JSON blob rather than in the DOM. |

**Tier 2 — modern JSON-API families; mechanically the easiest once identified**

| Theme | Shape | Notes |
|---|---|---|
| `heancms` | Headless JSON API + Next.js frontend | Clean REST-ish endpoints, no HTML parsing. Has gone through incompatible API revisions — version-detect. |
| `iken` | Next.js + JSON API | Related lineage to the above. |
| `keyoapp` | Hosted platform | Predictable structure. |

**Tier 3 — older PHP/CMS families, still widely deployed**

| Theme | Shape |
|---|---|
| `mmrcms` | "My Manga Reader CMS", PHP |
| `fmreader` | PHP, older generation |
| `wpcomics` | WordPress, distinct from the two tier-1 families |
| `zeistmanga` | Blogger/Blogspot-hosted |
| `madtheme` | — |

**Tier 4 — smaller or largely historical; implement only on demand**

`genkan` (largely defunct), `guya`, `readerfront`, `paprika`, `peachscan`,
`liliana`, `nepnep` (MangaSee/4Life lineage, largely defunct), `greenshit`,
`etoshore`.

For each theme implemented, `docs/THEME-NOTES.md` records: the fingerprint
signals used, the endpoint shapes, which `overrides` keys exist and why, and any
site-specific quirk encountered. That file is what makes theme #6 take an
afternoon instead of a weekend.

### 7.4 Fetch layer invariants

Non-negotiable, and not configurable by a source entry:

- Global and per-host concurrency caps; minimum inter-request delay per host.
  **The floor, set in M2 and stated here because §7.2's schema alone is
  misleading:** 4 global / 2 per-host / 2 s minimum inter-request delay
  (= 30 rpm). `source.schema.json` permits `requestsPerMinute` up to 120, but a
  source asking for 120 still gets 30 — **the schema bounds what a user may
  write; this floor bounds what Quire actually does.** Per-source `rateLimit`
  narrows, never widens.
- `robots.txt` fetched, cached, honoured. If robots disallows the paths a theme
  needs, the probe reports it and the source is not added.
  **Decided 2026-09-15 — what to do when `robots.txt` cannot be read.** Three
  cases, deliberately not collapsed into two:
  - **404 / no file → allowed.** Conventional and uncontroversial.
  - **5xx or transport error → treat as *unknown*, not as permission.** Retry
    with backoff; while unknown, do not proceed. This follows from §7.6 — we
    take "no" for an answer, and an unreadable `robots.txt` is not a "yes".
  - Report a persistent failure as **`unreachable`**, never as
    `robots_denied`. The site did not deny us; we could not ask. Reporting a
    denial that never happened is exactly the kind of lie §6 M3 forbids when it
    says a verdict must be a complete, honest answer.

  **DECIDED 2026-09-15 — robots binds *crawling*, not user-directed retrieval.**
  `robots.txt` is the Robots **Exclusion** Protocol, and RFC 9309 scopes it to
  "automatic clients known as crawlers". Browsers do not consult it; nor does a
  feed reader fetching a subscription the user chose. The line is not
  program-versus-human, it is **discovery versus retrieval**:

  | What Quire is doing | robots applies? |
  |---|---|
  | Search, popular/latest listings, following links, the probe's own crawling | **Yes, strictly.** This is discovery |
  | Fetching one series, chapter or page **the user explicitly asked for** | **No.** This is not crawling |

  Forced by a concrete case rather than invented in the abstract: MangaDex
  publishes a documented public API with published rate limits for third-party
  clients, and its `robots.txt` allows `/manga` and the feeds while disallowing
  `/at-home/` — the page-image endpoint. Under a blanket rule stage 5 refuses
  it, **and by extension refuses any officially-supported API whose robots.txt
  is written for search engines.** That is not what the operator is saying.

  **This narrows nothing else.** Rate limits, the honest `User-Agent`, the SSRF
  guard, and §7.6's absolute no-circumvention rule are untouched. A `Disallow`
  still blocks every discovery request. And it stays non-bypassable by source
  config: the *classification* is Quire's own, made per request-kind inside the
  fetch layer, never a per-source flag a user can flip. Requests must carry
  their kind explicitly — do not infer it from the URL.

  **SUPERSEDED 2026-09-16 — robots.txt is no longer consulted by default.**
  The owner's decision, recorded with the reasoning because it reverses what
  this section previously called non-bypassable.

  *The argument:* RFC 9309 scopes robots.txt to "automatic clients known as
  crawlers". A person typing a search and tapping a result is driving every
  request — that is a browser, not a crawler — and browsers do not consult it.
  **No comparable reader consults it either:** verified against
  `keiyoushi/extensions-source`, where nothing anywhere fetches `robots.txt`.
  Quire's discovery/retrieval split was therefore stricter than the entire
  ecosystem, not equal to it. The operator chooses every source by hand, and
  §1.3 puts that determination with them.

  *Where the argument does not reach, recorded honestly:* two things Quire does
  are automated however they were initiated — the **probe** (§7.5 fetches
  homepage, search, series, chapters and pages in sequence, unattended) and
  **watch checks** (§12.2 polls on open, per-source cooldown). Calling those
  manual browsing would be a stretch. They are in scope of the setting like
  everything else; this note exists so nobody later mistakes the decision for a
  claim that Quire never crawls.

  **Implementation, so it stays honest:**
  - A **single global setting**, default **off** (robots not consulted). Not
    per-source — one switch, one behaviour, nothing to reason about per site.
  - **Keep the machinery and the switch.** The parser, the three-way
    unreadable-robots handling and their tests stay, so turning it back on is a
    setting rather than a rewrite. Quire may not always have one user.
  - **Log at info every time the check is suppressed**, with host, path and
    request `kind`. *(Corrected 2026-09-16: an earlier draft said "when a fetch
    proceeds past a `Disallow`", which cannot be implemented — knowing a path
    was disallowed means fetching `robots.txt`, the exact request the setting
    exists to avoid. Logging the suppressed check is the same record without
    the extra request.)* A safeguard that is off silently is worse than one
    that was never there. Log the `kind`, not a claim about who asked: the
    probe and watch checks run unattended, so wording that implied every
    request was person-driven would be untrue.
  - **Nothing else changes.** Rate limits, per-host minimum delays, the honest
    `User-Agent`, the response size cap, byte accounting and the SSRF guard all
    still apply in full.
  - **§7.6 is untouched and is not weakened by this.** No UA spoofing, no TLS
    impersonation, no CAPTCHA solving, no challenge bypass. A
    `blocked_challenge` verdict stays terminal. A site that actively refuses us
    is still a site we do not argue with — that is a different thing from an
    advisory file aimed at crawlers.
  - The `Kind` (discovery/retrieval) plumbing may stay as documentation of
    intent, but it no longer gates anything while the setting is off.

  **Worked classifications — when in doubt, classify as discovery.** The rule
  above is a narrow exception and should stay narrow; "the user is ultimately
  responsible for every request" would swallow it whole and is not the test.
  The test is whether *this specific resource* is what the user asked for.

  | Request | Kind | Why |
  |---|---|---|
  | Search, popular/latest, the probe's crawl | discovery | Textbook crawling |
  | **Cover thumbnails for a results grid** | **discovery** | The user asked for a *list*, not for each cover. Fetching 20 thumbnails in a burst is crawling behaviour, whatever triggered it |
  | Series detail page the user tapped | retrieval | One named resource, explicitly requested |
  | Chapter list for that series | retrieval | Ditto |
  | Page images of a volume the user queued | retrieval | The whole point of the request |
  | Re-probe on failure (§6 M7) | discovery | Automated, not user-directed |
- `Retry-After` honoured; exponential backoff with jitter on 429/5xx.
- Honest `User-Agent` naming Quire, its version, and the project URL. **Never
  impersonate a browser** — see §7.6.
- SSRF guard on every resolved URL: reject private/loopback/link-local ranges,
  non-http(s) schemes, and redirects leaving the source's registrable domain
  unless an explicit `allowedHosts` entry permits it.
- Response size cap; total-bytes accounting.

### 7.5 The source probe

Runs when a user adds a URL, and again when a source starts failing (M7). Stages
run in order; any stage may terminate with a final verdict. Stream progress to
the UI so a slow site doesn't look hung.

**Stage 1 — Normalise and guard.** Parse the URL, require http(s), resolve DNS,
apply the SSRF guard. Failure verdicts: `invalid_url`, `blocked_address`.

**Stage 1 normalises a missing scheme to `https://`, and says so when that guess
fails (added 2026-09-16).** Typing on an e-ink keyboard is slow, so
`weebcentral.com` is accepted as what the user plainly meant. Two rules keep
that from becoming guesswork: Quire only ever fills in **https**, and **never
falls back to `http://`** — an unencrypted connection is the user's decision, so
an unreachable host is reported with *"Quire assumed https://; type `http://…`
if this site only works without encryption"*. Anything carrying a scheme keeps
it, so a typed `http://` is respected and `ftp://`, `file://` and `javascript:`
still reach the guard's scheme check. A *nearly* right scheme (`https:/host`,
`https//host`) is `invalid_url` with the typo named, because choosing between
"missing slash" and "a host called https:" for the user is the guessing this
avoids. `example.com:8080` is a host and a port, not a scheme. The SSRF guard is
unchanged and runs on the normalised URL, so `localhost` and friends are refused
whether or not a scheme was typed — and **no dot-counting heuristic was added**,
precisely so the guard stays the thing that does the refusing.

**Stage 2 — Reachability.** `GET` the homepage with the honest UA, following
redirects and recording the final registrable domain — if it differs from what
the user typed, say so and ask before continuing. Record status, headers,
timing, final URL, body. Failure verdict: `unreachable`, with the transport
error.

**Stage 3 — Challenge and gate detection.** A hard gate. If any signal fires,
the verdict is `blocked_challenge` and **the source is refused**. Do not add it
in a degraded state, do not offer a retry, do not suggest workarounds.

Signals to check — verify each against a live example and record what you
actually find in `docs/THEME-NOTES.md`; do not trust this list blindly:

- HTTP 403 or 503 from a CDN-managed edge, combined with challenge markers in
  the body or response headers.
- Body containing challenge-platform script paths, interstitial `<title>`
  strings, or a meta-refresh to a challenge endpoint.
- Cookies whose presence implies a solved challenge is required for content.
- DDoS-Guard, Sucuri, or similar vendor markers.
- A generic JS gate: 200 OK, tiny body, no theme fingerprint matches, and a
  `<noscript>` block or client-side-render placeholder.

Also flag, as a **warning** rather than a hard block: a login wall, paywall, or
age gate the theme cannot satisfy. Surface it; let the user decide.

**Stage 4 — Theme fingerprint.** Ask every registered theme to score the probed
page. Take the highest score above a threshold. Ties or near-ties go to the user
as a choice. Zero matches → verdict `unrecognised`, listing which themes were
tried so the user can report it usefully.

Fingerprint signals should be *cheap and structural* — asset paths, generator
meta tags, characteristic DOM class names, known endpoint shapes, distinctive
inline script markers. Prefer several weak signals over one brittle strong one.
**Derive fingerprints empirically** by fetching a site known to use the theme
and diffing against one that doesn't; record both the method and the result.

**Stage 5 — Capability check.** With the candidate theme, exercise the full path
against the live site: a search (or the popular/latest listing if search needs a
query), one series detail, one chapter list, and one page image — **extracted
*and fetched***.

**CORRECTION 2026-09-16 — extraction is not the question, forced by a real
probe.** A site of the `mangakakalot` family extracted 76 page URLs perfectly
and then answered **403 with a challenge interstitial on its image host**.
Checking extraction alone returns `ok` for that site, and the user discovers it
is useless minutes later when a download fails — a **false `ok`**, which is what
this section and §6 M3 exist to prevent. "If page extraction fails the source is
useless, so refuse" plainly meant *can we get pages*. So stage 5 fetches **one
image, not a chapter**: proportionate, and enough to answer it.

The fetch goes through the **real fetch client**, so the SSRF guard and the
`allowedHosts` seeded from the theme (§7.2) are exercised here rather than at
download time. Three outcomes, and they must not be collapsed:

- **A browser challenge on the image host is `blocked_challenge`, and terminal**
  (§7.6). The verdict must say *where* — "the host it serves its pages from
  requires a browser challenge" — because a bare `blocked_challenge` after a
  search that worked is baffling.
- **Any other refusal, or a body that is not an image, is `partial`**, naming
  page fetching as the failing step. A 403 with no challenge markers is a site
  refusing this request; asserting a challenge we did not observe is the same
  class of lie as a false `ok`.
- **A guard rejection is reported as itself**, not as a challenge: a theme
  extracting images from a host it never declared is a theme bug, and naming the
  host is what makes it fixable.
**Extract pages from the *newest* chapter, not the oldest.** A site that has
changed its reader markup leaves its back catalogue exactly as it was, so
probing the earliest chapter can report a capability the user will not actually
have on anything they read. The newest chapter is the one that reflects the
site as it is now.
Require all five to produce plausible non-empty results. This is what separates
"looks like Madara" from "works as Madara".

Partial success → verdict `partial`, naming the failing step. Offer to add in a
degraded state only if search and chapters work; if page extraction fails the
source is useless, so refuse.

**Stage 6 — Accept.** Persist the source entry with the detected theme, a
default name, and the verdict with a timestamp. Also seed `allowedHosts` from
the theme's declaration (§7.2).

**CORRECTION 2026-09-15 — "a default name derived from the site title" is not
good enough, found in first real use.** Adding `https://api.mangadex.org`
produced a source called **"MangaDex API documentation"**, because that is the
`<title>` of the page an API root serves. Accurate, and useless to anyone who
did not type the URL. Two changes, and **both are needed** — neither alone is
sufficient:

1. **A theme may supply a suggested display name** (§7.2), used in preference to
   the page title. `mangadex` returns "MangaDex". This fixes the default for
   every source of a theme we support, rather than fixing one site.
   Resist "clean up the title" heuristics — stripping " — Home", " | Official
   Site", " API documentation" is an unwinnable game that will mangle a
   legitimate name eventually.
2. **The user can rename a source at any time**, which §7.2's schema already
   promised ("then user-editable") and nothing implemented. This is the general
   answer: title detection will *always* be wrong for some site, and the cost of
   being stuck with a bad name is out of all proportion to the fix.

A theme with no suggestion falls back to the page title, then to the host.

Verdict enum: `ok | partial | unrecognised | blocked_challenge | robots_denied |
unreachable | invalid_url | blocked_address`.

**CORRECTIONS from M3 — three places this section could not be built as written:**

1. **Stage 5's "or the popular/latest listing if search needs a query" is not
   implementable.** The §7.2 `Theme` interface has no popular/latest method, so
   there is nothing to call. M3 probes with a one-character search query
   instead, and the UI's "Browse" is a search with an empty query. If a real
   popular/latest path is ever wanted it needs a **new interface method**, which
   is a change to §7.2 — not a detail.
2. **The verdict enum has no value for "the user declined a question"**, which
   stage 2 can produce by asking about an off-domain redirect. Do **not** add a
   ninth verdict: stopping is not a finding about the site. The result carries a
   separate `cancelled` flag with an empty verdict.
3. **§5's layout puts this in `backend/probe/`, which cannot work.** §7.2 spells
   the interface `Fingerprint(p *probe.Page)`, so `theme` imports `probe`; the
   stage machinery needs the registry, and that is an import cycle. `probe` keeps
   only the leaf `Page` type; the machinery lives in `backend/probe/prober/`.

**On challenge signals, be honest about what is verified.** §7.5 says to verify
each signal against a live example — that is impossible under §1.3, which
forbids reaching an aggregator from this project. `docs/THEME-NOTES.md` therefore
splits every signal into **published fact** (a vendor's own documented path,
header or cookie) versus **structural inference** (edge-status plus interstitial
shape, and the generic JS gate, which is the least certain). A test fails if a
marker ships without that provenance. Never describe the inferred ones as
verified.

**Testing:** every verdict needs a fixture-driven test. Record one real response
per verdict once, commit it, and never hit the live network in CI.

### 7.6 On challenge-protected sites

Quire detects browser challenges in order to **fail clearly**, not to get past
them. There is no bypass path in this codebase and none should be added.

The practical reason: Mihon solves these by executing the challenge in Android's
WebView. The Paper Pro has no browser engine available to us, so there is no
honest implementation even if we wanted one. The design reason: a challenge is a
site operator saying no, and we take the answer.

**DECIDED 2026-09-16 — a truthful `Referer` is permitted. It is not pretence.**
Three sites (`webtoons`, `fanfox`, `comick`) parse perfectly to page URLs and
then answer **403 on the image host with no `Referer`, 200 with one**. So the
question had to be settled rather than dodged.

**The test this section actually applies is "are we pretending to be something
we are not?"** Every item in the forbidden list below fails it: a spoofed
`User-Agent` claims to be a browser we are not, a forged TLS fingerprint claims
a client we are not, a replayed clearance cookie claims a challenge we did not
pass. Each is a lie told to a server.

A `Referer` naming **the page the image URL was actually extracted from** is not
a lie. It is a true statement about our request, in the header designed to carry
exactly that fact. Hotlink protection asks "did this request come from our
page?" — and our honest answer is *yes*, because we fetched that chapter page
and took the URL from it. We satisfy the check by **telling the truth**, which
is the opposite of circumventing it. Compare a challenge, which asks "are you a
browser?", where the only way through is to lie; that stays refused and stays
terminal. Quire already sends an honest `Referer` to xochitl's own upload
endpoint (`backend/library`), for the same reason.

**Constraints, and they are what keep this honest:**
- **Per-request, and it must be the real URL** the link was extracted from. The
  theme passes it because only the theme knows it.
- **Never a constant, never fabricated, never guessed.** A `Referer` naming a
  page we did not fetch *is* a lie, and is forbidden by this section like the
  rest. If a theme does not know the referring page, it sends **no** header.
- It changes nothing else. A challenge is still `blocked_challenge` and still
  terminal, and the list below is unchanged.
- **An *unusable* referrer (relative, non-http, no host) is handled differently
  in the probe and in the download path, on purpose.** §7.6 covers `""`; this is
  the case where a theme names something it cannot have fetched.
  - **Probe: fail stage 5**, naming it as the failing step. The probe exists to
    report theme breakage honestly, and proceeding quietly would manufacture the
    `ok`-that-hides-a-bug this milestone spent real effort eliminating.
  - **Download: log a warning and send no header.** The job here is to get the
    user their comic; aborting a 200-page download over a header would be
    disproportionate, and most hosts do not require one anyway.
  Different jobs, different right answers — and **neither invents a value**,
  which is the part §7.6 actually governs.

Concretely, **do not**: rotate or spoof `User-Agent` strings, impersonate
browser TLS fingerprints, integrate a CAPTCHA-solving service, proxy through a
third-party scraping API, or replay clearance cookies harvested elsewhere. If a
future contributor proposes any of these, the answer is no, and this section is
why.

**Implemented 2026-09-16.** `fetch.Referrer` and the `GetFrom` /
`GetRetrievalFrom` pair carry it; `theme.PageReferrer` is how a theme names the
page it read. `webtoons`, `fanfox` and `comick` ship against it. What remains
is noted in `docs/THEME-NOTES.md` under "The `Referer` wall".

### 7.7 Document metadata

**To be filled in during M5 from direct observation.** Create a folder and a PDF
document through the stock UI, `scp` the resulting files off, and paste the real
JSON here with field-by-field notes. Do not populate this from memory or from a
blog post — the format has changed across versions and the Paper Pro differs
from rM2.

### 7.8 Targeting OS 3.25.1.1

The device is pinned to 3.25.1.1 with updates off. This is good news for M6's
stability and bad news for its research cost, and the plan should be honest
about both.

**What gets easier.** The `.qmd` is written once against a frozen target. No
per-release matrix, no compatibility shims, no defensive coding for QML types
that might or might not exist. Write exactly what 3.25.1.1 has.

**Why staying here is the right call, not just the inherited one.** The binding
constraint on this platform is AppLoad, not our patch (§3.1). AppLoad's own
hooks into xochitl's main UI break on new releases, and until upstream ships
support, nothing built on AppLoad runs at all. Targeting the newest OS means
accepting that your app can be dead for weeks waiting on someone else's pull
request. A settled version where AppLoad demonstrably works is worth more than
any feature in a newer release. Upgrading is also close to irreversible (§3.1),
so the asymmetry strongly favours staying.

**What gets harder — less than it first appears.** The readable `.qmd` corpora
track the latest firmware, so they won't contain a drop-in patch for 3.25.1.1.
But you were never going to copy one: you extract the QML tree from your own
device and work against that. The hashtab is generated locally
(`xovi/rebuild_hashtable`), so nothing waits on a published artefact, and
`qmldiff` runs on the host, so iteration doesn't require the tablet. The real
cost of an older version is a few hours of reading your own resource dump.

**Why this matters more than usual for our specific hook.** The QML we need to
touch is the document-opening path — `DocumentView` / `MainView` and whatever
sits between them. That is precisely the area known to have changed in this
version range: a community port of a split-document hack to 3.27.x documents
having to fix stale anchors, convert an `onOpened` handler to a `REDEFINE`, and
remove replica properties that no longer existed. Those are exactly the kinds
of differences that will bite a blind copy. Read any newer patch as a *map of
where to look*, then confirm every type, property and signal against a dump of
3.25.1.1's own Qt resources.

**Host-side development loop.** Set this up before writing a single line of
QMLDiff. It converts M6 from on-device roulette into ordinary development:

1. Dump the QML resource tree from the device once. Keep it on the host, in a
   scratch branch or an ignored directory, so you can grep it repeatedly.
2. Build a hashtab from that tree with `qmldiff`, host-side.
3. Write the patch in unhashed form — readable, reviewable, diffable. This is
   what lives in git.
4. Apply it to a copy of the tree with `qmldiff` and inspect the output. A patch
   that doesn't apply cleanly here will never apply on-device; catch it now.
5. Only once it applies cleanly, hash the diff and deploy. On-device, run
   `xovi/rebuild_hashtable` after any OS change, and read `xovi/debug` output
   rather than guessing from UI behaviour.

Commit the unhashed source patch, and generate the hashed artefact at build
time. Never let the hashed form be the only copy — that's the mistake that makes
a project unmaintainable by anyone, including its author six months later.

**Finding the right hook, in order:**

1. Locate the document-open path in *your own dump*, not in a newer repo. Record
   exact type names, property names, signal names and file paths.
2. Only then read reTaskable's jump hook and the readable community corpora, as
   orientation for which call to make — never as code to copy, and never
   assuming their types exist here.
3. Write the smallest possible QMLDiff. Prefer a single `REDEFINE` or injection
   point over several.
4. Record every difference between 3.25.1.1 and whatever a reference patch
   targeted, in `docs/QMD-NOTES.md`, as a table. That table makes a future
   forward-port a diff instead of a re-excavation.

**Pre-flight, every single time you install a `.qmd`:** have an SSH session
already open and confirmed working, and know that triple-pressing power disables
xovi. Clear `/home/root/.cache/remarkable/xochitl/qmlcache` when a patch appears
to do nothing — a stale cache looks identical to a broken patch.

**`docs/QMD-NOTES.md` structure:**

```
## OS 3.25.1.1
### Document open path
| Thing | Type / property | File in resource dump | Notes |
### Differences from <newer version> reference patches
| What | 3.25.1.1 | Newer | Consequence |
```

---

## 8. Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| ~~OS update breaks the `.qmd`~~ | **Eliminated 2026-09-15** | — | **There is no `.qmd`.** M6 opens documents through xochitl's own QML singletons, proven on hardware. See the box at §6 M6 |
| ~~Bad `.qmd` crash-loops the UI~~ | **Eliminated 2026-09-15** | — | Same reason. This was rated *High likelihood during M6*; it is now impossible |
| A future OS removes `Global.documentViewLoader` or `Library.entryForId` | Low, but no longer guarded by a version pin | Goal 4 | These are internal APIs with no compatibility promise. Mitigation is the documented `.qmd` fallback plus graceful degradation to "the file is in Comics/<series>, open it yourself" |
| AppLoad upstream breaks on a new OS | Certain, on every release | Total — app won't start | Never chase the newest OS (§3.1). M0's hard gate proves AppLoad works before anything is built on it. Only move to a version AppLoad has a *released* build for, never an open PR |
| Readable `.qmd` corpora target newer firmware than ours | High | A few hours of M6 reading | Work from your own resource dump; hashtab is locally generated; `qmldiff` iterates on the host (§7.8) |
| Staying on 3.25.1.1 indefinitely | Certain | Missed fixes, eventual cloud/API drift | Accepted trade-off. When an upgrade becomes necessary, M6 is the only milestone needing rework — §7.8 exists to make that a diff |
| `/upload` not reachable on localhost | Medium | M5 redesign | M0.5/Q1 answers it before code depends on it |
| Open-by-UUID not reachable from QML | Medium | Goal 4 lost | M0.5/Q2; fallback is an acceptable v1 |
| Theme drift — a site family changes layout | High, ongoing | One theme at a time | Fixtures pin behaviour; re-probe on failure (M7) reports it honestly; `overrides` absorb small variations |
| Fingerprint false positive | Medium | Bad UX | Stage 5 capability check is the real gate, not the fingerprint |
| Many interesting sites are challenge-protected | Medium | Scope | Detected and refused (§7.6). A known limit of the platform, not a bug to fix |
| Bad `.qmd` crash-loops the UI | High during M6 | Dev time | `xovi-tripletap` from M0; recovery command in README; never test a `.qmd` without SSH already connected |
| Device storage fills | Medium | Data loss risk | Byte accounting in M4; hard cap |
| Oversized images erode reader performance | Medium | Core UX | M4 resize step; assert a max per-page byte size in the assembler |
| Accidental GPL contamination | Low | Relicensing | §1.4 provenance discipline; if in doubt, ask the human |

---

## 9. Testing strategy

- **Unit**: framer, each theme against recorded fixtures, probe verdicts against
  recorded fixtures, PDF dimensions, state migrations. No network.

> ### ⚠️ Circular fixture validation — the failure this project actually had
>
> **A synthetic fixture can encode an invented fact, and then confirm it.** On
> 2026-09-16 the `madara` fingerprint scored **43** against a real Madara site,
> under a threshold of 60, while scoring 88 against our own fixtures. Tier 1 —
> the largest family we support — could not recognise a genuine instance.
>
> The cause: **40 of the 60 points rode on `/wp-content/plugins/madara/`, a path
> no live install serves.** Madara ships as a WordPress *theme*
> (`/wp-content/themes/madara/`) with a companion plugin registering as
> `madara-core`. The string was invented while writing the fixture, and every
> test then passed because it was checked against the fixture that invented it.
> The loop closed on itself and nothing in a green suite could see out.
>
> A second instance of the same error sat beside it: the *home* fixture was
> built from *search-page* markup, so we were scoring the wrong rendering — and
> the home page is what the probe judges.
>
> **A second form of the same loop, found 2026-09-16 by the same method.** The
> first was an invented *string*. This one is an invented *relationship*: every
> fixture in the repository put its links on exactly the host its test
> configured as the base, so "a site's links are on the base host" was a claim
> no test could disprove. A real Madara install redirects its apex to `www.`
> and renders absolute links there — so a user who pasted the apex got a
> **perfect fingerprint and a search that returned nothing**. Not an error: an
> empty list, which reads as "this site has nothing". Six themes shared the
> defect. The fix is `theme.SameSite`; the durable half is that six fixtures
> now carry a link on the sibling host, so removing the rule turns six suites
> red. A fixture that cannot disagree with the code is not evidence.
>
> **This is the specific risk created by §6 M2's synthetic-but-faithful
> fixtures.** That trade remains right — §1.3 forbids committing aggregator
> HTML — but "faithful" is a claim, and a claim needs checking against reality
> at least once.
>
> **Therefore:**
> - **Every fingerprint must be measured against a real page at least once**, and
>   the measurement recorded with a date. Not "does it pass", but *what score*,
>   against *which rendering*.
> - **Clear the threshold with margin.** `TestFingerprintsClearTheThresholdWithMargin`
>   requires a landing page to beat 60 by 10, a number measured from the weakest
>   real landing page we ship, not chosen. `mangathemesia` was at **65** — five
>   points — and would have failed on a slightly different install.
> - **Fingerprint the page the probe actually judges.** Signals that only appear
>   on a series or reader page are worth little for a source added by its root.
> - **When a fixture and reality disagree, the fixture is wrong.** Fix the
>   fixture, never the threshold: 60 is load-bearing across every theme, and
>   lowering it to accommodate one erodes the false-positive protection of all
>   the others.
- **Host integration**: full flow against a local fixture HTTP server via the
  AppLoad PC emulator. Should catch ~80% of bugs.
- **Device**: manual checklist in `docs/DEVICE-CHECKLIST.md`, run before each
  release — clean install, each milestone's acceptance test, reboot, verify
  persistence.
- **Update drill**: before shipping, install an OS update on a spare device (or
  restore a backup) and confirm the failure mode is a disabled patch with a
  clear message, not a crash loop.

---

## 10. Sequencing

```
M0   device + host prep              ──┐
M0.5 spikes: /upload, open-by-UUID   ──┤ can invalidate M5/M6 — do not skip
M1   hello-world AppLoad app         ──┤
M2   theme engine           ─┐         │ M2–M4 are ordinary Go work,
M3   probe + browse UI       ├─────────┤ testable entirely on the host
M4   download + PDF         ─┘         │
M5   library integration             ──┤ device-coupled
M6   reader handoff                  ──┤ device-coupled, fragile
M7   resilience                      ──┤
M8   packaging                       ──┘
```

M2–M4 can proceed in parallel with M0.5 given hardware time; they don't depend
on the spike outcomes.

---

## 11. Open questions

Answer by experiment, then move the answer into §3.1 and delete it here.

1. ~~**Q1** — Is `POST /upload` reachable from on-device localhost?~~
   **ANSWERED 2026-09-15 — see §3.2 and `docs/DEVICE-NOTES.md` §5.** Not on
   loopback; only on `10.11.99.1:80` (interface `usb1`), which an on-device
   process can nonetheless reach. `POST /upload` → `201`, indexed immediately,
   no restart. Requires `WebInterfaceEnabled=true`, which is **off by default**.
   - ~~**Q1b** — does that bind survive **USB unplug**?~~ **ANSWERED: no, but
     it is fixable in one line, and the fix is now part of M5's design.**
     Unplugging strips the address from `usb1` (the socket itself survives), so
     the destination stops being local and the request leaks out over the
     default route. On-device access never used USB at all — it worked because
     the address was *local* and routed via `lo`. So Quire supplies that route
     itself:
     ```sh
     ip addr add 10.11.99.1/32 dev lo      # idempotent, root, not reboot-persistent
     ```
     Verified untethered (`HTTP_OK` with no USB), and verified **safe to leave
     in place while tethered** — USB SSH and the USB web interface both keep
     working with the address on `usb1` and `lo` simultaneously.
     **This rescues the preferred M5 path and Goal 1**: no restart, and xochitl
     keeps doing all the `.content`/`.metadata`/thumbnail bookkeeping.
     Relevant because xochitl does **not** watch its document directory (its
     inotify watches are on other inodes), so direct writes are invisible to it
     and a restart would otherwise be mandatory.
   - **Q1c (still open)** — does the "last listed folder" state persist across
     connections, i.e. must we `GET /documents/<guid>` immediately before every
     `POST /upload`? Assume yes and serialise until measured.
2. ~~**Q2** — Which QML type/method opens a document, in 3.25.1.1?~~
   **ANSWERED 2026-09-15 — `docs/QMD-NOTES.md`. Found natively in our own dump;
   reTaskable was never consulted.**
   `/qml/device/view/documentview/DocumentView.qml:536,540`:
   ```qml
   function openDocument(documentToOpen)
   function openDocumentOnPage(documentToOpen, pageToOpen, highlightDetails)
   ```
   **`documentToOpen` is a `Document` object, not a UUID.** The UUID→object step
   is `Library.entryForId(documentId)` (C++ singleton, `com.remarkable`).
   `documentView` is a `Loader` (`MainView.qml:385`), also reachable as
   `Global.documentViewLoader`. The one-call form is:
   ```js
   windowNavigator.open("legacydevice/window/main",
                        { documentId: "<uuid>", page: 12, openedFrom: "quire" })
   ```
   **Trap — page offset is silently dropped.** `MainView.qml:88` honours `page`
   only when `pageHighlightDetails` is *also* truthy (a search-highlight object
   we do not have), so `{documentId, page}` falls through to `openDocument()`
   and lands on `lastOpenedPage`. xochitl works around its own bug by calling
   `LibraryController.setLastOpenedPage(id, page)` first
   (`Navigator.qml:857-860`) — do the same. Calling
   `Global.documentViewLoader.item.openDocumentOnPage(...)` directly honours the
   page but skips the archived / load-error / password checks in
   `openDocument_helper`, so prefer the navigator route plus the workaround.
   Opening is sufficient to bring the reader forward: state `"DocumentView"`
   (`when: documentView.item.documentLoaded`) flips visibility by itself.
3. ~~Exact panel resolution, and the MediaBox the stock reader expects for a
   full-bleed page.~~ **ANSWERED 2026-09-15 — `docs/DEVICE-NOTES.md` §4.**
   **`MediaBox [0 0 514 685]`**, i.e. 1620×2160 px at **≈227 DPI** (not the
   assumed 229). Measured, not inferred: had xochitl render one of its own
   native notebooks to PDF via `GET /download/<uuid>/placeholder` and read the
   box out of the output, so it is by construction the geometry the stock
   reader does not letterbox. M4 emits exactly this box.
4. Do uploaded documents sync to reMarkable cloud, and does that matter for
   storage quota or for a Connect subscription? *(Affects M5 UX.)*
5. ~~Does AppLoad provide an on-screen keyboard, or must we build one?~~
   **ANSWERED 2026-09-15 — we must build one.** AppLoad *does* ship a virtual
   keyboard (layout JSON, key images, `virtualKeyboardLayout` /
   `virtualKeyboardRef` plumbing in its `window.qml`) — **but only from v0.5.x
   onward.** Our pinned **v0.4.2 has it in no form whatsoever**: zero occurrences
   of the string "keyboard" in the binary, against 60 in v0.5.3.
   **This is the first real cost of the AppLoad-version constraint** (§3.1
   correction): the newest AppLoad that runs on 3.25.1.1 predates the keyboard.
   M3 therefore ships its own on-screen keyboard, as §6 M3 already allowed for.
   Keep it in QML, keep it dumb, and keep it isolated so it can be deleted
   wholesale if we ever move to an OS/AppLoad pair that provides one.
   *Cheaper alternative if M3 runs long:* §7.5 stage 5 already permits probing
   via "the popular/latest listing if search needs a query", so a v1 without a
   search box is coherent — browse-only, keyboard deferred. Prefer building the
   keyboard; fall back to this only deliberately.
6. ~~What is the current authoritative `lib-multisrc/` theme list?~~
   **ANSWERED 2026-09-15 — full taxonomy in `docs/THEME-NOTES.md`.**
   keiyoushi `lib-multisrc/` has **68** themes (not 61); Komikku has 18; the
   true intersection is **14** once the two ecosystems' different names for the
   same theme are folded together (`mangathemesia`≡`manga_stream`,
   `mmrcms`≡`my_manga_reader_cms`). **§7.3's tier 1 and tier 2 are confirmed
   correct — build `madara` then `mangathemesia`.** Tier-4 `genkan`, `nepnep`,
   `readerfront` and `etoshore` are gone from keiyoushi entirely: drop them.
   New since this plan was written: **`madaralegacy`** — Madara has forked into
   current/legacy variants, so expect a variant switch inside our `madara`
   theme. Per-theme *site counts* were not obtainable from the directory
   listing alone and remain unmeasured; the ordering above does not depend on
   them.

*Resolved: stock-reader performance on long image PDFs (§3.1) — a 593-page manga
PDF is fast on device. The no-custom-reader design stands.*

---

## 12. Post-plan features

M0–M8 are complete. These were asked for after first real use, and are recorded
here because this file is the project's memory (§0).

### 12.1 Paginated lists — scrolling is wrong for e-ink

**Requested 2026-09-16: "scrolling sucks on e-ink".** It does, and this is a
platform argument rather than a preference. A scroll is a continuous stream of
partial refreshes, which on an e-ink panel means smearing and ghosting the whole
way down. A page turn is **one full refresh of a settled screen** — the thing
the display is actually good at, and what the stock reader does.

Replace scrolling with page-at-a-time navigation everywhere a list appears:
series grid, chapter list, source list, log viewer.

Requirements:
- **Page size is computed from the available height, never hardcoded.** A page
  must hold whole rows — a half-visible row at the bottom is the scrolling
  problem in miniature, and it invites the swipe we are trying to remove.
- Show position honestly: "Page 3 of 12", or "Page 3" when the total is not yet
  known (a source that paginates lazily may not have told us).
- **Decouple display pages from source pages.** MangaDex returns 20 per request;
  our page might hold 9 tiles. The backend fetches and caches; the UI pages
  through what is cached and asks for more when it runs short. Do not make one
  tap equal one HTTP request.
- **No animation on the turn** (§6 M3 already forbids animation generally).
- Keep the keyboard out of the way: paging controls must not move when the
  on-screen keyboard opens.

### 12.2 Watched series, and a "new since you last looked" indicator

**Requested 2026-09-16.** Mark a series as watched; see at a glance when it has
gained something since you last looked.

**Track new *chapters*, not new volumes.** Volumes are *our* grouping (§6 M4
groups chapters into a volume PDF) — the source publishes chapters, so chapters
are the only unit with an external truth. Waiting for a volume's worth before
saying anything would make the indicator both late and wrong whenever a source's
volume labels are missing. Present it in plain language: "3 new chapters".

**When the check runs — this is constrained, not chosen.** §6 M7 forbids holding
wifi awake for background work and confines downloads to the foreground. The
same reasoning applies with more force to a poll that exists purely for
convenience:
- Check **when the app is opened**, and on an explicit "check now".
- **Never** on a timer, never in the background, never with a wakelock.
- **Stagger and throttle.** Someone watching 40 series must not produce 40
  requests in a burst on launch — the §7.4 limiter will serialise them anyway,
  so make the UI honest about it and let results arrive incrementally rather
  than blocking the screen.
- Per-source cooldown, so reopening the app repeatedly does not re-poll.

**Robots classification: a watch check is `discovery`.** It is automated and
repeated, which is what §7.4's exception was written to exclude, even though the
user did ask for the series. The exception stays narrow — that was the condition
on granting it at all.

**State:** a watched flag plus a last-seen marker per series, in the versioned
store (§6 M7), so "new" is meaningful across restarts. Seeing the series clears
it. A watched series whose source is removed is dropped with it.

**At-a-glance summary (added 2026-09-16).** The point of the feature is knowing
*without looking*, so `WatchList` carries a composed summary alongside the rows:

```json
"summary": {
  "seriesWithNew": 3,      "newChapters": 11,      "failed": 1,
  "short":  "3 new",                      // for the entry point, may be ""
  "phrase": "3 series have new chapters"  // for the watched screen, may be ""
}
```

The **backend composes the wording**, including plurals and the failure case —
§2 keeps QML dumb, and "3 series have new chapters" is a sentence, not data.
QML renders `short` and `phrase` verbatim or shows neither. Counts travel too,
so the view can decide *whether* to show something without deciding *what* it
says. Empty strings mean nothing to report: an entry point that always shows a
badge teaches people to ignore it.

Prefer text to a dot. The panel is greyscale, and "3 new" survives a partial
refresh legibly in a way a small coloured marker does not.

**Failure is not "new".** If a check fails — network, challenge, a theme that no
longer matches — say so on the series rather than showing zero or, worse, a
false badge. §6 M7's re-probe applies here: a watched series that starts
returning nothing is evidence the source changed, and is exactly the trigger
that machinery exists for.

### 12.3 Strip splitting — making vertical-scroll sources readable

**Requested 2026-09-16.** `webtoons` parses and downloads correctly and the
result is unusable: its pages are single images thousands of pixels tall, and
§6 M4 fits a page into a 3:4 PDF page, so a strip becomes **a small centred
sliver with white margins either side**. Technically correct, unreadable. The
M4 memory guard bounds the cost and does not change the outcome.

The fix is to cut a tall strip into panel-shaped pages. **Do not cut blindly.**

**Why this is tractable at all:** vertical-scroll comics are *authored* with
horizontal gutters — bands of flat background between panels. That is a format
convention, not luck, and it is what a splitter exploits.

**The algorithm, in order of preference:**

1. **Gutter detection.** A row is a candidate cut when it is near-uniform across
   the full width. Allow tolerance — JPEG noise and gradients mean "uniform"
   is never exact.
2. **Search a window, not a point.** Around each target height (one panel page),
   look ±20% and take the best candidate. Pages come out slightly uneven and
   nearly every cut lands where the author already put a break. Cutting at
   exactly the target is what produces mid-panel slices.
3. **Minimum-energy seam as the fallback.** Where no gutter exists in the
   window, cut at the row of least gradient energy. Always finds something;
   degrades gracefully on art with no clean gutters.
4. **Small overlap (≈5%) across every cut.** A panel spanning a boundary then
   appears whole on both pages. Costs a little space and guarantees nothing is
   lost — which matters because a reader cannot scroll across our page break.

**The unavoidable case, stated honestly:** a single panel taller than one page
*must* be cut, or scaled below legibility. Rare — a full-height splash — but
real. Cut it, and let the overlap carry the loss.

**Constraints:**

- **Only engage for strips, detected per *image*, never per source.**
  A single index hosts both formats, and so does a single series, so a
  per-source flag or a hardcoded list is the wrong unit and will be wrong
  immediately.

  **The asymmetry decides the design.** Failing to split a strip leaves the user
  exactly where they are today — a sliver, bad but survivable. *Wrongly* splitting
  a manga page mangles content that was fine, across a whole volume, silently.
  So detection is biased hard toward **doing nothing**, and every ambiguous case
  resolves to "leave it alone". This is the same rule as §7.5's challenge tiers,
  for the same reason.

  **Primary signal: aspect ratio, with a deliberately huge dead zone.** Real
  values, measured:

  | content | h:w |
  |---|---|
  | double-page spread (3200×2200) | **0.69** |
  | A4 scan (2480×3508) | 1.41 |
  | typical page (1200×1700) | 1.42 |
  | two pages stacked vertically | ~2.8 |
  | *nothing legitimate lives here* | 3 – 5 |
  | short webtoon strip (800×4000) | 5.0 |
  | webtoon strip (800×8000) | 10.0 |
  | long strip (800×20000) | 25.0 |

  The gap between ordinary content and strips spans **several multiples**, not a
  few percent. Put the threshold in the empty middle and stay well clear of the
  vertically-stacked-pages case at ~2.8 — that shape is rare but real, and it
  must survive untouched.

  **Corroboration, because one signal is how we got the challenge detector
  wrong:** a tall image among ordinary pages is an *outlier* — a spread, a
  credits page, an author's note — and outliers are the false positives we care
  about. Require either that **most images in the chapter are also tall**, or a
  ratio so extreme that nothing else explains it. A single tall page in an
  otherwise normal chapter is left alone.

  **An escape hatch, because detection will eventually be wrong.** A per-source
  override (`splitStrips: auto | never | always`, default `auto`) so a user who
  sees a bad result can stop it without waiting for us. Detection decides;
  the user overrules.

  **Make it visible.** A `Stats` counter as `PagesGuarded` and
  `PagesRequantised` already do. Splitting that happens silently is splitting
  nobody can report.

  **Tests must include the shapes that must NOT split:** the double-page spread,
  the A4 scan, the typical page, two pages stacked, and a single tall page
  inside an otherwise ordinary chapter. Per §9, measure the thresholds against
  real page geometry rather than choosing round numbers — and state the measured
  margin, as `TestFingerprintsClearTheThresholdWithMargin` does.
- **The chapter→(PDF, page offset) map must follow the new page count.** It is
  M6's input, and §6 M4's volume splitting already records rather than derives
  it. One source image becoming eight pages changes every downstream offset; a
  splitter that does not update the map silently breaks "Read".
- **Renumber pages from 0 per chapter.** `assemble` rejects a chapter whose
  `Page.Index` does not start at 0 and run in order — found the hard way during
  volume splitting.
- **Mind the memory.** Strips are the largest inputs we handle and §10.5's guard
  exists for them. Split **before** the fit-and-pad resize where possible, so
  each piece is resized rather than the whole strip.
- Surface it: a `Stats` counter as `PagesGuarded` and `PagesRequantised` do, so
  a source being split unexpectedly is visible rather than silent.

**Testing.** Synthetic strips built with *known* gutter positions, so a test can
assert the cuts landed in them. Include: clean gutters at regular intervals;
gutters at irregular intervals; **no usable gutter at all** (forces the
energy fallback); a single panel taller than a page; and an ordinary 3:4 manga
page that must come through untouched. §9's warning applies with force — a
generated strip that is *convenient* proves nothing, so make the awkward cases
genuinely awkward.

#### The case this section did not contemplate — a source that pre-slices

**Measured 2026-09-17, Sinners' Game Ch 29 from comick, on the device.** 28
source images; cached dimensions 1080×1440, 1125×1500 and one pair at 1500×2000
— **every one exactly 3:4**. The assembled PDF: 28 pages, every page 514×685 pt,
h/w 1.33, which is the screen's own ratio. The geometry was right. Page 0003 was
nevertheless cut through a speech balloon at its top edge and through an arrow at
its bottom, and one page of the user's was nothing but the gutter between two
panels.

**The splitter had never fired in real use.** `find` across the whole page cache
returned no `*-of-*.jpg` at all, on any chapter. It was not misbehaving; it was
never reached.

Everything above assumes the choice is between one long strip and one real page.
This is a third thing: **the source delivers a vertical-scroll comic already cut
into fixed-ratio chunks, at arbitrary points.** `ShouldSplit` looked at each
chunk, correctly saw something that is not tall, and correctly did nothing —
after which Quire faithfully reproduced the site's own bad cuts. *We did not
split it badly; we passed on someone else's split.*

One symptom reported alongside it was withdrawn and is recorded so nobody
optimises for it again: "pages are longer than the screen so I have to scroll"
was the stock reader permitting a little overscroll on a page that already fits.
**Page size and the fit-and-pad behaviour are correct and unchanged.** The only
question is where the cuts fall.

**The new path is a chapter-level decision taken before the per-image one.**
`never` still disables everything, `always` still reaches the old splitter, and
the 3.76 threshold is untouched.

*Detection requires three independent signals, and any one failing leaves the
chapter exactly as the source sent it* — §12.3's asymmetry applies with more
force here, because a wrongly re-cut chapter is ruined art across all of it:

1. **Six images at least.** "These are all the same shape" says nothing about
   three images.
2. **One shape.** Every image's h:w within 12% of the others. Mechanical slicing
   produces one ratio; a real chapter carries a spread, a credits page, a plate.
3. **Seams that continue.** The decisive signal: the bottom row of image N and
   the top row of N+1 are compared, and a majority must *continue each other* —
   what a cut through a drawing looks like, and what a page boundary never does.
   A seam where both rows are flat background is evidence of neither and is
   excluded from the majority; a chapter whose seams are all clean is either an
   ordinary comic or a strip the source cut politely, and both are left alone.

**Widths are deliberately not required to match.** The measured chapter has
three of them. A shared-width rule — the obvious one — would have refused the
very chapter this exists for. Comparison happens through a fixed 256-sample comb
and stitching through a common width.

**Cutting** reuses §12.3's gutter test, and adds two rules that come straight
from the complaint: a page that is nearly all background is folded into a
neighbour, and a cut prefers a gutter past the target over cutting through art —
a tall unbroken panel is scaled to fit, as §6 M4 already does. Blank rows at the
very top and tail are trimmed first; without that, a chapter ending in white had
all of it merged onto the last page, measured at 15,841 rows against a
1,440-row target.

**Memory, on a 2 GB device.** Nothing decodes a chapter into one buffer. The
scan holds one image at a time and keeps one byte per row (~40 KB for 28
images); the renderer holds one source and one page. Measured for the 28×1080×1440
case: **19.0 MiB peak live heap**, against 166 MiB for the chapter in one buffer
and the 1.63 GB OOM that made this rule. The measurement itself took two
corrections — `HeapAlloc` counts uncollected garbage, and Go's precise stack
liveness collects the page *while it is nominally in hand* — so the test asserts
the figure is not merely small but **explicable**: one source plus the tallest
page, ±0.2 MiB.

##### What the source actually does, measured

The images are **letterboxed**: white bars above and below a band of drawing,
and the drawing cut mid-panel. That is the defect the user sees — more than half
of each page is white space — and it has two consequences for the code. The seam
test must compare the *trimmed* art edges, because the outermost rows are
padding: a first measurement of the true positive reported "0 informative seams",
which was true of the rows and useless about the chapter. And the stitcher must
drop the padding, or re-stitching rebuilds the very bars that are the complaint.

##### Detection, and the numbers it came from

Corpus: three real chapters off the user's device. `sinners-ch29` (28 images,
the comick webtoon, the true positive), `jjk-ch1` (52 images, Jujutsu Kaisen
ch.1, real manga as delivered) and `jjk-ch1-uniform` (the same chapter with its
three odd-sized pages removed, 49 images all 797×1062 — real manga art at
perfectly uniform dimensions, which is the norm for scanlations and therefore
the dangerous case).

| chapter | images | aspect spread | padding (median) | seams continuing | verdict |
|---|---|---|---|---|---|
| `sinners-ch29` | 28 | 1.333–1.333 | **58%** | **19/27 = 70%** | pre-sliced |
| `jjk-ch1-uniform` | 49 | 1.332–1.332 | 2% | 3/48 = 6% | left alone |
| `jjk-ch1` | 52 | 1.332–1.333 | 2% | 4/51 = 8% | left alone |

**Neither manga chapter is refused on shape.** 1455×1940 and 797×1062 are both
4:3, so the aspect rule passes `jjk-ch1` straight through and the evidence does
the work — which makes it a second real test of the seam signal rather than the
easy case it was assumed to be. A corpus test asserts that a future change must
not start refusing these on shape, because that would hide the thing being
tested.

The correlation threshold was swept rather than chosen. Fraction of seams
clearing each value:

| T | sliced webtoon | manga (uniform) | manga (delivered) |
|---|---|---|---|
| 0.30 | 78% | 19% | 16% |
| 0.50 | **70%** | **6%** | **8%** |
| 0.70 | 37% | 6% | 6% |

0.5 with a 50% majority sits where the gap is widest and is clear of both sides.

**The padding threshold is a median, and deliberately.** Independent measurement
with a different row-uniformity measure agreed on the separation — 58% (max 74%)
against 5–6% — but showed **a single real manga page reaching 16%**, above the
15% threshold. Individual pages cross it routinely; the median is what protects
us, and it does so by a factor of ten.

**The AND is what makes this safe, more than either number.** A letterboxed
manga release attacks the padding signal directly and is the negative most worth
having — but it would still need half its seams correlating at 0.5, and real
manga measured 6–8%. Neither signal is trusted alone: a chapter of letterboxed
*pages* is padded without being a strip, and near-identical pages could
correlate without being one.

**Correlation replaced brightness similarity after brightness accepted 13 of 13
seams on a chapter of unrelated full-bleed pages.** Two dark pages are alike in
tone and have no reason to agree about *where* their dark pixels are. It is the
clearest case in this feature of a plausible signal that had to be measured
against the adversarial one before it could be believed.

**The limitation: a webtoon sliced edge-to-edge with no padding is refused.**
The letterbox signal carries this rule and there is exactly one positive chapter,
from one source, to calibrate it on. That is a false negative — a page the user
can see and complain about, rather than a volume silently mangled — and
`splitStrips: always` is the way in until another positive chapter says
otherwise.

**Detection is tested against the corpus and nowhere else**, because synthetic
fixtures produced two false results while it was being written: a hand-made
"ordinary manga" whose pages were one formula with a per-page offset correlated
at 11 of 11 seams and was accepted, and a hand-made "sliced strip" whose texture
changed too fast row to row correlated at 0 of 9 and was refused. The synthetic
tests now cover mechanics only and call `ForcePlan`, which skips detection.
**Those corpus tests skip when the images are absent** — which is every machine
but the one holding them — so a green `make check` elsewhere does not verify
detection, only that nothing else broke.

##### On disk

Re-cut pages live in `<chapter>/restitched/`, beside the sources, with a marker
naming the sources they were made from:

- **The sources are kept**, because resume is what they are for: a chapter that
  cannot resume restarts from zero on a dropped connection, which on a tablet on
  wifi is the common case. A re-stitched chapter therefore costs roughly double
  its own cache footprint — a known cost, reclaimed whole by deleting the
  download and by the cache control, both of which remove the chapter directory.
- **The marker is written last, and atomically.** A re-cut that dies halfway
  leaves pages with no marker, which the next run treats as absent and redoes;
  the same rule the `-of-%03d` naming exists for.
- **A mismatched marker is not an error**, it is the old path: the chapter is
  re-cut, or left alone, as if nothing had been there.
- Every directory scanner skips directories explicitly rather than relying on a
  filename pattern not matching.

### 12.4 Deleting a downloaded volume — proven reachable

**Requested 2026-09-16: a delete button on a chapter, so removing a download
does not mean hunting for it in `Comics`.**

§6 M5 established that xochitl's *web interface* has no delete route — the whole
surface is `/documents/`, `/download/`, `/upload`. But the **QML** side does,
and it is reachable the same way M6's reader handoff is:

```qml
import com.remarkable
var ex = NavigationManager.treeExplorerForNavigation;  // C++ singleton
ex.selection.clear();
ex.selection.add(documentId);                          // an ID STRING
ex.selectionMoveToTrash();
```

Proven on hardware, step by step:

```
READ:   size=0 folder=root addType=function trashType=function
SELECT: size after add = 1
TRASH:  size before=1 after=0
```

The document's `.metadata` then reads `"parent": "trash"` — **moved to xochitl's
own Trash, not destroyed**, which is the right behaviour: recoverable by the
user, and it is what the stock UI does.

> ### ⚠️ There are TWO selections. Using the wrong one wedges the UI.
>
> A first attempt used `Library.documentSelection` and **broke the navigator** —
> the side menu became unreachable until xochitl was restarted.
>
> | object | API | role |
> |---|---|---|
> | **`explorer.selection`** | `.clear()` `.add(id)` `.remove(id)` `.size` | what `selectionMoveToTrash()` acts on |
> | `Library.documentSelection` | `.clear()` `.toggle()` `.hasContent` `.ids` | tags and document-view state; **`Navigator.qml:67,395` bind their enabled state to it** |
>
> Writing `Library.documentSelection` from outside leaves the navigator's own
> view of the selection inconsistent with the binding driving its UI. **Never
> touch it.** `explorer.selection.add` also takes an **id string**, not a
> `Document` object (`Navigator.qml:733`).
>
> **Method lesson, and it generalises:** "can I reach this API" and "is it safe
> to drive this API from outside" are different questions. The M6 probe was safe
> because it only *read* singletons and *called* a function. This one *wrote to
> state another component owns*, and the first attempt chained three writes
> behind one button so nothing could be inspected between them. Probe writes one
> at a time, verify after each (`selection.size` is the observable here), and
> provide a reset.

**Implementation notes:** clear the selection afterwards so nothing is left
selected under the user; and a stored `library.Record` for a trashed document
should be dropped, since §6 M6 already handles `entryForId` returning null with
an offer to download again.

#### Emptying the Trash — probed separately, and it destroys

The user asked for the Trash to be emptied after a delete ("i don't really mind
if my whole trash is emptied"). That was probed on its own rather than assumed
safe because a sibling method worked — the same discipline as above, one write
per button, read-only first. Hardware, OS 3.25.1.1, 2026-09-16:

```
READ:   selection.size=0 moveToTrash=function ex.removeAllTrashed=function
        ex.emptyTrash=undefined Library.removeAllTrashed=undefined
        LibraryController.removeAllTrashed=undefined
EMPTY:  called on explorer
```

- **`removeAllTrashed()` lives only on `NavigationManager.treeExplorerForNavigation`.**
  `emptyTrash` does not exist, and neither `Library` nor `LibraryController`
  carries it. There is nothing to fall back to and nothing to probe for.
- **It destroys rather than hides.** A document deleted through Quire at
  18:39:58 had its `.metadata` and content replaced, by the 18:41:22.745 call,
  with `<uuid>.tombstone` whose entire body is `Wed Sep 16 18:41:22 2026`.
  Documents left with `parent: trash` afterwards: 0.
- **Nothing else was touched.** No other file under
  `/home/root/.local/share/remarkable/xochitl/` had an mtime later than 17:23.
- Nothing wedged; later taps and the user's own UI were fine.

Two consequences for the code:

1. **The copy has to say so.** Quire's confirmation says the download is deleted
   *for good* and that the whole Trash goes with it, including anything of the
   user's own that is in there. It is destruction, and the sentence that asks
   for it is the last moment it can honestly be said.
2. **Emptying is a second step and fails separately.** It runs only after a move
   to Trash that worked, and a move that worked with an emptying that did not is
   still a delete: the download is out of the library and the record is gone, so
   it is reported as done with a note about the Trash, never as a failed delete.

**An incidental finding, and it is load-bearing:** the probe called
`selectionMoveToTrash()` on a UUID that no longer existed and `selection.size`
stayed **1** instead of dropping to 0. That is why the frontend treats a
non-zero size after the move as a failure rather than as defensive decoration.

#### What a delete reclaims — and why re-downloads used to be instant

Deleting the document was not the whole of deleting. Measured on the device
2026-09-16: a chapter deleted and downloaded again came back in **25 seconds for
64 pages**, without touching the network. The document really had been
destroyed; what survived was the page cache under
`~/.local/share/quire/downloads/<source>/<series>/<chapter-slug>/`, which the
re-download found and skipped straight past. That cache had reached **636 MB**,
42.7 MB of it for one series.

The cache exists on purpose — §6 M4's resume works by skipping page files that
are already there, so a cancel at page 300 of 325 costs 25 pages rather than
325 — but it outlived the document it was fetched for. Asked, the user chose:
**a delete clears the cached pages too**, accepting that a re-download refetches.

So a successful delete now removes, and reports the bytes freed in the log:

- the assembled PDF and its `.quire.json` manifest sidecar;
- each chapter directory the record listed;
- the series directory, and then the source directory, once empty.

Four rules, each of which is a way to get this wrong:

1. **The directory name is never recomputed.** `download.ChapterDir` is exported
   for exactly this, because the slug is 48 readable characters plus 8 hex of
   the sha256 of the *untruncated, unsanitised* id. A second implementation
   removes nothing, or removes another chapter's pages, and only one of those is
   loud.
2. **A chapter can belong to more than one record** — a volume document and a
   per-chapter one over the same ground, or two parts of a split. The check runs
   against the records that *remain* after this one is dropped, so it has to
   happen after the store removal, not before.
3. **Nothing outside the downloads root is removed.** Every path is resolved and
   checked against the root before anything is deleted, including the PDF taken
   straight from the record. Chapter ids and titles are the source's to choose,
   and "inside the downloads directory" is the property that has to survive
   whatever it chose.
4. **A running download keeps its pages.** The service tracks the chapter ids the
   download in flight is writing, and a delete skips those; removing them
   mid-write would leave a volume assembled out of whatever survived. A *queued*
   download is not protected and does not need to be — it has written nothing,
   and finding its pages gone costs it the fetch it would have skipped.

The confirmation wording is unchanged. It already says the download goes for
good; that a later re-download has to fetch the pages again is what "deleted"
means everywhere else, and a second clause for it would lengthen an already long
sentence to say something nobody is surprised by.

#### Clearing the cache — Settings, one button, no schedule

Reclaiming on delete does not reach everything, and what it misses is orphaned
**permanently**. Two ways in:

- **Pages a delete had to skip.** Rule 4 above leaves a running download's pages
  alone — and by then `libStore.Remove` has run, so no future delete will ever
  name those chapters again.
- **Pages no record ever pointed at.** A download that failed, or was cancelled
  before its document existed, leaves what it had already fetched.

The user spotted this from the delete work — *"will these files just stay in
limbo indefinitely? Maybe we can add a clear cache option in the main menu?"* —
and it is a control on the Settings screen, beside the robots toggle and the
log.

**It shows the size first.** A button to clear something whose size you cannot
see is a button nobody dares press, and the figure is also how the user confirms
it worked. The size and every sentence about it are composed in the backend
(PLAN §2); the view never formats a size, so "636 MB" is spelled one way in the
application.

**It clears everything not being written**, not only the orphans. "Clear cache"
should mean what it says, and a rule the user can predict beats a clever one
that reclaims slightly more. It is safe in a way worth stating: the documents
live on the tablet, in xochitl's library, wholly independent of these pages, so
the cost is a refetch and only if the user deletes a download and wants it back.

The same four rules as reclaiming apply, plus:

- **A running download keeps its pages, and the sentence says so.** The claim is
  keyed by *directory* rather than chapter id, because clearing sees only what
  is on disk and a slug cannot be turned back into the id it came from. What was
  kept is counted and reported — "Cleared 592 MB. 44 MB was left, because a
  download is still using it" — since a figure that silently omits 40 MB is the
  small lie that becomes a bug report.
- **It asks first**, like a delete, and for the same reason: hundreds of
  megabytes, and the only way back is to fetch them again.
- **Nothing else is touched.** Not `library.json`, not the source list, not the
  logs, not the covers.

**No automatic clearing, and no timer.** The user asked for a control, not a
policy. A cache that empties itself is a download that vanished the night before
a flight.

### 12.5 The downloaded overview

**Requested 2026-09-17: "since we can download stuff without watching them, the
watch list on its own is not enough, also add a 'Downloaded' overview, with a
list of mangas where at least 1 chapter/volume is downloaded, and links to the
entry."**

Downloads and the watched list are different sets, and until this existed a
download nobody was watching could only be found by remembering where it came
from. The screen lists every series with at least one volume on the tablet,
newest first, and each row opens that series' chapter list through the same
route the grid and the watched list use — one series screen, one message, one
Back behaviour.

**A row is one (source, series) pair, always, and always names its source.** The
user's reason is navigational — *"make sure to separate by source if multiple
entries from multiple sources exist for the same series, otherwise we dont know
which one to link to"* — and it is applied unconditionally rather than only when
two rows would collide. A grouping rule that changes shape depending on what else
is in the library is one nobody can predict, and a source label that appears only
sometimes is one nobody can rely on. "Which of my sources did this come from" is
worth knowing on its own.

**The decisions that were not defaults:**

- **A source that has been removed still lists its downloads.** They are
  documents on the user's tablet and do not stop existing because a source did.
  The row says why it cannot be opened, in the backend's words, and its tap
  target is *disabled* rather than failing quietly on a tap.
- **The count is of "downloads", not of chapters.** A record is one document — a
  volume, or one part of a volume split for the upload cap — so counting records
  is counting files, which is true for every record. "Chapters" would be wrong
  for a volume record and unanswerable for one written before chapter ids were
  kept, and a number that is right for some rows and wrong for others is worse
  than a vaguer one that is always right.
- **A record with no `SeriesTitle` is named by `seriesTitleOf`**, the same
  em-dash rule the attach-time filing pass uses, so one rule governs both. Where
  even that yields nothing the series id is shown: ugly, but true, and **nothing
  is skipped** — every row is a download the user made.
- **Fetched every time the screen is shown.** *(corrected 2026-09-18.)* No push
  and no live updates: a download or a delete shows up the next time the screen
  is opened, which is sufficient and is a great deal less machinery. It was
  first written as "fetched when the screen opens" and implemented on one route
  in only — from the source list — so opening a series from a row, deleting that
  series' last download and coming back left a row pointing at nothing until the
  screen was left entirely. **A list is not "fetched when it is opened" if one
  of the ways it is opened does not fetch it.** Every route now goes through
  `showScreen`, and `ui/Screens.js` says which screens need re-asking for and
  why the others do not: the watched list is *pushed* on attach and after every
  change, browse is the source's own catalogue served from the paging cache, and
  the series screen already refetches on every route in.
- **A refetch, not arithmetic in the view.** One delete can change a row's
  count, remove the row, or touch several rows at once, and the backend already
  works all of that out from the records (PLAN §2). A view that patched its own
  model would be a second implementation of that sum, and the two would disagree
  eventually — on the screen, in front of the user.
- **No covers.** Records carry no cover URL and inventing a lookup to decorate a
  list is work nobody asked for. Text rows, like the watched list.
- Paged like every other list (§12.1).

**A note on answering at all.** *(2026-09-18.)* Every one of these handlers is a
reply to a question the backend is blocking on, and until this date each called
into `ReaderHandoff.qml` unprotected. On 3.27 one of those calls threw, the
exception unwound past the `send` that would have reported it, and the backend
sat out its thirty-second ceiling — *"no answer about a filing"* — while the
error sat in a QML console nobody reads on a tablet. `ui/Answers.js` now stands
between every handler and the bridge: a throw becomes the negative reply the
handler already knew how to send, carrying the exception text to the backend's
log. **The timeouts are backstops, and a backstop reached routinely is a
mechanism.** The harness asserts first that nothing escapes at all, because the
symptom of a missing catch is a script that dies where it stands — the same
silence, in the one place it could otherwise hide.

**A note on testing an inert row.** The harness first asserted "tapping the
removed-source row does nothing" by emitting `clicked()` on its MouseArea — which
invokes the handler directly and bypasses `enabled`, so it fails for reasons
that have nothing to do with the device. What makes the row inert for real input
is the property, and the property is what is asserted.

#### Deleting a series' downloads from its row

**Requested 2026-09-17: "we should add a delete button to the entries in the
download overview, which directly deletes everything from that manga. clicking
on the rest of the row still should link to that manga to delete individual
chapters".** One action from this screen for the whole series, with the row body
still navigating — the same split the chapter rows use, kept deliberately rather
than reinvented.

- **"Directly" means one action, not "without asking".** It asks first. The
  contrast is the multi-select queue, where the confirmation was *removed*:
  queueing is reversible and costs only time, while this destroys every download
  of a series at once and the only way back is to fetch them all again. The
  sentence names the series and the count — a title alone does not distinguish
  "this deletes the chapter you just read" from "this deletes forty" — and it
  makes the same promise the single delete now makes: nothing else is touched,
  and the rest of the Trash is left alone.
- **The tap target stops where the button starts.** The row's MouseArea is
  anchored to the button's left edge rather than filling the row, which is what
  keeps the requested split true for real input; the harness asserts the
  geometry, because `clicked()` on a covered area would pass either way.
- **Every document is reported separately, and the run does not stop at the
  first failure.** They fail independently, so one flag for the batch would have
  to lie about one end of a partial run or the other. Five of seven reads as
  five of seven: a clean "done" hides two documents the user thinks are gone,
  and a flat failure hides five that really went and whose records are already
  dropped. Abandoning four deletable downloads because the first would not move
  leaves the user worse off than carrying on and saying so.
- **A document already off the tablet counts as deleted.** It is not on the
  reMarkable, which is the state the user asked for, and reporting it as a
  failure would keep a record for a document that does not exist.
- **Records go and pages are reclaimed one document at a time**, through
  `reclaimPages` and its remaining-records check, read *after* each record is
  removed. A series' records commonly share a chapter, so asking the question
  per document is what lets a chapter another record still needs survive.
- **A row whose source has been removed still offers Delete.** Its downloads are
  on the tablet and this screen is the only way left to reach them. Only
  *opening* is disabled.
- **The empty series folder is removed, and only when it has been measured
  empty.** After the last download goes, the series folder would otherwise sit
  in `Comics` empty. It is deleted through the same trash-then-`deleteEntries`
  path a document gets — folders and documents are both entries to that API, and
  the `quiredelete` probe made and removed folders exactly this way.

  **Emptiness is asked of the backend, and this correction matters.** It was
  first written here that Quire has no measured way to enumerate a folder's
  children. That is true of the **frontend** — ten QML candidates were tried on
  hardware and none of them listed a folder — and not true of Quire:
  `library.Library.List` enumerates a folder over the web interface and is what
  the sorting pass already uses, on every download, to find an existing series
  folder. So the backend lists the folder and decides; the frontend, which is
  the only side that can delete anything, is asked to act. Written as it was, it
  would have sent the next reader looking for a capability the repository
  already has.

  Every way of *not knowing* leaves the folder alone, which is the same rule as
  `checked` on the reconcile pass (§12.4): a listing that errors, a web
  interface that does not answer, a Comics folder that will not resolve, a
  folder id Quire never recorded. **Comics itself is never deleted**, guarded by
  id and tested — a user who deletes their only series should still have the
  folder every future download goes into. A partial delete never even asks: the
  documents that survived are still in the folder, and that is asserted rather
  than left to fall out of the listing.

  The tidy-up is reported only when it happened, in one sentence, and the folder
  that stayed is not mentioned at all: it is a tidy-up rather than the thing the
  user asked for, and claiming one that did not happen would be worse than
  saying nothing.
