//go:build !unix

package application

import (
	"errors"
	"github.com/stergiotis/boxer/public/observability/eh"
)

const fifoSupported = false

const nonBlockFlag = 0

// errNoReader is never returned where there are no FIFOs.
var errNoReader = errors.New("no fifo on this platform")

func makeFifo(path string) error {
	return eh.Errorf("no fifo on this platform: %s", path)
}
