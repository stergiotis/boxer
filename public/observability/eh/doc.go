// Package eh builds errors that carry the stack of the call site and,
// optionally, structured data, and reads them back for logs and records.
//
// [Errorf] and [New] are the constructors: [Errorf] behaves like
// fmt.Errorf with %w, so errors.Is and errors.As see through it, and adds
// the caller's stack. Structured fields are added with the builder in
// [github.com/stergiotis/boxer/public/observability/eh/eb], which ends in
// [ErrorfWithData]. On the way out, [MarshalError] is the zerolog error
// marshaller that the logging package installs, [FormatError] and
// [FormatErrorWithStack] render a chain for a terminal, and [WalkStreams]
// returns the same chain as plain values for a non-zerolog consumer.
//
// A message names what failed, not where: no package or function prefix,
// since the stack already records it. Values do not go into the message
// either — a format directive other than %w is a codelint error (CS013) —
// but onto the builder, where a sink can read them by name. See the Error
// Handling section of CODINGSTANDARDS.md.
package eh
