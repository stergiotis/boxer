package play

// The schema half of play's catalog: what a model composing a query needs
// to know before it writes into the person's buffer — the pinned endpoint's
// tables, a table's columns and, for a leeway table, its sections, handles
// and membership channels — and a check of a draft that runs nothing. They
// are the Model tab's tools (ADR-0139 §SD8) moved to where a coordinator
// reaches them, so exploring no longer goes through set_sql and run.
//
// The three are external reads (ADR-0269 §SD1): fixed probes of the
// endpoint's own catalog, never a statement the caller writes. A probe an
// agent causes needs the endpoint among the grant's destinations, as a run
// does (ADR-0270 §SD2); validate_sql degrades instead of refusing, making
// the rewrite without the steps that read the catalog.

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

const (
	opValidateSql   = "validate_sql"
	opListTables    = "list_tables"
	opDescribeTable = "describe_table"
)

// Bounds on what the schema reads return and how long they may take.
const (
	schemaOpTimeout    = 8 * time.Second
	schemaMaxTables    = 200
	schemaMaxColumns   = 400
	schemaMaxComment   = 200
	schemaMaxStatement = 16 << 10
)

// leewayReading is the one paragraph a model needs to read a leeway table
// it has just described; play's help (howto-example-queries) has the rest.
const leewayReading = "Write handles, never physical names: `section:column` reads one value column's lane " +
	"and `section:*` all of a section's value columns, both in backticks. " +
	"LW_GET('section', membership) reads the one attribute carrying a membership, LW_GET_NULL gives NULL when " +
	"it is absent, LW_GET_LIST reads a list-shaped column; add 'col:<column>' when the section has several " +
	"value columns and 'chan:<channel>' when it has several channels. A ref channel takes a registry id or the " +
	"membership's name (keelson('memberships') lists them); a verbatim channel takes the name as a string."

// ValidateArgs is validate_sql's argument.
type ValidateArgs struct {
	Sql string `json:",omitzero" desc:"the statement to check; the buffer when left out"`
}

// ValidateResult is validate_sql's result.
type ValidateResult struct {
	Valid     bool   `desc:"true when the statement parses, canonicalizes and every client-side rewrite play applied succeeded"`
	Error     string `json:",omitzero" desc:"why it is not valid"`
	Canonical string `json:",omitzero" desc:"the statement in the canonical dialect, as written (handles and macros unexpanded)"`
	Kind      string `json:",omitzero" desc:"read, or the reason an agent's run would not count as a plain read"`
	// Expanded is false when the catalog-reading steps were left out.
	Expanded bool     `desc:"true when sent is play's whole rewrite; false when the grant does not list the endpoint, so the steps that read its catalog were left out and declined names them"`
	Sent     string   `json:",omitzero" desc:"what play would send: handles, LW_GET and the other client-side macros expanded, as far as expanded says"`
	Declined []string `json:",omitzero" desc:"the steps left out without the endpoint: handles, LW_GET and LW_SEL, fs(), the constructor target; handles are then not checked"`
	Rewrites []string `json:",omitzero" desc:"client-side rewrite steps that failed, each with its error; the statement then ships with that step skipped"`
	Handles  []string `json:",omitzero" desc:"leeway handles that name no section or column of their table, each with candidates; they would ship unresolved"`
	Run      string   `json:",omitzero" desc:"allowed when run would accept this statement under the grant, else the agent limit it would hit"`
	Needs    []string `json:",omitzero" desc:"destinations request_access would have to add: for a run, and the endpoint for the whole rewrite"`
	Confined bool     `desc:"true when the statement reads confined data, which a run would carry into the window's label"`
}

// TablesArgs is list_tables' argument.
type TablesArgs struct {
	Database string `json:",omitzero" desc:"the database; the endpoint's current one when left out"`
	Search   string `json:",omitzero" desc:"a word to find in a table's name or comment"`
}

// TableInfo is one table of list_tables.
type TableInfo struct {
	Name    string `desc:"the table's name"`
	Engine  string `desc:"its engine"`
	Comment string `json:",omitzero" desc:"its comment, cut at 200 bytes"`
	Rows    uint64 `desc:"the engine's row count, 0 when it keeps none"`
	Columns uint32 `desc:"the number of columns"`
	Leeway  bool   `desc:"true when its column names carry leeway's encoding; describe_table lists its sections and handles"`
}

// TableList is list_tables' result.
type TableList struct {
	Database  string      `desc:"the database listed"`
	Databases []string    `json:",omitzero" desc:"the endpoint's other databases, when database was left out"`
	Tables    []TableInfo `desc:"the tables"`
	Truncated bool        `desc:"true when the bound cut the list; search narrows it"`
}

// DescribeArgs is describe_table's argument.
type DescribeArgs struct {
	Table string `desc:"a bare table name, or database.table"`
}

