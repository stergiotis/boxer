//go:build !wasip1

package browserhost

// InstallHostTransport is a no-op where the module has sockets of its own;
// see transport_wasip1.go for the tab.
func InstallHostTransport() {}
