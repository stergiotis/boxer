// Package typed provides Go-side typed wrappers over the raw FFFI2 wire
// runtime. Includes RetainedFffiBuilder, the per-frame capture handle,
// and helpers for retained-element identity. Consumers compose these
// instead of touching the raw byte buffers in fffi2/runtime/.
package typed
