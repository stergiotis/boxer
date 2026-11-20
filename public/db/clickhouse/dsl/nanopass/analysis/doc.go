// Package analysis answers read-only questions about a parsed ClickHouse
// statement: which tables, columns and functions it references
// ([ExtractTables], [ExtractColumns], [ExtractFunctions]), what kind of
// statement it is ([ClassifyStatementKind], [Classify]), which stratum a
// query falls in ([ClassifyQueryStratum]) and what it can reach
// ([ClassifyQuerySecurity]). [SplitStatements] splits a script.
//
// Most functions take a nanopass.ParseResult, so one parse serves several
// questions. The extractors report what the text names; they do not resolve
// a column against a schema — that is the passes package's
// ResolveColumnNames.
package analysis
