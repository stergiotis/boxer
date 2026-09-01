package constructsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// target.go is ADR-0181 §SD8 M2: target adoption. Under an INSERT wrapper
// whose destination the bound schema resolves, constructor mints stop being
// fresh-table compositions and become references to the target's own
// columns — segments, aspects and spelling included — and an opt-in shape
// check verifies the SELECT's output against the target's physical names.
//
// Adoption is resolve-first, compose-fallback: a mint's logical identity
// (section + column, channel, role, or item + name) is looked up against the
// target through the same fold-tolerant machinery handles use, and only a
// miss falls back to composing a fresh name with the target's table
// segments. That is what reconciles spelling — a constructor folds
// (`geoPoint` mints as `geo-point`) while an existing target may spell
// camelCase, and resolving instead of composing sidesteps the divergence
// entirely.

// TargetSchemaI is what target adoption needs of a schema: the fold-tolerant
// section lookup extraction already uses, the handle path for plain and
// support columns, and the table-level naming segments for the compose
// fallback. *lwsql.Resolver implements all three, so a host bound for handle
// resolution is bound for adoption too.
type TargetSchemaI interface {
	LaneSourceI
	TableSegments(dbName string, tableName string) (seg lwsql.TableSegments, ok bool)
	Resolve(dbName string, tableName string, handle string) passes.ResolveResult
}

// TargetPassName is the registered nanopass name of the target-adopting
// constructor expansion. It runs INSTEAD of the unbound LwConstructExpand on
// hosts that carry a schema binding (ordered just before it; whichever runs
// first consumes every constructor call and the other's marker scan finds
// nothing).
const TargetPassName = "LwConstructExpandTarget"

// ExpandPassWithTargetAdoption is LwConstructExpand bound to a schema: a
// statement that is not an INSERT, or whose target the schema does not
// know, expands exactly as the unbound pass does; an INSERT with a resolved
// target mints in that target's terms.
func ExpandPassWithTargetAdoption(schema TargetSchemaI, defaultDatabase string) nanopass.Pass {
	return nanopass.LiftBodyPass(TargetPassName, func(sql string) (string, error) {
		return expandCore(TargetPassName, sql, lwsql.DefaultTableSegments(), schema, defaultDatabase)
	}, nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})
}

// targetBinding carries the resolved INSERT destination through an
// expansion.
type targetBinding struct {
	schema TargetSchemaI
	db     string
	table  string
}

// insertTargetNames extracts the wrapper's destination. A parameterised
// target (`INSERT INTO {t:Identifier} …`) comes back as the slot text, which
// no schema lookup matches — correctly, since the real name is unknown until
// the server substitutes it.
func insertTargetNames(ins *grammar1.InsertStmtContext, defaultDatabase string) (db string, table string) {
	tid, ok := ins.TableIdentifier().(*grammar1.TableIdentifierContext)
	if !ok {
		return
	}
	table = nanopass.TableIdentifierName(tid)
	db = defaultDatabase
	if d, hasDb := tid.DatabaseIdentifier().(*grammar1.DatabaseIdentifierContext); hasDb {
		db = nanopass.DatabaseIdentifierName(d)
	}
	return
}

// bindTarget resolves the statement's INSERT destination against the schema,
// returning the adopted table segments and the binding the per-call
// reconciliation uses. A statement without a wrapper, or a wrapper whose
// target the schema does not carry, keeps the fallback segments and no
// binding — composition then behaves exactly as the unbound pass.
func bindTarget(pr *nanopass.ParseResult, schema TargetSchemaI, defaultDatabase string, fallback lwsql.TableSegments) (seg lwsql.TableSegments, tgt *targetBinding) {
	seg = fallback
	if schema == nil {
		return
	}
	ins := pr.InsertStmt()
	if ins == nil {
		return
	}
	db, table := insertTargetNames(ins, defaultDatabase)
	if table == "" {
		return
	}
	s, ok := schema.TableSegments(db, table)
	if !ok {
		return
	}
	seg = s
	tgt = &targetBinding{schema: schema, db: db, table: table}
	return
}

// reconcile swaps a composed name for the target's own physical column when
// the mint's logical identity resolves against it. A miss keeps the composed
// name: minting is not the place to rule on a column the target lacks — the
// schema may be degraded or mid-migration, and the verdict with the full
// column set in hand is LwShapeCheckTarget's.
func (inst *targetBinding) reconcile(normalized string, spec []string, minted string) string {
	switch normalized {
	case nanopass.NormalizeCallName(NamePlain):
		// spec = name, type, tokens…; the item token names the prefix a
		// plain handle carries (`item:id` → `id:naturalKey`).
		item := ""
		for _, tok := range spec[2:] {
			if v, ok := strings.CutPrefix(tok, lwsql.SpecTokenPrefixItem); ok {
				item = v
				break
			}
		}
		if item == "" {
			return minted
		}
		if r := inst.schema.Resolve(inst.db, inst.table, item+":"+spec[0]); r.Kind == passes.ResolveOK && len(r.Physical) == 1 {
			return r.Physical[0]
		}
	case nanopass.NormalizeCallName(NameTagged):
		// spec = section, name, type, tokens…
		if lanes, ok := inst.schema.ExtractLanesFor(inst.db, inst.table, spec[0]); ok {
			if vc, err := lanes.ValueColumnFor(spec[1]); err == nil {
				return vc.Physical
			}
		}
	case nanopass.NormalizeCallName(NameMembership):
		// spec = section, channel
		if lanes, ok := inst.schema.ExtractLanesFor(inst.db, inst.table, spec[0]); ok {
			if ch, err := lanes.ChannelFor(spec[1]); err == nil {
				return ch.Ident
			}
		}
	case nanopass.NormalizeCallName(NameSupport):
		// spec = section, role; support roles are lane short-names, which is
		// exactly the handle spelling (`geoPoint:hrcard`).
		if r := inst.schema.Resolve(inst.db, inst.table, spec[0]+":"+spec[1]); r.Kind == passes.ResolveOK && len(r.Physical) == 1 {
			return r.Physical[0]
		}
	}
	return minted
}

