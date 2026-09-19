import QtQuick 2.5

// A stand-in for Annex's Backend.qml (annex/lib/Backend.qml), for the
// offscreen harness only.
//
// # Why this file exists
//
// ui/Main.qml is the app shell, and until now nothing tested it. The reason
// given — repeatedly, in comments that are now corrected — was that Main.qml
// "imports the Backend plugin". It does not. `Backend` is a plain QML file in
// Annex's lib/, and the only obstacle was ever a path: Main.qml says
//
//     import "../../../lib"
//
// which resolves against the *device* layout, /home/root/annex/apps/quire/ui,
// and so lands on /home/root/annex/lib. build/qml-check.sh builds a directory
// with that shape at check time, puts this file where Annex's lib/ would be,
// and the real Main.qml loads and runs.
//
// # What it stands in for, and what it therefore cannot prove
//
// This is the transport, and it is a fake. It records what Main.qml sends and
// lets a test hand a reply back, which is enough to drive every handler, every
// model fill and every screen change in Main.qml — **our half of the
// conversation, and only that half.**
//
// It proves nothing whatsoever about:
//
//   * the real Backend.qml: discovery of the endpoint file, the token, the
//     long poll, the 401 rediscovery, the queue that holds sends made before
//     the backend is reachable;
//   * the HTTP transport: a payload mangled on the wire, a b64 body, a message
//     that never arrives;
//   * Annex's host: whether the app is loaded at all, whether unloading() is
//     called, what draws over what.
//
// A green harness means the QML reacts correctly to messages it was handed in
// process. It does not mean the app talks to anything. Nothing here is a
// substitute for running Quire on the device.
//
// The surface below mirrors annex/lib/Backend.qml's public one exactly —
// properties, signals and the three functions. If that file grows a member
// Main.qml uses, it has to be added here, and the harness will say so by
// failing to load rather than by quietly doing nothing.
Item {
    id: backend

    // So a test can find the transport the app actually connected itself to,
    // rather than being handed one. The real Backend.qml sets no objectName;
    // this is the one deliberate difference in the surface.
    objectName: "stubBackend"

    // ---- the real thing's configuration ------------------------------------

    property string appId: ""
    property string annexRoot: "/home/root/annex"
    property int waitMs: 15000
    property int retryMs: 2000
    property bool autoStart: true
    property int pendingMax: 32

    // ---- the real thing's state --------------------------------------------

    property string status: "idle"
    property string detail: ""
    readonly property bool connected: status === "connected"

    // ---- the real thing's signals ------------------------------------------

    // message is how a test feeds a reply in. Emit it exactly as the real
    // Backend.qml does — the type, the payload as text, and b64 — and the app
    // cannot tell the difference:
    //
    //     backend.message(Msg.Pong, JSON.stringify({...}), false)
    signal message(int type, string data, bool b64)
    signal failed(int type, string reason)

    visible: false

    // ---- what the stub records ---------------------------------------------

    // sent is every send in order: {"type": int, "body": string}. The body is
    // kept as the string that would have gone on the wire, because that is
    // what Main.qml composed — a test that read back an object would be
    // reading back its own argument rather than the message.
    property var sent: []

    // Counts rather than flags, for the reason the rest of the harness uses
    // them: the interesting assertion is usually that something was sent
    // *once*, or not at all.
    property int sendCount: 0
    property int starts: 0
    property int stops: 0

    function start() {
        backend.starts++
        backend.status = "connected"
        backend.detail = ""
    }

    function stop() {
        backend.stops++
        backend.status = "idle"
        backend.detail = ""
    }

    function send(type, payload) {
        var body = ""
        if (payload !== undefined && payload !== null)
            body = (typeof payload === "string") ? payload : JSON.stringify(payload)
        backend.sent.push({"type": type, "body": body})
        backend.sendCount++
        return true
    }

    // ---- reading the record back -------------------------------------------

    function forget() {
        backend.sent = []
        backend.sendCount = 0
    }

    function countOf(type) {
        var n = 0
        for (var i = 0; i < backend.sent.length; ++i)
            if (backend.sent[i].type === type)
                n++
        return n
    }

    // types is the whole conversation as "10,1", for asserting order.
    function types() {
        var out = []
        for (var i = 0; i < backend.sent.length; ++i)
            out.push(backend.sent[i].type)
        return out.join(",")
    }

    // bodyOf is the last payload sent of one type, parsed. Null when nothing
    // of that type was sent, which is distinguishable from an empty payload:
    // "{}" parses to an object.
    function bodyOf(type) {
        for (var i = backend.sent.length - 1; i >= 0; --i) {
            if (backend.sent[i].type !== type)
                continue
            try {
                return JSON.parse(backend.sent[i].body)
            } catch (e) {
                return {"unparseable": backend.sent[i].body}
            }
        }
        return null
    }

    // rawOf is the same, untouched, for the cases about what was composed
    // rather than about what it means.
    function rawOf(type) {
        for (var i = backend.sent.length - 1; i >= 0; --i)
            if (backend.sent[i].type === type)
                return backend.sent[i].body
        return null
    }

    Component.onCompleted: if (autoStart) start()
}
