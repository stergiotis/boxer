//go:build unix

package application

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/thestack/imzero2/hostwake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWakerWritesWakesToTheClientsFifo(t *testing.T) {
	cmd := exec.Command("true")
	w, err := newWaker(cmd)
	require.NoError(t, err)
	require.NotNil(t, w)
	var named string
	for _, kv := range cmd.Env {
		if v, ok := strings.CutPrefix(kv, wakeFifoEnv+"="); ok {
			named = v
		}
	}
	assert.Equal(t, w.path, named, "the client is told where the fifo is")

	go w.run()
	hostwake.Set(w.wake)
	r, err := os.Open(w.path) // what the client does
	require.NoError(t, err)
	got := make(chan int, 4)
	go func() {
		b := make([]byte, 16)
		for {
			n, e := r.Read(b)
			if e != nil {
				close(got)
				return
			}
			got <- n
		}
	}()
	hostwake.Wake()
	select {
	case n := <-got:
		assert.GreaterOrEqual(t, n, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("no byte reached the client")
	}

	require.NoError(t, w.Close())
	// The writer's end closes, so the client's read ends.
	select {
	case _, open := <-got:
		for open {
			_, open = <-got
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing did not end the client's read")
	}
	_ = r.Close()
	_, err = os.Stat(w.dir)
	assert.True(t, os.IsNotExist(err), "the fifo's directory is removed")
	hostwake.Wake() // uninstalled: a no-op, not a panic
}

func TestClosingAWakerTheClientNeverOpenedDoesNotHang(t *testing.T) {
	w, err := newWaker(exec.Command("true"))
	require.NoError(t, err)
	go w.run()
	done := make(chan struct{})
	go func() { _ = w.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a writer waiting for the client")
	}
}
