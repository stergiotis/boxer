---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-07-25. This is a survey
> with a recommendation *plus* the implementation plan that follows from it. The
> two decisions it reaches are to be frozen as ADR-0015 and ADR-0017 before the
> code lands (per [`CODINGSTANDARDS.md` § Design Before Code](../../CODINGSTANDARDS.md));
> this document is the input to those ADRs, not a substitute for them. Sources
> read in full are listed in §8 — in-tree sources were read from this working
> tree, `regexp/syntax` from `$GOROOT`, and `dlclark/regexp2` from the module
> cache.

# Regex explorer M3 — pattern highlighting and extraction hand-off

The regex explorer ([`public/thestack/imzero2/egui2/demo/apps/regex_explorer/`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer))
is an SQL-correctness tool: you type a pattern and a haystack, Go's `regexp`
paints the matches locally, and ClickHouse answers `match` / `extractAll` /
`replaceRegexpAll` / `multiMatchAllIndices` through the chlocalbroker, so you can
see what the engine you actually ship against will do
([ADR-0054](../adr/0054-regex-explorer-offset-authority.md)). It is also embedded
as an inspector by the [`widgets/regexsummary`](../../public/thestack/imzero2/egui2/widgets/regexsummary)
widget.

Two gaps motivate this milestone.

**The pattern editors are uncoloured plain text.** The app's entire subject
matter is regex, but the regex itself gets no visual structure — group nesting,
character classes, quantifiers and escapes all read as one undifferentiated
string. play's SQL editor closed exactly this gap in
[ADR-0130](../adr/0130-imzero2-textedit-highlight-seam.md) with a
`TextEdit.HighlightJob` seam fed by a Go-side lexer, and the repo already ships
three highlighters on that seam (`sql`, `json`, `go`, plus `markdown` for
read-only views). Regex is the conspicuous omission.

**The extraction dead-ends inside the window.** The Preview tab knows every match
and capture group with byte offsets; the List tab knows what ClickHouse returned.
You can look at both, but you cannot *query* either, and you cannot compare them
except by eye — even though the app's own docs warn that the two can disagree
(`extractAll` returns capture group 1, not the full match, whenever the pattern
captures). The ad-hoc dataset capability
([ADR-0134](../adr/0134-adhoc-datasets.md)) and launch requests
([ADR-0135](../adr/0135-app-launch-requests.md)) already provide the mechanism to
fix this; a downstream consumer has proven the pattern end to end.

---

## 1. What exists today

- **Editors.** `renderBody` in
  [`regex_explorer.go`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer.go)
  builds three `TextEdit`s: `pattern` (single-line, proportional), `patternList`
  (multi-line `CodeEditor`, one regex per line), `haystack` (multi-line
  `CodeEditor`). None carries a `HighlightJob`.
- **Compile cache.** `App.getCompiledRegexp`
  ([`regex_explorer_highlight.go`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_highlight.go))
  memoises `regexp.Compile` per pattern string, caching failures too — invalid
  patterns are the expected interactive state, not an exception (ADR-0054 §SD3).
  This is already the app's validity authority and drives the red error label.
- **The highlight seam.** `TextEditFluid.HighlightJob(job)` consumes a retained
  `CodeViewJobS`; the Rust layouter
  ([`rust/imzero2/src/imzero2/text_edit_highlight.rs`](../../rust/imzero2/src/imzero2/text_edit_highlight.rs))
  reconciles one-frame-stale sections against the live buffer, normalises, and
  **gap-fills uncovered bytes** — because "a `LayoutJob` that does not cover every
  byte drops glyphs" (ADR-0130 §3). It resolves `TextStyle::Monospace`
  unconditionally.
- **Span→job builders.** [`widgets/codeview`](../../public/thestack/imzero2/egui2/widgets/codeview)
  turns a `[]Span` into a retained `CodeViewJob`, with the `Build*` (always
  re-tokenise) / `Prepare*` (memoised, [ADR-0125](../adr/0125-codeview-prepare-memo.md))
  naming split.
- **Query lanes.** `queryLane[T]`
  ([`regex_explorer_lane.go`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_lane.go))
  is level-triggered: the render thread states which inputs it wants results for
  each frame and the lane converges. `listLane` holds the `extractAll` /
  `extractAllGroups` outcome the hand-off will publish.

---

## 2. Design space — where does the syntactic structure come from?

