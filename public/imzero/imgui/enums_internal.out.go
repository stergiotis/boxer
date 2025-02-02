package imgui

type ImGuiDataTypePrivate int

const (
	ImGuiDataType_String  = ImGuiDataTypePrivate(ImGuiDataType_COUNT + 1)
	ImGuiDataType_Pointer = iota
	ImGuiDataType_ID      = iota
)

type ImGuiItemFlagsPrivate int

const (
	ImGuiItemFlags_Disabled               = ImGuiItemFlagsPrivate(1 << 10)
	ImGuiItemFlags_ReadOnly               = ImGuiItemFlagsPrivate(1 << 11)
	ImGuiItemFlags_MixedValue             = ImGuiItemFlagsPrivate(1 << 12)
	ImGuiItemFlags_NoWindowHoverableCheck = ImGuiItemFlagsPrivate(1 << 13)
	ImGuiItemFlags_AllowOverlap           = ImGuiItemFlagsPrivate(1 << 14)
	ImGuiItemFlags_Inputable              = ImGuiItemFlagsPrivate(1 << 20)
	ImGuiItemFlags_HasSelectionUserData   = ImGuiItemFlagsPrivate(1 << 21)
	ImGuiItemFlags_IsMultiSelect          = ImGuiItemFlagsPrivate(1 << 22)
	ImGuiItemFlags_Default_               = ImGuiItemFlagsPrivate(ImGuiItemFlags_AutoClosePopups)
)

type ImGuiItemStatusFlags int

const (
	ImGuiItemStatusFlags_None             = ImGuiItemStatusFlags(0)
	ImGuiItemStatusFlags_HoveredRect      = ImGuiItemStatusFlags(1 << 0)
	ImGuiItemStatusFlags_HasDisplayRect   = ImGuiItemStatusFlags(1 << 1)
	ImGuiItemStatusFlags_Edited           = ImGuiItemStatusFlags(1 << 2)
	ImGuiItemStatusFlags_ToggledSelection = ImGuiItemStatusFlags(1 << 3)
	ImGuiItemStatusFlags_ToggledOpen      = ImGuiItemStatusFlags(1 << 4)
	ImGuiItemStatusFlags_HasDeactivated   = ImGuiItemStatusFlags(1 << 5)
	ImGuiItemStatusFlags_Deactivated      = ImGuiItemStatusFlags(1 << 6)
	ImGuiItemStatusFlags_HoveredWindow    = ImGuiItemStatusFlags(1 << 7)
	ImGuiItemStatusFlags_Visible          = ImGuiItemStatusFlags(1 << 8)
	ImGuiItemStatusFlags_HasClipRect      = ImGuiItemStatusFlags(1 << 9)
	ImGuiItemStatusFlags_HasShortcut      = ImGuiItemStatusFlags(1 << 10)
)

type ImGuiHoveredFlagsPrivate = ImGuiHoveredFlags

const (
	ImGuiHoveredFlags_DelayMask_                    = ImGuiHoveredFlagsPrivate(ImGuiHoveredFlags_DelayNone | ImGuiHoveredFlags_DelayShort | ImGuiHoveredFlags_DelayNormal | ImGuiHoveredFlags_NoSharedDelay)
	ImGuiHoveredFlags_AllowedMaskForIsWindowHovered = ImGuiHoveredFlagsPrivate(ImGuiHoveredFlags_ChildWindows | ImGuiHoveredFlags_RootWindow | ImGuiHoveredFlags_AnyWindow | ImGuiHoveredFlags_NoPopupHierarchy | ImGuiHoveredFlags_DockHierarchy | ImGuiHoveredFlags_AllowWhenBlockedByPopup | ImGuiHoveredFlags_AllowWhenBlockedByActiveItem | ImGuiHoveredFlags_ForTooltip | ImGuiHoveredFlags_Stationary)
	ImGuiHoveredFlags_AllowedMaskForIsItemHovered   = ImGuiHoveredFlagsPrivate(ImGuiHoveredFlags_AllowWhenBlockedByPopup | ImGuiHoveredFlags_AllowWhenBlockedByActiveItem | ImGuiHoveredFlags_AllowWhenOverlapped | ImGuiHoveredFlags_AllowWhenDisabled | ImGuiHoveredFlags_NoNavOverride | ImGuiHoveredFlags_ForTooltip | ImGuiHoveredFlags_Stationary | ImGuiHoveredFlags_DelayMask_)
)

type ImGuiInputTextFlagsPrivate int

