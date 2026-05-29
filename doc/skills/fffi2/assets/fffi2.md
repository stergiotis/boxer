---
type: reference
audience: agent reading this skill asset
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
Below is the public API surface of the library. Function bodies are stubbed (bodies replaced with `panic("stub")`) -- ignore this, it is an artifact of the export process. Your job is to write code that consumes this API.

--- FILE: compiletime/fffi2_compiletime_common.go ---
```go
package compiletime

import (
	_ "errors"
	_ "fmt"
	_ "iter"
	_ "math/bits"
	_ "slices"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/pebble2impl/public/compiletimeflags"
	"golang.org/x/exp/constraints"
	_ "golang.org/x/exp/constraints"
)

var ErrInvalidState = eh.Errorf("builder is in wrong state")

type StateAndErrTracker[T constraints.Unsigned] struct {
	ErrorMessagePrefix string
}

func NewStateAndErrTracker[T constraints.Unsigned](initial T, errorMessagePrefix string) *StateAndErrTracker[T] {
	panic("stub")
}

func (inst *StateAndErrTracker[T]) ResetStateAndError() { panic("stub") }

func (inst *StateAndErrTracker[T]) SetTransitionActionPost(src T, dest T, action func()) {
	panic("stub")
}

func (inst *StateAndErrTracker[T]) SetReachActionPost(dest T, action func()) { panic("stub") }

func (inst *StateAndErrTracker[T]) GetState() T { panic("stub") }

func (inst *StateAndErrTracker[T]) MergeError(err error) { panic("stub") }

func (inst *StateAndErrTracker[T]) CheckAndTransitionState(destState T, allowed T) (srcState T) {
	panic("stub")
}

func (inst *StateAndErrTracker[T]) CheckState(allowed T) { panic("stub") }

func (inst *StateAndErrTracker[T]) Check(destSate T, allowedStates T) (err error) { panic("stub") }


```

--- FILE: compiletime/goserver/fffi2_compiletime_go_server.go ---
```go
package goserver

import (
	_ "fmt"
	"io"
	_ "io"
	_ "iter"
	_ "slices"

	_ "github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/containers/ragged"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/compiletime"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

var ReservedMethodNames = []naming.StylableName{
	"Send",
	"Keep",
}

type GeneratorStateE uint8

const (
	GenerateStateInitial GeneratorStateE = 1 << 0
	GenerateStateChecked GeneratorStateE = 1 << 1
)

type WriterHolder struct {
	MethodWriter  io.Writer
	FactoryWriter io.Writer
	FetcherWriter io.Writer
	EnumWriter    io.Writer
	TypeWriter    io.Writer
}

//	idDefer = "i.PopIdFromStackChecked(v)\n"

// FIXME

func GenerateCode(wh WriterHolder, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (err error) {
	panic("stub")
}


```

--- FILE: compiletime/rustclient/fffi2_compiletime_rust_client.go ---
```go
package rustclient

import (
	_ "bytes"
	_ "fmt"
	"io"
	_ "io"
	_ "iter"
	_ "slices"

	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/compiletime"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

var ReservedFuncProcIds = []string{
	"EndFrame",
}

type GeneratorStateE uint8

const (
	GenerateStateInitial GeneratorStateE = 1 << 0
	GenerateStateChecked GeneratorStateE = 1 << 1
)

type WriterHolder struct {
	MethodWriter   io.Writer
	FactoryWriter  io.Writer
	DispatchWriter io.Writer
	EnumWriter     io.Writer
	TypeWriter     io.Writer
}

var BuilderFactoryCodeGenExprs = ir.BuilderFactoryCodeGenExprs{
	InterpreterLifetime:        "'a",
	Id:                         "i",
	Instance:                   "w",
	SendMessage:                "self.io.flush().expect(\"unable to flush\");\n",
	MarkReturn:                 "r = true;\n",
	FuncProcIdOuter:            "f",
	MethodProcId:               "m",
	EguiContext:                "c",
	EguiUiOptionalOuter:        "u",
	InterpreterDepth:           "d",
	EndConsumeFrameIfNecessary: "if d == 0 {\nself.end_consume_message();\n}\n",

	InvokeInterpreterInner: `if u2.is_some() {
	self.interpret_inner(c,u2,&f2,d+1);
} else {
	self.interpret_inner(c,u,&f2,d+1);
}
`,
	FuncProcIdInner:     "f2",
	EguiUiOptionalInner: "u2",

	AtomsRegister0Reference:      "self.r0_atoms",
	AtomsRegister0Transfer:       "std::mem::take(&mut self.r0_atoms)\n",
	WidgetTextRegister0Reference: "self.r1_widget_text",
	WidgetTextRegister0Transfer:  "std::mem::take(&mut self.r1_widget_text)\n",
	Color32Register0Transfer:     "self.r11_color32\n",
}

// arguments

// construct

// no-op special case

// methods

// apply

// arguments

// apply

// apply

func GenerateCode(wh WriterHolder, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (err error) {
	panic("stub")
}


```

