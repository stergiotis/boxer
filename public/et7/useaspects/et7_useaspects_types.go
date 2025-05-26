package useaspects

import "fmt"

type EncodedEt7AspectSet string

var _ fmt.Stringer = EncodedEt7AspectSet("")

type CanonicalEt7AspectCoder struct {
}

type DataAspectE uint8

var _ fmt.Stringer = DataAspectE(0)
