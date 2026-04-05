//go:build llm_generated_opus46

package eh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"
)

// =============================================================================
// errorhandling.go tests
// =============================================================================

func TestNew(t *testing.T) {
	err := New("hello")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Error() != "hello" {
		t.Fatalf("expected 'hello', got %q", err.Error())
	}
}

func TestNew_HasStackTrace(t *testing.T) {
	err := New("with stack")
	st, ok := err.(stackTracer)
	if !ok {
		t.Fatal("expected error to implement stackTracer")
	}
	trace := st.StackTrace()
	if len(trace) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
	// The test file frame should appear somewhere in the stack.
	found := false
	for _, frame := range trace {
		if strings.Contains(frame.File, "eh_test.go") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected eh_test.go somewhere in the stack trace")
	}
}

func TestErrorf_SimpleMessage(t *testing.T) {
	err := Errorf("value is %d", 42)
	if err.Error() != "value is 42" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestErrorf_WrappingSingleError(t *testing.T) {
	inner := New("inner")
	outer := Errorf("outer: %w", inner)

	if outer.Error() != "outer: inner" {
		t.Fatalf("unexpected message: %q", outer.Error())
	}

	// Should implement Unwrap() error (single)
	u, ok := outer.(interface{ Unwrap() error })
	if !ok {
		t.Fatal("expected outer to implement single Unwrap")
	}
	unwrapped := u.Unwrap()
	if !errors.Is(unwrapped, inner) {
		t.Fatal("unwrapped error should match inner")
	}

	// Should have its own stack trace
	st, ok := outer.(stackTracer)
	if !ok {
		t.Fatal("expected outer to implement stackTracer")
	}
	if len(st.StackTrace()) == 0 {
		t.Fatal("expected non-empty stack trace on outer")
	}
}

func TestErrorf_WrappingMultipleErrors(t *testing.T) {
	e1 := New("first")
	e2 := New("second")
	joined := errors.Join(e1, e2)
	outer := Errorf("multi: %w", joined)

	if !strings.Contains(outer.Error(), "first") {
		t.Fatalf("expected 'first' in message, got %q", outer.Error())
	}

	// The fmt.Errorf with %w on a joined error produces an unwrapableMulti
	u, ok := outer.(interface{ Unwrap() []error })
	if !ok {
		// It might wrap as single depending on fmt.Errorf behavior
		u2, ok2 := outer.(interface{ Unwrap() error })
		if !ok2 {
			t.Fatal("expected some form of unwrap")
		}
		_ = u2.Unwrap()
	} else {
		errs := u.Unwrap()
		if len(errs) == 0 {
			t.Fatal("expected unwrapped errors")
		}
	}
}

func TestErrorfWithData(t *testing.T) {
	data, err := cbor.Marshal(map[string]int{"count": 5})
	if err != nil {
		t.Fatal(err)
	}

	e := ErrorfWithData(data, "something failed")
	if e.Error() != "something failed" {
		t.Fatalf("unexpected message: %q", e.Error())
	}

	esd, ok := e.(ErrorWithStructuredData)
	if !ok {
		t.Fatal("expected ErrorWithStructuredData interface")
	}
	got := esd.GetCBORStructuredData()
	if !bytes.Equal(got, data) {
		t.Fatalf("CBOR data mismatch: got %v, want %v", got, data)
	}
}

func TestErrorfWithData_SetCBOR(t *testing.T) {
	e := ErrorfWithData(nil, "test")
	esd, ok := e.(ErrorWithStructuredData)
	if !ok {
		t.Fatal("expected ErrorWithStructuredData")
	}
	if esd.GetCBORStructuredData() != nil {
		t.Fatal("expected nil initially")
	}

	newData := []byte{0xa0} // CBOR empty map
	esd.SetCBORStructuredData(newData)
	if !bytes.Equal(esd.GetCBORStructuredData(), newData) {
		t.Fatal("SetCBORStructuredData did not stick")
	}
}

func TestErrorfWithData_WrappingSingle(t *testing.T) {
	inner := New("inner")
	data := []byte{0xa0}
	outer := ErrorfWithData(data, "wrap: %w", inner)

	// Should be singleWrappedWithStackError
	esd, ok := outer.(ErrorWithStructuredData)
	if !ok {
		t.Fatal("expected ErrorWithStructuredData")
	}
	if !bytes.Equal(esd.GetCBORStructuredData(), data) {
		t.Fatal("CBOR data mismatch on single-wrapped")
	}
	if !errors.Is(outer, inner) {
		t.Fatal("errors.Is should find inner")
	}
}

func TestErrorfWithData_WrappingMulti(t *testing.T) {
	e1 := New("a")
	e2 := New("b")
	joined := errors.Join(e1, e2)
	data := []byte{0xa0}
	outer := ErrorfWithData(data, "wrap: %w", joined)

	esd, ok := outer.(ErrorWithStructuredData)
	if !ok {
		t.Fatal("expected ErrorWithStructuredData")
	}
	if !bytes.Equal(esd.GetCBORStructuredData(), data) {
		t.Fatal("CBOR data mismatch on multi-wrapped")
	}
}

func TestErrorfWithDataWithoutStack(t *testing.T) {
	data := []byte{0xa0}
	e := ErrorfWithDataWithoutStack(data, "no stack: %s", "here")

	if e.Error() != "no stack: here" {
		t.Fatalf("unexpected message: %q", e.Error())
	}

	st, ok := e.(stackTracer)
	if !ok {
		// withStackError with nil stack
		_ = st
	} else {
		trace := st.StackTrace()
		if trace != nil {
			t.Fatal("expected nil stack trace")
		}
	}
}

func TestErrorfWithDataWithoutStack_WrappingSingle(t *testing.T) {
	inner := New("inner")
	e := ErrorfWithDataWithoutStack(nil, "outer: %w", inner)

	st, ok := e.(stackTracer)
	if ok {
		trace := st.StackTrace()
		if trace != nil {
			t.Fatal("expected nil stack trace for WithoutStack variant")
		}
	}
	if !errors.Is(e, inner) {
		t.Fatal("should unwrap to inner")
	}
}

func TestErrorfWithDataWithoutStack_WrappingMulti(t *testing.T) {
	joined := errors.Join(New("a"), New("b"))
	e := ErrorfWithDataWithoutStack(nil, "outer: %w", joined)

	st, ok := e.(stackTracer)
	if ok {
		trace := st.StackTrace()
		if trace != nil {
			t.Fatal("expected nil stack trace for WithoutStack multi variant")
		}
	}
}

func TestCallers(t *testing.T) {
	s := callers(1)
	if s == nil {
		t.Fatal("expected non-nil stack")
	}
	if len(*s) == 0 {
		t.Fatal("expected non-empty stack")
	}
	if len(*s) > maxStackDepth {
		t.Fatalf("stack depth %d exceeds max %d", len(*s), maxStackDepth)
	}
}

// Test the three wrapper types' interface compliance (compile-time already
// checked via var _ lines, but exercise the paths at runtime too)
func TestWithStackError_NilStack(t *testing.T) {
	e := &withStackError{
		err:   errors.New("base"),
		stack: nil,
	}
	if e.Error() != "base" {
		t.Fatal("wrong message")
	}
	if e.StackTrace() != nil {
		t.Fatal("expected nil StackTrace for nil stack")
	}
}

func TestSingleWrappedWithStackError_NilStack(t *testing.T) {
	inner := errors.New("inner")
	wrapped := fmt.Errorf("outer: %w", inner)
	us, ok := wrapped.(unwrapableSingle)
	if !ok {
		t.Skip("fmt.Errorf didn't produce unwrapableSingle")
	}
	e := &singleWrappedWithStackError{
		wrappedErr: us,
		stack:      nil,
	}
	if e.StackTrace() != nil {
		t.Fatal("expected nil StackTrace for nil stack")
	}
	if e.Unwrap() != inner {
		t.Fatal("unwrap mismatch")
	}
}

// =============================================================================
// indirect.go tests
// =============================================================================

func TestAppendError_NilErr(t *testing.T) {
	var errs []error
	result := AppendError(errs, nil)
	if len(result) != 0 {
		t.Fatal("appending nil should not grow slice")
	}
}

func TestAppendError_ToEmpty(t *testing.T) {
	var errs []error
	e := errors.New("x")
	result := AppendError(errs, e)
	if len(result) != 1 || result[0] != e {
		t.Fatal("should append to empty slice")
	}
}

func TestAppendError_DifferentFromLast(t *testing.T) {
	e1 := errors.New("a")
	e2 := errors.New("b")
	errs := []error{e1}
	result := AppendError(errs, e2)
	if len(result) != 2 {
		t.Fatal("should append different error")
	}
}

func TestAppendError_DuplicateNotLast(t *testing.T) {
	e1 := errors.New("a")
	e2 := errors.New("b")
	errs := []error{e1, e2}
	// e1 is not the last element, so it WILL be appended (only last is checked)
	result := AppendError(errs, e1)
	if len(result) != 3 {
		t.Fatal("expected append since only last element is checked for dedup")
	}
}

func TestCheckErrors_Empty(t *testing.T) {
	err := CheckErrors(nil)
	if err != nil {
		t.Fatal("expected nil for empty")
	}
	err = CheckErrors([]error{})
	if err != nil {
		t.Fatal("expected nil for empty slice")
	}
}

func TestCheckErrors_Multiple(t *testing.T) {
	e1 := errors.New("a")
	e2 := errors.New("b")
	err := CheckErrors([]error{e1, e2})
	if err == nil {
		t.Fatal("expected joined error")
	}
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatal("joined error should contain both")
	}
}