--- FILE: ir/fffi2_ir_codeloc.go ---
```go
package ir

import (
	_ "bytes"
	_ "io"
	_ "runtime"
)

const DefaultStackDepth = 0

type StackCapture struct {
	Files []string
	Lines []int
	Funcs []string
}

func NewStackCapture(skip int, depth int) (inst *StackCapture) { panic("stub") }

//t := false

type DefaultCodeS struct {
}

var DefaultCode = &DefaultCodeS{}

func (inst *DefaultCodeS) UseDefaultCode() bool { panic("stub") }

func (inst *DefaultCodeS) GetVerbatimCode() string { panic("stub") }

type EmptyCodeS struct {
}

var EmptyCode = &EmptyCodeS{}

func (inst *EmptyCodeS) UseDefaultCode() bool { panic("stub") }

func (inst *EmptyCodeS) GetVerbatimCode() string { panic("stub") }

type CodeLocationBufferWriter struct {
}

func NewCodeLocationBufferWriter(buf []byte) *CodeLocationBufferWriter { panic("stub") }

func (inst *CodeLocationBufferWriter) OverrideCodeLocation(loc *StackCapture) { panic("stub") }

func (inst *CodeLocationBufferWriter) Reset() { panic("stub") }

func (inst *CodeLocationBufferWriter) String() string { panic("stub") }

func (inst *CodeLocationBufferWriter) GetStack() (stack *StackCapture) { panic("stub") }

func (inst *CodeLocationBufferWriter) WriteString(s string) (n int, err error) { panic("stub") }

func (inst *CodeLocationBufferWriter) Write(p []byte) (n int, err error) { panic("stub") }

func (inst *CodeLocationBufferWriter) UseDefaultCode() bool { panic("stub") }

func (inst *CodeLocationBufferWriter) GetVerbatimCode() string { panic("stub") }


```

--- FILE: ir/fffi2_ir_enums.go ---
```go
package ir

const (
	LangGo   LangE = "go"
	LangRust LangE = "rust"
)


```

--- FILE: ir/fffi2_ir_impl.go ---
```go
package ir

import (
	_ "bytes"
	"iter"
	_ "iter"
	_ "slices"
	_ "strings"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/compiletimeflags"
)

func (inst AbstractType) IsAbstract() bool { panic("stub") }

func (inst AbstractType) GetName() naming.StylableName { panic("stub") }

func (inst AbstractType) ImplementedAbstractTypes() iter.Seq[AbstractType] { panic("stub") }

func NewAbstractType(name naming.StylableName) AbstractType { panic("stub") }

func (inst ConcreteType) IsAbstract() bool { panic("stub") }

func (inst ConcreteType) ImplementedAbstractTypes() iter.Seq[AbstractType] { panic("stub") }

func (inst ConcreteType) GetName() naming.StylableName { panic("stub") }

func (inst *BuilderFactoryNode) GetName() naming.StylableName { panic("stub") }

func (inst *ProceduralNode) GetName() naming.StylableName { panic("stub") }

func (inst EvaluatedArgumentSpec) IsEmpty() bool { panic("stub") }

func (inst EvaluatedArgumentSpec) Len() int { panic("stub") }

func (inst EvaluatedArgumentSpec) Iterate() iter.Seq2[naming.StylableName, TypeI] { panic("stub") }

func (inst PlainArgumentSpec) Iterate() iter.Seq2[naming.StylableName, canonicaltypes.PrimitiveAstNodeI] {
	panic("stub")
}

func (inst PlainArgumentSpec) IsEmpty() bool { panic("stub") }

func (inst PlainArgumentSpec) Len() int { panic("stub") }

func (inst *FetcherNode) GetName() naming.StylableName { panic("stub") }

func (inst *StringVerbatimCode) UseDefaultCode() bool { panic("stub") }

func (inst *StringVerbatimCode) GetVerbatimCode() string { panic("stub") }

func MergeVerbatimCode(code ...VerbatimCodeI) VerbatimCodeI { panic("stub") }


```

