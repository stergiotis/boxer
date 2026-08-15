---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to declare standing gloss rules for a deployment

A [gloss](../adr/0186-play-gloss-catalog.md) is a named way of showing a
value in the `play` Table and Detail panes — a byte count as `1.3 MiB`, a
nanosecond span as `12.3 ms`, a card number masked with its Luhn verdict.
A query binds one to a column with an alias (`` AS `size@gloss/bytes` ``) or
a `-- play: gloss` line in its buffer. Rules that should hold across every
query of a deployment are **Go code, checked in**: a `gloss.RuleSet`,
registered on a `gloss.Repository`, handed to `play` when the app is built.
Nothing is read from files, the environment, or persisted UI state.

## Declare a set

A set is an ordered list of named rules; each rule is a condition on the
column and the gloss to show. The chain reads as the rule does:

```go
import (
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

var sensorRules = gloss.Rules("acme-sensors").
	Rule("kelvin readings").
		When(gloss.Section("sensor"), gloss.NameMatches(`^temp`)).
		Show(gloss.MediaTypeTemperature, gloss.Unit("K")).
	Rule("payload sizes").
		When(gloss.NameMatches(`(^|_)bytes$`)).
		Show(gloss.MediaTypeBytes).
	Rule("secrets").
		When(gloss.Sem(valueaspects.AspectSecret)).
		Show(gloss.MediaTypeMasked)
```

`When` takes predicates over the column's spec — what an author would type
to mint it (`name:temperature section:sensor role:val ct:f64 sem:… arrow:…`):

| predicate | holds when |
| --- | --- |
| `Name(x)`, `NameMatches(re)` | the column name is `x` / matches `re` (RE2, unanchored) |
| `Section(x)`, `Role(x)`, `Item(x)`, `CT(x)` | the leeway section, role, backbone item type, canonical type is `x` — any string-kinded value, so `Role(common.ColumnRoleValue)` compiles |
| `Enc(a)`, `Sem(a)`, `Use(a)` | the column carries that aspect, by the vocabulary's enum — a misspelt aspect does not compile |
| `Arrow(prefix)` | the Arrow type starts with `prefix` (`"list<"`, `"float"`) |
| `SpecMatches(re)` | the whole spec line matches `re` — the `-- play: gloss` directive's own matcher |
| `All(…)`, `Any(…)`, `Not(…)` | combinations; `When(a, b)` is `All(a, b)` |

`Show` names a media type of the catalog and its parameters (`gloss.Unit`,
or `gloss.P("name", "value")`). Every predicate prints as it reads —
`section=sensor ∧ name~^temp` — which is what the Glosses tab and a header
hover show for a column the rule bound.

## Register it, and hand the repository to play

A `Repository` is rule sets over one catalog. Registration validates the
whole set — unknown media type, undeclared or missing parameter, a pattern
that does not compile, a rule without a condition, a duplicate or empty
name — and refuses it whole, so do it where a failure is loud at startup:

```go
var repo = func() *gloss.Repository {
	r := gloss.NewRepository(nil) // nil: the default catalog
	r.MustRegister(sensorRules)
	return r
}()
```

Then give it to `play` at construction — it is a constructor argument, not
something `play` looks up:

- an embedder builds the app with it:
  `play.NewLivePlayApp(client, initSQL, 100, repo)`;
- a sqlapplet host passes it as `EmbedConfig.Rules`
  ([`sqlapplet_embed.go`](../../apps/sqlapplet/sqlapplet_embed.go));
- a deployment that only links `play` and opens it through the registered
  launcher registers on `play.DefaultRepository()` before the first window
  mounts — an `init` in a package linked into the binary does:

  ```go
  func init() { play.DefaultRepository().MustRegister(sensorRules) }
  ```

A repository built over a wider catalog (`gloss.NewRepository(cat)` after
`cat.MustRegister(myGloss)`) is also how a host adds glosses of its own.

## What wins

Per column: an explicit alias › the buffer's `-- play: gloss` lines, top to
bottom › the repository's sets, in registration order and declaration order
› the affinities each gloss brings along (`gloss/masked` for `sem:secret`,
`gloss/url` for `sem:url`, `application/json` for `sem:json*`). A set is a
deployment default, not a mandate: a query that wants a column plain writes
`-- play: gloss gloss/raw name:that_column`. The Glosses tab lists every
set under its name and, per column, the rules its binding shadowed.

The first checked-in set is sqlapplet's own
([`sqlapplet_gloss_rules.go`](../../apps/sqlapplet/sqlapplet_gloss_rules.go)):
byte counts and durations by the suffixes the shipped books use.

## See also

- [ADR-0186](../adr/0186-play-gloss-catalog.md) — the catalog, the rule
  route, and the Update that made standing rules code.
- [play-pluggable-detail.md](./play-pluggable-detail.md),
  [play-pluggable-docs.md](./play-pluggable-docs.md) — the other embedder
  seams.
