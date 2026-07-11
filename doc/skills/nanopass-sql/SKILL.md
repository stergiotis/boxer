---
name: clickhouse-nanopass
description: "Use this skill when writing ClickHouse SQL transformation passes, pipelines, macro expanders, function evaluators, or analysis functions using the nanopass framework. Triggers include: any mention of 'nanopass', 'SQL pass', 'SQL transformation', 'SQL rewrite', 'ClickHouse pass', 'canonicalize SQL', 'macro expansion', 'qualify tables', 'expand columns', 'extract literals', 'add/modify SETTINGS', or requests to manipulate ClickHouse SQL programmatically in Go. Also use when parsing ClickHouse SQL, walking a CST, rewriting tokens, building scope-aware transformations, declaring PassProperties, or composing SQL→SQL pipelines with Sequence/FixedPoint. Do NOT use for general SQL querying, ClickHouse client usage, or ORM-based database access."
---

# ClickHouse SQL Nanopass Framework

A Go library for composable SQL→SQL transformations of ClickHouse SELECT
statements. Each transformation is a self-contained **pass**: a `nanopass.Pass`
struct value carrying an `Apply` function plus declared `PassProperties`. Passes
compose through combinators that take and return `Pass`; the runner threads a
shared `env.Environment` through the chain so settings, params, and FORMAT are
first-class Go values.

Authoritative design records — read these before changing the substrate:

- **[ADR-0002](../../adr/0002-nanopass-discipline.md)** — the substrate:
  stateless passes on the CST + a centralised `SelectScope`. No AST.
- **[ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md)** —
  the `Pass` struct + `PassProperties` and the `env` package.
- **[ADR-0084](../../adr/0084-nanopass-antlr-dfa-cache-bounding.md)** — the
  bounded process-local DFA cache that makes long-running parsing memory-safe.

The package README at
[`public/db/clickhouse/dsl/nanopass/README.md`](../../../public/db/clickhouse/dsl/nanopass/README.md)
is kept current and is the next stop after this skill.

## Critical Rules — Read Before Writing Any Code

1. **A pass is a `nanopass.Pass` struct, not a function.** `Pass{Name, Apply,
   Properties}` where `Apply` is `func(e *env.Environment, body string) (string,
   error)`. There is no `func(sql) (sql, error)` pass type and no `Pipeline()` —
   compose with `Sequence`, run with `.Run(sql)`. (Pre-ADR-0006 `Pipeline`,
   `FixedPointPipeline`, and the function alias were removed.)
2. **Every pass re-parses from scratch.** `Apply` receives a `body` string and
   returns a `body` string. Never share a `*ParseResult` or
   `*antlr.TokenStreamRewriter` across passes. This is what keeps passes
   composable and reorderable (ADR-0002).
3. **Two grammars.** `nanopass.Parse` uses **Grammar1** (the full ClickHouse
   SELECT surface). `nanopass.ParseCanonical` uses **Grammar2** (canonical forms
   only) and doubles as a *completeness proof* that normalisation finished.
   Normalisation passes parse with `Parse`.
4. **`Apply` sees `body`, not the whole statement.** `Pass.Run` calls
   `env.Extract` first, so the leading `SET …;` prelude is already split off into
   the `Environment`. Parse and rewrite `body`; read/write settings and params
   through `e`.
5. **Declare behaviour in `Properties`.** `Idempotent` and `NeedsFixedPoint` are
   mutually exclusive; the runner auto-wraps `NeedsFixedPoint` passes in
   `FixedPoint(p, 128)`. `AssertProperties` enforces both against a corpus — a
   wrong flag fails the test.
6. **Every pass must return syntactically valid SQL.** If you can't guarantee
   it, add `nanopass.ValidateGrammar1` (or `Validating(GrammarG1, p)`) after your
   pass.
7. **Use `BuildScopes` for anything touching tables, WHERE, or UNION ALL.** Never
   `FindAll` a `TableIdentifierContext` directly — `TableIdentifier` appears both
   in FROM clauses and inside `ColumnIdentifier` (column qualifiers like
   `t1.id`). Scopes disambiguate.
8. **Whitespace and comments are on the hidden channel** (`-> channel(HIDDEN)`).
   The `TokenStreamRewriter` preserves them automatically, giving lossless
   round-trips.
9. **`Parse`/`ParseCanonical` are total.** `CheckInputGuards` runs first and
   rejects pathological input (deep nesting, oversized payloads, invalid UTF-8).
   You never get a panic from parsing; you get an error.
10. **Fix canonicalisation at the canonicalize pass, not downstream.** Downstream
    consumers (the AST converter via `ParseCanonical`) only ever see canonical,
    function-call forms. If a shape reaches them un-normalised, fix the relevant
    `Canonicalize*` pass so it emits the canonical shape — do not special-case
    the non-canonical shape in the consumer.

## Architecture

```
SQL string
  → env.Extract            split leading `SET …;` prelude → (Environment, body)
  → Pass.Apply(env, body)  parse body → walk CST → rewrite tokens → emit body
  → env.Integrate(body)    re-emit SET prelude + body
  → next pass repeats (shares the same Environment inside a Sequence)
```

`Pass.Run(sql)` does the full round-trip. `Sequence` shares the `env` across its
children, so they observe each other's mutations to settings and params.
Re-parsing per `Apply` trades CPU for composability (negligible for typical
query sizes; `Parse` is the dominant per-pass cost).

## Module Layout

