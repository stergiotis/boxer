package idl

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

var testParser = canonicaltypes.NewParser()

// mustParseType is a tiny test helper that returns a real primitive canonical
// type AST node from its textual form. Panics on parser error.
func mustParseType(s string) canonicaltypes.PrimitiveAstNodeI {
	return testParser.MustParsePrimitiveTypeAst(s)
}

// expectPanic asserts that fn panics. Returns the recovered value so the
// caller can inspect it if desired.
func expectPanic(fn func()) (recovered interface{}) {
	defer func() {
		recovered = recover()
	}()
	fn()
	return
}
