package idl

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func TestMethodBuilder_NoMethods_BuildEmpty(t *testing.T) {
	b := NewMethodBuilder()
	if got := b.Build(); len(got) != 0 {
		t.Errorf("Build on empty MethodBuilder: got %d methods want 0", len(got))
	}
}

func TestMethodBuilder_SimpleHappyPath(t *testing.T) {
	mths := NewMethodBuilder().
		BeginMethod(naming.StylableName("setSize")).
		Arg(naming.StylableName("width"), mustParseType("u32")).
		Arg(naming.StylableName("height"), mustParseType("u32")).
		EndMethod().
		Build()

	if len(mths) != 1 {
		t.Fatalf("Build: got %d methods want 1", len(mths))
	}
	// Names are canonicalised via naming.DefaultNamingStyle (LowerSpinalCase).
	if mths[0].Spec.Name != "set-size" {
		t.Errorf("method name (canonicalised): got %q want %q", mths[0].Spec.Name, "set-size")
	}
	if mths[0].Spec.PlainArguments.Len() != 2 {
		t.Errorf("PlainArguments.Len: got %d want 2", mths[0].Spec.PlainArguments.Len())
	}
}

func TestMethodBuilder_BuildOne_HappyPath(t *testing.T) {
	mth := NewMethodBuilder().
		BeginMethod(naming.StylableName("show")).
		EndMethod().
		BuildOne()
	if mth.Spec.Name != "show" {
		t.Errorf("BuildOne: got %q want %q", mth.Spec.Name, "show")
	}
}

func TestMethodBuilder_BuildOne_ZeroMethods_Panics(t *testing.T) {
	if expectPanic(func() { NewMethodBuilder().BuildOne() }) == nil {
		t.Error("BuildOne with 0 methods should panic")
	}
}

func TestMethodBuilder_BuildOne_MultipleMethods_Panics(t *testing.T) {
	b := NewMethodBuilder().
		BeginMethod(naming.StylableName("a")).EndMethod().
		BeginMethod(naming.StylableName("b")).EndMethod()
	if expectPanic(func() { b.BuildOne() }) == nil {
		t.Error("BuildOne with 2 methods should panic")
	}
}

func TestMethodBuilder_BeginMethodTwice_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().
			BeginMethod(naming.StylableName("a")).
			BeginMethod(naming.StylableName("b"))
	}) == nil {
		t.Error("BeginMethod twice without EndMethod should panic")
	}
}

func TestMethodBuilder_EndMethodWithoutBegin_Panics(t *testing.T) {
	if expectPanic(func() { NewMethodBuilder().EndMethod() }) == nil {
		t.Error("EndMethod without BeginMethod should panic")
	}
}

func TestMethodBuilder_BuildInsideMethod_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().BeginMethod(naming.StylableName("a")).Build()
	}) == nil {
		t.Error("Build called inside an open method should panic")
	}
}

func TestMethodBuilder_ArgOutsideMethod_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().Arg(naming.StylableName("x"), mustParseType("u32"))
	}) == nil {
		t.Error("Arg outside BeginMethod should panic")
	}
}

func TestMethodBuilder_EvaluatedArgOutsideMethod_Panics(t *testing.T) {
	typ := ir.NewAbstractType("T")
	if expectPanic(func() {
		NewMethodBuilder().EvaluatedArg(naming.StylableName("x"), typ)
	}) == nil {
		t.Error("EvaluatedArg outside BeginMethod should panic")
	}
}

func TestMethodBuilder_DuplicateMethodName_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().
			BeginMethod(naming.StylableName("dup")).EndMethod().
			BeginMethod(naming.StylableName("dup"))
	}) == nil {
		t.Error("duplicate method name should panic")
	}
}

func TestMethodBuilder_DuplicateArgWithinMethod_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().
			BeginMethod(naming.StylableName("m")).
			Arg(naming.StylableName("dup"), mustParseType("u32")).
			Arg(naming.StylableName("dup"), mustParseType("u64"))
	}) == nil {
		t.Error("duplicate plain arg within a method should panic")
	}
}

func TestMethodBuilder_DuplicateEvaluatedArgWithinMethod_Panics(t *testing.T) {
	typ := ir.NewAbstractType("T")
	if expectPanic(func() {
		NewMethodBuilder().
			BeginMethod(naming.StylableName("m")).
			EvaluatedArg(naming.StylableName("dup"), typ).
			EvaluatedArg(naming.StylableName("dup"), typ)
	}) == nil {
		t.Error("duplicate evaluated arg within a method should panic")
	}
}

func TestMethodBuilder_AsColor_AfterPlainArg(t *testing.T) {
	mth := NewMethodBuilder().
		BeginMethod(naming.StylableName("paint")).
		Arg(naming.StylableName("colour"), mustParseType("u32")).AsColor().
		EndMethod().
		BuildOne()
	if mth.Spec.PlainArguments.ColorArgKinds[0] != ir.ColorArgKindScalar {
		t.Errorf("AsColor: got %d want Scalar", mth.Spec.PlainArguments.ColorArgKinds[0])
	}
}

func TestMethodBuilder_AsColors_AfterPlainArg(t *testing.T) {
	mth := NewMethodBuilder().
		BeginMethod(naming.StylableName("paint")).
		Arg(naming.StylableName("colours"), mustParseType("u32")).AsColors().
		EndMethod().
		BuildOne()
	if mth.Spec.PlainArguments.ColorArgKinds[0] != ir.ColorArgKindSlice {
		t.Errorf("AsColors: got %d want Slice", mth.Spec.PlainArguments.ColorArgKinds[0])
	}
}

