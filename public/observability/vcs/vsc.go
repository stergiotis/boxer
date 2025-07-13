package vcs

import (
	"runtime/debug"
	"strings"
)

const noBuildInfo = "<no-build-info-available>"

func BuildVersionInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return noBuildInfo
	}
	strs := make([]string, 0, 16)
	for _, setting := range info.Settings {
		if strings.HasPrefix(setting.Key, "vcs") {
			strs = append(strs, setting.Key+": "+setting.Value)
		}
	}
	return strings.Join(strs, ";")
}
func ModuleInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return noBuildInfo
	}
	return info.Path
}

const author = "Panos Stergiotis"
const copyrightYears = "2023, 2024, 2025"

func CopyrightInfo() string {
	return "Copyright © " + copyrightYears + " " + author
}
