# THEME-NOTES.md

Per-theme fingerprints, endpoint shapes, `overrides` keys and quirks.
PLAN §7.3: *"That file is what makes theme #6 take an afternoon instead of a
weekend."*

Per-theme sections are added as each theme is implemented (M2). The taxonomy
below is the answer to PLAN §11 Q6 and is the input to the M2 build order.

---

## Q6 — the authoritative theme taxonomy

Enumerated 2026-09-15 from both ecosystems directly, not from memory.

| Source | Path | Count |
|---|---|---|
| `keiyoushi/extensions-source` | `lib-multisrc/` | **68** |
| Komikku (GNOME) | `komikku/servers/multi/` | **18** |

> **PLAN correction.** §3.1 states "61 such themes". The current count is **68**.
> Treat any number in the plan as a snapshot; re-enumerate before M2 work.

> **PLAN correction — Komikku has moved.** §4 lists "Komikku (GNOME)" without a
> URL. It is **not** on GitHub: `github.com/komikku-app/komikku` is an unrelated
> **Android** Mihon/Tachiyomi fork with a different structure and licence, and is
> an easy thing to read by mistake. The GNOME project has also **left GitLab**
> (`gitlab.com/valos/Komikku` now 302s). It lives at
> **`codeberg.org/valos/Komikku`**. Its themes are *directories* under
> `komikku/servers/multi/`, not `.py` files.

### keiyoushi `lib-multisrc/` — all 68

```
aurora, bakkin, clipstudioreader, colorlibanime, comiciviewer, eromuse,
ezmanhwa, fansubscat, fmreader, foolslide, fuzzydoodle, galleryadults, gattsu,
gigaviewer, goda, greenshit, grouple, guya, heancms, hentaihand, hiper,
hwalumi, iken, initmanga, inkstory, kemono, keyoapp, libgroup, liliana, madara,
madaralegacy, madtheme, manga18, mangabox, mangacatalog, mangadventure,
mangahub, mangak, mangareader, mangataro, mangathemesia, mangawork, mangaworld,
mangotheme, manhwaz, masonry, mccms, mmlook, mmrcms, monochrome, moonlighttl,
multichan, natsuid, oceanwp, origines, pam, pizzareader, scanreader, senkuro,
sinmh, spicytheme, stalkercms, uzaymanga, vercomics, vinetheme, wpcomics,
zeistmanga, zmanga
```

**New since the plan was written: `madaralegacy`.** Madara has forked into
current and legacy variants upstream. Expect our `madara` theme to need a
variant switch — most likely an `overrides` key rather than a second theme.
Confirm which shape a probed site is before assuming.

### Komikku `servers/multi/` — all 18

```
foolslide, fuzzydoodle, genkan, guya, heancms, hiveworks, iken, keyoapp,
madara, madtheme, manga_stream, mangareader, my_manga_reader_cms, pizzareader,
paprika, peachscan, wpcomics, zeistmanga
```

### The intersection — PLAN §7.3's "safest bets"

Naive name intersection gives 12:

```
foolslide, fuzzydoodle, guya, heancms, iken, keyoapp, madara, madtheme,
mangareader, pizzareader, wpcomics, zeistmanga
```

> ⚠️ **The raw name match understates the overlap — the two ecosystems name the
> same theme differently.** Do not read a missing name as a missing theme:
>
> | keiyoushi | Komikku | same thing? |
> |---|---|---|
> | `mangathemesia` | `manga_stream` | **Yes** — PLAN §7.3 itself notes mangathemesia was "formerly WPMangaStream" |
> | `mmrcms` | `my_manga_reader_cms` | **Yes** — "My Manga Reader CMS" |
>
> With those folded in, the true intersection is **14**, and it includes *both*
> of the plan's tier-1 themes.

### Verdict on the plan's build order

**PLAN §7.3's tiers survive contact with the real data — build as written.**

