---
type: how-to
audience: engineer whose tagged ids live in more than one id space
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to claim one tag value in two id spaces

The task: a system mints two kinds of tagged id — say fact ids (`kid`) and
event ids (`eid`) — that live in **separate id spaces**: separate columns,
never compared, never stored in one slot without saying which kind they are.
It wants to give one minter the same tag value in both spaces, and still have
every claim checked by `identity/tagmint` for uniqueness and fit.

`tagmint` checks a tag value's uniqueness **within an id space**: a set of
columns in which ids of its claimants can meet. A claim made through
`tagmint.Claim` lives in the default space, which the vocabularies and the
runtime mint share. A claim made through `tagmint.ClaimIn` lives in the space
it names. The decision record is
[ADR-0183](../adr/0183-leeway-component-consumer-simplification.md) (D0, and
its Update of 2026-10-13).

## When to use this recipe

- Two kinds of id never share a column, and you want their tag values
  assigned independently — one registry per kind, each checked on its own.
- You want the same tag value to identify one minter in both kinds.

When ids of two kinds can meet in one column — a membership column, a
reference column holding either kind — they are one space. Stay in the
default space and claim distinct values.

## Steps

### 1. Claim a space per kind of id

```go
var (
	SpaceKid = tagmint.MustClaimSpace("kid")
	SpaceEid = tagmint.MustClaimSpace("eid")
)
```

A space name is claimed once per link set; a second `ClaimSpace` of the same
name is refused with the origin of the first. The name may not contain
whitespace or control characters, since the goldens store it as a
tab-separated field.

That the two spaces never share a column is your contract: `tagmint` cannot
see columns, the same way it takes `maxExpectedIds` on trust. Write the
columns of each space in the declaration's doc comment.

### 2. Claim tag values within a space

```go
var (
	CellAKid = tagmint.MustClaimIn(SpaceKid, "kid/cellA", 2178400, 1<<20)
	CellAEid = tagmint.MustClaimIn(SpaceEid, "eid/cellA", 2178400, 1<<24)
)
```

- One value in two different spaces is accepted.
- One value twice in one space is refused, naming the first claimant and its
  origin.
- Names are unique across all spaces, so `kid/cellA` and `eid/cellA` are two
  names for one value, and a name never needs its space to be read.
- The fit check is the same as `Claim`'s: the value's width class must hold
  `maxExpectedIds` bodies.

`ClaimIn` returns the error instead of panicking, for claims made at run time
(a cell claiming the tags it was handed at start).

### 3. Read the space back

`ClaimedTagValue.Space()` reports the space a claim was made in;
`Space.Name()` is `""` for the default space. `tagmint.IterateClaims` yields
claims of every space, so an audit over it keys by
`(c.Space().Name(), c.Value())`, not by the value alone.

### 4. Pin stream claims in a golden

A claim whose bodies are leased at run time is pinned by a row of its
package's `testdata/idstream-claims.golden`
(`assignments.WriteStreamGoldenFile` / `CompareToStreamGoldenFile`). A row of
a named space carries the space as a fifth field; a row of the default space
omits it. The union check, `assignments.CheckTagValuesDisjoint`, keys by
`(space, value)`.

## What does not change

- `tagmint.Claim` and `tagmint.MustClaim` claim in the default space.
- A natural-key registry (`namemint/registry`) refuses a claim outside the
  default space: membership ids share the facts membership columns with
  every vocabulary's, and the membership goldens record no space.
- `LW_ID_HAS_TAG` and the other `identsql` predicates work on one id
  expression, so they are per column. A tag value identifies a claimant only
  within the space of the column it is read from.

## Verify

```sh
go test -tags="$(cat ./tags)" ./public/identity/tagmint/ ./public/semistructured/leeway/namemint/...
```

`tagmint_space_test.go` covers the per-space rules;
`assignments_streams_test.go` covers the golden's space field and the union
keyed by `(space, value)`.
