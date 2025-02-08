package imgui

type ImGuiWindowFlags int

const (
	ImGuiWindowFlags_None                      = ImGuiWindowFlags(0)
	ImGuiWindowFlags_NoTitleBar                = ImGuiWindowFlags(1 << 0)
	ImGuiWindowFlags_NoResize                  = ImGuiWindowFlags(1 << 1)
	ImGuiWindowFlags_NoMove                    = ImGuiWindowFlags(1 << 2)
	ImGuiWindowFlags_NoScrollbar               = ImGuiWindowFlags(1 << 3)
	ImGuiWindowFlags_NoScrollWithMouse         = ImGuiWindowFlags(1 << 4)
	ImGuiWindowFlags_NoCollapse                = ImGuiWindowFlags(1 << 5)
	ImGuiWindowFlags_AlwaysAutoResize          = ImGuiWindowFlags(1 << 6)
	ImGuiWindowFlags_NoBackground              = ImGuiWindowFlags(1 << 7)
	ImGuiWindowFlags_NoSavedSettings           = ImGuiWindowFlags(1 << 8)
	ImGuiWindowFlags_NoMouseInputs             = ImGuiWindowFlags(1 << 9)
	ImGuiWindowFlags_MenuBar                   = ImGuiWindowFlags(1 << 10)
	ImGuiWindowFlags_HorizontalScrollbar       = ImGuiWindowFlags(1 << 11)
	ImGuiWindowFlags_NoFocusOnAppearing        = ImGuiWindowFlags(1 << 12)
	ImGuiWindowFlags_NoBringToFrontOnFocus     = ImGuiWindowFlags(1 << 13)
	ImGuiWindowFlags_AlwaysVerticalScrollbar   = ImGuiWindowFlags(1 << 14)
	ImGuiWindowFlags_AlwaysHorizontalScrollbar = ImGuiWindowFlags(1 << 15)
	ImGuiWindowFlags_NoNavInputs               = ImGuiWindowFlags(1 << 16)
	ImGuiWindowFlags_NoNavFocus                = ImGuiWindowFlags(1 << 17)
	ImGuiWindowFlags_UnsavedDocument           = ImGuiWindowFlags(1 << 18)
	ImGuiWindowFlags_NoDocking                 = ImGuiWindowFlags(1 << 19)
	ImGuiWindowFlags_NoNav                     = ImGuiWindowFlags(ImGuiWindowFlags_NoNavInputs | ImGuiWindowFlags_NoNavFocus)
	ImGuiWindowFlags_NoDecoration              = ImGuiWindowFlags(ImGuiWindowFlags_NoTitleBar | ImGuiWindowFlags_NoResize | ImGuiWindowFlags_NoScrollbar | ImGuiWindowFlags_NoCollapse)
	ImGuiWindowFlags_NoInputs                  = ImGuiWindowFlags(ImGuiWindowFlags_NoMouseInputs | ImGuiWindowFlags_NoNavInputs | ImGuiWindowFlags_NoNavFocus)
	ImGuiWindowFlags_DockNodeHost              = ImGuiWindowFlags(1 << 23)
	ImGuiWindowFlags_ChildWindow               = ImGuiWindowFlags(1 << 24)
	ImGuiWindowFlags_Tooltip                   = ImGuiWindowFlags(1 << 25)
	ImGuiWindowFlags_Popup                     = ImGuiWindowFlags(1 << 26)
	ImGuiWindowFlags_Modal                     = ImGuiWindowFlags(1 << 27)
	ImGuiWindowFlags_ChildMenu                 = ImGuiWindowFlags(1 << 28)
	ImGuiWindowFlags_NavFlattened              = ImGuiWindowFlags(1 << 29)
	ImGuiWindowFlags_AlwaysUseWindowPadding    = ImGuiWindowFlags(1 << 30)
)

type ImGuiChildFlags int

const (
	ImGuiChildFlags_None                   = ImGuiChildFlags(0)
	ImGuiChildFlags_Borders                = ImGuiChildFlags(1 << 0)
	ImGuiChildFlags_AlwaysUseWindowPadding = ImGuiChildFlags(1 << 1)
	ImGuiChildFlags_ResizeX                = ImGuiChildFlags(1 << 2)
	ImGuiChildFlags_ResizeY                = ImGuiChildFlags(1 << 3)
	ImGuiChildFlags_AutoResizeX            = ImGuiChildFlags(1 << 4)
	ImGuiChildFlags_AutoResizeY            = ImGuiChildFlags(1 << 5)
	ImGuiChildFlags_AlwaysAutoResize       = ImGuiChildFlags(1 << 6)
	ImGuiChildFlags_FrameStyle             = ImGuiChildFlags(1 << 7)
	ImGuiChildFlags_NavFlattened           = ImGuiChildFlags(1 << 8)
	ImGuiChildFlags_Border                 = ImGuiChildFlags(ImGuiChildFlags_Borders)
)

type ImGuiItemFlags int

const (
	ImGuiItemFlags_None              = ImGuiItemFlags(0)
	ImGuiItemFlags_NoTabStop         = ImGuiItemFlags(1 << 0)
	ImGuiItemFlags_NoNav             = ImGuiItemFlags(1 << 1)
	ImGuiItemFlags_NoNavDefaultFocus = ImGuiItemFlags(1 << 2)
	ImGuiItemFlags_ButtonRepeat      = ImGuiItemFlags(1 << 3)
	ImGuiItemFlags_AutoClosePopups   = ImGuiItemFlags(1 << 4)
	ImGuiItemFlags_AllowDuplicateId  = ImGuiItemFlags(1 << 5)
)

type ImGuiInputTextFlags int

const (
	ImGuiInputTextFlags_None                = ImGuiInputTextFlags(0)
	ImGuiInputTextFlags_CharsDecimal        = ImGuiInputTextFlags(1 << 0)
	ImGuiInputTextFlags_CharsHexadecimal    = ImGuiInputTextFlags(1 << 1)
	ImGuiInputTextFlags_CharsScientific     = ImGuiInputTextFlags(1 << 2)
	ImGuiInputTextFlags_CharsUppercase      = ImGuiInputTextFlags(1 << 3)
	ImGuiInputTextFlags_CharsNoBlank        = ImGuiInputTextFlags(1 << 4)
	ImGuiInputTextFlags_AllowTabInput       = ImGuiInputTextFlags(1 << 5)
	ImGuiInputTextFlags_EnterReturnsTrue    = ImGuiInputTextFlags(1 << 6)
	ImGuiInputTextFlags_EscapeClearsAll     = ImGuiInputTextFlags(1 << 7)
	ImGuiInputTextFlags_CtrlEnterForNewLine = ImGuiInputTextFlags(1 << 8)
	ImGuiInputTextFlags_ReadOnly            = ImGuiInputTextFlags(1 << 9)
	ImGuiInputTextFlags_Password            = ImGuiInputTextFlags(1 << 10)
	ImGuiInputTextFlags_AlwaysOverwrite     = ImGuiInputTextFlags(1 << 11)
	ImGuiInputTextFlags_AutoSelectAll       = ImGuiInputTextFlags(1 << 12)
	ImGuiInputTextFlags_ParseEmptyRefVal    = ImGuiInputTextFlags(1 << 13)
	ImGuiInputTextFlags_DisplayEmptyRefVal  = ImGuiInputTextFlags(1 << 14)
	ImGuiInputTextFlags_NoHorizontalScroll  = ImGuiInputTextFlags(1 << 15)
	ImGuiInputTextFlags_NoUndoRedo          = ImGuiInputTextFlags(1 << 16)
	ImGuiInputTextFlags_ElideLeft           = ImGuiInputTextFlags(1 << 17)
	ImGuiInputTextFlags_CallbackCompletion  = ImGuiInputTextFlags(1 << 18)
	ImGuiInputTextFlags_CallbackHistory     = ImGuiInputTextFlags(1 << 19)
	ImGuiInputTextFlags_CallbackAlways      = ImGuiInputTextFlags(1 << 20)
	ImGuiInputTextFlags_CallbackCharFilter  = ImGuiInputTextFlags(1 << 21)
	ImGuiInputTextFlags_CallbackResize      = ImGuiInputTextFlags(1 << 22)
	ImGuiInputTextFlags_CallbackEdit        = ImGuiInputTextFlags(1 << 23)
)

