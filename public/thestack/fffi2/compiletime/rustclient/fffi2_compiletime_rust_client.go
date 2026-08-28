package rustclient

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
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

var BuilderFactoryCodeGenExprs = ir.BuilderFactoryCodeGenExprs{
	InterpreterLifetime:        "'a",
	Id:                         "i",
	Instance:                   "w",
	SendMessage:                "self.io.flush()?;\n",
	MarkReturn:                 "r = true;\n",
	FuncProcIdOuter:            "f",
	MethodProcId:               "m",
	EguiContext:                "c",
	EguiUiOptionalOuter:        "u",
	InterpreterDepth:           "d",
	EndConsumeFrameIfNecessary: "if d == 0 {\nself.end_consume_message()?;\n}\n",

	InvokeInterpreterInner: `if u2.is_some() {
	self.interpret_inner(c,u2,&f2,d+1)?;
} else {
	self.interpret_inner(c,u,&f2,d+1)?;
}
`,
	FuncProcIdInner:     "f2",
	EguiUiOptionalInner: "u2",

	AtomsRegister0Reference:       "self.r0_atoms",
	AtomsRegister0Transfer:        "std::mem::take(&mut self.r0_atoms)\n",
	WidgetTextRegister0Reference:  "self.r1_widget_text",
	WidgetTextRegister0Transfer:   "std::mem::take(&mut self.r1_widget_text)\n",
	Color32Register0Transfer:      "self.r11_color32\n",
	CodeViewJobRegister0Reference: "self.r12_code_view_job",
	CodeViewJobRegister0Transfer:  "std::mem::take(&mut self.r12_code_view_job)\n",

	StyledSectionsRegister0Reference: "self.r24_styled_sections",
	StyledSectionsRegister0Transfer:  "std::mem::take(&mut self.r24_styled_sections)\n",
}

func resolveTypeToTransferRegister(t ir.TypeI) (consumeCode string, err error) {
	switch t.GetName().Convert(naming.LowerSnakeCase) {
	case "atoms":
		consumeCode = BuilderFactoryCodeGenExprs.AtomsRegister0Transfer
	case "widget_text":
		consumeCode = BuilderFactoryCodeGenExprs.WidgetTextRegister0Transfer
	case "color32":
		consumeCode = BuilderFactoryCodeGenExprs.Color32Register0Transfer
	case "code_view_job":
		consumeCode = BuilderFactoryCodeGenExprs.CodeViewJobRegister0Transfer
	case "styled_sections":
		consumeCode = BuilderFactoryCodeGenExprs.StyledSectionsRegister0Transfer
	default:
		err = eb.Build().Stringer("type", t.GetName()).Bool("isAbstract", t.IsAbstract()).Errorf("unabhe to resolve transfer register for given type")
	}
	return
}

