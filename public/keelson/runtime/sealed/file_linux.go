//go:build linux

package sealed

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// openUnnamed allocates a file under dir that never appears in it
// (O_TMPFILE); a filesystem without the flag is ErrUnsupported.
func openUnnamed(dir string) (f *os.File, err error) {
	fd, err := unix.Open(dir, unix.O_TMPFILE|unix.O_RDWR|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
	if err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EISDIR) || errors.Is(err, unix.ENOTSUP) {
			return nil, eb.Build().Str("dir", dir).Errorf("sealed: %w: %w", ErrUnsupported, err)
		}
		return nil, eb.Build().Str("dir", dir).Errorf("sealed: allocate unnamed file: %w", err)
	}
	f = os.NewFile(uintptr(fd), "sealed")
	return
}