```
github.com/stergiotis/boxer/public/db/clickhouse/dsl/
├── env/                          # the SELECT-statement environment (ADR-0006)
│   ├── env_environment.go        # Environment, Setting, Param, NewEnvironment
│   ├── env_extract.go            # Extract(sql) → (*Environment, body, err)
│   └── env_integrate.go          # (*Environment).Integrate(body) → (sql, err)
├── grammar1/                     # generated Grammar1 lexer+parser (full surface)
├── grammar2/                     # generated Grammar2 lexer+parser (canonical only)
└── nanopass/
    ├── nanopass_pipeline.go      # Pass, PassProperties, combinators, Run, validators, discard marker
    ├── nanopass_parse.go         # Parse (Grammar1), ParseCanonical (Grammar2), ParseResult
    ├── nanopass_guard.go         # CheckInputGuards, MaxInputBytes, MaxNestingDepth
    ├── nanopass_dfacache.go      # bounded DFA cache (ADR-0084), DFACacheStats
    ├── nanopass_walk.go          # WalkCST, FindAll, FindFirst
    ├── nanopass_rewrite.go       # RewriterI, node helpers, TrackedRewriter
    ├── nanopass_scope.go         # BuildScopes, FlattenScopes, SelectScope, TableSource, CTEDef
    ├── nanopass_macro.go         # MacroExpander, LiteralArg, LiteralTypeE, ExtractLiteralArgs
    ├── nanopass_identifier.go    # DecodeIdentifier, QuoteIdentifier, NormalizeCallName
    ├── nanopass_observation.go   # SourceRange, Observation, ObservationFuncI
    ├── nanopass_assert_properties.go  # AssertProperties(t, p, corpus)
    ├── analysis/                 # ExtractTables / ExtractColumns / ExtractFunctions
    ├── highlight/                # Highlight → []Span; RenderANSI / RenderHTML / HighlightCSS
    ├── passes/                   # the shipped passes (see "Included Passes")
    └── testdata/
        ├── nanopass_testdata_corpus.go   # LoadCorpus() []CorpusEntry via embed.FS
        └── corpus/               # 82 .sql files (+ corpus/unsupported/ with 4)
```

## Dependencies and Imports

```go
import (
    "github.com/antlr4-go/antlr/v4"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"      // Grammar1 CST types
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"      // Grammar2 CST types (rarely needed)
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"           // Environment
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"   // TypedLiteral, VerbatimSql, ControlFlow
    "github.com/stergiotis/boxer/public/observability/eh"                // eh.Errorf
    "github.com/stergiotis/boxer/public/observability/eh/eb"             // eb.Build() structured errors
    "github.com/rs/zerolog"                                              // TrackedRewriter logger
)
```

Build tags are mandatory for every `go build/test/vet` in this repo — pass
`-tags="$(cat ./tags)"` (and export `GOFLAGS=-tags=…` so gopls/`go doc` resolve
symbols). Without them, packages fail to compile with misleading "undefined"
errors.

## Coding Standards (Mandatory)

House style — full rules in
[CODINGSTANDARDS.md](../../../CODINGSTANDARDS.md). The load-bearing ones here:

- **Named return values** on functions/methods; **naked returns** after setting
  `err`.
- **No `if err := f(); err != nil`** — assign, then check.
- **Receiver name is always `inst`.** Interfaces end in `I`, enum types end in `E`.
- **Wrap errors with `eh.Errorf("…: %w", err)`**; build structured errors with
  `eb.Build().Str("k", v).Errorf("…")`.
- **Pre-allocate** slices/maps when the size is known.
- Use **explicit anonymous blocks** to scope locals within large functions.

## The Pass Model

```go
type Pass struct {
    Name       string
    Apply      ApplyFunc       // func(e *env.Environment, body string) (newBody string, err error)
    Properties PassProperties
}

type PassProperties struct {
    Idempotent      bool        // f(f(x)) == f(x) over the corpus
    NeedsFixedPoint bool        // runner auto-wraps Apply in FixedPoint(p, DefaultFixedPointMaxIter=128)
    Reads, Writes   EnvRegions  // bitset: RegionBody | RegionSessionSettings | RegionStatementSettings | RegionParams | RegionFormat
    Requires        []FormTag   // ordering hints (documentation in v1)
    Produces        []FormTag
}
```

- `Idempotent` and `NeedsFixedPoint` are **mutually exclusive** — declaring both
  is a contract violation caught by `AssertProperties`.
- `Reads`/`Writes` are documentation in v1 (a future scheduler may parallelise
  passes with disjoint write sets). Still, declare them honestly — they are the
  greppable record of which passes touch which env region.
- `Pass.Run(sql)` is the external entry point. A pass invoked *from another
  pass's `Apply`* calls the inner pass's `Apply` directly (sharing the env), not
  `Run`.

### Combinators

Everything that takes a `Pass` returns a `Pass`; composition stays first-class.

```go
func Sequence(name string, ps ...Pass) Pass                          // left-to-right, shared env
func FixedPoint(p Pass, maxIter int) Pass                            // iterate to convergence; sets Idempotent, clears NeedsFixedPoint
func Validating(g Grammar, p Pass) Pass                              // run p, then parse-validate its body against g
func Conditional(name string, pred func(*env.Environment) bool, p Pass) Pass  // run p only when pred(env)
func LiftBodyPass(name string, fn func(string)(string,error), props PassProperties) Pass  // wrap a body-only func

// Grammar selectors for Validating:
const ( GrammarG1 Grammar = iota; GrammarG2 )

// Validation passes (Pass values, usable directly in a Sequence):
var ValidateGrammar1 Pass  // body must parse with Grammar1
var ValidateGrammar2 Pass  // body must parse with Grammar2 (canonical-only) — final-step completeness check
```

`Validating` and `Conditional` delegate to the wrapped pass's own fixpoint
execution and therefore *clear* `NeedsFixedPoint` on the wrapper (no double
loop). `FixedPoint` preserves the wrapped properties, clears `NeedsFixedPoint`,
and declares `Idempotent`.

## The Environment (`dsl/env`)

```go
type Environment struct {
    SessionSettings   map[string]Setting   // leading `SET k = v;` where k does NOT start with "param_"
    StatementSettings map[string]Setting   // inline `... SETTINGS k=v`  (read-only view in v1)
    Params            map[string]Param     // unified: `SET param_x = …;` AND `{x: Type}` slots
    Format            string               // `FORMAT Name`              (read-only view in v1)
}

type Setting struct { Name, Raw string; Value any }   // Raw = verbatim SQL; Value = deserialised when known
type Param   struct { Name, Type, Raw string; Value any }

func (p Param) IsResolved() bool    // both Type (slot) and Raw (SET) present → Value holds the deserialised value
func (p Param) IsUnresolved() bool  // slot referenced from body, no SET bound

func Extract(sql string) (e *Environment, body string, err error)  // best-effort body parse; always returns a usable env
func NewEnvironment() *Environment                                  // all maps allocated
func (e *Environment) Integrate(body string) (sql string, err error)
const ParamPrefix = "param_"
```