const (
	ImGuiInputTextFlags_Multiline            = ImGuiInputTextFlagsPrivate(1 << 26)
	ImGuiInputTextFlags_NoMarkEdited         = ImGuiInputTextFlagsPrivate(1 << 27)
	ImGuiInputTextFlags_MergedItem           = ImGuiInputTextFlagsPrivate(1 << 28)
	ImGuiInputTextFlags_LocalizeDecimalPoint = ImGuiInputTextFlagsPrivate(1 << 29)
)

type ImGuiButtonFlagsPrivate int

const (
	ImGuiButtonFlags_PressedOnClick                = ImGuiButtonFlagsPrivate(1 << 4)
	ImGuiButtonFlags_PressedOnClickRelease         = ImGuiButtonFlagsPrivate(1 << 5)
	ImGuiButtonFlags_PressedOnClickReleaseAnywhere = ImGuiButtonFlagsPrivate(1 << 6)
	ImGuiButtonFlags_PressedOnRelease              = ImGuiButtonFlagsPrivate(1 << 7)
	ImGuiButtonFlags_PressedOnDoubleClick          = ImGuiButtonFlagsPrivate(1 << 8)
	ImGuiButtonFlags_PressedOnDragDropHold         = ImGuiButtonFlagsPrivate(1 << 9)
	ImGuiButtonFlags_Repeat                        = ImGuiButtonFlagsPrivate(1 << 10)
	ImGuiButtonFlags_FlattenChildren               = ImGuiButtonFlagsPrivate(1 << 11)
	ImGuiButtonFlags_AllowOverlap                  = ImGuiButtonFlagsPrivate(1 << 12)
	ImGuiButtonFlags_DontClosePopups               = ImGuiButtonFlagsPrivate(1 << 13)
	ImGuiButtonFlags_AlignTextBaseLine             = ImGuiButtonFlagsPrivate(1 << 15)
	ImGuiButtonFlags_NoKeyModifiers                = ImGuiButtonFlagsPrivate(1 << 16)
	ImGuiButtonFlags_NoHoldingActiveId             = ImGuiButtonFlagsPrivate(1 << 17)
	ImGuiButtonFlags_NoNavFocus                    = ImGuiButtonFlagsPrivate(1 << 18)
	ImGuiButtonFlags_NoHoveredOnFocus              = ImGuiButtonFlagsPrivate(1 << 19)
	ImGuiButtonFlags_NoSetKeyOwner                 = ImGuiButtonFlagsPrivate(1 << 20)
	ImGuiButtonFlags_NoTestKeyOwner                = ImGuiButtonFlagsPrivate(1 << 21)
	ImGuiButtonFlags_PressedOnMask_                = ImGuiButtonFlagsPrivate(ImGuiButtonFlags_PressedOnClick | ImGuiButtonFlags_PressedOnClickRelease | ImGuiButtonFlags_PressedOnClickReleaseAnywhere | ImGuiButtonFlags_PressedOnRelease | ImGuiButtonFlags_PressedOnDoubleClick | ImGuiButtonFlags_PressedOnDragDropHold)
	ImGuiButtonFlags_PressedOnDefault_             = ImGuiButtonFlagsPrivate(ImGuiButtonFlags_PressedOnClickRelease)
)

type ImGuiComboFlagsPrivate int

const (
	ImGuiComboFlags_CustomPreview = ImGuiComboFlagsPrivate(1 << 20)
)

type ImGuiSliderFlagsPrivate int

const (
	ImGuiSliderFlags_Vertical = ImGuiSliderFlagsPrivate(1 << 20)
	ImGuiSliderFlags_ReadOnly = ImGuiSliderFlagsPrivate(1 << 21)
)

type ImGuiSelectableFlagsPrivate int

const (
	ImGuiSelectableFlags_NoHoldingActiveID    = ImGuiSelectableFlagsPrivate(1 << 20)
	ImGuiSelectableFlags_SelectOnNav          = ImGuiSelectableFlagsPrivate(1 << 21)
	ImGuiSelectableFlags_SelectOnClick        = ImGuiSelectableFlagsPrivate(1 << 22)
	ImGuiSelectableFlags_SelectOnRelease      = ImGuiSelectableFlagsPrivate(1 << 23)
	ImGuiSelectableFlags_SpanAvailWidth       = ImGuiSelectableFlagsPrivate(1 << 24)
	ImGuiSelectableFlags_SetNavIdOnHover      = ImGuiSelectableFlagsPrivate(1 << 25)
	ImGuiSelectableFlags_NoPadWithHalfSpacing = ImGuiSelectableFlagsPrivate(1 << 26)
	ImGuiSelectableFlags_NoSetKeyOwner        = ImGuiSelectableFlagsPrivate(1 << 27)
)

