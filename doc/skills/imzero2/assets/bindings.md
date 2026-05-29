---
type: reference
audience: agent reading this skill asset
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
Below is the public API surface of the library. Function bodies are stubbed (bodies replaced with `panic("stub")`) -- ignore this, it is an artifact of the export process. Your job is to write code that consumes this API.

--- FILE: egui2_enums.go ---
```go
package components

import (
	"iter"
	_ "iter"
	_ "math/bits"
)

type ResponseFlagsE uint32

const NilResponseFlags ResponseFlagsE = 0
const (
	PrimaryClickedResponseFlags      ResponseFlagsE = 1 << 0
	SecondaryClickedResponseFlags    ResponseFlagsE = 1 << 1
	LongTouchedResponseFlags         ResponseFlagsE = 1 << 2
	MiddleClickedResponseFlags       ResponseFlagsE = 1 << 3
	DoubleClickedResponseFlags       ResponseFlagsE = 1 << 4
	TripleClickedResponseFlags       ResponseFlagsE = 1 << 5
	ClickedElsewhereResponseFlags    ResponseFlagsE = 1 << 6
	EnabledResponseFlags             ResponseFlagsE = 1 << 7
	HoveredResponseFlags             ResponseFlagsE = 1 << 8
	ContainsPointerResponseFlags     ResponseFlagsE = 1 << 9
	HighlighterResponseFlags         ResponseFlagsE = 1 << 10
	HasFocusResponseFlags            ResponseFlagsE = 1 << 11
	GainedFocusResponseFlags         ResponseFlagsE = 1 << 12
	LostFocusResponseFlags           ResponseFlagsE = 1 << 13
	DragStartedResponseFlags         ResponseFlagsE = 1 << 14
	DraggedResponseFlags             ResponseFlagsE = 1 << 15
	DragStoppedResponseFlags         ResponseFlagsE = 1 << 16
	IsPointerButtonDownResponseFlags ResponseFlagsE = 1 << 17
	ChangedResponseFlags             ResponseFlagsE = 1 << 18
	ShouldCloseResponseFlags         ResponseFlagsE = 1 << 19
	IsTooltipOpenResponseFlags       ResponseFlagsE = 1 << 20

	NodelikeSelectedFlags ResponseFlagsE = 1 << 30
	BlockSkippedFlags     ResponseFlagsE = 1 << 31
)

var AllResponseFlags = []ResponseFlagsE{
	PrimaryClickedResponseFlags,
	SecondaryClickedResponseFlags,
	LongTouchedResponseFlags,
	MiddleClickedResponseFlags,
	DoubleClickedResponseFlags,
	TripleClickedResponseFlags,
	ClickedElsewhereResponseFlags,
	EnabledResponseFlags,
	HoveredResponseFlags,
	ContainsPointerResponseFlags,
	HighlighterResponseFlags,
	HasFocusResponseFlags,
	GainedFocusResponseFlags,
	LostFocusResponseFlags,
	DragStartedResponseFlags,
	DraggedResponseFlags,
	DragStoppedResponseFlags,
	IsPointerButtonDownResponseFlags,
	ChangedResponseFlags,
	ShouldCloseResponseFlags,
	IsTooltipOpenResponseFlags,
	NodelikeSelectedFlags,
	BlockSkippedFlags,
}

func (inst ResponseFlagsE) Count() int { panic("stub") }

func (inst ResponseFlagsE) Iterate() iter.Seq[ResponseFlagsE] { panic("stub") }

func (inst ResponseFlagsE) Has(v ResponseFlagsE) bool { panic("stub") }

func (inst ResponseFlagsE) Set(v ResponseFlagsE) ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) Clear(v ResponseFlagsE) ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) HasPrimaryClicked() bool { panic("stub") }

func (inst ResponseFlagsE) HasSecondaryClicked() bool { panic("stub") }

func (inst ResponseFlagsE) HasLongTouched() bool { panic("stub") }

func (inst ResponseFlagsE) HasDoubleClicked() bool { panic("stub") }

func (inst ResponseFlagsE) HasTripleClicked() bool { panic("stub") }

func (inst ResponseFlagsE) HasClickedElsewhere() bool { panic("stub") }

func (inst ResponseFlagsE) HasEnabled() bool { panic("stub") }

func (inst ResponseFlagsE) HasMiddleClicked() bool { panic("stub") }

func (inst ResponseFlagsE) HasHovered() bool { panic("stub") }

func (inst ResponseFlagsE) HasContainsPointer() bool { panic("stub") }

func (inst ResponseFlagsE) HasHighlighter() bool { panic("stub") }

func (inst ResponseFlagsE) HasFocus() bool { panic("stub") }

func (inst ResponseFlagsE) HasGainedFocus() bool { panic("stub") }

func (inst ResponseFlagsE) HasLostFocus() bool { panic("stub") }

func (inst ResponseFlagsE) HasDragStarted() bool { panic("stub") }

func (inst ResponseFlagsE) HasDragged() bool { panic("stub") }

func (inst ResponseFlagsE) HasDragStopped() bool { panic("stub") }

func (inst ResponseFlagsE) HasIsPointerButtonDown() bool { panic("stub") }

func (inst ResponseFlagsE) HasChanged() bool { panic("stub") }

func (inst ResponseFlagsE) HasShouldClose() bool { panic("stub") }

func (inst ResponseFlagsE) HasIsTooltipOpen() bool { panic("stub") }

func (inst ResponseFlagsE) HasNodelikeSelected() bool { panic("stub") }

func (inst ResponseFlagsE) HasBlockSkipped() bool { panic("stub") }

func (inst ResponseFlagsE) ClearPrimaryClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearSecondaryClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearLongTouched() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearDoubleClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearTripleClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearClickedElsewhere() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearEnabled() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearMiddleClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearHovered() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearContainsPointer() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearHighlighter() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearFocus() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearGainedFocus() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearLostFocus() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearDragStarted() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearDragged() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearDragStopped() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearIsPointerButtonDown() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearChanged() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearShouldClose() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearIsTooltipOpen() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearNodelikeSelected() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) ClearBlockSkipped() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetPrimaryClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetSecondaryClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetLongTouched() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetDoubleClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetTripleClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetClickedElsewhere() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetEnabled() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetMiddleClicked() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetHovered() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetContainsPointer() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetHighlighter() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetFocus() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetGainedFocus() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetLostFocus() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetDragStarted() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetDragged() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetDragStopped() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetIsPointerButtonDown() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetChanged() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetShouldClose() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetIsTooltipOpen() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetNodelikeSelected() ResponseFlagsE { panic("stub") }

func (inst ResponseFlagsE) SetBlockSkipped() ResponseFlagsE { panic("stub") }


```

--- FILE: egui2_globals.go ---
```go
package components

import (
	_ "iter"

	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/runtime"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/typed"
)

const FuncProcIdOffset FuncProcIdE = 0

type Fetcher struct {
}

func NewFetcher() (inst *Fetcher) { panic("stub") }


```

--- FILE: egui2_id_handling.go ---
```go
package components

import (
	_ "fmt"
	"iter"
	_ "iter"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/hashing/splitmix64"
	_ "github.com/stergiotis/pebble2impl/public/compiletimeflags"
	_ "github.com/zeebo/xxh3"
)

//	func ensureNotZeroIdHighEntropySlow(id uint64) uint64 {
//		if id == 0 {
//			return 1 // egui disallows 0 as id
//		}
//		return id
//	}

// loose one bit of a high entropy number should not matter in practice.
// this is a fast way to prevent zero as id which is forbidden by egui
// ensure bit 0 is high

type WidgetIdCreatorI interface {
	// Derive side effect free
	Derive() uint64
	// DeriveStacked side effect: stack manipulation
	DeriveStacked() uint64
	// PopIdFromStack side effect: stack manipulation
	PopIdFromStack()
	// PopIdFromStackChecked side effect: stack manipulation
	PopIdFromStackChecked(expectedId uint64)
}

type AbsoluteWidgetId uint64

func MakeAbsoluteIdStr(str string) AbsoluteWidgetId { panic("stub") }

func MakeAbsoluteIdHighEntropy(id uint64) AbsoluteWidgetId { panic("stub") }

func MakeAbsoluteIdSeq(idx uint64) AbsoluteWidgetId { panic("stub") }

func (inst AbsoluteWidgetId) Derive() uint64 { panic("stub") }

func (inst AbsoluteWidgetId) DeriveStacked() uint64 { panic("stub") }

func (inst AbsoluteWidgetId) PopIdFromStack() {
	panic(
		// no-op
		"stub")
}

func (inst AbsoluteWidgetId) PopIdFromStackChecked(expectedId uint64) {
	panic(
		// no-op
		"stub")
}

type WidgetIdStackStateE uint8

func (inst WidgetIdStackStateE) String() string { panic("stub") }

const (
	WidgetIdStackInitial  WidgetIdStackStateE = 0
	WidgetIdStackPrepared WidgetIdStackStateE = 1
)

type WidgetIdStack struct {
}

func NewWidgetIdStack() *WidgetIdStack { panic("stub") }

func (inst *WidgetIdStack) Derive() (id uint64) { panic("stub") }

func (inst *WidgetIdStack) DeriveStacked() uint64 { panic("stub") }

func (inst *WidgetIdStack) PrepareStr(str string) *WidgetIdStack { panic("stub") }

// high entropy

// PrepareSeq use this for mapping index sequences 0,1,2,3,... to valid ids (high-entropy, non-zero)
func (inst *WidgetIdStack) PrepareSeq(idx uint64) *WidgetIdStack { panic("stub") }

func (inst *WidgetIdStack) PrepareHighEntropy(id uint64) *WidgetIdStack { panic("stub") }

func (inst *WidgetIdStack) PopIdFromStack() { panic("stub") }

func (inst *WidgetIdStack) PopIdFromStackChecked(expectedId uint64) { panic("stub") }

func (inst *WidgetIdStack) Depth() int { panic("stub") }

func (inst *WidgetIdStack) Reset() { panic("stub") }

func IdScope(i *WidgetIdStack) iter.Seq[functional.NilIteratorValueType] { panic("stub") }


```

