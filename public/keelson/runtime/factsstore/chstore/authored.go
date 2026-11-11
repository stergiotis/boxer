package chstore

import (
	"context"
	"io"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/factsread"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

// preparedCap bounds the memo of expanded statements. A read whose filter
// carries a moving timestamp authors a new text on every call and misses;
// the reads that repeat (a run's events, the launch statistics) hit.
const preparedCap = 64

var prepared struct {
	sync.Mutex
	byAuthored map[string]string
}

// prepare expands an authored statement for table and appends format. The
// expansion parses the statement, which costs milliseconds
// (BenchmarkPrepareLogRows), so the result is kept by authored text.
func prepare(authored string, table string, format string) (sql string, err error) {
	key := table + "\x00" + authored
	prepared.Lock()
	sql, hit := prepared.byAuthored[key]
	prepared.Unlock()
	if !hit {
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
	}
	return sql + "\nFORMAT " + format, nil
}

// queryAuthored expands authored, runs it and reads the whole body.
func (inst *Store) queryAuthored(ctx context.Context, what string, authored string, format string) (raw []byte, err error) {
	sql, err := prepare(authored, inst.qualifiedTable(), format)
	if err != nil {
		err = eb.Build().Str("read", what).Errorf("chstore: compose: %w", err)
		return
	}
	body, err := inst.cli.Query(ctx, sql)
	if err != nil {
		err = eb.Build().Str("read", what).Errorf("chstore: query: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	raw, err = io.ReadAll(body)
	if err != nil {
		err = eb.Build().Str("read", what).Errorf("chstore: body: %w", err)
	}
	return
}
