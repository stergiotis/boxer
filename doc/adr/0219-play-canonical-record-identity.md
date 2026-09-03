---
type: adr
status: proposed
date: 2026-09-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0219: `play` shows a record's canonical identity — canonform and canonwire on demand, and a CBOR diagnostic-notation widget

**In one paragraph:** the Detail pane gets an identity strip for the selected
row — its canonform digest ([ADR-0201](./0201-leeway-canonical-record-form.md))
and its canonwire fingerprint ([ADR-0210](./0210-leeway-canonical-wire-generator.md))
— and, behind a disclosure, the CBOR items both forms are computed from,
rendered in RFC 8949 §8 diagnostic notation by a new widget. The Table pane
gets an options-bar toggle that reveals the same two values as trailing
columns, computed for the whole result by a background job. To make the wire
form reachable from an arbitrary result, canonwire gains a stream encoder
(`canonwire/streamenc`) — a `streamreadaccess` sink, the shape
`canonform.Encoder` already has — and a fingerprint over its bytes.

## Context

Two canonical forms of a leeway entity exist and nothing displays either:

- **canonform** (ADR-0201) is the content identity: a quotient that erases
  width, aspects, section names and order, digested with keyed BLAKE3. Its
  ADR lists "first consumers" as an open milestone; no code outside the
  package calls it.
- **canonwire** (ADR-0210) is the lossless wire: slot keys are canonical-type
  signatures, attributes sorted into canonical order. Its encoder is
  *generated per table* over that table's generated `readaccess` classes.
  `play` has no generated classes for what it shows — a result's `TableDesc`
  is reconstructed at run time from physical column names
  (`datacatalog.Classify`, the `CardDriver`), so the generated route does not
  reach it. The runtime package has every writer the generated encoder calls,
  and the slot table (`canonwire.BuildSlotTable`) is computed from a
  `TableDesc`, which `play` has.

Four uses want them on screen, and they want different things:

- **troubleshooting canonicalisation** — a developer asks *why* two records
  that should hash alike do not, which needs the bytes, not the digest;
- **showcases** — the invariance story (widen a column, re-aspect it, permute
  attributes; the digest stands) is told by watching a value not change;
- **comparing records** — a person scanning a result wants to see that rows
  7 and 40 carry the same content, which needs the digest beside every row;
- **developers of the forms themselves** — the runtime's `VerifyCanonical`
  verdict beside the bytes it judged.

CBOR bytes are readable in this repository only through
`boxer.sh cbor diagnostics`, which prints one item per line in the
fxamacker library's diagnostic notation: single-line, no indentation, no
hook to label a tag or a map key. An entity item of either form is a nested
structure whose meaning sits in its position (`[version, plains, tagged]`,
slot keys, membership channels), and a single line of it is not readable.

Constraints from the host:

- The `CardDriver` is `play`'s single leeway-schema reconstruction point;
  every pane reads its classification so they agree on what a column is. It
  builds one `streamreadaccess.Driver` per schema and drives a one-row slice
  through it per frame. A `Driver` is not goroutine-safe.
- The per-DB-row grid renders Arrow columns by index (`visCols`), with width
  identity keyed on (name, type) per
  [ADR-0151](./0151-table-column-width-overrides.md), and every pane's cache is
  keyed on the `*arrow.Schema` pointer — rebuilding a record to append
  columns would read as a new query everywhere.
- The Detail pane's typed component report (ADR-0075, 2026-09-02 update)
  already sets the pattern for a fixed-height block above the card with a
  per-(record, row) cache.

## Design space (QOC)

**Question.** Where does a per-row canonical digest live in the Table pane?

**Options.**

- **O1** — synthetic trailing grid columns, revealed by an options-bar toggle;
  values from a per-record cache filled by a background job.
- **O2** — append the digests as real Arrow columns to a derived record.
- **O3** — a tooltip on the `#` selector cell of each row.
- **O4** — a separate pane listing row → digests.

**Criteria.**

- **C1 — comparability.** Can a person see two rows' digests at once and
  copy one?
- **C2 — blast radius.** What existing machinery (schema-pointer caches,
  selection, sort, width tiers) has to change?
- **C3 — cost when unused.** Nothing computed until asked.
- **C4 — one place.** The same values, from the same code, as the Detail pane.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | −− | +  |
| C2 | −  | −− | ++ | +  |
| C3 | ++ | −  | ++ | ++ |
| C4 | ++ | +  | +  | −  |

