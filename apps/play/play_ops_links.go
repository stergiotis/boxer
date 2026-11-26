package play

// Links into play (ADR-0409): set_sql and query take boxer:
// links, spelled with the statement as the query's sql key —
//
//	boxer:open/app/play/set_sql@1?sql=SELECT+1
//	boxer:open/app/play/query@2?sql=SELECT+1&limit=10
//
// A link a task minted runs query under the task's grant. A link written
// by anyone else puts the statement in a new window's buffer and runs
// nothing: query's stage is stage_query, and play marks what the link put
// there on the status line until the person edits or runs it. Live never
// runs it on its own: Live reruns only a statement already run.

import (
	"net/url"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const opStageQuery = "stage_query"

// linkExampleSql is the statement the codecs' examples spell.
const linkExampleSql = "SELECT number, number * number AS square FROM system.numbers LIMIT 5"

// setSqlLinkCodec spells set_sql's argument: the statement as sql.
var setSqlLinkCodec = applink.Codec[SetSqlArgs]{
	Format: func(v SetSqlArgs) (tail string, err error) {
		return applink.Tail{Query: url.Values{"sql": {v.Sql}}}.String(), nil
	},
	Parse: func(tail string) (v SetSqlArgs, err error) {
		t, err := parseSqlTail(tail, "sql")
		if err != nil {
			return
		}
		v.Sql, _ = t.Value("sql")
		return
	},
	Examples: []SetSqlArgs{{Sql: linkExampleSql}},
}

// queryLinkCodec spells query's argument: the statement as sql, and the
// statement number and the row limit when set. Fields, keep_as and
// order_by have no spelling: a link carries a statement to look at, not a
// session data set to keep.
var queryLinkCodec = applink.Codec[QueryArgs]{
	Format: func(v QueryArgs) (tail string, err error) {
		if len(v.Fields) > 0 || v.KeepAs != "" || len(v.OrderBy) > 0 {
			err = eb.Build().Errorf("play: a link to query carries sql, statement and limit only")
			return
		}
		q := url.Values{"sql": {v.Sql}}
		if v.Statement != 0 {
			q.Set("statement", strconv.FormatInt(int64(v.Statement), 10))
		}
		if v.Limit != 0 {
			q.Set("limit", strconv.FormatUint(uint64(v.Limit), 10))
		}
		return applink.Tail{Query: q}.String(), nil
	},
	Parse: func(tail string) (v QueryArgs, err error) {
		t, err := parseSqlTail(tail, "sql", "statement", "limit")
		if err != nil {
			return
		}
		v.Sql, _ = t.Value("sql")
		if s, ok := t.Value("statement"); ok {
			n, perr := strconv.ParseInt(s, 10, 32)
			if perr != nil || n < 1 || strconv.FormatInt(n, 10) != s {
				err = eb.Build().Str("statement", s).Errorf("play: statement is a number from 1")
				return
			}
			v.Statement = int32(n)
		}
		if s, ok := t.Value("limit"); ok {
			n, perr := strconv.ParseUint(s, 10, 32)
			if perr != nil || n < 1 || strconv.FormatUint(n, 10) != s {
				err = eb.Build().Str("limit", s).Errorf("play: limit is a number from 1")
				return
			}
			v.Limit = uint32(n)
		}
		return
	},
	Examples: []QueryArgs{{Sql: linkExampleSql}, {Sql: linkExampleSql + "; SELECT 1", Statement: 2, Limit: 10}},
}

// parseSqlTail reads a tail that is a query of the allowed keys, sql among
// them, and nothing else.
func parseSqlTail(tail string, allowed ...string) (t applink.Tail, err error) {
	if t, err = applink.ParseTail(tail); err != nil {
		return
	}
	if len(t.Segments) > 0 || t.Fragment != "" {
		err = eb.Build().Errorf("play: a link carries its statement as ?sql=, with no path or fragment")
		return
	}
	if err = t.OnlyKeys(allowed...); err != nil {
		return
	}
	if _, ok := t.Value("sql"); !ok {
		err = eb.Build().Errorf("play: a link carries its statement as ?sql=")
	}
	return
}

// addLinkStageOp declares stage_query: query's stage, which an authorless
// link calls instead of running anything.
func addLinkStageOp(playOps *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(playOps, app.OperationSpec{Name: opStageQuery, Version: 1,
		Summary: "put a query's statement in the buffer without running it: what a link to query does when no task's grant runs it",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSql},
		Rebuild: app.OperationRebuildRefold, Fork: app.OperationForkPerInstance,
		Follows: []string{"the person runs it, or edits it; play marks it on the status line until then"}},
		func(inst *PlayLauncher, call app.OperationCall, in QueryArgs) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			inst.inner.swapSql(in.Sql)
			inst.inner.markLink(call)
			return appops.None{}, nil
		})
}

// markLink marks a buffer a link written by someone else put in place: the
// status line says so until the person edits the buffer or runs it
// (ADR-0409 §SD5). A link the person or the system wrote is not marked.
func (inst *PlayApp) markLink(call app.OperationCall) {
	if !call.LinkMarked() {
		return
	}
	who := "external text"
	if call.Link.Level >= applink.LevelOwnModel {
		who = "a model"
	}
	inst.linkNotice = "the statement came from a link " + who + " wrote; read it before you run it"
	inst.linkSql = inst.sql
}

// linkNoticeNow is the link's mark while the buffer still holds what the
// link put there.
func (inst *PlayApp) linkNoticeNow() (s string) {
	if inst.linkNotice != "" && inst.sql == inst.linkSql {
		return inst.linkNotice
	}
	return ""
}
