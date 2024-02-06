//go:build !bootstrap

package implot

import "strings"

func MakeNullSeparatedStringArray(strs ...string) NullSeparatedStringArray {
	return NullSeparatedStringArray(strings.Join(strs, "\u0000"))
}