--- FILE: ir/fffi2_ir_types.go ---
```go
package ir

import (
	"iter"
	_ "iter"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/compiletimeflags"
)

type AbstractType struct {
}

type ConcreteType struct {
}

func NewConcreteType(name naming.StylableName, implementedAbstractTypes ...AbstractType) ConcreteType {
	panic("stub")
}

type TypeI interface {
	IsAbstract() bool
	GetName() naming.StylableName
	ImplementedAbstractTypes() iter.Seq[AbstractType]
}

type BuilderFactoryFeaturesSpec struct {
	Immediate     bool
	Retained      bool
	BlockIterator bool
}
type ProcedureFeaturesSpec struct {
	BlockIterator bool
}
type PlainArgumentSpec struct {
	Names []naming.StylableName
	Types []canonicaltypes.PrimitiveAstNodeI
}
type EvaluatedArgumentSpec struct {
	Names         []naming.StylableName
	AcceptedTypes []TypeI
}
type LangE string

type MethodSpec struct {
	Name           naming.StylableName
	PlainArguments PlainArgumentSpec
}
type Method struct {
	Spec       MethodSpec
	CodeHolder CodeHolder
}
type ArgumentSpec struct {
	EvaluatedArguments EvaluatedArgumentSpec
	PlainArguments     PlainArgumentSpec
}
type VerbatimCodeI interface {
	UseDefaultCode() bool
	GetVerbatimCode() string
}
type StringVerbatimCode struct {
	Default      bool
	VerbatimCode string
}

type CodeHolder struct {
	CodeClientRust VerbatimCodeI
	CodeServerGo   VerbatimCodeI
}
type IdentityArgumentSpec struct {
	HasId bool
}
type BuilderFactoryNode struct {
	Name              naming.StylableName
	IdentityArguments IdentityArgumentSpec
	Arguments         ArgumentSpec
	BuilderMethods    []Method
	Settings          BuilderFactoryFeaturesSpec
	ConstructionCode  CodeHolder
	ApplyCode         CodeHolder
	ReturnType        TypeI
}

type ProceduralNode struct {
	Name              naming.StylableName
	IdentityArguments IdentityArgumentSpec
	Arguments         ArgumentSpec
	Settings          ProcedureFeaturesSpec
	ApplyCode         CodeHolder
	ReturnType        TypeI
}

type FetcherNode struct {
	Name        naming.StylableName
	ApplyCode   CodeHolder
	ReturnTypes PlainArgumentSpec
}

type NodeI interface {
	GetName() naming.StylableName
}

type BuilderFactoryCodeGenExprs struct {
	InterpreterLifetime          string
	Id                           string
	Instance                     string
	SendMessage                  string
	MarkReturn                   string
	FuncProcIdOuter              string
	FuncProcIdInner              string
	MethodProcId                 string
	EguiContext                  string
	EguiUiOptionalOuter          string
	EguiUiOptionalInner          string
	EndConsumeFrameIfNecessary   string
	InterpreterDepth             string
	InvokeInterpreterInner       string
	AtomsRegister0Transfer       string
	AtomsRegister0Reference      string
	WidgetTextRegister0Reference string
	WidgetTextRegister0Transfer  string
	Color32Register0Transfer     string
}


```

--- FILE: ir/idl/fffi2_ir_idl_arguments.go ---
```go
package idl

import (
	_ "slices"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/ir"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

type ArgumentsBuilder struct {
}

func NewArgumentsBuilder() *ArgumentsBuilder { panic("stub") }

func (inst *ArgumentsBuilder) PlainArg(name naming.StylableName, typ canonicaltypes.PrimitiveAstNodeI) *ArgumentsBuilder {
	panic("stub")
}

func (inst *ArgumentsBuilder) EvaluatedArg(name naming.StylableName, acceptedTypes ir.TypeI) *ArgumentsBuilder {
	panic("stub")
}

func (inst *ArgumentsBuilder) Build() ir.ArgumentSpec { panic("stub") }


```

--- FILE: ir/idl/fffi2_ir_idl_common.go ---
```go
package idl

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)


```

--- FILE: ir/idl/fffi2_ir_idl_factory.go ---
```go
package idl

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/ir"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

type BuilderFactoryNodeBuilder struct {
}

func NewBuilderFactoryNode(name naming.StylableName) *BuilderFactoryNodeBuilder { panic("stub") }

func (inst *BuilderFactoryNodeBuilder) WithIdentityId(v bool) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithSettingImmediate(v bool) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithSettingRetained(v bool) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithSettingBlockIterator(v bool) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithReturnType(v ir.TypeI) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithConstructionCodeClientRust(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithConstructionCodeServerGo(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithApplyCodeClientRust(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) WithApplyCodeServerGo(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) AddArguments(spec ir.ArgumentSpec) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) AddMethods(mths ...ir.Method) *BuilderFactoryNodeBuilder {
	panic("stub")
}

func (inst *BuilderFactoryNodeBuilder) Build() *ir.BuilderFactoryNode { panic("stub") }


```

--- FILE: ir/idl/fffi2_ir_idl_fetcher.go ---
```go
package idl

import (
	_ "slices"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/ir"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

type FetcherNodeBuilder struct {
}

func NewFetcherNode(name naming.StylableName) *FetcherNodeBuilder { panic("stub") }

func (inst *FetcherNodeBuilder) WithApplyCodeClientRust(code ir.VerbatimCodeI) *FetcherNodeBuilder {
	panic("stub")
}

func (inst *FetcherNodeBuilder) WithApplyCodeServerGo(code ir.VerbatimCodeI) *FetcherNodeBuilder {
	panic("stub")
}

func (inst *FetcherNodeBuilder) AddReturnValue(name naming.StylableName, typ canonicaltypes.PrimitiveAstNodeI) *FetcherNodeBuilder {
	panic("stub")
}

func (inst *FetcherNodeBuilder) Build() *ir.FetcherNode { panic("stub") }


```

