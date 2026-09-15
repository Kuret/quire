# DEVICE-CHECKLIST.md

The manual pre-release pass (PLAN §9). Everything here needs a human with a
tablet in hand: these are the checks no test suite on the host can make.

Work top to bottom. Record the date and the OS build (`cat /etc/version`) at the
top of each run. A check that is skipped is written down as skipped, not left
blank — an unticked box and an untested box look identical a week later.

> **The M8 acceptance test has never been run end to end.** It needs a *second*,
> clean Paper Pro; there is one tablet and it is in daily use. §1 below is the
> documented path, and every individual step in it has been executed against the
> live device — but "a device that has never had Quire on it goes from zero to
> working via one command" is unverified. Say so in release notes until someone
> runs it.

---

## 0. Before you start

- [ ] Full backup: `/home/root/.local/share/remarkable/xochitl/` and
      `/home/root/.config/remarkable/xochitl.conf`.
- [ ] `xovi-tripletap` installed and *tested* — triple-press the power button,
      confirm xovi goes away, triple-press again. This is the escape hatch. It is
      only an escape hatch if you have used it once.
- [ ] An SSH session open and confirmed working, and left open, for the whole
      pass.
- [ ] `cat /etc/version` matches `docs/DEVICE-NOTES.md` §1 verbatim.
- [ ] Automatic OS updates are off.

## 1. Clean install

- [ ] xovi, `qt-resource-rebuilder` and AppLoad present on the device.
      **AppLoad must be v0.4.2, not v0.5.x.** v0.5.x aborts xochitl on 3.25.1.1
      and the device reboots, which hides the evidence — `docs/DEVICE-NOTES.md`
      §2. `remagic` pins v0.5.3, so a stock remagic bootstrap needs `appload.so`
      replaced afterwards.
- [ ] The AppLoad launcher itself opens, before Quire is involved at all.
- [ ] On the host: a clone of this repository, and Go 1.25 or newer. Qt is
      optional (see §8).
- [ ] `./install.sh` — one command, from the clone. It should report every check
      passing and finish with the first-run steps.
- [ ] Run `./install.sh` a second time. Same result, no errors: it is idempotent.
- [ ] The Quire tile appears in the AppLoad launcher.
- [ ] `./install.sh --verify` reports `appload registered Quire`.
- [ ] Quire opens and does not crash.

### Refusals worth provoking once per release

- [ ] Point `./install.sh --host` at an unreachable address → a clear
      explanation, not an ssh error.
- [ ] With AppLoad v0.5.x installed (on a device you are willing to break)
      `./install.sh` refuses and explains why. If you will not risk that, at
      minimum read the refusal text and confirm it still names v0.4.2.

## 2. First run — the two steps a user cannot guess

- [ ] `WebInterfaceEnabled` is **off** on a factory device. Settings → Storage →
      "USB web interface" on, then restart the tablet.
- [ ] With it still off, `./install.sh` says so in step 1 of its first-run
      output, and Quire itself explains the problem in words rather than failing
      with a connection error. (Worth testing deliberately: set it back to
      false, restart, attempt a download, read what Quire says.)
- [ ] A folder named **`Comics`** exists in My Files, created by hand on the
      tablet. xochitl exposes no folder-create API — the entire web surface is
      `/documents/`, `/download/` and `/upload`.
- [ ] With `Comics` absent, `./install.sh` says so, and a download lands at the
      top of My Files with Quire explaining why, instead of failing.

## 3. Milestone acceptance tests

Each is PLAN §6's own wording, condensed. Run them in order; a failure early
makes the later ones meaningless.

**M1 — backend liveness.**

- [ ] The tile opens and the UI shows real data from the Go process, not a
      placeholder.

**M2/M3 — sources and the probe.**

- [ ] Paste a URL for a site of a supported theme → added and browsable, with no
      manual configuration.
- [ ] Paste a challenge-protected URL → a clear refusal in plain language, and
      nothing added to the source list.
- [ ] Paste an unsupported shape → "unrecognised", naming what was tried.
- [ ] Rename a source; the new name survives leaving and re-entering the app.

**M4 — download and assembly.**

- [ ] A full volume downloads and assembles.
- [ ] Pull the resulting PDF to a host and open it: page count, page order and
      page dimensions correct, file size sane for the page count.

