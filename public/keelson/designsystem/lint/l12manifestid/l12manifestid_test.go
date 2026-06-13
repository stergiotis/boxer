package l12manifestid_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l12manifestid"
)

// stubAppPath is the import path of the stand-in app package under
// testdata/src/. The analyzer hard-codes the real runtime/app path; tests
// swap it via the exported AppPackagePath var. Restored after each Run so
// adjacent test files / parallel package tests are not affected.
const stubAppPath = "stub"

func withStubAppPath(t *testing.T, body func()) {
	t.Helper()
	saved := l12manifestid.AppPackagePath
	l12manifestid.AppPackagePath = stubAppPath
	defer func() { l12manifestid.AppPackagePath = saved }()
	body()
}

func TestL12_Violations(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "violator")
	})
}

func TestL12_Clean_ExactMatch(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "clean")
	})
}

func TestL12_Clean_SubpathPrefix(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "subpath")
	})
}

func TestL12_Clean_Allowlisted(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "allowed")
	})
}

func TestL12_Suppressed(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "ignored")
	})
}

func TestL12_NestedSubpathConst(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "nested/sub")
	})
}

// TestL12_CrossReferenceTablesExempt locks in the carousel pattern:
// `map[K]app.AppIdT` and `[]app.AppIdT` literals hold *other packages'*
// Ids as cross-reference data, not "this is my Id" assertions. They
// must not produce L12 findings even though their values are typed
// AppIdT. A regression here would re-introduce ~6 false positives in
// thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go.
func TestL12_CrossReferenceTablesExempt(t *testing.T) {
	withStubAppPath(t, func() {
		analysistest.Run(t, analysistest.TestData(), l12manifestid.Analyzer, "crossref")
	})
}