type ImGuiTreeNodeFlags int

const (
	ImGuiTreeNodeFlags_None                 = ImGuiTreeNodeFlags(0)
	ImGuiTreeNodeFlags_Selected             = ImGuiTreeNodeFlags(1 << 0)
	ImGuiTreeNodeFlags_Framed               = ImGuiTreeNodeFlags(1 << 1)
	ImGuiTreeNodeFlags_AllowOverlap         = ImGuiTreeNodeFlags(1 << 2)
	ImGuiTreeNodeFlags_NoTreePushOnOpen     = ImGuiTreeNodeFlags(1 << 3)
	ImGuiTreeNodeFlags_NoAutoOpenOnLog      = ImGuiTreeNodeFlags(1 << 4)
	ImGuiTreeNodeFlags_DefaultOpen          = ImGuiTreeNodeFlags(1 << 5)
	ImGuiTreeNodeFlags_OpenOnDoubleClick    = ImGuiTreeNodeFlags(1 << 6)
	ImGuiTreeNodeFlags_OpenOnArrow          = ImGuiTreeNodeFlags(1 << 7)
	ImGuiTreeNodeFlags_Leaf                 = ImGuiTreeNodeFlags(1 << 8)
	ImGuiTreeNodeFlags_Bullet               = ImGuiTreeNodeFlags(1 << 9)
	ImGuiTreeNodeFlags_FramePadding         = ImGuiTreeNodeFlags(1 << 10)
	ImGuiTreeNodeFlags_SpanAvailWidth       = ImGuiTreeNodeFlags(1 << 11)
	ImGuiTreeNodeFlags_SpanFullWidth        = ImGuiTreeNodeFlags(1 << 12)
	ImGuiTreeNodeFlags_SpanLabelWidth       = ImGuiTreeNodeFlags(1 << 13)
	ImGuiTreeNodeFlags_SpanAllColumns       = ImGuiTreeNodeFlags(1 << 14)
	ImGuiTreeNodeFlags_LabelSpanAllColumns  = ImGuiTreeNodeFlags(1 << 15)
	ImGuiTreeNodeFlags_NavLeftJumpsBackHere = ImGuiTreeNodeFlags(1 << 17)
	ImGuiTreeNodeFlags_CollapsingHeader     = ImGuiTreeNodeFlags(ImGuiTreeNodeFlags_Framed | ImGuiTreeNodeFlags_NoTreePushOnOpen | ImGuiTreeNodeFlags_NoAutoOpenOnLog)
	ImGuiTreeNodeFlags_AllowItemOverlap     = ImGuiTreeNodeFlags(ImGuiTreeNodeFlags_AllowOverlap)
	ImGuiTreeNodeFlags_SpanTextWidth        = ImGuiTreeNodeFlags(ImGuiTreeNodeFlags_SpanLabelWidth)
)

type ImGuiPopupFlags int

const (
	ImGuiPopupFlags_None                    = ImGuiPopupFlags(0)
	ImGuiPopupFlags_MouseButtonLeft         = ImGuiPopupFlags(0)
	ImGuiPopupFlags_MouseButtonRight        = ImGuiPopupFlags(1)
	ImGuiPopupFlags_MouseButtonMiddle       = ImGuiPopupFlags(2)
	ImGuiPopupFlags_MouseButtonMask_        = ImGuiPopupFlags(0x1F)
	ImGuiPopupFlags_MouseButtonDefault_     = ImGuiPopupFlags(1)
	ImGuiPopupFlags_NoReopen                = ImGuiPopupFlags(1 << 5)
	ImGuiPopupFlags_NoOpenOverExistingPopup = ImGuiPopupFlags(1 << 7)
	ImGuiPopupFlags_NoOpenOverItems         = ImGuiPopupFlags(1 << 8)
	ImGuiPopupFlags_AnyPopupId              = ImGuiPopupFlags(1 << 10)
	ImGuiPopupFlags_AnyPopupLevel           = ImGuiPopupFlags(1 << 11)
	ImGuiPopupFlags_AnyPopup                = ImGuiPopupFlags(ImGuiPopupFlags_AnyPopupId | ImGuiPopupFlags_AnyPopupLevel)
)

type ImGuiSelectableFlags int

const (
	ImGuiSelectableFlags_None              = ImGuiSelectableFlags(0)
	ImGuiSelectableFlags_NoAutoClosePopups = ImGuiSelectableFlags(1 << 0)
	ImGuiSelectableFlags_SpanAllColumns    = ImGuiSelectableFlags(1 << 1)
	ImGuiSelectableFlags_AllowDoubleClick  = ImGuiSelectableFlags(1 << 2)
	ImGuiSelectableFlags_Disabled          = ImGuiSelectableFlags(1 << 3)
	ImGuiSelectableFlags_AllowOverlap      = ImGuiSelectableFlags(1 << 4)
	ImGuiSelectableFlags_Highlight         = ImGuiSelectableFlags(1 << 5)
	ImGuiSelectableFlags_DontClosePopups   = ImGuiSelectableFlags(ImGuiSelectableFlags_NoAutoClosePopups)
	ImGuiSelectableFlags_AllowItemOverlap  = ImGuiSelectableFlags(ImGuiSelectableFlags_AllowOverlap)
)

type ImGuiComboFlags int

const (
	ImGuiComboFlags_None            = ImGuiComboFlags(0)
	ImGuiComboFlags_PopupAlignLeft  = ImGuiComboFlags(1 << 0)
	ImGuiComboFlags_HeightSmall     = ImGuiComboFlags(1 << 1)
	ImGuiComboFlags_HeightRegular   = ImGuiComboFlags(1 << 2)
	ImGuiComboFlags_HeightLarge     = ImGuiComboFlags(1 << 3)
	ImGuiComboFlags_HeightLargest   = ImGuiComboFlags(1 << 4)
	ImGuiComboFlags_NoArrowButton   = ImGuiComboFlags(1 << 5)
	ImGuiComboFlags_NoPreview       = ImGuiComboFlags(1 << 6)
	ImGuiComboFlags_WidthFitPreview = ImGuiComboFlags(1 << 7)
	ImGuiComboFlags_HeightMask_     = ImGuiComboFlags(ImGuiComboFlags_HeightSmall | ImGuiComboFlags_HeightRegular | ImGuiComboFlags_HeightLarge | ImGuiComboFlags_HeightLargest)
)

type ImGuiTabBarFlags int

const (
	ImGuiTabBarFlags_None                         = ImGuiTabBarFlags(0)
	ImGuiTabBarFlags_Reorderable                  = ImGuiTabBarFlags(1 << 0)
	ImGuiTabBarFlags_AutoSelectNewTabs            = ImGuiTabBarFlags(1 << 1)
	ImGuiTabBarFlags_TabListPopupButton           = ImGuiTabBarFlags(1 << 2)
	ImGuiTabBarFlags_NoCloseWithMiddleMouseButton = ImGuiTabBarFlags(1 << 3)
	ImGuiTabBarFlags_NoTabListScrollingButtons    = ImGuiTabBarFlags(1 << 4)
	ImGuiTabBarFlags_NoTooltip                    = ImGuiTabBarFlags(1 << 5)
	ImGuiTabBarFlags_DrawSelectedOverline         = ImGuiTabBarFlags(1 << 6)
	ImGuiTabBarFlags_FittingPolicyResizeDown      = ImGuiTabBarFlags(1 << 7)
	ImGuiTabBarFlags_FittingPolicyScroll          = ImGuiTabBarFlags(1 << 8)
	ImGuiTabBarFlags_FittingPolicyMask_           = ImGuiTabBarFlags(ImGuiTabBarFlags_FittingPolicyResizeDown | ImGuiTabBarFlags_FittingPolicyScroll)
	ImGuiTabBarFlags_FittingPolicyDefault_        = ImGuiTabBarFlags(ImGuiTabBarFlags_FittingPolicyResizeDown)
)

type ImGuiTabItemFlags int

