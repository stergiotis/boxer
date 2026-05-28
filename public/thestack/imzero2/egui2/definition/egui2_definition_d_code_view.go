//go:build llm_generated_opus46

package definition

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsCodeView() (nodes []*ir.BuilderFactoryNode) {
	nodes = make([]*ir.BuilderFactoryNode, 0, 4)

	// CodeViewJob is an evaluated argument builder that accumulates
	// text + colored sections for LayoutJob construction on the Rust side.
	// Usage: CodeViewJob("SELECT ...").Section(0, 6, blue).Section(7, 11, white).Keep()
	nodes = append(nodes, idl.NewBuilderFactoryNode("codeViewJob").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("section").
			Arg("byteStart", ctabb.U32).
			Arg("byteStop", ctabb.U32).
			EvaluatedArg("col", structColor32()).AsColor().
			CodeClientRust(rustClientCode("{{CodeViewJobRegister0Reference}}.sections.push(code_view::Section{byte_start, byte_stop, color: col});\n")).
			EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`{
	{{CodeViewJobRegister0Reference}}.sections.clear();
	{{CodeViewJobRegister0Reference}}.text = text;
	()
};
`)).
		WithSettingRetained(true).
		WithReturnType(structCodeViewJob()).
		Build())

	// CodeView renders syntax-highlighted text via a cached LayoutJob.
	// It consumes a CodeViewJob evaluated arg and renders it as a selectable Label.
	nodes = append(nodes, idl.NewBuilderFactoryNode("codeView").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("job", structCodeViewJob()).Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("selectable").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.selectable(val);\n")).EndMethod().
			BeginMethod("wrap").
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.wrap_mode(egui::TextWrapMode::Wrap);\n")).EndMethod().
			BeginMethod("truncate").
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.wrap_mode(egui::TextWrapMode::Truncate);\n")).EndMethod().
			BeginMethod("extend").
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.wrap_mode(egui::TextWrapMode::Extend);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`{
	let layout_job = code_view::get_or_build_layout_job(&mut self.code_view_cache, &job, c);
	egui::Label::new(layout_job).selectable(true)
};
`)).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithApplyCodeClientRust(applyCodeWidgetRust(true)).
		WithReturnType(structCodeView()).
		Build())

	return
}
