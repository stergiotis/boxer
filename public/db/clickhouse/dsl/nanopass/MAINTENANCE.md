Here's OPUS4.6 advice for long-term development:

**Keep the nanopass discipline strict**

The most important invariant is: every pass re-parses from scratch, receives valid SQL, returns valid SQL. The moment you start passing state between passes or sharing a single CST across multiple transformations, you lose the composability that makes this architecture robust. If a transformation is too complex for one pass, use two passes with fixed-point iteration rather than breaking the invariant.

**Grow the corpus before growing features**

Every bug we found came from a structural pattern we hadn't tested. Before implementing a new pass, add 5-10 SQL examples that exercise the patterns that pass will touch. The corpus is your most valuable asset — more so than the passes themselves. When you encounter a real query from your data scientists that breaks something, add it to the corpus immediately, even before fixing the bug.

**Resist the urge to build a full AST**

The CST + scope layer is sufficient for your stated goals. A full AST adds a translation layer that needs to be maintained in sync with the grammar, and it obscures the direct relationship between source tokens and transformations. If you find yourself wanting richer tree operations, first ask whether `BuildScopes` can be extended with one more field rather than introducing a new abstraction.

**Extend `BuildScopes` incrementally**

When you need new semantic information (e.g., "which columns are in the GROUP BY"), add it as a new field on `SelectScope` rather than building a separate analysis. Keep `SelectScope` as the single source of structural truth. The pattern is always: walk the CST once during `BuildScopes`, populate the field, consume it in passes.

**Grammar maintenance is the highest-risk area**

The ANTLR grammar is generated code that you've now modified (the `channel(HIDDEN)` fix). When the upstream grammar is updated, you'll need to re-apply this change. Keep a patch file or a script that applies your modifications after regeneration. Document every grammar modification in a `GRAMMAR_CHANGES.md` alongside the `.g4` file.

**Testing strategy for new passes**

For every new pass, write these four test categories in order:

1. **Explicit cases** — 5-10 input/expected pairs covering the happy path
2. **Idempotency** — `pass(pass(x)) == pass(x)` for all inputs
3. **Corpus validity** — the pass produces parseable SQL for every corpus entry
4. **Scope preservation** — pure passes (case, whitespace, comments, parens) don't change the scope structure

If a pass modifies structure (like `AddWhereCondition`), replace scope preservation with a scope delta check — verify the structure changed in the expected way and only the expected way.

**When to extend the grammar**

You deferred FROM-first syntax, scalar subquery CTEs, and `EXISTS`. Extend the grammar only when a real user query requires it — not speculatively. Each grammar change requires regenerating the Go code, re-running the full corpus, and verifying that the new rule doesn't introduce ambiguity with existing rules. The `FROM-first` change is safe (one line). The scalar subquery CTE is moderate risk. `EXISTS` would require understanding how ClickHouse actually parses it at the server level.

**Watch out for these specific risks going forward**

The `WalkCST` function visits nodes depth-first but doesn't let you control post-order vs pre-order. If you ever need bottom-up processing (e.g., resolving inner expressions before outer ones), you'll need to build a post-order walker. The current pre-order walk is fine for all existing passes.

**File organization as the module grows**

You're at roughly 15 files now. Once you pass 25-30, consider splitting into sub-packages: `nanopass/scope`, `nanopass/macro`, `nanopass/passes/security` (row-level filtering), `nanopass/passes/compat` (dialect rewrites). But don't split prematurely — the flat layout is easier to navigate until it genuinely becomes unwieldy.