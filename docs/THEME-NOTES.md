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

## Per-theme notes

*(populated during M2 — one section per theme: fingerprint signals, endpoint
shapes, `overrides` keys and why each exists, quirks encountered)*