type ImGuiTreeNodeFlagsPrivate int

const (
	ImGuiTreeNodeFlags_ClipLabelForTrailingButton = ImGuiTreeNodeFlagsPrivate(1 << 28)
	ImGuiTreeNodeFlags_UpsideDownArrow            = ImGuiTreeNodeFlagsPrivate(1 << 29)
	ImGuiTreeNodeFlags_OpenOnMask_                = ImGuiTreeNodeFlagsPrivate(ImGuiTreeNodeFlags_OpenOnDoubleClick | ImGuiTreeNodeFlags_OpenOnArrow)
)

type ImGuiSeparatorFlags int

const (
	ImGuiSeparatorFlags_None           = ImGuiSeparatorFlags(0)
	ImGuiSeparatorFlags_Horizontal     = ImGuiSeparatorFlags(1 << 0)
	ImGuiSeparatorFlags_Vertical       = ImGuiSeparatorFlags(1 << 1)
	ImGuiSeparatorFlags_SpanAllColumns = ImGuiSeparatorFlags(1 << 2)
)

type ImGuiFocusRequestFlags int

const (
	ImGuiFocusRequestFlags_None                = ImGuiFocusRequestFlags(0)
	ImGuiFocusRequestFlags_RestoreFocusedChild = ImGuiFocusRequestFlags(1 << 0)
	ImGuiFocusRequestFlags_UnlessBelowModal    = ImGuiFocusRequestFlags(1 << 1)
)

type ImGuiTextFlags int

const (
	ImGuiTextFlags_None                       = ImGuiTextFlags(0)
	ImGuiTextFlags_NoWidthForLargeClippedText = ImGuiTextFlags(1 << 0)
)

type ImGuiTooltipFlags int

const (
	ImGuiTooltipFlags_None             = ImGuiTooltipFlags(0)
	ImGuiTooltipFlags_OverridePrevious = ImGuiTooltipFlags(1 << 1)
)

type ImGuiLayoutType int

const (
	ImGuiLayoutType_Horizontal = ImGuiLayoutType(0)
	ImGuiLayoutType_Vertical   = ImGuiLayoutType(1)
)

type ImGuiLogType int

const (
	ImGuiLogType_None      = ImGuiLogType(0)
	ImGuiLogType_TTY       = iota
	ImGuiLogType_File      = iota
	ImGuiLogType_Buffer    = iota
	ImGuiLogType_Clipboard = iota
)

type ImGuiAxis int

const (
	ImGuiAxis_None = ImGuiAxis(-1)
	ImGuiAxis_X    = ImGuiAxis(0)
	ImGuiAxis_Y    = ImGuiAxis(1)
)

type ImGuiPlotType int

const (
	ImGuiPlotType_Lines     = iota
	ImGuiPlotType_Histogram = iota
)

type ImGuiWindowRefreshFlags int

const (
	ImGuiWindowRefreshFlags_None              = ImGuiWindowRefreshFlags(0)
	ImGuiWindowRefreshFlags_TryToAvoidRefresh = ImGuiWindowRefreshFlags(1 << 0)
	ImGuiWindowRefreshFlags_RefreshOnHover    = ImGuiWindowRefreshFlags(1 << 1)
	ImGuiWindowRefreshFlags_RefreshOnFocus    = ImGuiWindowRefreshFlags(1 << 2)
)

type ImGuiNextWindowDataFlags int

const (
	ImGuiNextWindowDataFlags_None              = ImGuiNextWindowDataFlags(0)
	ImGuiNextWindowDataFlags_HasPos            = ImGuiNextWindowDataFlags(1 << 0)
	ImGuiNextWindowDataFlags_HasSize           = ImGuiNextWindowDataFlags(1 << 1)
	ImGuiNextWindowDataFlags_HasContentSize    = ImGuiNextWindowDataFlags(1 << 2)
	ImGuiNextWindowDataFlags_HasCollapsed      = ImGuiNextWindowDataFlags(1 << 3)
	ImGuiNextWindowDataFlags_HasSizeConstraint = ImGuiNextWindowDataFlags(1 << 4)
	ImGuiNextWindowDataFlags_HasFocus          = ImGuiNextWindowDataFlags(1 << 5)
	ImGuiNextWindowDataFlags_HasBgAlpha        = ImGuiNextWindowDataFlags(1 << 6)
	ImGuiNextWindowDataFlags_HasScroll         = ImGuiNextWindowDataFlags(1 << 7)
	ImGuiNextWindowDataFlags_HasChildFlags     = ImGuiNextWindowDataFlags(1 << 8)
	ImGuiNextWindowDataFlags_HasRefreshPolicy  = ImGuiNextWindowDataFlags(1 << 9)
	ImGuiNextWindowDataFlags_HasViewport       = ImGuiNextWindowDataFlags(1 << 10)
	ImGuiNextWindowDataFlags_HasDock           = ImGuiNextWindowDataFlags(1 << 11)
	ImGuiNextWindowDataFlags_HasWindowClass    = ImGuiNextWindowDataFlags(1 << 12)
)

