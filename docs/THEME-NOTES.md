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
runtime against whatever site the user chooses to add** — which since
2026-09-16 **fetches** one page image rather than only extracting its URL, for
the reason recorded in the `mangakakalot` section: a site can hand over a
perfect list of image addresses and then refuse every one of them — and, when a working
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

## Chapter ordering — the contract every theme owes

**PLAN §7.2, decided 2026-09-15: `Chapters` returns ascending reading order,
earliest chapter first.** Implemented in `backend/theme/order.go`.

**The bug it replaced.** madara's markup lists chapters newest-first; MangaDex's
feed sorts oldest-first; nothing said which was correct. §6 M4 groups a run of
chapters into *one* volume PDF, so a descending source produced a PDF whose
pages ran backwards — inside a file whose reading position xochitl then owns.
M5 surfaced it as a volume titled "The Lantern Keeper — 4–1". Silent until
someone opened it.

**Why the theme and not the caller.** Only the theme knows what its identifiers
mean. A generic sort at the call site is how this goes wrong a second time:
`"10"` sorts before `"9"` as a string, `"12.5"` is not an integer, and
mangathemesia's chapter 1 is *titled* "Final Lamp" with the real number hidden
in a `data-num` attribute that no caller can see.

**What `theme.Chapter` grew**

| Field | Why |
|---|---|
| `Volume string` | The source's own volume label, `""` when it publishes none. A *label*, not a number, because that is what sites give ("3", "Vol. 3", "TBD"). §6 M4's "runs of ten" grouping is the fallback for sources with no volume structure; this field is what tells it which case it is in. |
| `OrderUnknown bool` | Quire could not establish a reading order for the list this chapter came from. Set on **every** chapter of such a list, because it describes the list; `theme.OrderIsKnown(chs)` reads it back at that level. |

`OrderUnknown` lives on the element only because `Theme.Chapters` returns a
plain slice, and inventing a wrapper type to carry one boolean would change the
interface for every caller to say something they can already be told.

**How `SortAscending` decides — direction first, sorting second**

Sites list chapters in a consistent direction. What they do *not* do is
interleave unnumbered extras predictably. Given `5 4 Extra 3 2 1`, sorting by
number puts `Extra` at one end, belonging to nobody; detecting that the numbered
chapters run *downwards* and reversing the whole list keeps `Extra` between 3
and 4, where the site put it.

| Case | Action | `ordered` |
|---|---|---|
| No chapter carries a number | keep source order | **false** |
| The numbered chapters already ascend | keep | true |
| The numbered chapters descend | reverse the whole list, extras included | true |
| Neither, and *every* chapter is numbered | stable sort by number | true |
| Neither, and some are unnumbered | keep source order | **false** |

Chapter number is the primary key. **Volume is a tie-break, not a prefix** —
chapter numbers run continuously across volumes on every source seen so far, so
keying on volume first would reorder a correct list the moment one label was
missing. Volume only speaks up when two chapters claim the same number, which
is what a re-release in volumes looks like.

**Signalling "unknowable".** The two `false` rows above are the honest failures:
the source order is kept untouched and `SortAndMark` sets `OrderUnknown` on
every chapter. A caller assembling several chapters into one PDF must consult
`theme.OrderIsKnown` rather than assume. A wrong order is worse than an admitted
one.

**What each theme does**

| Theme | Source order | Derived from |
|---|---|---|
| `madara` | newest first | `li.wp-manga-chapter` in document order, reversed. One call in `parseChapters`, the single exit for all three of its chapter paths (inline HTML, current AJAX, legacy AJAX). |
| `mangathemesia` | newest first | `#chapterlist li` in document order, reversed. The number comes from `data-num` when the title is decorative. |
| `generic` | unknown | Whatever the user's selectors or script yield. Normalised like everyone else: selectors say *where* the chapters are, never which way round they run, and a user's script is not trusted to have got it right either. |
| `mangakakalot` | newest first | The chapter **API**'s `chapters` array in response order, reversed. The number comes from the API's own `chapter_num`, which matters here: a title like `"Chapter 453: Night 453"` has two numbers in it and only one of them is the chapter's. |
| `mangadex` | oldest first | `order[chapter]=asc`, then normalised anyway — see below. |

**mangadex normalises even though the server sorts.** Not defensive
box-ticking: the feed is paginated and the pages are concatenated in *request*
order, `order[chapter]=desc` is an equally documented value of the same
parameter, and a chapter with no number has no defined position in the server's
sort at all. The cost is one pass over a slice we already own. `Volume` comes
from `attributes.volume` and is no longer baked into the display title.

**Testing it.** Every theme has a `TestChaptersAreAscending`, and the fixtures
are adversarial on purpose — a test over a fixture that happens to be ascending
proves nothing. `chapters-ajax.html` and `series.html` are newest-first because
that is what those themes' markup does, and `mangadex/testdata/feed-descending.json`
exists solely so the MangaDex test is not just watching the server sort.
Confirmed by mutation: deleting the `theme.SortAndMark` call in madara's
`parseChapters` fails `TestChaptersAreAscending` on the first comparison.

**One caller changed with it.** §7.5 stage 5 extracts pages from
`chapters[len-1]`, the *newest*, where it used to take `chapters[0]`. That is a
deliberate choice rather than an index fix: a site that has changed its reader
markup leaves old chapters exactly as they were, so probing the earliest one
can report a capability the user will not have on anything they actually read.

---

## Suggested display names

PLAN §7.2, 2026-09-15. `Theme.SuggestedName() string` returns the display name
a source of that shape should carry by default; `""` means "no opinion" and
§7.5 stage 6 falls back to the page `<title>`, then to the host.

| Theme | Suggestion | Why |
|---|---|---|
| `mangadex` | `"MangaDex"` | One site, which knows what it is called. Without this, adding `https://api.mangadex.org` produced a source named **"MangaDex API documentation"** — exactly what that page's `<title>` says, and meaningless in a source list. |
| `webtoons` | `"WEBTOON"` | One publisher, and a `<title>` that is a sentence of marketing copy. |
| `fanfox` | `"Manga Fox"` | One site, and likewise a `<title>` that is a slogan rather than a name. |
| `comick` | `"Comick"` | One piece of software, and the name is the same whichever host the user points at — which is what makes a suggestion right even though the theme deliberately names no host (see its notes on mirrors). |
| `weebcentral` | `"Weeb Central"` | One site, and one that spells its own name with a space the theme ID does not. The page `<title>` would in fact have done here — but only by luck, which is the case §7.5's stage-6 correction is about: a theme that knows its site's name should say so rather than leave the default to whatever the site puts in a `<title>` this week. |
| `mangakakalot` | `""` | A family of mirrors carrying different names and no shared branding. There is no one name to lend them, so the page title is the better default and a rename is one tap away. |
| `madara`, `mangathemesia`, `generic` | `""` | Families of hundreds of independently branded sites, or an escape hatch pointed at a site nobody has themed. For these the page title genuinely *is* the best available default; inventing a name would be worse than the title ever is. |

