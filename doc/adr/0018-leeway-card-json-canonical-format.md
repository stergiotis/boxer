---
type: adr
status: proposed
date: 2026-05-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0018: Canonical Card-JSON Format and JsonCardEmitter Rewrite Plan

## Context

`JsonCardEmitter` ([`../../public/semistructured/leeway/card/leeway_card_json.go`](../../public/semistructured/leeway/card/leeway_card_json.go)) is Leeway's canonical lossless JSON serialization. [ADR-0007](0007-leeway-membership-role-classifier.md) introduces the `membershiprole.ClassifierI` abstraction that decides primary versus secondary memberships at value level, but pins no JSON shape. Today's `JsonCardEmitter` is section-centric, repeats per-section schema on every entity, stringifies all scalar values, and does not consume the classifier at all. The format is lossless in principle but verbose in practice, validator-hostile (stringified numbers defeat `type: integer`), and not yet isomorphic (no parser exists).

This ADR pins the canonical JSON shape and the implementation plan to land it. The shape consumes `membershiprole.ClassifierI` to drive the primary/secondary split, separates schema from data into two artifacts (schema document + data document), and uses an attribute-centric per-entity layout rooted at primary memberships. The plan is staged so each milestone is independently shippable and the final artifact is reviewable against the existing `card_anchor_integration3_test.go` fixtures.

Forces the format must respect:

- **Lossless and isomorphic.** The format must round-trip Leeway state through a future parser (a named follow-on, below). Multi-membership aliasing, co-section topology, set-vs-array distinction, ragged `value-card` tensors, and the membership graph all need representation.
- **Human readable.** Standard JSON tooling — `jq`, JSON Path, JSON Schema validators, browser dev tools — should read the format productively without leeway-specific knowledge for the common case.
- **Data-contract amenable.** JSON Schema and data-contract generators consume the format directly; primary memberships map to `properties` + `required`, secondary memberships map to a scoped extension or `additionalProperties`. Stringified scalars are a non-starter.
- **Streaming friendly.** NDJSON-shaped output (one entity per line) is mandatory for batch streaming validators and Kafka-like transports.
- **Byte-deterministic.** Two emitters running on the same input produce identical bytes. Sort orders, key orders, and value formatting are pinned.
- **Backward compatible during cutover.** Existing fixtures and consumers depend on today's emitter output; the migration must be staged so each step has a comparable artifact.

## Decision

We adopt a two-document, attribute-centric canonical format and rewrite `JsonCardEmitter` in stages.

### Two documents

- **Schema document** — one per `TableDesc`, captures section structure, column types, allowed memberships, role declarations, and use-aspects. Fingerprint-addressed (blake3 over canonicalized bytes, fingerprint field excluded from the hash domain).
- **Data document** — one per `RecordBatch`, references the schema by fingerprint and emits per-entity records.

The two are independently consumable. The schema document is the natural input for a downstream schema or data-contract generator; the data document is the wire form a streaming consumer reads.

### Per-entity layout

Two subtrees inside each entity object:

- `byStructure` — section cardinalities, co-group topology, plain values. Schema-shape view; cheap to compute, cheap to validate.
- `byAttribute` — attribute-centric, keyed by primary memberships. Authoritative; the parser reconstructs leeway state from it. Secondary memberships ride as a per-attribute `labels` slot. Multi-primary aliasing collapses into a single key with an `aliases` field.

The previously-explored third subtree (`byMembership`) is dropped per [ADR-0007](0007-leeway-membership-role-classifier.md) SD10 — once primary memberships are declared, the primary *is* the attribute key, so a third subtree adds no information.

### Schema document shape

```json
{
  "leewayCardSchema": "1",
  "fingerprint": "blake3:…",
  "plainSections": [
    {
      "itemType": "entityId",
      "columns": [
        {"name": "blake3hash", "type": "y", "valueSemantics": ["idContentAddressableKey"]}
      ]
    },
    {
      "itemType": "timestamp",
      "columns": [{"name": "ts", "type": "i64l"}]
    }
  ],
  "taggedSections": [
    {
      "name": "string",
      "columns": [{"name": "value", "type": "s", "valueSemantics": ["scaleOfMeasurementNominal"]}],
      "memberships": [
        {"spec": "low-card-verbatim-high-card-params", "role": "primary", "paramTreatment": "identity"}
      ],
      "useAspects": ["humanReadable"]
    },
    {
      "name": "null",
      "columns": [],
      "memberships": [
        {"spec": "low-card-verbatim-high-card-params", "role": "primary", "paramTreatment": "identity"}
      ],
      "useAspects": ["sectionMembershipsAllPrimary"]
    },
    {
      "name": "null__labels",
      "columns": [],
      "memberships": [{"spec": "low-card-verbatim", "role": "secondary", "paramTreatment": "none"}],
      "useAspects": ["sectionMembershipsAllSecondary"]
    }
  ],
  "coSectionGroups": [
    {"key": "geo", "sections": ["latLng", "h3"]},
    {"key": "null__labelOverlay", "sections": ["null", "null__labels"]}
  ]
}
```

