#!/usr/bin/env bash
# Install Quire on a reMarkable Paper Pro, from a clone of this repository.
#
#   ./install.sh [--host ADDR] [--user NAME] [--verify] [--skip-appload-check]
#
#   --host   device address; default $QUIRE_DEVICE, then 10.11.99.1 (USB)
#   --user   ssh user; default $QUIRE_DEVICE_USER, then root
#   --verify re-run the checks and the post-install verification only; build
#            nothing, deploy nothing. Safe to run at any time.
#   --skip-appload-check
#            proceed even if this script cannot prove AppLoad's hooks resolve.
#            Read the refusal it prints first; the failure mode is a tablet
#            that reboots on every launch.
#
# Idempotent: running it twice leaves the same tree as running it once, and it
# never touches /home/root/.local/share/quire — your sources, downloads and
# cover cache live there precisely so that reinstalling cannot eat them.
#
# What it does NOT do: install xovi, qt-resource-rebuilder or AppLoad. Those are
# prerequisites, and this script checks for them rather than owning them.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source build/common.sh

HOST="${QUIRE_DEVICE:-10.11.99.1}"
SSH_USER="${QUIRE_DEVICE_USER:-root}"
VERIFY_ONLY=0
SKIP_APPLOAD=0
APP_DIR="/home/root/xovi/exthome/appload/quire"
STATE_DIR="/home/root/.local/share/quire"

while [[ $# -gt 0 ]]; do
    case "$1" in
    --host) HOST="${2:?--host needs an address}"; shift 2 ;;
    --user) SSH_USER="${2:?--user needs a name}"; shift 2 ;;
    --verify|--verify-only) VERIFY_ONLY=1; shift ;;
    --skip-appload-check) SKIP_APPLOAD=1; shift ;;
    -h|--help) sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
    esac
done

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST")
dev() { "${SSH[@]}" "$@"; }

# Problems are collected and reported together: being told about Go, then about
# rcc, then about ssh, one failed run at a time, is a miserable way to install
# anything.
PROBLEMS=()
problem() { PROBLEMS+=("$1"); }
report_problems() {
    (( ${#PROBLEMS[@]} )) || return 0
    printf '\nQuire cannot install yet:\n\n' >&2
    local p
    for p in "${PROBLEMS[@]}"; do printf '  * %s\n\n' "$p" >&2; done
    exit 1
}

hr() { printf '\n== %s\n' "$1" >&2; }

# ---------------------------------------------------------------- host checks

hr "host"

command -v ssh >/dev/null 2>&1 || problem "ssh is not on PATH. Install OpenSSH."

GO=""
if GO="$(find_go 2>/dev/null)"; then
    go_ver="$("$GO" version 2>/dev/null | awk '{print $3}' | sed 's/^go//')"
    # go.mod requires 1.25.14; an older toolchain refuses to build rather than
    # producing something subtly wrong, so check for it up front.
    if [[ "$(printf '%s\n1.25\n' "${go_ver:-0}" | sort -V | head -n 1)" != "1.25" ]]; then
        problem "Go ${go_ver:-?} is too old; Quire needs Go 1.25 or newer (go.mod says 1.25.14). https://go.dev/dl/"
    else
        say "go      $GO ($go_ver)"
    fi
else
    problem "No Go toolchain found. Install Go 1.25 or newer from https://go.dev/dl/, or set \$GO to its path."
fi

if rcc="$(rcc_path)"; then
    say "rcc     $rcc"
elif prebuilt_check; then
    say "rcc     none (using the committed prebuilt/resources.rcc — this is fine)"
else
    problem "No Qt rcc on this machine, and prebuilt/resources.rcc is stale or missing. Either install Qt (brew install qt / apt install qt6-base-dev) or update your clone."
fi

report_problems

# -------------------------------------------------------------- device checks

hr "device  $SSH_USER@$HOST"

dev true >/dev/null 2>&1 || die "cannot ssh to $SSH_USER@$HOST.
  * On USB the address is 10.11.99.1; over wifi it is whatever the tablet's
    Settings > Help > Copyrights and licenses screen reports.
  * Developer mode must be on, and an ssh key installed (this script does not
    prompt for passwords: ssh-copy-id $SSH_USER@$HOST first)."

model="$(dev 'tr -d "\0" < /proc/device-tree/model' 2>/dev/null || true)"
case "$model" in
*Ferrari*) say "model   $model" ;;
"")        problem "Could not read the device model. Is this a reMarkable?" ;;
*)         problem "This is a '$model'. Quire targets the reMarkable Paper Pro (Ferrari) and has never been run on anything else." ;;
esac

