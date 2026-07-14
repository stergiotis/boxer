package extbin

// This file is the manifest of external programs boxer is wired to invoke. It
// is deliberately the one place to look — or to grep — when auditing the
// toolkit's host-binary surface. Adding a program means adding a declaration
// here (or, for a caller-supplied path, a [Local] declaration in the consuming
// package); the CS012 codelint rule prevents reaching for os/exec directly.
//
// OverrideEnv is populated only where a path override is already a real need;
// the field is the airgap/hermetic hook and can be added per program later
// without touching call sites.

// Go toolchain.
var (
	// Go is the `go` binary itself (`go env`, `go build`). Reproducibility
	// comes from the go.mod toolchain directive + GOTOOLCHAIN.
	Go = Declare(Program{
		Name:        "go",
		Kind:        GoToolchain,
		InstallHint: "the Go toolchain (https://go.dev/dl)",
	})

	// SCC is the scc code-counter, pinned in go.mod's tool block and invoked as
	// `go tool scc`, falling back to an `scc` on PATH.
	SCC = Declare(Program{
		Name:        "scc",
		Kind:        GoTool,
		Module:      "github.com/boyter/scc/v3",
		InstallHint: "run via `go tool scc`, or install scc on PATH (https://github.com/boyter/scc)",
	})
)

// Version control.
var (
	// Git backs the governance tooling (commitdigest, repo, doclint) and repo
	// introspection (scctree).
	Git = Declare(Program{
		Name:        "git",
		Kind:        Host,
		InstallHint: "install git (https://git-scm.com)",
	})

	// Pijul backs the pushout pijul adapter.
	Pijul = Declare(Program{
		Name:        "pijul",
		Kind:        Host,
		InstallHint: "https://pijul.org/manual/installation.html",
	})
)

// Data / query engines.
var (
	// ClickHouseLocal runs one-shot SQL over local files (adr query, recordstore
	// exec, the chlocalpool worker, arrow formatting, mine-trends). Callers with
	// a configured binary path pass it via [Opts].Path.
	ClickHouseLocal = Declare(Program{
		Name:        "clickhouse-local",
		Kind:        Host,
		OverrideEnv: "BOXER_CLICKHOUSE_LOCAL",
		InstallHint: "https://clickhouse.com/docs/en/install",
	})
)

// Language toolchains for code synthesis / analysis.
var (
	// TinyGo compiles the WASM survey probes.
	TinyGo = Declare(Program{
		Name:        "tinygo",
		Kind:        Host,
		InstallHint: "https://tinygo.org/getting-started/install/",
	})

	// Rustfmt formats generated Rust (egui2 driver output).
	Rustfmt = Declare(Program{
		Name:        "rustfmt",
		Kind:        Host,
		InstallHint: "rustup component add rustfmt",
	})

	// Cargo drives Rust builds in the deploy showcase.
	Cargo = Declare(Program{
		Name:        "cargo",
		Kind:        Host,
		InstallHint: "install the Rust toolchain (https://rustup.rs)",
	})

	// Bash runs shell build steps in the deploy showcase.
	Bash = Declare(Program{
		Name:        "bash",
		Kind:        Host,
		InstallHint: "install bash",
	})
)

// Profiling wrappers for the imzero2 client (selected by BOXER_IMZERO_DEBUG_MODE).
var (
	Flamegraph = Declare(Program{
		Name:        "flamegraph",
		Kind:        Host,
		InstallHint: "cargo install flamegraph",
	})

	Valgrind = Declare(Program{
		Name:        "valgrind",
		Kind:        Host,
		InstallHint: "install valgrind (https://valgrind.org)",
	})

	Heaptrack = Declare(Program{
		Name:        "heaptrack",
		Kind:        Host,
		InstallHint: "install heaptrack",
	})
)

// System / desktop utilities.
var (
	// Restorecon relabels SELinux contexts during deploy.
	Restorecon = Declare(Program{
		Name:        "restorecon",
		Kind:        Host,
		InstallHint: "part of policycoreutils (SELinux)",
	})

	// FcMatch resolves a font file path via fontconfig for the deploy showcase.
	FcMatch = Declare(Program{
		Name:        "fc-match",
		Kind:        Host,
		InstallHint: "install fontconfig",
	})

	// Systemctl restarts the service after an atomic deploy swap.
	Systemctl = Declare(Program{
		Name:        "systemctl",
		Kind:        Host,
		InstallHint: "systemd (systemctl) is required on the deploy host",
	})
)
