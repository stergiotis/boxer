package canonicaltypes_test

import (
	"fmt"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

// ExampleParser_ParsePrimitiveTypeAst parses canonical type strings and
// prints them back. A Parser holds a lexer and is not safe for concurrent
// use; keep one per goroutine.
func ExampleParser_ParsePrimitiveTypeAst() {
	p := canonicaltypes.NewParser()
	for _, s := range []string{"u64", "f32lh", "sx16", "z64", "vc"} {
		ast, err := p.ParsePrimitiveTypeAst(s)
		if err != nil {
			panic(err)
		}
		fmt.Println(ast, ast.IsValid())
	}
	// The Go spelling is not a canonical type.
	_, err := p.ParsePrimitiveTypeAst("uint64")
	fmt.Println(err != nil)
	// Output:
	// u64 true
	// f32lh true
	// sx16 true
	// z64 true
	// vc true
	// true
}

// Example_abbreviations uses the generated ctabb values, which spare the
// parse where the type is known at compile time.
func Example_abbreviations() {
	fmt.Println(ctabb.U64, ctabb.Sh, ctabb.Z64)
	fmt.Println(canonicaltypes.DemoteToScalarPrim(ctabb.Sh))
	// Output:
	// u64 sh z64
	// s
}