// ShapeCheckTargetPassName is the registered nanopass name of the
// target-aware shape check.
const ShapeCheckTargetPassName = "LwShapeCheckTarget"

// ShapeCheckPassWithTarget (ADR-0181 §SD8 M2) is the INSERT half of the
// transform contract's validation, opt-in like LwShapeCheck: under a
// wrapper it verifies the SELECT's output column names against the
// destination's physical names — the vertical-subset rule applied to a
// concrete table — and, when a column list is present, that the list and
// the output correspond position by position. Names compare
// fold-equivalent (case and separator insensitive), so an adopted mint, a
// pass-through physical, and a hand-spelled variant all land on the column
// they mean; a true miss errors naming both sides. A statement without a
// wrapper falls back to the closure check LwShapeCheck performs.
func ShapeCheckPassWithTarget(columns passes.SchemaProviderI, defaultDatabase string) nanopass.Pass {
	return nanopass.LiftBodyPass(ShapeCheckTargetPassName, func(sql string) (string, error) {
		return shapeCheckTargetImpl(sql, columns, defaultDatabase)
	}, nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})
}

func shapeCheckTargetImpl(sql string, columns passes.SchemaProviderI, defaultDatabase string) (result string, err error) {
	result = sql
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf(ShapeCheckTargetPassName+": %w", err)
		return
	}
	ins := pr.InsertStmt()
	if ins == nil {
		return shapeCheckImpl(sql)
	}
	db, table := insertTargetNames(ins, defaultDatabase)
	colSeq, n, found := columns.GetColumns(db, table)
	if !found || n == 0 {
		err = eb.Build().Str("database", db).Str("table", table).
			Errorf(ShapeCheckTargetPassName + ": the INSERT target is not in the bound schema — there is nothing to verify the SELECT against")
		return
	}
	byFold := make(map[string]string, n)
	for name := range colSeq {
		byFold[foldPhysical(name)] = name
	}

	var listed []string
	if cc, hasList := ins.ColumnsClause().(*grammar1.ColumnsClauseContext); hasList {
		for _, ni := range cc.AllNestedIdentifier() {
			listed = append(listed, nanopass.DecodeIdentifier(ni.GetText()))
		}
		for _, l := range listed {
			if _, known := byFold[foldPhysical(l)]; !known {
				err = eb.Build().Str("column", l).Str("table", tableLabel(db, table)).
					Errorf(ShapeCheckTargetPassName + ": the column list names a column the target does not carry")
				return
			}
		}
	}

	scopes, err := nanopass.BuildScopes(pr, defaultDatabase)
	if err != nil {
		err = eb.Build().Errorf(ShapeCheckTargetPassName+": %w", err)
		return
	}
	for _, root := range scopes {
		var names []string
		names, err = outputNames(pr, root)
		if err != nil {
			err = eb.Build().Errorf(ShapeCheckTargetPassName+": %w", err)
			return
		}
		for i, name := range names {
			targetName, known := byFold[foldPhysical(name)]
			if !known {
				err = eb.Build().Str("column", name).Str("table", tableLabel(db, table)).
					Errorf(ShapeCheckTargetPassName + ": the SELECT outputs a column the target does not carry")
				return
			}
			if listed != nil {
				if i >= len(listed) {
					err = eb.Build().Int("outputs", len(names)).Int("listed", len(listed)).
						Errorf(ShapeCheckTargetPassName + ": the SELECT outputs more columns than the column list names — the positional mapping cannot hold")
					return
				}
				if foldPhysical(listed[i]) != foldPhysical(name) {
					err = eb.Build().Int("position", i+1).Str("output", name).Str("listed", listed[i]).Str("resolves", targetName).
						Errorf(ShapeCheckTargetPassName + ": output and column list disagree at this position — the INSERT maps positionally, so this writes a value into a column the statement does not say it writes")
					return
				}
			}
		}
		if listed != nil && len(names) < len(listed) {
			err = eb.Build().Int("outputs", len(names)).Int("listed", len(listed)).
				Errorf(ShapeCheckTargetPassName + ": the column list names more columns than the SELECT outputs")
			return
		}
	}
	return
}

// foldPhysical is the spelling-equivalence the target check compares under:
// case- and separator-insensitive, so `geoPoint` and `geo-point` land on the
// same key. Deliberately permissive — its job is telling "the same column,
// spelled by a different convention" from "not in the target", and the two
// spellings a leeway name can legitimately have differ exactly in case and
// separator placement.
func foldPhysical(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '_':
			return -1
		}
		return r
	}, strings.ToLower(name))
}
