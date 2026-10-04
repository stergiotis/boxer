//go:build !wasip1

package tabhost

// registerReactor has nothing to register natively: Main runs the program.
func registerReactor(*Program) {}
