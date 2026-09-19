#!/bin/sh
# Install Quire on a reMarkable Paper Pro, from nothing, in one command.
#
#     curl -fsSL https://raw.githubusercontent.com/Kuret/quire/main/install.sh | sh
#
# **This runs on your computer and drives the tablet over ssh.** It is not
# copied to the device and run there. That is what makes the password case
# work: the tablet's ssh password can be typed into `ssh` on this side, once,
# and every step after that reuses the same connection.
#
#     ./install.sh [host]        host defaults to $QUIRE_DEVICE, then 10.11.99.1
#     ./install.sh --uninstall   remove Quire; asks before touching your state
#     ./install.sh --help
#
# Quire is an **Annex app**: Annex is the framework that puts it in xochitl's
# sidebar and runs its backend as a service. Without Annex there is nothing to
# install into, so this script checks for it first and offers to run Annex's own
# installer. It never installs Annex behind your back.
#
# Requirements on this side: a POSIX shell, curl, ssh and tar. A Go toolchain is
# needed *only* when you are installing from a checkout; the published release
# ships the backend binary already built for the device.
#
# Requirements on the device: busybox and an ssh server, which is what it ships
# with. Every command sent to the tablet is written for busybox — no `timeout`,
# no `head -N`, no GNU-only flags.
set -eu

# ---------------------------------------------------------------------------
# Constants

# Which published build to install when there is no checkout to build from.
# "latest" asks the GitHub API for the most recent release; set it to a tag to
# pin the install to one known build, which is what you want if two people must
# end up with the same device:
#
#     QUIRE_RELEASE=v0.3.0 sh install.sh
#
RELEASE="${QUIRE_RELEASE:-latest}"
RELEASE_ASSET=quire-rmpp.tar.gz
RELEASE_API=https://api.github.com/repos/Kuret/quire/releases

ANNEX_INSTALLER=https://raw.githubusercontent.com/Kuret/annex/main/install.sh

ANNEX_ROOT=/home/root/annex
APP_DIR="$ANNEX_ROOT/apps/quire"
ENDPOINT="$ANNEX_ROOT/run/quire.json"
INDEX="$ANNEX_ROOT/apps.index"

# Everything you configured and everything Quire learned. Deliberately outside
# $APP_DIR, which this script deletes and recreates on every run.
STATE_DIR=/home/root/.local/share/quire

DEV_USER="${QUIRE_DEVICE_USER:-root}"

# The minimum Go that go.mod will accept. An older toolchain refuses to build
# rather than producing something subtly wrong, so it is checked up front.
GO_MIN_MAJOR=1
GO_MIN_MINOR=25

say()  { echo "quire: $*"; }
warn() { echo "quire: warning: $*" >&2; }
die()  { echo "quire: $*" >&2; exit 1; }
step() { echo; echo "quire: == $*"; }

usage() {
    cat <<'EOF'
Install Quire, a comic downloader, on a reMarkable Paper Pro over ssh.

    install.sh [host]        install (host defaults to $QUIRE_DEVICE, then
                             10.11.99.1 — the USB cable address)
    install.sh --uninstall [host]
                             remove Quire. Asks before deleting your sources
                             and library, and defaults to keeping them.
    install.sh --help        this

Environment:
    QUIRE_DEVICE             default host
    QUIRE_DEVICE_USER        ssh user (default: root)
    QUIRE_RELEASE            release tag to install (default: latest). Only
                             used when there is no checkout to build from.
    GO                       path to a Go toolchain, if it is not on PATH

What this does to your device:

  * requires Annex, the framework Quire is an app of. If it is missing you are
    asked — never assumed — whether to run Annex's installer, which is what
    installs xovi and patches xochitl's sidebar. Say no and this stops.
  * installs the Quire bundle — manifest.json, icon.svg, ui/ and the backend
    binary — into /home/root/annex/apps/quire. That directory is replaced
    wholesale on every run.
  * runs Annex's own annex-index and annex-service sync, so the sidebar sees
    the app and systemd starts and keeps starting its backend.
  * does NOT touch /home/root/.local/share/quire. Your configured sources,
    your watch list and the cover cache live there precisely so that
    reinstalling and upgrading cannot eat them.
  * does not modify xochitl's binary, your documents, or the package manager's
    state. Comics you have already downloaded are ordinary reMarkable
    documents and belong to xochitl, not to Quire.

The backend is a linux/arm64 Go binary and is not in the repository. From a
checkout with Go 1.25+ this builds it; otherwise it downloads the published
build for $QUIRE_RELEASE. If neither is possible it stops and says so rather
than installing an app that cannot start.
EOF
}

