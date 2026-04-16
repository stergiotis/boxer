---
type: explanation
audience: leeway maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified against the current documentation standard; migrated from `KnownIssues.md`. Do not cite as authoritative.

# Leeway Stream Readaccess Driver — Design Notes

## The Leeway Data Model

Leeway represents semi-structured data in a columnar layout. An entity (row) contains two kinds of data:

**Plain values** are scalar or non-scalar attributes with a fixed role: entity ID, timestamp, routing, lifecycle, transaction, or opaque. They map 1:1 to the entity row.

**Tagged values** are grouped into **sections** by canonical type (e.g. `float64`, `string`, `bool`). Each section contains zero or more **attributes** per entity. An attribute has value columns (scalar, homogenous array, or set) and **memberships** — tag-like metadata associating the value with logical paths or labels. Memberships come in variants: low/high cardinality, verbatim/reference/parametrized, and mixed combinations.

**Co-sections** share the same topology (attribute count per entity) and allow splitting values and memberships across sections without duplication.

The schema is captured in `IntermediateTableRepresentation` (IR), which organizes columns into sections and sub-groups (scalar, array, array-support, set, set-support, membership, membership-support).

## The Arrow Layout

The IR maps to an Arrow RecordBatch where:

- Plain scalar values are top-level columns.
- Plain non-scalar values are `List<X>` columns.
- All tagged value columns are `List<X>`. For entity row `i`, `list.ValueOffsets(i)` gives the range of attributes. For non-scalar tagged values, a cardinality support column (`List<Uint64>`) partitions the inner array into variable-length chunks per attribute.
- Membership columns follow the same `List<X>` pattern, with membership cardinality support columns enabling multiple memberships per attribute.

## The Driver's Job

The driver walks an Arrow RecordBatch and drives an `SinkI` — a SAX-like interface with calls like `BeginEntity`, `BeginSection`, `BeginTaggedValue`, `BeginColumn`, `WriteString`, `AddMembershipRef`, etc.

The sink expects **attribute-major order** for tagged sections: all columns for attribute 0, then all columns for attribute 1, etc. The IR and Arrow layout are **column-major**. This impedance mismatch is the fundamental reason the driver must collect per-section column metadata before driving.

## What We Learned

### The iterator is for building, not for driving

`IntermediateTableRepresentation.IterateColumnProps()` yields `(IntermediateColumnContext, *IntermediateColumnProps)` pairs in the correct sequential order with precomputed `cc.IndexOffset`. This is ideal for populating per-section index structs in `prepare()`, replacing manual offset tracking. However, it cannot be used at drive time because the sink requires attribute-major traversal while the iterator is column-major.

### Per-section index structs are necessary

The driver needs random access by section and sub-group to emit attributes. Attempts to eliminate the index structs — by reading directly from the IR at drive time, or by driving from the iterator — either reintroduced the same complexity in a different form or hit the column-major vs attribute-major mismatch.

### `PhysicalColumnDesc.String()` produces the physical column name

When resolving IR columns to Arrow columns by name, `PhysicalColumnDesc.String()` returns the physical column name as it appears in the Arrow schema. There is no need to manually join `NameComponents` with a separator.

### Precomputing static data pays off

The `names`/`types` slices passed to `BeginPlainSection` and `BeginSection` are invariant across entities. Precomputing them once in `prepare()` eliminates `N × S` slice allocations from the hot loop (N entities × S sections).

### `IntermediateColumnProps` slices are co-arrays

`Names`, `Roles`, `CanonicalType`, `EncodingHints`, and `ValueSemantics` within an `IntermediateColumnProps` are always the same length. Boundary checks like `if j < len(cp.Roles)` are unnecessary when `j` is valid for `Names`.

### Two preparation paths serve different use cases

- **Dense path** (`prepare`): assumes IR column order = Arrow column order. Uses `cc.IndexOffset + j` directly. Simple, fast, correct when the RecordBatch is produced from the same table definition.
- **Schema-resolved path** (`prepareFromSchema`): maps IR columns to Arrow columns by physical name via `NamingConventionFwdI.MapIntermediateToPhysicalColumns()`. Handles reordered, sparse, or subsetted RecordBatches. Requires a naming convention and the Arrow schema at construction time.

Both paths produce identical layout structs and share all driving code.

## Known Issues

### Silent skipping of missing columns in the schema-resolved path

When `prepareFromSchema` cannot find a physical column in the Arrow schema, it skips the column (does not add it to the layout). This is correct for value columns (vertical subsetting). However, if a **cardinality support column** is missing, the driver will see an empty `arrayCardCols`/`setCardCols`/`memberCardDetails` and fall back to treating each value as a single element — producing silently wrong results rather than an error.

### No validation that co-section topology is consistent

The driver assumes all sections in a co-group have the same attribute count per entity. If the Arrow data violates this (e.g. due to a bug in the ingestion pipeline), the driver will read out-of-bounds or produce misaligned output without any error.

### `sectionAttrCount` probes columns in a fixed priority order

The attribute count for a tagged section is determined by reading list offsets from the first available column, probed in order: scalar, array-card-support, set-card-support, membership-support, membership. If the schema-resolved path skips the column that would normally be probed first, a different column is used. This should produce the same result (all columns in a section have the same entity-level list length), but the assumption is not validated.

### `findMemberCardCol` uses string concatenation for role matching

The membership cardinality column for a given role is found by appending `"card"` to the role string (e.g. `"lr"` → `"lrcard"`). This relies on the convention that cardinality role strings are always `<membershipRole> + "card"`. If the `ColumnRoleE` constants change this pattern, the matching breaks silently.

### The `emitOneMembership` switch does not handle all mixed membership parameter pairing

For `ColumnRoleMixedLowCardRef` and `ColumnRoleMixedLowCardVerbatim`, the driver emits the low-card part with empty parameter strings. The corresponding high-card parameter columns (`ColumnRoleMixedRefHighCardParameters`, `ColumnRoleMixedVerbatimHighCardParameters`) are emitted separately, also with empty counterparts. The sink receives two half-populated calls rather than one unified call. Whether the sink correctly pairs them depends on its implementation.

### Per-entity allocations remain in `sectionColumnNamesTypes`-adjacent code

While the `names`/`types` slices for `BeginSection` are precomputed, the `PhysicalColumnAddr` struct is constructed inline per column per entity. This is a value type (no heap allocation), but the `FullColumnName` field requires `rec.ColumnName(idx)` which may allocate depending on the Arrow implementation.