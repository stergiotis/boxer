//go:build llm_generated_opus47

package ir

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func TestEmptyCodeS_Singleton(t *testing.T) {
	if EmptyCode == nil {
		t.Fatal("EmptyCode is nil; expected non-nil package singleton")
	}
	if EmptyCode.UseDefaultCode() {
		t.Error("EmptyCode.UseDefaultCode(): got true want false")
	}
	if got := EmptyCode.GetVerbatimCode(); got != "" {
		t.Errorf("EmptyCode.GetVerbatimCode(): got %q want \"\"", got)
	}
}

func TestDefaultCodeS_Singleton(t *testing.T) {
	if DefaultCode == nil {
		t.Fatal("DefaultCode is nil")
	}
	if !DefaultCode.UseDefaultCode() {
		t.Error("DefaultCode.UseDefaultCode(): got false want true")
	}
	if got := DefaultCode.GetVerbatimCode(); got != "" {
		t.Errorf("DefaultCode.GetVerbatimCode(): got %q want \"\"", got)
	}
}

func TestStringVerbatimCode(t *testing.T) {
	cases := []struct {
		name      string
		def       bool
		body      string
		wantUse   bool
		wantBody  string
	}{
		{"default flag true, empty body", true, "", true, ""},
		{"default flag false, with body", false, "fn foo() {}", false, "fn foo() {}"},
		{"default flag true, with body (atypical)", true, "// hand-overridden", true, "// hand-overridden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vc := &StringVerbatimCode{Default: tc.def, VerbatimCode: tc.body}
			if got := vc.UseDefaultCode(); got != tc.wantUse {
				t.Errorf("UseDefaultCode(): got %v want %v", got, tc.wantUse)
			}
			if got := vc.GetVerbatimCode(); got != tc.wantBody {
				t.Errorf("GetVerbatimCode(): got %q want %q", got, tc.wantBody)
			}
		})
	}
}

func TestMergeVerbatimCode_SingleInput_PassesThrough(t *testing.T) {
	in := &StringVerbatimCode{Default: false, VerbatimCode: "hello"}
	out := MergeVerbatimCode(in)
	if out != in {
		t.Errorf("single-input merge should return the same VerbatimCodeI; got different instance")
	}
}

func TestMergeVerbatimCode_TwoInputs_JoinedWithNewline(t *testing.T) {
	a := &StringVerbatimCode{VerbatimCode: "line1"}
	b := &StringVerbatimCode{VerbatimCode: "line2"}
	out := MergeVerbatimCode(a, b)
	got := out.GetVerbatimCode()
	want := "line1\nline2"
	if got != want {
		t.Errorf("merge: got %q want %q", got, want)
	}
}

func TestMergeVerbatimCode_NoExtraNewlineWhenFirstAlreadyEndsInNewline(t *testing.T) {
	a := &StringVerbatimCode{VerbatimCode: "line1\n"}
	b := &StringVerbatimCode{VerbatimCode: "line2"}
	out := MergeVerbatimCode(a, b)
	got := out.GetVerbatimCode()
	// Should not produce "line1\n\nline2"
	if strings.Contains(got, "\n\n") {
		t.Errorf("merge inserted a redundant newline: %q", got)
	}
	if got != "line1\nline2" {
		t.Errorf("merge: got %q want %q", got, "line1\nline2")
	}
}

func TestMergeVerbatimCode_DefaultFlagIsLogicalAnd(t *testing.T) {
	allDefault := MergeVerbatimCode(
		&StringVerbatimCode{Default: true, VerbatimCode: "a"},
		&StringVerbatimCode{Default: true, VerbatimCode: "b"},
	)
	if !allDefault.UseDefaultCode() {
		t.Error("merge of two default chunks should remain default")
	}

	mixed := MergeVerbatimCode(
		&StringVerbatimCode{Default: true, VerbatimCode: "a"},
		&StringVerbatimCode{Default: false, VerbatimCode: "b"},
	)
	if mixed.UseDefaultCode() {
		t.Error("merge with any non-default chunk should be non-default")
	}
}