func generateFactoryArgumentsHandlingPlain(w io.Writer, it iter.Seq2[naming.StylableName, canonicaltypes.PrimitiveAstNodeI], tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	for name, typ := range it {
		// No per-`let` lint attribute: `unused_mut` is allowed once on the
		// dispatch match that encloses every one of these reads.
		_, err := fmt.Fprintf(w, `let mut %s = self.io.read_plain_%s()?;
`,
			name.Convert(naming.LowerSnakeCase),
			typ,
		)
		tracker.MergeError(err)
	}
	return
}
func generateFactoryArgumentsHandlingEvaluated(w io.Writer, it iter.Seq2[naming.StylableName, ir.TypeI], tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (firstOut bool) {
	var err error
	for name, t := range it {
		var transferRegister string
		transferRegister, err = resolveTypeToTransferRegister(t)
		tracker.MergeError(err)
		_, err = fmt.Fprintf(w, `
let %s = {
	let (%s, _) = self.read_from_repr(FuncProcId::from_repr)?;
	let %s : &mut Option<&mut egui::Ui> = &mut None;
	%s
	%s
};
`,
			name.Convert(naming.LowerSnakeCase),
			BuilderFactoryCodeGenExprs.FuncProcIdInner,
			BuilderFactoryCodeGenExprs.EguiUiOptionalInner,
			BuilderFactoryCodeGenExprs.InvokeInterpreterInner,
			transferRegister,
		)
		tracker.MergeError(err)
	}
	return
}
func generateMatchClause(w io.Writer, typ string, element naming.StylableName, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	_, err := fmt.Fprintf(w, `%s::%s => {
    #[cfg(feature = "puffin")]
	puffin::profile_scope!("match %s::%s");
`, typ,
		element.Convert(naming.UpperCamelCase),
		typ,
		element.Convert(naming.UpperCamelCase))
	tracker.MergeError(err)
}
func generateFactoryInterpreterDispatchCode(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", factory.Name)
	generateMatchClause(w, "FuncProcId", factory.Name, tracker)
	var err error

	{ // arguments
		_, err = fmt.Fprint(w, "// arguments\n")
		tracker.MergeError(err)
		if factory.IdentityArguments.HasId {
			_, err = fmt.Fprintf(w, `    let %s = self.read_id()?;
`, BuilderFactoryCodeGenExprs.Id)
			tracker.MergeError(err)
		}

		generateFactoryArgumentsHandlingPlain(w, factory.Arguments.PlainArguments.Iterate(), tracker)
		generateFactoryArgumentsHandlingEvaluated(w, factory.Arguments.EvaluatedArguments.Iterate(), tracker)
	}

	{ // construct
		_, err = fmt.Fprint(w, "// construct\n")
		tracker.MergeError(err)
		c := factory.ConstructionCode.CodeClientRust
		code := bytes.NewBuffer(make([]byte, 0, 256))
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprintf(code, `
let mut %s = `, BuilderFactoryCodeGenExprs.Instance)
			tracker.MergeError(err)
			codeS := c.GetVerbatimCode()
			_, err = fmt.Fprint(code, codeS)
			tracker.MergeError(err)
			if codeS == "" {
				// no-op special case
				code.Reset()
			}
		} else {
			_, err = fmt.Fprintf(code, `    	let mut %s = self.construct_%s(%s,v,%s`,
				BuilderFactoryCodeGenExprs.Instance,
				factory.Name.Convert(naming.LowerSnakeCase),
				BuilderFactoryCodeGenExprs.EguiContext,
				BuilderFactoryCodeGenExprs.FuncProcIdOuter,
			)
			tracker.MergeError(err)
			if factory.IdentityArguments.HasId {
				_, err = fmt.Fprintf(code, `,%s`, BuilderFactoryCodeGenExprs.Id)
				tracker.MergeError(err)
			}
			for _, arg := range factory.Arguments.PlainArguments.Names {
				_, err = fmt.Fprintf(code, `,%s`, arg.Convert(naming.LowerSnakeCase))
				tracker.MergeError(err)
			}
			_, err = fmt.Fprint(code, `);
`)
			tracker.MergeError(err)
		}
		_, err = code.WriteTo(w)
		tracker.MergeError(err)
	}

	if len(factory.BuilderMethods) > 0 { // methods
		_, err = fmt.Fprint(w, "// methods\n")
		tracker.MergeError(err)
		b := factory.Name.Convert(naming.UpperCamelCase).String()
		_, err = fmt.Fprintf(w, `loop {
    let (%s,_) = self.read_from_repr(%sBuilderMethodId::from_repr)?;
    match %s {
`,
			BuilderFactoryCodeGenExprs.MethodProcId,
			b,
			BuilderFactoryCodeGenExprs.MethodProcId,
		)
		tracker.MergeError(err)
		if len(factory.BuilderMethods) > 0 {
			_, err = fmt.Fprintf(w, `%sBuilderMethodId::Build => {
    break;
}
`, b)
			tracker.MergeError(err)
			for _, mth := range factory.BuilderMethods {
				generateMatchClause(w, b+"BuilderMethodId", mth.Spec.Name, tracker)
				generateFactoryArgumentsHandlingPlain(w, mth.Spec.PlainArguments.Iterate(), tracker)
				generateFactoryArgumentsHandlingEvaluated(w, mth.Spec.EvaluatedArguments.Iterate(), tracker)
				c := mth.CodeHolder.CodeClientRust
				if c != nil && !c.UseDefaultCode() {
					_, err = fmt.Fprint(w, c.GetVerbatimCode())
					tracker.MergeError(err)
				} else {
					_, err = fmt.Fprintf(w, `%s = %s.%s(`,
						BuilderFactoryCodeGenExprs.Instance,
						BuilderFactoryCodeGenExprs.Instance,
						mth.Spec.Name.Convert(naming.LowerSnakeCase),
					)
					tracker.MergeError(err)
					if len(mth.Spec.PlainArguments.Names) > 0 {
						_, err = fmt.Fprint(w, mth.Spec.PlainArguments.Names[0].Convert(naming.LowerSnakeCase))
						tracker.MergeError(err)
						for _, pa := range mth.Spec.PlainArguments.Names[1:] {
							_, err = fmt.Fprintf(w, ", %s", pa.Convert(naming.LowerSnakeCase))
							tracker.MergeError(err)
						}
					}
					_, err = fmt.Fprint(w, ");\n")
					tracker.MergeError(err)
				}
				_, err = fmt.Fprint(w, `
}
`)
				tracker.MergeError(err)
			}
		}
		_, err = fmt.Fprint(w, `}
}
`)
		tracker.MergeError(err)
	}
	// For nodes with DeferredBlockMaps, the apply code reads the block map
	// from the IPC stream — so end_consume_message must come AFTER the apply
	// code, not before. Otherwise the frame validation skips the block data.
	hasDeferred := len(factory.DeferredBlockMaps) > 0
	if !hasDeferred {
		_, err = fmt.Fprint(w, BuilderFactoryCodeGenExprs.EndConsumeFrameIfNecessary)
		tracker.MergeError(err)
	}

	{ // apply
		_, err = fmt.Fprint(w, "// apply\n")
		tracker.MergeError(err)
		c := factory.ApplyCode.CodeClientRust
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprint(w, c.GetVerbatimCode())
			tracker.MergeError(err)
		}
	}

	if hasDeferred {
		_, err = fmt.Fprint(w, BuilderFactoryCodeGenExprs.EndConsumeFrameIfNecessary)
		tracker.MergeError(err)
	}

	_, err = fmt.Fprint(w, `
}
`)
	tracker.MergeError(err)
}
func generateProcedureInterpreterDispatchCode(w io.Writer, procedural *ir.ProceduralNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", procedural.Name)
	generateMatchClause(w, "FuncProcId", procedural.Name, tracker)

	var err error
	{ // arguments
		_, err = fmt.Fprint(w, "// arguments\n")
		tracker.MergeError(err)
		if procedural.IdentityArguments.HasId {
			_, err = fmt.Fprintf(w, `    let %s = self.read_id()?;
`, BuilderFactoryCodeGenExprs.Id)
			tracker.MergeError(err)
		}

		generateFactoryArgumentsHandlingPlain(w, procedural.Arguments.PlainArguments.Iterate(), tracker)
		generateFactoryArgumentsHandlingEvaluated(w, procedural.Arguments.EvaluatedArguments.Iterate(), tracker)
	}

	_, err = fmt.Fprint(w, BuilderFactoryCodeGenExprs.EndConsumeFrameIfNecessary)
	tracker.MergeError(err)

	{ // apply
		_, err = fmt.Fprint(w, "// apply\n")
		tracker.MergeError(err)
		c := procedural.ApplyCode.CodeClientRust
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprint(w, c.GetVerbatimCode())
			tracker.MergeError(err)
		}
	}

	_, err = fmt.Fprint(w, `
}
`)
	tracker.MergeError(err)
}
func generateFetcherInterpreterDispatchCode(w io.Writer, fetcher *ir.FetcherNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	tracker.ErrorMessagePrefix = fmt.Sprintf("%s ", fetcher.Name)
	generateMatchClause(w, "FuncProcId", fetcher.Name, tracker)

	_, err := fmt.Fprint(w, BuilderFactoryCodeGenExprs.EndConsumeFrameIfNecessary)
	tracker.MergeError(err)

	{ // apply
		_, err = fmt.Fprint(w, "// apply\n")
		tracker.MergeError(err)
		c := fetcher.ApplyCode.CodeClientRust
		if c != nil && !c.UseDefaultCode() {
			_, err = fmt.Fprint(w, c.GetVerbatimCode())
			tracker.MergeError(err)
		}
	}

	_, err = fmt.Fprint(w, `
}
`)
	tracker.MergeError(err)
}
func generateMethodEnum(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	b := factory.Name.Convert(naming.UpperCamelCase).String()
	_, err := fmt.Fprintf(w, `#[derive(strum::FromRepr, Debug, PartialEq, Eq)]
#[repr(u32)]
pub enum %sBuilderMethodId {
    Build = 0,
`, b)
	tracker.MergeError(err)

	for idx, method := range factory.BuilderMethods {
		_, err = fmt.Fprintf(w, "    %s = %d,\n",
			method.Spec.Name.Convert(naming.UpperCamelCase),
			idx+1,
		)
		tracker.MergeError(err)
	}
	_, err = fmt.Fprint(w, "}\n\n")
	tracker.MergeError(err)
}
func generateFactoryEnum(w io.Writer, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	// File-level, because this is the first thing written to the enum file:
	// every enum below mirrors the IDL, so a variant no build path names is
	// the IDL's shape rather than a mistake, and the attribute saying so is
	// itself generated (which is what `clippy::allow_attributes` objects to).
	_, err := fmt.Fprint(w, `#![allow(dead_code, clippy::allow_attributes)]

use crate::imzero2::fenums;
#[derive(strum::FromRepr, Debug, PartialEq, Eq)]
#[repr(u32)]
pub enum FuncProcId {
`)
	tracker.MergeError(err)
	for idx, tl := range tls {
		b := tl.GetName().Convert(naming.UpperCamelCase).String()
		if slices.Contains(ReservedFuncProcIds, b) {
			continue
		}

		if idx == 0 {
			// `+ 0` is clippy::identity_op, and this file is not editable.
			_, err = fmt.Fprintf(w, `	%s = fenums::FUNC_PROC_ID_OFFSET,
`, b)
		} else {
			_, err = fmt.Fprintf(w, `	%s = fenums::FUNC_PROC_ID_OFFSET + %d,
`, b, idx)
		}
		tracker.MergeError(err)
	}
	_, err = fmt.Fprint(w, `}
`)
	tracker.MergeError(err)
}