type ImGuiNextItemDataFlags int

const (
	ImGuiNextItemDataFlags_None         = ImGuiNextItemDataFlags(0)
	ImGuiNextItemDataFlags_HasWidth     = ImGuiNextItemDataFlags(1 << 0)
	ImGuiNextItemDataFlags_HasOpen      = ImGuiNextItemDataFlags(1 << 1)
	ImGuiNextItemDataFlags_HasShortcut  = ImGuiNextItemDataFlags(1 << 2)
	ImGuiNextItemDataFlags_HasRefVal    = ImGuiNextItemDataFlags(1 << 3)
	ImGuiNextItemDataFlags_HasStorageID = ImGuiNextItemDataFlags(1 << 4)
)

type ImGuiPopupPositionPolicy int

const (
	ImGuiPopupPositionPolicy_Default  = iota
	ImGuiPopupPositionPolicy_ComboBox = iota
	ImGuiPopupPositionPolicy_Tooltip  = iota
)

type ImGuiInputEventType int

const (
	ImGuiInputEventType_None          = ImGuiInputEventType(0)
	ImGuiInputEventType_MousePos      = iota
	ImGuiInputEventType_MouseWheel    = iota
	ImGuiInputEventType_MouseButton   = iota
	ImGuiInputEventType_MouseViewport = iota
	ImGuiInputEventType_Key           = iota
	ImGuiInputEventType_Text          = iota
	ImGuiInputEventType_Focus         = iota
	ImGuiInputEventType_COUNT         = iota
)

type ImGuiInputSource int

const (
	ImGuiInputSource_None     = ImGuiInputSource(0)
	ImGuiInputSource_Mouse    = iota
	ImGuiInputSource_Keyboard = iota
	ImGuiInputSource_Gamepad  = iota
	ImGuiInputSource_COUNT    = iota
)

type ImGuiInputFlagsPrivate = ImGuiInputFlags

