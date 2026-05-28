//go:build llm_generated_opus46

package goclient

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
)

func n(s string) naming.StylableName {
	return naming.MustBeValidStylableName(s)
}

func TestCanonicalTypeToReadMethod(t *testing.T) {
	tests := []struct {
		typ      canonicaltypes.PrimitiveAstNodeI
		expected string
	}{
		{ctabb.U8, "ReadUInt8"},
		{ctabb.U16, "ReadUInt16"},
		{ctabb.U32, "ReadUInt32"},
		{ctabb.U64, "ReadUInt64"},
		{ctabb.I8, "ReadInt8"},
		{ctabb.I16, "ReadInt16"},
		{ctabb.I32, "ReadInt32"},
		{ctabb.I64, "ReadInt64"},
		{ctabb.F32, "ReadFloat32"},
		{ctabb.F64, "ReadFloat64"},
		{ctabb.B, "ReadBool"},
		{ctabb.S, "ReadString"},
	}
	for _, tt := range tests {
		got, err := canonicalTypeToReadMethod(tt.typ)
		if err != nil {
			t.Errorf("canonicalTypeToReadMethod(%s): unexpected error: %v", tt.typ, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("canonicalTypeToReadMethod(%s) = %q, want %q", tt.typ, got, tt.expected)
		}
	}
}

func TestGenerateCodeProcedure(t *testing.T) {
	proc := idl.NewProceduralNode(n("myProc")).
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("count"), ctabb.U32).
			PlainArg(n("name"), ctabb.S).
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
	err := GenerateCode(wh, []ir.NodeI{proc}, tracker)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	enumCode := enumBuf.String()
	dispatchCode := dispatchBuf.String()

	// Check enum
	if !strings.Contains(enumCode, "FuncProcIdMyProc") {
		t.Errorf("enum missing FuncProcIdMyProc:\n%s", enumCode)
	}

	// Check dispatch reads identity
	if !strings.Contains(dispatchCode, "u.ReadUInt64()") {
		t.Errorf("dispatch missing identity read:\n%s", dispatchCode)
	}
	// Check dispatch reads plain args
	if !strings.Contains(dispatchCode, "u.ReadUInt32()") {
		t.Errorf("dispatch missing uint32 read:\n%s", dispatchCode)
	}
	if !strings.Contains(dispatchCode, "u.ReadString()") {
		t.Errorf("dispatch missing string read:\n%s", dispatchCode)
	}
	// Check case clause
	if !strings.Contains(dispatchCode, "case FuncProcIdMyProc:") {
		t.Errorf("dispatch missing case clause:\n%s", dispatchCode)
	}
}

func TestGenerateCodeFactory(t *testing.T) {
	mb := idl.NewMethodBuilder()
	mb.BeginMethod(n("setWidth")).Arg(n("width"), ctabb.F32).EndMethod()
	mb.BeginMethod(n("setHeight")).Arg(n("height"), ctabb.F32).EndMethod()

	factory := idl.NewBuilderFactoryNode(n("myWidget")).
		WithIdentityId(true).
		WithSettingImmediate(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("label"), ctabb.S).
			Build()).
		AddMethods(mb.Build()...).
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
	err := GenerateCode(wh, []ir.NodeI{factory}, tracker)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	enumCode := enumBuf.String()
	dispatchCode := dispatchBuf.String()

	// Check enum
	if !strings.Contains(enumCode, "FuncProcIdMyWidget") {
		t.Errorf("enum missing FuncProcIdMyWidget:\n%s", enumCode)
	}
	if !strings.Contains(enumCode, "MyWidgetMethodIdBuild") {
		t.Errorf("enum missing MyWidgetMethodIdBuild:\n%s", enumCode)
	}
	if !strings.Contains(enumCode, "MyWidgetMethodIdSetWidth") {
		t.Errorf("enum missing MyWidgetMethodIdSetWidth:\n%s", enumCode)
	}
	if !strings.Contains(enumCode, "MyWidgetMethodIdSetHeight") {
		t.Errorf("enum missing MyWidgetMethodIdSetHeight:\n%s", enumCode)
	}

	// Check dispatch has method loop
	if !strings.Contains(dispatchCode, "for {") {
		t.Errorf("dispatch missing method loop:\n%s", dispatchCode)
	}
	if !strings.Contains(dispatchCode, "MyWidgetMethodIdBuild") {
		t.Errorf("dispatch missing Build case:\n%s", dispatchCode)
	}
	if !strings.Contains(dispatchCode, "u.ReadFloat32()") {
		t.Errorf("dispatch missing float32 read:\n%s", dispatchCode)
	}
}