func TestMergeVerbatimCode_ThreeInputs(t *testing.T) {
	out := MergeVerbatimCode(
		&StringVerbatimCode{VerbatimCode: "a"},
		&StringVerbatimCode{VerbatimCode: "b"},
		&StringVerbatimCode{VerbatimCode: "c\n"},
	)
	if got := out.GetVerbatimCode(); got != "a\nb\nc\n" {
		t.Errorf("merge three: got %q want %q", got, "a\nb\nc\n")
	}
}

func TestMergeVerbatimCode_ZeroInputs_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MergeVerbatimCode() with no args should panic")
		}
	}()
	_ = MergeVerbatimCode()
}

func TestStackCapture_DepthZero_NoCapture(t *testing.T) {
	sc := NewStackCapture(0, 0)
	if sc == nil {
		t.Fatal("NewStackCapture returned nil")
	}
	if len(sc.Files) != 0 || len(sc.Lines) != 0 || len(sc.Funcs) != 0 {
		t.Errorf("depth=0 should leave fields nil/empty; got %d files, %d lines, %d funcs",
			len(sc.Files), len(sc.Lines), len(sc.Funcs))
	}
}

func TestStackCapture_DepthPositive_CapturesFrames(t *testing.T) {
	sc := NewStackCapture(0, 4)
	if sc == nil {
		t.Fatal("NewStackCapture returned nil")
	}
	if len(sc.Files) == 0 {
		// capture() walks until it finds fmt.Fprintf/Fprint, then slices from
		// there. When called from a test (no fmt.Fprint* on the stack), t==0
		// so files[0:min(len, 0+depth)] keeps the first `depth` frames.
		t.Errorf("depth=4 should capture frames; got 0")
	}
	if len(sc.Files) != len(sc.Lines) || len(sc.Files) != len(sc.Funcs) {
		t.Errorf("parallel slices must stay aligned: files=%d lines=%d funcs=%d",
			len(sc.Files), len(sc.Lines), len(sc.Funcs))
	}
	foundThisTest := false
	for _, fn := range sc.Funcs {
		if strings.Contains(fn, "TestStackCapture_DepthPositive_CapturesFrames") {
			foundThisTest = true
			break
		}
	}
	if !foundThisTest {
		t.Errorf("expected this test's function name in captured funcs; got %v", sc.Funcs)
	}
}

func TestCodeLocationBufferWriter_WriteAndString(t *testing.T) {
	w := NewCodeLocationBufferWriter(nil)
	n, err := w.Write([]byte("hello "))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 6 {
		t.Errorf("Write returned %d, want 6", n)
	}
	n, err = w.WriteString("world")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if n != 5 {
		t.Errorf("WriteString returned %d, want 5", n)
	}
	if got := w.String(); got != "hello world" {
		t.Errorf("String(): got %q want %q", got, "hello world")
	}
	if got := w.GetVerbatimCode(); got != "hello world" {
		t.Errorf("GetVerbatimCode(): got %q want %q", got, "hello world")
	}
	if w.UseDefaultCode() {
		t.Error("CodeLocationBufferWriter.UseDefaultCode(): got true want false")
	}
}

func TestCodeLocationBufferWriter_StackCapturedOnFirstWrite(t *testing.T) {
	w := NewCodeLocationBufferWriter(nil)
	if w.GetStack() != nil {
		t.Fatal("stack should be nil before any Write")
	}
	_, _ = w.Write([]byte("x"))
	stackAfterFirst := w.GetStack()
	if stackAfterFirst == nil {
		t.Fatal("stack should be captured on first Write")
	}
	_, _ = w.Write([]byte("y"))
	stackAfterSecond := w.GetStack()
	if stackAfterFirst != stackAfterSecond {
		t.Error("subsequent Writes should not re-capture the stack")
	}
}