v1 semantics to internalise:

- `Extract` owns the **`SET` prelude** and harvests `{name: Type}` slots from the
  body to populate `Param.Type`. A `SET` key wins a `Params` slot if it starts
  with `param_` *or* matches a body slot name; everything else is a
  `SessionSettings` entry.
- `StatementSettings` and `Format` are **read-only views** — they stay in `body`.
  Passes that change them rewrite the body's CST; `Integrate` re-emits only the
  `SET` prelude.
- `Integrate(Extract(sql))` is **normalising** (SET ordering, whitespace), not
  byte-identical.

## How to Write a New Pass

### Local-rewrite passes: the combinator core (preferred for `Canonicalize*`)

Most `Canonicalize*` passes are **local term rewrites** — "match a CST node shape,
replace it with text built from the original spans of the children you keep." For
those, do **not** hand-roll the parse/walk/emit skeleton; use the unexported
combinator core in `passes/nanopass_passes_rewrite_core.go` (ADR-0098). A pass
body becomes just its rule(s):

```go
var CanonicalizeTernary = nanopass.LiftBodyPass(
    "CanonicalizeTernary",
    func(sql string) (string, error) { return rewriteNodes(sql, "CanonicalizeTernary", ternaryRule) },
    nanopass.PassProperties{NeedsFixedPoint: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
)

// cond ? then : else → if(cond, then, else)
func ternaryRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
    c, ok := node.(*grammar1.ColumnExprTernaryOpContext)
    if !ok {
        return "", false
    }
    ops := columnExprOperands(pr, c) // spans of the columnExpr children
    if len(ops) != 3 {
        return "", false
    }
    return callForm("if", ops[0], ops[1], ops[2]), true // "if(<cond>, <then>, <else>)"
}
```

The core:

- `rewriteNodes(sql, name, rules...)` — top-down walk; the first matching
  `nodeRule` (`func(pr, node) (replacement string, ok bool)`) replaces the node
  and the walk skips its subtree, so edits never overlap. Declare the pass
  `NeedsFixedPoint` and the runner re-applies until nested matches converge.
- `rewriteTokens(sql, name, rule)` — the same for per-token rewrites
  (`tokenRule = func(tok) (replacement string, ok bool)`), e.g. `==` → `=`.
- Helpers: `spanOf(pr, node)`, `columnExprOperands(pr, node)`,
  `terminalText(node, tokenType)`, `callForm(fn, args...)`.

**The bright line (ADR-0098):** a rule returns replacement **text** assembled from
original child spans — never a tree. Mutation stays in the `TokenStreamRewriter`,
so hidden-channel trivia is preserved automatically.

**When NOT to use it** (write a plain `LiftBodyPass` instead): the pass needs
cross-token state (whitespace-run or quote-fusion handling), structural token
reordering (JOIN normalisation), a node-conditional token *rename* rather than a
whole-node replacement (`CanonicalizeMultiIf`), or a precedence engine
(`RemoveRedundantParens`). Forcing those onto the combinator adds code.

### Template: pure body pass (no env) via `LiftBodyPass`

For token-level or CST-node rewrites that never touch settings/params/format —
this is the most common shape. Declare it as a package-level `var`.

```go
package passes

import (
    "github.com/antlr4-go/antlr/v4"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
    "github.com/stergiotis/boxer/public/observability/eh"
)

var MyNodePass = nanopass.LiftBodyPass("MyNodePass", myNodePassImpl, nanopass.PassProperties{
    Idempotent: true,
    Reads:      nanopass.RegionBody,
    Writes:     nanopass.RegionBody,
})

func myNodePassImpl(sql string) (result string, err error) {
    pr, err := nanopass.Parse(sql)
    if err != nil {
        err = eh.Errorf("MyNodePass: %w", err)
        return
    }
    rw := nanopass.NewRewriter(pr)

    nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
        switch c := ctx.(type) {
        case *grammar1.ColumnExprFunctionContext:
            nanopass.ReplaceNode(rw, c, "new text")
            return false // don't descend into the replaced node
        }
        return true // keep walking
    })

    result = nanopass.GetText(rw)
    return
}
```

### Template: env-aware pass (`Pass` value with a real `Apply`)

When the pass reads or writes settings/params/format, write `Apply` directly so
it can consult `e`.

```go
var MyEnvPass = nanopass.Pass{
    Name: "MyEnvPass",
    Apply: func(e *env.Environment, body string) (result string, err error) {
        pr, err := nanopass.Parse(body)
        if err != nil {
            err = eh.Errorf("MyEnvPass: %w", err)
            return
        }
        rw := nanopass.NewRewriter(pr)
        // ... rewrite body, and read/mutate e.Params / e.SessionSettings ...
        result = nanopass.GetText(rw)
        return
    },
    Properties: nanopass.PassProperties{
        Idempotent: true,
        Reads:      nanopass.RegionBody | nanopass.RegionParams,
        Writes:     nanopass.RegionBody,
    },
}
```

### Template: scope-aware pass (tables / WHERE / UNION ALL)

`BuildScopes` returns one root scope per top-level UNION member and now returns
an **error** for unexpected tree shapes — handle it. Iterate the whole tree with
`FlattenScopes`.

```go
func QualifyTables(defaultDB string) nanopass.Pass {        // parameterised → factory returns a Pass
    return nanopass.LiftBodyPass("QualifyTables", func(sql string) (result string, err error) {
        pr, err := nanopass.Parse(sql)
        if err != nil {
            err = eh.Errorf("QualifyTables: %w", err)
            return
        }
        rw := nanopass.NewRewriter(pr)

        scopes, err := nanopass.BuildScopes(pr, defaultDB)
        if err != nil {
            err = eh.Errorf("QualifyTables: %w", err)
            return
        }
        for _, scope := range nanopass.FlattenScopes(scopes) {   // every scope exactly once
            for i := range scope.Tables {
                ts := &scope.Tables[i]
                if ts.IsCTE || ts.IsSubquery || ts.IsFunction {  // none of these live in a database
                    continue
                }
                if ts.Database == "" {
                    nanopass.InsertBefore(rw, ts.Node, nanopass.QuoteIdentifier(ts.ResolvedDatabase(scope))+".")
                }
            }
        }
        result = nanopass.GetText(rw)
        return
    }, nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody})
}
```