--- FILE: egui2_lifecycle.go ---
```go
package components

import (
	_ "github.com/rs/zerolog/log"
)

type ApplicationState struct {
	StateManager *StateManager
}

func NewApplicationState() *ApplicationState { panic("stub") }

func (inst *ApplicationState) GetIdStack() *WidgetIdStack { panic("stub") }

func (inst *ApplicationState) StartServersideFrame() { panic("stub") }

func (inst *ApplicationState) FinishServersideFrame() { panic("stub") }

var CurrentApplicationState = NewApplicationState()


```

--- FILE: egui2_methods.go ---
```go
package components

import (
	"iter"
	_ "iter"

	"github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/functional"
)

func (inst ButtonFluid) SendResp() ResponseFlagsE { panic("stub") }

func (inst NodeLeafFluid) SendResp() ResponseFlagsE { panic("stub") }

func (inst RadioButtonFluid) SendResp() ResponseFlagsE { panic("stub") }

func (inst NodeDirFluid) SendIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst SliderF64Fluid) SendRespVal(val *float64) ResponseFlagsE { panic("stub") }

func (inst CheckboxFluid) SendRespVal(val *bool) ResponseFlagsE { panic("stub") }

func (inst TextEditFluid) SendRespVal(val *string) ResponseFlagsE { panic("stub") }

func (inst DragValueF64Fluid) SendRespVal(val *float64) ResponseFlagsE { panic("stub") }

func (inst PushRichTextFluid) Display() { panic("stub") }

//LabelWidgetText(widgetTextKept).Send()

func (inst PushRichTextColoredFluid) Display() { panic("stub") }

//LabelWidgetText(widgetTextKept).Send()


```

--- FILE: egui2_statemanagement.go ---
```go
package components

import (
	"iter"
	_ "iter"

	_ "github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/containers/ragged"
	_ "github.com/stergiotis/boxer/public/functional"
)

type StateManager struct {
}

func NewStateManager() *StateManager { panic("stub") }

func (inst *StateManager) GetResponseById(id WidgetIdCreatorI) ResponseFlagsE { panic("stub") }

func (inst *StateManager) GetResponseByIdRaw(id uint64) ResponseFlagsE { panic("stub") }

func (inst *StateManager) AddR10Databinding(id uint64, ptr *bool) { panic("stub") }

func (inst *StateManager) AddR9F64Databinding(id uint64, ptr *float64) { panic("stub") }

func (inst *StateManager) AddR9SDatabinding(id uint64, ptr *string) { panic("stub") }

func (inst *StateManager) OverrideDatabindingWidget(id WidgetIdCreatorI) { panic("stub") }

func (inst *StateManager) OverrideDatabindingWidgetRaw(id uint64) { panic("stub") }

func (inst *StateManager) IterateDatabindingWidgetsByF64Ptr(ptr *float64) iter.Seq[uint64] {
	panic("stub")
}

func (inst *StateManager) OverrideDatabindingF64Ptr(ptr *float64) { panic("stub") }

func (inst *StateManager) IterateDatabindingWidgetsBySPtr(ptr *string) iter.Seq[uint64] {
	panic("stub")
}

func (inst *StateManager) OverrideDatabindingSPtr(ptr *string) { panic("stub") }

func (inst *StateManager) IterateDatabindingWidgetsByBPtr(ptr *bool) iter.Seq[uint64] { panic("stub") }

func (inst *StateManager) OverrideDatabindingBPtr(ptr *bool) { panic("stub") }

func (inst *StateManager) Sync() { panic("stub") }

// important: we need to consume all iterators as these directly read from the fffi channel!

func (inst *StateManager) Reset() { panic("stub") }


```

