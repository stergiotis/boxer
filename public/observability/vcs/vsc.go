package vcs

import (
	"runtime/debug"
	"strings"
)

func BuildVersionInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "<no-build-info-available>"
	}
	strs := make([]string, 0, 16)
	for _, setting := range info.Settings {
		if strings.HasPrefix(setting.Key, "vcs") {
			strs = append(strs, setting.Key+": "+setting.Value)
		}
	}
	return strings.Join(strs, ";")
}