# ---------------------------------------------------------------------------
# Prompts
#
# **Every prompt reads /dev/tty, never stdin.** In the advertised one-liner the
# script *is* stdin: a `read` without this consumes the rest of the installer
# and runs half of it. ssh's own password prompt already uses /dev/tty, which
# is why the password case needs nothing special here.
#
# With no controlling terminal — a CI job, a cron line — there is nobody to
# ask, so the default stands and the script says which way it went rather than
# hanging on a tty that will never answer.
ask() {
    prompt="$1"
    default="$2"        # y or n
    if [ ! -r /dev/tty ]; then
        say "no terminal to ask on; assuming \"$default\" for: $prompt"
        [ "$default" = y ]
        return
    fi
    if [ "$default" = y ]; then hint="[Y/n]"; else hint="[y/N]"; fi
    printf 'quire: %s %s ' "$prompt" "$hint" > /dev/tty
    read -r reply < /dev/tty || reply=""
    [ -n "$reply" ] || reply="$default"
    case "$reply" in
        y|Y|yes|YES|Yes) return 0 ;;
        *) return 1 ;;
    esac
}

# ---------------------------------------------------------------------------
# Arguments

MODE=install
HOST=""
for arg in "$@"; do
    case "$arg" in
        --help|-h)   usage; exit 0 ;;
        --uninstall) MODE=uninstall ;;
        -*)          die "unknown option: $arg (try --help)" ;;
        *)           HOST="$arg" ;;
    esac
done
[ -n "$HOST" ] || HOST="${QUIRE_DEVICE:-10.11.99.1}"

# ---------------------------------------------------------------------------
# Working directory and the ssh control socket
#
# Deliberately under /tmp and not $TMPDIR. The control socket path goes into a
# `sockaddr_un`, which is 104 bytes on macOS including the terminator, and
# macOS's $TMPDIR is a ~50-character path under /var/folders — long enough that
# "$TMPDIR/quire.XXXXXX/cm-root@10.11.99.1:22" is uncomfortably close to the
# limit. A socket that is silently not created means every step re-prompts for
# the password, which is the one thing this script exists to avoid.
WORK=$(mktemp -d /tmp/quire-install.XXXXXX) || die "could not create a temp directory"
CM="$WORK/cm-%r@%h:%p"
MASTER_UP=no

