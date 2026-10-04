package tabhost

import "github.com/stergiotis/boxer/public/config/env"

// BrowserTargetDir is where `bundle` has cargo build the Rust browser host
// when it builds from source; build_rust_browser.sh reads the same variable.
var BrowserTargetDir = env.NewPath(env.Spec{
	Name:        "IMZERO2_BROWSER_TARGET_DIR",
	Description: "cargo target directory for building the Rust browser host (rust/imzero2/build_rust_browser.sh, `bundle` of a tab binary); unset is rust/imzero2/target/browser inside boxer, and the user cache directory when bundling from another module, where boxer's sources are read-only",
	Category:    env.CategoryDev,
})
