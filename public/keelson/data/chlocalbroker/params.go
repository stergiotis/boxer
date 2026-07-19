package chlocalbroker

import (
	"sort"
	"strings"

	"lukechampine.com/blake3"
)

// validParamName bounds a parameter name to the identifier charset the
// input tables already enforce (ADR-0133 §SD2 reuses the ADR-0094 §SD5
// constraint): `[A-Za-z_][A-Za-z0-9_]*`, at most 64 bytes. A bad name must
// never reach the cache-key fold or the SQL prelude.
func validParamName(name string) (ok bool) {
	ok = validInputTableName(name)
	return
}

// paramPrelude renders one `SET param_<name> = '<value>';` statement per
// entry, sorted by name for a deterministic script, values quoted through
// the same sqlQuoteString the input-table prelude trusts. Empty input
// yields the empty string. Verified against clickhouse-local 26.6: SET
// param_* in a multi-statement stdin script substitutes `{name:Type}`
// placeholders exactly like the HTTP `param_*` channel (ADR-0133 Update
// 2026-07-19).
func paramPrelude(params map[string]string) (prelude string) {
	if len(params) == 0 {
		return
	}
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString("SET param_")
		b.WriteString(name)
		b.WriteString(" = ")
		b.WriteString(sqlQuoteString(params[name]))
		b.WriteString(";\n")
	}
	prelude = b.String()
	return
}

// foldParams derives a new cache key from a base key and a request's
// Params, so a cached result never outlives a changed binding under
// unchanged SQL — the foldInputTables discipline. Returns base unchanged
// when there are no params. The leading domain tag keeps this fold
// byte-distinct from foldInputTables: without it, Params{a:b} and
// InputTables{a:[]byte("b")} would hash identically and alias in the
// cache despite meaning different requests.
func foldParams(base cacheKey, params map[string]string) (key cacheKey) {
	if len(params) == 0 {
		key = base
		return
	}
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	h := blake3.New(32, nil)
	_, _ = h.Write(base[:])
	_, _ = h.Write([]byte{2}) // domain tag: params, not input tables
	for _, name := range names {
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{2})
		_, _ = h.Write([]byte(params[name]))
		_, _ = h.Write([]byte{2})
	}
	sum := h.Sum(nil)
	copy(key[:], sum)
	return
}
