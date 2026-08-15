---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Analysis feeding an extension of
> [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) §SD3's extraction
> family (`LW_GET`, `LW_GET_NULL`, `LW_GET_LIST`) to the two **mixed**
> membership channels. Code claims were verified by reading the tree on
> 2026-08-15; the SQL arithmetic in §7 was executed against
> `clickhouse-local`, not reasoned about.
>
> **The decision was taken**: ADR-0181's *2026-08-15* Update is authoritative
> — D6's selector family and D1's split rule as recommended below, D2 on the
> built-ins arm, D5's plural getter dropped. This page is the reasoning that
> led there, not a description of current behaviour, and is not maintained
> against the tree.

# Extracting from mixed membership channels

## 1. The question

`LW_GET` today reads four of leeway's eight membership channels —
`low-card-ref`, `low-card-verbatim`, `high-card-ref`, `high-card-verbatim`.
The vocabulary is declared in one place, `extractChannels` in
[`lwsql_lanes.go`](../../public/semistructured/leeway/lwsql/lwsql_lanes.go),
whose comment states the exclusion:

> Mixed and parametrized channels are deliberately absent: their parameter
> lane needs a recursion the extraction builder does not model.

What would it take to lift that for the **mixed** pair
(`MixedLowCardRefHighCardParameters`, `MixedLowCardVerbatimHighCardParameters`)?
Two findings shape the answer, and they pull in opposite directions:

- **Mechanically it is small.** The lane is already indexed, the arithmetic is
  a two-array `arrayFirstIndex`, and the invariant it rests on is already
  audited. Nothing recursive is involved.
- **Semantically it is a new contract.** On a mixed channel the membership tag
  is *not* an attribute identifier, which is the assumption `LW_GET`'s
  signature encodes. Extending the family without settling that would ship a
  call that returns an arbitrary member of a set.

## 2. Ground truth — what a mixed channel is, physically

A mixed channel occupies **three** columns per section, against a simple
channel's two.
[`ResolveMembership`](../../public/semistructured/leeway/ddl/lw_ddl_tech_common.go)
emits them:

| Role | Spelling | Type | Axis |
| --- | --- | --- | --- |
| identity | `lmr` / `lmv` | `UInt64` / bytes | flattened membership positions |
| parameters | `mrhp` / `mvhp` | bytes | flattened membership positions |
| cardinality | `lmrcard` / `lmvcard` | count | per attribute |

The two payload lanes are **co-indexed 1:1** — the generated DML appends to
both inside one `AddMembershipMixed…` call — and **one** cardinality lane
describes both. [ADR-0010 §wire keys](../adr/0010-leeway-cbor-rpc-codec.md)
shows the shape a row carries:

```json
"lmv":     ["/tags/_", "/tags/_"],
"mvhp":    ["0000",    "0001"],
"lmvcard": [1, 1]
```

Three properties matter for extraction, and each is already true rather than
something to build:

- **The parameter lane is already resolved.** `lwsql`'s section index keys
  every support column by role, so `mvhp`/`mrhp` are in `si.roles` today;
  `extractChannels` simply never looks. `MembershipParamPartner` in
  [`lwsql_classify.go`](../../public/semistructured/leeway/lwsql/lwsql_classify.go)
  already maps a parameter lane back to the identity lane whose `…card`
  covers it.
- **The co-length invariant is already audited.** Because of that partner
  mapping, ADR-0181 §SD5's runtime audit generator already emits
  `arraySum(lmvcard) = length(mvhp)` alongside the identity lane's. The
  invariant an extraction would lean on is checked, not assumed.
- **The cardinality lane is always emitted.** `loadSectionMembership` calls
  `addSetSupportColumn` unconditionally per channel, so the `Card == ""` fast
  form of [`lwextract`](../../public/semistructured/leeway/lwextract/lwextract.go)
  is not reachable from a schema-generated table. Mixed extraction only ever
  needs the general (pack-form) arm.