--- FILE: enums.out.go ---
```go
// Code generated; TheStack (github.com/stergiotis/pebble2impl/public/thestack/cmd/imzero2) DO NOT EDIT.

package components

type FuncProcIdE uint32

const (
	FuncProcIdAddSpace                            FuncProcIdE = FuncProcIdOffset + 0
	FuncProcIdAtoms                               FuncProcIdE = FuncProcIdOffset + 1
	FuncProcIdButton                              FuncProcIdE = FuncProcIdOffset + 2
	FuncProcIdCheckbox                            FuncProcIdE = FuncProcIdOffset + 3
	FuncProcIdCollapsingHeader                    FuncProcIdE = FuncProcIdOffset + 4
	FuncProcIdColor                               FuncProcIdE = FuncProcIdOffset + 5
	FuncProcIdComboBox                            FuncProcIdE = FuncProcIdOffset + 6
	FuncProcIdContextInspectionUi                 FuncProcIdE = FuncProcIdOffset + 7
	FuncProcIdContextSendViewPortCommandClose     FuncProcIdE = FuncProcIdOffset + 8
	FuncProcIdDragValueF64                        FuncProcIdE = FuncProcIdOffset + 9
	FuncProcIdDragValueI64                        FuncProcIdE = FuncProcIdOffset + 10
	FuncProcIdDragValueU64                        FuncProcIdE = FuncProcIdOffset + 11
	FuncProcIdEnd                                 FuncProcIdE = FuncProcIdOffset + 12
	FuncProcIdEndRow                              FuncProcIdE = FuncProcIdOffset + 13
	FuncProcIdFetchR10                            FuncProcIdE = FuncProcIdOffset + 14
	FuncProcIdFetchR7                             FuncProcIdE = FuncProcIdOffset + 15
	FuncProcIdFetchR9F64                          FuncProcIdE = FuncProcIdOffset + 16
	FuncProcIdFetchR9I64                          FuncProcIdE = FuncProcIdOffset + 17
	FuncProcIdFetchR9S                            FuncProcIdE = FuncProcIdOffset + 18
	FuncProcIdFetchR9U64                          FuncProcIdE = FuncProcIdOffset + 19
	FuncProcIdFrame                               FuncProcIdE = FuncProcIdOffset + 20
	FuncProcIdGrid                                FuncProcIdE = FuncProcIdOffset + 21
	FuncProcIdGuiZoomZoomMenuButtons              FuncProcIdE = FuncProcIdOffset + 22
	FuncProcIdHorizontal                          FuncProcIdE = FuncProcIdOffset + 23
	FuncProcIdHorizontalCentered                  FuncProcIdE = FuncProcIdOffset + 24
	FuncProcIdHorizontalTop                       FuncProcIdE = FuncProcIdOffset + 25
	FuncProcIdHorizontalWrapped                   FuncProcIdE = FuncProcIdOffset + 26
	FuncProcIdLabel                               FuncProcIdE = FuncProcIdOffset + 27
	FuncProcIdLabelAtoms                          FuncProcIdE = FuncProcIdOffset + 28
	FuncProcIdLabelWidgetText                     FuncProcIdE = FuncProcIdOffset + 29
	FuncProcIdMemoryResetAreas                    FuncProcIdE = FuncProcIdOffset + 30
	FuncProcIdMenuBar                             FuncProcIdE = FuncProcIdOffset + 31
	FuncProcIdMenuButton                          FuncProcIdE = FuncProcIdOffset + 32
	FuncProcIdNodeDir                             FuncProcIdE = FuncProcIdOffset + 33
	FuncProcIdNodeDirClose                        FuncProcIdE = FuncProcIdOffset + 34
	FuncProcIdNodeLeaf                            FuncProcIdE = FuncProcIdOffset + 35
	FuncProcIdPanelBottom                         FuncProcIdE = FuncProcIdOffset + 36
	FuncProcIdPanelBottomInside                   FuncProcIdE = FuncProcIdOffset + 37
	FuncProcIdPanelLeft                           FuncProcIdE = FuncProcIdOffset + 38
	FuncProcIdPanelLeftInside                     FuncProcIdE = FuncProcIdOffset + 39
	FuncProcIdPanelRight                          FuncProcIdE = FuncProcIdOffset + 40
	FuncProcIdPanelRightInside                    FuncProcIdE = FuncProcIdOffset + 41
	FuncProcIdPanelTop                            FuncProcIdE = FuncProcIdOffset + 42
	FuncProcIdPanelTopInside                      FuncProcIdE = FuncProcIdOffset + 43
	FuncProcIdPassthrough                         FuncProcIdE = FuncProcIdOffset + 44
	FuncProcIdPrepareNextFrame                    FuncProcIdE = FuncProcIdOffset + 45
	FuncProcIdPushRichText                        FuncProcIdE = FuncProcIdOffset + 46
	FuncProcIdPushRichTextColored                 FuncProcIdE = FuncProcIdOffset + 47
	FuncProcIdRadioButton                         FuncProcIdE = FuncProcIdOffset + 48
	FuncProcIdRequestRepaint                      FuncProcIdE = FuncProcIdOffset + 49
	FuncProcIdScalarSize                          FuncProcIdE = FuncProcIdOffset + 50
	FuncProcIdScrollArea                          FuncProcIdE = FuncProcIdOffset + 51
	FuncProcIdSeparator                           FuncProcIdE = FuncProcIdOffset + 52
	FuncProcIdShowDebugTools                      FuncProcIdE = FuncProcIdOffset + 53
	FuncProcIdShowPuffinProfiler                  FuncProcIdE = FuncProcIdOffset + 54
	FuncProcIdSliderF64                           FuncProcIdE = FuncProcIdOffset + 55
	FuncProcIdSliderI64                           FuncProcIdE = FuncProcIdOffset + 56
	FuncProcIdSliderU64                           FuncProcIdE = FuncProcIdOffset + 57
	FuncProcIdSpinner                             FuncProcIdE = FuncProcIdOffset + 58
	FuncProcIdTextEdit                            FuncProcIdE = FuncProcIdOffset + 59
	FuncProcIdTree                                FuncProcIdE = FuncProcIdOffset + 60
	FuncProcIdUiWithLayout                        FuncProcIdE = FuncProcIdOffset + 61
	FuncProcIdVectorSize                          FuncProcIdE = FuncProcIdOffset + 62
	FuncProcIdVertical                            FuncProcIdE = FuncProcIdOffset + 63
	FuncProcIdVerticalCentered                    FuncProcIdE = FuncProcIdOffset + 64
	FuncProcIdVerticalCenteredJustified           FuncProcIdE = FuncProcIdOffset + 65
	FuncProcIdWarnIfDebugBuild                    FuncProcIdE = FuncProcIdOffset + 66
	FuncProcIdWidgetText                          FuncProcIdE = FuncProcIdOffset + 67
	FuncProcIdWidgetsGlobalThemePreferenceButtons FuncProcIdE = FuncProcIdOffset + 68
	FuncProcIdWindow                              FuncProcIdE = FuncProcIdOffset + 69
)
const (
	AtomsMethodIdBuild AtomsMethodIdE = 0

	AtomsMethodIdText AtomsMethodIdE = 1
)

const (
	ButtonMethodIdBuild ButtonMethodIdE = 0

	ButtonMethodIdFrame             ButtonMethodIdE = 1
	ButtonMethodIdSmall             ButtonMethodIdE = 2
	ButtonMethodIdWrap              ButtonMethodIdE = 3
	ButtonMethodIdTruncate          ButtonMethodIdE = 4
	ButtonMethodIdSelected          ButtonMethodIdE = 5
	ButtonMethodIdFrameWhenInactive ButtonMethodIdE = 6
	ButtonMethodIdRightText         ButtonMethodIdE = 7
	ButtonMethodIdShortcutText      ButtonMethodIdE = 8
)

const (
	CheckboxMethodIdBuild CheckboxMethodIdE = 0

	CheckboxMethodIdIndeterminate CheckboxMethodIdE = 1
)

const (
	CollapsingHeaderMethodIdBuild CollapsingHeaderMethodIdE = 0

	CollapsingHeaderMethodIdDefaultOpen CollapsingHeaderMethodIdE = 1
	CollapsingHeaderMethodIdOpen        CollapsingHeaderMethodIdE = 2
	CollapsingHeaderMethodIdClose       CollapsingHeaderMethodIdE = 3
)

const (
	ColorMethodIdBuild ColorMethodIdE = 0

	ColorMethodIdFromRgb               ColorMethodIdE = 1
	ColorMethodIdFromRgbaUnmultiplied  ColorMethodIdE = 2
	ColorMethodIdFromRgbaPremultiplied ColorMethodIdE = 3
	ColorMethodIdFromGray              ColorMethodIdE = 4
	ColorMethodIdFromBlackAlpha        ColorMethodIdE = 5
	ColorMethodIdGammaMultiplyU8       ColorMethodIdE = 6
	ColorMethodIdGammaMultiplyF32      ColorMethodIdE = 7
	ColorMethodIdLinearMultiplyF32     ColorMethodIdE = 8
	ColorMethodIdToOpaque              ColorMethodIdE = 9
	ColorMethodIdColorTransparent      ColorMethodIdE = 10
	ColorMethodIdColorBlack            ColorMethodIdE = 11
	ColorMethodIdColorDarkGray         ColorMethodIdE = 12
	ColorMethodIdColorGray             ColorMethodIdE = 13
	ColorMethodIdColorLightGray        ColorMethodIdE = 14
	ColorMethodIdColorWhite            ColorMethodIdE = 15
	ColorMethodIdColorBrown            ColorMethodIdE = 16
	ColorMethodIdColorDarkRed          ColorMethodIdE = 17
	ColorMethodIdColorLightRed         ColorMethodIdE = 18
	ColorMethodIdColorCyan             ColorMethodIdE = 19
	ColorMethodIdColorMagenta          ColorMethodIdE = 20
	ColorMethodIdColorYellow           ColorMethodIdE = 21
	ColorMethodIdColorOrange           ColorMethodIdE = 22
	ColorMethodIdColorLightYellow      ColorMethodIdE = 23
	ColorMethodIdColorKhaki            ColorMethodIdE = 24
	ColorMethodIdColorDarkGreen        ColorMethodIdE = 25
	ColorMethodIdColorGreen            ColorMethodIdE = 26
	ColorMethodIdColorLightGreen       ColorMethodIdE = 27
	ColorMethodIdColorDarkBlue         ColorMethodIdE = 28
	ColorMethodIdColorBlue             ColorMethodIdE = 29
	ColorMethodIdColorLightBlue        ColorMethodIdE = 30
	ColorMethodIdColorPurple           ColorMethodIdE = 31
	ColorMethodIdColorGold             ColorMethodIdE = 32
	ColorMethodIdColorDebugColor       ColorMethodIdE = 33
	ColorMethodIdColorPlaceholder      ColorMethodIdE = 34
)

const (
	ComboBoxMethodIdBuild ComboBoxMethodIdE = 0

	ComboBoxMethodIdWidth    ComboBoxMethodIdE = 1
	ComboBoxMethodIdHeight   ComboBoxMethodIdE = 2
	ComboBoxMethodIdWrap     ComboBoxMethodIdE = 3
	ComboBoxMethodIdTruncate ComboBoxMethodIdE = 4
)

const (
	DragValueF64MethodIdBuild DragValueF64MethodIdE = 0

	DragValueF64MethodIdSpeed              DragValueF64MethodIdE = 1
	DragValueF64MethodIdPrefix             DragValueF64MethodIdE = 2
	DragValueF64MethodIdSuffix             DragValueF64MethodIdE = 3
	DragValueF64MethodIdMinDecimals        DragValueF64MethodIdE = 4
	DragValueF64MethodIdMaxDecimals        DragValueF64MethodIdE = 5
	DragValueF64MethodIdFixedDecimals      DragValueF64MethodIdE = 6
	DragValueF64MethodIdBinary             DragValueF64MethodIdE = 7
	DragValueF64MethodIdOctal              DragValueF64MethodIdE = 8
	DragValueF64MethodIdHexadecimal        DragValueF64MethodIdE = 9
	DragValueF64MethodIdUpdateWhileEditing DragValueF64MethodIdE = 10
)

const (
	DragValueI64MethodIdBuild DragValueI64MethodIdE = 0

	DragValueI64MethodIdSpeed              DragValueI64MethodIdE = 1
	DragValueI64MethodIdPrefix             DragValueI64MethodIdE = 2
	DragValueI64MethodIdSuffix             DragValueI64MethodIdE = 3
	DragValueI64MethodIdMinDecimals        DragValueI64MethodIdE = 4
	DragValueI64MethodIdMaxDecimals        DragValueI64MethodIdE = 5
	DragValueI64MethodIdFixedDecimals      DragValueI64MethodIdE = 6
	DragValueI64MethodIdBinary             DragValueI64MethodIdE = 7
	DragValueI64MethodIdOctal              DragValueI64MethodIdE = 8
	DragValueI64MethodIdHexadecimal        DragValueI64MethodIdE = 9
	DragValueI64MethodIdUpdateWhileEditing DragValueI64MethodIdE = 10
)

const (
	DragValueU64MethodIdBuild DragValueU64MethodIdE = 0

	DragValueU64MethodIdSpeed              DragValueU64MethodIdE = 1
	DragValueU64MethodIdPrefix             DragValueU64MethodIdE = 2
	DragValueU64MethodIdSuffix             DragValueU64MethodIdE = 3
	DragValueU64MethodIdMinDecimals        DragValueU64MethodIdE = 4
	DragValueU64MethodIdMaxDecimals        DragValueU64MethodIdE = 5
	DragValueU64MethodIdFixedDecimals      DragValueU64MethodIdE = 6
	DragValueU64MethodIdBinary             DragValueU64MethodIdE = 7
	DragValueU64MethodIdOctal              DragValueU64MethodIdE = 8
	DragValueU64MethodIdHexadecimal        DragValueU64MethodIdE = 9
	DragValueU64MethodIdUpdateWhileEditing DragValueU64MethodIdE = 10
)

const (
	FrameMethodIdBuild FrameMethodIdE = 0

	FrameMethodIdInnerMargin         FrameMethodIdE = 1
	FrameMethodIdCornerRadius        FrameMethodIdE = 2
	FrameMethodIdOuterMargin         FrameMethodIdE = 3
	FrameMethodIdMultiplyWithOpacity FrameMethodIdE = 4
)

const (
	GridMethodIdBuild GridMethodIdE = 0

	GridMethodIdNumColumns   GridMethodIdE = 1
	GridMethodIdStriped      GridMethodIdE = 2
	GridMethodIdMinColWidth  GridMethodIdE = 3
	GridMethodIdMinRowHeight GridMethodIdE = 4
	GridMethodIdMaxColWidth  GridMethodIdE = 5
	GridMethodIdStartRow     GridMethodIdE = 6
)

const (
	LabelMethodIdBuild LabelMethodIdE = 0

	LabelMethodIdSelectable LabelMethodIdE = 1
	LabelMethodIdWrap       LabelMethodIdE = 2
	LabelMethodIdTruncate   LabelMethodIdE = 3
	LabelMethodIdExtend     LabelMethodIdE = 4
)

const (
	PanelBottomMethodIdBuild PanelBottomMethodIdE = 0

	PanelBottomMethodIdResizable     PanelBottomMethodIdE = 1
	PanelBottomMethodIdDefaultHeight PanelBottomMethodIdE = 2
	PanelBottomMethodIdExactHeight   PanelBottomMethodIdE = 3
)

const (
	PanelBottomInsideMethodIdBuild PanelBottomInsideMethodIdE = 0

	PanelBottomInsideMethodIdResizable     PanelBottomInsideMethodIdE = 1
	PanelBottomInsideMethodIdDefaultHeight PanelBottomInsideMethodIdE = 2
	PanelBottomInsideMethodIdExactHeight   PanelBottomInsideMethodIdE = 3
)

const (
	PanelLeftMethodIdBuild PanelLeftMethodIdE = 0

	PanelLeftMethodIdResizable    PanelLeftMethodIdE = 1
	PanelLeftMethodIdDefaultWidth PanelLeftMethodIdE = 2
	PanelLeftMethodIdExactWidth   PanelLeftMethodIdE = 3
)

const (
	PanelLeftInsideMethodIdBuild PanelLeftInsideMethodIdE = 0

	PanelLeftInsideMethodIdResizable    PanelLeftInsideMethodIdE = 1
	PanelLeftInsideMethodIdDefaultWidth PanelLeftInsideMethodIdE = 2
	PanelLeftInsideMethodIdExactWidth   PanelLeftInsideMethodIdE = 3
)

const (
	PanelRightMethodIdBuild PanelRightMethodIdE = 0

	PanelRightMethodIdResizable    PanelRightMethodIdE = 1
	PanelRightMethodIdDefaultWidth PanelRightMethodIdE = 2
	PanelRightMethodIdExactWidth   PanelRightMethodIdE = 3
)

const (
	PanelRightInsideMethodIdBuild PanelRightInsideMethodIdE = 0

	PanelRightInsideMethodIdResizable    PanelRightInsideMethodIdE = 1
	PanelRightInsideMethodIdDefaultWidth PanelRightInsideMethodIdE = 2
	PanelRightInsideMethodIdExactWidth   PanelRightInsideMethodIdE = 3
)

const (
	PanelTopMethodIdBuild PanelTopMethodIdE = 0

	PanelTopMethodIdResizable     PanelTopMethodIdE = 1
	PanelTopMethodIdDefaultHeight PanelTopMethodIdE = 2
	PanelTopMethodIdExactHeight   PanelTopMethodIdE = 3
)

const (
	PanelTopInsideMethodIdBuild PanelTopInsideMethodIdE = 0

	PanelTopInsideMethodIdResizable     PanelTopInsideMethodIdE = 1
	PanelTopInsideMethodIdDefaultHeight PanelTopInsideMethodIdE = 2
	PanelTopInsideMethodIdExactHeight   PanelTopInsideMethodIdE = 3
)

const (
	PushRichTextMethodIdBuild PushRichTextMethodIdE = 0

	PushRichTextMethodIdSize               PushRichTextMethodIdE = 1
	PushRichTextMethodIdExtraLetterSpacing PushRichTextMethodIdE = 2
	PushRichTextMethodIdLineHeight         PushRichTextMethodIdE = 3
	PushRichTextMethodIdLineHeightDefault  PushRichTextMethodIdE = 4
	PushRichTextMethodIdHeading            PushRichTextMethodIdE = 5
	PushRichTextMethodIdMonospace          PushRichTextMethodIdE = 6
	PushRichTextMethodIdCode               PushRichTextMethodIdE = 7
	PushRichTextMethodIdStrong             PushRichTextMethodIdE = 8
	PushRichTextMethodIdWeak               PushRichTextMethodIdE = 9
	PushRichTextMethodIdUnderline          PushRichTextMethodIdE = 10
	PushRichTextMethodIdStrikethrough      PushRichTextMethodIdE = 11
	PushRichTextMethodIdItalics            PushRichTextMethodIdE = 12
	PushRichTextMethodIdSmall              PushRichTextMethodIdE = 13
	PushRichTextMethodIdSmallRaised        PushRichTextMethodIdE = 14
	PushRichTextMethodIdRaised             PushRichTextMethodIdE = 15
)

const (
	PushRichTextColoredMethodIdBuild PushRichTextColoredMethodIdE = 0

	PushRichTextColoredMethodIdSize               PushRichTextColoredMethodIdE = 1
	PushRichTextColoredMethodIdExtraLetterSpacing PushRichTextColoredMethodIdE = 2
	PushRichTextColoredMethodIdLineHeight         PushRichTextColoredMethodIdE = 3
	PushRichTextColoredMethodIdLineHeightDefault  PushRichTextColoredMethodIdE = 4
	PushRichTextColoredMethodIdHeading            PushRichTextColoredMethodIdE = 5
	PushRichTextColoredMethodIdMonospace          PushRichTextColoredMethodIdE = 6
	PushRichTextColoredMethodIdCode               PushRichTextColoredMethodIdE = 7
	PushRichTextColoredMethodIdStrong             PushRichTextColoredMethodIdE = 8
	PushRichTextColoredMethodIdWeak               PushRichTextColoredMethodIdE = 9
	PushRichTextColoredMethodIdUnderline          PushRichTextColoredMethodIdE = 10
	PushRichTextColoredMethodIdStrikethrough      PushRichTextColoredMethodIdE = 11
	PushRichTextColoredMethodIdItalics            PushRichTextColoredMethodIdE = 12
	PushRichTextColoredMethodIdSmall              PushRichTextColoredMethodIdE = 13
	PushRichTextColoredMethodIdSmallRaised        PushRichTextColoredMethodIdE = 14
	PushRichTextColoredMethodIdRaised             PushRichTextColoredMethodIdE = 15
)

const (
	ScalarSizeMethodIdBuild ScalarSizeMethodIdE = 0

	ScalarSizeMethodIdAvailableWidth  ScalarSizeMethodIdE = 1
	ScalarSizeMethodIdAvailableHeight ScalarSizeMethodIdE = 2
)

const (
	ScrollAreaMethodIdBuild ScrollAreaMethodIdE = 0

	ScrollAreaMethodIdHscroll  ScrollAreaMethodIdE = 1
	ScrollAreaMethodIdVscroll  ScrollAreaMethodIdE = 2
	ScrollAreaMethodIdAnimated ScrollAreaMethodIdE = 3
)

const (
	SeparatorMethodIdBuild SeparatorMethodIdE = 0

	SeparatorMethodIdHorizontal SeparatorMethodIdE = 1
	SeparatorMethodIdVertical   SeparatorMethodIdE = 2
	SeparatorMethodIdSpacing    SeparatorMethodIdE = 3
	SeparatorMethodIdGrow       SeparatorMethodIdE = 4
	SeparatorMethodIdShrink     SeparatorMethodIdE = 5
)

const (
	SliderF64MethodIdBuild SliderF64MethodIdE = 0

	SliderF64MethodIdShowValue          SliderF64MethodIdE = 1
	SliderF64MethodIdPrefix             SliderF64MethodIdE = 2
	SliderF64MethodIdSuffix             SliderF64MethodIdE = 3
	SliderF64MethodIdText               SliderF64MethodIdE = 4
	SliderF64MethodIdVertical           SliderF64MethodIdE = 5
	SliderF64MethodIdLogarithmic        SliderF64MethodIdE = 6
	SliderF64MethodIdSmallestPositive   SliderF64MethodIdE = 7
	SliderF64MethodIdLargestFinite      SliderF64MethodIdE = 8
	SliderF64MethodIdSmartAim           SliderF64MethodIdE = 9
	SliderF64MethodIdDragValueSpeed     SliderF64MethodIdE = 10
	SliderF64MethodIdMinDecimals        SliderF64MethodIdE = 11
	SliderF64MethodIdMaxDecimals        SliderF64MethodIdE = 12
	SliderF64MethodIdFixedDecimals      SliderF64MethodIdE = 13
	SliderF64MethodIdTrailingFill       SliderF64MethodIdE = 14
	SliderF64MethodIdBinary             SliderF64MethodIdE = 15
	SliderF64MethodIdOctal              SliderF64MethodIdE = 16
	SliderF64MethodIdHexadecimal        SliderF64MethodIdE = 17
	SliderF64MethodIdInteger            SliderF64MethodIdE = 18
	SliderF64MethodIdUpdateWhileEditing SliderF64MethodIdE = 19
)

const (
	SliderI64MethodIdBuild SliderI64MethodIdE = 0

	SliderI64MethodIdShowValue          SliderI64MethodIdE = 1
	SliderI64MethodIdPrefix             SliderI64MethodIdE = 2
	SliderI64MethodIdSuffix             SliderI64MethodIdE = 3
	SliderI64MethodIdText               SliderI64MethodIdE = 4
	SliderI64MethodIdVertical           SliderI64MethodIdE = 5
	SliderI64MethodIdLogarithmic        SliderI64MethodIdE = 6
	SliderI64MethodIdSmallestPositive   SliderI64MethodIdE = 7
	SliderI64MethodIdLargestFinite      SliderI64MethodIdE = 8
	SliderI64MethodIdSmartAim           SliderI64MethodIdE = 9
	SliderI64MethodIdDragValueSpeed     SliderI64MethodIdE = 10
	SliderI64MethodIdMinDecimals        SliderI64MethodIdE = 11
	SliderI64MethodIdMaxDecimals        SliderI64MethodIdE = 12
	SliderI64MethodIdFixedDecimals      SliderI64MethodIdE = 13
	SliderI64MethodIdTrailingFill       SliderI64MethodIdE = 14
	SliderI64MethodIdBinary             SliderI64MethodIdE = 15
	SliderI64MethodIdOctal              SliderI64MethodIdE = 16
	SliderI64MethodIdHexadecimal        SliderI64MethodIdE = 17
	SliderI64MethodIdInteger            SliderI64MethodIdE = 18
	SliderI64MethodIdUpdateWhileEditing SliderI64MethodIdE = 19
)

const (
	SliderU64MethodIdBuild SliderU64MethodIdE = 0

	SliderU64MethodIdShowValue          SliderU64MethodIdE = 1
	SliderU64MethodIdPrefix             SliderU64MethodIdE = 2
	SliderU64MethodIdSuffix             SliderU64MethodIdE = 3
	SliderU64MethodIdText               SliderU64MethodIdE = 4
	SliderU64MethodIdVertical           SliderU64MethodIdE = 5
	SliderU64MethodIdLogarithmic        SliderU64MethodIdE = 6
	SliderU64MethodIdSmallestPositive   SliderU64MethodIdE = 7
	SliderU64MethodIdLargestFinite      SliderU64MethodIdE = 8
	SliderU64MethodIdSmartAim           SliderU64MethodIdE = 9
	SliderU64MethodIdDragValueSpeed     SliderU64MethodIdE = 10
	SliderU64MethodIdMinDecimals        SliderU64MethodIdE = 11
	SliderU64MethodIdMaxDecimals        SliderU64MethodIdE = 12
	SliderU64MethodIdFixedDecimals      SliderU64MethodIdE = 13
	SliderU64MethodIdTrailingFill       SliderU64MethodIdE = 14
	SliderU64MethodIdBinary             SliderU64MethodIdE = 15
	SliderU64MethodIdOctal              SliderU64MethodIdE = 16
	SliderU64MethodIdHexadecimal        SliderU64MethodIdE = 17
	SliderU64MethodIdInteger            SliderU64MethodIdE = 18
	SliderU64MethodIdUpdateWhileEditing SliderU64MethodIdE = 19
)

const (
	SpinnerMethodIdBuild SpinnerMethodIdE = 0

	SpinnerMethodIdSize SpinnerMethodIdE = 1
)

const (
	TextEditMethodIdBuild TextEditMethodIdE = 0

	TextEditMethodIdCodeEditor   TextEditMethodIdE = 1
	TextEditMethodIdFrame        TextEditMethodIdE = 2
	TextEditMethodIdHintText     TextEditMethodIdE = 3
	TextEditMethodIdPassword     TextEditMethodIdE = 4
	TextEditMethodIdInteractive  TextEditMethodIdE = 5
	TextEditMethodIdDesiredWidth TextEditMethodIdE = 6
	TextEditMethodIdDesiredRows  TextEditMethodIdE = 7
	TextEditMethodIdLockFocus    TextEditMethodIdE = 8
	TextEditMethodIdCursorAtEnd  TextEditMethodIdE = 9
	TextEditMethodIdClipText     TextEditMethodIdE = 10
	TextEditMethodIdCharLimit    TextEditMethodIdE = 11
)

const (
	UiWithLayoutMethodIdBuild UiWithLayoutMethodIdE = 0

	UiWithLayoutMethodIdMainDirLeftToRight UiWithLayoutMethodIdE = 1
	UiWithLayoutMethodIdMainDirRightToLeft UiWithLayoutMethodIdE = 2
	UiWithLayoutMethodIdMainDirTopDown     UiWithLayoutMethodIdE = 3
	UiWithLayoutMethodIdMainDirBottomUp    UiWithLayoutMethodIdE = 4
	UiWithLayoutMethodIdMainWrap           UiWithLayoutMethodIdE = 5
	UiWithLayoutMethodIdMainJustify        UiWithLayoutMethodIdE = 6
	UiWithLayoutMethodIdCrossAlignMin      UiWithLayoutMethodIdE = 7
	UiWithLayoutMethodIdCrossAlignCenter   UiWithLayoutMethodIdE = 8
	UiWithLayoutMethodIdCrossAlignMax      UiWithLayoutMethodIdE = 9
	UiWithLayoutMethodIdCrossJustify       UiWithLayoutMethodIdE = 10
)

const (
	VectorSizeMethodIdBuild VectorSizeMethodIdE = 0

	VectorSizeMethodIdAvailableSize VectorSizeMethodIdE = 1
)

const (
	WidgetTextMethodIdBuild WidgetTextMethodIdE = 0

	WidgetTextMethodIdText WidgetTextMethodIdE = 1
)

const (
	WindowMethodIdBuild WindowMethodIdE = 0

	WindowMethodIdDefaultOpen  WindowMethodIdE = 1
	WindowMethodIdEnabled      WindowMethodIdE = 2
	WindowMethodIdInteractable WindowMethodIdE = 3
	WindowMethodIdMovable      WindowMethodIdE = 4
	WindowMethodIdResizable    WindowMethodIdE = 5
	WindowMethodIdCollapsible  WindowMethodIdE = 6
	WindowMethodIdTitleBar     WindowMethodIdE = 7
)


```

