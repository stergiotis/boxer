---
type: adr
status: proposed
date: 2026-07-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0015: Regex pattern syntax highlighting — a hand-rolled lexer with `regexp` as validator

> **On the number.** [ADR-0005](./0005-streaming-persisted-kafka-from-connect.md) records
> that it was "originally drafted in a downstream consumer … as its ADR-0015" and migrated
> here as ADR-0005. That is the *consumer's* numbering; this repository has never had an
> ADR-0015, and the slot is claimed here per the register's practice of reusing the lowest
> free hole (0008, 0120, 0124 and 0126 were all vacated and re-issued).

## Context

The regex explorer
([`demo/apps/regex_explorer`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer))
is an SQL-correctness tool: you type a pattern and a haystack, Go's `regexp` paints the
matches locally, and ClickHouse answers `match` / `extractAll` / `replaceRegexpAll` /
`multiMatchAllIndices` through the chlocalbroker, so you can see what the engine you
actually ship against will do ([ADR-0054](./0054-regex-explorer-offset-authority.md)). It
is also embedded as an inspector by the
[`widgets/regexsummary`](../../public/thestack/imzero2/egui2/widgets/regexsummary) widget.

Its pattern editors are uncoloured plain text. The app's entire subject matter is regex,
but the regex itself gets no visual structure — group nesting, character classes,
quantifiers and escapes all read as one undifferentiated string. play's SQL editor closed
exactly this gap in [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) with a
`TextEdit.HighlightJob` seam fed by a Go-side lexer, and the repo already ships three
highlighters on that seam (`sql`, `json`, `go`, plus `markdown` for read-only views). Regex
is the conspicuous omission.

Constraints that shape the answer:

- **Total byte coverage is non-negotiable.** ADR-0130 §3: "a `LayoutJob` that does not
  cover every byte drops glyphs". The Rust layouter gap-fills, but a highlighter that
  leaves holes is relying on that recovery rather than honouring the contract.
- **Invalid input is the normal state.** A half-typed pattern is what this editor holds
  most of the time. `App.getCompiledRegexp` already caches *failures* for exactly this
  reason (ADR-0054 §SD3). Anything that blanks or drops text on a partial pattern is
  unusable here.
- **Per-keystroke cost.** ADR-0130 budgets the editor tier at lex-only cost;
  [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) documents what happens when a
  per-input *parser* accumulates state on a hot path.
- **ADR-0054 already named an authority.** Go's `regexp` decides validity, matches, and
  offsets for this app. Colouring must agree with *that*, which means lexing on the Go side.

The option space below was surveyed in
[regex-explorer-m3-plan](../explanation/regex-explorer-m3-plan.md) §2, with in-tree sources
read from the working tree, `regexp/syntax` from `$GOROOT`, and `dlclark/regexp2` from the
module cache.

## Design space (QOC)

**Question.** How does the explorer obtain byte-offset-carrying syntactic structure for an
RE2 pattern, per keystroke, on partially-typed input?

**Options.**

- **O1** — Hand-rolled Go lexer in a new `widgets/regexhighlight`, matching the
  `jsonhighlight` / `gohighlight` / `markdownhighlight` shape.
- **O2** — stdlib `regexp/syntax.Parse` — the authoritative RE2 parser.
- **O3** — A new ANTLR grammar (`.g4` → generated Go), matching the workflow behind
  `dsl/grammar0|1|2` and `canonicaltypes/grammar`.
- **O4** — `github.com/dlclark/regexp2/syntax` — already in the module graph as an
  indirect dependency.
- **O5** — Rust-side `regex-syntax` AST inside the ADR-0130 layouter; the crate is already
  transitively present in the imzero2 Rust tree.
- **O6** — `alecthomas/chroma` — general-purpose Go highlighter, Pygments-derived.
- **O7** — `timtadh/lexmachine` — regex-driven DFA lexer generator with positions.

**Criteria.**

- **C1 — Byte offsets.** Does it yield `(start, stop)` per token over the exact buffer, with
  total coverage? Non-negotiable — the `CodeViewJob` contract is byte ranges.
