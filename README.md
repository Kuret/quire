# Quire

A comic/manga downloader for the **reMarkable Paper Pro** that files downloads
into the stock library and hands reading off to the stock xochitl reader — so
reading position, pen annotations and cloud sync all stay where they already
live.

Quire does not ship its own reader. It downloads, assembles a PDF, puts it in
your library, and gets out of the way.

> **Status: working, lightly travelled.** Everything described here runs on the
> one Paper Pro it was developed against, on OS 3.25.1.1. It has never been
> installed on a second device, so "clean device to working Quire in one
> command" is correct by construction and unproven in practice. See
> [`docs/DEVICE-CHECKLIST.md`](docs/DEVICE-CHECKLIST.md) for what a release pass
> actually covers, and [`PLAN.md`](PLAN.md) for the design.

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
- **reMarkable OS 3.25.1.1**, with automatic updates off. Quire itself is not
  version-locked — it patches no QML and installs nothing into the system UI —
  but AppLoad's hooks are, and that is what pins the version in practice. The
  installer warns on other versions rather than refusing, and checks AppLoad's
  hooks for real.
- [xovi](https://github.com/asivery/xovi), `qt-resource-rebuilder`, and
  [AppLoad](https://github.com/asivery/rm-appload) **v0.4.2** — read the next
  section before installing any of them.
- `xovi-tripletap`, so a triple-press of the power button disables xovi. This
  is your escape hatch. **Do not skip it.**

**On your computer:**

- **Go 1.25 or newer** ([go.dev/dl](https://go.dev/dl/)). That is the only hard
  build dependency.
- `ssh`, with a key already installed on the tablet (`ssh-copy-id root@10.11.99.1`).
  The installer does not prompt for passwords.
- Qt's `rcc` is **optional**. Quire's QML bundle is committed as
  `prebuilt/resources.rcc` and the installer uses it when no Qt is present —
  installing a comic reader should not cost you a gigabyte of build tooling. If
  you do have Qt, the installer builds the bundle from `ui/` instead, and
  `make check` proves the two agree.

### ⚠️ AppLoad version — read this before installing

**AppLoad v0.5.x does not work on OS 3.25.1.1.** Its hooks reference a QML
identifier that does not exist in this build; xochitl aborts with `SIGABRT`,
which reboots the device, which wipes the volatile `/etc` drop-in — so the
symptom looks like "xovi silently never started" rather than a crash.

**Use AppLoad v0.4.2**, the newest release whose hooks all resolve on 3.25.1.1.

This matters because [remagic](https://github.com/MaximeRivest/remagic) — the
easiest way to install the xovi stack — **pins v0.5.3**, so a stock remagic
install is broken on this OS version. Install remagic, then replace
`appload.so` with v0.4.2.

`install.sh` checks this for you and refuses rather than leaving you with a
tablet that reboots on every launch. Full detail, including a script that checks
any AppLoad build against your device in seconds, is in
[`docs/DEVICE-NOTES.md`](docs/DEVICE-NOTES.md) §2.

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

That is the whole thing. It checks your machine (Go, `rcc` or the prebuilt,
`ssh`), checks the tablet (Paper Pro, OS version, xovi, `qt-resource-rebuilder`,
and that **AppLoad's hooks actually resolve**), builds, deploys, and then
verifies that AppLoad registered the app and that xovi is still injected.

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

Then open the AppLoad launcher on the tablet and tap Quire.

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

An OS update replaces the system UI, which is what AppLoad hooks into, and it
is the one event that can break all of this.

1. **Check AppLoad first**, before Quire: does the launcher still open?
   `docs/DEVICE-NOTES.md` §2 has a script that checks an `appload.so` against
   the new build's hashtab in seconds.
2. Reinstall the xovi stack if the update removed it.
3. Re-run `./install.sh`. It will warn about the unfamiliar OS version and then
   test AppLoad's hooks properly.
4. Walk [`docs/DEVICE-CHECKLIST.md`](docs/DEVICE-CHECKLIST.md) §1, §2 and §4.

Quire itself carries nothing version-specific across an update: no QML patch, no
system files, nothing under `/`. Your sources and downloads are untouched.

## Recovery

If the UI wedges:

1. **Triple-press the power button** — `xovi-tripletap` disables xovi, which
   takes AppLoad and Quire with it and gives you back a stock tablet.
2. Remove the app: `./uninstall.sh`, or by hand,
   `ssh root@10.11.99.1 'rm -rf /home/root/xovi/exthome/appload/quire'`.
3. If something appears to do *nothing* rather than break, the QML cache is
   stale — clear it and restart:
   ```sh
   ssh root@10.11.99.1 'rm -rf /home/root/.cache/remarkable/xochitl/qmlcache'
   ```

Quire installs no `.qmd` and writes nothing to the root filesystem, so there is
no patch to remove and nothing of ours can wedge the UI on its own. The failure
modes worth knowing about all belong to the xovi stack underneath it — most of
all an AppLoad version whose hooks do not resolve, which crash-loops xochitl
(see above).

---

## Development

```sh
make check      # fmt, vet, tests, and the prebuilt-resources drift check
make rmpp       # build the device bundle into output-rmpp/
make install    # build and deploy — the fast inner loop, no prerequisite checks
make pc         # build for the AppLoad PC emulator
```

`build/install-device.sh` is the deploy step on its own; `install.sh` calls it
rather than duplicating it, so there is one implementation of "put the bundle on
the tablet".

`prebuilt/resources.rcc` is a committed build artefact. `make check` fails if it
drifts from `ui/` — the check hashes every input, so it works on machines with
no Qt — and `make prebuilt-update` regenerates it.

---

## Licence

[Apache-2.0](LICENSE).

No GPL-licensed code is copied into this repository. Prior art is read for
facts — protocol shapes, file paths, which QML type does what, how a site
family structures its URLs — and implemented from scratch. Facts and techniques
are not copyrightable; expression is. See [`PLAN.md` §1.4](PLAN.md).
