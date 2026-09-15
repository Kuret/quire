// Page arithmetic for PLAN §12.1's paged lists.
//
// Quire does not scroll. A scroll on e-ink is a continuous stream of partial
// refreshes — smearing and ghosting the whole way down — whereas a page turn is
// one full refresh of a settled screen, which is what the panel is good at.
//
// Only two things live here, and both are genuinely presentational (PLAN §2:
// everything else belongs in the backend, where it is testable):
//
//   1. **How many items fit.** Nobody but the view knows a pixel, so the page
//      size is worked out here, from the real viewport and the real item
//      height. It is never hardcoded: the panel is 1620×2160, but a windowed
//      AppLoad emulator launch is not, and a page size baked to the panel would
//      leave that one with a half-visible row — the scrolling problem in
//      miniature, and an invitation to the swipe we are removing.
//   2. **Which slice of an already-delivered list is on screen.** For the
//      chapter list, the source list and the log, the backend has already sent
//      everything; choosing a window into it is a view concern and making it a
//      round trip would put the network in front of a page turn for no reason.
//
// The part that is *not* here is the series grid's paging: there the list is
// fetched lazily and the cache-versus-source-page question is real, so it lives
// in backend/service/paging.go with tests on it.
.pragma library

// itemsPerPage is the page size: whole rows only, at least one.
//
// A page that ends in a half-row is the thing PLAN §12.1 rules out, so the
// remainder is left blank rather than part-filled. `columns` is 1 for a list
// and 3 for the grid.
function itemsPerPage(viewportHeight, itemHeight, columns) {
    if (!(itemHeight > 0) || !(columns > 0))
        return 0
    var rows = Math.floor(viewportHeight / itemHeight)
    if (rows < 1)
        rows = 1
    return rows * columns
}

// rowsPerPage is itemsPerPage before the columns are multiplied in — what the
// view needs to size itself to a whole number of rows.
function rowsPerPage(viewportHeight, itemHeight) {
    if (!(itemHeight > 0))
        return 0
    var rows = Math.floor(viewportHeight / itemHeight)
    return rows < 1 ? 1 : rows
}

// pageCount is how many pages `total` items make. An empty list is one empty
// page, not zero pages: "Page 1 of 0" is not a thing to show anyone.
function pageCount(total, size) {
    if (!(size > 0))
        return 1
    var n = Math.ceil(total / size)
    return n < 1 ? 1 : n
}

// clampPage keeps a page number inside the list. It does not wrap: PLAN §12.1
// wants a hard stop, because wrapping past the end lies about where the end is.
function clampPage(page, count) {
    if (!(page > 1))
        return 1
    return page > count ? count : page
}

// firstIndex is the model index of the first item on a page.
function firstIndex(page, size) {
    return (clampPage(page, Number.MAX_VALUE) - 1) * size
}

// label says where the user is, and says only what is known. A total that the
// source has not given us is left out rather than guessed at.
function label(page, totalPages) {
    if (totalPages > 0)
        return "Page " + page + " of " + totalPages
    return "Page " + page
}
