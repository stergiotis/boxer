// Package sysfs is a small set of read-only primitives over a /sys-shaped
// directory tree. It mirrors [procfs] in shape (root-injectable [Reader],
// ReadFile + ReadString convenience, directory enumeration) but its access
// patterns are tuned to /sys idioms — leaf files holding a single trimmed
// scalar, sibling label files describing the leaf, and class-rooted
// enumeration like /sys/class/hwmon/hwmon*.
//
// All returned strings are trimmed of the trailing newline kernel files
// universally append.
package sysfs