const (
	ImGuiTabItemFlags_None                         = ImGuiTabItemFlags(0)
	ImGuiTabItemFlags_UnsavedDocument              = ImGuiTabItemFlags(1 << 0)
	ImGuiTabItemFlags_SetSelected                  = ImGuiTabItemFlags(1 << 1)
	ImGuiTabItemFlags_NoCloseWithMiddleMouseButton = ImGuiTabItemFlags(1 << 2)
	ImGuiTabItemFlags_NoPushId                     = ImGuiTabItemFlags(1 << 3)
	ImGuiTabItemFlags_NoTooltip                    = ImGuiTabItemFlags(1 << 4)
	ImGuiTabItemFlags_NoReorder                    = ImGuiTabItemFlags(1 << 5)
	ImGuiTabItemFlags_Leading                      = ImGuiTabItemFlags(1 << 6)
	ImGuiTabItemFlags_Trailing                     = ImGuiTabItemFlags(1 << 7)
	ImGuiTabItemFlags_NoAssumedClosure             = ImGuiTabItemFlags(1 << 8)
)

type ImGuiFocusedFlags int

const (
	ImGuiFocusedFlags_None                = ImGuiFocusedFlags(0)
	ImGuiFocusedFlags_ChildWindows        = ImGuiFocusedFlags(1 << 0)
	ImGuiFocusedFlags_RootWindow          = ImGuiFocusedFlags(1 << 1)
	ImGuiFocusedFlags_AnyWindow           = ImGuiFocusedFlags(1 << 2)
	ImGuiFocusedFlags_NoPopupHierarchy    = ImGuiFocusedFlags(1 << 3)
	ImGuiFocusedFlags_DockHierarchy       = ImGuiFocusedFlags(1 << 4)
	ImGuiFocusedFlags_RootAndChildWindows = ImGuiFocusedFlags(ImGuiFocusedFlags_RootWindow | ImGuiFocusedFlags_ChildWindows)
)

type ImGuiHoveredFlags int

const (
	ImGuiHoveredFlags_None                         = ImGuiHoveredFlags(0)
	ImGuiHoveredFlags_ChildWindows                 = ImGuiHoveredFlags(1 << 0)
	ImGuiHoveredFlags_RootWindow                   = ImGuiHoveredFlags(1 << 1)
	ImGuiHoveredFlags_AnyWindow                    = ImGuiHoveredFlags(1 << 2)
	ImGuiHoveredFlags_NoPopupHierarchy             = ImGuiHoveredFlags(1 << 3)
	ImGuiHoveredFlags_DockHierarchy                = ImGuiHoveredFlags(1 << 4)
	ImGuiHoveredFlags_AllowWhenBlockedByPopup      = ImGuiHoveredFlags(1 << 5)
	ImGuiHoveredFlags_AllowWhenBlockedByActiveItem = ImGuiHoveredFlags(1 << 7)
	ImGuiHoveredFlags_AllowWhenOverlappedByItem    = ImGuiHoveredFlags(1 << 8)
	ImGuiHoveredFlags_AllowWhenOverlappedByWindow  = ImGuiHoveredFlags(1 << 9)
	ImGuiHoveredFlags_AllowWhenDisabled            = ImGuiHoveredFlags(1 << 10)
	ImGuiHoveredFlags_NoNavOverride                = ImGuiHoveredFlags(1 << 11)
	ImGuiHoveredFlags_AllowWhenOverlapped          = ImGuiHoveredFlags(ImGuiHoveredFlags_AllowWhenOverlappedByItem | ImGuiHoveredFlags_AllowWhenOverlappedByWindow)
	ImGuiHoveredFlags_RectOnly                     = ImGuiHoveredFlags(ImGuiHoveredFlags_AllowWhenBlockedByPopup | ImGuiHoveredFlags_AllowWhenBlockedByActiveItem | ImGuiHoveredFlags_AllowWhenOverlapped)
	ImGuiHoveredFlags_RootAndChildWindows          = ImGuiHoveredFlags(ImGuiHoveredFlags_RootWindow | ImGuiHoveredFlags_ChildWindows)
	ImGuiHoveredFlags_ForTooltip                   = ImGuiHoveredFlags(1 << 12)
	ImGuiHoveredFlags_Stationary                   = ImGuiHoveredFlags(1 << 13)
	ImGuiHoveredFlags_DelayNone                    = ImGuiHoveredFlags(1 << 14)
	ImGuiHoveredFlags_DelayShort                   = ImGuiHoveredFlags(1 << 15)
	ImGuiHoveredFlags_DelayNormal                  = ImGuiHoveredFlags(1 << 16)
	ImGuiHoveredFlags_NoSharedDelay                = ImGuiHoveredFlags(1 << 17)
)

type ImGuiDockNodeFlags int

const (
	ImGuiDockNodeFlags_None                     = ImGuiDockNodeFlags(0)
	ImGuiDockNodeFlags_KeepAliveOnly            = ImGuiDockNodeFlags(1 << 0)
	ImGuiDockNodeFlags_NoDockingOverCentralNode = ImGuiDockNodeFlags(1 << 2)
	ImGuiDockNodeFlags_PassthruCentralNode      = ImGuiDockNodeFlags(1 << 3)
	ImGuiDockNodeFlags_NoDockingSplit           = ImGuiDockNodeFlags(1 << 4)
	ImGuiDockNodeFlags_NoResize                 = ImGuiDockNodeFlags(1 << 5)
	ImGuiDockNodeFlags_AutoHideTabBar           = ImGuiDockNodeFlags(1 << 6)
	ImGuiDockNodeFlags_NoUndocking              = ImGuiDockNodeFlags(1 << 7)
	ImGuiDockNodeFlags_NoSplit                  = ImGuiDockNodeFlags(ImGuiDockNodeFlags_NoDockingSplit)
	ImGuiDockNodeFlags_NoDockingInCentralNode   = ImGuiDockNodeFlags(ImGuiDockNodeFlags_NoDockingOverCentralNode)
)

type ImGuiDragDropFlags int

const (
	ImGuiDragDropFlags_None                     = ImGuiDragDropFlags(0)
	ImGuiDragDropFlags_SourceNoPreviewTooltip   = ImGuiDragDropFlags(1 << 0)
	ImGuiDragDropFlags_SourceNoDisableHover     = ImGuiDragDropFlags(1 << 1)
	ImGuiDragDropFlags_SourceNoHoldToOpenOthers = ImGuiDragDropFlags(1 << 2)
	ImGuiDragDropFlags_SourceAllowNullID        = ImGuiDragDropFlags(1 << 3)
	ImGuiDragDropFlags_SourceExtern             = ImGuiDragDropFlags(1 << 4)
	ImGuiDragDropFlags_PayloadAutoExpire        = ImGuiDragDropFlags(1 << 5)
	ImGuiDragDropFlags_PayloadNoCrossContext    = ImGuiDragDropFlags(1 << 6)
	ImGuiDragDropFlags_PayloadNoCrossProcess    = ImGuiDragDropFlags(1 << 7)
	ImGuiDragDropFlags_AcceptBeforeDelivery     = ImGuiDragDropFlags(1 << 10)
	ImGuiDragDropFlags_AcceptNoDrawDefaultRect  = ImGuiDragDropFlags(1 << 11)
	ImGuiDragDropFlags_AcceptNoPreviewTooltip   = ImGuiDragDropFlags(1 << 12)
	ImGuiDragDropFlags_AcceptPeekOnly           = ImGuiDragDropFlags(ImGuiDragDropFlags_AcceptBeforeDelivery | ImGuiDragDropFlags_AcceptNoDrawDefaultRect)
	ImGuiDragDropFlags_SourceAutoExpirePayload  = ImGuiDragDropFlags(ImGuiDragDropFlags_PayloadAutoExpire)
)

type ImGuiDataType int

const (
	ImGuiDataType_S8     = iota
	ImGuiDataType_U8     = iota
	ImGuiDataType_S16    = iota
	ImGuiDataType_U16    = iota
	ImGuiDataType_S32    = iota
	ImGuiDataType_U32    = iota
	ImGuiDataType_S64    = iota
	ImGuiDataType_U64    = iota
	ImGuiDataType_Float  = iota
	ImGuiDataType_Double = iota
	ImGuiDataType_Bool   = iota
	ImGuiDataType_String = iota
	ImGuiDataType_COUNT  = iota
)

type ImGuiDir int

