package definition

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsRegistered() (registered []*ir.BuilderFactoryNode) {
	registered = make([]*ir.BuilderFactoryNode, 0, 32)
	registered = append(registered, idl.NewBuilderFactoryNode("nodeDir").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("label", structWidgetText()).Build()).
		WithSettingImmediate(true).WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode("egui_ltreeview::NodeBuilder::dir({{Id}}.value()).label(label);\n")).
		WithApplyCodeClientRust(rustClientCode("self.r3_node_cmds.push(NodeCommand::NodeDir({{Instance}}));\n")).
		WithReturnType(structNodeCommand()).Build())
	registered = append(registered, idl.NewBuilderFactoryNode("nodeLeaf").
		WithIdentityId(true).
		WithSettingImmediate(true).WithSettingRetained(true).
		AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("label", structWidgetText()).Build()).
		WithConstructionCodeClientRust(rustClientCode("egui_ltreeview::NodeBuilder::leaf({{Id}}.value()).label(label);\n")).
		WithApplyCodeClientRust(rustClientCode("self.r3_node_cmds.push(NodeCommand::NodeLeaf({{Instance}}));\n")).
		WithReturnType(structNodeCommand()).Build())
	registered = append(registered, idl.NewBuilderFactoryNode("nodeDirClose").
		WithSettingImmediate(true).WithSettingRetained(true).
		AddArguments(idl.NewArgumentsBuilder().PlainArg("childCount", ctabb.U32).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.r3_node_cmds.push(NodeCommand::NodeDirClose(child_count as usize));\n")).
		WithReturnType(structNodeCommand()).Build())
	return
}
