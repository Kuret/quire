# Quire

```sh
curl -fsSL https://raw.githubusercontent.com/Kuret/quire/main/install.sh | sh
```

Before you run it, here is what it does:

- **It runs on your computer**, not on the tablet, and drives the tablet over
  ssh — `10.11.99.1` on the USB cable by default, or `... | sh -s -- 192.168.1.42`
  over wifi. It asks for the device's ssh password once, if there is no key.
- **Quire needs [Annex](https://github.com/Kuret/annex)**, the framework that
  puts it in xochitl's sidebar and runs its backend as a service. If Annex is
  not there, the installer explains what it is and **asks** whether to run
  Annex's own installer. Say no and it stops, having changed nothing.
- It installs one directory, `/home/root/annex/apps/quire`, and then asks
  Annex's own tools to index it and start its backend.
- The backend is a 26 MB `linux/arm64` binary that is not in this repository:
  from a checkout with Go 1.25+ it is **built**, otherwise it is **downloaded**
  from the latest release. If neither is possible the installer stops and says
  so, rather than installing an app that cannot start.
- **It never touches `/home/root/.local/share/quire/state/`** — the sources you
  configured, your library and the cover cache. Reinstalling and upgrading
  cannot eat them. `install.sh --uninstall` asks before removing them, and
  defaults to keeping them.
- It modifies no system files of its own, and `install.sh --help` lists all of
  the above without installing anything.

A comic/manga downloader for the **reMarkable Paper Pro**. By default a
download is saved in Quire's own storage and read in Quire's own reader — no
PDF, no upload, no folder in your library. **Send to library** stays available
as an explicit action for when you want the stock xochitl reader instead, with
its reading position, pen annotations and cloud sync — the same PDF-and-upload
behaviour Quire always had. Books (from sources like Shelfmark) are unchanged:
they always go to the library, because Quire's own reader is image-only and a
book is not images.

> **Status: mid-port, and honest about it.** Quire was developed against one
> Paper Pro, originally hosted by a third-party launcher called AppLoad. It now
> targets [Annex](#requirements) instead, on OS **3.28.0.172**.
>
> What is proven on hardware: Annex itself — its host, its sidebar entry, and
> loading an app's QML from disk. What is **not**: Quire's own port to Annex.
> That builds and passes `make check` on the development host, and it has not
> yet been run on the tablet. Nothing below has been used on a device in its
> current form. Treat the install path as written-down intent until someone
> walks [`docs/DEVICE-CHECKLIST.md`](docs/DEVICE-CHECKLIST.md); see
> [`PLAN.md`](PLAN.md) for the design.

---

## You choose the sources. Quire ships none.

**Quire contains no source URLs, no bundled index, and no default catalogue.**
The shipped configuration is empty. You add each site yourself by pasting a URL,
and Quire works out how to read it.

That is deliberate. Much of the material on comic aggregator sites is
copyrighted, and whether accessing any given site is permissible depends on the
site and on your jurisdiction. **That determination is yours.** You are
responsible for the legality of the sources you configure and for how you use
what you download. Quire does not make that call for you, and does not nudge it
by shipping a list.

Quire also will not help you past a site that says no. Browser challenges, bot
detection and anti-scraping measures are **detected and refused**, never
circumvented — there is no bypass in this codebase and none will be added. A
challenge is a site operator declining, and Quire takes the answer. See
[`PLAN.md` §7.6](PLAN.md).

---

## Requirements

**On the tablet:**

- A reMarkable Paper Pro (`ferrari`) in **developer mode**.
  Enabling developer mode **factory-resets the device** — back up first.
- **reMarkable OS 3.28.0.172**, with automatic updates off. Quire itself is not
  version-locked — it patches no QML and installs nothing into the system UI.
  The OS coupling lives one layer down, in Annex. The installer warns on other
  versions rather than refusing.
- [xovi](https://github.com/asivery/xovi), `qt-resource-rebuilder`, and
  **Annex**, the host that puts Quire in xochitl's sidebar. You do not have to
  install these first: `install.sh` checks for Annex — the tools at
  `/home/root/annex/tools/` and the QML patch at
  `/home/root/xovi/exthome/qt-resource-rebuilder/annex.qmd` — and, if it is
  missing, **offers** to run [Annex's own installer](https://github.com/Kuret/annex),
  which installs xovi and `qt-resource-rebuilder` along the way. Decline and
  Quire's installer stops without changing anything.
- `xovi-tripletap`, so a triple-press of the power button disables xovi. This
  is your escape hatch. **Do not skip it.**

**On your computer:**

- A POSIX shell, `curl`, `ssh` and `tar`. macOS and Linux both have all four.
  No ssh key is required: the installer opens **one** multiplexed connection,
  so the tablet's password is typed once for the whole install. A key, if you
  have one on the device, makes it unattended.
- **Go 1.25 or newer** ([go.dev/dl](https://go.dev/dl/)) — *only* if you are
  installing from a clone and want the backend built here. Installing from the
  published release needs no toolchain at all, and Go is the only build
  dependency there has ever been.

Qt is **not** needed. Annex reads an app's QML as loose files from disk, by
path, so there is no resource bundle to compile and no `rcc` to hunt for.
Installing a comic reader should not cost you a gigabyte of build tooling.

### What can still go wrong, and what no longer can

An older version of this README carried a loud warning here: certain AppLoad
builds referenced a *hashed* QML identifier, and on an OS build where that hash
did not exist, qmldiff panicked and took xochitl down with it — a boot loop you
had to dig yourself out of.

**That hazard is gone, by construction.** Annex deliberately uses **zero hashed
identifiers**, so there is no panic path left to hit. Installing this cannot
cost you a booting tablet.

The remaining risk is smaller, but it is real. Annex patches xochitl's QML by
matching **element names**, and an OS update can move or rename those. When a
qmldiff selector matches nothing, it logs an error and returns the original
file unchanged — so the failure mode is **"Annex does not appear"**, not "the
tablet will not boot". xochitl runs exactly as it did before, minus the sidebar
entry.

If the UI does misbehave, `/home/root/annex/tools/annex-disable` removes
Annex's injection and restarts xochitl, which gets you back to a stock screen
without touching xovi or your downloads.

### Also worth knowing

remagic's boot-autostart script installs its systemd unit by **copying** it into
`multi-user.target.wants/`. systemd ignores non-symlink entries there, so xovi
never starts at boot. It has to be a symlink — and because `/etc` is a tmpfs
overlay on a read-only rootfs, `systemctl enable` alone does not survive a
reboot. See [`docs/DEVICE-NOTES.md`](docs/DEVICE-NOTES.md) §3.

---

## Install

With the tablet plugged in over USB, from nothing:

```sh
curl -fsSL https://raw.githubusercontent.com/Kuret/quire/main/install.sh | sh
```

or, from a clone of this repository:

```sh
./install.sh
```

That is the whole thing. It reaches the tablet over ssh, makes sure Annex is
there — offering to install it if it is not — gets the backend binary (built
from the clone if you have Go, downloaded from the latest release otherwise),
installs the app, asks Annex to index it and start its service, and then
verifies the app directory is complete, that **the backend actually came up**,
and that Quire is in the index the sidebar is built from.

The backend check is the one worth knowing about: `quired` writes
`/home/root/annex/run/quire.json` once it has bound its port, and Annex's
frontend reads the port out of that file. A service that is enabled but
crash-looping looks perfectly healthy from the app directory alone, and cannot
fake that file — so the installer waits for it, and tells you which journal to
read if it never appears.

It is idempotent — run it as often as you like — and it never touches
`/home/root/.local/share/quire`, which is where your configured sources,
library records, cover cache and part-finished downloads live. Reinstalling
cannot eat them. That directory sits outside the app directory for exactly this
reason.

Useful arguments:

```sh
./install.sh 192.168.1.42     # over wifi instead of USB (or $QUIRE_DEVICE)
./install.sh --uninstall      # remove it again; asks about your state
./install.sh --help           # what it does to your device, in full
QUIRE_RELEASE=v0.2.0 ./install.sh   # pin the published build to install
```

xochitl builds the Annex sidebar when it starts, so if Quire is not in it yet:

```sh
ssh root@10.11.99.1 'systemctl restart xochitl'
```

Then open the Annex sidebar on the tablet and tap Quire.

## First run — two things Quire cannot do for you

Neither of these is needed for an ordinary download any more — comics and
manga now save straight into Quire's own storage, and only **Send to
library** touches the reMarkable's library at all. Both still matter the
moment you use it, or download a book: `install.sh` checks both and tells you
if either is outstanding, and Quire also detects them at runtime and explains
them in plain language rather than failing with an error code.

**1. Turn on the USB web interface.** It is **off** on a factory device, and
it is what "Send to library" and book downloads reach your library through —
nothing can reach it until this is on.

> On the tablet: **Settings → Storage → "USB web interface" → on**, then restart
> the tablet.

Quire deliberately does not flip this setting itself: it only takes effect when
xochitl restarts, and a restart throws away whatever you were reading — the very
thing Quire exists to protect.

**2. Create a folder called `Comics`, by hand, in My Files.** Spelled exactly
that way. Needed only for **Send to library** and for books — an ordinary
comic download never touches this folder.

This one genuinely cannot be automated: xochitl exposes **no way to create a
folder** remotely. The entire interface is list, download and upload. Without
the folder, "Send to library" files a document at the top of My Files and
tells you it has done so — usable, but not what you want.

## Using it

- **Add a source** by pasting a URL. Quire probes it, works out which site
  family it belongs to, and tells you plainly if it cannot — including when the
  site is behind a challenge it will not try to pass.
- **Download a chapter**; it is saved in Quire's own storage — no PDF, no
  upload, no folder — and read in Quire's own reader from then on, opening
  back where you left off. Quire remembers that position; nothing about a
  saved chapter touches xochitl.
- **Send to library**, from the reader or the chapter list, when you want the
  stock xochitl reader instead: it becomes an ordinary PDF in
  `My Files → Comics`, with a real thumbnail, no xochitl restart. Your page
  position, highlights and pen annotations belong to xochitl from then on —
  the saved copy in Quire, if there is one, is untouched and stays independent
  of it.
- **Something looked wrong?** Settings has a log viewer — the same log the
  backend writes, readable on the tablet, no SSH.

### Annex's tools, when SSH is the answer

Quire's backend is a systemd service that Annex owns, so when the app itself
cannot tell you what is wrong, Annex can:

```sh
/home/root/annex/tools/annex-service status      # service state and port, per app
/home/root/annex/tools/annex-service log quire   # follow the backend's journal
/home/root/annex/tools/annex-index               # rebuild the app index (sidebar)
/home/root/annex/tools/annex-disable             # remove Annex's injection, restart xochitl
```

`annex-disable` is the one to reach for when the screen is not helping.

## Books, and sources on your own network

Quire reads books as well as comics. Point it at a Shelfmark instance you run
yourself and it recognises what it is talking to — not by the URL, but by asking
`/api/config`, which only the real application answers.

Books are not comics, and Quire does not pretend otherwise:

- A book is **one entry**, not a series of chapters and volumes.
- Quire lists only the formats **the reMarkable opens natively: epub and pdf.**
  A Shelfmark instance will happily offer mobi, azw3, fb2, djvu, cbz and cbr.
  Those are filtered out, because a file this device cannot open is not a
  download, it is a disappointment with a progress bar.
- The image-fetching and pdf-assembly pipeline is **skipped entirely.** A book
  arrives as a finished file; there is nothing to assemble.
- **It is slow, and that is normal.** A release search asks every indexer your
  instance is configured with, in turn — measured at roughly 36 seconds against
  a live instance. Adding a source can take a couple of minutes. Quire waits.

### Reaching a service on your own network

Quire refuses to fetch private addresses by default, so a self-hosted source
prompts a question during the probe rather than silently working. Answer it and
the exemption applies **to that source only**, and only to the host you
confirmed — a cover URL or a redirect pointing anywhere else on your network is
still refused.

If the address does not resolve at all, Quire asks the same question, because a
name that exists only inside a mesh VPN looks exactly like a typo to a resolver.

### If your mesh VPN runs in userspace mode

This is the part that will cost you an afternoon if nobody tells you, so:
Tailscale, Headscale, NetBird and friends can run **without a TUN interface**,
and on the reMarkable that is the usual arrangement. In that mode:

- there is **no `tailscale0` interface**, so the kernel has no route to the mesh;
- the system resolver knows nothing of `.ts.net` or any other mesh name, so the
  name genuinely does not resolve;
- **using the IP instead does not help** — the kernel hands it to your ordinary
  default gateway, which drops it. That fails *worse* than the name: a timeout
  instead of an instant error.

The tunnel exists only inside the VPN daemon's own userspace network stack, and
the only door into it is an outbound proxy. For tailscaled, that means starting
it with something like:

```
FLAGS="--tun userspace-networking --outbound-http-proxy-listen=localhost:1055"
```

then putting `http://localhost:1055` in the **proxy field** Quire offers when it
asks about the source. The daemon resolves the name and routes the request on
its own side; Quire never resolves it locally at all.

**Quire does not configure any of this, deliberately.** We cannot assume how you
reach your own services, so unlike `qt-resource-rebuilder` — which is always
required and is installed for you — your VPN is yours to set up. The port number
is arbitrary; use whatever your daemon is listening on.

### A trap worth knowing about

The proxy flags above are usually a **local edit** to the VPN daemon's
environment file. On a vellum-managed install that is
`/home/root/.vellum/etc/default/tailscaled`, and vellum ships it with
`--tun userspace-networking` and nothing else.

**A package update can overwrite that file and silently revert your proxy
flags.** Nothing warns you. The symptom is every book source failing at once
with `no such host`, which looks like a Quire fault and is not one. Check the
env file first, and keep a copy of your edit.

## Uninstall

```sh
./install.sh --uninstall      # or ./uninstall.sh, from a clone
```

Removes the app. **Asks** before removing your sources and downloads, and
defaults to keeping them; `--purge` removes those too. Comics already in your
library stay — they are ordinary reMarkable documents now, and nothing about
them depends on Quire.

---

## After a reMarkable OS update

An OS update replaces the system UI, which is the QML Annex patches, and it is
the one event that can break all of this.

1. Reinstall the xovi stack if the update removed it — an update usually does.
2. **Check Annex before Quire**: after xochitl has restarted, does the Annex
   entry still appear in the sidebar? That single observation answers whether
   Annex's selectors still match the new build's QML. If it is missing, Annex
   needs re-deploying or its selectors need updating against the new UI; Quire
   is not the problem and reinstalling it will not help.
3. If the UI is visibly wrong rather than merely missing the entry, run
   `/home/root/annex/tools/annex-disable` to pull the injection and restart
   xochitl, then sort it out from a stock screen.
4. Re-run `./install.sh`. It checks Annex, reinstalls the app, and verifies the
   app directory, the backend endpoint and the sidebar index. (The OS-version
   warning belongs to Annex's installer, which is where the version coupling
   is.)
5. Walk [`docs/DEVICE-CHECKLIST.md`](docs/DEVICE-CHECKLIST.md) §1, §2 and §4.

Note that the backend is a systemd service (`annex-app@quire`) and is entirely
independent of xochitl: it will happily be running, and
`/home/root/annex/tools/annex-service status` will happily say so, while the
sidebar entry is nowhere to be seen. The two questions are separate — "is the
backend up" is not evidence that the UI works, and a broken UI is no reason to
go looking at the service.

Quire itself carries nothing version-specific across an update: no QML patch, no
system files, nothing under `/`. Your sources and downloads are untouched.

## Recovery

If the UI wedges:

1. **Triple-press the power button** — `xovi-tripletap` disables xovi, which
   takes Annex and Quire with it and gives you back a stock tablet.
2. Or, less drastically, `ssh root@10.11.99.1
   '/home/root/annex/tools/annex-disable'` — that removes Annex's injection and
   restarts xochitl, leaving the rest of the xovi stack alone.
3. Remove the app: `./uninstall.sh`, or by hand,
   `ssh root@10.11.99.1 'systemctl disable --now annex-app@quire; rm -rf /home/root/annex/apps/quire'`.
4. If something appears to do *nothing* rather than break, the QML cache is
   stale — clear it and restart:
   ```sh
   ssh root@10.11.99.1 'rm -rf /home/root/.cache/remarkable/xochitl/qmlcache'
   ```

Quire installs no `.qmd` and writes nothing to the root filesystem, so there is
no patch of ours to remove and nothing of ours can wedge the UI on its own. The
failure modes worth knowing about belong to the layers underneath: xovi not
being injected at boot, or an Annex selector that no longer matches the running
OS build (see above).

---

## Development

```sh
make check      # fmt, vet, tests, and the QML check
make rmpp       # build the device bundle into output-rmpp/
make install    # build and deploy — the fast inner loop, no prerequisite checks
make pc         # build a host bundle into output/ for development
```

`make qml` lints `ui/` and instantiates every screen offscreen; it skips itself
on a machine with no Qt 6, so `make check` is green with Go alone.

`build/install-device.sh` is the developer inner loop: it assumes a working
device, an existing Annex and a built `output-rmpp/`, and only pushes the tree
(`make install` is the same thing). `install.sh` is the *user* path — ssh
multiplexing, the Annex prerequisite, fetching a published build when there is
no toolchain — and it does not call it. Both end by running Annex's own
`annex-index` and `annex-service`, because indexing an app and starting its
service belong to the host, not to the app.

There is no resource bundle to build any more: `ui/` ships as loose `.qml`
files and Annex loads them from disk by path.

---

## Licence

[Apache-2.0](LICENSE).

No GPL-licensed code is copied into this repository. Prior art is read for
facts — protocol shapes, file paths, which QML type does what, how a site
family structures its URLs — and implemented from scratch. Facts and techniques
are not copyrightable; expression is. See [`PLAN.md` §1.4](PLAN.md).
