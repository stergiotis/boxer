package definition

import (
	"slices"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	slices2 "github.com/stergiotis/boxer/public/slices"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func Definitions() []ir.NodeI {
	sl := slices.Concat(
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsBlock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsWidget(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsText(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsColor(), nil),
		slices2.CopySliceInterfaceCastable[*ir.ProceduralNode, ir.NodeI](definitionsWidgetProc(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsEvaluated(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsTableRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsEtBlock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsEtRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsNewTableBlock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsNewTableRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsPlotRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsPlotBlock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsGraphRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsGraphBlock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsWalkersRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsWalkersWidgets(), nil),
		definitionsWalkersFetchers(),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsPainterRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsPainterBlock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsScrollingTexture(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsImage(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsCodeView(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsDock(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsSnarlRegistered(), nil),
		slices2.CopySliceInterfaceCastable[*ir.BuilderFactoryNode, ir.NodeI](definitionsSnarlBlock(), nil),
		definitionsSnarlFetchers(),
		definitionsSpecial(),
		definitionsFetcher(),
	)
	slices.SortFunc(sl, func(a ir.NodeI, b ir.NodeI) int {
		return naming.Compare(a.GetName(), b.GetName())
	})
	return sl
}

var r9Types = []string{"u64", "f64", "i64"}
