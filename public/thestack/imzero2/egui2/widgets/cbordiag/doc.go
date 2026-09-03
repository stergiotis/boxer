// Package cbordiag shows CBOR bytes as RFC 8949 §8 diagnostic notation —
// ADR-0219 §SD6: a toolbar (byte count, a compact / expanded toggle, a copy
// button, an optional verdict line the host sets) over a codeview job built
// from the cbor/diag printer's spans.
//
// The notation is rebuilt only when the bytes or the options change: the
// caller-owned State keeps the last rendering under a content key, so a
// frame that shows the same item again costs a hash of the bytes and
// nothing else.
//
//	r := cbordiag.New(ids, "wire")                      // once
//	r.Render(&state, item, diag.Options{TagComments: true}) // per frame
//
// Two views shown at once need two States and two Renderers with different
// prefixes, as with fieldview: the prefix scopes the widget ids.
package cbordiag
