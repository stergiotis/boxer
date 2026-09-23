// Package wire is the framing half of the stevedore contract (ADR-0252 §SD2):
// how one message crosses a pipe to an external-process processor, and how
// several parts travel inside one message.
//
// Three frame codecs match what streaming frameworks offer for a subprocess
// processor — newline-delimited lines, a big-endian 32-bit length prefix, and
// netstrings — and one part format, the framework's binary archive: a
// big-endian 32-bit part count followed by each part as a big-endian 32-bit
// length and its bytes. Everything here is implemented from that statement and
// depends on no framework's module.
//
// The lines codec cannot carry a payload holding a newline; a processor that
// may receive or emit binary bytes runs under one of the other two, and the
// how-to says so.
package wire