**Question.** How does the explorer obtain byte-offset-carrying syntactic
structure for an RE2 pattern, per keystroke, on partially-typed input?

**Options.**

- **O1 — Hand-rolled Go lexer** in a new `widgets/regexhighlight`, matching the
  `jsonhighlight` / `gohighlight` / `markdownhighlight` shape.
- **O2 — stdlib `regexp/syntax.Parse`** — the authoritative RE2 parser.
- **O3 — New ANTLR grammar** (`.g4` → generated Go), matching the workflow behind
  `dsl/grammar0|1|2` and `canonicaltypes/grammar`.
- **O4 — `github.com/dlclark/regexp2/syntax`** — already in the module graph as
  an indirect dependency.
- **O5 — Rust-side `regex-syntax` AST** inside the ADR-0130 layouter; the crate
  is already transitively present in the imzero2 Rust tree.
- **O6 — `alecthomas/chroma`** — general-purpose Go highlighter, Pygments-derived.
- **O7 — `timtadh/lexmachine`** — regex-driven DFA lexer generator with positions.

**Criteria.**

- **C1 — Byte offsets.** Does it yield `(start, stop)` per token over the exact
  buffer? Non-negotiable: the `CodeViewJob` contract is byte ranges, and coverage
  must be total (ADR-0130 §3).
- **C2 — Behaviour on invalid input.** A half-typed pattern is the *normal* state
  in this app; the highlighter must degrade, never blank or drop text.
- **C3 — Per-keystroke cost and retained memory.** ADR-0130 budgets the editor
  tier at lex-only cost; [ADR-0084](../adr/0084-nanopass-antlr-dfa-cache-bounding.md)
  documents what happens when a per-input parser accumulates state.
- **C4 — Dialect fidelity to RE2** — the engine this app exists to predict.
- **C5 — Dependency and maintenance cost.** New module, build step, generated
  artefacts.
- **C6 — Fits the established shape.** Three highlighters already land on one
  `Span` → `codeview` seam.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 hand-rolled | O2 `regexp/syntax` | O3 ANTLR | O4 regexp2 | O5 Rust `regex-syntax` | O6 chroma | O7 lexmachine |
|----|----|----|----|----|----|----|----|
| C1 offsets       | ++ | −− | ++ | −− | ++ | −  | +  |
| C2 invalid input | ++ | −− | +  | −− | −  | ++ | +  |
| C3 cost/memory   | ++ | +  | −− | +  | ++ | −  | +  |
| C4 RE2 fidelity  | +  | ++ | +  | −− | −  | −  | +  |
| C5 dependencies  | ++ | ++ | −  | +  | −  | −− | −  |
| C6 fits shape    | ++ | −  | −  | −  | −− | −  | +  |

### Why each non-chosen option fails

**O2 — `regexp/syntax`.** `syntax.Regexp` (`$GOROOT/src/regexp/syntax/regexp.go:18`)
carries `Op, Flags, Sub, Sub0, Rune, Rune0, Min, Max, Cap, Name` — and **no source
positions of any kind**. Offsets are not merely inconvenient to recover, they are
absent; the parser also folds and rewrites (literal merging, `Simplify`), so even a
reconstruction attempt would not map back to the typed bytes. Worse for C2:
`Parse` returns an error on *any* invalid input, so the typical interactive state
produces nothing at all. Fails the non-negotiable criterion twice.

*This does not remove `regexp` from the design* — see the recommendation.

**O3 — a new ANTLR grammar.** Mechanically viable: ANTLR tokens carry start/stop
offsets, error recovery exists, and boxer already owns the whole workflow (four
`.g4` grammars with generated `*.out.go` checked in, `antlr4-go/antlr/v4` a direct
dependency). Two decisive costs:

1. **Unbounded, unflushable memoisation.** ADR-0084 measured the Go runtime's DFA
   cache growing without eviction and with **no `ClearDFA` in the Go port**:
   172,281 retained states / 1.95 GB at 10k adversarial nested inputs, 1,328,006 /
   14.7 GB at 500k. Its finding is that *nested expression structure does not
   saturate* (flat literal/identifier variety does). A regex editor is precisely a
   nested-structure generator, driven per keystroke, by a user deliberately writing
   unusual nesting. This is the documented worst case for the runtime, invoked on
   the hottest path.