func TestGenerateCodeFetcher(t *testing.T) {
	fetcher := idl.NewFetcherNode(n("fetchState")).
		AddReturnValue(n("count"), ctabb.U32).
		AddReturnValue(n("total"), ctabb.U64).
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
	err := GenerateCode(wh, []ir.NodeI{fetcher}, tracker)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	enumCode := enumBuf.String()
	dispatchCode := dispatchBuf.String()

	if !strings.Contains(enumCode, "FuncProcIdFetchState") {
		t.Errorf("enum missing FuncProcIdFetchState:\n%s", enumCode)
	}
	if !strings.Contains(dispatchCode, "case FuncProcIdFetchState:") {
		t.Errorf("dispatch missing case clause:\n%s", dispatchCode)
	}
}

func TestWireRoundtripProcedure(t *testing.T) {
	// Simulate the wire protocol: marshal a procedure call, then unmarshal it
	// using the same format the generated interpreter would read.
	var buf bytes.Buffer
	endianness := binary.LittleEndian
	m := runtime.NewMarshaller(&buf, endianness, func(err error) { t.Fatal(err) })

	// Write: FuncProcId (uint32) + identity (uint64) + plain args (uint32, string)
	const funcProcId uint32 = 42
	const id uint64 = 0xDEADBEEF
	const count uint32 = 123
	const name = "hello"

	m.WriteUint32(funcProcId)
	m.WriteUint64(id)
	m.WriteUint32(count)
	m.WriteString(name)

	// Now read back
	u := runtime.NewUnmarshaller(&buf, endianness, func(err error) { t.Fatal(err) }, nil)

	gotFuncProcId := u.ReadUInt32()
	gotId := u.ReadUInt64()
	gotCount := u.ReadUInt32()
	gotName := u.ReadString()

	if gotFuncProcId != funcProcId {
		t.Errorf("FuncProcId = %d, want %d", gotFuncProcId, funcProcId)
	}
	if gotId != id {
		t.Errorf("Id = %d, want %d", gotId, id)
	}
	if gotCount != count {
		t.Errorf("count = %d, want %d", gotCount, count)
	}
	if gotName != name {
		t.Errorf("name = %q, want %q", gotName, name)
	}
}

func TestWireRoundtripFactoryWithMethods(t *testing.T) {
	var buf bytes.Buffer
	endianness := binary.LittleEndian
	m := runtime.NewMarshaller(&buf, endianness, func(err error) { t.Fatal(err) })

	// Simulate: FuncProcId + identity + label(string)
	//           + [MethodId=1 + width(f32)] + [MethodId=2 + height(f32)] + [MethodId=0 (Build)]
	const funcProcId uint32 = 10
	const id uint64 = 0x1234
	const label = "test widget"
	const methodIdSetWidth uint32 = 1
	const methodIdSetHeight uint32 = 2
	const methodIdBuild uint32 = 0
	var width float32 = 3.14
	var height float32 = 2.71

	m.WriteUint32(funcProcId)
	m.WriteUint64(id)
	m.WriteString(label)
	// Method 1: setWidth
	m.WriteUint32(methodIdSetWidth)
	m.WriteFloat32(width)
	// Method 2: setHeight
	m.WriteUint32(methodIdSetHeight)
	m.WriteFloat32(height)
	// Build terminator
	m.WriteUint32(methodIdBuild)

	// Read back
	u := runtime.NewUnmarshaller(&buf, endianness, func(err error) { t.Fatal(err) }, nil)

	gotFuncProcId := u.ReadUInt32()
	gotId := u.ReadUInt64()
	gotLabel := u.ReadString()

	if gotFuncProcId != funcProcId {
		t.Errorf("FuncProcId = %d, want %d", gotFuncProcId, funcProcId)
	}
	if gotId != id {
		t.Errorf("Id = %d, want %d", gotId, id)
	}
	if gotLabel != label {
		t.Errorf("label = %q, want %q", gotLabel, label)
	}

	// Read method loop
	type methodCall struct {
		id  uint32
		arg float32
	}
	var calls []methodCall
	for {
		mid := u.ReadUInt32()
		if mid == methodIdBuild {
			break
		}
		arg := u.ReadFloat32()
		calls = append(calls, methodCall{id: mid, arg: arg})
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 method calls, got %d", len(calls))
	}
	if calls[0].id != methodIdSetWidth || calls[0].arg != width {
		t.Errorf("call[0] = {%d, %f}, want {%d, %f}", calls[0].id, calls[0].arg, methodIdSetWidth, width)
	}
	if calls[1].id != methodIdSetHeight || calls[1].arg != height {
		t.Errorf("call[1] = {%d, %f}, want {%d, %f}", calls[1].id, calls[1].arg, methodIdSetHeight, height)
	}
}

