package tally

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// componentHit is one registered kind that names the entry: the kind, the
// table it was found in, and how many rows carry the entry's key there.
type componentHit struct {
	kind  string
	table string
	rows  int64
}

// componentProbe is one kind's presence query over one table, keyed by the
// entry's backbone triple (mount, snapshot, path). The triple is the key of
// every facts-shaped table (ADR-0198 §SD3), so a component another domain
// formulated over the same rows — in boxer.facts or in a facts-shaped table
// of its own — answers the same question with the same three predicates.
type componentProbe struct {
	kind  string
	table string
	sql   string
}

var (
	plainID         = mustPlain("id")
	plainTs         = mustPlain("ts")
	plainNaturalKey = mustPlain("naturalKey")
)

func mustPlain(name string) string {
	q, err := ladingschema.PhysicalPlainName(name)
	if err != nil {
		panic(err)
	}
	return q
}

// componentProbes lists the probes for an entry: the store's own entry kinds
// over boxer.fsmeta first (the root row is two components, ADR-0198 M1), then
// every kind a host registered in the default component registry
// (ADR-0189) over its own table.
func componentProbes(reg *componentsql.Registry, mount identifier.TaggedId, snap time.Time, p string) (probes []componentProbe) {
	where := fmt.Sprintf("%s = %d AND %s = fromUnixTimestamp64Nano(toInt64(%d), 'UTC') AND %s = %s",
		plainID, mount.Value(), plainTs, snap.UnixNano(), plainNaturalKey, ladingschema.QuoteLiteral(p))
	add := func(kind, table, presence string) {
		probes = append(probes, componentProbe{
			kind:  kind,
			table: table,
			sql:   fmt.Sprintf("SELECT count() FROM %s WHERE (%s) AND %s", table, presence, where),
		})
	}
	metaKinds := make([]string, 0, len(ladingmeta.MetaComponentSQL.Kinds))
	for k := range ladingmeta.MetaComponentSQL.Kinds {
		metaKinds = append(metaKinds, k)
	}
	sort.Strings(metaKinds)
	for _, k := range metaKinds {
		add(k, qualifiedTable(ladingmeta.MetaComponentSQL.Table), ladingmeta.MetaComponentSQL.Kinds[k].Presence)
	}
	if reg != nil {
		for _, k := range reg.Kinds() {
			b, ok := reg.Lookup(k)
			if !ok || b.Presence == "" {
				continue
			}
			if strings.EqualFold(b.Table, ladingmeta.MetaComponentSQL.Table) {
				continue // already covered above
			}
			add(k, qualifiedTable(b.Table), b.Presence)
		}
	}
	return
}

// qualifiedTable prefixes the store's database when a set names a bare table.
func qualifiedTable(table string) string {
	if strings.Contains(table, ".") {
		return table
	}
	return ladingschema.DatabaseName + "." + table
}

// loadComponents runs every probe; a probe whose table or columns do not
// exist (a set over another shape) is skipped, not fatal. Off the render
// thread.
func loadComponents(ctx context.Context, exec recordstore.ExecutorI, reg *componentsql.Registry, mount identifier.TaggedId, snap time.Time, p string) (hits []componentHit, err error) {
	for _, probe := range componentProbes(reg, mount, snap, p) {
		if ctx.Err() != nil {
			err = ctx.Err()
			return
		}
		n, qerr := countQuery(ctx, exec, probe.sql)
		if qerr != nil {
			continue
		}
		if n > 0 {
			hits = append(hits, componentHit{kind: probe.kind, table: probe.table, rows: n})
		}
	}
	return
}

// countQuery reads the single count a probe returns.
func countQuery(ctx context.Context, exec recordstore.ExecutorI, sql string) (n int64, err error) {
	res, err := runTable(ctx, exec, sql)
	if err != nil {
		return
	}
	if len(res.rows) == 0 || len(res.rows[0]) == 0 {
		return 0, nil
	}
	_, perr := fmt.Sscan(res.rows[0][0], &n)
	if perr != nil {
		err = perr
	}
	return
}