2. **No grammar to adopt.** grammars-v4 ships PCRE, a different dialect; there is
   no RE2 `.g4`. So this means authoring *and maintaining* a grammar plus a
   generation step for what is a twelve-token language.

**O4 — `dlclark/regexp2/syntax`.** Present in `go.sum` (v1.11.5) as an indirect
dependency, so "free" on paper. In fact: `regexNode`
(`$GOMODCACHE/github.com/dlclark/regexp2@v1.11.5/syntax/tree.go:55`) holds
`t, children, str, set, ch, m, n, options, next` — **no offsets** — and is
*unexported*; `Parse` returns a `*RegexTree` whose root is unreachable from
outside the package. And it implements the **.NET/PCRE dialect** (backtracking,
backreferences, lookaround), not RE2. A non-starter twice over.

**O5 — Rust-side `regex-syntax`.** Its `ast` module genuinely does carry byte
spans, and the crate is already in the imzero2 Rust dependency tree transitively
via `regex`. But it parses the Rust `regex` crate's dialect, not RE2; it needs a
new opcode and IDL surface; its AST parser errors on invalid input exactly like
O2; and **ADR-0130 §O3 already rejected this exact shape** for SQL, on the grounds
that a second in-tree dialect definition outside the Go authority "is not an option
for this repository". The same rule applies with more force here: ADR-0054 makes
Go's `regexp` the app's declared authority, so the colouring must agree with *it*,
which means lexing on the Go side.

**O6 — chroma.** Not a dependency today. Neither Pygments' nor chroma's catalogue
contains a lexer for *regular-expression syntax as a language* — their catalogues
are programming and markup languages, and chroma's `Rule`/regex machinery is how
lexers are *written*, not a regex-language lexer. So adopting chroma means
**authoring** a lexer in its XML rule DSL rather than getting one for free. Its
`Iterator` yields `Token{Type, Value}` with no byte offsets (recoverable by
accumulating lengths), it has no notion of nesting depth, and it is a large new
dependency. Worst of both worlds: the lexer still has to be written, and a
dependency is added to carry it. *(Caveat: the "no regex-language lexer" claim
comes from the catalogue, not from an in-tree check — confirm with `chroma --list`
before relying on it. It does not change the ranking, since C1/C5 already sink the
option.)*

**O7 — `timtadh/lexmachine`.** A DFA lexer generator whose tokens carry start/end
byte offsets, so C1 is satisfied. But the regex language is context-sensitive in
ways it cannot express declaratively: inside-vs-outside a character class; `(?i)`
(a flag *setting* that consumes its own `)` and does **not** nest) versus `(?i:`
(a group that does); `\Q…\E` literal mode. lexmachine has no flex-style start
conditions, so all of that becomes a hand-written state machine *wrapped around*
the library — the same code as O1, plus a dependency and a generated DFA. The win
over O1 is nil.

### Recommendation — O1, with `regexp` kept as the validator

A hand-rolled lexer (~250 LOC, zero new dependencies) is the only option that
delivers all three things this surface needs together:

1. **Total byte coverage including the invalid tail** — required by the LayoutJob
   contract, and the reason a *parser* (O2, O3, O5) is the wrong tool: parsers
   answer "is this well-formed", and the answer here is usually "not yet".
2. **A per-span nesting depth**, enabling bracket-pair colourisation of group
   parens — the single highest-value affordance for reading a nested regex, and
   something no off-the-shelf option surfaces.
3. **A third instance of an established in-repo shape**, so it introduces no new
   concepts, no new build step, and no new generated artefacts.

The division of labour is the one ADR-0054 already set up, extended by one step:
**Go's `regexp` decides what is true** (validity, matches, offsets — the existing
`compileCache` and the red error label), **the lexer decides only how it is
painted**. The lexer therefore never claims authority it does not have, and a
lexer/compiler disagreement shows up visibly (colour versus the error label)
rather than silently. The honest residual risk — a hand-rolled lexer drifting from
RE2's real grammar — is contained by that split plus a property test pinning the
coverage invariant.

---

## 3. Work item A — regex pattern syntax highlighting → **ADR-0015**

### A1. New package `public/thestack/imzero2/egui2/widgets/regexhighlight/`

Files: `regexhighlight.go`, `regexhighlight_test.go`, `package_props.go`.

Public shape mirrors `jsonhighlight`: `CategoryE`, `Span`, `Highlight(src) []Span`,
plus two additions justified below.

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

