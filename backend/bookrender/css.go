// Package bookrender's CSS composer takes its settings values from
// backend/state (the reader settings' one source of truth, per
// books-contract.md §B) and turns them into what render.js's "layout"
// command needs: a point size and a CSS string.
package bookrender

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rickl/quire/backend/state"
)

// Settings is an alias for state.ReaderSettings, kept under this package's
// own name so callers that only deal with rendering do not have to import
// backend/state just to spell the type out.
type Settings = state.ReaderSettings

// Re-exported enum values and range, so bookrender's own callers (and its
// tests) do not have to import backend/state for them either.
const (
	FontBook     = state.ReaderFontBook
	FontGaramond = state.ReaderFontGaramond
	FontNoto     = state.ReaderFontNoto

	MarginsNarrow = state.ReaderMarginsNarrow
	MarginsNormal = state.ReaderMarginsNormal
	MarginsWide   = state.ReaderMarginsWide

	SpacingBook    = state.ReaderSpacingBook
	SpacingNormal  = state.ReaderSpacingNormal
	SpacingRelaxed = state.ReaderSpacingRelaxed

	AlignBook = state.ReaderAlignBook
	AlignLeft = state.ReaderAlignLeft

	SizeMin = state.ReaderSizeMin
	SizeMax = state.ReaderSizeMax
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

// SettingsChoice is one button of a SettingsField's choices: an id
// SetReaderSettings accepts, and the label the backend composes for it
// (PLAN §2 — the UI shows only what it is given, never invents a label of
// its own).
type SettingsChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// SettingsStep is one rung of the size field's stepper: the 1..9 step
// SetReaderSettings accepts, and the point size it maps to, spelled out as a
// label so the panel never has to compose "N pt" itself.
type SettingsStep struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// SettingsField is one row of the "Aa" panel: a key SetReaderSettings's
// payload uses, the backend's own label and one-sentence explanation of what
// it does (PLAN §2), and either Choices (font, margins, spacing, align) or
// Steps (size) — never both. QML renders this generically; it invents no
// wording of its own.
type SettingsField struct {
	Key     string           `json:"key"`
	Label   string           `json:"label"`
	Help    string           `json:"help"`
	Choices []SettingsChoice `json:"choices,omitempty"`
	Steps   []SettingsStep   `json:"steps,omitempty"`
}

// SettingsNote is shown under the "Aa" panel's own top bar: these settings
// are global, not per-book (books-contract.md §B), and the panel says so
// once rather than leaving that to be discovered by surprise.
const SettingsNote = "These apply to every book you read in Quire."

// fontChoices is the backend's own list, in display order.
func fontChoices() []SettingsChoice {
	return []SettingsChoice{
		{ID: FontBook, Label: "The book’s own"},
		{ID: FontGaramond, Label: "EB Garamond"},
		{ID: FontNoto, Label: "Noto Sans"},
	}
}

// sizeSteps turns sizeEm into the labels the "Text size" field shows,
// deriving them from the very table EmForSize uses rather than keeping a
// second copy that could drift from it.
func sizeSteps() []SettingsStep {
	steps := make([]SettingsStep, 0, SizeMax-SizeMin+1)
	for id := SizeMin; id <= SizeMax; id++ {
		steps = append(steps, SettingsStep{ID: id, Label: fmt.Sprintf("%d pt", int(sizeEm[id]))})
	}
	return steps
}

// SettingsFields is the "Aa" panel's whole description, in display order —
// what BookOpened and BookRelaid carry so the panel needs to invent nothing
// (PLAN §2, books-contract.md §B).
func SettingsFields() []SettingsField {
	return []SettingsField{
		{
			Key: "font", Label: "Font",
			Help:    "The typeface the text is set in. The book’s own keeps the fonts it was published with.",
			Choices: fontChoices(),
		},
		{
			Key: "size", Label: "Text size",
			Help:  "How large the text is. Bigger text means fewer words per page.",
			Steps: sizeSteps(),
		},
		{
			Key: "margins", Label: "Page margins",
			Help: "The blank space between the text and the edges of the screen.",
			Choices: []SettingsChoice{
				{ID: MarginsNarrow, Label: "Narrow"},
				{ID: MarginsNormal, Label: "Normal"},
				{ID: MarginsWide, Label: "Wide"},
			},
		},
		{
			Key: "spacing", Label: "Line spacing",
			Help: "The space between lines of text. The book’s own keeps the spacing it was published with.",
			Choices: []SettingsChoice{
				{ID: SpacingBook, Label: "The book’s own"},
				{ID: SpacingNormal, Label: "Normal"},
				{ID: SpacingRelaxed, Label: "Relaxed"},
			},
		},
		{
			Key: "align", Label: "Alignment",
			Help: "Quire doesn’t hyphenate, so justified text can leave wide gaps between words. Left-aligned avoids them.",
			Choices: []SettingsChoice{
				{ID: AlignBook, Label: "The book’s own"},
				{ID: AlignLeft, Label: "Left-aligned"},
			},
		},
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

// Defaults is the reader's out-of-the-box configuration; see
// state.DefaultReaderSettings.
func Defaults() Settings {
	return state.DefaultReaderSettings()
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