const (
	ImGuiDir_None  = ImGuiDir(-1)
	ImGuiDir_Left  = ImGuiDir(0)
	ImGuiDir_Right = ImGuiDir(1)
	ImGuiDir_Up    = ImGuiDir(2)
	ImGuiDir_Down  = ImGuiDir(3)
	ImGuiDir_COUNT = iota
)

type ImGuiSortDirection ImU8

const (
	ImGuiSortDirection_None       = ImGuiSortDirection(0)
	ImGuiSortDirection_Ascending  = ImGuiSortDirection(1)
	ImGuiSortDirection_Descending = ImGuiSortDirection(2)
)

type ImGuiKey int

const (
	ImGuiKey_None                = ImGuiKey(0)
	ImGuiKey_NamedKey_BEGIN      = ImGuiKey(512)
	ImGuiKey_Tab                 = ImGuiKey(512)
	ImGuiKey_LeftArrow           = iota
	ImGuiKey_RightArrow          = iota
	ImGuiKey_UpArrow             = iota
	ImGuiKey_DownArrow           = iota
	ImGuiKey_PageUp              = iota
	ImGuiKey_PageDown            = iota
	ImGuiKey_Home                = iota
	ImGuiKey_End                 = iota
	ImGuiKey_Insert              = iota
	ImGuiKey_Delete              = iota
	ImGuiKey_Backspace           = iota
	ImGuiKey_Space               = iota
	ImGuiKey_Enter               = iota
	ImGuiKey_Escape              = iota
	ImGuiKey_LeftCtrl            = iota
	ImGuiKey_LeftShift           = iota
	ImGuiKey_LeftAlt             = iota
	ImGuiKey_LeftSuper           = iota
	ImGuiKey_RightCtrl           = iota
	ImGuiKey_RightShift          = iota
	ImGuiKey_RightAlt            = iota
	ImGuiKey_RightSuper          = iota
	ImGuiKey_Menu                = iota
	ImGuiKey_0                   = iota
	ImGuiKey_1                   = iota
	ImGuiKey_2                   = iota
	ImGuiKey_3                   = iota
	ImGuiKey_4                   = iota
	ImGuiKey_5                   = iota
	ImGuiKey_6                   = iota
	ImGuiKey_7                   = iota
	ImGuiKey_8                   = iota
	ImGuiKey_9                   = iota
	ImGuiKey_A                   = iota
	ImGuiKey_B                   = iota
	ImGuiKey_C                   = iota
	ImGuiKey_D                   = iota
	ImGuiKey_E                   = iota
	ImGuiKey_F                   = iota
	ImGuiKey_G                   = iota
	ImGuiKey_H                   = iota
	ImGuiKey_I                   = iota
	ImGuiKey_J                   = iota
	ImGuiKey_K                   = iota
	ImGuiKey_L                   = iota
	ImGuiKey_M                   = iota
	ImGuiKey_N                   = iota
	ImGuiKey_O                   = iota
	ImGuiKey_P                   = iota
	ImGuiKey_Q                   = iota
	ImGuiKey_R                   = iota
	ImGuiKey_S                   = iota
	ImGuiKey_T                   = iota
	ImGuiKey_U                   = iota
	ImGuiKey_V                   = iota
	ImGuiKey_W                   = iota
	ImGuiKey_X                   = iota
	ImGuiKey_Y                   = iota
	ImGuiKey_Z                   = iota
	ImGuiKey_F1                  = iota
	ImGuiKey_F2                  = iota
	ImGuiKey_F3                  = iota
	ImGuiKey_F4                  = iota
	ImGuiKey_F5                  = iota
	ImGuiKey_F6                  = iota
	ImGuiKey_F7                  = iota
	ImGuiKey_F8                  = iota
	ImGuiKey_F9                  = iota
	ImGuiKey_F10                 = iota
	ImGuiKey_F11                 = iota
	ImGuiKey_F12                 = iota
	ImGuiKey_F13                 = iota
	ImGuiKey_F14                 = iota
	ImGuiKey_F15                 = iota
	ImGuiKey_F16                 = iota
	ImGuiKey_F17                 = iota
	ImGuiKey_F18                 = iota
	ImGuiKey_F19                 = iota
	ImGuiKey_F20                 = iota
	ImGuiKey_F21                 = iota
	ImGuiKey_F22                 = iota
	ImGuiKey_F23                 = iota
	ImGuiKey_F24                 = iota
	ImGuiKey_Apostrophe          = iota
	ImGuiKey_Comma               = iota
	ImGuiKey_Minus               = iota
	ImGuiKey_Period              = iota
	ImGuiKey_Slash               = iota
	ImGuiKey_Semicolon           = iota
	ImGuiKey_Equal               = iota
	ImGuiKey_LeftBracket         = iota
	ImGuiKey_Backslash           = iota
	ImGuiKey_RightBracket        = iota
	ImGuiKey_GraveAccent         = iota
	ImGuiKey_CapsLock            = iota
	ImGuiKey_ScrollLock          = iota
	ImGuiKey_NumLock             = iota
	ImGuiKey_PrintScreen         = iota
	ImGuiKey_Pause               = iota
	ImGuiKey_Keypad0             = iota
	ImGuiKey_Keypad1             = iota
	ImGuiKey_Keypad2             = iota
	ImGuiKey_Keypad3             = iota
	ImGuiKey_Keypad4             = iota
	ImGuiKey_Keypad5             = iota
	ImGuiKey_Keypad6             = iota
	ImGuiKey_Keypad7             = iota
	ImGuiKey_Keypad8             = iota
	ImGuiKey_Keypad9             = iota
	ImGuiKey_KeypadDecimal       = iota
	ImGuiKey_KeypadDivide        = iota
	ImGuiKey_KeypadMultiply      = iota
	ImGuiKey_KeypadSubtract      = iota
	ImGuiKey_KeypadAdd           = iota
	ImGuiKey_KeypadEnter         = iota
	ImGuiKey_KeypadEqual         = iota
	ImGuiKey_AppBack             = iota
	ImGuiKey_AppForward          = iota
	ImGuiKey_GamepadStart        = iota
	ImGuiKey_GamepadBack         = iota
	ImGuiKey_GamepadFaceLeft     = iota
	ImGuiKey_GamepadFaceRight    = iota
	ImGuiKey_GamepadFaceUp       = iota
	ImGuiKey_GamepadFaceDown     = iota
	ImGuiKey_GamepadDpadLeft     = iota
	ImGuiKey_GamepadDpadRight    = iota
	ImGuiKey_GamepadDpadUp       = iota
	ImGuiKey_GamepadDpadDown     = iota
	ImGuiKey_GamepadL1           = iota
	ImGuiKey_GamepadR1           = iota
	ImGuiKey_GamepadL2           = iota
	ImGuiKey_GamepadR2           = iota
	ImGuiKey_GamepadL3           = iota
	ImGuiKey_GamepadR3           = iota
	ImGuiKey_GamepadLStickLeft   = iota
	ImGuiKey_GamepadLStickRight  = iota
	ImGuiKey_GamepadLStickUp     = iota
	ImGuiKey_GamepadLStickDown   = iota
	ImGuiKey_GamepadRStickLeft   = iota
	ImGuiKey_GamepadRStickRight  = iota
	ImGuiKey_GamepadRStickUp     = iota
	ImGuiKey_GamepadRStickDown   = iota
	ImGuiKey_MouseLeft           = iota
	ImGuiKey_MouseRight          = iota
	ImGuiKey_MouseMiddle         = iota
	ImGuiKey_MouseX1             = iota
	ImGuiKey_MouseX2             = iota
	ImGuiKey_MouseWheelX         = iota
	ImGuiKey_MouseWheelY         = iota
	ImGuiKey_ReservedForModCtrl  = iota
	ImGuiKey_ReservedForModShift = iota
	ImGuiKey_ReservedForModAlt   = iota
	ImGuiKey_ReservedForModSuper = iota
	ImGuiKey_NamedKey_END        = iota
	ImGuiMod_None                = ImGuiKey(0)
	ImGuiMod_Ctrl                = ImGuiKey(1 << 12)
	ImGuiMod_Shift               = ImGuiKey(1 << 13)
	ImGuiMod_Alt                 = ImGuiKey(1 << 14)
	ImGuiMod_Super               = ImGuiKey(1 << 15)
	ImGuiMod_Mask_               = ImGuiKey(0xF000)
	ImGuiKey_NamedKey_COUNT      = ImGuiKey(ImGuiKey_NamedKey_END - ImGuiKey_NamedKey_BEGIN)
	ImGuiKey_COUNT               = ImGuiKey(ImGuiKey_NamedKey_END)
	ImGuiMod_Shortcut            = ImGuiKey(ImGuiMod_Ctrl)
	ImGuiKey_ModCtrl             = ImGuiKey(ImGuiMod_Ctrl)
	ImGuiKey_ModShift            = ImGuiKey(ImGuiMod_Shift)
	ImGuiKey_ModAlt              = ImGuiKey(ImGuiMod_Alt)
	ImGuiKey_ModSuper            = ImGuiKey(ImGuiMod_Super)
)

