package vcs

import (
	"strings"
	"testing"
)

func TestBoxerModuleVersionNamesThisModule(t *testing.T) {
	got := BoxerModuleVersion()
	if !strings.HasPrefix(got, BoxerModulePath+"@") {
		t.Fatalf("BoxerModuleVersion = %q, want %s@<version>", got, BoxerModulePath)
	}
}

func TestMainModuleVersion(t *testing.T) {
	for in, want := range map[string]string{
		"":                                     "(devel)",
		"(devel)":                              "(devel)",
		"v0.3.1":                               "v0.3.1",
		"v0.3.1+dirty":                         "(devel)",
		"v0.0.0-20261121004501-abcdef123456":   "(devel)",
		"v0.3.2-0.20261121004501-abcdef123456": "(devel)",
	} {
		if got := mainModuleVersion(in); got != want {
			t.Errorf("mainModuleVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
