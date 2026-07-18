---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Survey compiled 2026-07-18. Sources:
> `play.html` as served by a local ClickHouse 26.6.1 instance (read in full),
> the `egui` / `egui_extras` 0.35.0 crate sources this repo already builds
> against, and the public repositories/documentation of micro, neovim and the
> tree-sitter ecosystem (see §8). This is a survey with a recommendation, not
> a design; if the recommendation is adopted, the editor-side FFI seam should
> go through a design dialogue (and likely an ADR) first.

# Syntax-highlighted SQL editing in imzero2 — a survey

The play app's SQL editor is a plain monospace `TextEdit`; syntax color exists
only in read-only views. This survey compares four ways to get a highlighted
*editing* experience for ClickHouse SQL: imitating ClickHouse's own
`play.html`, building on what egui already provides, embedding the
[micro](https://github.com/micro-editor/micro) editor, and embedding neovim as
an editor server. The short version: the repo already owns the two hard parts
(an exact-dialect lexer and a span→`LayoutJob` render cache), egui already
ships the hook that connects spans to an editable widget, and none of the
embedding options improves highlighting fidelity — so the gap is one FFI seam,
not a subsystem.

## 1. Current state in this repository

What exists today, and where it stops:

- **The editor is unhighlighted.** All three SQL-editor variants in play go
  through one builder chain:
  `c.TextEdit(...).CodeEditor().DesiredRows(...)...`
  ([`apps/play/play_renderer.go`](../../apps/play/play_renderer.go),
  `sqlTextEditField`). egui's `code_editor()` preset sets monospace font and
  tab-capture — nothing else. No color, no line numbers, no error marks.
- **Read-only highlighting is solved.** The `codeView` opcode ships text plus
  byte-range color sections (a `CodeViewJob`) across the FFI; the Rust side
  builds an egui `LayoutJob` through a content-keyed cache and renders a
  selectable label
  ([`egui2_definition_d_code_view.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_code_view.go)).
  Go-side front-ends exist for SQL, Go, JSON and markdown
  ([`widgets/codeview`](../../public/thestack/imzero2/egui2/widgets/codeview)),
  memoised per [ADR-0125](../adr/0125-codeview-prepare-memo.md).
- **SQL span production is two-phase.**
  [`highlight.Highlight`](../../public/db/clickhouse/dsl/nanopass/highlight/dsl_highlight.go)
  first lexes with the repo's own ANTLR ClickHouse grammar (`grammar1`) for
  baseline classification, then runs a full `nanopass.Parse` + CST walk to
  refine identifiers into semantic categories (table/column/alias/CTE/…).
  When the parse fails, the lex-only spans are returned as-is — so a
  lex-only highlighter already exists as the (currently unexported) first
  phase, `lexHighlight`.
- **The cost problem sits in phase two.** ADR-0125 measured ~3.5 ms
  steady-state for a 180-byte CTE — all in parse + refine, not the lex — and
  left the cause open. This matters for editing specifically: content-keyed
  memoisation is defeated by construction while typing, since every keystroke
  produces new content. An editor path must not run the full parse per
  keystroke.

## 2. Reference point: ClickHouse `play.html`

ClickHouse's bundled web UI (one self-contained HTML file, read here as served
by ClickHouse 26.6.1) gets a highlighted editor out of a plain `<textarea>`:

- **Overlay architecture.** A `#query-backdrop` div sits behind the textarea
  and carries the colored copy of the text; once populated, the textarea's own
  text is made transparent (caret and selection survive via a
  `mix-blend-mode: difference` arrangement). A CSS comment states the
  invariant bluntly: *"Typography MUST match the textarea exactly."* The whole
  overlay exists because a DOM textarea cannot restyle its own content — a
  limitation egui does not share (§3).
- **The real lexer, not an imitation.** The token source is ClickHouse's own
  C++ `src/Parsers/Lexer` compiled to a ~8 KB WebAssembly module and embedded
  base64 in the HTML (`base64 -w0 build/src/Parsers/Lexer.wasm`). Dialect
  fidelity is achieved by shipping the server's actual lexer to the client.
- **Lex-only, whole-buffer, per input event, synchronous.** Every `input`
  event re-tokenizes the entire buffer and rebuilds the backdrop HTML. A code
  comment explains why it is deliberately *not* deferred with `setTimeout`:
  the browser would paint an unhighlighted frame first. There is no parse and
  no incrementality; linear lexing at query sizes is cheap enough.
- **Classification is lexer + two small tricks.** The lexer provides
  structure (strings, numbers, comments, quoted identifiers, operators, and
  error tokens — the latter rendered with a wavy underline). A JS keyword set
  (~300 entries) promotes bare words to keywords, and a peek-ahead for `(`
  distinguishes function names from identifiers.
- **The token stream pays twice.** The same tokenization drives
  multi-statement splitting and "query under cursor" (run the statement the
  caret is in, selecting it first) — the run-selected UX is lexer-derived,
  not string-heuristic.

Lessons carried forward: lex-only per edit is the accepted fidelity/cost point
for ClickHouse SQL editing; exactness comes from owning the lexer (which this
repo does, via `grammar1`); and statement-level affordances fall out of the
token stream for free.

## 3. What egui provides

- **The hook exists and is first-class.**
  `TextEdit::layouter(&mut dyn FnMut(&Ui, &dyn TextBuffer, f32) -> Arc<Galley>)`
  (egui 0.35, `widgets/text_edit/builder.rs`) lets the caller lay out the
  *live* buffer each frame — the widget paints whatever galley the layouter
  returns while remaining fully editable. A highlighted editor in egui is
  "TextEdit + a function that returns a colored `LayoutJob`". No overlay, no
  typography-matching invariant. This hook is not currently exposed through
  the egui2 IDL — that is the actual gap.
- **Stock highlighters are dialect-poor.**
  `egui_extras::syntax_highlighting::highlight()` is memoised in egui's frame
  cache; its default backend is a small hand-rolled lexer (six token types,
  per-language keyword tables — no SQL), and the optional `syntect` feature
  swaps in Sublime-grammar highlighting (generic SQL only) at the cost of the
  syntect/regex dependency tree. The repo links `egui_extras` 0.35 already,
  without `syntect`. Neither backend knows ClickHouse.
- **Third-party editor widgets are the same fidelity tier.**
  [`egui_code_editor`](https://github.com/p4ymak/egui_code_editor) (v0.3.1,
  maintained) is a self-contained TextEdit-with-layouter plus keyword-set
  syntax tables, line numbers and simple completion. A ClickHouse `Syntax`
  table would be easy to define, but classification stays keyword-set-grade.
  More useful as a parts reference (line-number gutter, completion popup) than
  as a dependency, since it brings its own theming and id handling.
- **No tree-sitter escape hatch.** There is no tree-sitter grammar for the
  ClickHouse dialect; the closest is the generic
  [tree-sitter-sql](https://github.com/derekstride/tree-sitter-sql).
  ClickHouse-specific syntax (lambdas `x -> y`, `{param:Type}` slots,
  `ARRAY JOIN`, `$$` heredocs, `::` casts) would degrade to error-node
  recovery. Separately, tree-sitter's defining advantage — incremental
  reparse of large buffers — does not bind at SQL-snippet sizes, where a full
  relex is linear and far below frame budget.

## 4. Embedding micro

micro is a Go terminal editor (MIT). Rendering is exclusively through tcell
(terminal cells); the editor core — buffer, display, action — lives under
`internal/` and is not importable; only `pkg/highlight` (a regex engine driven
by YAML syntax files, 130+ languages including a generic `sql.yaml`) is public
API. "Embedding" therefore decomposes into three distinct undertakings:

1. **Stock micro in a PTY behind a terminal-emulator widget.** egui terminal
   widgets exist ([egui_term](https://github.com/Harzu/egui_term),
   alacritty-backend, self-described as under development;
   [egui-terminal](https://crates.io/crates/egui-terminal) similarly). This
   buys micro's full UX — and creates a cell-grid island: its own fonts,
   theme, clipboard and key handling inside the egui2 look; a PTY lifecycle
   to manage; and complete opacity to the accessibility tree, which breaks
   the egui_mcp/kittest-driven verification this repo uses for its own UIs.
   Highlighting would be micro's regex tier (a hand-written
   `clickhouse.yaml` at best).
2. **Library reuse.** Not available for the part that matters: the editor
   core is `internal/`. The importable part — the regex highlighter — is
   precisely what this repo does not need, being strictly weaker than
   `grammar1`. (Prior art: `pgavlin/femto` extracted micro's core for tview,
   i.e. the extraction is possible but lands in terminal-widget land anyway.)
3. **Fork micro and fake the screen.** micro draws through the `tcell.Screen`
   interface, and tcell ships a `SimulationScreen`; a fork could run the
   editor in-process against a captured cell grid, blitted into an imzero2
   widget with injected key events. Feasible in principle; in practice a
   permanent fork of `internal/` packages, an event-loop marriage, and still
   a terminal-cell UX and regex-tier highlighting.

Verdict: every route pays subsystem-level cost, none improves fidelity, and
the one unique thing micro would add (a complete small editor with plugins)
arrives in a foreign shell that the repo's own tooling cannot see into.

## 5. Embedding neovim

Unlike micro, neovim is *architecturally designed* for external frontends:
`nvim --embed` runs the editor as a child process speaking msgpack-RPC; the
frontend calls `nvim_ui_attach` with `ext_linegrid` and receives batched
redraw events — `hl_attr_define` (rgb attribute table), `grid_line` (runs of
cells with highlight ids), cursor/mode events, `flush` — and sends keys back
via `nvim_input` ([UI protocol docs](https://neovim.io/doc/user/api-ui-events/)).
The official Go client ([neovim/go-client](https://github.com/neovim/go-client))
supports exactly this shape (`NewChildProcess`, `AttachUI`, redraw handlers),
and [goneovim](https://github.com/akiyosi/goneovim) is an existence proof of a
complete Go frontend.

What it would buy: a real modal editor (undo tree, macros, text objects,
search), the plugin ecosystem (including LSP — relevant if a SQL language
server ever matters), and a stable, versioned protocol built for this use.

What it would cost imzero2:

- **A grid-renderer widget family.** ext_linegrid is a styled monospace cell
  grid with its own cursor and modes; rendering it well (plus floats/popup
  menus via multigrid, or accepting them drawn into the main grid) is a new
  widget subsystem, not an opcode. The async redraw stream fits the existing
  bgjob pattern (apply batches on `flush`, request repaint), so the frame-loop
  marriage is workable — but it is still a subsystem.
- **An external binary dependency.** nvim would be a hard runtime dependency
  resolved at user machines, with version skew to manage.
  [ADR-0118](../adr/0118-extbin-external-process-chokepoint.md) gives
  external-binary *resolution* a chokepoint, so the dependency is
  representable — but a required third-party editor process sits uneasily
  with the sovereign-toolkit premise
  ([why-boxer](./why-boxer.md)).
- **No fidelity gain for ClickHouse SQL.** nvim bundles no SQL tree-sitter
  parser; installing one via nvim-treesitter yields the *generic* SQL grammar
  (§3), and the fallback is regex `syntax/sql.vim`. Either way, highlighting
  inside an embedded nvim would be less exact than what `grammar1` already
  produces in-process.
- **Interaction-model friction.** Modal editing as the default for casual
  query tweaks is a paradigm switch; caret/selection semantics live inside
  nvim, so accessibility-tree exposure and egui_mcp driving would be partial
  at best.

Verdict: the right architecture *if the requirement were "a general-purpose
embedded editor pane"* — and disproportionate for highlighted SQL editing,
where it adds a subsystem and an external dependency while *lowering* dialect
fidelity. Worth revisiting only if an editor-pane requirement materialises on
its own merits.

## 6. Comparison

| Approach | CH-SQL fidelity | Editing UX gained | Integration cost | Accessibility / egui_mcp | New dependencies |
| --- | --- | --- | --- | --- | --- |
| play.html imitation (overlay) | exact (their lexer) | none (textarea-grade) | n/a — DOM-specific workaround | n/a | n/a |
| egui layouter + in-repo lexer | **exact (`grammar1`)** | TextEdit as today, plus color/error marks | one IDL seam + span source | native (TextEdit unchanged) | none |
| `egui_extras` syntect | generic SQL | as above | feature flag + theme mapping | native | syntect tree |
| `egui_code_editor` | keyword-set | line numbers, completion | vendor/adapt widget | native-ish | small crate |
| micro (any route) | regex tier | full small editor | PTY+terminal widget, or fork | opaque | micro (+ terminal widget / fork) |
| neovim (`--embed`) | generic SQL (no CH grammar) | full modal editor, plugins, LSP | grid-renderer subsystem + process mgmt | partial | nvim binary + go-client |

## 7. Recommendation

Close the narrow gap rather than import an editor. The repo already holds the
exact lexer (`grammar1`), a span container that crosses the FFI
(`CodeViewJob`), and a Rust-side span→`LayoutJob` cache; egui already provides
`TextEdit::layouter`. What is missing is one seam and one discipline:

- **L1 — the seam.** A `TextEdit` builder method (sketch: `HighlightJob(job)`)
  that installs a Rust-side layouter consuming the same `CodeViewJob` section
  machinery `codeView` uses. Because Go computes spans from the buffer it
  received at the previous frame's `SendRespVal`, sections lag the live buffer
  by one frame while typing; the layouter must treat spans as advisory —
  clamp to buffer length, drop trailing mismatches, fall back to unstyled for
  undescribed suffixes. Text remains authoritative in the TextEdit; color is
  presentation only. (Design dialogue before implementation; this touches the
  IDL.)
- **L1 — the span source.** Export the lex-only phase of
  `highlight.Highlight` (today's unexported `lexHighlight`) and use it per
  keystroke: it is the same fidelity/cost point play.html ships, from a
  better lexer. Function-vs-identifier can use play.html's one-token
  peek-ahead on the token stream.
- **L2 — semantic refinement, off the keystroke path.** Run the full
  parse + CST refine (the existing phase two) only when the buffer goes
  quiescent, upgrading colors after the fact — the same two-tier scheme
  play.html uses (keyword set now, nothing later) but with a real semantic
  tier. This also contains ADR-0125's open steady-state cost by keeping
  `nanopass.Parse` out of the per-edit loop entirely.
- **L3 — token-stream affordances.** Error-token underlines (play.html's
  `q-err`), statement-under-cursor execution for multi-statement buffers
  (port of `getQueryUnderCursor`'s token walk), and — if wanted — a
  line-number gutter with `egui_code_editor` as a reference implementation.

Kill-reasons recorded above for the descoped options: micro (unimportable
core, terminal island, accessibility opacity, no fidelity gain) and neovim
(subsystem-scale cost and an external binary for *lower* dialect fidelity;
re-enters only if a general editor-pane requirement appears).

## 8. Sources

- `play.html` served by a local ClickHouse 26.6.1.1193 (`GET /play.html`,
  192 KB single file; backdrop CSS, embedded lexer WASM, `tokenize()`,
  `tokenClass()`, `getQueryUnderCursor()` read directly).
- `egui` 0.35.0 and `egui_extras` 0.35.0 crate sources (the versions in
  `rust/imzero2/Cargo.toml`): `widgets/text_edit/builder.rs`,
  `syntax_highlighting.rs`.
- [micro](https://github.com/micro-editor/micro) (MIT; tcell; `pkg/highlight`
  public, editor core `internal/`),
  [egui_term](https://github.com/Harzu/egui_term),
  [egui-terminal](https://crates.io/crates/egui-terminal),
  [egui_code_editor](https://github.com/p4ymak/egui_code_editor).
- [Neovim UI protocol](https://neovim.io/doc/user/api-ui-events/),
  [ui.txt](https://neo.vimhelp.org/ui.txt.html),
  [neovim/go-client](https://github.com/neovim/go-client),
  [goneovim](https://github.com/akiyosi/goneovim),
  [tree-sitter-sql](https://github.com/derekstride/tree-sitter-sql).
- In-repo: [`apps/play/play_renderer.go`](../../apps/play/play_renderer.go),
  [`widgets/codeview`](../../public/thestack/imzero2/egui2/widgets/codeview),
  [`nanopass/highlight`](../../public/db/clickhouse/dsl/nanopass/highlight),
  [ADR-0123](../adr/0123-play-content-typed-detail-cells.md),
  [ADR-0125](../adr/0125-codeview-prepare-memo.md).