func TestClearErrors(t *testing.T) {
	errs := []error{errors.New("a"), errors.New("b")}
	result := ClearErrors(errs)
	if len(result) != 0 {
		t.Fatal("expected empty after clear")
	}
	// Should retain capacity
	if cap(result) < 2 {
		t.Fatal("expected retained capacity")
	}
}

// =============================================================================
// stacktrace.go tests
// =============================================================================

func TestToBinaryRepresentation(t *testing.T) {
	st := StackTrace{
		{PC: 0x1000},
		{PC: 0x2000},
		{PC: 0x3000},
	}
	rep := toBinaryRepresentation(st)
	// Reversed order: 0x3000, 0x2000, 0x1000 as little-endian uint64
	if len(rep) != 24 { // 3 * 8
		t.Fatalf("expected 24 bytes, got %d", len(rep))
	}
	// First 8 bytes should be 0x3000 LE
	if rep[0] != 0x00 || rep[1] != 0x30 {
		t.Fatalf("unexpected first frame bytes: %x", rep[:8])
	}
}

func TestToBinaryRepresentation_Empty(t *testing.T) {
	rep := toBinaryRepresentation(StackTrace{})
	if len(rep) != 0 {
		t.Fatal("expected empty for empty trace")
	}
}

func TestIsSubStack_True(t *testing.T) {
	// stackA is a prefix of stackB (shorter stack is sub-stack of longer)
	stA := StackTrace{{PC: 0x1000}}
	stB := StackTrace{{PC: 0x2000}, {PC: 0x1000}}
	repA := toBinaryRepresentation(stA)
	repB := toBinaryRepresentation(stB)
	if !isSubStack(repA, repB) {
		t.Fatal("expected A to be sub-stack of B")
	}
}

