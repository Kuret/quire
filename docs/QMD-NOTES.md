# QMD-NOTES.md

Everything here was read out of a resource dump of **this device's own**
`/usr/bin/xochitl`, not from a community patch or a newer firmware. Reproduce
the dump with `tools/rccdump` (see its README); paths below are relative to the
extracted tree at `qmlroot/tree/`, which is `.gitignore`d and must stay that
way.

Line numbers are from the 3.25.1.1 dump and are stable for a given OS build.

## OS 3.25.1.1

### Document open path

| Thing | Type / property | File in resource dump | Notes |
|---|---|---|---|
| `Library.entryForId(id)` | C++ singleton, `import com.remarkable` | — (no QML file) | **The UUID → object converter.** Returns the `Document` entry for a document UUID string. This is what makes opening by UUID possible at all. |
| `MainView.onOpened(args)` | function | `/qml/device/view/main/MainView.qml:79` | Window entry point. Destructures `{ documentId, folderId, settings, page, pageHighlightDetails }` plus `openedFrom`, `analyticsDetails`. Calls `Library.entryForId(documentId)` then `openDocument_helper`. |
| window path | `"legacydevice/window/main"` | `MainView.qml:376`, `Navigator.qml:812`, `Carousel.qml:29`, `guides-window.qml:15` | The path `windowNavigator.open(...)` takes to reach `MainView.onOpened`. Four independent call sites use it. |
| `MainView.openDocument_helper(entry, openCall)` | function | `MainView.qml:222` | Gatekeeper. Defers if the Loader isn't ready; handles archived (needs cloud), `loadError`, and `lockedByPassword` before invoking `openCall`. Skipping it skips those checks. |
| `documentView` | **`Loader`**, `objectName: "DocumentView"`, `asynchronous: true` | `MainView.qml:385-404` | `documentView.item` is the `DocumentView` instance — this is where `.item` in `documentView.item.openDocumentOnPage` comes from. `visible: false` until the state below flips it. |
| `Global.documentViewLoader` | `property Loader` on a singleton | `/qml/device/global/Global.qml`, set at `MainView.qml:394` | Global handle to the same Loader, reachable from anywhere via `import device.global`. |
| state `"DocumentView"` | `when: documentView.item.documentLoaded` | `MainView.qml:346-357` | Sets `documentView.visible = true` and `navigator.visible = false`. Opening a document is therefore enough to bring the reader to the front; no extra visibility poking needed. |
| `DocumentView.openDocument(documentToOpen)` | function, 1 arg | `/qml/device/view/documentview/DocumentView.qml:536` | `documentToOpen` is a **`Document` object, not a UUID**. Delegates to `_open_helper(doc, doc.lastOpenedPage)`. |
| `DocumentView.openDocumentOnPage(documentToOpen, pageToOpen, highlightDetails)` | function, 3 args | `DocumentView.qml:540` | Same `Document` object; `pageToOpen` is a 0-based int; `highlightDetails` may be `null` (it is only used behind `highlightDetails?.hasDetails()`). |
| `DocumentView._open_helper(...)` | function | `DocumentView.qml:467` | Does the real work. Clamps: `pageToOpen = Math.max(0, Math.min(pageToOpen, document.pageCount - 1))`. Sets `Settings.lastOpen = document.id`, calls `LibraryController.loadTableOfContents(document.id)`, `sceneView.goToPage(pageToOpen)`, `document.markOpened()`. |
| `property Document document` | on `DocumentView` | `DocumentView.qml:54` | The currently open document. `document.id` is the UUID. |
| `LibraryController.setLastOpenedPage(id, page)` | C++ singleton | called at `Navigator.qml:859`, `DocumentView.qml:657` | Sets the page a plain `openDocument` will land on. reMarkable's own "open quick sheet on its last page" flow uses exactly this, then opens. |
| `Navigator.requestOpenDocumentOnPage(var entry, var page)` | signal | `/qml/device/view/navigator/Navigator.qml:44`, handled `MainView.qml:375` | Handler forwards `{ documentId: entry.id, page: page }` to `windowNavigator.open("legacydevice/window/main", ...)`. |
| `documentViewListener.openedDocumentId` | string property | `MainView.qml:458` | Kept in sync with the open document's id. Reads `""` when nothing is open. |

