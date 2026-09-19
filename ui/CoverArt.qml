// One cover, drawn at whatever size it is given: the picture on a tile and the
// thumbnail on a list row are this same file.
//
// It was CoverGrid's Image-and-placeholder pair until the rows wanted a cover
// too. Copying it into the four row delegates was rejected for the reason the
// grid itself was shared: the interesting behaviour here is what is drawn when
// there is *no* picture, "many existing downloads will have no cover" makes
// that the common case on Downloaded and Watching, and five copies would be
// five places for a blank square or a broken-image box to come back.
//
// What it does **not** own is its frame. A tile sits in a bordered rectangle
// that carries the press feedback, because the tile is what the finger is on
// and a cover hides the fill under it; a row darkens the whole row instead
// (rowPressFeedback). So this is the picture and nothing else, and what is
// behind it is the caller's.
//
// It fetches nothing. The screen asks for the batch its page wants in one
// message and the backend writes a file; this draws whatever arrived. Nothing
// image-shaped crosses the socket (PLAN §7.1).

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: art

    // The file the backend downscaled, or "" when there is none.
    property string coverPath: ""

    // What the placeholder says, which is the title. A tile or a row reading
    // "No cover" tells the user nothing about which book it is.
    property string caption: ""

    // How much of the title the placeholder has room for, and the room around
    // it. A tile is wide enough for five lines with a full gap to spare; a row
    // thumbnail is a fifth of that width, so it takes the first words and a
    // hairline's worth of margin rather than an empty box with a margin.
    property int lines: 5
    property int padding: Style.gap

    Image {
        id: cover
        objectName: "coverImage"
        anchors.fill: parent
        source: art.coverPath
        fillMode: Image.PreserveAspectFit
        // Decode at the size shown, not at the size stored — PLAN §6 M3's
        // memory rule, and the reason a row thumbnail is cheaper than a tile
        // rather than the same picture again.
        sourceSize.width: art.width
        sourceSize.height: art.height
        asynchronous: true
        // No fade-in: the panel would ghost the intermediate frames.
        cache: true
        visible: art.coverPath.length > 0 && cover.status !== Image.Error
    }

    // The placeholder covers three cases that look identical from here: no
    // cover was offered, one was offered and has not arrived, and the file is
    // there and will not decode.
    Text {
        objectName: "coverPlaceholder"
        anchors {
            fill: parent
            margins: art.padding
        }
        verticalAlignment: Text.AlignVCenter
        horizontalAlignment: Text.AlignHCenter
        wrapMode: Text.WordWrap
        maximumLineCount: art.lines
        elide: Text.ElideRight
        text: art.caption
        font.pointSize: Style.smallSize
        color: Style.muted
        visible: art.coverPath.length === 0 || cover.status === Image.Error
    }
}
