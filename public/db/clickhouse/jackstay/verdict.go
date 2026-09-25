package jackstay

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// VerdictE is a table's structure verdict against the target (ADR-0259 §SD3).
type VerdictE uint8

const (
	// VerdictUnsupported: the source table is not something a row copy can
	// move (a view, a dictionary, an integration engine, a materialized
	// view's inner table), or the target name is taken by one.
	VerdictUnsupported VerdictE = iota
	// VerdictCreate: the table is absent on the target; the plan carries the
	// source's CREATE TABLE, retargeted.
	VerdictCreate
	// VerdictExtend: the target lacks columns the source has; the plan
	// carries ADD COLUMN statements. The target may have extra columns too.
	VerdictExtend
	// VerdictNarrower: the target has every source column and more; the extra
	// columns take their defaults.
	VerdictNarrower
	// VerdictIdentical: same columns, same types, same sorting key.
	VerdictIdentical
	// VerdictIncompatible: a shared column differs in type or kind, the
	// sorting keys differ, or the DDL cannot be derived. The table is
	// excluded.
	VerdictIncompatible
)

var AllVerdicts = []VerdictE{VerdictUnsupported, VerdictCreate, VerdictExtend, VerdictNarrower, VerdictIdentical, VerdictIncompatible}

func (inst VerdictE) String() (s string) {
	switch inst {
	case VerdictUnsupported:
		return "unsupported"
	case VerdictCreate:
		return "create"
	case VerdictExtend:
		return "extend"
	case VerdictNarrower:
		return "narrower"
	case VerdictIdentical:
		return "identical"
	case VerdictIncompatible:
		return "incompatible"
	}
	return "invalid"
}

// IsSyncable reports whether rows of a table with this verdict can be copied,
// once any proposed DDL has been applied.
func (inst VerdictE) IsSyncable() (ok bool) {
	switch inst {
	case VerdictCreate, VerdictExtend, VerdictNarrower, VerdictIdentical:
		return true
	}
	return false
}

func (inst VerdictE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("verdict", uint8(inst)).Errorf("invalid verdict")
		return
	}
	return []byte(s), nil
}

func (inst *VerdictE) UnmarshalText(text []byte) (err error) {
	for _, v := range AllVerdicts {
		if v.String() == string(text) {
			*inst = v
			return
		}
	}
	return eb.Build().Str("verdict", string(text)).Errorf("unknown verdict")
}

// TableVerdict is the outcome of [Judge] for one source table.
type TableVerdict struct {
	Verdict VerdictE `json:"verdict"`
	// Reasons explain an unsupported or incompatible verdict.
	Reasons []string `json:"reasons,omitempty"`
	// Notes are facts an operator should see, whatever the verdict.
	Notes []string `json:"notes,omitempty"`
	// DDL brings the target in line; empty unless the verdict is create or
	// extend.
	DDL []string `json:"ddl,omitempty"`
	// CopyColumns is the explicit, ordered column list a row copy selects on
	// the source, inserts on the target, and hashes on both sides (§SD4). It
	// is in source column order.
	CopyColumns []string `json:"copyColumns,omitempty"`
	// Leeway is true when the source table classifies as leeway (ADR-0170).
	Leeway bool `json:"leeway"`
	// LeewayRelation reads "the source is a … of the target", from
	// [common.TableOperations.Relate]; empty unless both sides classify as
	// leeway.
	LeewayRelation string `json:"leewayRelation,omitempty"`
}

// IsCopyableEngine reports whether rows of a table on this engine can be
// selected and inserted by a sync: the MergeTree family, the Log family and
// Memory. Everything else either holds no rows of its own (views,
// Distributed, Merge, Null, Buffer) or reads them from elsewhere (Kafka, URL,
// File, S3, Dictionary, …).
func IsCopyableEngine(engine string) (ok bool) {
	switch engine {
	case "Log", "TinyLog", "StripeLog", "Memory":
		return true
	}
	return strings.HasSuffix(engine, "MergeTree")
}

// isInnerTable recognises the storage table of a materialized view. It is
// recreated with its view and is never synced on its own.
func isInnerTable(name string) (is bool) {
	return strings.HasPrefix(name, ".inner")
}

func normalizeExpr(s string) (n string) {
	return strings.Join(strings.Fields(s), " ")
}

func isStored(c ColumnInfo) (ok bool) {
	return c.IsInsertable() || c.DefaultKind == "MATERIALIZED"
}

// Judge gives src its structure verdict against the target table dst, which is
// nil when the target has no table named target (ADR-0259 §SD3). ops is the
// leeway operations handle; it is not safe for concurrent use, so neither is
// Judge.
//
// An error means the verdict could not be reached at all. A table whose DDL
// cannot be derived gets an incompatible verdict with the reason instead.
func Judge(ops *common.TableOperations, src *TableInfo, dst *TableInfo, target datacatalog.TableRef) (v TableVerdict, err error) {
	if src == nil {
		err = eh.Errorf("no source table")
		return
	}
	srcCl := datacatalog.Classify(src.ColumnNames())
	v.Leeway = srcCl.Kind == datacatalog.KindLeeway

	switch {
	case isInnerTable(src.Ref.Name):
		v.Verdict = VerdictUnsupported
		v.Reasons = append(v.Reasons, "materialized view storage; recreated with its view")
		return
	case !IsCopyableEngine(src.Engine):
		v.Verdict = VerdictUnsupported
		v.Reasons = append(v.Reasons, "engine "+src.Engine+" holds no rows a sync can copy")
		return
	}
	if strings.HasPrefix(src.Engine, "Replicated") {
		v.Notes = append(v.Notes, "replicated engine: the table's DDL names a Keeper path the target must be able to use")
	}

	if dst == nil {
		judgeCreate(src, target, &v)
		return
	}
	if !IsCopyableEngine(dst.Engine) {
		v.Verdict = VerdictUnsupported
		v.Reasons = append(v.Reasons, "target name is taken by a "+dst.Engine+" table")
		return
	}
	judgeExisting(src, dst, target, &v)

	dstCl := datacatalog.Classify(dst.ColumnNames())
	if v.Leeway && dstCl.Kind == datacatalog.KindLeeway {
		var rel common.TableRelationE
		var relErr error
		rel, relErr = ops.Relate(srcCl.Table, dstCl.Table)
		if relErr != nil {
			v.Notes = append(v.Notes, "leeway shapes could not be related: "+relErr.Error())
		} else {
			v.LeewayRelation = rel.String()
			if rel == common.TableRelationEqual && !sameNameSet(src, dst) {
				v.Verdict = VerdictIncompatible
				v.DDL = nil
				v.CopyColumns = nil
				v.Reasons = append(v.Reasons, "same leeway shape under different physical column names; rows cannot be copied by name")
			}
		}
	}
	return
}

