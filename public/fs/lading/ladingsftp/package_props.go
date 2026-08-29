package ladingsftp

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Not asserted on any target: the closure reaches arrow-go, which the survey
// seeded unsupported-external and therefore never probed. Measured, arrow-go
// compiles and runs under TinyGo (ADR-0078, Updates 2026-08-29), the seed is
// gone, and static mode proves only red — so this package stays unjudged
// until a TinyGo that accepts the repo's Go version probes it.
//
// Its own closure also carries `pkg/sftp` (and with it `golang.org/x/crypto/ssh`
// and `os/user`) for a convenience constructor this package never calls, since
// the transport is a pipe (§SD9); static mode does not flag either.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingsftp", PackageProps)
}
