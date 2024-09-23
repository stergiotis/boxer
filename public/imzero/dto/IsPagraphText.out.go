// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import "strconv"

type IsPagraphText byte

const (
	IsPagraphTextnever  IsPagraphText = 0
	IsPagraphTextalways IsPagraphText = 1
	IsPagraphTextauto   IsPagraphText = 2
)

var EnumNamesIsPagraphText = map[IsPagraphText]string{
	IsPagraphTextnever:  "never",
	IsPagraphTextalways: "always",
	IsPagraphTextauto:   "auto",
}

var EnumValuesIsPagraphText = map[string]IsPagraphText{
	"never":  IsPagraphTextnever,
	"always": IsPagraphTextalways,
	"auto":   IsPagraphTextauto,
}

func (v IsPagraphText) String() string {
	if s, ok := EnumNamesIsPagraphText[v]; ok {
		return s
	}
	return "IsPagraphText(" + strconv.FormatInt(int64(v), 10) + ")"
}
