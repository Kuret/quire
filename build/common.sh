#!/usr/bin/env bash
# Shared helpers for the Quire build scripts. Sourced, not executed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
say() { printf '  %s\n' "$*" >&2; }

# find_go echoes a usable go binary. mise installs are not on PATH for
# non-interactive shells, so look there too.
find_go() {
    if [[ -n "${GO:-}" ]]; then printf '%s' "$GO"; return; fi
    if command -v go >/dev/null 2>&1; then command -v go; return; fi
    local candidate
    candidate="$(ls -d "$HOME"/.local/share/mise/installs/go/*/bin/go 2>/dev/null | sort -V | tail -n 1 || true)"
    [[ -x "$candidate" ]] || die "no go toolchain found; set \$GO"
    printf '%s' "$candidate"
}

# build_bundle <outdir> <goos> <goarch>
# Produces the Annex app directory layout:
#   <outdir>/manifest.json
#   <outdir>/icon.svg
#   <outdir>/ui/...            <- loose QML, loaded from disk by path
#   <outdir>/backend/run       <- executable; systemd starts it, no argv needed
#
# **There is no resources.rcc any more.** Annex's host loads an app's QML with
# a plain file:// URL, measured on hardware 2026-09-18 against 3.28.0.172 — the
# log line reads
#
#   [hello] unloading (unloading file:///home/root/annex/apps/hello/ui/Main.qml:28)
#
# so the running QML really did come from the filesystem. That removes the Qt
# rcc from the build entirely: installing Quire now needs only a Go toolchain,
# which is what the committed prebuilt/resources.rcc existed to fake.
build_bundle() {
    local outdir="$1" goos="$2" goarch="$3"
    local go
    go="$(find_go)"

    cd "$REPO_ROOT"
    rm -rf "$outdir"
    mkdir -p "$outdir/backend" "$outdir/ui"

    # Everything ui/ ships: the QML and the JS it imports. Copied rather than
    # compiled, which is the whole point of the change.
    cp ui/*.qml ui/*.js "$outdir/ui/"
    say "ui      $(ls "$outdir/ui" | wc -l | tr -d ' ') files"

    say "go      $go ($goos/$goarch)"
    # CGO off keeps the binary static: the device has no toolchain and its
    # glibc is not ours to depend on.
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        "$go" build -trimpath \
        -ldflags "-s -w -X main.version=$(bundle_version)" \
        -o "$outdir/backend/run" ./backend/cmd/quired
    chmod +x "$outdir/backend/run"

    # icon.svg is the sidebar icon Annex reads from the manifest. The
    # AppLoad-era icon.png went with AppLoad: nothing reads it now.
    cp manifest.json icon.svg "$outdir/"

    say "output  $outdir"
}

# bundle_version echoes a build stamp baked into the backend, so a Pong can be
# traced back to a commit.
bundle_version() {
    local describe
    describe="$(git -C "$REPO_ROOT" describe --always --dirty 2>/dev/null || true)"
    printf '%s' "${describe:-dev}"
}