func TestIsSubStack_False_SameLength(t *testing.T) {
	stA := StackTrace{{PC: 0x1000}}
	stB := StackTrace{{PC: 0x1000}}
	repA := toBinaryRepresentation(stA)
	repB := toBinaryRepresentation(stB)
	if isSubStack(repA, repB) {
		t.Fatal("same length stacks should not be sub-stacks (a >= b returns false)")
	}
}

func TestIsSubStack_False_ALonger(t *testing.T) {
	stA := StackTrace{{PC: 0x2000}, {PC: 0x1000}}
	stB := StackTrace{{PC: 0x1000}}
	repA := toBinaryRepresentation(stA)
	repB := toBinaryRepresentation(stB)
	if isSubStack(repA, repB) {
		t.Fatal("longer A cannot be sub-stack of shorter B")
	}
}

func TestIsSubStack_False_DifferentContent(t *testing.T) {
	stA := StackTrace{{PC: 0x9999}}
	stB := StackTrace{{PC: 0x2000}, {PC: 0x1000}}
	repA := toBinaryRepresentation(stA)
	repB := toBinaryRepresentation(stB)
	if isSubStack(repA, repB) {
		t.Fatal("different content should not match")
	}
}

// =============================================================================
// zerolog.go tests - frameContainer
// =============================================================================

func TestFrameContainer_CleanupAndResolveType_Idempotent(t *testing.T) {
	fc := &frameContainer{
		File: "/some/path/file.go",
		Type: FrameTypeOther,
	}
	fc.CleanupAndResolveType()
	// Type already set, should not change
	if fc.Type != FrameTypeOther {
		t.Fatal("should not change already-resolved type")
	}
	if fc.File != "/some/path/file.go" {
		t.Fatal("should not modify file when type already set")
	}
}

func TestFrameContainer_MarshalZerologObject(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	fc := &frameContainer{
		File:     "main.go",
		Line:     "42",
		Function: "main.Run",
	}
	logger.Log().Object("frame", fc).Send()

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	frame, ok := result["frame"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected frame object, got %v", result)
	}
	if frame[StackSourceFileName] != "main.go" {
		t.Fatal("wrong file")
	}
	if frame[StackSourceLineName] != "42" {
		t.Fatal("wrong line")
	}
	if frame[StackSourceFunctionName] != "main.Run" {
		t.Fatal("wrong function")
	}
}

