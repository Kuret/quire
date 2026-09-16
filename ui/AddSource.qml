// The probe wizard — PLAN §6 M3: "paste URL → progress through probe stages →
// verdict", and "verdicts surface as plain language, not error codes".
//
// Every word of the verdict shown here is written by the backend. This file
// picks no wording of its own, and in particular never decides that a failure
// is worth retrying: PLAN §6 M3 says "This site requires a browser challenge
// that Quire can't pass" is a complete, final answer, not a retry prompt, and a
// retry button drawn by the frontend would contradict the sentence above it.
//
// Progress is discrete text, one line per stage. No spinner: on e-ink a
// continuously redrawing control leaves a smear and tells the user nothing a
// stage name does not.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    signal probeRequested(string url)
    signal answerRequested(string answerId)
    signal confirmRequested(string url, string theme, string name, string lang)
    signal doneRequested()

    // "form", "probing", "question" or "verdict".
    property string phase: "form"

    property string url: ""
    property string questionText: ""
    property string verdictHeadline: ""
    property string verdictDetail: ""
    property string verdictWarnings: ""
    property bool verdictAddable: false
    property string draftName: ""
    property string draftTheme: ""
    property string draftLang: ""
    property string draftUrl: ""

    // Only an unreachable site is worth trying again — the site may simply have
    // been down. Every other verdict is an answer, and offering a retry would
    // be pretending otherwise.
    property bool retryable: false
    // An address Quire could not use is worth editing rather than retrying.
    property bool editable: false

    ListModel { id: stageModel }
    ListModel { id: optionModel }

    function reset() {
        screen.phase = "form"
        screen.url = ""
        screen.questionText = ""
        screen.verdictHeadline = ""
        screen.verdictDetail = ""
        screen.verdictWarnings = ""
        screen.verdictAddable = false
        screen.retryable = false
        screen.editable = false
        stageModel.clear()
        optionModel.clear()
    }

    function start() {
        if (screen.url.length === 0) {
            return
        }
        stageModel.clear()
        optionModel.clear()
        screen.phase = "probing"
        screen.probeRequested(screen.url)
    }

    // One line per stage (PLAN §7.5 streams one message per stage, so a slow
    // site does not look hung). A stage that finishes replaces its own line
    // rather than adding a second.
    function onProgress(msg) {
        if (!msg) {
            return
        }
        if (msg.question) {
            screen.phase = "question"
            screen.questionText = msg.question.text
            optionModel.clear()
            for (var i = 0; i < msg.question.options.length; ++i) {
                optionModel.append({
                    "optionId": msg.question.options[i].id,
                    "label": msg.question.options[i].label
                })
            }
            return
        }
        var line = msg.stage + "/" + msg.total + "  " + msg.name + (msg.text ? " — " + msg.text : "")
        for (var j = 0; j < stageModel.count; ++j) {
            if (stageModel.get(j).stage === msg.stage) {
                stageModel.setProperty(j, "line", line)
                return
            }
        }
        stageModel.append({"stage": msg.stage, "line": line})
    }

    function onVerdict(msg) {
        screen.phase = "verdict"
        optionModel.clear()
        if (!msg) {
            screen.verdictHeadline = "No answer"
            screen.verdictDetail = "The check ended without saying why."
            return
        }
        screen.verdictHeadline = msg.cancelled ? "Stopped" : msg.headline
        screen.verdictDetail = msg.detail ? msg.detail : ""
        screen.verdictAddable = msg.addable === true
        screen.draftName = msg.name ? msg.name : ""
        screen.draftTheme = msg.theme ? msg.theme : ""
        screen.draftLang = msg.lang ? msg.lang : ""
        screen.draftUrl = msg.url ? msg.url : screen.url
        screen.retryable = msg.verdict === "unreachable"
        screen.editable = msg.verdict === "invalid_url" || msg.verdict === "blocked_address"

        screen.verdictWarnings = ""
        if (msg.warnings) {
            for (var i = 0; i < msg.warnings.length; ++i) {
                screen.verdictWarnings += (i > 0 ? "\n\n" : "") + msg.warnings[i]
            }
        }
    }

    function onBackendError(text) {
        if (screen.phase === "probing" || screen.phase === "question") {
            screen.phase = "verdict"
            screen.verdictHeadline = "Couldn't finish"
            screen.verdictDetail = text
            screen.verdictAddable = false
            screen.retryable = true
            screen.editable = false
        }
    }

    // ---- the form ----------------------------------------------------------

    Column {
        id: form
        anchors {
            top: parent.top; topMargin: Style.margin
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
        }
        spacing: Style.gap
        visible: screen.phase === "form"

        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Type the site's web address. Quire will check what it can read " +
                  "before adding anything."
            font.pointSize: Style.bodySize
            color: Style.muted
        }

        Rectangle {
            width: parent.width
            height: Style.buttonHeight
            color: Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            TextInput {
                id: urlField
                objectName: "urlField"
                anchors {
                    fill: parent
                    leftMargin: Style.gap
                    rightMargin: Style.gap
                }
                verticalAlignment: TextInput.AlignVCenter
                font.pointSize: Style.bodySize
                color: Style.ink
                // The device has no system keyboard available to an AppLoad app
                // (PLAN §11 Q5), so text comes from ui/Keyboard.qml below and
                // this field is never focused for input of its own.
                activeFocusOnPress: false
                text: screen.url
                onTextChanged: screen.url = text
            }

            Text {
                anchors { left: parent.left; leftMargin: Style.gap; verticalCenter: parent.verticalCenter }
                text: "https://"
                font.pointSize: Style.bodySize
                color: Style.rule
                visible: screen.url.length === 0
            }
        }

        Rectangle {
            objectName: "checkButton"
            width: parent.width
            height: Style.buttonHeight
            color: checkArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: screen.url.length > 0 ? Style.ink : Style.rule
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Check this site"
                font.pointSize: Style.bodySize
                color: screen.url.length > 0 ? Style.ink : Style.rule
            }

            MouseArea {
                id: checkArea
                anchors.fill: parent
                enabled: screen.url.length > 0
                onClicked: screen.start()
            }
        }
    }

    Keyboard {
        id: keyboard
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        visible: screen.phase === "form"
        layout: "url"
        onKeyTyped: screen.url += text
        onBackspace: screen.url = screen.url.substring(0, screen.url.length - 1)
        onClearAll: screen.url = ""
        onSubmit: screen.start()
    }

    // ---- progress ----------------------------------------------------------

    Column {
        anchors {
            top: parent.top; topMargin: Style.margin
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
        }
        spacing: Style.gap
        visible: screen.phase === "probing" || screen.phase === "question"

        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Checking " + screen.url
            font.pointSize: Style.bodySize
            color: Style.ink
        }

        Repeater {
            model: stageModel
            delegate: Text {
                width: form.width
                wrapMode: Text.WordWrap
                text: model.line
                font.pointSize: Style.smallSize
                color: Style.muted
            }
        }

        // A question, when the probe needs the user rather than a guess
        // (PLAN §7.5 stages 2 and 4).
        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.questionText
            font.pointSize: Style.bodySize
            color: Style.ink
            visible: screen.phase === "question"
        }

        Repeater {
            model: optionModel
            delegate: Rectangle {
                width: form.width
                height: Style.buttonHeight
                color: optionArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: model.label
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                MouseArea {
                    id: optionArea
                    anchors.fill: parent
                    onClicked: {
                        screen.phase = "probing"
                        optionModel.clear()
                        screen.answerRequested(model.optionId)
                    }
                }
            }
        }
    }

    // ---- verdict -----------------------------------------------------------

    Column {
        anchors {
            top: parent.top; topMargin: Style.margin
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
        }
        spacing: Style.gap
        visible: screen.phase === "verdict"

        Text {
            objectName: "verdictHeadline"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.verdictHeadline
            font.pointSize: Style.headingSize
            color: Style.ink
        }

        Text {
            objectName: "verdictDetail"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.verdictDetail
            font.pointSize: Style.bodySize
            color: Style.ink
        }

        Text {
            objectName: "verdictWarnings"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.verdictWarnings
            font.pointSize: Style.smallSize
            color: Style.muted
            visible: screen.verdictWarnings.length > 0
        }

        Rectangle {
            objectName: "addButton"
            width: parent.width
            height: Style.buttonHeight
            color: addArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6
            visible: screen.verdictAddable

            Text {
                anchors.centerIn: parent
                text: "Add " + screen.draftName
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: addArea
                anchors.fill: parent
                onClicked: screen.confirmRequested(screen.draftUrl, screen.draftTheme,
                                                   screen.draftName, screen.draftLang)
            }
        }

        Rectangle {
            width: parent.width
            height: Style.buttonHeight
            color: retryArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6
            visible: screen.retryable || screen.editable

            Text {
                anchors.centerIn: parent
                text: screen.editable ? "Edit the address" : "Try again"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: retryArea
                anchors.fill: parent
                onClicked: {
                    if (screen.editable) {
                        screen.phase = "form"
                    } else {
                        screen.start()
                    }
                }
            }
        }

        Rectangle {
            width: parent.width
            height: Style.buttonHeight
            color: closeArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.rule
            radius: 6

            Text {
                anchors.centerIn: parent
                text: screen.verdictAddable ? "Not now" : "Close"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: closeArea
                anchors.fill: parent
                onClicked: screen.doneRequested()
            }
        }
    }
}
