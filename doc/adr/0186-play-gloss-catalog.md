---
type: adr
status: proposed
date: 2026-08-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0186: `play` glosses — a catalog of named value renderings for the Table and Detail panes

## Context

[ADR-0123](./0123-play-content-typed-detail-cells.md) renders a column named
`` `<label>@<mime>` `` as one of eight media types, in the ad-hoc Detail pane
only. It deferred the Table tab and the leeway card (§SD7) and asked whether
result column names were the right namespace for a further convention.

The request: generalise that to values that are not documents — a temperature,
a height, a card number with a check digit. Keep `@` as the trigger, catalogue
the renderings, reach them by an explicit `@name` **and** by mapping leeway
columns to renderings with a regex over a written-out form of the column name
and its aspects, and render them in the Table as well as in Detail.

What constrains the design:

- A Table cell is one line of monospace text from `formatCell`, drawn per
  visible cell per frame, with column widths seeded from the same function;
  there is no per-cell hook.
- The leeway Detail card (`Table2CardEmitter`) receives values as text, and
  `BeginColumn` already applies a display rule keyed on value semantics
  (machine-readable and not human-readable → hidden).
- The aspect vocabularies (ADR-0182) carry `sem:secret`, `sem:url`,
  `sem:json*` — matchable — but by their admission criterion no units and no
  media typing; "this float is kelvin" belongs in a catalog, not an aspect.
- ADR-0181 §SD2 spells aspects as prefixed tokens (`item:` `enc:` `sem:`
  `use:`); `lwsql` composes names from them (§SD6) but does not decompose.
- An explicit `@` needs an alias, and an alias strips a column's leeway-ness —
  the result falls off the card path. A leeway column can only be reached by a
  rule keyed on its physical name.
- ADR-0124's `-- play: ungroup` / `-- play: enum` are the in-band directive
  family; ADR-0181's `LwConstructExpand` in the passreg standard set
  (ADR-0108) is the client-only constructor-macro shape.

## Design space (QOC)

**Q1 — How is a rendering named after the `@`?**

- *A bare token, `t@celsius`.* Killed: ADR-0123 §SD2 keeps every slash-less
  token silent (`dot_done@success`, email-like names); making them
  declarations needs a `dot_` exemption or a near-miss heuristic to stay loud
  on typos.
- *A private top-level media type, `t@gloss/temperature;unit=K`.* Chosen —
  SD2. The 0123 gate stands verbatim and parameters come from the same parser.
- *A second sigil (`@@`, `#`).* Killed: `#` opens a ClickHouse comment
  (ADR-0122), and any new sigil is one more convention on the namespace.

**Q2 — Where does a column's binding to a gloss come from?**

- *The explicit alias only.* Killed: it cannot reach a leeway column.
- *Arrow field metadata.* Killed as in ADR-0123: ClickHouse writes none and a
  SQL author cannot.
- *A `keelson` registry keyed by `table.column`.* Killed for v0 as in
  ADR-0123: `concat(a, b)` has no key. Noted as the home for rules that should
  outlive a query.
- *Three tiers — explicit alias, in-band directive, gloss affinity — matched
  against a written-out column.* Chosen — SD3.

**Q3 — What does a rule match against?**

- *The physical name.* Killed: aspects are base62 digit lists in it.
- *The ADR-0181 constructor call text.* Killed: quotes and commas are a hostile
  regex subject, and non-leeway columns have no constructor.
- *A one-line token spelling in ADR-0181's vocabulary.* Chosen — SD3: a rule
  matches what an author would type to mint the column.

## Decision

`play` gains a catalog of **glosses**, each a named way of showing a value. A
gloss reaches a column by an explicit `` `label@<media type>` `` alias, by a
`-- play: gloss` directive rule, or by the gloss's own affinity for aspects.
ADR-0123's media types become the content family of the catalog. Glosses
render in both Table grids and both Detail paths.

### SD1 — A gloss has two faces and lives in two layers

- **Inline face** — one line, monospace-safe, cheap enough for every visible
  cell every frame: `Inline{Text, Tone}`, Tone from the ADR-0031 semantic
  palette. `4111 •••• •••• 1111` in *success* or *error* by check digit;
  `21.5 °C`; `1.83 m`; `••••••`; `[image/png · 359 B]`.
- **Block face** — optional, Detail only: markdown, code view, image,
  hyperlink. Without one, Detail shows the inline face wrapped.
- **`Accepts(kind)`** over a small value-kind enum (numeric / text / bytes /
  temporal / bool / other), derivable from an Arrow type or a leeway canonical
  type, so the Arrow-backed grids and the text-backed card ask the same
  question. A mismatch is loud in the ADR-0123 style, never a coercion.
