// Package chtype parses ClickHouse type strings into a small syntax tree.
//
// A ClickHouse type is a name with an optional parenthesised argument list, and
// an argument is one of three things: a nested type (`Array(String)`), a named
// element (`Tuple(a UInt8)`, `Nested(x String)`), or a literal
// (`FixedString(16)`, `DateTime64(9, 'UTC')`, `Enum8('a' = 1)`). That is the
// whole grammar, and [Parse] implements exactly it — it does not know which
// names exist, so a type family added by a newer server parses without a change
// here.
//
// The package exists because three sources hand the SQL surface a type as a
// string and nothing else: a generated component store's Projection bakes its
// slot list only into the CAST's type literal (ADR-0066), `system.columns.type`
// is a string, and `DESCRIBE (SELECT …)` answers with one. ADR-0190 §SD5 and
// §SD6 read all three through here.
//
// # Whitespace
//
// ClickHouse 26.x pretty-prints compound types across lines in TSV output, so
// `DESCRIBE` can answer `Tuple(\n    a UInt8,\n    b String)`. The tokenizer
// skips whitespace between tokens, and [Type.String] re-emits the canonical
// one-line spelling.
//
// # What it does not do
//
// It does not validate. `Array(Nonesuch)` and `Tuple(a)` parse; whether the
// server accepts them is the server's answer. A consumer that needs a known
// family checks [Type.Name] itself.
package chtype
