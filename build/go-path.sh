#!/usr/bin/env bash
# Echo the path of a usable Go toolchain. Used by the Makefile.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
find_go