**There is no title cleaning, here or anywhere.** Stripping `" — Home"`,
`" | Official Site"` or `" API documentation"` is an unwinnable game that
eventually mangles a site whose real name ends in one of those. A theme either
knows its site's name outright or has no opinion, and the suggestion is used
verbatim — there is a test for that. Everything the suggestion does not cover
is a user-facing rename, which is the other half of the fix and lives in the
UI.

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
  generated labels under `mangadex.network`**, which is not. This is what made
  PLAN §7.2 grow an `AllowedHosts()` method: the theme declares
  `*.mangadex.network`, §7.5 stage 6 seeds it onto the stored source, and M4
  can download. Verified live on 2026-09-15 against one real page image —
  refused without the declaration, `HTTP 200, image/png` with it. See the
  `allowedHosts` section below for the numbers and the limits.
- **Covers must be requested as thumbnails.** `uploads.mangadex.org` serves
  the **original artwork** by default, which is print-resolution. On the device
  every cover failed with `response exceeds size cap (declared 10852108 >
  8388608)`. Appending `.512.jpg` to the filename asks for a 512px-long-edge
  version of the same cover. Measured live on 2026-09-15:

  | URL | bytes |
  |---|---|
  | `<file>.jpg` | 10,852,108 |
  | `<file>.jpg.512.jpg` | **235,535** |
  | `<file>.jpg.256.jpg` | 72,427 |

  512 and not 256, because the cover render is 300px wide and 256 would be
  upscaled; at 235 KB the larger one clears fetch's 8 MiB response cap and
  `backend/covers`' 4 MiB `MaxSourceBytes` by more than an order of magnitude.
  Neither cap is the thing to change — asking for a thumbnail when a thumbnail
  is what will be drawn is. The suffix lives in this theme rather than in
  `backend/covers` because it is a fact about MangaDex's URL scheme, and a
  generic cover cache has no business knowing one site's naming convention.
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

**`allowedHosts` — and why this theme changed `theme.Theme`**

Page images do not come from `api.mangadex.org`. `/at-home/server/{id}` returns
a base URL on a host like `cmdxd98sb0x3yprd.mangadex.network` — a generated
label on a **different registrable domain**. §7.4's redirect boundary refuses
it, so before this change M4 could list a chapter and download none of it, and
the user's only clue was an SSRF rejection naming a host they had never seen.

PLAN §7.2 therefore gained `AllowedHosts() []string` (2026-09-15). Two forms:
`example.test` matches that host and anything under it; `*.example.test`
matches subdomains only. This theme declares `*.mangadex.network` — the
narrower form, because the apex serves nothing. Covers are *not* declared:
`uploads.mangadex.org` is inside the API's own registrable domain, so naming it
would imply a widening that is not happening.

§7.5 stage 6 copies the declaration onto the stored source, so it is visible in
the source entry, editable by the user, and unaffected if a later version of
the theme changes its mind. Seeding happens when the draft is built, before
stage 5, so the capability check runs under the same policy the stored source
will have.

**What this does not do, and the test that pins it.** `allowedHosts` widens the
registrable-domain boundary and *only* that. `Guard.CheckURL` runs the address
check after the domain check, unconditionally, so a declared host that resolves
into a private, loopback or link-local range is still refused — a theme cannot
name its way onto the local network, and neither can a hand-edited or imported
source entry. `TestAllowedHostsCannotReachAPrivateAddress` declares each host
three ways at once (exact, wildcard, bare domain) and requires every one to be
refused; mutating `CheckURL` to short-circuit when `allowedHosts` matches makes
it fail.

Verified live on 2026-09-15 against one real page image:

```
theme declares: [*.mangadex.network]
unseeded  allowedHosts=[]                   -> REFUSED: leaves the source's domain "mangadex.org"
seeded    allowedHosts=[*.mangadex.network] -> OK: HTTP 200, 476737 bytes, image/png
```

**No other theme needed one.** `madara`, `mangathemesia` and `generic` all
return nil, checked rather than assumed: every image reference in their
fixtures is relative or on the site's own host, and none of them redirects
off-domain. A family of independently hosted installs has no shared CDN to
name, and inventing one would widen the boundary for every site in the family.

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
and series are classified as discovery.

**Superseded in part, 2026-09-16 — read this before relying on the paragraph
above.** PLAN §7.4 now makes the robots.txt consultation a **single global
setting, off by default**, so a `Disallow` binds nothing unless the setting is
turned on. The machinery is all still here — parser, cache, the three-way
handling of an unreadable file — and is still tested against the on-state, so
turning it back on is a setting rather than a rewrite.

`Kind` stays, and the three properties above still hold; what changes is that
while the setting is off it gates nothing. It remains the documentation of
intent at each call site, it is what the gate reads the moment the setting is
on, and it is named in the info line logged for **every** suppressed check — the
only record of what the setting actually did, and the only thing that
distinguishes a request a person drove from the probe and the watch checks,
which run unattended. Nothing else moved: rate limits, per-host delays, the
honest User-Agent, the size cap, byte accounting and the SSRF guard all still
apply, and §7.6 is untouched — a challenge is a site actively refusing us, which
is a different thing from an advisory file aimed at crawlers, and
`blocked_challenge` stays terminal.

The setting lives in `schema/settings.schema.json` as `consultRobots` and is
stored with the sources; `fetch.Client.SetConsultRobots` applies it.

---

### `mangakakalot` — a family of mirrors, and the first non-WordPress family

Implemented in `backend/theme/mangakakalot/`. Not in PLAN §7.3's list, and added
because none of the themes that were there fit it: the check was structural
rather than nominal. **No madara markers** (no `wp-manga` asset path, no
`admin-ajax.php` chapter POST) and **no mangathemesia markers** (no
`ts_reader.run(`, no `#chapterlist`, no `.bixbox`). It is its own lineage, and a
*family* rather than a site — the same markup is served by a long tail of
mirrors — so it is a theme like the others.

#### Fingerprint signals

Grouped by the page they appear on, because no page of this family carries all
of them and the probe only ever sees one. The home page in particular carries
none of the series, reader or listing markup, so its signals have to reach the
threshold of 60 on their own.