func TestCodeLocationBufferWriter_Reset_KeepsStack(t *testing.T) {
	w := NewCodeLocationBufferWriter(nil)
	_, _ = w.WriteString("payload")
	stackBefore := w.GetStack()
	w.Reset()
	if got := w.String(); got != "" {
		t.Errorf("Reset should clear buffer; got %q", got)
	}
	if w.GetStack() != stackBefore {
		t.Error("Reset should not clear the captured stack")
	}
}

func TestCodeLocationBufferWriter_OverrideCodeLocation(t *testing.T) {
	w := NewCodeLocationBufferWriter(nil)
	fake := NewStackCapture(0, 0)
	w.OverrideCodeLocation(fake)
	_, _ = w.Write([]byte("x"))
	if w.GetStack() != fake {
		t.Error("Write should not overwrite an explicit OverrideCodeLocation")
	}
}

func TestCodeLocationBufferWriter_InitialBufferPreserved(t *testing.T) {
	initial := []byte("preset ")
	w := NewCodeLocationBufferWriter(initial)
	_, _ = w.WriteString("append")
	if got := w.String(); got != "preset append" {
		t.Errorf("initial buf preserved: got %q want %q", got, "preset append")
	}
}

func TestNewAbstractType_RoundTrip(t *testing.T) {
	name := naming.StylableName("myType")
	at := NewAbstractType(name)
	if !at.IsAbstract() {
		t.Error("AbstractType.IsAbstract(): got false want true")
	}
	if got := at.GetName(); got != name {
		t.Errorf("GetName(): got %q want %q", got, name)
	}
	// ImplementedAbstractTypes yields self exactly once.
	count := 0
	for v := range at.ImplementedAbstractTypes() {
		count++
		if v.GetName() != name {
			t.Errorf("yielded AbstractType name: got %q want %q", v.GetName(), name)
		}
	}
	if count != 1 {
		t.Errorf("AbstractType.ImplementedAbstractTypes() yielded %d times, want 1", count)
	}
}

func TestNewConcreteType_RoundTrip(t *testing.T) {
	a1 := NewAbstractType(naming.StylableName("iface1"))
	a2 := NewAbstractType(naming.StylableName("iface2"))
	ct := NewConcreteType(naming.StylableName("concrete"), a1, a2)
	if ct.IsAbstract() {
		t.Error("ConcreteType.IsAbstract(): got true want false")
	}
	if got := ct.GetName(); got != "concrete" {
		t.Errorf("GetName(): got %q want %q", got, "concrete")
	}
	gotNames := []string{}
	for v := range ct.ImplementedAbstractTypes() {
		gotNames = append(gotNames, string(v.GetName()))
	}
	if len(gotNames) != 2 || gotNames[0] != "iface1" || gotNames[1] != "iface2" {
		t.Errorf("ImplementedAbstractTypes order: got %v want [iface1 iface2]", gotNames)
	}
}

func TestNewAbstractType_InvalidName_PanicsUnderExtraChecks(t *testing.T) {
	if !compiletimeflags.ExtraChecks {
		t.Skip("name validation only panics under the extrachecks build tag")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewAbstractType(\"\") should panic under extraChecks=true")
		}
	}()
	_ = NewAbstractType(naming.StylableName(""))
}

func TestNewConcreteType_InvalidName_PanicsUnderExtraChecks(t *testing.T) {
	if !compiletimeflags.ExtraChecks {
		t.Skip("name validation only panics under the extrachecks build tag")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewConcreteType(\"\") should panic under extraChecks=true")
		}
	}()
	_ = NewConcreteType(naming.StylableName(""))
}

