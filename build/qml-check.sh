#!/usr/bin/env bash
# Lint ui/ and instantiate every screen offscreen.
#
# There is no QML unit-test framework in this repository and adding one would
# pull Qt into a build that otherwise needs only Go — and since Annex loads
# Quire's QML loose from disk, Go really is the only build dependency now.
# What this does instead is the two things that catch the failures QML
# actually has:
#
#   1. qmllint, failing on *errors* only. The warnings are almost all
#      "unqualified access to `model`", which is how a ListView delegate is
#      written and which is not going to change.
#   2. Load every screen under the offscreen platform and assert the things
#      PLAN §12.1 is about — that a page size is derived from a real viewport,
#      that paging stops hard at both ends rather than wrapping, and that the
#      on-screen keyboard neither resizes a page nor moves the pager.
#   3. Load the real ui/Main.qml — the shell itself — against a stub Backend,
#      and drive it by message and by tap. See "the device layout" below and
#      build/qmlcheck/MainHarness.qml.
#
# Both need Qt 6. Without it this skips, loudly — a machine with only the Go
# toolchain still gets a green `make check`, which is the point: installing
# Quire must not require a gigabyte of build tooling.
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

# The device ships Noto Sans, Noto Serif, NotoSansUI and Noto Mono and nothing
# else. A codepoint outside them renders as a tofu box, which is how the
# backspace key came to be a square on the user's screen: U+232B ERASE TO THE
# LEFT lives in Noto Sans *Symbols*, which is not installed. The same hole
# swallows U+21E7 shift, U+23CE return, U+2423 space and U+2326 delete, so the
# trap is not specific to one key.
#
# The allow-list below was checked against the cmap tables of the fonts pulled
# off the device, not inferred from what a desktop happens to render. Anything
# else in a string that reaches the screen fails here. A glyph inside a comment
# is fine: nobody sees it.
echo ">> auditing rendered glyphs against the fonts on the device"
glyphs=0
for f in ui/*.qml ui/*.js; do
    hits="$(sed -e 's://.*::' "$f" \
            | sed -e 's/\xc2\xb7//g' -e 's/\xe2\x80\x94//g' -e 's/\xe2\x80\x99//g' \
                  -e 's/\xe2\x80\xa6//g' -e 's/\xe2\x80\xb9//g' -e 's/\xe2\x80\xba//g' \
            | LC_ALL=C grep -n '[^[:print:][:space:]]' || true)"
    if [[ -n "$hits" ]]; then
        echo "--- $f draws a glyph the device has no font for:"
        echo "$hits"
        glyphs=1
    fi
done
if [[ $glyphs -eq 0 ]]; then
    echo "   only codepoints present in the device fonts"
else
    status=1
fi

echo ">> instantiating every screen under the offscreen platform"
out="$(QT_QPA_PLATFORM=offscreen qml -platform offscreen \
        -I "$ROOT/ui" "$HERE/qmlcheck/Harness.qml" 2>&1)" || true
echo "$out" | grep -E '^qml: (ok|FAIL)' || true

if ! echo "$out" | grep -q '^qml: HARNESS OK'; then
    echo "--- the offscreen harness failed"
    echo "$out"
    status=1
fi

# ---- the device layout, built here so it cannot go stale --------------------
#
# ui/Main.qml — the shell — asks for Annex's shared library by relative path:
#
#     import "../../../lib"
#
# On the device it runs from /home/root/annex/apps/quire/ui, so that resolves
# to /home/root/annex/lib. From this repository it resolves to a directory
# three levels above the checkout, which is not there. That path, and only that
# path, is why Main.qml was never tested: `Backend` is a plain QML file in
# Annex's lib/ — its own qmldir says "that is a plain local directory import" —
# and not a C++ plugin, whatever the comments in this repository used to say.
#
# So the device's shape is built here, in a temporary directory, every run:
#
#     $fixture/lib/Backend.qml                 the stub, from qmlcheck/fixture/
#     $fixture/lib/qmldir
#     $fixture/apps/quire/ui       ->          this repository's real ui/
#     $fixture/apps/quire/harness/MainHarness.qml
#
# **Generated rather than committed, and ui/ is a symlink**, so there is no
# copy of the app to drift out of step with the app. A fixture that is a stale
# copy of the thing it tests is worse than no fixture.
echo ">> loading the real ui/Main.qml against a stub Backend"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/quire-qmlcheck.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/lib" "$fixture/apps/quire/harness"
cp "$HERE/qmlcheck/fixture/Backend.qml" "$fixture/lib/Backend.qml"
cp "$HERE/qmlcheck/fixture/qmldir" "$fixture/lib/qmldir"
ln -s "$ROOT/ui" "$fixture/apps/quire/ui"
# Copied rather than symlinked: Qt resolves a component's relative imports
# against the URL it was given, and the harness' own imports are the device's.
cp "$HERE/qmlcheck/MainHarness.qml" "$fixture/apps/quire/harness/MainHarness.qml"

mainout="$(QT_QPA_PLATFORM=offscreen qml -platform offscreen \
            "$fixture/apps/quire/harness/MainHarness.qml" 2>&1)" || true
echo "$mainout" | grep -E '^qml: (ok|FAIL)' || true

if ! echo "$mainout" | grep -q '^qml: MAIN HARNESS OK'; then
    echo "--- the harness for Main.qml failed"
    echo "$mainout"
    status=1
fi

exit $status