cleanup() {
    if [ "$MASTER_UP" = yes ]; then
        ssh -o ControlPath="$CM" -O exit "$DEV_USER@$HOST" >/dev/null 2>&1 || true
        MASTER_UP=no
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# dev runs one command on the tablet over the shared connection. Every remote
# command in this script goes through it, so there is exactly one place where
# the multiplexing options live.
dev() {
    ssh -o ControlPath="$CM" -o ControlMaster=no "$DEV_USER@$HOST" "$@"
}

# ---------------------------------------------------------------------------
# 1. Reach the device, and find out how it wants to authenticate
#
# The probe is a BatchMode ssh, which never prompts. Three outcomes, and they
# are distinguishable, which matters because "cannot connect" and "needs a
# password" have completely different fixes:
#
#   exit 0                     key auth works — the whole install is unattended
#   "Permission denied"        the host answered, so it is reachable and the
#                              only thing missing is credentials
#   anything else              no route, refused, timed out: a cable problem

step "device: $DEV_USER@$HOST"

probe_err="$WORK/probe.err"
AUTH=""
if ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new \
       "$DEV_USER@$HOST" true 2>"$probe_err"; then
    AUTH=key
elif grep -qi 'permission denied\|no supported authentication\|too many authentication' "$probe_err"; then
    AUTH=password
elif grep -qi 'host key verification failed\|remote host identification has changed' "$probe_err"; then
    # An OS update regenerates the tablet's host key, so this is a routine
    # event here rather than a sign of anything sinister — and it would
    # otherwise be reported as "check your cable", which is a fix that cannot
    # work.
    echo "quire: the device's ssh host key has changed, which an OS update does." >&2
    sed 's/^/quire:   /' "$probe_err" >&2 || true
    echo "quire: if that is the explanation, forget the old key and run this again:" >&2
    echo "quire:     ssh-keygen -R $HOST" >&2
    exit 1
else
    echo "quire: cannot reach $DEV_USER@$HOST." >&2
    sed 's/^/quire:   /' "$probe_err" >&2 || true
    cat >&2 <<EOF

quire: things to check, in the order they are usually wrong:
quire:   * the USB cable is plugged in and the tablet is awake
quire:   * 10.11.99.1 is the tablet's USB address; over wifi, pass its IP:
quire:         sh install.sh 192.168.1.42
quire:   * the tablet's ssh password is under Settings -> Help -> Copyrights,
quire:     at the bottom, together with the address it is listening on
quire:   * "USB web interface" must be enabled in Settings -> Storage for the
quire:     USB network to come up at all
quire:   * developer mode must be on, or there is no ssh server to talk to
EOF
    exit 1
fi

if [ "$AUTH" = key ]; then
    say "authenticating with an ssh key — this install is unattended"
else
    say "no ssh key on the device; ssh will ask for its password once"
    say "it is under Settings -> Help -> Copyrights, at the bottom of the page"
fi

# One master connection for the whole run. -f backgrounds it *after*
# authentication, so the password prompt (on /dev/tty, from ssh itself) happens
# here and nowhere else; every `dev` call after this reuses the open channel.
ssh -f -N -o ControlMaster=yes -o ControlPath="$CM" -o ControlPersist=5m \
    -o StrictHostKeyChecking=accept-new "$DEV_USER@$HOST" \
    || die "could not open the ssh connection to $DEV_USER@$HOST"
MASTER_UP=yes
dev true || die "the shared ssh connection did not come up"

# ---------------------------------------------------------------------------
# Uninstall takes the short path out.

uninstall() {
    step "removing Quire"

    # Stop and disable the service BEFORE the directory goes. systemd does not
    # care that annex-app@quire's binary no longer exists: it keeps restarting
    # the unit, failing, and backing off, forever. annex-service disable also
    # removes the stale endpoint file, so nothing is left claiming a port that
    # is not listening. Harmless if the unit was never enabled.
    if dev "[ -x $ANNEX_ROOT/tools/annex-service ]"; then
        dev "$ANNEX_ROOT/tools/annex-service disable quire" \
            || warn "annex-service disable quire reported a problem"
    else
        # No Annex tools — removed already, or never installed. Fall back to
        # systemd rather than leaving a unit running against a directory that
        # is about to be deleted.
        say "Annex's tools are gone; stopping the service with systemctl"
        dev "systemctl disable --now annex-app@quire >/dev/null 2>&1; rm -f $ENDPOINT; true"
    fi

    if dev "[ -d $APP_DIR ]"; then
        dev "rm -rf $APP_DIR" || die "could not remove $APP_DIR"
        say "removed $APP_DIR"
    else
        say "nothing to remove: $APP_DIR does not exist"
    fi

    # The sidebar is built from Annex's index; leaving Quire in it means an
    # entry that opens onto nothing.
    if dev "[ -x $ANNEX_ROOT/tools/annex-index ]"; then
        dev "$ANNEX_ROOT/tools/annex-index" \
            || warn "annex-index failed; run it by hand to drop Quire from the sidebar"
    fi

    # **State is never removed without being asked, and the default is keep.**
    # This is the directory holding the sources somebody configured by hand and
    # the watch list they built up; deleting it because they uninstalled an app
    # is not a decision an installer gets to make quietly.
    if dev "[ -d $STATE_DIR ]"; then
        size=$(dev "du -sh $STATE_DIR 2>/dev/null | cut -f1" || true)
        [ -n "$size" ] || size="?"
        echo
        say "$STATE_DIR ($size) holds your configured sources, your library"
        say "records and the cover cache."
        if ask "Delete those too?" n; then
            dev "rm -rf $STATE_DIR" && say "removed $STATE_DIR"
        else
            say "kept $STATE_DIR — remove it yourself with:"
            say "    ssh $DEV_USER@$HOST 'rm -rf $STATE_DIR'"
        fi
    fi

    echo
    say "Quire is removed. Annex, xovi and qt-resource-rebuilder are untouched:"
    say "this script did not install them."
    say "Comics already in your library stay there — they are ordinary"
    say "reMarkable documents now, and nothing about them depends on Quire."
    echo
    say "xochitl builds the Annex sidebar when it starts, so a Quire entry may"
    say "still be on screen until it restarts:"
    say "    ssh $DEV_USER@$HOST 'systemctl restart xochitl'"
    exit 0
}

[ "$MODE" = install ] || uninstall

# ---------------------------------------------------------------------------
# 2. Annex, which Quire cannot exist without
#
# Annex is two things on the device: the tools that index apps and run their
# backends, and the apps directory itself. Check for one of each — tools with
# no apps directory is a half-install, and an apps directory with no tools
# means nothing is indexed or started.

step "Annex"

annex_present() {
    dev "[ -x $ANNEX_ROOT/tools/annex-index ] && [ -d $ANNEX_ROOT/apps ]"
}

if annex_present; then
    say "Annex is installed at $ANNEX_ROOT"
else
    echo
    say "Annex is not installed on this device."
    say "Annex is the framework Quire runs inside: it adds an entry to"
    say "xochitl's sidebar and runs app backends as systemd services, and Quire"
    say "is one of its apps — on its own it has nowhere to go."
    echo
    if ask "Install Annex now, by running its own installer?" y; then
        say "handing over to Annex's installer at"
        say "    $ANNEX_INSTALLER"
        if [ "$AUTH" != key ]; then
            say "it opens its own ssh connection, so it will ask for the device"
            say "password once more. That is expected."
        fi
        echo
        # Run as a child, with the host passed through. Its prompts read
        # /dev/tty exactly as these do, so it stays interactive even though its
        # own stdin is the pipe it came down.
        # Annex reads the ssh user from its own variable; pass ours through so
        # a non-root device user does not have to be given twice.
        ANNEX_DEVICE_USER="$DEV_USER"
        export ANNEX_DEVICE_USER
        if curl -fsSL "$ANNEX_INSTALLER" | sh -s -- "$HOST"; then
            say "Annex's installer finished"
        else
            warn "Annex's installer exited non-zero"
        fi
        echo
        # Re-checked rather than assumed: the installer may have been declined
        # at one of its own prompts, or stopped on the OS-version warning.
        if annex_present; then
            say "Annex is installed at $ANNEX_ROOT"
        else
            die "Annex still is not installed ($ANNEX_ROOT/tools/annex-index is
       missing or not executable). Nothing of Quire has been put on the
       device. Install Annex first — https://github.com/Kuret/annex — and
       run this again."
        fi
    else
        echo
        say "Stopping, and nothing has been changed on the device."
        say "Quire needs Annex. When you want it:"
        say "    https://github.com/Kuret/annex"
        exit 0
    fi