--- FILE: ir/idl/fffi2_ir_idl_method.go ---
```go
package idl

import (
	_ "slices"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/containers/ragged"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/ir"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

type MethodBuilderStateE uint8

const (
	MethodBuilderStateInitial  MethodBuilderStateE = 0
	MethodBuilderStateInMethod MethodBuilderStateE = 1
)

type MethodBuilder struct {
}

func NewMethodBuilder() *MethodBuilder { panic("stub") }

func (inst *MethodBuilder) Merge(mths ...ir.Method) *MethodBuilder { panic("stub") }

func (inst *MethodBuilder) BeginMethod(name naming.StylableName) *MethodBuilder { panic("stub") }

func (inst *MethodBuilder) Arg(name naming.StylableName, typ canonicaltypes.PrimitiveAstNodeI) *MethodBuilder {
	panic("stub")
}

func (inst *MethodBuilder) CodeClientRust(code ir.VerbatimCodeI) *MethodBuilder { panic("stub") }

func (inst *MethodBuilder) CodeServerGo(code ir.VerbatimCodeI) *MethodBuilder { panic("stub") }

func (inst *MethodBuilder) EndMethod() *MethodBuilder { panic("stub") }

func (inst *MethodBuilder) Build() []ir.Method { panic("stub") }

func (inst *MethodBuilder) BuildOne() ir.Method { panic("stub") }


```

--- FILE: ir/idl/fffi2_ir_idl_procedure.go ---
```go
package idl

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/ir"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/ir"
)

type ProceduralNodeBuilder struct {
}

func NewProceduralNode(name naming.StylableName) *ProceduralNodeBuilder { panic("stub") }

func (inst *ProceduralNodeBuilder) WithIdentityId(v bool) *ProceduralNodeBuilder { panic("stub") }

func (inst *ProceduralNodeBuilder) WithSettingBlockIterator(v bool) *ProceduralNodeBuilder {
	panic("stub")
}

func (inst *ProceduralNodeBuilder) WithReturnType(v ir.TypeI) *ProceduralNodeBuilder { panic("stub") }

func (inst *ProceduralNodeBuilder) WithApplyCodeClientRust(code ir.VerbatimCodeI) *ProceduralNodeBuilder {
	panic("stub")
}

func (inst *ProceduralNodeBuilder) WithApplyCodeServerGo(code ir.VerbatimCodeI) *ProceduralNodeBuilder {
	panic("stub")
}

func (inst *ProceduralNodeBuilder) AddArguments(spec ir.ArgumentSpec) *ProceduralNodeBuilder {
	panic("stub")
}

func (inst *ProceduralNodeBuilder) Build() *ir.ProceduralNode { panic("stub") }


```

--- FILE: runtime/fffi2_rt_channel.go ---
```go
package runtime

import (
	"bufio"
	_ "bufio"
	"encoding/binary"
	_ "encoding/binary"
	"iter"
	_ "iter"

	_ "github.com/stergiotis/boxer/public/ea"
)

type InlineIoChannel[U UnmarshallReaderI] struct {
}

func (inst *InlineIoChannel[U]) ReceiveMsg() iter.Seq[U] { panic("stub") }

//inst.readOffset += int64(inst.sz.Size)
//inst.sz.Reset()

//delta := inst.sz.Size
//e := log.Info().Int64("offset", inst.readOffset).
//	Uint64("deltaBytes", delta)
//if delta%4 == 0 {
//	e.Uint64("delta32BitUnits", delta/4)
//}
//if delta%8 == 0 {
//	e.Uint64("delta64BitUnits", delta/8)
//}
//e.Msg("received message")

var DefaultErrorHandler = func(err error) { panic("stub") }

var DefaultAllocator = func(l uint32) []byte { panic("stub") }

func NewInlineIoChannel[U UnmarshallReaderI](unmarshaller U, in *bufio.Reader, out *bufio.Writer, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) (inst *InlineIoChannel[U]) {
	panic("stub")
}

func (inst *InlineIoChannel[U]) SetInOut(in *bufio.Reader, out *bufio.Writer) { panic("stub") }

//inst.unmarshall.SetInput(io.TeeReader(in, inst.sz))

func (inst *InlineIoChannel[U]) FlushMessages() { panic("stub") }

func (inst *InlineIoChannel[U]) SyncMultiUseMsg(id uint64, msg []byte) { panic("stub") }

func (inst *InlineIoChannel[U]) SendSingleUseMsg(msg []byte) { panic("stub") }

func (inst *InlineIoChannel[U]) Marshaller() *Marshaller { panic("stub") }


```

