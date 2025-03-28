// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import "strconv"

type TextAlignFlags byte

const (
	TextAlignFlagsLeft    TextAlignFlags = 0
	TextAlignFlagsRight   TextAlignFlags = 1
	TextAlignFlagsCenter  TextAlignFlags = 2
	TextAlignFlagsJustify TextAlignFlags = 3
)

var EnumNamesTextAlignFlags = map[TextAlignFlags]string{
	TextAlignFlagsLeft:    "Left",
	TextAlignFlagsRight:   "Right",
	TextAlignFlagsCenter:  "Center",
	TextAlignFlagsJustify: "Justify",
}

var EnumValuesTextAlignFlags = map[string]TextAlignFlags{
	"Left":    TextAlignFlagsLeft,
	"Right":   TextAlignFlagsRight,
	"Center":  TextAlignFlagsCenter,
	"Justify": TextAlignFlagsJustify,
}

func (v TextAlignFlags) String() string {
	if s, ok := EnumNamesTextAlignFlags[v]; ok {
		return s
	}
	return "TextAlignFlags(" + strconv.FormatInt(int64(v), 10) + ")"
}
