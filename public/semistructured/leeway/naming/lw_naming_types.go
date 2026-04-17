package naming

import "fmt"

// StylableName is a name that can be transformed to other naming styles without losing its descriptive, referencing, and uniqueness properties.
type StylableName string

var _ fmt.Stringer = StylableName("")

type Key string

var _ fmt.Stringer = Key("")

type NamingStyleE uint8

var _ fmt.Stringer = NamingStyleE(0)
