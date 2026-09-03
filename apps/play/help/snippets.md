---
type: reference
audience: end-user
status: draft
title: Snippets
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Snippets

A small library of ready-to-run queries against `anchor.facts`, the
self-contained demo table (seeded by
`public/semistructured/leeway/anchor/seed_facts.sh`). Click **Insert** above any
block to splice it into the editor at the cursor; place the caret where you want
it first (an empty editor takes the snippet whole). Do not add a `FORMAT` clause
— the app appends one. The verified, explained versions live on the **Example
queries** page. The **ADS-B geo-raster** section below targets `planes_mercator`
from the demo loader (`apps/play/demo/adsb/demo.sh`), not `anchor.facts`.

Leeway columns are named by their friendly handles (`` `section:column` ``,
resolved to physical names before the query ships), and tagged attributes are
read with the `LW_*` vocabulary — **The leeway surface** below covers the
one-time install the server-side part of it needs.

## Whole entities

`SELECT *` returns the full leeway row, so the Detail tab renders the leeway
card. Run it, then click a row in Table.

```sql
SELECT * FROM anchor.facts LIMIT 50
```

## One scenario

Narrow to a single scenario to vary which tagged sections are populated.

```sql
SELECT * FROM anchor.facts
WHERE hasAny(`symbol:value`, ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
```

## Query graph (CTEs become nodes)

Each top-level CTE splits into its own node of the reactive query-graph
(ADR-0097): `recent` and `by_kind` below become nodes — `by_kind` reads
`recent`, and the final `SELECT` reads `by_kind`. The chain fuses back into a
single query for execution (identical to running it inline); the **Graph** tab
shows the nodes and their edges, and its *observe in panels* button points the
result tabs at an intermediate node instead of the sink.

The handle read sits in `recent`, where `anchor.facts` is in scope: the
resolver rewrites handles per `SELECT`, against the tables that `SELECT` reads,
and a CTE has no catalog schema to ask — downstream nodes see only the aliases
the upstream node exported.

```sql
WITH
  recent AS (
    SELECT `symbol:value`[1] AS event_type
    FROM anchor.facts
    LIMIT 50
  ),
  by_kind AS (
    SELECT event_type, count() AS n
    FROM recent
    GROUP BY event_type
  )
SELECT event_type, n
FROM by_kind
ORDER BY n DESC
```

## Recursive CTEs (WITH RECURSIVE)

A `WITH RECURSIVE` CTE may reference its own name: a seed branch and a
recursive branch combined with `UNION ALL` (needs a server with recursive-CTE
support, ClickHouse ≥ 24.4). The Graph tab shows such a node as
`CTE (recursive)` — the self-reference stays inside the node rather than
becoming a graph edge (ADR-0097 §SD9). Table-free, so it runs against any
endpoint:

```sql
WITH RECURSIVE fib AS (
  SELECT 1 AS n, toUInt64(0) AS a, toUInt64(1) AS b
  UNION ALL
  SELECT n + 1, b, a + b FROM fib WHERE n < 40
)
SELECT n, a AS fib FROM fib
```

A recursive series also works as a spine for downstream CTEs — the calendar
idiom. `days` is the recursive generator, `by_week` aggregates it, and
"observe in panels" on either node in the Graph tab materialises it standalone:

```sql
WITH RECURSIVE days AS (
  SELECT toDate('2026-01-01') AS day
  UNION ALL
  SELECT day + 1 FROM days WHERE day < toDate('2026-01-31')
),
by_week AS (
  SELECT toStartOfWeek(day) AS week, count() AS days_in_week
  FROM days
  GROUP BY week
)
SELECT week, days_in_week FROM by_week ORDER BY week
```

## One entity by id

The ids 10005, 10010, 10015, 10020, 500003 carry the sparse `geoArea` section.

```sql
SELECT * FROM anchor.facts WHERE `id:id` = 10005
```

## The leeway surface (one-time install)

The `LW_*` names the next sections use are leeway's query vocabulary
(ADR-0171). Part of it is client-side — expanded by play before the statement
ships — and part is SQL user-defined functions the endpoint must carry. Ask
which revision a server has:

```sql
SELECT LW_SURFACE_VERSION()
```

An *unknown function* error means the server is not provisioned — it reads
like a typo and is not one. Install the whole surface (function pack,
read-back helpers, identity family, plus the version marker) with the CLI, or
pipe the same DDL through `clickhouse-client`:

```sh
boxer leeway sqlsurface install --url http://localhost:8123/
# or, offline:
boxer leeway sqlsurface print | clickhouse-client -n
```

The **Vocabulary** tab shows the same answer per function, marked against the
configured endpoint — including the client-expanded names whose *expansion*
needs the server-side families.

## Read a tagged attribute by its tag (`LW_GET`)

A tagged section stores attributes in parallel arrays; `LW_GET` locates one
**by its membership tag** and extracts the value, without a physical name in
sight. In this fixture every `symbol` attribute carries a tag on the
low-cardinality ref channel — the cyber events tag with the targeted port
(22, 443, 53), the drone events with the drone model (5), the sensor events
with the hardware version (12) — so one tag picks its rows out of all three
domains:

```sql
SELECT
  `id:naturalKey`                                AS entity,
  `symbol:value`                                 AS all_events,
  LW_GET_NULL('symbol', 22, 'chan:low-card-ref') AS event_on_port_22
FROM anchor.facts
WHERE LW_GET('symbol', 22, 'chan:low-card-ref') != ''
```

`LW_GET` returns the type default when the tag is absent (`''` here, which the
`WHERE` uses); `LW_GET_NULL` returns `NULL` instead, telling absent from
present-with-the-default. A section whose values are arrays or sets reads with
`LW_GET_LIST`. The `chan:` token is needed here because the fixture's sections
carry several membership channels; a single-channel section needs only the two
arguments.

The tag is written unquoted because a *ref* channel identifies memberships by
a `uint64` registry id, and this fixture uses domain numbers as those ids.
Quoting it — `'22'` — means the same thing on a ref channel and expands
identically; it is also the spelling a *verbatim* channel needs, since those
carry names rather than ids, and it is what lets a bound registry take the
tag as a *name* — see the next section.

A section with several value columns takes a `col:` token to say which one.
`geoPoint` carries three (`pointLat`, `pointLng`, `h3`), and its tag is the
attacker's autonomous-system number on the high-cardinality ref channel:

```sql
SELECT
  `id:naturalKey`                                                AS incident,
  LW_GET('geoPoint', 3360, 'chan:high-card-ref', 'col:pointLat') AS origin_lat,
  LW_GET('geoPoint', 3360, 'chan:high-card-ref', 'col:pointLng') AS origin_lng
FROM anchor.facts
WHERE LW_GET_NULL('geoPoint', 3360, 'chan:high-card-ref', 'col:h3') IS NOT NULL
```

Omit a token a call needs and the error lists the candidates rather than
guessing. Attributes that carry no tag at all (this fixture's `timeRange`,
`text`, `u64Array`) are read positionally through their handles —
`` `timeRange:beginIncl`[1] `` — as the **Timeline contract** section below
does.

## Read every attribute carrying a tag (`LW_SEL`)

`LW_GET` locates *one* attribute. Where a tag is carried by several, the
question is plural and the answer is a **selector**: `LW_SEL` returns the
positions the tag occupies in the membership lane, `LW_SEL_ATTRS` the
attribute indices, and you project whatever lane you want through them with
`LW_CO_GATHER`:

```sql
SELECT
  `id:naturalKey`                                                      AS entity,
  LW_CO_GATHER(`symbol:value`, LW_SEL_ATTRS('symbol', 22, 'chan:low-card-ref')) AS events_on_port_22,
  length(LW_SEL('symbol', 22, 'chan:low-card-ref'))                    AS how_many
FROM anchor.facts
WHERE has(`symbol:lr`, 22)
```

This is "argwhere + gather": select positions once, then every further lane —
including a co-section's — projects through the same selector. No `ARRAY JOIN`
is involved, so the row grain does not change and two tags can be read side by
side in one statement. The two selectors are co-indexed with each other, so
both pass to one lambda and stay aligned — that is what lets a membership-axis
lane and an attribute-axis lane be read together:

```sql
SELECT arrayMap((p, a) -> (`symbol:lr`[p], `symbol:value`[a]),
                LW_SEL('symbol', 22, 'chan:low-card-ref'),
                LW_SEL_ATTRS('symbol', 22, 'chan:low-card-ref')) AS tag_and_value
FROM anchor.facts
WHERE has(`symbol:lr`, 22)
```

Two things this fixture cannot show, said plainly rather than implied. Each of
its rows carries exactly one `symbol` attribute, so these arrays come back
with one element — the plural case is the norm on the **mixed** channels,
where the tag is shared by design and a `param:` token picks one attribute out
of the set (the canonical JSON mapping stores every `/tags/N` as the tag
`/tags/_` plus the index in the parameter lane). And the `has()` guard is not
decoration: a multi-lane `arrayFilter` is opaque to index analysis, so the
selector prunes nothing on its own.

`LW_SEL` returns indices, so it takes no `col:` token — gather the column you
want through the selector instead, which is also how one selector serves
several columns.

## Membership ids and names (`keelson('memberships')`)

A ref channel carries a `uint64` per tag. This fixture uses plain domain
numbers (ports, model ids), but boxer's own facts stores tag with the
process's membership registry, and those ids are not meant to be read or
typed. `keelson('memberships')` publishes that registry as a table — point the
**Endpoint** menu at *Keelson introspection* (or any `/query` node) and Run:

```sql
SELECT name, id, virtual, parents
FROM keelson('memberships')
ORDER BY name
```

Turn an id from a result column back into its name with the same table:

```sql
SELECT name, virtual FROM keelson('memberships') WHERE id = {id:UInt64}
```

Two things to know. The table publishes the **folded** spelling — a membership
declared as `naturalKey` lists as `natural-key` — so a join predicate must use
that form, though `LW_GET` accepts either. And a `virtual` row is a grouping
node that never appears on a lane: matching against one returns nothing, which
is a wrong question rather than missing data. In the write direction, `LW_GET`
on a ref channel takes the *name* and compiles it to the id before the
statement ships (play binds its registry for exactly this), so the SQL still
carries a constant. Where the id is what is at hand, it goes in as a plain
number — `LW_GET('metric', 6917529027641081861)` — and needs no registry.

## Ragged lanes (`LW_RAGGED_*`, `LW_CO_*`)

The flat streams behind a section regroup without hand-written array
arithmetic. Each `symbol` attribute here carries one or more ref tags; nesting
the flat `lr` stream by the per-attribute `lrcard` counts lines the tags up
with their attribute for a parallel `ARRAY JOIN`:

```sql
SELECT
  `id:naturalKey` AS incident,
  event,
  tags
FROM anchor.facts
ARRAY JOIN
  `symbol:value` AS event,
  LW_RAGGED_NEST(`symbol:lr`, `symbol:lrcard`) AS tags
WHERE hasAny(`symbol:value`, ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
```

The pack knows nothing about leeway — the lanes are ordinary arrays, so it
works on literals too, which is the quickest way to see what each function
does (this block needs only the installed pack, no table):

```sql
SELECT
  LW_RAGGED_NEST([10, 20, 30, 40, 50, 60], [2, 3, 1])                AS nested,
  LW_RAGGED_REDUCE('sum', [10, 20, 30, 40, 50, 60], [2, 3, 1])       AS sums,
  LW_RAGGED_EXISTS(v -> v > 45, [10, 20, 30, 40, 50, 60], [2, 3, 1]) AS any_gt_45,
  LW_CO_LOOKUP(['de', 'fr', 'it'], [83.2, 68.1, 59.0], 'fr')         AS co_lookup
```

`nested` regroups the stream into `[[10,20],[30,40,50],[60]]`, `sums`
aggregates per run without materializing the nesting, `any_gt_45` is the fused
per-run existence test, and `co_lookup` reads one lane at the position where a
sibling lane matches. Prefer the fused forms over nest-then-operate — nesting
copies the stream (ADR-0162 §SD4).

## Mint a leeway column (`LW_PLAIN`)

The write direction: a computed column with an ordinary alias stops being
leeway-shaped, but a constructor mints the physical name for it, so the result
classifies back into the schema — Detail renders the card again. `LW_PLAIN`
wraps the expression, names it, types it, and declares the item kind
(`item:oq`, an opaque plain value); it expands client-side into
`<expr> AS "<physical name>"`, so any endpoint runs the result:

```sql
SELECT
  `id:id`,
  `id:naturalKey`,
  LW_PLAIN(length(`symbol:value`), 'attack-count', 'u64', 'item:oq')
FROM anchor.facts
```

A constructor call is a projection item and nothing else — not aliased (the
minted name *is* the alias), not nested, not in a `WHERE`. The tagged-section
constructors (`LW_TV`, `LW_TV_MEMB`, `LW_TV_SUPPORT`) follow the same shape;
the *reading and authoring* how-to in the repository covers them.

## Write a slice back (`INSERT … SELECT`)

The whole authoring loop: an `INSERT INTO … SELECT` flows through the same
pipeline as a read, and because the destination is known, the constructor
mints **adopt the target's own column names** — spelling and aspect hints
included — instead of composing fresh ones. Check **Preview → As sent** to
see it: the wire body carries the target's physical names and, unlike a
read, no `FORMAT` clause.

Executing a write from play is gated: Run refuses until
`BOXER_PLAY_ALLOW_WRITES=1` is set (the refusal names the switch), and a
completed write reports its row count on the status line instead of filling
the result tabs. `anchor.silver` is created by the same integration seeding
that fills `anchor.facts`.

```sql
INSERT INTO anchor.silver
SELECT
  `id:id`,
  `id:naturalKey`,
  LW_TV(arrayMap(x -> upper(x), `symbol:value`), 'symbol', 'value', 's'),
  LW_TV_MEMB(`symbol:lr`, 'symbol', 'low-card-ref'),
  LW_TV_SUPPORT(`symbol:lrcard`, 'symbol', 'lrcard')
FROM anchor.facts
WHERE hasAny(`symbol:value`, ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
```

## Timeline contract

Map the `timeRange` section onto the canonical slot columns the Timeline tab
reads; timestamps must be `DateTime64`. The attributes carry no membership
tag, so this is positional reading — handles plus `[1]` — rather than a
`LW_GET`.

```sql
SELECT
  `timeRange:beginIncl`[1] AS _tl_time,
  `timeRange:endExcl`[1]   AS _tl_time_end,
  `symbol:value`[1]        AS _tl_lane
FROM anchor.facts
WHERE length(`timeRange:beginIncl`) > 0
ORDER BY _tl_time
```

## Timeline regions (background bands)

Unlike the others, this block belongs in the Timeline tab's **Background bands**
editor, not the main editor — Insert here would put it in the wrong box. Bands
return the `_tl_band_*` slots: a `from`/`to` `DateTime64` pair, a
`_tl_band_color` that must be an IDS token name (`neutral.default`,
`accent.default`, `warning.default`, `error.default`, …), and an optional
`_tl_band_label`. The `{tl_min:…}` / `{tl_max:…}` parameters carry the
events' time extent — the Timeline publishes them as signals after each
render — so a band can be sized relative to whatever the query returned. A
bands query that doesn't reference them runs on its own, without waiting for
events. This one shades the middle 50% of the visible window — adjust the
`0.25` / `0.75` fractions to move or resize the region.

`tl_min` / `tl_max` are a range pair (stem `tl`), so the editor offers one range
picker for the two of them — see **Time range** below. Filling it writes a `SET`
that pins the extent, which stops the Timeline driving it; clear the `SET` lines
to hand the range back.

```sql
WITH {tl_min:DateTime64(3, 'UTC')} AS lo,
     {tl_max:DateTime64(3, 'UTC')} AS hi
SELECT
  addMilliseconds(lo, toInt64(0.25 * dateDiff('millisecond', lo, hi))) AS _tl_band_from,
  addMilliseconds(lo, toInt64(0.75 * dateDiff('millisecond', lo, hi))) AS _tl_band_to,
  'accent.default'                                                     AS _tl_band_color,
  'mid 50% of window'                                                  AS _tl_band_label
```

## Ad-hoc columns

Aliased columns are not leeway-shaped, so Detail falls back to prefix grouping
and Table shows a plain grid. (An *unaliased* handle keeps its physical name
and stays leeway; the alias is what changes the shape — mint a real leeway
column instead with `LW_PLAIN`, above.)

```sql
SELECT
  `id:id`            AS id,
  `id:naturalKey`    AS natural_key,
  `symbol:value`[1]  AS event_type
FROM anchor.facts
ORDER BY id
```

## Time range (one picker from two bounds)

Two DateTime parameters that name the bounds of one range fold into a single
range control instead of two separate fields (ADR-0124). They pair by **stem**:
strip a `from`/`to`, `min`/`max`, `start`/`end`, `lo`/`hi` or `since`/`until`
suffix, and two placeholders left with the same stem are one range. Bare `from`
and `to` are the empty-stem case; `tl_min` / `tl_max` above is the same rule
with the stem `tl`.

Order does not matter and neither does distance — the two bounds can sit
anywhere in the query with anything between them. Both halves must be DateTime
or DateTime64; a mismatch gets two plain fields and the pane says why. Add a
`-- play: ungroup` comment to refuse the fold and edit the bounds as text.

```sql
SELECT toStartOfHour(now() - INTERVAL number MINUTE) AS bucket,
       count()                                       AS n
FROM numbers(600)
WHERE now() - INTERVAL number MINUTE
      BETWEEN {from:DateTime64(3, 'UTC')} AND {to:DateTime64(3, 'UTC')}
GROUP BY bucket
ORDER BY bucket
```

## Content-typed cells (markdown, images, code)

A column named `` `<label>@<mime>` `` renders its cell as that media type in the
**Detail** tab, instead of as the truncated one line every other ad-hoc column
gets — and in the **Table**, as its first line, or as `[image/png · 359 B]` for
an image (ADR-0123; since ADR-0186 these are the content family of the gloss
catalog — the next snippet). Run this, then click the row in **Table** — Detail
draws whatever the selection points at. Table-free; runs against any server.

Declared, never sniffed: nothing renders as markdown unless it says so. The
backticks are not optional — unquoted, both the `@` and the `/` are syntax
errors, which is the point of choosing them. Known types are `text/markdown`,
`text/plain`, `application/json`, `application/sql`, `text/x-go`,
`application/cbor`, `image/png`, `image/jpeg` and `image/gif`. A type outside that set, or a typo in one, renders
the cell plainly and says why rather than pretending. A column with an `@` but
no `/` — `dot_done@success`, an email address — is an ordinary column and is
left alone.

Image columns hold the encoded bytes verbatim: ClickHouse `String` is
byte-arbitrary, so a stored PNG round-trips untouched. `unhex` supplies one
here (a 16×16 PNG) in place of a `SELECT` from a blob column.

```sql
SELECT
  'boxer'                                          AS name,
  '# Heading\n\nA *rendered* cell — `code`, a [link](https://example.com), and:\n\n- a list\n- of items\n\n> a quote' AS `notes@text/markdown`,
  '{"lane":"proposed","dots":[0,1,4],"nested":{"ok":true}}' AS `req@application/json`,
  'SELECT count() FROM anchor.facts WHERE ts > now() - 3600' AS `q@application/sql`,
  'no wrapping,\nno truncation,\njust the bytes'   AS `stack@text/plain`,
  unhex('89504e470d0a1a0a0000000d4948445200000010000000100203000000629d17f200000009504c5445202020e6b55d3b6ea563f88312000000414944415478da620003a955ab9630a8ad5a35834173d5aa0c06adac952b18b4662d5bc1a0b56cd60a06ad95593002cc054b8094801583b5810d000140000000ffff54231bef9464752c0000000049454e44ae426082') AS `shot@image/png`
```

## Glosses (units, check digits, masks, links)

A **gloss** is a named way of showing a value (ADR-0186): a temperature with
its unit, a Unix epoch as a moment, a span as `1m 05s`, a card number masked
with its Luhn verdict, a value masked to six bullets, a URL as a link, a byte
count in KiB, an IP address written out. Glosses render in the **Table** grids
(one line, sometimes toned) and in **Detail** (a block where the gloss has
one). Three ways to reach one, in precedence order:

- an alias, `` AS `label@gloss/temperature;unit=C` `` — the ADR-0123 spelling
  with a private `gloss/` type; parameters ride after `;` and are validated
  (`;unti=C` is refused out loud, never rendered as °C);
- the `gloss(expr, 'gloss/…', 'key', value…)` macro, which writes that alias
  for you — no backticks, typed parameter values, and an unknown gloss is a
  Diagnostics error at rewrite time; `'label', 'name'` names the alias;
- a `-- play: gloss <media type> <regex>` line, matched against each column's
  **spec line** — its name, section, role, type and aspects in the
  `LW_TV` token spelling (`name:temperature section:sensor role:val ct:f64
  sem:secret …`) — which is how a leeway column, whose name cannot be aliased
  without losing its shape, gets one. Some glosses bring a rule along:
  `gloss/masked` for `sem:secret`, `gloss/url` for `sem:url`, `gloss/ipaddr`
  for a `ct:v` / `ct:w` address column.

