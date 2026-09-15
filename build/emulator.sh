#!/usr/bin/env bash
# Build and run the AppLoad PC emulator with Quire loaded.
#
# PLAN §4 calls the emulator "what makes iteration bearable" — it runs the same
# QML frontend and the same backend binary as the device, on the host, without
# a tablet in the loop. Use it for M3's UI work; use the device for anything
# touching the AppLoad transport (see docs/DEVICE-NOTES.md §9 — three framing
# bugs in a row were invisible on the host).
#
#   build/emulator.sh            build if needed, then run
#   build/emulator.sh --rebuild  force a fresh emulator build
#   build/emulator.sh --headless verify the app loads, then exit (for CI/agents)
#
# We do NOT vendor rm-appload (PLAN §1.4): this clones and builds it into an
# ignored directory. Nothing from it enters this repository.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
EMU_DIR="$ROOT/.emulator"
SRC_DIR="$EMU_DIR/rm-appload"
APP_BIN="$SRC_DIR/appload.app/Contents/MacOS/appload"   # macOS
[[ "$(uname -s)" == "Linux" ]] && APP_BIN="$SRC_DIR/appload"

REBUILD=0
HEADLESS=0
for arg in "$@"; do
    case "$arg" in
        --rebuild)  REBUILD=1 ;;
        --headless) HEADLESS=1 ;;
        *) echo "unknown option: $arg" >&2; exit 2 ;;
    esac
done

# --- locate Qt -------------------------------------------------------------
# Homebrew keeps rcc/moc in libexec rather than bin, so add both.
if command -v brew >/dev/null 2>&1 && brew --prefix qt >/dev/null 2>&1; then
    QT_PREFIX="$(brew --prefix qt)"
    export PATH="$QT_PREFIX/bin:$QT_PREFIX/share/qt/libexec:$PATH"
fi
if ! command -v qmake6 >/dev/null 2>&1 && ! command -v qmake >/dev/null 2>&1; then
    echo "error: qmake not found. Install Qt 6 (macOS: brew install qt)." >&2
    exit 1
fi
QMAKE="$(command -v qmake6 || command -v qmake)"

# --- build the emulator ----------------------------------------------------
if [[ $REBUILD -eq 1 ]]; then rm -rf "$SRC_DIR"; fi

if [[ ! -x "$APP_BIN" ]]; then
    mkdir -p "$EMU_DIR"
    if [[ ! -d "$SRC_DIR/.git" ]]; then
        echo ">> cloning rm-appload (not vendored — built here, ignored by git)"
        git clone --depth 1 https://github.com/asivery/rm-appload.git "$SRC_DIR"
    fi
    echo ">> building the emulator with $QMAKE"
    ( cd "$SRC_DIR" && "$QMAKE" . >/dev/null && make -j"$(getconf _NPROCESSORS_ONLN)" >/dev/null )
    echo ">> built $APP_BIN"
fi

# --- build Quire for the host and stage it ---------------------------------
echo ">> building Quire for the host"
"$HERE/build-pc.sh" >/dev/null

APPS_ROOT="$SRC_DIR/applications_root"
mkdir -p "$APPS_ROOT"
rm -rf "$APPS_ROOT/quire"
cp -R "$ROOT/output" "$APPS_ROOT/quire"
echo ">> staged Quire into $APPS_ROOT/quire"

# --- run -------------------------------------------------------------------
cd "$SRC_DIR"

if [[ $HEADLESS -eq 1 ]]; then
    # Loads the app registry and exits. Proves the manifest parses and the rcc
    # registers; it canNOT prove the QML renders or that a tap round-trips,
    # because nothing clicks the launcher tile. Do not overclaim this.
    log="$(mktemp)"
    QT_QPA_PLATFORM=offscreen "$APP_BIN" >"$log" 2>&1 &
    pid=$!
    sleep 8
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    echo "--- emulator output ---"
    cat "$log"
    if grep -q "Loaded app root.*quire" "$log"; then
        echo "--- OK: emulator registered the Quire app bundle"
        # "[QTFB]: Failed to initialize the socket!" here is expected and harmless:
        # QTFB is the framebuffer path for *external* apps; QML apps do not use it.
        rm -f "$log"; exit 0
    fi
    echo "--- FAILED: emulator did not load the app" >&2
    rm -f "$log"; exit 1
fi

echo ">> launching the emulator — click the Quire tile to open it"
exec "$APP_BIN"
