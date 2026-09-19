#!/usr/bin/env bash
# Build the Quire Annex app bundle for the reMarkable Paper Pro (linux/arm64)
# into output-rmpp/. Deploy it with build/install-device.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

build_bundle output-rmpp linux arm64

# Prove it is what the device can actually run, rather than assuming.
file "$REPO_ROOT/output-rmpp/backend/run"