fi

# Annex's QML patch is a separate question from its tools: without the patch
# there is no sidebar entry to launch Quire from, even though the backend would
# run perfectly well. A warning, not a refusal — the app installs either way and
# the fix belongs to Annex.
if ! dev "[ -e /home/root/xovi/exthome/qt-resource-rebuilder/annex.qmd ]"; then
    warn "Annex's QML patch (annex.qmd) is not on the device, so nothing will"
    warn "appear in the sidebar. Quire will install and its backend will run."
    warn "Re-run Annex's installer to fix it."
fi

# ---------------------------------------------------------------------------
# 3. The bundle, and the one part of it that cannot be shipped in a repository
#
# backend/run is a ~26 MB linux/arm64 Go binary. It is not committed — a
# compiled binary in a source repository is both a large thing to carry and a
# thing nobody can audit — so there are exactly two honest ways to get one:
# build it from the checkout you are standing in, or download the build
# published with a release. If neither is available this stops and explains,
# because the alternative is installing an app directory whose backend is
# missing and letting the device report it as a crash loop.

step "the Quire bundle"

# --- locate a checkout -----------------------------------------------------
#
# Run from a checkout, use the checkout. Run from `curl | sh` there is no
# checkout and $0 is not a path, so fall through to the release. Note the bare
# case: `sh install.sh` from inside the checkout leaves $0 with no slash in it,
# which means the current directory — not "no checkout". Getting that wrong
# sends a script that is standing in a source tree off to the network.
case "$0" in
    */*) HERE_DIR=$(dirname "$0") ;;
    *)   HERE_DIR=. ;;
esac
HERE=$(cd "$HERE_DIR" 2>/dev/null && pwd) || HERE=""

SRC=""
if [ -n "$HERE" ] && [ -f "$HERE/manifest.json" ] && [ -f "$HERE/go.mod" ] \
   && [ -d "$HERE/backend/cmd/quired" ] && [ -d "$HERE/ui" ]; then
    SRC="$HERE"
    say "found a checkout at $SRC"
fi

# --- find a Go toolchain, if there is a checkout to use it on --------------
#
# mise and asdf installs are not on PATH for non-interactive shells, which is
# exactly the shell this runs in, so look where they put things as well.
find_go() {
    if [ -n "${GO:-}" ] && [ -x "$GO" ]; then printf '%s' "$GO"; return 0; fi
    if command -v go >/dev/null 2>&1; then command -v go; return 0; fi
    for candidate in \
        "${HOME:-}"/.local/share/mise/installs/go/*/bin/go \
        "${HOME:-}"/.asdf/installs/golang/*/go/bin/go \
        /usr/local/go/bin/go \
        /opt/homebrew/bin/go
    do
        [ -x "$candidate" ] && { printf '%s' "$candidate"; return 0; }
    done
    return 1
}

# go_ok <go> — true when this toolchain is new enough for go.mod. A version
# string that is not two integers (a devel build, a vendor's odd stamp) is
# accepted with a warning rather than refused: refusing on an unparseable
# string is how a perfectly good toolchain gets rejected.
go_ok() {
    _v=$("$1" version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
    [ -n "$_v" ] || { warn "could not read a version from $1"; return 0; }
    _maj=${_v%%.*}
    _rest=${_v#*.}
    _min=${_rest%%.*}
    case "$_maj$_min" in
        *[!0-9]*|"") warn "unrecognised Go version '$_v' from $1; trying it anyway"; return 0 ;;
    esac
    if [ "$_maj" -gt "$GO_MIN_MAJOR" ]; then return 0; fi
    if [ "$_maj" -eq "$GO_MIN_MAJOR" ] && [ "$_min" -ge "$GO_MIN_MINOR" ]; then return 0; fi
    say "the Go at $1 is $_v; Quire needs $GO_MIN_MAJOR.$GO_MIN_MINOR or newer"
    return 1
}

GOBIN=""
if [ -n "$SRC" ]; then
    if GOBIN=$(find_go); then
        go_ok "$GOBIN" || GOBIN=""
    else
        GOBIN=""
        say "no Go toolchain on this machine"
    fi
fi

BUNDLE=""

# --- path 1: build it ------------------------------------------------------
if [ -n "$SRC" ] && [ -n "$GOBIN" ]; then
    say "building the backend from source with $GOBIN"
    say "this compiles a static linux/arm64 binary and takes a minute or two —"
    say "longer the first time, while Go downloads the modules"
    echo
    # `make rmpp` is the same entry point developers use, so there is one
    # definition of what the device bundle is. make is not universal, so the
    # script underneath it is the fallback; both write output-rmpp/.
    if command -v make >/dev/null 2>&1; then
        GO="$GOBIN" make -C "$SRC" rmpp || die "the build failed — see the output above"
    else
        GO="$GOBIN" "$SRC/build/build-rmpp.sh" || die "the build failed — see the output above"
    fi
    BUNDLE="$SRC/output-rmpp"
    echo
    say "built $BUNDLE"
fi

# --- path 2: download the published build ----------------------------------
if [ -z "$BUNDLE" ]; then
    if [ -n "$SRC" ]; then
        say "there is a checkout here but no usable Go toolchain, so the backend"
        say "cannot be built. Downloading the published build instead."
    fi

    if [ "$RELEASE" = latest ]; then
        API_URL="$RELEASE_API/latest"
        say "asking GitHub for the latest Quire release"
    else
        API_URL="$RELEASE_API/tags/$RELEASE"
        say "asking GitHub for release $RELEASE"
    fi

    ASSET_URL=""
    TAG=""
    if curl -fsSL -H 'Accept: application/vnd.github+json' "$API_URL" -o "$WORK/release.json"; then
        # No jq, and no assumption about the JSON being pretty-printed: split on
        # commas first so each URL lands on its own line either way.
        ASSET_URL=$(tr ',' '\n' < "$WORK/release.json" \
            | sed -n 's|.*"browser_download_url"[ ]*:[ ]*"\([^"]*'"$RELEASE_ASSET"'\)".*|\1|p' \
            | sed -n '1p')
        TAG=$(tr ',' '\n' < "$WORK/release.json" \
            | sed -n 's|.*"tag_name"[ ]*:[ ]*"\([^"]*\)".*|\1|p' \
            | sed -n '1p')
    fi

    if [ -z "$ASSET_URL" ]; then
        # The honest failure. Both halves of it are real situations — a repo
        # with no release yet, and a machine with no toolchain — and neither is
        # fixed by carrying on.
        cat >&2 <<EOF

quire: cannot get the backend binary, so there is nothing to install.

quire: backend/run is a linux/arm64 Go binary of about 26 MB. It is not in the
quire: repository, so it is either built here or downloaded from a release —
quire: and right now neither is possible:

quire:   * no $RELEASE_ASSET was found at
quire:         $API_URL
quire:     If Quire has no published release yet, that is the whole explanation.
EOF
        if [ -n "$SRC" ]; then
            cat >&2 <<EOF
quire:   * this checkout has no Go toolchain new enough to build it. Install Go
quire:     $GO_MIN_MAJOR.$GO_MIN_MINOR or newer from https://go.dev/dl/ (or point \$GO at one) and run
quire:     this again — Go is the only build dependency; Qt is not needed.
EOF
        else
            cat >&2 <<EOF
quire:   * there is no Quire checkout here to build from. Clone it and install
quire:     Go $GO_MIN_MAJOR.$GO_MIN_MINOR or newer from https://go.dev/dl/, then run ./install.sh from
quire:     inside the clone:
quire:         git clone https://github.com/Kuret/quire
quire:         cd quire && ./install.sh $HOST
EOF
        fi
        cat >&2 <<EOF

quire: nothing has been changed on the device.
EOF
        exit 1
    fi

    say "downloading ${TAG:-$RELEASE} $RELEASE_ASSET"
    curl -fsSL "$ASSET_URL" -o "$WORK/$RELEASE_ASSET" \
        || die "could not download $ASSET_URL"

    # Checked here, on this side, so that a truncated download or an HTML error
    # page served with a 200 never reaches the device — where the result is a
    # backend that will not exec, reported as a crash loop in a journal nobody
    # is reading.
    size=$(wc -c < "$WORK/$RELEASE_ASSET" | tr -d ' ')
    [ "$size" -gt 0 ] || die "the downloaded bundle is empty"
    magic=$(od -An -tx1 -N 2 "$WORK/$RELEASE_ASSET" | tr -d ' \n')
    [ "$magic" = "1f8b" ] || die "the downloaded bundle is not gzip (magic $magic) — check $ASSET_URL"

    mkdir -p "$WORK/bundle"
    tar xzf "$WORK/$RELEASE_ASSET" -C "$WORK/bundle" \
        || die "the downloaded bundle is not a readable tar archive"

    # Accept both shapes: the bundle's files at the root of the archive, or
    # wrapped in a single directory, which is what a tar of output-rmpp/ gives.
    if [ -f "$WORK/bundle/manifest.json" ]; then
        BUNDLE="$WORK/bundle"
    else
        for d in "$WORK"/bundle/*/; do
            if [ -f "$d/manifest.json" ]; then BUNDLE="${d%/}"; break; fi
        done
    fi
    [ -n "$BUNDLE" ] || die "the downloaded bundle has no manifest.json — wrong asset?"
    say "unpacked $size bytes from $ASSET_URL"
