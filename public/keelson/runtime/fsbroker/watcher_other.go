//go:build !linux

package fsbroker

import "time"

// pickNativeBackend without inotify: every watch polls. This is the shape
// under wasm (ADR-0077 SD8), where no file-change notification exists and
// the broker's file system is whatever the host mounts.
func pickNativeBackend(path string, interval time.Duration, recursive bool) (b watcherBackendI, name string, err error) {
	b, err = newPollerWatcher(path, interval, recursive)
	name = "poller"
	return
}
