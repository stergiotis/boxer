package idl

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func TestArgumentsBuilder_Empty(t *testing.T) {
	spec := NewArgumentsBuilder().Build()
	if !spec.PlainArguments.IsEmpty() {
		t.Error("Plain arguments should start empty")
	}
	if !spec.EvaluatedArguments.IsEmpty() {
		t.Error("Evaluated arguments should start empty")
	}
}

func TestArgumentsBuilder_PlainArg_AppendsWithDefaultColorKind(t *testing.T) {
	spec := NewArgumentsBuilder().
		PlainArg(naming.StylableName("count"), mustParseType("u32")).
		Build()

	if spec.PlainArguments.Len() != 1 {
		t.Fatalf("PlainArguments.Len(): got %d want 1", spec.PlainArguments.Len())
	}
	if got := spec.PlainArguments.Names[0]; got != "count" {
		t.Errorf("PlainArg name: got %q want %q", got, "count")
	}
	if got := spec.PlainArguments.ColorArgKinds[0]; got != ir.ColorArgKindNone {
		t.Errorf("default ColorArgKind: got %d want %d (None)", got, ir.ColorArgKindNone)
	}
}

func TestArgumentsBuilder_EvaluatedArg_AppendsWithDefaultColorKind(t *testing.T) {
	typ := ir.NewAbstractType("widgetType")
	spec := NewArgumentsBuilder().
		EvaluatedArg(naming.StylableName("widget"), typ).
		Build()

	if spec.EvaluatedArguments.Len() != 1 {
		t.Fatalf("EvaluatedArguments.Len(): got %d want 1", spec.EvaluatedArguments.Len())
	}
	if got := spec.EvaluatedArguments.Names[0]; got != "widget" {
		t.Errorf("EvaluatedArg name: got %q want %q", got, "widget")
	}
	if got := spec.EvaluatedArguments.ColorArgKinds[0]; got != ir.ColorArgKindNone {
		t.Errorf("default ColorArgKind: got %d want None", got)
	}
}

func TestArgumentsBuilder_AsColor_MarksLastPlainArg(t *testing.T) {
	spec := NewArgumentsBuilder().
		PlainArg(naming.StylableName("other"), mustParseType("u32")).
		PlainArg(naming.StylableName("col"), mustParseType("u32")).AsColor().
		Build()

	if spec.PlainArguments.ColorArgKinds[0] != ir.ColorArgKindNone {
		t.Errorf("earlier arg should remain unannotated; got %d", spec.PlainArguments.ColorArgKinds[0])
	}
	if spec.PlainArguments.ColorArgKinds[1] != ir.ColorArgKindScalar {
		t.Errorf("last arg should be Scalar; got %d", spec.PlainArguments.ColorArgKinds[1])
	}
}

func TestArgumentsBuilder_AsColors_MarksLastPlainArg(t *testing.T) {
	spec := NewArgumentsBuilder().
		PlainArg(naming.StylableName("cols"), mustParseType("u32")).AsColors().
		Build()

	if spec.PlainArguments.ColorArgKinds[0] != ir.ColorArgKindSlice {
		t.Errorf("AsColors should mark Slice; got %d", spec.PlainArguments.ColorArgKinds[0])
	}
}

func TestArgumentsBuilder_AsColor_AfterEvaluatedArg(t *testing.T) {
	typ := ir.NewAbstractType("Color32")
	spec := NewArgumentsBuilder().
		EvaluatedArg(naming.StylableName("col"), typ).AsColor().
		Build()

	if spec.EvaluatedArguments.ColorArgKinds[0] != ir.ColorArgKindScalar {
		t.Errorf("AsColor after EvaluatedArg should mark Scalar; got %d", spec.EvaluatedArguments.ColorArgKinds[0])
	}
}

func TestArgumentsBuilder_AsColors_AfterEvaluatedArg_Panics(t *testing.T) {
	typ := ir.NewAbstractType("Color32")
	if expectPanic(func() {
		NewArgumentsBuilder().
			EvaluatedArg(naming.StylableName("col"), typ).
			AsColors()
	}) == nil {
		t.Error("AsColors after EvaluatedArg should panic (SD9: retained is scalar-only)")
	}
}

func TestArgumentsBuilder_AsColor_NoPrecedingArg_Panics(t *testing.T) {
	if expectPanic(func() { NewArgumentsBuilder().AsColor() }) == nil {
		t.Error("AsColor with no preceding arg should panic")
	}
}

