package bookrender

import (
	"fmt"
	"strconv"
	"strings"
)

// Reader settings' enum values (books-contract.md §B, Settings). These are
// wire values too — sent back in BookOpened's settings and fontChoices, and
// accepted by SetReaderSettings — so they are spelled out rather than
// derived from anything a rename could silently break.
const (
	FontBook     = "book"
	FontGaramond = "garamond"
	FontNoto     = "noto"

	MarginsNarrow = "narrow"
	MarginsNormal = "normal"
	MarginsWide   = "wide"

	SpacingBook    = "book"
	SpacingNormal  = "normal"
	SpacingRelaxed = "relaxed"

	AlignBook = "book"
	AlignLeft = "left"
)

// SizeMin and SizeMax are the reader's font-size step range.
const (
	SizeMin = 1
	SizeMax = 9
)

// sizeEm maps a 1..9 step to a point size (books-contract.md §B, Settings).
// Index 0 is unused so the step number reads directly as the index.
var sizeEm = [...]float64{0, 9, 10, 11, 12, 13, 14, 16, 18, 20}

// EmForSize returns the point size for a 1..9 step, clamped to that range so
// a corrupt or out-of-range stored value never turns into a nonsense layout
// call.
func EmForSize(size int) float64 {
	if size < SizeMin {
		size = SizeMin
	}
	if size > SizeMax {
		size = SizeMax
	}
	return sizeEm[size]
}

// FontChoice is one entry of BookOpened's fontChoices: an id SetReaderSettings
// accepts, and the label the backend composes for it (PLAN §2 — the UI shows
// only what it is given, never invents a label of its own).
type FontChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// FontChoices is the backend's own list, in display order.
func FontChoices() []FontChoice {
	return []FontChoice{
		{ID: FontBook, Label: "The book's own"},
		{ID: FontGaramond, Label: "EB Garamond"},
		{ID: FontNoto, Label: "Noto Sans"},
	}
}

// Device font paths (docs/DEVICE-NOTES.md). FontExists lets a test substitute
// a fake filesystem; production passes nil, which checks the real one via
// os.Stat.
const (
	garamondRegular = "/usr/share/fonts/ttf/ebgaramond/EBGaramond-VariableFont_wght.ttf"
	garamondItalic  = "/usr/share/fonts/ttf/ebgaramond/EBGaramond-Italic-VariableFont_wght.ttf"
	notoRegular     = "/usr/share/fonts/ttf/noto/NotoSans-VariableFont_wdth,wght.ttf"
)

// Settings is the reader's global, per-book-independent configuration
// (books-contract.md §B). The zero value is not valid — use Defaults().
type Settings struct {
	Font    string
	Size    int
	Margins string
	Spacing string
	Align   string
}

// Defaults is the reader's out-of-the-box configuration: font book, size 4
// (12pt), margins normal, spacing book, align book.
func Defaults() Settings {
	return Settings{Font: FontBook, Size: 4, Margins: MarginsNormal, Spacing: SpacingBook, Align: AlignBook}
}

// marginsEm returns the top/bottom and left/right margin, in em, for one of
// the three margin settings (books-contract.md §B, Settings): narrow
// 0.8/1, normal 1.4/1.8, wide 2/3. An unrecognised value falls back to
// normal, silently — the same rule a missing font falls back to "book" with.
func marginsEm(margins string) (tb, sides float64) {
	switch margins {
	case MarginsNarrow:
		return 0.8, 1
	case MarginsWide:
		return 2, 3
	default:
		return 1.4, 1.8
	}
}

// lineHeight returns the CSS line-height for a spacing setting, or "" for
// "book" — the publisher's own line height, left untouched.
func lineHeight(spacing string) string {
	switch spacing {
	case SpacingNormal:
		return "1.45"
	case SpacingRelaxed:
		return "1.7"
	default:
		return ""
	}
}

// FontExistsFunc reports whether a font file exists on disk, so ComposeCSS
// can fall back silently (and let the caller log it) when a chosen font is
// missing from this build's font set.
type FontExistsFunc func(path string) bool

// ComposeCSS builds the user CSS for one Layout call: `@page` margins always,
// a font override via @font-face when one is chosen and its files exist, a
// line-height override for a non-default spacing, and a text-align override
// for "left" alignment. It reports which font id was actually used — the
// caller logs a mismatch with the requested one rather than silently keeping
// two ideas of the current font.
//
// exists is FontExistsFunc; a nil exists checks the real filesystem.
func ComposeCSS(s Settings, exists FontExistsFunc) (css string, effectiveFont string) {
	if exists == nil {
		exists = defaultFontExists
	}

	var b strings.Builder
	tb, sides := marginsEm(s.Margins)
	fmt.Fprintf(&b, "@page{margin:%sem %sem}", trimFloat(tb), trimFloat(sides))

	effectiveFont = s.Font
	switch s.Font {
	case FontGaramond:
		if exists(garamondRegular) {
			b.WriteString("@font-face{font-family:'Q';src:url('" + garamondRegular + "')}")
			if exists(garamondItalic) {
				b.WriteString("@font-face{font-family:'Q';font-style:italic;src:url('" + garamondItalic + "')}")
			}
			b.WriteString("body,p,div,span{font-family:'Q' !important}")
		} else {
			effectiveFont = FontBook
		}
	case FontNoto:
		if exists(notoRegular) {
			b.WriteString("@font-face{font-family:'Q';src:url('" + notoRegular + "')}")
			b.WriteString("body,p,div,span{font-family:'Q' !important}")
		} else {
			effectiveFont = FontBook
		}
	}

	if lh := lineHeight(s.Spacing); lh != "" {
		fmt.Fprintf(&b, "body,p,div,span{line-height:%s !important}", lh)
	}
	if s.Align == AlignLeft {
		b.WriteString("body,p,div,span{text-align:left !important}")
	}

	return b.String(), effectiveFont
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func defaultFontExists(path string) bool {
	return statExists(path)
}
