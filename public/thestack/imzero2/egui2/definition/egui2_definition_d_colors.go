package definition

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsColor() (widgets []*ir.BuilderFactoryNode) {
	widgets = make([]*ir.BuilderFactoryNode, 0, 32)
	widgets = append(widgets,
		idl.NewBuilderFactoryNode("color").
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("fromRgb").Arg("rv", ctabb.U8).Arg("gv", ctabb.U8).Arg("bv", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::from_rgb(rv,gv,bv);\n")).EndMethod().
				BeginMethod("fromRgbaUnmultiplied").Arg("rv", ctabb.U8).Arg("gv", ctabb.U8).Arg("bv", ctabb.U8).Arg("av", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::from_rgba_unmultiplied(rv,gv,bv,av);\n")).EndMethod().
				BeginMethod("fromRgbaPremultiplied").Arg("rv", ctabb.U8).Arg("gv", ctabb.U8).Arg("bv", ctabb.U8).Arg("av", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::from_rgba_premultiplied(rv,gv,bv,av);\n")).EndMethod().
				BeginMethod("fromGray").Arg("lv", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::from_gray(lv);\n")).EndMethod().
				BeginMethod("fromBlackAlpha").Arg("av", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::from_black_alpha(av);\n")).EndMethod().
				BeginMethod("gammaMultiplyU8").Arg("factor", ctabb.U8).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.gamma_multiply_u8(factor);\n")).EndMethod().
				BeginMethod("gammaMultiplyF32").Arg("factor", ctabb.F32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.gamma_multiply(factor);\n")).EndMethod().
				BeginMethod("linearMultiplyF32").Arg("factor", ctabb.F32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.linear_multiply(factor);\n")).EndMethod().
				BeginMethod("toOpaque").CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.to_opaque();\n")).EndMethod().
				BeginMethod("colorTransparent").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::TRANSPARENT;\n")).EndMethod().
				BeginMethod("colorBlack").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::BLACK;\n")).EndMethod().
				BeginMethod("colorDarkGray").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::DARK_GRAY;\n")).EndMethod().
				BeginMethod("colorGray").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::GRAY;\n")).EndMethod().
				BeginMethod("colorLightGray").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::LIGHT_GRAY;\n")).EndMethod().
				BeginMethod("colorWhite").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::WHITE;\n")).EndMethod().
				BeginMethod("colorBrown").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::BROWN;\n")).EndMethod().
				BeginMethod("colorDarkRed").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::DARK_RED;\n")).EndMethod().
				BeginMethod("colorLightRed").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::LIGHT_RED;\n")).EndMethod().
				BeginMethod("colorCyan").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::CYAN;\n")).EndMethod().
				BeginMethod("colorMagenta").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::MAGENTA;\n")).EndMethod().
				BeginMethod("colorYellow").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::YELLOW;\n")).EndMethod().
				BeginMethod("colorOrange").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::ORANGE;\n")).EndMethod().
				BeginMethod("colorLightYellow").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::LIGHT_YELLOW;\n")).EndMethod().
				BeginMethod("colorKhaki").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::KHAKI;\n")).EndMethod().
				BeginMethod("colorDarkGreen").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::DARK_GREEN;\n")).EndMethod().
				BeginMethod("colorGreen").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::GREEN;\n")).EndMethod().
				BeginMethod("colorLightGreen").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::LIGHT_GREEN;\n")).EndMethod().
				BeginMethod("colorDarkBlue").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::DARK_BLUE;\n")).EndMethod().
				BeginMethod("colorBlue").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::BLUE;\n")).EndMethod().
				BeginMethod("colorLightBlue").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::LIGHT_BLUE;\n")).EndMethod().
				BeginMethod("colorPurple").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::PURPLE;\n")).EndMethod().
				BeginMethod("colorGold").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::GOLD;\n")).EndMethod().
				BeginMethod("colorDebugColor").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::DEBUG_COLOR;\n")).EndMethod().
				BeginMethod("colorPlaceholder").CodeClientRust(rustClientCode("{{Instance}} = egui::Color32::PLACEHOLDER;\n")).EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::Color32::TRANSPARENT;\n")).
			WithApplyCodeClientRust(rustClientCode("{{Color32Register0Transfer}} = {{Instance}};\n")).
			WithSettingRetained(true).
			WithReturnType(structColor32()).
			Build())
	return
}