const (
	ImGuiInputFlags_RepeatRateDefault                = ImGuiInputFlagsPrivate(1 << 1)
	ImGuiInputFlags_RepeatRateNavMove                = ImGuiInputFlagsPrivate(1 << 2)
	ImGuiInputFlags_RepeatRateNavTweak               = ImGuiInputFlagsPrivate(1 << 3)
	ImGuiInputFlags_RepeatUntilRelease               = ImGuiInputFlagsPrivate(1 << 4)
	ImGuiInputFlags_RepeatUntilKeyModsChange         = ImGuiInputFlagsPrivate(1 << 5)
	ImGuiInputFlags_RepeatUntilKeyModsChangeFromNone = ImGuiInputFlagsPrivate(1 << 6)
	ImGuiInputFlags_RepeatUntilOtherKeyPress         = ImGuiInputFlagsPrivate(1 << 7)
	ImGuiInputFlags_LockThisFrame                    = ImGuiInputFlagsPrivate(1 << 20)
	ImGuiInputFlags_LockUntilRelease                 = ImGuiInputFlagsPrivate(1 << 21)
	ImGuiInputFlags_CondHovered                      = ImGuiInputFlagsPrivate(1 << 22)
	ImGuiInputFlags_CondActive                       = ImGuiInputFlagsPrivate(1 << 23)
	ImGuiInputFlags_CondDefault_                     = ImGuiInputFlagsPrivate(ImGuiInputFlags_CondHovered | ImGuiInputFlags_CondActive)
	ImGuiInputFlags_RepeatRateMask_                  = ImGuiInputFlagsPrivate(ImGuiInputFlags_RepeatRateDefault | ImGuiInputFlags_RepeatRateNavMove | ImGuiInputFlags_RepeatRateNavTweak)
	ImGuiInputFlags_RepeatUntilMask_                 = ImGuiInputFlagsPrivate(ImGuiInputFlags_RepeatUntilRelease | ImGuiInputFlags_RepeatUntilKeyModsChange | ImGuiInputFlags_RepeatUntilKeyModsChangeFromNone | ImGuiInputFlags_RepeatUntilOtherKeyPress)
	ImGuiInputFlags_RepeatMask_                      = ImGuiInputFlagsPrivate(ImGuiInputFlags_Repeat | ImGuiInputFlags_RepeatRateMask_ | ImGuiInputFlags_RepeatUntilMask_)
	ImGuiInputFlags_CondMask_                        = ImGuiInputFlagsPrivate(ImGuiInputFlags_CondHovered | ImGuiInputFlags_CondActive)
	ImGuiInputFlags_RouteTypeMask_                   = ImGuiInputFlagsPrivate(ImGuiInputFlags_RouteActive | ImGuiInputFlags_RouteFocused | ImGuiInputFlags_RouteGlobal | ImGuiInputFlags_RouteAlways)
	ImGuiInputFlags_RouteOptionsMask_                = ImGuiInputFlagsPrivate(ImGuiInputFlags_RouteOverFocused | ImGuiInputFlags_RouteOverActive | ImGuiInputFlags_RouteUnlessBgFocused | ImGuiInputFlags_RouteFromRootWindow)
	ImGuiInputFlags_SupportedByIsKeyPressed          = ImGuiInputFlagsPrivate(ImGuiInputFlags_RepeatMask_)
	ImGuiInputFlags_SupportedByIsMouseClicked        = ImGuiInputFlagsPrivate(ImGuiInputFlags_Repeat)
	ImGuiInputFlags_SupportedByShortcut              = ImGuiInputFlagsPrivate(ImGuiInputFlags_RepeatMask_ | ImGuiInputFlags_RouteTypeMask_ | ImGuiInputFlags_RouteOptionsMask_)
	ImGuiInputFlags_SupportedBySetNextItemShortcut   = ImGuiInputFlagsPrivate(ImGuiInputFlags_RepeatMask_ | ImGuiInputFlags_RouteTypeMask_ | ImGuiInputFlags_RouteOptionsMask_ | ImGuiInputFlags_Tooltip)
	ImGuiInputFlags_SupportedBySetKeyOwner           = ImGuiInputFlagsPrivate(ImGuiInputFlags_LockThisFrame | ImGuiInputFlags_LockUntilRelease)
	ImGuiInputFlags_SupportedBySetItemKeyOwner       = ImGuiInputFlagsPrivate(ImGuiInputFlags_SupportedBySetKeyOwner | ImGuiInputFlags_CondMask_)
)

type ImGuiActivateFlags int

const (
	ImGuiActivateFlags_None               = ImGuiActivateFlags(0)
	ImGuiActivateFlags_PreferInput        = ImGuiActivateFlags(1 << 0)
	ImGuiActivateFlags_PreferTweak        = ImGuiActivateFlags(1 << 1)
	ImGuiActivateFlags_TryToPreserveState = ImGuiActivateFlags(1 << 2)
	ImGuiActivateFlags_FromTabbing        = ImGuiActivateFlags(1 << 3)
	ImGuiActivateFlags_FromShortcut       = ImGuiActivateFlags(1 << 4)
)

type ImGuiScrollFlags int

const (
	ImGuiScrollFlags_None               = ImGuiScrollFlags(0)
	ImGuiScrollFlags_KeepVisibleEdgeX   = ImGuiScrollFlags(1 << 0)
	ImGuiScrollFlags_KeepVisibleEdgeY   = ImGuiScrollFlags(1 << 1)
	ImGuiScrollFlags_KeepVisibleCenterX = ImGuiScrollFlags(1 << 2)
	ImGuiScrollFlags_KeepVisibleCenterY = ImGuiScrollFlags(1 << 3)
	ImGuiScrollFlags_AlwaysCenterX      = ImGuiScrollFlags(1 << 4)
	ImGuiScrollFlags_AlwaysCenterY      = ImGuiScrollFlags(1 << 5)
	ImGuiScrollFlags_NoScrollParent     = ImGuiScrollFlags(1 << 6)
	ImGuiScrollFlags_MaskX_             = ImGuiScrollFlags(ImGuiScrollFlags_KeepVisibleEdgeX | ImGuiScrollFlags_KeepVisibleCenterX | ImGuiScrollFlags_AlwaysCenterX)
	ImGuiScrollFlags_MaskY_             = ImGuiScrollFlags(ImGuiScrollFlags_KeepVisibleEdgeY | ImGuiScrollFlags_KeepVisibleCenterY | ImGuiScrollFlags_AlwaysCenterY)
)

