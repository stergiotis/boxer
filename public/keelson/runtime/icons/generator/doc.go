// Package generator is the codegen library behind cmd/iconsgen. It reads
// the @phosphor-icons/core src/icons.ts catalogue (vendored at
// keelson/runtime/icons/phosphor-icons.ts) and emits Go constants for
// every Phosphor regular-weight icon — see ADR-0044 §SD3.
//
// Output lands at:
//
//   - phosphor.out.go         — PhXxx constants + alias constants
//   - phosphor_lookup.out.go  — PhosphorByName map + PhosphorNames slice
//
// The parser is intentionally not a full TypeScript parser. It walks
// the catalogue with a brace-depth-tracked entry slicer (skipping
// string literals so braces inside strings do not confuse the depth)
// and then regex-matches the four fields each entry must carry. If
// upstream renames any of those four fields, parsing produces zero
// entries and the build fails downstream — the correct signal to
// re-evaluate the parser against the new format.
package generator
