---
type: adr
status: proposed
date: 2026-09-29
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0268: retire the API-stub skill assets and the `stubber` / `llmuse` generators

## Context

Three skills carried a snapshot of a Go package's public API as an asset:
imzero2's `bindings.md`, fffi2's `fffi2.md` and leeway-advanced's
`leeway_boxer.md`, each under the skill's `assets/` directory. Each was the output of
two commands run by hand in sequence. `stubber` rewrote a package tree with
unexported declarations removed and every function body replaced by
`panic("stub")`. `llmuse` then concatenated the stubbed files into one
markdown prompt. The commands entered the tree on 2026-03-13 and the assets
with the skills port on 2026-05-29. No script or CI step regenerated them, and
no ADR decided to keep them.

The snapshots drifted. By the date of this ADR the imzero2 asset still named the
bindings package by its former name, `components`, and covered 11 of the
package's 24 source files. ADR-0195 records hand-editing it to remove one symbol.
None of the three skills linked its asset, so an agent found them only by
listing the directory. An agent working in this repository has the source, and
`go doc -all <package>` prints the current API surface in seconds. Regenerating
a bindings snapshot with `stubber`, by contrast, did not finish in reasonable
time on this tree.

## Decision

Remove the three API-stub assets, the `stubber` and `llmuse` packages, and the
`codedriven` command group that registered them. `code analysis golang` keeps
`wasmsurvey`, which was their only sibling.

A skill that needs to show an API names the package and points at `go doc`. It
does not carry a copy of the API.

Out of scope, and kept:
- `doc/skills/imzero2/assets/egui2_api_reference.md` stays. It is a widget
  catalogue that `egui2gen generate doc` regenerates from the IDL on every
  `generate.sh` run, so it cannot drift the same way.
- The hand-written assets stay: `leeway_structure_summary.md`, the leeway
  diagrams and the beginner answers.

## Alternatives

- **Regenerate the snapshots in `generate.sh`.** This would stop the drift,
  but each asset would still be a copy of what `go doc` prints from the
  source. `stubber` was also too slow on this tree to run on every
  generation.
- **Replace them with `go doc -all` snapshots.** These are fast and current on
  the day they are written, but they drift the same way. The detail budget
  says to link to what a reader can regenerate from the code rather than
  transcribe it.
- **Keep `stubber` and `llmuse`, delete only the assets.** Nothing else in the
  tree or its consumers calls them, so the code would stay only to be
  maintained.

## Consequences

- An agent reading a skill no longer sees an API listing that can disagree with
  the code, and can no longer read one offline without the source.
- Nothing outside this repository imported `stubber` or `llmuse`. The commands
  `boxer codedriven go stub|prompt` and `boxer code analysis golang
  stub|prompt` are gone.
- Anyone who wants a redacted, compilable copy of a package, which was
  `stubber`'s original purpose, would have to restore the package from history.

## Status

Proposed 2026-09-29. The removal lands with this ADR.

## Updates

## References

- [ADR-0078](./0078-tinygo-wasm-amenability-survey.md) — `wasmsurvey`, the
  surviving sibling under `public/code/analysis/golang/`.
- [ADR-0195](./0195-retire-puffin-egui-dependency.md) — its migration hand-edited
  the imzero2 bindings asset this ADR removes.
- [ADR-0083](./0083-retire-llm-generated-build-tags.md) — an earlier retirement
  of LLM-specific tooling.
