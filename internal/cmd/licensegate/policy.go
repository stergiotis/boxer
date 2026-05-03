package main

// Inbound-license policy for boxer (MIT-licensed). See ADR-0004
// (doc/adr/0004-license-gate-cyclonedx.md) for the rationale.
//
// The map below is the source of truth for the gate. SPDX evolves over
// time (https://spdx.org/licenses/); review on each dependency-tooling
// refresh and add new identifiers as they appear in upstream
// dependencies.
//
// Categories follow go-licenses' classification conventions:
//
//   - Forbidden:    prohibits use entirely or imposes commercially-
//                   incompatible terms (AGPL, SSPL, BUSL, OSL, CC-BY-NC*).
//   - Restricted:   strong copyleft requiring source distribution under
//                   the same terms (GPL, LGPL, GFDL, NPL, QPL).
//   - Reciprocal:   weak copyleft with file- or library-level reciprocity
//                   (MPL, EPL, CDDL, MS-RL, CC-BY-SA).
//   - Notice:       permissive with attribution requirement (Apache, BSD,
//                   MIT, ISC, BSL).
//   - Permissive:   relaxed permissive; treated identically to Notice.
//   - Unencumbered: public-domain-equivalent (0BSD, CC0, Unlicense).
//   - Unknown:      SPDX ID not in the map; reported advisorily, does
//                   not fail the gate.
//
// IsViolation reports the gating predicate: forbidden and restricted
// fail CI; everything else (including reciprocal and unknown) passes.

type CategoryE uint8

const (
	CategoryUnknown      CategoryE = 0
	CategoryForbidden    CategoryE = 1
	CategoryRestricted   CategoryE = 2
	CategoryReciprocal   CategoryE = 3
	CategoryNotice       CategoryE = 4
	CategoryPermissive   CategoryE = 5
	CategoryUnencumbered CategoryE = 6
)

func (c CategoryE) String() (s string) {
	switch c {
	case CategoryForbidden:
		s = "forbidden"
	case CategoryRestricted:
		s = "restricted"
	case CategoryReciprocal:
		s = "reciprocal"
	case CategoryNotice:
		s = "notice"
	case CategoryPermissive:
		s = "permissive"
	case CategoryUnencumbered:
		s = "unencumbered"
	default:
		s = "unknown"
	}
	return
}

func (c CategoryE) IsViolation() (b bool) {
	b = c == CategoryForbidden || c == CategoryRestricted
	return
}

