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

# rcc_path echoes a usable Qt rcc binary, or nothing (exit 1) if there is none.
# It never dies: an installing user is not required to have a gigabyte of Qt on
# their machine, so "no rcc" is an ordinary state handled by make_resources.
rcc_path() {
    if [[ -n "${RCC:-}" ]]; then printf '%s' "$RCC"; return 0; fi
    if command -v rcc >/dev/null 2>&1; then command -v rcc; return 0; fi
    local candidate
    for candidate in \
        /opt/homebrew/share/qt/libexec/rcc \
        /usr/local/share/qt/libexec/rcc \
        /usr/lib/qt6/libexec/rcc \
        /usr/lib/x86_64-linux-gnu/qt6/libexec/rcc
    do
        [[ -x "$candidate" ]] && { printf '%s' "$candidate"; return 0; }
    done
    return 1
}

# find_rcc echoes a usable Qt rcc binary or dies. For callers that genuinely
# cannot proceed without one — regenerating the prebuilt, mainly.
find_rcc() {
    rcc_path || die "no Qt rcc found; set \$RCC (brew install qt, or apt install qt6-base-dev)"
}

PREBUILT_RCC="$REPO_ROOT/prebuilt/resources.rcc"
PREBUILT_FINGERPRINT="$REPO_ROOT/prebuilt/resources.rcc.sources"

# sha256_of <file> echoes the hex digest. macOS ships shasum, Linux sha256sum.
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1
    else die "no sha256sum or shasum on PATH"
    fi
}

# qrc_inputs echoes every file resources.rcc is built from, application.qrc
# included, relative to the repo root.
qrc_inputs() {
    printf 'application.qrc\n'
    sed -n 's|.*<file>\(.*\)</file>.*|\1|p' "$REPO_ROOT/application.qrc"
}

# prebuilt_fingerprint echoes "<sha256>  <path>" for each input, sorted by path.
# This is what lets a machine with no Qt at all still notice that the committed
# resources.rcc no longer matches ui/.
prebuilt_fingerprint() {
    cd "$REPO_ROOT"
    local f
    qrc_inputs | sort | while read -r f; do
        printf '%s  %s\n' "$(sha256_of "$f")" "$f"
    done
}

# prebuilt_check returns 0 if prebuilt/resources.rcc is in step with ui/.
prebuilt_check() {
    [[ -f "$PREBUILT_RCC" && -f "$PREBUILT_FINGERPRINT" ]] || return 1
    [[ "$(prebuilt_fingerprint)" == "$(cat "$PREBUILT_FINGERPRINT")" ]]
}

# make_resources <outfile> produces resources.rcc, preferring a real rcc and
# falling back to the committed prebuilt.
#
# The prebuilt exists so that installing Quire needs only a Go toolchain. It is
# a build artefact in git, which is a cost; the drift it could cause is paid for
# by prebuilt_check, which `make check` runs and which needs no Qt.
make_resources() {
    local out="$1" rcc
    if rcc="$(rcc_path)"; then
        say "rcc     $rcc (format-version $RCC_FORMAT_VERSION)"
        "$rcc" --binary --format-version "$RCC_FORMAT_VERSION" -o "$out" application.qrc
        return
    fi
    [[ -f "$PREBUILT_RCC" ]] \
        || die "no Qt rcc found and no prebuilt/resources.rcc; set \$RCC"
    prebuilt_check \
        || die "no Qt rcc found, and prebuilt/resources.rcc is stale relative to ui/ — install Qt and run build/prebuilt.sh update"
    say "rcc     none found; using prebuilt/resources.rcc"
    cp "$PREBUILT_RCC" "$out"
}

# build_bundle <outdir> <goos> <goarch>
# Produces the AppLoad app directory layout:
#   <outdir>/manifest.json
#   <outdir>/icon.png
#   <outdir>/resources.rcc     <- required; AppLoad has no loose-file fallback
#   <outdir>/backend/entry     <- executable, argv[1] = AppLoad socket path
build_bundle() {
    local outdir="$1" goos="$2" goarch="$3"
    local go
    go="$(find_go)"

    cd "$REPO_ROOT"
    rm -rf "$outdir"
    mkdir -p "$outdir/backend"

    make_resources "$outdir/resources.rcc"

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