--- FILE: factories.out.go ---
```go
// Code generated; TheStack (github.com/stergiotis/pebble2impl/public/thestack/cmd/imzero2) DO NOT EDIT.

package components

import (
	"github.com/stergiotis/pebble2impl/public/thestack/fffi2/typed"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/typed"
)

func AddSpace(amount float32) { panic("stub") }

func Atoms() (inst AtomsFluid) { panic("stub") }

func Button(i WidgetIdCreatorI, atoms typed.RetainedFffiHolderTyped[AtomsS]) (inst ButtonFluid) {
	panic("stub")
}

func Checkbox(i WidgetIdCreatorI, checked bool, text string) (inst CheckboxFluid) { panic("stub") }

func CollapsingHeader(i WidgetIdCreatorI, label typed.RetainedFffiHolderTyped[WidgetTextS]) (inst CollapsingHeaderFluid) {
	panic("stub")
}

func Color() (inst ColorFluid) { panic("stub") }

func ComboBox(i WidgetIdCreatorI, label typed.RetainedFffiHolderTyped[WidgetTextS], selectedText typed.RetainedFffiHolderTyped[WidgetTextS]) (inst ComboBoxFluid) {
	panic("stub")
}

func ContextInspectionUi() { panic("stub") }

func ContextSendViewPortCommandClose() { panic("stub") }

func DragValueF64(i WidgetIdCreatorI, val float64) (inst DragValueF64Fluid) { panic("stub") }

func DragValueI64(i WidgetIdCreatorI, val int64) (inst DragValueI64Fluid) { panic("stub") }

func DragValueU64(i WidgetIdCreatorI, val uint64) (inst DragValueU64Fluid) { panic("stub") }

func End() { panic("stub") }

func EndRow() { panic("stub") }

func Frame(i WidgetIdCreatorI) (inst FrameFluid) { panic("stub") }

func Grid(i WidgetIdCreatorI) (inst GridFluid) { panic("stub") }

func GuiZoomZoomMenuButtons() { panic("stub") }

func Horizontal() (inst HorizontalFluid) { panic("stub") }

func HorizontalCentered() (inst HorizontalCenteredFluid) { panic("stub") }

func HorizontalTop() (inst HorizontalTopFluid) { panic("stub") }

func HorizontalWrapped() (inst HorizontalWrappedFluid) { panic("stub") }

func Label(text string) (inst LabelFluid) { panic("stub") }

func LabelAtoms(atoms typed.RetainedFffiHolderTyped[AtomsS]) (inst LabelAtomsFluid) { panic("stub") }

func LabelWidgetText(widgetText typed.RetainedFffiHolderTyped[WidgetTextS]) (inst LabelWidgetTextFluid) {
	panic("stub")
}

func MemoryResetAreas() { panic("stub") }

func MenuBar() (inst MenuBarFluid) { panic("stub") }

func MenuButton(atoms typed.RetainedFffiHolderTyped[AtomsS]) (inst MenuButtonFluid) { panic("stub") }

func NodeDir(i WidgetIdCreatorI, label typed.RetainedFffiHolderTyped[WidgetTextS]) (inst NodeDirFluid) {
	panic("stub")
}

func NodeDirClose(childCount uint32) (inst NodeDirCloseFluid) { panic("stub") }

func NodeLeaf(i WidgetIdCreatorI, label typed.RetainedFffiHolderTyped[WidgetTextS]) (inst NodeLeafFluid) {
	panic("stub")
}

func PanelBottom(i WidgetIdCreatorI) (inst PanelBottomFluid) { panic("stub") }

func PanelBottomInside(i WidgetIdCreatorI) (inst PanelBottomInsideFluid) { panic("stub") }

func PanelLeft(i WidgetIdCreatorI) (inst PanelLeftFluid) { panic("stub") }

func PanelLeftInside(i WidgetIdCreatorI) (inst PanelLeftInsideFluid) { panic("stub") }

func PanelRight(i WidgetIdCreatorI) (inst PanelRightFluid) { panic("stub") }

func PanelRightInside(i WidgetIdCreatorI) (inst PanelRightInsideFluid) { panic("stub") }

func PanelTop(i WidgetIdCreatorI) (inst PanelTopFluid) { panic("stub") }

func PanelTopInside(i WidgetIdCreatorI) (inst PanelTopInsideFluid) { panic("stub") }

func Passthrough(i WidgetIdCreatorI, input uint64) { panic("stub") }

func PrepareNextFrame() { panic("stub") }

func PushRichText(text string) (inst PushRichTextFluid) { panic("stub") }

func PushRichTextColored(cl typed.RetainedFffiHolderTyped[Color32S], bk typed.RetainedFffiHolderTyped[Color32S], text string) (inst PushRichTextColoredFluid) {
	panic("stub")
}

func RadioButton(i WidgetIdCreatorI, atoms typed.RetainedFffiHolderTyped[AtomsS], checked bool) (inst RadioButtonFluid) {
	panic("stub")
}

func RequestRepaint() { panic("stub") }

func ScalarSize() (inst ScalarSizeFluid) { panic("stub") }

func ScrollArea() (inst ScrollAreaFluid) { panic("stub") }

func Separator() (inst SeparatorFluid) { panic("stub") }

func ShowDebugTools() { panic("stub") }

func ShowPuffinProfiler() { panic("stub") }

func SliderF64(i WidgetIdCreatorI, val float64, rangeBeginIncl float64, rangeEndIncl float64) (inst SliderF64Fluid) {
	panic("stub")
}

func SliderI64(i WidgetIdCreatorI, val int64, rangeBeginIncl int64, rangeEndIncl int64) (inst SliderI64Fluid) {
	panic("stub")
}

func SliderU64(i WidgetIdCreatorI, val uint64, rangeBeginIncl uint64, rangeEndIncl uint64) (inst SliderU64Fluid) {
	panic("stub")
}

func Spinner() (inst SpinnerFluid) { panic("stub") }

func TextEdit(i WidgetIdCreatorI, text string) (inst TextEditFluid) { panic("stub") }

func Tree(i WidgetIdCreatorI) (inst TreeFluid) { panic("stub") }

func UiWithLayout() (inst UiWithLayoutFluid) { panic("stub") }

func VectorSize() (inst VectorSizeFluid) { panic("stub") }

func Vertical() (inst VerticalFluid) { panic("stub") }

func VerticalCentered() (inst VerticalCenteredFluid) { panic("stub") }

func VerticalCenteredJustified() (inst VerticalCenteredJustifiedFluid) { panic("stub") }

func WarnIfDebugBuild() { panic("stub") }

func WidgetText() (inst WidgetTextFluid) { panic("stub") }

func WidgetsGlobalThemePreferenceButtons() { panic("stub") }

func Window(i WidgetIdCreatorI, label typed.RetainedFffiHolderTyped[WidgetTextS]) (inst WindowFluid) {
	panic("stub")
}


```

