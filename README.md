# Quire

A comic/manga downloader for the **reMarkable Paper Pro** that files downloads
into the stock library and hands reading off to the stock xochitl reader — so
reading position, pen annotations and cloud sync all stay where they already
live.

Quire does not ship its own reader. It downloads, assembles a PDF, puts it in
your library, and gets out of the way.

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
  **Annex**, the host that puts Quire in xochitl's sidebar. Annex is built in
  this project but installs itself: run its own `deploy.sh` **before** Quire's
  installer. `install.sh` checks for both halves of it — the tools at
  `/home/root/annex/tools/` and the QML patch at
  `/home/root/xovi/exthome/qt-resource-rebuilder/annex.qmd` — and stops with an
  explanation if either is missing.
- `xovi-tripletap`, so a triple-press of the power button disables xovi. This
  is your escape hatch. **Do not skip it.**

**On your computer:**

- **Go 1.25 or newer** ([go.dev/dl](https://go.dev/dl/)). That is the *only*
  build dependency — there is nothing else to install.
- `ssh`, with a key already installed on the tablet (`ssh-copy-id root@10.11.99.1`).
  The installer does not prompt for passwords.

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

From a clone of this repository, with the tablet plugged in over USB:

```sh
./install.sh
```

That is the whole thing. It checks your machine (Go, `ssh`), checks the tablet
(Paper Pro, OS version, xovi, `qt-resource-rebuilder`, and that Annex is
installed — both its tools and its QML patch), builds, deploys, and then
verifies the app directory is complete, that **the backend actually came up**,
and that xovi is still injected.

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

Useful flags:

```sh
./install.sh --host 192.168.1.42   # over wifi instead of USB
./install.sh --verify              # re-run every check, build and deploy nothing
```

xochitl builds the Annex sidebar when it starts, so if Quire is not in it yet:

```sh
ssh root@10.11.99.1 'systemctl restart xochitl'
```

Then open the Annex sidebar on the tablet and tap Quire.

## First run — two things Quire cannot do for you

`install.sh` checks both of these and tells you if either is outstanding. Quire
also detects them at runtime and explains them in plain language rather than
failing with an error code. But they are easier done now:

**1. Turn on the USB web interface.** It is **off** on a factory device, and
Quire saves downloaded comics through it — nothing can reach your library until
it is on.

> On the tablet: **Settings → Storage → "USB web interface" → on**, then restart
> the tablet.

Quire deliberately does not flip this setting itself: it only takes effect when
xochitl restarts, and a restart throws away whatever you were reading — the very
thing Quire exists to protect.

**2. Create a folder called `Comics`, by hand, in My Files.** Spelled exactly
that way.

This one genuinely cannot be automated: xochitl exposes **no way to create a
folder** remotely. The entire interface is list, download and upload. Without
the folder, Quire files downloads at the top of My Files and tells you it has
done so — usable, but not what you want.

## Using it

- **Add a source** by pasting a URL. Quire probes it, works out which site
  family it belongs to, and tells you plainly if it cannot — including when the
  site is behind a challenge it will not try to pass.
- **Download a volume**; it lands in `My Files → Comics` with a real thumbnail,
  no xochitl restart.
- **Tap Read**; the stock reader opens it. Your page position, highlights and
  pen annotations belong to xochitl from then on. Quire stores no reading
  position, anywhere.
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

## Uninstall

```sh
./uninstall.sh
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
4. Re-run `./install.sh`. It will warn about the unfamiliar OS version and then
   check Annex, the app directory and the backend endpoint.
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

`build/install-device.sh` is the deploy step on its own; `install.sh` calls it
rather than duplicating it, so there is one implementation of "put the bundle on
the tablet". It also runs Annex's own `annex-index` and
`annex-service enable quire` afterwards, because indexing an app and starting
its service belong to the host, not to the app.

There is no resource bundle to build any more: `ui/` ships as loose `.qml`
files and Annex loads them from disk by path.

---

## Licence

[Apache-2.0](LICENSE).

No GPL-licensed code is copied into this repository. Prior art is read for
facts — protocol shapes, file paths, which QML type does what, how a site
family structures its URLs — and implemented from scratch. Facts and techniques
are not copyrightable; expression is. See [`PLAN.md` §1.4](PLAN.md).