`FlattenScopes` is the flat idiom. When you need to walk structurally instead,
recurse through the typed fields: `scope.CTEDefs[i].Scopes`,
`scope.Tables[i].Scopes` (FROM subqueries), and `scope.Subqueries`
(expression-level subqueries). All names (`Table`, `Database`, `Alias`,
`CTEDef.Name`) are stored **decoded** — re-encode with `QuoteIdentifier` (or
splice `NodeText`) when writing back.

### Template: macro registration (compile-time string expansion)

```go
expander := nanopass.NewMacroExpander()
expander.Register("jsonCol", func(args []nanopass.LiteralArg) (string, error) {
    // args[i].Type ∈ {LiteralTypeString, LiteralTypeInt, LiteralTypeFloat, LiteralTypeBool, LiteralTypeNull, LiteralTypeUnknown}
    // args[i].Value is RAW text — strings keep their quotes, e.g. "'hello'"
    return "JSONExtractString(payload, " + args[0].Value + ")", nil
})
result, err := nanopass.Sequence("expand", expander.Pass(), nanopass.ValidateGrammar1).Run(sql)
```

`MacroExpander.Pass()` declares `NeedsFixedPoint` (a macro nested in another's
argument list only becomes expandable after the inner one expands). Name matching
is case- and quoting-insensitive (`NormalizeCallName`). A registered macro that is
*never* reducible to literals is an error — macros are not real ClickHouse
functions, so leaving the call in the output would fail at query time.

### Template: compile-time function evaluation (Go-evaluable functions)

```go
eval := passes.NewFunctionEvaluator()
eval.RegisterBuiltins()                               // array(), tuple()
eval.Register("daysInMonth", func(args []any) (any, error) {   // EvalFuncI
    year, month := args[0].(uint64), args[1].(uint64)
    return computeDays(year, month), nil
}, true /* useAny: hand the evaluator unwrapped Go values, not marshalling.TypedLiteral */)

eval.OnObservation(func(obs nanopass.Observation) { /* inspect call sites; always-fire */ })

result, err := eval.Pass().Run("SELECT daysInMonth(2024, 2)")  // → "SELECT 29"
```

`FunctionEvaluator` folds recursively and partially: `myAdd(myAdd(1,2),3)` → `6`;
`myAdd(a, myAdd(1,2))` → `myAdd(a, 3)`. A `{name: Type}` slot argument resolves
via `env.Params` (resolved param ⇒ literal; unresolved ⇒ the outer call is
non-evaluable). Returning `marshalling.VerbatimSql{SQL: …}` splices raw SQL,
which is what justifies its `NeedsFixedPoint` (the spliced text may contain more
registered calls only the next re-parse sees).

## Canonicalization Pipeline

`passes.CanonicalizeFull(maxIter)` is a `Sequence` in this exact order — and is
the reference for how the substrate's "fix it at canonicalisation" rule cashes
out. It rewrites every sugar/operator form into **function-call form**, then
normalises case and identifier quoting last:

```
CanonicalizeWhitespaceSingleLine     // idempotent
CanonicalizeEquals                   // ==  →  =
CanonicalizeSugar                    // DATE/TIMESTAMP/EXTRACT/SUBSTRING/TRIM → function calls   (fixpoint)
FixedPoint(CanonicalizeConstructors(ConstructorFormFunction), maxIter)  // [..]/(..) → array()/tuple()
FixedPoint(CanonicalizeCaseConditionals, maxIter)                       // CASE → if()/multiIf()/caseWithExpression()
CanonicalizeMultiIf                  // 3-arg multiIf(c,r,d) → if(c,r,d)
FixedPoint(CanonicalizeCasts, maxIter)        // x::T and CAST(x AS T) → CAST(x, 'T')
CanonicalizeJoin                     // strictness-before-direction, drop OUTER, comma→CROSS, parenthesise USING
FixedPoint(CanonicalizeTernary, maxIter)      // c ? a : b → if(c, a, b)
CanonicalizeKeywordCase              // uppercase keywords (must be 2nd-to-last)
CanonicalizeIdentifiers              // double-quote identifiers (must be last)
```

`CanonicalizeConstructors(form)` takes `ConstructorFormFunction` (toward
`array()`/`tuple()`, what `CanonicalizeFull` uses — Grammar2 forbids `[..]`/`(..)`
literal syntax) or `ConstructorFormLiteral` (the reverse). End a normalisation
pipeline with `nanopass.ValidateGrammar2`: if it parses, canonicalisation is
provably complete.

## Analytical Passes — observation side channels and the discard marker

A pass that exists only to *observe* (not rewrite) returns the **discard marker**
so the runner forwards the input unchanged while keeping any env mutations:

- Return `marshalling.ControlFlow{Sentinel: nanopass.PassDiscardOutput}` from a
  handler; the marshaller renders it as a comment-shaped marker spliced into the
  body. `Sequence`, `runFixedPoint`, and `Pass.Run` detect it via
  `IsDiscardOutput` and forward the *input* instead of the rewrite.
- The marker scan is **quote-aware** — marker text inside a string literal or
  quoted identifier does not trigger; marker text inside a comment does. So
  `Sequence(p).Run(x) == p.Run(x)` holds for analytical passes.
- `FunctionEvaluator.OnObservation` is the canonical consumer: it fires one
  `Observation{Name, Args, Evaluated, Src}` per registered call site, whether or
  not the call folded. `Observation.Src` is a `SourceRange` into the *current*
  body (earlier passes / fixpoint iterations may have rewritten it).

## Testing a Pass