The **Glosses** tab lists the catalog, the buffer's rules, and how each column
of the current result resolved; **Raw cells** on the Table toolbar switches
every gloss off for the session. Table-free; runs against any server. The
last column here carries a typo on purpose.

```sql
-- play: gloss gloss/length;unit=m name:height
SELECT number AS n,
       toUInt64(12393906174523604992) + number + 1 AS `id@gloss/taggedid`,
       gloss(20 + number * 1.7, 'gloss/temperature', 'unit', 'C', 'label', 'temp'),
       1.5 + number * 0.31 AS height,
       ['4111111111111111', '4111111111111112', '378282246310005'][1 + number % 3] AS `card@gloss/luhn`,
       1024 * number * number AS `size@gloss/bytes`,
       'hunter2' AS `pw@gloss/masked`,
       toUnixTimestamp(now()) + number * 86400 AS `when@gloss/epoch`,
       number * 90500 AS `took@gloss/duration;unit=ms`,
       'https://example.com/' || toString(number) AS `link@gloss/url`,
       toIPv4('192.0.2.' || toString(number)) AS `host@gloss/ipaddr`,
       toIPv6('2001:db8::' || toString(number)) AS `peer@gloss/ipaddr`,
       20 + number * 1.7 AS `oops@gloss/temperature;unti=C`
FROM numbers(12)
```

The `id` column is tag value 12 over a counter, so it reads `c:1`, `c:2`, …
Select a row and open **Detail**: the tagged id spells its split out there —
tag value, code width, counter — with **Copy id** and **Copy hex** buttons.
Swap the expression for `toUInt64(4294967296)` to see the other half of the
contract: a `UInt64` carrying no fibonacci comma is not a tagged id, and shows
plain in the warning tone rather than pretending to split.

`host` and `peer` are declared here because this is a plain result: it carries
column names and Arrow types, and neither says an address is an address. Turn
**Raw cells** on to see what the gloss is reading — row 1's `host` is the
`UInt32` `3221225985`, its `peer` sixteen packed bytes. A **leeway** column
needs no declaration for this one: its spec line carries the canonical type
(`ct:v`, `ct:w`, and their CIDR and array spellings), which `gloss/ipaddr`
claims as an affinity.

## Parameter prelude

A top-level `SET param_*` statement rides the request URL; the `{name:Type}`
placeholder is substituted by ClickHouse.

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`symbol:value`, {event:String})
```

## A parameter whose value is SQL

`Expr` and `ExprList` hold SQL rather than a value, so the playground
substitutes them into the query text before sending it; `Identifier` is
ClickHouse's own name parameter and rides the URL like any value. Each gets a
one-line SQL field above the editor — edit `cond` there and the `-- play: expr`
line follows.

The declarations sit **below** the `SET` prelude on purpose: a comment above it
ends the prelude, and the query would then run without its parameters.
Table-free, so it runs against any server.

```sql
SET param_col = 'number';
-- play: expr cond = number % 3 = 0
-- play: expr cols = number AS n, number * 2 AS doubled
SELECT {cols:ExprList}, {col:Identifier}
FROM numbers(12)
WHERE {cond:Expr}
```

## Signals (unbound parameter)

A placeholder with no `SET` is a live signal. Run refuses while nothing fills
`lim` (the status bar says what to do): open the Graph tab's **signals**
section, give `lim` a value, press **set**, then Run. Check **Live** in the
top bar and further **set** clicks re-run the query by themselves.

```sql
SELECT * FROM anchor.facts LIMIT {lim:UInt64}
```

## World choropleth (countries)

A result with a country column (ISO 3166 alpha-2/alpha-3 codes or country
names) plus a numeric column lights up the **World** tab (ADR-0114): countries
fill by value, hover shows `name · value`, clicking a country selects its row.
Table-free — the `values()` literal runs against any server. The `'XK'` row
exercises the code path, and `'atlantis'` deliberately resolves nowhere so the
status line shows an unmatched count. Values are illustrative, not
authoritative statistics.

```sql
SELECT *
FROM values('country String, population Float64',
  ('Germany', 83.2), ('France', 68.1), ('Brazil', 216.4),
  ('United States', 334.9), ('India', 1428.6), ('China', 1411.8),
  ('Australia', 26.5), ('Norway', 5.5), ('Egypt', 112.7),
  ('Japan', 124.5), ('Mexico', 128.5), ('Russia', 144.4),
  ('South Africa', 60.4), ('Canada', 40.1), ('Argentina', 46.7),
  ('XK', 1.7), ('atlantis', 0.1))
```

## Kanban board (lanes and cards)

A result naming a `lane` and a `title` column renders as a board in the
**Kanban** tab (ADR-0122). `subtitle` is optional, and up to three `dot_<label>`
integer columns become a tally of coloured dots along each card's bottom edge.
Lanes appear in first-seen row order, so `ORDER BY` decides the layout;
clicking a card selects its row, so Detail and Table follow. Both blocks are
table-free and run against any server.

The first block is the contract itself. `@success`, `@warning` and `@disabled`
name design-system tones — `accent`, `error`, `info` and `neutral` are the rest.
The backticks are not optional: unquoted, the `@` is a syntax error. A zero
tally paints no dot, so the `ADR-0066` row carries none at all, and the last
row's empty `lane` lands in a `(none)` column. `indexOf` returns 0 for a value
its list does not carry, which is why the `= 0` sort key comes first — without
it an unrecognised lane sorts *before* the canonical ones rather than after.

```sql
WITH ['proposed', 'accepted', 'superseded', 'withdrawn', 'deferred'] AS lifecycle
SELECT *
FROM values('lane String, title String, subtitle String,
             `dot_done@success` UInt64, `dot_cited@warning` UInt64, `dot_todo@disabled` UInt64',
  ('proposed',   'ADR-0122 — kanban result pane',   '2026-07-15',   0, 1, 4),
  ('proposed',   'ADR-0112 — DimensionStore',       '2026-07-09',   0, 0, 3),
  ('accepted',   'ADR-0114 — world choropleth',     '2026-07-11',   7, 0, 0),
  ('accepted',   'ADR-0097 — reactive query graph', '2026-07-02',   6, 2, 1),
  ('accepted',   'ADR-0066 — DQL read-back',        '2026-06-24',   0, 0, 0),
  ('superseded', 'ADR-0085 — operator break-glass', '→ ADR-P-0001', 2, 1, 3),
  ('withdrawn',  'ADR-0010 — leeway CBOR codec',    '2026-05-02',   0, 0, 2),
  ('',           'ADR-0999 — (frontmatter has no status)', '',      0, 0, 0))
ORDER BY indexOf(lifecycle, lane) = 0, indexOf(lifecycle, lane)
```

Real boards are aggregations rather than literal tuples: one row per card, with
the dots built by `countIf` over that card's parts. Dropping the `@token`
colours a dot from the ramp by its position instead, and the ramp is tuned for
exactly this reading — the three below come out the same green, amber and grey
the block above names explicitly.

The three `countIf`s are worth reading closely. This board was a Go program
once, and there the buckets were a first-match switch, where the rule "an
author's ✓ outranks code evidence" was implicit in the case order — invisible in
the code that implemented it. SQL has no case order to inherit, so the same rule
has to be said out loud: `NOT done AND code_refs > 0`. The buckets are disjoint
and sum to `count()` either way; only one of the two forms *can* leave the rule
unsaid.

```sql
WITH
  ['proposed', 'accepted', 'superseded'] AS lifecycle,
  sub AS (
    SELECT * FROM values(
      'adr String, status String, marker String, done Bool, code_refs UInt32',
      ('ADR-0122', 'proposed', 'SD1', false, 2), ('ADR-0122', 'proposed', 'SD2', false, 1),
      ('ADR-0122', 'proposed', 'SD3', false, 0), ('ADR-0122', 'proposed', 'SD4', false, 0),
      ('ADR-0114', 'accepted', 'SD1', true,  3), ('ADR-0114', 'accepted', 'SD2', true,  1),
      ('ADR-0114', 'accepted', 'SD3', true,  4), ('ADR-0114', 'accepted', 'SD4', false, 2),
      ('ADR-0097', 'accepted', 'SD1', true,  5), ('ADR-0097', 'accepted', 'SD2', false, 0),
      ('ADR-0085', 'superseded', 'M1', true, 1), ('ADR-0085', 'superseded', 'M2', false, 0))
  )
SELECT
  status                              AS lane,
  adr                                 AS title,
  concat(toString(countIf(done)), ' of ', toString(count()), ' declared done') AS subtitle,
  countIf(done)                       AS dot_done,
  countIf(NOT done AND code_refs > 0) AS dot_cited,
  countIf(NOT done AND code_refs = 0) AS dot_todo
FROM sub
GROUP BY adr, status
ORDER BY indexOf(lifecycle, lane) = 0, indexOf(lifecycle, lane), title
```

## ADR board (this repository's decisions)

This repository's own decision corpus, as a board — and, click a card, the
decision itself. It needs no setup and no ClickHouse: `keelson('adr')`,
`keelson('subtask')` and `keelson('adrcontent')` read `doc/adr` in-process, so
point the **Endpoint** menu at *Keelson introspection* and Run. The rows are the
same ones `boxer adr` emits, under the same names — a query written here runs
verbatim against its Arrow dump, and a test pins the two schema sets equal
(ADR-0122 §SD4).

The tables read the corpus per query, so an edited ADR shows up on the next Run;
that costs about half a second, most of it parsing rather than the citation
scan.

The last joined column is what makes the board readable rather than only
countable. `keelson('adrcontent')` carries each decision's markdown source, and
its column declares its media type in its *name*, so the **Detail** tab renders
the selected card's ADR as prose with no alias written here (ADR-0123 §SD2).
The join is `LEFT` on purpose: a decision whose file cannot be read drops out of
`adrcontent` rather than arriving blank, and an inner join would silently take
its card off the board with it.

That column is not free, and the price is worth knowing before you copy this.
Measured on this corpus through the in-process endpoint, the board without it
runs in ~63 ms for an 11 KB result; with it, ~122 ms for ~1.9 MB. Naming
`adrcontent` reads every ADR — a projection prunes columns, never rows — so the
board carries all 141 decisions to show you the one you clicked. Drop the last
join line and the `c.` column to get the cheap board back; nothing else changes.
The **Table** tab is where the size shows, since it renders whole cells rather
than one selection. Stay on **Kanban** and **Detail** and you will not notice.

The `lanes` CTE is the board's lane vocabulary: its rows are the lanes, in
order, whether or not a card sits in one — which is how the board says "nothing
is withdrawn" rather than dropping the lane. A status `lanes` does not name is
appended after them rather than lost, so a new word in the corpus shows up on the
board instead of vanishing from it. Nothing in the main `SELECT` references the
CTE; an unused CTE is legal and costs the query nothing.

The three `countIf`s are the whole board. `done` is the author's `✓` — the only
claim of completion — and `code_refs > 0` is evidence that something cites the
sub-item by its `§marker`, which is weaker. The buckets are disjoint and sum to
`count()`, and the order matters: a `✓` outranks evidence, so the cited bucket
has to say `NOT done` out loud. The `n_done` aliases must not be named `done`:
ClickHouse substitutes a `SELECT` alias into the expression that defines it, so
`countIf(done) AS done` becomes `countIf(countIf(done))` and is rejected as a
nested aggregate.

```sql
WITH
  lanes AS (
    SELECT arrayJoin(['proposed', 'accepted', 'superseded', 'withdrawn', 'deferred']) AS lane
  ),
  tally AS (
    SELECT num,
           countIf(done)                       AS n_done,
           countIf(NOT done AND code_refs > 0) AS n_cited,
           countIf(NOT done AND code_refs = 0) AS n_todo
    FROM keelson('subtask')
    GROUP BY num
  )
