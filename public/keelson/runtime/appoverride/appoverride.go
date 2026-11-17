// Package appoverride lets a deployment restate the descriptive properties
// of the apps it boots with — their names, summaries, icons, topics,
// keywords and maturity — by a SQL statement over keelson.apps (ADR-0376).
//
// The statement comes from two places, applied in order: the host's wiring
// (hostboot.Options.AppsOverrideSql), for a downstream binary, and
// KEELSON_APPS_OVERRIDE_SQL, for the operator. The second reads the
// table as the first left it. Each is evaluated once, at boot, on
// clickhouse-local, before the launch limit, so the predicates over
// keelson.apps (ADR-0272, ADR-0352, ADR-0364) select by the overridden
// values. Its result lands on the app registry, so the launcher and every
// other reader of the manifests agree with the table. Unset, nothing here
// runs and chlocal is not touched.
package appoverride

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/launchlimit"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Sql is the operator's override. It is spliced into
// `SELECT * FROM (<Sql>)`, so it carries the trust of any other variable
// the process reads.
var Sql = env.NewString(env.Spec{
	Name:        "KEELSON_APPS_OVERRIDE_SQL",
	Description: "ClickHouse SELECT over keelson.apps returning id and any of display, summary, icon, topics, keywords, maturity; each row restates those properties of that app at boot, before the KEELSON_*_WHERE predicates read them (ADR-0376, proposed). Applied after the host's own override. Rows it omits keep their values; unset means no override; set and not applicable, the host does not boot",
	Category:    env.CategorySystem,
})

// AppId is the bus identity the override is evaluated under.
const AppId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/appoverride"

// Writable are the keelson.apps columns an override may change, each
// backed by an app.ManifestOverride field.
var Writable = []string{
	app.OverriddenDisplay, app.OverriddenSummary, app.OverriddenIcon,
	app.OverriddenTopics, app.OverriddenKeywords, app.OverriddenMaturity,
}

// derived are the columns computed from writable ones. A statement may
// return them unchanged, as a statement listing every column does, or with the
// value the override implies; any other value is refused, since the
// registry recomputes them.
const (
	colTitle        = "title"
	colMaturityRank = "maturity_rank"
	colId           = "id"
)

// Apply evaluates the wiring's statement and then Sql against reg and
// applies what each returns. Neither set, it returns at once. Set, any
// failure — no chlocal, a statement that does not run, a row that names no
// registered app or changes a column it may not — is returned, so the host
// refuses to boot rather than run with properties other than those it was
// given.
func Apply(ctx context.Context, bus *inprocbus.Inst, reg *app.Registry, chlocalAvailable bool, wired string, log zerolog.Logger) (err error) {
	steps := []struct{ source, sql string }{
		{"hostboot.Options.AppsOverrideSql", wired},
		{Sql.Spec().Name, Sql.Get()},
	}
	for _, s := range steps {
		sql := strings.TrimSpace(s.sql)
		if sql == "" {
			continue
		}
		if !chlocalAvailable {
			err = eb.Build().Str("source", s.source).Errorf("appoverride: the override needs clickhouse-local, which did not start")
			return
		}
		var n int
		n, err = Transform(ctx, bus, reg, sql, log)
		if err != nil {
			err = eb.Build().Str("source", s.source).Errorf("appoverride: %w", err)
			return
		}
		log.Info().Str("source", s.source).Str("sql", sql).Int("overridden", n).Msg("appoverride: app properties overridden")
	}
	return
}

// Transform evaluates sql over keelson.apps as reg holds it now and applies
// the result to reg. n is the number of apps whose properties changed.
func Transform(ctx context.Context, bus *inprocbus.Inst, reg *app.Registry, sql string, log zerolog.Logger) (n int, err error) {
	in, err := query(ctx, bus, reg, "SELECT * FROM apps ORDER BY id", log)
	if err != nil {
		err = eh.Errorf("read keelson.apps: %w", err)
		return
	}
	// The newline keeps a trailing line comment in sql from swallowing the
	// parenthesis.
	out, err := query(ctx, bus, reg, "SELECT * FROM (\n"+sql+"\n)", log)
	if err != nil {
		err = eb.Build().Str("sql", sql).Errorf("evaluate the override: %w", err)
		return
	}
	overrides, err := Diff(in, out)
	if err != nil {
		return
	}
	if err = reg.OverrideManifests(overrides); err != nil {
		return
	}
	n = len(overrides)
	return
}

// Row is one keelson.apps row as ClickHouse serialises it in JSONEachRow.
type Row map[string]json.RawMessage

func query(ctx context.Context, bus *inprocbus.Inst, reg *app.Registry, sql string, log zerolog.Logger) (rows []Row, err error) {
	body, err := launchlimit.QueryAs(ctx, bus, reg, AppId, "appoverride", sql, "JSONEachRow", log)
	if err != nil {
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Row
		if err = json.Unmarshal(line, &r); err != nil {
			err = eh.Errorf("decode a row: %w", err)
			return
		}
		rows = append(rows, r)
	}
	err = sc.Err()
	return
}

