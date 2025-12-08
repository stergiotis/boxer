package egui

import "github.com/stergiotis/boxer/public/fffi/runtime"

type FuncProcId = runtime.FuncProcId

const (
	FuncProcIdGetCompatibilityRecord FuncProcId = 0

	FuncProcIdWidgetLabel     FuncProcId = 1
	FuncProcIdWidgetButton    FuncProcId = 2
	FuncProcIdWidgetTree      FuncProcId = 3
	FuncProcIdWidgetSeparator FuncProcId = 4

	FuncProcIdEnd             FuncProcId = 30
	FuncProcIdBeginHorizontal FuncProcId = 31

	ManipSense FuncProcId = 40

	FuncProcIdR0AtomPush         FuncProcId = 50
	FuncProcIdR1Get              FuncProcId = 51
	FuncProcIdR2FromR1Masked     FuncProcId = 52
	FuncProcIdR2Get              FuncProcId = 53
	FuncProcIdR3NodeDirPush      FuncProcId = 54
	FuncProcIdR3NodeLeafPush     FuncProcId = 55
	FuncProcIdR3NodeDirClosePush FuncProcId = 56
)

type ButtonBuilderMethodId = runtime.FuncProcId

const (
	ButtonBuilderMethodIdBuild ButtonBuilderMethodId = 0
	ButtonBuilderMethodFrame   ButtonBuilderMethodId = 1
)

type LabelBuilderMethodId = runtime.FuncProcId

const (
	LabelBuilderMethodIdBuild      LabelBuilderMethodId = 0
	LabelBuilderMethodIdSelectable LabelBuilderMethodId = 1
)

type TreeNodeBuilderMethodId = runtime.FuncProcId

const (
	TreeNodeBuilderIdBuild          TreeNodeBuilderMethodId = 0
	TreeNodeBuilderIdLabelText LabelBuilderMethodId    = 1
)

type ResponseFlags uint32

func (inst ResponseFlags) HasPrimaryClicked() bool {
	return inst&ResponseFlagsPrimaryClicked != 0
}
func (inst ResponseFlags) HasSecondaryClicked() bool {
	return inst&ResponseFlagsSecondaryClicked != 0
}

const (
	ResponseFlagsPrimaryClicked      = 1 << 0
	ResponseFlagsSecondaryClicked    = 1 << 1
	ResponseFlagsLongTouched         = 1 << 2
	ResponseFlagsMiddleClicked       = 1 << 3
	ResponseFlagsDoubleClicked       = 1 << 4
	ResponseFlagsTripleClicked       = 1 << 5
	ResponseFlagsClickedElsewhere    = 1 << 6
	ResponseFlagsEnabled             = 1 << 7
	ResponseFlagsHovered             = 1 << 8
	ResponseFlagsContainsPointer     = 1 << 9
	ResponseFlagsHighlighted         = 1 << 10
	ResponseFlagsHasFocus            = 1 << 11
	ResponseFlagsGainedFocus         = 1 << 12
	ResponseFlagsLostFocus           = 1 << 13
	ResponseFlagsDragStarted         = 1 << 14
	ResponseFlagsDragged             = 1 << 15
	ResponseFlagsDragStopped         = 1 << 16
	ResponseFlagsIsPointerButtonDown = 1 << 17
	ResponseFlagsChanged             = 1 << 18
	ResponseFlagsShouldClose         = 1 << 19
	ResponseFlagsIsTooltipOpen       = 1 << 20
)

var AllResponseFlagss = []ResponseFlags{
	ResponseFlagsPrimaryClicked,
	ResponseFlagsSecondaryClicked,
	ResponseFlagsLongTouched,
	ResponseFlagsMiddleClicked,
	ResponseFlagsDoubleClicked,
	ResponseFlagsTripleClicked,
	ResponseFlagsClickedElsewhere,
	ResponseFlagsEnabled,
	ResponseFlagsHovered,
	ResponseFlagsContainsPointer,
	ResponseFlagsHighlighted,
	ResponseFlagsHasFocus,
	ResponseFlagsGainedFocus,
	ResponseFlagsLostFocus,
	ResponseFlagsDragStarted,
	ResponseFlagsDragged,
	ResponseFlagsDragStopped,
	ResponseFlagsIsPointerButtonDown,
	ResponseFlagsChanged,
	ResponseFlagsShouldClose,
	ResponseFlagsIsTooltipOpen,
}
