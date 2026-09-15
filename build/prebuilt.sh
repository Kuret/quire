#!/usr/bin/env bash
# Manage prebuilt/resources.rcc — the committed Qt resource bundle.
#
#   build/prebuilt.sh update   regenerate it and its fingerprint (needs Qt rcc)
#   build/prebuilt.sh check    fail if it is stale relative to ui/ (needs nothing)
#
# Why a build artefact lives in git: building resources.rcc needs Qt's rcc, and
# a Qt install is ~1 GB. The Go half of Quire needs only the Go toolchain, so
# requiring Qt would make installing a comic reader cost a gigabyte of build
# tooling for a 22 KB deterministic file. The honest cost of the shortcut is
# drift, and `check` below is how drift is caught: it hashes every input listed
# in application.qrc, so a machine with no Qt still notices.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

cmd="${1:-check}"

case "$cmd" in
update)
    rcc="$(find_rcc)"
    mkdir -p "$(dirname "$PREBUILT_RCC")"
    say "rcc     $rcc (format-version $RCC_FORMAT_VERSION)"
    cd "$REPO_ROOT"
    "$rcc" --binary --format-version "$RCC_FORMAT_VERSION" \
        -o "$PREBUILT_RCC" application.qrc
    prebuilt_fingerprint > "$PREBUILT_FINGERPRINT"
    say "wrote   ${PREBUILT_RCC#"$REPO_ROOT"/} ($(wc -c < "$PREBUILT_RCC" | tr -d ' ') bytes)"
    ;;
check)
    if ! [[ -f "$PREBUILT_RCC" && -f "$PREBUILT_FINGERPRINT" ]]; then
        die "prebuilt/resources.rcc is missing; run build/prebuilt.sh update"
    fi
    if ! prebuilt_check; then
        printf 'error: prebuilt/resources.rcc is stale relative to ui/.\n' >&2
        printf '       Run build/prebuilt.sh update (needs Qt rcc) and commit the result.\n' >&2
        printf '       Changed inputs:\n' >&2
        diff <(cat "$PREBUILT_FINGERPRINT") <(prebuilt_fingerprint) >&2 || true
        exit 1
    fi
    # With a real rcc to hand, prove the bytes too, not just the inputs.
    if rcc="$(rcc_path)"; then
        tmp="$(mktemp)"
        trap 'rm -f "$tmp"' EXIT
        cd "$REPO_ROOT"
        "$rcc" --binary --format-version "$RCC_FORMAT_VERSION" -o "$tmp" application.qrc
        cmp -s "$tmp" "$PREBUILT_RCC" \
            || die "prebuilt/resources.rcc differs from what $rcc produces; run build/prebuilt.sh update"
        say "prebuilt resources.rcc matches ui/ and byte-matches $rcc"
    else
        say "prebuilt resources.rcc matches ui/ (no rcc here, so bytes unchecked)"
    fi
    ;;
*)
    die "usage: build/prebuilt.sh [update|check]"
    ;;
esac