**The parameters have a canonical codec.**
[`membership/params.go`](../../public/semistructured/leeway/membership/params.go)
defines fixed-width lowercase hex, `.`-separated, one 4-digit group per index
(`/a/12/b/3` → `"000c.0003"`), and its doc comment carries a section titled
*"Reading a blob in SQL"* with the `unhex`/`reinterpretAsUInt16` idioms
spelled out. Four properties are called out as load-bearing there, and one is
decisive for us: fixed width means **lexicographic order equals numeric
order**, so a params match is plain string equality — no decode on the locate
path.

## 3. The recorded deferral no longer describes the blocker

ADR-0181 §SD8 defers this as *"Mixed/parametrized channel extraction —
ADR-0008 Cut 2 front-end"*, and the explanation page and how-to repeat it.
That trigger has already fired, and it was never the binding constraint for
`LW_GET` anyway:

- **Cut 2 landed.** ADR-0008's 2026-06-04 Update records all four Cut-2
  channels implemented across `mappingplan`, `marshallgen` and
  `marshallreflect`; `mappingplan.channelTable` carries
  `MembershipChannelMixedLowCardRef` and its verbatim sibling with full
  descriptors.
- **`LW_GET` never consumed that front-end.** `LwExtractExpand` resolves lanes
  from a table's **physical column names** through `lwsql.Resolver`, not from a
  `mappingplan.Plan`. The mappingplan gap is real but belongs to the
  *read-back generator*, whose `readback.channelSpec()` still rejects
  everything but the four simple channels.

So the two consumers of the shared `lwextract` builder are blocked on
different things, and only one of them is blocked at all. Whatever is decided
here, §SD8's deferral note wants rewording — it currently sends a reader to a
milestone that is done.

## 4. Who is waiting

Three in-tree consumers, in descending order of evidence:

**`public/gov/capmapfacts/read.go` — hand-written array arithmetic, today.**
The capmap corpus dump reads `lmr`/`mrhp`/`lmrcard` through
`positionsFor`/`attrsFor`/`pickParameters`, and says why in its own header:

> The lane resolver enumerates the plain and high-cardinality channels only,
> so `chan:` cannot name it.

This is the exact failure mode [ADR-0171](../adr/0171-leeway-sql-read-surface.md)
was written about — a consumer reimplementing lane arithmetic because the
vocabulary does not reach their shape. It is milder here than the jsonbench
case, because this code is careful and routes through `LW_RAGGED_PARENT_IDS`
rather than a hand-rolled prefix sum. But the reason it is careful is that
somebody had to think about it.

**The canonical JSON mapping — unreachable in full.**
[`mapping/lw_ddl_schema.go`](../../public/semistructured/leeway/mapping/lw_ddl_schema.go)
declares `MixedLowCardVerbatimHighCardParameters` on *every* section
(`string`, `text`, `symbol`, `bool`, `int64`, `uint64`, `date32`). This is the
shape `mapping.LoadJsonMapping` produces and the one `apps/jsonbench`'s
`jsonmap` arm loads. **No table produced by the canonical JSON mapping can be
read with `LW_GET` at all** — not one section, not one attribute. A path rides
`lmv` verbatim and its array indices ride `mvhp`, which is precisely the
design that makes the mapping work and precisely what the extraction family
cannot name.

**`common.PopulateSchemaTable` — leeway's own system table.** The `text`
section of `system_table_columns` writes three memberships on one mixed-ref
channel (`encodingHint`, `valueSemantic`, `useAspect`), with the aspect's
ordinal in the params via `membership.EncodeParams`. This is also the
clearest illustration of §5's problem: the tag says *which vocabulary*, the
params say *which member*.

## 5. The semantic problem — the tag is not an identifier

`LW_GET`'s contract is singular: *locate the attribute carrying this
membership, return its value.* The read-back family is built on it —
`LW_LU_ATTR_BY_TAG` takes `indexOf`, the **first** match, and every other
helper layers on that.

