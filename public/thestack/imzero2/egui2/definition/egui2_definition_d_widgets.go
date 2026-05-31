//go:build llm_generated_opus47

package definition

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsWidgetProc() (widgets []*ir.ProceduralNode) {
	widgets = make([]*ir.ProceduralNode, 0, 8)
	widgets = append(widgets, idl.NewProceduralNode("addSpace").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("amount", ctabb.F32).
			Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().add_space(amount);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("endRow").
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().end_row();
					}
`)).
		Build())
	// scrollToCursor — schedules the enclosing ScrollArea to bring the
	// current cursor position into view at the requested alignment. align
	// follows egui's convention: 0 = Min (top of the scrollable area),
	// 1 = Center, 2 = Max (bottom). The markdown widget consumes this op
	// before emitting a target heading so help readers land at the
	// requested section after a section-nav click; any other widget
	// rendered inside a ScrollArea can call this similarly.
	//
	// Outside a ScrollArea the op is a no-op — egui silently drops the
	// request when there is no parent that can apply it.
	widgets = append(widgets, idl.NewProceduralNode("scrollToCursor").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("align", ctabb.U8).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let a = match align {
							0 => egui::Align::Min,
							1 => egui::Align::Center,
							_ => egui::Align::Max,
						};
						{{EguiUiOptionalOuter}}.as_mut().unwrap().scroll_to_cursor(Some(a));
					}
`)).
		Build())
	// copyTextToClipboard — copy a UTF-8 string to the viewport clipboard via
	// egui's Context::copy_text (egui >=0.34, resolved here to 0.34.2). This is
	// the mechanism half of the clipboard.write capability (ADR-0026 Update
	// 2026-05-30): the only way to reach the OS clipboard from this stack, since
	// Go is CGO-free and the real clipboard belongs to the egui/winit viewport,
	// not the Go process. The clipboardbroker accumulates copy requests off the
	// bus; the host frame loop drains them and emits this op once per pending
	// string.
	//
	// copy_text is a Context method (it pushes an OutputCommand), not a Ui
	// method, so this uses the interpreter's frame-scoped `c: &egui::Context`
	// directly rather than the optional Ui — same handle the codeView node uses
	// for its layout-job cache. That deliberately removes any active-Ui-scope
	// requirement: the host can drain and emit after its panels have closed.
	// The FFFI2 string arg arrives as an owned String, which copy_text consumes
	// directly.
	widgets = append(widgets, idl.NewProceduralNode("copyTextToClipboard").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
		WithApplyCodeClientRust(rustClientCode("c.copy_text(text);\n")).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiSetMinWidth").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("width", ctabb.F32).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().set_min_width(width);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiSetMinHeight").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("height", ctabb.F32).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().set_min_height(height);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiSetMaxWidth").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("width", ctabb.F32).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().set_max_width(width);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiSetMaxHeight").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("height", ctabb.F32).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().set_max_height(height);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiSetWidth").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("width", ctabb.F32).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().set_width(width);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiSetHeight").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("height", ctabb.F32).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().set_height(height);
					}
`)).
		Build())
	widgets = append(widgets, idl.NewProceduralNode("uiDisable").
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().disable();
					}
`)).
		Build())
	return
}