- **Parameters** are declared by the gloss and bound once per column
  (`Bind(params) → Instance`), not per cell. An **affinity** is a default rule
  the gloss brings along (SD3).
- **Two layers.** Catalog, kinds, cell accessor, rules engine and every inline
  face are pure Go in `public/hmi/gloss`, no imzero2, so they test without a
  GUI and the SD7 macro can validate against them. Block faces are imzero2
  code, bound to a gloss by name in `play`, where ADR-0123's renderers already
  live.

### SD2 — Naming: `label@<media type>`, the 0123 gate kept, `gloss/` private

The token after `@` is a media type (RFC 2045 §5.1), parsed by
`mime.ParseMediaType`. ADR-0123 §SD2's table is unchanged; "known type" now
means "in the catalog".

| declared type | family | example |
| --- | --- | --- |
| a registered IANA type | content | `` `notes@text/markdown` `` |
| `gloss/<name>[;k=v…]` | presentation | `` `t@gloss/temperature;unit=K` `` |
| any other type with a slash | — | plain, reason shown (0123 §SD2) |

`gloss` is play's private top-level type, never on any wire: a display
directive in media-type syntax, not the type of any bytes. Not `x-gloss/`
(RFC 6838 §3.4 discourages it; it buys nothing), not `application/vnd.…`
(unreadable at the point of use).

**Parameters are validated.** A gloss declares its parameter names; an
undeclared one is as loud as an unknown type, so `;unti=K` cannot render as
°C. The content family declares `charset` (accepted, ignored) on the text types
and reserves `encoding` on the image types (0123's `;encoding=base64`, still
deferred). `gloss/raw` — the identity face — is a catalog member, so a
directive can override an affinity without a reserved word.

