package definition

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func traitBlock() ir.AbstractType {
	return ir.NewAbstractType("block")
}
func traitWidget() ir.AbstractType {
	return ir.NewAbstractType("widget")
}
func structAtoms() ir.ConcreteType {
	return ir.NewConcreteType("atoms")
}
func structWidgetText() ir.ConcreteType {
	return ir.NewConcreteType("widgetText")
}
func typeDefScalarSize() ir.ConcreteType {
	return ir.NewConcreteType("scalarSize")
}
func structLabel() ir.ConcreteType {
	return ir.NewConcreteType("label", traitWidget())
}
func structButton() ir.ConcreteType {
	return ir.NewConcreteType("button", traitWidget())
}
func structDragValue() ir.ConcreteType {
	return ir.NewConcreteType("dragValue", traitWidget())
}
func structSlider() ir.ConcreteType {
	return ir.NewConcreteType("slider", traitWidget())
}
func structSpinner() ir.ConcreteType {
	return ir.NewConcreteType("spinner", traitWidget())
}
func structCheckBox() ir.ConcreteType {
	return ir.NewConcreteType("checkbox", traitWidget())
}
func structTextEdit() ir.ConcreteType {
	return ir.NewConcreteType("textEdit", traitWidget())
}
func structDatePickerButton() ir.ConcreteType {
	return ir.NewConcreteType("datePickerButton", traitWidget())
}
func structDateTimePickerButton() ir.ConcreteType {
	return ir.NewConcreteType("dateTimePickerButton", traitWidget())
}
func structTimeRangePicker() ir.ConcreteType {
	return ir.NewConcreteType("timeRangePicker", traitWidget())
}
func structPassthrough() ir.ConcreteType {
	return ir.NewConcreteType("passthrough")
}
func structNodeCommand() ir.ConcreteType {
	return ir.NewConcreteType("nodeCommand")
}
func structColor32() ir.ConcreteType {
	return ir.NewConcreteType("color32")
}
func structRichText() ir.ConcreteType {
	return ir.NewConcreteType("richText")
}
func structTableColumn() ir.ConcreteType {
	return ir.NewConcreteType("tableColumn")
}
func structTableHeaderText() ir.ConcreteType {
	return ir.NewConcreteType("tableHeaderText")
}
func structTableCell() ir.ConcreteType {
	return ir.NewConcreteType("tableCell")
}
func structEtColumn() ir.ConcreteType {
	return ir.NewConcreteType("etColumn")
}
func structEtHeaderText() ir.ConcreteType {
	return ir.NewConcreteType("etHeaderText")
}
func structEtDummy() ir.ConcreteType {
	return ir.NewConcreteType("etDummy")
}
func structNewTableColumn() ir.ConcreteType {
	return ir.NewConcreteType("newTableColumn")
}
func structNewTableHeight() ir.ConcreteType {
	return ir.NewConcreteType("newTableHeight")
}
func structNewTableDummy() ir.ConcreteType {
	return ir.NewConcreteType("newTableDummy")
}
func structCodeViewJob() ir.ConcreteType {
	return ir.NewConcreteType("codeViewJob")
}
func structCodeView() ir.ConcreteType {
	return ir.NewConcreteType("codeView", traitWidget())
}
func structHyperlink() ir.ConcreteType {
	return ir.NewConcreteType("hyperlink", traitWidget())
}
func structSelectableLabel() ir.ConcreteType {
	return ir.NewConcreteType("selectableLabel", traitWidget())
}
func structProgressBar() ir.ConcreteType {
	return ir.NewConcreteType("progressBar", traitWidget())
}
func structHoverUiDummy() ir.ConcreteType {
	return ir.NewConcreteType("hoverUiDummy")
}
func structDockAreaDummy() ir.ConcreteType {
	return ir.NewConcreteType("dockAreaDummy")
}
func structGraphNode() ir.ConcreteType {
	return ir.NewConcreteType("graphNode")
}
func structGraphEdge() ir.ConcreteType {
	return ir.NewConcreteType("graphEdge")
}
func structGraphDrain() ir.ConcreteType {
	return ir.NewConcreteType("graphDrain")
}

func structWalkersMap() ir.ConcreteType {
	return ir.NewConcreteType("walkersMap", traitWidget())
}
func structMapMarker() ir.ConcreteType {
	return ir.NewConcreteType("mapMarker")
}
func structMapPolyline() ir.ConcreteType {
	return ir.NewConcreteType("mapPolyline")
}
func structH3Region() ir.ConcreteType {
	return ir.NewConcreteType("h3Region")
}
func structH3CellsColored() ir.ConcreteType {
	return ir.NewConcreteType("h3CellsColored")
}
func structMapRaster() ir.ConcreteType {
	return ir.NewConcreteType("mapRaster")
}

func structImage() ir.ConcreteType {
	return ir.NewConcreteType("image", traitWidget())
}

func structSnarlNode() ir.ConcreteType {
	return ir.NewConcreteType("snarlNode")
}
func structSnarlConnection() ir.ConcreteType {
	return ir.NewConcreteType("snarlConnection")
}
func structSnarlPin() ir.ConcreteType {
	return ir.NewConcreteType("snarlPin")
}
func structSnarlEditor() ir.ConcreteType {
	return ir.NewConcreteType("snarlEditor")
}