func TestEvaluatedArgumentSpec_LenAndIsEmpty(t *testing.T) {
	empty := EvaluatedArgumentSpec{}
	if empty.Len() != 0 {
		t.Errorf("empty.Len(): got %d want 0", empty.Len())
	}
	if !empty.IsEmpty() {
		t.Error("empty.IsEmpty(): got false want true")
	}

	a := NewAbstractType(naming.StylableName("t1"))
	populated := EvaluatedArgumentSpec{
		Names:         []naming.StylableName{"x", "y"},
		AcceptedTypes: []TypeI{a, a},
	}
	if populated.Len() != 2 {
		t.Errorf("populated.Len(): got %d want 2", populated.Len())
	}
	if populated.IsEmpty() {
		t.Error("populated.IsEmpty(): got true want false")
	}
}

func TestEvaluatedArgumentSpec_Iterate_OrderMatchesNames(t *testing.T) {
	a1 := NewAbstractType(naming.StylableName("ta"))
	a2 := NewAbstractType(naming.StylableName("tb"))
	spec := EvaluatedArgumentSpec{
		Names:         []naming.StylableName{"first", "second"},
		AcceptedTypes: []TypeI{a1, a2},
	}
	type pair struct {
		name string
		typ  string
	}
	var got []pair
	for n, ty := range spec.Iterate() {
		got = append(got, pair{name: string(n), typ: string(ty.GetName())})
	}
	want := []pair{{"first", "ta"}, {"second", "tb"}}
	if len(got) != len(want) {
		t.Fatalf("Iterate yielded %d pairs, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Iterate[%d]: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestEvaluatedArgumentSpec_Iterate_RespectsEarlyBreak(t *testing.T) {
	a := NewAbstractType(naming.StylableName("t"))
	spec := EvaluatedArgumentSpec{
		Names:         []naming.StylableName{"a", "b", "c"},
		AcceptedTypes: []TypeI{a, a, a},
	}
	count := 0
	for range spec.Iterate() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("early break after 2 should yield count=2; got %d", count)
	}
}

func TestPlainArgumentSpec_LenAndIsEmpty(t *testing.T) {
	empty := PlainArgumentSpec{}
	if !empty.IsEmpty() {
		t.Error("empty.IsEmpty(): got false want true")
	}
	if empty.Len() != 0 {
		t.Errorf("empty.Len(): got %d want 0", empty.Len())
	}

	populated := PlainArgumentSpec{
		Names: []naming.StylableName{"a", "b", "c"},
	}
	if populated.Len() != 3 {
		t.Errorf("populated.Len(): got %d want 3", populated.Len())
	}
	if populated.IsEmpty() {
		t.Error("populated.IsEmpty(): got true want false")
	}
}

func TestNodeI_GetName_AcrossAllNodeKinds(t *testing.T) {
	bf := &BuilderFactoryNode{Name: "builder"}
	pn := &ProceduralNode{Name: "procedure"}
	fn := &FetcherNode{Name: "fetcher"}
	cases := []struct {
		name string
		got  naming.StylableName
		want naming.StylableName
	}{
		{"BuilderFactoryNode", bf.GetName(), "builder"},
		{"ProceduralNode", pn.GetName(), "procedure"},
		{"FetcherNode", fn.GetName(), "fetcher"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("GetName(): got %q want %q", tc.got, tc.want)
			}
		})
	}
}

func TestLangE_Constants(t *testing.T) {
	// Stability check — these are part of the codegen contract.
	if LangGo != "go" {
		t.Errorf("LangGo: got %q want %q", LangGo, "go")
	}
	if LangRust != "rust" {
		t.Errorf("LangRust: got %q want %q", LangRust, "rust")
	}
}

func TestColorArgKindE_ZeroValueIsNone(t *testing.T) {
	// ADR-0003: zero value preserves pre-ADR-0003 behavior.
	var k ColorArgKindE
	if k != ColorArgKindNone {
		t.Errorf("zero value: got %d want ColorArgKindNone (0)", k)
	}
	if ColorArgKindScalar != 1 {
		t.Errorf("ColorArgKindScalar: got %d want 1", ColorArgKindScalar)
	}
	if ColorArgKindSlice != 2 {
		t.Errorf("ColorArgKindSlice: got %d want 2", ColorArgKindSlice)
	}
}
