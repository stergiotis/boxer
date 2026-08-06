// Package codevol measures how much code a Go program is made of, and how
// much of it is somebody else's (ADR-0173).
//
// Three acquisition tiers live here, separated by what they need rather than
// by what they answer. That separation is the point: the cheap tiers work on
// a deploy target with no toolchain, no source tree and no module cache,
// where the expensive one cannot run at all.
//
//   - [Modules] reads the module list out of the running binary via
//     runtime/debug. Costs microseconds, needs nothing.
//   - [ReadSelfSymbols] reads the binary's own symbol table to report what
//     the linker actually kept after dead-code elimination. Tens of
//     milliseconds, needs only an unstripped ELF binary.
//   - [CountFiles] classifies source lines with go/scanner. Needs the source
//     tree, so the caller supplies the file paths — this package deliberately
//     does not import golang.org/x/tools, which is what lets it be registered
//     into the static provider set beside the other two.
//
// Nothing here imports a UI package or the introspect registry; the providers
// in keelson/runtime/introspect/providers wrap these into tables.
package codevol
