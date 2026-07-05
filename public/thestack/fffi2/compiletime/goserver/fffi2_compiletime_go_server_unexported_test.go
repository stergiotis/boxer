package goserver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func nm(s string) naming.StylableName {
	return naming.MustBeValidStylableName(s)
}

// TestUnexportedMethodEmitsLowerCamel guards the MethodBuilder.Unexported()
// flag (added to hide the atoms richText/style sub-protocol behind
// RichTextScope). A flagged method must emit a lower-camel — i.e. unexported —
// Go receiver method, while its opcode enum reference stays UpperCamel so the
// wire format and client dispatch are untouched. A normal method stays
// UpperCamel throughout.
func TestUnexportedMethodEmitsLowerCamel(t *testing.T) {
	mb := idl.NewMethodBuilder()
	mb.BeginMethod(nm("plainMethod")).Arg(nm("val"), ctabb.S).EndMethod()
	mb.BeginMethod(nm("hiddenMethod")).Unexported().Arg(nm("val"), ctabb.S).EndMethod()

	factory := idl.NewBuilderFactoryNode(nm("richDemo")).
		WithSettingImmediate(true).
		WithReturnType(ir.NewConcreteType("richDemo")).
		AddMethods(mb.Build()...).
		Build()

	var method, factoryW, fetcher, enum, typ bytes.Buffer
	wh := WriterHolder{
		MethodWriter:  &method,
		FactoryWriter: &factoryW,
		FetcherWriter: &fetcher,
		EnumWriter:    &enum,
		TypeWriter:    &typ,
	}
	tracker := compiletime.NewStateAndErrTracker[GeneratorStateE](GenerateStateInitial, "")
	if err := GenerateCode(wh, []ir.NodeI{factory}, tracker); err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	code := method.String()

	// Normal method: exported (UpperCamel) receiver method.
	if !strings.Contains(code, "func (inst RichDemoFluid) PlainMethod(") {
		t.Errorf("expected exported PlainMethod receiver, not found in:\n%s", code)
	}
	// Flagged method: unexported (lowerCamel) receiver method — this is the
	// whole point of the flag.
	if !strings.Contains(code, "func (inst RichDemoFluid) hiddenMethod(") {
		t.Errorf("expected UNEXPORTED hiddenMethod receiver, not found in:\n%s", code)
	}
	// And it must NOT emit the exported spelling.
	if strings.Contains(code, "func (inst RichDemoFluid) HiddenMethod(") {
		t.Errorf("flagged method leaked an exported HiddenMethod receiver:\n%s", code)
	}
	// The opcode enum stays UpperCamel regardless of Go method visibility, so
	// the wire format and the (unchanged) client dispatch still agree.
	if !strings.Contains(code, "RichDemoMethodIdHiddenMethod") {
		t.Errorf("expected UpperCamel opcode enum RichDemoMethodIdHiddenMethod, not found in:\n%s", code)
	}
}
