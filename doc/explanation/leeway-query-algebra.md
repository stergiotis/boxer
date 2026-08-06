---
type: explanation
audience: engineer designing or reviewing leeway query tooling
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative. Decisions (substrates, packaging, naming of a shipped
> function pack) are deliberately absent here — they belong to a future ADR.

# The leeway query algebra

Leeway stores semi-structured data as flat, aligned arrays: parallel lanes
that share positions, and ragged value streams chunked by cardinality
columns. Querying that layout directly in SQL works — the idioms are
catalogued in the
[array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md) — but the
idioms alone don't say what the *complete* operation set is, which
compositions are meaningful, or why certain mistakes (zipping misaligned
lanes, querying for an empty list) are wrong rather than merely unlucky.
This page states the underlying model: a small query algebra over leeway's
two shape concepts, **co** (alignment) and **ragged** (values plus lengths).

The algebra was constructed from the data model, not distilled from a query
corpus: leeway is young, so existing queries reflect what today's tooling
happens to emit, and an algebra derived from them would inherit that
accident. The completeness claim is instead by reduction to a known-complete
calculus (see [Completeness](#completeness-and-exclusions)). Realizations
shown here were verified against a live ClickHouse 26.7; the how-to carries
the executable forms.

## Terminology

- **co** — two lanes are *co* when they are indexed by the same axis:
  position `i` in one refers to the same thing as position `i` in the other.
  Sections and their co-sections are co by construction.
- **ragged** — a collection-per-instance shape stored as one flat value
  stream plus a lengths lane (leeway's `card` roles). The three equivalent
  encodings are lengths, cumulative offsets (`cusumcard`), and per-element
  parent ids — respectively TensorFlow RaggedTensor's `row_lengths`,
  `row_splits`, and `value_rowids`; Arrow's list layout is the offsets form.
- Rejected terms: *CSR* (connotes sparse coordinates — there is no
  column-index array here) and *segmented* (overloaded).

## The data model: axes

Every lane is indexed by exactly one **axis**, and the axes of a table form
a bounded tree:

- **R** — the row axis (entities / batches; what SQL calls rows).
- **I** — one instance axis per section: the attribute instances. All
  tagged-value lanes of a section *and of its co-sections* are lanes on the
  same I.
- **E_val, E_mem** — ragged child axes of I: the flattened value stream and
  the flattened membership stream, each described by its own lengths lane.

Depth is fixed at these three levels by leeway's design; nothing in the
algebra recurses. SQL erases all of this — every lane is just `Array(T)` —
which is why the type discipline lives client-side: leeway's physical
column-naming scheme (see
[leeway-column-names](./leeway-column-names.md)) lets tooling reconstruct
the axis assignment of any lane from its name alone, including across raw
SQL that merely preserves column names. The axis system is a *gradual* type
system: unknown expressions type as unknown, and a runtime-free ascription
(an identity function the expander erases) lets a query author assert an
axis where inference cannot reach.

## The data model: planes

Orthogonal to axes, every attribute sits on a rung of a **value-structure
ladder**:

1. **value-less** — membership only, no value column (the `null` /
   `emptyArray` / `emptyObject` sections).
2. **scalar** — one value per instance; no descriptor.
3. **trivial-non-scalar** — the non-scalar representation with cardinality
   identically 1; isomorphic to scalar via a coercion pair (embedding
   upward is free; projecting downward needs `≡ 1` evidence from schema or
   a dynamic check).
4. **ragged** — general non-scalar, cardinality **at least 1**.

Two structural facts do most of the work on this ladder:

**Positivity.** An empty list is not representable in a valued section — it
is *absent* instead (the lossless JSON mapping routes empty arrays and
objects to dedicated value-less sections). The same holds on the membership
plane: an instance exists *because* it is tagged, so membership cardinality
is also at least 1. Both descriptors are therefore positive, with
consequences listed under [Refinements](#refinements).

**Orthogonality.** The value plane and the membership plane are independent
ragged children of the same I. There is *no* morphism between E_val and
E_mem: any cross-plane operation must factor through I (broadcast up,
operate, reduce down). A term that zips or gathers directly between the two
streams is ill-typed — which converts a raw-SQL runtime failure class
("arrays must have equal size", or worse, a silent coincidence of lengths)
into a static error. Parametrized memberships carry their parameters as
membership-plane payload; there is no third plane.

Emptiness questions consequently *relocate*: "attribute present but empty"
is not a descriptor state but membership in a value-less section — a
membership-plane query. Tooling that knows the model can reject the naïve
`card = 0` formulation (it matches nothing) and point at the right plane.

## Objects

- **Axes** as above, plus fresh axes created by operations.
- **Lanes** — `Lane_A(τ)`: values of canonical type τ indexed by axis A. A
  predicate is a `Lane_A(Bool)`.
- **Descriptors** — `Desc(A→B)`, positive: B is a ragged child of A;
  physically a `card` lane. Derived views: starts, ends (cumulative sums),
  `parent : B→A`, and per-run positions (iota).
- **Index maps** — `h : B→A`, consumed by gather. Four sources: permutations
  (from sorting), witnesses (from filtering), parents (from descriptors),
  and key matches (from joins).

One definition carries much of the design: a list-valued lane *is* a pair,
`Lane_A(List τ) ≜ (Desc(A→B), Lane_B(τ))`. Nesting and flattening are
thereby representation coercions, not computations — though the cost
interpretation below qualifies what that means in practice.

## Primitives

Ten primitives; everything else in this page is derived from them.

| primitive | signature | ClickHouse realization |
|---|---|---|
| `lift f` | `Lane_A(τ₁)×…→Lane_A(σ)` | scalar expressions / multi-lane `arrayMap` |
| `gather_h` | `Lane_A(τ)→Lane_B(τ)`, `h:B→A` | `arrayMap(i -> lane[i], h)` |
| `argwhere` | `Lane_A(Bool)→(A′, ι:A′↪A)` | `arrayFilter` over `arrayEnumerate` |
| `sortPerm` | `Lane_A(κ)→(A≅A)` | `arraySort(i -> key[i], arrayEnumerate(key))` |
| `mkDesc` | `Lane_A(ℕ⁺)→Desc(A→B)`, B fresh | a computed lengths lane |
| `reduce_m` | `Lane_B(τ)×Desc(A→B)→Lane_A(σ)`, m a semigroup | `arrayReduceInRanges` over `(start, len)` ranges |
| `scan_m` | per-run prefix | `arrayCumSum` today; general case open |
| `keyMatch` | `Lane_A(κ)×Lane_B(κ)→(Desc(A→E′), h:E′→B)` | per-key index harvesting (an `indicesOf` shape) |
| `promote` / `demote` | I ↔ R | `ARRAY JOIN` / `groupArray` plus an order witness |
| generators | iota, const | `range`, `arrayEnumerate`, `arrayWithConstant` |

Notes: `gather` is the *single* reindexing operation — permute, select,
broadcast, and duplicate differ only in where `h` came from. `argwhere` is
the only primitive that mints witnesses from data; the discipline that
argwhere returns the witness and gather consumes it is what makes multi-lane
selection checkable. `mkDesc` makes shapes constructible, not only
inherited — windows and re-chunking enter there.

## Derived operations

- **Quantifiers are reductions**: ∃ = `reduce_Or`, ∀ = `reduce_And`, count =
  `reduce_Σ` after a constant lift. Membership tests are ∃ of equality.
- **Nesting is the free-monoid reduction**; flattening is its inverse — both
  coercions under the list-lane definition.
- **Broadcast** (instance data onto the element axis) is `gather_parent`.
- **Per-run positions** (`RAGGED_IOTA`) come from enumerate minus a broadcast
  of run starts.
- **GROUP BY** is `sortPerm` + boundary detection (`lift`) + `mkDesc` +
  `reduce` — grouping is not primitive.
- **Windows / re-chunking** are `mkDesc` of computed lengths + `gather`.
- **Relational algebra at R** is the same primitives instantiated on the row
  axis: selection is argwhere+gather, join is `keyMatch`+gathers.
- **Ragged join inside a row**: `keyMatch` between two instance axes yields
  a descriptor plus stream — matches per key. Its kernel is the
  all-match-indices operation (`indicesOf`), which is why that function
  matters more than it looks.
- Every idiom in the how-to is a composite of one to three primitives.

## Laws

The equational theory is half the value; the same equations serve three
masters:

- functoriality of `lift` and `gather`; contravariant fusion
  `gather_h ∘ gather_k = gather_{k∘h}`; map–gather commutation;
  reduce-of-lift fusion,
- descriptor composition (lengths compose by segmented sum),
- promote/demote inverse given the order witness; nest/flatten identities.

They are simultaneously: the optimizer's rewrite system (with a direction —
see the cost interpretation), an extension of the canonical-form discipline
(a normal form with gathers fused, reduces fused, witnesses shared), and the
correctness oracle for any future native replacement of a derived form — a
native operation is correct iff it satisfies the same equations as its macro
definition.

## Completeness and exclusions

The yardstick is the nested relational calculus / monoid-comprehension
calculus (Fegaras–Maier) over the bounded axis tree, extended with the
reindexing operations. NRC's constructs translate into the primitives and
conversely. The classical conservativity results (Paredaens–Van Gucht for
the algebra; Wong for NRC) state that flat-to-flat queries gain nothing from
deeper intermediate nesting — which is precisely the license to compile
everything to flat lanes plus descriptors and treat nesting as
presentation.

Excluded *by design*, not omission: recursion and transitive closure;
nesting beyond the fixed axis tree; cross-row raggedness other than
promote/demote. The lineage is the flattening transformation of nested data
parallelism (Blelloch's NESL): nested operations execute on flat vectors
plus descriptors. Array languages reach the same shapes via boxing and rank
(J, K); typed relatives are Remora (static rank polymorphism) and Dex
(typed index sets); Futhark is the instructive contrast — it bans irregular
nesting, where leeway embraces bounded raggedness by reifying the
descriptor. Rank selection also names the surface question: choosing the
plane an operation applies at (a scalar map versus a per-run map) is a rank
annotation, with silent upward coercion along the ladder.

## Groundings in familiar systems

The lineage above cites theory, but none of it is load-bearing for using
the model; each concept has an exact twin in systems closer to hand.

**Wolfram Language.** `gather` is `Part` with an index list
(`lane[[{3, 1}]]`); `argwhere` is `Position`; `sortPerm` is `Ordering`
(which returns the permutation, not the sorted list); nesting a ragged
stream is literally `TakeList[vals, card]` — empty runs included — and
flattening is `Catenate`; per-run reduction is `Total /@ TakeList[…]`,
which the fused ClickHouse forms compute without materializing the split.
Rank resolution with silent scalar coercion is the `Listable` attribute.
The laws-with-a-direction are Wolfram's native mode of being: terms
rewritten by rules (`//.`) toward a normal form. A practical consequence:
Wolfram is a second executable model of the algebra — an independent
differential-testing oracle for a future function pack.

**Scheme.** The macro substrate is `define-syntax` phase separation:
expansion happens before execution, and whether it happens in the server
(SQL user-defined functions) or in a client-side pass is invisible in the
expanded program — the two substrates are one program under
macroexpansion. SRFI-1's split between `fold` (always seeded) and `reduce`
(seed consulted only for the empty list) is the positivity dividend in
library form: on leeway reads the empty case is dead, so everything lives
in `reduce`'s world and no identity element is ever fabricated.

**Dhall.** The exclusions are the Dhall move: no recursion, folds as the
only consumption, hence every term normalizes and analysis is total —
deliberately not Turing-complete, for the same reason Dhall isn't. Dhall's
semantic import hashes (content-addressing the *normal form*) also name a
door the laws open here: with the rewrite system as normalizer, query
terms gain a canonical identity — a natural fit for a system that treats
queries as data.

**Interval arithmetic** (Wolfram's `Interval`) grounds the guard
semantics: conservative bounds propagated through operations. A minmax
skip index evaluates a predicate in exactly that arithmetic per granule; a
Bloom filter is the set-shaped analog; the (S, N) pair generalizes both to
arbitrary predicates with polarity made explicit.

## Three interpretations

A term of the algebra has three compositional semantics. That they share
one term language — and one type environment — is the point.

**Exact.** Compilation to ClickHouse expressions. SQL user-defined
functions are pure macros (inlined at analysis time, plan-identical to
handwritten expansions, transparent to index analysis and PREWHERE, lambda
parameters allowed), so a function pack can realize the primitives with
zero interpretive overhead; a client-side expansion of the same bodies is
semantically equivalent where server state is unavailable. Mechanics and
measurements: see the how-to.

**Guard.** An abstract interpretation assigning every predicate a pair
(S, N) — a sufficient and a necessary condition, S ⇒ P ⇒ N. Conjunction and
disjunction go pointwise; negation swaps the pair; existentials over
equality atoms emit `has(...)`-shaped necessary conditions, which are the
forms data-skipping indexes serve. Information loss is localized and
nameable: it occurs exactly at non-injective gathers and at Or-reductions
over conjunctions, where the abstraction erases axis correlation — the
false positives of a `has(a,x) AND has(b,y)` guard are rows where x and y
occur at *different* instances. Pruning works for positive polarity only (a
skip index can only ever answer "definitely not / maybe"). Guard usefulness
is a two-tier estimate: a prior from leeway's encoding aspects (a
low-cardinality symbol lane is a poor needle; a content-addressed id lane
an excellent one) and a measurable posterior — the guard-vs-exact slack is
one `countIf` comparison away.

**Cost.** The objective is *bytes of temporaries*: sum of element width ×
axis length over intermediates. Widths come from canonical types, lengths
from descriptors — the cost model reads the same type environment as the
correctness checker. Consequences, all measured (ClickHouse 26.7,
single-threaded, ~5.6M elements): nesting is **virtual** — its macro
realization copies the stream, and range-fused forms of per-instance
reductions ran ~1.5–2.4× faster than materialize-then-operate; the normal
form evaluates each atom at the rank where its lane lives, moves results
rather than operands, and crosses planes at I with the narrowest possible
payload (a wide-lane broadcast measured *slower* than the nesting it
avoided). Descriptor-only reshapes (nest, windows, re-chunking) are O(E) as
macros but O(offsets) for a native implementation that shares the data
column — an asymptotic gap, which is the earn-condition for extending the
engine rather than macro-expanding.

## Refinements

- **Positivity** (both planes): reductions need only semigroups — no
  identity element, so no fabricated defaults (`argMax`, first/last of a
  run are total); ∀ over a run is never vacuous; `parent` is surjective.
  Empty-run handling in the ClickHouse realizations remains total for
  foreign data but is dead code on leeway reads.
- **Set versus array** (`m` vs `h` in canonical types): on set-typed lanes,
  runs are order-free — commutative semigroups are canonical and
  order-sensitive picks are ill-typed rather than suspicious.
- **Constant cardinality** (`≡ k`): rectangular data unlocks element-wise /
  `-ForEach` shapes as a typed capability.
- **Fresh axes**: order-destroying operations (`arrayDistinct`,
  `arrayIntersect`) return lanes on a fresh axis with no witness — the type
  system's way of saying their output aligns with nothing. The familiar
  footgun becomes a typing rule.

## Evidence

Verified against a live ClickHouse 26.7 during construction (executable
forms in the how-to): the broadcast/parent-id constructor; per-run
positions; the ragged join shape; the fused `RAGGED_EXISTS` body including
the empty-run boundary case; macro inlining being plan-identical and
index-transparent; the index machinery recognizing enumerated syntactic
shapes only (`indexOf(a,x) > 0` prunes, the equivalent `!= 0` does not) —
which is an argument for carrying guards explicitly rather than relying on
recognition; and the cost measurements quoted above. The guard slack
factor of an uncorrelated two-lane conjunction was demonstrated at 10× on a
synthetic distribution.

## Open questions

Deferred to the coming ADR and further dialogue: whether `scan_m` earns
primitive status (its general per-run realization is the one gap);
`keyMatch` symmetry (witnesses into both sides); surface syntax for rank
markers and ascriptions; how strictly the witness discipline is enforced;
where guards are emitted (inside function-pack bodies, derived by a pass,
or both with the pass auditing); pack contents, naming, and versioning; and
the substrate portfolio (server pack, client expansion, proxy, upstream
contributions) with its migration story. The
[semantic-layer ADR](../adr/0139-semantic-layer-text2dsl.md) and the
[pass-registry ADR](../adr/0108-keelson-sql-pass-registry.md) are the
natural seams for the checking side; the
[read-back generator](../adr/0066-leeway-dql-clickhouse-readback-generator.md)
is unaffected — it emits its own SQL.
