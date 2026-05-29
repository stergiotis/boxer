// Stand-in for the runtime/app package used by analysistest fixtures.
// Mirrors only the surface the L12 analyzer inspects: AppIdT (a string
// named type) and Manifest (struct with an Id field of type AppIdT).
//
// The l12manifestid_test.go harness swaps the analyzer's AppPackagePath
// to "stub" so lookupAppIdType resolves to *this* package's AppIdT.
package stub

type AppIdT string

type Manifest struct {
	Id      AppIdT
	Version string
	Display string
}
