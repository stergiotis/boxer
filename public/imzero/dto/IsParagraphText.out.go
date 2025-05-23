// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import "strconv"

type IsParagraphText byte

const (
	IsParagraphTextNever  IsParagraphText = 0
	IsParagraphTextAlways IsParagraphText = 1
	IsParagraphTextAuto   IsParagraphText = 2
)

var EnumNamesIsParagraphText = map[IsParagraphText]string{
	IsParagraphTextNever:  "Never",
	IsParagraphTextAlways: "Always",
	IsParagraphTextAuto:   "Auto",
}

var EnumValuesIsParagraphText = map[string]IsParagraphText{
	"Never":  IsParagraphTextNever,
	"Always": IsParagraphTextAlways,
	"Auto":   IsParagraphTextAuto,
}

func (v IsParagraphText) String() string {
	if s, ok := EnumNamesIsParagraphText[v]; ok {
		return s
	}
	return "IsParagraphText(" + strconv.FormatInt(int64(v), 10) + ")"
}
