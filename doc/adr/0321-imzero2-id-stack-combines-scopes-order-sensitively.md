---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-13
---

# ADR-0321: The imzero2 id stack combines a scope and its local id order-sensitively

## Context

`WidgetIdStack.Derive` combined the enclosing scope (the stack top, or the
base salt) with the prepared local id by plain XOR:

```go
id = ensureNotZeroId(last ^ id)
```

XOR is commutative and self-inverse, and both properties leak into widget
identity:

- `IdScope(a){W(b)}` and `IdScope(b){W(a)}` derive one id. Any widget keyed
  by two ordinals collides with the one whose ordinals trade places.
- `IdScope(x){W(x)}` cancels to the scope's parent.

A collision does not break rendering. It breaks response read-back: `Sync`
compacts duplicate r7 keys newest-wins, so the earlier widget reads
`NilResponseFlags`, and `checkId` only logs "id has already been used".

The bug class recurred, each time fixed locally:

- `tree.paddedCell` and `tree.disclose` (ADR-0267 W5) — node scope around a
  column seq, and a node scope under a cell keyed by the node.
- `windowhostInstanceSalt` — a salt of `PrepareSeq(key)` aliased window 1
  tab 2 with window 2 tab 1.
- `fsbrowser.paddedCell` — `IdScope(Seq(ord)){Frame(Seq(col))}`; a filtered
  list in codescope (3 built-in plus 4 host columns) logged duplicates on
  every frame.

The skill doc warned about the algebra, but a warning every widget author
must remember is not a fix. The stack already stores each pushed id and pops
by value, so nothing relies on XOR being self-inverse; the combination is
free to change.

## Decision

### SD1 — Scramble the scope, then XOR

```go
func combineScopeId(scope, local uint64) uint64 {
	if scope != 0 {
		scope = splitmix64.Forward(scope)
	}
	return scope ^ local
}
```

`Derive` calls it and keeps `ensureNotZeroId` on the result.

- **Order-sensitive.** `F(F(s)^a)^b` and `F(F(s)^b)^a` are unrelated; a
  self-keyed scope no longer cancels. Collisions across scopes are left to
  64-bit chance.
- **Injective within one scope.** For a fixed scope the map is XOR with a
  constant, a bijection — the property the read-back path rests on holds as
  before.
- **Root unchanged.** The empty, unsalted scope is zero and is not
  scrambled, so a relative id at the root equals the absolute id of the same
  value. Pushed scopes are never zero.
- **Cheap.** One splitmix64 step per derive.

### SD2 — Local workarounds stay

The `<<32` packing in `tree.paddedCell` and `fsbrowser.paddedCell` and the
spread salt in `windowhostInstanceSalt` remain correct and are kept; their
comments no longer cite the XOR algebra.

## Alternatives

- **Keep XOR, enforce packing by review or lint.** Rejected: the bug recurred
  under exactly that regime; a lint cannot see that two ordinals may trade
  places.
- **Hash the whole path (`xxh3(scope‖local)`).** Equivalent collision
  behaviour, but loses exact injectivity within a scope, which
  `TestIdDerivationIsInjective` pins for caller-supplied ids.

## Consequences

### Positive

- The commuting and cancelling collision class is gone for every widget,
  present and future.

### Negative

- Every id derived under a non-empty scope changes once. egui memory that
  outlives a process (persisted scroll, collapse, column widths keyed by id)
  resets one time.
- Tests asserting the XOR relation directly needed updating.

### Neutral

- Real duplicates — one widget drawn twice in a frame, two rows sharing a
  key — still collide, as they must.

## Verification plan — Tier 1

- `TestScopedIdsAreOrderSensitive`: 64×64 `Seq(a)/Seq(b)` pairs under one
  scope derive distinct ids, and none with `a == b` cancels to the parent.
- `TestRootRelativeIdEqualsAbsolute`: the unsalted root leaves ids as is.
- `TestWidgetIdStackBaseSalt`, `TestDerivedIdIsNeverZero` updated to the new
  combination.

## Status

Accepted 2026-10-13.
