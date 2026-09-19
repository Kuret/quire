#!/usr/bin/env bash
# Install Quire on a reMarkable Paper Pro, from a clone of this repository.
#
#   ./install.sh [--host ADDR] [--user NAME] [--verify]
#
#   --host   device address; default $QUIRE_DEVICE, then 10.11.99.1 (USB)
#   --user   ssh user; default $QUIRE_DEVICE_USER, then root
#   --verify re-run the checks and the post-install verification only; build
#            nothing, deploy nothing. Safe to run at any time.
#
# Idempotent: running it twice leaves the same tree as running it once, and it
# never touches /home/root/.local/share/quire — your sources, downloads and
# cover cache live there precisely so that reinstalling cannot eat them.
#
# What it does NOT do: install xovi, qt-resource-rebuilder or Annex. Those are
# prerequisites, and this script checks for them rather than owning them.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source build/common.sh

HOST="${QUIRE_DEVICE:-10.11.99.1}"
SSH_USER="${QUIRE_DEVICE_USER:-root}"
VERIFY_ONLY=0
ANNEX_ROOT="/home/root/annex"
APP_DIR="$ANNEX_ROOT/apps/quire"
ENDPOINT="$ANNEX_ROOT/run/quire.json"
STATE_DIR="/home/root/.local/share/quire"

while [[ $# -gt 0 ]]; do
    case "$1" in
    --host) HOST="${2:?--host needs an address}"; shift 2 ;;
    --user) SSH_USER="${2:?--user needs a name}"; shift 2 ;;
    --verify|--verify-only) VERIFY_ONLY=1; shift ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
    esac
done

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST")
dev() { "${SSH[@]}" "$@"; }

# Problems are collected and reported together: being told about Go, then about
# ssh, then about Annex, one failed run at a time, is a miserable way to install
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

# Annex loads an app's QML as loose files from disk, so there is nothing to
# compile into a resource bundle and no Qt on the host to hunt for.
say "qt      not needed — Annex reads Quire's QML from disk; Go is the only toolchain"

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
3.28.*) say "os      $os_ver (build $os_build)" ;;
"")     say "os      unknown (build ${os_build:-?})"
        printf '  warning: could not read IMG_VERSION from /etc/os-release.\n' >&2 ;;
*)      say "os      $os_ver (build $os_build)"
        # Deliberately a warning, not a refusal. Quire itself ships no QML
        # patch, so there is no version-matched .qmd of ours to gate on; the OS
        # coupling lives in Annex's annex.qmd, and Annex is what would fail to
        # load — visibly, and without taking xochitl with it (see below).
        printf '  warning: Quire has only ever been run on OS 3.28.0.172. Nothing\n' >&2
        printf '           here is version-locked, but if the sidebar entry never\n' >&2
        printf '           appears, suspect Annex against this OS build first.\n' >&2 ;;
esac

dev '[ -e /home/root/xovi/xovi.so ]' \
    || problem "xovi is not installed (/home/root/xovi/xovi.so is missing). Install the xovi stack first — remagic (https://github.com/MaximeRivest/remagic) is the usual way — then re-run this script."
dev '[ -e /home/root/xovi/extensions.d/qt-resource-rebuilder.so ]' \
    || problem "qt-resource-rebuilder is not installed (/home/root/xovi/extensions.d/qt-resource-rebuilder.so). Annex is a qmldiff patch; qt-resource-rebuilder is what applies it."

# Annex is two things on disk: the tools that manage apps, and the qmldiff
# patch that puts them in xochitl's UI. Check for one of each — tools without
# the patch means nothing launches, the patch without the tools means nothing
# is indexed or started.
dev "[ -x '$ANNEX_ROOT/tools/annex-index' ]" \
    || problem "Annex is not installed ($ANNEX_ROOT/tools/annex-index is missing or not executable). Quire is an Annex app; run Annex's own deploy.sh first, then re-run this script."
dev '[ -e /home/root/xovi/exthome/qt-resource-rebuilder/annex.qmd ]' \
    || problem "Annex's QML patch is missing (/home/root/xovi/exthome/qt-resource-rebuilder/annex.qmd). Without it xochitl has no Annex sidebar and nothing can launch Quire. Run Annex's own deploy.sh first."

report_problems