var spdxCategory = map[string]CategoryE{
	// Forbidden: prohibits use entirely or imposes commercially-incompatible terms.
	"AGPL-1.0":          CategoryForbidden,
	"AGPL-1.0-only":     CategoryForbidden,
	"AGPL-1.0-or-later": CategoryForbidden,
	"AGPL-3.0":          CategoryForbidden,
	"AGPL-3.0-only":     CategoryForbidden,
	"AGPL-3.0-or-later": CategoryForbidden,
	"BUSL-1.1":          CategoryForbidden,
	"OSL-1.0":           CategoryForbidden,
	"OSL-1.1":           CategoryForbidden,
	"OSL-2.0":           CategoryForbidden,
	"OSL-2.1":           CategoryForbidden,
	"OSL-3.0":           CategoryForbidden,
	"SSPL-1.0":          CategoryForbidden,
	"CC-BY-NC-1.0":      CategoryForbidden,
	"CC-BY-NC-2.0":      CategoryForbidden,
	"CC-BY-NC-2.5":      CategoryForbidden,
	"CC-BY-NC-3.0":      CategoryForbidden,
	"CC-BY-NC-4.0":      CategoryForbidden,
	"CC-BY-NC-SA-1.0":   CategoryForbidden,
	"CC-BY-NC-SA-2.0":   CategoryForbidden,
	"CC-BY-NC-SA-2.5":   CategoryForbidden,
	"CC-BY-NC-SA-3.0":   CategoryForbidden,
	"CC-BY-NC-SA-4.0":   CategoryForbidden,
	"CC-BY-NC-ND-1.0":   CategoryForbidden,
	"CC-BY-NC-ND-2.0":   CategoryForbidden,
	"CC-BY-NC-ND-2.5":   CategoryForbidden,
	"CC-BY-NC-ND-3.0":   CategoryForbidden,
	"CC-BY-NC-ND-4.0":   CategoryForbidden,
	"CC-BY-ND-1.0":      CategoryForbidden,
	"CC-BY-ND-2.0":      CategoryForbidden,
	"CC-BY-ND-2.5":      CategoryForbidden,
	"CC-BY-ND-3.0":      CategoryForbidden,
	"CC-BY-ND-4.0":      CategoryForbidden,

	// Restricted: strong copyleft (must distribute source under same terms).
	"GPL-1.0":                          CategoryRestricted,
	"GPL-1.0+":                         CategoryRestricted,
	"GPL-1.0-only":                     CategoryRestricted,
	"GPL-1.0-or-later":                 CategoryRestricted,
	"GPL-2.0":                          CategoryRestricted,
	"GPL-2.0+":                         CategoryRestricted,
	"GPL-2.0-only":                     CategoryRestricted,
	"GPL-2.0-or-later":                 CategoryRestricted,
	"GPL-2.0-with-autoconf-exception":  CategoryRestricted,
	"GPL-2.0-with-bison-exception":     CategoryRestricted,
	"GPL-2.0-with-classpath-exception": CategoryRestricted,
	"GPL-2.0-with-font-exception":      CategoryRestricted,
	"GPL-2.0-with-GCC-exception":       CategoryRestricted,
	"GPL-3.0":                          CategoryRestricted,
	"GPL-3.0+":                         CategoryRestricted,
	"GPL-3.0-only":                     CategoryRestricted,
	"GPL-3.0-or-later":                 CategoryRestricted,
	"GPL-3.0-with-autoconf-exception":  CategoryRestricted,
	"GPL-3.0-with-GCC-exception":       CategoryRestricted,
	"LGPL-2.0":                         CategoryRestricted,
	"LGPL-2.0+":                        CategoryRestricted,
	"LGPL-2.0-only":                    CategoryRestricted,
	"LGPL-2.0-or-later":                CategoryRestricted,
	"LGPL-2.1":                         CategoryRestricted,
	"LGPL-2.1+":                        CategoryRestricted,
	"LGPL-2.1-only":                    CategoryRestricted,
	"LGPL-2.1-or-later":                CategoryRestricted,
	"LGPL-3.0":                         CategoryRestricted,
	"LGPL-3.0+":                        CategoryRestricted,
	"LGPL-3.0-only":                    CategoryRestricted,
	"LGPL-3.0-or-later":                CategoryRestricted,
	"GFDL-1.1":                         CategoryRestricted,
	"GFDL-1.1-only":                    CategoryRestricted,
	"GFDL-1.1-or-later":                CategoryRestricted,
	"GFDL-1.2":                         CategoryRestricted,
	"GFDL-1.2-only":                    CategoryRestricted,
	"GFDL-1.2-or-later":                CategoryRestricted,
	"GFDL-1.3":                         CategoryRestricted,
	"GFDL-1.3-only":                    CategoryRestricted,
	"GFDL-1.3-or-later":                CategoryRestricted,
	"NPL-1.0":                          CategoryRestricted,
	"NPL-1.1":                          CategoryRestricted,
	"QPL-1.0":                          CategoryRestricted,
	"Sleepycat":                        CategoryRestricted,

	// Reciprocal: weak copyleft (file- or library-level reciprocity).
	"MPL-1.0":      CategoryReciprocal,
	"MPL-1.1":      CategoryReciprocal,
	"MPL-2.0":      CategoryReciprocal,
	"CDDL-1.0":     CategoryReciprocal,
	"CDDL-1.1":     CategoryReciprocal,
	"EPL-1.0":      CategoryReciprocal,
	"EPL-2.0":      CategoryReciprocal,
	"MS-RL":        CategoryReciprocal,
	"IPL-1.0":      CategoryReciprocal,
	"APSL-2.0":     CategoryReciprocal,
	"IBM-pibs":     CategoryReciprocal,
	"CC-BY-SA-1.0": CategoryReciprocal,
	"CC-BY-SA-2.0": CategoryReciprocal,
	"CC-BY-SA-3.0": CategoryReciprocal,
	"CC-BY-SA-4.0": CategoryReciprocal,
	"CECILL-2.0":   CategoryReciprocal,
	"CECILL-2.1":   CategoryReciprocal,

	// Notice: permissive with attribution requirement.
	"Apache-1.0":                CategoryNotice,
	"Apache-1.1":                CategoryNotice,
	"Apache-2.0":                CategoryNotice,
	"Artistic-1.0":              CategoryNotice,
	"Artistic-1.0-Perl":         CategoryNotice,
	"Artistic-1.0-cl8":          CategoryNotice,
	"Artistic-2.0":              CategoryNotice,
	"BSD-2-Clause":              CategoryNotice,
	"BSD-2-Clause-Patent":       CategoryNotice,
	"BSD-2-Clause-Views":        CategoryNotice,
	"BSD-3-Clause":              CategoryNotice,
	"BSD-3-Clause-Clear":        CategoryNotice,
	"BSD-3-Clause-Modification": CategoryNotice,
	"BSD-3-Clause-Open-MPI":     CategoryNotice,
	"BSD-4-Clause":              CategoryNotice,
	"BSD-4-Clause-Shortened":    CategoryNotice,
	"BSD-4-Clause-UC":           CategoryNotice,
	"BSL-1.0":                   CategoryNotice,
	"ISC":                       CategoryNotice,
	"MIT":                       CategoryNotice,
	"MIT-0":                     CategoryNotice,
	"MIT-CMU":                   CategoryNotice,
	"MIT-feh":                   CategoryNotice,
	"MIT-Modern-Variant":        CategoryNotice,
	"MITNFA":                    CategoryNotice,
	"X11":                       CategoryNotice,
	"FTL":                       CategoryNotice,
	"Zlib":                      CategoryNotice,
	"Zlib-acknowledgement":      CategoryNotice,
	"bzip2-1.0.6":               CategoryNotice,
	"libtiff":                   CategoryNotice,
	"OpenSSL":                   CategoryNotice,
	"PostgreSQL":                CategoryNotice,
	"ICU":                       CategoryNotice,
	"W3C":                       CategoryNotice,
	"W3C-19980720":              CategoryNotice,
	"W3C-20150513":              CategoryNotice,
	"libpng":                    CategoryNotice,
	"libpng-2.0":                CategoryNotice,
	"Python-2.0":                CategoryNotice,
	"Ruby":                      CategoryNotice,
	"TCL":                       CategoryNotice,
	"Unicode-DFS-2015":          CategoryNotice,
	"Unicode-DFS-2016":          CategoryNotice,
	"Unicode-TOU":               CategoryNotice,

	// Unencumbered: public-domain-equivalent.
	"0BSD":             CategoryUnencumbered,
	"BSD-Source-Code":  CategoryUnencumbered,
	"CC-PDDC":          CategoryUnencumbered,
	"CC0-1.0":          CategoryUnencumbered,
	"Unlicense":        CategoryUnencumbered,
	"WTFPL":            CategoryUnencumbered,
	"blessing":         CategoryUnencumbered,
	"Beerware":         CategoryUnencumbered,
	"NIST-PD":          CategoryUnencumbered,
	"NIST-PD-fallback": CategoryUnencumbered,
	"PDDL-1.0":         CategoryUnencumbered,
}