| Plan tier | Theme | In keiyoushi | In Komikku | Verdict |
|---|---|---|---|---|
| 1 | `madara` | ✅ | ✅ | **Confirmed.** Build first. Watch for the `madaralegacy` split |
| 1 | `mangathemesia` | ✅ | ✅ (as `manga_stream`) | **Confirmed.** Build second — structurally very different from madara, which is what M2's acceptance test wants |
| 2 | `heancms` | ✅ | ✅ | Confirmed |
| 2 | `iken` | ✅ | ✅ | Confirmed |
| 2 | `keyoapp` | ✅ | ✅ | Confirmed |
| 3 | `mmrcms` | ✅ | ✅ (as `my_manga_reader_cms`) | Confirmed |
| 3 | `wpcomics` | ✅ | ✅ | Confirmed |
| 3 | `madtheme` | ✅ | ✅ | Confirmed |
| 3 | `zeistmanga` | ✅ | ✅ | Confirmed |
| 3 | `fmreader` | ✅ | ❌ | keiyoushi only — lower confidence than its tier-3 peers |
| 4 | `guya` | ✅ | ✅ | In both despite tier 4 |
| 4 | `genkan` | ❌ | ✅ | **Gone from keiyoushi.** Plan already calls it "largely defunct" — confirmed, do not build |
| 4 | `nepnep` | ❌ | ❌ | **Gone from both.** Plan calls it "largely defunct" — confirmed, do not build |
| 4 | `readerfront` | ❌ | ❌ | In neither. Drop |
| 4 | `etoshore` | ❌ | ❌ | In neither. Drop |
| 4 | `liliana` | ✅ | ❌ | keiyoushi only |
| 4 | `greenshit` | ✅ | ❌ | keiyoushi only |
| 4 | `paprika` | ❌ | ✅ | Komikku only |
| 4 | `peachscan` | ❌ | ✅ | Komikku only |

**M2 plan of record:** implement `madara` end to end first, then
`mangathemesia`. Those two satisfy M2's acceptance test ("two structurally
different themes") and are the two highest-volume families in both ecosystems.
Reassess after tier 1, exactly as §7.3 instructs.

### How these lists were produced

```sh
# keiyoushi (GitHub API)
curl -s "https://api.github.com/repos/keiyoushi/extensions-source/contents/lib-multisrc"

# Komikku (Codeberg, sparse clone — themes are directories)
git clone --depth 1 --filter=blob:none --sparse \
    https://codeberg.org/valos/Komikku.git
git -C Komikku sparse-checkout set komikku/servers
ls -d Komikku/komikku/servers/multi/*/
```

Re-run both before starting any new theme; the landscape drifts.

---

## Licence boundary — read this before opening either repo

PLAN §1.4 governs. keiyoushi is Apache-2.0; **Komikku is GPL-3.0**; Quire is
Apache-2.0.

Both are read **for facts about the sites**, never as a parts bin: which URL
shapes a family uses, where the chapter list actually lives, what the page-list
blob looks like. Those facts are not copyrightable. The implementation is
written from scratch against observed HTTP traffic and observed HTML from a
live site.

Concretely, for each theme, record *here* — in our own words — the fingerprint
signals, endpoint shapes and quirks. Never transcribe a selector list, a regex,
or a parsing routine out of either project.

---

## What the fixtures do and do not prove

**Read this before trusting a green test run.** It is the single most
important caveat in this file, and it is stated plainly because overclaiming
here would mislead whoever adds theme #6.

PLAN §6 M2 asks for recorded HTTP responses, committed once and tested offline
forever. PLAN §1.3 forbids this repository from containing source URLs or
anything that functions as a catalogue, and §1.4 forbids copying others'
expression. Those two cannot both be satisfied literally: recording a real
aggregator's HTML would put its domain and its content in the repo.

So the fixtures under each theme's `testdata/` are **synthetic but faithful**:

- They are written by us, from scratch, to reproduce the *markup shape* a
  published piece of software generates. Madara is a commercially distributed
  WordPress plugin and MangaThemesia a distributed WordPress theme; the shape
  of their output is an observable, documented fact about that software, not
  anyone's expression.
- Every host in them is `example.invalid` (RFC 2606), which can never resolve.
- Every file opens with a header saying what shape it reproduces, why it is
  synthetic, and which deliberate variations the tests exercise.
- No aggregator domain appears anywhere in this repository — not in a fixture,
  a filename, a test name or a comment.

**What that buys us:** the fixtures pin *our parser's* behaviour. A refactor
that breaks lazy-image handling, chapter-number parsing or the ts_reader blob
extraction fails immediately and offline. Every test in `backend/theme/...`
runs with DNS disabled (`backend/internal/nonet`), so a future "just point it
at a real site to check" is a red test rather than a quiet network call.

**What that does not buy us:** the fixtures prove nothing about whether any
live site is parsed correctly. They cannot. A site could change its markup
tomorrow, or could have shipped a skin we never anticipated, and every test
here would still pass.

