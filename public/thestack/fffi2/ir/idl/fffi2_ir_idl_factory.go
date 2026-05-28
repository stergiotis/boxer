package idl

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

type BuilderFactoryNodeBuilder struct {
	node ir.BuilderFactoryNode
}

func NewBuilderFactoryNode(name naming.StylableName) *BuilderFactoryNodeBuilder {
	if !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("name is not valid")
	}
	return &BuilderFactoryNodeBuilder{
		node: ir.BuilderFactoryNode{
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
			BuilderMethods: nil,
			Settings: ir.BuilderFactoryFeaturesSpec{
				Immediate:     false,
				Retained:      false,
				BlockIterator: false,
			},
			ConstructionCode: ir.CodeHolder{
				CodeClientRust: ir.DefaultCode,
				CodeServerGo:   ir.DefaultCode,
			},
			ApplyCode: ir.CodeHolder{
				CodeClientRust: ir.DefaultCode,
				CodeServerGo:   ir.DefaultCode,
			},
			ReturnType: nil,
		},
	}
}
func (inst *BuilderFactoryNodeBuilder) WithIdentityId(v bool) *BuilderFactoryNodeBuilder {
	inst.node.IdentityArguments.HasId = v
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithSettingImmediate(v bool) *BuilderFactoryNodeBuilder {
	inst.node.Settings.Immediate = v
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithSettingRetained(v bool) *BuilderFactoryNodeBuilder {
	inst.node.Settings.Retained = v
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithSettingBlockIterator(v bool) *BuilderFactoryNodeBuilder {
	inst.node.Settings.BlockIterator = v
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithReturnType(v ir.TypeI) *BuilderFactoryNodeBuilder {
	inst.node.ReturnType = v
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithConstructionCodeClientRust(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	inst.node.ConstructionCode.CodeClientRust = code
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithConstructionCodeServerGo(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	inst.node.ConstructionCode.CodeServerGo = code
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithApplyCodeClientRust(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	inst.node.ApplyCode.CodeClientRust = code
	return inst
}
func (inst *BuilderFactoryNodeBuilder) WithApplyCodeServerGo(code ir.VerbatimCodeI) *BuilderFactoryNodeBuilder {
	inst.node.ApplyCode.CodeServerGo = code
	return inst
}
func (inst *BuilderFactoryNodeBuilder) AddArguments(spec ir.ArgumentSpec) *BuilderFactoryNodeBuilder {
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
func (inst *BuilderFactoryNodeBuilder) AddMethods(mths ...ir.Method) *BuilderFactoryNodeBuilder {
	for _, n := range inst.node.BuilderMethods {
		for _, m := range mths {
			if naming.Compare(n.Spec.Name, m.Spec.Name) == 0 {
				log.Panic().Stringer("name1", n.Spec.Name).Stringer("name2", m.Spec.Name).Msg("clashing names found")
			}
		}
	}
	inst.node.BuilderMethods = append(inst.node.BuilderMethods, mths...)
	return inst
}
func (inst *BuilderFactoryNodeBuilder) Build() *ir.BuilderFactoryNode {
	return &inst.node
}

// WithDeferredBlockMap declares that this node consumes a deferred block map.
// When .Send() is called on the Go side, all captured deferred blocks are
// spliced into the message after the normal arguments.
//
// Usage in IDL:
//
//	idl.NewBuilderFactoryNode("endETable").
//	    AddArguments(...).
//	    WithDeferredBlockMap("cells", ctabb.U64, ctabb.U32).
//	    WithApplyCodeClientRust(rustClientCode(`
//	        // "cells" is available as HashMap<(u64, u32), Vec<u8>>
//	        for ((row, col), block) in cells.iter() {
//	            // replay inside delegate
//	        }
//	    `)).
//	    Build()
//
// Multiple DeferredBlockMaps can be declared on a single node if needed
// (e.g. one for body cells, one for header cells).
func (inst *BuilderFactoryNodeBuilder) WithDeferredBlockMap(name string, keyTypes ...canonicaltypes.PrimitiveAstNodeI) *BuilderFactoryNodeBuilder {
	if inst.node.DeferredBlockMaps == nil {
		inst.node.DeferredBlockMaps = make([]ir.DeferredBlockMapSpec, 0, 2)
	}
	inst.node.DeferredBlockMaps = append(inst.node.DeferredBlockMaps, ir.DeferredBlockMapSpec{
		Name:     name,
		KeyTypes: keyTypes,
	})
	return inst
}
