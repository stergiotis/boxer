---
type: adr
status: proposed
date: 2026-09-02
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0218: markdown ingestor — a document as an item-per-row fact family, its frontmatter as an mlvhp row

## Context

[ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md) persists a markdown
document as one `boxer.facts` row: the source, a title, a name, a hash. That
is enough to hand a document to play; it is not enough to ask the questions a
note-taking tool answers over a vault — which notes link to this one, which
carry a tag, what a property is set to — because the source is opaque to SQL.

The wish is an ingestor whose stored form is sufficient for the prominent
features of an Obsidian-style vault — the link graph, backlinks, tag
resolution, properties — for the `semistructured/markdown` flavour only. The
frontmatter is to be parsed fully and represented the way the canonical leeway
JSON mapping represents a document (the mlvhp scheme: a low-cardinality path
template with the high-cardinality array indices split off, values grouped by
type). The heading structure is to be represented structurally, and every
fenced code block, hyperlink, emphasised span and tag extracted.

Three properties of the table shape the answer. `boxer.facts` sections are
one per canonical type and are not co-sections, so two fields of one item
stored in two sections align only by position. Its sections accept
`LowCardRef`, `HighCardRef` and `MixedLowCardRefHighCardParameters` and no
verbatim channel, so a path cannot be a membership name. And the generated
store lane reads only the simple channels (facts-bound record stores,
[ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md)), so
anything on the mixed channel is written by hand and read through the SQL
surface's `param:` selectors ([ADR-0181](./0181-leeway-dql-authoring-surface.md),
2026-08-15 Update).

## Decision

### SD1 — Extraction is a pure package, apart from persistence

`public/semistructured/markdown/mdextract` reads one document into a
`Document`: frontmatter leaves, headings, code blocks, links, emphases, tags,
each with its document-order ordinal, its source line and the heading it sits
under. It renders nothing, resolves nothing, and depends on no store, so its
reading is pinned by a golden over a fixture vault and reusable by anything
that wants the same reading. Link targets are carried as written (fragment
split off, non-URL targets percent-decoded); resolving them against a set of
documents is the reader's join.

### SD2 — One row per extracted item, in the generated lane, beside `MdDoc`

Five kinds join the store ADR-0217 created — `MdHeading`, `MdCodeBlock`,
`MdLink`, `MdEmphasis`, `MdTag` — each a flat DTO of scalar, `unit`, `Option`
and container fields, so every one has a component, a generated `Ingest`,
`Scan` and `LW_COMPONENT` read. Every item row carries the same spine under
kind-specific memberships: the kind marker, the document row's id on
`foreignKey`, the document's content hash (the bytes of the document row's
natural key), the ordinal, the line, and the enclosing heading's ordinal. A
heading carries its parent's ordinal and its ancestors' texts, so the tree is
walkable and filterable without recursion.

