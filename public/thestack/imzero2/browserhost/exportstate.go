package browserhost

import (
	"bytes"
	"strconv"
)

// exportBlocked reads, from an all-goroutines stack dump, whether goroutine
// id is parked rather than running or waiting to run. Not found counts as not
// blocked: the wait goes on.
func exportBlocked(dump []byte, id uint64) bool {
	head := []byte("goroutine " + strconv.FormatUint(id, 10) + " [")
	i := bytes.Index(dump, head)
	if i < 0 {
		return false
	}
	state := dump[i+len(head):]
	if j := bytes.IndexAny(state, "],"); j >= 0 {
		state = state[:j]
	}
	switch string(state) {
	case "running", "runnable", "syscall":
		return false
	}
	return true
}
