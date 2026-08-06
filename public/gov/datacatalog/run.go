package datacatalog

import (
	"context"
	"encoding/json/v2"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// CatalogRow is one row of boxer.tables_catalog — the inventory, both kinds.
//
// ClassifyDetail has one meaning throughout: *why this table has no row in
// boxer.tables_leeway*. For an opaque table that is the parse failure; for a
// leeway table it is a normalization failure, which is rare and a defect worth
// seeing; and it is empty exactly when the table did land in tables_leeway.
type CatalogRow struct {
	Ref              TableRef
	Engine           string
	Kind             KindE
	NColumns         uint32
	NormalizedSchema string
	ClassifyDetail   string
}

// LeewayRow is one row of boxer.tables_leeway — the restoration payload.
type LeewayRow struct {
	Ref            TableRef
	TableRowConfig string
	SchemaHash     uint64
	NAttrs         uint32
	AttrKeys       []string
	DescJson       string
}

// ShapeRow is one row of boxer.tables_opaque_shapes — one satisfied
// (opaque table, panel shape).
type ShapeRow struct {
	Ref   TableRef
	Shape string
}

// Result is everything one run computed, stamped. It is the writer's input and
// what `--dry-run` counts; nothing in it has touched the server yet.
type Result struct {
	RunId        string
	DiscoveredAt time.Time
	Catalog      []CatalogRow
	Leeway       []LeewayRow
	Pairs        []Pair
	Shapes       []ShapeRow
}

// NewRunId mints a run's stamp. Every row of every table carries it, so a
// consumer can tell a join that spans a rebuild boundary from one that does
// not.
func NewRunId() (id string, err error) {
	id, err = gonanoid.New()
	if err != nil {
		err = eh.Errorf("unable to mint run id: %w", err)
	}
	return
}

// Analyze turns a snapshot of discovered tables into the four tables' rows.
// It is the whole engine and touches nothing: no server, no clock — runId and
// discoveredAt are handed in, so a test pins them and two runs over the same
// input differ in nothing else.
//
// The single-threaded pair loop is the expensive part; see [RelatePairs].
//
// A table that classifies as leeway but fails to yield attribute keys is
// reported through its CatalogRow's ClassifyDetail and left out of
// tables_leeway and the pair matrix — visible rather than dropped. log may be
// the zero Logger.
func Analyze(tables []TableSnapshot, runId string, discoveredAt time.Time, log zerolog.Logger) (res Result, err error) {
	var ops *common.TableOperations
	ops, err = common.NewTableOperations()
	if err != nil {
		err = eh.Errorf("unable to create table operations: %w", err)
		return
	}
	var battery *panelshapes.Battery
	battery, err = panelshapes.NewBattery()
	if err != nil {
		err = eh.Errorf("unable to compile panel shapes: %w", err)
		return
	}

	SortTables(tables)
	res = Result{
		RunId:        runId,
		DiscoveredAt: discoveredAt,
		Catalog:      make([]CatalogRow, 0, len(tables)),
		Leeway:       make([]LeewayRow, 0, len(tables)),
		Shapes:       make([]ShapeRow, 0, len(tables)),
	}
	leewayTables := make([]LeewayTable, 0, len(tables))
	for _, snap := range tables {
		cl := Classify(snap.ColumnNames())
		row := CatalogRow{
			Ref:              snap.Ref,
			Engine:           snap.Engine,
			Kind:             cl.Kind,
			NColumns:         uint32(len(snap.Columns)),
			NormalizedSchema: NormalizedSchema(snap.Columns),
			ClassifyDetail:   cl.Detail(),
		}
		switch cl.Kind {
		case KindLeeway:
			lt, ltErr := NewLeewayTable(ops, snap.Ref, cl)
			if ltErr != nil {
				// The naming grammar produced something the normalizer
				// rejects. Rare, and a defect — but one bad table must not
				// cost the whole catalog, so it is recorded and skipped.
				log.Warn().Err(ltErr).Str("table", snap.Ref.String()).
					Msg("datacatalog: leeway table did not normalize; omitted from tables_leeway")
				row.ClassifyDetail = ltErr.Error()
				break
			}
			var descJson string
			descJson, err = marshalDesc(lt.Table)
			if err != nil {
				err = eb.Build().Str("table", snap.Ref.String()).Errorf("unable to serialize table description: %w", err)
				return
			}
			leewayTables = append(leewayTables, lt)
			res.Leeway = append(res.Leeway, LeewayRow{
				Ref:            snap.Ref,
				TableRowConfig: lt.RowConfig.String(),
				SchemaHash:     lt.SchemaHash,
				NAttrs:         uint32(len(lt.AttrKeys)),
				AttrKeys:       lt.AttrKeys,
				DescJson:       descJson,
			})
		case KindOpaque:
			// Shapes are matched against opaque tables only (§SD4): a leeway
			// table's physical names would satisfy patterns for reasons that
			// have nothing to do with what its columns mean.
			for _, shape := range battery.Match(row.NormalizedSchema) {
				res.Shapes = append(res.Shapes, ShapeRow{Ref: snap.Ref, Shape: shape})
			}
		}
		res.Catalog = append(res.Catalog, row)
	}

	res.Pairs, err = RelatePairs(ops, leewayTables)
	if err != nil {
		err = eh.Errorf("unable to build the pair matrix: %w", err)
		res = Result{}
		return
	}
	return
}

// marshalDesc renders a TableDesc as its DTO in JSON — the desc_json column.
// The DTO carries json tags already, so nothing about the catalog's storage
// leaks back into the leeway types.
func marshalDesc(tbl *common.TableDesc) (s string, err error) {
	var dto common.TableDescDto
	err = tbl.LoadTo(&dto)
	if err != nil {
		err = eh.Errorf("unable to load table description into its dto: %w", err)
		return
	}
	var b []byte
	b, err = json.Marshal(&dto)
	if err != nil {
		err = eh.Errorf("unable to marshal table description: %w", err)
		return
	}
	return string(b), nil
}

// Run is one refresh end to end: fetch, analyze, and — unless dryRun — apply
// the DDL and insert. It returns what it computed either way, so a dry run can
// report the row counts it would have written.
//
// exec may be nil when dryRun is set; nothing else about the two paths differs.
func Run(ctx context.Context, fetcher FetcherI, exec ExecI, target TargetDatabase, dryRun bool, log zerolog.Logger) (res Result, err error) {
	var tables []TableSnapshot
	tables, err = fetcher.FetchTables(ctx)
	if err != nil {
		err = eh.Errorf("unable to fetch tables: %w", err)
		return
	}
	var runId string
	runId, err = NewRunId()
	if err != nil {
		return
	}
	log.Info().Int("tables", len(tables)).Str("runId", runId).Msg("datacatalog: discovered tables")

	res, err = Analyze(tables, runId, time.Now(), log)
	if err != nil {
		return
	}
	log.Info().
		Int("catalog", len(res.Catalog)).
		Int("leeway", len(res.Leeway)).
		Int("pairs", len(res.Pairs)).
		Int("shapes", len(res.Shapes)).
		Msg("datacatalog: analysis complete")

	if dryRun {
		return
	}
	if exec == nil {
		err = eh.Errorf("no ClickHouse client for a non-dry run")
		return
	}
	err = ApplyDDL(ctx, exec, target)
	if err != nil {
		err = eh.Errorf("unable to apply the catalog DDL: %w", err)
		return
	}
	err = Insert(ctx, exec, target, res)
	if err != nil {
		err = eh.Errorf("unable to insert catalog rows: %w", err)
		return
	}
	return
}
