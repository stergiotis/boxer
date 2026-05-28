package ir

import (
	"iter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/compiletimeflags"
)

type AbstractType struct {
	name naming.StylableName
}

var _ TypeI = AbstractType{}

type ConcreteType struct {
	name                     naming.StylableName
	implementedAbstractTypes []AbstractType
}

var _ TypeI = ConcreteType{}

func NewConcreteType(name naming.StylableName, implementedAbstractTypes ...AbstractType) ConcreteType {
	if compiletimeflags.ExtraChecks && !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("invalid name")
	}
	return ConcreteType{
		name:                     name,
		implementedAbstractTypes: implementedAbstractTypes,
	}
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

// ColorArgKindE marks whether an argument is to be surfaced as a unified
// color type in generated Go signatures. Parallel-slice entries on the
// argument specs carry one value per argument position; the zero value
// [ColorArgKindNone] preserves pre-ADR-0003 behaviour and is emitted for
// every non-annotated argument so the slice stays index-synchronous with
// Names and Types.
type ColorArgKindE uint8

const (
	// ColorArgKindNone is the zero value: argument is not color-annotated.
	ColorArgKindNone ColorArgKindE = 0
	// ColorArgKindScalar marks a scalar color argument; Go signature surfaces
	// as color.Color regardless of which wire transport (Plain u32 or
	// Evaluated Color32) carries the value.
	ColorArgKindScalar ColorArgKindE = 1
	// ColorArgKindSlice marks a bulk color argument; Go signature surfaces as
	// color.Colors. Valid only on Plain slice (ctabb.U32h) args; ADR-0003 SD9
	// forbids retained values in arrays.
	ColorArgKindSlice ColorArgKindE = 2
)

type PlainArgumentSpec struct {
	Names         []naming.StylableName
	Types         []canonicaltypes.PrimitiveAstNodeI
	ColorArgKinds []ColorArgKindE
}
type EvaluatedArgumentSpec struct {
	Names         []naming.StylableName
	AcceptedTypes []TypeI
	ColorArgKinds []ColorArgKindE
}
type LangE string

type MethodSpec struct {
	Name               naming.StylableName
	PlainArguments     PlainArgumentSpec
	EvaluatedArguments EvaluatedArgumentSpec
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

var _ VerbatimCodeI = (*StringVerbatimCode)(nil)

type CodeHolder struct {
	CodeClientRust VerbatimCodeI
	CodeServerGo   VerbatimCodeI
}
type IdentityArgumentSpec struct {
	HasId bool
	// IsReference marks the id as naming an existing widget rather than
	// creating a new one. Generated Go call sites take a
	// widgethandle.WidgetHandle (opaque) instead of WidgetIdCreatorI, and
	// write the resolved raw id directly without going through the
	// duplicate-id guard used by widget factories.
	IsReference bool
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
	DeferredBlockMaps []DeferredBlockMapSpec
}

var _ NodeI = (*BuilderFactoryNode)(nil)

type ProceduralNode struct {
	Name              naming.StylableName
	IdentityArguments IdentityArgumentSpec
	Arguments         ArgumentSpec
	Settings          ProcedureFeaturesSpec
	ApplyCode         CodeHolder
	ReturnType        TypeI
}

var _ NodeI = (*ProceduralNode)(nil)

type FetcherNode struct {
	Name        naming.StylableName
	ApplyCode   CodeHolder
	ReturnTypes PlainArgumentSpec
}

var _ NodeI = (*FetcherNode)(nil)

type NodeI interface {
	GetName() naming.StylableName
}

type BuilderFactoryCodeGenExprs struct {
	InterpreterLifetime           string
	Id                            string
	Instance                      string
	SendMessage                   string
	MarkReturn                    string
	FuncProcIdOuter               string
	FuncProcIdInner               string
	MethodProcId                  string
	EguiContext                   string
	EguiUiOptionalOuter           string
	EguiUiOptionalInner           string
	EndConsumeFrameIfNecessary    string
	InterpreterDepth              string
	InvokeInterpreterInner        string
	AtomsRegister0Transfer        string
	AtomsRegister0Reference       string
	WidgetTextRegister0Reference  string
	WidgetTextRegister0Transfer   string
	Color32Register0Transfer      string
	CodeViewJobRegister0Reference string
	CodeViewJobRegister0Transfer  string
}

// A DeferredBlockMap is a first-class argument type in the IDL.
// It represents a collection of opcode sequences, each addressed by
// a composite key, that are captured on the Go side and replayed on
// the Rust side inside delegate/callback contexts.
//
// This is the fundamental primitive that enables binding any Rust API
// with a trait/delegate/callback pattern (egui_table, plot delegates,
// tab bar delegates, dock/tile layouts, etc.).
//
// The DeferredBlockMap is:
//   - Declared in the IDL via WithDeferredBlockMap(name, keyTypes...)
//   - Go side: captured via BeginDeferred(key...) / EndDeferred()
//   - Serialized: count + (key, len, bytes) tuples, spliced into the
//     consuming node's message
//   - Rust side: deserialized into a HashMap and made available as a
//     local variable in the apply code
//   - Replayed: via self.replay_deferred_block(ctx, ui, &block_bytes)

// DeferredBlockMapSpec declares a deferred block map on an IDL node.
//
// The code generator uses this to:
//   - Go side: generate BeginDeferred/EndDeferred methods and the
//     writer-swap + splicing logic in .Send().
//   - Rust side: generate deserialization code in the apply code that
//     reads the block map from the IPC stream into a local HashMap.
type DeferredBlockMapSpec struct {
	// Name of the block map variable in the Rust apply code.
	// E.g. "cells" → available as `cells: HashMap<K, Vec<u8>>` in apply code.
	Name string

	// KeyTypes defines the composite key type for addressing blocks.
	// E.g. for a table: [U64, U32] → key is (row: u64, col: u32)
	// E.g. for a tab bar: [U32] → key is (tab_idx: u32)
	// E.g. for a plot: [U32] → key is (series_idx: u32)
	KeyTypes []canonicaltypes.PrimitiveAstNodeI
}
