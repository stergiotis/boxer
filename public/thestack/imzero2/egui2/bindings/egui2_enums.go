package bindings

import (
	"iter"
	"math/bits"
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

func (inst ResponseFlagsE) Count() int {
	return bits.OnesCount32(uint32(inst))
}
func (inst ResponseFlagsE) Iterate() iter.Seq[ResponseFlagsE] {
	l := inst.Count()
	if l == 0 {
		return func(yield func(ResponseFlagsE) bool) {
		}
	} else {
		return func(yield func(ResponseFlagsE) bool) {
			i := 0
			for u := ResponseFlagsE(1); ; u = u << 1 {
				if inst&u == u {
					i++
					if !yield(u) || i == l {
						return
					}
				}
			}
		}
	}
}
func (inst ResponseFlagsE) Has(v ResponseFlagsE) bool {
	return inst&v == v
}
func (inst ResponseFlagsE) Set(v ResponseFlagsE) ResponseFlagsE {
	return inst | v
}
func (inst ResponseFlagsE) Clear(v ResponseFlagsE) ResponseFlagsE {
	return inst & (^v)
}

func (inst ResponseFlagsE) HasPrimaryClicked() bool {
	return inst.Has(PrimaryClickedResponseFlags)
}
func (inst ResponseFlagsE) HasSecondaryClicked() bool {
	return inst.Has(SecondaryClickedResponseFlags)
}
func (inst ResponseFlagsE) HasLongTouched() bool {
	return inst.Has(LongTouchedResponseFlags)
}
func (inst ResponseFlagsE) HasDoubleClicked() bool {
	return inst.Has(DoubleClickedResponseFlags)
}
func (inst ResponseFlagsE) HasTripleClicked() bool {
	return inst.Has(TripleClickedResponseFlags)
}
func (inst ResponseFlagsE) HasClickedElsewhere() bool {
	return inst.Has(ClickedElsewhereResponseFlags)
}
func (inst ResponseFlagsE) HasEnabled() bool {
	return inst.Has(EnabledResponseFlags)
}
func (inst ResponseFlagsE) HasMiddleClicked() bool {
	return inst.Has(MiddleClickedResponseFlags)
}
func (inst ResponseFlagsE) HasHovered() bool {
	return inst.Has(HoveredResponseFlags)
}
func (inst ResponseFlagsE) HasContainsPointer() bool {
	return inst.Has(ContainsPointerResponseFlags)
}
func (inst ResponseFlagsE) HasHighlighter() bool {
	return inst.Has(HighlighterResponseFlags)
}
func (inst ResponseFlagsE) HasFocus() bool {
	return inst.Has(HasFocusResponseFlags)
}
func (inst ResponseFlagsE) HasGainedFocus() bool {
	return inst.Has(GainedFocusResponseFlags)
}
func (inst ResponseFlagsE) HasLostFocus() bool {
	return inst.Has(LostFocusResponseFlags)
}
func (inst ResponseFlagsE) HasDragStarted() bool {
	return inst.Has(DragStartedResponseFlags)
}
func (inst ResponseFlagsE) HasDragged() bool {
	return inst.Has(DraggedResponseFlags)
}
func (inst ResponseFlagsE) HasDragStopped() bool {
	return inst.Has(DragStoppedResponseFlags)
}
func (inst ResponseFlagsE) HasIsPointerButtonDown() bool {
	return inst.Has(IsPointerButtonDownResponseFlags)
}
func (inst ResponseFlagsE) HasChanged() bool {
	return inst.Has(ChangedResponseFlags)
}
func (inst ResponseFlagsE) HasShouldClose() bool {
	return inst.Has(ShouldCloseResponseFlags)
}
func (inst ResponseFlagsE) HasIsTooltipOpen() bool {
	return inst.Has(IsTooltipOpenResponseFlags)
}
func (inst ResponseFlagsE) HasNodelikeSelected() bool {
	return inst.Has(NodelikeSelectedFlags)
}
func (inst ResponseFlagsE) HasBlockSkipped() bool {
	return inst.Has(BlockSkippedFlags)
}
func (inst ResponseFlagsE) ClearPrimaryClicked() ResponseFlagsE {
	return inst.Clear(PrimaryClickedResponseFlags)
}
func (inst ResponseFlagsE) ClearSecondaryClicked() ResponseFlagsE {
	return inst.Clear(SecondaryClickedResponseFlags)
}
func (inst ResponseFlagsE) ClearLongTouched() ResponseFlagsE {
	return inst.Clear(LongTouchedResponseFlags)
}
func (inst ResponseFlagsE) ClearDoubleClicked() ResponseFlagsE {
	return inst.Clear(DoubleClickedResponseFlags)
}
func (inst ResponseFlagsE) ClearTripleClicked() ResponseFlagsE {
	return inst.Clear(TripleClickedResponseFlags)
}
func (inst ResponseFlagsE) ClearClickedElsewhere() ResponseFlagsE {
	return inst.Clear(ClickedElsewhereResponseFlags)
}
func (inst ResponseFlagsE) ClearEnabled() ResponseFlagsE {
	return inst.Clear(EnabledResponseFlags)
}
func (inst ResponseFlagsE) ClearMiddleClicked() ResponseFlagsE {
	return inst.Clear(MiddleClickedResponseFlags)
}
func (inst ResponseFlagsE) ClearHovered() ResponseFlagsE {
	return inst.Clear(HoveredResponseFlags)
}
func (inst ResponseFlagsE) ClearContainsPointer() ResponseFlagsE {
	return inst.Clear(ContainsPointerResponseFlags)
}
func (inst ResponseFlagsE) ClearHighlighter() ResponseFlagsE {
	return inst.Clear(HighlighterResponseFlags)
}
func (inst ResponseFlagsE) ClearFocus() ResponseFlagsE {
	return inst.Clear(HasFocusResponseFlags)
}
func (inst ResponseFlagsE) ClearGainedFocus() ResponseFlagsE {
	return inst.Clear(GainedFocusResponseFlags)
}
func (inst ResponseFlagsE) ClearLostFocus() ResponseFlagsE {
	return inst.Clear(LostFocusResponseFlags)
}
func (inst ResponseFlagsE) ClearDragStarted() ResponseFlagsE {
	return inst.Clear(DragStartedResponseFlags)
}
func (inst ResponseFlagsE) ClearDragged() ResponseFlagsE {
	return inst.Clear(DraggedResponseFlags)
}
func (inst ResponseFlagsE) ClearDragStopped() ResponseFlagsE {
	return inst.Clear(DragStoppedResponseFlags)
}
func (inst ResponseFlagsE) ClearIsPointerButtonDown() ResponseFlagsE {
	return inst.Clear(IsPointerButtonDownResponseFlags)
}
func (inst ResponseFlagsE) ClearChanged() ResponseFlagsE {
	return inst.Clear(ChangedResponseFlags)
}
func (inst ResponseFlagsE) ClearShouldClose() ResponseFlagsE {
	return inst.Clear(ShouldCloseResponseFlags)
}
func (inst ResponseFlagsE) ClearIsTooltipOpen() ResponseFlagsE {
	return inst.Clear(IsTooltipOpenResponseFlags)
}
func (inst ResponseFlagsE) ClearNodelikeSelected() ResponseFlagsE {
	return inst.Clear(NodelikeSelectedFlags)
}
func (inst ResponseFlagsE) ClearBlockSkipped() ResponseFlagsE {
	return inst.Clear(BlockSkippedFlags)
}
func (inst ResponseFlagsE) SetPrimaryClicked() ResponseFlagsE {
	return inst.Set(PrimaryClickedResponseFlags)
}
func (inst ResponseFlagsE) SetSecondaryClicked() ResponseFlagsE {
	return inst.Set(SecondaryClickedResponseFlags)
}
func (inst ResponseFlagsE) SetLongTouched() ResponseFlagsE {
	return inst.Set(LongTouchedResponseFlags)
}
func (inst ResponseFlagsE) SetDoubleClicked() ResponseFlagsE {
	return inst.Set(DoubleClickedResponseFlags)
}
func (inst ResponseFlagsE) SetTripleClicked() ResponseFlagsE {
	return inst.Set(TripleClickedResponseFlags)
}
func (inst ResponseFlagsE) SetClickedElsewhere() ResponseFlagsE {
	return inst.Set(ClickedElsewhereResponseFlags)
}
func (inst ResponseFlagsE) SetEnabled() ResponseFlagsE {
	return inst.Set(EnabledResponseFlags)
}
func (inst ResponseFlagsE) SetMiddleClicked() ResponseFlagsE {
	return inst.Set(MiddleClickedResponseFlags)
}
func (inst ResponseFlagsE) SetHovered() ResponseFlagsE {
	return inst.Set(HoveredResponseFlags)
}
func (inst ResponseFlagsE) SetContainsPointer() ResponseFlagsE {
	return inst.Set(ContainsPointerResponseFlags)
}
func (inst ResponseFlagsE) SetHighlighter() ResponseFlagsE {
	return inst.Set(HighlighterResponseFlags)
}
func (inst ResponseFlagsE) SetFocus() ResponseFlagsE {
	return inst.Set(HasFocusResponseFlags)
}
func (inst ResponseFlagsE) SetGainedFocus() ResponseFlagsE {
	return inst.Set(GainedFocusResponseFlags)
}
func (inst ResponseFlagsE) SetLostFocus() ResponseFlagsE {
	return inst.Set(LostFocusResponseFlags)
}
func (inst ResponseFlagsE) SetDragStarted() ResponseFlagsE {
	return inst.Set(DragStartedResponseFlags)
}
func (inst ResponseFlagsE) SetDragged() ResponseFlagsE {
	return inst.Set(DraggedResponseFlags)
}
func (inst ResponseFlagsE) SetDragStopped() ResponseFlagsE {
	return inst.Set(DragStoppedResponseFlags)
}
func (inst ResponseFlagsE) SetIsPointerButtonDown() ResponseFlagsE {
	return inst.Set(IsPointerButtonDownResponseFlags)
}
func (inst ResponseFlagsE) SetChanged() ResponseFlagsE {
	return inst.Set(ChangedResponseFlags)
}
func (inst ResponseFlagsE) SetShouldClose() ResponseFlagsE {
	return inst.Set(ShouldCloseResponseFlags)
}
func (inst ResponseFlagsE) SetIsTooltipOpen() ResponseFlagsE {
	return inst.Set(IsTooltipOpenResponseFlags)
}
func (inst ResponseFlagsE) SetNodelikeSelected() ResponseFlagsE {
	return inst.Set(NodelikeSelectedFlags)
}
func (inst ResponseFlagsE) SetBlockSkipped() ResponseFlagsE {
	return inst.Set(BlockSkippedFlags)
}
