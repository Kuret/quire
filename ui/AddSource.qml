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
import "Screens.js" as Screens

Item {
    id: screen

    signal probeRequested(string url)
    // answerText is what the user typed into the question's own field, "" when
    // the question carried none. It travels with the answer rather than on a
    // message of its own: the probe has one place where it stops and asks, and
    // a second channel would be a second thing to keep in step.
    signal answerRequested(string answerId, string answerText)
    signal confirmRequested(string url, string theme, string name, string lang)
    signal doneRequested()

    // "form", "probing", "question" or "verdict".
    property string phase: "form"

    property string url: ""
    property string questionText: ""
    // The optional field a question may carry (PLAN §7.5 stage 1's self-hosted
    // offer asks for a proxy this way). An empty label means no field, which is
    // what every other question has.
    property string questionInputLabel: ""
    property string questionInputHint: ""
    property string questionInput: ""
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
        screen.questionInputLabel = ""
        screen.questionInputHint = ""
        screen.questionInput = ""
        screen.verdictHeadline = ""
        screen.verdictDetail = ""
        screen.verdictWarnings = ""
        screen.verdictAddable = false
        screen.retryable = false
        screen.editable = false
        stageModel.clear()
        optionModel.clear()
    }

    // dismissInput puts the keyboard away. Called when this screen is
    // navigated away from, and when the thing the keyboard was raised for
    // happens — never while the user is still in the field.
    //
    // Quire's keyboard is part of the layout and its visibility follows
    // `phase`, so leaving the form is what lowers it; the focus drop in
    // Screens.dismissKeyboard still comes first, and still comes first for a
    // reason — see the note there.
    function dismissInput() {
        Screens.dismissKeyboard([urlField, questionField])
    }

    // answer sends the user's reply and puts the question away.
    //
    // It lives on the screen rather than in the option delegate's own handler,
    // and that is not a tidiness preference: clearing optionModel destroys the
    // delegate whose handler is running, and the rest of a handler that has
    // deleted itself does not reliably run. The answer — the one line that has
    // to happen — was the last of those, so the tap looked right on screen and
    // told the backend nothing. Everything here happens in the screen's own
    // frame, with the option id read out of the model before the call.
    function answer(optionId) {
        var typed = screen.questionInput
        screen.dismissInput()
        screen.phase = "probing"
        optionModel.clear()
        screen.questionInputLabel = ""
        screen.questionInputHint = ""
        screen.questionInput = ""
        screen.answerRequested(optionId, typed)
    }

    function start() {
        screen.dismissInput()
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
            // A question that asks for nothing typed carries no input block,
            // and the field is absent rather than empty — an always-present
            // box would invite an answer to a question nobody asked.
            screen.questionInputLabel = msg.question.input ? msg.question.input.label : ""
            screen.questionInputHint = msg.question.input && msg.question.input.placeholder
                                       ? msg.question.input.placeholder : ""
            screen.questionInput = ""
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
            text: "Type the site's address — the name is enough, no https:// needed. " +
                  "Quire will check what it can read before adding anything."
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
                // The device has no system keyboard available to an embedded
                // app (PLAN §11 Q5) — Annex supplies none, so text comes from
                // ui/Keyboard.qml below and this field is never focused for
                // input of its own.
                activeFocusOnPress: false
                text: screen.url
                onTextChanged: screen.url = text
            }

            Text {
                anchors { left: parent.left; leftMargin: Style.gap; verticalCenter: parent.verticalCenter }
                // An example rather than "https://": the probe fills the scheme
                // in, and every character not typed on a touch keyboard counts.
                text: "weebcentral.com"
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
        objectName: "addSourceKeyboard"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        visible: screen.phase === "form"
        layout: "url"
        onKeyTyped: screen.url += text
        onBackspace: screen.url = screen.url.substring(0, screen.url.length - 1)
        onClearAll: screen.url = ""
        onSubmit: screen.start()
    }

    // The question's own keyboard, up only while a question with a field is on
    // screen. It is a second Keyboard rather than a shared one because the two
    // type into different properties, and a keyboard that switches targets is
    // how text ends up in the wrong field.
    //
    // It has no Submit of its own: a typed value is not an answer by itself —
    // the answer is the option the user taps — so there is nothing for Enter to
    // mean here.
    Keyboard {
        id: questionKeyboard
        objectName: "questionKeyboard"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        visible: screen.phase === "question" && screen.questionInputLabel.length > 0
        layout: "url"
        onKeyTyped: screen.questionInput += text
        onBackspace: screen.questionInput = screen.questionInput.substring(0, screen.questionInput.length - 1)
        onClearAll: screen.questionInput = ""
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

        // The optional field a question may carry. Today that is the proxy
        // offered alongside a private address, because the user adding a host
        // on their own network is the one who may need it — see
        // theme.Source.Proxy. It is optional in the plainest sense: leaving it
        // empty answers the question just as completely.
        Text {
            objectName: "questionInputLabel"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.questionInputLabel
            font.pointSize: Style.smallSize
            color: Style.muted
            visible: screen.phase === "question" && screen.questionInputLabel.length > 0
        }

        Rectangle {
            objectName: "questionInputBox"
            width: parent.width
            height: Style.buttonHeight
            color: Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6
            visible: screen.phase === "question" && screen.questionInputLabel.length > 0

            TextInput {
                id: questionField
                objectName: "questionField"
                anchors {
                    fill: parent
                    leftMargin: Style.gap
                    rightMargin: Style.gap
                }
                verticalAlignment: TextInput.AlignVCenter
                font.pointSize: Style.bodySize
                color: Style.ink
                // As on the address field: the device has no system keyboard
                // available to an embedded app, so text comes from the keyboard
                // below and this field is never focused for input of its own.
                activeFocusOnPress: false
                text: screen.questionInput
                onTextChanged: screen.questionInput = text
            }

            Text {
                objectName: "questionFieldHint"
                anchors { left: parent.left; leftMargin: Style.gap; verticalCenter: parent.verticalCenter }
                text: screen.questionInputHint
                font.pointSize: Style.bodySize
                color: Style.rule
                visible: screen.questionInput.length === 0
            }
        }

        Repeater {
            model: optionModel
            delegate: Rectangle {
                objectName: "questionOption"
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
                    objectName: "questionOptionArea"
                    anchors.fill: parent
                    onClicked: screen.answer(model.optionId)
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