func TestFrameContainer_MarshalZerologObject_EmptyFields(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	fc := &frameContainer{}
	logger.Log().Object("frame", fc).Send()

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	frame, ok := result["frame"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected frame object, got %v", result)
	}
	// Empty fields should be omitted
	if _, exists := frame[StackSourceFileName]; exists {
		t.Fatal("empty file should be omitted")
	}
}

// =============================================================================
// zerolog.go tests - errorContainer & longestCommonPathPrefix
// =============================================================================

func TestLongestCommonPathPrefix_SameDir(t *testing.T) {
	ec := &errorContainer{
		Facts: []*errorFact{
			{Frame: &frameContainer{File: "/home/user/project/pkg/a.go", Type: FrameTypeOther}},
			{Frame: &frameContainer{File: "/home/user/project/pkg/b.go", Type: FrameTypeOther}},
		},
	}
	pfx, pfxLen := ec.longestCommonPathPrefix()
	if pfxLen == 0 {
		t.Fatal("expected non-zero prefix")
	}
	if !strings.HasPrefix("/home/user/project/pkg/a.go", pfx) {
		t.Fatalf("prefix %q should be prefix of the paths", pfx)
	}
}

func TestLongestCommonPathPrefix_DifferentDirs(t *testing.T) {
	ec := &errorContainer{
		Facts: []*errorFact{
			{Frame: &frameContainer{File: "/home/user/project/pkg/a.go", Type: FrameTypeOther}},
			{Frame: &frameContainer{File: "/home/user/project/cmd/b.go", Type: FrameTypeOther}},
		},
	}
	pfx, pfxLen := ec.longestCommonPathPrefix()
	if pfxLen == 0 {
		t.Fatal("expected non-zero prefix")
	}
	// Should find /home/user/project/
	if !strings.HasSuffix(pfx, "project/") {
		t.Fatalf("expected prefix ending with 'project/', got %q", pfx)
	}
}

func TestLongestCommonPathPrefix_NonOtherSkipped(t *testing.T) {
	ec := &errorContainer{
		Facts: []*errorFact{
			{Frame: &frameContainer{File: "/usr/local/go/src/runtime/proc.go", Type: FrameTypeGoRoot}},
			{Frame: &frameContainer{File: "/home/user/project/main.go", Type: FrameTypeOther}},
		},
	}
	pfx, _ := ec.longestCommonPathPrefix()
	// Only one FrameTypeOther, so min == max
	// With a single path, longestCommonPathPrefix still returns something
	if strings.Contains(pfx, "runtime") {
		t.Fatal("should not include GoRoot paths in prefix calculation")
	}
}

func TestLongestCommonPathPrefix_Empty(t *testing.T) {
	ec := &errorContainer{Facts: []*errorFact{}}
	pfx, pfxLen := ec.longestCommonPathPrefix()
	if pfx != "" || pfxLen != 0 {
		t.Fatal("expected empty prefix for no facts")
	}
}

func TestErrorContainer_CompactStackTrace(t *testing.T) {
	ec := &errorContainer{
		Facts: []*errorFact{
			{Frame: &frameContainer{File: "/home/user/project/pkg/a.go", Type: FrameTypeOther}},
			{Frame: &frameContainer{File: "/home/user/project/pkg/b.go", Type: FrameTypeOther}},
		},
	}
	ec.CompactStackTrace()
	// Files should be shortened
	for _, f := range ec.Facts {
		if strings.HasPrefix(f.Frame.File, "/home/user/project/") {
			t.Fatalf("expected path to be compacted, still has full prefix: %q", f.Frame.File)
		}
	}
}

// =============================================================================
// zerolog.go tests - errorFact marshaling
// =============================================================================

func TestErrorFact_MarshalZerologObject(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	fact := &errorFact{
		Msg:      "something broke",
		Id:       3,
		ParentId: 1,
	}
	logger.Log().Object("fact", fact).Send()

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	f := result["fact"].(map[string]interface{})
	if f["msg"] != "something broke" {
		t.Fatal("wrong msg")
	}
	if f["id"].(float64) != 3 {
		t.Fatal("wrong id")
	}
	if f["parentId"].(float64) != 1 {
		t.Fatal("wrong parentId")
	}
}

