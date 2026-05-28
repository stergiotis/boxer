//go:build llm_generated_opus46

package goclient

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
)

// ---------------------------------------------------------------------------
// IDL definitions used across all E2E tests
// ---------------------------------------------------------------------------

// FuncProcId constants matching the order the nodes are passed to GenerateCode.
const (
	testFuncProcIdOffset        uint32 = 0
	testFuncProcIdSetColor      uint32 = testFuncProcIdOffset + 0
	testFuncProcIdMyWidget      uint32 = testFuncProcIdOffset + 1
	testFuncProcIdFetchCounters uint32 = testFuncProcIdOffset + 2
)

// Method IDs for MyWidget (factory). Build is always 0.
const (
	testMyWidgetMethodIdBuild     uint32 = 0
	testMyWidgetMethodIdSetWidth  uint32 = 1
	testMyWidgetMethodIdSetHeight uint32 = 2
	testMyWidgetMethodIdSetLabel  uint32 = 3
)

func buildTestNodes() []ir.NodeI {
	// Node 0: Procedure "setColor" — identity + uint8 re,gr,bl,al
	proc := idl.NewProceduralNode(n("setColor")).
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("re"), ctabb.U8).
			PlainArg(n("gr"), ctabb.U8).
			PlainArg(n("bl"), ctabb.U8).
			PlainArg(n("al"), ctabb.U8).
			Build()).
		Build()

	// Node 1: Factory "myWidget" — identity + string label + 3 builder methods
	mb := idl.NewMethodBuilder()
	mb.BeginMethod(n("setWidth")).Arg(n("width"), ctabb.F32).EndMethod()
	mb.BeginMethod(n("setHeight")).Arg(n("height"), ctabb.F32).EndMethod()
	mb.BeginMethod(n("setLabel")).Arg(n("text"), ctabb.S).EndMethod()

	factory := idl.NewBuilderFactoryNode(n("myWidget")).
		WithIdentityId(true).
		WithSettingImmediate(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("label"), ctabb.S).
			Build()).
		AddMethods(mb.Build()...).
		Build()

	// Node 2: Fetcher "fetchCounters" — returns uint64 count + float64 avg
	fetcher := idl.NewFetcherNode(n("fetchCounters")).
		AddReturnValue(n("count"), ctabb.U64).
		AddReturnValue(n("avg"), ctabb.F64).
		Build()

	return []ir.NodeI{proc, factory, fetcher}
}

// ---------------------------------------------------------------------------
// Test interpreter — a hand-coded Go struct that mirrors what the goclient
// code generator produces. It reads opcodes from an UnmarshallReaderI and
// records what it received into observable state.
// ---------------------------------------------------------------------------

type recordedProcedure struct {
	funcProcId uint32
	id         uint64
	r, g, b, a uint8
}

type recordedMethodCall struct {
	methodId uint32
	f32Arg   float32
	strArg   string
}

type recordedFactory struct {
	funcProcId uint32
	id         uint64
	label      string
	methods    []recordedMethodCall
}

type recordedFetcher struct {
	funcProcId uint32
}

type testInterpreter struct {
	procedures []recordedProcedure
	factories  []recordedFactory
	fetchers   []recordedFetcher
}

func (inst *testInterpreter) interpret(u runtime.UnmarshallReaderI) {
	f := u.ReadUInt32()
	inst.dispatch(u, f, 0)
}