Notes:
- **Encoding aspects are absent by default** (materialization choices, not interface concerns). `--include-encoding` flag opts in for debugging.
- **Membership roles** are recorded per-spec when known (uniformity hints declared, custom classifier marked the spec). When the classifier decides per-membership, the schema document still emits the spec without a `role` field; consumers run the classifier inline.
- **Order**: `plainSections` follow `PlainItemTypeE` enum order; `taggedSections` ordered by section name (lexicographic StylableName); columns by IR order; `coSectionGroups` by `naming.Key`.
- **Fingerprint**: blake3 of the canonicalized JSON with the `fingerprint` field set to the empty string. Stable under re-emission.

### Data document shape

```json
{
  "leewayCardData": "1",
  "schemaFingerprint": "blake3:…",
  "entities": [
    {
      "entityId": "hash-001",
      "byStructure": {
        "plain": [
          {"itemType": "entityId",  "values": {"blake3hash": "hash-001"}},
          {"itemType": "timestamp", "values": {"ts": 1735689600000000000}}
        ],
        "sections": [
          {"name": "string",  "nAttrs": 1},
          {"name": "symbol",  "nAttrs": 2},
          {"name": "float64", "nAttrs": 1},
          {"name": "int64",   "nAttrs": 1},
          {"name": "null",    "nAttrs": 1},
          {"name": "bool",    "nAttrs": 1}
        ],
        "coGroups": []
      },
      "byAttribute": {
        "/active":          {"section": "bool",    "scalar": true},
        "/hostname":        {"section": "string",  "scalar": "server-alpha"},
        "/metrics/cpu":     {"section": "float64", "scalar": 45.5},
        "/metrics/error":   {"section": "null",    "labels": [{"name": "errormsg"}]},
        "/metrics/mem":     {"section": "int64",   "scalar": 80},
        "/tags/0":          {"section": "symbol",  "scalar": "production"},
        "/tags/1":          {"section": "symbol",  "scalar": "eu-west"}
      }
    }
  ]
}
```

The seven attributes are enumerated under `byAttribute` keyed by the *resolved* primary path; `/tags/_` with params `[0]` becomes `/tags/0`, etc.

### `byAttribute` value-shape rules

The shape inside each attribute object is selected by section schema and classifier output:

| Section shape | Param treatment | Field | Value JSON shape |
|---|---|---|---|
| Single value column, scalar canonical type | n/a | `scalar` | JSON-native scalar |
| Single value column, `h` (homogenous-array) canonical type | n/a | `value` | JSON array |
| Single value column, `m` (set) canonical type | n/a | `value` | `{"set": [...]}` |
| Multiple value columns | n/a | `values` | `{col: <native>, ...}` |
| Single value column + `paramTreatment: index` | Index | `indexed` | `[{"params": [...], "value": <native>}, ...]` |
| Value-less section (e.g. `null`) | any | (absent) | — |

Multi-membership aliasing adds an `aliases` field:

```json
"/price/current": {
  "section": "float64",
  "scalar": 19.99,
  "aliases": ["/promo/flash_sale", "/stats/min"]
}
```

The key is the lexicographically smallest primary path; `aliases` lists the others, sorted lexicographically.

Co-grouped multi-primary attributes (multiple sections each carry value columns for one logical attribute) use a different shape:

```json
"/location": {
  "coGroup": "geo",
  "byCoSection": {
    "h3":     {"scalar": "8a2a1072b59ffff"},
    "latLng": {"values": {"lat": 48.13, "lng": 11.58}}
  }
}
```

`coGroup` names the group; `byCoSection` keys per-section payloads (each payload follows the table above, minus `aliases` / `labels`). Section keys sorted lexicographically.

