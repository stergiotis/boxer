package ladingsftp

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked: the head reads through the generated stores, so the transitive
// closure reaches arrow-go, which the survey classifies unsupported-external.
// `pkg/sftp` adds `golang.org/x/crypto/ssh` to that closure — for a single
// convenience constructor this package never calls, since the transport is a
// pipe (§SD9) — and `os/user`, from the server's long-listing formatter.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingsftp", PackageProps)
}