On a simple channel that is close enough to sound: a section normally carries
one attribute per membership, and where it does not, ADR-0181 §SD1's guard
contract at least makes the guard honest. On a mixed channel it is
structurally false. The high-cardinality half exists *because* many attributes
share the low-cardinality half:

| Corpus | `lmv`/`lmr` says | `mvhp`/`mrhp` says |
| --- | --- | --- |
| JSON mapping | the path with indices elided, `/tags/_` | which index, `0000` |
| `system_table_columns` | which aspect vocabulary | which member of the set |
| capmap | which relation kind | the section heading / lifecycle phase |

In all three, a tag-only `indexOf` returns the first of several and raises
nothing. §7 shows this concretely: on the fixture there, `tag_only` returns
`'a'` where the caller almost certainly meant one of `['a','b']`.

That makes the params argument the central design question, not a decoration —
and it is why this is worth a dialogue rather than two lines added to a table.

## 6. Design space

### 6.0 The plural read is already expressible, and D1 depends on that

D1 below turns on what a caller does when refused, so it matters that
"iterate the attributes carrying this membership" is **not** a missing
capability. It is expressible today on installed functions alone. Three
shapes, all executed against `clickhouse-local` on §7's fixture:

**A — attribute-major `ARRAY JOIN`.** One row per attribute, its membership
set nested beside it. This is the documented idiom: how-to §6 and play's
snippet library both show it, in the `LW_RAGGED_NEST` form.

```sql
SELECT id, v AS value, tags, params
FROM t
ARRAY JOIN val AS v,
           LW_RAGGED_NEST(lmv,  lmvcard) AS tags,
           LW_RAGGED_NEST(mvhp, lmvcard) AS params
WHERE has(tags, '/tags/_')
```

→ `('a', ['/tags/_'], ['0000'])` and `('b', ['/tags/_','/alias/_'],
['0001','0000'])`. Note the second row: this shape hands back the attribute's
*whole* membership set, so pairing a tag with its parameter is left to the
caller.

**B — membership-major `ARRAY JOIN`.** One row per *occurrence*, with tag,
parameter and value already lined up — the shape a mixed channel actually
wants:

```sql
SELECT id, m AS tag, p AS param, v AS value
FROM t
ARRAY JOIN lmv AS m, mvhp AS p, LW_LU_VAL_BY_MEMB_IDX(val, lmvcard) AS v
WHERE m = '/tags/_'
```

→ `('/tags/_','0000','a')`, `('/tags/_','0001','b')`. This is on-the-fly
exactly the exploded representation [ADR-0171 §SD5](../adr/0171-leeway-sql-read-surface.md)
prices as a stored second copy.

**C — in-projection gather, no row multiplication.** Arrays out, one row in:

```sql
SELECT arrayFilter((v, m) -> m = '/tags/_', LW_LU_VAL_BY_MEMB_IDX(val, lmvcard), lmv) AS vals,
       arrayFilter((p, m) -> m = '/tags/_', mvhp, lmv)                                AS params
FROM t
```

→ `['a','b']`, `['0000','0001']`. This is what `capmapfacts` does, by the
longer route: it selects positions with `arrayFilter` over `arrayEnumerate`,
maps them through `LW_RAGGED_PARENT_IDS` to attribute indices, then gathers.
Both are correct and both were checked against each other.

Three observations that bear on the decisions:

- **`LW_LU_VAL_BY_MEMB_IDX` is the key to B and C, and it is documented
  nowhere a consumer would look.** It is in the read-back family's own `.sql`
  under a one-line comment ("Value broadcast") and in the roster — not in the
  how-to, the explanation page, the skills, or the snippets. It broadcasts
  each attribute's value onto its membership positions, which is what makes
  the value lane co-indexed with the tag and parameter lanes and collapses
  the plural read to one `arrayFilter`. That a careful in-tree consumer
  reached for the longer route is the ADR-0171 finding in miniature, at a
  much smaller scale.