- **C2 — Behaviour on invalid input.** Must degrade, never blank or drop text.
- **C3 — Per-keystroke cost and retained memory.**
- **C4 — Dialect fidelity to RE2** — the engine this app exists to predict.
- **C5 — Dependency and maintenance cost.** New module, build step, generated artefacts.
- **C6 — Fits the established shape.** Three highlighters already land on one `Span` →
  `codeview` seam.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 hand-rolled | O2 `regexp/syntax` | O3 ANTLR | O4 regexp2 | O5 Rust `regex-syntax` | O6 chroma | O7 lexmachine |
|----|----|----|----|----|----|----|----|
| C1 offsets       | ++ | −− | ++ | −− | ++ | −  | +  |
| C2 invalid input | ++ | −− | +  | −− | −  | ++ | +  |
| C3 cost/memory   | ++ | +  | −− | +  | ++ | −  | +  |
| C4 RE2 fidelity  | +  | ++ | +  | −− | −  | −  | +  |
| C5 dependencies  | ++ | ++ | −  | +  | −  | −− | −  |
| C6 fits shape    | ++ | −  | −  | −  | −− | −  | +  |

## Decision

We will hand-roll a Go lexer in a new package
`public/thestack/imzero2/egui2/widgets/regexhighlight` (O1), feeding the existing
`codeview` → `TextEdit.HighlightJob` seam, and we will **keep Go's `regexp` as the
validity authority**.

The division of labour is the one ADR-0054 already set up, extended by one step: **Go's
`regexp` decides what is true** (validity, matches, offsets — the existing `compileCache`
and the red error label), **the lexer decides only how it is painted**. The lexer therefore
never claims authority it does not have, and a lexer/compiler disagreement shows up visibly
(colour versus the error label) rather than silently.

Three things this surface needs together, and only O1 delivers all three:

1. **Total byte coverage including the invalid tail** — which is why a *parser* (O2, O3,
   O5) is the wrong tool: parsers answer "is this well-formed", and the answer here is
   usually "not yet".
2. **A per-span nesting depth**, enabling bracket-pair colourisation of group parens — the
   highest-value affordance for reading a nested regex, and something no off-the-shelf
   option surfaces.
3. **A third instance of an established in-repo shape**, so it introduces no new concepts,
   no new build step, and no new generated artefacts.

### SD1 — Categories, and what `CategoryError` is for

The package exports `CategoryE`, `Span`, and `Highlight(src) []Span`, mirroring
`jsonhighlight`. Categories:

