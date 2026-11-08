//go:build !unix

package application

import "github.com/stergiotis/boxer/public/observability/eh"

const fifoSupported = false

const nonBlockFlag = 0

func makeFifo(path string) error {
	return eh.Errorf("no fifo on this platform: %s", path)
}