O1 is chosen. O2 is the obvious shape and the worst one here: a derived
record is a new schema pointer, so every pane forgets its widths, its
classification and its selection, and the digests would then flow into every
consumer (export, send-to-play) as if they were data. O3 cannot compare. O4
separates the digest from the row it belongs to.

## Decision

We add a canonical-identity view to `play` in both result panes, a stream
encoder and a fingerprint to canonwire so an arbitrary leeway-shaped result
can reach the wire form, and a CBOR diagnostic-notation pretty-printer with a
widget over it.

### SD1 — Both forms are computed client-side, from the result, through the card's schema

Digests are a function of the entity, and the entity is the Arrow row the
result already holds. Both encoders are `streamreadaccess` sinks driven over
the same `TableDesc` and IR the `CardDriver` reconstructed, so the card, the
identity strip and the grid columns cannot disagree about what a column is.
The `CardDriver` exposes what a second driver needs (`TableDesc`, the IR, the
naming convention and row config it classified with) so a background job can
build a `Driver` of its own instead of sharing the card's.

Nothing is asked of the server: the digest has no SQL implementation
(ADR-0201 M3 defers a UDF) and a client-side value is the one a person can
recompute from the bytes on screen.

### SD2 — canonform: the card's classifier, entity id excluded, the pin shown

`canonform.Options` are fixed as: the classifier the card emitter uses
(`membershiprole.PathPrefixClassifier`, so the primary/secondary split the
card draws is the one the hash counted); `IncludeEntityId` false — the
content-comparison use is exactly "same content, different id"; the default
plains mask and digester. The encoder's `FormPin` is shown as the digest's
tooltip, so a reader knows which `(classifier, plains, digester)` the value is
a function of. A row whose drive fails shows the error in place of the value.

### SD3 — canonwire gains a stream encoder, byte-identical to the generated one

`streamenc.Encoder`, in a subpackage of `canonwire` so a consumer that only
encodes does not import the generator's code-emission half, is a
`streamreadaccess.SinkI` with the `MembershipSinkI`, `ArrowValueSinkI` and
`CoSectionTagSinkI` capabilities — the sink `canonform.Encoder` already is —
constructed from a `TableDesc` and its IR. It builds the slot table once (SD2 of ADR-0210) and, per entity,
drives the runtime's `EntityWriter` / `AttributeWriter` / `SetWriter` exactly
as a generated encoder does: plains keyed by item type in `PlainGroupOf`
order, tagged attributes into the slot their section's signature names, values
written in key order, memberships with the channel the sink call implies
(`lowCard` × ref / verbatim / parametrized / mixed → the eight
`mappingplan.MembershipChannel`s). Every entity item is flushed to the
caller's writer; per-row byte ranges are reported so a caller can address one
entity of a batch.

Two things the generated encoder has, the stream encoder states rather than
supplies:

- **No tagger.** ADR-0210 SD5's discriminator rides only when a `TaggerI` is
  supplied; the stream encoder supplies none, so an ambiguous source slot
  encodes without one — the same bytes the generated encoder emits with a nil
  tagger. A consumer that needs section-name round-tripping generates.
- **Null values.** The generated encoder never produces the `null` value form
  (ADR-0210 Neutral), and the stream encoder refuses an Arrow null as
  canonform does — leeway has no null. Parity is pinned only over what both
  can produce.
- **Placeholders.** The driver's empty-slot memberships
  (`membership.IsPlaceholder`) are a text-lane reading convention; the
  stream encoder applies none of it. A zero ref is a value the lane holds,
  the generated encoder writes it, and so does this one.

The invariant is **byte parity**: over every table in `canonwire/example`, an
entity encoded through the generated encoder and through the stream encoder
yields identical bytes, and every stream-encoded item passes
`VerifyCanonical`. Value writing shares code with `canonform` where the two
forms agree (temporal, network, strings, bytes) and diverges where ADR-0210
SD3 says they differ (no numeric reduction, IPv4-mapped kept, set duplicates
kept) — the split already drawn between `canonform` and the shared writer.

### SD4 — A canonwire fingerprint: keyed BLAKE3 over the entity item, a representation identity