| Category | Covers |
| --- | --- |
| `CategoryLiteral` | ordinary characters |
| `CategoryMeta` | `.` `\|` |
| `CategoryQuantifier` | `*` `+` `?` `{n,m}` and the lazy `?` suffix |
| `CategoryAnchor` | `^` `$` `\A` `\b` `\B` `\z` |
| `CategoryEscape` | `\.` `\n` `\x7f` `\x{…}` `\Q` `\E` |
| `CategoryClassName` | `\d` `\w` `\s` `\pL` `\p{Greek}` `[:alpha:]` |
| `CategoryClassDelim` | `[` `[^` `-` `]` |
| `CategoryClassLiteral` | members inside a character class |
| `CategoryGroup` | `(` `)` `(?:` `(?P<` `>` `:` |
| `CategoryGroupName` | the name in `(?P<name>…)` |
| `CategoryFlags` | the `imsU-` letters in `(?ims)` / `(?i:` |
| `CategoryError` | a lone trailing `\`; an unbalanced `)` |

`CategoryError` is reserved for those two **byte-level certainties**. An unterminated `[…`
or `(…` keeps its content categories rather than turning red: those are the normal
mid-typing states, and the compile-error label already carries the truth. Painting them red
would make the editor flash error colour on every second keystroke.

### SD2 — `Span.Depth`, and depth-cycled group parens

`Span` carries a `Depth int32` — group nesting depth — so the palette can cycle group-paren
colour by depth rather than painting one flat colour. A `(` and its matching `)` share a
depth; content between them carries one more.

The cycle is a hand-picked four-colour set in `codeview`, **not**
`styletokens.QualitativeCycle` — the palette the Preview tab's per-capture-group cells use.
That palette (BatlowS) is chosen for *fills*, and its first entry is near-black `(1, 25,
89)`: legible behind dark text, invisible as foreground text on the dark editor background.
The relationship to the Preview tab is therefore conceptual — both cycle by ordinal — not a
shared constant.

Two correctness points the depth counter turns on:

- `(?i)` is a **flag setting**, not a group: it consumes its own `)` and must not change
  nesting depth. `(?i:` is a group and does. Same two-character prefix, opposite structural
  effect.
- A bare `]` or `}` outside its construct is a literal in RE2 and must not be mistaken for
  a delimiter.

### SD3 — `HighlightLines` for the multi-pattern editor

The `patternList` editor holds a *list* of regexes, one per line, not one regex.
`HighlightLines(src)` lexes each line independently and resets depth at each newline;
lexing it as a single pattern would let an unclosed `(` on line 1 mis-colour line 7.

### SD4 — No L2 / async tier

ADR-0130 needed a two-tier lex/semantic split because the SQL semantic tier costs 70 ms per
2.5 KB. The regex lexer is O(n) over a pattern of tens of bytes — orders of magnitude below
that. `sqlSemanticHl` has no analogue here and is deliberately not copied.

### SD5 — The coverage invariant, asserted in tests

Exactly as `jsonhighlight` documents it: spans cover every byte of the input exactly once,
in ascending order, with `src[Start:Stop] == Text`. Asserted by table-driven tests per
category plus a `pgregory.net/rapid` property test over random byte strings. Escaped
non-ASCII (`\é`) consumes a whole rune via `utf8.DecodeRuneInString`, so no span ever splits
a UTF-8 sequence.

### SD6 — Wiring, and what is left uncoloured

`HighlightJob` is attached to the `pattern` and `patternList` editors, gated on a non-empty
buffer, with the same rebuild-on-change cache shape as `PlayApp.sqlEditorHighlightJob`. The
`pattern` editor also gains `.CodeEditor()`: the Rust layouter resolves
`TextStyle::Monospace` unconditionally, so without it the field's font would change the
moment a character is typed.

The `replacement` editor is deliberately left alone. `\1` backrefs are not RE2 *pattern*
syntax, and colouring them with the pattern palette would be a lie.

## Alternatives

- **O2 — `regexp/syntax`.** `syntax.Regexp` (`$GOROOT/src/regexp/syntax/regexp.go:18`)
  carries `Op, Flags, Sub, Sub0, Rune, Rune0, Min, Max, Cap, Name` — and **no source
  positions of any kind**. Offsets are absent, not merely inconvenient; the parser also
  folds and rewrites (literal merging, `Simplify`), so even a reconstruction attempt would
  not map back to the typed bytes. Worse for C2: `Parse` errors on *any* invalid input, so
  the typical interactive state produces nothing at all. Fails the non-negotiable criterion
  twice. *This does not remove `regexp` from the design* — it stays as the validator.
- **O3 — a new ANTLR grammar.** Mechanically viable, and boxer owns the whole workflow. Two
  decisive costs. (1) ADR-0084 measured the Go runtime's DFA cache growing without eviction
  and with no `ClearDFA` in the Go port — 172,281 retained states / 1.95 GB at 10k
  adversarial nested inputs, 1,328,006 / 14.7 GB at 500k — and found that *nested expression
  structure does not saturate*. A regex editor is precisely a nested-structure generator,
  driven per keystroke by a user deliberately writing unusual nesting: the documented worst
  case, on the hottest path. (2) grammars-v4 ships PCRE, a different dialect; there is no
  RE2 `.g4`, so this means authoring *and maintaining* a grammar plus a generation step for
  a twelve-token language.
- **O4 — `dlclark/regexp2/syntax`.** Present in `go.sum` (v1.11.5), so "free" on paper. In
  fact `regexNode` (`syntax/tree.go:55`) holds `t, children, str, set, ch, m, n, options,
  next` — no offsets — and is *unexported*; `Parse` returns a `*RegexTree` whose root is
  unreachable from outside the package. And it implements the .NET/PCRE dialect
  (backtracking, backreferences, lookaround), not RE2. A non-starter twice over.
- **O5 — Rust-side `regex-syntax`.** Its `ast` module does carry byte spans and the crate is
  already transitively present. But it parses the Rust `regex` crate's dialect, not RE2; it
  needs a new opcode and IDL surface; its AST parser errors on invalid input exactly like
  O2; and **ADR-0130 §O3 already rejected this exact shape** for SQL, on the grounds that a
  second in-tree dialect definition outside the Go authority "is not an option for this
  repository". The rule applies with more force here, since ADR-0054 makes Go's `regexp`
  this app's declared authority.
- **O6 — chroma.** Not a dependency today, and neither Pygments' nor chroma's catalogue
  contains a lexer for *regular-expression syntax as a language* — their catalogues are
  programming and markup languages, and chroma's `Rule`/regex machinery is how lexers are
  *written*, not a regex-language lexer. So adopting chroma means **authoring** a lexer in
  its XML rule DSL rather than getting one for free. Its `Iterator` yields
  `Token{Type, Value}` with no byte offsets (recoverable by accumulating lengths), it has no
  notion of nesting depth, and it is a large new dependency: worst of both worlds. *(Caveat:
  the "no regex-language lexer" claim comes from the catalogue, not an in-tree check —
  confirm with `chroma --list` before relying on it. It does not change the ranking, since
  C1/C5 already sink the option.)*
- **O7 — `timtadh/lexmachine`.** Its tokens carry start/end byte offsets, so C1 is
  satisfied. But the regex language is context-sensitive in ways it cannot express
  declaratively: inside-vs-outside a character class; `(?i)` versus `(?i:`; `\Q…\E` literal
  mode. lexmachine has no flex-style start conditions, so all of that becomes a hand-written
  state machine *wrapped around* the library — the same code as O1, plus a dependency and a
  generated DFA. The win over O1 is nil.

## Consequences

### Positive

- Group nesting, character classes, quantifiers and escapes become readable at a glance,
  with depth-cycled parens for nested groups.
- Zero new dependencies, no new build step, no generated artefacts. `go mod tidy --diff`
  stays clean, and that is checked mechanically.
- A fourth instance of one seam (`Span` → `codeview` → `HighlightJob`) rather than a fourth
  way of doing highlighting.

### Negative

- **A hand-rolled lexer can drift from RE2's real grammar.** This is the honest residual
  risk. It is contained by the authority split (the lexer never decides validity) plus the
  SD5 coverage invariant, not eliminated.
- One more palette to keep in the family with `sql` / `json` / `go`.

### Neutral

- The `replacement` editor stays plain text; a replacement-syntax highlighter would be its
  own decision.
- Depth cycling shares no palette constant with `renderCaptureGroups`' `QualitativeCycle`
  (see SD2 for why), and the indices would not line up even if it did: in `(a)(b)` both
  groups sit at depth 0, while the Preview tab gives them cycle slots 0 and 1. Unifying them
  is deferred.
- The `codeview` face is `BuildRegex` / `PrepareRegex` and `BuildRegexList` /
  `PrepareRegexList`. `List`, not `Lines`: in `codeview` the `*Lines` suffix already means a
  line-numbered gutter window over one document (`BuildGoLines`), which is a different
  operation from lexing each line as its own pattern.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0054](./0054-regex-explorer-offset-authority.md) — Go `regexp` as the explorer's
  offset and validity authority; the split this ADR extends by one step.
- [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) — the `TextEdit.HighlightJob` seam,
  its total-coverage requirement, and the O3 (Rust-side dialect) rejection reused here.
- [ADR-0125](./0125-codeview-prepare-memo.md) — the `Build*` / `Prepare*` naming split this
  package's `codeview` face follows.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the unbounded ANTLR DFA cache
  that sinks O3.
- [regex-explorer-m3-plan](../explanation/regex-explorer-m3-plan.md) — the survey this ADR
  freezes, with the full source list.
- [RE2 syntax](https://github.com/google/re2/wiki/Syntax) — the dialect being lexed.
