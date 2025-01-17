// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import "strconv"

type PathFillType byte

const (
	PathFillTypeWinding        PathFillType = 0
	PathFillTypeEvenOdd        PathFillType = 1
	PathFillTypeInverseWinding PathFillType = 2
	PathFillTypeInverseEvenOdd PathFillType = 3
)

var EnumNamesPathFillType = map[PathFillType]string{
	PathFillTypeWinding:        "Winding",
	PathFillTypeEvenOdd:        "EvenOdd",
	PathFillTypeInverseWinding: "InverseWinding",
	PathFillTypeInverseEvenOdd: "InverseEvenOdd",
}

var EnumValuesPathFillType = map[string]PathFillType{
	"Winding":        PathFillTypeWinding,
	"EvenOdd":        PathFillTypeEvenOdd,
	"InverseWinding": PathFillTypeInverseWinding,
	"InverseEvenOdd": PathFillTypeInverseEvenOdd,
}

func (v PathFillType) String() string {
	if s, ok := EnumNamesPathFillType[v]; ok {
		return s
	}
	return "PathFillType(" + strconv.FormatInt(int64(v), 10) + ")"
}