--- FILE: runtime/fffi2_rt_funcs_get.go ---
```go
package runtime

import (
	"iter"
	_ "iter"
)

func GetBoolRetr[D UnmarshallReaderI, T ~bool](unmarshaller D) (r T) { panic("stub") }

func GetUint8Retr[D UnmarshallReaderI, T ~uint8](unmarshaller D) (r T) { panic("stub") }

func GetUint16Retr[D UnmarshallReaderI, T ~uint16](unmarshaller D) (r T) { panic("stub") }

func GetUint32Retr[D UnmarshallReaderI, T ~uint32](unmarshaller D) (r T) { panic("stub") }

func GetUint64Retr[D UnmarshallReaderI, T ~uint64](unmarshaller D) (r T) { panic("stub") }

func GetStringRetr[D UnmarshallReaderI, T ~string](unmarshaller D) (r T) { panic("stub") }

func GetStringRetrMostLikelyEmpty[D UnmarshallReaderI, T ~string](unmarshaller D) (r T) {
	panic("stub")
}

func GetInt8Retr[D UnmarshallReaderI, T ~int8](unmarshaller D) (r T) { panic("stub") }

func GetInt16Retr[D UnmarshallReaderI, T ~int16](unmarshaller D) (r T) { panic("stub") }

func GetInt32Retr[D UnmarshallReaderI, T ~int32](unmarshaller D) (r T) { panic("stub") }

func GetInt64Retr[D UnmarshallReaderI, T ~int64](unmarshaller D) (r T) { panic("stub") }

func GetFloat32Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r T) { panic("stub") }

func GetFloat32Array3Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r [3]T) { panic("stub") }

func GetFloat32Array4Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r [4]T) { panic("stub") }

func GetFloat32Array2Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r [2]T) { panic("stub") }

func GetFloat64Retr[D UnmarshallReaderI, T ~float64](unmarshaller D) (r T) { panic("stub") }

func GetComplex64Retr[D UnmarshallReaderI, T ~complex64](unmarshaller D) (r T) { panic("stub") }

func GetComplex128Retr[D UnmarshallReaderI, T ~complex128](unmarshaller D) (r T) { panic("stub") }

func GetUintptrRetr[D UnmarshallReaderI, T ~uintptr](unmarshaller D) (r T) { panic("stub") }

func GetBytesRetr[D UnmarshallReaderI, T byte](unmarshaller D) (r []byte) { panic("stub") }

func GetBoolSliceRetr[D UnmarshallReaderI, T ~bool](unmarshaller D) (r []T) { panic("stub") }

func GetFloat32SliceRetr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r []T) { panic("stub") }

func GetFloat64SliceRetr[D UnmarshallReaderI, T ~float64](unmarshaller D) (r []T) { panic("stub") }

func GetUint8SliceRetr[D UnmarshallReaderI, T ~uint8](unmarshaller D) (r []T) { panic("stub") }

func GetUint16SliceRetr[D UnmarshallReaderI, T ~uint16](unmarshaller D) (r []T) { panic("stub") }

func GetUint32SliceRetr[D UnmarshallReaderI, T ~uint32](unmarshaller D) (r []T) { panic("stub") }

func GetUint64SliceRetr[D UnmarshallReaderI, T ~uint64](unmarshaller D) (r []T) { panic("stub") }

func IterateUint64SliceRetr[D UnmarshallReaderI, T ~uint64](unmarshaller D) iter.Seq[T] {
	panic("stub")
}

func IterateUint32SliceRetr[D UnmarshallReaderI, T ~uint32](unmarshaller D) iter.Seq[T] {
	panic("stub")
}

func IterateFloat64SliceRetr[D UnmarshallReaderI, T ~float64](unmarshaller D) iter.Seq[T] {
	panic("stub")
}

func IterateFloat32SliceRetr[D UnmarshallReaderI, T ~float32](unmarshaller D) iter.Seq[T] {
	panic("stub")
}

func IterateInt64SliceRetr[D UnmarshallReaderI, T ~int64](unmarshaller D) iter.Seq[T] { panic("stub") }

func IterateStringSliceRetr[D UnmarshallReaderI, T ~string](unmarshaller D) iter.Seq[T] {
	panic("stub")
}

func GetInt8SliceRetr[D UnmarshallReaderI, T ~int8](unmarshaller D) (r []T) { panic("stub") }

func GetInt16SliceRetr[D UnmarshallReaderI, T ~int16](unmarshaller D) (r []T) { panic("stub") }

func GetInt32SliceRetr[D UnmarshallReaderI, T ~int32](unmarshaller D) (r []T) { panic("stub") }

func GetInt64SliceRetr[D UnmarshallReaderI, T ~int64](unmarshaller D) (r []T) { panic("stub") }


```

