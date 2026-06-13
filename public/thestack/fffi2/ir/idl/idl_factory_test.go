package idl

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func TestNewBuilderFactoryNode_ValidName_SetsDefaults(t *testing.T) {
	// Factory-node names are stored verbatim — unlike MethodBuilder names,
	// the constructor does not canonicalise via DefaultNamingStyle.
	node := NewBuilderFactoryNode(naming.StylableName("Button")).Build()
	if node.Name != "Button" {
		t.Errorf("Name: got %q want %q", node.Name, "Button")
	}
	if node.IdentityArguments.HasId {
		t.Error("IdentityArguments.HasId should default to false")
	}
	if node.Settings.Immediate || node.Settings.Retained || node.Settings.BlockIterator {
		t.Errorf("Settings should default to zeroed; got %+v", node.Settings)
	}
	if node.ConstructionCode.CodeClientRust != ir.DefaultCode || node.ConstructionCode.CodeServerGo != ir.DefaultCode {
		t.Error("ConstructionCode should default to DefaultCode in both lanes")
	}
	if node.ApplyCode.CodeClientRust != ir.DefaultCode || node.ApplyCode.CodeServerGo != ir.DefaultCode {
		t.Error("ApplyCode should default to DefaultCode in both lanes")
	}
}

func TestNewBuilderFactoryNode_InvalidName_Panics(t *testing.T) {
	if expectPanic(func() { NewBuilderFactoryNode(naming.StylableName("")) }) == nil {
		t.Error("empty name should panic")
	}
}

func TestBuilderFactoryNode_WithSettingFlags(t *testing.T) {
	node := NewBuilderFactoryNode(naming.StylableName("Window")).
		WithIdentityId(true).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithSettingBlockIterator(true).
		Build()

	if !node.IdentityArguments.HasId {
		t.Error("WithIdentityId(true) did not flip HasId")
	}
	if !node.Settings.Immediate || !node.Settings.Retained || !node.Settings.BlockIterator {
		t.Errorf("setting flags not applied: %+v", node.Settings)
	}
}

func TestBuilderFactoryNode_WithReturnType(t *testing.T) {
	at := ir.NewAbstractType(naming.StylableName("Response"))
	node := NewBuilderFactoryNode(naming.StylableName("Slider")).
		WithReturnType(at).
		Build()
	if node.ReturnType == nil {
		t.Fatal("ReturnType should be set")
	}
	if node.ReturnType.GetName() != "Response" {
		t.Errorf("ReturnType.GetName(): got %q want %q", node.ReturnType.GetName(), "Response")
	}
}

func TestBuilderFactoryNode_CodeSetters(t *testing.T) {
	rc := &ir.StringVerbatimCode{VerbatimCode: "rust-cons"}
	gc := &ir.StringVerbatimCode{VerbatimCode: "go-cons"}
	ra := &ir.StringVerbatimCode{VerbatimCode: "rust-apply"}
	ga := &ir.StringVerbatimCode{VerbatimCode: "go-apply"}

	node := NewBuilderFactoryNode(naming.StylableName("X")).
		WithConstructionCodeClientRust(rc).
		WithConstructionCodeServerGo(gc).
		WithApplyCodeClientRust(ra).
		WithApplyCodeServerGo(ga).
		Build()

	if node.ConstructionCode.CodeClientRust.GetVerbatimCode() != "rust-cons" {
		t.Errorf("ConstructionCode Rust: got %q", node.ConstructionCode.CodeClientRust.GetVerbatimCode())
	}
	if node.ConstructionCode.CodeServerGo.GetVerbatimCode() != "go-cons" {
		t.Errorf("ConstructionCode Go: got %q", node.ConstructionCode.CodeServerGo.GetVerbatimCode())
	}
	if node.ApplyCode.CodeClientRust.GetVerbatimCode() != "rust-apply" {
		t.Errorf("ApplyCode Rust: got %q", node.ApplyCode.CodeClientRust.GetVerbatimCode())
	}
	if node.ApplyCode.CodeServerGo.GetVerbatimCode() != "go-apply" {
		t.Errorf("ApplyCode Go: got %q", node.ApplyCode.CodeServerGo.GetVerbatimCode())
	}
}

