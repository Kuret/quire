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
  **Known limitation: webtoon strips** (aspect far from 3:4) pad down to a small
  centred image. Slicing tall strips into panel-height pages is a real feature,
  deliberately not invented in M4 — revisit if a vertical-scroll source matters.
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
- **One PDF per volume, not per chapter.** Chapter PDFs clutter the library and
  make reading position meaningless. Quire maps chapters → (volume PDF, page
  offset). Where a source has no volume structure, group by a configurable
  chapter count (default 10).
  **Grouping precedence (settled after §7.2's ordering contract):**
  1. **`Chapter.Volume`**, the source's own label, whenever it has one. This is
     what the plan meant by "volume"; runs-of-N is the *fallback* it was always
     described as, not the primary mechanism.
  2. Runs of N (default 10) only when the source publishes no volume labels.
  3. **A hard byte budget, which overrides both.** Measured on the device
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

**Resolved design — flat, not nested:**
- **`Comics` is a one-time user setup step**, alongside `WebInterfaceEnabled`.
  Detect its absence and say so in plain language, with the remedy.
- **Upload flat into `Comics`**, carrying the series in the document name
  (`<Series> — Vol N`). Per-series subfolders are *optional*: if the user has
  made one, use it; never require it. Tidy nesting we cannot create is worth
  less than a correct file the user can find.
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
query), one series detail, one chapter list, and one page-image extraction.
**Extract pages from the *newest* chapter, not the oldest.** A site that has
changed its reader markup leaves its back catalogue exactly as it was, so
probing the earliest chapter can report a capability the user will not actually
have on anything they read. The newest chapter is the one that reflects the
site as it is now.
Require all four to produce plausible non-empty results. This is what separates
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

Concretely, **do not**: rotate or spoof `User-Agent` strings, impersonate
browser TLS fingerprints, integrate a CAPTCHA-solving service, proxy through a
third-party scraping API, or replay clearance cookies harvested elsewhere. If a
future contributor proposes any of these, the answer is no, and this section is
why.

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