func TestArgumentsBuilder_AsColors_NoPrecedingArg_Panics(t *testing.T) {
	if expectPanic(func() { NewArgumentsBuilder().AsColors() }) == nil {
		t.Error("AsColors with no preceding arg should panic")
	}
}

func TestArgumentsBuilder_DoubleAnnotation_Panics(t *testing.T) {
	typ := ir.NewAbstractType("T")
	cases := []struct {
		name string
		run  func(*ArgumentsBuilder)
	}{
		{"plain AsColor twice", func(b *ArgumentsBuilder) {
			b.PlainArg(naming.StylableName("c"), mustParseType("u32")).AsColor().AsColor()
		}},
		{"plain AsColor then AsColors", func(b *ArgumentsBuilder) {
			b.PlainArg(naming.StylableName("c"), mustParseType("u32")).AsColor().AsColors()
		}},
		{"plain AsColors then AsColor", func(b *ArgumentsBuilder) {
			b.PlainArg(naming.StylableName("c"), mustParseType("u32")).AsColors().AsColor()
		}},
		{"evaluated AsColor twice", func(b *ArgumentsBuilder) {
			b.EvaluatedArg(naming.StylableName("c"), typ).AsColor().AsColor()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if expectPanic(func() { tc.run(NewArgumentsBuilder()) }) == nil {
				t.Error("expected panic from double annotation")
			}
		})
	}
}

func TestArgumentsBuilder_ReservedArgNames_Panic(t *testing.T) {
	// Names that match the generator's single-letter-plus-digits regex are
	// reserved for FFFI2's internal Rust variables (r, w, u, d, i, f, m, c).
	cases := []string{"r", "w", "u", "d", "i", "f", "m", "c", "r0", "w10", "a1", "x99", "R", "X1"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if expectPanic(func() {
				NewArgumentsBuilder().PlainArg(naming.StylableName(name), mustParseType("u32"))
			}) == nil {
				t.Errorf("PlainArg(%q) should panic (reserved)", name)
			}
		})
	}
}

func TestArgumentsBuilder_AllowedArgNames(t *testing.T) {
	// Names with two or more letters (no trailing-digit-only suffix) sidestep
	// the reserved regex.
	cases := []string{"wi", "row", "count", "r2x", "abc"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if r := expectPanic(func() {
				NewArgumentsBuilder().PlainArg(naming.StylableName(name), mustParseType("u32"))
			}); r != nil {
				t.Errorf("PlainArg(%q) should not panic; recovered %v", name, r)
			}
		})
	}
}

func TestArgumentsBuilder_EmptyArgName_Panics(t *testing.T) {
	if expectPanic(func() {
		NewArgumentsBuilder().PlainArg(naming.StylableName(""), mustParseType("u32"))
	}) == nil {
		t.Error("PlainArg(\"\") should panic")
	}
}

func TestArgumentsBuilder_DuplicatePlainName_Panics(t *testing.T) {
	if expectPanic(func() {
		NewArgumentsBuilder().
			PlainArg(naming.StylableName("xx"), mustParseType("u32")).
			PlainArg(naming.StylableName("xx"), mustParseType("u64"))
	}) == nil {
		t.Error("duplicate plain-arg name should panic")
	}
}

func TestArgumentsBuilder_EvaluatedArgClashesWithExistingPlain_Panics(t *testing.T) {
	// EvaluatedArg checks against PlainArguments.Names too (cross-spec clash).
	typ := ir.NewAbstractType("T")
	if expectPanic(func() {
		NewArgumentsBuilder().
			PlainArg(naming.StylableName("dup"), mustParseType("u32")).
			EvaluatedArg(naming.StylableName("dup"), typ)
	}) == nil {
		t.Error("EvaluatedArg colliding with prior PlainArg name should panic")
	}
}

func TestArgumentsBuilder_BuildOrderMatchesInsertion(t *testing.T) {
	spec := NewArgumentsBuilder().
		PlainArg(naming.StylableName("first"), mustParseType("u32")).
		PlainArg(naming.StylableName("second"), mustParseType("u64")).
		Build()

	if spec.PlainArguments.Names[0] != "first" || spec.PlainArguments.Names[1] != "second" {
		t.Errorf("insertion order not preserved: got %v", spec.PlainArguments.Names)
	}
}