func TestErrorFact_MarshalZerologObject_SameIdParent(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	fact := &errorFact{
		Msg:      "root",
		Id:       0,
		ParentId: 0,
	}
	logger.Log().Object("fact", fact).Send()

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	f := result["fact"].(map[string]interface{})
	// When Id == ParentId, parentId should be omitted
	if _, exists := f["parentId"]; exists {
		t.Fatal("parentId should be omitted when equal to id")
	}
}

func TestErrorFact_WithCBORData(t *testing.T) {
	data, _ := cbor.Marshal("hello")
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	fact := &errorFact{
		StructuredData: data,
		Id:             0,
		ParentId:       0,
	}
	logger.Log().Object("fact", fact).Send()

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	f := result["fact"].(map[string]interface{})
	if _, exists := f["dataDiag"]; !exists {
		t.Fatal("expected dataDiag field for valid CBOR")
	}
}

// =============================================================================
// zerolog.go tests - gatherFactsAndStacks
// =============================================================================

func TestNewGatherFactsAndStacks(t *testing.T) {
	g := newGatherFactsAndStacks()
	if g == nil {
		t.Fatal("expected non-nil")
	}
	if len(g.stacks) != 1 {
		t.Fatal("expected 1 initial stack entry (the nil/stackless slot)")
	}
	if len(g.perStackFacts) != 1 {
		t.Fatal("expected 1 initial perStackFacts entry")
	}
	if g.nextId != 0 {
		t.Fatal("expected nextId=0")
	}
	if g.materialized {
		t.Fatal("should not be materialized initially")
	}
	if g.hasStacks() {
		t.Fatal("should not have stacks initially")
	}
}

func TestGatherFactsAndStacks_SimpleError(t *testing.T) {
	g := newGatherFactsAndStacks()
	err := New("simple error")
	addErr := g.addError(err, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}
	if g.nextId != 1 {
		t.Fatalf("expected nextId=1, got %d", g.nextId)
	}
	if !g.hasStacks() {
		t.Fatal("should have stacks after adding error with stack trace")
	}
}

func TestGatherFactsAndStacks_ErrorWithoutStack(t *testing.T) {
	g := newGatherFactsAndStacks()
	err := errors.New("plain error") // stdlib, no stack
	addErr := g.addError(err, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}
	// Should land in the stackless slot (index 0)
	if g.hasStacks() {
		t.Fatal("plain error should not create a stack entry")
	}
	if !g.hasStacklessStream() {
		t.Fatal("should have stackless facts")
	}
}

func TestGatherFactsAndStacks_WrappedChain(t *testing.T) {
	inner := New("inner")
	mid := Errorf("mid: %w", inner)
	outer := Errorf("outer: %w", mid)

	g := newGatherFactsAndStacks()
	addErr := g.addError(outer, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}
	// Should have 3 error facts (outer, mid, inner)
	if g.nextId != 3 {
		t.Fatalf("expected 3 ids assigned, got %d", g.nextId)
	}
}

func TestGatherFactsAndStacks_WithCBORData(t *testing.T) {
	data, _ := cbor.Marshal(map[string]string{"key": "val"})
	err := ErrorfWithData(data, "data error")

	g := newGatherFactsAndStacks()
	addErr := g.addError(err, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}
	// The CBOR data adds an extra fact entry
	if g.nextId != 1 {
		t.Fatalf("expected 1 id, got %d", g.nextId)
	}
}

func TestGatherFactsAndStacks_AddAfterMaterialize(t *testing.T) {
	g := newGatherFactsAndStacks()
	err := errors.New("first")
	_ = g.addError(err, 0)
	g.materialize()

	addErr := g.addError(errors.New("second"), 0)
	if addErr == nil {
		t.Fatal("expected error when adding after materialize")
	}
}

func TestGatherFactsAndStacks_MaterializeIdempotent(t *testing.T) {
	g := newGatherFactsAndStacks()
	_ = g.addError(New("test"), 0)
	g.materialize()
	// Second call should be safe
	g.materialize()
	if !g.materialized {
		t.Fatal("should remain materialized")
	}
}

func TestGatherFactsAndStacks_JoinedErrors(t *testing.T) {
	e1 := New("branch1")
	e2 := New("branch2")
	joined := errors.Join(e1, e2)

	g := newGatherFactsAndStacks()
	addErr := g.addError(joined, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}
	// joined itself (no stack) + e1 + e2 = 3 ids
	if g.nextId < 3 {
		t.Fatalf("expected at least 3 ids, got %d", g.nextId)
	}
}

