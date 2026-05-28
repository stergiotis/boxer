//go:build llm_generated_opus46

package goclient

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
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

// GoCodeGenExprs holds template variable names used in generated Go interpreter code.
type GoCodeGenExprs struct {
	Id               string
	Instance         string
	FuncProcIdOuter  string
	MethodProcId     string
	InterpreterDepth string

	EndConsumeFrameIfNecessary string
	InvokeInterpreterInner     string
	FuncProcIdInner            string
}

var DefaultGoCodeGenExprs = GoCodeGenExprs{
	Id:               "id",
	Instance:         "w",
	FuncProcIdOuter:  "f",
	MethodProcId:     "m",
	InterpreterDepth: "d",

	EndConsumeFrameIfNecessary: "if d == 0 {\ninst.endConsumeMessage()\n}\n",
	InvokeInterpreterInner:     "inst.dispatch(u, f2, d+1)\n",
	FuncProcIdInner:            "f2",
}

// canonicalTypeToReadMethod maps a canonical type to the corresponding
// UnmarshallReaderI method name. The UnmarshallReaderI interface uses
// "UInt" (capital I) for unsigned integers, unlike the MarshallWriterI
// which uses "Uint" (lowercase i).
func canonicalTypeToReadMethod(typ canonicaltypes.PrimitiveAstNodeI) (readMethod string, err error) {
	var typeCode string
	typeCode, _, _, err = codegen.GenerateGoCode(typ, encodingaspects.EmptyAspectSet)
	if err != nil {
		return
	}
	camel := naming.MustBeValidStylableName(typeCode).Convert(naming.UpperCamelCase).String()
	// Fix unsigned integer naming: MarshallWriterI uses WriteUint32 but
	// UnmarshallReaderI uses ReadUInt32 (capital I).
	camel = strings.Replace(camel, "Uint", "UInt", 1)
	readMethod = "Read" + camel
	return
}

func generateArgumentsReadPlain(w io.Writer, it iter.Seq2[naming.StylableName, canonicaltypes.PrimitiveAstNodeI], tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	for name, typ := range it {
		readMethod, err := canonicalTypeToReadMethod(typ)
		tracker.MergeError(err)
		_, err = fmt.Fprintf(w, "%s := u.%s()\n",
			name.Convert(naming.LowerCamelCase),
			readMethod,
		)
		tracker.MergeError(err)
	}
}

func generateArgumentsReadEvaluated(w io.Writer, it iter.Seq2[naming.StylableName, ir.TypeI], tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	var err error
	for name, _ := range it {
		_, err = fmt.Fprintf(w, `%s := u.ReadUInt32()
inst.dispatch(u, runtime.FuncProcId(%s), %s+1)
_ = %s
`,
			DefaultGoCodeGenExprs.FuncProcIdInner,
			DefaultGoCodeGenExprs.FuncProcIdInner,
			DefaultGoCodeGenExprs.InterpreterDepth,
			name.Convert(naming.LowerCamelCase),
		)
		tracker.MergeError(err)
	}
}

