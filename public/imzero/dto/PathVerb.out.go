// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import "strconv"

type PathVerb byte

const (
	PathVerbMove  PathVerb = 0
	PathVerbLine  PathVerb = 1
	PathVerbQuad  PathVerb = 2
	PathVerbConic PathVerb = 3
	PathVerbCubic PathVerb = 4
	PathVerbClose PathVerb = 5
	PathVerbDone  PathVerb = 6
)

var EnumNamesPathVerb = map[PathVerb]string{
	PathVerbMove:  "Move",
	PathVerbLine:  "Line",
	PathVerbQuad:  "Quad",
	PathVerbConic: "Conic",
	PathVerbCubic: "Cubic",
	PathVerbClose: "Close",
	PathVerbDone:  "Done",
}

var EnumValuesPathVerb = map[string]PathVerb{
	"Move":  PathVerbMove,
	"Line":  PathVerbLine,
	"Quad":  PathVerbQuad,
	"Conic": PathVerbConic,
	"Cubic": PathVerbCubic,
	"Close": PathVerbClose,
	"Done":  PathVerbDone,
}

func (v PathVerb) String() string {
	if s, ok := EnumNamesPathVerb[v]; ok {
		return s
	}
	return "PathVerb(" + strconv.FormatInt(int64(v), 10) + ")"
}