type ImGuiNavHighlightFlags int

const (
	ImGuiNavHighlightFlags_None       = ImGuiNavHighlightFlags(0)
	ImGuiNavHighlightFlags_Compact    = ImGuiNavHighlightFlags(1 << 1)
	ImGuiNavHighlightFlags_AlwaysDraw = ImGuiNavHighlightFlags(1 << 2)
	ImGuiNavHighlightFlags_NoRounding = ImGuiNavHighlightFlags(1 << 3)
)

type ImGuiNavMoveFlags int

const (
	ImGuiNavMoveFlags_None                = ImGuiNavMoveFlags(0)
	ImGuiNavMoveFlags_LoopX               = ImGuiNavMoveFlags(1 << 0)
	ImGuiNavMoveFlags_LoopY               = ImGuiNavMoveFlags(1 << 1)
	ImGuiNavMoveFlags_WrapX               = ImGuiNavMoveFlags(1 << 2)
	ImGuiNavMoveFlags_WrapY               = ImGuiNavMoveFlags(1 << 3)
	ImGuiNavMoveFlags_WrapMask_           = ImGuiNavMoveFlags(ImGuiNavMoveFlags_LoopX | ImGuiNavMoveFlags_LoopY | ImGuiNavMoveFlags_WrapX | ImGuiNavMoveFlags_WrapY)
	ImGuiNavMoveFlags_AllowCurrentNavId   = ImGuiNavMoveFlags(1 << 4)
	ImGuiNavMoveFlags_AlsoScoreVisibleSet = ImGuiNavMoveFlags(1 << 5)
	ImGuiNavMoveFlags_ScrollToEdgeY       = ImGuiNavMoveFlags(1 << 6)
	ImGuiNavMoveFlags_Forwarded           = ImGuiNavMoveFlags(1 << 7)
	ImGuiNavMoveFlags_DebugNoResult       = ImGuiNavMoveFlags(1 << 8)
	ImGuiNavMoveFlags_FocusApi            = ImGuiNavMoveFlags(1 << 9)
	ImGuiNavMoveFlags_IsTabbing           = ImGuiNavMoveFlags(1 << 10)
	ImGuiNavMoveFlags_IsPageMove          = ImGuiNavMoveFlags(1 << 11)
	ImGuiNavMoveFlags_Activate            = ImGuiNavMoveFlags(1 << 12)
	ImGuiNavMoveFlags_NoSelect            = ImGuiNavMoveFlags(1 << 13)
	ImGuiNavMoveFlags_NoSetNavHighlight   = ImGuiNavMoveFlags(1 << 14)
	ImGuiNavMoveFlags_NoClearActiveId     = ImGuiNavMoveFlags(1 << 15)
)

type ImGuiNavLayer int

const (
	ImGuiNavLayer_Main  = ImGuiNavLayer(0)
	ImGuiNavLayer_Menu  = ImGuiNavLayer(1)
	ImGuiNavLayer_COUNT = iota
)

type ImGuiTypingSelectFlags int

const (
	ImGuiTypingSelectFlags_None                = ImGuiTypingSelectFlags(0)
	ImGuiTypingSelectFlags_AllowBackspace      = ImGuiTypingSelectFlags(1 << 0)
	ImGuiTypingSelectFlags_AllowSingleCharMode = ImGuiTypingSelectFlags(1 << 1)
)

type ImGuiOldColumnFlags int

const (
	ImGuiOldColumnFlags_None                   = ImGuiOldColumnFlags(0)
	ImGuiOldColumnFlags_NoBorder               = ImGuiOldColumnFlags(1 << 0)
	ImGuiOldColumnFlags_NoResize               = ImGuiOldColumnFlags(1 << 1)
	ImGuiOldColumnFlags_NoPreserveWidths       = ImGuiOldColumnFlags(1 << 2)
	ImGuiOldColumnFlags_NoForceWithinWindow    = ImGuiOldColumnFlags(1 << 3)
	ImGuiOldColumnFlags_GrowParentContentsSize = ImGuiOldColumnFlags(1 << 4)
)

type ImGuiDockNodeFlagsPrivate = ImGuiDockNodeFlags

