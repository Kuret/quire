# Quire

A comic/manga downloader for the **reMarkable Paper Pro** that files downloads
into the stock library and hands reading off to the stock xochitl reader — so
reading position, pen annotations and cloud sync all stay where they already
live.

Quire does not ship its own reader. It downloads, assembles a PDF, puts it in
your library, and gets out of the way.

> **Status: in development.** See [`PLAN.md`](PLAN.md) for the design and the
> milestone breakdown. Nothing here is released yet.

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

- A reMarkable Paper Pro (`ferrari`) in **developer mode**.
  Enabling developer mode **factory-resets the device** — back up first.
- **reMarkable OS 3.25.1.1** specifically, with automatic updates off.
  Quire's QML patch is written against this one version and the installer
  refuses to run on anything else. This is a deliberate choice, not laziness —
  see [`PLAN.md` §7.8](PLAN.md).
- [xovi](https://github.com/asivery/xovi), `qt-resource-rebuilder`, and
  [AppLoad](https://github.com/asivery/rmpp-appload).
- `xovi-tripletap`, so a triple-press of the power button disables xovi. This
  is your escape hatch. **Do not skip it.**

### ⚠️ AppLoad version — read this before installing

**AppLoad v0.5.3 does not work on OS 3.25.1.1.** Its hooks reference a QML
identifier that does not exist in this build; xochitl aborts with `SIGABRT`,
which reboots the device, which wipes the volatile `/etc` drop-in — so the
symptom looks like "xovi silently never started" rather than a crash.

**Use AppLoad v0.4.2**, the newest release whose hooks all resolve on 3.25.1.1.

This matters because [remagic](https://github.com/MaximeRivest/remagic) — the
easiest way to install the xovi stack — **pins v0.5.3**, so a stock remagic
install is broken on this OS version. Install remagic, then replace
`appload.so` with v0.4.2.

Full detail, including a script that checks any AppLoad build against your
device in seconds, is in
[`docs/DEVICE-NOTES.md`](docs/DEVICE-NOTES.md) §2.

### Also worth knowing

remagic's boot-autostart script installs its systemd unit by **copying** it into
`multi-user.target.wants/`. systemd ignores non-symlink entries there, so xovi
never starts at boot. It has to be a symlink — and because `/etc` is a tmpfs
overlay on a read-only rootfs, `systemctl enable` alone does not survive a
reboot. See [`docs/DEVICE-NOTES.md`](docs/DEVICE-NOTES.md) §3.

---

## Recovery

If a patch wedges the UI:

1. **Triple-press the power button** — `xovi-tripletap` disables xovi.
2. Remove the patch:
   ```sh
   ssh root@10.11.99.1 'rm -f /home/root/xovi/exthome/qt-resource-rebuilder/quireOpen.qmd \
       && systemctl restart xochitl'
   ```
3. If a patch appears to do *nothing* rather than break things, the QML cache is
   stale — clear it first:
   ```sh
   ssh root@10.11.99.1 'rm -rf /home/root/.cache/remarkable/xochitl/qmlcache'
   ```

**Never install a `.qmd` without an SSH session already open and confirmed
working.**

---

## Licence

[Apache-2.0](LICENSE).

No GPL-licensed code is copied into this repository. Prior art is read for
facts — protocol shapes, file paths, which QML type does what, how a site
family structures its URLs — and implemented from scratch. Facts and techniques
are not copyrightable; expression is. See [`PLAN.md` §1.4](PLAN.md).
