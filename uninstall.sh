#!/usr/bin/env bash
# Remove Quire from a reMarkable Paper Pro.
#
#   ./uninstall.sh [--host ADDR] [--user NAME] [--purge] [--keep-state] [--yes]
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
APP_DIR="/home/root/xovi/exthome/appload/quire"
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
    -h|--help) sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
    esac
done

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST")
dev() { "${SSH[@]}" "$@"; }

dev true >/dev/null 2>&1 || die "cannot ssh to $SSH_USER@$HOST"

if ! dev "[ -d '$APP_DIR' ]"; then
    say "nothing to remove: $APP_DIR does not exist on $HOST"
else
    say "remove  $APP_DIR"
    dev "rm -rf '$APP_DIR'"
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

xovi, qt-resource-rebuilder and AppLoad are left alone: this script did not
install them. Comics already in your library stay there — they are ordinary
reMarkable documents now, and nothing about them depends on Quire.

The AppLoad launcher drops the tile when it next rescans; open it, or restart
xochitl, if the tile is still showing.
EOF
