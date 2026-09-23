#!/usr/bin/env bash
# Fetch and build a pinned MuPDF (mutool) for Quire's own book reader.
#
#   build/mupdf.sh [arm64|host]
#
# arm64 (default) cross-compiles a fully static aarch64-linux-musl mutool
# with zig — that is what ships on the reMarkable Paper Pro, so it must not
# depend on any shared library the device might lack.
#
# host builds a native mutool for this machine, used only by the optional
# QUIRE_MUTOOL-driven integration test (backend/bookrender) and never shipped.
#
# The source tarball is downloaded into build/cache/ (gitignored) and its
# SHA-256 is checked against a hard-coded, pinned value — a mismatch is a
# hard failure, not a warning, because this is untrusted C parsing untrusted
# input on the device. Output is cached too, so a rerun with the target
# binary already present is a no-op; delete the cached binary to force a
# rebuild (also delete build/cache/src-<target> if you changed the make
# flags below — stale objects from a previous flag set break the link).
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

TARGET="${1:-arm64}"
case "$TARGET" in
    arm64|host) ;;
    *) die "unknown target '$TARGET' (want 'arm64' or 'host')" ;;
esac

VERSION=1.28.4
URL="https://mupdf.com/downloads/archive/mupdf-${VERSION}-source.tar.gz"
# Pinned once from the real download; see THIRD_PARTY.md.
SHA256=2d97e043a616f96b148657c9c3d81ad71c4bd2052c59a2a3315ad842599340f9

CACHE="$REPO_ROOT/build/cache"
TARBALL="$CACHE/mupdf-${VERSION}-source.tar.gz"

if [[ "$TARGET" == arm64 ]]; then
    OUT="$CACHE/mutool-${VERSION}-linux-arm64"
else
    OUT="$CACHE/mutool-${VERSION}-host"
fi

if [[ -e "$OUT" ]]; then
    say "cached  $OUT"
    exit 0
fi

command -v zig >/dev/null 2>&1 \
    || die "zig not found; install it (e.g. 'brew install zig') — it is the compiler used to build mutool, including the static arm64 cross-build"

mkdir -p "$CACHE"

if [[ ! -e "$TARBALL" ]]; then
    say "fetch   $URL"
    curl -fsSL -o "$TARBALL.part" "$URL"
    mv "$TARBALL.part" "$TARBALL"
fi

say "verify  sha256"
ACTUAL="$(shasum -a 256 "$TARBALL" | awk '{print $1}')"
[[ "$ACTUAL" == "$SHA256" ]] \
    || die "SHA-256 mismatch for $TARBALL: got $ACTUAL, want $SHA256 — corrupt download or tampered upstream; not building. Remove $TARBALL to retry the download."

SRC="$CACHE/src-$TARGET"
rm -rf "$SRC"
mkdir -p "$SRC"
say "extract $SRC"
tar -xzf "$TARBALL" -C "$SRC" --strip-components=1

MAKE_COMMON=(make -C "$SRC" OS=Linux build=release
    HAVE_X11=no HAVE_GLUT=no HAVE_CURL=no HAVE_OBJCOPY=no HAVE_LIBCRYPTO=no)

if [[ "$TARGET" == arm64 ]]; then
    say "build   mutool $VERSION for linux/arm64 (static, cross-compiled with zig)"
    "${MAKE_COMMON[@]}" \
        CC="zig cc -target aarch64-linux-musl" \
        CXX="zig c++ -target aarch64-linux-musl" \
        AR="zig ar" \
        LD="zig cc -target aarch64-linux-musl" \
        XLDFLAGS=-static \
        tools
else
    say "build   mutool $VERSION for the host (native, via zig)"
    "${MAKE_COMMON[@]}" \
        CC="zig cc" \
        CXX="zig c++" \
        AR="zig ar" \
        LD="zig cc" \
        tools
fi

BUILT="$SRC/build/release/mutool"
[[ -x "$BUILT" ]] || die "build finished but $BUILT is missing"

cp "$BUILT" "$OUT"
chmod +x "$OUT"
say "output  $OUT"