### Secondary memberships and the `labels` slot

Secondary memberships from the same section *or* from a co-section with `AspectSectionMembershipsAllSecondary` are folded into a `labels` slot on the matching primary attribute:

```json
"/metrics/error": {
  "section": "null",
  "labels": [
    {"name": "errormsg"},
    {"name": "severity", "params": "warn"}
  ]
}
```

Each label is an object with `name` and optional `params` (string). Object form is uniform whether the secondary kind has parameters or not; sort by `name` then `params`.

### Encoding rules for value content

Primary aim: emit JSON-native types so standard JSON Schema validators cooperate. Quote-encode only when JSON cannot represent the value losslessly.

| Canonical type | JSON shape |
|---|---|
| `b` (bool) | JSON `true` / `false` |
| `i8` … `i32`, `u8` … `u32` | JSON number |
| `i64`, `u64` (within ±2^53) | JSON number |
| `i64`, `u64` (outside ±2^53) | JSON string |
| `f32`, `f64` | JSON number; `NaN` / `Inf` / `-Inf` as JSON string sentinels (`"NaN"`, `"+Inf"`, `"-Inf"`) |
| `s` (UTF-8 string) | JSON string |
| `y` (byte blob) | base64-url JSON string |
| `b` with width (bit string) | JSON string of `01` digits |
| `f` (fixed-width) | JSON string (hex or base64-url depending on aspect) |
| `z` / `d` / `t` (temporal) | JSON string in ISO-8601 form; epoch i64 if `valueSemantics` requires |
| `v` / `w` (network) | JSON string in canonical address form |
| `h` (homogenous array) | JSON array of the element shape |
| `m` (set) | `{"set": [...]}` with the array sorted in canonical bytes |

**Null section** carries no value column; the attribute object simply omits `scalar` / `value` / `values`.

**Empty homogenous array** vs **null section**: distinct. Empty arrays appear as `"value": []`; null is the value-less attribute.

**Empty-object / empty-array singleton sections** (per leeway-advanced lossless mapping): emitted with `"section": "emptyObject"` (or `emptyArray`) and no value field.

### Determinism rules

