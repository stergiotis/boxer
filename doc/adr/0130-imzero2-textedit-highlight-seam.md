---
type: adr
status: proposed
date: 2026-07-18
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0130: TextEdit highlight seam — syntax-colored SQL editing via CodeViewJob

## Context

The play app's SQL editor is a plain monospace `TextEdit`
(`c.TextEdit(...).CodeEditor()`); syntax color exists only in read-only views
through the `codeView` opcode, which ships text plus byte-range color sections
(a `CodeViewJob`) over the FFI and renders a cached egui `LayoutJob`
([ADR-0125](./0125-codeview-prepare-memo.md)). The survey
[sql-editor-highlighting-survey](../explanation/sql-editor-highlighting-survey.md)
compared the ways to get a highlighted *editing* experience (ClickHouse
`play.html`, the egui ecosystem, embedding micro, embedding neovim, plus a
second pass) and concluded the gap is one FFI seam: egui's
`TextEdit::layouter` lets a caller lay out the live buffer each frame, but the
egui2 IDL does not expose it, and nothing connects the repo's exact-dialect
lexer (`grammar1`) to an editable widget.

Constraints that shape the seam:

- **The FFI split makes spans stale by one frame.** Go computes sections from
  the buffer it received at the previous frame's `SendRespVal`; the live
  buffer inside egui may already contain this frame's keystrokes. The seam
  must tolerate mismatched sections gracefully — in an editor, a `LayoutJob`
  that does not cover every byte drops glyphs, which is unacceptable.
- **Cost discipline** (measured 2026-07-18, linux/amd64, Ryzen AI MAX+ 395,
  `-benchtime=2s`):

  | input | lex-only phase | full `Highlight` (lex+parse+CST refine) |
  | --- | --- | --- |
  | 180 B CTE | 26 µs, 20 KB, 219 allocs | 5.7 ms, 3.4 MB, 46 k allocs |
  | 2.5 KB query | 279 µs, 187 KB, 1.8 k allocs | 70 ms, 41 MB, 545 k allocs |

  The lex-only phase is comfortably per-keystroke (≤ 2 % of a 60 fps frame
  budget at editor-typical sizes; roughly linear at ~9 MB/s). The full parse
  is not — and at 70 ms / 2.5 KB it is not even per-quiescence on the render
  goroutine. (ADR-0125 recorded the steady-state parse cost as an open
  problem; these numbers confirm it worsens super-linearly with input size.)
- **Precedent.** The `insertAtCursor` method (ADR-0063) already demonstrates
  the required mechanics: a builder method stashes pending state on the
  interpreter, and TextEdit's custom apply block consumes it around
  `apply_widget`. Construction, the method loop, and apply share one
  interpreter match-arm scope, so a stack-local layouter closure can be
  attached between taking the widget and adding it — the `&mut FnMut`
  lifetime works without storing a closure.

The shape below was settled in a design dialogue on 2026-07-18.

## Design space (QOC)

**Question.** How does an editable egui2 TextEdit obtain syntax-colored
rendering for ClickHouse SQL?

**Options.**

- **O1** — `highlightJob` method on the existing `textEdit` opcode, consuming
  the existing `CodeViewJob` evaluated arg; spans produced Go-side by the
  lex-only phase of `nanopass/highlight`.
- **O2** — a dedicated `codeEdit` opcode/widget wrapping its own editor state
  and highlighting.
- **O3** — Rust-side ClickHouse lexer (vendored or ported
  `src/Parsers/Lexer`) running inside the layouter; no per-keystroke span
  traffic.
- *(Embedding an editor — micro, neovim, kakoune — was killed in the survey
  §4–§6 and is not re-argued here.)*

**Criteria.**

- **C1** — dialect fidelity with a single source of truth (`grammar1`).
- **C2** — integration cost / added IDL and Rust surface.
- **C3** — editor-state continuity (caret, undo, focus, `insertAtCursor`,
  `SendRespVal` unchanged).
