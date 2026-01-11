package vcs

import (
	"runtime/debug"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

const NoBuildInfo = "<no-build-info-available>"

func GetVcsRevision() (revision string, modified bool, err error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		err = eh.Errorf("no build information available")
		return
	}
	found := 0
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
			found++
			if found == 2 {
				return
			}
			break
		case "vcs.modified":
			modified = setting.Value == "true"
			found++
			if found == 2 {
				return
			}
			break
		}
	}
	return
}

// BuildVersionInfo Human readable build info (vcs fields)
func BuildVersionInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return NoBuildInfo
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
		return NoBuildInfo
	}
	return info.Path
}

const author = "Panos Stergiotis"
const copyrightYears = "2023, 2024, 2025, 2026"

func CopyrightInfo() string {
	return "Copyright © " + copyrightYears + " " + author
}