fi

# --- whichever path got us here, the bundle must be complete ---------------
for required in manifest.json icon.svg ui/Main.qml backend/run; do
    [ -e "$BUNDLE/$required" ] || die "$BUNDLE/$required is missing; the bundle is incomplete"
done
# The device runs backend/run directly, and a tar can carry a mode of 0644 out
# of an archive built carelessly. Fixed here and again on the far side.
chmod +x "$BUNDLE/backend/run" 2>/dev/null || true
say "bundle complete: manifest.json, icon.svg, ui/ and backend/run"

# ---------------------------------------------------------------------------
# 4. Install the app
#
# The app directory is replaced wholesale, which is what makes a second run
# leave exactly the same tree as the first. Nothing the user cares about may
# live under it — and nothing does: state is at $STATE_DIR, outside this,
# precisely because this line once ate somebody's configured sources on a
# routine redeploy.

step "installing into $APP_DIR"

# Stop the backend before replacing its binary. Without this the running
# process keeps its deleted inode, the new binary is never started, and the
# endpoint file on disk points at the old one — which looks exactly like a
# deploy that did nothing.
dev "systemctl stop annex-app@quire >/dev/null 2>&1; true"

# COPYFILE_DISABLE and the two excludes keep macOS AppleDouble files off the
# device: Annex reads the QML from disk by path, so a `._Main.qml` sits
# directly beside the file the host loads.
export COPYFILE_DISABLE=1
tar --exclude='._*' --exclude='.DS_Store' -cf - -C "$BUNDLE" manifest.json icon.svg ui backend \
    | dev "rm -rf $APP_DIR && mkdir -p $APP_DIR && tar -xof - -C $APP_DIR &&
           chmod 0755 $APP_DIR/backend/run" \
    || die "could not copy the bundle to $APP_DIR"