What proves a live site works is **PLAN §7.5 stage 5's capability check, run at
runtime against whatever site the user chooses to add** — and, when a working
source later goes quiet, the re-probe of PLAN §6 M7. Those are the real
verification. These fixtures are regression protection for our own code, and
should be described as nothing more.

M2's acceptance criterion is met *as far as it can be met offline*: two
structurally different themes, search → series → chapters → page URLs working
against fixtures for both, and a third site added by config alone. The clause
"recorded real HTTP responses" is the one part implemented differently, for the
reason above.

---

## Per-theme notes

### `madara` — WordPress `wp-manga` plugin

Implemented in `backend/theme/madara/`. PLAN §7.3 tier 1, and the largest
family by a wide margin.

**Fingerprint signals**, in descending weight. Scores are summed and clamped to
0..100; the probe treats 60 as the confidence threshold.

| Signal | Weight | Why it is worth that much |
|---|---|---|
| `/wp-content/plugins/madara/` in the body | 40 | The plugin's own asset path. Nothing else emits it. Near-conclusive on its own. |
| `wp-manga` anywhere in the body | 25 | The custom post type, which leaks into body classes, search forms and REST links. |
| `manga_get_chapters` | 20 | The AJAX action name, present in inline script on series pages of both shapes. |
| `#manga-chapters-holder` | 15 | The chapter container, present whether or not the list has loaded. |
| `li.wp-manga-chapter` | 15 | One chapter row. |
| `.c-tabs-item__content` | 10 | Listing/search card. |
| `.reading-content .page-break` | 10 | Reader page wrapper. |
| `.c-blog__heading` | 8 | Section heading. |
| `input.rating-post-id` | 8 | Carries the WordPress post ID. |
| `.post-title h1/h3/h4` | 5 | Weak on its own; useful as a tie-breaker. |
| `.manga-title-badges, .summary_image` | 5 | Ditto. |

Comments are stripped from the body before any substring signal is tested
(`probe.Page.Contains`). A signal found in a comment is not evidence: sites
carry commented-out markup from the theme they used to run, and without this a
page that merely *mentions* another theme gets assigned to it.

**Endpoint shapes**

| Operation | Request |
|---|---|
| Search, page 1 | `GET /?s={q}&post_type={searchPostType}` |
| Search, page N | `GET /page/{N}/?s={q}&post_type={searchPostType}` |
| Series | `GET /{mangaSubPath}/{slug}/` |
| Chapters (current) | `POST /{mangaSubPath}/{slug}/ajax/chapters/`, empty body |
| Chapters (legacy) | `POST /wp-admin/admin-ajax.php` with `action=manga_get_chapters&manga={postID}` |
| Chapters (fallback) | the series HTML, when the POST yields nothing |
| Pages | `GET {chapterID}` |

Both AJAX endpoints return the *same* bare `<ul>` fragment of
`li.wp-manga-chapter`, which is why one parser serves all three paths.

**`overrides` keys and why each exists**

| Key | Default | Why |
|---|---|---|
| `mangaSubPath` | `manga` | PLAN §7.3's own example of per-site variation. Sites rename the permalink segment to `series`, `comics`, and others. |
| `useAjaxChapters` | `true` | The plugin's recent default lazy-loads the list. When false, or when the POST returns nothing, the series HTML is read instead — so a site of the inline shape needs no override at all. |
| `ajaxStyle` | `current` | The current/legacy fork. `current` POSTs to `{series}ajax/chapters/`; `legacy` POSTs `manga_get_chapters` to `admin-ajax.php`. **This is deliberately an override, not a second theme** — see below. |
| `dateFormat` | `MMMM d, yyyy` | The site's WordPress date setting. A CLDR-ish spelling rather than a Go layout, so a user can copy theirs across. |
| `searchPostType` | `wp-manga` | A site that renamed the custom post type. Separate from `mangaSubPath` because renaming the permalink and renaming the post type are independent. |

**On the `madaralegacy` split.** Upstream (keiyoushi) has forked this family
into two themes. We did not, and the reason is worth recording: the two shapes
differ in *exactly one endpoint* and produce *identical chapter markup*. A
second theme would double the fingerprinting surface — two near-identical
scorers competing on every madara page — to express one boolean. The
`ajaxStyle` override costs one config key and keeps the fan-out clean. If a
future divergence turns out to be deeper than the endpoint, revisit this.

**Quirks**