func TestBuilderFactoryNode_AddArguments_AccumulatesAcrossCalls(t *testing.T) {
	typ := ir.NewAbstractType(naming.StylableName("T"))

	spec1 := ir.ArgumentSpec{
		PlainArguments: ir.PlainArgumentSpec{
			Names:         []naming.StylableName{"a"},
			Types:         nil, // unused for length-only check
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
		EvaluatedArguments: ir.EvaluatedArgumentSpec{
			Names:         []naming.StylableName{"x"},
			AcceptedTypes: []ir.TypeI{typ},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	spec2 := ir.ArgumentSpec{
		PlainArguments: ir.PlainArgumentSpec{
			Names:         []naming.StylableName{"b"},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	node := NewBuilderFactoryNode(naming.StylableName("N")).
		AddArguments(spec1).
		AddArguments(spec2).
		Build()

	if node.Arguments.PlainArguments.Len() != 2 {
		t.Errorf("PlainArguments after two AddArguments: got %d want 2",
			node.Arguments.PlainArguments.Len())
	}
	if node.Arguments.EvaluatedArguments.Len() != 1 {
		t.Errorf("EvaluatedArguments: got %d want 1", node.Arguments.EvaluatedArguments.Len())
	}
}

func TestBuilderFactoryNode_AddArguments_PlainNameClash_Panics(t *testing.T) {
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
		NewBuilderFactoryNode(naming.StylableName("N")).
			AddArguments(spec1).
			AddArguments(spec2)
	}) == nil {
		t.Error("plain-name clash across AddArguments calls should panic")
	}
}

func TestBuilderFactoryNode_AddArguments_EvaluatedNameClash_Panics(t *testing.T) {
	typ := ir.NewAbstractType(naming.StylableName("T"))
	spec1 := ir.ArgumentSpec{
		EvaluatedArguments: ir.EvaluatedArgumentSpec{
			Names:         []naming.StylableName{"dup"},
			AcceptedTypes: []ir.TypeI{typ},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	spec2 := ir.ArgumentSpec{
		EvaluatedArguments: ir.EvaluatedArgumentSpec{
			Names:         []naming.StylableName{"dup"},
			AcceptedTypes: []ir.TypeI{typ},
			ColorArgKinds: []ir.ColorArgKindE{ir.ColorArgKindNone},
		},
	}
	if expectPanic(func() {
		NewBuilderFactoryNode(naming.StylableName("N")).
			AddArguments(spec1).
			AddArguments(spec2)
	}) == nil {
		t.Error("evaluated-name clash across AddArguments calls should panic")
	}
}

func TestBuilderFactoryNode_AddMethods_AcrossCalls_DetectsClash(t *testing.T) {
	mth := NewMethodBuilder().BeginMethod(naming.StylableName("m")).EndMethod().BuildOne()
	if expectPanic(func() {
		NewBuilderFactoryNode(naming.StylableName("N")).
			AddMethods(mth).
			AddMethods(mth)
	}) == nil {
		t.Error("re-adding the same-named method via a second AddMethods call should panic")
	}
}

func TestBuilderFactoryNode_AddMethods_AppendsInOrder(t *testing.T) {
	mthA := NewMethodBuilder().BeginMethod(naming.StylableName("a")).EndMethod().BuildOne()
	mthB := NewMethodBuilder().BeginMethod(naming.StylableName("b")).EndMethod().BuildOne()
	node := NewBuilderFactoryNode(naming.StylableName("N")).
		AddMethods(mthA).
		AddMethods(mthB).
		Build()
	if len(node.BuilderMethods) != 2 {
		t.Fatalf("BuilderMethods: got %d want 2", len(node.BuilderMethods))
	}
	if node.BuilderMethods[0].Spec.Name != "a" || node.BuilderMethods[1].Spec.Name != "b" {
		t.Errorf("method order: got [%q,%q] want [a,b]",
			node.BuilderMethods[0].Spec.Name, node.BuilderMethods[1].Spec.Name)
	}
}

func TestBuilderFactoryNode_WithDeferredBlockMap_Accumulates(t *testing.T) {
	u32 := mustParseType("u32")
	u64 := mustParseType("u64")
	node := NewBuilderFactoryNode(naming.StylableName("Table")).
		WithDeferredBlockMap("cells", u64, u32).
		WithDeferredBlockMap("headers", u32).
		Build()

	if len(node.DeferredBlockMaps) != 2 {
		t.Fatalf("DeferredBlockMaps: got %d want 2", len(node.DeferredBlockMaps))
	}
	if node.DeferredBlockMaps[0].Name != "cells" || node.DeferredBlockMaps[1].Name != "headers" {
		t.Errorf("block-map names: got [%q,%q] want [cells,headers]",
			node.DeferredBlockMaps[0].Name, node.DeferredBlockMaps[1].Name)
	}
	if len(node.DeferredBlockMaps[0].KeyTypes) != 2 {
		t.Errorf("cells KeyTypes: got %d want 2", len(node.DeferredBlockMaps[0].KeyTypes))
	}
	if len(node.DeferredBlockMaps[1].KeyTypes) != 1 {
		t.Errorf("headers KeyTypes: got %d want 1", len(node.DeferredBlockMaps[1].KeyTypes))
	}
}

func TestBuilderFactoryNode_CompileTimeInterfaceCheck(t *testing.T) {
	// var _ ir.NodeI = (*ir.BuilderFactoryNode)(nil) is asserted in
	// fffi2_ir_types.go at compile time. Verify the runtime GetName.
	var n ir.NodeI = NewBuilderFactoryNode(naming.StylableName("X")).Build()
	if n.GetName() != "X" {
		t.Errorf("GetName via NodeI: got %q want %q", n.GetName(), "X")
	}
}
