package definition

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

type respEventE string

const (
	respEventChanged respEventE = "changed"
	respEventClicked respEventE = "clicked"
)

func applyCodeWidget(hasId bool) ir.CodeHolder {
	if hasId {
		return ir.CodeHolder{
			CodeClientRust: applyCodeWidgetRust(hasId),
			CodeServerGo:   ir.DefaultCode,
		}
	} else {
		return ir.CodeHolder{
			CodeClientRust: applyCodeWidgetRust(hasId),
			CodeServerGo:   ir.DefaultCode,
		}
	}
}
func applyCodeWidgetRust(hasId bool) ir.VerbatimCodeI {
	if hasId {
		return rustClientCode("self.apply_widget({{Instance}},{{EguiUiOptionalOuter}},{{FuncProcIdOuter}},Some({{Id}}));\n")
	} else {
		return rustClientCode("self.apply_widget({{Instance}},{{EguiUiOptionalOuter}},{{FuncProcIdOuter}},None);\n")
	}
}

// applyCodeWrappedWidgetRustOnEvent is applyCodeWidgetRustOnEvent for a widget
// the host adds through a wrapper of its own: wrapper names a Rust tuple
// struct over the built instance that implements egui::Widget. The builder
// methods still run on the stock widget, which is what has them.
func applyCodeWrappedWidgetRustOnEvent(wrapper string, event respEventE, onEventCode ir.VerbatimCodeI) ir.VerbatimCodeI {
	return ir.MergeVerbatimCode(
		rustClientCode("let resp ="), // no trailing whitespace (rustfmt)
		rustClientCode("self.apply_widget("+wrapper+"({{Instance}}),{{EguiUiOptionalOuter}},{{FuncProcIdOuter}},Some({{Id}}));\n"),
		rustClientCode("if resp.is_some() && resp.unwrap()."+string(event)+"() {"),
		onEventCode,
		rustClientCode("}"),
	)
}

func applyCodeWidgetRustOnEvent(hasId bool, event respEventE, onEventCode ir.VerbatimCodeI) ir.VerbatimCodeI {
	return ir.MergeVerbatimCode(
		rustClientCode("let resp ="), // no trailing whitespace (rustfmt)
		applyCodeWidgetRust(hasId),
		rustClientCode("if resp.is_some() && resp.unwrap()."+string(event)+"() {"),
		onEventCode,
		rustClientCode("}"),
	)
}