// Diff turns a statement's rows (out) into overrides against the table it
// read (in). Each out row must carry an id that names one row of in, and
// no id twice; rows in does not appear in out keep their values. A column
// out carries that in lacks is refused, so a misspelt name is not read as
// nothing to change. A writable column becomes an override field; any
// other column must hold in's value (a derived one may hold the value the
// override implies instead).
func Diff(in []Row, out []Row) (overrides map[app.AppIdT]app.ManifestOverride, err error) {
	byId := make(map[string]Row, len(in))
	for _, r := range in {
		var id string
		if err = json.Unmarshal(r[colId], &id); err != nil {
			err = eh.Errorf("keelson.apps row without a string id: %w", err)
			return
		}
		byId[id] = r
	}
	seen := make(map[string]struct{}, len(out))
	for _, r := range out {
		raw, ok := r[colId]
		if !ok {
			err = eh.Errorf("the override returns no id column")
			return
		}
		var id string
		if err = json.Unmarshal(raw, &id); err != nil {
			err = eh.Errorf("the override's id column is not a string: %w", err)
			return
		}
		if _, dup := seen[id]; dup {
			err = eb.Build().Str("id", id).Errorf("the override returns the app twice")
			return
		}
		seen[id] = struct{}{}
		prev, ok := byId[id]
		if !ok {
			err = eb.Build().Str("id", id).Errorf("the override returns an app that is not registered")
			return
		}
		var o app.ManifestOverride
		var changed bool
		o, changed, err = rowOverride(id, prev, r)
		if err != nil {
			return
		}
		if changed {
			if overrides == nil {
				overrides = make(map[app.AppIdT]app.ManifestOverride)
			}
			overrides[app.AppIdT(id)] = o
		}
	}
	return
}

// rowOverride compares one returned row with the row it restates.
func rowOverride(id string, prev Row, r Row) (o app.ManifestOverride, changed bool, err error) {
	// Columns in a stable order, so the first refusal is reproducible.
	cols := make([]string, 0, len(r))
	for c := range r {
		cols = append(cols, c)
	}
	slices.Sort(cols)
	for _, c := range cols {
		v := r[c]
		was, known := prev[c]
		if !known {
			err = eb.Build().Str("id", id).Str("column", c).Errorf("the override returns a column keelson.apps does not have")
			return
		}
		if c == colId || bytes.Equal(v, was) {
			continue
		}
		if !slices.Contains(Writable, c) {
			if c == colTitle || c == colMaturityRank {
				// Checked once the writable fields are known.
				continue
			}
			err = eb.Build().Str("id", id).Str("column", c).Strs("writable", Writable).
				Errorf("the override changes a column it may not")
			return
		}
		if err = setField(&o, c, v); err != nil {
			err = eb.Build().Str("id", id).Str("column", c).Errorf("the override's value: %w", err)
			return
		}
		changed = true
	}
	err = checkDerived(id, prev, r, o)
	return
}

func setField(o *app.ManifestOverride, col string, v json.RawMessage) (err error) {
	switch col {
	case app.OverriddenDisplay, app.OverriddenSummary, app.OverriddenIcon:
		var s string
		if err = json.Unmarshal(v, &s); err != nil {
			return
		}
		switch col {
		case app.OverriddenDisplay:
			o.Display = &s
		case app.OverriddenSummary:
			o.Summary = &s
		default:
			o.Icon = &s
		}
	case app.OverriddenTopics:
		var ss []string
		if err = json.Unmarshal(v, &ss); err != nil {
			return
		}
		ts := make([]app.TopicT, 0, len(ss))
		for _, s := range ss {
			t, ok := app.ParseTopic(s)
			if !ok {
				err = eb.Build().Str("topic", s).Errorf("unregistered topic")
				return
			}
			ts = append(ts, t)
		}
		o.Topics = &ts
	case app.OverriddenKeywords:
		var ss []string
		if err = json.Unmarshal(v, &ss); err != nil {
			return
		}
		if ss == nil {
			ss = []string{}
		}
		o.Keywords = &ss
	case app.OverriddenMaturity:
		var s string
		if err = json.Unmarshal(v, &s); err != nil {
			return
		}
		m, ok := app.ParseMaturity(s)
		if !ok {
			err = eb.Build().Str("maturity", s).Errorf("unknown grade")
			return
		}
		o.Maturity = &m
	default:
		err = eh.Errorf("not a writable column")
	}
	return
}

// checkDerived refuses a derived column that holds neither its previous
// value nor the value the override implies.
func checkDerived(id string, prev Row, r Row, o app.ManifestOverride) (err error) {
	if v, ok := r[colMaturityRank]; ok && !bytes.Equal(v, prev[colMaturityRank]) {
		var rank int32
		if json.Unmarshal(v, &rank) != nil || o.Maturity == nil || rank != o.Maturity.Rank() {
			err = eb.Build().Str("id", id).Str("column", colMaturityRank).
				Errorf("the override changes a derived column; set maturity, and the rank follows")
			return
		}
	}
	if v, ok := r[colTitle]; ok && !bytes.Equal(v, prev[colTitle]) {
		err = eb.Build().Str("id", id).Str("column", colTitle).
			Errorf("the override changes a derived column; set display or icon, and the title follows")
		return
	}
	return
}
