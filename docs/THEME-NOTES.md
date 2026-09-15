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

### What the fixture *server* adds — and what it still does not prove

`backend/fixtures` closes one specific gap in the paragraphs above.

Everything described so far stubs the transport: `themetest.Fetcher` hands a
theme a `fetch.Response` it built in memory. That is the right tool for "does
this theme parse this shape", and it proves nothing whatever about the layer
underneath — connection handling, a redirect the HTTP client genuinely decides
to follow, `robots.txt` being *fetched* rather than injected, the limiter's
delays against elapsed time, `Retry-After`, backoff, the response cap meeting a
real `Content-Length`, byte accounting, or the SSRF guard running on a hop
Quire did not choose.

The fixture server serves the same committed synthetic fixtures over a real
socket on loopback, and can be told to misbehave on demand: 429 with
`Retry-After`, 5xx for a bounded number of attempts, slow responses, oversized
bodies with *and without* a declared length, a redirect chain leaving the
registrable domain, and a `robots.txt` that disallows a path. `make check` runs
the full path against it end to end, including the mangadex theme.

Run it by hand when something needs poking at with curl:

```sh
go run ./backend/cmd/fixtureserver -dir backend/theme/mangadex/testdata \
    -robots 'User-agent: *
Disallow: /at-home/'
curl -i 'localhost:8099/sim/status/429?retryAfter=5'
curl -sD- -o/dev/null 'localhost:8099/sim/large?bytes=20000000'
```

**The SSRF exemption, and why it cannot reach production.** The guard rejects
loopback, so a server on `127.0.0.1` is refused by default and the guard is not
weakened to accommodate it. The exemption is granted by
`fetch.AllowLoopbackForTests`, which lives in
`backend/fetch/loopback_quiretest.go` behind `//go:build quiretest`. Three
things hold, in the order they matter:

1. `Options.allowLoopback` is unexported and has no JSON tag, no schema entry
   and no flag, so no config file, imported sources file or goja script can set
   it however it is constructed.
2. Without `-tags quiretest` the function **does not exist**. It is not dead
   code in a shipped binary; it is not compiled into one. `make test` runs the
   tagged tests *and then* an untagged `go build ./...`, so the production
   shape is built on every check.
3. No production target passes the tag. `build/build-rmpp.sh` and
   `build/build-pc.sh` do not.

**Still not proved by any of this:** that a live site is parsed correctly. The
socket is real; the fixtures are still ours. PLAN §7.5 stage 5 against the
user's chosen site remains the only thing that proves that, and the paragraph
above about overclaiming still stands.

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

### `mangadex` — a documented JSON API, and a different animal

Implemented in `backend/theme/mangadex/`. Read this section before assuming it
works like the two above, because in the ways that matter it does not.

**It is one site, not a family.** PLAN §7.2 defines a theme as "a Go
implementation of one site *family's* shape", and madara and mangathemesia are
exactly that: one distributed WordPress plugin or theme, deployed across
hundreds of independent sites, recognised from markup and configured per site
with `overrides` because every deployment renames something. MangaDex has no
family, no second deployment and nothing to rename. What it has instead is an
official, documented, versioned, unauthenticated JSON API with published rate
limits for third-party clients.

It still implements `theme.Theme` — a theme with one instance costs nothing and
keeps a single interface for the probe, the browse UI and the download queue.
But three things follow from being a site rather than a family, and each is a
place where copying the madara section's habits would be wrong:

| | madara / mangathemesia | mangadex |
|---|---|---|
| Recognised by | markup: class names, asset paths, inline script markers | the API host and its response envelope |
| Score for a non-match | low but non-zero (best near-miss 5) | **exactly 0** |
| `overrides` absorb | where the site put things | what the user wants shown |
| Parsing | goquery over HTML that may be skinned | `encoding/json` over a documented schema |
| Language | one site, one language | multilingual by design; the filter is load-bearing |

**Fingerprint signals**

Unusually, the first signal is a **gate, not a weight**.

| Signal | Weight | Why |
|---|---|---|
| registrable domain is `mangadex.org` | **gate** | Fails → score 0, and nothing below can lift it |
| host is `api.mangadex.org` | 25 | What a source must actually point at |
| `Server: MangaDex` response header | 20 | Set on every response from its edge, including the 308 at `/` and the four-byte `/ping` |
| `"result":"ok"` envelope, `pong`, or a `/docs` path | 15 | The shape every endpoint shares |
| base, given the gate | 40 | |

