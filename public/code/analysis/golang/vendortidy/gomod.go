package vendortidy

import (
	"go/version"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// FormatGoMod returns the go.mod src with the result's go line and
// requirements, laid out as cmd/go lays them out for that go version
// (ADR-0384 §SD2). Every other directive is kept.
func FormatGoMod(src []byte, res Result) (out []byte, err error) {
	var mf *modfile.File
	mf, err = modfile.Parse("go.mod", src, nil)
	if err != nil {
		err = eb.Build().Errorf("parse go.mod: %w", err)
		return
	}
	if res.GoVersion != "" && (mf.Go == nil || mf.Go.Version != res.GoVersion) {
		err = mf.AddGoStmt(res.GoVersion)
		if err != nil {
			err = eb.Build().Str("go", res.GoVersion).Errorf("set go line: %w", err)
			return
		}
	}
	goVersion := "1.16"
	if mf.Go != nil {
		goVersion = mf.Go.Version
	}
	if mf.Toolchain != nil {
		tv := strings.TrimPrefix(mf.Toolchain.Name, "go")
		if mf.Toolchain.Name == "go"+goVersion || version.Compare("go"+tv, "go1.21") < 0 {
			mf.DropToolchainStmt()
		}
	}
	reqs := make([]*modfile.Require, 0, len(res.Requires))
	for _, r := range res.Requires {
		reqs = append(reqs, &modfile.Require{Mod: module.Version{Path: r.Path, Version: r.Version}, Indirect: r.Indirect})
	}
	switch {
	case version.Compare("go"+goVersion, "go1.17") < 0:
		mf.SetRequire(reqs)
	case version.Compare("go"+goVersion, "go1.27") < 0:
		mf.SetRequireSeparateIndirect(reqs)
	default:
		mf.SetRequireAtMostTwo(reqs)
	}
	mf.Cleanup()
	out, err = mf.Format()
	if err != nil {
		err = eb.Build().Errorf("format go.mod: %w", err)
	}
	return
}

// VersionChangeKindE says how one requirement differs between a go.mod and a
// result.
type VersionChangeKindE uint8

const (
	VersionChangeKindAdded VersionChangeKindE = iota + 1
	VersionChangeKindRemoved
	VersionChangeKindUpgraded
	VersionChangeKindDowngraded
	VersionChangeKindMarking
)

var AllVersionChangeKinds = []VersionChangeKindE{
	VersionChangeKindAdded, VersionChangeKindRemoved, VersionChangeKindUpgraded, VersionChangeKindDowngraded, VersionChangeKindMarking,
}

func (inst VersionChangeKindE) String() string {
	switch inst {
	case VersionChangeKindAdded:
		return "added"
	case VersionChangeKindRemoved:
		return "removed"
	case VersionChangeKindUpgraded:
		return "upgraded"
	case VersionChangeKindDowngraded:
		return "downgraded"
	case VersionChangeKindMarking:
		return "marking"
	}
	return "unknown"
}

// VersionChange is one requirement that differs.
type VersionChange struct {
	Path       string
	OldVersion string
	NewVersion string
	Kind       VersionChangeKindE
	Indirect   bool
}

// CompareRequires lists how the result's requirements differ from those of
// the go.mod src: added, removed, moved up or down, or only re-marked
// direct/indirect.
func CompareRequires(src []byte, res Result) (changes []VersionChange, err error) {
	var mf *modfile.File
	mf, err = modfile.Parse("go.mod", src, nil)
	if err != nil {
		err = eb.Build().Errorf("parse go.mod: %w", err)
		return
	}
	old := make(map[string]*modfile.Require, len(mf.Require))
	for _, r := range mf.Require {
		old[r.Mod.Path] = r
	}
	changes = make([]VersionChange, 0, 16)
	for _, r := range res.Requires {
		o, found := old[r.Path]
		delete(old, r.Path)
		c := VersionChange{Path: r.Path, NewVersion: r.Version, Indirect: r.Indirect}
		switch {
		case !found:
			c.Kind = VersionChangeKindAdded
		case o.Mod.Version != r.Version:
			c.OldVersion = o.Mod.Version
			c.Kind = VersionChangeKindUpgraded
			if semver.Compare(r.Version, o.Mod.Version) < 0 {
				c.Kind = VersionChangeKindDowngraded
			}
		case o.Indirect != r.Indirect:
			c.OldVersion = o.Mod.Version
			c.Kind = VersionChangeKindMarking
		default:
			continue
		}
		changes = append(changes, c)
	}
	for _, r := range mf.Require {
		if _, gone := old[r.Mod.Path]; gone {
			changes = append(changes, VersionChange{Path: r.Mod.Path, OldVersion: r.Mod.Version, Kind: VersionChangeKindRemoved, Indirect: r.Indirect})
		}
	}
	return
}