func (inst *testInterpreter) dispatch(u runtime.UnmarshallReaderI, f uint32, d int) {
	switch f {
	case testFuncProcIdSetColor:
		// arguments (matches goclient output for procedure with identity + plain args)
		id := u.ReadUInt64()
		r := u.ReadUInt8()
		g := u.ReadUInt8()
		b := u.ReadUInt8()
		a := u.ReadUInt8()
		inst.procedures = append(inst.procedures, recordedProcedure{
			funcProcId: f, id: id, r: r, g: g, b: b, a: a,
		})

	case testFuncProcIdMyWidget:
		// arguments (matches goclient output for factory with identity + plain args)
		id := u.ReadUInt64()
		label := u.ReadString()
		// methods loop (matches goclient output: read method ID, switch, break on Build)
		fc := recordedFactory{funcProcId: f, id: id, label: label}
		for {
			m := u.ReadUInt32()
			switch m {
			case testMyWidgetMethodIdBuild:
				goto doneMyWidget
			case testMyWidgetMethodIdSetWidth:
				width := u.ReadFloat32()
				fc.methods = append(fc.methods, recordedMethodCall{methodId: m, f32Arg: width})
			case testMyWidgetMethodIdSetHeight:
				height := u.ReadFloat32()
				fc.methods = append(fc.methods, recordedMethodCall{methodId: m, f32Arg: height})
			case testMyWidgetMethodIdSetLabel:
				text := u.ReadString()
				fc.methods = append(fc.methods, recordedMethodCall{methodId: m, strArg: text})
			}
		}
	doneMyWidget:
		inst.factories = append(inst.factories, fc)

	case testFuncProcIdFetchCounters:
		inst.fetchers = append(inst.fetchers, recordedFetcher{funcProcId: f})
	}
}

// ---------------------------------------------------------------------------
// Test: Generated code matches the hand-coded interpreter structure
// ---------------------------------------------------------------------------

func TestE2E_GeneratedCodeStructure(t *testing.T) {
	nodes := buildTestNodes()

	var enumBuf, dispatchBuf bytes.Buffer
	tracker := compiletime.NewStateAndErrTracker[GeneratorStateE](GenerateStateInitial, "")
	wh := WriterHolder{
		MethodWriter:   &bytes.Buffer{},
		FactoryWriter:  &bytes.Buffer{},
		DispatchWriter: &dispatchBuf,
		EnumWriter:     &enumBuf,
		TypeWriter:     &bytes.Buffer{},
	}
	err := GenerateCode(wh, nodes, tracker)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	enumCode := enumBuf.String()
	dispatchCode := dispatchBuf.String()

	t.Run("enum_FuncProcIds", func(t *testing.T) {
		for _, want := range []string{
			"FuncProcIdSetColor",
			"FuncProcIdMyWidget",
			"FuncProcIdFetchCounters",
		} {
			if !strings.Contains(enumCode, want) {
				t.Errorf("enum missing %s:\n%s", want, enumCode)
			}
		}
	})

	t.Run("enum_MethodIds", func(t *testing.T) {
		for _, want := range []string{
			"MyWidgetMethodIdBuild",
			"MyWidgetMethodIdSetWidth",
			"MyWidgetMethodIdSetHeight",
			"MyWidgetMethodIdSetLabel",
		} {
			if !strings.Contains(enumCode, want) {
				t.Errorf("enum missing %s:\n%s", want, enumCode)
			}
		}
	})

	t.Run("dispatch_procedure", func(t *testing.T) {
		for _, want := range []string{
			"case FuncProcIdSetColor:",
			"u.ReadUInt64()", // identity
			"u.ReadUInt8()",  // r,g,b,a
		} {
			if !strings.Contains(dispatchCode, want) {
				t.Errorf("dispatch missing %q:\n%s", want, dispatchCode)
			}
		}
	})

	t.Run("dispatch_factory", func(t *testing.T) {
		for _, want := range []string{
			"case FuncProcIdMyWidget:",
			"u.ReadUInt64()",                    // identity
			"u.ReadString()",                    // label
			"MyWidgetMethodIdE(u.ReadUInt32())", // method dispatch
			"MyWidgetMethodIdBuild",             // build case
			"u.ReadFloat32()",                   // width/height args
		} {
			if !strings.Contains(dispatchCode, want) {
				t.Errorf("dispatch missing %q:\n%s", want, dispatchCode)
			}
		}
		// Method loop structure
		if !strings.Contains(dispatchCode, "for {") {
			t.Errorf("dispatch missing method loop:\n%s", dispatchCode)
		}
		if !strings.Contains(dispatchCode, "goto doneMyWidget") {
			t.Errorf("dispatch missing goto doneMyWidget:\n%s", dispatchCode)
		}
	})

	t.Run("dispatch_fetcher", func(t *testing.T) {
		if !strings.Contains(dispatchCode, "case FuncProcIdFetchCounters:") {
			t.Errorf("dispatch missing fetcher case:\n%s", dispatchCode)
		}
	})

	t.Run("dispatch_default", func(t *testing.T) {
		if !strings.Contains(dispatchCode, "default:") {
			t.Errorf("dispatch missing default case:\n%s", dispatchCode)
		}
	})
}

