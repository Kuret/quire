// render.js drives one MuPDF document for Quire's own book reader. It is run
// as `mutool run render.js`, embedded into the Go binary (go:embed) and
// written out to disk at startup by backend/bookrender.WriteScript.
//
// The protocol is line-based JSON over stdin/stdout: one request line in, one
// response line out, in order. There is no framing beyond the newline, and no
// concurrent requests — backend/bookrender.Renderer only ever has one call in
// flight at a time.
//
// Commands (see backend/bookrender's package comment for the Go side):
//   {"cmd":"open","path":...}                      -> {ok, fixedLayout, title}
//   {"cmd":"layout","w":...,"h":...,"em":...,"css":...} -> {pages}
//   {"cmd":"render","page":...,"out":...,"scale":...}   -> {ok}
//   {"cmd":"text","page":...}                      -> {text}
//   {"cmd":"outline"}                               -> {toc:[{title,page,level}]}
//
// A request naming something wrong (bad page number, nothing open yet)
// answers {"ok":false,"error":"..."} on this same line rather than throwing
// off the process — a thrown JS exception is still caught below and turned
// into the same shape, so mutool itself never has to die to report "no such
// page". Only an actual crash (a native MuPDF assertion, an OOM under the
// memory cap) ends the process, which is backend/bookrender.Renderer's job to
// notice and recover from.
//
// Measured on the device (docs/DEVICE-NOTES.md): 1620x2160 px @229dpi, layout
// W=509.3 H=679.2 pt, render scale 229/72.
var DPI = 229;
var SCALE = DPI / 72;

var doc = null;
var fixedLayout = false;

// flatten turns MuPDF's outline tree (each node {title, uri or page, down})
// into a flat list with an explicit level, resolving a node's page number
// through resolveLink when the node only carries a uri — the outline gotcha
// noted in the mupdf-spike memory: there is no bookmark API, but there is a
// link resolver, and every outline node in practice is a link.
function flatten(outline, level, out) {
  if (!outline) return;
  for (var i = 0; i < outline.length; i++) {
    var n = outline[i];
    if (!n) continue;
    var page = -1;
    if (typeof n.page === "number") {
      page = n.page;
    } else if (n.uri) {
      try { page = doc.resolveLink(n.uri); } catch (e) { page = -1; }
    }
    out.push({ title: String(n.title || ""), page: page, level: level });
    if (n.down) flatten(n.down, level + 1, out);
  }
}

// renderScale picks the scale for one page's pixmap: the fixed render scale
// for a reflowable document already laid out to the target point size, or
// whatever fits a fixed-layout page (PDF/XPS/CBZ) into 1620x2160 without
// distorting it.
function renderScale(page) {
  if (!fixedLayout) return SCALE;
  try {
    var b = page.getBounds();
    var w = b[2] - b[0], h = b[3] - b[1];
    if (w > 0 && h > 0) {
      return Math.min(1620 / w, 2160 / h);
    }
  } catch (e) {}
  return SCALE;
}

function handle(req) {
  if (req.cmd === "open") {
    doc = mupdf.Document.openDocument(req.path);
    fixedLayout = !doc.isReflowable();
    var title = "";
    try { title = doc.getMetaData("info:Title") || ""; } catch (e) {}
    return { ok: true, fixedLayout: fixedLayout, title: title };
  }
  if (!doc) return { ok: false, error: "no document open" };

  if (req.cmd === "layout") {
    if (fixedLayout) return { pages: doc.countPages() };
    // doc.style(true, css) — the first argument keeps the book's own CSS in
    // play; passing "" instead of true silently drops it (justification,
    // indents), per the mupdf-spike memory.
    doc.style(true, req.css || "");
    doc.layout(req.w, req.h, req.em);
    return { pages: doc.countPages() };
  }

  if (req.cmd === "render") {
    var page = doc.loadPage(req.page);
    var scale = req.scale || renderScale(page);
    var pix = page.toPixmap(mupdf.Matrix.scale(scale, scale), mupdf.ColorSpace.DeviceGray, false);
    pix.saveAsPNG(req.out);
    return { ok: true };
  }

  if (req.cmd === "text") {
    var page2 = doc.loadPage(req.page);
    var t = page2.toStructuredText().asText().replace(/\s+/g, " ").trim();
    return { text: t.slice(0, 200) };
  }

  if (req.cmd === "outline") {
    var toc = [];
    try { flatten(doc.loadOutline(), 0, toc); } catch (e) {}
    return { toc: toc };
  }

  return { ok: false, error: "unknown command " + JSON.stringify(req.cmd) };
}

// The main loop. readline() throws (rather than returning null) at EOF —
// measured against mutool 1.28.4's mujs-based `mutool run` — so EOF is
// caught, not compared against.
// Requests arrive framed (see frameRequest in renderer.go): pieces of at most
// 200 bytes, each prefixed with ">", then a lone "." — because readline()
// reads into a 256-byte buffer and would split a longer line in two.
var pending = "";
while (true) {
  var raw;
  try { raw = readline(); } catch (e) { break; }
  if (raw.charAt(0) === ">") { pending += raw.slice(1); continue; }
  if (raw !== ".") continue;
  var line = pending;
  pending = "";
  if (line === "") continue;
  var req, res;
  try {
    req = JSON.parse(line);
  } catch (e) {
    print(JSON.stringify({ ok: false, error: "bad request" }));
    continue;
  }
  try {
    res = handle(req);
  } catch (e) {
    res = { ok: false, error: String(e) };
  }
  print(JSON.stringify(res));
}
