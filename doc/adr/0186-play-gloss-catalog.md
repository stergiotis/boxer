---
type: adr
status: accepted
date: 2026-08-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-15
---

# ADR-0186: `play` glosses — a catalog of named value renderings for the Table and Detail panes

**In one paragraph:** a *gloss* is a named way of showing a value.
`gloss/duration;unit=ms` shows `65000` as `1m 05s`; `gloss/bytes` shows `40858`
as `40 KiB`; `gloss/luhn` groups a card number into fours, masks the middle
groups and ticks its check digit (`4111 •••• •••• 1111 ✓`); `gloss/taggedid`
splits a 19-digit identifier into its tag and counter (`12393906174523605050`
reads as `c:3a`). A column reaches a gloss by an explicit
`` `label@<media type>` `` alias, by a rule matched against a written-out
spelling of the column — a `-- play: gloss` line in the buffer, or a standing
rule set in Go — or by a default the gloss brings along (an *affinity*).
Glosses render in both Table grids and both Detail paths.

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

Six constraints shape the design:

- **A Table cell is one line of monospace text** from `formatCell`, drawn per
  visible cell per frame, with column widths seeded from the same function;
  there is no per-cell hook.
- **The leeway Detail card** (`Table2CardEmitter`) receives values as text, and
  `BeginColumn` already applies a display rule keyed on value semantics
  (machine-readable and not human-readable → hidden).
- **Aspects carry semantics but no units.** The aspect vocabularies (ADR-0182)
  carry `sem:secret`, `sem:url`, `sem:json*` — matchable — but by their
  admission criterion no units and no media typing; "this float is kelvin"
  belongs in a catalog, not an aspect.
- **`lwsql` composes names but does not decompose them.** ADR-0181 §SD2 spells
  aspects as prefixed tokens (`item:` `enc:` `sem:` `use:`); `lwsql` composes
  names from them (§SD6) but has no read side.
- **An alias strips a column's leeway-ness.** An explicit `@` needs an alias,
  and the aliased result falls off the card path. A leeway column can only be
  reached by a rule keyed on its physical name.
- **The conventions already exist.** ADR-0124's `-- play: ungroup` /
  `-- play: enum` are the in-band directive family; ADR-0181's
  `LwConstructExpand` in the passreg standard set (ADR-0108) is the
  client-only constructor-macro shape.

**Terms.** *gloss* — a named way of showing a value, a member of the catalog.
*content family* — ADR-0123's media types, now one family of the same catalog.
*presentation gloss* — a `gloss/…` type for non-document values. *inline face*
— the one line a cell shows; *block face* — a larger, Detail-only rendering.
*affinity* — a default rule a gloss brings (e.g. `sem:secret` →
`gloss/masked`). *spec line* — a one-line token spelling of a column, what
rules match against. *card path* — the leeway Detail card, which receives
values as text. *passreg* — the SQL pass registry (ADR-0108), where the
client-side `gloss(…)` macro registers.

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
  `21.5 °C`; `1.83 m`; `2026-08-15T12:00:00Z`; `1m 05s`; `••••••`;
  `[image/png · 359 B]`.
- **Block face** — optional, Detail only: markdown, code view, image,
  hyperlink. Without one, Detail shows the inline face wrapped.
- **`Accepts(kind)`** over a small value-kind enum (numeric / text / bytes /
  temporal / bool / other), derivable from an Arrow type or a leeway canonical
  type, so the Arrow-backed grids and the text-backed card ask the same
  question. A mismatch is loud in the ADR-0123 style, never a coercion —
  which also retires 0123's "odd but total" `SELECT 42 AS x@text/markdown`:
  the content family accepts text and bytes and refuses a number with the
  reason.
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

### SD3 — The rule route: spec line, rules, directive, affinities, precedence

An alias only reaches columns you can name. To reach a leeway column — and to
let one rule serve many columns — play computes a **spec line** for each result
column and matches rules against it.

**Spec line** — a one-line token spelling of a result column, computed once
per (schema, directive set) and cached like `colLabels`; the leeway tokens
come from a new `lwsql.SpecLines`, the read-direction dual of `Composer`
(0181 §SD6), and the host appends `arrow:<type>` last. Prefixes are
ADR-0181's where one exists; aspects are one token each, in enum order, by
`String()`; backbone columns carry `item:` and no `section:`/`use:`; a
non-leeway column carries `name:` and `arrow:` only.