SELECT
  if(a.status = '', '(no status)', a.status)                            AS lane,
  concat('ADR-', leftPad(toString(a.num), 4, '0'), ' — ', a.title)      AS title,
  if(a.superseded_by != '', concat('→ ', a.superseded_by), a.last_date) AS subtitle,
  t.n_done  AS `dot_done@success`,
  t.n_cited AS `dot_cited@warning`,
  t.n_todo  AS `dot_todo@disabled`,
  c.`content@text/markdown`
FROM keelson('adr') AS a
LEFT JOIN tally AS t ON t.num = a.num
LEFT JOIN keelson('adrcontent') AS c ON c.num = a.num
ORDER BY a.num
```

The sub-item worklist the board's amber dots point at — cited by code, and not
declared done by anyone:

```sql
SELECT s.num, s.marker, s.code_refs, substring(s.title, 1, 60) AS title
FROM keelson('subtask') AS s
WHERE s.code_refs > 0 AND NOT s.done
ORDER BY s.code_refs DESC, s.num
LIMIT 25
```

## Read an ADR where you found it

The same join as the board above, without the board — a plain table narrowed to
what you want to read, and then the pattern for searching the prose instead.
`keelson('adrcontent')` carries one row per decision with the source text whole,
and the column declares its media type in its *name*, so **Detail** renders it as
markdown with no alias written here at all (ADR-0092, ADR-0123 §SD2). Same
no-setup path: point **Endpoint** at *Keelson introspection*, Run, then click a
row in **Table**.

The backticks around the content column are not optional. Unquoted, `@` is a
syntax error rather than a plausible column, which is what makes a forgotten
backtick fail at once instead of quietly producing a plain cell.

Naming this table reads the whole corpus — a few megabytes of markdown — which
is why the source lives apart from `keelson('adr')` rather than as a column of
it, and why the `WHERE` here does not make it cheap: the rows are read before
ClickHouse filters them. Keep the row count small for reading, not for speed.

```sql
SELECT
  concat('ADR-', leftPad(toString(a.num), 4, '0')) AS id,
  a.status                                         AS status,
  a.title                                          AS title,
  c.`content@text/markdown`
FROM keelson('adr') AS a
JOIN keelson('adrcontent') AS c ON c.num = a.num
WHERE a.status = 'proposed'
ORDER BY a.num
```

To search the prose instead of reading it, filter on the same column and select
only the metadata — the corpus is still read, but nothing large comes back:

```sql
SELECT c.num, a.title
FROM keelson('adrcontent') AS c
JOIN keelson('adr') AS a ON a.num = c.num
WHERE c.`content@text/markdown` ILIKE '%airgap%'
ORDER BY c.num
```

## Search all the documentation at once (`docsearch`)

The `ILIKE` above sweeps one corpus with one literal. `docsearch('…')`
(ADR-0164) is a table-function macro that expands into one search over three
corpora at section grain: the registered help books
(`keelson('helpsections')`), the decision corpus (`keelson('adrsections')`),
and the executing engine's own `system.documentation` — so one query can
land on a snippets section, an ADR §, and a ClickHouse function. The
argument is a battery, exactly as the Help center's search box compiles it:
whitespace-separated case-insensitive RE2 patterns, all of which must hit
(a token that is not a valid regex matches literally). A token naming a
ClickHouse function alias or a launcher keyword also matches its canonical
spelling — `lcase` finds docs that only write `lower` — folded into the
token's own pattern, so "all must hit" is unchanged. Scoring matches the
GUI tiers — title 8, heading 4, body 1 per pattern — and `ref` is the
canonical reference (`help://…`, `adr://…`, `chdoc://…`). Edit the string
inside `docsearch('…')` to change the query; `ORDER BY` and `LIMIT` are
yours to write, the macro adds none.

Same no-setup path as the ADR tables: point **Endpoint** at *Keelson
introspection* and Run. `chdoc` rows then describe the in-process engine's
ClickHouse version; run against a remote server and they describe *that*
server's, which is the honest answer either way.

```sql
SELECT source, ref, title, heading, score, context
FROM docsearch('deduplicate argMax')
ORDER BY score DESC
LIMIT 50
```

## Decision graph (ADRs and the code that cites them)

The same corpus as a node-link graph in the **Network** tab (ADR-0129), and the
same no-setup path as the board above: `keelson('adr')` and `keelson('coderef')`
read `doc/adr` in-process, so point the **Endpoint** menu at *Keelson
introspection*, focus the Network tab, and Run.

The panel reads two CTEs of this query **by name**. `edges` — required — carries
a `source` and a `target` column; here one arc per ADR→package citation, its
`label` the number of references. `vertices` — optional — decorates the nodes it
names: an `id` (the key `source`/`target` reference), a `label`, a `group` that
colours the node, and a `shape`. So each ADR is a `box` in one colour and each
package an `ellipse` in another. The package `id` is the full import path (unique,
so the edges resolve) while its `label` is the last two segments (readable) — a
`vertices` row can name a node more briefly than its key. Drop the whole
`vertices` CTE and the panel still draws, inferring plain nodes from the edge
endpoints alone.

The picked ADRs fan out to the packages that cite them, and `apps/play` is the
hub they all reach — that shared node is what makes this a graph rather than a
row of separate stars. Edit the `num IN (…)` list to point it at a different
slice of the corpus.

```sql
WITH
  picked AS (
    SELECT num FROM keelson('adr') WHERE num IN (97, 114, 122, 123, 124, 129)
  ),
  refs AS (
    SELECT num, pkg, count() AS n
    FROM keelson('coderef')
    WHERE pkg != '' AND num IN (SELECT num FROM picked)
    GROUP BY num, pkg
  ),
  vertices AS (
    SELECT concat('ADR-', leftPad(toString(num), 4, '0')) AS id,
           concat('ADR-', leftPad(toString(num), 4, '0')) AS label,
           'decision'                                     AS `group`,
           'box'                                          AS shape
    FROM picked
    UNION ALL
    SELECT DISTINCT
           pkg                                                            AS id,
           arrayStringConcat(arraySlice(splitByChar('/', pkg), -2), '/')  AS label,
           'package'                                                      AS `group`,
           'ellipse'                                                      AS shape
    FROM refs
  ),
  edges AS (
    SELECT concat('ADR-', leftPad(toString(num), 4, '0')) AS source,
           pkg                                            AS target,
           toString(n)                                    AS label
    FROM refs
  )
SELECT * FROM edges
```

## Flow diagram (Sankey / alluvial)

The **Sankey** tab draws a result as a flow-quantity diagram: ribbons whose
thickness is proportional to a conserved value (ADR-0159). It reads two CTEs of
this query **by name**, on the same footing as the Network tab above. `flows` —
required — carries `source`, `target` and `value`; that alone draws a diagram,
with the nodes inferred from the endpoints. `nodes` — optional — decorates the
ones it names: an `id` (the key the flows reference), a `label`, a `stage`, an
`order` within that stage, a `group` that colours by category, and a `tone`.
Duplicate `source`/`target` rows are summed rather than drawn twice, so an
un-aggregated flow table still totals correctly.

Supplying a `stage` for **every** node is what switches the reading from Sankey —
where the columns are derived from the graph and the order within one is relaxed
to reduce crossings — to alluvial, where both are fixed by the query. The mode
control's *auto* setting is that rule; the other two settings override it.

Alluvial data usually starts one row per entity with one column per stage, which
is a pivot ClickHouse does natively — no panel support needed. Build the path as
an array and walk its adjacent pairs. Here each ADR code citation is one entity
whose path is *decision status → language → top-level area*, so the diagram
shows where the corpus's evidence actually sits. Same no-setup path as the two
sections above: point the **Endpoint** menu at *Keelson introspection*, focus the
Sankey tab, and Run.

The stage index is prefixed onto each node id (`1:go`) deliberately. It is what
keeps a category that recurs in two stages from collapsing into one node — and
with it, what keeps the diagram acyclic, since a flow that returns to an earlier
stage is rejected rather than drawn. The `label` drops the prefix again, so the
bars read plainly.

```sql
WITH
  paths AS (
    SELECT [a.status, r.lang, splitByChar('/', r.path)[1]] AS p
    FROM keelson('coderef') AS r
    INNER JOIN keelson('adr') AS a ON a.num = r.num
    WHERE r.lang != '' AND r.path != ''
  ),
  flows AS (
    SELECT e.1 AS source, e.2 AS target, count() AS value
    FROM (
      SELECT arrayJoin(arrayMap(
               i -> (concat(toString(i - 1), ':', p[i]), concat(toString(i), ':', p[i + 1])),
               range(1, length(p)))) AS e
      FROM paths
    )
    GROUP BY source, target
  ),
  nodes AS (
    SELECT concat(toString(x.1), ':', x.2) AS id,
           x.2                             AS label,
           x.1                             AS stage
    FROM (
      SELECT arrayJoin(arrayMap((i, v) -> (i - 1, v), arrayEnumerate(p), p)) AS x
      FROM paths
    )
    GROUP BY id, label, stage
  )
SELECT * FROM flows ORDER BY value DESC
```

If the data is already long — one row per entity per stage — `groupArray` and
`arraySort` rebuild the same `p` array and the rest is unchanged:

```sql
SELECT arrayMap(x -> x.2, arraySort(x -> x.1, groupArray((stage, category)))) AS p
FROM journey
GROUP BY entity
```

## Where the disk went (a two-hop flow)

The same Sankey tab over a question every server can answer with no fixtures at
all: which databases and tables are holding the bytes. Every active part belongs
to exactly one disk, one database and one table, so each byte flows the whole
width of the diagram exactly once — which is what makes the node bars honest
subdivisions rather than three unrelated bar charts.

The two hops are written out and stacked with `UNION ALL`. That is the other
natural way to build `flows`, and when the stages are few and named it reads
better than the array pivot above; reach for the pivot when the stage count is
a property of the data rather than of the query.

