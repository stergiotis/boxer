// Package callsites surveys the call expressions of Go packages and
// classifies each site's dispatch: monomorphic, static-polymorphic
// (generics), dynamic-polymorphic (interfaces, func values), conversion or
// builtin — optionally joined with the compiler's own devirtualization and
// inlining decisions parsed from `go build -gcflags=-m`.
//
// Architecture and vocabulary follow ADR-0107: the static layer implements
// §SD2–§SD6, the adjudication layer §SD1, the CLI §SD7. The static
// classification states what the code says; the compiler join states what
// the toolchain did; the two never override each other.
package callsites

import (
	"fmt"
	"strings"
)

// CallTypeE classifies the dispatch of a call expression (ADR-0107 §SD2).
type CallTypeE uint8

const (
	// CallTypeUnknown marks a site whose callee could not be resolved from
	// type information. Never fabricated into a real class (ADR-0107 §SD6).
	CallTypeUnknown CallTypeE = 0
	// CallTypeMonomorphic is a direct static call to a named function or a
	// non-generic method, including immediately-invoked func literals.
	CallTypeMonomorphic CallTypeE = 1
	// CallTypeStaticPolymorphic is a generic instantiation: a call to an
	// instantiated generic function, or a method call whose receiver is an
	// instantiated generic type or an unresolved type parameter.
	CallTypeStaticPolymorphic CallTypeE = 2
	// CallTypeDynamicPolymorphic dispatches at runtime: interface method
	// calls and calls through func values.
	CallTypeDynamicPolymorphic CallTypeE = 3
	// CallTypeConversion is a type conversion in call syntax — not a call.
	CallTypeConversion CallTypeE = 4
	// CallTypeBuiltin is a call to a language builtin (make, len, panic, …).
	CallTypeBuiltin CallTypeE = 5
)

func (inst CallTypeE) String() string {
	switch inst {
	case CallTypeMonomorphic:
		return "Mono"
	case CallTypeStaticPolymorphic:
		return "StaticPoly"
	case CallTypeDynamicPolymorphic:
		return "DynPoly"
	case CallTypeConversion:
		return "Conversion"
	case CallTypeBuiltin:
		return "Builtin"
	default:
		return "Unknown"
	}
}

// OriginE locates the callee relative to the scanned module (ADR-0107 §SD4).
type OriginE uint8

const (
	// OriginUnknown marks sites without a nameable callee (calls of computed
	// func values) and conversions.
	OriginUnknown OriginE = 0
	// OriginLocal is a callee defined in the scanned root's module.
	OriginLocal OriginE = 1
	// OriginStdLib is the standard library, universe builtins included.
	OriginStdLib OriginE = 2
	// Origin3rdParty is any other module.
	Origin3rdParty OriginE = 3
)

func (inst OriginE) String() string {
	switch inst {
	case OriginLocal:
		return "Local"
	case OriginStdLib:
		return "StdLib"
	case Origin3rdParty:
		return "3rdParty"
	default:
		return "Unknown"
	}
}

// ShapeClassE classifies a generic type argument by the gcshape behaviour the
// toolchain actually exhibits (ADR-0107 §SD3; measurement procedure recorded
// there). It is the governance axis: Pointer and Interface arguments are the
// dictionary-degraded cases, Stenciled arguments cost ~nothing extra.
type ShapeClassE uint8

const (
	ShapeClassUnknown ShapeClassE = 0
	// ShapeClassStenciled gets its own instantiation per memory layout:
	// basics, strings, structs, arrays, slices, maps, chans, funcs.
	ShapeClassStenciled ShapeClassE = 1
	// ShapeClassPointer collapses into the single go.shape.*uint8
	// instantiation shared by all pointer types; devirtualization is lost.
	ShapeClassPointer ShapeClassE = 2
	// ShapeClassInterface adds interface indirection on top of the
	// dictionary — the documented worst case.
	ShapeClassInterface ShapeClassE = 3
	// ShapeClassTypeParam is an unresolved T passed through inside a generic
	// body; the cost is decided per outer instantiation.
	ShapeClassTypeParam ShapeClassE = 4
)

func (inst ShapeClassE) String() string {
	switch inst {
	case ShapeClassStenciled:
		return "Stenciled"
	case ShapeClassPointer:
		return "Pointer"
	case ShapeClassInterface:
		return "Interface"
	case ShapeClassTypeParam:
		return "TypeParam"
	default:
		return "Unknown"
	}
}

// TypeArgInfo describes one generic type argument: its shape class plus the
// rendered type for reporting (ADR-0107 §SD3).
type TypeArgInfo struct {
	Type  string
	Shape ShapeClassE
}

func (inst TypeArgInfo) String() string {
	return inst.Shape.String() + "(" + inst.Type + ")"
}

// CompilerDecision carries the ADR-0107 §SD1 adjudication verdicts for one
// call site. The zero value means "not adjudicated".
type CompilerDecision struct {
	// Checked is true when an adjudication build covered this site's file
	// (test files are outside `go build` and stay unchecked).
	Checked bool
	// Devirtualized: the compiler rewrote this interface call to a direct
	// call ("devirtualizing …").
	Devirtualized bool
	// InlinedCall: the compiler inlined this call ("inlining call to …").
	InlinedCall bool
}

// LoadStats reports survey coverage (ADR-0107 §SD5): constraint-excluded
// files are legitimate build configuration, not load errors, so they cannot
// fail the run — but a survey must not present a hollowed-out package as
// fully covered. Non-zero IgnoredFiles usually means missing --tags.
type LoadStats struct {
	// Packages is the number of scanned root packages.
	Packages int
	// IgnoredFiles counts Go files in the matched directories that build
	// constraints excluded (test files not counted).
	IgnoredFiles int
}

// CallSite is one classified call expression (ADR-0107 §SD2).
type CallSite struct {
	// File is the absolute path of the containing file.
	File string
	// Func is the qualified callee where resolvable (types.Func.FullName
	// form), the rendered target type for conversions, "(func literal)" for
	// immediately-invoked literals, "(indirect)" for computed func values.
	Func string
	// TypeArgs is the callee's own instantiation (generic function call).
	TypeArgs []TypeArgInfo
	// RecvTypeArgs is the receiver's instantiation (method on G[T] or on a
	// type parameter). Distinct from TypeArgs by provenance.
	RecvTypeArgs []TypeArgInfo
	// Line/Col address the call's opening parenthesis — the position the
	// compiler's -m diagnostics use, and the §SD1 join key.
	Line int
	Col  int

	Type     CallTypeE
	Origin   OriginE
	Compiler CompilerDecision
}

func (inst CallSite) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s:%d:%d %s %s %s", inst.File, inst.Line, inst.Col, inst.Type, inst.Origin, inst.Func)
	if len(inst.TypeArgs) > 0 {
		fmt.Fprintf(&sb, " args=%v", inst.TypeArgs)
	}
	if len(inst.RecvTypeArgs) > 0 {
		fmt.Fprintf(&sb, " recvArgs=%v", inst.RecvTypeArgs)
	}
	if inst.Compiler.Checked {
		fmt.Fprintf(&sb, " devirt=%t inlined=%t", inst.Compiler.Devirtualized, inst.Compiler.InlinedCall)
	}
	return sb.String()
}
