---
type: adr
status: accepted
date: 2026-05-01
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-01
---

# ADR-0006: Nanopass Environment and First-Class Pass

## Context

The nanopass package ([`public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass)) currently models a pass as a bare function, `type Pass = func(sql string) (string, error)`. Composition is `Pipeline(sql, p1, p2, ...)`. Convergence wrapping is the caller's responsibility via `FixedPoint(p, maxIter)`.

ADR-0002 fixed the *substrate* of nanopass — stateless passes on the CST plus a centralised `SelectScope`. This ADR is about the *pass interface* and the *non-body data* that surrounds a SELECT statement.

Three problems have surfaced as the pipeline has grown:

- **Implicit behavioural contracts.** Whether a pass is idempotent, or whether it requires fixed-point iteration to converge, lives in the author's head, in scattered comments, and in test scaffolding. Recent example: the canonicalize pass needed fixpoint iteration to converge (commit `6a397b6`), a property that was discovered by failing tests rather than declared at the pass.
- **Fixpoint as a call-site concern.** `FixedPoint(p, 10)` is sprinkled through tests and consumers. The pass itself does not declare that it needs convergence wrapping; every caller has to know.
- **Settings, params, and FORMAT are spread across the SQL and across passes.** `SET key = value;` lines, `SETTINGS k=v` clauses, `{name: Type}` parameter slots, and `FORMAT TabSeparated` are extra-syntactic decoration on a SELECT. Each pass that touches them re-implements its own extraction (e.g. `ParseExtractedQuery`, `ManipulateSetting`, ad-hoc string scanning in `InjectParamsAsCTE`). The recent `EvaluateFunctions` rework that allows literal arguments to be bound as ClickHouse params has no path to actually *consult* a param binding — the slot reaches the evaluator as opaque text, regardless of whether `SET param_x = ...` was given.

Observed pressures:

- **Authoring cost of new metadata.** Adding any new "fact about a pass" today (idempotency, convergence, what it touches, what it requires) means convincing every consumer to honour it. With Pass as a function alias, there is nowhere on the value to *declare* that fact.
- **Scheme-flavoured framing fits.** A SELECT statement is naturally a body together with an environment — settings, params, format — exactly the `(eval expr env)` shape from Chez Scheme. The current code treats the environment as comments embedded in the SQL string. Lifting it to a Go value is the same move Scheme made: turn the implicit context into a first-class object that passes can inspect, mutate, and forward.
- **Atomic refactoring is on the table.** All consumers of `nanopass.Pass` are inside this repository (verified: non-test callers only use `Parse` / `WalkCST` / `NodeText`, which are unaffected). A breaking redesign is reachable in a single PR; staging it would force us to carry both shapes and is not warranted.

This ADR proposes the design to address those three problems together. Doing them separately would land Environment on top of the function-typed Pass and then refactor again to lift Pass to a value — a wasted intermediate state.

## Design space (QOC)

**Question.** How should nanopass encode pass behaviour, sequencing, and SELECT-statement environment so that (a) behavioural properties such as idempotency and convergence are declared and machine-checked, (b) settings/params/format are first-class Go values rather than embedded SQL text, and (c) consumers compose passes through combinators rather than ad-hoc wrappers?

**Options.**

- **O1** — Status quo: `Pass = func(sql)→(sql,err)`; `FixedPoint` at call sites; params/settings re-extracted by every pass that needs them.
- **O2** — Thread Environment as an extra parameter (`Pass = func(*Environment, sql)→(sql,err)`); leave behaviour metadata implicit.
- **O3** — `Pass` becomes a struct value carrying an `Apply(env, body)` function plus a `PassProperties` metadata block; introduce `dsl/env/` package; combinators replace ad-hoc wrappers; atomic migration *(chosen)*.
- **O4** — `Pass` becomes an interface; Environment threaded; combinators as in O3.
- **O5** — Encode metadata at the type level (e.g. generic type parameters or marker types per property) rather than as runtime fields.

**Criteria.**

- **C1 — Behavioural declarability:** can a pass declare idempotency, convergence, and what env regions it reads/writes, in a way the runner and tests can consume?
- **C2 — Convergence is authored once:** does `NeedsFixedPoint` live on the pass rather than at every call site?
- **C3 — Environment composability:** are settings, params, and FORMAT first-class Go values that the entire pipeline shares without re-parsing?
- **C4 — Combinator-driven composition:** do `Sequence`, `FixedPoint`, `Validate`, `Conditional`, etc. take and return `Pass`, so the composition stays first-class?
- **C5 — Authoring ergonomics:** how heavy is "define a new pass" in lines of code and ceremony?
- **C6 — Migration cost:** how disruptive is the move from current state, given that the migration is in scope?
- **C7 — Future extensibility:** can new metadata (cost, determinism, dialect-targeting, …) be added without re-changing every existing pass?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ | +  |
| C2 | −− | −  | ++ | ++ | +  |
| C3 | −− | ++ | ++ | ++ | +  |
| C4 | +  | +  | ++ | +  | −  |
| C5 | ++ | ++ | +  | −  | −− |
| C6 | ++ | +  | −− | −− | −− |
| C7 | −  | −  | ++ | +  | −− |

O3 dominates O4 on C5 (struct + function field is lighter than an interface) and on C7 (additive struct fields don't break implementations the way interface methods do). O3 dominates O5 across the board except on C1, where the win does not justify the C5/C6/C7 cost. O3 dominates O2 on the metadata axis, and O2 dominates O1 — confirming that the Environment move alone is necessary but insufficient.

## Decision

We adopt O3: **Pass as a first-class struct value with declared properties, alongside a new `dsl/env/` package for the SELECT-statement environment**, migrated atomically.

The decision has three parts.

### 1. `Environment` lives at `public/db/clickhouse/dsl/env/`

```go
package env

type Environment struct {
    SessionSettings   map[string]Setting   // from `SET key = value;` (key not prefixed with `param_`)
    StatementSettings map[string]Setting   // from inline `... SETTINGS k=v`
    Params            map[string]Param     // unified view; populated from SET param_* and from {x: T} slots
    Format            string               // from `FORMAT TabSeparated`; "" if absent
}

type Setting struct {
    Name  string
    Raw   string  // verbatim SQL text of the value
    Value any     // deserialised when known; nil if not yet decoded
}

type Param struct {
    Name  string
    Type  string  // ClickHouse type from a `{name: Type}` slot occurrence; "" if no slot was seen
    Raw   string  // verbatim SQL text from `SET param_name = <here>`; "" if no SET was seen
    Value any     // deserialised per Type when both Raw and Type are known; nil otherwise
}

func Extract(sql string) (env *Environment, body string, err error)
func (e *Environment) Integrate(body string) (sql string, err error)
```

Lookup semantics for `Params`:

- Both `Raw` and `Type` populated → param is **resolved**; `Value` is the deserialised Go value.
- Only `Type` populated (slot exists, no SET) → param is **unresolved**; `Value` is `nil`.
- Only `Raw` populated (SET exists, no slot) → param is **resolved-without-slot-type**; `Value` is `nil` until a consumer requests deserialisation against an explicit type.
- Neither → not in env.

Round-tripping `Integrate(Extract(sql))` is **normalising**, not byte-identical. Whitespace, ordering of SET lines, and the SETTINGS clause position will be canonicalised. Callers depending on byte-identity must not rely on Extract/Integrate.

`Environment` is **flat** in v1. Hierarchical / parent-chained environments (Chez `eval-environment` style) are out of scope; revisit if a session-vs-statement use case beyond settings emerges.

### 2. `Pass` is a struct value with declared properties

```go
package nanopass

type Pass struct {
    Name       string         // human-readable; used in error messages and AssertProperties output
    Apply      ApplyFunc      // the actual transformation
    Properties PassProperties // declared behaviour
}

type ApplyFunc func(e *env.Environment, body string) (newBody string, err error)

type PassProperties struct {
    // Behavioural — machine-checkable via AssertProperties.
    Idempotent      bool        // f(f(x)) == f(x) for every x in the corpus
    NeedsFixedPoint bool        // pass should be wrapped in FixedPoint by default; mutually exclusive with Idempotent

    // Env interaction — what the pass touches.
    Reads, Writes EnvRegions

    // Pre/postconditions — string tags, validated at registration time only in v1.
    Requires []FormTag
    Produces []FormTag
}

type EnvRegions uint8
const (
    RegionBody EnvRegions = 1 << iota
    RegionSessionSettings
    RegionStatementSettings
    RegionParams
    RegionFormat
)

type FormTag string  // e.g. "canonical-keyword-case", "columns-expanded", "params-extracted"
```

`PassProperties` is a struct, not an interface. Adding a new metadata field is additive and does not break existing pass declarations.

`Idempotent: true` and `NeedsFixedPoint: true` are mutually exclusive — declaring both is a registration-time error caught by `AssertProperties`. The runner auto-wraps `NeedsFixedPoint: true` passes in `FixedPoint(p, DefaultFixedPointMaxIter)`; call sites no longer encode that knowledge.

```go
const DefaultFixedPointMaxIter = 128
```

128 is the default convergence cap. Passes that need a different cap pass an explicit value to the `FixedPoint` combinator at the declaration site; `NeedsFixedPoint: true` plus auto-wrapping is the common case.

`Reads`/`Writes` are documentation today and become enforcement points later if pass scheduling needs them (e.g. running independent passes in parallel based on disjoint write sets). v1: documentation only.

`Requires`/`Produces` are tag strings for now. Form-tag validators (functions that decide whether `body` satisfies a tag) are explicitly **out of scope for v1** — tags are checked indirectly by `AssertProperties` on a corpus, not at every Apply call.

### 3. Combinators replace ad-hoc wrappers

Everything that takes a `Pass` returns a `Pass`. Composition stays first-class.

```go
func Sequence(name string, ps ...Pass) Pass
func FixedPoint(p Pass, maxIter int) Pass
func Validating(g Grammar, p Pass) Pass            // wraps p; validates the body produced by p.Apply
func Conditional(name string, pred func(*env.Environment) bool, p Pass) Pass
func LiftBodyPass(name string, fn func(string)(string,error), props PassProperties) Pass
```

`Validating(g, p)` runs `p.Apply` first, then validates the resulting body against grammar `g`. The motivating use case is detecting passes that produce ill-formed output; pre-validation is left to the caller (insert `nanopass.ValidateGrammar1` as a separate step in a `Sequence` if pre-checks are needed).

`LiftBodyPass` is the bridge for any helper function that genuinely doesn't need the environment (e.g. pure CST rewriters). It wraps a body-only function into a full `Pass`, ignoring the env on read and leaving it untouched on write.

`Pipeline(sql, p1, p2, ...)` and `FixedPointPipeline(...)` are removed. The replacement is:

```go
result, err := Sequence("normalize", p1, p2, p3).Run(sql)

// Pass.Run does the env round-trip:
//   env, body, err := env.Extract(sql)
//   body, err     = p.Apply(env, body)
//   return env.Integrate(body)
```

`Validate` (Grammar1) and `ValidateCanonical` (Grammar2) become `Pass` values: `nanopass.ValidateGrammar1`, `nanopass.ValidateGrammar2`. Use them with `Sequence` or `Validating`.

### Test infrastructure

`nanopass.AssertProperties(t *testing.T, p Pass, corpus []string)` is the load-bearing piece that turns annotations from comments into contracts.

For each corpus entry it verifies:

- `Idempotent: true` ⇒ `p.Apply(e, p.Apply(e, body)) == p.Apply(e, body)` byte-for-byte.
- `NeedsFixedPoint: true` ⇒ on at least one corpus entry, the second application of `p` differs from the first (otherwise the flag is unjustified or the corpus is too narrow).
- `Idempotent` and `NeedsFixedPoint` are not both set.
- `Produces` ⊇ tags that subsequent `Requires`-bearing passes in a `Sequence` consume — checked per `Sequence` rather than per pass, as a registration-time lint over the combinator tree.

Every existing pass test file gets an `AssertProperties` invocation. The corpus is the shared corpus from `public/db/clickhouse/dsl/nanopass/testdata`.

## Alternatives

The QOC matrix captures comparative rankings. Notes here record nuance.

- **O1 — Status quo.** Adding any new pass-level concept (e.g. dialect-targeting, cost) requires the same move we are making now; deferring it pays the migration cost later for less clear motivation and no Environment.
- **O2 — Thread Environment, keep function Pass.** Solves C3 alone. Idempotency and fixpoint stay implicit, which is the bug the canonicalize-fixpoint commit exposed. The marginal change against the migration cost is too small to be worth a separate landing.
- **O4 — Pass as interface.** Ergonomics regress: each pass either becomes a named type with a method, or relies on a small wrapper to lift a function value into the interface. Adding metadata fields means changing the interface, which breaks every implementation. Struct + function field has neither problem.
- **O5 — Type-level metadata.** Generics and marker types could express `IdempotentPass[T]` etc., but `Sequence`-ing heterogeneous passes would require type erasure anyway, defeating the static guarantee. Authoring cost rises sharply for no concrete win over runtime fields.

## Consequences

### Positive

- Idempotency, fixpoint requirement, and env reach are *declared on the pass*, not folded into call sites or test names. Misclassifications are caught by `AssertProperties` running over the corpus.
- `FixedPoint` becomes an authoring concept, not a caller concept. The pipeline runner wraps `NeedsFixedPoint: true` passes automatically; consumers stop having to know which passes converge.
- Settings, params, and FORMAT are Go values throughout the pipeline. `EvaluateFunctions` can resolve `{name: Type}` slots against `env.Params` without the pass duplicating SET-line parsing.
- Combinators are first-class. `Sequence(name, ...)` is itself a `Pass` and composes recursively; named subpipelines become a real organisational tool rather than a comment.
- New metadata fields are additive: introducing `Cost`, `Deterministic`, `Dialect`, etc. does not require touching the existing pass declarations beyond filling in the new field if relevant.

### Negative

- **Atomic migration is large.** ~25 pass files plus ~25 test files, plus the pipeline core and the new `env` package. The PR will be reviewable but not small.
- **Public-API break inside the repository.** Every test invocation that currently calls `pass(sql)` changes. The change is mechanical (most uses become `p.Run(sql)` or `p.Apply(env, body)`) but it touches a lot of files.
- **Environment introduces a normalising round-trip.** Any caller that depended on whitespace-faithful round-trip through the pipeline will see canonicalisation. We accept this — pipeline output has never been byte-faithful in practice — but it warrants explicit documentation in the package README.
- **Two surfaces to keep coherent: `dsl/env` and `dsl/nanopass`.** Adding a new env field requires changes in both packages (the data model and any pass that consumes it). Mitigated by `Reads`/`Writes` annotations making consumer impact greppable.

### Neutral

- ADR-0002's invariants stand: passes remain stateless, the CST + SelectScope is the structural representation, and fixed-point iteration is the answer for non-convergent transformations. This ADR refines *how* fixed-point iteration is wired (declared on the pass instead of imposed by the caller) and *what surrounds the body* (an explicit Environment), without contradicting ADR-0002's substrate decisions.
- ADR-0002's "four test categories per pass" derived practice is preserved; `AssertProperties` is the mechanisation of category (2).

### Migration plan

Single PR, internally staged for review readability:

1. **Create `public/db/clickhouse/dsl/env/`** with `Environment`, `Param`, `Setting`, `Extract`, `Integrate`. Cover with unit tests against a small corpus of SET/SETTINGS/FORMAT/param-slot combinations.
2. **Rewrite `public/db/clickhouse/dsl/nanopass/nanopass_pipeline.go`** to introduce the `Pass` struct, `PassProperties`, `EnvRegions`, `FormTag`, the combinators (`Sequence`, `FixedPoint`, `Validating`, `Conditional`, `LiftBodyPass`), and the `Pass.Run` entry point. Remove `Pipeline`, `FixedPointPipeline`, the function alias.
3. **Port pass files** from `public/db/clickhouse/dsl/nanopass/passes/`. Each existing pass becomes `var FooPass = nanopass.Pass{Name: "Foo", Apply: ..., Properties: ...}`. Pure CST passes use `LiftBodyPass`; env-aware passes (`EvaluateFunctions`, `InjectParamsAsCTE`, `ManipulateSetting`, `SetFormat`, `ExtractLiterals`) read/write the environment directly.
4. **Port test files** from `public/db/clickhouse/dsl/nanopass/passes_test/` and `public/db/clickhouse/dsl/nanopass_test/`. Add `AssertProperties` for each pass.
5. **Wire `EvaluateFunctions` to consult `env.Params`.** Add `marshalling.UnresolvedParamSlot` and `marshalling.ResolvedParamSlot` types per the prior conversation; `evalColumnExpr` handles `ColumnExprParamSlotContext` by lookup.
6. **Update consumers** in `public/db/clickhouse/dsl/nanopass/nanopass_macro.go` and any other Pass user inside the repo. (Verified: no consumers outside `dsl/nanopass` use the `Pass` shape today.)

Out of scope of this ADR (deferred):

- Parse-once optimisation across a `Sequence` (CST cache). Hooked at the runner layer when profiling justifies it.
- Form-tag *validators* (runtime checks that body conforms to a tag). v1 tags are documentation + corpus-asserted.
- Hierarchical environments. Single-level only in v1.
- Pass-level scheduling based on `Reads`/`Writes` (e.g. parallel independent passes). Annotation only in v1.

## Status

Accepted — 2026-05-01 (reviewed by p@stergiotis). The Pass / Environment design described here is implemented in [`public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass) and [`public/db/clickhouse/dsl/env`](../../public/db/clickhouse/dsl/env): the `Pass` struct and `PassProperties`, the combinators, `Pass.Run`, `AssertProperties`, and the `env` package. The items under "Out of scope of this ADR" remain deferred.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0002: Nanopass Pipeline Discipline](0002-nanopass-discipline.md) — the substrate this ADR refines (stateless passes on CST + scopes).
- [`public/db/clickhouse/dsl/nanopass/README.md`](../../public/db/clickhouse/dsl/nanopass/README.md) — package overview.
- [`public/db/clickhouse/dsl/nanopass/nanopass_pipeline.go`](../../public/db/clickhouse/dsl/nanopass/nanopass_pipeline.go) — current `Pass` definition and combinators.
- [`public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_evaluate_functions.go`](../../public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_evaluate_functions.go) — concrete motivation: a pass that needs `Environment` to resolve `{name: Type}` slots.
- Chez Scheme environments: <https://www.scheme.com/csug8/system.html#./system:s55> — `(eval expr env)` framing that motivates lifting the SELECT context to a first-class object.