| Page | Signal | Weight | What it is |
|---|---|---|---|
| Series | `#chapter-list-container[data-api-url]` | 35 | The defining one. The chapter list is *not in the document*; what ships is an empty container carrying the API endpoint |
| Series | `[data-chapter-url-template]` | 10 | The reader URL shape, alongside it |
| Series | `.manga-info-top, .panel-story-info` | 20 | The info block |
| Series | `ul.manga-info-text` | 10 | Its flat list of `Label : value` rows |
| Series | `.manga-info-pic, span.info-image` | 10 | The cover |
| Reader | `.container-chapter-reader` | 35 | The reader, server-rendered |
| Reader | `chapterImages` / `cdns` | 20 / 5 | The inline image arrays |
| Listing | `.list-comic-item-wrap, .list-truyen-item-wrap` | 25 | The card grid |
| Listing | `a.list-story-item` | 20 | Its cover anchor |
| Listing | `a.list-story-item-wrap-chapter` | 10 | The "newest chapter" line on a card |
| Search | `.panel_story_list .story_item` | 35 | The older row-per-result rendering |
| Search | `.story_item .story_name, .story_item_right` | 25 | Its inner structure |
| Either | `.group_page, .group-page` | 5 | The pager |
| Home | `#contentstory .doreamon` | 35 | The latest-releases row |
| Home | `.itemupdate a.bookmark_check, a.cover.bookmark_check` | 25 | Its cards |
| Home | `.daily-update` | 10 | The section heading block |
| **Negative** | `wp-manga` | **-40** | A madara page is madara's, whatever else it shares |
| **Negative** | `ts_reader.run(` | **-40** | Likewise mangathemesia's |

The negatives are not decoration. Without them a page could in principle be
claimed by two themes at once and §7.5 stage 4 would put a near-tie to the user
over a question that is not actually close. Against the committed corpus this
theme scores 60–85 on its own pages and 0 on every other theme's, and the
cross-theme suite requires a 30-point gap.

#### Endpoint shapes

| What | Shape |
|---|---|
| Series | `GET /{seriesSubPath}/{slug}` |
| Chapter list | `GET /api/manga/{slug}/chapters?limit=-1` → `{success, data:{chapters:[{chapter_name, chapter_slug, chapter_num, updated_at}]}}` |
| Reader | `GET /{seriesSubPath}/{slug}/{chapterSlug}` |
| Search | `GET /{searchPath}/{normalised query}?page=N` — the query is a **path segment**, lower-cased with every non-alphanumeric run folded to `_`, because that is what the site looks up |
| Browse (empty query) | `GET /{browsePath}?page=N` |

Two of those are worth dwelling on.

**The chapter list is an API, not markup.** Parsing the series HTML for chapters
finds *nothing* — not a short list, nothing — and `limit=-1` returns the whole
list in one response, so there is no pagination to walk. The API answers
**newest-first**, which is why `Chapters` reverses it; `testdata/chapters.json`
is descending on purpose so that forgetting to is a red test rather than a
volume that reads backwards.

**Search is a path, not a parameter.** `?s=lantern` finds nothing here. The
normalisation is the site's own and is reproduced rather than improved on:
folding more than it does produces a query it cannot answer.

#### `overrides` keys, and why each exists

| Key | Default | Why |
|---|---|---|
| `seriesSubPath` | `manga` | Series and readers both hang off it. A mirror renaming a segment is exactly the drift a family of mirrors produces, and it should be a config change rather than a new build |
| `searchPath` | `search/story` | Same reasoning; prior art keeps this overridable for the same reason |
| `browsePath` | `manga-list/latest-manga` | The UI's Browse is a search with an empty query (§7.5, M3 correction 1) and this family's search endpoint has nothing to say about an empty string, so Browse goes to a listing instead |
| `pageSource` | `dom` | The reader is server-rendered **and** carries the inline arrays. Whichever is chosen, the other is tried when the first yields nothing — a mirror that lazy-loads has only the arrays, and returning no pages while the fetched page plainly contained them would be a failure we could see and chose not to look at |

#### `AllowedHosts`

`*.2xstorage.com`, `storage.waitst.com`.

This is the second theme after `mangadex` to declare anything, and for the same
reason: **it does not host its own images.** Covers and page images both come
from dedicated image hosts on a different registrable domain from the site, so
without the declaration the SSRF guard refuses every download and the user's
only route out is to guess a CDN's name from a rejection.

Narrow in both available senses. One domain is named exactly; the other gets
`*.` — subdomains only, never the bare domain — because the hosts under it are
rotated numbered labels (`img-r1`, `img-r2`, `imgs-2`) rather than one stable
name. A bare-domain entry there would permit a host the family never serves
from.

#### Quirks, and two honest limitations

**This family's `robots.txt` disallows its search and its paginated listings.**
Observed, not assumed: `Disallow: */search/story/*` and `Disallow: *?page=*`,
alongside `Allow: /`. Under the rule this project shipped until 2026-09-16 —
robots consulted, a `Disallow` binding every discovery request — a source of
this theme could not be searched or browsed at all, which is to say it could not
be used. That is the concrete case behind PLAN §7.4's global robots setting, and
it is why the two pieces of work landed together. With the setting at its
default (not consulted) browsing works; with it turned on, expect
`robots_denied` on the first search.

**What was actually measured, 2026-09-16.** Run by hand against three live
mirrors of this family with the honest `User-Agent`, so nobody has to repeat the
afternoon:

| Step | Result |
|---|---|
| Home page | **200**, parsed |
| Listing / browse (`manga-list/...`) | **200**, 24 results parsed |
| Series page | **200**, title, status, authors, genres, cover, description all parsed |
| Chapter API | **200**, 286 chapters, ascending after the reversal, `OrderUnknown=false` |
| Reader page | **200**, 76 page URLs extracted |
| **Search** (`/search/story/...`) | **403, Cloudflare interstitial** — on all three mirrors |
| **Page image** on the image host | **403, Cloudflare "Attention Required!"** |

So: **the theme parses this family correctly, and the family is not usable for
downloading today.** Browsing, series detail and chapter lists work; search and
downloads are challenged. PLAN §7.6 is the answer to both — a challenge is a
site saying no, we take the answer, and there is no bypass in this codebase.

Two things follow that are worth stating plainly.

**robots.txt was not the blocker on the critical path.** It blocked search and
paginated listings, which is real and is what made the family unusable under the
old rule — but the series page, the chapter API and the reader were all
`Allow`ed, and what actually stops a download is the image host's challenge. The
policy change of §7.4 was necessary to browse this family and is *not* sufficient
to use it. Anyone reading the robots decision as "and then it worked" has the
wrong picture.

**This family is what corrected §7.5 stage 5.** Extraction succeeded — 76 URLs,
parsed perfectly — and the fetch of the first one 403'd. Stage 5 as originally
written checked "one page-image *extraction*", so it would have returned `ok`,
the source would have been added, and the user would have discovered the
problem minutes later when a download failed. That is a false `ok`, which is the
one thing §7.5 and §6 M3 are most emphatic about not producing. Stage 5 now
**fetches one image** through the real client, which also exercises the SSRF
guard against the seeded `allowedHosts`. A challenge there is `blocked_challenge`
and terminal, and the verdict names the host so it is not baffling after a
search that worked; an unmarked 403 is `partial` naming page fetching.