- **C4** — typing latency / staleness of colors.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | ++ | −− |
| C2 | ++ | −  | −  |
| C3 | ++ | −− | ++ |
| C4 | +  | +  | ++ |

O3's only win is C4, and the reconcile step below recovers most of it for O1.
O3 was **rejected outright** in the design dialogue: a second in-tree dialect
definition outside `grammar1` (C++ vendoring or a hand port) is not an option
for this repository.

## Decision

We will add a `highlightJob` builder method to the existing `textEdit` opcode.
It consumes a `CodeViewJob` (the evaluated arg `codeView` already uses) and
installs, at apply time, an egui `TextEdit::layouter` that renders the live
buffer with the job's sections applied *advisorily*. Contract:

1. **Text stays authoritative in the TextEdit.** `SendRespVal`,
   `insertAtCursor`, undo, caret, focus and hint behavior are unchanged.
   Color is presentation only; an absent or empty job renders exactly as
   today. The feature is strictly additive and per-widget opt-in.
2. **Reconcile.** If the live buffer equals `job.text`, sections apply
   directly. Otherwise the helper computes the single edit region by
   common-prefix/common-suffix diff (O(n) on KB-sized buffers) and shifts
   section boundaries past the edit by the length delta, merging sections
   that overlap the edit region. This makes the one-frame staleness invisible
   during continuous typing: existing tokens keep their colors; just-typed
   characters inherit the surrounding span until corrected sections arrive
   next frame.
3. **Normalize (fail-safe).** Clamp sections to the buffer length, drop
   inverted ranges, enforce ascending order, and gap-fill uncovered byte
   ranges with the default monospace format. A malformed job degrades to
   plain text, never to missing text.
