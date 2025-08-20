package runtime

import "fmt"

type EntityStateE uint8

var _ fmt.Stringer = EntityStateE(0)
