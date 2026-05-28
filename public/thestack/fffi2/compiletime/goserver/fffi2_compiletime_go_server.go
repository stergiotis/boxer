package goserver

import (
	"fmt"
	"io"
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/containers/ragged"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
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

func composeConcreteTypeName(t ir.ConcreteType) string {
	return t.GetName().Convert(naming.UpperCamelCase).String() + "S"
}
func composeAbstractTypeName(t ir.AbstractType) string {
	return t.GetName().Convert(naming.UpperCamelCase).String() + "I"
}
func composeTypeName(t ir.TypeI) string {
	switch tt := t.(type) {
	case ir.ConcreteType:
		return composeConcreteTypeName(tt)
	case ir.AbstractType:
		return composeAbstractTypeName(tt)
	}
	return "<invalid>"
}

// colorTypeCodeFor returns the Go type string to emit for a color-annotated
// plain argument. Returns "" if the argument is not color-annotated, letting
// the caller fall through to its default primitive-type emission.
func colorTypeCodeFor(kind ir.ColorArgKindE) string {
	switch kind {
	case ir.ColorArgKindScalar:
		return "color.Color"
	case ir.ColorArgKindSlice:
		return "color.Colors"
	}
	return ""
}

func generateArgumentsDeclPlain(first bool, w io.Writer, spec ir.PlainArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (firstOut bool) {
	var err error
	for i, name := range spec.Names {
		typ := spec.Types[i]
		if !first {
			_, err = fmt.Fprint(w, ", ")
			tracker.MergeError(err)
		}
		var typeCode string
		if i < len(spec.ColorArgKinds) {
			typeCode = colorTypeCodeFor(spec.ColorArgKinds[i])
		}
		if typeCode == "" {
			typeCode, _, _, err = codegen.GenerateGoCode(typ, encodingaspects.EmptyAspectSet)
			tracker.MergeError(err)
		}
		_, err = fmt.Fprintf(w, "%s %s",
			name.Convert(naming.LowerCamelCase),
			typeCode,
		)
		first = false
	}
	firstOut = first
	return
}
func generateArgumentsDeclPlainLastIterator(w io.Writer, it iter.Seq2[naming.StylableName, canonicaltypes.PrimitiveAstNodeI], tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	var err error
	names := make([]naming.StylableName, 0, 4)
	typs := make([]canonicaltypes.PrimitiveAstNodeI, 0, 4)
	for name, typ := range it {
		names = append(names, name)
		typs = append(typs, typ)
	}
	l := len(names)
	first := true
	if l == 0 {
		return
	}
	if l > 1 {
		for name, typ := range ragged.Zip2(names[:l-1], typs[:l-1]) {
			if !first {
				_, err = fmt.Fprint(w, ", ")
				tracker.MergeError(err)
			}
			var typeCode string
			typeCode, _, _, err = codegen.GenerateGoCode(typ, encodingaspects.EmptyAspectSet)
			tracker.MergeError(err)
			_, err = fmt.Fprintf(w, "%s %s",
				name.Convert(naming.LowerCamelCase),
				typeCode,
			)
			first = false
		}
	}
	if !first {
		_, err = fmt.Fprint(w, ", ")
		tracker.MergeError(err)
	}
	if typs[l-1].IsScalar() {
		var typeCode string
		typeCode, _, _, err = codegen.GenerateGoCode(typs[l-1], encodingaspects.EmptyAspectSet)
		tracker.MergeError(err)
		_, err = fmt.Fprintf(w, "%s %s",
			names[l-1].Convert(naming.LowerCamelCase),
			typeCode,
		)
	} else {
		var typeCode string
		typeCode, _, _, err = codegen.GenerateGoCode(canonicaltypes.DemoteToScalarPrim(typs[l-1]), encodingaspects.EmptyAspectSet)
		tracker.MergeError(err)
		_, err = fmt.Fprintf(w, "%s iter.Seq[%s]",
			names[l-1].Convert(naming.LowerCamelCase),
			typeCode,
		)
	}
	tracker.MergeError(err)
	return
}
func generateArgumentsDeclEvaluated(first bool, w io.Writer, spec ir.EvaluatedArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (firstOut bool) {
	var err error
	for i, name := range spec.Names {
		typ := spec.AcceptedTypes[i]
		if !first {
			_, err = fmt.Fprint(w, ", ")
			tracker.MergeError(err)
		}
		if i < len(spec.ColorArgKinds) && spec.ColorArgKinds[i] == ir.ColorArgKindScalar {
			_, err = fmt.Fprintf(w, "%s color.Color",
				name.Convert(naming.LowerCamelCase),
			)
		} else {
			_, err = fmt.Fprintf(w, "%s typed.RetainedFffiHolderTyped[%s]",
				name.Convert(naming.LowerCamelCase),
				composeTypeName(typ),
			)
		}
		tracker.MergeError(err)
		first = false
	}
	firstOut = first
	return
}
func generateFactoryArgumentsHandlingPlain(first bool, w io.Writer, spec ir.PlainArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (firstOut bool) {
	var err error
	firstOut = first
	for i, name := range spec.Names {
		typ := spec.Types[i]
		var typeCode string
		if firstOut {
			_, err = fmt.Fprintf(w, "\tr := typed.NewRetainedFffiBuilder()\n")
			tracker.MergeError(err)
		}
		firstOut = false
		var kind ir.ColorArgKindE
		if i < len(spec.ColorArgKinds) {
			kind = spec.ColorArgKinds[i]
		}
		switch kind {
		case ir.ColorArgKindScalar:
			_, err = fmt.Fprintf(w, "\tcolor.PutAsU32(r, %s)\n",
				name.Convert(naming.LowerCamelCase),
			)
			tracker.MergeError(err)
			continue
		case ir.ColorArgKindSlice:
			_, err = fmt.Fprintf(w, "\tcolor.PutColorsSlice(r, %s)\n",
				name.Convert(naming.LowerCamelCase),
			)
			tracker.MergeError(err)
			continue
		}
		if typ.IsScalar() {
			typeCode, _, _, err = codegen.GenerateGoCode(typ, encodingaspects.EmptyAspectSet)
			tracker.MergeError(err)
			typeCode = naming.MustBeValidStylableName(typeCode).Convert(naming.UpperCamelCase).String()
			_, err = fmt.Fprintf(w, "\tr.Write%s(%s)\n",
				typeCode,
				name.Convert(naming.LowerCamelCase),
			)
		} else {
			// Non-scalar types (homogeneous arrays like F64h, U32h, Sh, etc.)
			// Use runtime.Put<ScalarType>SliceArg which handles nil + length prefix + elements
			scalar := canonicaltypes.DemoteToScalarPrim(typ)
			typeCode, _, _, err = codegen.GenerateGoCode(scalar, encodingaspects.EmptyAspectSet)
			tracker.MergeError(err)
			typeCode = naming.MustBeValidStylableName(typeCode).Convert(naming.UpperCamelCase).String()
			_, err = fmt.Fprintf(w, "\truntime.Put%sSliceArg(r, %s)\n",
				typeCode,
				name.Convert(naming.LowerCamelCase),
			)
		}
	}
	return
}
func generateFetcherReturnHandlingPlain(w io.Writer, it iter.Seq2[naming.StylableName, canonicaltypes.PrimitiveAstNodeI], tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	names := make([]naming.StylableName, 0, 4)
	typs := make([]canonicaltypes.PrimitiveAstNodeI, 0, 4)
	for name, typ := range it {
		names = append(names, name)
		typs = append(typs, typ)
	}
	l := len(names)
	if l == 0 {
		return
	}
	if l > 1 {
		for name, typ := range ragged.Zip2(names[:l-1], typs[:l-1]) {
			typS := naming.MustBeValidStylableName(typ.String()).Convert(naming.UpperCamelCase).String()
			_, err := fmt.Fprintf(w, "\t%s = inst.read%s()\n",
				name.Convert(naming.LowerCamelCase),
				typS,
			)
			tracker.MergeError(err)
		}
	}
	if typs[l-1].IsScalar() {
		typS := naming.MustBeValidStylableName(typs[l-1].String()).Convert(naming.UpperCamelCase).String()
		_, err := fmt.Fprintf(w, "\t%s = inst.read%s()\n",
			names[l-1].Convert(naming.LowerCamelCase),
			typS,
		)
		tracker.MergeError(err)
	} else {
		typS := naming.MustBeValidStylableName(canonicaltypes.DemoteToScalarPrim(typs[l-1]).String()).Convert(naming.UpperCamelCase).String()
		_, err := fmt.Fprintf(w, "\t%s = inst.iterate%sh()\n",
			names[l-1].Convert(naming.LowerCamelCase),
			typS,
		)
		tracker.MergeError(err)
	}
	return
}
func generateFactoryArgumentsHandlingEvaluated(w io.Writer, spec ir.EvaluatedArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (firstOut bool) {
	var err error
	for i, name := range spec.Names {
		var kind ir.ColorArgKindE
		if i < len(spec.ColorArgKinds) {
			kind = spec.ColorArgKinds[i]
		}
		if kind == ir.ColorArgKindScalar {
			// PutColorAsRetainedColor32 is defined in the components package
			// itself (generated code lives there), not in the color package,
			// to avoid a color→components import cycle.
			_, err = fmt.Fprintf(w, "\tPutColorAsRetainedColor32(r, %s)\n",
				name.Convert(naming.LowerCamelCase),
			)
			tracker.MergeError(err)
			continue
		}
		_, err = fmt.Fprintf(w, "\tr.SpliceRetained(%s.Untype())\n",
			name.Convert(naming.LowerCamelCase),
		)
		tracker.MergeError(err)
	}
	return
}
func generateMethodArgumentsHandlingPlain(first bool, w io.Writer, spec ir.PlainArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (firstOut bool) {
	return generateFactoryArgumentsHandlingPlain(first, w, spec, tracker)
}
func anyColorKind(kinds []ir.ColorArgKindE) bool {
	for _, k := range kinds {
		if k != ir.ColorArgKindNone {
			return true
		}
	}
	return false
}
func argSpecUsesColor(arguments ir.ArgumentSpec) bool {
	return anyColorKind(arguments.PlainArguments.ColorArgKinds) ||
		anyColorKind(arguments.EvaluatedArguments.ColorArgKinds)
}
func methodUsesColor(m ir.Method) bool {
	return anyColorKind(m.Spec.PlainArguments.ColorArgKinds) ||
		anyColorKind(m.Spec.EvaluatedArguments.ColorArgKinds)
}
func methodUsesNonScalarPlain(m ir.Method) bool {
	for _, typ := range m.Spec.PlainArguments.Types {
		if typ == nil {
			continue
		}
		if !typ.IsScalar() {
			return true
		}
	}
	return false
}
func generateImports(wh WriterHolder, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	rt2 := false
	functional := false
	hasDeferred := false
	hasReference := false
	hasColor := false
	methodHasRuntime := false
	for _, tl := range tls {
		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			functional = functional || tlt.Settings.BlockIterator
			hasDeferred = hasDeferred || len(tlt.DeferredBlockMaps) > 0
			hasReference = hasReference || tlt.IdentityArguments.IsReference
			if !hasColor {
				hasColor = argSpecUsesColor(tlt.Arguments)
				if !hasColor {
					for _, m := range tlt.BuilderMethods {
						if methodUsesColor(m) {
							hasColor = true
							break
						}
					}
				}
			}
			if !methodHasRuntime {
				for _, m := range tlt.BuilderMethods {
					if methodUsesNonScalarPlain(m) {
						methodHasRuntime = true
						break
					}
				}
			}
		case *ir.ProceduralNode:
			functional = functional || tlt.Settings.BlockIterator
			hasReference = hasReference || tlt.IdentityArguments.IsReference
			hasColor = hasColor || argSpecUsesColor(tlt.Arguments)
		}
		rt2 = true
	}
	if rt2 {
		for _, w := range []io.Writer{wh.MethodWriter, wh.TypeWriter, wh.FactoryWriter} {
			_, err := fmt.Fprintf(w, `
import "github.com/stergiotis/boxer/public/thestack/fffi2/typed"
`)
			tracker.MergeError(err)
		}
	}
	if hasColor {
		for _, w := range []io.Writer{wh.MethodWriter, wh.FactoryWriter} {
			_, err := fmt.Fprintf(w, `
import "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
`)
			tracker.MergeError(err)
		}
	}
	if hasReference {
		for _, w := range []io.Writer{wh.FactoryWriter} {
			_, err := fmt.Fprintf(w, `
import "github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
`)
			tracker.MergeError(err)
		}
	}
	if hasDeferred {
		for _, w := range []io.Writer{wh.FactoryWriter} {
			_, err := fmt.Fprintf(w, `
import "encoding/binary"
import "github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
`)
			tracker.MergeError(err)
		}
		for _, w := range []io.Writer{wh.TypeWriter} {
			_, err := fmt.Fprintf(w, `
import "github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
`)
			tracker.MergeError(err)
		}
	}
	if methodHasRuntime {
		_, err := fmt.Fprintf(wh.MethodWriter, `
import "github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
`)
		tracker.MergeError(err)
	}
	if functional {
		for _, w := range []io.Writer{wh.MethodWriter} {
			_, err := fmt.Fprintf(w, `
import (
	"github.com/stergiotis/boxer/public/functional"
	"iter"
)
`)
			tracker.MergeError(err)
		}
	}
	if wh.FetcherWriter != nil {
		for _, w := range []io.Writer{wh.FetcherWriter} {
			_, err := fmt.Fprintf(w, `
import (
"iter"
)
`)
			tracker.MergeError(err)
		}
	}
	return
}
func generateRootFunc(w io.Writer, name naming.StylableName, identitySpec ir.IdentityArgumentSpec, arguments ir.ArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	_, err := fmt.Fprintf(w, `func %s(`, name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
	{
		first := true
		if identitySpec.HasId {
			first = false
			if identitySpec.IsReference {
				_, err = fmt.Fprint(w, "h widgethandle.WidgetHandle")
			} else {
				_, err = fmt.Fprint(w, "i WidgetIdCreatorI")
			}
			tracker.MergeError(err)
		}
		first = generateArgumentsDeclEvaluated(first, w, arguments.EvaluatedArguments, tracker)
		generateArgumentsDeclPlain(first, w, arguments.PlainArguments, tracker)
	}
}
func generateIdentityHandling(w io.Writer, identitySpec ir.IdentityArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE], variant string) {
	var err error
	if identitySpec.HasId {
		if identitySpec.IsReference {
			_, err = fmt.Fprintf(w, "\tr.WriteWidgetId(h.Resolve())\n")
		} else {
			_, err = fmt.Fprintf(w, `	v := i.Derive%s()
	r.WriteWidgetId(checkId(v))
`, variant)
		}
		tracker.MergeError(err)
	}
}
func generateTypeCode(w io.Writer, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	concreteTypes := containers.NewBinarySearchGrowingKVOrdered[string, []string](128)
	abstractTypes := containers.NewHashSet[string](128)
	addT := func(t ir.TypeI) {
		s := composeTypeName(t)
		if t.IsAbstract() {
			abstractTypes.Add(s)
		} else {
			if !concreteTypes.Has(s) {
				var l []string
				for a := range t.ImplementedAbstractTypes() {
					sa := composeAbstractTypeName(a)
					abstractTypes.Add(sa)
					l = append(l, sa)
				}
				concreteTypes.UpsertSingle(s, l)
			}
		}
	}
	for _, tl := range tls {
		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			for _, t := range tlt.Arguments.EvaluatedArguments.AcceptedTypes {
				addT(t)
			}
			if tlt.ReturnType != nil {
				addT(tlt.ReturnType)
			}
			generateTypeCodeFactory(w, tlt, tracker)
			break
		case *ir.ProceduralNode:
			for _, t := range tlt.Arguments.EvaluatedArguments.AcceptedTypes {
				addT(t)
			}
			if tlt.ReturnType != nil {
				addT(tlt.ReturnType)
			}
			break
		}
	}
	for concrete, abstracts := range concreteTypes.IteratePairs() {
		_, err := fmt.Fprintf(w, "type %s struct {}\n\n", concrete)
		tracker.MergeError(err)
		for _, a := range abstracts {
			_, err = fmt.Fprintf(w, `func (inst %s) DummyInterfaceImplementationMethod%s() {}
var _ %s = %s{}

`, concrete, a,
				a, concrete)
			tracker.MergeError(err)
		}
	}
	{
		sl := abstractTypes.Slice()
		slices.Sort(sl)
		for _, a := range sl {
			_, err := fmt.Fprintf(w, `type %s interface {
	DummyInterfaceImplementationMethod%s()
}

`, a, a)
			tracker.MergeError(err)
		}
	}
}
func generateTypeCodeFactory(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", factory.Name)
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	_, err := fmt.Fprintf(w, `type %sFluid struct {
	r *typed.RetainedFffiBuilder
`, b)
	if factory.IdentityArguments.HasId {
		_, err = fmt.Fprint(w, "\tid uint64\n\tidGen WidgetIdCreatorI\n")
		tracker.MergeError(err)
	}
	for _, dbm := range factory.DeferredBlockMaps {
		dbmName := naming.MustBeValidStylableName(dbm.Name).Convert(naming.UpperCamelCase)
		_, err = fmt.Fprintf(w, "\tdeferred%s *runtime.DeferredBlockScope\n", dbmName)
		tracker.MergeError(err)
	}
	tracker.MergeError(err)
	_, err = fmt.Fprintf(w, `}
type %sMethodIdE uint32

`, b)
	tracker.MergeError(err)
}
func generateProceduralCode(w io.Writer, procedure *ir.ProceduralNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	generateRootFunc(w, procedure.Name, procedure.IdentityArguments, procedure.Arguments, tracker)
	tracker.ErrorMessagePrefix = fmt.Sprintf("proc %s ", procedure.Name)
	_, err := fmt.Fprint(w, `) {
`)
	tracker.MergeError(err)

	_, err = fmt.Fprintf(w, `	r := typed.NewRetainedFffiBuilder()
	r.WriteUint32(uint32(FuncProcId%s))
`, procedure.Name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)

	generateIdentityHandling(w, procedure.IdentityArguments, tracker, "")
	generateFactoryArgumentsHandlingPlain(false, w, procedure.Arguments.PlainArguments, tracker)
	generateFactoryArgumentsHandlingEvaluated(w, procedure.Arguments.EvaluatedArguments, tracker)

	_, err = fmt.Fprint(w, `
	r.SendIntermediate()
}

`)
	tracker.MergeError(err)
}
func generateFetcherCode(w io.Writer, fetcher *ir.FetcherNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("fetcher %s ", fetcher.Name)
	_, err := fmt.Fprintf(w, `func (inst *Fetcher) %s() (`, fetcher.Name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
	{
		generateArgumentsDeclPlainLastIterator(w, fetcher.ReturnTypes.Iterate(), tracker)
	}
	_, err = fmt.Fprint(w, `) {
`)
	tracker.MergeError(err)

	_, err = fmt.Fprintf(w, `	inst.invoke(FuncProcId%s)
`, fetcher.Name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)

	generateFetcherReturnHandlingPlain(w, fetcher.ReturnTypes.Iterate(), tracker)

	_, err = fmt.Fprint(w, `	return
}
`)
	tracker.MergeError(err)
}
func generateFactoryCode(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	generateRootFunc(w, factory.Name, factory.IdentityArguments, factory.Arguments, tracker)
	tracker.ErrorMessagePrefix = fmt.Sprintf("builder factory %s ", factory.Name)
	_, err := fmt.Fprintf(w, `) (inst %sFluid) {
`, factory.Name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)

	_, err = fmt.Fprintf(w, `	r := typed.NewRetainedFffiBuilder()
	r.WriteOpCode(uint32(FuncProcId%s))
`, factory.Name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
	idVariant := ""
	idDefer := ""
	if factory.Settings.BlockIterator {
		idVariant = "Stacked"
		if factory.IdentityArguments.HasId {
			//	idDefer = "i.PopIdFromStackChecked(v)\n"
		}
	}
	generateIdentityHandling(w, factory.IdentityArguments, tracker, idVariant)
	generateFactoryArgumentsHandlingPlain(false, w, factory.Arguments.PlainArguments, tracker)
	generateFactoryArgumentsHandlingEvaluated(w, factory.Arguments.EvaluatedArguments, tracker)

	_, err = fmt.Fprintf(w, `
	inst = %sFluid{
		r: r,
	}

`, factory.Name.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
	if factory.IdentityArguments.HasId {
		_, err = fmt.Fprint(w, `
	inst.id = v
    inst.idGen = i
`)
		tracker.MergeError(err)
	}
	for _, dbm := range factory.DeferredBlockMaps {
		dbmName := naming.MustBeValidStylableName(dbm.Name).Convert(naming.UpperCamelCase)
		// Per ADR-0049: hint the dataBuf initial capacity with the
		// per-kind atomic registered at the runtime layer. The hint is
		// folded back via ReleaseWithHint in the generated Send().
		// RegisterScopeHint is idempotent — repeated calls with the
		// same kind name return the same *atomic.Uint64.
		_, err = fmt.Fprintf(w,
			"\tinst.deferred%s = runtime.NewDeferredBlockScopeHinted(typed.GetCurrentFffiCapture, binary.LittleEndian, runtime.RegisterScopeHint(%q))\n",
			dbmName, dbmName)
		tracker.MergeError(err)
	}
	_, err = fmt.Fprintf(w, `
	%s
	return
}

`, idDefer)
	tracker.MergeError(err)
}
func generateMethodCodeBuildMethods(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	var buildMethodInvoke string
	if len(factory.BuilderMethods) > 0 {
		buildMethodInvoke = fmt.Sprintf("r.WriteOpCode(uint32(%sMethodIdBuild))", b)
	}
	if factory.Settings.Immediate {
		if len(factory.DeferredBlockMaps) > 0 {
			// Send() must splice deferred block maps before sending.
			// Per ADR-0049: each scope's buffer high-water is folded back
			// into the per-kind hint via ReleaseWithHint, AFTER Splice
			// has copied the captured bytes out of the scope. Releasing
			// before Splice would race the scope's buffers into GC while
			// the Splice still aliases them.
			var spliceCalls string
			var releaseCalls string
			for _, dbm := range factory.DeferredBlockMaps {
				dbmName := naming.MustBeValidStylableName(dbm.Name).Convert(naming.UpperCamelCase)
				spliceCalls += fmt.Sprintf("\tr.SpliceDeferredBlockMap(inst.deferred%s)\n", dbmName)
				releaseCalls += fmt.Sprintf("\tinst.deferred%s.ReleaseWithHint()\n", dbmName)
			}
			_, err := fmt.Fprintf(w, `func (inst %sFluid) Send() {
	r := inst.r
	%s
%s%s	r.SendIntermediate()
}
`, b, buildMethodInvoke, spliceCalls, releaseCalls)
			tracker.MergeError(err)
		} else {
			_, err := fmt.Fprintf(w, `func (inst %sFluid) Send() {
	r := inst.r
	%s
	r.SendIntermediate()
}
`, b, buildMethodInvoke)
			tracker.MergeError(err)
		}
	}
	if factory.Settings.Retained && len(factory.DeferredBlockMaps) == 0 {
		// Keep() is not generated for nodes with DeferredBlockMaps because
		// deferred blocks are per-frame and cannot be captured in a retained holder.
		t := composeTypeName(factory.ReturnType)
		_, err := fmt.Fprintf(w, `func (inst %sFluid) Keep() typed.RetainedFffiHolderTyped[%s] {
	r := inst.r
	%s
	return typed.NewRetainedFffiHolderTyped[%s](r.BuildRetained())
}
`, b,
			t,
			buildMethodInvoke,
			t)
		tracker.MergeError(err)
	}
	if factory.Settings.BlockIterator {
		var idHandlingDefer = ""
		if factory.IdentityArguments.HasId {
			// FIXME
			idHandlingDefer = `/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/
defer func() { inst.idGen.PopIdFromStackChecked(inst.id) }()
`
		}
		if buildMethodInvoke != "" {
			buildMethodInvoke = "inst." + buildMethodInvoke
		}
		_, err := fmt.Fprintf(w, `func (inst %sFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] {
	%s
	r := inst.r.BuildRetained()
	return func(yield func(functional.NilIteratorValueType) bool) {
		%s
		r.SyncRetained()
		defer func() {
			End()
		}()
`, b,
			buildMethodInvoke,
			idHandlingDefer,
		)
		tracker.MergeError(err)
		// Per ADR-0012: block iterators always yield. The previous-frame
		// BLOCK_SKIPPED gate caused click-to-open flicker and lag compounding
		// through nested collapsibles. Rust drains the body when collapsed
		// (every block's apply code now has an else-arm calling
		// `interpret_outer(c, &mut None)?` so each block consumes its own
		// `End` regardless of u). App-level skip on the (advisory)
		// HasBlockSkipped signal remains available via
		// StateManager.GetResponse(handle).
		_, err = fmt.Fprint(w, `
			yield(functional.NilIteratorValue)
`)
		tracker.MergeError(err)
		_, err = fmt.Fprint(w, `
	}
}
`)
		tracker.MergeError(err)
	}
}
func generateMethodCode(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	for _, method := range factory.BuilderMethods {
		tracker.ErrorMessagePrefix = fmt.Sprintf("%s.%s ", factory.Name, method.Spec.Name)
		_, err := fmt.Fprintf(w, `func (inst %sFluid) %s(`,
			b,
			method.Spec.Name.Convert(naming.UpperCamelCase),
		)
		tracker.MergeError(err)
		first := generateArgumentsDeclPlain(true, w, method.Spec.PlainArguments, tracker)
		generateArgumentsDeclEvaluated(first, w, method.Spec.EvaluatedArguments, tracker)
		_, err = fmt.Fprintf(w, `) %sFluid {
	r := inst.r
	r.WriteOpCode(uint32(%sMethodId%s))
`, factory.Name.Convert(naming.UpperCamelCase),
			b,
			method.Spec.Name.Convert(naming.UpperCamelCase),
		)
		tracker.MergeError(err)

		generateMethodArgumentsHandlingPlain(false, w, method.Spec.PlainArguments, tracker)
		generateFactoryArgumentsHandlingEvaluated(w, method.Spec.EvaluatedArguments, tracker)

		_, err = fmt.Fprintf(w, `
	return inst
}

`)
		tracker.MergeError(err)
	}
	generateMethodCodeBuildMethods(w, factory, tracker)
	generateDeferredBlockMethods(w, factory, tracker)
}
func generateDeferredBlockMethods(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	if len(factory.DeferredBlockMaps) == 0 {
		return
	}
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	for _, dbm := range factory.DeferredBlockMaps {
		dbmName := naming.MustBeValidStylableName(dbm.Name).Convert(naming.UpperCamelCase).String()

		// Generate Begin method with typed key parameters
		_, err := fmt.Fprintf(w, "func (inst %sFluid) Begin%s(", b, dbmName)
		tracker.MergeError(err)
		for i, kt := range dbm.KeyTypes {
			if i > 0 {
				_, err = fmt.Fprint(w, ", ")
				tracker.MergeError(err)
			}
			var typeCode string
			typeCode, _, _, err = codegen.GenerateGoCode(kt, encodingaspects.EmptyAspectSet)
			tracker.MergeError(err)
			_, err = fmt.Fprintf(w, "key%d %s", i, typeCode)
			tracker.MergeError(err)
		}
		_, err = fmt.Fprintf(w, ") %sFluid {\n", b)
		tracker.MergeError(err)

		// Build the Begin call with typed key args
		_, err = fmt.Fprintf(w, "\tinst.deferred%s.Begin(", dbmName)
		tracker.MergeError(err)
		for i := range dbm.KeyTypes {
			if i > 0 {
				_, err = fmt.Fprint(w, ", ")
				tracker.MergeError(err)
			}
			_, err = fmt.Fprintf(w, "key%d", i)
			tracker.MergeError(err)
		}
		_, err = fmt.Fprint(w, ")\n\treturn inst\n}\n\n")
		tracker.MergeError(err)

		// Generate End method
		_, err = fmt.Fprintf(w, `func (inst %sFluid) End%s() %sFluid {
	inst.deferred%s.End()
	return inst
}

`, b, dbmName, b, dbmName)
		tracker.MergeError(err)
	}
}
func generateMethodEnum(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	if len(factory.BuilderMethods) == 0 {
		return
	}
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	_, err := fmt.Fprintf(w, `const (
	%sMethodIdBuild %sMethodIdE = 0

`, b, b)
	tracker.MergeError(err)

	for idx, method := range factory.BuilderMethods {
		_, err = fmt.Fprintf(w, "\t%sMethodId%s %sMethodIdE = %d\n",
			b,
			method.Spec.Name.Convert(naming.UpperCamelCase),
			b,
			idx+1,
		)
		tracker.MergeError(err)
	}
	_, err = fmt.Fprint(w, ")\n\n")
	tracker.MergeError(err)
}
func generateFuncProcIdEnum(w io.Writer, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	_, err := fmt.Fprint(w, `type FuncProcIdE uint32
const (
`)
	tracker.MergeError(err)
	for idx, tl := range tls {
		b := tl.GetName().Convert(naming.UpperCamelCase).String()
		_, err = fmt.Fprintf(w, `	FuncProcId%s FuncProcIdE = FuncProcIdOffset + %d
`, b, idx)
		tracker.MergeError(err)
	}
	_, err = fmt.Fprint(w, `
)
`)
	tracker.MergeError(err)
}

func GenerateCode(wh WriterHolder, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (err error) {
	generateImports(wh, tls, tracker)
	generateTypeCode(wh.TypeWriter, tls, tracker)
	generateFuncProcIdEnum(wh.EnumWriter, tls, tracker)
	err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
	if err != nil {
		err = eb.Build().Errorf("unable to create go code: %w", err)
		return
	}

	for _, tl := range tls {
		tracker.ResetStateAndError()

		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			generateFactoryCode(wh.FactoryWriter, tlt, tracker)
			generateMethodCode(wh.MethodWriter, tlt, tracker)
			generateMethodEnum(wh.EnumWriter, tlt, tracker)
			break
		case *ir.ProceduralNode:
			generateProceduralCode(wh.FactoryWriter, tlt, tracker)
			break
		case *ir.FetcherNode:
			generateFetcherCode(wh.FetcherWriter, tlt, tracker)
			break
		default:
			err = eb.Build().Type("type", tl).Errorf("unhandled top level type")
			return
		}

		err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
		if err != nil {
			err = eb.Build().Stringer("name", tl.GetName()).Errorf("unable to create go code for top level node: %w", err)
			return
		}
	}
	return
}