Every pass MUST be covered by the categories below. The mechanised core is
`nanopass.AssertProperties(t, p, corpus)`, which turns declared `PassProperties`
into enforced contracts (`Idempotent`, `NeedsFixedPoint`, their mutual exclusion,
and "the property must actually be exercised — no vacuous pass").

### 1. `AssertProperties` (idempotency / fixpoint, machine-checked)

Build the corpus from the shared `testdata` and feed it to every pass. For a
`NeedsFixedPoint` pass, the corpus must contain at least one entry that does
*not* converge in a single `Apply`, or the flag is rejected as unjustified.

```go
import (
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
    "github.com/stretchr/testify/require"
)

func TestMyPassProperties(t *testing.T) {
    entries, err := testdata.LoadCorpus()
    require.NoError(t, err)
    corpus := make([]string, 0, len(entries))
    for _, e := range entries {
        corpus = append(corpus, e.SQL)   // CorpusEntry{Name, SQL}
    }
    nanopass.AssertProperties(t, passes.MyPass, corpus)
}
```

(See `passes_test/nanopass_passes_assert_properties_test.go` for the live sweep
over every pass, including how factory passes — `MacroExpander`,
`FunctionEvaluator`, `ExpandColumns` — get registries/schemas wired and a
nested-forms corpus for the `NeedsFixedPoint` passes.)

### 2. Explicit input/output pairs

```go
func TestMyPass(t *testing.T) {
    tests := []struct{ name, input, expected string }{
        {name: "basic", input: "SELECT a FROM t", expected: "SELECT a FROM t"},
        // 5–10 cases covering the transformation
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := passes.MyPass.Run(tt.input)
            require.NoError(t, err)
            require.Equal(t, tt.expected, got)
            _, err = nanopass.Parse(got)                 // ALWAYS verify output parses
            require.NoError(t, err, "produced invalid SQL: %s", got)
        })
    }
}
```

### 3. Corpus validity, 4. UNION ALL / CTEs / subqueries, 5. Invalid-input rejection

- **Corpus validity** — `passes.MyPass.Run(entry.SQL)` produces parseable SQL for
  every corpus entry (skip entries the pass legitimately rejects).
- **UNION ALL / CTEs / subqueries** (structural passes) — assert the
  transformation reaches every branch; CTE *references* stay untouched while CTE
  *bodies* are transformed; use `len(BuildScopes(before)) == len(BuildScopes(after))`
  to check scope structure is preserved for pure passes.
- **Invalid input** — `""`, `"   "`, `"SELECT"`, `";;;"` must error (the input
  guards and parser reject them).

## ANTLR4 Go Runtime API Quick Reference

```go
// Parsing (Grammar1 unless you specifically want canonical-only)
pr, err := nanopass.Parse(sql)            // *ParseResult, Grammar1
pr, err := nanopass.ParseCanonical(sql)   // *ParseResult, Grammar2 (rejects non-canonical SQL)
// pr.Tree        antlr.ParserRuleContext  — assert to *grammar1.QueryStmtContext (Parse) / *grammar2.QueryStmtContext
// pr.TokenStream *antlr.CommonTokenStream — includes hidden-channel + EOF tokens
// pr.Parser      antlr.Parser
// pr.Source      string                   — original input (for SourceRangeOf)

// Token access
tok := pr.TokenStream.Get(i)              // antlr.Token
tok.GetTokenType(); tok.GetText(); tok.GetTokenIndex(); tok.GetChannel()
pr.TokenStream.Size()                     // total tokens incl. hidden + EOF

// Rewriter (node-level helpers accept RewriterI: both *antlr.TokenStreamRewriter and *TrackedRewriter)
rw := nanopass.NewRewriter(pr)
nanopass.ReplaceNode(rw, ctx, "new text")
nanopass.DeleteNode(rw, ctx)
nanopass.InsertBefore(rw, ctx, "prefix ")
nanopass.InsertAfter(rw, ctx, " suffix")
nanopass.ReplaceToken(rw, tokenIndex, "new text")
nanopass.DeleteToken(rw, tokenIndex)
result := nanopass.GetText(rw)
text   := nanopass.NodeText(pr, ctx)      // original source text of a node (incl. hidden-channel whitespace)
rng    := pr.SourceRangeOf(ctx)           // SourceRange{Start,End} byte offsets — src[rng.Start:rng.End]

// Conflict detection during development
trw := nanopass.NewTrackedRewriter(pr, logger)  // logs fatal/lossy edit overlaps as they are recorded
trw.HasConflicts(); trw.ConflictCount(); trw.Inner()

// CST navigation
ctx.GetStart().GetTokenIndex(); ctx.GetStop().GetTokenIndex()
ctx.GetParent(); ctx.GetChildCount(); ctx.GetChild(i); ctx.GetRuleIndex()
```

`TrackedRewriter` classifies the antlr4-go v4.13.x rewriter's quirks: partially
overlapping replaces and an insert strictly inside a replace **panic at
GetText** (fatal); a replace containing an earlier replace, overlapping deletes,
and inserts at a shared index are silently dropped/merged (lossy). Panics are
recovered to errors at the `Pass` boundary, so a conflicting pass fails its `Run`
rather than crashing the process.

## Identifier Codec

```go
nanopass.DecodeIdentifier(`"a""b"`)   // → a"b   (strip quoting/escapes to the raw name)
nanopass.QuoteIdentifier(`a"b`)       // → "a""b" (inverse, double-quoted output)
nanopass.NormalizeCallName(`"MyFn"`)  // → myfn   (registry key: case- and quoting-insensitive)
```

Scope names are stored decoded; compare decoded-to-decoded, re-encode on write.

## SelectScope Reference

```go
scopes, err := nanopass.BuildScopes(pr, "production")  // one root per top-level UNION member; errors on unexpected shapes
for _, scope := range nanopass.FlattenScopes(scopes) { // dedup'd: CTE defs are shared between UNION members
    for _, ts := range scope.Tables {
        db := ts.ResolvedDatabase(scope)               // explicit db, else scope.DefaultDatabase
    }
}

