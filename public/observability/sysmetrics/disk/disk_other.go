//go:build !linux

package disk

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// ErrStatfsUnsupported is what realStatfs returns where statfs(2) is not
// available (wasm, ADR-0077 SD8): the collector reports no capacity for
// such a mount and the rest of the snapshot stands.
var ErrStatfsUnsupported = errors.New("disk: statfs unsupported on this platform")

func realStatfs(path string) (cap sysmsnap.DiskCapacity, err error) {
	err = ErrStatfsUnsupported
	return
}
