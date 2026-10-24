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

	// StyledSections is the ADR-0130 L3 overlay channel: an evaluated
	// argument builder accumulating sparse style spans (underline,
	// background, strikethrough, italics) over a buffer somebody else owns.
	// Deliberately parallel to CodeViewJob rather than an extension of its
	// Section: the color-only struct is shared with four read-only codeview
	// producers, and this channel carries no text of its own — it decorates
	// whatever buffer the consuming widget holds.
	//
	// Usage: StyledSections().Section(12, 19, underline|background, red).Keep()
	//
	// flags are the STYLE_* bits from text_edit_highlight.rs; a section with
	// no bits set expresses nothing and is dropped Rust-side. A font overlay
	// (ADR-0334 §SD6) goes through sectionFont, below, on the same register.
	nodes = append(nodes, idl.NewBuilderFactoryNode("styledSections").
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("section").
			Arg("byteStart", ctabb.U32).
			Arg("byteStop", ctabb.U32).
			Arg("flags", ctabb.U32).
			EvaluatedArg("col", structColor32()).AsColor().
			CodeClientRust(rustClientCode("{{StyledSectionsRegister0Reference}}.push(text_edit_highlight::StyledSection{byte_start, byte_stop, flags, color: col, family: text_edit_highlight::FONT_FAMILY_INHERIT, size: 0.0});\n")).
			EndMethod().
			// sectionFont (ADR-0334 §SD6): a font overlay — family (0 inherit,
			// 1 proportional, 2 monospace) and a size factor of the editor's
			// base font (<= 0 keeps it). Rides the styled register under the
			// STYLE_FONT flag; the layouter splits colour sections at its
			// boundaries rather than overlap-merging it, since a font change
			// inside one shaping run would land on the wrong bytes.
			BeginMethod("sectionFont").
			Arg("byteStart", ctabb.U32).
			Arg("byteStop", ctabb.U32).
			Arg("family", ctabb.U32).
			Arg("size", ctabb.F32).
			CodeClientRust(rustClientCode("{{StyledSectionsRegister0Reference}}.push(text_edit_highlight::StyledSection{byte_start, byte_stop, flags: text_edit_highlight::STYLE_FONT, color: egui::Color32::TRANSPARENT, family, size});\n")).
			EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`{
	{{StyledSectionsRegister0Reference}}.clear();
	()
};
`)).
		WithSettingRetained(true).
		WithReturnType(structStyledSections()).
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