func generateCaseClause(w io.Writer, element naming.StylableName, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	_, err := fmt.Fprintf(w, "case FuncProcId%s:\n",
		element.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
}

func generateMethodCaseClause(w io.Writer, factoryName string, element naming.StylableName, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	_, err := fmt.Fprintf(w, "case %sMethodId%s:\n",
		factoryName,
		element.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
}

func generateFactoryDispatch(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", factory.Name)
	generateCaseClause(w, factory.Name, tracker)
	var err error

	{ // arguments
		_, err = fmt.Fprint(w, "// arguments\n")
		tracker.MergeError(err)
		if factory.IdentityArguments.HasId {
			_, err = fmt.Fprintf(w, "%s := u.ReadUInt64()\n",
				DefaultGoCodeGenExprs.Id)
			tracker.MergeError(err)
		}

		generateArgumentsReadPlain(w, factory.Arguments.PlainArguments.Iterate(), tracker)
		generateArgumentsReadEvaluated(w, factory.Arguments.EvaluatedArguments.Iterate(), tracker)
	}

	{ // construct
		_, err = fmt.Fprint(w, "// construct\n")
		tracker.MergeError(err)
		c := factory.ConstructionCode.CodeServerGo
		code := bytes.NewBuffer(make([]byte, 0, 256))
		if c != nil && !c.UseDefaultCode() {
			codeS := c.GetVerbatimCode()
			if codeS != "" {
				_, err = fmt.Fprintf(code, "%s := %s",
					DefaultGoCodeGenExprs.Instance,
					codeS)
				tracker.MergeError(err)
			}
		} else {
			_, err = fmt.Fprintf(code, "%s := inst.construct%s(%s",
				DefaultGoCodeGenExprs.Instance,
				factory.Name.Convert(naming.UpperCamelCase),
				DefaultGoCodeGenExprs.FuncProcIdOuter,
			)
			tracker.MergeError(err)
			if factory.IdentityArguments.HasId {
				_, err = fmt.Fprintf(code, ", %s", DefaultGoCodeGenExprs.Id)
				tracker.MergeError(err)
			}
			for _, arg := range factory.Arguments.PlainArguments.Names {
				_, err = fmt.Fprintf(code, ", %s", arg.Convert(naming.LowerCamelCase))
				tracker.MergeError(err)
			}
			_, err = fmt.Fprint(code, ")\n")
			tracker.MergeError(err)
		}
		_, err = code.WriteTo(w)
		tracker.MergeError(err)
	}

	if len(factory.BuilderMethods) > 0 { // methods
		_, err = fmt.Fprint(w, "// methods\n")
		tracker.MergeError(err)
		b := factory.Name.Convert(naming.UpperCamelCase).String()
		_, err = fmt.Fprintf(w, `for {
%s := %sMethodIdE(u.ReadUInt32())
switch %s {
`,
			DefaultGoCodeGenExprs.MethodProcId,
			b,
			DefaultGoCodeGenExprs.MethodProcId,
		)
		tracker.MergeError(err)

		_, err = fmt.Fprintf(w, "case %sMethodIdBuild:\ngoto done%s\n",
			b, b)
		tracker.MergeError(err)

		for _, mth := range factory.BuilderMethods {
			generateMethodCaseClause(w, b, mth.Spec.Name, tracker)
			generateArgumentsReadPlain(w, mth.Spec.PlainArguments.Iterate(), tracker)
			c := mth.CodeHolder.CodeServerGo
			if c != nil && !c.UseDefaultCode() {
				_, err = fmt.Fprint(w, c.GetVerbatimCode())
				tracker.MergeError(err)
			} else {
				_, err = fmt.Fprintf(w, "%s = %s.%s(",
					DefaultGoCodeGenExprs.Instance,
					DefaultGoCodeGenExprs.Instance,
					mth.Spec.Name.Convert(naming.UpperCamelCase),
				)
				tracker.MergeError(err)
				if len(mth.Spec.PlainArguments.Names) > 0 {
					_, err = fmt.Fprint(w, mth.Spec.PlainArguments.Names[0].Convert(naming.LowerCamelCase))
					tracker.MergeError(err)
					for _, pa := range mth.Spec.PlainArguments.Names[1:] {
						_, err = fmt.Fprintf(w, ", %s", pa.Convert(naming.LowerCamelCase))
						tracker.MergeError(err)
					}
				}
				_, err = fmt.Fprint(w, ")\n")
				tracker.MergeError(err)
			}
		}

		_, err = fmt.Fprintf(w, "}\n}\ndone%s:\n", b)
		tracker.MergeError(err)
	}

	hasDeferred := len(factory.DeferredBlockMaps) > 0
	if !hasDeferred {
		_, err = fmt.Fprint(w, DefaultGoCodeGenExprs.EndConsumeFrameIfNecessary)
		tracker.MergeError(err)
	}

	{ // apply
		_, err = fmt.Fprint(w, "// apply\n")
		tracker.MergeError(err)
		c := factory.ApplyCode.CodeServerGo
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprint(w, c.GetVerbatimCode())
			tracker.MergeError(err)
		}
	}

	if hasDeferred {
		_, err = fmt.Fprint(w, DefaultGoCodeGenExprs.EndConsumeFrameIfNecessary)
		tracker.MergeError(err)
	}

	_, err = fmt.Fprint(w, "\n")
	tracker.MergeError(err)
}

func generateProcedureDispatch(w io.Writer, procedural *ir.ProceduralNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", procedural.Name)
	generateCaseClause(w, procedural.Name, tracker)

	var err error
	{ // arguments
		_, err = fmt.Fprint(w, "// arguments\n")
		tracker.MergeError(err)
		if procedural.IdentityArguments.HasId {
			_, err = fmt.Fprintf(w, "%s := u.ReadUInt64()\n",
				DefaultGoCodeGenExprs.Id)
			tracker.MergeError(err)
		}

		generateArgumentsReadPlain(w, procedural.Arguments.PlainArguments.Iterate(), tracker)
		generateArgumentsReadEvaluated(w, procedural.Arguments.EvaluatedArguments.Iterate(), tracker)
	}

	_, err = fmt.Fprint(w, DefaultGoCodeGenExprs.EndConsumeFrameIfNecessary)
	tracker.MergeError(err)

	{ // apply
		_, err = fmt.Fprint(w, "// apply\n")
		tracker.MergeError(err)
		c := procedural.ApplyCode.CodeServerGo
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprint(w, c.GetVerbatimCode())
			tracker.MergeError(err)
		}
	}

	_, err = fmt.Fprint(w, "\n")
	tracker.MergeError(err)
}

