//go:build llm_generated_opus47

package main

import "testing"

func TestCategorize(t *testing.T) {
	cases := []struct {
		id   string
		want CategoryE
	}{
		// Forbidden
		{"AGPL-1.0", CategoryForbidden},
		{"AGPL-3.0-or-later", CategoryForbidden},
		{"BUSL-1.1", CategoryForbidden},
		{"SSPL-1.0", CategoryForbidden},
		{"OSL-3.0", CategoryForbidden},
		{"CC-BY-NC-4.0", CategoryForbidden},
		{"CC-BY-NC-SA-4.0", CategoryForbidden},
		{"CC-BY-NC-ND-4.0", CategoryForbidden},
		{"CC-BY-ND-4.0", CategoryForbidden},

		// Restricted
		{"GPL-2.0", CategoryRestricted},
		{"GPL-2.0-only", CategoryRestricted},
		{"GPL-2.0-or-later", CategoryRestricted},
		{"GPL-3.0-or-later", CategoryRestricted},
		{"LGPL-2.1", CategoryRestricted},
		{"LGPL-3.0-or-later", CategoryRestricted},
		{"GFDL-1.3", CategoryRestricted},
		{"NPL-1.1", CategoryRestricted},
		{"QPL-1.0", CategoryRestricted},

		// Reciprocal
		{"MPL-2.0", CategoryReciprocal},
		{"EPL-2.0", CategoryReciprocal},
		{"CDDL-1.0", CategoryReciprocal},
		{"MS-RL", CategoryReciprocal},
		{"CC-BY-SA-4.0", CategoryReciprocal},

		// Notice
		{"Apache-2.0", CategoryNotice},
		{"BSD-2-Clause", CategoryNotice},
		{"BSD-3-Clause", CategoryNotice},
		{"BSD-4-Clause", CategoryNotice},
		{"MIT", CategoryNotice},
		{"MIT-0", CategoryNotice},
		{"ISC", CategoryNotice},
		{"BSL-1.0", CategoryNotice},
		{"Zlib", CategoryNotice},
		{"Python-2.0", CategoryNotice},
		{"FTL", CategoryNotice},

		// Unencumbered
		{"0BSD", CategoryUnencumbered},
		{"CC0-1.0", CategoryUnencumbered},
		{"Unlicense", CategoryUnencumbered},
		{"WTFPL", CategoryUnencumbered},

		// Unknown — anything outside the map, including empty string
		{"NotARealLicense", CategoryUnknown},
		{"", CategoryUnknown},
		{"LicenseRef-custom-thing", CategoryUnknown},
	}
	for _, tc := range cases {
		got := Categorize(tc.id)
		if got != tc.want {
			t.Errorf("Categorize(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestIsViolation(t *testing.T) {
	cases := []struct {
		cat  CategoryE
		want bool
	}{
		{CategoryForbidden, true},
		{CategoryRestricted, true},
		{CategoryReciprocal, false},
		{CategoryNotice, false},
		{CategoryPermissive, false},
		{CategoryUnencumbered, false},
		{CategoryUnknown, false},
	}
	for _, tc := range cases {
		got := tc.cat.IsViolation()
		if got != tc.want {
			t.Errorf("(%v).IsViolation() = %v, want %v", tc.cat, got, tc.want)
		}
	}
}

func TestCategoryEString(t *testing.T) {
	cases := []struct {
		cat  CategoryE
		want string
	}{
		{CategoryForbidden, "forbidden"},
		{CategoryRestricted, "restricted"},
		{CategoryReciprocal, "reciprocal"},
		{CategoryNotice, "notice"},
		{CategoryPermissive, "permissive"},
		{CategoryUnencumbered, "unencumbered"},
		{CategoryUnknown, "unknown"},
		{CategoryE(255), "unknown"},
	}
	for _, tc := range cases {
		got := tc.cat.String()
		if got != tc.want {
			t.Errorf("(%v).String() = %q, want %q", tc.cat, got, tc.want)
		}
	}
}

// TestElectedLicense verifies the per-module license election path used
// to override the SBOM-detected license set for dual-licensed
// dependencies (e.g. freetype-go: FTL OR GPL-2.0-or-later, where boxer
// elects the permissive FTL branch).
func TestElectedLicense(t *testing.T) {
	id, ok := ElectedLicense("github.com/golang/freetype")
	if !ok {
		t.Fatal("ElectedLicense(freetype) returned ok=false; expected an election entry")
	}
	if id != "FTL" {
		t.Errorf("ElectedLicense(freetype) = %q, want %q", id, "FTL")
	}
	if cat := Categorize(id); cat.IsViolation() {
		t.Errorf("elected license %q for freetype categorises as %v, which would re-fail the gate", id, cat)
	}

	_, ok = ElectedLicense("github.com/example/not-elected")
	if ok {
		t.Error("ElectedLicense returned ok=true for a module with no election entry")
	}
}

// TestSelfModuleFilter sanity-checks the purl-prefix exclusion for the
// project's own module — both the bare module and synthesized variants
// like the goarch/goos-qualified component-purls cyclonedx-gomod emits.
func TestSelfModuleFilter(t *testing.T) {
	cases := []struct {
		purl string
		want bool
	}{
		{"pkg:golang/github.com/stergiotis/boxer", true},
		{"pkg:golang/github.com/stergiotis/boxer?type=module", true},
		{"pkg:golang/github.com/stergiotis/boxer?goarch=amd64&goos=linux&type=module", true},
		// Sibling repositories owned by the same author are *not* filtered (see ADR-0004 SD6).
		{"pkg:golang/github.com/stergiotis/capmap?type=module", false},
		{"pkg:golang/github.com/stergiotis/pebble2impl@v0.1.0?type=module", false},
		{"pkg:golang/github.com/google/go-cmp@v0.6.0?type=module", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isSelfModule(tc.purl)
		if got != tc.want {
			t.Errorf("isSelfModule(%q) = %v, want %v", tc.purl, got, tc.want)
		}
	}
}