--- FILE: fetchers.out.go ---
```go
// Code generated; TheStack (github.com/stergiotis/pebble2impl/public/thestack/cmd/imzero2) DO NOT EDIT.

package components

import (
	"iter"
	_ "iter"
)

func (inst *Fetcher) FetchR10() (idsTrue []uint64, idsFalse iter.Seq[uint64]) { panic("stub") }

func (inst *Fetcher) FetchR7() (ids []uint64, responses iter.Seq[uint32]) { panic("stub") }

func (inst *Fetcher) FetchR9F64() (ids []uint64, values iter.Seq[float64]) { panic("stub") }

func (inst *Fetcher) FetchR9I64() (ids []uint64, values iter.Seq[int64]) { panic("stub") }

func (inst *Fetcher) FetchR9S() (ids []uint64, values iter.Seq[string]) { panic("stub") }

func (inst *Fetcher) FetchR9U64() (ids []uint64, values iter.Seq[uint64]) { panic("stub") }


```

--- FILE: methods.out.go ---
```go
// Code generated; TheStack (github.com/stergiotis/pebble2impl/public/thestack/cmd/imzero2) DO NOT EDIT.

package components

import (
	"iter"

	"github.com/stergiotis/pebble2impl/public/thestack/fffi2/typed"
	_ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/typed"

	_ "iter"

	"github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/functional"
)

func (inst AtomsFluid) Text(val string) AtomsFluid { panic("stub") }

func (inst AtomsFluid) Keep() typed.RetainedFffiHolderTyped[AtomsS] { panic("stub") }

func (inst ButtonFluid) Frame(val bool) ButtonFluid { panic("stub") }

func (inst ButtonFluid) Small() ButtonFluid { panic("stub") }

func (inst ButtonFluid) Wrap() ButtonFluid { panic("stub") }

func (inst ButtonFluid) Truncate() ButtonFluid { panic("stub") }

func (inst ButtonFluid) Selected(selected bool) ButtonFluid { panic("stub") }

func (inst ButtonFluid) FrameWhenInactive(v bool) ButtonFluid { panic("stub") }

func (inst ButtonFluid) RightText(text string) ButtonFluid { panic("stub") }

func (inst ButtonFluid) ShortcutText(text string) ButtonFluid { panic("stub") }

func (inst ButtonFluid) Send() { panic("stub") }

func (inst ButtonFluid) Keep() typed.RetainedFffiHolderTyped[ButtonS] { panic("stub") }

func (inst CheckboxFluid) Indeterminate(indeterminate bool) CheckboxFluid { panic("stub") }

func (inst CheckboxFluid) Send() { panic("stub") }

func (inst CollapsingHeaderFluid) DefaultOpen(val bool) CollapsingHeaderFluid { panic("stub") }

func (inst CollapsingHeaderFluid) Open(val bool) CollapsingHeaderFluid { panic("stub") }

func (inst CollapsingHeaderFluid) Close(val bool) CollapsingHeaderFluid { panic("stub") }

func (inst CollapsingHeaderFluid) Send() { panic("stub") }

func (inst CollapsingHeaderFluid) Keep() typed.RetainedFffiHolderTyped[BlockI] { panic("stub") }

func (inst CollapsingHeaderFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst ColorFluid) FromRgb(rv uint8, gv uint8, bv uint8) ColorFluid { panic("stub") }

func (inst ColorFluid) FromRgbaUnmultiplied(rv uint8, gv uint8, bv uint8, av uint8) ColorFluid {
	panic("stub")
}

func (inst ColorFluid) FromRgbaPremultiplied(rv uint8, gv uint8, bv uint8, av uint8) ColorFluid {
	panic("stub")
}

func (inst ColorFluid) FromGray(lv uint8) ColorFluid { panic("stub") }

func (inst ColorFluid) FromBlackAlpha(av uint8) ColorFluid { panic("stub") }

func (inst ColorFluid) GammaMultiplyU8(factor uint8) ColorFluid { panic("stub") }

func (inst ColorFluid) GammaMultiplyF32(factor float32) ColorFluid { panic("stub") }

func (inst ColorFluid) LinearMultiplyF32(factor float32) ColorFluid { panic("stub") }

func (inst ColorFluid) ToOpaque() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorTransparent() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorBlack() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorDarkGray() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorGray() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorLightGray() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorWhite() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorBrown() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorDarkRed() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorLightRed() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorCyan() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorMagenta() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorYellow() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorOrange() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorLightYellow() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorKhaki() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorDarkGreen() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorGreen() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorLightGreen() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorDarkBlue() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorBlue() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorLightBlue() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorPurple() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorGold() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorDebugColor() ColorFluid { panic("stub") }

func (inst ColorFluid) ColorPlaceholder() ColorFluid { panic("stub") }

func (inst ColorFluid) Keep() typed.RetainedFffiHolderTyped[Color32S] { panic("stub") }

func (inst ComboBoxFluid) Width(width float32) ComboBoxFluid { panic("stub") }

func (inst ComboBoxFluid) Height(height float32) ComboBoxFluid { panic("stub") }

func (inst ComboBoxFluid) Wrap() ComboBoxFluid { panic("stub") }

func (inst ComboBoxFluid) Truncate() ComboBoxFluid { panic("stub") }

func (inst ComboBoxFluid) Send() { panic("stub") }

func (inst ComboBoxFluid) Keep() typed.RetainedFffiHolderTyped[BlockI] { panic("stub") }

func (inst ComboBoxFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst DragValueF64Fluid) Speed(speed float64) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) Prefix(prefix string) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) Suffix(suffix string) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) MinDecimals(digits uint32) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) MaxDecimals(digits uint32) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) FixedDecimals(digits uint32) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) Binary(minWidth uint32, twosComplement bool) DragValueF64Fluid {
	panic("stub")
}

func (inst DragValueF64Fluid) Octal(minWidth uint32, twosComplement bool) DragValueF64Fluid {
	panic("stub")
}

func (inst DragValueF64Fluid) Hexadecimal(minWidth uint32, twosComplement bool, upper bool) DragValueF64Fluid {
	panic("stub")
}

func (inst DragValueF64Fluid) UpdateWhileEditing(update bool) DragValueF64Fluid { panic("stub") }

func (inst DragValueF64Fluid) Send() { panic("stub") }

func (inst DragValueF64Fluid) Keep() typed.RetainedFffiHolderTyped[DragValueS] { panic("stub") }

func (inst DragValueI64Fluid) Speed(speed float64) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) Prefix(prefix string) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) Suffix(suffix string) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) MinDecimals(digits uint32) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) MaxDecimals(digits uint32) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) FixedDecimals(digits uint32) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) Binary(minWidth uint32, twosComplement bool) DragValueI64Fluid {
	panic("stub")
}

func (inst DragValueI64Fluid) Octal(minWidth uint32, twosComplement bool) DragValueI64Fluid {
	panic("stub")
}

func (inst DragValueI64Fluid) Hexadecimal(minWidth uint32, twosComplement bool, upper bool) DragValueI64Fluid {
	panic("stub")
}

func (inst DragValueI64Fluid) UpdateWhileEditing(update bool) DragValueI64Fluid { panic("stub") }

func (inst DragValueI64Fluid) Send() { panic("stub") }

func (inst DragValueI64Fluid) Keep() typed.RetainedFffiHolderTyped[DragValueS] { panic("stub") }

func (inst DragValueU64Fluid) Speed(speed float64) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) Prefix(prefix string) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) Suffix(suffix string) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) MinDecimals(digits uint32) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) MaxDecimals(digits uint32) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) FixedDecimals(digits uint32) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) Binary(minWidth uint32, twosComplement bool) DragValueU64Fluid {
	panic("stub")
}

func (inst DragValueU64Fluid) Octal(minWidth uint32, twosComplement bool) DragValueU64Fluid {
	panic("stub")
}

func (inst DragValueU64Fluid) Hexadecimal(minWidth uint32, twosComplement bool, upper bool) DragValueU64Fluid {
	panic("stub")
}

func (inst DragValueU64Fluid) UpdateWhileEditing(update bool) DragValueU64Fluid { panic("stub") }

func (inst DragValueU64Fluid) Send() { panic("stub") }

func (inst DragValueU64Fluid) Keep() typed.RetainedFffiHolderTyped[DragValueS] { panic("stub") }

func (inst FrameFluid) InnerMargin(val float32) FrameFluid { panic("stub") }

func (inst FrameFluid) CornerRadius(val float32) FrameFluid { panic("stub") }

func (inst FrameFluid) OuterMargin(val float32) FrameFluid { panic("stub") }

func (inst FrameFluid) MultiplyWithOpacity(val float32) FrameFluid { panic("stub") }

func (inst FrameFluid) Send() { panic("stub") }

func (inst FrameFluid) Keep() typed.RetainedFffiHolderTyped[BlockI] { panic("stub") }

func (inst FrameFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst GridFluid) NumColumns(val uint32) GridFluid { panic("stub") }

func (inst GridFluid) Striped(val bool) GridFluid { panic("stub") }

func (inst GridFluid) MinColWidth(val float32) GridFluid { panic("stub") }

func (inst GridFluid) MinRowHeight(val float32) GridFluid { panic("stub") }

func (inst GridFluid) MaxColWidth(val float32) GridFluid { panic("stub") }

func (inst GridFluid) StartRow(val uint64) GridFluid { panic("stub") }

func (inst GridFluid) Send() { panic("stub") }

func (inst GridFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst HorizontalFluid) Send() { panic("stub") }

func (inst HorizontalFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst HorizontalCenteredFluid) Send() { panic("stub") }

func (inst HorizontalCenteredFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] {
	panic("stub")
}

func (inst HorizontalTopFluid) Send() { panic("stub") }

func (inst HorizontalTopFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst HorizontalWrappedFluid) Send() { panic("stub") }

func (inst HorizontalWrappedFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] {
	panic("stub")
}

func (inst LabelFluid) Selectable(val bool) LabelFluid { panic("stub") }

func (inst LabelFluid) Wrap() LabelFluid { panic("stub") }

func (inst LabelFluid) Truncate() LabelFluid { panic("stub") }

func (inst LabelFluid) Extend() LabelFluid { panic("stub") }

func (inst LabelFluid) Send() { panic("stub") }

func (inst LabelFluid) Keep() typed.RetainedFffiHolderTyped[LabelS] { panic("stub") }

func (inst LabelAtomsFluid) Send() { panic("stub") }

func (inst LabelAtomsFluid) Keep() typed.RetainedFffiHolderTyped[LabelS] { panic("stub") }

func (inst LabelWidgetTextFluid) Send() { panic("stub") }

func (inst LabelWidgetTextFluid) Keep() typed.RetainedFffiHolderTyped[LabelS] { panic("stub") }

func (inst MenuBarFluid) Send() { panic("stub") }

func (inst MenuBarFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst MenuButtonFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst NodeDirFluid) Send() { panic("stub") }

func (inst NodeDirFluid) Keep() typed.RetainedFffiHolderTyped[NodeCommandS] { panic("stub") }

func (inst NodeDirCloseFluid) Send() { panic("stub") }

func (inst NodeDirCloseFluid) Keep() typed.RetainedFffiHolderTyped[NodeCommandS] { panic("stub") }

func (inst NodeLeafFluid) Send() { panic("stub") }

func (inst NodeLeafFluid) Keep() typed.RetainedFffiHolderTyped[NodeCommandS] { panic("stub") }

func (inst PanelBottomFluid) Resizable(val bool) PanelBottomFluid { panic("stub") }

func (inst PanelBottomFluid) DefaultHeight(val float32) PanelBottomFluid { panic("stub") }

func (inst PanelBottomFluid) ExactHeight(val float32) PanelBottomFluid { panic("stub") }

func (inst PanelBottomFluid) Send() { panic("stub") }

func (inst PanelBottomFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelBottomInsideFluid) Resizable(val bool) PanelBottomInsideFluid { panic("stub") }

func (inst PanelBottomInsideFluid) DefaultHeight(val float32) PanelBottomInsideFluid { panic("stub") }

func (inst PanelBottomInsideFluid) ExactHeight(val float32) PanelBottomInsideFluid { panic("stub") }

func (inst PanelBottomInsideFluid) Send() { panic("stub") }

func (inst PanelBottomInsideFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] {
	panic("stub")
}

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelLeftFluid) Resizable(val bool) PanelLeftFluid { panic("stub") }

func (inst PanelLeftFluid) DefaultWidth(val float32) PanelLeftFluid { panic("stub") }

func (inst PanelLeftFluid) ExactWidth(val float32) PanelLeftFluid { panic("stub") }

func (inst PanelLeftFluid) Send() { panic("stub") }

func (inst PanelLeftFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelLeftInsideFluid) Resizable(val bool) PanelLeftInsideFluid { panic("stub") }

func (inst PanelLeftInsideFluid) DefaultWidth(val float32) PanelLeftInsideFluid { panic("stub") }

func (inst PanelLeftInsideFluid) ExactWidth(val float32) PanelLeftInsideFluid { panic("stub") }

func (inst PanelLeftInsideFluid) Send() { panic("stub") }

func (inst PanelLeftInsideFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelRightFluid) Resizable(val bool) PanelRightFluid { panic("stub") }

func (inst PanelRightFluid) DefaultWidth(val float32) PanelRightFluid { panic("stub") }

func (inst PanelRightFluid) ExactWidth(val float32) PanelRightFluid { panic("stub") }

func (inst PanelRightFluid) Send() { panic("stub") }

func (inst PanelRightFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelRightInsideFluid) Resizable(val bool) PanelRightInsideFluid { panic("stub") }

func (inst PanelRightInsideFluid) DefaultWidth(val float32) PanelRightInsideFluid { panic("stub") }

func (inst PanelRightInsideFluid) ExactWidth(val float32) PanelRightInsideFluid { panic("stub") }

func (inst PanelRightInsideFluid) Send() { panic("stub") }

func (inst PanelRightInsideFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelTopFluid) Resizable(val bool) PanelTopFluid { panic("stub") }

func (inst PanelTopFluid) DefaultHeight(val float32) PanelTopFluid { panic("stub") }

func (inst PanelTopFluid) ExactHeight(val float32) PanelTopFluid { panic("stub") }

func (inst PanelTopFluid) Send() { panic("stub") }

func (inst PanelTopFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PanelTopInsideFluid) Resizable(val bool) PanelTopInsideFluid { panic("stub") }

func (inst PanelTopInsideFluid) DefaultHeight(val float32) PanelTopInsideFluid { panic("stub") }

func (inst PanelTopInsideFluid) ExactHeight(val float32) PanelTopInsideFluid { panic("stub") }

func (inst PanelTopInsideFluid) Send() { panic("stub") }

func (inst PanelTopInsideFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/

func (inst PushRichTextFluid) Size(size float32) PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) ExtraLetterSpacing(spacing float32) PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) LineHeight(height float32) PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) LineHeightDefault() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Heading() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Monospace() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Code() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Strong() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Weak() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Underline() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Strikethrough() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Italics() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Small() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) SmallRaised() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Raised() PushRichTextFluid { panic("stub") }

func (inst PushRichTextFluid) Send() { panic("stub") }

func (inst PushRichTextFluid) Keep() typed.RetainedFffiHolderTyped[AtomsS] { panic("stub") }

func (inst PushRichTextColoredFluid) Size(size float32) PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) ExtraLetterSpacing(spacing float32) PushRichTextColoredFluid {
	panic("stub")
}

func (inst PushRichTextColoredFluid) LineHeight(height float32) PushRichTextColoredFluid {
	panic("stub")
}

func (inst PushRichTextColoredFluid) LineHeightDefault() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Heading() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Monospace() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Code() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Strong() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Weak() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Underline() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Strikethrough() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Italics() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Small() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) SmallRaised() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Raised() PushRichTextColoredFluid { panic("stub") }

func (inst PushRichTextColoredFluid) Send() { panic("stub") }

func (inst PushRichTextColoredFluid) Keep() typed.RetainedFffiHolderTyped[AtomsS] { panic("stub") }

func (inst RadioButtonFluid) Send() { panic("stub") }

func (inst ScalarSizeFluid) AvailableWidth() ScalarSizeFluid { panic("stub") }

func (inst ScalarSizeFluid) AvailableHeight() ScalarSizeFluid { panic("stub") }

func (inst ScalarSizeFluid) Keep() typed.RetainedFffiHolderTyped[ScalarSizeS] { panic("stub") }

func (inst ScrollAreaFluid) Hscroll(val bool) ScrollAreaFluid { panic("stub") }

func (inst ScrollAreaFluid) Vscroll(val bool) ScrollAreaFluid { panic("stub") }

func (inst ScrollAreaFluid) Animated(val bool) ScrollAreaFluid { panic("stub") }

func (inst ScrollAreaFluid) Send() { panic("stub") }

func (inst ScrollAreaFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst SeparatorFluid) Horizontal() SeparatorFluid { panic("stub") }

func (inst SeparatorFluid) Vertical() SeparatorFluid { panic("stub") }

func (inst SeparatorFluid) Spacing(spacing float32) SeparatorFluid { panic("stub") }

func (inst SeparatorFluid) Grow(extra float32) SeparatorFluid { panic("stub") }

func (inst SeparatorFluid) Shrink(shrink float32) SeparatorFluid { panic("stub") }

func (inst SeparatorFluid) Send() { panic("stub") }

func (inst SliderF64Fluid) ShowValue(enabled bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Prefix(prefix string) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Suffix(suffix string) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Text(text string) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Vertical() SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Logarithmic(enabled bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) SmallestPositive(smallestNum float64) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) LargestFinite(largestNum float64) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) SmartAim(enabled bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) DragValueSpeed(speed float64) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) MinDecimals(digits uint32) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) MaxDecimals(digits uint32) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) FixedDecimals(digits uint32) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) TrailingFill(enabled bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Binary(minWidth uint32, twosComplement bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Octal(minWidth uint32, twosComplement bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Hexadecimal(minWidth uint32, twosComplement bool, upper bool) SliderF64Fluid {
	panic("stub")
}

func (inst SliderF64Fluid) Integer() SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) UpdateWhileEditing(update bool) SliderF64Fluid { panic("stub") }

func (inst SliderF64Fluid) Send() { panic("stub") }

func (inst SliderF64Fluid) Keep() typed.RetainedFffiHolderTyped[SliderS] { panic("stub") }

func (inst SliderI64Fluid) ShowValue(enabled bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Prefix(prefix string) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Suffix(suffix string) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Text(text string) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Vertical() SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Logarithmic(enabled bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) SmallestPositive(smallestNum float64) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) LargestFinite(largestNum float64) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) SmartAim(enabled bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) DragValueSpeed(speed float64) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) MinDecimals(digits uint32) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) MaxDecimals(digits uint32) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) FixedDecimals(digits uint32) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) TrailingFill(enabled bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Binary(minWidth uint32, twosComplement bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Octal(minWidth uint32, twosComplement bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Hexadecimal(minWidth uint32, twosComplement bool, upper bool) SliderI64Fluid {
	panic("stub")
}

func (inst SliderI64Fluid) Integer() SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) UpdateWhileEditing(update bool) SliderI64Fluid { panic("stub") }

func (inst SliderI64Fluid) Send() { panic("stub") }

func (inst SliderI64Fluid) Keep() typed.RetainedFffiHolderTyped[SliderS] { panic("stub") }

func (inst SliderU64Fluid) ShowValue(enabled bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Prefix(prefix string) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Suffix(suffix string) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Text(text string) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Vertical() SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Logarithmic(enabled bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) SmallestPositive(smallestNum float64) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) LargestFinite(largestNum float64) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) SmartAim(enabled bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) DragValueSpeed(speed float64) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) MinDecimals(digits uint32) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) MaxDecimals(digits uint32) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) FixedDecimals(digits uint32) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) TrailingFill(enabled bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Binary(minWidth uint32, twosComplement bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Octal(minWidth uint32, twosComplement bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Hexadecimal(minWidth uint32, twosComplement bool, upper bool) SliderU64Fluid {
	panic("stub")
}

func (inst SliderU64Fluid) Integer() SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) UpdateWhileEditing(update bool) SliderU64Fluid { panic("stub") }

func (inst SliderU64Fluid) Send() { panic("stub") }

func (inst SliderU64Fluid) Keep() typed.RetainedFffiHolderTyped[SliderS] { panic("stub") }

func (inst SpinnerFluid) Size(size float32) SpinnerFluid { panic("stub") }

func (inst SpinnerFluid) Send() { panic("stub") }

func (inst TextEditFluid) CodeEditor() TextEditFluid { panic("stub") }

func (inst TextEditFluid) Frame(frame bool) TextEditFluid { panic("stub") }

func (inst TextEditFluid) HintText(hint string) TextEditFluid { panic("stub") }

func (inst TextEditFluid) Password(password bool) TextEditFluid { panic("stub") }

func (inst TextEditFluid) Interactive(interactive bool) TextEditFluid { panic("stub") }

func (inst TextEditFluid) DesiredWidth(width float32) TextEditFluid { panic("stub") }

func (inst TextEditFluid) DesiredRows(rows uint32) TextEditFluid { panic("stub") }

func (inst TextEditFluid) LockFocus(lock bool) TextEditFluid { panic("stub") }

func (inst TextEditFluid) CursorAtEnd(b bool) TextEditFluid { panic("stub") }

func (inst TextEditFluid) ClipText(b bool) TextEditFluid { panic("stub") }

func (inst TextEditFluid) CharLimit(chars uint32) TextEditFluid { panic("stub") }

func (inst TextEditFluid) Send() { panic("stub") }

func (inst TreeFluid) Send() { panic("stub") }

func (inst UiWithLayoutFluid) MainDirLeftToRight() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) MainDirRightToLeft() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) MainDirTopDown() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) MainDirBottomUp() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) MainWrap(wrap bool) UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) MainJustify(justify bool) UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) CrossAlignMin() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) CrossAlignCenter() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) CrossAlignMax() UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) CrossJustify(justify bool) UiWithLayoutFluid { panic("stub") }

func (inst UiWithLayoutFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst VectorSizeFluid) AvailableSize() VectorSizeFluid { panic("stub") }

func (inst VectorSizeFluid) Keep() typed.RetainedFffiHolderTyped[ScalarSizeS] { panic("stub") }

func (inst VerticalFluid) Send() { panic("stub") }

func (inst VerticalFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst VerticalCenteredFluid) Send() { panic("stub") }

func (inst VerticalCenteredFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

func (inst VerticalCenteredJustifiedFluid) Send() { panic("stub") }

func (inst VerticalCenteredJustifiedFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] {
	panic("stub")
}

func (inst WidgetTextFluid) Text(val string) WidgetTextFluid { panic("stub") }

func (inst WidgetTextFluid) Keep() typed.RetainedFffiHolderTyped[WidgetTextS] { panic("stub") }

func (inst WindowFluid) DefaultOpen(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) Enabled(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) Interactable(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) Movable(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) Resizable(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) Collapsible(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) TitleBar(val bool) WindowFluid { panic("stub") }

func (inst WindowFluid) Send() { panic("stub") }

func (inst WindowFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] { panic("stub") }

/*if inst.idGen.DeriveStacked() != inst.id {
	panic("id handling is incorrect. iterators are nested in an unhandled way.")
}*/


```