ADR-0210 states the wire is not a hash preimage, and that stands: the content
identity is canonform. What `play` shows beside it is a **fingerprint of the
bytes** — equal iff the two entities have the same lossless wire form: same
values at the same widths, same memberships on the same channels, same set
multiplicities. It answers a different question ("are these the same
*record*?") from canonform's ("the same *content*?"), and showing both is the
point: two rows with equal canonform digests and unequal canonwire
fingerprints differ only in something the quotient erases, which is the
troubleshooting case in one glance.

It is defined in `canonwire/runtime` — `Fingerprint(item []byte)` — so every
tool computes the same value: BLAKE3-256 in keyed mode, key derived from a
pinned context string naming the form and its version (the ADR-0201 SD7
construction), golden-pinned. It is a function of `Version`: a form bump
changes every fingerprint, which is correct, since the bytes changed.

### SD5 — Detail: an identity strip above the card; the bytes behind a disclosure

Below the component report and above the leeway card, a fixed-height block:

- one row per form — a label, the value in monospace hex, a copy button; the
  canonwire row also shows the item's byte length and the
  `VerifyCanonical` verdict;
- a disclosure, closed by default, that opens the diagnostic widget (SD6) in
  two sections: the canonform **attribute items and entity item** (captured
  with `NewRecordingDigester`, which is what that seam exists for) and the
  canonwire **entity item**.

The values are computed once per (record, row) when the row is first drawn,
by driving a one-row slice through fresh encoders on the render thread — the
cost is that of the card's own `Prepare`, which runs every frame. The bytes
are captured only while the disclosure is open. A non-leeway result draws
nothing, like every other leeway-only block.

### SD6 — CBOR diagnostic notation: a span-producing pretty-printer, and a widget over it

A new package, `diag`, under `public/semistructured/cbor` renders CBOR bytes as RFC 8949 §8
diagnostic notation with indentation: one element per line inside a
container past a line-width threshold, nested containers indented, map
entries as `key: value`. It walks the bytes itself — CBOR heads are a page of
code, and the walk must accept **any** well-formed CBOR, not the
deterministic subset the leeway `CborReader` enforces, because the
troubleshooting case is precisely a non-canonical item. Output is a slice of
spans (category + text: structural, key, number, text, bytes, tag, simple,
comment, error), so one walk serves the plain string (clipboard, CLI) and the
highlighter. Options: indent, bytes as `h'…'` with fold width, §8.1 float
precision suffixes, and §8 comments for tags — a built-in table for the tags
the repository writes (`258` set, `1001` time, `52`/`54` network) plus a
caller hook that labels a position from its path, so a form-aware host can
annotate `[version, plains, tagged]` and slot keys. A CBOR sequence renders
item by item. Malformed input degrades: what parsed is printed, the failure is
a comment, the remainder is hex — the `jsonhighlight` posture.

The widget, `widgets/cbordiag`, is the `codeview` pattern: a `CodeViewJob`
built from the spans (a `BuildCborDiag` beside `BuildJson`), a copy button, a
byte count, and a compact / expanded toggle. `boxer.sh cbor diagnostics`
gains a `--pretty` flag routed through the same printer, so the notation a
person sees in `play` and on a terminal is one implementation.

### SD7 — Table: an options-bar toggle, two trailing columns, one background job per record

The leeway options bar (ADR-0097 Update) gains "canonical hashes", off by
default. When on, the per-DB-row grid appends two synthetic columns after the
Arrow columns — sentinel indices past `schema.NumFields()`, so
`visCols` stays a list of column ordinals and cell ids stay keyed on
them — with width identities of their own under ADR-0151 (a fixed name and a
format tag, never sampled). A cell shows a prefix of the hex with the full
value as tooltip; a click selects the row as every cell's does, and copying
is the Detail strip's button. The column is not sortable (a digest has no
order worth sorting by) and not glossed.

The values come from a per-record cache filled by **one job per record**: a
goroutine builds its own `Driver` from the `CardDriver`'s recipe, drives the
**whole** record once through both encoders — each is a streaming sink, so
this is one pass and 64 bytes of retained state per row — and publishes the
two digest slices under a mutex. Cells read "…" until the job lands; a new
result abandons the old job by record identity; an error is shown once in
the bar. The per-attribute grid does not take part (SD9).

### SD8 — A result identifier for pane caches

Panes today key their caches on what happens to be stable: the
`*arrow.Schema` pointer, the record pointer, the executed timestamp, or a
pair of them read under one lock. Each is an accident of the delivery path —
a bound node's memo serves the same record under a fresh frame, a failed run
replaces a record with nothing, and the intermediate lane and the main lane
never agree on any of them. The two caches this ADR adds (SD5, SD7) would be
the third and fourth such idioms.

`play` mints a **`ResultID`** — a process-unique, monotonically increasing
integer — at the one moment a lane replaces what it serves: `QueryStore`'s
finish, a node lane's landing. It travels with the result through the
snapshot into `TabFrame`, beside the record. Its contract:

- it changes exactly when the record a pane is handed is replaced — a
  landed run, a failed run (the record becomes nil), a bound node's memo
  swap — and never otherwise; equal ids mean the same record object and the
  same metadata;
- it is unique across lanes and app instances in one process, so a pane
  that alternates between the active result and a bound node's view cannot
  alias two results under one id;
- zero is "no result yet";
- it is **not** a content fingerprint: a re-run that returns identical bytes
  gets a new id. The node lane's FNV fingerprint remains the early-cutoff
  hook for the observers that want one.

New caches key on it; the identity strip's cache is (id, row) and the Table
job's is the id. Existing pointer-keyed caches are not migrated by this ADR
— each is correct today, and moving one is a leaf change to make when that
pane is next touched.

### SD9 — Deferred

- **Leaf digests per attribute.** `canonform` computes one per attribute and
  keeps it internal; the per-attribute grid and the card could show them.
  Needs an exported accessor and a display rule; not built until asked.
- **An outline mode** for the widget (a `fieldview` tree over the CBOR data
  model) beside the text mode.
- **Diffing two rows' items** — pin one, colour the other's differences.
- **Options in the strip** (include the entity id, choose a classifier). The
  pin says what was used; changing it is a developer's rebuild today.

### Milestones

- **M0 — the printer and the widget.** ✓ (2026-09-02) `cbor/diag`, checked
  against the fxamacker library's notation over RFC 8949 Appendix A's
  examples table, every float16 bit pattern, the repository's random CBOR
  generator and the canonform goldens; a pretty-mode golden;
  `widgets/cbordiag` with a demo registration; the `--pretty` flag.
- **M1 — canonwire stream encoder and fingerprint.** ✓ (2026-09-02) Parity
  over the six `canonwire/example` tables, each also through one-row slices
  and a second drive; `VerifyCanonical` on every item; the fingerprint's
  derived key and one fingerprint pinned. The ADR-0210 M2 `rapid` entities
  are not driven through the stream encoder: their generators are private
  to the example package, and the six fixtures cover every channel, every
  lane and both ambiguity sets.
- **M2 — the result identifier and Detail.** ✓ (2026-09-02) `ResultID`
  through both lanes into `TabFrame` and `ChannelResult`, with a test that a
  landed, a failed and a memo-served result each move it and a repeated
  snapshot does not; the strip and the disclosure over the `boxer.facts`
  fixture the component report's tests build; the canonform value equals the
  package's own digest of the same row and the fingerprint the runtime's.
- **M3 — Table.** ✓ (2026-09-02) The toggle, the columns, the job; the value
  in the column equals the value in the strip for the same row (single-row
  slice and whole-batch drive agree). No live-endpoint check has been done.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `canonwire/runtime` (exported Go API) | added: `Fingerprint`, its context string and derived key | a key golden; nothing stores fingerprints at M1 |
| `canonwire/streamenc` (new exported Go API) | added: `Encoder` (a `streamreadaccess` sink) | parity tests against `example/`; no wire byte moves |
| `diag` under `public/semistructured/cbor` (new exported Go API) | added: pretty-printer, spans, options | goldens; the `cbor diagnostics` CLI gains `--pretty` |
| `widgets/cbordiag`, `codeview.BuildCborDiag` | added | demo registry entry |
| `CardDriver` (leaf, `apps/play`) | reshaped: exposes the IR and a detached driver over the same recipe | the Projector and Schema pane are unaffected; they read what they read |
| `PlayApp.MainSnapshot` (exported embedder seam, `apps/play`) | reshaped: gains a trailing `ResultID` return | no in-tree caller outside the package |
| `play` result snapshot (`TabFrame`, the lane views) | added: `ResultID` | new caches key on it; existing pointer-keyed caches unchanged |
| `play` Table options bar, Detail body | added: a toggle, two synthetic columns, a block | the help corpus (`features.md` § Table / § Detail); the launch-config seed if the toggle persists |

## Alternatives

- **Generate canonwire classes for `boxer.facts` and show the wire only for
  facts rows.** Reaches one table; `play` shows any leeway-shaped result, and
  the stream encoder costs one sink beside an existing one.
- **Reformat fxamacker's `Diagnose` output.** Single-line text with no
  positional hook; indenting it means re-tokenising diagnostic notation,
  which is a parser of the same size as walking the bytes, minus the ability
  to annotate.
- **Pretty-print over the leeway `CborReader`.** Strict by design — it
  refuses a non-shortest head — and the widget's first job is to show an item
  that is *not* canonical.
