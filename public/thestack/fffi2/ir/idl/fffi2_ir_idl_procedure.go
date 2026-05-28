package idl

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

type ProceduralNodeBuilder struct {
	node ir.ProceduralNode
}

func NewProceduralNode(name naming.StylableName) *ProceduralNodeBuilder {
	if !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("name is not valid")
	}
	return &ProceduralNodeBuilder{
		node: ir.ProceduralNode{
			Name: name,
			IdentityArguments: ir.IdentityArgumentSpec{
				HasId: false,
			},
			Arguments: ir.ArgumentSpec{
				EvaluatedArguments: ir.EvaluatedArgumentSpec{
					Names:         nil,
					AcceptedTypes: nil,
				},
				PlainArguments: ir.PlainArgumentSpec{
					Names: nil,
					Types: nil,
				},
			},
			Settings: ir.ProcedureFeaturesSpec{
				BlockIterator: false,
			},
			ApplyCode: ir.CodeHolder{
				CodeClientRust: ir.DefaultCode,
				CodeServerGo:   ir.DefaultCode,
			},
			ReturnType: nil,
		},
	}
}
func (inst *ProceduralNodeBuilder) WithIdentityId(v bool) *ProceduralNodeBuilder {
	inst.node.IdentityArguments.HasId = v
	return inst
}

// WithIdentityIdReference marks the identity id as naming an existing widget
// rather than creating one. The generated call site takes
// widgethandle.WidgetHandle (opaque to callers) and writes its resolved id
// directly — no WidgetIdCreatorI Derive/Prepare dance, no duplicate-id guard.
// Appropriate for operations like SetWindowCollapsed or MoveWindowToTop that
// act on a widget already created elsewhere in the frame.
func (inst *ProceduralNodeBuilder) WithIdentityIdReference() *ProceduralNodeBuilder {
	inst.node.IdentityArguments.HasId = true
	inst.node.IdentityArguments.IsReference = true
	return inst
}
func (inst *ProceduralNodeBuilder) WithSettingBlockIterator(v bool) *ProceduralNodeBuilder {
	inst.node.Settings.BlockIterator = v
	return inst
}
func (inst *ProceduralNodeBuilder) WithReturnType(v ir.TypeI) *ProceduralNodeBuilder {
	inst.node.ReturnType = v
	return inst
}
func (inst *ProceduralNodeBuilder) WithApplyCodeClientRust(code ir.VerbatimCodeI) *ProceduralNodeBuilder {
	inst.node.ApplyCode.CodeClientRust = code
	return inst
}
func (inst *ProceduralNodeBuilder) WithApplyCodeServerGo(code ir.VerbatimCodeI) *ProceduralNodeBuilder {
	inst.node.ApplyCode.CodeServerGo = code
	return inst
}
func (inst *ProceduralNodeBuilder) AddArguments(spec ir.ArgumentSpec) *ProceduralNodeBuilder {
	checkNameClashesPedantic(inst.node.Arguments.PlainArguments.Names, spec.PlainArguments.Names)
	inst.node.Arguments.PlainArguments.Names = append(inst.node.Arguments.PlainArguments.Names, spec.PlainArguments.Names...)
	inst.node.Arguments.PlainArguments.Types = append(inst.node.Arguments.PlainArguments.Types, spec.PlainArguments.Types...)
	inst.node.Arguments.PlainArguments.ColorArgKinds = append(inst.node.Arguments.PlainArguments.ColorArgKinds, spec.PlainArguments.ColorArgKinds...)
	checkNameClashesPedantic(inst.node.Arguments.EvaluatedArguments.Names, spec.EvaluatedArguments.Names)
	inst.node.Arguments.EvaluatedArguments.Names = append(inst.node.Arguments.EvaluatedArguments.Names, spec.EvaluatedArguments.Names...)
	inst.node.Arguments.EvaluatedArguments.AcceptedTypes = append(inst.node.Arguments.EvaluatedArguments.AcceptedTypes, spec.EvaluatedArguments.AcceptedTypes...)
	inst.node.Arguments.EvaluatedArguments.ColorArgKinds = append(inst.node.Arguments.EvaluatedArguments.ColorArgKinds, spec.EvaluatedArguments.ColorArgKinds...)
	return inst
}
func (inst *ProceduralNodeBuilder) Build() *ir.ProceduralNode {
	return &inst.node
}
