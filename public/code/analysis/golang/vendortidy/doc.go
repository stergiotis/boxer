// Package vendortidy computes, without a network or module cache, the go.mod
// requirements `go mod tidy` would give a member of a vendored Go workspace,
// and writes them with a go.sum merged from the checksums the workspace
// carries (ADR-0384).
//
// The inputs are the workspace's go.work, its vendor/modules.txt and the
// sources of the used modules and vendor/. Imports are walked under cmd/go's
// "any tags" rule (ADR-0384 §SD1); versions are those the vendor tree builds
// (§SD2). Requirements the vendor tree cannot judge are kept rather than
// added (§SD3), go.sum is kept a superset (§SD4), and an import nothing in the
// workspace provides is refused (§SD5).
package vendortidy
