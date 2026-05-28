package idl

import (
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

type FetcherNodeBuilder struct {
	node ir.FetcherNode
}

func NewFetcherNode(name naming.StylableName) *FetcherNodeBuilder {
	if !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("name is not valid")
	}
	return &FetcherNodeBuilder{
		node: ir.FetcherNode{
			Name: name,
			ApplyCode: ir.CodeHolder{
				CodeClientRust: nil,
				CodeServerGo:   nil,
			},
			ReturnTypes: ir.PlainArgumentSpec{
				Names: nil,
				Types: nil,
			},
		},
	}
}
func (inst *FetcherNodeBuilder) WithApplyCodeClientRust(code ir.VerbatimCodeI) *FetcherNodeBuilder {
	inst.node.ApplyCode.CodeClientRust = code
	return inst
}
func (inst *FetcherNodeBuilder) WithApplyCodeServerGo(code ir.VerbatimCodeI) *FetcherNodeBuilder {
	inst.node.ApplyCode.CodeServerGo = code
	return inst
}
func (inst *FetcherNodeBuilder) AddReturnValue(name naming.StylableName, typ canonicaltypes.PrimitiveAstNodeI) *FetcherNodeBuilder {
	if !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("invalid name")
	}
	if !typ.IsValid() {
		log.Panic().Msg("invalid type")
	}
	if slices.ContainsFunc(inst.node.ReturnTypes.Names, func(name2 naming.StylableName) bool {
		return naming.Compare(name, name2) == 0
	}) {
		log.Panic().Stringer("name", name).Msg("clashing return type name")
	}
	inst.node.ReturnTypes.Names = append(inst.node.ReturnTypes.Names, name)
	inst.node.ReturnTypes.Types = append(inst.node.ReturnTypes.Types, typ)
	return inst
}
func (inst *FetcherNodeBuilder) Build() *ir.FetcherNode {
	return &inst.node
}
