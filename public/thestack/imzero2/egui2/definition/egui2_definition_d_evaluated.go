package definition

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsEvaluated() (evaluated []*ir.BuilderFactoryNode) {
	evaluated = make([]*ir.BuilderFactoryNode, 0, 16)
	richTextInnerLoop := rustClientCode(`
{
let mut rt = egui::RichText::new(val);
loop {
    let (m2, _) = self.read_from_repr(AtomsBuilderMethodId::from_repr)?;
    match m2 {
        AtomsBuilderMethodId::EndRichText => {
            {{AtomsRegister0Reference}}.push_right(rt);
            break;
        }
        AtomsBuilderMethodId::Size => {
            let sz = self.io.read_plain_f32()?;
            rt = rt.size(sz);
        }
        AtomsBuilderMethodId::ExtraLetterSpacing => {
            let sp = self.io.read_plain_f32()?;
            rt = rt.extra_letter_spacing(sp);
        }
        AtomsBuilderMethodId::LineHeight => {
            let lh = self.io.read_plain_f32()?;
            rt = rt.line_height(Some(lh));
        }
        AtomsBuilderMethodId::LineHeightDefault => {
            rt = rt.line_height(None);
        }
        AtomsBuilderMethodId::Heading => { rt = rt.heading(); }
        AtomsBuilderMethodId::Monospace => { rt = rt.monospace(); }
        AtomsBuilderMethodId::Code => { rt = rt.code(); }
        AtomsBuilderMethodId::Strong => { rt = rt.strong(); }
        AtomsBuilderMethodId::Weak => { rt = rt.weak(); }
        AtomsBuilderMethodId::Underline => { rt = rt.underline(); }
        AtomsBuilderMethodId::Strikethrough => { rt = rt.strikethrough(); }
        AtomsBuilderMethodId::Italics => { rt = rt.italics(); }
        AtomsBuilderMethodId::Small => { rt = rt.small(); }
        AtomsBuilderMethodId::SmallRaised => { rt = rt.small_raised(); }
        AtomsBuilderMethodId::Raised => { rt = rt.raised(); }
        AtomsBuilderMethodId::TextStyleName => {
            // egui's TextStyle::Name(Arc<str>) slot — addresses any
            // custom text style the host's apply path may have written
            // into Style::text_styles (e.g., IDS's "ids-display" /
            // "ids-micro" tiers per ADR-0030 §SD3).
            let name = self.io.read_plain_s()?;
            rt = rt.text_style(egui::TextStyle::Name(name.into()));
        }
        AtomsBuilderMethodId::Text => {
            // Self-heal: a plain-text atom arriving inside an unclosed rich
            // segment means the caller skipped endRichText (the classic
            // RichTextColored(...).Text(...) desync footgun). Close the
            // segment implicitly so its content is not lost, then push the
            // text as its own atom. Byte-safe: we consume Text's string arg
            // here, so the outer loop stays aligned and the frame does not
            // crash. The Go-side sub-protocol methods are unexported, so this
            // is unreachable from normal code — it guards hand-written raw
            // streams and future regressions.
            {{AtomsRegister0Reference}}.push_right(rt);
            let v = self.io.read_plain_s()?;
            {{AtomsRegister0Reference}}.push_right(v);
            break;
        }
        _ => {
            // Any other out-of-scope method: preserve the accumulated segment
            // and stop. Not guaranteed byte-aligned if the method carried args,
            // but this path is unreachable from the (unexported) safe Go API.
            {{AtomsRegister0Reference}}.push_right(rt);
            tracing::warn!("unexpected method {:?} inside richText sub-loop — closing segment", m2);
            break;
        }
    }
}
}
`)
	richTextColoredInnerLoop := rustClientCode(`
{
let mut rt = egui::RichText::new(val).color(cl).background_color(bk);
loop {
    let (m2, _) = self.read_from_repr(AtomsBuilderMethodId::from_repr)?;
    match m2 {
        AtomsBuilderMethodId::EndRichText => {
            {{AtomsRegister0Reference}}.push_right(rt);
            break;
        }
        AtomsBuilderMethodId::Size => {
            let sz = self.io.read_plain_f32()?;
            rt = rt.size(sz);
        }
        AtomsBuilderMethodId::ExtraLetterSpacing => {
            let sp = self.io.read_plain_f32()?;
            rt = rt.extra_letter_spacing(sp);
        }
        AtomsBuilderMethodId::LineHeight => {
            let lh = self.io.read_plain_f32()?;
            rt = rt.line_height(Some(lh));
        }
        AtomsBuilderMethodId::LineHeightDefault => {
            rt = rt.line_height(None);
        }
        AtomsBuilderMethodId::Heading => { rt = rt.heading(); }
        AtomsBuilderMethodId::Monospace => { rt = rt.monospace(); }
        AtomsBuilderMethodId::Code => { rt = rt.code(); }
        AtomsBuilderMethodId::Strong => { rt = rt.strong(); }
        AtomsBuilderMethodId::Weak => { rt = rt.weak(); }
        AtomsBuilderMethodId::Underline => { rt = rt.underline(); }
        AtomsBuilderMethodId::Strikethrough => { rt = rt.strikethrough(); }
        AtomsBuilderMethodId::Italics => { rt = rt.italics(); }
        AtomsBuilderMethodId::Small => { rt = rt.small(); }
        AtomsBuilderMethodId::SmallRaised => { rt = rt.small_raised(); }
        AtomsBuilderMethodId::Raised => { rt = rt.raised(); }
        AtomsBuilderMethodId::TextStyleName => {
            // egui's TextStyle::Name(Arc<str>) slot — addresses any
            // custom text style the host's apply path may have written
            // into Style::text_styles (e.g., IDS's "ids-display" /
            // "ids-micro" tiers per ADR-0030 §SD3).
            let name = self.io.read_plain_s()?;
            rt = rt.text_style(egui::TextStyle::Name(name.into()));
        }
        AtomsBuilderMethodId::Text => {
            // Self-heal: see the richText sub-loop above. Close the colored
            // segment implicitly (don't lose it), consume Text's string arg
            // to stay byte-aligned, and push it as its own atom instead of
            // crashing the frame.
            {{AtomsRegister0Reference}}.push_right(rt);
            let v = self.io.read_plain_s()?;
            {{AtomsRegister0Reference}}.push_right(v);
            break;
        }
        _ => {
            // Any other out-of-scope method: preserve the accumulated segment
            // and stop. Not guaranteed byte-aligned if the method carried args,
            // but this path is unreachable from the (unexported) safe Go API.
            {{AtomsRegister0Reference}}.push_right(rt);
            tracing::warn!("unexpected method {:?} inside richTextColored sub-loop — closing segment", m2);
            break;
        }
    }
}
}
`)
	richTextStyleWarn := rustClientCode(`tracing::warn!("rich text style method called outside richText/endRichText scope, ignoring");
`)
	// For methods with args: the code generator reads the arg before our custom code,
	// so we only need to emit the warning (arg is already consumed into a local variable).
	richTextStyleWarnWithArg := richTextStyleWarn

	evaluated = append(evaluated, idl.NewBuilderFactoryNode("atoms").
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("text").Arg("val", ctabb.S).CodeClientRust(rustClientCode("{{AtomsRegister0Reference}}.push_right(val);\n")).EndMethod().
			// RichText sub-protocol: richText(text) starts inner loop, endRichText terminates it.
			// Every method below is .Unexported() so it emits a lower-camel Go
			// name callable only from inside the bindings package. Public code
			// must go through the hand-written RichTextScope (BeginRichText /
			// StyledText / DisplayRichText), which balances the richText…endRichText
			// pair for you. That makes an unbalanced chain — the classic
			// `Atoms().RichTextColored(...).Text(...)` FFI-desync footgun — a
			// compile error instead of a runtime crash. The opcode enums and wire
			// format are unchanged; only the Go method visibility differs.
			BeginMethod("richText").Unexported().Arg("val", ctabb.S).CodeClientRust(richTextInnerLoop).EndMethod().
			BeginMethod("richTextColored").Unexported().EvaluatedArg("cl", structColor32()).AsColor().EvaluatedArg("bk", structColor32()).AsColor().Arg("val", ctabb.S).CodeClientRust(richTextColoredInnerLoop).EndMethod().
			BeginMethod("endRichText").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			// Style methods — only meaningful inside richText/endRichText; warn if used in outer loop
			BeginMethod("size").Unexported().Arg("sz", ctabb.F32).CodeClientRust(richTextStyleWarnWithArg).EndMethod().
			BeginMethod("extraLetterSpacing").Unexported().Arg("sp", ctabb.F32).CodeClientRust(richTextStyleWarnWithArg).EndMethod().
			BeginMethod("lineHeight").Unexported().Arg("lh", ctabb.F32).CodeClientRust(richTextStyleWarnWithArg).EndMethod().
			BeginMethod("lineHeightDefault").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("heading").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("monospace").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("code").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("strong").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("weak").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("underline").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("strikethrough").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("italics").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("small").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("smallRaised").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			BeginMethod("raised").Unexported().CodeClientRust(richTextStyleWarn).EndMethod().
			// Selects a custom TextStyle::Name slot — most commonly the
			// IDS "ids-display" / "ids-micro" tiers bound by the Rust
			// apply path (ADR-0030 §SD3). Built-in tiers (Heading/Body/
			// Small/Monospace/Button) stay on their dedicated methods.
			BeginMethod("textStyleName").Unexported().Arg("name", ctabb.S).CodeClientRust(richTextStyleWarnWithArg).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithSettingRetained(true).
		WithReturnType(structAtoms()).
		Build())
	evaluated = append(evaluated, idl.NewBuilderFactoryNode("widgetText").
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("text").Arg("val", ctabb.S).CodeClientRust(rustClientCode("{{WidgetTextRegister0Reference}} = egui::WidgetText::Text(val);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithSettingRetained(true).
		WithReturnType(structWidgetText()).
		Build())
	evaluated = append(evaluated, idl.NewBuilderFactoryNode("scalarSize").
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("availableWidth").CodeClientRust(rustClientCode("{{Instance}} = if {{EguiUiOptionalOuter}}.is_some() { {{EguiUiOptionalOuter}}.as_mut().unwrap().available_width() } else { 0.0 };\n")).EndMethod().
			BeginMethod("availableHeight").CodeClientRust(rustClientCode("{{Instance}} = if {{EguiUiOptionalOuter}}.is_some() { {{EguiUiOptionalOuter}}.as_mut().unwrap().available_height() } else { 0.0 };\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode("0.0f32;")).
		WithSettingRetained(true).
		WithReturnType(typeDefScalarSize()).
		Build())
	evaluated = append(evaluated, idl.NewBuilderFactoryNode("vectorSize").
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("availableSize").CodeClientRust(rustClientCode("{{Instance}} = if {{EguiUiOptionalOuter}}.is_some() { {{EguiUiOptionalOuter}}.as_mut().unwrap().available_size() } else { egui::emath::Vec2::new(0.0f32,0.0f32) };\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode("egui::emath::Vec2::new(0.0f32,0.0f32);\n")).
		WithSettingRetained(true).
		WithReturnType(typeDefScalarSize()).
		Build())
	/*evaluated = append(evaluated, idl.NewBuilderFactoryNode("layout").
	AddMethods(idl.NewMethodBuilder().
		BeginMethod("availableSize").CodeClientRust(rustClientCode("{{Instance}} = if {{EguiUiOptionalOuter}}.is_some() { {{EguiUiOptionalOuter}}.as_mut().unwrap().available_size() } else { egui::emath::Vec2::new(0.0f32,0.0f32) };\n")).EndMethod().
		Build()...).
	WithConstructionCodeClientRust(rustClientCode("egui::emath::Vec2::new(0.0f32,0.0f32);\n")).
	WithSettingRetained(true).
	WithReturnType(typeDefScalarSize()).
	Build())*/
	return
}