- **B and C's broadcast form has a shape precondition.** It requires a
  *scalar* section: `LW_LU_VAL_BY_MEMB_IDX` indexes `valcol` by attribute, so
  on an array- or set-valued section — where the value lane is the flattened
  element stream — it is wrong. `capmapfacts` handles that case with
  `LW_RAGGED_ELEM(val, len, attr, 1)`, and shape A is unaffected.
- **None of this is mixed-specific.** Every shape above works the same on a
  simple channel; the mixed channel only adds the parameter lane to carry
  along. So the plural read is a *general* gap in the sugar, not a mixed one.

### D1 — what a call must supply (the decision)

> **D6 changes the answer to this.** Read it first: once a plural *selector*
> exists, D1's refusal has a sibling call to point at rather than a raw SQL
> shape, and the mandatory-parameter rule stops being a restriction and
> becomes a split between two well-posed questions.


- **O1 — `param:` mandatory on a mixed channel.** `LW_GET('symbol',
  '/tags/_', 'chan:…', 'param:0000')`. The pair is the identifier, matching
  the physical truth. A caller who wants the whole set gets a loud refusal
  naming what to do instead. Costs: verbose for a scalar JSON path
  (`/hostname` carries empty params and must spell `'param:'`), and the
  `''` spelling has to be legible.
- **O2 — `param:` optional, absent means empty params.** Exact, and the
  common JSON scalar path stays short. But a call that forgets `param:` on an
  array path silently reads as *absent* — the type default — which the
  family's own contract says means "no such attribute". Wrong answer, no
  signal.
- **O3 — `param:` optional, absent means ignore the parameter lane.** Reads
  like the simple channels; expands to today's `indexOf`. Silently returns an
  arbitrary member of a set on exactly the three corpora in §4.

O1 is the one consistent with how this subsystem already resolves this class
of question. `lwsql.Channel.Card` carries the same argument in its doc
comment — an absent lane is refused rather than treated as licence, because
"not in the listing I was given" is not proof. The same reasoning says a
missing `param:` is not proof that the caller meant the empty tuple.

**§6.0 is what makes O1 defensible.** A refusal that leaves the caller with
nowhere to go is not rigour, it is an obstruction — and it would reproduce
the exact failure ADR-0171 was written about, since the caller's only
remaining move is the hand-written arithmetic. That is not the situation:
the plural read has three working spellings today, one of them documented.
So O1's error message can name the fix, and the fix is a shape a person can
copy. Concretely, the refusal should carry shape B or C rather than the bare
sentence "this channel needs a parameter" — the trial's evidence is that
naming the alternative is the whole difference between a surface that works
and one that gets open-coded around.

