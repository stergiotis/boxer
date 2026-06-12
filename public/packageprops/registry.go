package packageprops

import (
	"sort"
	"sync"
)

// This file is the runtime side of ADR-0080: a process-global registry that
// generated package_props.go files populate from init. Because Go runs init
// for every package linked into a binary (and dead-code elimination never drops
// it), packageprops.All() reflects exactly the packages compiled into the
// running binary — the reflect-like "what's in me" view a GUI can enumerate.
// For the whole-repo view regardless of what a binary links, use the static
// Table emitted by `wasmsurvey props harvest --emit go` instead.

// Entry is one package's declared Props keyed by its import path. It is both the
// registry's record type and the element type of the harvested Table.
type Entry struct {
	ImportPath string
	Props      Props
}

// Table is a set of Entries — the static harvest output and the All() snapshot.
type Table []Entry

var (
	regMu  sync.RWMutex
	regMap = make(map[string]Props)
)

// Register records a package's Props under its import path. Generated
// package_props.go files call it from init, so it normally runs single-threaded
// at startup; the lock keeps a late runtime Register safe against a concurrent
// All(). Last write wins for a given path.
func Register(importPath string, p Props) {
	regMu.Lock()
	regMap[importPath] = p
	regMu.Unlock()
}

// All returns a snapshot of the registry sorted by import path: the Props of
// every package whose init has run (i.e. that is linked into this binary).
func All() (t Table) {
	regMu.RLock()
	t = make(Table, 0, len(regMap))
	for ip, p := range regMap {
		t = append(t, Entry{ImportPath: ip, Props: p})
	}
	regMu.RUnlock()
	sort.Slice(t, func(i, j int) bool { return t[i].ImportPath < t[j].ImportPath })
	return
}

// Lookup returns the registered Props for an import path, if present.
func Lookup(importPath string) (p Props, ok bool) {
	regMu.RLock()
	p, ok = regMap[importPath]
	regMu.RUnlock()
	return
}