--- FILE: types.out.go ---
```go
// Code generated; TheStack (github.com/stergiotis/pebble2impl/public/thestack/cmd/imzero2) DO NOT EDIT.

package components

import _ "github.com/stergiotis/pebble2impl/public/thestack/fffi2/typed"

type AtomsFluid struct {
}
type AtomsMethodIdE uint32

type ButtonFluid struct {
}
type ButtonMethodIdE uint32

type CheckboxFluid struct {
}
type CheckboxMethodIdE uint32

type CollapsingHeaderFluid struct {
}
type CollapsingHeaderMethodIdE uint32

type ColorFluid struct {
}
type ColorMethodIdE uint32

type ComboBoxFluid struct {
}
type ComboBoxMethodIdE uint32

type DragValueF64Fluid struct {
}
type DragValueF64MethodIdE uint32

type DragValueI64Fluid struct {
}
type DragValueI64MethodIdE uint32

type DragValueU64Fluid struct {
}
type DragValueU64MethodIdE uint32

type FrameFluid struct {
}
type FrameMethodIdE uint32

type GridFluid struct {
}
type GridMethodIdE uint32

type HorizontalFluid struct {
}
type HorizontalMethodIdE uint32

type HorizontalCenteredFluid struct {
}
type HorizontalCenteredMethodIdE uint32

type HorizontalTopFluid struct {
}
type HorizontalTopMethodIdE uint32

type HorizontalWrappedFluid struct {
}
type HorizontalWrappedMethodIdE uint32

type LabelFluid struct {
}
type LabelMethodIdE uint32

type LabelAtomsFluid struct {
}
type LabelAtomsMethodIdE uint32

type LabelWidgetTextFluid struct {
}
type LabelWidgetTextMethodIdE uint32

type MenuBarFluid struct {
}
type MenuBarMethodIdE uint32

type MenuButtonFluid struct {
}
type MenuButtonMethodIdE uint32

type NodeDirFluid struct {
}
type NodeDirMethodIdE uint32

type NodeDirCloseFluid struct {
}
type NodeDirCloseMethodIdE uint32

type NodeLeafFluid struct {
}
type NodeLeafMethodIdE uint32

type PanelBottomFluid struct {
}
type PanelBottomMethodIdE uint32

type PanelBottomInsideFluid struct {
}
type PanelBottomInsideMethodIdE uint32

type PanelLeftFluid struct {
}
type PanelLeftMethodIdE uint32

type PanelLeftInsideFluid struct {
}
type PanelLeftInsideMethodIdE uint32

type PanelRightFluid struct {
}
type PanelRightMethodIdE uint32

type PanelRightInsideFluid struct {
}
type PanelRightInsideMethodIdE uint32

type PanelTopFluid struct {
}
type PanelTopMethodIdE uint32

type PanelTopInsideFluid struct {
}
type PanelTopInsideMethodIdE uint32

type PushRichTextFluid struct {
}
type PushRichTextMethodIdE uint32

type PushRichTextColoredFluid struct {
}
type PushRichTextColoredMethodIdE uint32

type RadioButtonFluid struct {
}
type RadioButtonMethodIdE uint32

type ScalarSizeFluid struct {
}
type ScalarSizeMethodIdE uint32

type ScrollAreaFluid struct {
}
type ScrollAreaMethodIdE uint32

type SeparatorFluid struct {
}
type SeparatorMethodIdE uint32

type SliderF64Fluid struct {
}
type SliderF64MethodIdE uint32

type SliderI64Fluid struct {
}
type SliderI64MethodIdE uint32

type SliderU64Fluid struct {
}
type SliderU64MethodIdE uint32

type SpinnerFluid struct {
}
type SpinnerMethodIdE uint32

type TextEditFluid struct {
}
type TextEditMethodIdE uint32

type TreeFluid struct {
}
type TreeMethodIdE uint32

type UiWithLayoutFluid struct {
}
type UiWithLayoutMethodIdE uint32

type VectorSizeFluid struct {
}
type VectorSizeMethodIdE uint32

type VerticalFluid struct {
}
type VerticalMethodIdE uint32

type VerticalCenteredFluid struct {
}
type VerticalCenteredMethodIdE uint32

type VerticalCenteredJustifiedFluid struct {
}
type VerticalCenteredJustifiedMethodIdE uint32

type WidgetTextFluid struct {
}
type WidgetTextMethodIdE uint32

type WindowFluid struct {
}
type WindowMethodIdE uint32

type AtomsS struct{}

type ButtonS struct{}

func (inst ButtonS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type CheckboxS struct{}

func (inst CheckboxS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type Color32S struct{}

type DragValueS struct{}

func (inst DragValueS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type LabelS struct{}

func (inst LabelS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type NodeCommandS struct{}

type ScalarSizeS struct{}

type SliderS struct{}

func (inst SliderS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type SpinnerS struct{}

func (inst SpinnerS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type TextEditS struct{}

func (inst TextEditS) DummyInterfaceImplementationMethodWidgetI() { panic("stub") }

type WidgetTextS struct{}

type BlockI interface {
	DummyInterfaceImplementationMethodBlockI()
}

type WidgetI interface {
	DummyInterfaceImplementationMethodWidgetI()
}


```