The same store, not a second one: a store *is* its component set, and two
stores over one table are two entity types (facts-bound record stores, "What a
store is made of"). The registry-resolved ids lift the disjoint-section gate,
so the kinds share `symbol`, `stringArray` and the rest.

### SD3 — The frontmatter is its own row, on the mixed channel, written raw

A document with a YAML block gets one `mdFrontmatter` row beside it: the kind
marker, the document reference and the content hash on the low-card-ref
channel of `symbol`, `foreignKey` and `blobArray`; every leaf as one attribute
in the section of its YAML type — `stringArray`, `i64Array`, `f64Array`,
`bool`, `timeArray`, and `symbolArray` for the value-less markers `null`,
`[]`, `{}` — carrying two memberships on the mixed channel: `mdFrontmatterPath`, whose
parameter is the path with array positions elided to `_`, and
`mdFrontmatterParams`, whose parameter is the elided indices in the params
codec's canonical form, attached only when the path crosses an array. That is
the canonical mapping's `lmv`/`mvhp` split translated into a schema with no
verbatim channel — the jsonbench trial's construction, and the reason the path
is stored per attribute rather than dictionary-encoded.

A row of its own, not attributes on the document row, because the generated
entity builder makes `Raw()` and `Add<Kind>()` exclusive per entity: a
component row cannot also carry hand-written attributes. No section on the
row mixes channels, so nothing here is the shape `PlanFor` refuses.

### SD4 — Two-level identity, per row

The document row keeps ADR-0217's rule. An item row's `Id` hashes the
document's id, the kind and the ordinal — distinct within one ingest — and its
natural key hashes the content hash, the kind and the ordinal, so re-ingesting
identical text yields the same natural keys throughout. Every row's natural
key is set through the entity envelope explicitly.

### SD5 — One write path: the CLI and mdedit's send-to-play

`boxer markdown ingest <file|dir>…` walks `.md` files (dot-directories
skipped), stores each under its path relative to the directory it was found
from, and writes through the store; `boxer markdown extract` prints the
reading as JSON without a database. ClickHouse is reached through the
registered `CLICKHOUSE_*` variables; the schema is verified, never created
([ADR-0184](./0184-sysmetrics-persistence-tee.md) §SD2). mdedit's
send-to-play (ADR-0217) writes through the same `IngestDocument`, so a sent
document and an ingested one carry the same rows; its title and word count
are the extractor's, taken off the same bytes.

### SD6 — Resolution lives in SQL, and the how-to is the contract

The store answers "what links to what" as rows; which document a target names
is a join the reader writes — by path, by basename, by alias out of the
frontmatter row. The how-to publishes those queries, and an integration test
executes them against a `clickhouse local` provisioned from the store's own
DDL and the leeway SQL surface, asserting the answers over the fixture vault.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `mddocvocab` natural-key registry | ordinals 6–58 added; none re-pointed | the assignments golden |
| `mddocfacts` store (`MddocComponentSQL`, `Ingest*`, `Scan*`) | five kinds added; `MdDoc` unchanged | the component-expansion golden; play's kind-roster test |
| `MdDoc.FileName` | widened: a basename from an editor, a vault-relative path from the ingestor | the field's doc comment |
| `boxer.facts` rows | a new kind label per item kind and `mdFrontmatter`; the mixed channel used on the frontmatter row's typed sections | the how-to's queries |
| `boxer` CLI | `markdown ingest`, `markdown extract` | `public/app/main.go` |

## Alternatives

- **One row per document with arrays per item field.** Rejected: facts
  sections are per-type and not co-sections, so a heading's level in
  `u8Array` and its text in `stringArray` align only by position, with
  nothing binding them; the reads become the array arithmetic the SQL read
  surface exists to retire.
- **Frontmatter as a generated kind, one row per leaf** (path as a symbol,
  indices as a list, the value in a typed field). Rejected: it fits the
  generated lane, but it transposes the leeway address into columns instead
  of carrying it as the membership `(path, params)` the mlvhp scheme names,
  and it forfeits the `param:` selectors the surface already provides for
  exactly that channel.
- **Frontmatter attributes on the document row.** Rejected: the entity
  builder refuses raw writes on an entity carrying a component, and the
  document row must stay a component for ADR-0217's launch query.
- **A verbatim channel for the path.** Rejected: the table does not declare
  one, and adding a membership spec to `boxer.facts` is a schema decision this
  ingestor has no standing to take.
- **Obsidian-specific typed columns for `tags` and `aliases` on the
  frontmatter row.** Rejected for aliases: a low-card-ref attribute beside the
  leaves would mix channels within a section, and the leaf read is one
  selector. Tags do get a typed home — `MdTag` rows with source
  `frontmatter` — because tag resolution is one question across both
  spellings.

## Consequences

### Positive

- The graph, backlinks, tag resolution, section membership and property
  reads are plain SQL over component tuples plus one selector family, all
  executed in a test rather than asserted in prose.
- A component per item kind means play resolves `LW_COMPONENT('MdLink')` and
  the rest as soon as the store is registered, which it already is.

### Negative

- The frontmatter row is unreadable through the generated lane: no `Scan`,
  no `LW_COMPONENT`, no cache mirroring. Its readers are the SQL surface and
  the raw lanes.
- Path strings are stored per leaf in the parameter lane rather than
  dictionary-encoded, the cost the jsonbench trial measured for this
  construction.
- A date is recognised on the string, since the YAML decoder hands
  timestamps back as text: a quoted `"2024-03-01"` becomes a time leaf as
  much as an unquoted one, and a zone-less value is read as UTC. The
  spelling survives on the extraction, not on the row.
- Observed while building (2026-09-02): the generated `Ingest<Kind>` verbs
  pass an empty envelope, so a DTO's `NaturalKey` never reaches the row
  through them — ADR-0217's sender writes rows with an empty natural key. The
  ingestor sidesteps it by using `Begin` with an explicit envelope; the
  generator is the place to fix it.

### Neutral

- Emphasis covers highlight and strikethrough beside bold and italic: the
  walk sees them at no extra cost and Obsidian renders them as emphasis.
- `MdDoc.Words` counts text nodes of the parsed body, which differs from
  mdedit's lexer-based count for the same text; neither is wrong, and the
  column is a size signal.

## Migration — Tier 1

- **Breaks.** Nothing: the vocabulary grows by new ordinals only, `MdDoc`'s
  shape is unchanged, and rows already written read as before.
- **Regeneration.** The store's gen-test, the component golden, the
  vocabulary golden — all in the tree.
- **Old shape.** Kept.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the extraction golden over the fixture vault
  and the per-feature extraction tests (`mdextract`); the row-building and
  identity tests, the component-expansion golden covering every kind
  (`mddocfacts`); the vocabulary assignments golden; play's kind roster. The
  `integration` lane: `TestObsidianQueriesOverTheFixtureVault` provisions
  `clickhouse local`, ingests the vault and runs the how-to's queries.
- **What would fail.** A parser priority or feature flag change alters the
  golden; a re-pointed id or renamed membership alters the goldens; a change
  in what the passes emit for the mixed channel, or in the DTOs, alters an
  answer in the integration lane.
- **Gap.** No live-server run in the default lane, and no measurement of the
  frontmatter row's storage cost beyond the trial's figure for the same
  construction.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md) — the document kind this extends.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — the `param:` selectors the frontmatter row is read with.
- [doc/explanation/facts-bound-record-stores.md](../explanation/facts-bound-record-stores.md) — the three refusals, and the mixed-channel question.
- [doc/trials/jsonbench-on-facts](../trials/jsonbench-on-facts/README.md) — the path-in-the-parameter-lane construction and its cost.
- [doc/howto/markdown-facts-obsidian-queries.md](../howto/markdown-facts-obsidian-queries.md) — the queries.
