// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import "strconv"

type KeyCode uint32

const (
	KeyCodeKey_None           KeyCode = 0
	KeyCodeKey_Tab            KeyCode = 1
	KeyCodeKey_LeftArrow      KeyCode = 2
	KeyCodeKey_RightArrow     KeyCode = 3
	KeyCodeKey_UpArrow        KeyCode = 4
	KeyCodeKey_DownArrow      KeyCode = 5
	KeyCodeKey_PageUp         KeyCode = 6
	KeyCodeKey_PageDown       KeyCode = 7
	KeyCodeKey_Home           KeyCode = 8
	KeyCodeKey_End            KeyCode = 9
	KeyCodeKey_Insert         KeyCode = 10
	KeyCodeKey_Delete         KeyCode = 11
	KeyCodeKey_Backspace      KeyCode = 12
	KeyCodeKey_Space          KeyCode = 13
	KeyCodeKey_Enter          KeyCode = 14
	KeyCodeKey_Escape         KeyCode = 15
	KeyCodeKey_Apostrophe     KeyCode = 16
	KeyCodeKey_Comma          KeyCode = 17
	KeyCodeKey_Minus          KeyCode = 18
	KeyCodeKey_Period         KeyCode = 19
	KeyCodeKey_Slash          KeyCode = 20
	KeyCodeKey_Semicolon      KeyCode = 21
	KeyCodeKey_Equal          KeyCode = 22
	KeyCodeKey_LeftBracket    KeyCode = 23
	KeyCodeKey_Backslash      KeyCode = 24
	KeyCodeKey_RightBracket   KeyCode = 25
	KeyCodeKey_GraveAccent    KeyCode = 26
	KeyCodeKey_CapsLock       KeyCode = 27
	KeyCodeKey_ScrollLock     KeyCode = 28
	KeyCodeKey_NumLock        KeyCode = 29
	KeyCodeKey_PrintScreen    KeyCode = 30
	KeyCodeKey_Pause          KeyCode = 31
	KeyCodeKey_Keypad0        KeyCode = 32
	KeyCodeKey_Keypad1        KeyCode = 33
	KeyCodeKey_Keypad2        KeyCode = 34
	KeyCodeKey_Keypad3        KeyCode = 35
	KeyCodeKey_Keypad4        KeyCode = 36
	KeyCodeKey_Keypad5        KeyCode = 37
	KeyCodeKey_Keypad6        KeyCode = 38
	KeyCodeKey_Keypad7        KeyCode = 39
	KeyCodeKey_Keypad8        KeyCode = 40
	KeyCodeKey_Keypad9        KeyCode = 41
	KeyCodeKey_KeypadDecimal  KeyCode = 42
	KeyCodeKey_KeypadDivide   KeyCode = 43
	KeyCodeKey_KeypadMultiply KeyCode = 44
	KeyCodeKey_KeypadSubtract KeyCode = 45
	KeyCodeKey_KeypadAdd      KeyCode = 46
	KeyCodeKey_KeypadEnter    KeyCode = 47
	KeyCodeKey_KeypadEqual    KeyCode = 48
	KeyCodeKey_LeftCtrl       KeyCode = 49
	KeyCodeKey_LeftShift      KeyCode = 50
	KeyCodeKey_LeftAlt        KeyCode = 51
	KeyCodeKey_LeftSuper      KeyCode = 52
	KeyCodeKey_RightCtrl      KeyCode = 53
	KeyCodeKey_RightShift     KeyCode = 54
	KeyCodeKey_RightAlt       KeyCode = 55
	KeyCodeKey_RightSuper     KeyCode = 56
	KeyCodeKey_Menu           KeyCode = 57
	KeyCodeKey_0              KeyCode = 58
	KeyCodeKey_1              KeyCode = 59
	KeyCodeKey_2              KeyCode = 60
	KeyCodeKey_3              KeyCode = 61
	KeyCodeKey_4              KeyCode = 62
	KeyCodeKey_5              KeyCode = 63
	KeyCodeKey_6              KeyCode = 64
	KeyCodeKey_7              KeyCode = 65
	KeyCodeKey_8              KeyCode = 66
	KeyCodeKey_9              KeyCode = 67
	KeyCodeKey_A              KeyCode = 68
	KeyCodeKey_B              KeyCode = 69
	KeyCodeKey_C              KeyCode = 70
	KeyCodeKey_D              KeyCode = 71
	KeyCodeKey_E              KeyCode = 72
	KeyCodeKey_F              KeyCode = 73
	KeyCodeKey_G              KeyCode = 74
	KeyCodeKey_H              KeyCode = 75
	KeyCodeKey_I              KeyCode = 76
	KeyCodeKey_J              KeyCode = 77
	KeyCodeKey_K              KeyCode = 78
	KeyCodeKey_L              KeyCode = 79
	KeyCodeKey_M              KeyCode = 80
	KeyCodeKey_N              KeyCode = 81
	KeyCodeKey_O              KeyCode = 82
	KeyCodeKey_P              KeyCode = 83
	KeyCodeKey_Q              KeyCode = 84
	KeyCodeKey_R              KeyCode = 85
	KeyCodeKey_S              KeyCode = 86
	KeyCodeKey_T              KeyCode = 87
	KeyCodeKey_U              KeyCode = 88
	KeyCodeKey_V              KeyCode = 89
	KeyCodeKey_W              KeyCode = 90
	KeyCodeKey_X              KeyCode = 91
	KeyCodeKey_Y              KeyCode = 92
	KeyCodeKey_Z              KeyCode = 93
	KeyCodeKey_F1             KeyCode = 94
	KeyCodeKey_F2             KeyCode = 95
	KeyCodeKey_F3             KeyCode = 96
	KeyCodeKey_F4             KeyCode = 97
	KeyCodeKey_F5             KeyCode = 98
	KeyCodeKey_F6             KeyCode = 99
	KeyCodeKey_F7             KeyCode = 100
	KeyCodeKey_F8             KeyCode = 101
	KeyCodeKey_F9             KeyCode = 102
	KeyCodeKey_F10            KeyCode = 103
	KeyCodeKey_F11            KeyCode = 104
	KeyCodeKey_F12            KeyCode = 105
	KeyCodeKey_F13            KeyCode = 106
	KeyCodeKey_F14            KeyCode = 107
	KeyCodeKey_F15            KeyCode = 108
	KeyCodeKey_F16            KeyCode = 109
	KeyCodeKey_F17            KeyCode = 110
	KeyCodeKey_F18            KeyCode = 111
	KeyCodeKey_F19            KeyCode = 112
	KeyCodeKey_F20            KeyCode = 113
	KeyCodeKey_F21            KeyCode = 114
	KeyCodeKey_F22            KeyCode = 115
	KeyCodeKey_F23            KeyCode = 116
	KeyCodeKey_F24            KeyCode = 117
	KeyCodeKey_AppBack        KeyCode = 118
	KeyCodeKey_AppForward     KeyCode = 119
)

