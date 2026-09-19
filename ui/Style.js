// Quire's shared look — PLAN §6 M3: "Match the stock UI palette, and spend
// colour only where it carries meaning. Do not invent a brand."
//
// **The rule used to be "no accent colour at all", on the grounds that the
// device is greyscale e-ink. That premise was simply wrong.** The panel
// reports itself as "reMarkable Ferrari" (Paper Pro) and is a Gallery 3
// colour display — the stock UI ships colour pens and highlighters, and
// backend/covers already renders covers in colour. So the old rule was
// protecting against a constraint that does not exist.
//
// What replaced it is not "colour is fine now". Gallery 3 refreshes a colour
// region more slowly than black on white and ghosts it harder, so colour is
// rationed: **one** accent, below, and it appears only where it means
// something — live state, what is currently active, and the headings that let
// you find your place on a screen. Body text, rules, hairlines, row text,
// buttons and the context menu are all still black on white — as, since the
// measurement below, is every other piece of text in the app.
//
// And the hard half of the rule, which outlives any palette: **colour is
// never the only carrier of meaning.** The active half of the layout switch
// is already the filled one and the other half is already dead to touch; the
// current source chip is already filled; a source that did not answer is
// already named in words. The accent joins those signals. Anything that could
// only be told apart by hue is a bug, not a style choice — a e-ink panel in
// direct sun, and a reader who cannot distinguish the hue, both have to get
// the same answer.
//
// ---------------------------------------------------------------------------
// **Measured on the device, 2026-09-19: the accent is only ever a solid area.
// Never a glyph, never a hairline, never a thin stroke.**
//
// The accent used to be spent on *text* — screen titles, section headings, the
// badge count, the progress line, the active toggle's word, the page
// indicator. On hardware every one of them read muddy: the strokes smeared at
// their edges and the type looked a size smaller than the black beside it.
//
// The cause is the panel, not the hue, which is why picking a different orange
// would not have fixed it. Gallery 3 draws black at the panel's full
// monochrome resolution, but a coloured pixel is composed through the colour
// filter array, at a fraction of that resolution. A letterform is almost all
// edge, so it loses the part of itself that carries the shape. A large solid
// region has almost no edge in proportion to its area, so it keeps its colour
// cleanly and its outline is where the black around it stops.
//
// The ~5.2:1 measured for the old deep orange on paper was correct and beside
// the point: a contrast ratio describes a solid block, not a letterform seen
// through a filter. It answered a question nobody was failing.
//
// So every piece of text in ui/ is Style.ink, and the accent became shapes:
// a filled rule under a heading (ui/AccentRule.qml), a small solid square
// beside a line (ui/AccentMark.qml), and a solid fill behind black text on the
// handful of controls that were already filled. Nothing else. If a future
// change wants the accent on a word, it is asking for the muddy text back.
// ---------------------------------------------------------------------------
//
// No second typeface: that part of "do not invent a brand" stands unchanged.
//
// Everything else in the UI rules follows from the panel:
//   - No animations, transitions or fades. E-ink ghosts, and a control that
//     redraws continuously leaves a smear behind it.
//   - No spinners. Progress is discrete text that changes a few times.
//   - Big touch targets. A finger on an e-ink panel has no hover state and no
//     second chance.
.pragma library

var paper = "#FFFFFF"
var ink = "#000000"
var muted = "#6B6B6B"
var rule = "#B4B4B4"
var pressed = "#E4E4E4"
var panel = "#F4F4F4"

// The one accent. Every coloured pixel in ui/ comes from this name; there is
// no palette to pick from and adding a second colour is a design decision,
// not a local one.
//
// **The value moved when the rule above did, and the two contrast figures it
// has to satisfy are now different ones.** It was #C2410C, chosen for ~5.2:1
// against paper because it had to carry text. It carries no text any more; it
// is a solid area, and a solid area answers to two other numbers:
//
//   - **black text ON it: 5.90:1.** The toggle's active half, the current
//     source chip and the new-chapters badge are accent fills with black
//     labels on them. This is the trap in this change: black on the old
//     #C2410C is 4.06:1 — *worse* than the coloured text being replaced, and
//     under the 4.5:1 body text needs. Of the three ways out (lighten the
//     fill, keep the deep fill and use paper-coloured text, or move the accent
//     beside the text) this takes the first, because it is the only one that
//     keeps every glyph black. Paper-coloured text on a deep fill would put
//     letterforms back inside the colour region and walk straight back into
//     the muddiness measured above; black glyphs sit in the panel's
//     full-resolution channel whatever is behind them.
//   - **against paper: 3.56:1.** Not the 4.5:1 text needs, and it does not
//     need to be: every use is a block or a rule, and 3:1 is the threshold for
//     a graphical object. #C2410C's 5.2:1 bought nothing once the text left.
//
// Still not pale. #F97316, the bright orange rejected before, is 2.80:1 on
// paper and washes toward cream on this panel in daylight; this is a stop
// short of it and still reads as a deep orange. It is also dark enough
// (relative luminance 0.245 against paper's 1.0) that a fill drawn in it is
// still visibly *the filled one* with the hue taken away — which is what keeps
// the "colour is never the only signal" rule true for the toggle and the chip.
//
// One value, as before. A second, lighter tint for fills is not needed: this
// one already clears black text, which is the only thing that wanted a tint.
var accent = "#EA580C"

