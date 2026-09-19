#!/usr/bin/env bash
# Deploy output-rmpp/ to the device's Annex app directory.
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
ANNEX_ROOT="/home/root/annex"
APP_DIR="$ANNEX_ROOT/apps/quire"
BUNDLE="$REPO_ROOT/output-rmpp"

[[ -d "$BUNDLE" ]] || die "$BUNDLE does not exist; run build/build-rmpp.sh first"
for required in manifest.json icon.svg ui/Main.qml backend/run; do
    [[ -e "$BUNDLE/$required" ]] || die "$BUNDLE/$required is missing; the bundle is incomplete"
done

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST")

say "host    $SSH_USER@$HOST"
"${SSH[@]}" true || die "cannot ssh to $SSH_USER@$HOST"

# The parent must exist before we can install into it; if Annex is not
# installed, say so rather than creating a directory nothing will read.
"${SSH[@]}" "[ -d $ANNEX_ROOT/apps ]" \
    || die "$ANNEX_ROOT/apps does not exist on $HOST — is Annex installed? (run Annex's deploy.sh first)"

# Stop the backend before replacing its binary. Without this the running
# process keeps its deleted inode, the new binary is never started, and the
# endpoint file on disk points at the old one — which looks exactly like a
# deploy that did nothing.
say "stop    annex-app@quire"
"${SSH[@]}" "systemctl stop annex-app@quire 2>/dev/null; true"

say "deploy  $APP_DIR"
# tar over ssh: one round trip, preserves the +x bit on backend/run, and
# needs only busybox tar on the far side.
#
# COPYFILE_DISABLE=1 stops macOS tar emitting AppleDouble "._*" resource-fork
# entries. Without it every file ships with a 163-byte "._name" twin. Under
# AppLoad that was only clutter; under Annex the QML is read from disk by path,
# so a "._Main.qml" sits directly beside the file the host loads.
#
# The rm -rf below replaces the whole app bundle, so NOTHING the user cares
# about may live under $APP_DIR. State lives at /home/root/.local/share/quire
# (see deviceDataDir in backend/cmd/quired/main.go) precisely because this line
# once ate a user's configured sources on a routine redeploy.
COPYFILE_DISABLE=1 tar -C "$BUNDLE" --no-xattrs --exclude='._*' --exclude='.DS_Store' \
    -cf - manifest.json icon.png icon.svg ui backend \
    | "${SSH[@]}" "rm -rf '$APP_DIR' && mkdir -p '$APP_DIR' && tar -xof - -C '$APP_DIR' && chown -R root:root '$APP_DIR' && chmod 0755 '$APP_DIR/backend/run'"

say "verify"
# Repair earlier installs that shipped AppleDouble files, and prove none remain.
"${SSH[@]}" "find '$APP_DIR' -name '._*' -delete 2>/dev/null; leftover=\$(find '$APP_DIR' -name '._*'); if [ -n \"\$leftover\" ]; then echo 'AppleDouble files still present:'; echo \"\$leftover\"; exit 1; fi"

# Re-index so the sidebar picks the app up, and start the backend. Both are
# Annex's, not Quire's: an app is a directory, and the framework is what knows
# what to do with one.
say "index   $ANNEX_ROOT/tools/annex-index"
"${SSH[@]}" "$ANNEX_ROOT/tools/annex-index"
say "start   annex-app@quire"
"${SSH[@]}" "$ANNEX_ROOT/tools/annex-service enable quire"

"${SSH[@]}" "ls -la '$APP_DIR' '$APP_DIR/backend'"
"${SSH[@]}" "$ANNEX_ROOT/tools/annex-service status"

cat >&2 <<EOF

Installed. The backend is running now; the sidebar entry appears when xochitl
next builds it, so restart xochitl:

    ssh $SSH_USER@$HOST 'systemctl restart xochitl'

and then confirm xovi is still injected:

    ssh $SSH_USER@$HOST 'tr "\\0" "\\n" < /proc/\$(pidof xochitl)/environ | grep LD_PRELOAD'

If Quire's screen says the backend is not running:

    ssh $SSH_USER@$HOST '$ANNEX_ROOT/tools/annex-service log quire'
EOF
