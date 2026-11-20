// Package canonicaltypes is the canonical type system leeway columns are
// declared in: a compact, target-independent spelling of primitive types
// (`u64`, `f32l`, `s`, `y`, `z64`, `vc`), their homogeneous-array and set
// forms (`sh`, `u64m`), groups of them, and signatures.
//
// [NewParser] returns a [Parser] whose ParsePrimitiveTypeAst and
// ParsePrimitiveTypeOrGroupAst turn a string into an AST node
// ([PrimitiveAstNodeI], [AstNodeI]); a node's String method prints it
// back. Types known at compile time are the generated values of the ctabb
// subpackage (ctabb.U64, ctabb.Sh), which avoid the parse. Helpers such as
// [DemoteToScalarPrim] and [PromoteScalarPrim] move between a type and its
// array form. The grammar is in the canonicaltypes skill.
//
// A Parser reuses one lexer and token stream across calls and is not safe
// for concurrent use; give each goroutine its own.
package canonicaltypes
