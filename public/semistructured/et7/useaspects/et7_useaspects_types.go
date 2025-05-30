package useaspects

import "fmt"

type AspectSet string

var _ fmt.Stringer = AspectSet("")

type CanonicalEt7AspectCoder struct {
}

type AspectE uint8

var _ fmt.Stringer = AspectE(0)
