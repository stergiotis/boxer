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
	// uiSetItemSpacing — override `spacing.item_spacing` for the CURRENT Ui.
	//
	// Scope is the Ui the op lands in: egui's Style is cloned into each
	// child Ui when it is created, so children opened AFTER this op inherit
	// the override and siblings of the enclosing Ui are untouched. Emit it
	// inside the block whose gaps you want to change; there is no restore
	// op, and none is needed — the enclosing Ui keeps its own Style.
	//
	// The x axis is the one with no other lever. A row of inline runs
	// (markdown's links and images inside HorizontalWrapped) gets
	// item_spacing.x inserted at every run boundary IN ADDITION to whatever
	// space characters the text already carries, so links float in a
	// double-wide gap and punctuation after a link detaches ("a link .").
	// Trimming the adjacent spaces cannot fix the second case — there is no
	// space to trim there — so the gap has to go to zero and the text's own
	// spaces have to carry the word gap.
	//
	// Generally useful beyond that: control rows and badge clusters that
	// want to sit tighter than the global density without every widget
	// growing a knob. Carries no widget id.
	widgets = append(widgets, idl.NewProceduralNode("uiSetItemSpacing").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("sx", ctabb.F32).
			PlainArg("sy", ctabb.F32).
			Build()).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						ui.spacing_mut().item_spacing = egui::vec2(sx, sy);
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
	// uiSetMinWidthAvailable — set_min_width(available_width()). The Go side
	// cannot compute this: available width is a client-side layout fact, and
	// the whole point of the interpreted-size mechanism is that Go ships an
	// instruction instead of a number.
	//
	// Added for ADR-0176 SD5. A deferred row block gets a Ui spanning the
	// whole row, but every widget in it sizes to its own content, so a row
	// background or click sense drawn from Go covered only its own text.
	// Nothing consumes the existing ScalarSize().AvailableWidth() holder, so
	// there was no way to say "as wide as the row" at all.
	widgets = append(widgets, idl.NewProceduralNode("uiSetMinWidthAvailable").
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let aw = ui.available_width();
						ui.set_min_width(aw);
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
	// uiClipToMaxRect — constrain painting in the current Ui to its max_rect.
	// egui sizes Frames to their content with only minimums and does not clip
	// a Ui allocated at a fixed rect (allocateUiAtRect's max_rect is
	// advisory), so content taller/wider than the rect paints over whatever
	// lies beyond it. Emitting this as the first op inside the allocated Ui
	// makes the rect a hard paint boundary. shrink_clip_rect intersects with
	// the inherited clip, so an enclosing scroll area / window edge keeps
	// clipping as before; this can only tighten, never widen. Layout is
	// unaffected — min_rect growth (and the parent-size ratchet it feeds)
	// is an allocation property, not a paint property.
	widgets = append(widgets, idl.NewProceduralNode("uiClipToMaxRect").
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let r = ui.max_rect();
						ui.shrink_clip_rect(r);
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
				// Mirrors label.selectable, and exists for the same reason it
				// matters there: egui defaults selectable_labels to true, and a
				// selectable Label senses click_and_drag. Inside a row that is
				// itself click-sensed (ADR-0176 SD7) the label is registered
				// later, so it sits ABOVE the row sense and swallows the click.
				// Selectable(false) hands the click back to the row.
				BeginMethod("selectable").Arg("val", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.selectable(val);\n")).EndMethod().
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
	let style = c.style_of(c.theme());
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
			// egui 0.35 removed the `SelectableLabel` widget; `Button::selectable`
			// is its replacement (frameless selectable button, same visual).
			WithConstructionCodeClientRust(rustClientCode("egui::Button::selectable(checked, text);\n")).
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
				// highlightJob (ADR-0130): stage an evaluated CodeViewJob whose
				// sections the apply code below applies ADVISORILY via a
				// TextEdit layouter — reconciled against the live buffer and
				// gap-filled Rust-side (text_edit_highlight.rs), so a one-frame
				// -stale or malformed job degrades to plain text.
				BeginMethod("highlightJob").
				EvaluatedArg("job", structCodeViewJob()).
				CodeClientRust(rustClientCode("self.text_edit_pending_highlight = Some(job);\n")).
				EndMethod().
				// sectionStyled (ADR-0130 L3): the parallel overlay channel —
				// sparse underline / background / strikethrough / italics spans
				// riding the same reconcile as the color sections. Sparse by
				// contract: uncovered bytes simply have no styling, so unlike
				// the color tier there is no gap-fill.
				BeginMethod("sectionStyled").
				EvaluatedArg("styled", structStyledSections()).
				CodeClientRust(rustClientCode("self.text_edit_pending_styled = Some(styled);\n")).
				EndMethod().
				// noWrapLayout (ADR-0130 L3): lay the buffer out unwrapped, so
				// galley rows equal logical lines — the alignment contract a
				// line-number gutter beside the editor depends on. The caller
				// owns the horizontal scrolling that the wider galley needs.
				BeginMethod("noWrapLayout").
				CodeClientRust(rustClientCode("self.text_edit_pending_no_wrap = true;\n")).
				EndMethod().
				// reportCursor (ADR-0130 L3): opt in to the caret channel. The
				// apply block below pushes the sorted cursor char range packed
				// into one r9_u64 (low half start, high half end), on EVERY
				// frame the method is present — change detection around
				// end-of-frame value application never fires, and one u64 per
				// frame is noise next to the buffer itself.
				BeginMethod("reportCursor").
				CodeClientRust(rustClientCode("self.text_edit_pending_report_cursor = true;\n")).
				EndMethod().
				// setCursor: the inbound half of the caret channel, taking the
				// same packed u64 reportCursor emits (low half start, high
				// half end, CHAR offsets) so a range read out of an editor can
				// be written straight back into it. A collapsed caret is
				// start == end.
				//
				// One-shot, like insertAtCursor and unlike reportCursor: it
				// applies only on the frames Go sends it. Sent every frame it
				// would pin the caret against the person typing.
				//
				// `focus` is separate on purpose. An unfocused TextEdit paints
				// no caret, so a caller that wants the new position SEEN must
				// ask for focus — but taking it unconditionally would break
				// the obvious consumer: a find field that moves the caret per
				// keystroke would pull focus out of itself and the user could
				// not type a second character. So the caller decides, and
				// find-as-you-type passes false until the user commits.
				// The argument is `sel` and not `range`: the generator emits the
				// IDL name verbatim into the Go signature, and `range` is a Go
				// keyword — it produces a file that does not parse, which the
				// generator reports as a formatting warning rather than an
				// error.
				BeginMethod("setCursor").Arg("sel", ctabb.U64).Arg("focus", ctabb.B).
				CodeClientRust(rustClientCode("self.text_edit_pending_set_cursor = Some((sel, focus));\n")).
				EndMethod().
				// captureTab (ADR-0147 §SD10, built for ADR-0190 §SD5): take
				// Tab away from the editor for one frame and report it through
				// ADR-0177's key-capture channel.
				//
				// One key, opt-in per frame. A completion pane emits it only
				// while it has something to complete, so Tab inserts a tab
				// character every other frame — which is what a code editor's
				// Tab means and what someone who is not completing expects.
				//
				// The consume happens BEFORE the widget runs (see the apply
				// block), which is the whole reason this is a builder method
				// and not a fetcher: a fetcher drains at frame end, long after
				// the TextEdit turned the key into a tab character.
				BeginMethod("captureTab").
				CodeClientRust(rustClientCode("self.text_edit_pending_capture_tab = true;\n")).
				EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("if multiline { egui::TextEdit::multiline(&mut text).id({{Id}}) } else { egui::TextEdit::singleline(&mut text).id({{Id}}) };\n")).
			WithSettingImmediate(true).
			// Apply: keep the user-edit changed-push, and fold in the
			// programmatic insert-at-cursor (TextEditFluid.InsertAtCursor).
			// text is moved into r9_s_push, so push exactly once at the end
			// gated on a single `changed` (user-edited OR snippet-inserted) —
			// pushing twice would move text twice. See ADR-0063.
			WithApplyCodeClientRust(ir.MergeVerbatimCode(
				rustClientCode(`// ADR-0130: builder methods stashed an evaluated CodeViewJob on
// self.text_edit_pending_highlight, the L3 overlay list on
// self.text_edit_pending_styled, and the no-wrap switch on
// self.text_edit_pending_no_wrap. Build the layouter closure as a stack
// local — closure and widget are same-scope locals, and the widget is
// consumed by apply below, so the &mut FnMut borrow stays sound.
//
// The styled list and the no-wrap switch are taken unconditionally so
// neither leaks into the next TextEdit; they only reach a layouter when a
// highlight job is present, which is the only thing that installs one.
let styled_sections = self.text_edit_pending_styled.take().unwrap_or_default();
let no_wrap_layout = std::mem::take(&mut self.text_edit_pending_no_wrap);
let mut hl_layouter = self.text_edit_pending_highlight.take().map(|job| {
    crate::imzero2::text_edit_highlight::make_layouter(job, styled_sections, no_wrap_layout)
});
if let Some(cl) = hl_layouter.as_mut() {
    {{Instance}} = {{Instance}}.layouter(cl);
}
// captureTab (ADR-0147 §SD10): remove one Tab press from the queue before
// the widget sees it, and report it on ADR-0177's key-capture register.
//
// BEFORE the widget, not after: with code_editor()/lock_focus(true) the
// TextEdit turns Tab into a tab character while it runs, so anything reading
// the queue afterwards sees a key that has already been spent. That ordering
// is why this is a builder method rather than a fetcher.
//
// Gated on focus, so a Tab pressed while the caret is elsewhere still moves
// focus normally. The id is the TextEdit's own — construction gives it
// {{Id}} — so no focus salt is involved, unlike a Frame's capture.
//
// Modifiers ride along rather than narrowing the match (ADR-0177 §SD5), so
// Shift+Tab arrives as Tab with shift set and the Go side decides.
if std::mem::take(&mut self.text_edit_pending_capture_tab) {
    if let Some(ctx) = {{EguiUiOptionalOuter}}.as_deref().map(|ui| ui.ctx().clone()) {
        if ctx.memory(|m| m.has_focus({{Id}})) {
            let mods_now = ctx.input(|inp| inp.modifiers);
            let mods_byte = (mods_now.shift as u8)
                | ((mods_now.ctrl as u8) << 1)
                | ((mods_now.alt as u8) << 2)
                | ((mods_now.command as u8) << 3);
            let mut hit = false;
            ctx.input_mut(|inp| {
                let before = inp.events.len();
                inp.events.retain(|ev| !matches!(ev,
                    egui::Event::Key { key: egui::Key::Tab, pressed: true, .. }));
                hit = inp.events.len() != before;
            });
            if hit {
                self.r26_key_capture_push({{Id}}.value(), crate::imzero2::keycodes::imzero_key_code(egui::Key::Tab), mods_byte);
                // R26 is read at the END of this frame, so Go acts on the
                // capture while building the NEXT one — and the keypress that
                // would have asked for that frame has just been eaten here.
                ctx.request_repaint();
            }
        }
    }
}
let resp =`),
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
		// egui 0.35 returns a Range<CharIndex>; splice_text_at_cursor works in
		// plain usize, so unwrap the newtype at this boundary.
		.map(|cr| { let r = cr.as_sorted_char_range(); r.start.0..r.end.0 })
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
// setCursor: write the caret / selection the caller asked for.
//
// Placed HERE deliberately, between the two blocks it sits among. After the
// insert, so an explicit position wins over the caret the splice would have
// left behind when a frame carries both. Before the report, so this frame's
// report already reflects it and Go is never told a caret it just replaced.
//
// Both halves are clamped to the buffer and then sorted, so a stale range
// from a longer buffer lands at the end rather than panicking the char
// splice — this arrives from Go one frame after the text it described.
//
// What this does NOT do is scroll the caret into view. egui only scrolls when
// the widget has focus AND either the response changed or its selection
// changed (text_edit/builder.rs). That selection-changed test compares two
// reads of the same stored state within one frame, so a range stored from
// outside reads back equal and is invisible to it. Revealing an off-screen
// caret needs the galley, which lives on the other side of this seam.
if let Some((packed, want_focus)) = self.text_edit_pending_set_cursor.take() {
	if let Some(ctx) = {{EguiUiOptionalOuter}}.as_deref().map(|ui| ui.ctx().clone()) {
		let end = text.chars().count();
		let a = ((packed & 0xffff_ffff) as usize).min(end);
		let b = ((packed >> 32) as usize).min(end);
		let (lo, hi) = if a <= b { (a, b) } else { (b, a) };
		let mut st = egui::text_edit::TextEditState::load(&ctx, {{Id}}).unwrap_or_default();
		st.cursor.set_char_range(Some(egui::text::CCursorRange::two(
			egui::text::CCursor::new(lo),
			egui::text::CCursor::new(hi),
		)));
		st.store(&ctx, {{Id}});
		if want_focus {
			ctx.memory_mut(|m| m.request_focus({{Id}}));
		}
	}
}
// ADR-0130 L3 caret report: push the persisted cursor's sorted CHAR range
// packed low=start / high=end. Runs BEFORE the text push below, which moves
// the buffer out. Unconditional while the method is present — a caret move
// with no text change must still be reported, and change detection around
// end-of-frame value application never fires. No stored state (the editor
// was never focused) reports (end, end), matching the insert path's
// convention above; Go clamps against its own copy of the buffer.
if std::mem::take(&mut self.text_edit_pending_report_cursor) {
	const CARET_HALF_MAX: u64 = 0xffff_ffff;
	let end = text.chars().count() as u64;
	let (cs, ce) = {{EguiUiOptionalOuter}}
		.as_deref()
		.map(|ui| ui.ctx().clone())
		.and_then(|ctx| egui::text_edit::TextEditState::load(&ctx, {{Id}}))
		.and_then(|st| st.cursor.char_range())
		.map(|cr| { let r = cr.as_sorted_char_range(); (r.start.0 as u64, r.end.0 as u64) })
		.unwrap_or((end, end));
	self.r9_u64_push({{Id}}.value(), cs.min(CARET_HALF_MAX) | (ce.min(CARET_HALF_MAX) << 32));
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