# Repair earlier installs that shipped AppleDouble files, and prove none remain.
leftover=$(dev "find $APP_DIR -name '._*' -delete 2>/dev/null; find $APP_DIR -name '._*' 2>/dev/null" || true)
[ -z "$leftover" ] || warn "AppleDouble files are still present under $APP_DIR:
$leftover"

say "installed the bundle"

# Annex's own tools, not a reimplementation of them: an app is a directory, and
# the framework is what knows what to do with one. annex-index is what the
# sidebar reads; annex-service sync installs the unit template, enables a
# service for every app that has a backend, and starts it.
dev "$ANNEX_ROOT/tools/annex-index" || die "annex-index failed on the device"
dev "$ANNEX_ROOT/tools/annex-service sync" || die "annex-service sync failed on the device"

# ---------------------------------------------------------------------------
# 5. Verify
#
# Three separate questions, because each one fails silently on its own:
#   * is the app directory complete and executable
#   * did the backend actually come up — the endpoint file is the only thing a
#     crash-looping service cannot fake
#   * is Quire in the index the sidebar is built from

step "verifying"

ok=1

if dev "[ -x $APP_DIR/backend/run ] && [ -f $APP_DIR/ui/Main.qml ] && [ -f $APP_DIR/manifest.json ] && [ -f $APP_DIR/icon.svg ]"; then
    say "check: $APP_DIR is complete"
