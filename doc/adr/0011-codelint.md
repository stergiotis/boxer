---
type: adr
status: accepted
date: 2026-05-18
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-18
---

# ADR-0011: Code-linter for the Boxer coding standard

## Context

CODINGSTANDARDS.md after the 2026-05-18 trim retains ~16 rules covering error handling, concurrency, naming, typing, memory layout, and iteration. `scripts/ci/lint.sh` currently enforces ~2 of these mechanically (vet's printf check recognises `eh.Errorf` as a wrapper; doclint's DL009 covers exported-symbol doc-comment shape, warn-only). Everything else is human-enforced and drifts under AI-assisted authoring.

A rule-by-rule audit identified roughly ten conventions that fit deterministic AST/grep checks at 30–80 LoC each, plus a smaller second tier that needs `go/types`. They cover the must-enforce tier (error wrapping via `eh`/`eb`, ctx-first, mutex-by-value, typed atomics) and the high-AI-leverage tier (interface `I` suffix, enum `E` suffix + value prefix, banned imports, iter naming).

doclint and llmtag already established a precedent for in-tree governance linters under `public/gov/`. A symmetric package for Go-code rules slots in alongside them rather than as a separate binary.

## Decision

We add **`public/gov/codelint`** — a Go-code linter for project-specific conventions. Rules are implemented as `golang.org/x/tools/go/analysis` analyzers. The CLI shell mirrors doclint's output and exit conventions.

### 1. Package location and CLI

Code lives under `public/gov/codelint/`. The CLI subcommand is `boxer gov codelint`, registered in `public/gov/gov.go` alongside `doclint`/`llmtag`/etc. File-naming matches the neighbouring packages (`gov_codelint_*.go`, one rule per `gov_codelint_rule_csNNN.go`).

### 2. Rule ID prefix

Rule IDs use prefix `CS` (coding-standard), three-digit numbering: `CS001`, `CS002`, … This back-references CODINGSTANDARDS.md directly and disambiguates from DL* (doclint) and the LLM-tag rules.

### 3. Analyzer framework

Each rule is internally an `*analysis.Analyzer`. The package supplies a small custom driver that:

- loads packages via `golang.org/x/tools/go/packages` (Mode includes `NeedSyntax | NeedTypes | NeedTypesInfo | NeedImports`)
- runs each analyzer per-package against a manually-constructed `*analysis.Pass`
- translates `analysis.Diagnostic` → `Finding` (matching doclint's `Finding` struct)
- applies per-line `//boxer:lint disable=` suppression before yielding

We do not use `multichecker.Main` / `singlechecker.Main` as the entry point — their CLI conventions diverge from the in-repo `boxer gov *` style. We do depend on `go/analysis` (and `go/analysis/passes/inspect` when shared traversal is wanted) so rules remain liftable into a `go vet -vettool=` driver later.

### 4. Severity, output, exit

Mirrors doclint exactly:

- `info` / `warn` / `error` severities
- human or JSON output (`--format`)
- `--min-severity` threshold
- exit non-zero only on `error`-severity findings

Phase-1 rules ship at `warn` so a baseline can be measured without breaking the build. Promotion to `error` is per-rule, by separate commit, once the in-tree fallout is cleared.

### 5. Disable directive

Per-line suppression:

```go
err := fmt.Errorf("legitimately raw: %w", e) //boxer:lint disable=CS001 reason="boundary adapter"
```

The directive lives on the same line as the offending token. A directive without a `reason="…"` clause is itself a violation (CS999 is reserved for the meta-check). Wholesale file or package disables are deliberately not provided — blanket disables erode the standard, and per-line + `reason=` keeps every exception auditable.

### 6. Generated-file scope

`*.out.go` and `*.gen.go` files are skipped at the loader level, matching the grep filters already in `scripts/ci/lint.sh`. `llm_generated_*` build-tag files are linted normally — they are the most drift-prone authors.

### 7. CI wiring

A `step_begin "codelint"` block is added to `scripts/ci/lint.sh` between `staticcheck` and `doclint`. Output handling matches the existing warn/fail pattern: `warn` when findings exist but rc stays 0; `fail` only when an error-severity finding sets rc=1.

### 8. Rule phasing

**Phase 1** (deterministic AST/grep checks):

- CS001 `fmt.Errorf` outside `public/observability/eh`
- CS002 `ctx` not in first-parameter position
- CS003 `*sync.Mutex` / `*sync.RWMutex` field (mutex by value preferred)
- CS004 `atomic.Load*(&v)` / `atomic.Store*(&v)` — prefer typed `atomic.Int64` etc.
- CS005 Interface name without `I` suffix
- CS006 Enum type without `E` suffix (named integer with const block)
- CS007 Enum value not prefixed with type-minus-`E`
- CS008 Type alias (`type X = Y`)
- CS009 Banned imports (curated list: `crypto/sha256`, `encoding/json` non-v2, `github.com/google/uuid`, stdlib `log`)
- CS010 Iter-method naming (`iter.Seq[…]` return → name ∈ {`All`, `Values`, `Keys`, `Backward`})

CS001 ships first as the scaffold validator.

**Phase 2** (needs `go/types`): `Is*` → `bool`, `*E`-suffix returns error last, always-`%w` for error args.

**Phase 3** (LLM-assisted, lives in `ultrareview` not here): opposite-pair vocabulary, doc-comment tautologies, zero-value usability — judgment-bound, not deterministic.

## Alternatives

- **`multichecker.Main` entry point.** Different CLI shape and diagnostic format from `boxer gov *`. The custom driver is ~100 LoC and gives a unified UX.
- **Hand-rolled AST walks (mirror doclint exactly).** Forfeits `*analysis.Analyzer` interop. Rules become harder to lift into `go vet -vettool=` if we ever want that.
- **Rule prefix `GL`.** Less explicit back-reference; `CS` ties each finding to a section of CODINGSTANDARDS.md.
- **Per-file / per-package disables.** Erode the standard. Per-line + `reason=` is the only sanctioned escape hatch.

## Status

Accepted 2026-05-18. Phase 1 lands incrementally; CS001 ships with this ADR. The CS-numbering is append-only from this point.

## Consequences

- One more lint step in `scripts/ci/lint.sh`; expected overhead is well below staticcheck (no SSA construction, single AST pass per rule per package).
- Adds an explicit `golang.org/x/tools/go/analysis` import surface under `public/gov/`. Previously only doclint and llmtag lived there and neither uses go/analysis.
- CS-numbering carries semantic weight (back-references the standard). Renumbering is therefore a breaking change for any out-of-tree disable-directive comments — the numbering should be treated as append-only.
- Phase-1 rules will surface in-tree fallout. Plan: address commit-by-commit at `warn`; promote individual rules to `error` only when the residual count is zero.
