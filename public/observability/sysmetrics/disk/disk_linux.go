//go:build linux

package disk

import (
	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// realStatfs is the production [StatfsFunc].
func realStatfs(path string) (cap sysmsnap.DiskCapacity, err error) {
	var s unix.Statfs_t
	err = unix.Statfs(path, &s)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("statfs: %w", err)
		return
	}
	cap = computeCapacity(uint64(s.Bsize), s.Blocks, s.Bavail)
	return
}
