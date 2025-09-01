// Code generated from CanonicalTypeSignatureParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // CanonicalTypeSignatureParser
import "github.com/antlr4-go/antlr/v4"

type BaseCanonicalTypeSignatureParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitBaseString(ctx *BaseStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitBaseMachineNumeric(ctx *BaseMachineNumericContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitBaseTemporal(ctx *BaseTemporalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitScalarModifier(ctx *ScalarModifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitByteOrderModifier(ctx *ByteOrderModifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitWidthModifier(ctx *WidthModifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeString(ctx *CanonicalTypeStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeTemporal(ctx *CanonicalTypeTemporalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeMachineNumeric(ctx *CanonicalTypeMachineNumericContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeSequence(ctx *CanonicalTypeSequenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeGroup(ctx *CanonicalTypeGroupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeOrGroup(ctx *CanonicalTypeOrGroupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeOrGroupSequence(ctx *CanonicalTypeOrGroupSequenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeSignature(ctx *CanonicalTypeSignatureContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitSingleCanonicalType(ctx *SingleCanonicalTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitSingleCanonicalTypeOrGroup(ctx *SingleCanonicalTypeOrGroupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitSingleCanonicalGroup(ctx *SingleCanonicalGroupContext) interface{} {
	return v.VisitChildren(ctx)
}