type ImGuiInputFlags int

const (
	ImGuiInputFlags_None                 = ImGuiInputFlags(0)
	ImGuiInputFlags_Repeat               = ImGuiInputFlags(1 << 0)
	ImGuiInputFlags_RouteActive          = ImGuiInputFlags(1 << 10)
	ImGuiInputFlags_RouteFocused         = ImGuiInputFlags(1 << 11)
	ImGuiInputFlags_RouteGlobal          = ImGuiInputFlags(1 << 12)
	ImGuiInputFlags_RouteAlways          = ImGuiInputFlags(1 << 13)
	ImGuiInputFlags_RouteOverFocused     = ImGuiInputFlags(1 << 14)
	ImGuiInputFlags_RouteOverActive      = ImGuiInputFlags(1 << 15)
	ImGuiInputFlags_RouteUnlessBgFocused = ImGuiInputFlags(1 << 16)
	ImGuiInputFlags_RouteFromRootWindow  = ImGuiInputFlags(1 << 17)
	ImGuiInputFlags_Tooltip              = ImGuiInputFlags(1 << 18)
)

type ImGuiConfigFlags int

const (
	ImGuiConfigFlags_None                    = ImGuiConfigFlags(0)
	ImGuiConfigFlags_NavEnableKeyboard       = ImGuiConfigFlags(1 << 0)
	ImGuiConfigFlags_NavEnableGamepad        = ImGuiConfigFlags(1 << 1)
	ImGuiConfigFlags_NoMouse                 = ImGuiConfigFlags(1 << 4)
	ImGuiConfigFlags_NoMouseCursorChange     = ImGuiConfigFlags(1 << 5)
	ImGuiConfigFlags_NoKeyboard              = ImGuiConfigFlags(1 << 6)
	ImGuiConfigFlags_DockingEnable           = ImGuiConfigFlags(1 << 7)
	ImGuiConfigFlags_ViewportsEnable         = ImGuiConfigFlags(1 << 10)
	ImGuiConfigFlags_DpiEnableScaleViewports = ImGuiConfigFlags(1 << 14)
	ImGuiConfigFlags_DpiEnableScaleFonts     = ImGuiConfigFlags(1 << 15)
	ImGuiConfigFlags_IsSRGB                  = ImGuiConfigFlags(1 << 20)
	ImGuiConfigFlags_IsTouchScreen           = ImGuiConfigFlags(1 << 21)
	ImGuiConfigFlags_NavEnableSetMousePos    = ImGuiConfigFlags(1 << 2)
	ImGuiConfigFlags_NavNoCaptureKeyboard    = ImGuiConfigFlags(1 << 3)
)

type ImGuiBackendFlags int

const (
	ImGuiBackendFlags_None                    = ImGuiBackendFlags(0)
	ImGuiBackendFlags_HasGamepad              = ImGuiBackendFlags(1 << 0)
	ImGuiBackendFlags_HasMouseCursors         = ImGuiBackendFlags(1 << 1)
	ImGuiBackendFlags_HasSetMousePos          = ImGuiBackendFlags(1 << 2)
	ImGuiBackendFlags_RendererHasVtxOffset    = ImGuiBackendFlags(1 << 3)
	ImGuiBackendFlags_PlatformHasViewports    = ImGuiBackendFlags(1 << 10)
	ImGuiBackendFlags_HasMouseHoveredViewport = ImGuiBackendFlags(1 << 11)
	ImGuiBackendFlags_RendererHasViewports    = ImGuiBackendFlags(1 << 12)
)

type ImGuiCol int

const (
	ImGuiCol_Text                      = iota
	ImGuiCol_TextDisabled              = iota
	ImGuiCol_WindowBg                  = iota
	ImGuiCol_ChildBg                   = iota
	ImGuiCol_PopupBg                   = iota
	ImGuiCol_Border                    = iota
	ImGuiCol_BorderShadow              = iota
	ImGuiCol_FrameBg                   = iota
	ImGuiCol_FrameBgHovered            = iota
	ImGuiCol_FrameBgActive             = iota
	ImGuiCol_TitleBg                   = iota
	ImGuiCol_TitleBgActive             = iota
	ImGuiCol_TitleBgCollapsed          = iota
	ImGuiCol_MenuBarBg                 = iota
	ImGuiCol_ScrollbarBg               = iota
	ImGuiCol_ScrollbarGrab             = iota
	ImGuiCol_ScrollbarGrabHovered      = iota
	ImGuiCol_ScrollbarGrabActive       = iota
	ImGuiCol_CheckMark                 = iota
	ImGuiCol_SliderGrab                = iota
	ImGuiCol_SliderGrabActive          = iota
	ImGuiCol_Button                    = iota
	ImGuiCol_ButtonHovered             = iota
	ImGuiCol_ButtonActive              = iota
	ImGuiCol_Header                    = iota
	ImGuiCol_HeaderHovered             = iota
	ImGuiCol_HeaderActive              = iota
	ImGuiCol_Separator                 = iota
	ImGuiCol_SeparatorHovered          = iota
	ImGuiCol_SeparatorActive           = iota
	ImGuiCol_ResizeGrip                = iota
	ImGuiCol_ResizeGripHovered         = iota
	ImGuiCol_ResizeGripActive          = iota
	ImGuiCol_TabHovered                = iota
	ImGuiCol_Tab                       = iota
	ImGuiCol_TabSelected               = iota
	ImGuiCol_TabSelectedOverline       = iota
	ImGuiCol_TabDimmed                 = iota
	ImGuiCol_TabDimmedSelected         = iota
	ImGuiCol_TabDimmedSelectedOverline = iota
	ImGuiCol_DockingPreview            = iota
	ImGuiCol_DockingEmptyBg            = iota
	ImGuiCol_PlotLines                 = iota
	ImGuiCol_PlotLinesHovered          = iota
	ImGuiCol_PlotHistogram             = iota
	ImGuiCol_PlotHistogramHovered      = iota
	ImGuiCol_TableHeaderBg             = iota
	ImGuiCol_TableBorderStrong         = iota
	ImGuiCol_TableBorderLight          = iota
	ImGuiCol_TableRowBg                = iota
	ImGuiCol_TableRowBgAlt             = iota
	ImGuiCol_TextLink                  = iota
	ImGuiCol_TextSelectedBg            = iota
	ImGuiCol_DragDropTarget            = iota
	ImGuiCol_NavCursor                 = iota
	ImGuiCol_NavWindowingHighlight     = iota
	ImGuiCol_NavWindowingDimBg         = iota
	ImGuiCol_ModalWindowDimBg          = iota
	ImGuiCol_COUNT                     = iota
	ImGuiCol_TabActive                 = ImGuiCol(ImGuiCol_TabSelected)
	ImGuiCol_TabUnfocused              = ImGuiCol(ImGuiCol_TabDimmed)
	ImGuiCol_TabUnfocusedActive        = ImGuiCol(ImGuiCol_TabDimmedSelected)
	ImGuiCol_NavHighlight              = ImGuiCol(ImGuiCol_NavCursor)
)

type ImGuiStyleVar int

