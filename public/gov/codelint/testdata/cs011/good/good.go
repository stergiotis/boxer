package good

import "os"

// Non-env os calls are fine.
func ok() (path string) {
	path = os.TempDir()
	return
}