func TestMethodBuilder_AsColors_AfterEvaluatedArg_Panics(t *testing.T) {
	typ := ir.NewAbstractType("Color32")
	if expectPanic(func() {
		NewMethodBuilder().
			BeginMethod(naming.StylableName("paint")).
			EvaluatedArg(naming.StylableName("col"), typ).AsColors()
	}) == nil {
		t.Error("AsColors after EvaluatedArg should panic (SD9)")
	}
}

func TestMethodBuilder_AsColor_NoPrecedingArg_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().BeginMethod(naming.StylableName("m")).AsColor()
	}) == nil {
		t.Error("AsColor with no preceding arg in current method should panic")
	}
}

func TestMethodBuilder_AsColors_NoPrecedingArg_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().BeginMethod(naming.StylableName("m")).AsColors()
	}) == nil {
		t.Error("AsColors with no preceding arg should panic")
	}
}

func TestMethodBuilder_DoubleColorAnnotation_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().
			BeginMethod(naming.StylableName("m")).
			Arg(naming.StylableName("c"), mustParseType("u32")).AsColor().AsColor()
	}) == nil {
		t.Error("double AsColor on the same arg should panic")
	}
}

func TestMethodBuilder_AsColorOutsideMethod_Panics(t *testing.T) {
	if expectPanic(func() { NewMethodBuilder().AsColor() }) == nil {
		t.Error("AsColor outside BeginMethod should panic")
	}
}

func TestMethodBuilder_CodeSettersAttachToCurrentMethod(t *testing.T) {
	rust := &ir.StringVerbatimCode{VerbatimCode: "rust body"}
	gos := &ir.StringVerbatimCode{VerbatimCode: "go body"}
	mth := NewMethodBuilder().
		BeginMethod(naming.StylableName("m")).
		CodeClientRust(rust).
		CodeServerGo(gos).
		EndMethod().
		BuildOne()
	if got := mth.CodeHolder.CodeClientRust.GetVerbatimCode(); got != "rust body" {
		t.Errorf("CodeClientRust: got %q want %q", got, "rust body")
	}
	if got := mth.CodeHolder.CodeServerGo.GetVerbatimCode(); got != "go body" {
		t.Errorf("CodeServerGo: got %q want %q", got, "go body")
	}
}

func TestMethodBuilder_CodeClientRustOutsideMethod_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().CodeClientRust(&ir.StringVerbatimCode{})
	}) == nil {
		t.Error("CodeClientRust outside an open method should panic")
	}
}

func TestMethodBuilder_InvalidMethodName_Panics(t *testing.T) {
	if expectPanic(func() {
		NewMethodBuilder().BeginMethod(naming.StylableName(""))
	}) == nil {
		t.Error("BeginMethod(\"\") should panic")
	}
}

func TestMethodBuilder_Merge_RoundTripsArgsAndColors(t *testing.T) {
	typ := ir.NewAbstractType("Color32")
	// Build a method that exercises all four arg/color combinations.
	src := NewMethodBuilder().
		BeginMethod(naming.StylableName("paint")).
		Arg(naming.StylableName("col"), mustParseType("u32")).AsColor().
		Arg(naming.StylableName("colors"), mustParseType("u32")).AsColors().
		Arg(naming.StylableName("count"), mustParseType("u64")).
		EvaluatedArg(naming.StylableName("fill"), typ).AsColor().
		EvaluatedArg(naming.StylableName("widget"), typ).
		CodeClientRust(&ir.StringVerbatimCode{VerbatimCode: "rust"}).
		CodeServerGo(&ir.StringVerbatimCode{VerbatimCode: "go"}).
		EndMethod().
		BuildOne()

	merged := NewMethodBuilder().Merge(src).BuildOne()

	if merged.Spec.Name != src.Spec.Name {
		t.Errorf("merged name: got %q want %q", merged.Spec.Name, src.Spec.Name)
	}
	if got, want := merged.Spec.PlainArguments.ColorArgKinds, src.Spec.PlainArguments.ColorArgKinds; len(got) != len(want) {
		t.Fatalf("plain color-kind lens differ: got %v want %v", got, want)
	}
	for i := range src.Spec.PlainArguments.ColorArgKinds {
		if merged.Spec.PlainArguments.ColorArgKinds[i] != src.Spec.PlainArguments.ColorArgKinds[i] {
			t.Errorf("plain color-kind[%d] differs: got %d want %d",
				i, merged.Spec.PlainArguments.ColorArgKinds[i], src.Spec.PlainArguments.ColorArgKinds[i])
		}
	}
	for i := range src.Spec.EvaluatedArguments.ColorArgKinds {
		if merged.Spec.EvaluatedArguments.ColorArgKinds[i] != src.Spec.EvaluatedArguments.ColorArgKinds[i] {
			t.Errorf("evaluated color-kind[%d] differs: got %d want %d",
				i, merged.Spec.EvaluatedArguments.ColorArgKinds[i], src.Spec.EvaluatedArguments.ColorArgKinds[i])
		}
	}
	if got := merged.CodeHolder.CodeClientRust.GetVerbatimCode(); got != "rust" {
		t.Errorf("merged CodeClientRust: got %q want %q", got, "rust")
	}
}

func TestMethodBuilder_StateMachine_TwoSequentialMethods(t *testing.T) {
	mths := NewMethodBuilder().
		BeginMethod(naming.StylableName("first")).EndMethod().
		BeginMethod(naming.StylableName("second")).EndMethod().
		Build()
	if len(mths) != 2 {
		t.Fatalf("Build: got %d methods want 2", len(mths))
	}
	if mths[0].Spec.Name != "first" || mths[1].Spec.Name != "second" {
		t.Errorf("method order: got [%q,%q] want [first,second]",
			mths[0].Spec.Name, mths[1].Spec.Name)
	}
}