const (
	ImGuiDockNodeFlags_DockSpace                 = ImGuiDockNodeFlagsPrivate(1 << 10)
	ImGuiDockNodeFlags_CentralNode               = ImGuiDockNodeFlagsPrivate(1 << 11)
	ImGuiDockNodeFlags_NoTabBar                  = ImGuiDockNodeFlagsPrivate(1 << 12)
	ImGuiDockNodeFlags_HiddenTabBar              = ImGuiDockNodeFlagsPrivate(1 << 13)
	ImGuiDockNodeFlags_NoWindowMenuButton        = ImGuiDockNodeFlagsPrivate(1 << 14)
	ImGuiDockNodeFlags_NoCloseButton             = ImGuiDockNodeFlagsPrivate(1 << 15)
	ImGuiDockNodeFlags_NoResizeX                 = ImGuiDockNodeFlagsPrivate(1 << 16)
	ImGuiDockNodeFlags_NoResizeY                 = ImGuiDockNodeFlagsPrivate(1 << 17)
	ImGuiDockNodeFlags_DockedWindowsInFocusRoute = ImGuiDockNodeFlagsPrivate(1 << 18)
	ImGuiDockNodeFlags_NoDockingSplitOther       = ImGuiDockNodeFlagsPrivate(1 << 19)
	ImGuiDockNodeFlags_NoDockingOverMe           = ImGuiDockNodeFlagsPrivate(1 << 20)
	ImGuiDockNodeFlags_NoDockingOverOther        = ImGuiDockNodeFlagsPrivate(1 << 21)
	ImGuiDockNodeFlags_NoDockingOverEmpty        = ImGuiDockNodeFlagsPrivate(1 << 22)
	ImGuiDockNodeFlags_NoDocking                 = ImGuiDockNodeFlagsPrivate(ImGuiDockNodeFlags_NoDockingOverMe | ImGuiDockNodeFlags_NoDockingOverOther | ImGuiDockNodeFlags_NoDockingOverEmpty | ImGuiDockNodeFlags_NoDockingSplit | ImGuiDockNodeFlags_NoDockingSplitOther)
	ImGuiDockNodeFlags_SharedFlagsInheritMask_   = ImGuiDockNodeFlagsPrivate(^0)
	ImGuiDockNodeFlags_NoResizeFlagsMask_        = ImGuiDockNodeFlagsPrivate(ImGuiDockNodeFlags_NoResize | ImGuiDockNodeFlags_NoResizeX | ImGuiDockNodeFlags_NoResizeY)
	ImGuiDockNodeFlags_LocalFlagsTransferMask_   = ImGuiDockNodeFlagsPrivate(ImGuiDockNodeFlags_NoDockingSplit | ImGuiDockNodeFlags_NoResizeFlagsMask_ | ImGuiDockNodeFlags_AutoHideTabBar | ImGuiDockNodeFlags_CentralNode | ImGuiDockNodeFlags_NoTabBar | ImGuiDockNodeFlags_HiddenTabBar | ImGuiDockNodeFlags_NoWindowMenuButton | ImGuiDockNodeFlags_NoCloseButton)
	ImGuiDockNodeFlags_SavedFlagsMask_           = ImGuiDockNodeFlagsPrivate(ImGuiDockNodeFlags_NoResizeFlagsMask_ | ImGuiDockNodeFlags_DockSpace | ImGuiDockNodeFlags_CentralNode | ImGuiDockNodeFlags_NoTabBar | ImGuiDockNodeFlags_HiddenTabBar | ImGuiDockNodeFlags_NoWindowMenuButton | ImGuiDockNodeFlags_NoCloseButton)
)

type ImGuiDataAuthority int

const (
	ImGuiDataAuthority_Auto     = iota
	ImGuiDataAuthority_DockNode = iota
	ImGuiDataAuthority_Window   = iota
)

type ImGuiDockNodeState int

const (
	ImGuiDockNodeState_Unknown                                   = iota
	ImGuiDockNodeState_HostWindowHiddenBecauseSingleWindow       = iota
	ImGuiDockNodeState_HostWindowHiddenBecauseWindowsAreResizing = iota
	ImGuiDockNodeState_HostWindowVisible                         = iota
)

type ImGuiWindowDockStyleCol int

const (
	ImGuiWindowDockStyleCol_Text                      = iota
	ImGuiWindowDockStyleCol_TabHovered                = iota
	ImGuiWindowDockStyleCol_TabFocused                = iota
	ImGuiWindowDockStyleCol_TabSelected               = iota
	ImGuiWindowDockStyleCol_TabSelectedOverline       = iota
	ImGuiWindowDockStyleCol_TabDimmed                 = iota
	ImGuiWindowDockStyleCol_TabDimmedSelected         = iota
	ImGuiWindowDockStyleCol_TabDimmedSelectedOverline = iota
	ImGuiWindowDockStyleCol_COUNT                     = iota
)

