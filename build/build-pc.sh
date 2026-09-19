#!/usr/bin/env bash
# Build a Quire bundle for this host into output/. Same QML, host-arch backend.
#
# There is no PC emulator for Annex. What this is good for is running the
# backend directly — it serves loopback HTTP and needs no host at all:
#
#   ANNEX_RUN=/tmp/annexrun QUIRE_DATA_DIR=/tmp/quiredata output/backend/run
#
# then talk to it with curl using the port and token from
# /tmp/annexrun/quire.json. See backend/annex for the wire.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

go_bin="$(find_go)"
build_bundle output "$("$go_bin" env GOOS)" "$("$go_bin" env GOARCH)"

file "$REPO_ROOT/output/backend/run"