type SelectScope struct {
    Node            *grammar1.SelectStmtContext
    Tables          []TableSource   // FROM/JOIN sources
    Parent          *SelectScope    // enclosing scope (nil at top level)
    CTEDefs         []CTEDef        // visible CTEs: own WITH first (shadows), then inherited
    UnionMembers    []*SelectScope  // every SELECT of the enclosing UNION chain incl. self
    Subqueries      []*SelectScope  // expression-level subqueries (projection/WHERE/HAVING/IN/args), one per UNION branch
    DefaultDatabase string
}

type TableSource struct {
    Node       antlr.ParserRuleContext
    Database   string  // decoded; empty if unqualified
    Table      string  // decoded; for IsFunction holds the function name (diagnostics only)
    Alias      string  // decoded; from both `t x` and `t AS x`
    IsCTE      bool    // references a CTE name
    IsSubquery bool    // FROM (SELECT …)
    IsFunction bool    // table function: numbers(10), remote(…) — leave its args alone
    Scopes     []*SelectScope  // inner scopes of a FROM subquery, one per UNION branch
}

type CTEDef struct { Name string; Node antlr.ParserRuleContext; Scopes []*SelectScope; Recursive bool }  // Scopes: one per UNION branch of the body; Recursive: WITH RECURSIVE def, visible in its own body (self-entry has nil Scopes)

// Methods:
scope.ResolveAlias(name) (TableSource, bool)  // alias hides table name; name may be quoted or bare
scope.ResolveCTE(name)   (CTEDef, bool)        // walks ancestors
scope.AllScopes()        []*SelectScope        // this scope + descendants, deduped
ts.ResolvedDatabase(scope) string              // meaningless for IsCTE/IsSubquery/IsFunction
```

Database resolution matches ClickHouse: each table resolves independently against
the connection default; there is no ambient inheritance from sibling tables.

## Included Passes

| Pass / factory | Purpose | Properties |
|---|---|---|
| `StripComments` | remove `--` and `/* */` comments | Idempotent |
| `CanonicalizeKeywordCase` | uppercase keywords; preserve identifier case | Idempotent |
| `CanonicalizeWhitespace` / `CanonicalizeWhitespaceSingleLine` | collapse whitespace (preserve / drop newlines) | Idempotent |
| `CanonicalizeEquals` | `==` → `=` | Idempotent |
| `CanonicalizeIdentifiers` | double-quote identifiers (param slots & type exprs stay bare) | Idempotent |
| `CanonicalizeSugar` | DATE/TIMESTAMP/EXTRACT/SUBSTRING/TRIM → function calls | NeedsFixedPoint |
| `CanonicalizeConstructors(form)` | tuple/array between literal and function form | NeedsFixedPoint |
| `CanonicalizeCaseConditionals` | CASE → `if`/`multiIf`/`caseWithExpression` | NeedsFixedPoint |
| `CanonicalizeMultiIf` | 3-arg `multiIf` → `if` | Idempotent |
| `CanonicalizeCasts` | `x::T`, `CAST(x AS T)` → `CAST(x, 'T')` | NeedsFixedPoint |
| `CanonicalizeJoin` | strictness-before-direction, drop OUTER, comma→CROSS, parenthesise USING | Idempotent |
| `CanonicalizeTernary` | `c ? a : b` → `if(c, a, b)` | NeedsFixedPoint |
| `RemoveRedundantParens` | drop parens unneeded given operator precedence | Idempotent |
| `CanonicalizeFull(maxIter)` | the full canonicalisation `Sequence` (see above) | — |
| `QualifyTables(defaultDB)` | prefix unqualified tables with a default database; skip CTEs | Idempotent |
| `ExpandColumns(schema, defaultDB)` | expand `*`, `t.*`, `COLUMNS('regex')` via a `SchemaProviderI` | Idempotent |
| `WrapColumnsWithDynamic(pattern)` | wrap matching column names in `COLUMNS('^name$')` | Idempotent |
| `SetFormat(name)` / `RemoveFormat` | set/replace/remove the FORMAT clause; mirror into `env.Format` | Idempotent |
| `WriteSettings(map)` / `ModifySettings(fn)` | set / read-modify-write the SETTINGS clause; mirror into `env.StatementSettings` | Idempotent / factory |
| `ExtractLiterals(config)` | replace literals with `{name: Type}` slots; write Raw to `env.Params` | factory |
| `InjectParamsAsCTE(prefix, predicate, mapper)` | turn selected params into WITH-clause CTE defs | Idempotent |
| `PruneUnreferencedParams(prefix)` | drop `env.Params` entries no longer referenced by body | Idempotent |
| `ValidateColumnNames(pattern)` | error if any projected column name fails a regex | Idempotent |
| `MacroExpander.Pass()` | expand registered string macros (literal args) | NeedsFixedPoint |
| `FunctionEvaluator.Pass()` | evaluate registered Go functions; partial/recursive eval; param-slot resolution | NeedsFixedPoint |
| `nanopass.ValidateGrammar1` / `ValidateGrammar2` | parse-validate the body; pass it through unchanged | Idempotent |

Supporting types: `SchemaProviderI` (`GetColumns(db, table) (iter.Seq[string], int, bool)`),
`NewStaticSchemaProvider(map[string][]string)`, `NewCachingSchemaProvider(maxAge, delegate, maxSize)`;
`ExtractLiteralsConfig` (builder via `NewExtractLiteralsConfig(minLength)` + `SetPrefix`/`Blacklist`/…);
`ConstructorFormE` (`ConstructorFormLiteral`=1, `ConstructorFormFunction`=2);
`EvalFuncI = func([]any) (any, error)`; `ColumnNameValidationError` + `GetColumnNameViolations(err)`.

## Analysis Functions (`analysis/`)

Purely syntactic CST walks — no scope resolution. CTE references look like table
references and ARE included; resolve against `BuildScopes` (`TableSource.IsCTE`)
when you must distinguish real tables.

```go
analysis.ExtractTables(pr)    // []TableRef{Database, Table}
analysis.ExtractColumns(pr)   // []ColumnRef{Table, Column}   (Column may be a nested path "a.b")
analysis.ExtractFunctions(pr) // []FunctionRef{Name, IsParametric, IsWindow}
```

## Highlight Package (`highlight/`)

Two-phase semantic highlighter: lex for a baseline category, then parse and walk
the CST to refine identifiers into roles. Falls back to lexical-only if the parse
fails.

```go
spans := highlight.Highlight(sql)   // []Span{Start, Stop, Text, Category}  (byte offsets, half-open)
highlight.RenderANSI(spans)         // terminal output
highlight.RenderHTML(spans)         // <code> fragment; pair with highlight.HighlightCSS()
highlight.CategoryName(cat)         // human-readable category name
```

`CategoryE` has 18 values: `CatPlain, CatKeyword, CatOperator, CatIdentifier,
CatTableName, CatTableAlias, CatColumnName, CatColumnAlias, CatCTEName,
CatFunctionName, CatDatabaseName, CatTypeName, CatStringLit, CatNumberLit,
CatPunctuation, CatComment, CatWhitespace, CatParamSlot`.

## Key Grammar Types

Concrete CST types live in `grammar1` (for `Parse` results) and `grammar2` (for
`ParseCanonical` results). Both packages share one lexer, so token *values* match;
the constant *names* differ by package prefix.

### Token type constants (use the named constants, not the numbers)

```go
// grammar1.ClickHouseLexer<NAME> — keyword tokens occupy the low range (< 200):
grammar1.ClickHouseLexerIDENTIFIER          // 200
grammar1.ClickHouseLexerSTRING_LITERAL      // 205
grammar1.ClickHouseLexerMULTI_LINE_COMMENT  // 238   (hidden channel)
grammar1.ClickHouseLexerSINGLE_LINE_COMMENT // 239   (hidden channel)
grammar1.ClickHouseLexerWHITESPACE          // 240   (hidden channel)
```

The token stream includes **EOF** as the last token (`antlr.TokenEOF`, type `-1`)
— guard against it when iterating `pr.TokenStream`.

### `columnExpr` alternatives (all `GetRuleIndex() == grammar1.ClickHouseParserGrammar1RULE_columnExpr`)

```go
*grammar1.ColumnExprOrContext            // a OR b
*grammar1.ColumnExprAndContext           // a AND b
*grammar1.ColumnExprNotContext           // NOT a (only WITHOUT parens; NOT(x) is a function call)
*grammar1.ColumnExprIsNullContext        // a IS [NOT] NULL
*grammar1.ColumnExprPrecedence3Context   // =, !=, <, >, <=, >=, [NOT] IN, [NOT] LIKE, GLOBAL [NOT] IN
*grammar1.ColumnExprBetweenContext       // a [NOT] BETWEEN b AND c
*grammar1.ColumnExprPrecedence2Context   // +, -, ||
*grammar1.ColumnExprPrecedence1Context   // *, /, %
*grammar1.ColumnExprNegateContext        // -a (unary)
*grammar1.ColumnExprTernaryOpContext     // a ? b : c
*grammar1.ColumnExprLiteralContext       // 42, 'hello', NULL
*grammar1.ColumnExprIdentifierContext    // column_name
*grammar1.ColumnExprFunctionContext      // func(args) — includes NOT(x)!
*grammar1.ColumnExprParensContext        // (expr) — single expression in parens
*grammar1.ColumnExprTupleContext         // (expr, expr, …) — 2+ expressions
*grammar1.ColumnExprArrayContext         // [1, 2, 3]
*grammar1.ColumnExprSubqueryContext      // (SELECT …)
*grammar1.ColumnExprCaseContext          // CASE WHEN … END
*grammar1.ColumnExprCastContext          // CAST(x AS T)
*grammar1.ColumnExprAliasContext         // expr AS alias
*grammar1.ColumnExprArrayAccessContext   // arr[i]
*grammar1.ColumnExprTupleAccessContext   // t.1
*grammar1.ColumnExprWinFunctionContext   // func() OVER (…)
*grammar1.ColumnExprParamSlotContext     // {name: Type}
*grammar1.ColumnExprAsteriskContext      // *
*grammar1.ColumnExprDynamicContext       // COLUMNS('regex')
// Sugar forms (rewritten away by CanonicalizeSugar; present in Grammar1, absent from Grammar2):
*grammar1.ColumnExprDateContext          // DATE 'YYYY-MM-DD'
*grammar1.ColumnExprTimestampContext     // TIMESTAMP '…'
*grammar1.ColumnExprIntervalContext      // INTERVAL expr unit
*grammar1.ColumnExprExtractContext       // EXTRACT(unit FROM expr)
*grammar1.ColumnExprSubstringContext     // SUBSTRING(s FROM a FOR b)
*grammar1.ColumnExprTrimContext          // TRIM(… FROM s)
```

### `tableExpr` / `joinExpr` alternatives

```go
*grammar1.TableExprIdentifierContext  // table or db.table
*grammar1.TableExprAliasContext       // tableExpr AS alias  (or bare `tableExpr alias`)
*grammar1.TableExprSubqueryContext    // (SELECT …) in FROM
*grammar1.TableExprFunctionContext    // table_function(args)

