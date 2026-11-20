// Package passes holds the SQL-to-SQL passes built on
// [github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass]: the
// canonicalize family that brings a statement into the function-call form
// later passes expect ([CanonicalizeFull] composes them into one pass), and
// rewrites such as [QualifyTables], [ExtractLiterals] with [InjectParams],
// [SetFormat], [StripComments], [ValidateColumnNames] and
// [ResolveColumnNames].
//
// Most passes are values or constructors returning a nanopass.Pass, to be
// combined with nanopass.Sequence and applied with Pass.Run. A pass that
// needs configuration (a default database, a schema, a registry) is a
// constructor taking it.
//
// A pass assumes its input parses and, unless it says otherwise, that it is
// in canonical form; run the canonicalize passes first when the SQL comes
// from a person.
package passes