os_ver="$(dev '. /etc/os-release 2>/dev/null; echo "$IMG_VERSION"' 2>/dev/null || true)"
os_build="$(dev 'cat /etc/version' 2>/dev/null || true)"
case "$os_ver" in
3.25.*) say "os      $os_ver (build $os_build)" ;;
"")     say "os      unknown (build ${os_build:-?})"
        printf '  warning: could not read IMG_VERSION from /etc/os-release.\n' >&2 ;;
*)      say "os      $os_ver (build $os_build)"
        # Deliberately a warning, not a refusal. Quire ships no QML patch, so
        # there is no version-matched .qmd to gate on; the real OS coupling is
        # AppLoad's own hooks, and that is tested for real below.
        printf '  warning: Quire has only ever been run on OS 3.25.1.1. Nothing here\n' >&2
        printf '           is version-locked, but the AppLoad check below is the one\n' >&2
        printf '           that matters — do not skip it.\n' >&2 ;;
esac

dev '[ -e /home/root/xovi/xovi.so ]' \
    || problem "xovi is not installed (/home/root/xovi/xovi.so is missing). Install the xovi stack first — remagic (https://github.com/MaximeRivest/remagic) is the usual way — then re-run this script."
dev '[ -e /home/root/xovi/extensions.d/qt-resource-rebuilder.so ]' \
    || problem "qt-resource-rebuilder is not installed (/home/root/xovi/extensions.d/qt-resource-rebuilder.so). AppLoad needs it."
dev '[ -e /home/root/xovi/extensions.d/appload.so ]' \
    || problem "AppLoad is not installed (/home/root/xovi/extensions.d/appload.so). Quire is an AppLoad app; without it there is nothing to launch."

report_problems

# --------------------------------------------- the AppLoad compatibility gate
#
# AppLoad v0.5.x does not work on OS 3.25.1.1: its hooks reference a hashed QML
# identifier that does not exist in this build, so xochitl aborts (SIGABRT),
# which reboots the whole device, which wipes the volatile /etc drop-in — so the
# symptom looks like "xovi never started" rather than a crash. v0.4.2 is the
# newest release whose hooks all resolve. remagic pins v0.5.3, so a stock
# remagic bootstrap is broken here. See docs/DEVICE-NOTES.md §2.
#
# The check below prefers evidence over version numbers: a journal line saying
# the hooks loaded is proof, and it stays true if a future AppLoad release fixes
# this.

hr "AppLoad"

hooks_ok=0
hooks_bad=0
dev 'journalctl -u xochitl -b --no-pager 2>/dev/null | grep -q "Loaded external AppLoad hooks in main UI"' && hooks_ok=1 || true
dev 'journalctl -u xochitl -b --no-pager 2>/dev/null | grep -q "Couldn.t resolve the hashed identifier" \
     || journalctl -u xochitl -b -1 --no-pager 2>/dev/null | grep -q "Couldn.t resolve the hashed identifier"' && hooks_bad=1 || true

appload_ver="$(dev '/home/root/.vellum/bin/vellum list -I appload 2>/dev/null | sed -n "s/^appload-\([0-9.]*\)-r.*/\1/p"' 2>/dev/null || true)"
[[ -n "$appload_ver" ]] && say "version $appload_ver (per vellum)"

appload_refusal() {
    cat >&2 <<EOF

AppLoad on this device is not one Quire will install against.

  $1

  AppLoad v0.5.x does not work on reMarkable OS 3.25.1.1. Its hooks reference a
  QML identifier that does not exist in this build; xochitl aborts and the
  device reboots, every time the UI starts. Use **v0.4.2**, the newest release
  whose hooks all resolve.

  If you installed the xovi stack with remagic, you have v0.5.3: remagic pins
  it. Replace /home/root/xovi/extensions.d/appload.so with the v0.4.2 build from
  https://github.com/asivery/rm-appload/releases and restart xochitl.

  Detail, including how to check any AppLoad build against your own device in
  seconds: docs/DEVICE-NOTES.md §2.

  If you know better than this check — a newer AppLoad whose hooks do resolve,
  say — re-run with --skip-appload-check.
EOF
    exit 1
}

if (( SKIP_APPLOAD )); then
    say "skipped (--skip-appload-check)"
elif (( hooks_bad )); then
    appload_refusal "This device's journal contains AppLoad's \"Couldn't resolve the hashed identifier\" panic — the crash has already happened here."
