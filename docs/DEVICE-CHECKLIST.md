# DEVICE-CHECKLIST.md

The manual pre-release pass (PLAN §9). Everything here needs a human with a
tablet in hand: these are the checks no test suite on the host can make.

Work top to bottom. Record the date and the OS build (`cat /etc/version`) at the
top of each run. A check that is skipped is written down as skipped, not left
blank — an unticked box and an untested box look identical a week later.

> **Quire on Annex now runs on hardware, but this file has not been worked
> through as a pass.** As of 2026-09-20, on OS 3.28.0.172: installing,
> browsing, searching across sources, downloading, reading, deleting, the
> watch list, the downloaded overview, the layout toggle, the hold menus and
> both installers have all been exercised on the live device. What has *not*
> happened is someone going down this list ticking boxes, so the individual
> assertions below — particularly the negative ones, and anything about a
> cold boot or a second device — remain documented intent.
>
> Treat an unticked box as unknown rather than broken, and write down what you
> find.
>
> **The M8 acceptance test has never been run end to end**, separately from
> that. It needs a *second*, clean Paper Pro; there is one tablet and it is in
> daily use. So "a device that has never had Quire on it goes from zero to
> working via one command" stays unverified. Say so in release notes until
> someone runs it.

---

## 0. Before you start

- [ ] Full backup: `/home/root/.local/share/remarkable/xochitl/` and
      `/home/root/.config/remarkable/xochitl.conf`.
- [ ] `xovi-tripletap` installed and *tested* — triple-press the power button,
      confirm xovi goes away, triple-press again. This is the escape hatch. It is
      only an escape hatch if you have used it once.
- [ ] An SSH session open and confirmed working, and left open, for the whole
      pass.
- [ ] `. /etc/os-release; echo $IMG_VERSION` reports **3.28.0.172**, the only
      build Annex has been proven against. Record `cat /etc/version` alongside
      it. (`docs/DEVICE-NOTES.md` §1 still records the pre-port 3.25.1.1
      device; do not gate on it.)
- [ ] Automatic OS updates are off.

## 1. Clean install

- [ ] xovi, `qt-resource-rebuilder` and Annex present on the device: the tools
      at `/home/root/annex/tools/` and the patch at
      `/home/root/xovi/exthome/qt-resource-rebuilder/annex.qmd`. Annex installs
      itself via its own `deploy.sh`, before Quire.
- [ ] The Annex sidebar entry appears in xochitl, before Quire is involved at
      all. A missing entry means Annex's selectors do not match this OS build;
      stop here, because nothing Quire does can fix it.
- [ ] On the host: a clone of this repository, and Go 1.25 or newer. Nothing
      else (see §8).
- [ ] `./install.sh` — one command, from the clone. It should report every check
      passing and finish with the first-run steps.
- [ ] Run `./install.sh` a second time. Same result, no errors: it is idempotent.
- [ ] Quire appears in the Annex sidebar, after `systemctl restart xochitl`.
- [ ] `./install.sh` re-run reports every check green (there is no --verify;
      the install ends in the same verification stage)
      `backend published /home/root/annex/run/quire.json`.
- [ ] Quire opens and does not crash.

### Refusals worth provoking once per release

- [ ] Point `./install.sh 192.0.2.1` (unreachable) at it → a clear
      explanation, not an ssh error.
- [ ] Move `annex.qmd` aside → `./install.sh` refuses, names the missing file,
      and points at Annex's `deploy.sh`. Put it back afterwards.
- [ ] `systemctl stop annex-app@quire; rm -f /home/root/annex/run/quire.json`,
      then re-run `./install.sh` → it fails on the endpoint file and prints
      the `annex-service log quire` command. This is the check that catches a
      crash-looping backend, so it is worth knowing it still fires.

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

- [ ] The sidebar entry opens Quire and the UI shows real data from the Go
      process, not a placeholder.
- [ ] `/home/root/annex/tools/annex-service status` shows `annex-app@quire`
      active, with a port.

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
- [ ] Quire is still in the Annex sidebar.
- [ ] The backend came back on its own: `/home/root/annex/run/quire.json`
      exists again after the reboot, without anyone running `install.sh`.
- [ ] Configured sources, downloaded volumes and the cover cache are all still
      there.
- [ ] Downloaded volumes are still in `Comics` and still open in the stock
      reader.
- [ ] `./install.sh` re-run is clean after the reboot.

## 5. Redeploy and uninstall

- [ ] Redeploy over the running install (`./install.sh`, or `make install` for
      the developer loop). **Configured sources must survive.** State lives at
      `/home/root/.local/share/quire`, deliberately outside the app directory
      that the installer replaces wholesale — this check exists because an
      earlier layout ate a user's sources on a routine redeploy.
- [ ] Nothing user-visible has appeared under `/home/root/annex/apps/quire`: it
      should contain only `manifest.json`, `icon.svg`, `ui/` (with
      `ui/Main.qml`) and `backend/run` (executable).
- [ ] `/home/root/annex/run/quire.json` exists after the redeploy — the backend
      was stopped to swap its binary, and this is the proof it came back rather
      than crash-looping on the new one.
- [ ] `find /home/root/annex/apps/quire -name '._*'` is empty (macOS
      AppleDouble files; `COPYFILE_DISABLE=1` prevents them). This matters more
      than it did under AppLoad: the QML is read from disk by path, so a
      `._Main.qml` sits directly beside the file the host loads.
- [ ] `./uninstall.sh` removes the app, **asks** about state, and defaults to
      keeping it. Answer no; confirm `/home/root/.local/share/quire` is intact.
- [ ] Reinstall, and confirm the sources are all still configured.
- [ ] `./uninstall.sh --purge` on a device you are finished with: state gone,
      comics already in the library untouched.

## 6. After a reMarkable OS update

Only relevant if an update slipped through, or deliberately.

- [ ] Check Annex first: after xochitl restarts, is the Annex entry still in
      the sidebar? Annex matches xochitl's QML by element name, and an update
      can move those. A selector that matches nothing logs an error and returns
      the file unchanged, so the symptom is a missing entry, not a boot loop.
- [ ] If the UI is wrong rather than merely missing the entry,
      `/home/root/annex/tools/annex-disable` and start from a stock screen.
- [ ] Reinstall Quire: `./install.sh`. Read its OS warning — Quire ships no QML
      patch, so nothing of ours is version-locked; the coupling is Annex's.
- [ ] Re-run §1, §2 and §4 in full. An OS update invalidates every assumption
      Annex's selectors make about the system UI.

## 7. Space and safety, every pass

- [ ] `df -h /` — the root filesystem has ~47 MB free. **Never write to `/`.**
      Everything Quire owns lives on `/home`.
- [ ] `df -h /home` — enough headroom for the volumes you are about to download.
- [ ] `du -sh /home/root/.local/share/quire` — the cover cache and downloads are
      the things that grow.

## 8. Host-side, on a machine that is not the developer's

- [ ] A clone plus **Go only, no Qt** installs successfully: `install.sh` says
      `qt not needed` and deploys `ui/` as loose files.
- [ ] `make check` is green on that same Qt-less machine (`make qml` skips
      itself rather than failing).
- [ ] With no Go at all, `install.sh` names Go as the missing piece and links to
      the download page, rather than failing at a compiler error.
- [ ] With a Go older than 1.25, `install.sh` says which version it found and
      what `go.mod` needs.