const (
	ImGuiStyleVar_Alpha                       = iota
	ImGuiStyleVar_DisabledAlpha               = iota
	ImGuiStyleVar_WindowPadding               = iota
	ImGuiStyleVar_WindowRounding              = iota
	ImGuiStyleVar_WindowBorderSize            = iota
	ImGuiStyleVar_WindowMinSize               = iota
	ImGuiStyleVar_WindowTitleAlign            = iota
	ImGuiStyleVar_ChildRounding               = iota
	ImGuiStyleVar_ChildBorderSize             = iota
	ImGuiStyleVar_PopupRounding               = iota
	ImGuiStyleVar_PopupBorderSize             = iota
	ImGuiStyleVar_FramePadding                = iota
	ImGuiStyleVar_FrameRounding               = iota
	ImGuiStyleVar_FrameBorderSize             = iota
	ImGuiStyleVar_ItemSpacing                 = iota
	ImGuiStyleVar_ItemInnerSpacing            = iota
	ImGuiStyleVar_IndentSpacing               = iota
	ImGuiStyleVar_CellPadding                 = iota
	ImGuiStyleVar_ScrollbarSize               = iota
	ImGuiStyleVar_ScrollbarRounding           = iota
	ImGuiStyleVar_GrabMinSize                 = iota
	ImGuiStyleVar_GrabRounding                = iota
	ImGuiStyleVar_TabRounding                 = iota
	ImGuiStyleVar_TabBorderSize               = iota
	ImGuiStyleVar_TabBarBorderSize            = iota
	ImGuiStyleVar_TabBarOverlineSize          = iota
	ImGuiStyleVar_TableAngledHeadersAngle     = iota
	ImGuiStyleVar_TableAngledHeadersTextAlign = iota
	ImGuiStyleVar_ButtonTextAlign             = iota
	ImGuiStyleVar_SelectableTextAlign         = iota
	ImGuiStyleVar_SeparatorTextBorderSize     = iota
	ImGuiStyleVar_SeparatorTextAlign          = iota
	ImGuiStyleVar_SeparatorTextPadding        = iota
	ImGuiStyleVar_DockingSeparatorSize        = iota
	ImGuiStyleVar_COUNT                       = iota
)

type ImGuiButtonFlags int

const (
	ImGuiButtonFlags_None              = ImGuiButtonFlags(0)
	ImGuiButtonFlags_MouseButtonLeft   = ImGuiButtonFlags(1 << 0)
	ImGuiButtonFlags_MouseButtonRight  = ImGuiButtonFlags(1 << 1)
	ImGuiButtonFlags_MouseButtonMiddle = ImGuiButtonFlags(1 << 2)
	ImGuiButtonFlags_MouseButtonMask_  = ImGuiButtonFlags(ImGuiButtonFlags_MouseButtonLeft | ImGuiButtonFlags_MouseButtonRight | ImGuiButtonFlags_MouseButtonMiddle)
	ImGuiButtonFlags_EnableNav         = ImGuiButtonFlags(1 << 3)
)

type ImGuiColorEditFlags int

const (
	ImGuiColorEditFlags_None             = ImGuiColorEditFlags(0)
	ImGuiColorEditFlags_NoAlpha          = ImGuiColorEditFlags(1 << 1)
	ImGuiColorEditFlags_NoPicker         = ImGuiColorEditFlags(1 << 2)
	ImGuiColorEditFlags_NoOptions        = ImGuiColorEditFlags(1 << 3)
	ImGuiColorEditFlags_NoSmallPreview   = ImGuiColorEditFlags(1 << 4)
	ImGuiColorEditFlags_NoInputs         = ImGuiColorEditFlags(1 << 5)
	ImGuiColorEditFlags_NoTooltip        = ImGuiColorEditFlags(1 << 6)
	ImGuiColorEditFlags_NoLabel          = ImGuiColorEditFlags(1 << 7)
	ImGuiColorEditFlags_NoSidePreview    = ImGuiColorEditFlags(1 << 8)
	ImGuiColorEditFlags_NoDragDrop       = ImGuiColorEditFlags(1 << 9)
	ImGuiColorEditFlags_NoBorder         = ImGuiColorEditFlags(1 << 10)
	ImGuiColorEditFlags_AlphaOpaque      = ImGuiColorEditFlags(1 << 11)
	ImGuiColorEditFlags_AlphaNoBg        = ImGuiColorEditFlags(1 << 12)
	ImGuiColorEditFlags_AlphaPreviewHalf = ImGuiColorEditFlags(1 << 13)
	ImGuiColorEditFlags_AlphaBar         = ImGuiColorEditFlags(1 << 16)
	ImGuiColorEditFlags_HDR              = ImGuiColorEditFlags(1 << 19)
	ImGuiColorEditFlags_DisplayRGB       = ImGuiColorEditFlags(1 << 20)
	ImGuiColorEditFlags_DisplayHSV       = ImGuiColorEditFlags(1 << 21)
	ImGuiColorEditFlags_DisplayHex       = ImGuiColorEditFlags(1 << 22)
	ImGuiColorEditFlags_Uint8            = ImGuiColorEditFlags(1 << 23)
	ImGuiColorEditFlags_Float            = ImGuiColorEditFlags(1 << 24)
	ImGuiColorEditFlags_PickerHueBar     = ImGuiColorEditFlags(1 << 25)
	ImGuiColorEditFlags_PickerHueWheel   = ImGuiColorEditFlags(1 << 26)
	ImGuiColorEditFlags_InputRGB         = ImGuiColorEditFlags(1 << 27)
	ImGuiColorEditFlags_InputHSV         = ImGuiColorEditFlags(1 << 28)
	ImGuiColorEditFlags_DefaultOptions_  = ImGuiColorEditFlags(ImGuiColorEditFlags_Uint8 | ImGuiColorEditFlags_DisplayRGB | ImGuiColorEditFlags_InputRGB | ImGuiColorEditFlags_PickerHueBar)
	ImGuiColorEditFlags_AlphaMask_       = ImGuiColorEditFlags(ImGuiColorEditFlags_NoAlpha | ImGuiColorEditFlags_AlphaOpaque | ImGuiColorEditFlags_AlphaNoBg | ImGuiColorEditFlags_AlphaPreviewHalf)
	ImGuiColorEditFlags_DisplayMask_     = ImGuiColorEditFlags(ImGuiColorEditFlags_DisplayRGB | ImGuiColorEditFlags_DisplayHSV | ImGuiColorEditFlags_DisplayHex)
	ImGuiColorEditFlags_DataTypeMask_    = ImGuiColorEditFlags(ImGuiColorEditFlags_Uint8 | ImGuiColorEditFlags_Float)
	ImGuiColorEditFlags_PickerMask_      = ImGuiColorEditFlags(ImGuiColorEditFlags_PickerHueWheel | ImGuiColorEditFlags_PickerHueBar)
	ImGuiColorEditFlags_InputMask_       = ImGuiColorEditFlags(ImGuiColorEditFlags_InputRGB | ImGuiColorEditFlags_InputHSV)
	ImGuiColorEditFlags_AlphaPreview     = ImGuiColorEditFlags(0)
)

type ImGuiSliderFlags int

const (
	ImGuiSliderFlags_None            = ImGuiSliderFlags(0)
	ImGuiSliderFlags_Logarithmic     = ImGuiSliderFlags(1 << 5)
	ImGuiSliderFlags_NoRoundToFormat = ImGuiSliderFlags(1 << 6)
	ImGuiSliderFlags_NoInput         = ImGuiSliderFlags(1 << 7)
	ImGuiSliderFlags_WrapAround      = ImGuiSliderFlags(1 << 8)
	ImGuiSliderFlags_ClampOnInput    = ImGuiSliderFlags(1 << 9)
	ImGuiSliderFlags_ClampZeroRange  = ImGuiSliderFlags(1 << 10)
	ImGuiSliderFlags_NoSpeedTweaks   = ImGuiSliderFlags(1 << 11)
	ImGuiSliderFlags_AlwaysClamp     = ImGuiSliderFlags(ImGuiSliderFlags_ClampOnInput | ImGuiSliderFlags_ClampZeroRange)
	ImGuiSliderFlags_InvalidMask_    = ImGuiSliderFlags(0x7000000F)
)

type ImGuiMouseButton int

const (
	ImGuiMouseButton_Left   = ImGuiMouseButton(0)
	ImGuiMouseButton_Right  = ImGuiMouseButton(1)
	ImGuiMouseButton_Middle = ImGuiMouseButton(2)
	ImGuiMouseButton_COUNT  = ImGuiMouseButton(5)
)