4. **Wrap preserved.** The layouter sets `wrap.max_width` from its
   `wrap_width` parameter, keeping TextEdit's existing wrap semantics
   (codeview's no-wrap job builder is not reused). Galley layout cost is
   absorbed by egui's persistent `Fonts` galley cache; no new Rust-side cache.
5. **Span source: the lex-only phase of `nanopass/highlight`,** exported from
   today's unexported `lexHighlight`, plus a one-token peek-ahead for `(` to
   classify function names (play.html's trick; a token-stream pass, no
   parse). `grammar1` remains the single dialect definition.
6. **Cost discipline.** Go relexes only when the bound text changed since the
   last frame; unchanged frames re-use the previous job (`.Keep()`). The
   editor path uses the **uncached `Build` tier** — per-keystroke content is
   new by construction, so the ADR-0125 `Prepare` memo would only churn its
   LRU. The full parse (`nanopass.Parse` + CST refine) never runs on the
   keystroke path.

Implementation mechanics (all expressed in the IDL definition +
`egui2gen`-regenerated dispatch; no hand edits to generated code):

- Method snippet stashes the job:
  `self.text_edit_pending_highlight = Some(std::mem::take(&mut self.r12_code_view_job));`
  — the same register take `codeView` performs, the same pending-on-self
  pattern as `insertAtCursor`.
- TextEdit's custom apply (ADR-0063 `MergeVerbatimCode` idiom) takes the
  pending job, declares the layouter closure as a stack local, attaches it
  via `w.layouter(&mut cl)`, then proceeds with the existing
  `apply_widget` / changed-push / pending-insert logic.
- Reconcile/normalize/job-build live in a new hand-written helper module
  (`text_edit_highlight.rs`, sibling of `code_view.rs`) with unit tests for
  the edit-region diff and the normalization invariants.
- Go-side glue: export the lex phase (`highlight` package), add a small
  builder that maps lex categories to theme colors (shared with codeview's
  SQL front-end), and switch play's `sqlTextEditField` over.

## Alternatives

- **O3 — Rust-side ClickHouse lexer in the layouter.** Rejected in the design
  dialogue (2026-07-18): it duplicates the dialect definition outside
  `grammar1` (vendored C++ or a port to keep in sync), for a latency
  advantage the reconcile step already neutralizes. Not deferred — rejected.
- **O2 — dedicated `codeEdit` opcode.** Duplicates TextEdit's retained editor
  state (caret, undo, focus, snippet insertion) behind a second identity and
  adds IDL surface for no fidelity or UX gain. A future line-number gutter
  (L3) can wrap a TextEdit in a container without moving this seam.
- **Clamp-only guard (no reconcile).** Saves ~40 lines of diff/shift logic
  but leaves every span after the caret misaligned by the insertion length
  for one frame — a visible color shimmer on the buffer tail during fast
  typing.
- **Extending `Section` with style flags now** (underline for error
  squiggles). Touches a wire struct shared with four existing codeview
  producers; deferred to L3 as a parallel `sectionStyled` method so existing
  producers do not move.
- **Overlay imitation of play.html, embedded editors (micro / neovim /
  kakoune), syntect grammars, webview editors, LSP.** Killed in the survey
  ([§2, §4–§6](../explanation/sql-editor-highlighting-survey.md)); the
  kill-reasons are recorded there and not repeated.

## Consequences

### Positive

- Exact-dialect highlighted SQL editing with zero new dependencies, one new
  IDL method, and one hand-written Rust helper; the read path (`codeView`)
  and its producers are untouched.
- The seam is span-source-agnostic and lands where L2 (quiescent semantic
  refine) and L3 (squiggles via `sectionStyled`, statement-under-cursor)
  attach without further IDL changes to the base contract.
- Other SQL-bearing TextEdits (e.g. the from/to ClickHouse-expression fields
  in the widgets IDL) can adopt the method for free.
- The fail-safe normalization means highlighting bugs degrade to plain text —
  the editor never loses content to a bad job.

### Negative

- Sections stream over the FFI on every frame in which the text changed
  (few KB per keystroke frame at editor-typical sizes). Idle frames pay
  nothing (`.Keep()`), but a future very-large-buffer use would need the
  per-line incremental approach noted in the survey.
- One-frame color staleness is inherent to the FFI split; reconcile hides it
  but cannot eliminate it (a keystroke that *changes the lexical class* of
  earlier text — e.g. typing the closing `*/` of a comment — corrects one
  frame later).
- The reconcile/normalize helper is subtle enough to need real unit tests
  (edit-region diff, UTF-8 boundary handling, pathological section lists);
  this package becomes load-bearing for editor rendering.
- L1 color depth is lexical only: keywords, literals, comments, operators,
  quoted identifiers, param slots, and peek-ahead function names — semantic
  colors (table/column/alias/CTE) arrive only with L2.

### Neutral

- The editor path deliberately bypasses the ADR-0125 memo; the memo remains
  correct for read-only and quiescent paths.
- The measured full-`Highlight` cost (70 ms at 2.5 KB) hardens a constraint
  on **L2**: the quiescent semantic pass must run off the render goroutine
  (bgjob) or wait for ADR-0125's open steady-state problem to be fixed —
  synchronous-on-quiescence would drop frames. Recorded here so L2's design
  starts from it.
- The lex-only exported API creates the natural place to later fix the open
  ADR-0125 item (the 46 k-alloc parse) without touching the editor seam.

## Status

Proposed — awaiting review by the repository maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [sql-editor-highlighting-survey](../explanation/sql-editor-highlighting-survey.md) —
  the option space and kill-reasons this ADR builds on.
- [ADR-0125](./0125-codeview-prepare-memo.md) — codeview `Prepare*` memo; the
  open steady-state parse cost this ADR routes around.
- [ADR-0063](./0063-imzero2-textedit-insert-at-cursor.md) — TextEdit
  `insertAtCursor`; the pending-state + custom-apply idiom this seam reuses.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the ANTLR DFA
  cache the lex phase shares.
- egui 0.35 `TextEdit::layouter` (`widgets/text_edit/builder.rs`) — the
  upstream hook.
- ClickHouse `play.html` (read as served by ClickHouse 26.6.1) — prior art
  for lex-only per-edit highlighting and the function-name peek-ahead.
