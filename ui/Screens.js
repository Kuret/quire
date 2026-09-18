// What a screen has to re-ask for when it becomes the active one.
//
// The bug this exists to stop: the Downloaded overview was fetched on the way
// in from the source list, and *only* there. Opening a series from it, deleting
// the series' last download and coming back left the row on screen pointing at
// nothing — leaving the screen entirely and returning was the only way to see
// the truth. A list is not "fetched when it is opened" if one of the ways it is
// opened does not fetch it.
//
// **A refetch, never arithmetic in the view.** Deleting one document can change
// a row's count, remove the row, or touch several rows at once, and the backend
// already works all of that out from the records. A view that patches its own
// model on a delete is a second implementation of that sum, and the two will
// disagree eventually — on the screen, in front of the user.
//
// # Why this is one small function and not a rule per screen
//
// Most screens do not belong here, and the reasons differ:
//
//   * **watching** is *pushed*. MessageWatchList arrives on attach and after
//     every change to the list, so it cannot go stale by being returned to.
//     Asking again on entry would also mean a network check of every watched
//     series (PLAN §6 M7 keeps those explicit), which is a real cost for a list
//     that is already correct.
//   * **browse** is a source's catalogue, and a page of it is served from the
//     cache the backend keeps for paging (PLAN §12.1). Nothing Quire does to
//     its own downloads changes what the source publishes.
//   * **series** already refetches: every route into it sends
//     MessageSeriesDetail, which is what carries the per-chapter download
//     state.
//   * **sources**, **add**, **settings** carry no list that a delete can
//     falsify.
//
// Those are the findings from checking the same class of bug elsewhere, kept
// here rather than in a commit message because the next screen with a list will
// be added by someone asking exactly this question.

// refreshOnShow names what to re-ask for, or "" for the screens that need
// nothing. The caller maps the name to a message, so this file stays free of
// the protocol and the harness can drive it without one.
function refreshOnShow(screen) {
    if (screen === "downloaded")
        return "listDownloaded"
    return ""
}

// ---- the on-screen keyboard --------------------------------------------
//
// AppLoad's keyboard raises itself when a field takes focus and **nothing
// lowers it**. Quire's own keyboard was part of the layout and went away with
// the screen that owned it; this one is an overlay that stays up, covering the
// bottom 544 px — the pager and the last rows — until something dismisses it.
// That is the cost of using a keyboard we do not own, and this is the code that
// pays it.
//
// # Both levers, in this order
//
// Measured off-device, because the sequence decides the code:
//
//   * `Qt.inputMethod.hide` is a function and does not throw.
//   * **Tapping something else does not move focus.** A MouseArea click leaves
//     the TextInput focused, so "the user tapped a row" does not dismiss
//     anything by itself.
//   * `hide()` does not clear focus either.
//
// So focus is dropped **first** and the panel hidden **second**. The other
// order invites the fight the panel would win: hiding while a field is still
// focused is an invitation to be raised again by the next focus event, and
// hiding harder produces a flicker rather than a dismissal.
//
// What cannot be checked without the device is whether the panel *obeys*
// `hide()`. The probe measured `Qt.inputMethod.visible` going true when a field
// took focus, so the panel is wired into Qt's input-method framework and should
// hear it; dropping focus is the second lever precisely because "should" is not
// "does".

// dismissKeyboard drops focus from these fields and asks the panel to go.
//
// Fields are passed in rather than found, because a helper that goes looking
// for inputs would dismiss one the caller did not mean — including the one the
// user is still typing into.
function dismissKeyboard(fields) {
    var dropped = 0
    if (fields) {
        for (var i = 0; i < fields.length; ++i) {
            if (!fields[i])
                continue
            if (fields[i].activeFocus || fields[i].focus)
                dropped += 1
            fields[i].focus = false
        }
    }
    try {
        Qt.inputMethod.hide()
    } catch (e) {
        // An environment with no input method at all is not an error: the
        // offscreen harness is one.
        console.log("[quire] no input method to hide: " + e)
    }
    return dropped
}