type ImGuiMouseCursor int

const (
	ImGuiMouseCursor_None       = ImGuiMouseCursor(-1)
	ImGuiMouseCursor_Arrow      = ImGuiMouseCursor(0)
	ImGuiMouseCursor_TextInput  = iota
	ImGuiMouseCursor_ResizeAll  = iota
	ImGuiMouseCursor_ResizeNS   = iota
	ImGuiMouseCursor_ResizeEW   = iota
	ImGuiMouseCursor_ResizeNESW = iota
	ImGuiMouseCursor_ResizeNWSE = iota
	ImGuiMouseCursor_Hand       = iota
	ImGuiMouseCursor_NotAllowed = iota
	ImGuiMouseCursor_COUNT      = iota
)

type ImGuiMouseSource int

const (
	ImGuiMouseSource_Mouse       = ImGuiMouseSource(0)
	ImGuiMouseSource_TouchScreen = iota
	ImGuiMouseSource_Pen         = iota
	ImGuiMouseSource_COUNT       = iota
)

type ImGuiCond int

const (
	ImGuiCond_None         = ImGuiCond(0)
	ImGuiCond_Always       = ImGuiCond(1 << 0)
	ImGuiCond_Once         = ImGuiCond(1 << 1)
	ImGuiCond_FirstUseEver = ImGuiCond(1 << 2)
	ImGuiCond_Appearing    = ImGuiCond(1 << 3)
)

type ImGuiTableFlags int

const (
	ImGuiTableFlags_None                       = ImGuiTableFlags(0)
	ImGuiTableFlags_Resizable                  = ImGuiTableFlags(1 << 0)
	ImGuiTableFlags_Reorderable                = ImGuiTableFlags(1 << 1)
	ImGuiTableFlags_Hideable                   = ImGuiTableFlags(1 << 2)
	ImGuiTableFlags_Sortable                   = ImGuiTableFlags(1 << 3)
	ImGuiTableFlags_NoSavedSettings            = ImGuiTableFlags(1 << 4)
	ImGuiTableFlags_ContextMenuInBody          = ImGuiTableFlags(1 << 5)
	ImGuiTableFlags_RowBg                      = ImGuiTableFlags(1 << 6)
	ImGuiTableFlags_BordersInnerH              = ImGuiTableFlags(1 << 7)
	ImGuiTableFlags_BordersOuterH              = ImGuiTableFlags(1 << 8)
	ImGuiTableFlags_BordersInnerV              = ImGuiTableFlags(1 << 9)
	ImGuiTableFlags_BordersOuterV              = ImGuiTableFlags(1 << 10)
	ImGuiTableFlags_BordersH                   = ImGuiTableFlags(ImGuiTableFlags_BordersInnerH | ImGuiTableFlags_BordersOuterH)
	ImGuiTableFlags_BordersV                   = ImGuiTableFlags(ImGuiTableFlags_BordersInnerV | ImGuiTableFlags_BordersOuterV)
	ImGuiTableFlags_BordersInner               = ImGuiTableFlags(ImGuiTableFlags_BordersInnerV | ImGuiTableFlags_BordersInnerH)
	ImGuiTableFlags_BordersOuter               = ImGuiTableFlags(ImGuiTableFlags_BordersOuterV | ImGuiTableFlags_BordersOuterH)
	ImGuiTableFlags_Borders                    = ImGuiTableFlags(ImGuiTableFlags_BordersInner | ImGuiTableFlags_BordersOuter)
	ImGuiTableFlags_NoBordersInBody            = ImGuiTableFlags(1 << 11)
	ImGuiTableFlags_NoBordersInBodyUntilResize = ImGuiTableFlags(1 << 12)
	ImGuiTableFlags_SizingFixedFit             = ImGuiTableFlags(1 << 13)
	ImGuiTableFlags_SizingFixedSame            = ImGuiTableFlags(2 << 13)
	ImGuiTableFlags_SizingStretchProp          = ImGuiTableFlags(3 << 13)
	ImGuiTableFlags_SizingStretchSame          = ImGuiTableFlags(4 << 13)
	ImGuiTableFlags_NoHostExtendX              = ImGuiTableFlags(1 << 16)
	ImGuiTableFlags_NoHostExtendY              = ImGuiTableFlags(1 << 17)
	ImGuiTableFlags_NoKeepColumnsVisible       = ImGuiTableFlags(1 << 18)
	ImGuiTableFlags_PreciseWidths              = ImGuiTableFlags(1 << 19)
	ImGuiTableFlags_NoClip                     = ImGuiTableFlags(1 << 20)
	ImGuiTableFlags_PadOuterX                  = ImGuiTableFlags(1 << 21)
	ImGuiTableFlags_NoPadOuterX                = ImGuiTableFlags(1 << 22)
	ImGuiTableFlags_NoPadInnerX                = ImGuiTableFlags(1 << 23)
	ImGuiTableFlags_ScrollX                    = ImGuiTableFlags(1 << 24)
	ImGuiTableFlags_ScrollY                    = ImGuiTableFlags(1 << 25)
	ImGuiTableFlags_SortMulti                  = ImGuiTableFlags(1 << 26)
	ImGuiTableFlags_SortTristate               = ImGuiTableFlags(1 << 27)
	ImGuiTableFlags_HighlightHoveredColumn     = ImGuiTableFlags(1 << 28)
	ImGuiTableFlags_SizingMask_                = ImGuiTableFlags(ImGuiTableFlags_SizingFixedFit | ImGuiTableFlags_SizingFixedSame | ImGuiTableFlags_SizingStretchProp | ImGuiTableFlags_SizingStretchSame)
)

type ImGuiTableColumnFlags int

const (
	ImGuiTableColumnFlags_None                 = ImGuiTableColumnFlags(0)
	ImGuiTableColumnFlags_Disabled             = ImGuiTableColumnFlags(1 << 0)
	ImGuiTableColumnFlags_DefaultHide          = ImGuiTableColumnFlags(1 << 1)
	ImGuiTableColumnFlags_DefaultSort          = ImGuiTableColumnFlags(1 << 2)
	ImGuiTableColumnFlags_WidthStretch         = ImGuiTableColumnFlags(1 << 3)
	ImGuiTableColumnFlags_WidthFixed           = ImGuiTableColumnFlags(1 << 4)
	ImGuiTableColumnFlags_NoResize             = ImGuiTableColumnFlags(1 << 5)
	ImGuiTableColumnFlags_NoReorder            = ImGuiTableColumnFlags(1 << 6)
	ImGuiTableColumnFlags_NoHide               = ImGuiTableColumnFlags(1 << 7)
	ImGuiTableColumnFlags_NoClip               = ImGuiTableColumnFlags(1 << 8)
	ImGuiTableColumnFlags_NoSort               = ImGuiTableColumnFlags(1 << 9)
	ImGuiTableColumnFlags_NoSortAscending      = ImGuiTableColumnFlags(1 << 10)
	ImGuiTableColumnFlags_NoSortDescending     = ImGuiTableColumnFlags(1 << 11)
	ImGuiTableColumnFlags_NoHeaderLabel        = ImGuiTableColumnFlags(1 << 12)
	ImGuiTableColumnFlags_NoHeaderWidth        = ImGuiTableColumnFlags(1 << 13)
	ImGuiTableColumnFlags_PreferSortAscending  = ImGuiTableColumnFlags(1 << 14)
	ImGuiTableColumnFlags_PreferSortDescending = ImGuiTableColumnFlags(1 << 15)
	ImGuiTableColumnFlags_IndentEnable         = ImGuiTableColumnFlags(1 << 16)
	ImGuiTableColumnFlags_IndentDisable        = ImGuiTableColumnFlags(1 << 17)
	ImGuiTableColumnFlags_AngledHeader         = ImGuiTableColumnFlags(1 << 18)
	ImGuiTableColumnFlags_IsEnabled            = ImGuiTableColumnFlags(1 << 24)
	ImGuiTableColumnFlags_IsVisible            = ImGuiTableColumnFlags(1 << 25)
	ImGuiTableColumnFlags_IsSorted             = ImGuiTableColumnFlags(1 << 26)
	ImGuiTableColumnFlags_IsHovered            = ImGuiTableColumnFlags(1 << 27)
	ImGuiTableColumnFlags_WidthMask_           = ImGuiTableColumnFlags(ImGuiTableColumnFlags_WidthStretch | ImGuiTableColumnFlags_WidthFixed)
	ImGuiTableColumnFlags_IndentMask_          = ImGuiTableColumnFlags(ImGuiTableColumnFlags_IndentEnable | ImGuiTableColumnFlags_IndentDisable)
	ImGuiTableColumnFlags_StatusMask_          = ImGuiTableColumnFlags(ImGuiTableColumnFlags_IsEnabled | ImGuiTableColumnFlags_IsVisible | ImGuiTableColumnFlags_IsSorted | ImGuiTableColumnFlags_IsHovered)
	ImGuiTableColumnFlags_NoDirectResize_      = ImGuiTableColumnFlags(1 << 30)
)