elif (( hooks_ok )); then
    say "hooks   resolved (xochitl logged \"Loaded external AppLoad hooks in main UI\")"
elif [[ -n "$appload_ver" ]] && [[ "$(printf '%s\n0.5\n' "$appload_ver" | sort -V | head -n 1)" == "0.5" ]]; then
    appload_refusal "vellum reports AppLoad $appload_ver installed."
else
    appload_refusal "This script could not find evidence either way: xochitl's journal for this boot mentions neither AppLoad's hooks loading nor them failing.${appload_ver:+ (vellum reports $appload_ver.)}
  Open the AppLoad launcher on the tablet, then re-run this script — a launcher
  that opens has resolved its hooks, and the journal will say so."
fi

# --------------------------------------------------------------- build/deploy

if (( VERIFY_ONLY )); then
    hr "verify only — not building, not deploying"
else
    hr "build"
    build/build-rmpp.sh

    hr "deploy"
    # install-device.sh is the developer inner loop and the deploy half of this
    # script both; there is one implementation of "put the bundle on the tablet".
    QUIRE_DEVICE_USER="$SSH_USER" build/install-device.sh "$HOST"
fi

# -------------------------------------------------------------- verification

hr "verify"

dev "[ -x '$APP_DIR/backend/entry' ] && [ -f '$APP_DIR/resources.rcc' ] && [ -f '$APP_DIR/manifest.json' ] && [ -f '$APP_DIR/icon.png' ]" \
    || die "the app directory on the device is incomplete: $APP_DIR"
say "files   $APP_DIR complete"

preload="$(dev 'tr "\0" "\n" < /proc/$(pidof xochitl)/environ 2>/dev/null | grep "^LD_PRELOAD=" || true')"
if [[ "$preload" == *"/home/root/xovi/xovi.so"* ]]; then
    say "xovi    injected ($preload)"
else
    printf '  warning: xochitl is running without xovi (%s).\n' "${preload:-no LD_PRELOAD}" >&2
    printf '           Quire cannot appear in the launcher until it is. A common\n' >&2
    printf '           cause is the boot unit in multi-user.target.wants being a\n' >&2
    printf '           copy rather than a symlink — docs/DEVICE-NOTES.md §3.\n' >&2
fi

if dev 'journalctl -u xochitl -b --no-pager 2>/dev/null | grep -q "Loaded app root .*appload..quire"'; then
    say "appload registered Quire (\"Loaded app root .../quire\")"
else
    printf '  note: AppLoad has not registered Quire in this boot yet. It rescans\n' >&2
    printf '        app directories when the launcher is opened — open it on the\n' >&2
    printf '        tablet, then run ./install.sh --verify to confirm.\n' >&2
fi

# --------------------------------------------------------------- first run

hr "first run"

web_on=0
dev 'grep -q "^WebInterfaceEnabled=true" /home/root/.config/remarkable/xochitl.conf' && web_on=1 || true
if (( web_on )); then
    say "web     USB web interface is on"
else
    cat >&2 <<'EOF'

  1. TURN ON THE USB WEB INTERFACE. It is off on a factory device, and Quire
     saves downloaded comics through it — nothing can reach your library until
     it is on.

         On the tablet: Settings > Storage > "USB web interface" > on,
         then restart the tablet.

     Quire deliberately does not flip this for you: the setting only takes
     effect when xochitl restarts, and restarting throws away whatever you were
     reading.
EOF
fi

comics_ok=0
if (( web_on )); then
    dev 'wget -q -O - --post-data "" http://10.11.99.1/documents/ 2>/dev/null | grep -q "\"VissibleName\": \"Comics\""' && comics_ok=1 || true
fi
if (( comics_ok )); then
    say "folder  My Files > Comics exists"
else
    cat >&2 <<'EOF'

  2. CREATE A FOLDER CALLED "Comics" ON THE TABLET, by hand, in My Files.
     Spelled exactly that way.

     This one cannot be automated: xochitl exposes no way to create a folder
     remotely — the whole interface is list, download and upload. Without the
     folder, Quire puts downloads at the top of My Files and tells you it did.
EOF
fi

cat >&2 <<EOF

Done. Open the AppLoad launcher on the tablet and tap Quire.

  Sources:   Quire ships none. Add a site by pasting its URL; what you configure,
             and whether you may, is your call (README, "You choose the sources").
  Logs:      Settings, inside the app.
  State:     $STATE_DIR — reinstalling never touches it.
  Uninstall: ./uninstall.sh
EOF