*grammar1.JoinExprTableContext        // single table source
*grammar1.JoinExprOpContext           // left JOIN right ON …
*grammar1.JoinExprCrossOpContext      // CROSS JOIN / comma join
*grammar1.JoinExprParensContext       // (joinExpr)
```

### `SelectStmtContext` clause accessors (all return an interface or nil)

```go
stmt.WithClause()        stmt.FromClause()       stmt.WhereClause()
stmt.ProjectionClause()  stmt.GroupByClause()    stmt.HavingClause()
stmt.OrderByClause()     stmt.LimitClause()      stmt.LimitByClause()
stmt.SettingsClause()    stmt.PrewhereClause()   stmt.ArrayJoinClause()
stmt.WindowClause()      stmt.QualifyClause()
```

Cast to the concrete type to walk into them, e.g.
`stmt.WhereClause().(*grammar1.WhereClauseContext)`.

## Operator Precedence Table

Used by `RemoveRedundantParens`. Higher number = tighter binding.

| Level | Operators | CST context |
|---|---|---|
| 1 | OR | `ColumnExprOr` |
| 2 | AND | `ColumnExprAnd` |
| 3 | NOT (unary) | `ColumnExprNot` |
| 4 | IS [NOT] NULL | `ColumnExprIsNull` |
| 5 | =, !=, <, >, IN, LIKE, BETWEEN | `ColumnExprPrecedence3`, `ColumnExprBetween` |
| 6 | +, -, \|\| | `ColumnExprPrecedence2` |
| 7 | *, /, % | `ColumnExprPrecedence1` |
| 8 | unary - | `ColumnExprNegate` |
| 9 | ?: ternary | `ColumnExprTernaryOp` |
| 99 | atoms | literals, identifiers, functions, … |

Caveat: in ClickHouse's grammar ladder, `BETWEEN` and `?:` bind *looser* than
`OR`, so parens around them — and around BETWEEN operands — can be load-bearing.

## Input Guards & Robustness

```go
nanopass.CheckInputGuards(sql)   // Parse/ParseCanonical call this first; callers may pre-validate
const MaxInputBytes   = 1 << 20  // 1 MiB
const MaxNestingDepth = 128      // brackets ((,[,{) and CASE…END counted separately; quote/comment-aware
```

Measured pathologies the guards bound: deep parenthesis nesting drives
adaptive-prediction lookahead roughly quadratic (depth 400 ≈ 20 s, CPU not
stack); deep CASE nesting exhausts the goroutine stack (fatal) between depth 16k
and 64k. Invalid UTF-8 is rejected because the ANTLR runtime decodes to runes —
undecodable bytes become U+FFFD, silently corrupting string literals on any
rewrite. Escape binary data instead of embedding it raw.

The DFA cache (ADR-0084) replaces ANTLR's unbounded package-global with a bounded
process-local cache: `MaxDFAStates` (8192, ≈40–90 MB/grammar) triggers a rebuild,
checked every `DFACheckInterval` (256) parses. `nanopass.DFACacheStats()` returns
per-grammar `DFACacheStat{States, Resets}` — a rising `Resets` count means the
workload's structural diversity exceeds the cap and the cache is sawtoothing.

## Common Pitfalls

1. **Don't call `rw.GetTextDefault()` directly** — use `nanopass.GetText(rw)`.
2. **Don't mutate the CST.** It's read-only after parsing; all edits go through
   the `TokenStreamRewriter`.
3. **`WalkCST` returns `false` to skip a subtree, not to stop the walk.** There
   is no early-stop — set a flag and check it (`FindFirst` exists for "first
   match").
4. **`GetChild(i)` returns `antlr.Tree`**, not `ParserRuleContext` — always type
   assert: `if s, ok := node.GetChild(i).(*grammar1.SelectStmtContext); ok {…}`.
5. **`NOT(x)` is a function call** (`ColumnExprFunctionContext`); only `NOT x`
   (no parens) is `ColumnExprNotContext`.
6. **`IN (expr)` with a single value** parses the `(expr)` as
   `ColumnExprParens`, not `ColumnExprTuple` — guard paren-removal against IN's
   right operand.
7. **Don't `FindAll` `TableIdentifierContext`** to locate tables — it also
   matches column qualifiers (`t1.id`). Use `BuildScopes`.
8. **`BuildScopes` returns an error now** — handle it; an error means the tree
   shape was not produced by `Parse`.
9. **Iterate scopes with `FlattenScopes`**, not per-root `AllScopes` — CTE defs
   are shared between UNION members and would be visited once per member.
10. **Overlapping rewriter edits** silently drop/merge or panic at `GetText`.
    Use `TrackedRewriter` while developing to surface them at the offending call.
11. **The token stream includes hidden-channel tokens and EOF.** Check
    `tok.GetTokenType() == antlr.TokenEOF` (`-1`) when iterating.
12. **`go doc`/build need the repo build tags** (`GOFLAGS=-tags="$(cat ./tags)"`)
    or symbols read as "undefined".

## Grammar Modifications

The upstream ClickHouse ANTLR4 grammar carries these required local changes (full
detail in the package README and
[`../EXPLANATION.md`](../../../public/db/clickhouse/dsl/EXPLANATION.md)):

- **Whitespace/comments** moved from `-> skip` to `-> channel(HIDDEN)` for
  `WHITESPACE`, `SINGLE_LINE_COMMENT`, `MULTI_LINE_COMMENT` — without this the
  rewriter cannot preserve formatting.
- **Setting values** — `settingExpr` extended with a `settingValue` rule
  admitting arrays (`[1,2]`), tuples (`(1,2)`), and function-form constructors.
- **Grammar2 `typeName`** accepts the lexer keywords that double as type names
  (`Array`, `Date`, `Interval`, `Timestamp`, `UUID`) so canonicalisation can
  close `{d: Array(UInt8)}` / `CAST(x, 'Date')` into Grammar2.
- **`WITH RECURSIVE`** (ClickHouse ≥ 24.4 recursive CTEs) — `RECURSIVE` token +
  `RECURSIVE?` in both grammars' `ctes`/`withClause`; kept by canonicalisation
  (semantics, not sugar) and carried by `ast.Query.Recursive` /
  `ast.Select.Recursive`. In scopes, a recursive def is visible inside its own
  body (`CTEDef.Recursive`; the self-entry has nil `Scopes` — no self-descent).

## Known Grammar Limitations

These parse-fail at `Parse()` (add a `.sql` to `testdata/corpus/unsupported/`
if/when support lands):

- `FROM t SELECT a` (FROM-first syntax)
- `WITH (SELECT x) AS name` (scalar-subquery CTE)
- `EXISTS (SELECT …)` (EXISTS predicate)
- `* EXCEPT(col)`, `COLUMNS('…') APPLY(func)`, `REPLACE(…)` (column modifiers)
- `SET param = {'key': [1,2]}` (map literals in SET)
- Param slots with keyword names (`{date: UInt64}`) parse in Grammar1 but not
  Grammar2 (slot names there are bare `IDENTIFIER`).

Grammar1 also *over-accepts* a few shapes ClickHouse itself rejects (empty quoted
identifiers, non-type param-slot type expressions, `INTERVAL <expr> <non-unit>`);
the AST converter rejects them at its boundary.