**M5 — library integration.**

- [ ] The volume appears in `My Files → Comics` with the right name and a real
      thumbnail, without restarting xochitl.
- [ ] It opens in the stock reader.

**M6 — reader handoff.**

- [ ] Tap "Read" → the stock reader opens that volume.
- [ ] Read a few pages, back out, reopen from the stock library → resumes at the
      right page.
- [ ] Annotate with the pen → the annotation persists, and syncs.
- [ ] Quire stores no page number anywhere (`grep` the state directory if in
      doubt).

**M7 — resilience.**

- [ ] Kill wifi mid-volume, sleep the tablet, wake it, resume: no corrupt PDF,
      no orphaned temp files under `/home/root/.local/share/quire/downloads`, no
      duplicate library entry.
- [ ] Force-close Quire mid-download and reopen it: the quiet notice appears and
      the app is usable.
- [ ] Point a source at a URL that starts challenging → the next sync reports it
      accurately.
- [ ] Settings → the in-app log viewer shows recent entries, including the ones
      from the failures just provoked.

## 4. Reboot and persistence

- [ ] `reboot`. Wait for the device to come back on its own.
- [ ] xovi is still injected:
      `tr '\0' '\n' < /proc/$(pidof xochitl)/environ | grep LD_PRELOAD`
      — empty output means xovi did not start. Common cause: the boot unit in
      `multi-user.target.wants/` is a *copy* rather than a symlink, and `/etc` is
      a tmpfs overlay so `systemctl enable` alone does not survive
      (`docs/DEVICE-NOTES.md` §3).
- [ ] The Quire tile is still in the launcher.
- [ ] Configured sources, downloaded volumes and the cover cache are all still
      there.
- [ ] Downloaded volumes are still in `Comics` and still open in the stock
      reader.
- [ ] `./install.sh --verify` is clean after the reboot.

## 5. Redeploy and uninstall

- [ ] Redeploy over the running install (`./install.sh`, or `make install` for
      the developer loop). **Configured sources must survive.** State lives at
      `/home/root/.local/share/quire`, deliberately outside the app directory
      that the installer replaces wholesale — this check exists because an
      earlier layout ate a user's sources on a routine redeploy.
- [ ] Nothing user-visible has appeared under
      `/home/root/xovi/exthome/appload/quire`: it should contain only
      `manifest.json`, `icon.png`, `resources.rcc` and `backend/entry`.
- [ ] `find /home/root/xovi/exthome/appload/quire -name '._*'` is empty (macOS
      AppleDouble files; `COPYFILE_DISABLE=1` prevents them).
- [ ] `./uninstall.sh` removes the app, **asks** about state, and defaults to
      keeping it. Answer no; confirm `/home/root/.local/share/quire` is intact.
- [ ] Reinstall, and confirm the sources are all still configured.
- [ ] `./uninstall.sh --purge` on a device you are finished with: state gone,
      comics already in the library untouched.

## 6. After a reMarkable OS update

Only relevant if an update slipped through, or deliberately.

- [ ] Reinstall Quire: `./install.sh`. Read its OS warning — Quire ships no QML
      patch, so nothing of ours is version-locked, but AppLoad's hooks are, and
      the installer checks them.
- [ ] Re-check AppLoad's own hooks against the new build first;
      `docs/DEVICE-NOTES.md` §2 has the script.
- [ ] Re-run §1, §2 and §4 in full. An OS update invalidates every AppLoad hook
      assumption.

## 7. Space and safety, every pass

- [ ] `df -h /` — the root filesystem has ~47 MB free. **Never write to `/`.**
      Everything Quire owns lives on `/home`.
- [ ] `df -h /home` — enough headroom for the volumes you are about to download.
- [ ] `du -sh /home/root/.local/share/quire` — the cover cache and downloads are
      the things that grow.

## 8. Host-side, on a machine that is not the developer's

- [ ] A clone plus **Go only, no Qt** installs successfully: `install.sh` falls
      back to `prebuilt/resources.rcc` and says so.
- [ ] A machine with Qt present builds `resources.rcc` from `ui/` and produces
      byte-identical output (`build/prebuilt.sh check`).
- [ ] With no Go at all, `install.sh` names Go as the missing piece and links to
      the download page, rather than failing at a compiler error.