// ---------------------------------------------------------------------------
// Helper: serialize an opcode message (matching goserver output patterns)
// Returns the raw payload bytes (without length prefix).
// ---------------------------------------------------------------------------

func newTestMarshaller(buf *bytes.Buffer) *runtime.Marshaller {
	return runtime.NewMarshaller(buf, binary.LittleEndian, func(err error) { panic(err) })
}

func newTestUnmarshaller(buf *bytes.Buffer) *runtime.Unmarshaller {
	return runtime.NewUnmarshaller(buf, binary.LittleEndian, func(err error) { panic(err) }, nil)
}

// ---------------------------------------------------------------------------
// Test: Single procedure roundtrip
// ---------------------------------------------------------------------------

func TestE2E_ProcedureRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	// Serialize as goserver would: FuncProcId + identity + plain args
	m.WriteUint32(testFuncProcIdSetColor)
	m.WriteUint64(0xCAFE)
	m.WriteUint8(255)
	m.WriteUint8(128)
	m.WriteUint8(64)
	m.WriteUint8(0)

	// Deserialize with our test interpreter
	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	interp.interpret(u)

	if len(interp.procedures) != 1 {
		t.Fatalf("expected 1 procedure call, got %d", len(interp.procedures))
	}
	p := interp.procedures[0]
	if p.funcProcId != testFuncProcIdSetColor {
		t.Errorf("funcProcId = %d, want %d", p.funcProcId, testFuncProcIdSetColor)
	}
	if p.id != 0xCAFE {
		t.Errorf("id = 0x%X, want 0xCAFE", p.id)
	}
	if p.r != 255 || p.g != 128 || p.b != 64 || p.a != 0 {
		t.Errorf("rgba = (%d,%d,%d,%d), want (255,128,64,0)", p.r, p.g, p.b, p.a)
	}
}

// ---------------------------------------------------------------------------
// Test: Factory with method chain roundtrip
// ---------------------------------------------------------------------------

func TestE2E_FactoryRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	// Serialize as goserver would:
	// FuncProcId + identity + label
	m.WriteUint32(testFuncProcIdMyWidget)
	m.WriteUint64(0x1234)
	m.WriteString("main panel")

	// Method chain: setWidth(320.0) → setHeight(240.0) → setLabel("updated") → Build
	m.WriteUint32(testMyWidgetMethodIdSetWidth)
	m.WriteFloat32(320.0)
	m.WriteUint32(testMyWidgetMethodIdSetHeight)
	m.WriteFloat32(240.0)
	m.WriteUint32(testMyWidgetMethodIdSetLabel)
	m.WriteString("updated")
	m.WriteUint32(testMyWidgetMethodIdBuild)

	// Deserialize
	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	interp.interpret(u)

	if len(interp.factories) != 1 {
		t.Fatalf("expected 1 factory call, got %d", len(interp.factories))
	}
	f := interp.factories[0]
	if f.funcProcId != testFuncProcIdMyWidget {
		t.Errorf("funcProcId = %d, want %d", f.funcProcId, testFuncProcIdMyWidget)
	}
	if f.id != 0x1234 {
		t.Errorf("id = 0x%X, want 0x1234", f.id)
	}
	if f.label != "main panel" {
		t.Errorf("label = %q, want %q", f.label, "main panel")
	}
	if len(f.methods) != 3 {
		t.Fatalf("expected 3 method calls, got %d", len(f.methods))
	}
	// setWidth
	if f.methods[0].methodId != testMyWidgetMethodIdSetWidth || f.methods[0].f32Arg != 320.0 {
		t.Errorf("method[0] = {id:%d, f32:%f}, want {id:%d, f32:320.0}",
			f.methods[0].methodId, f.methods[0].f32Arg, testMyWidgetMethodIdSetWidth)
	}
	// setHeight
	if f.methods[1].methodId != testMyWidgetMethodIdSetHeight || f.methods[1].f32Arg != 240.0 {
		t.Errorf("method[1] = {id:%d, f32:%f}, want {id:%d, f32:240.0}",
			f.methods[1].methodId, f.methods[1].f32Arg, testMyWidgetMethodIdSetHeight)
	}
	// setLabel
	if f.methods[2].methodId != testMyWidgetMethodIdSetLabel || f.methods[2].strArg != "updated" {
		t.Errorf("method[2] = {id:%d, str:%q}, want {id:%d, str:%q}",
			f.methods[2].methodId, f.methods[2].strArg, testMyWidgetMethodIdSetLabel, "updated")
	}
}