--- FILE: runtime/fffi2_rt_funcs_put.go ---
```go
package runtime

func PutBoolArg[D MarshallWriterI, T ~bool](marshaller D, v T) { panic("stub") }

func PutRuneArg[D MarshallWriterI, T ~rune](marshaller D, v T) { panic("stub") }

func PutUint8Arg[D MarshallWriterI, T ~uint8](marshaller D, v T) { panic("stub") }

func PutUint16Arg[D MarshallWriterI, T ~uint16](marshaller D, v T) { panic("stub") }

func PutUint32Arg[D MarshallWriterI, T ~uint32](marshaller D, v T) { panic("stub") }

func PutUint64Arg[D MarshallWriterI, T ~uint64](marshaller D, v T) { panic("stub") }

func PutInt8Arg[D MarshallWriterI, T ~int8](marshaller D, v T) { panic("stub") }

func PutInt16Arg[D MarshallWriterI, T ~int16](marshaller D, v T) { panic("stub") }

func PutInt32Arg[D MarshallWriterI, T ~int32](marshaller D, v T) { panic("stub") }

func PutInt64Arg[D MarshallWriterI, T ~int64](marshaller D, v T) { panic("stub") }

func PutStringArg[D MarshallWriterI, T ~string](marshaller D, v T) { panic("stub") }

func PutBytesArg[D MarshallWriterI](marshaller D, v []byte) { panic("stub") }

func PutStringsArg[D MarshallWriterI, T ~string](marshaller D, vs []T) { panic("stub") }

func PutUintptrArg[D MarshallWriterI, T ~uintptr](marshaller D, v T) {
	panic(
		// FIXME pointer length
		"stub")
}

func PutFloat32Arg[D MarshallWriterI, T ~float32](marshaller D, v T) { panic("stub") }

func PutFloat64Array4Arg[D MarshallWriterI, T ~float64](marshaller D, v [4]T) { panic("stub") }

func PutBoolSliceArg[D MarshallWriterI, T ~bool](marshaller D, vs []T) { panic("stub") }

func PutUint8SliceArg[D MarshallWriterI, T ~uint8](marshaller D, vs []T) { panic("stub") }

func PutUint16SliceArg[D MarshallWriterI, T ~uint16](marshaller D, vs []T) { panic("stub") }

func PutUint32SliceArg[D MarshallWriterI, T ~uint32](marshaller D, vs []T) { panic("stub") }

func PutInt8SliceArg[D MarshallWriterI, T ~int8](marshaller D, vs []T) { panic("stub") }

func PutInt16SliceArg[D MarshallWriterI, T ~int16](marshaller D, vs []T) { panic("stub") }

func PutInt32SliceArg[D MarshallWriterI, T ~int32](marshaller D, vs []T) { panic("stub") }

func PutFloat32SliceArg[D MarshallWriterI, T ~float32](marshaller D, vs []T) { panic("stub") }

func PutFloat64SliceArg[D MarshallWriterI, T ~float64](marshaller D, vs []T) { panic("stub") }

func PutRuneSliceArg[D MarshallWriterI, T ~rune](marshaller D, vs []T) { panic("stub") }

func PutFloat32Array2Arg[D MarshallWriterI, T ~float32](marshaller D, v [2]T) { panic("stub") }

func PutFloat32Array3Arg[D MarshallWriterI, T ~float32](marshaller D, v [3]T) { panic("stub") }

func PutFloat32Array4Arg[D MarshallWriterI, T ~float32](marshaller D, v [4]T) { panic("stub") }

func PutFloat64Arg[D MarshallWriterI, T ~float64](marshaller D, v T) { panic("stub") }

func PutComplex64Arg[D MarshallWriterI, T ~complex64](marshaller D, v T) { panic("stub") }

func PutComplex128Arg[D MarshallWriterI, T ~complex128](marshaller D, v T) { panic("stub") }

func PutComplex64Array2Arg[D MarshallWriterI, T ~complex64](marshaller D, v [2]T) { panic("stub") }


```

--- FILE: runtime/fffi2_rt_impl.go ---
```go
package runtime

import (
	"iter"
	_ "iter"
)

func NewFffi2[U UnmarshallReaderI](channel ChannelI[U]) *Fffi2[U] { panic("stub") }

//func (inst *Fffi2[U]) readError() (err error) {
//	s := GetStringRetrMostLikelyEmpty[*Unmarshaller, string](inst.unmarshaller)
//	if s != "" {
//		err = eh.New(s)
//	}
//	return
//}

func (inst *Fffi2[U]) SyncRetained(id uint64, buf []byte) (err error) {
	panic(
		// return inst.channel.SyncRetained(id, buf)
		"stub")
}

func (inst *Fffi2[U]) SendIntermediate(buf []byte) (err error) { panic("stub") }

func (inst *Fffi2[U]) ReceiveMsg() iter.Seq[U] { panic("stub") }

func (inst *Fffi2[U]) CallFunctionMayThrow() (err error) { panic("stub") }

//err = inst.readError()

func (inst *Fffi2[U]) CallFunctionNoThrow() {
	panic(
		// no-op
		"stub")
}

func (inst *Fffi2[U]) PipelineProcedureNoThrow() {
	panic(
		// no-op
		"stub")
}


```