func TestWireRoundtripSignedIntegers(t *testing.T) {
	// Test sign-magnitude encoding roundtrip
	var buf bytes.Buffer
	endianness := binary.LittleEndian
	m := runtime.NewMarshaller(&buf, endianness, func(err error) { t.Fatal(err) })

	m.WriteInt8(-42)
	m.WriteInt16(-1000)
	m.WriteInt32(-100000)
	m.WriteInt64(-9999999999)
	m.WriteInt8(42)
	m.WriteInt16(1000)
	m.WriteInt32(100000)
	m.WriteInt64(9999999999)

	u := runtime.NewUnmarshaller(&buf, endianness, func(err error) { t.Fatal(err) }, nil)

	if v := u.ReadInt8(); v != -42 {
		t.Errorf("ReadInt8 = %d, want -42", v)
	}
	if v := u.ReadInt16(); v != -1000 {
		t.Errorf("ReadInt16 = %d, want -1000", v)
	}
	if v := u.ReadInt32(); v != -100000 {
		t.Errorf("ReadInt32 = %d, want -100000", v)
	}
	if v := u.ReadInt64(); v != -9999999999 {
		t.Errorf("ReadInt64 = %d, want -9999999999", v)
	}
	if v := u.ReadInt8(); v != 42 {
		t.Errorf("ReadInt8 = %d, want 42", v)
	}
	if v := u.ReadInt16(); v != 1000 {
		t.Errorf("ReadInt16 = %d, want 1000", v)
	}
	if v := u.ReadInt32(); v != 100000 {
		t.Errorf("ReadInt32 = %d, want 100000", v)
	}
	if v := u.ReadInt64(); v != 9999999999 {
		t.Errorf("ReadInt64 = %d, want 9999999999", v)
	}
}

func TestWireRoundtripAllTypes(t *testing.T) {
	var buf bytes.Buffer
	endianness := binary.LittleEndian
	m := runtime.NewMarshaller(&buf, endianness, func(err error) { t.Fatal(err) })

	m.WriteBool(true)
	m.WriteBool(false)
	m.WriteUint8(255)
	m.WriteUint16(65535)
	m.WriteUint32(4294967295)
	m.WriteUint64(18446744073709551615)
	m.WriteFloat32(3.14)
	m.WriteFloat64(2.718281828459045)
	m.WriteString("hello world")
	m.WriteString("")

	u := runtime.NewUnmarshaller(&buf, endianness, func(err error) { t.Fatal(err) }, nil)

	if v := u.ReadBool(); v != true {
		t.Errorf("ReadBool = %v, want true", v)
	}
	if v := u.ReadBool(); v != false {
		t.Errorf("ReadBool = %v, want false", v)
	}
	if v := u.ReadUInt8(); v != 255 {
		t.Errorf("ReadUInt8 = %d, want 255", v)
	}
	if v := u.ReadUInt16(); v != 65535 {
		t.Errorf("ReadUInt16 = %d, want 65535", v)
	}
	if v := u.ReadUInt32(); v != 4294967295 {
		t.Errorf("ReadUInt32 = %d, want 4294967295", v)
	}
	if v := u.ReadUInt64(); v != 18446744073709551615 {
		t.Errorf("ReadUInt64 = %d, want 18446744073709551615", v)
	}
	if v := u.ReadFloat32(); v != 3.14 {
		t.Errorf("ReadFloat32 = %f, want 3.14", v)
	}
	if v := u.ReadFloat64(); v != 2.718281828459045 {
		t.Errorf("ReadFloat64 = %f, want 2.718281828459045", v)
	}
	if v := u.ReadString(); v != "hello world" {
		t.Errorf("ReadString = %q, want %q", v, "hello world")
	}
	if v := u.ReadString(); v != "" {
		t.Errorf("ReadString = %q, want %q", v, "")
	}
}