Beyond the existing highlighters:

- **`Span.Depth int32`** — group nesting depth, so the palette can cycle group-paren
  colour by depth. This ties visually to the Preview tab's existing per-group
  `QualitativeCycle` colouring in `renderCaptureGroups`.
- **`HighlightLines(src)`** — lexes one independent pattern per line, resetting
  depth at each newline. The multi-pattern editor holds a *list* of regexes, not
  one regex; lexing it as a single pattern would let an unclosed `(` on line 1
  mis-colour line 7.

Correctness points that will bite an implementer who does not know them:

- `(?i)` is a **flag setting**, not a group: it consumes its own `)` and must not
  change nesting depth. `(?i:` is a group and does. Same two-character prefix,
  opposite structural effect.
- Escaped non-ASCII (`\é`) must consume a whole rune via `utf8.DecodeRuneInString`,
  so no span ever splits a UTF-8 sequence.
- An unterminated `[…` or `(…` keeps its content categories rather than turning
  red — those are the normal mid-typing states, and the compile-error label already
  carries the truth. `CategoryError` is reserved for the two byte-level certainties
  in the table.
- A bare `]` or `}` outside its construct is a literal in RE2, and must not be
  mistaken for a delimiter.

**Invariant, asserted in tests** exactly as `jsonhighlight` documents it: spans
cover every byte of the input exactly once, in ascending order, with
`src[Start:Stop] == Text`. Table-driven tests per category, plus a
`pgregory.net/rapid` property test over random byte strings for the coverage
invariant (per `CODINGSTANDARDS.md`, rapid is the repo's property-testing tool).

### A2. `codeview/regex.go`

Mirrors [`codeview/json.go`](../../public/thestack/imzero2/egui2/widgets/codeview/json.go):
palette interned at `init()`, a `highlighterSpec`, and `BuildRegex` /
`PrepareRegex` / `BuildRegexLines`. Add `langRegex` to the `langE` enum in
[`codeview/memo.go`](../../public/thestack/imzero2/egui2/widgets/codeview/memo.go).
Group parens resolve colour from `Span.Depth` through a short depth cycle rather
than one flat colour.

No L2/async tier. The regex lexer is O(n) over a pattern of tens of bytes — orders
of magnitude below the SQL parse (70 ms / 2.5 KB) that forced ADR-0130's two-tier
split. `sqlSemanticHl` has no analogue here and should not be copied.

### A3. Wiring

In `regex_explorer.go` plus a new `regex_explorer_syntax.go`:

- Attach `.HighlightJob(job)` to the `pattern` and `patternList` editors, gated on
  a non-empty buffer, using the same rebuild-on-change cache shape as
  `PlayApp.sqlEditorHighlightJob`
  ([`apps/play/play_renderer.go:1684`](../../apps/play/play_renderer.go)) — two
  small `(src, job, ok)` triples on `App`, render-thread-confined like the rest of
  the input state.
- Give the `pattern` editor `.CodeEditor()`. The Rust layouter resolves
  `TextStyle::Monospace` unconditionally, so without this the field's font would
  change the moment a character is typed.
- Leave the `replacement` editor alone: `\1` backrefs are not RE2 *pattern* syntax,
  and colouring them with the pattern palette would be a lie.

---

## 4. Work item B — extraction hand-off to the playground → **ADR-0017**

New file `regex_explorer_evalplay.go`, following
`gitpulse_evalplay.go` (the proven downstream consumer of ADR-0134 + ADR-0135)
beat for beat.

### B1. Two datasets, published side by side

**`regex_matches`** — from Go's `FindAllStringSubmatchIndex`, the offset authority
per ADR-0054. One row per (match, group); group 0 is the full match:

```
match_idx  Int32   -- 0-based match ordinal
group_idx  Int32   -- 0 = whole match, k = capture group k
group_name String  -- (?P<name>…) name, or ''
text       String  -- the matched substring
start_byte Int32   -- -1 when the group did not participate
stop_byte  Int32
matched    UInt8   -- 1 when the group participated
```

**`regex_ch_extract`** — whatever `listLane` currently holds, keyed the same way so
the two join:

```
match_idx  Int32   -- ordinal within extractAll / extractAllGroups
group_idx  Int32   -- 0 = the extractAll element; k>=1 = extractAllGroups[match][k]
text       String
```

Both encoded with `ipc.NewWriter` — Arrow IPC **stream** form, no file footer,
which is what ADR-0134 expects (note the contrast with the `chlocalbroker`
`InputTables` path, which takes the *file* form) — and published via
`adhocdata.PublishRequest`, reusing the prior handle on republish so one window
holds at most two datasets against the `MaxDatasets = 64` cap.

If the CH lane holds nothing fresh (bus unwired, query in flight, pattern invalid)
only `regex_matches` is published and the seeded SQL degrades to a plain `SELECT`
over it — a partial result, clearly labelled, not a failed click.

### B2. Seeded SQL

```sql
-- regex explorer: one extraction, both engines.
--   go_* — Go regexp (RE2), the byte-offset authority (ADR-0054)
--   ch_* — ClickHouse extractAll/extractAllGroups, the engine being predicted
-- A NULL on either side is a disagreement worth looking at. Note that
-- extractAll returns capture group 1 — not the full match — whenever the
-- pattern captures, so group_idx 0 lines up only for group-less patterns.
SELECT coalesce(g.match_idx, c.match_idx) AS match_idx,
       coalesce(g.group_idx, c.group_idx) AS group_idx,
       g.group_name,
       g.text AS go_text,
       c.text AS ch_text,
       g.start_byte, g.stop_byte
FROM keelson('<goHandle>') AS g
FULL OUTER JOIN keelson('<chHandle>') AS c
  ON g.match_idx = c.match_idx AND g.group_idx = c.group_idx
ORDER BY match_idx, group_idx
SETTINGS join_use_nulls = 1
```

`join_use_nulls = 1` is load-bearing: without it a missing side comes back as `''`
rather than `NULL`, which reads as agreement on an empty string — the exact failure
this surface exists to expose.

Two `keelson('…')` calls in one statement are already proven — see
`introspectengine/encrypted_test.go`, which queries an ad-hoc handle and `env` in a
single statement — and the expansion is a nanopass rewrite
([`keelsonsql`](../../public/keelson/runtime/introspect/keelsonsql/keelsonsql.go)),
so nothing new is required of the engine.

This turns ADR-0054's Go-versus-ClickHouse duality from a caveat in a UI label
into something the user can `JOIN` — an SD1 tripwire in the large, over their own
pattern rather than a fixed corpus.

### B3. Launch and lifecycle

- `launchcfg.PlayLaunch{Sql: …, AutoRun: true, Endpoint: launchcfg.EndpointIntrospection}`
  → `buscodec.Encode` → `windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfg)`.
- **`AppId` moves from `apps/play/app_register.go:155` into the leaf package
  `apps/play/launchcfg`**, with `play.AppId` retained as `const AppId = launchcfg.AppId`.
  Without this, `regex_explorer` → `apps/play` would drag the whole play app — and
  its registry-registering `init()` — into every host that links the `regexsummary`
  widget, silently registering the SQL playground as a side effect of using an
  inspector. Verified: `apps/play` does not depend on `regex_explorer`
  (`go list -deps`), and `keelson/runtime/app` does not depend on `launchcfg`, so
  no cycle. The one existing external `play.AppId` call site keeps compiling
  untouched.
- Manifest gains three `Pub` caps: `adhocdata.SubjectPublish`,
  `adhocdata.SubjectRetract`, `windowhost.OpenSubject`.
- `AppInstance.Unmount` retracts both handles alongside the existing
  `cancelQueries()`.

**Known gap to record in the ADR.** `EmbeddedApp` has no teardown hook —
`regexsummary` keeps embedded explorers alive for the widget's lifetime — so an
embedded explorer that published would hold its handles until process exit. Add
`EmbeddedApp.Close()` for hosts that can call it and state the gap plainly; it is
bounded by handle reuse (at most two per instance) and the ephemeral store. In
practice the embedded case degrades earlier and more visibly: the host's bus client
carries the *host app's* manifest caps, which will not include `adhoc.publish`, so
the publish is refused with a clear status line rather than silently doing nothing.

### B4. UI

A single affordance row below the tab `DockArea` in `renderBody` — neutral ground,
since the Go half comes from the Preview tab and the CH half from the List tab. A
click snapshots both result sets **on the render thread** and hands them to a
goroutine (the imzero2 rule: workers never call `c.*`); status folds back under the
existing `mu`, which already covers the "written by workers, read by the render
thread" field group. A re-click while in flight is dropped.

---

## 5. The two ADRs

- **ADR-0015 — Regex pattern syntax highlighting.** Records §2's decision (O1 with
  `regexp` as validator) and its rejected alternatives with the evidence above;
  plus depth-cycled group colour, per-line lexing for the multi-pattern editor, the
  no-L2-tier call, and the coverage invariant. Cross-links ADR-0054, ADR-0125,
  ADR-0130 and this survey.