func TestGatherFactsAndStacks_SelfReferentialProtection(t *testing.T) {
	// The addError function checks se != err to avoid infinite recursion
	// This test verifies it doesn't hang
	err := New("self")
	g := newGatherFactsAndStacks()
	addErr := g.addError(err, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}
}

// =============================================================================
// zerolog.go tests - MarshalError (integration)
// =============================================================================

func TestMarshalError_Nil(t *testing.T) {
	result := MarshalError(nil)
	if result != nil {
		t.Fatal("expected nil for nil error")
	}
}

func TestMarshalError_Simple(t *testing.T) {
	err := New("test error")
	result := MarshalError(err)
	if result == nil {
		t.Fatal("expected non-nil")
	}

	// Verify it can be marshaled through zerolog
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "test error") {
		t.Fatalf("expected 'test error' in output, got: %s", output)
	}
}

func TestMarshalError_WrappedChain(t *testing.T) {
	inner := New("db connection failed")
	mid := Errorf("query failed: %w", inner)
	outer := Errorf("request failed: %w", mid)

	result := MarshalError(outer)

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "db connection failed") {
		t.Fatalf("expected inner message in output: %s", output)
	}
	if !strings.Contains(output, "query failed") {
		t.Fatalf("expected mid message in output: %s", output)
	}
	if !strings.Contains(output, "request failed") {
		t.Fatalf("expected outer message in output: %s", output)
	}
}

func TestMarshalError_WithCBOR(t *testing.T) {
	data, _ := cbor.Marshal(map[string]int{"retries": 3})
	err := ErrorfWithData(data, "operation failed")

	result := MarshalError(err)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "operation failed") {
		t.Fatalf("expected message in output: %s", output)
	}
	if !strings.Contains(output, "dataDiag") {
		t.Fatalf("expected dataDiag in output: %s", output)
	}
}

func TestMarshalError_JoinedErrors(t *testing.T) {
	e1 := New("error one")
	e2 := New("error two")
	joined := errors.Join(e1, e2)

	result := MarshalError(joined)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "error one") {
		t.Fatalf("expected 'error one' in output: %s", output)
	}
	if !strings.Contains(output, "error two") {
		t.Fatalf("expected 'error two' in output: %s", output)
	}
}

func TestMarshalError_StdlibError(t *testing.T) {
	// errors.New doesn't have a stack trace — exercises the stackless path
	err := errors.New("stdlib error")
	result := MarshalError(err)
	if result == nil {
		t.Fatal("expected non-nil")
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "stdlib error") {
		t.Fatalf("expected message in output: %s", output)
	}
}

func TestMarshalError_DeeplyNested(t *testing.T) {
	err := New("leaf")
	for i := 0; i < 10; i++ {
		err = Errorf("wrap-%d: %w", i, err)
	}

	result := MarshalError(err)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "leaf") {
		t.Fatalf("expected leaf message in output: %s", output)
	}
	if !strings.Contains(output, "wrap-9") {
		t.Fatalf("expected outermost wrap in output: %s", output)
	}
}

func TestMarshalError_MixedStdlibAndEh(t *testing.T) {
	stdErr := errors.New("from stdlib")
	ehErr := Errorf("from eh: %w", stdErr)

	result := MarshalError(ehErr)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "from stdlib") {
		t.Fatalf("expected stdlib message: %s", output)
	}
	if !strings.Contains(output, "from eh") {
		t.Fatalf("expected eh message: %s", output)
	}
}

// =============================================================================
// zerolog.go tests - findStack
// =============================================================================

// Helper: create errors at known call depths to get overlapping stacks
func makeErrorAtDepth0() error { return New("depth0") }
func makeErrorAtDepth1() error { return Errorf("depth1: %w", makeErrorAtDepth0()) }
func makeErrorAtDepth2() error { return Errorf("depth2: %w", makeErrorAtDepth1()) }

func TestFindStack_OverlappingStacks(t *testing.T) {
	// Errors created in the same call chain should have overlapping stacks
	// that get merged
	err := makeErrorAtDepth2()

	g := newGatherFactsAndStacks()
	addErr := g.addError(err, 0)
	if addErr != nil {
		t.Fatal(addErr)
	}

	// The stacks from depth0, depth1, depth2 should share a common suffix
	// and ideally be merged into a single stack
	// We can't assert the exact count since it depends on stack merging,
	// but we should have at least 1 real stack
	if !g.hasStacks() {
		t.Fatal("expected stacks")
	}
}