# ------------------------------------ why there is no host compatibility gate
#
# There used to be one here, and it earned its place: AppLoad's hooks referenced
# a *hashed* QML identifier. On an OS build where that hash did not exist,
# qmldiff panicked, xochitl aborted, and the device rebooted — every time the UI
# started. So install.sh refused to install until it could prove, from
# xochitl's journal, that AppLoad's hooks resolved on this exact device.
#
# Annex removes the failure mode rather than detecting it: it deliberately uses
# **zero hashed identifiers**. A selector that matches nothing logs an error and
# returns the original file unchanged, so the worst case is that Annex does not
# load and xochitl runs exactly as it did before. There is no state in which
# installing Quire can cost you a booting tablet, which means there is nothing
# left here worth gating on — and a gate that can only ever say yes is worse
# than none, because it teaches people to skip gates.
#
# The checks that remain (annex-index, annex.qmd, and the endpoint file at the
# end of this script) ask "is Annex there, and did the backend actually come
# up" — questions that still have a real no.

# --------------------------------------------------------------- build/deploy

if (( VERIFY_ONLY )); then
    hr "verify only — not building, not deploying"
else
    hr "build"
    build/build-rmpp.sh

    hr "deploy"
    # install-device.sh is the developer inner loop and the deploy half of this
    # script both; there is one implementation of "put the bundle on the tablet".
    # It also runs Annex's own tools afterwards — annex-index so the sidebar
    # sees the app, and annex-service enable quire so systemd starts and keeps
    # starting the backend. Both belong to Annex, not to Quire, and they are
    # not repeated here: doing them twice in one run proves nothing.
    QUIRE_DEVICE_USER="$SSH_USER" build/install-device.sh "$HOST"
fi

# -------------------------------------------------------------- verification

hr "verify"

dev "[ -x '$APP_DIR/backend/run' ] && [ -f '$APP_DIR/ui/Main.qml' ] && [ -f '$APP_DIR/manifest.json' ] && [ -f '$APP_DIR/icon.png' ]" \
    || die "the app directory on the device is incomplete: $APP_DIR
  Expected backend/run (executable), ui/Main.qml, manifest.json and icon.png.
  Re-run ./install.sh without --verify to deploy the bundle again."
say "files   $APP_DIR complete"

# The endpoint file is the one piece of evidence that the backend is actually
# alive: quired writes it after it has bound its loopback port, and Annex's
# frontend reads the port and token out of it. A service that is enabled but
# crash-looping looks perfectly healthy from the app directory alone; it cannot
# fake this file. Give it a few seconds — systemd starts it asynchronously.
endpoint_ok=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
    if dev "[ -f '$ENDPOINT' ]"; then endpoint_ok=1; break; fi
    sleep 1
done
if (( endpoint_ok )); then
    say "backend published $ENDPOINT"
else
    die "the backend never published $ENDPOINT.
  The service is installed but is not staying up — usually a crash on start.
  Read the journal, which says why in its first few lines:

      ssh $SSH_USER@$HOST '$ANNEX_ROOT/tools/annex-service log quire'

  and check what systemd thinks of every Annex app:

      ssh $SSH_USER@$HOST '$ANNEX_ROOT/tools/annex-service status'"
fi

preload="$(dev 'tr "\0" "\n" < /proc/$(pidof xochitl)/environ 2>/dev/null | grep "^LD_PRELOAD=" || true')"
if [[ "$preload" == *"/home/root/xovi/xovi.so"* ]]; then
    say "xovi    injected ($preload)"
else
    printf '  warning: xochitl is running without xovi (%s).\n' "${preload:-no LD_PRELOAD}" >&2
    printf '           Quire cannot appear in the sidebar until it is. A common\n' >&2
    printf '           cause is the boot unit in multi-user.target.wants being a\n' >&2
    printf '           copy rather than a symlink — docs/DEVICE-NOTES.md §3.\n' >&2
fi

# Annex's own view of the app: service state and port, straight from the tool
# that owns them. Printed rather than judged — it is the same output the user
# will be asked for if anything later goes wrong, and one line of it is worth
# more than this script's guess about what a healthy state looks like.
status="$(dev "$ANNEX_ROOT/tools/annex-service status" 2>/dev/null || true)"
if [[ -n "$status" ]]; then
    printf '%s\n' "$status" | sed 's/^/  /' >&2
else
    printf '  note: %s/tools/annex-service status printed nothing.\n' "$ANNEX_ROOT" >&2
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

Done. The backend is running now. xochitl builds the Annex sidebar when it
starts, so if Quire is not in it yet, restart xochitl:

    ssh $SSH_USER@$HOST 'systemctl restart xochitl'

then open the Annex sidebar on the tablet and tap Quire.

  Sources:   Quire ships none. Add a site by pasting its URL; what you configure,
             and whether you may, is your call (README, "You choose the sources").
  Logs:      Settings, inside the app — or, for the backend itself,
             ssh $SSH_USER@$HOST '$ANNEX_ROOT/tools/annex-service log quire'
  State:     $STATE_DIR — reinstalling never touches it.
  Uninstall: ./uninstall.sh
EOF
