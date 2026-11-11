package chstore

import (
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/factsread"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// The reads of this store are authored against leeway's SQL read surface
// (ADR-0367 §SD5, update of 2026-11-11): columns are handles, and an
// attribute is located by naming its section and membership. No statement
// here computes a lane position; [factsread.Prepare] expands the authored
// form before it ships, into functions [Store.SetupTable] installs.
//
// What is still named as a lane is what the surface does not hide: the
// envelope, the membership lane a `has()` prunes on, and the lanes a
// selector projects through.
const (
	hId = "`id:id`"
	hTs = "`timestamp:ts`"

	// hSymLr is the symbol section's membership lane. A kind test is a
	// has() over it with the id as a literal: that is what reaches the skip
	// index, and no expansion stands between the two.
	hSymLr    = "`symbol:lr`"
	hSymValue = "`symbol:value`"
	hStrValue = "`stringArray:value`"
)

// sym, str, text and u64 read the attribute carrying m on the section each
// is named for. Only `symbol` stores one value per attribute; the others are
// array-valued and read their first value, which is the only one for an
// attribute written single.
func sym(m registry.RegisteredNaturalKey) string  { return factsread.Scalar("symbol", m) }
func str(m registry.RegisteredNaturalKey) string  { return factsread.ListFirst("stringArray", m) }
func text(m registry.RegisteredNaturalKey) string { return factsread.ListFirst("textArray", m) }
func u64(m registry.RegisteredNaturalKey) string  { return factsread.ListFirst("u64Array", m) }

// strList is every value of the attribute carrying m on the string section.
func strList(m registry.RegisteredNaturalKey) string { return factsread.List("stringArray", m) }

// binds are the values a read sends beside its statement, by placeholder
// name. A filter's value goes here rather than into the text, so the text of
// a read depends on which filters are set and not on what they hold: a
// polled read with a moving look-back is one statement, expanded once, and a
// value a person typed is never part of the SQL.
type binds map[string]string

// i64 binds v and returns its placeholder.
func (inst binds) i64(name string, v int64) string {
	inst[name] = strconv.FormatInt(v, 10)
	return "{" + name + ":Int64}"
}

// u64 binds v and returns its placeholder.
func (inst binds) u64(name string, v uint64) string {
	inst[name] = strconv.FormatUint(v, 10)
	return "{" + name + ":UInt64}"
}

// str binds v and returns its placeholder. The server reads a bound string
// in its escaped text form, where a backslash, a tab and a line break are
// written as escapes.
func (inst binds) str(name string, v string) string {
	inst[name] = bindEscaper.Replace(v)
	return "{" + name + ":String}"
}

var bindEscaper = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

// preparedCap bounds the memo of expanded statements, which holds one entry
// per read and combination of filters set.
const preparedCap = 64

var prepared struct {
	sync.Mutex
	byAuthored map[string]string
}

// prepare expands an authored statement for table. The expansion parses the
// statement, which costs milliseconds (BenchmarkPrepareLogRows), so the
// result is kept by authored text.
func prepare(authored string, table string) (sql string, err error) {
	key := table + "\x00" + authored
	prepared.Lock()
	sql, hit := prepared.byAuthored[key]
	prepared.Unlock()
	if hit {
		return
	}
	sql, err = factsread.Prepare(authored, table)
	if err != nil {
		return "", err
	}
	prepared.Lock()
	if prepared.byAuthored == nil || len(prepared.byAuthored) >= preparedCap {
		prepared.byAuthored = make(map[string]string, preparedCap)
	}
	prepared.byAuthored[key] = sql
	prepared.Unlock()
	return
}
