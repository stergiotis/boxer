package naming

import "fmt"

// StylableName a name that can be transformed to other naming styles without loosing is descriptive, referencing and uniqueness properties
type StylableName string

var _ fmt.Stringer = StylableName("")

type Key string

var _ fmt.Stringer = Key("")

type NamingStyleE uint8

var _ fmt.Stringer = NamingStyleE(0)
