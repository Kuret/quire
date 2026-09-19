#!/usr/bin/env bash
# Remove Quire from a reMarkable Paper Pro.
#
#   ./uninstall.sh [ADDR] [--user NAME] [--purge] [--keep-state] [--yes]
#
# The host is positional, matching install.sh; --host ADDR is still accepted
# so anything already scripted against it keeps working.
#
#   --purge       also delete Quire's state: configured sources, the library
#                 records, the cover cache and any part-finished download.
#   --keep-state  never ask; leave state alone. This is the default behaviour
#                 when the answer is anything but an explicit yes.
#   --yes         do not prompt at all (implies --keep-state unless --purge).
#
# State is left in place unless you say otherwise. Downloaded comics already in
# your library are xochitl's, not Quire's, and are never touched by this script.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source build/common.sh

HOST="${QUIRE_DEVICE:-10.11.99.1}"
SSH_USER="${QUIRE_DEVICE_USER:-root}"
ANNEX_ROOT="/home/root/annex"
APP_DIR="$ANNEX_ROOT/apps/quire"
STATE_DIR="/home/root/.local/share/quire"
PURGE=""
ASSUME_YES=0

while [[ $# -gt 0 ]]; do
    case "$1" in
    --host) HOST="${2:?--host needs an address}"; shift 2 ;;
    --user) SSH_USER="${2:?--user needs a name}"; shift 2 ;;
    --purge) PURGE=1; shift ;;
    --keep-state) PURGE=0; shift ;;
    --yes|-y) ASSUME_YES=1; shift ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    # A bare address is the host, matching install.sh's positional form.
    -*) die "unknown option: $1 (try --help)" ;;
    *)  HOST="$1"; shift ;;
    esac
done

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST")
dev() { "${SSH[@]}" "$@"; }

dev true >/dev/null 2>&1 || die "cannot ssh to $SSH_USER@$HOST"

# Stop and disable the service BEFORE the directory goes. systemd does not
# care that annex-app@quire's binary no longer exists: it keeps restarting the
# unit, failing, and backing off, forever. annex-service disable also removes
# the stale endpoint file, so nothing is left claiming a port that is not
# listening. Harmless if the unit was never enabled.
if dev "[ -x '$ANNEX_ROOT/tools/annex-service' ]"; then
    say "disable annex-app@quire"
    dev "$ANNEX_ROOT/tools/annex-service disable quire" >/dev/null 2>&1 \
        || say "note    annex-service disable quire failed; check '$ANNEX_ROOT/tools/annex-service status'"
else
    # No Annex tools (removed already, or never installed). Fall back to
    # systemd directly rather than leaving a unit running against a directory
    # this script is about to delete.
    say "disable annex-app@quire (via systemctl; Annex's tools are gone)"
    dev "systemctl disable --now annex-app@quire 2>/dev/null; rm -f '$ANNEX_ROOT/run/quire.json'; true"
fi

if ! dev "[ -d '$APP_DIR' ]"; then
    say "nothing to remove: $APP_DIR does not exist on $HOST"
else
    say "remove  $APP_DIR"
    dev "rm -rf '$APP_DIR'"
fi

# The sidebar is built from Annex's index; leaving Quire in it means a tile
# that opens onto nothing.
if dev "[ -x '$ANNEX_ROOT/tools/annex-index' ]"; then
    say "index   $ANNEX_ROOT/tools/annex-index"
    dev "$ANNEX_ROOT/tools/annex-index" >/dev/null 2>&1 \
        || say "note    annex-index failed; run it by hand to drop Quire from the sidebar"
fi

if dev "[ -d '$STATE_DIR' ]"; then
    size="$(dev "du -sh '$STATE_DIR' 2>/dev/null | cut -f1" || true)"
    if [[ -z "$PURGE" ]]; then
        if (( ASSUME_YES )) || [[ ! -t 0 ]]; then
            PURGE=0
        else
            printf '\n%s holds your configured sources, library records and cover\ncache (%s). Delete it too? [y/N] ' "$STATE_DIR" "${size:-?}" >&2
            read -r answer
            case "$answer" in [yY]|[yY][eE][sS]) PURGE=1 ;; *) PURGE=0 ;; esac
        fi
    fi
    if [[ "$PURGE" == 1 ]]; then
        say "remove  $STATE_DIR (${size:-?})"
        dev "rm -rf '$STATE_DIR'"
    else
        say "kept    $STATE_DIR (${size:-?}) — delete it yourself, or re-run with --purge"
    fi
fi

cat >&2 <<EOF

Quire is removed from $HOST.

xovi, qt-resource-rebuilder and Annex are left alone: this script did not
install them. Comics already in your library stay there — they are ordinary
reMarkable documents now, and nothing about them depends on Quire.

xochitl builds the Annex sidebar when it starts, so a Quire tile may still be
on screen until it restarts:

    ssh $SSH_USER@$HOST 'systemctl restart xochitl'
EOF