var EnumNamesKeyCode = map[KeyCode]string{
	KeyCodeKey_None:           "Key_None",
	KeyCodeKey_Tab:            "Key_Tab",
	KeyCodeKey_LeftArrow:      "Key_LeftArrow",
	KeyCodeKey_RightArrow:     "Key_RightArrow",
	KeyCodeKey_UpArrow:        "Key_UpArrow",
	KeyCodeKey_DownArrow:      "Key_DownArrow",
	KeyCodeKey_PageUp:         "Key_PageUp",
	KeyCodeKey_PageDown:       "Key_PageDown",
	KeyCodeKey_Home:           "Key_Home",
	KeyCodeKey_End:            "Key_End",
	KeyCodeKey_Insert:         "Key_Insert",
	KeyCodeKey_Delete:         "Key_Delete",
	KeyCodeKey_Backspace:      "Key_Backspace",
	KeyCodeKey_Space:          "Key_Space",
	KeyCodeKey_Enter:          "Key_Enter",
	KeyCodeKey_Escape:         "Key_Escape",
	KeyCodeKey_Apostrophe:     "Key_Apostrophe",
	KeyCodeKey_Comma:          "Key_Comma",
	KeyCodeKey_Minus:          "Key_Minus",
	KeyCodeKey_Period:         "Key_Period",
	KeyCodeKey_Slash:          "Key_Slash",
	KeyCodeKey_Semicolon:      "Key_Semicolon",
	KeyCodeKey_Equal:          "Key_Equal",
	KeyCodeKey_LeftBracket:    "Key_LeftBracket",
	KeyCodeKey_Backslash:      "Key_Backslash",
	KeyCodeKey_RightBracket:   "Key_RightBracket",
	KeyCodeKey_GraveAccent:    "Key_GraveAccent",
	KeyCodeKey_CapsLock:       "Key_CapsLock",
	KeyCodeKey_ScrollLock:     "Key_ScrollLock",
	KeyCodeKey_NumLock:        "Key_NumLock",
	KeyCodeKey_PrintScreen:    "Key_PrintScreen",
	KeyCodeKey_Pause:          "Key_Pause",
	KeyCodeKey_Keypad0:        "Key_Keypad0",
	KeyCodeKey_Keypad1:        "Key_Keypad1",
	KeyCodeKey_Keypad2:        "Key_Keypad2",
	KeyCodeKey_Keypad3:        "Key_Keypad3",
	KeyCodeKey_Keypad4:        "Key_Keypad4",
	KeyCodeKey_Keypad5:        "Key_Keypad5",
	KeyCodeKey_Keypad6:        "Key_Keypad6",
	KeyCodeKey_Keypad7:        "Key_Keypad7",
	KeyCodeKey_Keypad8:        "Key_Keypad8",
	KeyCodeKey_Keypad9:        "Key_Keypad9",
	KeyCodeKey_KeypadDecimal:  "Key_KeypadDecimal",
	KeyCodeKey_KeypadDivide:   "Key_KeypadDivide",
	KeyCodeKey_KeypadMultiply: "Key_KeypadMultiply",
	KeyCodeKey_KeypadSubtract: "Key_KeypadSubtract",
	KeyCodeKey_KeypadAdd:      "Key_KeypadAdd",
	KeyCodeKey_KeypadEnter:    "Key_KeypadEnter",
	KeyCodeKey_KeypadEqual:    "Key_KeypadEqual",
	KeyCodeKey_LeftCtrl:       "Key_LeftCtrl",
	KeyCodeKey_LeftShift:      "Key_LeftShift",
	KeyCodeKey_LeftAlt:        "Key_LeftAlt",
	KeyCodeKey_LeftSuper:      "Key_LeftSuper",
	KeyCodeKey_RightCtrl:      "Key_RightCtrl",
	KeyCodeKey_RightShift:     "Key_RightShift",
	KeyCodeKey_RightAlt:       "Key_RightAlt",
	KeyCodeKey_RightSuper:     "Key_RightSuper",
	KeyCodeKey_Menu:           "Key_Menu",
	KeyCodeKey_0:              "Key_0",
	KeyCodeKey_1:              "Key_1",
	KeyCodeKey_2:              "Key_2",
	KeyCodeKey_3:              "Key_3",
	KeyCodeKey_4:              "Key_4",
	KeyCodeKey_5:              "Key_5",
	KeyCodeKey_6:              "Key_6",
	KeyCodeKey_7:              "Key_7",
	KeyCodeKey_8:              "Key_8",
	KeyCodeKey_9:              "Key_9",
	KeyCodeKey_A:              "Key_A",
	KeyCodeKey_B:              "Key_B",
	KeyCodeKey_C:              "Key_C",
	KeyCodeKey_D:              "Key_D",
	KeyCodeKey_E:              "Key_E",
	KeyCodeKey_F:              "Key_F",
	KeyCodeKey_G:              "Key_G",
	KeyCodeKey_H:              "Key_H",
	KeyCodeKey_I:              "Key_I",
	KeyCodeKey_J:              "Key_J",
	KeyCodeKey_K:              "Key_K",
	KeyCodeKey_L:              "Key_L",
	KeyCodeKey_M:              "Key_M",
	KeyCodeKey_N:              "Key_N",
	KeyCodeKey_O:              "Key_O",
	KeyCodeKey_P:              "Key_P",
	KeyCodeKey_Q:              "Key_Q",
	KeyCodeKey_R:              "Key_R",
	KeyCodeKey_S:              "Key_S",
	KeyCodeKey_T:              "Key_T",
	KeyCodeKey_U:              "Key_U",
	KeyCodeKey_V:              "Key_V",
	KeyCodeKey_W:              "Key_W",
	KeyCodeKey_X:              "Key_X",
	KeyCodeKey_Y:              "Key_Y",
	KeyCodeKey_Z:              "Key_Z",
	KeyCodeKey_F1:             "Key_F1",
	KeyCodeKey_F2:             "Key_F2",
	KeyCodeKey_F3:             "Key_F3",
	KeyCodeKey_F4:             "Key_F4",
	KeyCodeKey_F5:             "Key_F5",
	KeyCodeKey_F6:             "Key_F6",
	KeyCodeKey_F7:             "Key_F7",
	KeyCodeKey_F8:             "Key_F8",
	KeyCodeKey_F9:             "Key_F9",
	KeyCodeKey_F10:            "Key_F10",
	KeyCodeKey_F11:            "Key_F11",
	KeyCodeKey_F12:            "Key_F12",
	KeyCodeKey_F13:            "Key_F13",
	KeyCodeKey_F14:            "Key_F14",
	KeyCodeKey_F15:            "Key_F15",
	KeyCodeKey_F16:            "Key_F16",
	KeyCodeKey_F17:            "Key_F17",
	KeyCodeKey_F18:            "Key_F18",
	KeyCodeKey_F19:            "Key_F19",
	KeyCodeKey_F20:            "Key_F20",
	KeyCodeKey_F21:            "Key_F21",
	KeyCodeKey_F22:            "Key_F22",
	KeyCodeKey_F23:            "Key_F23",
	KeyCodeKey_F24:            "Key_F24",
	KeyCodeKey_AppBack:        "Key_AppBack",
	KeyCodeKey_AppForward:     "Key_AppForward",
}

