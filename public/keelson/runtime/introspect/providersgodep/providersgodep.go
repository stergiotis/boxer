// Package providersgodep exposes this module's Go package graph as keelson
// introspection tables — `go_packages`, `go_imports`, `go_collection` and
// `go_package_props` — so the dependency questions ADR-0064 answers inside
// one app can be asked in SQL, and joined with the rest of the repository
// corpus (`keelson('adr')`, `keelson('coderef')`, `keelson('sbom')`).
//
// # These tables cost more than the others
//
// The ADR corpus tables (see providers/adr.go) already stretched ADR-0094's
// founding premise — state that "exists only inside a running process" — by
// reading files at query time; ADR-0122 §SD4 records that tension. This
// package stretches it further: collecting the graph means running the `go`
// toolchain through golang.org/x/tools/go/packages, which is seconds rather
// than milliseconds. Three properties keep that honest:
//
//   - The manifest is collected once per process and cached. A query answers
//     from the cache; it never re-runs the toolchain. That is also
//     godepview's own semantics — its snapshot is taken when the window
//     opens — so the tables are as fresh as the process, no fresher.
//   - The first query to touch any of the tables starts collection and waits
//     a bounded moment for it. On this repository the collection takes about
//     1.4 s warm (1411 packages, 13030 import edges); a cold toolchain can
//     take much longer, so past the budget the query returns **zero rows**
//     with `go_collection.status = 'collecting'` rather than blocking. The
//     next query gets the rows.
//   - Off-repo — no go.mod above the working directory, or a toolchain that
//     cannot run — the tables are empty and `go_collection` carries the
//     reason in its `error` column. An empty table is a fact about the
//     process, not a query failure (the keelson('sbom') behaviour).
//
// # Where this is registered matters
//
// golang.org/x/tools is a heavy dependency that appliance builds have no use
// for, so [Register] is called from a dev composition root (the carousel's
// shared registry) and deliberately not from introspecthost's static
// provider set. providersgui is the existing precedent for a provider
// package excluded from that set.
package providersgodep

import (
	"os"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The collection target is env-pinned per ADR-0009, the BOXER_ADR_DIR idiom:
// a table whose rows depend on where the process was started should say so
// explicitly rather than only inferring it. godepview keeps its own
// GODEPVIEW_* pair for as long as it exists; the two resolve the same way but
// are read independently, so setting one does not move the other.
var (
	// EnvRoot overrides the module directory to collect. Empty resolves the
	// nearest go.mod above the process working directory.
	EnvRoot = env.NewPath(env.Spec{
		Name:        "BOXER_GODEP_ROOT",
		Description: "module directory the keelson go_packages/go_imports tables collect from; empty resolves the nearest go.mod above the working directory",
		Category:    env.CategoryDev,
	})
	// EnvTags overrides the build tags collection runs under
	// (comma-separated). The repo's tags are load-bearing: collecting without
	// them yields a graph missing every tag-gated package.
	EnvTags = env.NewString(env.Spec{
		Name:        "BOXER_GODEP_TAGS",
		Description: "comma-separated build tags for the keelson go_packages/go_imports collection; empty falls back to <root>/tags then inherited GOFLAGS",
		Category:    env.CategoryDev,
	})
)

// Config parameterises the collection behind the tables. The zero value is
// usable: it resolves root and tags from the environment.
type Config struct {
	// Root is the module directory to collect. Empty reads EnvRoot, then
	// falls back to the nearest go.mod above the working directory.
	Root string
	// Tags are the build tags collection runs under. Nil reads EnvTags, then
	// the root's `tags` file, then leaves the inherited GOFLAGS in charge.
	Tags []string
	// Log receives the collection outcome — one line when it finishes, one
	// when it fails. Collection is off the query path, so its failure is
	// otherwise visible only in go_collection.error.
	Log zerolog.Logger
}

// Register adds the four godep tables to r, sharing one cache between them
// so a query joining `go_packages` with `go_imports` collects once. It does
// not collect: the first query that touches a table starts that (see the
// package doc).
func Register(r *introspect.Registry, cfg Config) (err error) {
	c := newCache(resolveConfig(cfg))
	for _, p := range []introspect.Provider{
		packagesProvider{cache: c},
		importsProvider{cache: c},
		collectionProvider{cache: c},
		packagePropsProvider{},
	} {
		if err = r.Register(p); err != nil {
			return
		}
	}
	return
}

// resolveConfig fills the empty fields of cfg from the environment. Per
// ADR-0064 §SD3 the collector itself stays env-free and takes an explicit
// Config; this is the composition-root half of that split.
func resolveConfig(cfg Config) (out Config) {
	out = cfg
	if out.Root == "" {
		out.Root = EnvRoot.Get()
	}
	if out.Root == "" {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			if root, ok := godepcollect.ModuleRoot(wd); ok {
				out.Root = root
			}
		}
	}
	if out.Tags == nil {
		out.Tags = godepcollect.ResolveTags(EnvTags.Get(), out.Root)
	}
	return
}
