package bookrender

import (
	"strings"
	"testing"
)

func TestComposeCSSDefaults(t *testing.T) {
	css, font := ComposeCSS(Defaults(), func(string) bool { return true })
	if font != FontBook {
		t.Errorf("effective font = %q, want %q", font, FontBook)
	}
	if css != "@page{margin:1.4em 1.8em}" {
		t.Errorf("css = %q", css)
	}
}

func TestComposeCSSMargins(t *testing.T) {
	cases := []struct {
		margins string
		want    string
	}{
		{MarginsNarrow, "@page{margin:0.8em 1em}"},
		{MarginsNormal, "@page{margin:1.4em 1.8em}"},
		{MarginsWide, "@page{margin:2em 3em}"},
		{"bogus", "@page{margin:1.4em 1.8em}"}, // unknown falls back to normal
	}
	for _, c := range cases {
		s := Defaults()
		s.Margins = c.margins
		css, _ := ComposeCSS(s, func(string) bool { return true })
		if css != c.want {
			t.Errorf("margins=%q: css = %q, want %q", c.margins, css, c.want)
		}
	}
}

func TestComposeCSSFontMissingFallsBackSilently(t *testing.T) {
	s := Defaults()
	s.Font = FontGaramond
	css, font := ComposeCSS(s, func(string) bool { return false })
	if font != FontBook {
		t.Errorf("effective font = %q, want fallback to %q", font, FontBook)
	}
	if strings.Contains(css, "font-family") {
		t.Errorf("css should not reference a missing font: %q", css)
	}
}

func TestComposeCSSGaramondPresent(t *testing.T) {
	s := Defaults()
	s.Font = FontGaramond
	css, font := ComposeCSS(s, func(string) bool { return true })
	if font != FontGaramond {
		t.Errorf("effective font = %q, want %q", font, FontGaramond)
	}
	if !strings.Contains(css, garamondRegular) || !strings.Contains(css, garamondItalic) {
		t.Errorf("css missing garamond font-face(s): %q", css)
	}
}

func TestComposeCSSNotoNoItalicFile(t *testing.T) {
	s := Defaults()
	s.Font = FontNoto
	// Noto has no italic file in the contract; only the regular file's
	// presence should matter.
	exists := func(p string) bool { return p == notoRegular }
	css, font := ComposeCSS(s, exists)
	if font != FontNoto {
		t.Errorf("effective font = %q, want %q", font, FontNoto)
	}
	if !strings.Contains(css, notoRegular) {
		t.Errorf("css missing noto font-face: %q", css)
	}
}

func TestComposeCSSSpacing(t *testing.T) {
	cases := []struct {
		spacing string
		want    string
	}{
		{SpacingBook, ""},
		{SpacingNormal, "1.45"},
		{SpacingRelaxed, "1.7"},
	}
	for _, c := range cases {
		s := Defaults()
		s.Spacing = c.spacing
		css, _ := ComposeCSS(s, func(string) bool { return true })
		if c.want == "" {
			if strings.Contains(css, "line-height") {
				t.Errorf("spacing=book should not override line-height: %q", css)
			}
			continue
		}
		if !strings.Contains(css, "line-height:"+c.want) {
			t.Errorf("spacing=%q: css = %q, want line-height %s", c.spacing, css, c.want)
		}
	}
}

func TestComposeCSSAlignLeft(t *testing.T) {
	s := Defaults()
	s.Align = AlignLeft
	css, _ := ComposeCSS(s, func(string) bool { return true })
	if !strings.Contains(css, "text-align:left") {
		t.Errorf("css missing text-align override: %q", css)
	}
}

func TestEmForSizeClampsAndMaps(t *testing.T) {
	if got := EmForSize(4); got != 12 {
		t.Errorf("EmForSize(4) = %v, want 12", got)
	}
	if got := EmForSize(0); got != EmForSize(SizeMin) {
		t.Errorf("EmForSize(0) should clamp to the minimum")
	}
	if got := EmForSize(99); got != EmForSize(SizeMax) {
		t.Errorf("EmForSize(99) should clamp to the maximum")
	}
}
