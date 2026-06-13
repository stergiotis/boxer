package idl

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func TestNewFetcherNode_ValidName(t *testing.T) {
	node := NewFetcherNode(naming.StylableName("Fetch")).Build()
	if node.Name != "Fetch" {
		t.Errorf("Name (stored verbatim): got %q want %q", node.Name, "Fetch")
	}
	if node.ReturnTypes.Len() != 0 {
		t.Errorf("ReturnTypes default len: got %d want 0", node.ReturnTypes.Len())
	}
}

func TestNewFetcherNode_InvalidName_Panics(t *testing.T) {
	if expectPanic(func() { NewFetcherNode(naming.StylableName("")) }) == nil {
		t.Error("empty name should panic")
	}
}

func TestFetcherNode_WithApplyCodes(t *testing.T) {
	rust := &ir.StringVerbatimCode{VerbatimCode: "rust"}
	gos := &ir.StringVerbatimCode{VerbatimCode: "go"}
	node := NewFetcherNode(naming.StylableName("Fetch")).
		WithApplyCodeClientRust(rust).
		WithApplyCodeServerGo(gos).
		Build()
	if got := node.ApplyCode.CodeClientRust.GetVerbatimCode(); got != "rust" {
		t.Errorf("CodeClientRust: got %q", got)
	}
	if got := node.ApplyCode.CodeServerGo.GetVerbatimCode(); got != "go" {
		t.Errorf("CodeServerGo: got %q", got)
	}
}

func TestFetcherNode_AddReturnValue_Appends(t *testing.T) {
	node := NewFetcherNode(naming.StylableName("Fetch")).
		AddReturnValue(naming.StylableName("count"), mustParseType("u32")).
		AddReturnValue(naming.StylableName("label"), mustParseType("s")).
		Build()
	if node.ReturnTypes.Len() != 2 {
		t.Fatalf("ReturnTypes.Len: got %d want 2", node.ReturnTypes.Len())
	}
	if node.ReturnTypes.Names[0] != "count" || node.ReturnTypes.Names[1] != "label" {
		t.Errorf("Names: got %v want [count label]", node.ReturnTypes.Names)
	}
}

func TestFetcherNode_AddReturnValue_InvalidName_Panics(t *testing.T) {
	if expectPanic(func() {
		NewFetcherNode(naming.StylableName("F")).
			AddReturnValue(naming.StylableName(""), mustParseType("u32"))
	}) == nil {
		t.Error("AddReturnValue with empty name should panic")
	}
}

func TestFetcherNode_AddReturnValue_DuplicateName_Panics(t *testing.T) {
	if expectPanic(func() {
		NewFetcherNode(naming.StylableName("F")).
			AddReturnValue(naming.StylableName("dup"), mustParseType("u32")).
			AddReturnValue(naming.StylableName("dup"), mustParseType("u64"))
	}) == nil {
		t.Error("AddReturnValue with duplicate name should panic")
	}
}

func TestFetcherNode_AsNodeI(t *testing.T) {
	var n ir.NodeI = NewFetcherNode(naming.StylableName("Fetch")).Build()
	if n.GetName() != "Fetch" {
		t.Errorf("GetName via NodeI: got %q want %q", n.GetName(), "Fetch")
	}
}

func TestCheckNameClashesPedantic_DetectsCrossSliceClash(t *testing.T) {
	// White-box: exercise the unexported helper directly.
	if expectPanic(func() {
		checkNameClashesPedantic(
			[]naming.StylableName{"foo", "bar"},
			[]naming.StylableName{"baz", "foo"},
		)
	}) == nil {
		t.Error("checkNameClashesPedantic should panic on a shared name")
	}
}

func TestCheckNameClashesPedantic_NoClashIsSilent(t *testing.T) {
	// No panic expected.
	checkNameClashesPedantic(
		[]naming.StylableName{"a", "b"},
		[]naming.StylableName{"c", "d"},
	)
}

func TestCheckNameClashesPedantic_EmptySlicesAreSilent(t *testing.T) {
	checkNameClashesPedantic(nil, nil)
	checkNameClashesPedantic([]naming.StylableName{"x"}, nil)
	checkNameClashesPedantic(nil, []naming.StylableName{"x"})
}