// ColumnInfo is one column of describe_table.
type ColumnInfo struct {
	Name    string `desc:"the column's name"`
	Type    string `desc:"its ClickHouse type"`
	Comment string `json:",omitzero" desc:"its comment, cut at 200 bytes"`
}

// LeewayValue is one value column of a leeway section.
type LeewayValue struct {
	Handle  string `desc:"the handle to write, in backticks: section:column"`
	Type    string `desc:"the physical column's ClickHouse type"`
	List    bool   `json:",omitzero" desc:"true for an array- or set-valued column, read with LW_GET_LIST; left out for a scalar"`
	Comment string `json:",omitzero" desc:"the physical column's comment, cut at 200 bytes"`
}

// LeewayChannel is one membership channel of a tagged section.
type LeewayChannel struct {
	Name     string `desc:"the channel, as chan:<name> spells it"`
	Verbatim bool   `json:",omitzero" desc:"true when memberships are names; left out when they are registry ids (keelson('memberships') names them)"`
	Single   bool   `json:",omitzero" desc:"true when the schema declares one membership per attribute"`
	Mixed    bool   `json:",omitzero" desc:"true when a membership is shared and a high-cardinality parameter tells its attributes apart: LW_GET then needs param:"`
}

// LeewaySection is one section of a leeway table.
type LeewaySection struct {
	Name     string          `desc:"the section, as handles and LW_GET name it"`
	Tagged   bool            `json:",omitzero" desc:"true for a tagged section, whose attributes carry memberships; left out for a plain one, one value per row"`
	Values   []LeewayValue   `desc:"its value columns"`
	Channels []LeewayChannel `json:",omitzero" desc:"its membership channels"`
}

// TableDescription is describe_table's result.
type TableDescription struct {
	Database string `desc:"the database"`
	Table    string `desc:"the table"`
	Leeway   bool   `desc:"true for a leeway table: write its handles, not its physical names"`
	// Columns are a plain table's columns, or a leeway table's columns that
	// carry no leeway encoding.
	Columns   []ColumnInfo    `json:",omitzero" desc:"the columns to write by name: every column of a plain table, the unencoded ones of a leeway table"`
	Sections  []LeewaySection `json:",omitzero" desc:"a leeway table's sections"`
	Physical  int             `json:",omitzero" desc:"a leeway table's physical column count, whose names are left out"`
	Reading   string          `json:",omitzero" desc:"how to read a leeway table in play"`
	Truncated bool            `desc:"true when the bound cut the columns"`
}

func addSchemaOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.ExternalRead(s, app.OperationSpec{Name: opValidateSql, Version: 1,
		Summary: "check a statement without running it: grammar, canonical form, play's rewrite of it, and whether run would accept it",
		Reads:   []string{opsResSql}, Agents: true,
		Follows: []string{"nothing runs and the buffer is unchanged; set_sql and run when it is right"}},
		func(sn opsSnap, call app.OperationCall, in ValidateArgs) (ValidateResult, error) {
			if !sn.mounted {
				return ValidateResult{}, app.RefuseOperation("the window has not mounted")
			}
			stmt := in.Sql
			if strings.TrimSpace(stmt) == "" {
				stmt = sn.state.Sql
			}
			return validateStatement(sn.client, call.OnBehalfOf, stmt)
		})
	appops.ExternalRead(s, app.OperationSpec{Name: opListTables, Version: 1,
		Summary: "list the endpoint's tables with their engine, row count and comment, and which are leeway tables",
		Agents:  true, Untrusted: true,
		Follows: []string{"describe_table reads one table's columns, or a leeway table's sections and handles"}},
		func(sn opsSnap, call app.OperationCall, in TablesArgs) (TableList, error) {
			if err := schemaReadable(sn, call.OnBehalfOf); err != nil {
				return TableList{}, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), schemaOpTimeout)
			defer cancel()
			return listTables(ctx, sn.client, in)
		})
	appops.ExternalRead(s, app.OperationSpec{Name: opDescribeTable, Version: 1,
		Summary: "describe one table: its columns, or for a leeway table its sections, the handles to write and the membership channels",
		Agents:  true, Untrusted: true},
		func(sn opsSnap, call app.OperationCall, in DescribeArgs) (TableDescription, error) {
			if err := schemaReadable(sn, call.OnBehalfOf); err != nil {
				return TableDescription{}, err
			}
			if strings.TrimSpace(in.Table) == "" {
				return TableDescription{}, app.RefuseOperation("name the table; list_tables lists them")
			}
			ctx, cancel := context.WithTimeout(context.Background(), schemaOpTimeout)
			defer cancel()
			return describeTable(ctx, sn.client, in.Table)
		})
}

// endpointDestination is how a grant names the pinned endpoint.
func endpointDestination(client *Client) (dest string) {
	return DestinationClickHouse(endpointHost(client.URL()))
}