**Author rows are messy by nature.** The `Author(s)` row often carries a list of
romanisations of one name. They are returned as the site gives them; picking a
canonical one is a guess, and a wrong guess looks like a different author.

---

### `weebcentral` — an htmx fragment application, and the first single site that is not an API

Implemented in `backend/theme/weebcentral/`. Not in PLAN §7.3's list, and added
for the same structural reason `mangakakalot` was: nothing already here fits it.
No `wp-manga`, no `ts_reader.run(`, no `admin-ajax.php`, no WordPress at all —
and no JSON API either. It is a server-rendered **fragment** application: named
endpoints answer HTML snippets and htmx swaps them into the page. `mangadex` is
the only other single-site theme, and it is an API; this is the first theme
where the interesting endpoints are markup that no browser ever shows whole.

#### Fingerprint signals

The site is built on a utility-CSS framework, so its class names are
`flex items-center gap-2` and describe half the web. Class-name signals are
worthless here. What is distinctive is *where it fetches from*: the fragment
endpoints named in `hx-get` attributes, and the two element IDs those fragments
are swapped into.

| Page | Signal | Weight | What it is |
|---|---|---|---|
| Reader | `#chapter-images` | 45 | The defining one. The ID the page images are swapped into; nothing else emits it |
| Any | `#quick-search-input, #quick-search-result` | 20 | The header type-ahead, present site-wide |
| Series / chapters | `#chapter-list` | 15 | The chapter-list container |
| Series / chapters | `checkNewChapter(` | 20 | The Alpine expression every chapter row carries |
| Search | `/search/data` | 25 | The search fragment endpoint, in the next-page button's `hx-get` |
| Search | `display_mode=` | 20 | Its row-layout parameter, alongside |
| Home | `/hot-series` | 15 | A home-page panel endpoint |
| Home | `/latest-updates/` | 10 | Another |
| Home | `/recently-added/` | 10 | Another |
| Series | `/full-chapter-list` | 20 | The chapter-list fragment endpoint |
| Any | `/static/images/chapter-badge` | 20 | The badge on every chapter row |
| Reader | `/static/images/broken_image` | 15 | The `onerror` placeholder every page image carries |
| Any | `hx-get=` | 10 | htmx itself — weak on purpose, plenty of sites use it |
| Any | `a[href*="/chapters/"]` | 10 | The chapter link shape |
| **Negative** | `/wp-content/` | **→ 0** | WordPress. This site is not, and shares no lineage with the two themes that are |
| **Negative** | `ts_reader.run(` | **→ 0** | mangathemesia's reader bootstrap |
| **Negative** | `data-chapter-url-template` | **→ 0** | mangakakalot's client-rendered chapter list |

The negatives here **zero the score outright** rather than subtracting, which is
stronger than `mangakakalot`'s −40 and is warranted by a stronger fact: those
three markers belong to software this site has nothing to do with, so there is
no legitimate page of this theme on which any of them can appear. A page
carrying one is that theme's page whatever else it contains.

A `/series/` link signal was considered and **left out**. It would have been the
easiest signal to add and is the one most likely to leak: a series path is
common enough that scoring it would put points on other families' pages for
nothing. Against the committed corpus this theme scores 65–95 on its own pages
and **0 on every other theme's**, with every other theme scoring **0** on its.

#### Endpoint shapes

| What | Shape |
|---|---|
| Search / Browse | `GET /search/data?text=&sort=&order=Descending&adult=False&limit=32&offset=N&display_mode=Full+Display` → rows fragment |
| Series | `GET /series/{id}/{slug}` → whole page |
| Chapter list | `GET /series/{id}/full-chapter-list` → rows fragment, **newest first** |
| Reader | `GET /chapters/{id}/images?is_prev=False&reading_style=long_strip` → images fragment |

Four things are worth dwelling on.

**Identifiers are opaque, and the slug is decoration.** A series is
`/series/{26-character token}/{slug}`, and the token alone is the identity. The
chapter-list endpoint takes **the bare token** —
`/series/{id}/full-chapter-list`, with no slug in between. Appending the suffix
to the full path answers 404, which costs an afternoon to find because
everything else about the ID works. `urls.go` keeps the two forms apart on
purpose.

**The series page's chapter list is truncated.** It renders the most recent
entries server-side and hides the rest behind the fragment endpoint. Parsing the
series page for chapters therefore returns a plausible *short* list rather than
an error — the newest few, looking entirely healthy — which is the worst kind of
wrong. `Chapters()` never fetches the series page, and
`TestChaptersNeverReadsTheSeriesPage` asserts it.

**`display_mode` is not cosmetic.** The compact row layout omits the title
element entirely. Drop the parameter and the search comes back as a list of
unnamed results.

**The chapter list is newest-first**, which is why `Chapters` reverses it.
`testdata/chapters.html` is descending on purpose, and carries an unnumbered
"Special Chapter" between 5 and 4, so that both halves of the ordering contract
are load-bearing: forgetting to sort is a red test, and *sorting by number* is
also a red test, because it flings the unnumbered entry to one end.

#### `overrides` keys, and why each exists

Two, and both are about *what to show* rather than where things live — the same
shape `mangadex`'s take, and for the same reason: this is one deployment, so
there is no second site to rename a path segment. An override nobody can
usefully set is a knob that only ever gets typed wrong, so the path segments are
constants.

| Key | Default | Why |
|---|---|---|
| `browseSort` | `Popularity` | Browse is a search with an empty query (§7.5, M3 correction 1). The site's default sort is relevance, and relevance against an empty query orders nothing — the browse list comes back meaningless rather than useful. A query, by contrast, always sorts by `Best Match`; the override only applies when there is no query |
| `includeAdultContent` | `false` | The site includes adult-flagged series by default. Quire does not, for the same reason `mangadex` caps its content rating: the device's library is visible from the stock UI, and a browse list is not a place to be surprised. Opt-in, never inferred |

#### `AllowedHosts`

`*.lastation.us`, `*.planeptune.us`, `*.lowee.us`, `*.leanbox.us`,
`*.compsci88.com`.

The third theme to declare anything, and the broadest list so far — five
domains, because this site shards its images across a set of sibling hosts
rather than using one CDN. Every entry is the `*.` form: the apexes serve
nothing, and the hostnames that do are per-shard labels (`scans`, `scans-hot`,
`official`) on domains that exist for no purpose but this.

**What was observed and what was inferred**, because the distinction matters and
the guard makes it invisible once it works:

| Host | Status |
|---|---|
| `*.lastation.us` | **Observed** serving pages, 2026-09-16 |
| `*.planeptune.us` | **Observed** serving pages, 2026-09-16 |
| `*.lowee.us` | **Observed** serving pages (official translations), 2026-09-16 |
| `*.leanbox.us` | **Inferred.** Not seen in the sample taken; included because the four are plainly one deliberately-named family and a missing member is a download that fails with an SSRF rejection naming a host the user has never heard of |
| `*.compsci88.com` | **Observed** serving covers, 2026-09-16 |

Covers are included here, unlike `mangadex`'s, because here they are genuinely
off-domain: search rows and series pages both point their thumbnails at a
separate host, so leaving it out would mean a browse list of empty frames.

#### Quirks

**The page images carry a same-site placeholder in `onerror`.** Every `<img>` in
the reader fragment has
`onerror="...this.src='/static/images/broken_image.jpg'"`. It is a string in an
attribute, not a `src`, but a helper looking for "any image URL on this node" —
which is what `theme.ImageURL` does, and what every WordPress theme here needs —
would pick some of them up. `Pages()` reads `src` and only `src`, and skips
anything under `/static/`. A chapter of broken-image icons is a silent failure;
a missing page is a loud one.

**Covers come in several renditions and the first `<source>` is the best one.**
The markup is a `<picture>` with a `<source>` per breakpoint and an `<img>`
fallback in a lower-quality format. Preferring the source is preferring the
better image, which matters because M4 resizes for a 1620×2160 screen and cannot
add back detail that was never fetched.

**Dates are machine-readable.** Chapter rows carry `<time datetime="...">` in
RFC 3339, so there is no `dateFormat` override here and no locale to get wrong —
the one theme so far where that whole class of bug does not exist. A row without
a `<time>` yields the zero time, which §7.2 says is fine.

**What was actually measured, 2026-09-16.** Run by hand with the honest
`User-Agent`, so nobody has to repeat it:

| Step | Result |
|---|---|
| Home page | **200**, 168 KB. Behind a CDN, **no challenge** — one injected script tag and nothing else |
| Search fragment | **200**, rows parsed, covers and series IDs extracted |
| Series page | **200**, title, cover, authors, tags, status, description, associated names all parsed |
| Chapter-list fragment | **200**, whole list, descending, ascending after the reversal |
| Reader fragment | **200**, page URLs extracted |
| **Page image** on the image host | **200, `image/*`** — fetched, with no `Referer` and no cookie |

That last row is the one that matters and the one the `mangakakalot` family
failed. This is, as of today, **the only theme in the registry whose §7.5
stage-5 image fetch succeeds against a live site with Quire's fetch layer as it
stands.** See "The `Referer` wall" below for why that sentence needs the
qualifier.

---

## The `Referer` wall — decided, implemented, wired

Recorded 2026-09-16. Three otherwise-clean candidates were taken end to end —
search → series → chapters → page URLs → fetch one image — and all three passed
every step **except the last**, in the same way:

| Candidate | Everything up to page URLs | Image host, no `Referer` | Image host, with `Referer` |
|---|---|---|---|
| `webtoons` | **200** throughout; the chapter list is a clean JSON API | **403**, Akamai `Referral Denied` | **200**, `image/jpeg` |
| `fanfox` | **200** throughout; the mobile reader ships every page URL eagerly | **403**, Cloudflare `Attention Required!` | **200**, `image/jpeg` |
| `comick` | **200** throughout on one mirror; the reader page embeds the page list as JSON | **403**, Cloudflare `Attention Required!` | **200**, `image/webp` |

### The decision

**A truthful `Referer` is permitted** (PLAN §7.6, decided 2026-09-16). The test
§7.6 applies is *"are we pretending to be something we are not?"*, and a
`Referer` naming the page a URL was actually extracted from fails to be
pretence: it is a true statement, in the header designed to carry exactly that
fact. Hotlink protection asks "did this come from one of our pages?" and the
honest answer is yes. We satisfy the check **by telling the truth**, which is
the opposite of circumventing it. A challenge asks "are you a browser?", where
the only way through is a lie — that stays refused and stays terminal.

### How it is built, and why in that shape

Three pieces, and the constraints are structural rather than conventional:

- **`fetch.Referrer`** (`backend/fetch/referrer.go`). An opaque value, not a
  string parameter. The **zero value sends no header**, and `Get` /
  `GetRetrieval` are literally `GetFrom` / `GetRetrievalFrom` called with one —
  so "never defaulted" is a property of the code rather than a promise. There
  is deliberately **no client-wide or per-source setting**: one would be pinned
  to a constant within a week, and a constant naming a page we did not fetch is
  exactly the lie §7.6 forbids.
