// Quire's shared look — PLAN §6 M3: "Match the stock UI palette. Do not invent
// a brand."
//
// So there is no accent colour, no logo colour and no second typeface here.
// The device is greyscale e-ink and the stock UI is black text on white with
// hairline rules; that is all this file describes.
//
// Everything else in the UI rules follows from the panel:
//   - No animations, transitions or fades. E-ink ghosts, and a control that
//     redraws continuously leaves a smear behind it.
//   - No spinners. Progress is discrete text that changes a few times.
//   - Big touch targets. A finger on an e-ink panel has no hover state and no
//     second chance.
.pragma library

var paper = "#FFFFFF"
var ink = "#000000"
var muted = "#6B6B6B"
var rule = "#B4B4B4"
var pressed = "#E4E4E4"
var panel = "#F4F4F4"

// Type scale, in points.
var titleSize = 26
var headingSize = 20
var bodySize = 15
var smallSize = 12

// Spacing and touch targets, in device-independent pixels.
var gap = 16
var margin = 32
var rowHeight = 96
var buttonHeight = 72
var hairline = 1
