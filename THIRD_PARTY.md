# Third-party software

## MuPDF 1.28.4

Quire's book reader renders pages by running `mutool run <script.js>` as a
separate, long-lived child process (see `backend/bookrender`). The `mutool`
binary bundled with Quire is built, unmodified, from MuPDF's own source:

- Source: <https://mupdf.com/downloads/archive/mupdf-1.28.4-source.tar.gz>
- SHA-256: `2d97e043a616f96b148657c9c3d81ad71c4bd2052c59a2a3315ad842599340f9`
- Copyright © Artifex Software, Inc. All rights reserved.
- Licence: GNU Affero General Public License v3.0 or later (AGPL-3.0-or-later).
  The full licence text ships alongside the binary as
  `licenses/MuPDF-COPYING` and is reproduced upstream at
  <https://www.gnu.org/licenses/agpl-3.0.html>.

Built by `build/mupdf.sh`, which downloads the tarball above into
`build/cache/` (gitignored), verifies it against the SHA-256 pinned in that
script before building anything, then cross-compiles `mutool` with zig:

```
make OS=Linux build=release HAVE_X11=no HAVE_GLUT=no HAVE_CURL=no HAVE_OBJCOPY=no \
    HAVE_LIBCRYPTO=no CC="zig cc -target aarch64-linux-musl" CXX="zig c++ -target \
    aarch64-linux-musl" AR="zig ar" LD="zig cc -target aarch64-linux-musl" \
    XLDFLAGS=-static tools
```

This produces a fully static `aarch64-linux-musl` executable with no shared
library dependency the device might lack. `build/build-rmpp.sh` copies it,
unmodified, into the app bundle as `backend/mutool` and copies its licence as
`licenses/MuPDF-COPYING`; nothing in Quire links against MuPDF or embeds any
of its code — it is invoked as a separate subprocess and communicated with
over stdin/stdout.

`build/mupdf.sh host` builds a second, native copy for this machine
(`build/cache/mutool-1.28.4-host`) that is never shipped; it exists only so
`backend/bookrender`'s optional integration test can exercise the real
`mutool` when `QUIRE_MUTOOL` points at it.
