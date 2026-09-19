// The solid accent square set beside a line that is live: a chapter that is
// downloading, the page you are on, the sources that did not answer, the
// Watching button with news behind it.
//
// **Beside the words, never on them and never around them.** This is the third
// of the three ways of pairing the accent with text (ui/Style.js): the mark is
// a solid area on paper and the sentence next to it stays black on paper, so
// neither one is a coloured letterform and neither depends on the other being
// legible. The words are still the whole answer — the mark says "this line is
// the one moving", and a reader who sees no colour has lost nothing but the
// shortcut for finding it.
//
// Square rather than a dot or a chevron: a circle at this size is mostly
// anti-aliased edge, which is the one thing the colour filter array renders
// badly, and a chevron is a glyph.
import QtQuick 2.5
import "Style.js" as Style

Rectangle {
    width: Style.accentMark
    height: Style.accentMark
    color: Style.accent
}