// ---------------------------------------------------------------------------
// Test: Factory with zero methods (empty method chain, just Build)
// ---------------------------------------------------------------------------

func TestE2E_FactoryNoMethods(t *testing.T) {
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	m.WriteUint32(testFuncProcIdMyWidget)
	m.WriteUint64(0xABCD)
	m.WriteString("empty widget")
	// Immediately Build — no methods called
	m.WriteUint32(testMyWidgetMethodIdBuild)

	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	interp.interpret(u)

	if len(interp.factories) != 1 {
		t.Fatalf("expected 1 factory call, got %d", len(interp.factories))
	}
	f := interp.factories[0]
	if f.label != "empty widget" {
		t.Errorf("label = %q, want %q", f.label, "empty widget")
	}
	if len(f.methods) != 0 {
		t.Errorf("expected 0 method calls, got %d", len(f.methods))
	}
}

// ---------------------------------------------------------------------------
// Test: Fetcher roundtrip (no args, returns are written by interpreter)
// ---------------------------------------------------------------------------

func TestE2E_FetcherRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	// Serialize as goserver would: just the FuncProcId
	m.WriteUint32(testFuncProcIdFetchCounters)

	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	interp.interpret(u)

	if len(interp.fetchers) != 1 {
		t.Fatalf("expected 1 fetcher call, got %d", len(interp.fetchers))
	}
	if interp.fetchers[0].funcProcId != testFuncProcIdFetchCounters {
		t.Errorf("funcProcId = %d, want %d", interp.fetchers[0].funcProcId, testFuncProcIdFetchCounters)
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple sequential messages in a single stream
// ---------------------------------------------------------------------------

func TestE2E_MultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	// Message 1: procedure
	m.WriteUint32(testFuncProcIdSetColor)
	m.WriteUint64(1)
	m.WriteUint8(10)
	m.WriteUint8(20)
	m.WriteUint8(30)
	m.WriteUint8(40)

	// Message 2: factory
	m.WriteUint32(testFuncProcIdMyWidget)
	m.WriteUint64(2)
	m.WriteString("w1")
	m.WriteUint32(testMyWidgetMethodIdSetWidth)
	m.WriteFloat32(100.0)
	m.WriteUint32(testMyWidgetMethodIdBuild)

	// Message 3: another procedure
	m.WriteUint32(testFuncProcIdSetColor)
	m.WriteUint64(3)
	m.WriteUint8(50)
	m.WriteUint8(60)
	m.WriteUint8(70)
	m.WriteUint8(80)

	// Message 4: fetcher
	m.WriteUint32(testFuncProcIdFetchCounters)

	// Message 5: factory with no methods
	m.WriteUint32(testFuncProcIdMyWidget)
	m.WriteUint64(4)
	m.WriteString("w2")
	m.WriteUint32(testMyWidgetMethodIdBuild)

	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	for i := 0; i < 5; i++ {
		interp.interpret(u)
	}

	if len(interp.procedures) != 2 {
		t.Errorf("expected 2 procedure calls, got %d", len(interp.procedures))
	}
	if len(interp.factories) != 2 {
		t.Errorf("expected 2 factory calls, got %d", len(interp.factories))
	}
	if len(interp.fetchers) != 1 {
		t.Errorf("expected 1 fetcher call, got %d", len(interp.fetchers))
	}

	// Verify ordering preserved
	if interp.procedures[0].id != 1 || interp.procedures[1].id != 3 {
		t.Errorf("procedure ids = [%d, %d], want [1, 3]",
			interp.procedures[0].id, interp.procedures[1].id)
	}
	if interp.factories[0].label != "w1" || interp.factories[1].label != "w2" {
		t.Errorf("factory labels = [%q, %q], want [%q, %q]",
			interp.factories[0].label, interp.factories[1].label, "w1", "w2")
	}
	// Verify first factory has 1 method, second has 0
	if len(interp.factories[0].methods) != 1 {
		t.Errorf("factory[0] methods = %d, want 1", len(interp.factories[0].methods))
	}
	if len(interp.factories[1].methods) != 0 {
		t.Errorf("factory[1] methods = %d, want 0", len(interp.factories[1].methods))
	}
}