- **Lazy images everywhere.** Reader images put a base64 placeholder in `src`
  and the real URL in `data-src`, `data-lazy-src` or `data-cfsrc`. Reading
  `src` first yields a chapter of grey squares and no error. `theme.ImageURL`
  encodes the attribute priority; `testdata/reader.html` exercises all of it.
- **URLs carry stray whitespace.** The plugin's templating leaves newlines and
  indentation inside `data-src`. Untrimmed, the request 404s.
- **Two date shapes.** Recent chapters render as `<a title="March 2, 2026">2
  days ago</a>`. The `title` attribute holds the absolute date and is preferred;
  relative text is parsed against the injected clock as a fallback.
- **The legacy path costs an extra round trip.** It needs the WordPress post ID,
  which only the series page carries. If the ID is missing, we parse the
  chapters out of the page we just fetched rather than failing.
- **Search results contain non-series links** — tag archives, author archives.
  Requiring the `mangaSubPath` segment filters them out.
- **`<h3>` vs `<h4>` in search cards** varies by skin; both are matched.

---

### `mangathemesia` — WordPress theme, formerly WPMangaStream

Implemented in `backend/theme/mangathemesia/`. PLAN §7.3 tier 1, and the
"structurally different" second theme M2's acceptance test asks for. It differs
from madara in every way that matters to this engine, which is the point.

**Fingerprint signals**

| Signal | Weight | Why |
|---|---|---|
| `ts_reader.run(` | 45 | The theme's own reader bootstrap. Near-conclusive. |
| `#readerarea` | 20 | Reader container. |
| `#chapterlist` | 20 | Chapter container — present on series pages, which is where madara has nothing. |
| `.listupd .bsx` | 20 | Listing card. |
| `.seriestucon, .seriestuheader` | 15 | Series header block. |
| `.tsinfo .imptdt` | 15 | The info row shape. |
| `.listupd .bsx .tt` | 15 | Card title. |
| `.listupd .utao .uta .imgu` | 15 | The theme's older card markup, still shipped. |
| `.eplister, .epcur` | 10 | Chapter list wrapper. |
| `.bsx .limit` | 10 | Card image frame. |
| `.adds .epxs` | 10 | "latest chapter" badge on a card. |
| `.bixbox` | 10 | The theme's generic section box. |
| `.mgen` | 5 | Genre list. |
| `/wp-content/plugins/madara/` | **−30** | A *negative* signal. See below. |

The listing signals are weighted to reach the confidence threshold **on their
own**, because a search or archive page has no reader and no series info block.
Without them a search page scored 25 and the probe would have called it
unrecognised.

The negative madara signal is what makes the near-miss test pass cleanly. A
heavily-skinned madara site shares enough generic WordPress surface to creep
into double figures here; subtracting on the other family's conclusive signal
keeps the two apart by a wide margin. Measured gaps against our fixtures:
madara pages score 83–100 / 0, mangathemesia pages 65–80 / 0.

**Endpoint shapes**

| Operation | Request |
|---|---|
| Search, page 1 | `GET /?s={q}` — **no `post_type` filter**; this theme does not implement one |
| Search, page N | `GET /page/{N}/?s={q}` |
| Series | `GET /{seriesSubPath}/{slug}/` |
| Chapters | none — the list is in the series HTML |
| Pages | `GET {chapterID}`, then the inline `ts_reader.run({...})` blob |

**`overrides` keys and why each exists**

| Key | Default | Why |
|---|---|---|
| `seriesSubPath` | `manga` | Same variation as madara's `mangaSubPath`. Deliberately a *different key name*: the two themes are independently configurable, and a shared name would invite pasting one theme's overrides into the other. The validator rejects `mangaSubPath` here by name, with a test for it. |
| `dateFormat` | `MMMM d, yyyy` | The site's WordPress date setting. |
| `pageSource` | `ts_reader` | `dom` for the minority of sites that render the reader server-side and ship no blob. |

**Quirks**

- **The DOM lies about the page count.** `#readerarea` holds only the first two
  or three images; the reader hydrates the rest from the blob. A DOM-only
  parser returns a truncated chapter *with no error* — the worst kind of bug,
  because it looks like a short chapter. `testdata/reader.html` is built to
  catch exactly this: its DOM has 2 images and its blob has 5.
- **The blob must be extracted by balancing braces, not by regex.** It nests
  objects inside its `sources` array; a non-greedy regex stops at the first
  inner `}` and a greedy one runs past the call. `balancedObject` skips string
  contents so a brace inside a URL cannot unbalance the count.