- **ADR-0017 — Regex explorer extraction hand-off.** Records publishing *both*
  engines rather than one, the join key and `join_use_nulls`, the degraded
  single-dataset path, the `AppId` → `launchcfg` move and why, and the embedded
  retract gap. Cross-links ADR-0054, ADR-0134, ADR-0135.

### Numbering — reuse the gaps, densely

The register runs 0001–0140 with 133 files and seven holes. Rather than extending
the range, new ADRs claim the **lowest free** hole, so the numbering stays dense.
Audited 2026-07-25 (`grep -rIl 'ADR-NNNN|adr/NNNN-'` across this repo and its
downstream consumer, plus `git log --diff-filter=DR -- doc/adr/`):

| Slot | History | Status |
| --- | --- | --- |
| **0015** | never issued in this repo | **free — claimed by work item A** |
| **0017** | never issued, never cited anywhere | **free — claimed by work item B** |
| 0055 | held `0055-adopt-boxer-standards.md`, deleted a906c430 (2026-06-21) | free — next in line |
| 0110 | held `0110-leeway-marshall-carrier-memberships-in-tuples.md`, deleted 634e62f1 | **retired — do not reuse** |
| 0136 | held `0136-play-query-dispatch-resolver.md`, deleted ccaf5024 (2026-07-21) | free |
| 0137 | held `0137-query-placement-clusters-balancing.md`, same commit | free |
| 0138 | held `0138-streaming-query-transport-observability.md`, same commit | free |

