// Package diag renders CBOR bytes as RFC 8949 §8 diagnostic notation —
// ADR-0219 §SD6: the pretty-printer under the cbordiag widget and the
// `cbor diagnostics --pretty` CLI.
//
// It walks the bytes itself and accepts any well-formed CBOR item, the
// non-canonical ones included: a non-shortest head, an indefinite length,
// a float wider than its value needs. The strict readers of the leeway
// canonical forms refuse exactly those, and the first thing a person wants
// to see when troubleshooting a form is the item that was refused.
//
// Two renderings share one walk. In Compact mode the output is, byte for
// byte, what the fxamacker library's Diagnose produces for the same bytes
// and the same options — pinned by an oracle test — so a compact rendering
// here and one from `cbor diagnostics` without --pretty agree. In pretty
// mode a container whose compact rendering does not fit the remaining
// line width is broken one element per line and indented.
//
// The output is a slice of spans, each carrying a category, so one walk
// serves the plain string (clipboard, terminal) and a syntax highlighter.
// The spans cover the rendered text exactly once, contiguously and in
// order, the guarantee the codeview builders rely on.
//
// Malformed input degrades rather than fails: what parsed is rendered, the
// failure is one Error span, the unparsed remainder follows as hex, and
// the error is also returned so a caller can badge it.
package diag
