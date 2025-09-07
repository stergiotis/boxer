package units

import "fmt"

type AspectSet string

var _ fmt.Stringer = AspectSet("")

type AspectE uint8

var _ fmt.Stringer = AspectE(0)
