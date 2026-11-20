// Package grammar1 is the ANTLR-generated lexer and parser for ClickHouse
// SQL that nanopass.Parse uses: the permissive grammar, which accepts the
// spellings people write. The *.out.go files are generated from the .g4
// grammars beside them and are not edited by hand; the stricter grammar the
// canonical form satisfies is the grammar2 package.
//
// Code outside the dsl tree reaches this package through the nanopass
// package rather than directly: the context types appear in a ParseResult's
// tree and in the visitor and walker callbacks.
package grammar1