type ImGuiLocKey int

const (
	ImGuiLocKey_VersionStr                    = iota
	ImGuiLocKey_TableSizeOne                  = iota
	ImGuiLocKey_TableSizeAllFit               = iota
	ImGuiLocKey_TableSizeAllDefault           = iota
	ImGuiLocKey_TableResetOrder               = iota
	ImGuiLocKey_WindowingMainMenuBar          = iota
	ImGuiLocKey_WindowingPopup                = iota
	ImGuiLocKey_WindowingUntitled             = iota
	ImGuiLocKey_OpenLink_s                    = iota
	ImGuiLocKey_CopyLink                      = iota
	ImGuiLocKey_DockingHideTabBar             = iota
	ImGuiLocKey_DockingHoldShiftToDock        = iota
	ImGuiLocKey_DockingDragToUndockOrMoveNode = iota
	ImGuiLocKey_COUNT                         = iota
)

type ImGuiDebugLogFlags int

const (
	ImGuiDebugLogFlags_None               = ImGuiDebugLogFlags(0)
	ImGuiDebugLogFlags_EventActiveId      = ImGuiDebugLogFlags(1 << 0)
	ImGuiDebugLogFlags_EventFocus         = ImGuiDebugLogFlags(1 << 1)
	ImGuiDebugLogFlags_EventPopup         = ImGuiDebugLogFlags(1 << 2)
	ImGuiDebugLogFlags_EventNav           = ImGuiDebugLogFlags(1 << 3)
	ImGuiDebugLogFlags_EventClipper       = ImGuiDebugLogFlags(1 << 4)
	ImGuiDebugLogFlags_EventSelection     = ImGuiDebugLogFlags(1 << 5)
	ImGuiDebugLogFlags_EventIO            = ImGuiDebugLogFlags(1 << 6)
	ImGuiDebugLogFlags_EventInputRouting  = ImGuiDebugLogFlags(1 << 7)
	ImGuiDebugLogFlags_EventDocking       = ImGuiDebugLogFlags(1 << 8)
	ImGuiDebugLogFlags_EventViewport      = ImGuiDebugLogFlags(1 << 9)
	ImGuiDebugLogFlags_EventMask_         = ImGuiDebugLogFlags(ImGuiDebugLogFlags_EventActiveId | ImGuiDebugLogFlags_EventFocus | ImGuiDebugLogFlags_EventPopup | ImGuiDebugLogFlags_EventNav | ImGuiDebugLogFlags_EventClipper | ImGuiDebugLogFlags_EventSelection | ImGuiDebugLogFlags_EventIO | ImGuiDebugLogFlags_EventInputRouting | ImGuiDebugLogFlags_EventDocking | ImGuiDebugLogFlags_EventViewport)
	ImGuiDebugLogFlags_OutputToTTY        = ImGuiDebugLogFlags(1 << 20)
	ImGuiDebugLogFlags_OutputToTestEngine = ImGuiDebugLogFlags(1 << 21)
)

type ImGuiContextHookType int

const (
	ImGuiContextHookType_NewFramePre     = iota
	ImGuiContextHookType_NewFramePost    = iota
	ImGuiContextHookType_EndFramePre     = iota
	ImGuiContextHookType_EndFramePost    = iota
	ImGuiContextHookType_RenderPre       = iota
	ImGuiContextHookType_RenderPost      = iota
	ImGuiContextHookType_Shutdown        = iota
	ImGuiContextHookType_PendingRemoval_ = iota
)

type ImGuiTabBarFlagsPrivate int

const (
	ImGuiTabBarFlags_DockNode     = ImGuiTabBarFlagsPrivate(1 << 20)
	ImGuiTabBarFlags_IsFocused    = ImGuiTabBarFlagsPrivate(1 << 21)
	ImGuiTabBarFlags_SaveSettings = ImGuiTabBarFlagsPrivate(1 << 22)
)

type ImGuiTabItemFlagsPrivate int

const (
	ImGuiTabItemFlags_SectionMask_  = ImGuiTabItemFlagsPrivate(ImGuiTabItemFlags_Leading | ImGuiTabItemFlags_Trailing)
	ImGuiTabItemFlags_NoCloseButton = ImGuiTabItemFlagsPrivate(1 << 20)
	ImGuiTabItemFlags_Button        = ImGuiTabItemFlagsPrivate(1 << 21)
	ImGuiTabItemFlags_Unsorted      = ImGuiTabItemFlagsPrivate(1 << 22)
)