Node ids are prefixed (`db:`, `tbl:`) because a database and a table can share a
name, and two stages sharing an id would fuse into one node — the same
collision the pivot's stage prefix avoids, arriving by a different route. The
`nodes` CTE hands the readable name back as a `label`, so the bars read plainly
while the ids stay unique. It carries no `stage`, so this is the Sankey reading:
the columns are derived, and they come out disk → database → table anyway.

One thing not to do: leave `value` a bare number. `formatReadableSize` would
make it text and the flow would be dropped for having no positive value. The
status line abbreviates the total for you.

A server with one large table and a long tail of small ones will report most of
its flows as too thin to read, which is the honest answer for a linear
thickness encoding. Add a `HAVING sum(bytes_on_disk) > …` to the second hop, or
group the tail, if you want the small tables collapsed rather than drawn at a
size you cannot see.

```sql
WITH
  parts AS (
    SELECT disk_name, database, table AS tbl, bytes_on_disk
    FROM system.parts
    WHERE active
  ),
  flows AS (
    SELECT concat('disk:', disk_name)         AS source,
           concat('db:', database)            AS target,
           sum(bytes_on_disk)                 AS value
    FROM parts
    GROUP BY source, target
    UNION ALL
    SELECT concat('db:', database)            AS source,
           concat('tbl:', database, '.', tbl) AS target,
           sum(bytes_on_disk)                 AS value
    FROM parts
    GROUP BY source, target
  ),
  nodes AS (
    SELECT DISTINCT concat('disk:', disk_name) AS id, disk_name AS label FROM parts
    UNION ALL
    SELECT DISTINCT concat('db:', database) AS id, database AS label FROM parts
    UNION ALL
    SELECT DISTINCT concat('tbl:', database, '.', tbl) AS id, tbl AS label FROM parts
  )
SELECT * FROM flows ORDER BY value DESC
```

## The same disk, as an icicle

The **Icicle** tab draws a result as a space-filling hierarchy: one row per
depth, each frame's *width* its value, children abutting under their parent
(ADR-0160). Turned upside down — root at the bottom — the same layout is a
flamegraph, which is the switch at the left of its control row.

Where the Sankey above reads these bytes as a *flow* between three stages, this
reads them as *containment*: a table is in a database is on a disk. Neither is
more correct. The flow answers "how much went from here to there"; the
hierarchy answers "what is big, and what is it part of".

The folded contract is one row per root-to-leaf path — `stack`, an array from
the outermost frame inward, and `value`, that path's own quantity. The panel
interns the paths into a trie, so the interior frames (the databases, the disk)
are synthesised and take the sum of what sits under them. Nothing needs
prefixing the way the Sankey's node ids did: a frame's identity is its whole
path, so a database and a table sharing a name cannot collide. `unit` is
optional and only labels the value axis.

```sql
SELECT [disk_name, database, table] AS stack,
       sum(bytes_on_disk)           AS value,
       'bytes'                      AS unit
FROM system.parts
WHERE active
GROUP BY stack
ORDER BY value DESC
```

Any delimited path is one function away: `splitByChar('/', path) AS stack`
turns a file path, a package path or a folded stack trace into the same shape.
A pprof capture published as an ad-hoc dataset already arrives in it — one row
per unique call stack — so `SELECT stack, value FROM keelson('<handle>')` needs
nothing added.

## One row per node (the other Icicle contract)

When the hierarchy already carries its parents — a self-join, a `WITH
RECURSIVE`, a table that stores a tree — say so directly instead of building
paths. `id`, `parent` and `value`, one row per node; an empty or NULL `parent`
marks a root, and `label` overrides the drawn text. Several roots are fine and
are laid out side by side, so a forest needs no synthetic root inventing a
total no row supports.

The two contracts are not interchangeable. This is the only one in which an
**interior** node can carry a value of its own. A database has none — its width
below is exactly the sum of its tables — but a directory with loose files in it,
or a profiled function with both its own samples and callees, does, and the
uncovered remainder of its bar is that value. The folded contract cannot say it:
every value there lands on a leaf.

```sql
WITH parts AS (
    SELECT database AS db, table AS tbl, sum(bytes_on_disk) AS bytes
    FROM system.parts
    WHERE active
    GROUP BY db, tbl
)
SELECT concat('db:', db) AS id,
       ''                AS parent,
       db                AS label,
       toFloat64(0)      AS value,
       'bytes'           AS unit
FROM parts
GROUP BY db
UNION ALL
SELECT concat('tbl:', db, '.', tbl) AS id,
       concat('db:', db)            AS parent,
       tbl                          AS label,
       toFloat64(bytes)             AS value,
       'bytes'                      AS unit
FROM parts
```

The ids are prefixed for the reason the Sankey's were, arriving by a different
route: `parent` has to name exactly one row, and a database and a table may
share a name. `toFloat64` on both branches because a `UNION ALL` takes its
column types from the first — a bare `0` there would make every table's byte
count an integer cast of it.

## The same hierarchy as area, and a second measure (Treemap)

The **Treemap** tab (ADR-0166) reads exactly the columns the two blocks above
produce — nothing here is a new contract. It spends both dimensions of the pane
on magnitude instead of one, which is the trade: a treemap discards the order
and the depth of a path, and gives back the room to say "what is big" and, at
the same time, "of what kind".

That second channel is an optional `color`. A **numeric** one drives a
continuous ramp; a **string** one drives the qualitative cycle. It is read
independently of `value`, so area and colour can disagree — which is the point,
because a cell that is large and off-colour is the thing you were looking for.

Here area is bytes and colour is the part format, so a table stored the
unexpected way stands out without changing what the picture measures:

```sql
SELECT [disk_name, database, table] AS stack,
       sum(bytes_on_disk)           AS value,
       any(part_type)               AS color,
       'bytes'                      AS unit
FROM system.parts
WHERE active
GROUP BY stack
ORDER BY value DESC
```

Swap `any(part_type) AS color` for `sum(rows) AS color` and the same picture
answers a different question: which tables are big on disk *without* holding
many rows. The cycle wraps past seven categories and the status line says how
many shared a colour, so a `color` with fifty distinct values is telling you to
group it first.

## Rolling the tail up into its parent (a cell of one's own)

A treemap is subdivided by its children, so a container that also has a
quantity *of its own* needs a rectangle for it — otherwise that quantity is
silently redistributed among the children and every one of them reads too
large. Give an interior `id` a non-zero `value` and it gets one, labelled with
its own name.

The node contract is the one that can say this. Here every table under a
mebibyte is dropped and its bytes are held at the database instead, so the
picture keeps its total while spending cells only on the tables worth one:

```sql
WITH p AS (
    SELECT database AS db, table AS tbl, sum(bytes_on_disk) AS bytes
    FROM system.parts
    WHERE active
    GROUP BY db, tbl
)
SELECT concat('db:', db)                        AS id,
       ''                                       AS parent,
       db                                       AS label,
       toFloat64(sumIf(bytes, bytes < 1048576)) AS value,
       'bytes'                                  AS unit
FROM p
GROUP BY db
UNION ALL
SELECT concat('tbl:', db, '.', tbl) AS id,
       concat('db:', db)            AS parent,
       tbl                          AS label,
       toFloat64(bytes)             AS value,
       'bytes'                      AS unit
FROM p
WHERE bytes >= 1048576
```

The threshold is a query decision, not a panel one: a roll-up you wrote is
reproducible and shows up in the total, where a cell the renderer dropped for
being too small to draw does neither.

## The same disk, as a tree you can walk (Files)

The **Files** tab (ADR-0200) reads one required column — `path` — and browses
the result as a file tree: a sortable listing of one directory at a time, or
the whole subtree as an outline, with a breadcrumb, a quick filter and the
arrow keys. It is the widget `tally` browses lading snapshots with, over
whatever a query returned.

`is_dir`, `size`, `mtime`, `link_target` and `is_symlink` are read by name when
the query projects them, and **every other column becomes a column of the
browser**, so what a query selects is what the listing shows. Here the parts of
the tables above are read as `database/table/part`, which is a hierarchy the
server has and no directory on disk does:

```sql
SELECT concat(database, '/', table, '/', name) AS path,
       bytes_on_disk                           AS size,
       modification_time                       AS mtime,
       part_type,
       rows
FROM system.parts
WHERE active
ORDER BY path
```

The databases and tables are **synthesised**: no row named them, so they carry
no size and no time of their own — a directory's size is a claim this result
did not make, and the `du` sections above are where totals belong. Clicking a
row publishes two things, and the difference is that split: `selection_key` is
the path of whatever was clicked, always, while `selection` — the row cursor
the **Detail** tab follows — moves only for an entry a row named. Detail is the
preview: a row here is metadata, not bytes.

## A snapshot, browsed (`fs()`)

The case the panel was built for. `fs()` (ADR-0198) projects the lading
snapshot store one row per entry, so its result browses directly, with the
store's own columns — the BLAKE3 hash, whether the text guarantee holds,
whether the bytes were stored inline or referenced — riding beside name, size
and modified. Nothing here is a listing this app assembled: it is the query.

`'*'` is every mount the caller may read, and the newest complete snapshot of
each; a mount id in its place browses one. Glossed columns answer the contract
under their label, so `size` still sizes the entries while rendering as bytes:

```sql
SELECT path,
       is_dir,
       size AS "size@gloss/bytes",
       mtime,
       ext,
       text,
       lower(hex(content_hash)) AS hash
FROM fs('*')
ORDER BY path
LIMIT 20000
```

This needs a store with something in it — `boxer fs snapshot` takes one — and
the **lading** book has the same query as a chapter, beside find, diff, history
and `du`. Two mounts under `'*'` merge into one tree; project `mount` and the
column says which, or name a single mount to keep them apart. The mount is
written here as a literal rather than as a `{m:String}` knob because a slot is
resolved from the prelude and this app harvests the prelude away before the
macro expands — the book's chapters take the knob and that is where it is
being fixed.

## Bars, lines and a heatmap (Chart)

The **Chart** tab (ADR-0172) is the plain chart the other panels leave
uncovered. It claims four column names — `x`, `y`, `z` and `series` — and reads
a result one of two ways depending on whether `z` is there. Only the heatmap
reading needs `x`: without it the rows number themselves, so anything carrying a
number can be drawn.

These blocks carry their own data, in the `values(...)` / `numbers(...)` style
the Kanban and World sections use, so each one runs on any server and draws the
same picture every time. The contract is about column names and types, not about
a dataset — and a demo whose numbers move underfoot cannot show you what the
axis did. Point them at your own tables by replacing the `FROM`.

Without `z` it reads **lanes**: `x` is the key, and *every other numeric column*
becomes a series labelled by its own column name. A `GROUP BY` with several
aggregates is already that shape, so nothing needs restructuring:

