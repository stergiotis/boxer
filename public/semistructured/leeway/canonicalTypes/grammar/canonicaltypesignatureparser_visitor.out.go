// Code generated from CanonicalTypeSignatureParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // CanonicalTypeSignatureParser
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by CanonicalTypeSignatureParser.
type CanonicalTypeSignatureParserVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by CanonicalTypeSignatureParser#baseString.
	VisitBaseString(ctx *BaseStringContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#baseMachineNumeric.
	VisitBaseMachineNumeric(ctx *BaseMachineNumericContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#baseTemporal.
	VisitBaseTemporal(ctx *BaseTemporalContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#scalarModifier.
	VisitScalarModifier(ctx *ScalarModifierContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#byteOrderModifier.
	VisitByteOrderModifier(ctx *ByteOrderModifierContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#widthModifier.
	VisitWidthModifier(ctx *WidthModifierContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#CanonicalTypeString.
	VisitCanonicalTypeString(ctx *CanonicalTypeStringContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#CanonicalTypeTemporal.
	VisitCanonicalTypeTemporal(ctx *CanonicalTypeTemporalContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#CanonicalTypeMachineNumeric.
	VisitCanonicalTypeMachineNumeric(ctx *CanonicalTypeMachineNumericContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeSequence.
	VisitCanonicalTypeSequence(ctx *CanonicalTypeSequenceContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeGroup.
	VisitCanonicalTypeGroup(ctx *CanonicalTypeGroupContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeOrGroup.
	VisitCanonicalTypeOrGroup(ctx *CanonicalTypeOrGroupContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeOrGroupSequence.
	VisitCanonicalTypeOrGroupSequence(ctx *CanonicalTypeOrGroupSequenceContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeSignature.
	VisitCanonicalTypeSignature(ctx *CanonicalTypeSignatureContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#singleCanonicalType.
	VisitSingleCanonicalType(ctx *SingleCanonicalTypeContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#singleCanonicalTypeOrGroup.
	VisitSingleCanonicalTypeOrGroup(ctx *SingleCanonicalTypeOrGroupContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#singleCanonicalGroup.
	VisitSingleCanonicalGroup(ctx *SingleCanonicalGroupContext) interface{}
}
