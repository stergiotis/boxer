package encodingaspects

import "fmt"

type EncodedEt7AspectSet string

var _ fmt.Stringer = EncodedEt7AspectSet("")

type DataAspectE uint8

var _ fmt.Stringer = DataAspectE(0)