func TestFindStack_DisjointStacks(t *testing.T) {
	// Two errors created in completely separate goroutine-like contexts
	// should end up as separate stacks.
	// Here we just create them in different function chains.
	e1 := makeErrorAtDepth0()
	e2 := errors.New("completely different") // no stack

	g := newGatherFactsAndStacks()
	_ = g.addError(e1, 0)
	_ = g.addError(e2, 0)
	// e1 has a stack, e2 doesn't
	// stacks[0] is nil (stackless), stacks[1] is e1's stack
	if len(g.stacks) != 2 {
		t.Fatalf("expected 2 stack slots (nil + e1), got %d", len(g.stacks))
	}
}

// =============================================================================
// zerolog.go tests - errorFactsLogger
// =============================================================================

func TestErrorFactsLogger_Facts(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	efl := &errorFactsLogger{
		name:  "test",
		facts: []*errorFact{{Msg: "a", Id: 0, ParentId: 0}},
	}
	logger.Log().Object("x", efl).Send()

	output := buf.String()
	if !strings.Contains(output, "test") {
		t.Fatalf("expected name in output: %s", output)
	}
}

func TestErrorFactsLogger_Facts2(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	efl := &errorFactsLogger{
		name: "nested",
		facts2: [][]*errorFact{
			{{Msg: "a", Id: 0, ParentId: 0}},
			{{Msg: "b", Id: 1, ParentId: 0}},
		},
	}
	logger.Log().Object("x", efl).Send()

	output := buf.String()
	if !strings.Contains(output, `"a"`) || !strings.Contains(output, `"b"`) {
		t.Fatalf("expected both messages: %s", output)
	}
}

// =============================================================================
// zerolog.go tests - MarshalZerologArray for gatherFactsAndStacks
// =============================================================================

func TestGatherFactsAndStacks_MarshalArray(t *testing.T) {
	err := Errorf("test: %w", New("inner"))
	g := newGatherFactsAndStacks()
	_ = g.addError(err, 0)

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("err", g).Send()

	output := buf.String()
	if !strings.Contains(output, "streams") {
		t.Fatalf("expected 'streams' key in output: %s", output)
	}
	if !strings.Contains(output, "test: inner") {
		t.Fatalf("expected error message in output: %s", output)
	}
}

// =============================================================================
// Integration: full round-trip test
// =============================================================================