type ImGuiTableRowFlags int

const (
	ImGuiTableRowFlags_None    = ImGuiTableRowFlags(0)
	ImGuiTableRowFlags_Headers = ImGuiTableRowFlags(1 << 0)
)

type ImGuiTableBgTarget int

const (
	ImGuiTableBgTarget_None   = ImGuiTableBgTarget(0)
	ImGuiTableBgTarget_RowBg0 = ImGuiTableBgTarget(1)
	ImGuiTableBgTarget_RowBg1 = ImGuiTableBgTarget(2)
	ImGuiTableBgTarget_CellBg = ImGuiTableBgTarget(3)
)

type ImGuiMultiSelectFlags int

const (
	ImGuiMultiSelectFlags_None                  = ImGuiMultiSelectFlags(0)
	ImGuiMultiSelectFlags_SingleSelect          = ImGuiMultiSelectFlags(1 << 0)
	ImGuiMultiSelectFlags_NoSelectAll           = ImGuiMultiSelectFlags(1 << 1)
	ImGuiMultiSelectFlags_NoRangeSelect         = ImGuiMultiSelectFlags(1 << 2)
	ImGuiMultiSelectFlags_NoAutoSelect          = ImGuiMultiSelectFlags(1 << 3)
	ImGuiMultiSelectFlags_NoAutoClear           = ImGuiMultiSelectFlags(1 << 4)
	ImGuiMultiSelectFlags_NoAutoClearOnReselect = ImGuiMultiSelectFlags(1 << 5)
	ImGuiMultiSelectFlags_BoxSelect1d           = ImGuiMultiSelectFlags(1 << 6)
	ImGuiMultiSelectFlags_BoxSelect2d           = ImGuiMultiSelectFlags(1 << 7)
	ImGuiMultiSelectFlags_BoxSelectNoScroll     = ImGuiMultiSelectFlags(1 << 8)
	ImGuiMultiSelectFlags_ClearOnEscape         = ImGuiMultiSelectFlags(1 << 9)
	ImGuiMultiSelectFlags_ClearOnClickVoid      = ImGuiMultiSelectFlags(1 << 10)
	ImGuiMultiSelectFlags_ScopeWindow           = ImGuiMultiSelectFlags(1 << 11)
	ImGuiMultiSelectFlags_ScopeRect             = ImGuiMultiSelectFlags(1 << 12)
	ImGuiMultiSelectFlags_SelectOnClick         = ImGuiMultiSelectFlags(1 << 13)
	ImGuiMultiSelectFlags_SelectOnClickRelease  = ImGuiMultiSelectFlags(1 << 14)
	ImGuiMultiSelectFlags_NavWrapX              = ImGuiMultiSelectFlags(1 << 16)
)

type ImGuiSelectionRequestType int

const (
	ImGuiSelectionRequestType_None     = ImGuiSelectionRequestType(0)
	ImGuiSelectionRequestType_SetAll   = iota
	ImGuiSelectionRequestType_SetRange = iota
)

type ImDrawFlags int

const (
	ImDrawFlags_None                    = ImDrawFlags(0)
	ImDrawFlags_Closed                  = ImDrawFlags(1 << 0)
	ImDrawFlags_RoundCornersTopLeft     = ImDrawFlags(1 << 4)
	ImDrawFlags_RoundCornersTopRight    = ImDrawFlags(1 << 5)
	ImDrawFlags_RoundCornersBottomLeft  = ImDrawFlags(1 << 6)
	ImDrawFlags_RoundCornersBottomRight = ImDrawFlags(1 << 7)
	ImDrawFlags_RoundCornersNone        = ImDrawFlags(1 << 8)
	ImDrawFlags_RoundCornersTop         = ImDrawFlags(ImDrawFlags_RoundCornersTopLeft | ImDrawFlags_RoundCornersTopRight)
	ImDrawFlags_RoundCornersBottom      = ImDrawFlags(ImDrawFlags_RoundCornersBottomLeft | ImDrawFlags_RoundCornersBottomRight)
	ImDrawFlags_RoundCornersLeft        = ImDrawFlags(ImDrawFlags_RoundCornersBottomLeft | ImDrawFlags_RoundCornersTopLeft)
	ImDrawFlags_RoundCornersRight       = ImDrawFlags(ImDrawFlags_RoundCornersBottomRight | ImDrawFlags_RoundCornersTopRight)
	ImDrawFlags_RoundCornersAll         = ImDrawFlags(ImDrawFlags_RoundCornersTopLeft | ImDrawFlags_RoundCornersTopRight | ImDrawFlags_RoundCornersBottomLeft | ImDrawFlags_RoundCornersBottomRight)
	ImDrawFlags_RoundCornersDefault_    = ImDrawFlags(ImDrawFlags_RoundCornersAll)
	ImDrawFlags_RoundCornersMask_       = ImDrawFlags(ImDrawFlags_RoundCornersAll | ImDrawFlags_RoundCornersNone)
)

type ImDrawListFlags int

const (
	ImDrawListFlags_None                   = ImDrawListFlags(0)
	ImDrawListFlags_AntiAliasedLines       = ImDrawListFlags(1 << 0)
	ImDrawListFlags_AntiAliasedLinesUseTex = ImDrawListFlags(1 << 1)
	ImDrawListFlags_AntiAliasedFill        = ImDrawListFlags(1 << 2)
	ImDrawListFlags_AllowVtxOffset         = ImDrawListFlags(1 << 3)
)

type ImFontAtlasFlags int

const (
	ImFontAtlasFlags_None               = ImFontAtlasFlags(0)
	ImFontAtlasFlags_NoPowerOfTwoHeight = ImFontAtlasFlags(1 << 0)
	ImFontAtlasFlags_NoMouseCursors     = ImFontAtlasFlags(1 << 1)
	ImFontAtlasFlags_NoBakedLines       = ImFontAtlasFlags(1 << 2)
)

type ImGuiViewportFlags int

const (
	ImGuiViewportFlags_None                = ImGuiViewportFlags(0)
	ImGuiViewportFlags_IsPlatformWindow    = ImGuiViewportFlags(1 << 0)
	ImGuiViewportFlags_IsPlatformMonitor   = ImGuiViewportFlags(1 << 1)
	ImGuiViewportFlags_OwnedByApp          = ImGuiViewportFlags(1 << 2)
	ImGuiViewportFlags_NoDecoration        = ImGuiViewportFlags(1 << 3)
	ImGuiViewportFlags_NoTaskBarIcon       = ImGuiViewportFlags(1 << 4)
	ImGuiViewportFlags_NoFocusOnAppearing  = ImGuiViewportFlags(1 << 5)
	ImGuiViewportFlags_NoFocusOnClick      = ImGuiViewportFlags(1 << 6)
	ImGuiViewportFlags_NoInputs            = ImGuiViewportFlags(1 << 7)
	ImGuiViewportFlags_NoRendererClear     = ImGuiViewportFlags(1 << 8)
	ImGuiViewportFlags_NoAutoMerge         = ImGuiViewportFlags(1 << 9)
	ImGuiViewportFlags_TopMost             = ImGuiViewportFlags(1 << 10)
	ImGuiViewportFlags_CanHostOtherWindows = ImGuiViewportFlags(1 << 11)
	ImGuiViewportFlags_IsMinimized         = ImGuiViewportFlags(1 << 12)
	ImGuiViewportFlags_IsFocused           = ImGuiViewportFlags(1 << 13)
)
