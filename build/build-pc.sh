#!/usr/bin/env bash
# Build the Quire AppLoad bundle for this host into output/, for the AppLoad PC
# emulator. Same QML, same resources.rcc, host-arch backend.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

go_bin="$(find_go)"
build_bundle output "$("$go_bin" env GOOS)" "$("$go_bin" env GOARCH)"

file "$REPO_ROOT/output/backend/entry"