That reframes the companion decision. An explicit "any parameter" token
(`'param:*'`, expanding to today's tag-only `indexOf`) is *not* the escape
hatch for the plural case — it would still return one arbitrary member, and
would read as though it answered the plural question. Its only honest use is
"I know this tag is unique in this section and I do not want to spell its
parameter", which is a narrower claim. Whether that is worth a token at all
is open; the plural escape hatch is §6.0, not `param:*`.

### D2 — how it renders

The locate step needs `arrayFirstIndex` over two lanes instead of `indexOf`
over one. Two ways to get there:

- **New read-back UDFs** — `LW_LU_ATTR_BY_TAG2`, `LW_VALUE_BY_TAG2_EQUAL`,
  `LW_LIST_BY_TAG2_EQUAL`, mirroring the existing trio. Consistent with the
  family, keeps emitted SQL short, and the presence half needs nothing new:
  `LW_CO_EXISTS_EQ2(a, x, b, y)` is already in `chpack` with the right shape,
  including the `has(a,x) AND has(b,y)` prefix that prunes.
  **Cost:** ADR-0171 §SD2's handshake — `lwsqlsurface.Version` bumps from 1,
  the declared set grows, the roster tests and the truth-table fixture move,
  and every provisioned server needs `leeway sqlsurface install` before a
  mixed read works. The invariant is one marker for all three families, so the
  bump invalidates servers that will never issue a mixed read.
- **Inline builtins** — `val[LW_RAGGED_PARENT_IDS(card)[arrayFirstIndex(…)]]`,
  which needs only what is already installed. No version bump, no new roster
  entry, works on a server provisioned before this lands. **Cost:** it is a
  partial exception to §SD3's "v0 wires only the pack-form renderer", and it
  duplicates `LW_LU_ATTR_BY_TAG`'s body at the call site, so the two can
  drift.

Worth noting that ADR-0181 §SD8 already defers a full *inline builtins-only
renderer* with its own trigger (a read-only ClickHouse target). Taking the
inline route here for one channel pair would either pre-empt that or want to
be framed as its first arm.

### D3 — how the channel is spelled

Two vocabularies already exist and disagree:

- `MembershipSpecE.String()` — `"low-card-verbatim-high-card-params"`. This
  is what `LW_TV_MEMB`'s `ParseMembershipSpec` accepts, and it happens to
  coincide with `extractChannels`' four names for the simple channels.
- `ColumnRoleE.LongString()` — `"mixed-low-card-verbatim"`.

Following `MembershipSpecE.String()` keeps one vocabulary across the
constructor and extraction families at the price of a long token. Inventing a
third spelling should not happen. Note `ParseMembershipChannel` currently
*rejects* mixed for the constructor family for an unrelated and still-valid
reason — one expression cannot mint two columns — so the two families would
legitimately accept different subsets of one vocabulary.

### D4 — what a `param:` literal is

The token value could be the raw blob (`'param:000c.0003'`) or a typed index
tuple the pass encodes through `membership.AppendParams`
(`'param:12,3'`). The typed form is friendlier and routes every writer and
reader through one codec, which is what `params.go` exists to enforce. The raw
form is honest about corpora that predate the codec or do not use it —
`leewaywidgets`' fixture writes `k=10`, and nothing stops an application from
writing anything into an opaque bytes lane. Accepting both, with the typed
form as the documented one, is probably the shape; it needs deciding rather
than defaulting.

### D5 — the plural *getter* stays out; D6 replaces it with a selector

capmapfacts wants *all* attributes carrying a tag plus their parameters, in
write order. That is a different function from `LW_GET` — plural, not
singular — and it is **not mixed-specific**: capmapfacts' own header lists
"a membership carried by several attributes" as a separate case from "a
membership on the mixed channel", and §6.0 shows every spelling works
identically on a simple channel. Folding a gather *function* into this work
would make a small change large and would answer a question mixed channels
only sharpened, not created. Recommend descoping the plural *getter*
permanently rather than with a trigger: **D6 is the better answer to the
same need**, and shipping both would leave two ways to ask one question.

The **documentation** half is a different matter and is nearly free. §6.0's
three shapes are the answer to a question consumers demonstrably have, and
one of them turns on `LW_LU_VAL_BY_MEMB_IDX`, which no user-facing page
mentions. Adding a "read every attribute carrying a membership" section to
the [reading-and-authoring how-to](../howto/leeway-sql-reading-and-authoring.md)
costs a page edit, is worth doing whether or not any of D1–D4 proceeds, and
is what O1's refusal message would point at. It is also the cheaper half of
what §4's consumers are actually short of: capmapfacts needs the plural
shape more than it needs `LW_GET` on a mixed channel.

### D6 — a selector family, so the plural read composes instead of unnesting

§6.0's shapes all work, but A and B both spend an `ARRAY JOIN`: they change
the row grain, so anything else the query wants at row grain has to be
re-aggregated afterwards, and two memberships read in one statement need two
`ARRAY JOIN`s that then have to be reconciled. Shape C avoids that but is
the one nobody finds.

The house idiom for exactly this is already written down — the array-idioms
how-to calls it out under *"Select positions once, gather any number of
lanes"*, and states the plan in one sentence: **"Argwhere + gather is the
general plan: every further lane — including lanes of co-sections —
projects through the same `sel`."** The pack ships the *gather* half as
`LW_CO_GATHER(lane, sel)`. It ships no *argwhere* half. That asymmetry is
the gap, and it is what a selector closes.

**The proposal.** A call that returns the selector rather than the values,
so the caller's own higher-order functions consume it:

```sql
LW_SEL('<section>', '<membership>' [, 'chan:…'] [, 'param:…'])
```

→ the positions in the section's membership lane where the membership
occurs. Plus a companion returning the same selection on the other axis:

```sql
LW_SEL_ATTRS('<section>', '<membership>' [, …])   -- attribute indices
```

The two are **co-indexed with each other by construction**
(`LW_SEL_ATTRS = LW_CO_GATHER(LW_RAGGED_PARENT_IDS(card), LW_SEL(…))`), which
is the property that makes them composable: they pass as two lambda
arguments and stay aligned.

```sql
SELECT arrayMap((p, a) -> (`symbol:mvhp`[p], `symbol:value`[a]),
                LW_SEL('symbol', '/tags/_'), LW_SEL_ATTRS('symbol', '/tags/_'))
FROM t
```

Verified on §7's fixture: `[('0000','a'), ('0001','b')]`. Also verified —
`arrayFilter` over the pair, `LW_CO_GATHER` over either axis,
`LW_CO_GATHER(LW_RAGGED_NEST(val, len), attrs)` for a list-valued section,
and plain `length()`/`arrayExists`/`arrayStringConcat` straight off the
selector. An absent membership yields `[]`, so every consumer degrades to
the empty answer without a special case — the family's existing
absence rule, unchanged.

**Why a selector rather than plural getters.** The alternative is one
function per lane — values, parameters, the other channel's tags — which
multiplies with every lane a section carries and cannot reach a co-section
lane at all. One selector reaches all of them, which is the argument the
how-to already makes for the hand-written form.

**Why not a tuple-returning iterator.** `LW_ITER(…)` yielding
`(attrIdx, membPos)` pairs looks tidier at the call site but reads worse
(`t.1`/`t.2`), and it cannot be handed to `LW_CO_GATHER`. It also invites
`arrayMap(p -> val[LW_RAGGED_PARENT_IDS(card)[p]], …)` — recomputing the
position→attribute map *inside* the lambda, which is the one real
performance trap in this shape. Two co-indexed selectors evaluate it once.

**It needs no new server-side function.** The expansion is builtins plus
`LW_RAGGED_PARENT_IDS` and `LW_CO_GATHER`, both already in the declared set:

```sql
LW_SEL       →  arrayFilter((i, m) -> m = <lit>, arrayEnumerate(<ident>), <ident>)
LW_SEL       →  arrayFilter((i, m, p) -> m = <lit> AND p = <par>,          -- with param:
                            arrayEnumerate(<ident>), <ident>, <param>)
LW_SEL_ATTRS →  LW_CO_GATHER(LW_RAGGED_PARENT_IDS(<card>), <the above>)
```

So this family can ship without touching `lwsqlsurface.Version` and works on
any server already carrying the surface. If the generic argwhere is wanted
server-side later, the pack names are derivable from two existing
precedents — `LW_CO_ARG_WHERE_EQ` from `LW_CO_ARG_SORT`, and
`LW_CO_ARG_WHERE_EQ2` from `LW_CO_EXISTS_EQ2` — and `LW_SEL` would re-point
at them without changing what a caller types.

**It interacts with D2.** If the plural path emits builtins, emitting them
for the singular mixed path too becomes the consistent choice rather than an
exception to §SD3's pack-form rule. D6 therefore argues for D2's second
option, and the two together let the whole of this analysis ship with no
version bump and no reprovisioning.

**It resolves D1.** `param:` is optional on `LW_SEL` and mandatory on a
mixed `LW_GET`, and the two rules are the same rule: *the parameter is
required exactly when the answer must be unique.* "All attributes carrying
this tag" is well posed without it; "the attribute carrying this tag" is
not. The mixed `LW_GET` refusal then names `LW_SEL` — a sibling call in the
same vocabulary, not a SQL shape to copy.

**Open within D6:**

- **Naming.** `LW_SEL` / `LW_SEL_ATTRS` reads with the how-to's own `sel`
  variable and both comply with the `LW_` + UPPER_SNAKE rule, but `SEL` sits
  close to `SELECT`. `LW_ARGWHERE` matches the how-to's prose and the numpy
  convention at the cost of not saying it is membership-scoped.
- **Token reuse.** `chan:` and `param:` carry over unchanged; `col:` is
  meaningless for a selector — it touches no value lane — and should be a
  loud rejection rather than ignored.
- **Pruning.** A multi-lane `arrayFilter` is opaque to index analysis, so
  `LW_SEL` in a projection does not prune. It wants the same sargable-guard
  advice `LW_GET` already carries: keep `has(<ident>, <lit>)` in `WHERE` as
  the pruner. Whether the pass can emit that guard itself, or only document
  it, is open.
- **List sections.** `LW_SEL_ATTRS` indexes the attribute axis, so a
  list-valued section still needs `LW_RAGGED_NEST` (which copies the stream,
  ADR-0162 §SD4) or `LW_RAGGED_ELEM` per attribute. Whether a third
  companion is worth it, or whether documenting the two-step is enough, is
  open.

## 7. The arithmetic, verified

Executed against `clickhouse-local` on 2026-08-15, on a fixture with a ragged
row (attribute 2 carries two memberships) so the position→attribute map is
not the identity:

```sql
WITH ['a','b','c']                                AS val,
     ['/tags/_','/tags/_','/alias/_','/hostname'] AS lmv,
     ['0000',   '0001',   '0000',    '']          AS mvhp,
     [1,        2,        1]                      AS lmvcard
SELECT
  arrayFirstIndex((a,b) -> a='/tags/_' AND b='0001', lmv, mvhp)                AS pos_hit,
  val[LW_RAGGED_PARENT_IDS(lmvcard)[pos_hit]]                                  AS value_hit,
  arrayFirstIndex((a,b) -> a='/tags/_' AND b='9999', lmv, mvhp)                AS pos_miss,
  val[LW_RAGGED_PARENT_IDS(lmvcard)[pos_miss]]                                 AS value_miss,
  arrayExists((a,b) -> a='/alias/_' AND b='0000', lmv, mvhp)                   AS present,
  val[LW_RAGGED_PARENT_IDS(lmvcard)[indexOf(lmv, '/tags/_')]]                  AS tag_only
```

| column | result | reading |
| --- | --- | --- |
| `pos_hit` | `2` | flattened membership position of the pair |
| `value_hit` | `'b'` | `LW_RAGGED_PARENT_IDS` = `[1,2,2,3]`, so position 2 → attribute 2 |
| `pos_miss` | `0` | absent pair |
| `value_miss` | `''` | `arrayElement(arr, 0)` yields the **type default**, so §Value's "absent yields the type default, never NULL and never an error" holds unchanged |
| `present` | `1` | the presence half needs no new primitive |
| `tag_only` | `'a'` | §5's hazard, concretely: the caller meant one of `['a','b']` |

Two operational notes fell out of the run:

- **A length mismatch throws.** `arrayFirstIndex` over two arrays raises
  `SIZES_OF_ARRAYS_DONT_MATCH` when the lanes disagree. A malformed row
  therefore fails loudly where a simple-channel read would quietly mis-index.
  Given §2's audit already checks that invariant, this reads as a feature —
  but it is a behaviour difference worth stating in whatever ships.
- **No decode on the locate path.** The params match is string equality
  against the canonical fixed-width form. `unhex`/`reinterpretAsUInt16` are
  only needed to *project* an index, never to find one.

## 8. Blast radius, if it proceeds

| File | Change |
| --- | --- |
| `lwsql/lwsql_lanes.go` | `extractChannels` gains two rows; `Channel` gains a `Param` field; `ChannelFor`'s "no readable channel" message loses its mixed caveat |
| `lwextract/lwextract.go` | `Lanes` gains `Param`; `Request` gains the params literal and whether it was supplied; `Value`, `ValueCount`, `Present`, `CountEqual` grow a two-lane arm; `validate` gains the D1 rule |
| `constructsql/extractsql.go` | `param:` token; the D1 refusal; `ExtractExpansionDependencies` grows if D2 takes the UDF route |
| `readback/lw_readback_udfs.sql`, `…_roster.go`, `…_truth_test.sql` | D2-UDF route only: three functions, roster entries, truth-table fixtures |
| `lwsqlsurface/lwsqlsurface.go` | D2-UDF route only: `Version` 1 → 2 |
| `doc/howto/leeway-sql-reading-and-authoring.md` §3 | the "channel vocabulary is …; mixed and parametrized are out of scope" sentence |
| `doc/explanation/leeway-sql-read-surface.md` §gaps | same bullet |
| `doc/adr/0181-…` §SD3, §SD8 | dated Update; §SD8's stale trigger (§3) wants correcting either way |
| `doc/skills/leeway-*` | the channel table in `leeway-advanced` |
| `apps/play/help/snippets.md`, anchor's snippet fixture gate | a mixed example, if one is wanted under the gate |

Not on the list, deliberately: `readback`'s generator. Its
`channelSpec()` bridge is a separate gap (§3) with a separate consumer, and
the shared `lwextract` builder would gain the capability either way.

## 9. What stays out, and why it is a different problem

**Parametrized channels** (`lp`, `hp`) are lumped with mixed in every
deferral note in the tree. They should not be. A parametrized membership is
**one** lane carrying a single opaque blob that encodes identity and
parameters together — there is no separate identity lane to match against,
and no shared codec says how the blob is laid out. `membership/params.go`
covers the *mixed* parameter channel; the one in-tree parametrized writer,
`leewaywidgets`' fixture, writes `[]byte("k=10")` by hand. So a parametrized
extraction has no literal a caller could supply, and unblocking it means
first deciding a serialization contract — a genuinely larger and different
question from the one in §6.

Recommend splitting the deferral rather than lifting it wholesale: mixed is
unblocked and has three waiting consumers; parametrized is blocked on
something nobody has proposed.

## 10. For the dialogue

0. **D6 first** — does the selector family (`LW_SEL` / `LW_SEL_ATTRS`) go
   ahead? It is the largest idea here and the cheapest to ship, it answers a
   need that is not mixed-specific, and it changes the answers to D1, D2 and
   D5. Its own opens are naming, the `col:` rejection, whether the pass emits
   the sargable guard, and whether list sections want a third companion.
1. **D1** — given D6, is `param:` mandatory on a mixed `LW_GET` and optional
   on `LW_SEL` (recommended — the parameter is required exactly when the
   answer must be unique)? Is a `'param:*'` token wanted at all?
2. **D2** — new UDF trio plus a `LW_SURFACE_VERSION` bump, or inline builtins
   that need no reprovisioning? The second interacts with §SD8's deferred
   inline renderer, and D6 argues for it: if the plural path emits builtins,
   the singular path doing so is consistency rather than an exception.
3. **D3** — `chan:low-card-verbatim-high-card-params` (one vocabulary, long)
   or something shorter that forks the spelling?
4. **D4** — is a `param:` literal the raw blob, a typed index tuple encoded
   through `membership.AppendParams`, or both?
5. **D5** — confirm the plural *getter* is dropped in favour of D6's
   selector, and separately: is the §6.0 documentation edit worth doing on
   its own, ahead of and independent of everything else?
6. Does this warrant its own ADR, or a dated Update to ADR-0181 §SD3? With
   D6 in, it is no longer only about mixed channels — a selector family is
   new client vocabulary that serves every channel, which pushes it towards
   its own ADR. Either way it is Tier-1 Surfaces, and Migration too under
   D2's first option.
