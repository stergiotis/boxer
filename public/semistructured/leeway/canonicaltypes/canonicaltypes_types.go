package canonicaltypes

import (
	"fmt"
	"io"
	"iter"

	"github.com/antlr4-go/antlr/v4"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

type BaseTypeStringE rune

var _ fmt.Stringer = BaseTypeStringE(0)

type BaseTypeTemporalE rune

var _ fmt.Stringer = BaseTypeTemporalE(0)

type BaseTypeMachineNumericE rune

var _ fmt.Stringer = BaseTypeMachineNumericE(0)

type ScalarModifierE rune

var _ fmt.Stringer = ScalarModifierE(0)

type ByteOrderModifierE rune

var _ fmt.Stringer = ByteOrderModifierE(0)

type WidthModifierE rune

var _ fmt.Stringer = WidthModifierE(0)

type Width uint32

var _ fmt.Stringer = Width(0)

type PrimitiveAstNodeI interface {
	IsStringNode() bool
	IsTemporalNode() bool
	IsMachineNumericNode() bool
	IsScalar() bool
	GenerateGoCode(w io.Writer) (err error)
	AstNodeI
}
type AstNodeI interface {
	cbor.Marshaler
	IsSignature() bool
	IsPrimitive() bool
	IsValid() bool
	IterateMembers() iter.Seq[PrimitiveAstNodeI]
	fmt.Stringer
}
type SignatureAstNode struct {
	members []AstNodeI
	str     string
}

var _ AstNodeI = SignatureAstNode{}

type GroupAstNode struct {
	members []PrimitiveAstNodeI
	str     string
}

var _ AstNodeI = GroupAstNode{}

type StringAstNode struct {
	BaseType       BaseTypeStringE
	WidthModifier  WidthModifierE
	Width          Width
	ScalarModifier ScalarModifierE
}

var _ PrimitiveAstNodeI = StringAstNode{}

type TemporalTypeAstNode struct {
	BaseType       BaseTypeTemporalE
	Width          Width
	ScalarModifier ScalarModifierE
}

var _ PrimitiveAstNodeI = TemporalTypeAstNode{}

type MachineNumericTypeAstNode struct {
	BaseType          BaseTypeMachineNumericE
	Width             Width
	ByteOrderModifier ByteOrderModifierE
	ScalarModifier    ScalarModifierE
}

var _ PrimitiveAstNodeI = MachineNumericTypeAstNode{}
var ErrInternalParserError = eh.Errorf("internal parser error")

type Parser struct {
	errListener *antlr4utils.StoringErrListener
	lex         resetableLexerI
	tokenStream *antlr.CommonTokenStream
}
