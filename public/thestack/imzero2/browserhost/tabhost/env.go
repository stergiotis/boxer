package tabhost

import "github.com/stergiotis/boxer/public/config/env"

// BrowserTargetDir is where `bundle` has cargo build the Rust browser host
// when it builds from source; build_rust_browser.sh reads the same variable.
var BrowserTargetDir = env.NewPath(env.Spec{
	Name:        "IMZERO2_BROWSER_TARGET_DIR",
	Description: "cargo target directory for building the Rust browser host (rust/imzero2/build_rust_browser.sh, `bundle` of a tab binary); unset is rust/imzero2/target/browser inside boxer, and the user cache directory when bundling from another module, where boxer's sources are read-only",
	Category:    env.CategoryDev,
})

// HostURL is where `bundle` fetches the browser host by digest: the base URL a
// file named <sha256>.wasm is appended to (ADR-0278 SD5, proposed).
var HostURL = env.NewString(env.Spec{
	Name:        "BOXER_TAB_HOST_URL",
	Default:     "https://stergiotis.github.io/boxer/tabhost/",
	Description: "base URL a tab binary's `bundle` fetches the Rust browser host from, as <base><sha256>.wasm checked against the digest in browserhost.sum; point it at a mirror or a local directory server for an offline build",
	Category:    env.CategoryDev,
})

// PageOrigin is the origin of the page a tab runs in, which the worker passes
// so the module can name same-origin resources (ADR-0299 §SD2, proposed).
var PageOrigin = env.NewString(env.Spec{
	Name:        "BOXER_TAB_ORIGIN",
	Description: "the origin of the page a browser tab runs in, set by the tab's worker (ADR-0299, proposed); a tab binary's prepare step resolves same-origin paths against it",
	Category:    env.CategoryDev,
})