Two caveats an implementer must carry into the new files:

- **0110 is not a gap, it is a decision.** [ADR-0113 §D4](../adr/0113-leeway-marshall-nested-primary-consolidation.md)
  states: *"ADR-0110 is folded into D5 and its file deleted; the number stays
  retired."* ADR-0113 and `doc/changelog/2026-07-02--2026-07-16.md` both still cite
  it by number. Reusing 0110 would repoint two live citations at an unrelated
  document — strictly worse than the current dangling reference.
- **0015 needs one disambiguating sentence.** [ADR-0005](../adr/0005-streaming-persisted-kafka-from-connect.md)
  records that it was "originally drafted in a downstream consumer … as its
  ADR-0015" and migrated here as ADR-0005. That is the *consumer's* numbering, not
  this repo's — this repo never had an ADR-0015 — but a reader meeting both
  documents will wonder. The new ADR-0015 should say so in one line. (Reusing a
  vacated slot instead, e.g. 0055, would trade this for a different confusion:
  the consumer's own ADR-0005 narrates "boxer has no ADR-0055".)

Prior art for reuse: 0008, 0120, 0124 and 0126 were all vacated and re-issued, so
this is the register's established practice, not a new one.

---

## 6. Files

**New**

- `public/thestack/imzero2/egui2/widgets/regexhighlight/{regexhighlight,regexhighlight_test,package_props}.go`
- `public/thestack/imzero2/egui2/widgets/codeview/regex.go`
- `public/thestack/imzero2/egui2/demo/apps/regex_explorer/{regex_explorer_syntax,regex_explorer_evalplay}.go` (+ tests)
- `doc/adr/0015-regex-pattern-syntax-highlighting.md`
- `doc/adr/0017-regex-explorer-extraction-handoff.md`

**Modified**

- `public/thestack/imzero2/egui2/widgets/codeview/memo.go` — `langRegex`
- `regex_explorer/regex_explorer.go` — highlight jobs on two editors, affordance row
- `regex_explorer/app_register.go` — three new caps
- `regex_explorer/embedded.go` — `Close()`
- `apps/play/launchcfg/launchcfg.go` — `AppId`
- `apps/play/app_register.go` — `AppId` re-exported from `launchcfg`

All of it is in this repository; no downstream consumer needs a change.

---

## 7. Verification

1. `go build -tags "$(cat ./tags)" ./...` and
   `go test -tags "$(cat ./tags)" ./public/thestack/imzero2/egui2/widgets/regexhighlight/... ./public/thestack/imzero2/egui2/widgets/codeview/... ./public/thestack/imzero2/egui2/demo/apps/regex_explorer/... ./apps/play/...`
2. New unit tests: lexer coverage invariant (table + rapid property); per-category
   classification; `(?i)` versus `(?i:` depth behaviour; per-line independence in
   `HighlightLines`; Arrow record shape for both datasets; seeded-SQL contents;
   publish/open against a fake bus (the downstream `fakeEvalBus` pattern — route
   `Request` by subject, answer `adhoc.publish` and `windowhost.open` — is the
   template).
3. Regression: the existing `regex_explorer` suite (781 lines) must stay green.
   Demo scenes are already flagged `NonDeterministic`, so the new affordance row
   does not disturb golden screenshots.
4. Live check in the demo carousel host, which already wires `chlocalbroker`,
   `adhocdata` and the introspection endpoint and registers `play`
   ([`imzero2_demo_cli.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_cli.go)):
   type a nested pattern and confirm depth-cycled parens plus class colouring;
   click the affordance and confirm a play window opens and auto-runs the join.
5. `go mod tidy --diff` must be clean — O1 adds no dependency, and that is a
   claim worth checking mechanically.

---

## 8. Sources

- In-repo, read in full:
  [`demo/apps/regex_explorer`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer)
  (`regex_explorer.go`, `_highlight.go`, `_job.go`, `_lane.go`, `_chlocal.go`,
  `embedded.go`, `app_register.go`, `_tour.go`),
  [`widgets/codeview`](../../public/thestack/imzero2/egui2/widgets/codeview)
  (`codeview.go`, `sql.go`, `json.go`, `memo.go`, `doc.go`),
  [`widgets/jsonhighlight`](../../public/thestack/imzero2/egui2/widgets/jsonhighlight),
  [`widgets/gohighlight`](../../public/thestack/imzero2/egui2/widgets/gohighlight),
  [`nanopass/highlight`](../../public/db/clickhouse/dsl/nanopass/highlight),
  [`widgets/sqleditor`](../../public/thestack/imzero2/egui2/widgets/sqleditor)
  (the L1/L2 highlight tiers, extracted from `apps/play` by ADR-0147 §SD1),
  [`apps/play/play_renderer.go`](../../apps/play/play_renderer.go),
  [`apps/play/launchcfg`](../../apps/play/launchcfg),
  [`keelson/runtime/adhocdata`](../../public/keelson/runtime/adhocdata),
  [`keelson/runtime/windowhost/openclient.go`](../../public/keelson/runtime/windowhost/openclient.go),
  [`rust/imzero2/src/imzero2/text_edit_highlight.rs`](../../rust/imzero2/src/imzero2/text_edit_highlight.rs),
  `rust/imzero2/src/imzero2/interpreter.rs` (TextEdit apply block).
- ADRs: [0054](../adr/0054-regex-explorer-offset-authority.md),
  [0084](../adr/0084-nanopass-antlr-dfa-cache-bounding.md),
  [0125](../adr/0125-codeview-prepare-memo.md),
  [0130](../adr/0130-imzero2-textedit-highlight-seam.md),
  [0134](../adr/0134-adhoc-datasets.md),
  [0135](../adr/0135-app-launch-requests.md);
  and the prior survey [`sql-editor-highlighting-survey.md`](./sql-editor-highlighting-survey.md)
  whose shape this document follows.
- External libraries: `regexp/syntax` (`$GOROOT`, struct definition read directly);
  [`dlclark/regexp2`](https://github.com/dlclark/regexp2) v1.11.5 (module cache,
  `syntax/tree.go` and `syntax/parser.go` read directly);
  [`alecthomas/chroma`](https://github.com/alecthomas/chroma);
  [`timtadh/lexmachine`](https://github.com/timtadh/lexmachine);
  [`antlr4-go/antlr/v4`](https://github.com/antlr4-go/antlr) v4.13.1;
  the Rust [`regex-syntax`](https://docs.rs/regex-syntax) crate.
- Specification: [RE2 syntax](https://github.com/google/re2/wiki/Syntax),
  [Go `regexp`](https://pkg.go.dev/regexp),
  [ClickHouse string-search functions](https://clickhouse.com/docs/en/sql-reference/functions/string-search-functions).
