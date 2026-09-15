# rccdump

Extracts the Qt resource (rcc) trees compiled into a stripped ELF binary, so
xochitl's QML can be read and patched on the host instead of by trial and error
on the device (PLAN §7.8).

xochitl's QML is **source**, not compiled `.qmlc` units: every `.qml` in the
resource tree comes out as readable text.

## Re-running it after an OS change

Everything lives under `qmlroot/`, which is `.gitignore`d. The extracted tree is
proprietary reMarkable code — it must never be committed.

```sh
mkdir -p qmlroot
scp root@10.11.99.1:/usr/bin/xochitl qmlroot/xochitl
rm -rf qmlroot/tree
go run ./tools/rccdump -out qmlroot/tree qmlroot/xochitl
```

On OS 3.25.1.1 this yields 147 resources / 1521 files, of which 1496 extract
(the 25 failures are duplicates of files recovered under their other alias —
see "Known gaps"). 803 of them are `.qml`.

Qt's own QML (`/qt-project.org/imports/QtQuick/Controls/...`) is **not** in
xochitl; it lives in the Qt libraries. Point the tool at those too if you need
them:

```sh
scp root@10.11.99.1:'/usr/lib/libQt6QuickControls2*.so.6.8.2' qmlroot/
go run ./tools/rccdump -out qmlroot/tree qmlroot/libQt6QuickControls2*.so.6.8.2
```

### Verifying the extraction

Build a hashtab from the extracted tree and compare it against the device's own
(`/home/root/xovi/exthome/qt-resource-rebuilder/hashtab`). Every hash the host
produces for a file we extracted must be present in the device's table; that
proves the recovered source is the source xochitl actually loads.

```sh
qmldiff create-hashtab qmlroot/tree qmlroot/hashtab.host
```

On 3.25.1.1 the host table covers 18979 of the device's 19826 entries (95.7%).
The remainder are Qt's Fusion/Universal style QML and identifiers registered at
runtime, neither of which is in xochitl.

## How it finds the resources

Qt only writes the `qres` magic header for a standalone `.rcc` file. When rcc
output is compiled into the executable, the three arrays it generates end up in
`.rodata` as anonymous blobs — no magic, and no symbols in a stripped binary.
`rccdump` therefore locates each section by shape:

- **tree** — fixed-size big-endian nodes; node 0 is the root (name offset 0,
  directory flag, first child index 1), and a valid tree numbers its nodes
  densely from 0, which is what rejects false roots.
- **names** — `u16` length, `u32` hash, UTF-16BE text. The hash is Qt's own
  28-bit string hash, so a candidate entry can be verified outright. The names
  base is the single offset at which *every* name offset in the tree decodes.
- **data** — `u32` length then payload. Entries are laid out back to back, so
  the gap between the two lowest data offsets in the tree equals 4 plus the
  length stored at the lower one — a value that can be searched for without
  knowing the base, then confirmed against the whole chain and against each
  entry's compression magic.

Payloads are raw, zlib (with a `u32` uncompressed size in front of the stream)
or zstd (framed, no size prefix), per the node's flags.

Written from the format description alone. No code from any other project was
copied (PLAN §1.4).

## Known gaps

- 25 of 1521 entries sit in resources holding a single data entry, where the
  offset chain gives nothing to confirm a base against. All 6 `.qml` files among
  them are recovered from their `/qt/qml/...` alias of the same module, so no
  QML source is actually lost. The rest are images, a `.qm` and a `.md`.
- Qt's built-in QML is in the Qt libraries, not in xochitl (see above).
