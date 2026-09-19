// The filled accent bar that sits under a screen title or a section heading.
//
// It exists as a component rather than as a Rectangle copied into each screen
// because it is half of a rule that is easy to half-forget: the heading is
// black and the *bar* is the accent (ui/Style.js — "the accent is only ever a
// solid area"). A heading written by hand is a heading somebody can colour.
//
// It is as wide as the words above it, not as wide as the screen. The bar is
// pointing at a heading, so a bar that ran past the last letter would be a
// band across the page rather than an underline, and five of those down
// Settings would make the screen mostly orange.
import QtQuick 2.5
import "Style.js" as Style

Rectangle {
    // No radius and no border: this is a block of colour, and the panel draws
    // a block of colour better than it draws anything else.
    height: Style.accentRule
    color: Style.accent
}