```sql
SELECT * FROM values(
  'x String, above_10k UInt32, below_10k UInt32',
  ('A320', 736, 349),
  ('B738', 922, 158),
  ('A21N', 455, 166),
  ('A20N', 419, 155),
  ('B38M', 390,  66),
  ('A319', 222, 129))
```

Two lanes, two legend entries reading `above_10k` and `below_10k` — the column
names, with nothing else naming them. `x` is a string here, so the axis is
**categorical**: the distinct values take positions in the order the rows
arrived, which is why the bars come out in the order they are written above.
That is deliberate — a string has no order of its own, so your `ORDER BY` is the
one it gets, and the panel will not sort behind your back.

A numeric or `DateTime64` `x` gets a **continuous** axis instead, the temporal
one labelled in UTC, and then the spacing between keys is real:

```sql
SELECT * FROM values(
  'x UInt16, fast Nullable(UInt32), slow Nullable(UInt32)',
  ( 0,  114, 3905), ( 5,   75, 3725), (10,  166, 2759),
  (15,  990, 2466), (20, 1855, 2022), (25, 2472, 1188),
  (30, 3468,  814), (35, 4361, 1127), (40,  994,  229),
  (45,   94,   21), (50, NULL,    2), (90, NULL,    1))
```

Two things follow from the axis being continuous. Bars take their width from
the smallest gap between distinct `x` values — 5 here — so they cannot overlap a
neighbour whatever the spacing is. And a hole in `x` stays a hole: nothing sits
between 50 and 90, and the axis shows that emptiness where a categorical axis
would have closed the distance up.

The two `NULL`s are the other half of the same honesty. Nothing was measured for
`fast` in the last two bands, and `NULL` is what stops the lane being drawn down
to the axis and read as a zero somebody counted. The status line counts them.

When the grouping key is a column rather than a set of aggregates, name it
`series` and the rows split into one drawn series each:

```sql
SELECT * FROM values(
  'x String, series String, y UInt32',
  ('eu', 'read', 120), ('eu', 'write', 45),
  ('us', 'read', 210), ('us', 'write', 80),
  ('ap', 'read',  90), ('ap', 'write', 30))
```

Now the legend reads `read` and `write` — the values of `series`, not a column
name — and each has its own three points. That is the same picture the wide form
above draws; which one you write is decided by the shape your query already has,
not by the panel.

With `z` present the reading changes: `x` and `y` become the two cell keys and
`z` the cell value, drawn as a heatmap with a colour legend under it.

```sql
SELECT number % 24                                          AS x,
       intDiv(number, 24) + 1                               AS y,
       toUInt32(50 + 30 * sin(number / 7) + 15 * cos(number / 3)) AS z
FROM numbers(168)
WHERE (number * 7) % 13 > 1
```

An hour-of-day by day-of-week grid, which is the shape a two-key `GROUP BY`
produces. The cells are **ordinal** — one column per distinct `x`, one row per
distinct `y`, all the same width — and the status line says so, because such a
grid is a matrix and reading its spacing as a numeric scale would be reading
something that is not there. Numeric keys sort ascending; string keys keep row
order, as on the categorical axis above.

The `WHERE` drops 26 of the 168 cells, and those stay **holes**: a cell no row
filled is transparent, so the plot's own grid lines show through it. It is not
drawn at the bottom of the colour ramp, because "nothing was observed here" and
"the lowest value" are different claims. The status line counts the empty cells.

In the lanes reading `x` is optional. With no column claiming it the rows are
**numbered 1, 2, 3 …** in the order the query returned them, which is the one
order every result has — so an ordinary ranking draws without being aliased for:

```sql
SELECT * FROM values(
  'name String, rows UInt64',
  ('alpha', 1200000), ('bravo', 980000), ('charlie', 870000),
  ('delta', 610000), ('echo', 520000), ('foxtrot', 348603))
```

`name` is a string, so it is neither the key (which is claimed by *name*, and
nothing here is called `x`) nor a lane (which has to be a number). The picture
is `rows` against rank, and the status line says the abscissa is the panel's
rather than yours. Alias the key `AS x` when you want it *labelled* — the
numbering is what happens when there is nothing to label with, not a way of
avoiding the question.

With a `series` column the numbering restarts inside each group, so groups of
unequal length overlay instead of being laid end to end. The cost is an
alignment nothing measured: the third row of one group is drawn beside the third
of another because they are both third. The status line discloses that too. A
heatmap gets no such courtesy — `x` and `y` there must be real columns, since
numbering the rows would put every row in a column of its own.

Two refusals are worth knowing before you meet them. A `NULL` value breaks the
line rather than being drawn across — the same rule the Series tab keeps, for
the same reason. And a repeated `(x, y)` pair rejects the whole heatmap instead
of letting the last row win: the fix is an aggregate over `z`, which is a
decision only the query can make.

Which mark is drawn is yours — the chips offer Bar, Line and Scatter (Heatmap
alone for a grid), starting from whichever suits the resolved types. Bars for
several series sit side by side within each slot.

## A number against time (Series)

The **Series** tab (ADR-0163) is the one panel that asks for no contract: give
it a time column and a number and it draws them. The first temporal column is
the x axis, every numeric column becomes a lane, and anything else is ignored.

One cast is unavoidable, and the panel says so when it is missing: a plain
`DateTime` reaches the client as a bare `UInt32` of epoch seconds, which
nothing distinguishes from a count — so wrap the bucket in `toDateTime64(…, 3)`.
`DateTime64` and `Date` carry their own types and need nothing.

```sql
SELECT toDateTime64(toStartOfMinute(event_time), 3) AS t,
       avg(query_duration_ms)                       AS avg_ms,
       quantile(0.99)(query_duration_ms)            AS p99_ms
FROM system.query_log
WHERE event_time > now() - INTERVAL 6 HOUR AND type > 1
GROUP BY t
ORDER BY t
```

Two lanes on one axis because both are milliseconds. Numbers of different
magnitudes share an axis badly — split those into two queries and bind a second
Series pane to the other, rather than watching one lane flatten the other.

## Grids, gaps, and what the panel will not do for you

The status line classifies the spacing before it draws: **regular**, **regular
with gaps**, or **irregular**. That is a finding about the data, not an error —
an irregular series still charts, time-true.

What the panel never does is fill. A missing minute is a break in the line, not
a segment drawn across it, because an interpolated sample is a value nothing
measured — and on this tab a value is something a detector may later score. The
fix is yours to write, and the refusal hint offers it with the measured step
already substituted:

```sql
SELECT t, v
FROM (
    SELECT toDateTime64(toStartOfMinute(event_time), 3) AS t, count() AS v
    FROM system.query_log
    WHERE event_time > now() - INTERVAL 6 HOUR
    GROUP BY t
)
ORDER BY t WITH FILL STEP INTERVAL 1 MINUTE
```

`WITH FILL` inserts the absent minutes as rows with a zero `v`. If the honest
value is "unknown" rather than "zero", leave the gap: the break says something
the zero would hide.

## Long series, and why the picture is still exact

Above a couple of points per pixel the panel draws a per-pixel **min/max
envelope** — both extremes of every pixel column, rather than a selection of
representative samples. The distinction matters on exactly the data you opened
the panel for: a decimator that picks samples can drop a one-sample spike, and
an envelope cannot. Hover, selection and every analysis read the full series
regardless; only the drawing is reduced, and the status line says by how much.

For a range too long to fetch at all, decimate in SQL instead — ClickHouse
ships `lttb`, which returns real samples (never interpolated ones), at the cost
of non-uniform spacing:

```sql
SELECT arrayJoin(lttb(2000)(t, v)) AS point
FROM (
    SELECT toDateTime64(toStartOfMinute(event_time), 3) AS t, count() AS v
    FROM system.query_log
    GROUP BY t
)
```

Its output is deliberately not on a grid, so it is a way to *look* at a long
range — not an input to analysis, which needs the spacing `WITH FILL` or
`toStartOfInterval` gives it.

## Analysis you spell in SQL, run in play (the `ts*` vocabulary)

A CTE whose whole body is a `ts*` call is computed **client-side** (ADR-0163).
ClickHouse cannot express a matrix profile or a left-discord scan at all, so
play runs those itself — but the invocation still lives in the buffer, which
means it is recorded, replayable, pinnable and readable by anyone who opens
the query later. Nothing is hidden in a panel control.

```sql
WITH
  base AS (
    SELECT toDateTime64(toStartOfMinute(event_time), 3) AS t, count() AS v
    FROM system.query_log
    WHERE event_time > now() - INTERVAL 12 HOUR
    GROUP BY t
    ORDER BY t
  ),
  scores AS (SELECT tsAnomalyScores(t, v, 60) FROM base),
  spans  AS (SELECT tsAnomalySpans(t, v, 60, 3) FROM base)
SELECT * FROM base
```

Those two CTE names are not decoration. The **Series** tab fills its optional
channels BY NAME — `scores` and `spans` — the way the Sankey takes its `flows`
and `nodes`, so calling them anything else leaves the overlays empty. The sink
selects from `base`, which is the series being charted.

Four functions ship. `tsSmooth(t, v, halfWidth)` and `tsProfile(t, v, window)`
are two-sided — every value sees the whole series, so they describe the data
but are not what an alert would have known. `tsAnomalyScores(t, v, window)` is
causal: each score uses only what came before it, which is what makes replaying
it a backtest rather than a recap. `tsAnomalySpans(t, v, window, k)` reports the
top-k flagged extents directly as Timeline bands.

## What the overlays add, and what they refuse to leave out

With those two CTEs present the Series tab draws the score on its own plot,
x-linked to the series above it, and the flagged extents as bands behind both.
Three things come with it whether or not you asked:

- a **baseline** — a moving-average residual at the detector's own window,
  drawn in grey beside the score. A detector's curve alone always looks
  impressive; beside the one-liner it has to beat, it has to earn the
  difference. If the panel cannot compute it, it says why rather than quietly
  omitting it.
- the **warm-up region shaded**, because a detector that has not trained yet
  reports zeros, and a flat zero is indistinguishable from a quiet period.
- a **causality label** from the function itself. `tsAnomalyScores` is causal,
  so replaying it IS a backtest; `tsProfile` is two-sided and says so, because
  a two-sided score read as an alert history is hindsight wearing a uniform.

## Adjudicating a flagged span, and the number it buys

Under the extents is a row each, with **confirm** and **false alarm**. Marking
one appends a row to `boxer.tslabels` — never an update, so changing your mind
is recorded as a change of mind, with its own timestamp; the panel reads the
latest verdict per span.

A verdict is attached to the **compiled input**, not to the query text. Reword
the buffer and the labels follow; change what the input actually computes, or
the detector's window, and they correctly stop applying, because a span flagged
at one window was not adjudicated at another.