// schemaReadable refuses a catalog probe the window cannot make: no endpoint,
// or an agent whose grant does not list it.
func schemaReadable(sn opsSnap, obo *app.OnBehalfOf) (err error) {
	switch {
	case !sn.mounted:
		return app.RefuseOperation("the window has not mounted")
	case sn.client == nil:
		return app.RefuseOperation("the window has no endpoint")
	}
	if dest := endpointDestination(sn.client); obo != nil && !slices.Contains(obo.Destinations, dest) {
		return app.RefuseForDestinations("the grant does not list "+dest+", whose catalog this reads", dest)
	}
	return
}

func validateStatement(client *Client, obo *app.OnBehalfOf, stmt string) (out ValidateResult, err error) {
	if err = statementBounds(stmt); err != nil {
		return
	}
	out.Canonical, err = orchestrator.Validate(stmt)
	if err != nil {
		out.Error, err = err.Error(), nil
		return
	}
	out.Kind = statementKind(stmt)
	if client == nil {
		out.Valid = true
		return
	}
	var failed, declined []string
	observe := func(o passreg.ApplyObservation) {
		switch {
		case o.Err != nil:
			failed = append(failed, o.Name+": "+o.Err.Error())
		case o.Outcome == passreg.ApplyOutcomeDeclined:
			declined = append(declined, o.Name)
		}
	}
	residual, _, catalog := client.rewriteFor(obo, stmt, observe)
	if catalog {
		out.Handles = unresolvedHandles(client, stmt)
	} else {
		out.Declined = declined
	}
	out.Expanded, out.Sent, out.Rewrites = catalog, residual, failed
	if _, perr := nanopass.Parse(residual); perr != nil {
		out.Error = "play's rewrite of the statement does not parse: " + perr.Error()
		return
	}
	switch {
	case len(out.Handles) > 0:
		out.Error = "a leeway handle does not resolve"
	case len(failed) > 0:
		out.Error = "a client-side rewrite failed"
	default:
		out.Valid = true
	}
	dec := client.previewDispatch(residual, "")
	out.Confined = dec.sensitivity == queryengine.SensitivityConfined
	if !catalog && dec.class != dispatchClassIntrospection {
		out.Needs = []string{endpointDestination(client)}
	}
	if obo == nil {
		return
	}
	out.Run = "allowed"
	if lerr := checkAgentLimits(residual, dec, obo, client.datasetAliasOf()); lerr != nil {
		out.Run = lerr.Error()
		if limit, ok := lerr.(*AgentLimitError); ok && limit.Destination != "" && !slices.Contains(out.Needs, limit.Destination) {
			out.Needs = append(out.Needs, limit.Destination)
		}
	}
	return
}

// unresolvedHandles are the leeway handles of stmt the client's resolver
// cannot map, as the Diagnostics pane lists them.
func unresolvedHandles(client *Client, stmt string) (out []string) {
	b, ok := client.passBinding.(*clientPassBinding)
	if !ok || b.Resolver == nil {
		return
	}
	_, _ = passes.ResolveColumnNames(b.Resolver, "", func(d passes.ColumnDiagnostic) {
		line := d.Handle + ": " + d.Message
		if len(d.Candidates) > 0 {
			line += "; candidates: " + strings.Join(d.Candidates, ", ")
		}
		out = append(out, line)
	}).Run(stmt)
	return
}

// statementKind is "read", or why the statement is not a plain read.
func statementKind(stmt string) (kind string) {
	pr, err := nanopass.Parse(stmt)
	if err != nil {
		return "unclassifiable"
	}
	class, _, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil {
		return "unclassifiable"
	}
	if class == analysis.QuerySecurityRead {
		return "read"
	}
	return class.String()
}

// jsonRows decodes a JSONEachRow body.
func jsonRows[T any](raw []byte) (rows []T, err error) {
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r T
		if err = json.Unmarshal([]byte(line), &r); err != nil {
			return
		}
		rows = append(rows, r)
	}
	return
}

const catalogDb = "if({db:String} = '', currentDatabase(), {db:String})"

