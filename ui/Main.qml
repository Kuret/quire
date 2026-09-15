// Quire — M1 frontend.
//
// PLAN §2: the QML frontend is a dumb view. It sends Ping and renders the Pong
// payload. No logic beyond that lives here, because QML is the layer that
// breaks on OS updates.
//
// PLAN §6 M3 UI rules apply from the start:
//   - No animations. E-ink ghosts.
//   - Match the stock UI palette. Do not invent a brand.
//
// Verified against the installed appload.so: the QML type exposes
// applicationID, messageReceived, sendMessage (one 's' in the middle — the
// upstream README's "sendMesssage" is a typo) and terminate. The host calls
// unloading() on the root element when the app is closed.

import QtQuick 2.5
import net.asivery.AppLoad 1.0
import "Messages.js" as Msg

Rectangle {
    id: root
    anchors.fill: parent
    color: "#FFFFFF"

    // The AppLoad host connects to this and closes the frontend.
    signal close

    // Stock-ish palette. Greyscale only; the panel is effectively 1-bit-ish.
    readonly property color inkColor: "#000000"
    readonly property color mutedColor: "#6B6B6B"
    readonly property color ruleColor: "#B4B4B4"
    readonly property color pressedColor: "#E4E4E4"

    property string statusText: "Not yet asked."
    property string detailText: ""

    AppLoad {
        id: appload
        // Must equal the "id" field in manifest.json.
        applicationID: "quire"

        onMessageReceived: (type, contents) => {
            switch (type) {
            case Msg.Pong:
                root.showPong(contents)
                break
            case Msg.Error:
                root.showError(contents)
                break
            default:
                root.statusText = "Unexpected message " + type
                root.detailText = contents
            }
        }
    }

    // Called by the AppLoad host when the app is being unloaded. Terminating
    // the backend here is what stops the quired process.
    function unloading() {
        appload.terminate()
    }

    function showPong(contents) {
        var s = null
        try {
            s = JSON.parse(contents)
        } catch (e) {
            root.statusText = "Unreadable reply"
            root.detailText = contents
            return
        }
        root.statusText = "Device up " + s.uptime
        root.detailText = "quired " + s.version + " · " + s.goVersion +
                          " · " + s.os + "/" + s.arch + " · pid " + s.pid
    }

    function showError(contents) {
        var e = null
        try {
            e = JSON.parse(contents)
        } catch (err) {
            root.statusText = "Backend error"
            root.detailText = contents
            return
        }
        root.statusText = "Backend error"
        root.detailText = e.code + ": " + e.message
    }

    Column {
        anchors.centerIn: parent
        width: Math.min(parent.width - 96, 720)
        spacing: 48

        Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            text: "Quire"
            color: root.inkColor
            font.pointSize: 32
        }

        Rectangle {
            id: pingButton
            objectName: "pingButton"
            anchors.horizontalCenter: parent.horizontalCenter
            width: 320
            height: 96
            color: pingArea.pressed ? root.pressedColor : "#FFFFFF"
            border.width: 2
            border.color: root.inkColor
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Ping backend"
                color: root.inkColor
                font.pointSize: 18
            }

            MouseArea {
                id: pingArea
                anchors.fill: parent
                onClicked: appload.sendMessage(Msg.Ping, "")
            }
        }

        Column {
            width: parent.width
            spacing: 12

            Text {
                id: statusLabel
                objectName: "statusLabel"
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.WordWrap
                text: root.statusText
                color: root.inkColor
                font.pointSize: 22
            }

            Text {
                id: detailLabel
                objectName: "detailLabel"
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.WordWrap
                text: root.detailText
                color: root.mutedColor
                font.pointSize: 12
            }
        }

        Rectangle {
            anchors.horizontalCenter: parent.horizontalCenter
            width: parent.width
            height: 1
            color: root.ruleColor
        }
    }
}