func judgeCreate(src *TableInfo, target datacatalog.TableRef, v *TableVerdict) {
	ddl, err := RetargetCreateQuery(src.CreateQuery, target)
	if err != nil {
		v.Verdict = VerdictIncompatible
		v.Reasons = append(v.Reasons, "create DDL could not be derived: "+err.Error())
		return
	}
	v.Verdict = VerdictCreate
	v.DDL = []string{ddl}
	for _, c := range src.Columns {
		if c.IsInsertable() {
			v.CopyColumns = append(v.CopyColumns, c.Name)
		}
	}
	for _, c := range src.Columns {
		if c.DefaultKind == "MATERIALIZED" {
			v.Notes = append(v.Notes, "column "+c.Name+" is MATERIALIZED; the target recomputes it")
		}
	}
}

func judgeExisting(src *TableInfo, dst *TableInfo, target datacatalog.TableRef, v *TableVerdict) {
	if normalizeExpr(src.SortingKey) != normalizeExpr(dst.SortingKey) {
		v.Reasons = append(v.Reasons, "sorting keys differ: source ("+src.SortingKey+"), target ("+dst.SortingKey+")")
	}
	if src.Engine != dst.Engine {
		v.Notes = append(v.Notes, "engines differ: source "+src.Engine+", target "+dst.Engine)
	}
	if normalizeExpr(src.PartitionKey) != normalizeExpr(dst.PartitionKey) {
		v.Notes = append(v.Notes, "partition keys differ: source ("+src.PartitionKey+"), target ("+dst.PartitionKey+")")
	}

	var ddl []string
	prev := ""
	for _, sc := range src.Columns {
		dc, has := dst.Column(sc.Name)
		if !has {
			ddl = append(ddl, AddColumnDDL(target, sc, prev))
			if sc.IsInsertable() {
				v.CopyColumns = append(v.CopyColumns, sc.Name)
			}
			prev = sc.Name
			continue
		}
		prev = sc.Name
		judgeSharedColumn(sc, dc, v)
	}
	var extras []string
	for _, dc := range dst.Columns {
		if _, has := src.Column(dc.Name); !has && dc.IsInsertable() {
			extras = append(extras, dc.Name)
		}
	}

	switch {
	case len(v.Reasons) > 0:
		v.Verdict = VerdictIncompatible
		v.CopyColumns = nil
	case len(ddl) > 0:
		v.Verdict = VerdictExtend
		v.DDL = ddl
	case len(extras) > 0:
		v.Verdict = VerdictNarrower
	default:
		v.Verdict = VerdictIdentical
	}
	if len(extras) > 0 && v.Verdict != VerdictIncompatible {
		v.Notes = append(v.Notes, "target-only columns take their defaults: "+strings.Join(extras, ", "))
	}
}

// judgeSharedColumn decides whether a column present on both sides is copied,
// computed by the target, or a reason for incompatibility.
func judgeSharedColumn(sc ColumnInfo, dc ColumnInfo, v *TableVerdict) {
	switch {
	case !isStored(sc):
		if dc.IsInsertable() {
			v.Notes = append(v.Notes, "column "+sc.Name+" is "+sc.DefaultKind+" on the source and stored on the target; the target gets its default")
		}
		return
	case dc.IsInsertable():
		// Copied: the types must agree.
	case dc.DefaultKind == "MATERIALIZED":
		note := "column " + sc.Name + " is MATERIALIZED on the target; the target recomputes it"
		if sc.DefaultKind == "MATERIALIZED" && normalizeExpr(sc.DefaultExpression) != normalizeExpr(dc.DefaultExpression) {
			note += " with a different expression"
		}
		v.Notes = append(v.Notes, note)
		return
	default:
		v.Reasons = append(v.Reasons, "column "+sc.Name+" is stored on the source but "+dc.DefaultKind+" on the target")
		return
	}
	if sc.Type != dc.Type {
		if datacatalog.NormalizeType(sc.Type) == datacatalog.NormalizeType(dc.Type) {
			v.Notes = append(v.Notes, "column "+sc.Name+" differs only in LowCardinality: "+sc.Type+" → "+dc.Type)
		} else {
			v.Reasons = append(v.Reasons, "column "+sc.Name+" type differs: source "+sc.Type+", target "+dc.Type)
			return
		}
	}
	v.CopyColumns = append(v.CopyColumns, sc.Name)
}

func sameNameSet(a *TableInfo, b *TableInfo) (same bool) {
	an := a.ColumnNames()
	bn := b.ColumnNames()
	slices.Sort(an)
	slices.Sort(bn)
	return slices.Equal(an, bn)
}