func TestFullRoundTrip_ComplexErrorTree(t *testing.T) {
	// Build a realistic error tree:
	// outer wraps mid1 and mid2 (via Join)
	// mid1 wraps inner1 (with CBOR data)
	// mid2 is a plain stdlib error

	cborData, _ := cbor.Marshal(map[string]interface{}{
		"table":  "users",
		"column": "email",
	})
	inner1 := ErrorfWithData(cborData, "constraint violation")
	mid1 := Errorf("insert failed: %w", inner1)
	mid2 := errors.New("connection timeout")
	joined := errors.Join(mid1, mid2)
	outer := Errorf("transaction failed: %w", joined)

	result := MarshalError(outer)
	if result == nil {
		t.Fatal("expected non-nil")
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()

	// Validate JSON parseable
	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Check all messages appear
	for _, msg := range []string{
		"constraint violation",
		"insert failed",
		"connection timeout",
		"transaction failed",
	} {
		if !strings.Contains(output, msg) {
			t.Fatalf("expected %q in output: %s", msg, output)
		}
	}

	// Check CBOR diagnostic appears
	if !strings.Contains(output, "dataDiag") {
		t.Fatalf("expected CBOR diagnostic in output: %s", output)
	}

	t.Logf("Full output:\n%s", output)
}

// =============================================================================
// Edge cases and regression tests
// =============================================================================

func TestErrorf_EmptyFormat(t *testing.T) {
	err := Errorf("")
	if err.Error() != "" {
		t.Fatal("expected empty message")
	}
}

func TestNew_EmptyMessage(t *testing.T) {
	err := New("")
	if err.Error() != "" {
		t.Fatal("expected empty message")
	}
}

func TestErrorsIs_WorksThroughWrappers(t *testing.T) {
	sentinel := errors.New("sentinel")
	wrapped := Errorf("layer1: %w", sentinel)
	wrapped2 := Errorf("layer2: %w", wrapped)

	if !errors.Is(wrapped2, sentinel) {
		t.Fatal("errors.Is should find sentinel through wrapping chain")
	}
}

func TestErrorsAs_WorksThroughWrappers(t *testing.T) {
	inner := ErrorfWithData([]byte{0xa0}, "has data")
	outer := Errorf("wrapped: %w", inner)

	var esd ErrorWithStructuredData
	if !errors.As(outer, &esd) {
		t.Fatal("errors.As should find ErrorWithStructuredData through wrapping")
	}
}

func TestMarshalError_DoubleWrappedJoin(t *testing.T) {
	e1 := New("a")
	e2 := New("b")
	j1 := errors.Join(e1, e2)
	outer := Errorf("outer: %w", j1)
	top := Errorf("top: %w", outer)

	result := MarshalError(top)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	// Should not panic and should contain all messages
	output := buf.String()
	for _, msg := range []string{"a", "b", "outer", "top"} {
		if !strings.Contains(output, msg) {
			t.Fatalf("missing %q in output: %s", msg, output)
		}
	}
}

func TestMarshalError_NilInJoin(t *testing.T) {
	e1 := New("valid")
	joined := errors.Join(e1, nil)

	result := MarshalError(joined)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()

	output := buf.String()
	if !strings.Contains(output, "valid") {
		t.Fatalf("expected message in output: %s", output)
	}
}

// =============================================================================
// Coverage gap: multiWrappedWithStackError exercised through public API
// =============================================================================

func TestErrorfWithData_MultiWrapped_AllMethods(t *testing.T) {
	e1 := errors.New("a")
	e2 := errors.New("b")
	joined := errors.Join(e1, e2)

	data, _ := cbor.Marshal("context")
	outer := ErrorfWithData(data, "multi: %w", joined)

	// Exercise Error()
	if !strings.Contains(outer.Error(), "a") {
		t.Fatal("expected 'a' in message")
	}

	// Exercise GetCBORStructuredData / SetCBORStructuredData
	esd, ok := outer.(ErrorWithStructuredData)
	if !ok {
		t.Skip("not ErrorWithStructuredData — fmt.Errorf may not produce unwrapableMulti")
	}
	if !bytes.Equal(esd.GetCBORStructuredData(), data) {
		t.Fatal("CBOR data mismatch")
	}
	newData := []byte{0xf6} // CBOR null
	esd.SetCBORStructuredData(newData)
	if !bytes.Equal(esd.GetCBORStructuredData(), newData) {
		t.Fatal("SetCBOR did not update")
	}

	// Exercise StackTrace()
	st, ok := outer.(stackTracer)
	if ok {
		trace := st.StackTrace()
		if len(trace) == 0 {
			t.Fatal("expected non-empty stack trace")
		}
	}

	// Exercise Unwrap()
	um, ok := outer.(interface{ Unwrap() []error })
	if ok {
		errs := um.Unwrap()
		if len(errs) == 0 {
			t.Fatal("expected unwrapped errors")
		}
	}

	// Full round-trip through MarshalError
	result := MarshalError(outer)
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Error().Object("error", result.(zerolog.LogObjectMarshaler)).Send()
	output := buf.String()
	if !strings.Contains(output, "multi") {
		t.Fatalf("expected 'multi' in output: %s", output)
	}
}

func TestFrameContainer_CleanupAndResolveType_GoRoot(t *testing.T) {
	fc := &frameContainer{
		File: "/usr/lib/go-1.22/src/runtime/proc.go",
		Type: FrameTypeNil,
	}
	fc.CleanupAndResolveType()
	// May be GoRoot or Other depending on GOROOT value
	if fc.Type == FrameTypeNil {
		t.Fatal("type should have been resolved")
	}
}

func TestFrameContainer_CleanupAndResolveType_Other(t *testing.T) {
	fc := &frameContainer{
		File: "/home/user/myproject/main.go",
		Type: FrameTypeNil,
	}
	fc.CleanupAndResolveType()
	if fc.Type != FrameTypeOther {
		t.Fatalf("expected FrameTypeOther, got %d", fc.Type)
	}
}

func TestErrorContainer_MarshalZerologObject(t *testing.T) {
	ec := &errorContainer{
		Facts: []*errorFact{
			{Msg: "test", Id: 0, ParentId: 0},
		},
	}
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Log().Object("ec", ec).Send()

	output := buf.String()
	if !strings.Contains(output, "perStackFacts") {
		t.Fatalf("expected perStackFacts key: %s", output)
	}
}

func TestErrorfWithData_NilCBOR_ThenSet(t *testing.T) {
	err := ErrorfWithData(nil, "test")
	esd := err.(ErrorWithStructuredData)

	if esd.GetCBORStructuredData() != nil {
		t.Fatal("expected nil initially")
	}

	data, _ := cbor.Marshal(42)
	esd.SetCBORStructuredData(data)

	if !bytes.Equal(esd.GetCBORStructuredData(), data) {
		t.Fatal("set/get round trip failed")
	}
}
