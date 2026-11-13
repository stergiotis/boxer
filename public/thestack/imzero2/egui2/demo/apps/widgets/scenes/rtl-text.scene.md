---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1400x1000
  fallbackFont: Noto Naskh Arabic
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Right-to-left text — what egui does with Arabic

A tripwire for egui's right-to-left support, not a test of ours. imzero2 does
no bidi processing of its own and does not patch egui; this scene measures
what upstream does with the gallery's "right-to-left text" demo, and its
expectations pin the behaviour **as measured** (egui 0.36.2, 2026-10-13), not
the correct one. When it fails, egui changed: re-measure, update the
expectations and the findings below, and check whether the issues named here
have closed.

The face is Noto Naskh Arabic, in the fallback slot (`fallbackFont`); the
main face, Noto Sans, has no Arabic. Without that family installed the scene
is skipped.

## Findings at egui 0.36.2

- **Letters join.** Each run is shaped by harfrust (egui 0.35, #8031), so
  Arabic takes its contextual forms. Before 0.35 egui drew isolated letters.
- **Word order across direction runs is wrong.** egui has no bidi step
  (emilk/egui#1016). Each right-to-left run is drawn right to left, but runs
  are placed left to right in typed order: in `عدد 123 مرحبا` the first word
  starts at the left edge, where a bidi layout puts the last. The capture's
  SVG sidecar carries an x per character and shows it; the accessibility tree
  does not, so the trace cannot assert it.
- **The caret overshoots the end** (emilk/egui#8576). A right-to-left run of
  n characters lays out as 2n − 1 glyphs, and `End` counts glyphs: in the
  13-character seed it lands 6 past the text, so ArrowLeft does nothing for
  six presses and typing appends at the end.
- **A word jump from the end goes to the start** (emilk/egui#8698). In a
  debug build of egui the same keystroke can panic on overflow; this scene
  runs the release host.
- **Moving from the start is unaffected.** The surplus glyphs sit at the end of
  each run's range, so `Home` and ArrowRight count characters correctly.
- **imzero2's SVG export writes each run's characters in drawn order** (the
  run reversed). A viewer that shapes the SVG's text again joins the reversed
  string. That one is ours, and outside what this scene asserts.

Upstream fixes in flight at the time of measuring: emilk/egui#8698 (one glyph
per character for right-to-left runs, and the cursor clamp) and the draft
emilk/egui#8577 (bidi runs, visual row order, direction-aware caret).

```jsonl trace
{"do":"note","text":"open the demo: narrow the gallery, then expand its section"}
{"do":"wait","role":"text_input","comment":"the gallery's filter box is the only text input while every demo is folded"}
{"do":"type","role":"text_input","text":"right-to-left"}
{"do":"wait","contains":"right-to-left text","settleMs":400}
{"do":"click","contains":"right-to-left text"}
{"do":"wait","name":"reset","settleMs":600}
{"do":"capture","text":"rtl-text","sidecars":["svg","tree"]}

{"do":"note","text":"--- emilk/egui#8576's repro: End, ArrowLeft x3, type. Correct: x at index 10 (عدد 123 مرxحبا) ---"}
{"do":"click","id":3895437918154282393}
{"do":"key","text":"End"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"type","id":3895437918154282393,"text":"x"}
{"do":"read","id":3895437918154282393,"pattern":"^(?P<endLeft3>.*)$"}
{"do":"expect","of":"endLeft3","is":"عدد 123 مرحباx","comment":"pinned: the caret did not leave the end"}

{"do":"note","text":"--- the surplus: seven presses are the first to move the caret, i.e. End sat 6 glyphs past 13 characters. Correct: 1 press, x at 12 ---"}
{"do":"click","name":"reset"}
{"do":"click","id":3895437918154282393}
{"do":"key","text":"End"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"type","id":3895437918154282393,"text":"6"}
{"do":"read","id":3895437918154282393,"pattern":"^(?P<endLeft6>.*)$"}
{"do":"expect","of":"endLeft6","is":"عدد 123 مرحبا6","comment":"pinned: six presses still at the end"}
{"do":"click","name":"reset"}
{"do":"click","id":3895437918154282393}
{"do":"key","text":"End"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"key","text":"ArrowLeft"}
{"do":"type","id":3895437918154282393,"text":"7"}
{"do":"read","id":3895437918154282393,"pattern":"^(?P<endLeft7>.*)$"}
{"do":"expect","of":"endLeft7","is":"عدد 123 مرحب7ا","comment":"pinned: the seventh press is the first to move"}

{"do":"note","text":"--- emilk/egui#8698: a word jump from the end. Correct: y before the last word (عدد 123 yمرحبا) ---"}
{"do":"click","name":"reset"}
{"do":"click","id":3895437918154282393}
{"do":"key","text":"End"}
{"do":"key","text":"ArrowLeft","modifiers":18,"comment":"ctrl | command"}
{"do":"type","id":3895437918154282393,"text":"y"}
{"do":"read","id":3895437918154282393,"pattern":"^(?P<endWordLeft>.*)$"}
{"do":"expect","of":"endWordLeft","is":"yعدد 123 مرحبا","comment":"pinned: the jump lands at the start"}

{"do":"note","text":"--- control: from the start the caret counts characters ---"}
{"do":"click","name":"reset"}
{"do":"click","id":3895437918154282393}
{"do":"key","text":"Home"}
{"do":"key","text":"ArrowRight"}
{"do":"key","text":"ArrowRight"}
{"do":"key","text":"ArrowRight"}
{"do":"type","id":3895437918154282393,"text":"z"}
{"do":"read","id":3895437918154282393,"pattern":"^(?P<homeRight3>.*)$"}
{"do":"expect","of":"homeRight3","is":"عددz 123 مرحبا","comment":"correct, and expected to stay so"}
```
