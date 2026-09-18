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

// resumePage is the page to land on when a screen is restored.
//
// The stored page can be past the end: the series published fewer chapters than
// it had, or the list is grouped into volumes this time. Landing past the end
// is a blank screen, which reads as a broken app rather than as a restored one.
//
// A list with nothing in it is still page 1 — pages are 1-based and there is no
// page 0 to show.
function resumePage(want, totalPages) {
    if (!want || want < 1)
        return 1
    if (!totalPages || totalPages < 1)
        return 1
    return want > totalPages ? totalPages : want
}