#### Answer to PLAN §6 Q2

**Yes — a document can be opened by UUID from QML on 3.25.1.1, and the page can
be chosen.** There are two routes.

1. **Through the window navigator (what xochitl itself does).** One call, and it
   goes through every safety check in `openDocument_helper`:

   ```js
   windowNavigator.open("legacydevice/window/main", {
       documentId: "<uuid>",          // string UUID, not an object
       page: 12,                      // 0-based
       openedFrom: "quire"            // free-form, analytics only
   })
   ```

2. **Directly on the Loader**, from anywhere that can `import device.global`:

   ```js
   Global.documentViewLoader.item.openDocumentOnPage(Library.entryForId("<uuid>"), 12, null)
   ```

   This honours the page but bypasses the archived / load-error / password
   checks in `openDocument_helper`. Prefer route 1 unless it proves unreachable
   from an AppLoad window.

**Trap — `page` alone does nothing on route 1.** `MainView.qml:88` reads:

```js
const openDocument_cb = (page == null || page < 0 || !pageHighlightDetails) ? () => documentView.item.openDocument(document)
                                            : () => documentView.item.openDocumentOnPage(document, page, pageHighlightDetails);
```

`page` is ignored unless `pageHighlightDetails` is *also* truthy — and
`pageHighlightDetails` is a search-highlight object (`hasDetails()`,
`currentSearchText()`), not something we have. So `{documentId, page}` falls
through to `openDocument()`, which opens at `document.lastOpenedPage`.
xochitl's own code works around this the same way we should:

```js
LibraryController.setLastOpenedPage(documentId, page);   // then
windowNavigator.open("legacydevice/window/main", { documentId: documentId });
```

(`Navigator.openLastQuickSheet`, `Navigator.qml:857-860`.)

A QMLDiff patch may therefore not be needed to *open* anything — only to get a
handle on `windowNavigator` from our own window. Confirm that before writing one
(PLAN §7.8: "Prefer a single `REDEFINE` or injection point over several"; the
smallest patch is no patch).

### Differences from newer-version reference patches

To be filled in during M6, when a reference `.qmd` written against newer
firmware is read as a map (PLAN §7.8, step 2 of "Finding the right hook"). The
rows below are the differences already established.

| What | 3.25.1.1 | Newer | Consequence |
|---|---|---|---|
| AppLoad release | v0.4.2 — v0.5.3's main-UI hook hashes do not resolve here | v0.5.3 | Pinned in the installer; see `docs/DEVICE-NOTES.md` §2 |
| Qt | 6.8.2 | — | QML is source in the qrc, not `qmlcachegen` units, so `qmldiff` sees text |
| `MainView` root element | `Background` (`/qml/common/Background.qml`) | — | A `TRAVERSE FocusScope` selector copied from a `DocumentView` patch will not match `MainView` |

## Host-side loop

Proven end to end on the host; nothing here has been deployed to the device.

| Step | Command |
|---|---|
| 1. dump the tree | `go run ./tools/rccdump -out qmlroot/tree qmlroot/xochitl` |
| 2. build a hashtab | `qmldiff create-hashtab qmlroot/tree qmlroot/hashtab.host` |
| 3. write the patch unhashed | this is what lives in git |
| 4. apply to a copy | `qmldiff apply-diffs qmlroot/tree qmlroot/tree-patched -c patch.qmd` |
| 5. hash it (build time, not by hand) | `qmldiff hash-diffs qmlroot/hashtab.device patch.qmd` — **edits in place**, so hash a copy |

`qmldiff` is `asivery/qmldiff`, built with `cargo build --release`; the binary is
`target/release/qmldiff`. It builds clean on macOS/aarch64.