Measured: the API root scores **100**, `/ping` **100**, a collection response
**100**, the browser-facing `mangadex.org` **80**, and *everything else 0* —
including a JSON API elsewhere returning the identical envelope with the
identical `Server` header, and a `mangadex.org` URL that redirected off-domain.
That is the whole reason for the gate. "Returns JSON with a `result` field"
describes half the web, so a single-site theme that matched on shape alone
would false-positive constantly. Consistent with the §7.5 threshold of 60:
winners 80–100, losers 0.

`Validate` (the `theme.SourceValidator` side interface) does the matching job
for a configured source: it refuses any `baseUrl` that is not
`https://api.mangadex.org`, permitting `.invalid` hosts only so the offline
tests can run. Without it, an imported sources file could aim Quire's MangaDex
support at any host answering MangaDex-shaped JSON.

**Endpoint shapes**

Confirmed live on 2026-09-15 against `https://api.mangadex.org` and against the
official docs at `https://api.mangadex.org/docs/`.

| Operation | Request | Kind (§7.4) |
|---|---|---|
| Search | `GET /manga?title={q}&limit=20&offset={n}&includes[]=cover_art&contentRating[]=…&availableTranslatedLanguage[]={lang}` | discovery |
| Series | `GET /manga/{id}?includes[]=cover_art&includes[]=author&includes[]=artist` | discovery |
| Chapters | `GET /manga/{id}/feed?translatedLanguage[]={lang}&limit=500&offset={n}&order[chapter]=asc&includes[]=scanlation_group` | discovery |
| Pages | `GET /at-home/server/{chapterId}` | **retrieval** |
| Cover image | `https://uploads.mangadex.org/covers/{mangaId}/{fileName}` | — |
| Page image | `{baseUrl}/data/{hash}/{filename}` | — |

Everything is the same envelope: `{result, response, data, limit, offset,
total}` for a collection, `{result, response, data}` for one object. Each
object is `{id, type, attributes, relationships}`, and a relationship carries
its own `attributes` **only** if the request asked for it with `includes[]` —
otherwise it is a bare UUID and a second round-trip.

**Paging.** `limit` caps at 100 on `/manga` and 500 on a feed, and the API
refuses `offset` past 10000. `Chapters` pages with `offset` until it reaches
`total`, bounded at 20 requests. Asking for the documented maximum is the
*polite* choice here, not the greedy one: one 500-chapter request is one
request, and §7.4's floor spaces requests 2 s apart.

**Rate limits.** MangaDex documents a global ≈5 req/s per IP. Quire's §7.4
floor — 4 global / 2 per-host / 2 s minimum delay, i.e. 30 rpm — is well inside
it and **must not be raised on that basis**. The floor is about being a polite
guest, not about staying under a published ceiling. 429s do happen; the fetch
layer's `Retry-After` handling covers them, and the theme reports a 429 that
survived its retries as rate limiting rather than as a decode failure.

**`overrides` keys and why each exists**

| Key | Default | Why |
|---|---|---|
| `maxContentRating` | `suggestive` | MangaDex rates every series (`safe`, `suggestive`, `erotica`, `pornographic`) and filters on request. Naming the maximum makes the choice explicit instead of inheriting a site default that could change. |
| `includeExternal` | `false` | Some chapters are hosted on the publisher's own site: they arrive with an `externalUrl` and `pages: 0`, and MangaDex serves no images for them. Hidden by default, because offering a chapter that cannot be downloaded is worse than omitting it. |

Note what is *absent*: no path segments, no date format, no selector. A single
site has nowhere to vary.

**Quirks**

- **There is no canonical title.** `attributes.title` is a language-keyed map
  that often holds only a romanisation (`ja-ro`), while the English title lives
  in `altTitles` — a list of *single-entry maps*, not one map, with repeats.
  `bestTitle` therefore prefers the **language** over the **field**: for each of
  `lang`, `en`, `{orig}-ro`, `{orig}` it checks `title` and then `altTitles`
  before moving on. A series stored as "Tooi Tou no Kiroku" shows up for an
  English source as "Record of the Distant Tower".
- **The language filter is load-bearing, and checked twice.** Without
  `translatedLanguage[]` the feed returns every translation of every chapter:
  duplicate numbers in a dozen languages, and the download queue taking
  whichever sorted first. The theme sends the parameter *and* re-checks
  `attributes.translatedLanguage` locally, so a refactor that drops the
  parameter yields an empty list — a bug someone notices — rather than a
  quietly multilingual one.