func GenerateCode(wh WriterHolder, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) (err error) {
	generateFactoryEnum(wh.EnumWriter, tls, tracker)
	{
		// One lint allowance for the whole dispatch, because no hand edit
		// inside it survives a regeneration.
		//
		// The rustc half is structural: an opcode's arguments are read off the
		// wire whether or not the widget's apply code names them (the read is
		// the point — it advances the frame cursor), and an evaluated
		// builder's holder is only read when that builder is consumed as a
		// nested argument, never at top level. `unused_mut` rides along so the
		// per-`let` attribute this used to emit at every argument can go.
		//
		// The clippy half is the shape of the IDL's Rust templates as they
		// come out the other side — `{{EguiUiOptionalOuter}}.as_mut().unwrap()`
		// alone accounts for most of `unwrap_used`. Correctness lints are
		// deliberately absent from this list: one firing here would be a
		// generator bug, and should still be heard. Extend the list when a new
		// template shape trips a lint; do not reach for `clippy::all`.
		_, err = fmt.Fprintf(wh.DispatchWriter, `#[allow(
    unused_variables,
    unused_assignments,
    unused_mut,
    clippy::allow_attributes,
    clippy::assign_op_pattern,
    clippy::collapsible_if,
    clippy::elidable_lifetime_names,
    clippy::explicit_into_iter_loop,
    clippy::explicit_iter_loop,
    clippy::indexing_slicing,
    clippy::let_underscore_must_use,
    clippy::let_underscore_untyped,
    clippy::let_unit_value,
    clippy::missing_assert_message,
    clippy::mut_mut,
    clippy::needless_return,
    clippy::redundant_type_annotations,
    clippy::semicolon_if_nothing_returned,
    clippy::unnecessary_unwrap,
    clippy::unused_unit,
    clippy::unwrap_used,
    clippy::useless_conversion,
    clippy::useless_let_if_seq
)]
match %s {
`,
			BuilderFactoryCodeGenExprs.FuncProcIdOuter)
		tracker.MergeError(err)
	}
	err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
	if err != nil {
		err = eb.Build().Errorf("unable to create rust code: %w", err)
		return
	}

	for _, tl := range tls {
		tracker.ResetStateAndError()

		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			generateFactoryInterpreterDispatchCode(wh.DispatchWriter, tlt, tracker)
			generateMethodEnum(wh.EnumWriter, tlt, tracker)
			break
		case *ir.ProceduralNode:
			generateProcedureInterpreterDispatchCode(wh.DispatchWriter, tlt, tracker)
			break
		case *ir.FetcherNode:
			generateFetcherInterpreterDispatchCode(wh.DispatchWriter, tlt, tracker)
			break
		}

		err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
		if err != nil {
			err = eb.Build().Stringer("name", tl.GetName()).Errorf("unable to create rust code for factory: %w", err)
			return
		}
	}

	tracker.ResetStateAndError()
	{
		// The arms above cover every FuncProcId variant, so this one is
		// unreachable today and rustc says so. It stays as the landing site
		// for a peer that sends an opcode this build does not know — a
		// mismatched Go/Rust pair — which `from_repr` cannot produce but a
		// future non-exhaustive id type could.
		_, err = fmt.Fprintf(wh.DispatchWriter, `
#[allow(unreachable_patterns)]
_ => {
        tracing::warn!("received unhandled procedure {:?}", f);
        %s
    }
}
`, BuilderFactoryCodeGenExprs.EndConsumeFrameIfNecessary)
		tracker.MergeError(err)
	}
	if err != nil {
		err = eb.Build().Errorf("unable to create rust code: %w", err)
		return
	}
	return
}