Once a span is confirmed the panel scores the detector AND its baseline on the
adjudicated spans, and says which is ahead. That is the point of the exercise:
the readout is equally able to report that the moving-average one-liner won, on
your data, which is the only version of the claim worth having. It leads with
VUS-PR and annotates VUS-ROC with its usable band — under the buffered measure
a random scorer lands near 0.55 and a perfectly-located one near 0.92, so
reading it as 0-to-1 flatters everything. A handful of spans is a reading
rather than evidence, and the line says so until there are more.

The table is ordinary SQL, so the labels are queryable like anything else:

```sql
SELECT input_hash, detector, window,
       argMax(verdict, created_at) AS verdict,
       count()                     AS revisions
FROM boxer.tslabels
GROUP BY input_hash, detector, window, span_from, span_to
ORDER BY input_hash, span_from
```

## A fixture with known ground truth (the lab)

The Series tab's **fixture lab** generates a labelled synthetic series from a
kind and a seed, and publishes it as two ordinary ad-hoc datasets:
`keelson('fixture_series')` with `t` and `v`, and `keelson('fixture_truth')`
with the planted extents on the same `_tl_band_*` contract the detectors emit.

There is no demo mode. The tables are queried like anything else, every panel
behaves exactly as it does on real data, and nothing downstream knows the data
is synthetic — which is the only way a workbench can tell you what it will do
on yours. What the fixture adds is **ground truth**: the M3 readout scores a
detector against a human's adjudication, and `fixture_truth` is a second
opinion nobody had to earn, so the two can be compared.

The generator is the one behind ADR-0150's scoring work, chosen because its
fixtures avoid the four flaws that let a trivial detector look good on
synthetic data — a detector that wins here has not simply learned that
anomalies come last. Same (kind, seed), same series, every time.

The lab needs the ad-hoc capability; without it the affordance is absent
rather than offering a button that could not work.

## Reading a client node, and why the sink cannot

A `ts*` CTE is a **terminal leaf**: its output never exists as SQL, so nothing
downstream can select from it. `SELECT * FROM scores` is a loud error, not a
silent empty — the fix is to point a pane at the CTE instead, from the Graph
tab's *fill tab* row, and the error says so.

The body is exactly `SELECT <one ts* call> FROM <one cte>`. A `WHERE`, an
`ORDER BY` or a second select item is refused rather than ignored: the body is
replaced by the transform, so anything else there would quietly do nothing.
Shape the input in the input CTE, where it is visible and runs on the server.

The Graph tab badges such a node *computed in play* and says what was actually
sent; the Preview pane's **As sent to server** view says it again. If your
server happens to have a function of the same name, play's wins inside play and
the caption tells you so.

## Query outcomes (an alluvial over the query log)

Where the server's own traffic goes: how a query arrived, what kind it was, and
how it ended. `system.query_log` is on by default, so this needs no setup
either.

Every node here carries a `stage`, so the panel takes the alluvial reading
without being told — the columns are the query's life stages, in the order the
query passed through them, and nothing may reorder them to reduce crossings.
The stages are written by hand rather than pivoted out of an array, which is
worth seeing beside the pivot: three named stages do not need that machinery.

On a healthy server the exception outcomes will be *below* a pixel and the
status line will say how many flows were too thin to read — a failure rate of a
fraction of a percent is exactly the thing this form cannot draw, and it says so
rather than rounding it up to something visible. That is also what makes the
picture worth keeping: the exceptions peel off the same flow that carries the
successes, so when they do grow into a readable ribbon you are seeing a share of
the traffic and not a number on a chart of its own. To read the tail on a quiet
log, filter to one `kind` first.

```sql
WITH
  q AS (
    SELECT if(interface = 1, 'native', 'http') AS via,
           query_kind                          AS kind,
           toString(type)                      AS outcome
    FROM system.query_log
    WHERE type != 'QueryStart' AND query_kind != ''
  ),
  flows AS (
    SELECT concat('0:', via) AS source, concat('1:', kind) AS target, count() AS value
    FROM q
    GROUP BY source, target
    UNION ALL
    SELECT concat('1:', kind) AS source, concat('2:', outcome) AS target, count() AS value
    FROM q
    GROUP BY source, target
  ),
  nodes AS (
    SELECT DISTINCT concat('0:', via) AS id, via AS label, 0 AS stage FROM q
    UNION ALL
    SELECT DISTINCT concat('1:', kind) AS id, kind AS label, 1 AS stage FROM q
    UNION ALL
    SELECT DISTINCT concat('2:', outcome) AS id, outcome AS label, 2 AS stage FROM q
  )
SELECT * FROM flows ORDER BY value DESC
```

## Distribution summary (`descriptiveStatistics`)

The **Distribution** tab draws a result as a distribution rather than a table:
an ECDF with a confidence band, a shift function against a chosen baseline, and
a letter-value (boxen) column per series (ADR-0161). It claims a result by
column names — `series`, `n`, `ps`, `qs` — so hand-written SQL that emits them
is on the same footing as the macro here.

`descriptiveStatistics(...)` writes those columns for you. It expands before the
query ships, into one `UNION ALL` branch per argument column, each carrying the
original `FROM` / `WHERE` / `GROUP BY`. Table-free, so this one runs against any
endpoint:

```sql
WITH sample AS (
  SELECT exp(randNormal(0, 1)) AS latency_ms
  FROM numbers(100000)
)
SELECT descriptiveStatistics(latency_ms)
FROM sample
```

One row comes back per series: the probability grid in `ps` and its quantiles in
`qs` (87 levels, p from 1.5e-5 to 1 − 1.5e-5), `n` and the null count beside
them, the sample moments, and the estimator that produced the quantiles —
`tdigest` unless you name another as a leading string argument (`'exact'`,
`'gk'`, `'dd'`). The `series` label is the argument's own text, which is why
aliasing it in a CTE is worth the line; it arrives quoted, because the
pre-execute canonicaliser quotes identifiers.

Three rules the expansion enforces, each a loud error before anything reaches
the server:

- The call must be the **sole select item** — not aliased, not beside other
  expressions, one call per statement. There is no merged output shape.
- `ORDER BY`, `LIMIT`, `HAVING` and `QUALIFY` have no home across the
  expansion's branches. Do their work in a CTE instead — the last section below
  does exactly that.
- `GROUP BY` is carried, and its key values fold into the `series` label. Name
  the expressions rather than their positions.

## Same mean and sd, four different shapes

Four columns in one call become four series over the same `FROM`. All four
samples are standardised by construction — mean 0, sd 1 — so those two moments
agree to about three decimals, and that agreement is most of what they have to
say about the four samples.

```sql
WITH shapes AS (
  SELECT
    randUniform(-1.7320508075688772, 1.7320508075688772) AS uniform,
    randNormal(0, 1)                                     AS gaussian,
    randUniform(0, 1)                                    AS u,
    if(u < 0.5, log(2 * u), -log(2 - 2 * u)) / sqrt(2.)  AS laplace,
    randExponential(1) - 1                               AS exponential
  FROM numbers(200000)
)
SELECT descriptiveStatistics(uniform, gaussian, laplace, exponential)
FROM shapes
```

The ECDF view separates them at a glance; **Boxen** says where. The observed
range runs ±1.73 for the uniform, about ±4.4 for the Gaussian and about ±8 for
the Laplace — one scale parameter, three different tails — while the exponential
cannot go below −1 and reaches +11 or so above. `kurt` is the moment that tracks
that ordering (ClickHouse reports it unstandardised, so ≈ 1.8 / 3.0 / 6.0 / 9.2
here) and `skew` separates only the exponential. Neither says the uniform is
bounded or that the Laplace has a cusp at its median.

Four series is also where the band policy changes: up to three, every series
carries its confidence band; beyond that only the selected one does, and the
rest draw as curves.

Two constructions above are worth keeping. The Laplace comes from a single
uniform pushed through its quantile function, because ClickHouse folds identical
expressions into one — `randExponential(1) - randExponential(1)` is one draw
minus itself and yields a column of zeros. That same folding is what makes `u` a
single draw across the three places it appears. And `randNormal`'s second
argument behaves as the standard deviation rather than the variance its
reference names (measured against 26.7); `randNormal(0, 1)` means the same thing
under either reading, which is why it is the one used here.

## Two treatments and the shift function

`GROUP BY` makes each group a series, so a controlled comparison is one call.
Three arms of the same lognormal latency: a baseline, the baseline plus a
constant 0.5 ms, and the baseline scaled by 1.25.

```sql
WITH
  draws AS (
    SELECT ['A baseline', 'B shift +0.5', 'C scale ×1.25'][1 + (number % 3)] AS arm,
           exp(randNormal(0, 1))                                             AS draw
    FROM numbers(300000)
  ),
  trial AS (
    SELECT arm,
           multiIf(arm = 'B shift +0.5',  draw + 0.5,
                   arm = 'C scale ×1.25', draw * 1.25,
                   draw) AS latency_ms
    FROM draws
  )
SELECT descriptiveStatistics(latency_ms)
FROM trial
GROUP BY arm
```

Click a series chip — or the matching row in **Table** — to make it the
baseline. The **Shift** view then draws Δ(p) = Q_arm(p) − Q_baseline(p) against
p, with W₁, the mean absolute quantile gap ∫|Δ| dp, in the legend.

With `A baseline` selected, the two Δ curves are the two things a difference of
distributions can be, and this is the view that tells them apart. The additive
arm sits flat at ≈ 0.5 from p ≈ 0.001 to p ≈ 0.98. The multiplicative arm rises
with the baseline quantile it multiplies — ≈ 0.13 at the first quartile, ≈ 0.25
at the median, ≈ 0.5 at the third, ≈ 2 at p = 0.984. Both arms move the *mean*
by a similar amount (1.65 → 2.15 and 1.65 → 2.07), so a comparison of means
would report one effect where there are two.

Past p ≈ 0.998 both curves come apart into noise, and that is the honest
reading rather than a defect: the grid runs to p = 1 − 1.5e-5, which at 100 000
rows per arm is where a quantile rests on one or two observations. The band
around each curve is what says so.

## A real corpus, and what small n looks like

`keelson('adr')` reads this repository's decisions in-process, so this one needs
no server at all: point the **Endpoint** menu at *Keelson introspection*, focus
the Distribution tab, and Run. `body_bytes` is each ADR's size on disk; grouping
by status compares the accepted corpus against what is still proposed.

```sql
WITH busy AS (
  SELECT status
  FROM keelson('adr')
  GROUP BY status
  HAVING count() >= 10
)
SELECT descriptiveStatistics('exact', body_bytes)
FROM keelson('adr')
WHERE status IN (SELECT status FROM busy)
GROUP BY status
```

