---
type: reference
audience: end-user
status: draft
title: Biggest dependencies
summary: "Chart the modules taking the most room in the binary"
icon: "📊"
endpoint: introspection
tabs: [chart, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Biggest dependencies

The modules that take up the most room in this binary, as bars. One
question, one unit, one ranking — which is what makes it a chart rather
than a table you have to read.

**The knobs.** `top` is how many bars; `party` filters to `first`, `third`,
`stdlib`, or `all`. Third-party is the default because "which dependency is
big" is the question this answers; `first` turns it into a map of your own
module's weight, and `stdlib` shows what the runtime and standard library
cost before any dependency is counted.

Bars are ordered by size and the abscissa is categorical, so the row order
*is* the bar order — the chart panel takes distinct `x` values in
first-appearance order rather than sorting them, which is what lets a query
choose its own ranking.

Only one numeric column is projected, on purpose: the chart contract makes
every numeric column a lane, so a result carrying packages, bytes and
percentages side by side would draw four grouped bars on one axis at four
incompatible scales. The Table tab is where the other columns live.

```sql
SET param_top = '15';
SET param_party = 'third';

WITH s AS (
  SELECT module_path,
         sum(text_bytes) AS bytes
  FROM keelson('go_symbols')
  WHERE {party:String} = 'all' OR party = {party:String}
  GROUP BY module_path
)
SELECT
  -- The bar's label. A full module path is too long to read as an axis tick,
  -- and the last segment alone is wrong for versioned modules — `…/arrow-go/v18`
  -- is named arrow-go, not v18.
  if(match(arrayElement(splitByChar('/', module_path), -1), '^v[0-9]+$')
     AND length(splitByChar('/', module_path)) > 1,
     arrayElement(splitByChar('/', module_path), -2),
     arrayElement(splitByChar('/', module_path), -1)) AS x,
  bytes AS text_bytes
FROM s
WHERE bytes > 0
ORDER BY text_bytes DESC
LIMIT {top:UInt32}
```

## Reading it honestly

- **The bar is machine code after dead-code elimination**, not the
  dependency's size on disk or in source. A module can be enormous to
  download and small here.
- **The standard library is one party, not one module.** Under
  `party = 'stdlib'` every bar reads `std`, because no module owns the
  standard library; the map applet is where it breaks down by directory.
- **A missing dependency is not a bug.** Modules that contributed no
  symbols are filtered out — they are real dependencies you still build
  against and must still patch, and the modules applet lists them with
  zeroes.
- **`top` truncates silently by design.** The bars shown are the biggest
  ones; the total across everything is in the overview, not implied by
  summing what is on screen.
- **The bar label is the module's short name**, so two modules with the same
  last segment from different hosts would draw identical ticks. The Table tab
  carries the full path, which is the disambiguator.
- **A tall first bar can hide the tail.** Contributions span an order of
  magnitude or more, so the smallest bars in a `top = 15` are a few pixels;
  the `log y` chip is the way to read them, and the Table tab is the way to
  be sure.