var EnumValuesKeyCode = map[string]KeyCode{
	"Key_None":           KeyCodeKey_None,
	"Key_Tab":            KeyCodeKey_Tab,
	"Key_LeftArrow":      KeyCodeKey_LeftArrow,
	"Key_RightArrow":     KeyCodeKey_RightArrow,
	"Key_UpArrow":        KeyCodeKey_UpArrow,
	"Key_DownArrow":      KeyCodeKey_DownArrow,
	"Key_PageUp":         KeyCodeKey_PageUp,
	"Key_PageDown":       KeyCodeKey_PageDown,
	"Key_Home":           KeyCodeKey_Home,
	"Key_End":            KeyCodeKey_End,
	"Key_Insert":         KeyCodeKey_Insert,
	"Key_Delete":         KeyCodeKey_Delete,
	"Key_Backspace":      KeyCodeKey_Backspace,
	"Key_Space":          KeyCodeKey_Space,
	"Key_Enter":          KeyCodeKey_Enter,
	"Key_Escape":         KeyCodeKey_Escape,
	"Key_Apostrophe":     KeyCodeKey_Apostrophe,
	"Key_Comma":          KeyCodeKey_Comma,
	"Key_Minus":          KeyCodeKey_Minus,
	"Key_Period":         KeyCodeKey_Period,
	"Key_Slash":          KeyCodeKey_Slash,
	"Key_Semicolon":      KeyCodeKey_Semicolon,
	"Key_Equal":          KeyCodeKey_Equal,
	"Key_LeftBracket":    KeyCodeKey_LeftBracket,
	"Key_Backslash":      KeyCodeKey_Backslash,
	"Key_RightBracket":   KeyCodeKey_RightBracket,
	"Key_GraveAccent":    KeyCodeKey_GraveAccent,
	"Key_CapsLock":       KeyCodeKey_CapsLock,
	"Key_ScrollLock":     KeyCodeKey_ScrollLock,
	"Key_NumLock":        KeyCodeKey_NumLock,
	"Key_PrintScreen":    KeyCodeKey_PrintScreen,
	"Key_Pause":          KeyCodeKey_Pause,
	"Key_Keypad0":        KeyCodeKey_Keypad0,
	"Key_Keypad1":        KeyCodeKey_Keypad1,
	"Key_Keypad2":        KeyCodeKey_Keypad2,
	"Key_Keypad3":        KeyCodeKey_Keypad3,
	"Key_Keypad4":        KeyCodeKey_Keypad4,
	"Key_Keypad5":        KeyCodeKey_Keypad5,
	"Key_Keypad6":        KeyCodeKey_Keypad6,
	"Key_Keypad7":        KeyCodeKey_Keypad7,
	"Key_Keypad8":        KeyCodeKey_Keypad8,
	"Key_Keypad9":        KeyCodeKey_Keypad9,
	"Key_KeypadDecimal":  KeyCodeKey_KeypadDecimal,
	"Key_KeypadDivide":   KeyCodeKey_KeypadDivide,
	"Key_KeypadMultiply": KeyCodeKey_KeypadMultiply,
	"Key_KeypadSubtract": KeyCodeKey_KeypadSubtract,
	"Key_KeypadAdd":      KeyCodeKey_KeypadAdd,
	"Key_KeypadEnter":    KeyCodeKey_KeypadEnter,
	"Key_KeypadEqual":    KeyCodeKey_KeypadEqual,
	"Key_LeftCtrl":       KeyCodeKey_LeftCtrl,
	"Key_LeftShift":      KeyCodeKey_LeftShift,
	"Key_LeftAlt":        KeyCodeKey_LeftAlt,
	"Key_LeftSuper":      KeyCodeKey_LeftSuper,
	"Key_RightCtrl":      KeyCodeKey_RightCtrl,
	"Key_RightShift":     KeyCodeKey_RightShift,
	"Key_RightAlt":       KeyCodeKey_RightAlt,
	"Key_RightSuper":     KeyCodeKey_RightSuper,
	"Key_Menu":           KeyCodeKey_Menu,
	"Key_0":              KeyCodeKey_0,
	"Key_1":              KeyCodeKey_1,
	"Key_2":              KeyCodeKey_2,
	"Key_3":              KeyCodeKey_3,
	"Key_4":              KeyCodeKey_4,
	"Key_5":              KeyCodeKey_5,
	"Key_6":              KeyCodeKey_6,
	"Key_7":              KeyCodeKey_7,
	"Key_8":              KeyCodeKey_8,
	"Key_9":              KeyCodeKey_9,
	"Key_A":              KeyCodeKey_A,
	"Key_B":              KeyCodeKey_B,
	"Key_C":              KeyCodeKey_C,
	"Key_D":              KeyCodeKey_D,
	"Key_E":              KeyCodeKey_E,
	"Key_F":              KeyCodeKey_F,
	"Key_G":              KeyCodeKey_G,
	"Key_H":              KeyCodeKey_H,
	"Key_I":              KeyCodeKey_I,
	"Key_J":              KeyCodeKey_J,
	"Key_K":              KeyCodeKey_K,
	"Key_L":              KeyCodeKey_L,
	"Key_M":              KeyCodeKey_M,
	"Key_N":              KeyCodeKey_N,
	"Key_O":              KeyCodeKey_O,
	"Key_P":              KeyCodeKey_P,
	"Key_Q":              KeyCodeKey_Q,
	"Key_R":              KeyCodeKey_R,
	"Key_S":              KeyCodeKey_S,
	"Key_T":              KeyCodeKey_T,
	"Key_U":              KeyCodeKey_U,
	"Key_V":              KeyCodeKey_V,
	"Key_W":              KeyCodeKey_W,
	"Key_X":              KeyCodeKey_X,
	"Key_Y":              KeyCodeKey_Y,
	"Key_Z":              KeyCodeKey_Z,
	"Key_F1":             KeyCodeKey_F1,
	"Key_F2":             KeyCodeKey_F2,
	"Key_F3":             KeyCodeKey_F3,
	"Key_F4":             KeyCodeKey_F4,
	"Key_F5":             KeyCodeKey_F5,
	"Key_F6":             KeyCodeKey_F6,
	"Key_F7":             KeyCodeKey_F7,
	"Key_F8":             KeyCodeKey_F8,
	"Key_F9":             KeyCodeKey_F9,
	"Key_F10":            KeyCodeKey_F10,
	"Key_F11":            KeyCodeKey_F11,
	"Key_F12":            KeyCodeKey_F12,
	"Key_F13":            KeyCodeKey_F13,
	"Key_F14":            KeyCodeKey_F14,
	"Key_F15":            KeyCodeKey_F15,
	"Key_F16":            KeyCodeKey_F16,
	"Key_F17":            KeyCodeKey_F17,
	"Key_F18":            KeyCodeKey_F18,
	"Key_F19":            KeyCodeKey_F19,
	"Key_F20":            KeyCodeKey_F20,
	"Key_F21":            KeyCodeKey_F21,
	"Key_F22":            KeyCodeKey_F22,
	"Key_F23":            KeyCodeKey_F23,
	"Key_F24":            KeyCodeKey_F24,
	"Key_AppBack":        KeyCodeKey_AppBack,
	"Key_AppForward":     KeyCodeKey_AppForward,
}

func (v KeyCode) String() string {
	if s, ok := EnumNamesKeyCode[v]; ok {
		return s
	}
	return "KeyCode(" + strconv.FormatInt(int64(v), 10) + ")"
}