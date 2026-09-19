// A section heading: black words, with the accent as a filled bar under them.
//
// Settings is the one screen that is a list of unrelated sections rather than
// a list of like things, so its headings are what is actually navigated. They
// are already the largest type in their section and already the only lines
// with space above them; the bar is a third signal on top of those, and a
// reader who sees no colour at all still has the size and the spacing.
//
// The heading itself is Style.ink. It used to be the accent, and on the device
// the coloured type read muddy — the measurement in ui/Style.js. The colour
// moved to the bar, which is a solid area and the one thing the panel renders
// cleanly.
import QtQuick 2.5
import "Style.js" as Style

Column {
    id: heading

    property alias text: label.text

    spacing: 6

    Text {
        id: label
        objectName: "sectionHeading"
        width: heading.width
        wrapMode: Text.WordWrap
        font.pointSize: Style.headingSize
        color: Style.ink
    }

    AccentRule {
        objectName: "sectionHeadingRule"
        // As wide as the words, and no wider — see ui/AccentRule.qml. A
        // wrapped heading measures its longest line, which is what contentWidth
        // gives, so a two-line heading is not underlined to the full column.
        width: Math.min(label.contentWidth, heading.width)
    }
}