- **Compute digests on the server.** No CBOR, no BLAKE3 in ClickHouse SQL
  without a UDF (ADR-0201 M3), and the client already holds the bytes.
- **Compute the Table values on the render thread, page by page.** A page
  drive per frame would be the card's cost times the page size; a one-shot
  whole-record job is one pass and never blocks a frame.
- **Store the canonwire fingerprint nowhere and show only bytes and length.**
  Loses the one-glance "same content, different representation" read that
  pairs the two values.
- **A hash of the card-JSON (ADR-0018) as the second value.** Section- and
  column-name-dependent and text-lane-derived; the two existing forms already
  bracket the question.

## Consequences

### Positive

- canonform gets its first consumer, and canonwire a route that does not need
  generated code — any Arrow batch with leeway column names encodes.
- One implementation of readable CBOR for the widget and the CLI, with
  form-aware labels where the host knows the form.
- Comparing records is a column beside them; troubleshooting is one
  disclosure away from the digest.

### Negative

- A second `streamreadaccess` sink that must track the generated encoder
  byte for byte; the parity suite is what holds them together, and a
  generator change that moves bytes fails it — which is the intent.
- The synthetic columns are a new case in the grid's column model, and every
  place that maps a display position to an Arrow column has to skip them.
- The stream encoder cannot carry a discriminator, so for a source table
  with name-only-distinguished sections the wire it shows is the one a
  nil-tagger generated encoder would emit — stated in the strip's tooltip.

### Neutral

- The fingerprint is a hash of a serialization, and two entities with equal
  fingerprints have equal wire forms by construction; the converse holds up
  to collision. It is not a content identity and the strip labels it as the
  wire's.
- The values are computed from what the result carries: a projection that
  drops a section changes both, as it should — the entity on screen is the
  entity being identified.

## Migration — Tier 1

- **Breaks.** Nothing; every change is additive. `CardDriver` gains
  accessors.
- **Path.** None.
- **Regeneration.** None; the generated `canonwire` outputs are untouched.
- **Old shape.** n/a.

## Verification plan — Tier 1

- **Lane.** Default `go test`: parity goldens in `canonwire/example`
  (generated vs stream bytes), `VerifyCanonical` over every stream item,
  the fingerprint key golden, `cbor/diag` goldens over the RFC 8949 Appendix
  A vectors and the two forms' items, `play` unit tests over the facts and
  anchor fixtures (strip value = package digest; column value = strip value;
  synthetic columns present only under the toggle).
- **What would fail.** A byte moved by the stream encoder fails parity; a
  changed `Version` without a new fingerprint golden fails the key pin; a
  notation regression fails a diag golden; a grid change that renders a
  sentinel index as an Arrow column panics the column-model test.
- **Gap.** No headless scene for the widget beyond the demo screenshot; no
  live-endpoint check. The parity suite covers the example tables and the
  random entities of ADR-0210 M2, not random *tables* — the same limit that
  ADR recorded.

## Forks — open at proposal

1. **The canonwire fingerprint (SD4).** Keyed BLAKE3 in the runtime, as
   proposed; or an unkeyed hash; or none, showing only the bytes' length.
2. **Detail cost (SD5).** Computed on first draw of a selected row, as
   proposed (the card already pays that cost per frame); or behind an explicit
   "compute" button.
3. **Table placement (SD7).** The QOC's O1; O3's tooltip is the cheaper cut
   if the column-model change is judged too wide.
4. **Widget home (SD6).** `widgets/cbordiag` over `cbor/diag`, with the CLI
   flag; or the widget alone, without touching the CLI.
5. **Persistence of the toggle.** Session-only, as proposed; or in the
   launch config beside the other options-bar state.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0201](./0201-leeway-canonical-record-form.md) — canonform, the content identity.
- [ADR-0210](./0210-leeway-canonical-wire-generator.md) — canonwire, the lossless wire and its runtime.
- [ADR-0075](./0075-leeway-typed-component-views.md) — the Detail pane's typed component block, the pattern SD5 follows.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the Table pane's leeway options bar.
- [ADR-0151](./0151-table-column-width-overrides.md) — column width identity.
- [ADR-0186](./0186-play-gloss-catalog.md) — the Detail and Table rendering seams.
- [ADR-0209](./0209-pushout-cbor-identity-and-wire.md) — the `cbor diagnostics` CLI this ADR extends.
- RFC 8949 §8 (diagnostic notation), Appendix A (examples).
