package splashscreen

import (
	"runtime/debug"
)

// party is one copyright holder the splash window credits: boxer, the
// library (boxer.go), and, when one is registered, the program that links
// it (program.go). A party's tab draws only what its own entry carries, so
// one party's copyright, licence or NOTICE never shows under another's
// name. The running binary's build provenance belongs to neither and has a
// tab of its own.
type party struct {
	// key is stable: it names the party's tab and scopes its texture.
	key  string
	name string
	icon string
	// module is the party's Go module path; the About page shows the
	// version the build info records for it. Empty for none.
	module    string
	copyright string
	license   string
	notice    string
	// image returns the party's artwork; nil when it has none, and then
	// the party's tab has no Splash page.
	image func() (img decodedImage, err error)
}

// decodedImage is a PNG unpacked to the Image widget's pixel contract:
// 0xRRGGBBAA, straight alpha, row-major.
type decodedImage struct {
	pixels []uint32
	w      uint32
	h      uint32
	// version changes whenever the artwork is replaced, so a window
	// re-uploads the texture.
	version uint64
}

// label is the party's tab label, icon included.
func (inst party) label() (l string) {
	l = inst.name
	if inst.icon != "" {
		l = inst.icon + " " + l
	}
	return
}

// imgKey is both the widget-id key and the version-tracker key of the
// party's texture; the two must stay 1:1 (see ImageVersionTracker docs).
func (inst party) imgKey() (k string) {
	k = inst.key + "-img"
	return
}

// identityLines are the party's identity as its About page draws them and
// the help page lists them: the module version when known, the copyright
// and the licence when the party supplied them.
func (inst party) identityLines() (lines []string) {
	if v := moduleVersion(inst.module); v != "" {
		lines = append(lines, inst.module+" · version "+v)
	}
	if inst.copyright != "" {
		lines = append(lines, inst.copyright)
	}
	if inst.license != "" {
		lines = append(lines, inst.license)
	}
	return
}

// parties lists the credited parties in tab order: the program first, then
// boxer.
func parties() (ps []party) {
	if p, ok := programParty(); ok {
		ps = append(ps, p)
	}
	ps = append(ps, boxerParty())
	return
}

// lookupParty finds the party with the given key.
func lookupParty(ps []party, key string) (p party, ok bool) {
	for _, q := range ps {
		if q.key == key {
			p, ok = q, true
			return
		}
	}
	return
}

// moduleVersion is the version the build info records for module: the
// main module's or a dependency's, marked when a replace directive points
// it elsewhere. Empty when the module is not in the build info.
func moduleVersion(module string) (version string) {
	if module == "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Path == module {
		version = info.Main.Version
		return
	}
	for _, d := range info.Deps {
		if d.Path != module {
			continue
		}
		version = d.Version
		if d.Replace != nil {
			version += " (replaced)"
		}
		return
	}
	return
}
