#!/usr/bin/env bash
# Deploy output-rmpp/ to the device's AppLoad app directory.
#
#   build/install-device.sh [host]
#
# host defaults to $QUIRE_DEVICE, then to the USB address 10.11.99.1. The wifi
# address works too if it is reachable.
#
# Device-side requirements: busybox (sh, tar, mkdir, rm, chmod) and an ssh
# server. Nothing else — the device has no rsync, no curl and no python3, and
# its coreutils are busybox applets (so: `head -n 20`, never `head -20`).
#
# Idempotent: the app directory is replaced wholesale each run, so a second run
# leaves exactly the same tree as the first.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

HOST="${1:-${QUIRE_DEVICE:-10.11.99.1}}"
SSH_USER="${QUIRE_DEVICE_USER:-root}"
APP_DIR="/home/root/xovi/exthome/appload/quire"
BUNDLE="$REPO_ROOT/output-rmpp"

[[ -d "$BUNDLE" ]] || die "$BUNDLE does not exist; run build/build-rmpp.sh first"
for required in manifest.json icon.png resources.rcc backend/entry; do
    [[ -e "$BUNDLE/$required" ]] || die "$BUNDLE/$required is missing; the bundle is incomplete"
done

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST")

say "host    $SSH_USER@$HOST"
"${SSH[@]}" true || die "cannot ssh to $SSH_USER@$HOST"

# The parent must exist before we can install into it; if AppLoad is not
# installed, say so rather than creating a directory nothing will read.
"${SSH[@]}" "[ -d /home/root/xovi/exthome/appload ]" \
    || die "/home/root/xovi/exthome/appload does not exist on $HOST — is AppLoad installed?"

say "deploy  $APP_DIR"
# tar over ssh: one round trip, preserves the +x bit on backend/entry, and
# needs only busybox tar on the far side.
tar -C "$BUNDLE" -cf - manifest.json icon.png resources.rcc backend \
    | "${SSH[@]}" "rm -rf '$APP_DIR' && mkdir -p '$APP_DIR' && tar -xof - -C '$APP_DIR' && chown -R root:root '$APP_DIR' && chmod 0755 '$APP_DIR/backend/entry'"

say "verify"
"${SSH[@]}" "ls -l '$APP_DIR' '$APP_DIR/backend'"

cat >&2 <<EOF

Installed. AppLoad rescans app directories when the launcher is opened; if the
tile does not appear, restart xochitl:

    ssh $SSH_USER@$HOST 'systemctl restart xochitl'

and then confirm xovi is still injected:

    ssh $SSH_USER@$HOST 'tr "\\0" "\\n" < /proc/\$(pidof xochitl)/environ | grep LD_PRELOAD'
EOF