--- FILE: runtime/fffi2_rt_marshaller_raw_bin.go ---
```go
package runtime

import (
	"encoding/binary"
	_ "encoding/binary"
	"io"
	_ "io"
	_ "math"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/unsafeperf"
)

type Marshaller struct {
}

func NewMarshaller(w io.Writer, bin binary.ByteOrder, errHandler func(err error)) *Marshaller {
	panic("stub")
}

func (inst *Marshaller) ResetWrittenBytes() { panic("stub") }

func (inst *Marshaller) GetWrittenBytes() int { panic("stub") }

func (inst *Marshaller) WriteUint8(v uint8) { panic("stub") }

func (inst *Marshaller) WriteBool(v bool) { panic("stub") }

func (inst *Marshaller) WriteUint16(v uint16) { panic("stub") }

func (inst *Marshaller) WriteUint32(v uint32) { panic("stub") }

func (inst *Marshaller) WriteUint64(v uint64) { panic("stub") }

func (inst *Marshaller) WriteInt8(v int8)   { panic("stub") }

func (inst *Marshaller) WriteInt16(v int16) { panic("stub") }

func (inst *Marshaller) WriteInt32(v int32) { panic("stub") }

func (inst *Marshaller) WriteInt64(v int64) { panic("stub") }

func (inst *Marshaller) WriteFloat32(v float32) { panic("stub") }

func (inst *Marshaller) WriteFloat64(v float64) { panic("stub") }

func (inst *Marshaller) WriteComplex64(v complex64) { panic("stub") }

func (inst *Marshaller) WriteComplex128(v complex128) { panic("stub") }

func (inst *Marshaller) WriteString(v string) { panic("stub") }

func (inst *Marshaller) WriteVerbatim(v []byte) { panic("stub") }

func (inst *Marshaller) WriteBytes(v []byte) { panic("stub") }

func (inst *Marshaller) WriteSliceLength(l int) { panic("stub") }

func (inst *Marshaller) WriteNilSlice() { panic("stub") }


```

--- FILE: runtime/fffi2_rt_types.go ---
```go
package runtime

import (
	"encoding/binary"
	_ "encoding/binary"
	"io"
	_ "io"
	"iter"
	_ "iter"
)

type FuncProcId uint32

type ByteOrderI interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

type ChannelI[U UnmarshallReaderI] interface {
	SyncMultiUseMsg(id uint64, buf []byte)
	SendSingleUseMsg(buf []byte)
	ReceiveMsg() iter.Seq[U]
	FlushMessages()
}

type Fffi2[U UnmarshallReaderI] struct {
}

type MarshallWriterI interface {
	WriteUint8(v uint8)
	WriteBool(v bool)
	WriteUint16(v uint16)
	WriteUint32(v uint32)
	WriteUint64(v uint64)
	WriteInt8(v int8)
	WriteInt16(v int16)
	WriteInt32(v int32)
	WriteInt64(v int64)
	WriteFloat32(v float32)
	WriteFloat64(v float64)
	WriteComplex64(v complex64)
	WriteComplex128(v complex128)
	WriteString(v string)
	WriteBytes(v []byte)
	WriteSliceLength(l int)
	WriteNilSlice()
}
type UnmarshallReaderI interface {
	SetInput(r io.Reader)
	SetEndianness(endi binary.ByteOrder)
	SetErrorHandler(f func(err error))
	SetAllocateBufferFunc(f func(l uint32) []byte)

	ReadUInt8() (v uint8)
	ReadUInt16() (v uint16)
	ReadUInt32MostLikelyZero() (v uint32)
	ReadUInt32() (v uint32)
	ReadUInt64() (v uint64)
	ReadInt8() (v int8)
	ReadInt16() (v int16)
	ReadInt32() (v int32)
	ReadInt64() (v int64)
	ReadFloat32() (v float32)
	ReadFloat64() (v float64)
	ReadComplex64() (v complex64)
	ReadComplex128() (v complex128)
	ReadUintptr() (v uintptr)
	ReadString() (v string)
	ReadStringMostLikelyEmpty() (v string)
	ReadBytes() (v []byte)
	ReadBool() (v bool)
	ReadSliceLength() (l int)
}


```