Gotchas found while proving the loop:

- **File paths in `AFFECT` must use backticks, not double quotes.** The lexer
  keeps `"` and `'` as part of the string value, so `AFFECT "/qml/..."` looks
  for a file whose name literally contains quote characters and fails with
  *"file ... does not exist"*. `` AFFECT `/qml/device/view/main/MainView.qml` ``
  works. A leading `/` is stripped when resolving against the tree root.
- Blocks close with `END TRAVERSE` **and** `END AFFECT`; omitting the latter
  gives *"expected END directive, got EoF"*.
- `hash-diffs` leaves a backtick `AFFECT` path unhashed and hashes identifiers
  and string literals inside the block.
- The re-emitted QML is reformatted (MainView goes 660 → 782 lines) even where
  nothing changed. Diff the *patched* tree against a *re-emitted unpatched*
  tree, not against the dump, or the noise will bury the change.

## OS 3.28.0.172

### `entry.id` is an object, and stringifying it is a silent no-op

Measured on hardware 2026-09-18 by dumping an entry wrapper from inside an
Annex app:

```
[quire] entry for 11fd53c9-... is QmlDocumentWrapper(0xdf4c410)
[quire]   .id = 11fd53c9-ecbf-4fa4-bd51-75a09a2bb31f  (object)
[quire]   .visibleName = I Fell for My Friend's Older Sister - Ch 0002.pdf  (string)
```

`Library.entryForId(uuid).id` is **not a string**. It is a wrapper whose
`toString()` is the uuid, which is why the distinction stayed invisible for so
long: on 3.25 and 3.27 the id and the uuid genuinely were the same string.

Every `LibraryController` method that takes entries wants that object.  Hand it
the stringified form and the call is **accepted, does nothing, and reports
nothing**. The only evidence is in xochitl's own log, which the app cannot see:

```
rm.library.controller  moving "" to trash (moveEntryToTrash .../librarycontroller.cpp:453)
```

The empty string in that line is the entry the controller resolved our argument
to. Affected, all through one shared helper:

| Call | Argument that must stay an object |
|---|---|
| `LibraryController.moveEntriesToTrash(ids)` | each id |
| `LibraryController.deleteEntries(ids)` | each id |
| `LibraryController.moveEntries(ids, destination)` | each id **and** the destination |

`Library.createCollectionWrapper(parentUuidString, name)` is the exception: it
takes and returns plain uuid strings, and worked throughout.

**The rule this leaves behind.** Resolve uuid to id at the call, in
`ReaderHandoff.qml`, and let no wrapper object out of that file. Everything
above it — `Sorting.js`, `Deleting.js`, the backend records — stays plain
strings, so no upstream string handling can break an id it never holds.

Note also that this is invisible to `make check`: the harness fakes the device
API, and the bug lives in the real boundary. Two OS bumps have now broken this
exact seam (`com.remarkable` disappearing on 3.28, and this), and it is the one
part of the app no test reaches.

### A trashed entry's parent is the literal `"trash"`

`Library.parentIdForId(uuid)` returns `"trash"` — not a uuid — once the entry is
in the Trash. Measured 2026-09-18:

```
[quire] parentOf(e3259d7b-...) = "trash"
```

That is the comparison `Deleting.js` makes to tell "it reached the Trash" from
"the move silently did nothing", and it had never actually been exercised on
this OS: every delete here was failing at the object-id problem above, well
before anything got this far.

### The resource dump is not always faithful

`rccdump` extracted `NavigatorWindow.qml` and `GesturesWindow.qml` as
byte-identical files. They are not. The same qmldiff selector matched one and
failed on the other, which two identical files cannot both do — so the dump had
misattributed one file's contents.

Treat `qmlroot328/tree` as a strong hint about what xochitl runs, not as
evidence. Where it matters, patch and read the device log: qmldiff names every
file it processes and reports a selector that does not match, and an unmatched
selector is a safe failure.