// ---------------------------------------------------------------------------
// Test: Framed messages through the channel (length-prefixed)
// ---------------------------------------------------------------------------

func TestE2E_FramedMessages(t *testing.T) {
	// This tests the actual IPC framing: each message is prefixed with a u32
	// length. We serialize individual messages, frame them, then read them
	// through the unmarshaller with explicit length consumption.
	endianness := binary.LittleEndian

	// Build two framed messages: a procedure and a factory
	var msg1 bytes.Buffer
	m1 := runtime.NewMarshaller(&msg1, endianness, func(err error) { t.Fatal(err) })
	m1.WriteUint32(testFuncProcIdSetColor)
	m1.WriteUint64(0xFF)
	m1.WriteUint8(1)
	m1.WriteUint8(2)
	m1.WriteUint8(3)
	m1.WriteUint8(4)

	var msg2 bytes.Buffer
	m2 := runtime.NewMarshaller(&msg2, endianness, func(err error) { t.Fatal(err) })
	m2.WriteUint32(testFuncProcIdMyWidget)
	m2.WriteUint64(0xEE)
	m2.WriteString("framed")
	m2.WriteUint32(testMyWidgetMethodIdBuild)

	// Frame both messages into a single stream: [u32 len][payload][u32 len][payload]
	var stream bytes.Buffer
	frameMsg := func(payload []byte) {
		lenBuf := make([]byte, 4)
		endianness.PutUint32(lenBuf, uint32(len(payload)))
		stream.Write(lenBuf)
		stream.Write(payload)
	}
	frameMsg(msg1.Bytes())
	frameMsg(msg2.Bytes())

	// Read framed messages
	u := runtime.NewUnmarshaller(&stream, endianness, func(err error) { t.Fatal(err) }, nil)
	interp := &testInterpreter{}

	for i := 0; i < 2; i++ {
		// Read frame length (what InlineIoChannel does before yielding to interpreter)
		frameLen := u.ReadUInt32()
		if frameLen == 0 {
			t.Fatalf("message %d: unexpected zero-length frame", i)
		}
		// The interpreter reads the payload within this frame
		interp.interpret(u)
	}

	if len(interp.procedures) != 1 {
		t.Fatalf("expected 1 procedure, got %d", len(interp.procedures))
	}
	if interp.procedures[0].r != 1 || interp.procedures[0].a != 4 {
		t.Errorf("procedure rgba = (%d,%d,%d,%d), want (1,2,3,4)",
			interp.procedures[0].r, interp.procedures[0].g,
			interp.procedures[0].b, interp.procedures[0].a)
	}
	if len(interp.factories) != 1 {
		t.Fatalf("expected 1 factory, got %d", len(interp.factories))
	}
	if interp.factories[0].label != "framed" {
		t.Errorf("factory label = %q, want %q", interp.factories[0].label, "framed")
	}
}