--- FILE: runtime/fffi2_rt_unmarshaller_raw_bin.go ---
```go
package runtime

import (
	"encoding/binary"
	_ "encoding/binary"
	"errors"
	_ "errors"
	"io"
	_ "io"
	_ "math"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/unsafeperf"
)

type Unmarshaller struct {
}

func NewUnmarshaller(r io.Reader, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) *Unmarshaller {
	panic("stub")
}

func (inst *Unmarshaller) SetInput(r io.Reader) { panic("stub") }

func (inst *Unmarshaller) SetEndianness(endi binary.ByteOrder) { panic("stub") }

func (inst *Unmarshaller) SetErrorHandler(f func(err error)) { panic("stub") }

func (inst *Unmarshaller) SetAllocateBufferFunc(f func(l uint32) []byte) { panic("stub") }

func (inst *Unmarshaller) ReadUInt8() (v uint8) { panic("stub") }

func (inst *Unmarshaller) ReadUInt16() (v uint16) { panic("stub") }

func (inst *Unmarshaller) ReadUInt32MostLikelyZero() (v uint32) { panic("stub") }

func (inst *Unmarshaller) ReadUInt32() (v uint32) { panic("stub") }

func (inst *Unmarshaller) ReadUInt64() (v uint64) { panic("stub") }

func (inst *Unmarshaller) ReadInt8() (v int8) { panic("stub") }

func (inst *Unmarshaller) ReadInt16() (v int16) { panic("stub") }

func (inst *Unmarshaller) ReadInt32() (v int32) { panic("stub") }

func (inst *Unmarshaller) ReadInt64() (v int64) { panic("stub") }

func (inst *Unmarshaller) ReadFloat32() (v float32) { panic("stub") }

func (inst *Unmarshaller) ReadFloat64() (v float64) { panic("stub") }

func (inst *Unmarshaller) ReadComplex64() (v complex64) { panic("stub") }

func (inst *Unmarshaller) ReadComplex128() (v complex128) { panic("stub") }

func (inst *Unmarshaller) ReadUintptr() (v uintptr) {
	panic(
		// TODO check size using unsafe.Sizeof(...) ?
		"stub")
}

var StringAllocationError = errors.New("allocated string buffer does not have correct length")

func (inst *Unmarshaller) ReadString() (v string) { panic("stub") }

func (inst *Unmarshaller) ReadStringMostLikelyEmpty() (v string) { panic("stub") }

// fast path

func (inst *Unmarshaller) ReadBytes() (v []byte) { panic("stub") }

// TODO

func (inst *Unmarshaller) ReadBool() (v bool) { panic("stub") }

func (inst *Unmarshaller) ReadSliceLength() (v int) { panic("stub") }


```

--- FILE: typed/fffi2_typed_globals.go ---
```go
package typed

import (
	_ "errors"

	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/pebble2impl/doc/skills/fffi2/references/runtime"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/runtime"
)

func HasErrors() bool { panic("stub") }

func GetError() (err error) { panic("stub") }

func SetCurrentFffiVar(fffi *runtime.Fffi2[*runtime.Unmarshaller]) { panic("stub") }

func GetCurrentFffiVar() (fffi *runtime.Fffi2[*runtime.Unmarshaller]) { panic("stub") }

func SetCurrentFffiErrorHandler(handler func(err error)) { panic("stub") }


```

--- FILE: typed/fffi2_typed_impl.go ---
```go
package typed

import (
	_ "bytes"
	_ "encoding/binary"
	_ "sync"
	_ "unique"
	_ "unsafe"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/unsafeperf"
	_ "github.com/stergiotis/pebble2impl/public/compiletimeflags"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/runtime"
)

func (inst *RetainedFffiHolder) GetRetainedElementId() (id RetainedElementId) { panic("stub") }

func NewRetainedFffiHolderTyped[T any](r *RetainedFffiHolder) RetainedFffiHolderTyped[T] {
	panic("stub")
}

func (inst RetainedFffiHolderTyped[T]) Untype() *RetainedFffiHolder { panic("stub") }

func (inst *RetainedFffiBuilder) WriteOpCode(code uint32) { panic("stub") }

func (inst *RetainedFffiBuilder) SpliceRetained(r *RetainedFffiHolder) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteUint8(v uint8) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteBool(v bool) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteUint16(v uint16) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteUint32(v uint32) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteUint64(v uint64) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteInt8(v int8) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteInt16(v int16) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteInt32(v int32) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteInt64(v int64) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteFloat32(v float32) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteFloat64(v float64) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteComplex64(v complex64) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteComplex128(v complex128) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteString(v string) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteBytes(v []byte) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteSliceLength(l int) { panic("stub") }

func (inst *RetainedFffiBuilder) WriteNilSlice() { panic("stub") }

func NewRetainedFffiBuilder() *RetainedFffiBuilder { panic("stub") }

// reserve space for encoding frame length
//_, err = inst.builder.buf.Write([]byte{0, 0, 0, 0})

func (inst *RetainedFffiBuilder) BuildRetained() *RetainedFffiHolder { panic("stub") }

// NOTE: copies a freshly allocated string
// FIXME this is bad (e.g. if go ever introduces a copying GC). should redirect through a weakmap

// see https://github.com/golang/go/issues/23199

func (inst *RetainedFffiBuilder) SendIntermediate() { panic("stub") }

func (inst *RetainedFffiHolder) SyncRetained() { panic("stub") }


```

--- FILE: typed/fffi2_typed_types.go ---
```go
package typed

import _ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/runtime"

type RetainedElementId uint64
type RetainedFffiHolder struct {
}
type RetainedFffiHolderTyped[T any] struct {
}
type RetainedFffiBuilder struct {
}


```