func Categorize(spdxID string) (cat CategoryE) {
	cat = spdxCategory[spdxID]
	return
}

// moduleLicenseElection records, per module path, the SPDX identifier
// boxer formally elects when an upstream is dual-licensed and the
// detector reports only one branch (typically the copyleft branch).
//
// Each entry replaces the SBOM-detected license set for that module
// with a single asserted SPDX ID. The election is a deliberate
// corporate-license choice; new entries require a comment citing the
// upstream LICENSE wording that authorises the election.
//
// Module paths are matched without `@version` — the election follows
// the module across version bumps. If an upstream changes its license
// terms the test suite catches it (the elected SPDX ID still has to be
// present in `spdxCategory` and resolve to a non-violating category),
// but a manual review on each dependency bump is the primary guard.
var moduleLicenseElection = map[string]string{
	// freetype-go is dual-licensed FTL OR GPL-2.0-or-later. Per upstream
	// LICENSE: "Use of the Freetype-Go software is subject to your choice
	// of exactly one of the following two licenses". Boxer elects FTL
	// (BSD-like with an advertising clause). cyclonedx-gomod's detector
	// only emits GPL-2.0-or-later for this module.
	"github.com/golang/freetype": "FTL",
}

// ElectedLicense returns (spdxID, true) when boxer elects a specific
// SPDX identifier for the given module path, replacing whatever the
// SBOM detector reported. Returns ("", false) when no election applies
// and the detected license set should be used as-is.
func ElectedLicense(module string) (spdxID string, ok bool) {
	spdxID, ok = moduleLicenseElection[module]
	return
}
