#!/usr/bin/env bash
# Shared helpers for the Quire build scripts. Sourced, not executed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# AppLoad's resource format. The device runs Qt 6.8.2, which reads RCC format
# version 3; a host rcc from a newer Qt may default to something newer, so pin
# it rather than trusting the default.
RCC_FORMAT_VERSION=3

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

# find_rcc echoes a usable Qt rcc binary.
find_rcc() {
    if [[ -n "${RCC:-}" ]]; then printf '%s' "$RCC"; return; fi
    if command -v rcc >/dev/null 2>&1; then command -v rcc; return; fi
    local candidate
    for candidate in \
        /opt/homebrew/share/qt/libexec/rcc \
        /usr/local/share/qt/libexec/rcc \
        /usr/lib/qt6/libexec/rcc \
        /usr/lib/x86_64-linux-gnu/qt6/libexec/rcc
    do
        [[ -x "$candidate" ]] && { printf '%s' "$candidate"; return; }
    done
    die "no Qt rcc found; set \$RCC"
}

# build_bundle <outdir> <goos> <goarch>
# Produces the AppLoad app directory layout:
#   <outdir>/manifest.json
#   <outdir>/icon.png
#   <outdir>/resources.rcc     <- required; AppLoad has no loose-file fallback
#   <outdir>/backend/entry     <- executable, argv[1] = AppLoad socket path
build_bundle() {
    local outdir="$1" goos="$2" goarch="$3"
    local go rcc
    go="$(find_go)"
    rcc="$(find_rcc)"

    cd "$REPO_ROOT"
    rm -rf "$outdir"
    mkdir -p "$outdir/backend"

    say "rcc     $rcc (format-version $RCC_FORMAT_VERSION)"
    "$rcc" --binary --format-version "$RCC_FORMAT_VERSION" \
        -o "$outdir/resources.rcc" application.qrc

    say "go      $go ($goos/$goarch)"
    # CGO off keeps the binary static: the device has no toolchain and its
    # glibc is not ours to depend on.
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        "$go" build -trimpath \
        -ldflags "-s -w -X main.version=$(bundle_version)" \
        -o "$outdir/backend/entry" ./backend/cmd/quired
    chmod +x "$outdir/backend/entry"

    cp manifest.json icon.png "$outdir/"

    say "output  $outdir"
}

# bundle_version echoes a build stamp baked into the backend, so a Pong can be
# traced back to a commit.
bundle_version() {
    local describe
    describe="$(git -C "$REPO_ROOT" describe --always --dirty 2>/dev/null || true)"
    printf '%s' "${describe:-dev}"
}
