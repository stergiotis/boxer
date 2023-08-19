package eh

import (
	"bytes"
	"encoding/binary"
	"github.com/pkg/errors"
)

func toBinaryRepresentation(st errors.StackTrace) []byte {
	l := len(st)
	r := make([]byte, 0, l*8)
	for i := l - 1; i >= 0; i-- {
		r = binary.LittleEndian.AppendUint64(r, uint64(st[i]))
	}
	return r
}
func isSubStack(binRepStackA []byte, binRepStackB []byte) bool {
	a := len(binRepStackA)
	b := len(binRepStackB)
	if a >= b {
		return false
	}
	return bytes.Equal(binRepStackA, binRepStackB[:a])
}