func definitionsWidget() (widgets []*ir.BuilderFactoryNode) {
	widgets = make([]*ir.BuilderFactoryNode, 0, 32)
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("separator").
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("horizontal").CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.horizontal();\n")).EndMethod().
				BeginMethod("vertical").CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.vertical();\n")).EndMethod().
				BeginMethod("spacing").Arg("spacing", ctabb.F32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.spacing(spacing);\n")).EndMethod().
				BeginMethod("grow").Arg("extra", ctabb.F32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.grow(extra);\n")).EndMethod().
				BeginMethod("shrink").Arg("shrink", ctabb.F32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.shrink(shrink);\n")).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Separator::default();\n")).
			WithSettingImmediate(true).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("label").
			AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("selectable").Arg("val", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.selectable(val);\n")).EndMethod().
				BeginMethod("wrap").EndMethod().
				BeginMethod("truncate").EndMethod().
				BeginMethod("extend").EndMethod().
				Build()...).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithConstructionCodeClientRust(rustClientCode("egui::Label::new(text);\n")).
			WithReturnType(structLabel()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("labelWidgetText").
			AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("widgetText", structWidgetText()).Build()).
			AddMethods(idl.NewMethodBuilder().
				Build()...).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithConstructionCodeClientRust(rustClientCode("egui::Label::new(widget_text);\n")).
			WithReturnType(structLabel()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("labelAtoms").
			AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("atoms", structAtoms()).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("wrap").CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.wrap_mode(egui::TextWrapMode::Wrap);")).EndMethod().
				BeginMethod("truncate").CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.wrap_mode(egui::TextWrapMode::Truncate);")).EndMethod().
				BeginMethod("extend").CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.wrap_mode(egui::TextWrapMode::Extend);")).EndMethod().
				Build()...).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithConstructionCodeClientRust(rustClientCode(`{
	// Flatten atoms into a single LayoutJob so egui's text shaper word-wraps
	// across style boundaries. Atoms' native AtomLayout only lets one atom
	// (the first text atom, auto-shrunk) wrap inside itself; every other
	// atom is sized to its intrinsic width. In paragraphs whose non-shrink
	// atoms exceed the available width, the shrink atom collapses to ~0
	// and the shaper falls back to character-by-character wrapping. A
	// LayoutJob with one section per styled span sidesteps that — the
	// shaper sees one continuous run and breaks on word boundaries.
	let style = c.style();
	let mut lj = egui::text::LayoutJob::default();
	for atom in atoms.into_iter() {
		if let egui::AtomKind::Text(wt) = atom.kind {
			match wt {
				egui::WidgetText::RichText(rt) => {
					std::sync::Arc::unwrap_or_clone(rt).append_to(
						&mut lj,
						&style,
						egui::FontSelection::Default,
						egui::Align::Center,
					);
				}
				egui::WidgetText::Text(s) => {
					let format = egui::TextFormat {
						font_id: egui::FontSelection::Default.resolve(&style),
						color: style.visuals.text_color(),
						..Default::default()
					};
					lj.append(&s, 0.0, format);
				}
				egui::WidgetText::LayoutJob(j) => {
					let mut j = std::sync::Arc::unwrap_or_clone(j);
					let base = lj.text.len();
					lj.text.push_str(&j.text);
					for mut sec in j.sections.drain(..) {
						sec.byte_range.start += base;
						sec.byte_range.end += base;
						lj.sections.push(sec);
					}
				}
				egui::WidgetText::Galley(_) => {}
			}
		}
	}
	egui::Label::new(lj)
};
`)).
			WithReturnType(structLabel()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("button").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("atoms", structAtoms()).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("frame").Arg("val", ctabb.B).EndMethod().
				BeginMethod("small").EndMethod().
				BeginMethod("wrap").EndMethod().
				BeginMethod("truncate").EndMethod().
				BeginMethod("selected").Arg("selected", ctabb.B).EndMethod().
				BeginMethod("frameWhenInactive").Arg("val", ctabb.B).EndMethod().
				BeginMethod("rightText").Arg("text", ctabb.S).EndMethod().
				BeginMethod("shortcut_text").Arg("text", ctabb.S).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Button::new(atoms);\n")).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithReturnType(structButton()).
			Build())

	{
		p := canonicaltypes.NewParser()
		for _, f := range r9Types {
			ff := naming.MustBeValidStylableName(f)
			c := p.MustParsePrimitiveTypeAst(f)
			widgets = append(widgets,
				idl.NewBuilderFactoryNode("slider"+ff.Convert(naming.UpperCamelCase)).
					WithIdentityId(true).
					AddArguments(idl.NewArgumentsBuilder().PlainArg("val", c).Build()).
					AddArguments(idl.NewArgumentsBuilder().PlainArg("rangeBeginIncl", c).Build()).
					AddArguments(idl.NewArgumentsBuilder().PlainArg("rangeEndIncl", c).Build()).
					AddMethods(idl.NewMethodBuilder().
						BeginMethod("showValue").Arg("enabled", ctabb.B).EndMethod().
						BeginMethod("prefix").Arg("prefix", ctabb.S).EndMethod().
						BeginMethod("suffix").Arg("suffix", ctabb.S).EndMethod().
						BeginMethod("text").Arg("text", ctabb.S).EndMethod().
						BeginMethod("vertical").EndMethod().
						BeginMethod("logarithmic").Arg("enabled", ctabb.B).EndMethod().
						BeginMethod("smallestPositive").Arg("smallestNum", ctabb.F64).EndMethod().
						BeginMethod("largestFinite").Arg("largestNum", ctabb.F64).EndMethod().
						BeginMethod("smartAim").Arg("enabled", ctabb.B).EndMethod().
						BeginMethod("dragValueSpeed").Arg("speed", ctabb.F64).EndMethod().
						BeginMethod("minDecimals").Arg("digits", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.min_decimals(digits as usize);\n")).EndMethod().
						BeginMethod("maxDecimals").Arg("digits", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.max_decimals(digits as usize);\n")).EndMethod().
						BeginMethod("fixedDecimals").Arg("digits", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.fixed_decimals(digits as usize);\n")).EndMethod().
						BeginMethod("trailingFill").Arg("enabled", ctabb.B).EndMethod().
						BeginMethod("binary").Arg("min_width", ctabb.U32).Arg("twosComplement", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.binary(min_width as usize,twos_complement);\n")).EndMethod().
						BeginMethod("octal").Arg("min_width", ctabb.U32).Arg("twosComplement", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.octal(min_width as usize,twos_complement);\n")).EndMethod().
						BeginMethod("hexadecimal").Arg("min_width", ctabb.U32).Arg("twosComplement", ctabb.B).Arg("upper", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.hexadecimal(min_width as usize,twos_complement,upper);\n")).EndMethod().
						BeginMethod("integer").EndMethod().
						BeginMethod("update_while_editing").Arg("update", ctabb.B).EndMethod().
						Build()...).
					WithConstructionCodeClientRust(rustClientCode("egui::Slider::new(&mut val,range_begin_incl..=range_end_incl);\n")).
					WithSettingImmediate(true).
					WithSettingRetained(true).
					WithApplyCodeClientRust(applyCodeWidgetRustOnEvent(true, respEventChanged,
						rustClientCode("self.r9_"+ff.Convert(naming.LowerCamelCase).String()+"_push({{Id}}.value(),val);\n"))).
					WithReturnType(structSlider()).
					Build())
			widgets = append(widgets,
				idl.NewBuilderFactoryNode("dragValue"+ff.Convert(naming.UpperCamelCase)).
					WithIdentityId(true).
					AddArguments(idl.NewArgumentsBuilder().PlainArg("val", c).Build()).
					AddMethods(idl.NewMethodBuilder().
						BeginMethod("speed").Arg("speed", ctabb.F64).EndMethod().
						BeginMethod("prefix").Arg("prefix", ctabb.S).EndMethod().
						BeginMethod("suffix").Arg("suffix", ctabb.S).EndMethod().
						BeginMethod("minDecimals").Arg("digits", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.min_decimals(digits as usize);\n")).EndMethod().
						BeginMethod("maxDecimals").Arg("digits", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.max_decimals(digits as usize);\n")).EndMethod().
						BeginMethod("fixedDecimals").Arg("digits", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.fixed_decimals(digits as usize);\n")).EndMethod().
						BeginMethod("binary").Arg("min_width", ctabb.U32).Arg("twosComplement", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.binary(min_width as usize,twos_complement);\n")).EndMethod().
						BeginMethod("octal").Arg("min_width", ctabb.U32).Arg("twosComplement", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.octal(min_width as usize,twos_complement);\n")).EndMethod().
						BeginMethod("hexadecimal").Arg("min_width", ctabb.U32).Arg("twosComplement", ctabb.B).Arg("upper", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.hexadecimal(min_width as usize,twos_complement,upper);\n")).EndMethod().
						BeginMethod("update_while_editing").Arg("update", ctabb.B).EndMethod().
						Build()...).
					WithConstructionCodeClientRust(rustClientCode("egui::DragValue::new(&mut val);\n")).
					WithSettingImmediate(true).
					WithSettingRetained(true).
					WithApplyCodeClientRust(applyCodeWidgetRustOnEvent(true, respEventChanged,
						rustClientCode("self.r9_"+ff.Convert(naming.LowerCamelCase).String()+"_push({{Id}}.value(),val);\n"))).
					WithReturnType(structDragValue()).
					Build())
		}
	}
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("spinner").
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("size").Arg("size", ctabb.F32).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Spinner::new();\n")).
			WithSettingImmediate(true).
			WithReturnType(structSpinner()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("checkbox").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("checked", ctabb.B).
				PlainArg("text", ctabb.S).
				//EvaluatedArg("atoms", structAtoms()). // FIXME signature of CheckBox::new has wrong lifetimes (lifetime unification)
				Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("indeterminate").Arg("indeterminate", ctabb.B).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Checkbox::new(&mut checked,text);\n")).
			WithSettingImmediate(true).
			WithApplyCodeClientRust(applyCodeWidgetRustOnEvent(true, respEventChanged,
				rustClientCode("self.r10_push({{Id}}.value(),checked);\n"))).
			WithReturnType(structCheckBox()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("radioButton").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("checked", ctabb.B).
				EvaluatedArg("atoms", structAtoms()).
				Build()).
			AddMethods(idl.NewMethodBuilder().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::RadioButton::new(checked,atoms);\n")).
			WithSettingImmediate(true).
			WithApplyCodeClientRust(applyCodeWidgetRustOnEvent(true, respEventClicked,
				rustClientCode("self.r10_push({{Id}}.value(), true);\n"))).
			WithReturnType(structCheckBox()).
			Build())
	// Hyperlink / HyperlinkTo carry their url alongside the rendered text.
	// The default apply just shows the widget and discards the Response;
	// here we capture it so the SVG exporter can wrap the matching text
	// shape in `<a href="…">`. `link_zones` is a per-frame register on
	// `ImZeroFffi`, cleared in `prepare_next_frame`.
	hyperlinkApply := rustClientCode(`
				let resp = self.apply_widget(w, u, f, None);
				if let Some(r) = resp {
					if let Ok(mut zones) = self.link_zones.lock() {
						zones.push(crate::imzero2::svgexport::LinkZone {
							rect: r.rect,
							url: url.clone(),
						});
					}
				}
`)
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("hyperlink").
			AddArguments(idl.NewArgumentsBuilder().PlainArg("url", ctabb.S).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("openInNewTab").Arg("enabled", ctabb.B).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Hyperlink::from_label_and_url(url.clone(), url.clone());\n")).
			WithApplyCodeClientRust(hyperlinkApply).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithReturnType(structHyperlink()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("hyperlinkTo").
			AddArguments(idl.NewArgumentsBuilder().PlainArg("label", ctabb.S).PlainArg("url", ctabb.S).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("openInNewTab").Arg("enabled", ctabb.B).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Hyperlink::from_label_and_url(label, url.clone());\n")).
			WithApplyCodeClientRust(hyperlinkApply).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithReturnType(structHyperlink()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("selectableLabel").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("checked", ctabb.B).
				PlainArg("text", ctabb.S).
				Build()).
			WithConstructionCodeClientRust(rustClientCode("egui::SelectableLabel::new(checked, text);\n")).
			WithSettingImmediate(true).
			WithReturnType(structSelectableLabel()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("progressBar").
			AddArguments(idl.NewArgumentsBuilder().PlainArg("progress", ctabb.F32).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("text").Arg("text", ctabb.S).EndMethod().
				BeginMethod("animate").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.animate(enabled && !self.animation_freeze);\n")).EndMethod().
				BeginMethod("showPercentage").EndMethod().
				BeginMethod("desiredWidth").Arg("width", ctabb.F32).EndMethod().
				BeginMethod("desiredHeight").Arg("height", ctabb.F32).EndMethod().
				BeginMethod("cornerRadius").Arg("radius", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.corner_radius(radius);\n")).EndMethod().
				BeginMethod("fill").EvaluatedArg("col", structColor32()).AsColor().CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.fill(col);\n")).EndMethod().
				Build()...).
			// Default fill to ACCENT_DEFAULT (L=0.80). egui's ProgressBar otherwise reads
			// visuals.selection.bg_fill — which IDS pins at ACCENT_SUBTLE (L=0.20) for
			// SelectableLabel text contrast (ADR-0037), giving a near-invisible bar over
			// extreme_bg_color (L=0.06). Explicit `.fill(col)` from Go still overrides.
			WithConstructionCodeClientRust(rustClientCode("egui::ProgressBar::new(progress).fill(imzero2_egui::style::tokens::palette_generated::ACCENT_DEFAULT);\n")).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithReturnType(structProgressBar()).
			Build())
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("textEdit").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("text", ctabb.S).
				PlainArg("multiline", ctabb.B).
				Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("codeEditor").EndMethod().
				BeginMethod("frame").Arg("frame", ctabb.B).CodeClientRust(rustClientCode("if !frame { {{Instance}} = {{Instance}}.frame(egui::Frame::NONE); }\n")).EndMethod().
				BeginMethod("hintText").Arg("hint", ctabb.S).EndMethod().
				BeginMethod("password").Arg("password", ctabb.B).EndMethod().
				BeginMethod("interactive").Arg("interactive", ctabb.B).EndMethod().
				BeginMethod("desired_width").Arg("width", ctabb.F32).EndMethod().
				BeginMethod("desired_rows").Arg("rows", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.desired_rows(rows as usize);\n")).EndMethod().
				BeginMethod("lock_focus").Arg("lock", ctabb.B).EndMethod().
				BeginMethod("cursor_at_end").Arg("val", ctabb.B).EndMethod().
				BeginMethod("clip_text").Arg("val", ctabb.B).EndMethod().
				BeginMethod("char_limit").Arg("chars", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.char_limit(chars as usize);\n")).EndMethod().
				BeginMethod("insertAtCursor").Arg("snippet", ctabb.S).CodeClientRust(rustClientCode("self.text_edit_pending_insert = Some(snippet);\n")).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("if multiline { egui::TextEdit::multiline(&mut text).id({{Id}}) } else { egui::TextEdit::singleline(&mut text).id({{Id}}) };\n")).
			WithSettingImmediate(true).
			// Apply: keep the user-edit changed-push, and fold in the
			// programmatic insert-at-cursor (TextEditFluid.InsertAtCursor).
			// text is moved into r9_s_push, so push exactly once at the end
			// gated on a single `changed` (user-edited OR snippet-inserted) —
			// pushing twice would move text twice. See ADR-0063.
			WithApplyCodeClientRust(ir.MergeVerbatimCode(
				rustClientCode("let resp ="),
				applyCodeWidgetRust(true),
				rustClientCode(`
let mut changed = resp.is_some() && resp.unwrap().changed();
// A builder method stashed the snippet on self.text_edit_pending_insert.
// Splice it at the editor's persisted caret (replacing any selection) and
// force the push: a programmatic edit never sets egui's .changed(). With no
// stored cursor (editor never focused) we append at end.
if let Some(ins) = self.text_edit_pending_insert.take() {
	let ctx_opt = {{EguiUiOptionalOuter}}.as_deref().map(|ui| ui.ctx().clone());
	let end = text.chars().count();
	let range = ctx_opt
		.as_ref()
		.and_then(|ctx| egui::text_edit::TextEditState::load(ctx, {{Id}}))
		.and_then(|st| st.cursor.char_range())
		.map(|cr| cr.as_sorted_char_range())
		.unwrap_or(end..end);
	let caret = splice_text_at_cursor(&mut text, &ins, range);
	if let Some(ctx) = ctx_opt {
		if let Some(mut st) = egui::text_edit::TextEditState::load(&ctx, {{Id}}) {
			st.cursor.set_char_range(Some(egui::text::CCursorRange::one(egui::text::CCursor::new(caret))));
			st.store(&ctx, {{Id}});
		}
	}
	changed = true;
}
if changed {
	self.r9_s_push({{Id}}.value(), text);
}
`))).
			WithReturnType(structTextEdit()).
			Build())
	// datePickerButton wraps egui_extras::DatePickerButton. egui_extras
	// requires &mut NaiveDate at construction; the codegen template puts
	// our construction code on the RHS of `let mut w = ...`, leaving no
	// outer-scope room for `let mut date = ...` ahead of the egui call.
	// So construction emits a plain DatePickerButtonRequest accumulator,
	// builder methods set fields on it, and apply hands it to the
	// hand-written self.apply_date_picker_button which owns the NaiveDate
	// local across self.apply_widget() and pushes a packed YYYYMMDD u64
	// back via r9_u64 on .changed().
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("datePickerButton").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("packedYmd", ctabb.U64).
				Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("format").Arg("format", ctabb.S).CodeClientRust(rustClientCode("{{Instance}}.format = Some(format);\n")).EndMethod().
				BeginMethod("highlightWeekends").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.highlight_weekends = Some(enabled);\n")).EndMethod().
				BeginMethod("showIcon").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.show_icon = Some(enabled);\n")).EndMethod().
				BeginMethod("calendar").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.calendar = Some(enabled);\n")).EndMethod().
				BeginMethod("calendarWeek").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.calendar_week = Some(enabled);\n")).EndMethod().
				BeginMethod("startEndYears").Arg("startYear", ctabb.I16).Arg("endYear", ctabb.I16).CodeClientRust(rustClientCode("{{Instance}}.start_end_years = Some((start_year, end_year));\n")).EndMethod().
				BeginMethod("arrows").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.arrows = Some(enabled);\n")).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("crate::imzero2::date_picker_button::DatePickerButtonRequest::default();\n")).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithApplyCodeClientRust(rustClientCode("self.apply_date_picker_button({{Instance}},{{EguiUiOptionalOuter}},{{FuncProcIdOuter}},{{Id}},packed_ymd);\n")).
			WithReturnType(structDatePickerButton()).
			Build())
	// dateTimePickerButton extends datePickerButton with three integer
	// drag-spinners (h:m:s) in a horizontal row. The whole composite is
	// rendered as a single FFFI2 widget. Wire format is a u64 carrying
	// the bit pattern of an i64 (epoch milliseconds, UTC); see the
	// comment block in rust/src/imzero2/datetime_picker.rs for the
	// rationale (Phase 1 reuses r9_u64 instead of plumbing r9_i64).
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("dateTimePickerButton").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("packedEpochMs", ctabb.U64).
				Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("format").Arg("format", ctabb.S).CodeClientRust(rustClientCode("{{Instance}}.format = Some(format);\n")).EndMethod().
				BeginMethod("highlightWeekends").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.highlight_weekends = Some(enabled);\n")).EndMethod().
				BeginMethod("showIcon").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.show_icon = Some(enabled);\n")).EndMethod().
				BeginMethod("calendar").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.calendar = Some(enabled);\n")).EndMethod().
				BeginMethod("calendarWeek").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.calendar_week = Some(enabled);\n")).EndMethod().
				BeginMethod("startEndYears").Arg("startYear", ctabb.I16).Arg("endYear", ctabb.I16).CodeClientRust(rustClientCode("{{Instance}}.start_end_years = Some((start_year, end_year));\n")).EndMethod().
				BeginMethod("arrows").Arg("enabled", ctabb.B).CodeClientRust(rustClientCode("{{Instance}}.arrows = Some(enabled);\n")).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("crate::imzero2::datetime_picker::DateTimePickerButtonRequest::default();\n")).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithApplyCodeClientRust(rustClientCode("self.apply_date_time_picker_button({{Instance}},{{EguiUiOptionalOuter}},{{FuncProcIdOuter}},{{Id}},packed_epoch_ms);\n")).
			WithReturnType(structDateTimePickerButton()).
			Build())
	// timeRangePicker is the composite widget for ADR-0016:
	// two egui::TextEdit fields (from / to ClickHouse SQL expressions)
	// each followed by a calendar-pop button with h:m:s DragValues,
	// an Apply + Cancel pair, a horizontal preset row populated by Go
	// via the addPreset builder method, and a tz ComboBox + refresh-ms
	// readout. Wire format is r9_s carrying the packed
	// `tz\x1efrom\x1eto` string; pre-evaluation against ClickHouse via
	// the chlocalbroker cap happens Go-side after unpacking via
	// timerangepicker.UnpackRange. The Tz / RefreshInterval builders
	// seed the dropdown's initial selection and the refresh-ms label;
	// the auto-refresh runner is out of scope (the picker exposes the
	// value, a separate runner subscribes). See rust/src/imzero2/
	// time_range_picker.rs for the draft-state egui-memory pattern.
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("timeRangePicker").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().PlainArg("fromInitial", ctabb.S).Build()).
			AddArguments(idl.NewArgumentsBuilder().PlainArg("toInitial", ctabb.S).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("addPreset").Arg("label", ctabb.S).Arg("fromSQL", ctabb.S).Arg("toSQL", ctabb.S).CodeClientRust(rustClientCode("{{Instance}}.presets.push(crate::imzero2::time_range_picker::PresetEntry{label, from_sql, to_sql});\n")).EndMethod().
				BeginMethod("tz").Arg("zone", ctabb.S).CodeClientRust(rustClientCode("{{Instance}}.tz = Some(zone);\n")).EndMethod().
				BeginMethod("refreshInterval").Arg("intervalMs", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}}.refresh_interval_ms = Some(interval_ms);\n")).EndMethod().
				// evaluatedBounds feeds the most recently chlocalbroker-
				// evaluated (from, to) epoch-millisecond bounds back into
				// the picker so the trigger button can render them as
				// human wall-clock time instead of raw SQL expressions.
				// Both args travel together — Go skips the call when
				// no evaluation has happened yet, so absence in the
				// request struct (Option::None on both) is the "render
				// SQL fallback" signal.
				BeginMethod("evaluatedBounds").Arg("fromMs", ctabb.I64).Arg("toMs", ctabb.I64).CodeClientRust(rustClientCode("{{Instance}}.evaluated_from_ms = Some(from_ms);\n{{Instance}}.evaluated_to_ms = Some(to_ms);\n")).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("crate::imzero2::time_range_picker::TimeRangePickerRequest::default();\n")).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithApplyCodeClientRust(rustClientCode("self.apply_time_range_picker({{Instance}},{{EguiUiOptionalOuter}},{{FuncProcIdOuter}},{{Id}},from_initial,to_initial);\n")).
			WithReturnType(structTimeRangePicker()).
			Build())
	for _, w := range widgets {
		if w.ApplyCode.CodeClientRust.UseDefaultCode() {
			w.ApplyCode = applyCodeWidget(w.IdentityArguments.HasId)
		}
		if w.ReturnType == nil {
			w.ReturnType = traitWidget()
		} else {
			found := false
			for t := range w.ReturnType.ImplementedAbstractTypes() {
				found = found || t == traitWidget()
			}
			if !found {
				err := eb.Build().Stringer("widget", w.Name).Errorf("return type does not implement abstract type widget")
				log.Panic().Err(err).Msg("invalid definition")
			}
		}
	}
	return
}
