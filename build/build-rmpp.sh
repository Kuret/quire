#!/usr/bin/env bash
# Build the Quire Annex app bundle for the reMarkable Paper Pro (linux/arm64)
# into output-rmpp/. Deploy it with build/install-device.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

build_bundle output-rmpp linux arm64

# mutool renders book pages (backend/bookrender); it ships as a separate
# statically-linked executable, not linked into the Go binary, so the AGPL
# licence it carries applies to it and not to Quire (see THIRD_PARTY.md).
"$(dirname "${BASH_SOURCE[0]}")/mupdf.sh" arm64
MUTOOL="$REPO_ROOT/build/cache/mutool-1.28.4-linux-arm64"
MUPDF_COPYING="$REPO_ROOT/build/cache/src-arm64/COPYING"
[[ -e "$MUTOOL" ]] || die "$MUTOOL is missing after build/mupdf.sh arm64 ran"
[[ -e "$MUPDF_COPYING" ]] || die "$MUPDF_COPYING is missing; delete build/cache/mutool-1.28.4-linux-arm64 to force build/mupdf.sh to re-extract the source"
mkdir -p "$REPO_ROOT/output-rmpp/licenses"
cp "$MUTOOL" "$REPO_ROOT/output-rmpp/backend/mutool"
chmod 0755 "$REPO_ROOT/output-rmpp/backend/mutool"
cp "$MUPDF_COPYING" "$REPO_ROOT/output-rmpp/licenses/MuPDF-COPYING"
cp "$REPO_ROOT/THIRD_PARTY.md" "$REPO_ROOT/output-rmpp/licenses/THIRD_PARTY.md"
say "mutool  $REPO_ROOT/output-rmpp/backend/mutool"

# Prove it is what the device can actually run, rather than assuming.
file "$REPO_ROOT/output-rmpp/backend/run"
file "$REPO_ROOT/output-rmpp/backend/mutool"