else
    warn "check: $APP_DIR is incomplete — backend/run, ui/Main.qml,"
    warn "       manifest.json and icon.svg should all be there"
    ok=0
fi

# quired writes the endpoint file once it has bound its loopback port, and
# Annex's frontend reads the port and token out of it. systemd starts the
# service asynchronously, so give it a few seconds. The wait is on this side:
# the device has no `timeout`, and one ssh round trip per second doubles as a
# liveness check on the connection.
endpoint_ok=0
n=0
while [ "$n" -lt 15 ]; do
    if dev "[ -f $ENDPOINT ]"; then endpoint_ok=1; break; fi
    n=$((n + 1))
    sleep 1
done
if [ "$endpoint_ok" -eq 1 ]; then
    say "check: the backend published $ENDPOINT"
else
    warn "check: the backend never published $ENDPOINT"
    warn "       the service is installed but is not staying up — usually a"
    warn "       crash on start, and the journal says why in its first few lines"
    ok=0
fi

# Annex's own view of the service, from the tool that owns it. Its line for
# quire reads "quire: active, port N".
status=$(dev "$ANNEX_ROOT/tools/annex-service status" 2>/dev/null || true)
quire_status=""
if [ -n "$status" ]; then
    quire_status=$(printf '%s\n' "$status" | sed -n 's/^quire: //p' | sed -n '1p')
