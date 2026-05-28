//go:build llm_generated_opus47

package idl

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func TestNewProceduralNode_ValidName_SetsDefaults(t *testing.T) {
	node := NewProceduralNode(naming.StylableName("openWindow")).Build()
	if node.Name != "openWindow" {
		t.Errorf("Name (stored verbatim): got %q want %q", node.Name, "openWindow")
	}
	if node.IdentityArguments.HasId || node.IdentityArguments.IsReference {
		t.Errorf("IdentityArguments default: got %+v", node.IdentityArguments)
	}
	if node.Settings.BlockIterator {
		t.Error("Settings.BlockIterator default: got true want false")
	}
	if node.ApplyCode.CodeClientRust != ir.DefaultCode {
		t.Error("ApplyCode.CodeClientRust default: should be DefaultCode")
	}
	if node.ApplyCode.CodeServerGo != ir.DefaultCode {
		t.Error("ApplyCode.CodeServerGo default: should be DefaultCode")
	}
}

func TestNewProceduralNode_InvalidName_Panics(t *testing.T) {
	if expectPanic(func() { NewProceduralNode(naming.StylableName("")) }) == nil {
		t.Error("empty name should panic")
	}
}

func TestProceduralNode_WithIdentityId(t *testing.T) {
	node := NewProceduralNode(naming.StylableName("op")).
		WithIdentityId(true).
		Build()
	if !node.IdentityArguments.HasId {
		t.Error("WithIdentityId(true) did not flip HasId")
	}
	if node.IdentityArguments.IsReference {
		t.Error("WithIdentityId should not flip IsReference (use WithIdentityIdReference for that)")
	}
}

func TestProceduralNode_WithIdentityIdReference_SetsBothFields(t *testing.T) {
	node := NewProceduralNode(naming.StylableName("collapseWindow")).
		WithIdentityIdReference().
		Build()
	if !node.IdentityArguments.HasId {
		t.Error("WithIdentityIdReference should set HasId=true")
	}
	if !node.IdentityArguments.IsReference {
		t.Error("WithIdentityIdReference should set IsReference=true")
	}
}

func TestProceduralNode_WithSettingBlockIterator(t *testing.T) {
	node := NewProceduralNode(naming.StylableName("loop")).
		WithSettingBlockIterator(true).
		Build()
	if !node.Settings.BlockIterator {
		t.Error("WithSettingBlockIterator(true) did not flip the flag")
	}
}

func TestProceduralNode_WithReturnType(t *testing.T) {
	at := ir.NewAbstractType(naming.StylableName("Resp"))
	node := NewProceduralNode(naming.StylableName("op")).
		WithReturnType(at).
		Build()
	if node.ReturnType == nil || node.ReturnType.GetName() != "Resp" {
		t.Errorf("ReturnType: got %+v", node.ReturnType)
	}
}

func TestProceduralNode_CodeSetters(t *testing.T) {
	rust := &ir.StringVerbatimCode{VerbatimCode: "rust apply"}
	gos := &ir.StringVerbatimCode{VerbatimCode: "go apply"}
	node := NewProceduralNode(naming.StylableName("op")).
		WithApplyCodeClientRust(rust).
		WithApplyCodeServerGo(gos).
		Build()
	if got := node.ApplyCode.CodeClientRust.GetVerbatimCode(); got != "rust apply" {
		t.Errorf("ApplyCode rust: got %q", got)
	}
	if got := node.ApplyCode.CodeServerGo.GetVerbatimCode(); got != "go apply" {
		t.Errorf("ApplyCode go: got %q", got)
	}
}

func TestProceduralNode_AddArguments_PlainNameClash_Panics(t *testing.T) {
	spec1 := ir.ArgumentSpec{
		PlainArguments: ir.PlainArgumentSpec{
			Names:         []naming.StylableName{"dup"},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	spec2 := ir.ArgumentSpec{
		PlainArguments: ir.PlainArgumentSpec{
			Names:         []naming.StylableName{"dup"},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	if expectPanic(func() {
		NewProceduralNode(naming.StylableName("op")).
			AddArguments(spec1).
			AddArguments(spec2)
	}) == nil {
		t.Error("plain-name clash across AddArguments calls should panic")
	}
}

func TestProceduralNode_AddArguments_AccumulatesPlainAndEvaluated(t *testing.T) {
	typ := ir.NewAbstractType(naming.StylableName("T"))
	spec := ir.ArgumentSpec{
		PlainArguments: ir.PlainArgumentSpec{
			Names:         []naming.StylableName{"a", "b"},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone, ir.ColorArgKindScalar},
		},
		EvaluatedArguments: ir.EvaluatedArgumentSpec{
			Names:         []naming.StylableName{"x"},
			AcceptedTypes: []ir.TypeI{typ},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	node := NewProceduralNode(naming.StylableName("op")).AddArguments(spec).Build()

	if node.Arguments.PlainArguments.Len() != 2 {
		t.Errorf("plain args: got %d want 2", node.Arguments.PlainArguments.Len())
	}
	if node.Arguments.EvaluatedArguments.Len() != 1 {
		t.Errorf("evaluated args: got %d want 1", node.Arguments.EvaluatedArguments.Len())
	}
	if node.Arguments.PlainArguments.ColorArgKinds[1] != ir.ColorArgKindScalar {
		t.Error("color-arg-kinds were not carried through")
	}
}

func TestProceduralNode_AsNodeI(t *testing.T) {
	var n ir.NodeI = NewProceduralNode(naming.StylableName("op")).Build()
	if n.GetName() != "op" {
		t.Errorf("GetName via NodeI: got %q want %q", n.GetName(), "op")
	}
}
