package play

// get_schema reads the Schema pane (ADR-0270, update of 2026-10-08): the
// leeway schema the pane infers from the column names of the result it is
// fed — tagged sections with their membership spec and value columns, plain
// columns — with each column's handle, spelled as the result stores it.
// list_tables and describe_table read the endpoint's catalog instead; this
// reads a result, so it works on a node or a column subset as the pane does.

import (
	"iter"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

const (
	opGetSchema      = "get_schema"
	schemaPaneId     = "schema"
	schemaOpsReading = "the schema is read off the result's column names; describe_table reads a table's from the endpoint, with its comments"
)

// SchemaArgs is get_schema's argument.
type SchemaArgs struct {
	Node string `json:",omitzero" desc:"the split node whose result to read; the result the Schema pane is fed when left out"`
}

// SchemaColumn is one column as the Schema pane reads it.
type SchemaColumn struct {
	Name   string `desc:"the column, spelled as the result's physical names spell it"`
	Handle string `json:",omitzero" desc:"the handle to write for it, section:column; empty for a result that is not leeway-shaped, whose column names are what to write"`
	Type   string `desc:"its canonical type"`
	// ItemType is a plain column's scope.
	ItemType       string   `json:",omitzero" desc:"for a plain column, what it holds: entity-id, timestamp and the like, or opaque for a column of a result that is not leeway-shaped"`
	EncodingHints  []string `json:",omitzero" desc:"its encoding hints"`
	ValueSemantics []string `json:",omitzero" desc:"its value semantics"`
}

// SchemaSection is one tagged section.
type SchemaSection struct {
	Name string `desc:"the section, spelled as the result's physical names spell it, as handles and LW_GET name it"`
	// Memberships is the section's membership spec, one entry per channel.
	Memberships    []string       `json:",omitzero" desc:"how memberships attach to its attributes, one channel each: low-card-verbatim names are written out, low-card-ref ones are registry ids keelson('memberships') names; empty for a section without memberships"`
	UseAspects     []string       `json:",omitzero" desc:"its use aspects"`
	CoSectionGroup string         `json:",omitzero" desc:"the co-section group it belongs to"`
	StreamingGroup string         `json:",omitzero" desc:"the streaming group it belongs to"`
	Columns        []SchemaColumn `desc:"its value columns"`
}

// SchemaReading is get_schema's result.
type SchemaReading struct {
	ResultId uint64          `desc:"the result read"`
	Node     string          `desc:"the split node the result is of"`
	Leeway   bool            `desc:"true when the result's column names carry leeway's encoding; false and every column is an opaque plain one"`
	Plain    []SchemaColumn  `json:",omitzero" desc:"the plain columns: a leeway result's backbone (id, timestamp and the like), or every column of one that is not leeway-shaped"`
	Sections []SchemaSection `json:",omitzero" desc:"the tagged sections, in the result's order"`
	Columns  int             `desc:"physical columns in the result"`
	// More counts what the column bound left out.
	More int    `json:",omitzero" desc:"columns past the bound, not listed"`
	Note string `desc:"where the reading comes from"`
}

// getSchema reads the schema of the result the Schema pane is fed, or of a
// node.
func getSchema(r *opsResults, in SchemaArgs) (out SchemaReading, err error) {
	pane := ""
	if in.Node == "" {
		pane = schemaPaneId
	}
	lr, err := r.read(pane, in.Node)
	if err != nil {
		return
	}
	defer lr.release()
	if lr.schema == nil {
		return out, app.RefuseOperation("node " + string(lr.node) + " holds no result")
	}
	td, leeway := resultSchemaDesc(lr.schema)
	out = SchemaReading{ResultId: uint64(lr.id), Node: string(lr.node), Leeway: leeway, Columns: lr.schema.NumFields(), Note: schemaOpsReading}
	out.Plain, out.Sections, out.More = schemaReading(td, leeway, lr.schema.Fields(), schemaMaxColumns)
	return
}

// schemaReading lists td's columns, at most limit of them; handles are
// the result's own labels for its plain columns and section:column for a
// value column.
func schemaReading(td *common.TableDesc, leeway bool, fields []arrow.Field, limit int) (plain []SchemaColumn, sections []SchemaSection, more int) {
	listed := 0
	take := func() bool {
		if listed >= limit {
			more++
			return false
		}
		listed++
		return true
	}
	plainHandles := map[string]string{}
	if leeway {
		names := make([]string, 0, len(fields))
		for i := range fields {
			names = append(names, fields[i].Name)
		}
		for _, l := range lwsql.BuildLabels(names) {
			if _, col, ok := strings.Cut(l, ":"); ok {
				plainHandles[col] = l
			}
		}
	}
	for i, n := range td.PlainValuesNames {
		if !take() {
			continue
		}
		col := SchemaColumn{Name: n.String(), Type: typeChipOf(td.PlainValuesTypes, i)}
		if i < len(td.PlainValuesItemTypes) {
			col.ItemType = td.PlainValuesItemTypes[i].String()
		}
		if i < len(td.PlainValuesEncodingHints) {
			col.EncodingHints = aspectNames(td.PlainValuesEncodingHints[i].IterateAspects())
		}
		if i < len(td.PlainValuesValueSemantics) {
			col.ValueSemantics = aspectNames(td.PlainValuesValueSemantics[i].IterateAspects())
		}
		if leeway {
			col.Handle = plainHandles[col.Name]
		}
		plain = append(plain, col)
	}
	for _, sec := range td.TaggedValuesSections {
		s := SchemaSection{Name: sec.Name.String(), CoSectionGroup: sec.CoSectionGroup.String(), StreamingGroup: sec.StreamingGroup.String(),
			UseAspects: aspectNames(sec.UseAspects.IterateAspects())}
		if sec.MembershipSpec != common.MembershipSpecNone {
			for m := range sec.MembershipSpec.Iterate() {
				s.Memberships = append(s.Memberships, m.String())
			}
		}
		for i, cn := range sec.ValueColumnNames {
			if !take() {
				continue
			}
			col := SchemaColumn{Name: cn.String(), Handle: s.Name + ":" + cn.String(), Type: typeChipOf(sec.ValueColumnTypes, i)}
			if i < len(sec.ValueEncodingHints) {
				col.EncodingHints = aspectNames(sec.ValueEncodingHints[i].IterateAspects())
			}
			if i < len(sec.ValueSemantics) {
				col.ValueSemantics = aspectNames(sec.ValueSemantics[i].IterateAspects())
			}
			s.Columns = append(s.Columns, col)
		}
		sections = append(sections, s)
	}
	return
}

// typeChipOf is the i-th canonical type's terse form, as the pane's
// navigator prints it.
func typeChipOf(types []canonicaltypes.PrimitiveAstNodeI, i int) string {
	if i >= len(types) || types[i] == nil {
		return "—"
	}
	return types[i].String()
}

// aspectNames spells an aspect set's members.
func aspectNames[A interface{ String() string }](aspects iter.Seq2[int, A]) (out []string) {
	for _, a := range aspects {
		out = append(out, a.String())
	}
	return
}

func addSchemaPaneOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: a result's column names are the data's.
	appops.Query(s, app.OperationSpec{Name: opGetSchema, Version: 1,
		Summary: "read the Schema pane: the leeway schema of the result it is fed — sections with their membership channels and value columns, plain columns — with each column's handle and canonical type",
		Reads:   []string{opsResResult, opsResPanes}, Agents: true, Untrusted: true,
		Follows: []string{"a result that is not leeway-shaped reads as opaque plain columns, as the pane draws it"}},
		func(sn opsSnap, in SchemaArgs) (SchemaReading, error) {
			if !sn.mounted {
				return SchemaReading{}, app.RefuseOperation("the window has not mounted")
			}
			return getSchema(&sn.results, in)
		})
}