- **`Response.Referrer()`** is the preferred constructor and cannot lie: the
  only way to hold a `*Response` is to have fetched the page, and it uses
  `FinalURL` so a redirect is reflected honestly. `fetch.PageReferrer(url)` is
  the escape for a caller holding an address rather than a response; it
  validates absolute/http(s)/host, and strips **userinfo** (a `Referer` is the
  classic way a credential reaches someone else's access log) and the
  **fragment**. The query survives, because on two of the three sites the
  chapter is identified by nothing else.
- **`theme.PageReferrer`** (`backend/theme/theme.go`). An optional side
  interface: `PageReferer(s *Source, chapterID string) string`. It belongs to
  the theme for the same reason the ordering contract does — only the theme
  knows which URL its `Pages()` read. It is a **pure function of the source and
  chapter ID**, not state left behind by the last call, so it cannot go stale
  or be read for the wrong chapter. In all three themes it is literally the
  same one-line call `Pages()` uses to build its own request, which is what
  keeps the two from drifting apart into a header naming a page we did not
  read.

Tests: sent when supplied, absent when not, never defaulted, and never leaked
to a later request on the same client (`backend/fetch/referrer_test.go`). Each
theme asserts that its `PageReferer` equals the URL its `Pages()` actually
fetched.

### Wired at both call sites (2026-09-16)

Page images are not fetched by themes; they are fetched in two places, and both
now pass a truthful referrer:

- `backend/service/download.go` — the download queue, via `GetRetrievalFrom`
- `backend/probe/prober/capability.go` — §7.5 stage 5, via `GetFrom`

The value **travels with the page** rather than being derived at fetch time:
`PageReferer` is resolved **once per chapter at enqueue time**, where the
chapter is known, and copied onto every job of that chapter. Recovering it later
from an image URL would mean guessing which chapter it came from — the exact lie
§7.6 forbids, and the failure would be silent, every page carrying the first
chapter's referrer. A test pins that each page carries **its own** chapter's.

A theme that does not implement `PageReferrer` yields the zero value and
therefore **no header**, asserted by absence rather than by empty string.

### The strip problem, which is separate and not solved

`webtoons` pages are strips: single images several thousand pixels tall. PLAN
§6 M4 fits a page image into a 3:4 PDF page, so a strip becomes a small centred
sliver with white space either side — output that is technically correct and
unreadable. M4's memory guard bounds the damage and does not change the result.

Making that platform genuinely useful needs real strip-splitting — cutting a
tall image into screen-shaped pages at sensible boundaries — which is its own
design decision and **was deliberately not attempted** while writing the theme.
A theme that quietly half-solved it would be worse than one that does not try.

---

### `webtoons` — a first-party publisher, and a vertical-scroll platform

Implemented in `backend/theme/webtoons/`. The first **publisher** in the
registry rather than an aggregator or a CMS family, and the first theme whose
site has an app, a mobile web surface and a desktop one that disagree.

#### Fingerprint signals

| Page | Signal | Weight | What it is |
|---|---|---|---|
| Viewer | `#_imageList` | 50 | The defining one. One element ID, the images hang off it, nothing else emits it |
| Viewer | `/viewer?title_no=` | 15 | The episode link shape, in the navigation |
| Any listing | `.webtoon_list` | 25 | The card grid |
| Any listing | `data-title-no=` | 25 | The numeric series identifier, on every card |
| Any listing | `.info_text .title` | 15 | The card's title block |
| Any listing | `.image_wrap` | 10 | Its cover wrapper |
| Series | `h1.subj, h3.subj` | 20 | The title — **two heading levels for the same class**, one per tier |
| Series | `.detail_header` | 15 | The metadata block |
| Series | `#_listUl, ._episodeItem` | 20 | The (paginated) episode list |
| Any | `list?title_no=` | 15 | The series link shape every surface agrees on |
| **Negative** | `/wp-content/`, `ts_reader.run(`, `data-chapter-url-template`, `checkNewChapter(` | **→ 0** | madara/mangathemesia, mangathemesia, mangakakalot, weebcentral |

Scores 80–100 on its own four fixtures, 0 on every other theme's.

#### Endpoint shapes

| What | Shape |
|---|---|
| Search / Browse | `GET /{lang}/search?keyword=` — or `/{lang}/search/{tier}?keyword=&page=N` when scoped |
| Series | `GET /{lang}/{genre}/{slug}/list?title_no={n}` |
| Chapter list | `GET https://m.{host}/api/v1/{webtoon\|canvas}/{n}/episodes?pageSize=5000` → JSON |
| Reader | the episode's own `viewerLink` |

Four things are worth dwelling on.

**The chapter list is on a different hostname.** `m.` rather than `www.`, under
the same registrable domain — so PLAN §7.4's boundary permits it with no
`AllowedHosts` entry, and the declaration this theme *does* make is for the
image CDN alone. The desktop series page paginates its episode list ten at a
time; the API returns all of them in one response (652 episodes, measured).

**The two tiers have different names in the path and in the API.** A series at
`/{lang}/canvas/...` is `canvas` to the API; everything else is `webtoon`, a
word that appears nowhere in the site path. The tier is readable from the
series URL, which is why it is not an override. Only the user-submitted tier
takes `readingLanguageCode`; the publisher's own rejects it.

**The chapter number is not in the title.** Episodes are titled `[Season 2]
Ep. 1`, where a number-finding heuristic returns **2** — the season. Ordering by
that puts season 2 episode 1 before season 1 episode 3, which is a volume that
reads out of order: exactly the failure PLAN §7.2's contract exists to prevent.
The API's own `episodeNo` is authoritative and monotonic across seasons, so that
is `Chapter.Number`, and the season becomes `Chapter.Volume` — which is what M4
wants anyway, and is better than its runs-of-ten fallback.

**The unscoped search has no second page.** It answers a fixed set of best
matches per tier and ignores `page`. Sending it anyway would invent results, so
`Search` returns an empty page instead and makes no request.

#### `overrides` keys, and why each exists

| Key | Default | Why |
|---|---|---|
| `searchScope` | `all` | The user-submitted tier is very large and dominates a text search: a query for a licensed title can return a page of fan work before the publisher's own edition. Which tier a reader wants is a preference |
| `fullQualityImages` | `false` | Page URLs carry a rendition parameter and dropping it yields the unresized original. Off by default because M4 downscales to 1620×2160 anyway — the extra bytes are fetched and discarded, and these are already the largest images Quire handles |

#### `AllowedHosts`

`webtoon-phinf.pstatic.net`, `swebtoon-phinf.pstatic.net`.

**Named exactly, with no wildcard** — the only theme so far to do that, and
deliberately. The parent domain is the platform's parent company's general CDN
and carries a great deal that has nothing to do with comics, so `*.` there would
widen §7.4's boundary far past anything this theme needs. Both labels are single
hosts with nothing beneath them.

| Host | Status |
|---|---|
| `webtoon-phinf.pstatic.net` | **Observed** serving pages and thumbnails on both tiers and in two locales, 2026-09-16 |
| `swebtoon-phinf.pstatic.net` | **Inferred.** Not seen in the sample taken; included because a missing host is a download that fails with an SSRF rejection naming something the user has never heard of |

Same treatment as `weebcentral`'s `*.leanbox.us`, and recorded here for the same
reason: the guard makes the distinction invisible once it works.

#### Quirks

**The real URL is in `data-url`, not `src`.** `src` holds a transparent GIF
until the reader scrolls. A theme reading `src` returns a chapter of blank
pixels — which is why `Pages()` does not use `theme.ImageURL`: that helper knows
the lazy-load attributes WordPress plugins use, and this is not one of them.

**The author block holds a button.** Removing only the `<button>` rather than
reading the profile link is what makes both tiers work: the publisher's own
credits are a bare text node with a "more info" button beside it, and the
user-submitted tier's is an anchor.

**There is no status, only a schedule.** The platform publishes "EVERY MONDAY"
where other sites publish a status. That is not one of PLAN §7.2's five values,
so it normalises to `ongoing`, and a series that says it is completed maps to
`completed`. Passing the schedule through would make the UI render a site's own
vocabulary, which §7.2 forbids.

**And the strips.** See above — recorded there because it is a platform
property, not a parsing one, and because someone will otherwise download a
volume and wonder why it looks wrong.

#### What was measured, 2026-09-16

| Step | Result |
|---|---|
| Home page | **200**, 244 KB, no challenge |
| Search | **200**, series and both tiers parsed; tabs and promo links correctly dropped |
| Series page | **200**, title, cover, author, genres, description, schedule all parsed |
| Episode API | **200**, **652 episodes in one response**, ascending, `OrderUnknown=false` |
| Viewer | **200**, page URLs extracted from `data-url` |
| **Page image, no `Referer`** | **403**, Akamai `Referral Denied` |
| **Page image, `Referer` = the viewer page** | **200**, `image/jpeg` |

---

### `fanfox` — the oldest generation here, and real volume structure

Implemented in `backend/theme/fanfox/`. Server-rendered PHP with numbered
utility class names that have plainly accreted over a decade, which makes it
both the least elegant thing in the registry and the easiest to fingerprint.

#### Fingerprint signals

| Page | Signal | Weight | What it is |
|---|---|---|---|
| Reader | `#viewer .reader-page` | 45 | The mobile scroll reader |
| Reader | `.roll-page` | 20 | Its page counter between the images |
| Listing | `.manga-list-{1,2,4}-list` | 30 | The grid families. Nothing else in the registry names a grid this way |
| Listing | `.manga-list-{1,2,4}-item-title` | 20 | Their title blocks |
| Listing | `.manga-list-{1,4}-cover` | 15 | Their covers |
| Series | `.detail-info-right` | 25 | The metadata block |
| Series | `ul.detail-main-list` | 25 | The chapter list, in the page |
| Series | `.detail-main-list-main .title3` | 15 | A chapter row's name |
| Series | `.detail-info-cover-img` | 10 | The cover |
| Any | `/manga/` | 5 | The link shape — weak on purpose |
| **Negative** | `/wp-content/`, `ts_reader.run(`, `data-chapter-url-template`, `checkNewChapter(` | **→ 0** | as above |
| **Negative (selector)** | `#_imageList` | **→ 0** | webtoons' viewer |

Note the negatives are **two lists**, strings and selectors. A substring search
for `#_imageList` finds nothing in a document that spells it `id="_imageList"`,
which is a mistake worth making impossible rather than remembering.

Scores 70–85 on its own five fixtures, 0 on every other theme's.

#### Endpoint shapes

| What | Shape |
|---|---|
| Search | `GET /search?title=&page=N&stype=1` → `ul.manga-list-4-list` |
| Browse | `GET /directory/` , `/directory/{N}.html` , optionally `?latest` |
| Series | `GET /manga/{slug}/` — chapter list included |
| Reader | `GET https://m.{host}/roll_manga/{slug}/v01/c006/1.html` |

Four things are worth dwelling on.

**`stype=1` is load-bearing.** Without it the search endpoint searches nothing
and answers an empty list, which looks exactly like "no results" and is not.

**Browse pages are files and the sort has no value.** `/directory/3.html`, not
`?page=3`; and `?latest`, which `url.Values` would render as `latest=` — at
which point the server ignores it and sorts by rank instead. Both are unlike
anything else in the registry and both have a test.

**The desktop reader is unusable and the mobile one is not.** The desktop reader
hands out one page at a time behind an obfuscated script. The mobile host serves
a scroll view that ships every page URL in the markup at once, so `Pages()`
changes host **and** rewrites one path segment — `manga` → `roll_manga`. The
mobile host is a sibling under the same registrable domain, so §7.4 permits it
with no `AllowedHosts` entry.

**The volume is in the URL.** `/{slug}/v01/c006/1.html`. This is one of the few
sites here that publishes real volume structure, so `Chapter.Volume` is read
from the path rather than from the rendered title — the path is generated and
the title is a free-text field an uploader fills in. `v00` is a real volume
(extras and prologues live there), so its zeros are not trimmed away to nothing.

#### `overrides` keys, and why each exists

| Key | Default | Why |
|---|---|---|
| `browseOrder` | `popular` | Browse is a search with an empty query (§7.5, M3 correction 1) and this site's search cannot answer one, so Browse reads the directory. The directory has two orders answering different questions — "what is popular" and "what moved today" |
| `dateFormat` | `MMM d,yyyy` | Chapter dates follow a server-side locale setting, as in madara and mangathemesia. Relative dates ("Today", "3 days ago") are recognised without it; this is for the absolute ones |

#### `AllowedHosts`

`*.mangafox.me`, `*.mfcdn.net`.

Two different registrable domains, neither of them the site's: pages come from
one and covers from the other, both leftovers from names the platform used to
operate under. Both observed serving on 2026-09-16. The wildcard form is the
honest one — the hosts are shard labels (`zjcdn`, `fmcdn`) and the apexes serve
nothing.

#### Quirks

**The real URL is in `data-original`; `src` is a loading animation.** Same
lesson as `webtoons`, different attribute. A theme reading `src` returns a
chapter of spinners.

**Image URLs are protocol-relative.** `//host/path` throughout, which inherits
the base's scheme — https — so a page image never silently downgrades. There is
a test asserting it.

**Licensed series are visible and unreadable.** A series the publisher has
licensed keeps its page, its chapter list and its working links, and its reader
answers **200** with "it's licensed and not available" where the images should
be. `Pages()` matches that and says so. Two reasons it matters: an empty chapter
reads as a Quire bug rather than the site's decision, and one of those two is
worth retrying while the other never will be. It is emphatically **not** a
challenge — no interstitial, no 403, no CDN marker — and the error does not call
it one.

#### What was measured, 2026-09-16

| Step | Result |
|---|---|
| Home page | **200**, 163 KB, no challenge |
| Search (`stype=1`) | **200**, rows parsed |
| Browse (`/directory/2.html?latest`) | **200**, rows parsed |
| Series page | **200**, title, status, authors, genres, cover, full description, and the whole chapter list with volumes |
| Mobile reader | **200**, all 6 page URLs of the chapter |
| **Page image, no `Referer`** | **403**, Cloudflare `Attention Required!` |
| **Page image, `Referer` = the reader page** | **200**, `image/jpeg` |

---

### `comick` — an application's own backend, and a mirror set that disagrees

Implemented in `backend/theme/comick/`. The second JSON API in the registry
after `mangadex`, and a different animal from it: `mangadex` is a documented,
versioned, public API with published rate limits, and this is an application's
own frontend backend — undocumented, and half of it not endpoints at all but
JSON embedded in an HTML page.

#### Mirrors — read this before pointing anything at it

**The mirrors do not behave alike.** On 2026-09-16 one hostname answered a
**browser challenge (403, ~5.6 KB interstitial)** on the same `/api/search` path
another answered **200 JSON** on. PLAN §7.6 is clear about what a challenge
means and §7.5 stage 3 refuses such a source outright, so a user who points
Quire at the challenged host gets `blocked_challenge` and a verdict that says
why.

**The theme therefore names no host, and neither does this file.** PLAN §1.3 is
the answer: Quire ships no source URLs, the user supplies the host, and the
probe tells them whether it works. Recommending a mirror would be shipping a
source list one entry at a time, and it would age badly — the challenged and
unchallenged hosts can trade places without notice. This is also why
`Fingerprint` has **no host gate**, unlike `mangadex`'s: a host gate here would
mean a theme that stops recognising its own site the week it moves.

#### Fingerprint signals

| Page | Signal | Weight | What it is |
|---|---|---|---|
| Series | `#comic-data` | 45 | The series page's own JSON payload |
| Reader | `#sv-data` | 45 | The chapter page's |
| API | `"next_cursor"` **and** `"data"` | 35 | The search envelope, for a probe that lands on it |
| Any | `/api/comics/` | 25 | The chapter-list API path |
| Any | `/chapter-list` | 20 | Its suffix |
| Any | `/api/search` | 15 | The search path |
| Any | `/comic/` | 10 | The link shape |
| Any | `"hid"` | 15 | The identifier vocabulary — weak, plenty of JSON has one |
| Any | `"chap"` | 10 | Likewise |
| **Negative** | `/wp-content/`, `ts_reader.run(`, `data-chapter-url-template`, `checkNewChapter(` | **→ 0** | as above |
| **Negative (selector)** | `#_imageList`, `#chapter-images`, `#viewer .reader-page` | **→ 0** | webtoons, weebcentral, fanfox |

Scores 65–70 on its own three fixtures and 0 on every other theme's — including
`mangadex`'s, which is the pair worth checking: two JSON APIs in one registry is
where "it answered JSON" would have become a fingerprint if anyone let it.

#### Endpoint shapes

| What | Shape |
|---|---|
| Search | `GET /api/search?q=&type=comic[&page=N]` → `{data:[…], next_cursor, …}` |
| Series | `GET /comic/{slug}` → HTML carrying `<script type="application/json" id="comic-data">` |
| Chapter list | `GET /api/comics/{slug}/chapter-list?lang={lang}` → `{data:[{hid,chap,title,vol,lang,group_name,created_at}]}` |
| Reader | `GET /comic/{slug}/{hid}-chapter-{chap}-{lang}` → HTML carrying `<script type="application/json" id="sv-data">` |

Four things are worth dwelling on.

**Two of the four are embedded payloads, and that is an upgrade rather than a
fallback.** They are not a scrape of rendered markup — they are the data the
page is built from, served as JSON in a script element of declared type. The
payload is *more* complete than the page (which truncates the description and
hides the alternate titles behind a control) and considerably more stable than
whatever its JavaScript does with it afterwards.

**The payload is matched by identifier *and* declared type.** The page carries a
dozen other script elements, several third-party, and one deliberately named
`sv-data-init`. Matching on the identifier alone means eventually handing
someone else's payload to a JSON parser.

**A chapter is addressed by a composite of three fields** — `{hid}-chapter-
{chap}-{lang}` — and it wants all three. The identifier alone answers **404**,
which was checked rather than assumed. A one-shot has no chapter number and the
segment is built with it empty, which is what the site's own frontend does with
the same template; that case is **inferred rather than observed**.

**`type=comic` is not optional.** Without it the search endpoint also answers
with people and groups, which are not series and have no chapters.

#### `overrides` keys, and why each exists

| Key | Default | Why |
|---|---|---|
| `maxContentRating` | `suggestive` | Every result carries a rating. **Applied to the response, not sent as a query parameter**: the endpoint accepts a parameter of that name but its semantics are undocumented — maximum, exact match or repeated key — and guessing wrong drops results silently. Filtering what came back cannot. An entry the site never rated is *kept*: refusing to show something because the site forgot to classify it is the wrong way round |
| `includeAllLanguages` | `false` | The chapter list is filtered to the source's language. Listing every language produces duplicate chapter numbers, and §7.2's ordering contract has nothing sensible to do with two chapter 5s |

#### `AllowedHosts`

`*.comicknew.pictures`.

One entry, wildcard, and both halves deliberate: the payload names the shard in
a `cdn_id` field and the hostnames are numbered labels on that one domain, so
the wildcard is the shape of the fact and the apex serves nothing. **No second
domain is named**, although this software has used others in the past —
widening §7.4's boundary on the strength of a hostname nobody observed is worse
than a download that fails with the host named in the error.

#### Quirks

**Status is an integer.** `1` ongoing, `2` completed, `3` cancelled — all three
observed directly across a search of forty series on 2026-09-16. `4` → hiatus is
**inferred** from the gap and from the vocabulary every site of this kind uses.
Anything else becomes `StatusUnknown` rather than being invented.

**Chapter titles come from two fields.** A number and, separately and often
emptily, a name. Neither alone is enough — a bare number is a poor PDF title and
a bare name loses the position in the series — so they are combined, and a
chapter with neither is called "Oneshot".

#### What was measured, 2026-09-16

| Step | Result |
|---|---|
| `/api/search?q=&type=comic` | **200**, JSON, 61 KB |
| `/comic/{slug}` | **200**, `#comic-data` payload present and parsed |
| `/api/comics/{slug}/chapter-list?lang=en` | **200**, whole list, descending, ascending after the reversal |
| Reader page | **200**, `#sv-data` payload, **113 page URLs** |
| `robots.txt` | `Allow: /`, with `/user*` and an auth callback disallowed |
| **A second mirror, same `/api/search`** | **403**, challenge interstitial — terminal under §7.6 |
| **Page image, no `Referer`** | **403**, Cloudflare `Attention Required!` |
| **Page image, `Referer` = the chapter page** | **200**, `image/webp` |

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

## Writing the next theme — the short version

1. New package under `backend/theme/`. Implement `theme.Theme` (eight methods
   as of 2026-09-15 — the six of PLAN §7.2 plus `AllowedHosts` and
   `SuggestedName`) and
   `theme.OverrideValidator`.
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
6. Implement `SuggestedName()`. `""` is the right answer for a site family —
   the page title is a better default than anything the theme could invent —
   and a real name is right only for a theme that drives exactly one site.
   Never clean a title.
7. Add a section here: fingerprint signals with weights, endpoint table,
   overrides table with a "why" column, and the quirks that cost you an hour.
8. Return chapters in **ascending reading order** (PLAN §7.2). Call
   `theme.SortAndMark` and make the fixture adversarial — if it is already
   ascending, the test proves nothing. See the chapter-ordering section above
   for what to do when the order is genuinely unknowable.
9. Implement `AllowedHosts()`. **nil is the right answer for a site family**
   — hundreds of independent installs have no CDN in common, and naming one
   widens the redirect boundary for all of them. Return hosts only if the
   theme's own images genuinely live on another registrable domain, and check
   the fixtures rather than assuming.
10. If the theme needs a request that robots disallows, read §7.4 and the
   `mangadex` section before reaching for `GetRetrieval`. The bar is "the user
   named this thing", not "this request is inconvenient to lose" — and note
   that since 2026-09-16 the consultation is off by default, so `Kind` is
   documentation of intent and the answer to "which is this?" unless the
   setting is on. Classify honestly anyway: the setting can be turned on, and
   the kind is what the suppressed-check log line reports.
11. If it is a JSON API rather than a markup family, say so at the top of its
   section the way `mangadex` does, and gate the fingerprint on something that
   cannot be worn by accident. A JSON envelope is not a fingerprint.
