package browserhost

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The export's goroutine counts as blocked only when its dump header names a
// parked state; running, runnable and a missing goroutine are not blocked.
func TestExportBlocked(t *testing.T) {
	dump := []byte("goroutine 1 [runnable]:\nruntime.Gosched()\n\n" +
		"goroutine 9 [running]:\nmain.f()\n\n" +
		"goroutine 12 [chan receive, 2 minutes]:\nmain.g()\n\n" +
		"goroutine 13 [sync.Mutex.Lock]:\nmain.h()\n")
	require.False(t, exportBlocked(dump, 1))
	require.False(t, exportBlocked(dump, 9))
	require.True(t, exportBlocked(dump, 12))
	require.True(t, exportBlocked(dump, 13))
	require.False(t, exportBlocked(dump, 99), "not in the dump")
	require.False(t, exportBlocked(dump, 3), "goroutine 3 is not goroutine 13")
}
