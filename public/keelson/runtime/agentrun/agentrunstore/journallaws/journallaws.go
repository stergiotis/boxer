// Package journallaws is the journal's laws as SQL over the journal views
// (journalviews): the queries the integration lane runs against a journal
// written to a server, where the in-process checks of
// keelson('evidence_checks') read a store (ADR-0309 §SD6, ADR-0311 §SD8,
// ADR-0316 §SD4).
//
// A law here is a SELECT returning one row per breach — run, ordinal, why
// — over the entries view alone, so it reads metadata and never a body:
// the laws that need content (L7's required notes, L15's digests, L25's
// verbatim text, L26's outcome phase, L27's limits) are not here, and say
// so where they are deferred.
package journallaws

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journalviews"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Law is one law as a query: its name, the statement it checks, and the
// SELECT that returns its breaches as (run UInt64, ordinal UInt32, why
// String), ordered by run and ordinal.
type Law struct {
	Name      string
	Statement string
	Sql       string
}

// The columns of the entries view a law reads, by logical name, with the
// alias the law bodies use.
var entryReads = []struct{ name, alias string }{
	{"kind", "kind"}, {"key", "call_key"}, {"target", "cut"}, {"from", "from_ordinal"},
	{"label-level", "level"}, {"label-tier", "tier"}, {"label-exposure", "exposure"}, {"label-origin", "origin"},
}

// laws are the bodies over the common table e (run, ordinal and
// entryReads' aliases), in the order Compose returns them.
var laws = []Law{
	{Name: "L1", Statement: "every outcome follows an intent with its call key; every reply its request", Sql: `
SELECT o.run AS run, o.ordinal AS ordinal,
       concat('L1: the ', o.kind, ' under key ', o.call_key, ' has no earlier ', o.needs, ' under it') AS why
FROM (SELECT run, ordinal, kind, call_key, if(kind = 'outcome', 'intent', 'request') AS needs FROM e WHERE kind IN ('outcome', 'reply')) AS o
LEFT JOIN (SELECT run, call_key, kind, min(ordinal) AS first FROM e WHERE kind IN ('intent', 'request') GROUP BY run, call_key, kind) AS i
  ON i.run = o.run AND i.call_key = o.call_key AND i.kind = o.needs
WHERE i.first = 0 OR i.first > o.ordinal
ORDER BY run, ordinal`},
	{Name: "L3", Statement: "a compaction summary's label is at least the join of what it replaces", Sql: `
SELECT c.run AS run, c.ordinal AS ordinal,
       concat('L3: the summary carries level ', toString(c.level), ' tier ', toString(c.tier),
              ' below the join of what it replaces, level ', toString(max(x.level)), ' tier ', toString(max(x.tier))) AS why
FROM (SELECT * FROM e WHERE kind = 'compact') AS c
INNER JOIN e AS x ON x.run = c.run
WHERE (x.kind IN ('user', 'reply', 'outcome', 'note') AND x.ordinal >= c.from_ordinal AND x.ordinal < c.cut)
   OR (x.kind = 'compact' AND x.ordinal < c.ordinal)
GROUP BY c.run, c.ordinal, c.level, c.tier
HAVING c.level < max(x.level) OR c.tier < max(x.tier)
ORDER BY run, ordinal`},
	{Name: "L6", Statement: "a reply's label is at least its request's", Sql: `
SELECT r.run AS run, r.ordinal AS ordinal,
       concat('L6: reply ', r.call_key, ' carries level ', toString(r.level), ' tier ', toString(r.tier),
              ' below its request, level ', toString(q.level), ' tier ', toString(q.tier), ', or drops its marks') AS why
FROM (SELECT * FROM e WHERE kind = 'reply') AS r
INNER JOIN (SELECT run, call_key, max(level) AS level, max(tier) AS tier,
                   arrayDistinct(groupArrayArray(exposure)) AS exposure, arrayDistinct(groupArrayArray(origin)) AS origin
            FROM e WHERE kind = 'request' GROUP BY run, call_key) AS q
  ON q.run = r.run AND q.call_key = r.call_key
WHERE r.level < q.level OR r.tier < q.tier OR NOT hasAll(r.exposure, q.exposure) OR NOT hasAll(r.origin, q.origin)
ORDER BY run, ordinal`},
	{Name: "L22", Statement: "each compaction's from is the previous cut, the first's 0", Sql: `
SELECT run, ordinal, concat('L22: from ', toString(from_ordinal), ' is not the previous cut ', toString(prev_cut)) AS why
FROM (SELECT run, ordinal, from_ordinal, cut,
             row_number() OVER w AS n, lagInFrame(cut, 1, toUInt32(0)) OVER w AS prev_cut
      FROM e WHERE kind = 'compact'
      WINDOW w AS (PARTITION BY run ORDER BY ordinal ASC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW))
WHERE from_ordinal != prev_cut
ORDER BY run, ordinal`},
	{Name: "L23", Statement: "cuts strictly increase, and a cut is an earlier ordinal", Sql: `
SELECT run, ordinal,
       multiIf(cut = 0 OR cut >= ordinal, concat('L23: the cut ', toString(cut), ' is not an earlier ordinal'),
               concat('L23: the cut ', toString(cut), ' does not increase on ', toString(prev_cut))) AS why
FROM (SELECT run, ordinal, cut,
             row_number() OVER w AS n, lagInFrame(cut, 1, toUInt32(0)) OVER w AS prev_cut
      FROM e WHERE kind = 'compact'
      WINDOW w AS (PARTITION BY run ORDER BY ordinal ASC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW))
WHERE cut = 0 OR cut >= ordinal OR (n > 1 AND cut <= prev_cut)
ORDER BY run, ordinal`},
}

// Names lists the laws Compose returns, in order.
func Names() (names []string) {
	for _, l := range laws {
		names = append(names, l.Name)
	}
	return
}

// Compose returns the laws over the journal views in database (empty is
// the ledger's default), each a complete SELECT: the views must be
// installed (journalviews.Compose) for them to run. The entries view's
// physical column names are read from its composition, so a law never
// spells one.
func Compose(database string) (out []Law, err error) {
	views, err := journalviews.Compose(database)
	if err != nil {
		return nil, eh.Errorf("journallaws: %w", err)
	}
	if database == "" {
		database = strings.SplitN(strings.TrimPrefix(views[0].Sql, "CREATE OR REPLACE VIEW "), ".", 2)[0]
	}
	var entries *journalviews.View
	for i := range views {
		if views[i].Name == journalviews.ViewEntries {
			entries = &views[i]
		}
	}
	if entries == nil {
		return nil, eh.Errorf("journallaws: the views carry no " + journalviews.ViewEntries)
	}
	physical := map[string]string{}
	for _, c := range entries.Columns {
		physical[c.Name] = c.Physical
	}
	var b strings.Builder
	b.WriteString("WITH e AS (SELECT ")
	for i, c := range append([]struct{ name, alias string }{{"run", "run"}, {"ordinal", "ordinal"}}, entryReads...) {
		p, ok := physical[c.name]
		if !ok {
			return nil, eb.Build().Str("column", c.name).Errorf("journallaws: the entries view has no such column")
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`"` + p + `" AS ` + c.alias)
	}
	b.WriteString(" FROM " + database + "." + journalviews.ViewEntries + ")")
	with := b.String()
	out = make([]Law, 0, len(laws))
	for _, l := range laws {
		out = append(out, Law{Name: l.Name, Statement: l.Statement, Sql: with + "\n" + strings.TrimSpace(l.Sql)})
	}
	return
}