- **Try every occurrence of the call, not the first.** The name appears in HTML
  comments and in the theme's own documentation blocks. We take the first
  occurrence that yields images.
- **Several sources may be listed** (site plus mirrors). The first with images
  is the site's own and is the one the reader itself defaults to.
- **The Status row is two sibling `<i>` elements** — label and value. Removing
  every `<i>` to isolate the value removes the value too; remove only the first.
- **Placeholder metadata.** Unfilled Author/Artist fields render as `-`, which
  must yield no author rather than an author called "-".
- **Decorative chapter titles.** A chapter titled "Final Lamp" carries its real
  number in `data-num`, which wins over the title.
- **Search returns ordinary pages and posts**, since there is no post_type
  filter. Requiring the `seriesSubPath` segment is load-bearing here, not
  defensive.

---

### `generic` — the escape hatch

Implemented in `backend/theme/generic/`. PLAN §6 M2: "escape hatch, not the
main path." It is deliberately small and should stay that way. If a site needs
more than the vocabulary below, write a theme.

**Fingerprint: always 0.** The generic theme has no shape to recognise — it
*is* whatever its config says. `theme.Registry.Fingerprint` also excludes it
from the fan-out. Two locks on the same door, because a generic source winning
a probe would cost the user the honest "unrecognised" verdict.

**Selector vocabulary** (closed; an unknown key is a validation error, for the
same reason an unknown override is):

`searchPath`, `searchItem`, `searchLink`, `searchTitle`, `searchCover`,
`seriesPath`, `seriesTitle`, `seriesCover`, `seriesDescription`,
`seriesGenres`, `seriesStatus`, `chapterItem`, `chapterLink`, `chapterTitle`,
`chapterDate`, `pageImage`.

`searchPath` and `seriesPath` are URL templates rather than selectors:
`{query}` (URL-escaped), `{page}`, `{id}`.

**`overrides` keys**

| Key | Default | Why |
|---|---|---|
| `dateFormat` | `MMMM d, yyyy` | Same spelling as the themed sources. |
| `scriptTimeoutMs` | `2000` | Wall-clock limit on the goja hook. A runaway loop is interrupted, not tolerated. |

**The script hook.** A source may carry a small script exporting
`pages(html, url)` and/or `chapters(html, url)`. A missing function, or one
returning null/undefined/`[]`, means "no opinion" and the selector path runs.

The runtime has **no host bindings** beyond `atob` and `btoa`: no fetch, no
file access, no timers, no console, nothing from Node. This is not about
distrusting a user scripting their own device — it is about where the PLAN §7.4
fetch invariants live. Every request Quire makes goes through one
rate-limited, robots-respecting, SSRF-guarded client; a script that could fetch
would be a second HTTP path with none of that, reachable from a config file a
user might have imported from someone else. `atob`/`btoa` earn their place by
being pure string transforms, and because base64 is how a one-off site most
commonly hides a page list from selectors.

Scripts are compiled when the source is added, so a syntax error surfaces then
rather than when a chapter is opened.

**Both restrictions are enforced twice.** `selectors` and `script` are legal
only when `theme == "generic"`: `schema/source.schema.json` says so with an
`allOf` clause, and `theme.Registry.Validate` says so again in Go, because an
imported or hand-edited source reaches the struct without ever meeting the
schema.

---

## Writing theme #6 — the short version

1. New package under `backend/theme/`. Implement `theme.Theme` (six methods,
   PLAN §7.2) plus `theme.OverrideValidator`.
2. Declare a `theme.OverrideSpec`. Every key needs a `Default` and a `Why` —
   there is a test that fails if either is missing, because a key whose reason
   has been forgotten is a key nobody dares remove.
3. Write fixtures under `testdata/`, hosted at `example.invalid`, each with a
   header saying what shape it reproduces and which variations it exercises.
   Read the fidelity caveat above before deciding what a green run means.
4. Use `themetest.Fetcher`: routes are `"METHOD /path"`, and any unrouted
   request fails the test. A theme quietly asking for the wrong URL and getting
   a blank page is the bug these tests exist to catch.
5. Add the theme to `backend/theme/fingerprint_test.go`. It is not enough that
   it recognises its own pages — every other theme must *fail* to. Add a
   negative signal if two families share too much surface.
6. Add a section here: fingerprint signals with weights, endpoint table,
   overrides table with a "why" column, and the quirks that cost you an hour.
