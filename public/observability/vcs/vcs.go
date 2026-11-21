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
		case "vcs.modified":
			modified = setting.Value == "true"
			found++
			if found == 2 {
				return
			}
		}
	}
	return
}

// BuildVersionInfo returns a human-readable build-info string (vcs fields).
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

// BoxerModulePath is this module's path.
const BoxerModulePath = "github.com/stergiotis/boxer"

// BoxerModuleVersion names the version of this module the running binary was
// built from, as "<module>@<version>": the version of the dependency (its
// replacement's, when replaced) when another module is the main module; when
// this module is the main module, its release tag, or "(devel)" for a build
// of an untagged or modified tree. Without build info it is NoBuildInfo.
//
// The result depends on how the binary was built, so a code generator does
// not stamp it into the files it writes: one generator at one commit would
// write different bytes as a dependency and as the main module.
func BoxerModuleVersion() (s string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return NoBuildInfo
	}
	version := ""
	if info.Main.Path == BoxerModulePath {
		version = mainModuleVersion(info.Main.Version)
	} else {
		for _, dep := range info.Deps {
			if dep.Path != BoxerModulePath {
				continue
			}
			version = dep.Version
			if dep.Replace != nil {
				version = dep.Replace.Version
				if version == "" {
					version = "(replaced)"
				}
			}
			break
		}
	}
	if version == "" {
		version = "(unknown)"
	}
	return BoxerModulePath + "@" + version
}

// mainModuleVersion keeps a clean release tag and maps a pseudo-version, a
// "+dirty" build or no version to "(devel)".
func mainModuleVersion(v string) (out string) {
	if v == "" || strings.Contains(v, "+") || isPseudoVersion(v) {
		return "(devel)"
	}
	return v
}

// isPseudoVersion reports whether v ends in the "yyyymmddhhmmss-abcdefabcdef"
// suffix the go command gives an untagged commit.
func isPseudoVersion(v string) (b bool) {
	i := strings.LastIndexByte(v, '-')
	if i < 15 || len(v)-i-1 != 12 {
		return false
	}
	ts := v[i-14 : i]
	if v[i-15] != '-' && v[i-15] != '.' {
		return false
	}
	for _, c := range ts {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
