// Package gvtmpdir gives go-graphviz a per-user compilation cache.
//
// go-graphviz compiles its embedded Graphviz module in a package init and
// caches the result under os.TempDir()/go-graphviz. With TMPDIR unset that is
// /tmp/go-graphviz, shared by every user of the machine: the first user to run
// any binary linking it owns the directory, and every other user's binary then
// panics during init with "permission denied" — before main runs, so no flag or
// error handling can intervene.
//
// Importing this package points TMPDIR at a directory under os.UserCacheDir()
// for the duration of process initialization, so the cache lands there
// instead; Restore puts TMPDIR back once go-graphviz has initialized. An
// explicitly set TMPDIR is left alone.
//
// This only works if this package initializes before
// github.com/goccy/go-graphviz/internal/wasm. The Go spec initializes, at each
// step, the first package by import path whose imports are all initialized.
// This package imports only os and path/filepath, which
// github.com/tetratelabs/wazero (a dependency of go-graphviz's wasm package)
// imports too, and its import path sorts before wazero's; so it is always
// eligible no later than wazero and is chosen first. Keep the import list to
// packages wazero already depends on.
package gvtmpdir

import (
	"os"
	"path/filepath"
)

const envTmpDir = "TMPDIR"

var overridden bool

func init() {
	if _, set := os.LookupEnv(envTmpDir); set {
		return
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(base, "boxer", "goccyengine")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	if os.Setenv(envTmpDir, dir) != nil {
		return
	}
	overridden = true
}

// Restore unsets TMPDIR again if this package set it. Call it from the init of
// a package that imports go-graphviz, which therefore runs after go-graphviz's
// own init.
func Restore() {
	if overridden {
		_ = os.Unsetenv(envTmpDir)
		overridden = false
	}
}