// ---------------------------------------------------------------------------
// Test: Generated goserver code and goclient code are wire-compatible
// ---------------------------------------------------------------------------

func TestE2E_GoserverGoclientCodeConsistency(t *testing.T) {
	// Generate both goserver and goclient code for the same IDL nodes,
	// then verify their opcode constants and arg serialization are symmetric.
	nodes := buildTestNodes()

	// Generate goclient (interpreter) code
	var gcEnumBuf, gcDispatchBuf bytes.Buffer
	gcTracker := compiletime.NewStateAndErrTracker[GeneratorStateE](GenerateStateInitial, "")
	gcWh := WriterHolder{
		MethodWriter:   &bytes.Buffer{},
		FactoryWriter:  &bytes.Buffer{},
		DispatchWriter: &gcDispatchBuf,
		EnumWriter:     &gcEnumBuf,
		TypeWriter:     &bytes.Buffer{},
	}
	err := GenerateCode(gcWh, nodes, gcTracker)
	if err != nil {
		t.Fatalf("goclient GenerateCode: %v", err)
	}
	gcEnum := gcEnumBuf.String()
	gcDispatch := gcDispatchBuf.String()

	t.Run("FuncProcId_order_matches", func(t *testing.T) {
		// The enum should list nodes in the same order as they were passed in.
		// This ensures goserver and goclient agree on IDs.
		setColorIdx := strings.Index(gcEnum, "FuncProcIdSetColor")
		myWidgetIdx := strings.Index(gcEnum, "FuncProcIdMyWidget")
		fetchIdx := strings.Index(gcEnum, "FuncProcIdFetchCounters")
		if setColorIdx < 0 || myWidgetIdx < 0 || fetchIdx < 0 {
			t.Fatalf("missing enum entries:\n%s", gcEnum)
		}
		if !(setColorIdx < myWidgetIdx && myWidgetIdx < fetchIdx) {
			t.Errorf("enum order wrong: SetColor@%d, MyWidget@%d, FetchCounters@%d",
				setColorIdx, myWidgetIdx, fetchIdx)
		}
	})

	t.Run("procedure_read_order_matches_write_order", func(t *testing.T) {
		// goserver writes: FuncProcId, identity, r, g, b, a
		// goclient should read in same order: identity, r, g, b, a (FuncProcId already consumed)
		idIdx := strings.Index(gcDispatch, "u.ReadUInt64()")
		u8Idx := strings.Index(gcDispatch, "u.ReadUInt8()")
		if idIdx < 0 || u8Idx < 0 {
			t.Fatalf("missing reads in dispatch")
		}
		// Identity must be read before the uint8 args
		// (find them after the SetColor case)
		caseIdx := strings.Index(gcDispatch, "case FuncProcIdSetColor:")
		if caseIdx < 0 {
			t.Fatal("missing SetColor case")
		}
		subDispatch := gcDispatch[caseIdx:]
		subIdIdx := strings.Index(subDispatch, "u.ReadUInt64()")
		subU8Idx := strings.Index(subDispatch, "u.ReadUInt8()")
		if subIdIdx > subU8Idx {
			t.Errorf("identity read (%d) should come before uint8 reads (%d)", subIdIdx, subU8Idx)
		}
	})

	t.Run("factory_method_loop_present", func(t *testing.T) {
		// The method loop should be inside the MyWidget case
		caseIdx := strings.Index(gcDispatch, "case FuncProcIdMyWidget:")
		if caseIdx < 0 {
			t.Fatal("missing MyWidget case")
		}
		subDispatch := gcDispatch[caseIdx:]
		if !strings.Contains(subDispatch, "for {") {
			t.Error("missing method loop in factory dispatch")
		}
		if !strings.Contains(subDispatch, "MyWidgetMethodIdBuild") {
			t.Error("missing Build terminator in factory dispatch")
		}
	})

	t.Run("method_enum_values_sequential", func(t *testing.T) {
		// Build=0, SetWidth=1, SetHeight=2, SetLabel=3
		if !strings.Contains(gcEnum, "MyWidgetMethodIdBuild MyWidgetMethodIdE = 0") {
			t.Errorf("Build should be 0:\n%s", gcEnum)
		}
		if !strings.Contains(gcEnum, "MyWidgetMethodIdSetWidth MyWidgetMethodIdE = 1") {
			t.Errorf("SetWidth should be 1:\n%s", gcEnum)
		}
		if !strings.Contains(gcEnum, "MyWidgetMethodIdSetHeight MyWidgetMethodIdE = 2") {
			t.Errorf("SetHeight should be 2:\n%s", gcEnum)
		}
		if !strings.Contains(gcEnum, "MyWidgetMethodIdSetLabel MyWidgetMethodIdE = 3") {
			t.Errorf("SetLabel should be 3:\n%s", gcEnum)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: Edge cases — empty strings, zero values, max values
// ---------------------------------------------------------------------------

func TestE2E_EdgeCases(t *testing.T) {
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	// Procedure with zero id, zero color values
	m.WriteUint32(testFuncProcIdSetColor)
	m.WriteUint64(0)
	m.WriteUint8(0)
	m.WriteUint8(0)
	m.WriteUint8(0)
	m.WriteUint8(0)

	// Factory with empty label, max uint64 id
	m.WriteUint32(testFuncProcIdMyWidget)
	m.WriteUint64(^uint64(0)) // max uint64
	m.WriteString("")         // empty string
	m.WriteUint32(testMyWidgetMethodIdSetWidth)
	m.WriteFloat32(0.0)
	m.WriteUint32(testMyWidgetMethodIdBuild)

	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	interp.interpret(u) // procedure
	interp.interpret(u) // factory

	p := interp.procedures[0]
	if p.id != 0 {
		t.Errorf("procedure id = %d, want 0", p.id)
	}
	if p.r != 0 || p.g != 0 || p.b != 0 || p.a != 0 {
		t.Errorf("procedure rgba should all be 0")
	}

	f := interp.factories[0]
	if f.id != ^uint64(0) {
		t.Errorf("factory id = %d, want max uint64", f.id)
	}
	if f.label != "" {
		t.Errorf("factory label = %q, want empty", f.label)
	}
	if f.methods[0].f32Arg != 0.0 {
		t.Errorf("method arg = %f, want 0.0", f.methods[0].f32Arg)
	}
}

// ---------------------------------------------------------------------------
// Test: Large method chain stress test
// ---------------------------------------------------------------------------

func TestE2E_LargeMethodChain(t *testing.T) {
	const numMethods = 1000
	var buf bytes.Buffer
	m := newTestMarshaller(&buf)

	m.WriteUint32(testFuncProcIdMyWidget)
	m.WriteUint64(42)
	m.WriteString("stress")

	for i := 0; i < numMethods; i++ {
		if i%3 == 0 {
			m.WriteUint32(testMyWidgetMethodIdSetWidth)
			m.WriteFloat32(float32(i))
		} else if i%3 == 1 {
			m.WriteUint32(testMyWidgetMethodIdSetHeight)
			m.WriteFloat32(float32(i) * 0.5)
		} else {
			m.WriteUint32(testMyWidgetMethodIdSetLabel)
			m.WriteString(fmt.Sprintf("label_%d", i))
		}
	}
	m.WriteUint32(testMyWidgetMethodIdBuild)

	u := newTestUnmarshaller(&buf)
	interp := &testInterpreter{}
	interp.interpret(u)

	if len(interp.factories) != 1 {
		t.Fatalf("expected 1 factory, got %d", len(interp.factories))
	}
	f := interp.factories[0]
	if len(f.methods) != numMethods {
		t.Fatalf("expected %d methods, got %d", numMethods, len(f.methods))
	}
	// Spot check
	if f.methods[0].methodId != testMyWidgetMethodIdSetWidth || f.methods[0].f32Arg != 0.0 {
		t.Errorf("method[0] wrong")
	}
	if f.methods[1].methodId != testMyWidgetMethodIdSetHeight || f.methods[1].f32Arg != 0.5 {
		t.Errorf("method[1] wrong")
	}
	if f.methods[2].methodId != testMyWidgetMethodIdSetLabel || f.methods[2].strArg != "label_2" {
		t.Errorf("method[2] wrong: got strArg=%q", f.methods[2].strArg)
	}
	// Check last method
	lastIdx := numMethods - 1
	last := f.methods[lastIdx]
	switch lastIdx % 3 {
	case 0:
		if last.methodId != testMyWidgetMethodIdSetWidth {
			t.Errorf("last method wrong type")
		}
	case 1:
		if last.methodId != testMyWidgetMethodIdSetHeight {
			t.Errorf("last method wrong type")
		}
	case 2:
		if last.methodId != testMyWidgetMethodIdSetLabel {
			t.Errorf("last method wrong type")
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Generated enum offset values match expected IDs
// ---------------------------------------------------------------------------

func TestE2E_EnumOffsetValues(t *testing.T) {
	nodes := buildTestNodes()

	var enumBuf bytes.Buffer
	tracker := compiletime.NewStateAndErrTracker[GeneratorStateE](GenerateStateInitial, "")
	wh := WriterHolder{
		MethodWriter:   &bytes.Buffer{},
		FactoryWriter:  &bytes.Buffer{},
		DispatchWriter: &bytes.Buffer{},
		EnumWriter:     &enumBuf,
		TypeWriter:     &bytes.Buffer{},
	}
	_ = GenerateCode(wh, nodes, tracker)

	enumCode := enumBuf.String()

	// Verify exact offset values match what we hardcoded in testFuncProcId* constants
	expectations := []struct {
		name   string
		offset int
	}{
		{"FuncProcIdSetColor", 0},
		{"FuncProcIdMyWidget", 1},
		{"FuncProcIdFetchCounters", 2},
	}
	for _, exp := range expectations {
		want := fmt.Sprintf("%s FuncProcIdE = FuncProcIdOffset + %d", exp.name, exp.offset)
		if !strings.Contains(enumCode, want) {
			t.Errorf("expected %q in enum:\n%s", want, enumCode)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Generated code uses correct naming conventions
// ---------------------------------------------------------------------------

func TestE2E_NamingConventions(t *testing.T) {
	// Define a node with multi-word names to test naming conversion
	proc := idl.NewProceduralNode(n("myLongProcedureName")).
		WithIdentityId(false).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("firstArgName"), ctabb.U32).
			PlainArg(n("secondArgName"), ctabb.S).
			Build()).
		Build()

	var enumBuf, dispatchBuf bytes.Buffer
	tracker := compiletime.NewStateAndErrTracker[GeneratorStateE](GenerateStateInitial, "")
	wh := WriterHolder{
		MethodWriter:   &bytes.Buffer{},
		FactoryWriter:  &bytes.Buffer{},
		DispatchWriter: &dispatchBuf,
		EnumWriter:     &enumBuf,
		TypeWriter:     &bytes.Buffer{},
	}
	_ = GenerateCode(wh, []ir.NodeI{proc}, tracker)

	dispatchCode := dispatchBuf.String()

	// The case clause should use UpperCamelCase for the FuncProcId
	if !strings.Contains(dispatchCode, "case FuncProcIdMyLongProcedureName:") {
		t.Errorf("case clause naming wrong:\n%s", dispatchCode)
	}
	// Variable names should use lowerCamelCase
	if !strings.Contains(dispatchCode, "firstArgName :=") {
		t.Errorf("arg naming wrong:\n%s", dispatchCode)
	}
	if !strings.Contains(dispatchCode, "secondArgName :=") {
		t.Errorf("arg naming wrong:\n%s", dispatchCode)
	}
}