The `busy` CTE is how a `HAVING` is spelled here, since the macro refuses one at
the top level. It also keeps the query honest as the corpus grows: the statuses
holding a handful of documents each — superseded, withdrawn, deferred — would
otherwise arrive as series the panel can say nothing about.

`'exact'` rather than the default `tdigest`, because at a few hundred rows the
exact Hyndman–Fan type-7 quantiles cost nothing, and the status line then drops
its *excludes sketch error* caveat: the band becomes the whole error budget.

Most of what the panel then shows is the smallness of the sample, which is the
point of running it here. The 95% DKW band is ±ε with ε = √(ln(2/α) / 2n), so
about ±0.12 in F over the ~125 accepted decisions and ±0.28 over the ~23
proposed ones. The largest vertical gap between the two curves is smaller than
those two half-widths added together, and a two-sample Kolmogorov–Smirnov test
on the same columns does not reject at 5% either (D ≈ 0.27, p ≈ 0.09 today).
The Boxen ladder stops at three letter values for the larger series and one for
the smaller — `letterval.RecommendedDepth` wants eight observations in a tail
before it draws the box that rests on them. Only the moments look confident,
and they are the misleading ones: the accepted set's mean sits several kilobytes
above its median, a gap made mostly by one outlying document.

## The shell's own apps and windows

Two more in-process tables, same no-setup path as the ADR board above: point the
**Endpoint** menu at *Keelson introspection* and Run. `keelson('apps')` is the
registry — every app this binary links, open or not — and `keelson('windows')` is
what is open right now, so it changes under you as windows come and go.

What can be opened with arguments, and what remembers where you left it
(ADR-0135, ADR-0148). `launch_kind` names the config an opener must send; empty
means the app accepts none, and an argument-carrying open of it is refused at the
host boundary. `workingset` says the shell pulls a record out of the window when
you close it and hands that back at the next plain open — it implies a launch
kind, because the record *is* an instance of that kind. `persisted_keys` is the
older untyped channel, still the right declaration for content too large to
travel inside a config.

`registration` is the one column here that no manifest declares — it is how the
app was registered. `factory` mints a fresh instance per window; `singleton`
hands the same one to every window, which is the legacy path. It bounds the two
columns before it: a config, and so a restored workingset, is delivered at the
Mount that runs once per instance, so a singleton with a window already open can
consume neither. `workingset` and `singleton` together would be a
misdeclaration, and the registry refuses that pair at registration — so the
combination cannot appear in these rows, and what the column tells you here is
whether two windows of an app are independent.

```sql
SELECT id, launch_kind, workingset, registration, persisted_keys, surface
FROM keelson('apps')
WHERE launch_kind != '' OR length(persisted_keys) > 0
ORDER BY workingset DESC, id
```

Where each open window's content came from. `launch_reason` is `plain` when
nobody supplied a config, `caller` when another app opened the window with
arguments — press **Save as applet…** and the creator window that appears is one
— and `restore` when the shell handed back the state that window's app was left
in. To see that third one: open a second Playground from the **Apps** menu, type
in it, close it, then open another; its row says `restore`, and this window's
still says whatever it opened as. `config_bytes` is the payload's size. The
payload itself stays in the window — it is the app's own DTO and may carry your
query text.

```sql
SELECT key, app_id, launch_reason, config_kind, config_bytes, title
FROM keelson('windows')
ORDER BY key
```

Which of the windows in front of you *can* leave anything behind — the join the
two tables exist to allow. It answers half the question honestly rather than the
whole: a window leaves a record only if its app participates **and** someone
acted in it, and the second half is the app's own to judge. The rarely-true
`shares_instance` column, absent here, is the exception worth knowing — a window
sharing its app instance with another open window neither takes a config nor
saves anything, because the state is not that window's alone.

```sql
SELECT w.key, w.app_id, w.launch_reason, a.workingset, a.launch_kind
FROM keelson('windows') AS w
LEFT JOIN keelson('apps') AS a ON a.id = w.app_id
ORDER BY a.workingset DESC, w.key
```

What is actually stored, as opposed to declared or open. `keelson('workingsets')`
is one row per stored record — the set a plain open would restore from, not the
trail of saves — so a name that has been saved four times and then deleted is
simply absent, and one saved four times shows the fourth. `config_bytes` is the
payload's size: the record is an instance of the app's own launch DTO, readable
only by that app's codec, so the table reports its size and its kind rather than
pretending to decode it. `reason` is why the window that wrote it closed,
`tile_key` which window that was, `run_id` which process — the last joins to
`keelson('build').run_id`.

```sql
SELECT app_id, name, kind, config_bytes, reason, tile_key, saved_at
FROM keelson('workingsets')
ORDER BY app_id, name
```

Which participating apps have nothing stored yet — the anti-join that says where a
plain open will start from scratch. An app can be missing here because nobody has
opened it, or because nobody acted in the window they did open: the shell writes a
record only on intent, so a window opened and closed untouched leaves the previous
one standing (or nothing at all).

```sql
SELECT a.id, a.launch_kind
FROM keelson('apps') AS a
LEFT JOIN keelson('workingsets') AS w ON w.app_id = a.id
WHERE a.workingset AND w.app_id = ''
ORDER BY a.id
```

Three caveats. `keelson('windows')` exists only where a window host does — the
desktop shell registers it, so a play started on its own has no windows to report
and naming the table is an error rather than an empty result.
`keelson('workingsets')` reads through whichever facts store the shell got at
start: with ClickHouse down that is the in-memory one, so the table then shows
this process's saves only and they go with the process. And the *history* stays
untabled on purpose — the saves are append-only rows in `boxer.facts`, queried
there directly, where a restored window's launch row also carries the caller
`runtime.workingset`.

## Live coverage of this process

These need two things: a binary built with `-cover -covermode=atomic`
(`rust/imzero2/hmi_coverage.sh` builds and launches one) and the **Endpoint**
menu at *Keelson introspection*. On a plain build the tables exist and are
empty. Coverage is cumulative since process start and monotone — drive a
feature, re-run, watch the numbers move. The canned lenses are the coverage
applets (Apps → Observability); these are the raw reads.

```sql
SELECT * FROM keelson('coverage_status')
```

The biggest gaps, package by package — sorted by untested statements, which is
the map applet's `size_by = 'uncovered'` reading as a table.

```sql
SELECT pkg_path, covered_stmts, total_stmts,
       total_stmts - covered_stmts AS uncovered_stmts,
       round(100 * covered_stmts / nullIf(total_stmts, 0), 1) AS pct
FROM keelson('coverage_pkgs')
ORDER BY uncovered_stmts DESC
LIMIT 25
```

Functions never entered in this session, in one corner of the tree — swap the
pattern for yours. An error path no session exercises and a feature simply not
driven yet look identical here, so the list is an upper bound on the gap, not
a defect count.

```sql
SELECT func, src_file, total_stmts, total_units
FROM keelson('coverage_funcs')
WHERE pkg_path ILIKE '%play%' AND covered_units = 0
ORDER BY total_stmts DESC
LIMIT 50
```

## ADS-B geo-raster (demo loader)

These target `planes_mercator`, the aircraft-position table loaded by
`apps/play/demo/adsb/demo.sh` (see its README) — point play's endpoint at that
local ClickHouse first. The **Map** tab renders the raster visually (ADR-0096);
the queries here run in the main editor.

Positions per 5,000-ft altitude band:

```sql
SELECT intDiv(altitude, 5000) * 5000 AS alt_band_ft, count() AS positions
FROM planes_mercator
GROUP BY alt_band_ft
ORDER BY alt_band_ft
```

Busiest aircraft types in the loaded slice:

```sql
SELECT t AS type, count() AS positions, uniqExact(icao) AS aircraft,
       round(avg(altitude)) AS avg_alt_ft
FROM planes_mercator
WHERE t != ''
GROUP BY type
ORDER BY positions DESC
LIMIT 20
```

The Map tab's raster query as a snippet (ADR-0096 §SD6): it bins the visible
points into a `W×H` grid and derives an RGBA value per pixel, so it returns one
row per pixel — a `W*H`-row framebuffer, not a readable table (the **Map** tab
draws it). Here the viewport is fixed to a Zürich box at 256×256; the Map tab
injects the live viewport instead.

```sql
WITH
  45.5 AS min_lat, 49.0 AS max_lat, 5.5 AS min_lon, 12.0 AS max_lon,
  256 AS W, 256 AS H, 100 AS sampling,
  toUInt32(greatest(0., least(4294967295., floor(4294967296. * (min_lon + 180) / 360 + 0.5)))) AS min_x,
  toUInt32(greatest(0., least(4294967295., floor(4294967296. * (max_lon + 180) / 360 + 0.5)))) AS max_x,
  toUInt32(greatest(0., least(4294967295., floor(4294967296. * (0.5 - asinh(tan(max_lat / 180 * pi())) / (2 * pi())) + 0.5)))) AS min_y,
  toUInt32(greatest(0., least(4294967295., floor(4294967296. * (0.5 - asinh(tan(min_lat / 180 * pi())) / (2 * pi())) + 0.5)))) AS max_y,
  toUInt64(max_x) - min_x AS span_x,
  toUInt64(max_y) - min_y AS span_y,
  mercator_x >= min_x AND mercator_x < max_x
    AND mercator_y >= min_y AND mercator_y < max_y AS in_view,
  least((toUInt64(mercator_x - min_x) * W) DIV span_x, W - 1) AS px,
  least((toUInt64(mercator_y - min_y) * H) DIV span_y, H - 1) AS py,
  py * W + px AS pos,
  (span_x / W) * (span_y / H) AS pixel_area,
  pow(2, 22) / sqrt(pixel_area) AS zoom_factor,
  count() AS total,
  greatest(1000000. / sampling / zoom_factor, toFloat64(count())) AS max_total,
  pow(total / max_total, 1/5) AS transparency,
  greatest(0, least(avg(altitude), 5000)) / 5000 AS color1,
  greatest(0, least(avg(altitude), 50000)) / 50000 AS color3,
  greatest(0, least(avg(ground_speed), 700)) / 700 AS color2,
  255 AS alpha,
  (1 + transparency) / 2 * (1 - color3) * 255 AS red,
  transparency * color1 * 255 AS green,
  color2 * 255 AS blue
SELECT round(red)::UInt8 AS r, round(green)::UInt8 AS g,
       round(blue)::UInt8 AS b, round(alpha)::UInt8 AS a
FROM planes_mercator
WHERE in_view
GROUP BY pos
ORDER BY pos WITH FILL FROM 0 TO toUInt64(W) * H
```

## Trivial states

```sql
SELECT * FROM anchor.facts LIMIT 1
```

```sql
SELECT 1 AS hello, now() AS ts
```