- **`externalUrl` and `isUnavailable` are not cosmetic.** Both mean "listed but
  not readable". A popular title can have its entire English run external; a
  capability check that picked the first chapter would conclude page extraction
  was broken.
- **Off-domain hosts.** Covers come from `uploads.mangadex.org` (same
  registrable domain, so the guard is content) but **page images come from
  `*.mangadex.network`**, which is not. See the §7.4 note below — a MangaDex
  source needs `mangadex.network` in `allowedHosts` before M4 can download a
  page.
- **Data-saver exists and we do not use it.** `/at-home/` returns both a
  full-quality `data` list and a recompressed `dataSaver` list (JPEG, smaller).
  Quire takes full quality: M4 resizes and re-encodes for the device anyway, so
  starting from an already-degraded JPEG compounds the loss. A future
  `dataSaver` override is a reasonable option for a metered connection — it is
  a deliberate omission, not an oversight.
- **Volume in the title.** `theme.Chapter` has no volume field and MangaDex is
  the first theme that reliably knows one, so it is composed into the title:
  `Vol. 3 Chapter 12: The Long Walk`, degrading to `Chapter 12` and to
  `Oneshot` when there is no number.
- **UUIDs, stored as paths.** IDs are `/manga/{uuid}` and `/chapter/{uuid}`, so
  they keep the "site-relative" convention, stay self-describing in a state
  file, and make a series UUID handed to `Pages()` an error rather than a 404.

**robots.txt — and why this theme changed the fetch layer**

`api.mangadex.org/robots.txt` is, in full (confirmed 2026-09-15):

```
User-agent: *
Disallow: /at-home/
```

Everything used to *find* a chapter is allowed. The single disallowed prefix is
`/at-home/` — the endpoint that hands out page images. Under a blanket robots
rule, Quire could search MangaDex, open a series and list its chapters, and
then refuse to read one; stage 5 would report `partial` and §7.5 would refuse
the source outright, because page extraction failing makes a source useless.
By extension it would refuse **any officially supported API whose robots.txt
was written for search engines**.

That is what forced PLAN §7.4's decision of 2026-09-15: robots binds
*crawling*, not user-directed retrieval. RFC 9309 scopes robots.txt to
"automatic clients known as crawlers". Search, listings, link following and the
probe's own crawling honour `Disallow` **strictly**; fetching one series,
chapter or page the user explicitly asked for is retrieval.

Implemented as `fetch.Kind` (`KindDiscovery` / `KindRetrieval`), passed
explicitly at each call site. Three properties are load-bearing and are worth
restating where a theme author will read them:

1. **Never inferred.** Not from the path, not from the method. A caller says
   which it is, where the reason is visible.
2. **Never configurable.** `Kind` has no JSON tag, no schema entry, and is not
   reachable from a `Source`. A source entry cannot flip a discovery request
   into a retrieval one to get past a `Disallow`.
3. **Narrows nothing else.** Rate limits, per-host delays, the honest
   User-Agent, `Retry-After`, backoff, the response-size cap, byte accounting
   and the SSRF guard apply identically to both. Retrieval is not a fast lane.

In this theme exactly one call is retrieval — `Pages()` — and it says so in a
comment at the call site. `TestPagesIsFetchedAsRetrieval` asserts it, and
`TestDiscoveryCallsAreNotRetrieval` asserts the more important converse: search
and series are discovery and stay bound by robots.

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

## Challenge detection — what is verified, and what is not

Implemented in `backend/probe/prober/challenge.go` (PLAN §7.5 stage 3). A fired
signal means verdict `blocked_challenge` and the source is **refused**: no
degraded add, no retry, no workaround (PLAN §7.6). There is no flag that skips
it, and `TestNoBypassVocabularyInSource` fails if one starts to appear.

**Read this before quoting the table below as coverage.** PLAN §7.5 asks each
signal to be checked against a live example. PLAN §1.3 forbids this repository
from naming an aggregator, and the developer cannot reach a challenge-protected
site from CI, so **not one signal here has been observed firing against a live
interstitial**. What each one rests on instead is stated in the "basis" column,
and the honest summary is:

- **Published-fact signals.** The marker is a vendor's own documented endpoint
  path, header name or cookie name. The inference "if this string is on the page
  we were served, that vendor is interstitialling us" is sound, but the exact
  markup a current interstitial emits is unverified.
- **Structural signals.** No vendor involved: a refusal status, an interstitial
  title, an empty page. These are inference from shape alone and are the ones
  most likely to be wrong in either direction.

The fixtures under `backend/probe/prober/testdata/` are synthetic, at
`example.invalid`, exactly like the theme fixtures, and the caveat in "What the
fixtures do and do not prove" applies unchanged: they pin *our detector*, not
any live page.

| Signal | Fires on | Basis |
|---|---|---|
| `/cdn-cgi/challenge-platform/` | body | Published fact — Cloudflare's reserved `/cdn-cgi/` path prefix, which only its own edge serves |
| `challenge-platform/h/b/orchestrate` | body | Published fact — the orchestrate script under that same prefix |
| `__cf_chl_` | body | Published fact — the prefix of Cloudflare's challenge parameters/handles |
| `cf-challenge-running` | body | Published fact — the class the challenge page sets while running |
| `challenges.cloudflare.com/turnstile` | body | Published fact — the Turnstile widget's own host |
| `cf-mitigated: challenge` | header | Published fact — a header Cloudflare added specifically so clients can tell a challenge from a block. Fires alone |
| `/_incapsula_resource?swcgh`, `_incapsula_resource?swjsv` | body | Published fact — Imperva/Incapsula's own resource endpoint |
| `sucuri_cloudproxy_js` | body | Published fact — Sucuri CloudProxy's script identifier |
| `/ddos-guard/js-challenge`, `check.ddos-guard.net` | body | Published fact — DDoS-Guard's challenge path and check host |
| `/.well-known/captcha/` | body | Published fact — the registered well-known URI for a CAPTCHA gate |
| 403/503 from a CDN edge **and** an interstitial `<title>` | status + header + title | **Structural.** Either half alone is ordinary; the pair is the classic shape. Titles matched: "just a moment", "attention required", "checking your browser", "please wait while we verify", "verifying you are human", "ddos-guard", "access denied", "security check" |
| 503 from a CDN edge, any body | status + header | **Structural.** A managed edge answering 503 for a home page is an interstitial far more often than it is an outage — but this is the signal most likely to misfire during a genuine outage |
| `<meta http-equiv="refresh">` to a challenge endpoint | body | **Structural**, with a published-fact target list (`/cdn-cgi/`, `captcha`, `__ddg`, `_incapsula_resource`) |
| Clearance cookie (`cf_clearance`, `__ddg*`, `sucuri_cloudproxy_uuid`, `incap_ses_*`, `visid_incap_*`, `datadome`) | `Set-Cookie` | Published fact for the names; **structural** for when it counts. Only fires alongside a refusal status *or* a page with no recognisable content, because a long-lived clearance cookie can be re-issued on an ordinary page view. `TestOrdinarySiteBehindACDNIsNotRefused` pins that |
| Generic JS gate | 200 + body < 6 KiB + no theme match + `<noscript>` or an empty mount point | **Structural, and the least certain.** It is what catches a vendor we have never heard of, which is why it needs all four conditions at once |

Edge headers used as the second half of a combined signal, never alone:
`cf-ray`, `cf-mitigated`, `server: cloudflare`, `server: ddos-guard`,
`server: sucuri/cloudproxy`, `x-sucuri-id`, `x-iinfo`, `x-datadome`,
`x-datadome-cid`.

**Warnings, not blocks.** A login wall (a password input on a page we cannot
otherwise read), a paywall ("subscribers only", "members only", "subscribe to
continue reading") and an age gate ("age verification", "are you over 18",
"confirm your age") are surfaced as warnings and the user decides. PLAN §7.5 is
explicit that these are not challenges, and treating them as blocks would make
Quire refuse sites it can read perfectly well.

**When one of these turns out to be wrong,** correct the table with what was
actually seen, and say where it was seen in general terms — never name the site
(PLAN §1.3).

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
7. If the theme needs a request that robots disallows, read §7.4's
   discovery/retrieval decision and the `mangadex` section before reaching for
   `GetRetrieval`. The bar is "the user named this thing", not "this request is
   inconvenient to lose".
8. If it is a JSON API rather than a markup family, say so at the top of its
   section the way `mangadex` does, and gate the fingerprint on something that
   cannot be worn by accident. A JSON envelope is not a fingerprint.
