//go:build linux

package fsbroker_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// A handle op that blocks in the filesystem — here a read of a FIFO nobody
// writes to — must not hold the requester past its own timeout: the op runs
// off the requester's goroutine, so the timeout the caller passes applies.
func TestService_Handle_BlockedReadTimesOut(t *testing.T) {
	inst := inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(time.Second)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()
	appBus := inst.NewClient("test.fifo", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
	})
	fifo := filepath.Join(t.TempDir(), "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	dr := resolveDialog(t, svc, appBus, fsbroker.SubjectDialogRead, "read", fifo)

	done := make(chan error, 1)
	go func() {
		_, rerr := appBus.RequestWithTimeout(dr.HandleSubjectPrefix+".read", nil, 100*time.Millisecond)
		done <- rerr
	}()
	select {
	case rerr := <-done:
		require.Error(t, rerr, "the read cannot complete, so the request must time out")
	case <-time.After(5 * time.Second):
		// Unblock the broker's open so the goroutine does not outlive the test.
		if w, oerr := os.OpenFile(fifo, os.O_WRONLY, 0); oerr == nil {
			_ = w.Close()
		}
		t.Fatal("the request outlived its 100ms timeout: the handle op ran on the requester's goroutine")
	}
	// Release the broker's blocked open: a writer that closes at once gives
	// the reader EOF.
	w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}
