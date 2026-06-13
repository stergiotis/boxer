---
type: adr
status: proposed
date: 2026-06-13
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Implemented on a review branch; do not treat as accepted until reviewed and merged.

# ADR-0083: Retire the `llm_generated_*` build-tag governance

## Context

From commit `aa78183` onward, every Go file whose git-blame attribution was
majority-LLM carried a `//go:build llm_generated_<model>` constraint
(`opus46`, `opus47`, `opus48`, `gemini3pro`). The `gov llmtag` tool derived and
maintained the directives from `Co-Authored-By` trailers; CI linted for drift;
`./tags` enumerated the active model tags so the tree would compile. The stated
purpose (README *AI Codegen Declaration*) was that **an AI-free build remains
possible** — omit the tags and only human-authored files compile.

By 2026-06 the scheme covered **1165 of 2286 tracked files (≥51%)**, and its
value had eroded:

- **The AI-free build is symbolic, not functional.** Most of the working surface
  (leeway, clickhouse/dsl, imzero2, keelson) is LLM-authored; an AI-free
  configuration omits it. The gate is per-file majority-vote, so a "human" file
  may still hold up to 49% LLM lines — the artifact is *mostly*-human, not
  provably AI-free. Keeping it even *compilable* required hand-written
  negative-tag stubs (`//go:build !(llm_generated_…)`, e.g. `gov_repo_empty.go`),
  maintained per package and of unverified completeness.
- **It taxed every invocation.** `-tags="$(cat ./tags)"` had to be threaded
  through every build/test/vet/generate/lint and the IDE/gopls; omitting it
  produced misleading "undefined" errors. Cross-repo consumers (`pebble2impl`,
  `hackathon_2026`) had to replicate `./tags` (ADR-0055).
- **It was drifting.** The current authoring model, `opus48`, was never added to
  the tool's identity registry, so `gov llmtag` would have *stripped* the
  `opus48` tags that `./tags` and four files carried by hand.
- **The provenance it encodes is redundant with git.** The tags are a cached
  projection of `Co-Authored-By` trailers; the trailers are the durable source
  and reconstruct the tags on demand.

## Decision

Retire the build-tag governance.

1. **Strip the directives.** A constraint-aware pass substituted
   `llm_generated_* → true` and constant-folded every `//go:build` expression:
   `true` → strip the directive (**1132 files**, now compiled unconditionally);
   `false` → delete the file (**3 AI-free stubs**); residual → rewrite to the
   non-LLM remainder (**30 files**, e.g. `integration && llm_generated_opus47`
   → `integration`, `!bootstrap && llm_generated_opus47` → `!bootstrap`,
   `linux && gpu_intel` preserved).
2. **Trim `./tags`** to the genuine feature/experiment tags
   (`identifier_tag_fixed16`, `boxer_enable_profiling`, `goexperiment.jsonv2`).
3. **Repoint attribution to the source of record.** `gov repo authorship` now
   reads `Co-Authored-By` trailers from a single `git log --numstat` pass rather
   than the build-tag line; `codestat.yaml` drops its tag-grep `scc` split and
   relies on that step. The code generator that stamped the tag into generated
   files (`keelson/designsystem/colors/emit`) no longer emits it.
4. **Drop the CI gate.** The `llmtag --diff` step is removed from
   `scripts/ci/lint.sh` (post-strip it would flag all 1132 files as untagged).
5. **Keep `gov llmtag` dormant.** The tool is retained (now unconditional).
   Because the trailers are intact, `gov llmtag --apply` can reconstruct the
   tags at any time — the lift is reversible short of a history rewrite.

The whole tree compiles and all default-tagged tests compile **without** the
LLM tags; this was expected, since the canonical build already passed every
model tag simultaneously, so their union was always co-compilable.

## Consequences

**Positive**

- The bulk of the tree builds with no LLM tags; the most common "undefined"
  papercut is gone, and downstream modules no longer need the model tags to
  build boxer's packages or tools.
- Attribution is sourced from the durable trailers and is now more correct in
  one respect: the vendor-family match (`claude`/`gemini`) counts `opus48` and
  future revisions that the fixed tag registry missed.

**Negative / accepted**

- The functional AI-free build is gone. It was already symbolic (see Context),
  so this formalises reality rather than losing a working capability.
- The authorship metric changes shape — from a per-month *stock* (lines living
  in tagged files at each snapshot) to cumulative *additions* attributed by
  commit trailer. Deletions are not subtracted, so totals over-count rewritten
  lines; the report labels these as "authored," not current.
- Cross-repo imported provenance is undercounted. Code imported from another
  repo (e.g. the 2026-05 pebble2impl import) is attributed to its boxer import
  commit, which carried no LLM trailer; the tags used to carry that origin. The
  signal still exists in the source repo's history.

**Neutral**

- `gov llmtag` and its `KnownLLMIdentities` registry remain in-tree as the
  reversal path; a stale `llm_generated_*` mention survives in a commented-out
  nilaway block in `lint.sh`.
- Cross-repo consumers (`pebble2impl`, `hackathon_2026`) keep their own
  `llm_generated_*` governance until separately lifted; nothing here forces it.

## Alternatives considered

- **Simplify to a single `llm_generated` tag** (drop the per-model granularity).
  Keeps the AI-free-build posture and in-source attribution, fixes the `opus48`
  drift, and roughly halves the maintenance — but retains the threading tax and
  the negative-stub upkeep, and preserves a capability whose artifact is no
  longer useful. Rejected in favour of removing the tax outright; per-model
  detail remains recoverable from the trailers.
- **Keep, fix the drift only** (add `opus48` to the registry). Lowest effort, but
  banks the full recurring cost to preserve the symbolic artifact. Rejected for
  the same reason.
