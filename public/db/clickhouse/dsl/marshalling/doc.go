// Package marshalling converts between Go values and ClickHouse SQL literal
// text: scalars, homogeneous arrays and tuples, as a [TypedLiteral] that
// keeps the canonical type beside the value so a literal round-trips
// without losing its type.
//
// [MarshalGoValueToSQL] and [MarshalTypedLiteralToSQL] render a literal,
// [UnmarshalScalarLiteral] parses one back, and [EscapeString] and
// [EscapeIdentifier] quote a string or an identifier for embedding. The
// nanopass-marshalling skill has the full surface.
//
// Embed a value in SQL through these functions rather than with string
// formatting: [EscapeIdentifier] in particular is what makes leeway's
// physical column names, which contain ':' and other separators, legal in
// a statement.
package marshalling