- `byAttribute` keys: lexicographic order over the resolved primary path string.
- `byStructure.sections`: schema order (matches schema document).
- `byStructure.coGroups`: lexicographic by group key.
- `aliases`: lexicographic.
- `labels`: sorted by `name`, ties broken by `params`.
- `byCoSection`: lexicographic by section name.
- Object key order follows the field order in the spec tables above; serializer must emit in the listed sequence so byte equality holds across runs.
- Number formatting: shortest round-trippable decimal for floats (Go default `%g` is acceptable when it round-trips); integers as `%d`; trailing zeros stripped.
- String escaping: minimal — only escape what JSON requires (`"`, `\`, control chars). UTF-8 stays UTF-8.

### NDJSON mode

Two output modes, selectable by emitter flag:

- **Batch object** (default) — one root object per file: `{"leewayCardData": "1", "schemaFingerprint": "...", "entities": [...]}`. Suitable for human reading, file artifacts, REST responses.
- **NDJSON** — first line is a header object `{"leewayCardData": "1", "schemaFingerprint": "..."}`; each subsequent line is one entity object (no trailing comma). Suitable for streaming, line-oriented validators, Kafka transport.

The two are deterministically interconvertible: an NDJSON stream is the batch-object's `entities` array unrolled.

### Subsidiary design decisions

- **SD1 — `byAttribute` is authoritative; `byStructure` is a derived projection.** Round-trip parsers read `byAttribute` and recompute `byStructure`. Emitters produce both. Inconsistencies surface as warnings, not protocol violations.

- **SD2 — Path resolution embeds params for `paramTreatmentIdentity`, leaves them in `indexed` for `paramTreatmentIndex`.** A primary `/users/_/email` membership with params `["abc-123"]` becomes the key `/users/abc-123/email`. A primary `/measurements/_` with `paramTreatmentIndex` and params `[0]` stays at key `/measurements/_` with an `indexed` entry.

- **SD3 — Labels carry their own params.** When a secondary membership has params (e.g. `severity` with text `"warn"`), the label object emits `{"name": "severity", "params": "warn"}`. Param treatment for secondaries is irrelevant for shape (always object form).

- **SD4 — Co-grouped attributes are recognized by classifier output, not by schema declaration.** When two co-grouped sections both contribute primary memberships at the same attribute index, the emitter folds them into a `coGroup` / `byCoSection` shape. When only one section contributes (the other is `AllSecondary`), the secondary's memberships fold into the primary's `labels` and the result emits with the normal single-section shape.

- **SD5 — Schema fingerprint is content-addressable, not version-bumped.** Two schemas with identical structure produce identical fingerprints regardless of the `TableDesc`'s authoring path. The fingerprint is the primary identity for caching, registry lookup, and data-contract addressing.

- **SD6 — Stringified scalars are a one-way migration.** Today's emitter quotes everything; the new emitter does not. The `JsonCardEmitterV2` lands behind a flag (`--stringify-scalars`) defaulting to false. The flag exists to bridge consumers that hard-coded the old shape; it is deleted once all consumers have migrated.

- **SD7 — Existing card emitters (`UnicodeCardEmitter`, `HtmlCardEmitter`, `SvgCardEmitter`, `ImZero2CardEmitter`, `TypstCardEmitter`) are unaffected by this ADR.** They continue to operate against the existing `streamreadaccess.SinkI` protocol; consuming the classifier is a per-emitter follow-on at each maintainer's discretion. Only `JsonCardEmitter` is rewritten here.

- **SD8 — Schema-document writer is a separate `SinkI` implementation.** It rides the same SAX protocol but emits the schema artifact only — no entity bodies. The driver gains `Driver.DriveSchema(sink)` for this pass; the existing `DriveRecordBatch` is untouched.

- **SD9 — `nAttrs == 0` sections appear in `byStructure.sections` but not in `byAttribute`.** Round-trip preserves this distinction (empty section is meaningful) without bloating `byAttribute`.

- **SD10 — Driver order is the wire order for `byAttribute` *during emission*; the JSON reader sees the lexicographic order.** The emitter buffers per entity, sorts attributes at `EndEntity`, flushes. This is the cost of attribute-centric layout under a column-major driver; bounded by entity size.

## Implementation plan

Eight milestones, each independently shippable. Round-trip and golden-file tests gate each transition.

### M1 — Bump boxer pin in pebble2impl

`go get -u github.com/stergiotis/boxer` to pick up boxer commits `699e0a1` (membershiprole + use-aspects) and `ec97676` (`AspectSet.Contains`). Re-run `scripts/ci/lint.sh` per `../../CLAUDE.md` ("Bumping boxer"). No code changes downstream of the bump in M1; this is the prerequisite for M2.

**Done when:** `go build ./...` succeeds with the new pin; `useaspects.AspectSectionMembershipsAllPrimary` resolves; `membershiprole.DefaultClassifier{}` is callable from pebble2impl.

### M2 — `JsonCardEmitterV2` skeleton, classifier wiring

Add `leeway_card_jsonv2.go` next to today's `leeway_card_json.go`. The new emitter takes a `membershiprole.ClassifierI` (default `DefaultClassifier{}`) and an `enc *jsontext.Encoder`. Initial scope: per-entity buffer, classifier invocation per `AddMembership*`, stub `byStructure` / `byAttribute` writers that produce minimal valid output.

Old `JsonCardEmitter` stays in place, unchanged. Both register as valid `streamreadaccess.SinkI` implementations.

**Done when:** `JsonCardEmitterV2` emits *something* for the canonical leeway-advanced fixture; existing tests against `JsonCardEmitter` remain green.

### M3 — Type-aware scalar emission

Replace today's `Write` / `WriteString` blanket-string fallback with a canonical-type dispatch (per the encoding-rules table in this ADR). Numbers, bools, nulls land as JSON-native; strings/blobs/temporals quote-encode.

Test: encode a 7-attribute fixture (one per scalar canonical type), assert byte-equality against a golden file.

**Done when:** scalar-typed columns round-trip through V2 with native JSON types; golden file reviewed.

### M4 — `byStructure` + `byAttribute` skeleton with primary keys

Implement `byStructure` (plain section dump, section nAttrs, co-group enumeration) and `byAttribute` keyed by resolved primary paths (single-membership case). Multi-column sections, scalar-vs-array dispatch, and section-name pinning land here.

Test: canonical leeway-advanced fixture round-trips byte-equal to a golden file; key order is lexicographic.

**Done when:** the canonical example in this ADR's "Data document shape" reproduces from the V2 emitter on the `card_anchor_integration3_test.go` fixtures (with classifier-mapped sections).

### M5 — Multi-membership aliasing + `labels` slot

Wire the classifier's `Secondary` output into a `labels` accumulator on the matching primary attribute. Wire multi-`Primary` into the `aliases` field, picking the lexicographically smallest path as the canonical key.

Test: a fixture with `/price/current` ≡ `/stats/min` ≡ `/promo/flash_sale` produces one attribute object with two-element `aliases`; a `null` section attribute with an `errormsg` secondary co-section emits `labels: [{"name": "errormsg"}]`.

**Done when:** both fixtures round-trip; secondary co-section pattern documented in `doc/skills/leeway-advanced/SKILLS.md` exercises this path.

### M6 — Co-grouped attributes, sets, ragged tensors

Co-grouped multi-primary attributes emit `coGroup` + `byCoSection`. Set-typed value columns wrap as `{"set": [...]}`. Homogenous-array sections emit native JSON arrays per logical value; ragged tensors via `value-card` emit one array per attribute (length = `value-card[i]`).

Test: fixtures for lat/lng + h3 (co-group), a string-set attribute, a ragged 1024-dim embedding section.

**Done when:** all three corner cases round-trip; emitter passes lint + race tests.

### M7 — Schema document writer ✅ DONE

New `JsonCardSchemaEmitter` (sink) plus `Driver.DriveSchema(sink)` extension on the boxer side. The schema emitter walks `IntermediateTableRepresentation` once per `TableDesc` and emits the schema document per the spec above. Fingerprint computed from canonicalized output.

Two output modes wired: schema-as-sidecar (one file per table) and schema-as-header (first NDJSON line). Default: sidecar for batch-object mode, header for NDJSON mode.

**Done when:** every emitted data document is valid against its schema document; the fingerprint round-trips through emit-then-read; a downstream contract or schema generator can consume the schema doc directly.

Landed across boxer commits `f66b86c` (SinkI carries `useaspects.AspectSet` on `BeginSection`) + `606af4a` (`Driver.DriveSchema`); pebble2impl commits `835526dd` (drop IR shim, adopt new SinkI signature, add `WithSchemaFingerprint`/`WithSchemaDocument`) + `e4f0fee5` (`JsonCardSchemaEmitter` + tests + anchor `TestCardE2eSchema` integration + gold regen).

### M8 — NDJSON mode + cutover

Add `--ndjson` flag that suppresses the outer batch-object wrapper, emits the schema header as line 1, and writes one entity object per subsequent line. Validate against a streaming JSON validator (`gojsonschema`).

Cutover: rename `JsonCardEmitterV2` → `JsonCardEmitter`, retire the old emitter, update all consumers (`cli/lw_cmd_card.go`, `proxy/clickhouse_grafana/transformer/`, `anchor/card_anchor_integration3_test.go`). Update [`../skills/leeway-streamreadaccess/SKILLS.md`](../skills/leeway-streamreadaccess/SKILLS.md) reference table.

**Done when:** old emitter removed; NDJSON streaming validates with stock tooling.

### Out of scope for this ADR (named follow-ons)

- **Card-JSON parser** — separate ADR. The format defined here is the parser's input contract.
- **Other card emitter cutovers** — Unicode/HTML/SVG/Typst/ImZero2 each cut over on their own schedule.
- **Downstream schema / data-contract generator** — separate deliverable consuming the schema document.
- **Mining drift report** — separate offline tool, never modifies the wire form.

## Worked examples

### Example 1 — canonical leeway-advanced entity

Input:
```json
{"hostname": "server-alpha",
 "tags": ["production", "eu-west"],
 "metrics": {"cpu": 45.5, "mem": 80, "error": null},
 "active": true}
```

Schema (excerpt; full schema document elsewhere in this ADR):
- `string`, `symbol`, `float64`, `int64`, `null`, `bool` — all primary, mixed-low-card-verbatim-high-card-params.
- Entity ID + timestamp in plain sections.

Data:
```json
{
  "entityId": "hash-001",
  "byStructure": {
    "plain": [
      {"itemType": "entityId",  "values": {"blake3hash": "hash-001"}},
      {"itemType": "timestamp", "values": {"ts": 1735689600000000000}}
    ],
    "sections": [
      {"name": "bool",    "nAttrs": 1},
      {"name": "float64", "nAttrs": 1},
      {"name": "int64",   "nAttrs": 1},
      {"name": "null",    "nAttrs": 1},
      {"name": "string",  "nAttrs": 1},
      {"name": "symbol",  "nAttrs": 2}
    ],
    "coGroups": []
  },
  "byAttribute": {
    "/active":        {"section": "bool",    "scalar": true},
    "/hostname":      {"section": "string",  "scalar": "server-alpha"},
    "/metrics/cpu":   {"section": "float64", "scalar": 45.5},
    "/metrics/error": {"section": "null"},
    "/metrics/mem":   {"section": "int64",   "scalar": 80},
    "/tags/0":        {"section": "symbol",  "scalar": "production"},
    "/tags/1":        {"section": "symbol",  "scalar": "eu-west"}
  }
}
```

### Example 2 — multi-membership aliasing

Schema declares `/price/current`, `/stats/min`, `/promo/flash_sale` all as primary memberships in the `float64` section. The same value `19.99` carries all three.

```json
"byAttribute": {
  "/price/current": {
    "section": "float64",
    "scalar": 19.99,
    "aliases": ["/promo/flash_sale", "/stats/min"]
  }
}
```

### Example 3 — secondary co-section labeling

Schema: primary section `null` + co-grouped secondary section `null__labels` with `AllSecondary` use-aspect.

```json
"byAttribute": {
  "/metrics/error": {
    "section": "null",
    "labels": [{"name": "errormsg"}, {"name": "severity", "params": "warn"}]
  }
}
```

### Example 4 — co-grouped multi-primary (lat/lng + h3)

Co-group `geo` contains primary sections `latLng` (two value columns) and `h3` (one value column).

```json
"byAttribute": {
  "/location": {
    "coGroup": "geo",
    "byCoSection": {
      "h3":     {"scalar": "8a2a1072b59ffff"},
      "latLng": {"values": {"lat": 48.13, "lng": 11.58}}
    }
  }
}
```

### Example 5 — homogenous-array section (embedding)

Schema: section `float64array` with canonical type `f64h` (homogenous array), single value column, `value-card` partitions per attribute.

```json
"byAttribute": {
  "/embedding": {"section": "float64array", "value": [0.1, 0.5, 0.9]}
}
```

### Example 6 — `paramTreatmentIndex` (dimensional measurements)

Schema: section `float64` (no `h`), one membership `/measurements/_` declared as `paramTreatment: index`.

```json
"byAttribute": {
  "/measurements/_": {
    "section": "float64",
    "indexed": [
      {"params": [0], "value": 1.1},
      {"params": [1], "value": 1.2},
      {"params": [2], "value": 1.3}
    ]
  }
}
```

(Versus `paramTreatmentIdentity` for the same shape, which would yield three separate attributes keyed `/measurements/0`, `/measurements/1`, `/measurements/2`.)

## Alternatives

- **Section-centric layout retained.** Keep today's `taggedSections: [{section, attributes: [...]}, ...]` shape. Rejected as the canonical form: verbose, redundant per-entity schema, JSON-pretending-to-be-Arrow, contract-validator-hostile. Today's emitter remains until M8 cutover but is not the canonical shape.

- **Three peer subtrees (`byStructure` + `byAttribute` + `byMembership`).** Earlier in the design space ([ADR-0007](0007-leeway-membership-role-classifier.md) SD10). Rejected — once primary memberships are declared, the third subtree adds no information.

- **Single combined document (schema embedded in data).** Inline the schema on entity 0 and suppress on subsequent entities. Rejected: makes parsing position-sensitive, breaks NDJSON streaming where lines may arrive out of order, and conflates two artifacts that consumers want separately (schema consumers read the schema; quality SQL reads data).

- **Stringified-scalar legacy mode as default.** Keep today's `"scalar": "45.5"` shape for compatibility. Rejected for default; available behind `--stringify-scalars` flag through M3–M8 to bridge consumers.

- **Avro-style schema-by-position (no field names in data).** Drop the `byAttribute` keys and emit positional tuples zipped against the schema. Rejected: defeats the readability goal; schema-by-position is what Arrow already is.

## Consequences

### Positive

- **Lossless and isomorphic by construction.** Once the parser lands (a named follow-on), card-JSON round-trips Leeway state exactly.
- **Standard JSON tooling reads the format productively.** `jq '.byAttribute["/hostname"].scalar'` works out of the box; JSON Schema validators with `properties` work directly against `byAttribute`.
- **Data-contract generation reads the schema document.** The schema-doc shape mirrors typical data-contract field structure (per-section column lists, role-tagged memberships); a generator's job is mostly translation.
- **NDJSON-mode unblocks streaming validators and Kafka transport.** A producer streams entities; a consumer validates each line independently against the schema.
- **Fingerprint-addressed schema enables registries and caching.** Consumers verify they're reading the schema they expect; producers re-emit identical fingerprints from identical `TableDesc`.
- **Migration is staged.** Each milestone produces a usable artifact and the old emitter remains a fallback through M7.

### Negative

- **Cutover risk on M8.** Renaming `JsonCardEmitterV2` to `JsonCardEmitter` and retiring the old emitter touches every consumer at once. Mitigated by golden-file equivalence tests through M3–M7 and by keeping the `--stringify-scalars` flag through one release cycle past cutover.
- **`JsonCardEmitterV2` is a parallel implementation for several milestones.** Maintenance overhead — bug fixes during M2–M7 may need to land in both. Bounded by the two-phase test gate (each milestone has a golden file; bug fixes triggered by the gate only).
- **Per-entity buffering is now mandatory.** Today's streaming emitter writes tokens as they arrive; V2 buffers per entity to enable lex sort + classifier-driven role split. Memory bounded by entity size (typically ≪ MB), but no longer truly streaming at sub-entity granularity. Acceptable given that even per-entity Arrow rows already exist as buffered structures.
- **`JsonCardEmitterV2` adds a tunable surface (classifier, schema mode, stringify flag, NDJSON mode).** Default values matter; the ADR pins them but tooling/CLI documentation needs to call them out.

### Neutral

- **Today's `JsonCardEmitter` remains until M8.** Both emitters coexist; consumer choice is by configuration.
- **The schema document is fingerprint-addressed but not versioned.** Schema evolution is a separate concern (a contract-version policy, if one is later adopted); fingerprints differ when schemas differ.
- **`AspectSetEncoderConfig` and other emitter knobs (existing) carry forward unchanged.** This ADR only changes JSON-shape decisions, not encoding decisions like base62 column names.

### Derived practices

- **New consumers default to V2.** As of M8, `card.NewJsonCardEmitter(...)` instantiates V2; the old constructor is renamed `NewJsonCardEmitterLegacy` with a deprecation note pointing at this ADR.
- **Test fixtures regenerate on every milestone.** Golden files under `testdata/` for each shape; CI compares byte-equality.
- **Schema documents land in `testdata/schemas/`** keyed by fingerprint; data documents reference them by fingerprint. CI checks the cross-reference.

## Open questions

Tracked as named follow-ons:

1. **Driver-side `DriveSchema(sink)` API.** Whether it lives on `streamreadaccess.Driver` or a new `streamreadaccess.SchemaDriver`; whether the sink contract gains `Begin/EndSchema` methods or a separate `SchemaSinkI` interface. Decided in M7.
2. **JSON-Schema-of-the-format.** A meta-schema describing valid card-JSON data documents (independent of any specific `TableDesc`) is a useful CI artifact; deferred until M8.
3. **`paramTreatmentIndex` declaration channel.** Currently the classifier returns it inline; whether a per-membership annotation channel should record it explicitly (so the schema document carries the answer without re-running the classifier) is open. Defer until a real consumer needs it.
4. **Custom value-encoder hook.** Some applications want to emit `decimal128` as a typed JSON object `{"decimal": "19.99"}` rather than a string. A `ValueFormatterI`-style hook on V2 is plausible but not committed.
5. **CBOR companion format.** `TableDescDto` already has a CBOR carrier; whether a card-CBOR mirror of card-JSON is worth building is an open architectural question.

## Status

Proposed — awaiting review by repo owner.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0007](0007-leeway-membership-role-classifier.md) — membership-role classifier design.
- [`../../public/semistructured/leeway/card/leeway_card_json.go`](../../public/semistructured/leeway/card/leeway_card_json.go) — current `JsonCardEmitter`; rewrite source.
- [`../../public/semistructured/leeway/anchor/card_anchor_integration3_test.go`](../../public/semistructured/leeway/anchor/card_anchor_integration3_test.go) — existing fixtures.
- [`../skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md) §"Membership roles" — primary/secondary semantics.
- [`../skills/leeway-streamreadaccess/SKILLS.md`](../skills/leeway-streamreadaccess/SKILLS.md) §"Membership Role Classification" — classifier on the sink side.