fi
case "$quire_status" in
    active*port*)
        say "check: annex-service reports quire: $quire_status" ;;
    "")
        warn "check: annex-service status has no line for quire"
        warn "       the unit template or the service was never installed"
        ok=0 ;;
    *)
        warn "check: annex-service reports quire: $quire_status"
        ok=0 ;;
esac

# The sidebar is built from this index, one absolute app directory per line.
# Read it here and matched here — a `grep -q` on the far side of an ssh pipe
# closes the pipe early and makes the writing side report a write error
# mid-install.
index=$(dev "cat $INDEX 2>/dev/null" || true)
case "
$index
" in
    *"
$APP_DIR
"*)
        say "check: Quire is in $INDEX, so the sidebar will show it" ;;
    *)
        warn "check: $APP_DIR is not in $INDEX"
        warn "       no sidebar entry will appear; annex-index did not see it"
        ok=0 ;;
esac

# ---------------------------------------------------------------------------
# 6. What is left for a human
#
# Two things Quire genuinely cannot do for itself. Checked rather than
# recited, so a device that is already set up is not nagged.

step "first run"

if dev "grep '^WebInterfaceEnabled=true' /home/root/.config/remarkable/xochitl.conf >/dev/null 2>&1"; then
    say "the USB web interface is on"
else
    cat <<'EOF'

quire: 1. TURN ON THE USB WEB INTERFACE. It is off on a factory device, and
quire:    Quire saves downloaded comics through it — nothing can reach your
quire:    library until it is on.
quire:
quire:        Settings -> Storage -> "USB web interface" -> on, then restart
quire:        the tablet.
quire:
quire:    Quire deliberately does not flip this for you: it only takes effect
quire:    when xochitl restarts, and restarting throws away whatever you were
quire:    reading.

quire: 2. CREATE A FOLDER CALLED "Comics" IN MY FILES, by hand, spelled exactly
quire:    that way. xochitl exposes no way to create a folder remotely — the
quire:    whole interface is list, download and upload. Without it, downloads
quire:    land at the top of My Files and Quire tells you so.
EOF
fi

echo
if [ "$ok" -eq 1 ]; then
    say "installed."
    say "xochitl builds the Annex sidebar when it starts, so if Quire is not in"
    say "it yet, restart xochitl and then open the sidebar and tap Quire:"
    say "    ssh $DEV_USER@$HOST 'systemctl restart xochitl'"
    echo
    say "Your state is untouched: $STATE_DIR holds the sources you"
    say "configured, your library records and the cover cache, and neither this"
    say "install nor any upgrade writes to it."
    echo
    say "Quire ships no sources. Add a site by pasting its URL; what you"
    say "configure, and whether you may, is your call."
    echo
    say "If something misbehaves:"
    say "    ssh $DEV_USER@$HOST '$ANNEX_ROOT/tools/annex-service log quire'"
    say "    ssh $DEV_USER@$HOST '$ANNEX_ROOT/tools/annex-extension check'"
    say "To remove Quire again:  sh install.sh --uninstall"
else
    say "installed, but at least one check above failed."
    say "The backend's own journal says why a service will not stay up, in its"
    say "first few lines:"
    say "    ssh $DEV_USER@$HOST '$ANNEX_ROOT/tools/annex-service log quire'"
    say "And Annex reports whether the layer underneath Quire is healthy — the"
    say "extension, the QML patch and the units:"
    say "    ssh $DEV_USER@$HOST '$ANNEX_ROOT/tools/annex-extension check'"
    echo
    say "The tablet is usable either way: Quire installs nothing into the system"
    say "UI and writes nothing to the root filesystem. 'sh install.sh"
    say "--uninstall' removes what this put there."
    exit 1
fi
