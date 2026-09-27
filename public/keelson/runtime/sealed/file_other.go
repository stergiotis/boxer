//go:build !linux

package sealed

import (
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// openUnnamed needs O_TMPFILE, which only Linux has: a sealed file is
// unnamed by construction, and a named temporary file that is unlinked
// after creation is not the same promise. Callers probe for ErrUnsupported
// and take their fallback (ADR-0240).
func openUnnamed(dir string) (f *os.File, err error) {
	return nil, eb.Build().Str("dir", dir).Errorf("sealed: %w: unnamed files need O_TMPFILE", ErrUnsupported)
}
