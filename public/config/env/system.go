package env

// System variables — OS- and Go-toolchain-owned. Declared here, not in
// the consuming package, because they have no single owner and are
// referenced from multiple subsystems. Defaults are intentionally empty:
// the OS or shell supplies the value, and "" signals "not set".

var (
	// Home is the user's $HOME. Reads should tolerate an empty result;
	// the OS does not set it in every context (e.g. minimal containers).
	Home = NewPath(Spec{
		Name:        "HOME",
		Description: "user home directory; supplied by the OS or shell",
		Category:    CategorySystem,
	})

	// GoPath is $GOPATH, the Go workspace root. Empty means "Go default"
	// (effectively $HOME/go), which callers must apply themselves; the
	// env package does not synthesise it.
	GoPath = NewPath(Spec{
		Name:        "GOPATH",
		Description: "Go workspace root; empty means $HOME/go applied by the Go toolchain",
		Category:    CategorySystem,
	})

	// GoRoot is $GOROOT, the Go installation root. Empty means the
	// toolchain's built-in default; eh then reads the root from the
	// binary's own frames rather than running the go tool.
	GoRoot = NewPath(Spec{
		Name:        "GOROOT",
		Description: "Go installation root, used to shorten standard-library paths in error stacks; empty means read it from the binary's own frames",
		Category:    CategorySystem,
	})

	// GoWork is $GOWORK. The literal "off" disables workspace mode;
	// any other non-empty value is the path to a go.work file.
	GoWork = NewString(Spec{
		Name:        "GOWORK",
		Description: "Go workspace file path; \"off\" disables workspace mode",
		Category:    CategorySystem,
	})
)