```text
name:temperature section:sensor role:val ct:f64 enc:… sem:measured sem:scale-of-measurement-metric-ratio use:… arrow:list<item: float64, nullable>
name:ts item:ts ct:z64 sem:transaction-time arrow:timestamp[ms, tz=UTC]
name:temp_c arrow:float64
```

**Rule** — a Go RE2 regex, unanchored, case-sensitive, over the spec line, plus
a media type. Rules are ordered; the first match wins.

**Directive** — joins the ADR-0124 family. `<token>` is the first
whitespace-delimited word in compact `;k=v` form; the regex is the rest of the
line. Unknown type, undeclared parameter, empty or invalid regex each surface
as a note, as `-- play: enum` errors do; the buffer still runs.

```sql
-- play: gloss gloss/temperature;unit=K name:.*temp\b
-- play: gloss gloss/raw sem:secret
```

**Affinities in v0**, narrow: `gloss/masked` ← `\bsem:secret\b`, `gloss/url`
← `\bsem:url\b`, `application/json` ← `\bsem:json(-scalar|-array|-object)?\b`.
Units have no aspect and hence no affinity; that is what the directive is for.

**Precedence per column**, strongest first:

1. an **explicit alias** — an aliased column is never offered to the rules;
2. **`-- play: gloss` directives**, in buffer order;
3. **standing rule sets** from the injected repository, in registration order
   (added by the 2026-08-15 Update on standing rules below, which replaced this
   ADR's original plan to leave them to files or an editor);
4. **affinities**, in catalog order;
5. otherwise, **no gloss**.

The buffer sits above code so a query's `gloss/raw` can switch a standing rule
off for itself. The header hover names the winner, its source and the spec
line; the Glosses tab (SD6) lists shadowed matches.

### SD4 — Where glosses render

| surface | face | mechanism |
| --- | --- | --- |
| Table, per-row grid | inline | cell text and width seed come from the bound instance instead of `formatCell`; Tone colours the run |
| Table, per-attribute grid | inline | applied where the sink builds each cell string, on the items (element kind); header tag as above |
| Detail, ad-hoc | block, else inline | ADR-0123's `renderRichCell` generalised; its `(executed, row)` cache keeps its shape |
| Detail, leeway card | inline + block | two setters on `Table2CardEmitter`: `SetCellGloss`, a per-column glosser consulted in `BeginColumn` beside the hide rule and applied to the cell text in `EndColumn`; and `SetCellBlock` (2026-08-15 Update on block faces) |
| column header | — | label + small gloss name; physical name, spec line and rule on hover |

A **Raw cells** toggle on the Table toolbar row (beside the pager and the
pin — the leeway options bar is leeway-only, and glosses are not) bypasses
every gloss for the session, in the grids and in Detail. Sorting is untouched:
it permutes rows on the raw values.

### SD5 — The v0 catalog

| media type | accepts | params | inline face | block face | affinity |
| --- | --- | --- | --- | --- | --- |
| `text/markdown`, `text/plain`, `application/json`, `application/sql`, `text/x-go` | text, bytes | `charset` | first line | as ADR-0123 | json ← `sem:json*` |
| `image/png`, `image/jpeg`, `image/gif` | bytes | (`encoding` reserved) | `[<type> · <size>]`, no decode | as ADR-0123 | — |
| `gloss/temperature` | numeric | `unit` ∈ K, C, F — the stored unit, required | `21.5 °C` | — | — |
| `gloss/length` | numeric | `unit` ∈ m, cm, mm, km, ft — the stored unit, required | auto-scaled SI, `1.234 km` | — | — |
| `gloss/epoch` | numeric | `unit` ∈ s (default), ms, us, ns — the stored resolution | RFC 3339 UTC, `2026-08-15T12:00:00Z`; an absurd year (the s/ms mix-up) shows raw in warning | — | — |
| `gloss/duration` | numeric | `unit` ∈ ns, us, ms, s, min, h — the stored unit, required | the two largest units that apply: `12.3 ms`, `1m 05s`, `3d 4h 05m` | — | — |
| `gloss/bytes` | numeric ≥ 0 | — | `humanize.IBytes` | — | — |
| `gloss/taggedid` | numeric, text | — | tag value and counter in hex, `c:3a` (added by the 2026-08-16 Update) | the split spelled out + copy | — |
| `gloss/luhn` | text, numeric | — | groups of four, middle groups masked, ✓/✗ tone by check digit | mask + verdict | — |
| `gloss/masked` | any | — | `••••••`, never length-revealing | same | ← `sem:secret` |
| `gloss/url` | text | — | the URL, accent tone; a hyperlink cell in the grids | `HyperlinkTo` | ← `sem:url` |
| `gloss/raw` | any | — | `formatCell` | — | — |

One or two exemplars per archetype — unit formatting, check digit, masking,
linking — is v0; more are one-file additions. Quantity glosses are spelled
after `public/science/units` (`temperature`, `length`, `mass`, …); unit
conversion (`;show=F`) is deferred, v0 formats the stored unit.

### SD6 — The Glosses tab

A result-side sibling of the Vocabulary tab (ADR-0174): each gloss with its
kinds, parameters, a sample rendering and affinities; the buffer's effective
rules; and each column of the current result with its spec line and resolution
(gloss, source, or why a declaration was refused). It also lists what a
column's binding **shadowed** — the winner first, then the matches behind it,
so a later directive shows behind an earlier one, an affinity behind a
directive, and any rule behind an alias; the tab is where "never offered to the
rules" becomes visible. Insert-at-caret writes a `-- play: gloss` line or a
`gloss(…)` call. (The shadowed list, the accepted-kinds column and the
`gloss(…)` insertion landed in the 2026-08-15 Update on the Glosses tab below,
over `gloss.MatchAll` and `gloss.AcceptedKinds`.)

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
  catalog: a Diagnostics error with a source range instead of a per-cell
  note. Registered as a passreg **Entry** over the built-in catalog, not the
  late-bound Factory first planned — the unbound `/query` path applies no
  factory, and a call left unexpanded there would reach the server as an
  unknown function. A host with a wider catalog registers
  `glosssql.ExpandPass(cat)` in place of the standard entry. Listed in the
  Vocabulary tab's Client section.
- `glosssql.Call` is `Expand`'s dual: it spells the call for a media type and
  its parameters, so `Expand("SELECT " + Call(x, mt, params))` yields the alias
  with the same token — pinned by a round trip over the whole default catalog
  (2026-08-15 Update on the Glosses tab below).

### SD8 — Deferred

Still deferred:

- Reveal-on-click for `gloss/masked`.
- A paint variant of the inline face (arrays as sparklines).
- Unit conversion; `iban`, `isbn`, `percent`, colour swatches; ADR-0123
  §SD7's own list (base64, URL sources, webp/avif/svg).
- Reading kanban's `dot_*@<tone>` tokens as glosses — it would move
  ADR-0122's contract; not taken.

Deferred here, settled since — each by its own 2026-08-15 Update below:

- **Block faces on the leeway card** — taken up, on a second card seam (SD4).
- **Rule files / env, and an in-app rule editor over ADR-0185** — the plan was
  text rules first, because SQL travels and UI state does not. Retired: standing
  rules are Go instead, under version control (SD3).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/hmi/gloss` (new exported Go API) | catalog, `Gloss`/`Instance`, value kinds, cell accessor, rules engine, built-in inline faces | play's block-face bindings; the SD7 pass; tests moved from `play_detail_rich_test.go` |
| `gloss.CellI` (exported interface) | +`Uint64()` (2026-08-16 Update) | `ArrowCell`, `TextCell`, any out-of-tree implementation |
| `leewaywidgets.Table2CardEmitter` | plain-section fan-out keeps `blocks` (2026-08-16 Update) | plain-section row heights on the card |
| play's capability manifest | +`clipboard.write` (2026-08-16 Update) | `play_caps_test.go`'s asserted cap count |
| `public/hmi/gloss/glosssql` + passreg standard set | new pass entry `gloss(…)` | `defaults.RegisterStandard`; the Vocabulary tab's Client list |
| `lwsql` (exported Go API under `public/`) | +`SpecLines` | golden over the leeway fixture names |
| `leewaywidgets.Table2CardEmitter` (exported Go API under `public/`) | +`SetCellGloss` (inline); +`SetCellBlock` (block, 2026-08-15 Update on block faces) | play's card driver wiring |
| result-column convention (ADR-0123 §SD2/§SD3) | "known type" = catalog; `gloss/` family; parameters validated | 0123's status and §SD7; `features.md` §Table/§Detail; `snippets.md` |
| `-- play:` directive family (ADR-0124) | +`gloss` | `features.md` cross-reference |
| play tab roster | +Glosses (chrome in sqlapplet, like Vocabulary) | Panes menu; tab marks; the derived `BOXER_PLAY_FOCUS_GLOSSES` knob in `doc/env-vars.md` |

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
  `clickhouse-local` for the alias measurement; headless tour scenes for the
  Table + Detail rendering — `02_table_glosses`, `03_detail_glosses` (with a
  per-attribute capture and, from the Glosses-tab Update, a capture of the
  shadowed Columns section), and `02_table_taggedid` (the grid, then a
  later row's Detail block face, and the card's plain-section face against
  `anchor.facts`).
- **What would fail.**
  - The ADR-0123 parse cases, moved verbatim (`dot_done@success` silent,
    `notes@text/markdwn` loud, `TEXT/Markdown` folded), plus `;unit=K`
    accepted and `;unti=K` / `unit=k` refused.
  - One golden per inline face — Luhn pass and fail with their tones; a
    `gloss/masked` face of equal length for inputs of different lengths.
  - A spec-line golden over the leeway fixture names.
  - Precedence: alias › directive › repository set › affinity; a `gloss/raw`
    directive suppresses `gloss/masked`'s affinity.
  - The `gloss(…)` golden: label rules, typed parameters, the position rule, a
    non-literal argument rejected with a source range.
- **Gap.** The card hook's look and the hover text are checked by hand at the
  milestone; the per-cell cost of inline faces on a wide visible range is not
  benchmarked — the raw toggle is the mitigation.

## Milestones

- **M0 — core.** ✓ `public/hmi/gloss`: catalog, kinds, cell accessor, params,
  the content family's inline faces; ADR-0123's parser and tests migrated.
- **M1 — Table.** ✓ Inline faces in both grids, width seed, tone runs, header
  label + hover, raw toggle; the `gloss/*` members of SD5 (`epoch` and
  `duration` joined after M5, and `gloss/secret` was renamed `gloss/masked` —
  the word collided).
- **M2 — rules.** ✓ `lwsql.SpecLines`, rules engine, affinities, the directive,
  precedence and hover provenance.
- **M3 — Detail.** ✓ Block faces bound in play; the card seam; the Glosses tab.
- **M4 — macro.** ✓ `glosssql`, passreg entry, Vocabulary listing, goldens.
- **M5 — docs.** ✓ `features.md`, `snippets.md`, the ADR-0123 pointers, two
  tour scenes.

Recorded, not milestones: the Table width seed (`colCharPx`, 7 px per rune)
under-measures the monospace advance (~7.6 px at the tour's density), which a
gloss face wider than its header makes visible as truncation — a calibration
predating this ADR and left to its owner. (Superseded by the 2026-08-15 Update
on the re-fit frame below: the seed was half the story, is now 7.8, and the
re-fit frame was the rest. The reading above is kept as what was believed at
M5.)

## Status

Accepted 2026-08-15, with M0–M5 shipped (see Milestones) and the corrections
found while implementing folded in above. From here, changes land as dated
entries under `## Updates`.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-15 — the truncated faces were the re-fit frame, not only the seed

Milestones recorded a pre-existing width-seed under-measurement as the cause
of glossed faces truncating against header-fitted columns. Half right: the
seed (`colCharPx`, 7 px per rune) was ~11% under Hack's 13 px advance, and
is now 7.8 — but the seed only lives until the re-fit frame. On that frame
egui_table sizes each column in a sizing pass with the cell rect shrunk to
the column minimum and takes the cell's *allocated* width, so a `Truncate()`d
cell reports its truncated width and only the header ever sets the column.
The per-DB-row grid now emits its cells untruncated on the re-fit frame (the
frame egui_table discards and re-runs), so a column fits the wider of its
header and its cells: `4111 •••• •••• 1111 ✓` renders whole. The
per-attribute grid gets the same one-frame re-fit on column-set change,
inside ADR-0151's contract: only columns without a user override are
auto-sized, cells go out untruncated (bounded) on that frame, and the
read-back is adopted as the crate's width rather than captured as the user's.

### 2026-08-15 — the Glosses tab catches up with SD3 and SD6

M3 recorded the Glosses tab as shipped with three things the text above
promises still missing: the matches a column's binding shadowed (SD3), each
gloss's accepted kinds (SD6), and Insert-at-caret for a `gloss(…)` call
(SD6). Landed now: `gloss.MatchAll` — the winner first, then what it
shadows; the resolution keeps `MatchFirst`'s answer and stores the rest —
so the Columns section lists a later directive behind an earlier one, an
affinity behind a directive, and any rule behind an alias, bound or refused
(the tab is where "never offered to the rules" becomes visible).
`gloss.AcceptedKinds` probes an instance once per listing (`any` when it
refuses nothing). `glosssql.Call` spells the call for a media type and its
parameters, `Expand`'s dual: `Expand("SELECT " + Call(x, mt, params))`
yields the alias with the same token, pinned by a round trip over the whole
default catalog. Tour scene `03_detail_glosses` gains a third directive that
loses to the two above it and a capture of the Columns section, so the
shadowed list is in the tour. Nothing changes in the grids or in Detail.

### 2026-08-15 — block faces on the leeway card

SD8 deferred them; SD4 gave the card one seam, the inline text. Taken up
with a second seam on `Table2CardEmitter`: `SetCellBlock(CellBlockFunc)`,
asked per value before the text is glossed and answering with a
`CellBlock{Render, Height}` or declining. The card keeps the inline text —
`SectionDigests` and the Detail timeline's flags read it — stacks each
block-faced pair under the row's inline line (a caption when the row has
more than one pair, then the blocks) and grows the row by what they ask,
clamped to [one text line, 320 pt]. `play` binds the faces the ad-hoc pane
already has: markdown, plain text, code and images from the row's artifact
cache, now keyed by column *and* value ordinal since a card shows every
attribute of a column, and `gloss/url`'s hyperlink. The height is the
host's estimate — 18 pt per source line, at most twelve — because a table
row is declared before its cells are laid out; a face that runs longer
scrolls inside its area, and images box at 400 × 240. Raw cells switches
this seam off with the other. Tour scene `03_detail_glosses` binds
`text/markdown` to the text column, so the card's block face is in the
tour; reveal-on-click for `gloss/masked` stays deferred.

### 2026-08-15 — standing rules are code: `gloss.Repository`, rule sets, injected

SD8 had left rules that outlive a query to files, an env var, or an editor
over ADR-0185. Decided instead: such rules are Go, under version control,
declared beside the glosses they bind — a query's own overrides stay in its
buffer as `-- play: gloss` lines, and nothing is read from files, the
environment, or persisted UI state. `public/hmi/gloss` gains:

- **`Repository`** — first-class: rule sets over one catalog, in
  registration order, then the catalog's affinities (`Rules()`); built at
  wiring time and **injected** — `NewPlayApp` / `NewLivePlayApp` take a
  `*gloss.Repository`, `PlayLauncher.Rules` and sqlapplet's
  `EmbedConfig.Rules` carry one, and `play.DefaultRepository()` is what a
  nil argument and the self-registered launcher use, so a deployment that
  links play registers its sets there before the first window mounts.
- **`RuleSet`**, built with a chain that reads as the rule does —
  `gloss.Rules("acme-sensors").Rule("kelvin readings").When(gloss.Section("sensor"),
  gloss.NameMatches("^temp")).Show(gloss.MediaTypeTemperature, gloss.Unit("K"))`
  — validated whole at `Repository.Register` (unknown type, bad parameter,
  bad pattern, no condition, duplicate or missing name), so a set that does
  not validate applies to nothing, loudly, at startup.
- **Predicates** over a parsed **`Spec`** (the spec line taken apart by
  prefix): `Name`, `NameMatches`, `Section`, `Role`, `Item`, `CT`, `Arrow`,
  `Enc`/`Sem`/`Use` typed by the aspect enums — a misspelt aspect does not
  compile — `All`, `Any`, `Not`, and `SpecMatches`, the directive's own
  regex as the escape hatch. Each prints as it reads (`section=sensor ∧
  name~^temp`), which is what the Glosses tab and the hover show; a
  directive is now the predicate `SpecMatches(re)`, so one matcher serves
  every source.

Precedence per column: alias › buffer directives › repository sets ›
affinities. The buffer stays above code so a query's `gloss/raw` can switch
a standing rule off for itself; a set is a deployment default, not a
mandate. The first checked-in set is sqlapplet's `sqlapplet-books`: byte
counts (`(^|_)bytes$`), nanosecond and millisecond durations (`_ns$`,
`_ms$`) — the suffixes the shipped books use — bound to every standalone
applet window through `EmbedConfig.Rules`. The recipe is
[doc/howto/play-gloss-rules.md](../howto/play-gloss-rules.md).

### 2026-08-16 — `gloss/taggedid`, and the unsigned read it needed

A tenth presentation gloss: `gloss/taggedid` shows a fibonacci-tagged
identifier ([ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md))
as its two halves — the tag value, a colon, the per-tag counter, both in hex —
rather than as the one 19-digit decimal it is stored as. `12393906174523605050`
reads as `c:3a`. The split is the one `LW_ID_TAG_VALUE` / `LW_ID_BODY` compute
server-side, done client-side so a column of ids reads without changing the
projection.

Three things fell out of it:

- **`CellI` gained `Uint64()`.** The smallest tag sets the top two bits, so
  most tagged ids are above 2^63: `Int64()` refuses them and `Float64()`
  rounds the counter away. The typed accessor also keeps the face inside its
  one-allocation budget, which parsing `Text()` would not. `ArrowCell` reads
  every integer array and refuses a negative rather than wrapping it;
  `TextCell` parses base 10, like its `Int64`. The gloss itself falls back to
  parsing the text as decimal or `0x`-hex, so a `toString(id)` column and the
  text-backed leeway card both work.
- **A block face with copy buttons.** Like `gloss/url`, the gloss is
  special-cased by name where the block faces render: the inline face, then
  the tag value with its code width and the counter with the room its tag
  leaves, then **Copy id** (decimal, for a `WHERE id =`) and **Copy hex**.
  One settled `taggedIdBlock` feeds both the render and the height the card
  reserves, so the two cannot disagree — the heights themselves are measured
  against a rendered row, since a button is taller than a text line and an
  underestimate clips rather than merely crowds.
- **play declares `clipboard.write`.** It did not, and the Definition pane's
  per-fence Copy buttons had been requesting it and being denied. The
  capability is now declared with the count assertion in `play_caps_test.go`
  moved deliberately; `PlayApp.CanCopy` gates the affordance on a wired bus,
  which is why the omission was silent rather than loud.

Wiring the card block face turned up a gap in the 2026-08-15 Update's own
work: `Table2CardEmitter.EndPlainValue` fans a **plain** section out into one
row per value column, rebuilding each pair rather than moving it, and it did
not carry `blocks` over. A claimed block therefore never drew in a plain
section — which is precisely where a leeway entity's id sits, so it was the
one place `gloss/taggedid`'s block face was needed. Fixed, with a test that
drives the plain fan-out and fails without it. The tagged path was never
affected, which is why the markdown card face read as working.

No parameters — the split is a property of the value — and **no affinity**:
`sem:` says a column is a surrogate key, not that its surrogates are
fibonacci-tagged, and glossing a plain sequence as a tagged id would fill the
column with warnings. Deployments that mint tagged ids bind it by alias or by
a rule set. What the face cannot show, it says: a word carrying no fibonacci
comma shows plain in the warning tone, and so does a real tag over the
reserved counter 0 — a well-formed word that was never minted.

Verified live through the headless tour: a new `02_table_taggedid` scene
captures the grid and, after selecting a later row, the Detail block face;
the leeway card's plain-section face was captured against `anchor.facts` with
its ids shifted into a tag.

## References

- [ADR-0123](./0123-play-content-typed-detail-cells.md) — content-typed detail cells; the gate this ADR keeps.
- [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md) — the fibonacci-tagged id scheme `gloss/taggedid` reads.
- [ADR-0122](./0122-play-kanban-panel.md) §SD2 — the measured `@` separator; `dot_*@<token>`.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md), [ADR-0121](./0121-selection-condition-columns.md) — the other conventions on result column names.
- [ADR-0124](./0124-play-param-editing-widgets.md) — the `-- play:` directive family.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) §SD2/§SD6 — spec tokens, constructor family, `Composer`.
- [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) — aspect vocabularies and their admission criterion.
- [ADR-0108](./0108-keelson-sql-pass-registry.md) — passreg and the late-bound Factory.
- [ADR-0174](./0174-play-sql-vocabulary-panel.md) — the Vocabulary tab the Glosses tab mirrors.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the semantic palette behind `Tone`.
- RFC 2045 §5.1 (parameter syntax), RFC 6838 §3.4 (`x-` discouraged); Go `mime.ParseMediaType` / `FormatMediaType`.