// The two shapes the accent is allowed to take, in device-independent pixels.
// Both are deliberately solid and blunt — see the measurement above.
//
// accentRule is the bar under a screen title or a section heading
// (ui/AccentRule.qml). Thick enough to be an area rather than a hairline: a
// 1px coloured line is the same failure as a coloured glyph, drawn sideways.
//
// accentMark is the square set beside a line that is live — a download in
// flight, the page you are on, a source that did not answer
// (ui/AccentMark.qml). Sized against smallSize type so it reads as a marker
// next to the words and not as a second heading; a page of rows with one of
// these on each is still a page of black text.
var accentRule = 6
var accentMark = 18

// Type scale, in points.
var titleSize = 26
var headingSize = 20
var bodySize = 15
var smallSize = 12

// Spacing and touch targets, in device-independent pixels.
var gap = 16
var margin = 32
var rowHeight = 96
var buttonHeight = 72

// A list row that carries a cover (ui/CoverArt.qml). Taller than an ordinary
// row on purpose, and its own token rather than a bigger rowHeight: rowHeight
// is also the search bar, the confirm strips, the pager and a context menu
// item, and none of those are improved by growing.
//
// **It is a deliberate trade, settled on the device over three passes.** Every
// paged screen works out how many rows fit by dividing the viewport by this,
// so a taller row is a larger thumbnail *and* fewer series per page: 96 gave
// ~19 rows, 132 gave ~14, and this gives ~8.
//
// 96 and 132 were both judged too small in use, which is worth recording
// because the reason is not obvious from the number. The comparison that
// matters is not the row, it is the rest of the screen: the grid draws three
// columns, so a tile's cover is ~518x777 and a page holds about six. Beside
// that, a 116px thumbnail reads as a bullet point rather than a cover.
//
// ~8 rows a page is still more than the grid's six, which is the point at
// which the two views stop being different things — so this is close to the
// useful ceiling, not an arbitrary stop.
var coverRowHeight = 216
var hairline = 1

// The shape of a cover, as height ÷ width. Covers are published 2:3 and the
// cache writes them 300x450 (backend/covers/covers.go), so a tile and a row's
// thumbnail are both the shape of the file on disk and neither has to crop.
var coverRatio = 1.5

// The cover on a list row (ui/CoverArt.qml). Derived from the row rather than
// chosen, and deliberately *smaller* than it: the row's height is what every
// paged screen divides the viewport by, so a thumbnail that set the row's
// height would decide the page size as a side effect. The gap is the breathing
// room above and below it.
//
// It is drawn from the same cache entry the tiles use. The file is already
// thumbnail-sized — the cache downscales to 300x450 and MangaDex is asked for
// its pre-scaled .512.jpg (backend/theme/mangadex/mangadex.go) — so drawing it
// at row height costs no second request, no second cache entry and no guessed
// size suffix that a source would answer with a 404.
var thumbHeight = coverRowHeight - gap
var thumbWidth = Math.round(thumbHeight / coverRatio)

// How long a finger has to stay down, in milliseconds (ui/HoldArea.qml).
//
// Both numbers are about the panel rather than about taste. The refresh is
// slow enough that "did my hold register?" is a genuine question, and the
// answer has to arrive well before the menu does — a hold with no
// acknowledgement is one people lift out of early and then report as broken.
// So the item darkens at 150ms, which is soon enough to read as a response to
// the finger and late enough that an ordinary tap never flashes it, and the
// menu follows at 500ms, the shortest hold that a scroll-less, flick-less
// screen full of tap targets cannot produce by accident. Qt's own 800ms
// pressAndHold was measured against a mouse and feels stuck here.
var pressFeedbackDelay = 150
var holdDelay = 500
