---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to diagnose a Leeway read-access type mismatch

You loaded an Arrow record through generated Leeway read access and got:

```
unexpected data type for column 0 "country": got STRING (utf8), want UINT64: unexpected arrow data type
```

This recipe explains what the message tells you and the two causes worth
checking, in order of likelihood.

## Reading the message

- **`column 0 "country"`** — the position the loader read, and the name the
  record's schema gives it. The name is the useful half.
- **`got STRING (utf8)`** — what the column actually is. Both sides are named by
  their `arrow.Type` so they compare directly; the parenthesised full type
  carries a list's element type, which is often the part that is wrong.
- **`want UINT64`** — what the generated read access expects *at that position*.

The error wraps `runtime.ErrUnexpectedArrowDataType`, so `errors.Is` still
matches it.

## Cause 1: the projection is not a plain `SELECT *`

**This is almost always it.** Generated read access binds columns **by
position**, not by name — it assumes the record is the table's own column
layout. Any expression placed before the table's columns shifts every one of
them:

```sql
-- breaks: `country` takes slot 0, every facts11 column shifts by one
SELECT upper(...) AS country, * FROM dspl.facts11

-- works: the table's columns keep slots 0…n, the extra lands at the end
SELECT *, upper(...) AS country FROM dspl.facts11
```

The message names the offender directly: seeing `"country"` in slot 0 when a
`u64` id was expected says a column was prepended.

Consumers that decorate a row for display (a map column, a computed label)
should **append**. Anything picking that column up — a widget locating it by
name, a table rendering every column — is unaffected by where it sits.

## Cause 2: the table's schema drifted from the generated model

If the projection is a plain `SELECT *` and the types still disagree, the table
no longer matches the DDL the read access was generated from. A column whose
ClickHouse type changed comes back as a different Arrow type — a `DateTime`
reads as `uint32` where a `DateTime64` reads as `timestamp`. Re-provision the
table from the current DDL, or regenerate the read access against the table.

## Why the diagnosis is in the message

`eb` structured fields reach log sinks, not `Error()`. Read access is surfaced
through GUIs and CLIs that render `Error()` and have no log sink in the loop —
the facts viewer's detail pane prints it into a panel — and `eb` exposes no way
to read the fields back, so a consumer cannot reconstruct them. A message
without them is a bare "unexpected data type" naming neither the column nor
what was wrong with it, which is not actionable from the surface it appears on.

## Why this lives in the runtime and not the caller

The check, the expected type and the record's schema all exist only inside
`LoadScalarValueFieldFromRecord` and its siblings. A caller sees a finished
`error` and cannot recover the operands. Formatting it at the point of failure
is the only place with the information, and it fixes every consumer at once
rather than repeating the same reconstruction in each.
