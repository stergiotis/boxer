package ladingremote

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked: it spawns an external process and speaks SFTP over its pipes, and
// `pkg/sftp` brings `golang.org/x/crypto/ssh` with it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingremote", PackageProps)
}
