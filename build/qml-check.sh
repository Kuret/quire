#!/usr/bin/env bash
# Lint ui/ and instantiate every screen offscreen.
#
# There is no QML unit-test framework in this repository and adding one would
# pull Qt into a build that otherwise needs only Go (see build/prebuilt.sh for
# the same trade). What this does instead is the two things that catch the
# failures QML actually has:
#
#   1. qmllint, failing on *errors* only. The warnings are almost all
#      "unqualified access to `model`", which is how a ListView delegate is
#      written and which is not going to change.
#   2. Load every screen under the offscreen platform and assert the things
#      PLAN §12.1 is about — that a page size is derived from a real viewport,
#      that paging stops hard at both ends rather than wrapping, and that the
#      on-screen keyboard neither resizes a page nor moves the pager.
#
# Both need Qt 6. Without it this skips, loudly, exactly as the prebuilt check
# does — a machine with only the Go toolchain still gets a green `make check`.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

if command -v brew >/dev/null 2>&1 && brew --prefix qt >/dev/null 2>&1; then
    QT_PREFIX="$(brew --prefix qt)"
    export PATH="$QT_PREFIX/bin:$QT_PREFIX/share/qt/libexec:$PATH"
fi

if ! command -v qmllint >/dev/null 2>&1 || ! command -v qml >/dev/null 2>&1; then
    echo "skipped: qmllint/qml not found (install Qt 6 to run the QML checks)"
    exit 0
fi

cd "$ROOT"

status=0

echo ">> qmllint ui/*.qml"
for f in ui/*.qml; do
    # Errors fail the build; warnings are printed and tolerated.
    if qmllint "$f" 2>&1 | grep -q '^Error:'; then
        echo "--- errors in $f"
        qmllint "$f" 2>&1 | grep '^Error:' || true
        status=1
    fi
done
[[ $status -eq 0 ]] && echo "   no errors"

echo ">> instantiating every screen under the offscreen platform"
out="$(QT_QPA_PLATFORM=offscreen qml -platform offscreen \
        -I "$ROOT/ui" "$HERE/qmlcheck/Harness.qml" 2>&1)" || true
echo "$out" | grep -E '^qml: (ok|FAIL)' || true

if ! echo "$out" | grep -q '^qml: HARNESS OK'; then
    echo "--- the offscreen harness failed"
    echo "$out"
    status=1
fi

exit $status