func generateFetcherDispatch(w io.Writer, fetcher *ir.FetcherNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", fetcher.Name)
	generateCaseClause(w, fetcher.Name, tracker)

	_, err := fmt.Fprint(w, DefaultGoCodeGenExprs.EndConsumeFrameIfNecessary)
	tracker.MergeError(err)

	{ // apply
		_, err = fmt.Fprint(w, "// apply\n")
		tracker.MergeError(err)
		c := fetcher.ApplyCode.CodeServerGo
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprint(w, c.GetVerbatimCode())
			tracker.MergeError(err)
		}
	}

	_, err = fmt.Fprint(w, "\n")
	tracker.MergeError(err)
}

func generateMethodEnum(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	if len(factory.BuilderMethods) == 0 {
		return
	}
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	_, err := fmt.Fprintf(w, `type %sMethodIdE uint32
const (
	%sMethodIdBuild %sMethodIdE = 0

`, b, b, b)
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
const FuncProcIdOffset FuncProcIdE = 0
const (
`)
	tracker.MergeError(err)
	for idx, tl := range tls {
		b := tl.GetName().Convert(naming.UpperCamelCase).String()
		if slices.Contains(ReservedFuncProcIds, b) {
			continue
		}
		_, err = fmt.Fprintf(w, "\tFuncProcId%s FuncProcIdE = FuncProcIdOffset + %d\n",
			b, idx)
		tracker.MergeError(err)
	}
	_, err = fmt.Fprint(w, ")\n\n")
	tracker.MergeError(err)
}

func GenerateCode(wh WriterHolder, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (err error) {
	generateFuncProcIdEnum(wh.EnumWriter, tls, tracker)
	{
		_, err = fmt.Fprintf(wh.DispatchWriter, "switch %s {\n",
			DefaultGoCodeGenExprs.FuncProcIdOuter)
		tracker.MergeError(err)
	}
	err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
	if err != nil {
		err = eb.Build().Errorf("unable to create go interpreter code: %w", err)
		return
	}

	for _, tl := range tls {
		tracker.ResetStateAndError()

		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			generateFactoryDispatch(wh.DispatchWriter, tlt, tracker)
			generateMethodEnum(wh.EnumWriter, tlt, tracker)
		case *ir.ProceduralNode:
			generateProcedureDispatch(wh.DispatchWriter, tlt, tracker)
		case *ir.FetcherNode:
			generateFetcherDispatch(wh.DispatchWriter, tlt, tracker)
		}

		err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
		if err != nil {
			err = eb.Build().Stringer("name", tl.GetName()).Errorf("unable to create go interpreter code for node: %w", err)
			return
		}
	}

	tracker.ResetStateAndError()
	{
		_, err = fmt.Fprintf(wh.DispatchWriter, `default:
log.Printf("received unhandled procedure %%d", %s)
%s}
`, DefaultGoCodeGenExprs.FuncProcIdOuter, DefaultGoCodeGenExprs.EndConsumeFrameIfNecessary)
		tracker.MergeError(err)
	}
	if err != nil {
		err = eb.Build().Errorf("unable to create go interpreter code: %w", err)
		return
	}
	return
}