func listTables(ctx context.Context, client *Client, in TablesArgs) (out TableList, err error) {
	type row struct {
		Db      string `json:"db"`
		Name    string `json:"name"`
		Engine  string `json:"engine"`
		Comment string `json:"comment"`
		Rows    uint64 `json:"rows"`
		Columns uint32 `json:"columns"`
		Encoded uint32 `json:"encoded"`
	}
	q := "SELECT " + catalogDb + " AS db, t.name AS name, t.engine AS engine, t.comment AS comment, " +
		"ifNull(t.total_rows, 0) AS rows, toUInt32(c.n) AS columns, toUInt32(c.encoded) AS encoded " +
		"FROM system.tables AS t LEFT JOIN (SELECT table, count() AS n, countIf(position(name, ':') > 0) AS encoded " +
		"FROM system.columns WHERE database = " + catalogDb + " GROUP BY table) AS c ON c.table = t.name " +
		"WHERE t.database = " + catalogDb + " AND NOT t.is_temporary " +
		"AND (positionCaseInsensitive(t.name, {q:String}) > 0 OR positionCaseInsensitive(t.comment, {q:String}) > 0) " +
		"ORDER BY t.name LIMIT " + itoa(schemaMaxTables+1) +
		" SETTINGS output_format_json_quote_64bit_integers = 0 FORMAT JSONEachRow"
	raw, err := client.queryTabSeparated(ctx, q, map[string]string{"db": strings.TrimSpace(in.Database), "q": strings.TrimSpace(in.Search)})
	if err != nil {
		return
	}
	rows, err := jsonRows[row](raw)
	if err != nil {
		return
	}
	out.Database = strings.TrimSpace(in.Database)
	for i, r := range rows {
		out.Database = r.Db
		if i == schemaMaxTables {
			out.Truncated = true
			break
		}
		out.Tables = append(out.Tables, TableInfo{Name: r.Name, Engine: r.Engine, Comment: truncateBytes(r.Comment, schemaMaxComment),
			Rows: r.Rows, Columns: r.Columns, Leeway: r.Encoded > 0 && r.Encoded*2 >= r.Columns})
	}
	if strings.TrimSpace(in.Database) == "" {
		type dbRow struct {
			Name string `json:"name"`
		}
		raw, err = client.queryTabSeparated(ctx, "SELECT name FROM system.databases "+
			"WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') AND name != currentDatabase() "+
			"ORDER BY name LIMIT 100 FORMAT JSONEachRow", nil)
		if err != nil {
			return
		}
		var dbs []dbRow
		if dbs, err = jsonRows[dbRow](raw); err != nil {
			return
		}
		for _, d := range dbs {
			out.Databases = append(out.Databases, d.Name)
		}
	}
	return
}

func describeTable(ctx context.Context, client *Client, table string) (out TableDescription, err error) {
	type row struct {
		Db      string `json:"db"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Comment string `json:"comment"`
	}
	db, name := splitQualified(table)
	q := "SELECT " + catalogDb + " AS db, name, type, comment FROM system.columns " +
		"WHERE table = {tbl:String} AND database = " + catalogDb + " ORDER BY position LIMIT 4000 FORMAT JSONEachRow"
	raw, err := client.queryTabSeparated(ctx, q, map[string]string{"tbl": name, "db": db})
	if err != nil {
		return
	}
	rows, err := jsonRows[row](raw)
	if err != nil {
		return
	}
	if len(rows) == 0 {
		return out, app.RefuseOperation("no table " + table + "; list_tables lists them")
	}
	out.Database, out.Table = rows[0].Db, name
	var sections []string
	var resolver *lwsql.Resolver
	if b, ok := client.passBinding.(*clientPassBinding); ok && b.Resolver != nil {
		resolver = b.Resolver
		// The resolver keys a table by the database a query names it with, so
		// it is asked with the spelling the caller used.
		sections = resolver.Sections(db, name)
	}
	if len(sections) == 0 {
		for i, r := range rows {
			if i == schemaMaxColumns {
				out.Truncated = true
				break
			}
			out.Columns = append(out.Columns, ColumnInfo{Name: r.Name, Type: r.Type, Comment: truncateBytes(r.Comment, schemaMaxComment)})
		}
		return
	}
	byPhysical := make(map[string]row, len(rows))
	for _, r := range rows {
		if strings.Contains(r.Name, ":") {
			byPhysical[r.Name] = r
			out.Physical++
			continue
		}
		out.Columns = append(out.Columns, ColumnInfo{Name: r.Name, Type: r.Type, Comment: truncateBytes(r.Comment, schemaMaxComment)})
	}
	out.Leeway, out.Reading = true, leewayReading
	for _, s := range sections {
		lanes, ok := resolver.ExtractLanesFor(db, name, s)
		if !ok {
			continue
		}
		sec := LeewaySection{Name: lanes.Section, Tagged: len(lanes.Channels) > 0}
		for _, v := range lanes.Values {
			p := byPhysical[v.Physical]
			sec.Values = append(sec.Values, LeewayValue{Handle: lanes.Section + ":" + v.Name, Type: p.Type, List: v.Shape == lwextract.ShapeList,
				Comment: truncateBytes(p.Comment, schemaMaxComment)})
		}
		for _, c := range lanes.Channels {
			sec.Channels = append(sec.Channels, LeewayChannel{Name: c.Name, Verbatim: c.Verbatim, Single: c.SingleMembership, Mixed: c.Param != ""})
		}
		out.Sections = append(out.Sections, sec)
	}
	return
}