Measured for this ADR: `` AS `t@gloss/temperature;unit=K` `` parses in
`clickhouse-local`, grammar1 and play's quote-aware statement splitter, with or
without a space after `;`; unquoted it fails at the `@` (ADR-0122's finding).
`ParseMediaType` folds the type and parameter names and keeps parameter values
case-sensitive — `unit=K` needs that.

### SD3 — The rule route: spec line, rules, precedence, directive

**Spec line** — a one-line token spelling of a result column, computed once
per schema and cached like `colLabels`; built by a new `lwsql.SpecLine`, the
read-direction dual of `Composer` (0181 §SD6). Prefixes are ADR-0181's where
one exists; aspects are one token each, in enum order, by `String()`;
backbone columns carry `item:` and no `section:`/`use:`; a non-leeway column
carries `name:` and `arrow:` only.

```text
name:temperature section:sensor role:val ct:f64 arrow:float64 enc:… sem:measured sem:scale-of-measurement-metric-ratio use:…
name:ts item:ts ct:z64 arrow:timestamp[ms, tz=UTC] sem:transaction-time
name:temp_c arrow:float64
```

**Rule** — a Go RE2 regex, unanchored, case-sensitive, over the spec line, plus
a media type. Rules are ordered; the first match wins.

**Precedence per column** — explicit alias › directive rules in buffer order ›
affinities in catalog order › none. An aliased column is never offered to the
rules. The header hover names the winner, its source and the spec line; the
Glosses tab (SD6) lists shadowed matches.

**Directive** — joins the ADR-0124 family. `<token>` is the first
whitespace-delimited word in compact `;k=v` form; the regex is the rest of the
line. Unknown type, undeclared parameter, empty or invalid regex each surface
as a note, as `-- play: enum` errors do; the buffer still runs.

```sql
-- play: gloss gloss/temperature;unit=K name:.*temp\b
-- play: gloss gloss/raw sem:secret
```

**Affinities in v0**, narrow: `gloss/secret` ← `\bsem:secret\b`, `gloss/url`
← `\bsem:url\b`, `application/json` ← `\bsem:json(-scalar|-array|-object)?\b`.
Units have no aspect and hence no affinity; that is what the directive is for.

### SD4 — Where glosses render

| surface | face | mechanism |
| --- | --- | --- |
| Table, per-row grid | inline | cell text and width seed come from the bound instance instead of `formatCell`; Tone colours the run |
| Table, per-attribute grid | inline | applied where the sink builds each cell string, on the inner array |
| Detail, ad-hoc | block, else inline | ADR-0123's `renderRichCell` generalised; its `(executed, row)` cache keeps its shape |
| Detail, leeway card | inline | one setter on `Table2CardEmitter`: a per-column glosser consulted in `BeginColumn` beside the hide rule, applied to the cell text in `EndColumn` |
| column header | — | label + small gloss name; physical name, spec line and rule on hover |

A **raw** toggle on the Table options bar bypasses every gloss for the session.
Sorting is untouched: it permutes rows on the raw values.

### SD5 — The v0 catalog

| media type | accepts | params | inline face | block face | affinity |
| --- | --- | --- | --- | --- | --- |
| `text/markdown`, `text/plain`, `application/json`, `application/sql`, `text/x-go` | text, bytes | `charset` | first line | as ADR-0123 | json ← `sem:json*` |
| `image/png`, `image/jpeg`, `image/gif` | bytes | (`encoding` reserved) | `[<type> · <size>]`, no decode | as ADR-0123 | — |
| `gloss/temperature` | numeric | `unit` ∈ K, C, F — the stored unit | `21.5 °C` | — | — |
| `gloss/length` | numeric | `unit` ∈ m, cm, mm, km, ft — the stored unit | auto-scaled SI, `1.234 km` | — | — |
| `gloss/bytes` | numeric ≥ 0 | — | `humanize.IBytes` | — | — |
| `gloss/luhn` | text, numeric | — | groups of four, middle groups masked, ✓/✗ tone by check digit | mask + verdict | — |
| `gloss/secret` | any | — | `••••••`, never length-revealing | same | ← `sem:secret` |
| `gloss/url` | text | — | text in accent tone | `HyperlinkTo` | ← `sem:url` |
| `gloss/raw` | any | — | `formatCell` | — | — |

One or two exemplars per archetype — unit formatting, check digit, masking,
linking — is v0; more are one-file additions. Quantity glosses are spelled
after `public/science/units` (`temperature`, `length`, `mass`, …); unit
conversion (`;show=F`) is deferred, v0 formats the stored unit.

### SD6 — The Glosses tab

A result-side sibling of the Vocabulary tab (ADR-0174): each gloss with its
kinds, parameters, a sample rendering and affinities; the buffer's effective
rules; and each column of the current result with its spec line and resolution
(gloss, source, or why a declaration was refused). Insert-at-caret writes a
`-- play: gloss` line or a `gloss(…)` call.

### SD7 — `gloss(…)`, a constructor macro

A client-side nanopass macro in the ADR-0181 shape, registered in the passreg
standard set beside `LwConstructExpand`; never installed server-side.

```sql
SELECT gloss(reading, 'gloss/temperature', 'unit', 'K'),
       gloss(a + b,  'gloss/length', 'unit', 'm', 'label', 'span')
-- ⇒  reading AS `reading@gloss/temperature;unit=K`,  a + b AS `span@gloss/length;unit=m`
```

- Arguments: the expression, the media type as a string literal, then
  `key, value` pairs. Values are ClickHouse literals of any type the nanopass
  `marshalling` package reads, marshalled to text and quoted by
  `mime.FormatMediaType`. Non-literal arguments are rejected with the call's
  source range.
- `label` is the one reserved key. Without it, a bare identifier contributes
  its name; any other expression contributes its source text, as ClickHouse
  would.
- Legal only as a whole projection item (0181 §SD2's rule).
- The media type and its parameters are validated at rewrite time against the
  catalog (late-bound Factory, ADR-0108 §SD7): a Diagnostics error with a
  source range instead of a per-cell note. Listed in the Vocabulary tab.

### SD8 — Deferred

- Block faces on the leeway card; reveal-on-click for `gloss/secret`.
- A paint variant of the inline face (arrays as sparklines).
- Rule files / env, and an in-app rule editor over ADR-0185 — text rules
  first, because SQL travels and UI state does not.
- Unit conversion; `iban`, `isbn`, `epoch`, `duration`, `percent`, colour
  swatches; ADR-0123 §SD7's own list (base64, URL sources, webp/avif/svg).
- Reading kanban's `dot_*@<tone>` tokens as glosses — it would move
  ADR-0122's contract; not taken.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/hmi/gloss` (new exported Go API) | catalog, `Gloss`/`Instance`, value kinds, cell accessor, rules engine, built-in inline faces | play's block-face bindings; the SD7 pass; tests moved from `play_detail_rich_test.go` |
| `public/hmi/gloss/glosssql` + passreg standard set | new pass entry `gloss(…)` | `defaults.RegisterStandard`; the Vocabulary tab's Client list |
| `lwsql` (exported Go API under `public/`) | +`SpecLine` | golden over the leeway fixture names |
| `leewaywidgets.Table2CardEmitter` (exported Go API under `public/`) | +1 setter for a per-column glosser | play's card driver wiring |
| result-column convention (ADR-0123 §SD2/§SD3) | "known type" = catalog; `gloss/` family; parameters validated | 0123's status and §SD7; `features.md` §Table/§Detail; `snippets.md` |
| `-- play:` directive family (ADR-0124) | +`gloss` | `features.md` cross-reference |
| play tab roster | +Glosses | Panes menu; tab marks |

## Alternatives

- **Sniffing, per-type prefixes, Arrow metadata.** Rejected in ADR-0123; the
  reasons stand.
- **Peeling `@…` off a physical name before leeway classification.** Rejected:
  a play convention inside leeway's name parser, or a schema rewrite at every
  classifier site; the rule route needs neither.
- **An `LW_`-prefixed macro.** Rejected: `LW_` is leeway's namespace; play's
  client macros are lower-case (`keelson`, `docsearch`).
- **A per-column overrides UI first.** Rejected on sequencing: a rule must
  exist as text before it exists as a widget.
- **One gloss per unit.** Rejected once parameters were free: one family per
  quantity, the unit a parameter.

## Consequences

### Positive

- One catalog answers "how is this column shown" for content and non-content
  values, and reaches leeway data without an alias.
- ADR-0123's gate and separator survive unchanged; the new convention is a
  rule set, not another name shape.
- Inline faces test in plain Go; other hosts share the catalog and the macro.

### Negative

- Six more characters per explicit alias.
- Per-cell work in the Table grid — bounded to the visible range and to inline
  faces, but a Luhn check per visible cell per frame is more than a `strconv`.
- One more registry to keep documented; SD6 exists to keep it honest.

### Neutral

- Two spellings under one syntax — IANA types for content, `gloss/` for
  presentation — that the catalog treats identically.
- `Table2CardEmitter` grows a display hook its own demo does not need.

## Migration — Tier 1

- **Breaks.** Nothing at rest or in SQL: every ADR-0123 declaration parses to
  the same gloss. In Go, `richKindE` / `parseRichColumn` / `richCellCache` fold
  into the catalog and its cache; only `play` calls them.
- **Path.** Additive. Hosts embedding `PlayApp` (sqlapplet) inherit the default
  catalog; a host that wants more registers glosses at wiring time.
- **Regeneration.** None — no IDL, codegen or FFI change.
- **Old shape.** ADR-0123's names and semantics are kept as the content family;
  its file is edited in place (still proposed) to point here.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the catalog, the parse table, every inline
  face, the rules engine and the spec line; a nanopass golden for `gloss(…)`;
  `clickhouse-local` for the alias measurement; a headless play scene for the
  Table + Detail rendering, per the play screenshot recipe.
- **What would fail.**
  - The ADR-0123 parse cases, moved verbatim (`dot_done@success` silent,
    `notes@text/markdwn` loud, `TEXT/Markdown` folded), plus `;unit=K`
    accepted and `;unti=K` / `unit=k` refused.
  - One golden per inline face — Luhn pass and fail with their tones; a
    `gloss/secret` face of equal length for inputs of different lengths.
  - A spec-line golden over the leeway fixture names.
  - Precedence: alias › directive › affinity; a `gloss/raw` directive
    suppresses `gloss/secret`'s affinity.
  - The `gloss(…)` golden: label rules, typed parameters, the position rule, a
    non-literal argument rejected with a source range.
- **Gap.** The card hook's look and the hover text are checked by hand at the
  milestone; the per-cell cost of inline faces on a wide visible range is not
  benchmarked — the raw toggle is the mitigation.

## Milestones

- **M0 — core.** `public/hmi/gloss`: catalog, kinds, cell accessor, params,
  the content family's inline faces; ADR-0123's parser and tests migrated,
  behaviour identical.
- **M1 — Table.** Inline faces in both grids, width seed, tone runs, header
  label + hover, raw toggle; the seven `gloss/*` members of SD5.
- **M2 — rules.** `lwsql.SpecLine`, rules engine, affinities, the directive,
  precedence and hover provenance.
- **M3 — Detail.** Block faces bound in play; the card seam; the Glosses tab.
- **M4 — macro.** `glosssql`, passreg entry, Vocabulary listing, goldens.
- **M5 — docs.** `features.md`, `snippets.md`, the ADR-0123 pointers.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0123](./0123-play-content-typed-detail-cells.md) — content-typed detail cells; the gate this ADR keeps.
- [ADR-0122](./0122-play-kanban-panel.md) §SD2 — the measured `@` separator; `dot_*@<token>`.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md), [ADR-0121](./0121-selection-condition-columns.md) — the other conventions on result column names.
- [ADR-0124](./0124-play-param-editing-widgets.md) — the `-- play:` directive family.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) §SD2/§SD6 — spec tokens, constructor family, `Composer`.
- [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) — aspect vocabularies and their admission criterion.
- [ADR-0108](./0108-keelson-sql-pass-registry.md) — passreg and the late-bound Factory.
- [ADR-0174](./0174-play-sql-vocabulary-panel.md) — the Vocabulary tab the Glosses tab mirrors.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the semantic palette behind `Tone`.
- RFC 2045 §5.1 (parameter syntax), RFC 6838 §3.4 (`x-` discouraged); Go `mime.ParseMediaType` / `FormatMediaType`.
