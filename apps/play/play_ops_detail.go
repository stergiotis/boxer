package play

// play_ops_detail.go: get_detail, the Detail pane's reading of one row for
// an agent (ADR-0270, update of 2026-10-05). The read builds its own Driver
// from the result's column names (discoverCardRecipe, the same derivation the
// pane's CardDriver makes) and drives one row into the read model (lwread,
// ADR-0289 §SD3) on the query's goroutine: attributes named by
// their first membership through the session's registries, values spelled
// once, hidden and cut values counted. Nothing the pane holds is touched, so
// the read neither needs the pane raised nor disturbs it.

import (
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwread"
)

const (
	opGetDetail  = "get_detail"
	detailPaneId = "detail"
)

// Bounds on get_detail.
const (
	detailOpsMaxBytes = 8 << 10
	detailOpsMaxValue = 300
)

// detailOpsView is what get_detail reads of the window besides the result.
type detailOpsView struct {
	// embedded is true when an embedder replaced the pane's body.
	embedded bool
}

// DetailArgs is get_detail's argument.
type DetailArgs struct {
	Row  *int64 `json:",omitzero" desc:"the row to read, 0 for the first; the selection when left out, as the Detail pane draws it"`
	Node string `json:",omitzero" desc:"the split node whose row to read; the node the Detail pane follows (selection_node, or its binding) when left out"`
}

// DetailValue is one value column of an attribute.
type DetailValue struct {
	Column string   `desc:"the value column; for a result that is not leeway-shaped, the column's short label"`
	Type   string   `json:",omitzero" desc:"its canonical type, of one item for a list or set"`
	Value  string   `json:",omitzero" desc:"a scalar's value: bytes as text when printable, else 0x-hex; cut at 300 bytes with …"`
	Items  []string `json:",omitzero" desc:"a list's items in order, or a set's in value order"`
	More   int      `json:",omitzero" desc:"items left out after the first 64"`
}

// DetailAttribute is one attribute of the row.
type DetailAttribute struct {
	Name    string        `desc:"what the attribute is: its first membership, a ref named through the session's registries, parameters in brackets; the section when no membership names it; a plain column's name; section·name when two sections give one name"`
	Section string        `desc:"its section, spelled as the result's physical names and its handle spell it; for a plain column, the plain item type; for a result that is not leeway-shaped, the group the pane draws it in (pinned, relations, data, meta)"`
	Plain   bool          `json:",omitzero" desc:"true for a plain column, which every row carries once"`
	Labels  []string      `json:",omitzero" desc:"its further memberships"`
	Values  []DetailValue `json:",omitzero" desc:"its value columns the card shows"`
	Hidden  int           `json:",omitzero" desc:"its value columns the card hides as machine-readable only"`
	Handle  string        `json:",omitzero" desc:"the LW_GET expression that reads it in SQL"`
}

// DetailIdentity is a row's canonical identity, as the pane's identity strip
// shows it.
type DetailIdentity struct {
	Canonform string `desc:"the canonform digest, hex"`
	Canonwire string `desc:"the canonwire fingerprint, hex"`
	Canonical bool   `desc:"true when the canonical wire item passes the runtime's check"`
	Verdict   string `json:",omitzero" desc:"why it is not canonical"`
	WireBytes int    `desc:"the wire item's size in bytes"`
	Error     string `json:",omitzero" desc:"why no identity could be computed"`
}

// DetailTemporal is one temporal attribute of a row.
type DetailTemporal struct {
	Label   string `desc:"the attribute"`
	Section string `json:",omitzero" desc:"its leeway section"`
	Kind    string `desc:"instants or intervals"`
	Count   int    `desc:"how many instants or intervals"`
	From    string `json:",omitzero" desc:"the earliest, UTC"`
	To      string `json:",omitzero" desc:"the latest, UTC"`
	Summary string `desc:"the pane's legend line for it"`
}

// DetailReading is get_detail's result.
type DetailReading struct {
	ResultId         uint64            `desc:"the result the row is of"`
	Node             string            `desc:"the split node the result is of"`
	Row              int64             `desc:"the row read"`
	Rows             int64             `desc:"the result's row count"`
	Entity           string            `json:",omitzero" desc:"the entity type the row's tagged id names"`
	NaturalKey       string            `json:",omitzero" desc:"the row's natural key"`
	Leeway           bool              `desc:"true when the result is leeway-shaped and the sections are its own"`
	Attributes       []DetailAttribute `json:",omitzero" desc:"the row's attributes: plain columns, then each tagged section's in card order; for a result that is not leeway-shaped, its non-empty columns grouped as the pane groups them"`
	More             int               `json:",omitzero" desc:"attributes the byte bound left out"`
	Hidden           int               `json:",omitzero" desc:"value columns the card hides as machine-readable only"`
	Identity         *DetailIdentity   `json:",omitzero" desc:"the row's canonical identity, for a leeway result"`
	Temporal         []DetailTemporal  `json:",omitzero" desc:"the row's temporal attributes, as the pane's time strip draws them"`
	TemporalDropped  int               `json:",omitzero" desc:"marks the strip's density bound left out"`
	Embedded         bool              `json:",omitzero" desc:"true when the window replaced the Detail pane's body: the person sees the embedder's view, not this reading"`
	Truncated        bool              `json:",omitzero" desc:"true when the byte bound left attributes out (more counts them)"`
	TruncationReason string            `json:",omitzero" desc:"which bound cut it"`
	Note             string            `json:",omitzero" desc:"what the reading leaves out"`
}

const detailOpsNote = "values are read before the glosses the pane draws them through; typed components are not read; get_canonical reads the row losslessly"

func addDetailOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: every value is the data's.
	appops.Query(s, app.OperationSpec{Name: opGetDetail, Version: 2,
		Summary: "read one row as the Detail pane does: its attributes by name with their values, labels and SQL handles, its canonical identity and its temporal attributes",
		Reads:   []string{opsResResult, opsResSignals, opsResPanes}, Agents: true, Untrusted: true,
		Follows: []string{"the Detail pane is unchanged; set_signal selection moves the row it draws"}},
		func(sn opsSnap, in DetailArgs) (DetailReading, error) {
			if !sn.mounted {
				return DetailReading{}, app.RefuseOperation("the window has not mounted")
			}
			return getDetail(&sn.results, sn.state.Signals, sn.detail, in)
		})
	addCanonicalOps(s)
}

// getDetail reads one row of the result the Detail pane follows, or of a
// node.
func getDetail(r *opsResults, signals []SignalState, v detailOpsView, in DetailArgs) (out DetailReading, err error) {
	pane := ""
	if in.Node == "" {
		pane = detailPaneId
	}
	lr, err := r.read(pane, in.Node)
	if err != nil {
		return
	}
	defer lr.release()
	if lr.rec == nil || lr.schema == nil {
		return out, app.RefuseOperation("node " + string(lr.node) + " holds no result")
	}
	row, err := opsRow(signals, in.Row, lr.numRows)
	if err != nil {
		return
	}
	out = DetailReading{ResultId: uint64(lr.id), Node: string(lr.node), Row: row, Rows: lr.numRows, Embedded: v.embedded, Note: detailOpsNote}
	out.Entity, out.NaturalKey = entityHeader(lr.rec, row)
	if out.Entity == "<not-a-tagged-id>" {
		out.Entity = ""
	}
	recipe, leeway := discoverCardRecipe(lr.schema)
	attrs, dropped := detectTemporalAttrs(lr.rec, lr.schema, row, recipe.classes)
	out.TemporalDropped = dropped
	for _, a := range attrs {
		t := DetailTemporal{Label: a.label, Section: a.section, Kind: "instants", Count: len(a.points), Summary: a.summary()}
		if a.kind == kindIntervals {
			t.Kind, t.Count = "intervals", len(a.spans)
		}
		if lo, hi, ok := a.extent(); ok {
			t.From, t.To = formatEpochMS(lo), formatEpochMS(hi)
		}
		out.Temporal = append(out.Temporal, t)
	}
	var read []DetailAttribute
	if leeway {
		out.Leeway = true
		sink := lwread.NewSink(lwread.Options{Renderer: registryRenderer(), MaxValueBytes: detailOpsMaxValue})
		slice := lr.rec.NewSlice(row, row+1)
		derr := recipe.driver.DriveRecordBatch(sink, slice)
		slice.Release()
		if derr != nil {
			return out, app.RefuseOperation("the row did not read as leeway: " + truncateRunes(derr.Error(), 300))
		}
		m := sink.Model()
		m.Qualify()
		if len(m.Records) == 1 {
			read, out.Hidden = detailAttributes(m), m.Records[0].Hidden
		}
		out.Identity = detailIdentity(recipe, lr.rec, row)
	} else {
		read = adHocDetailAttributes(lr.rec, lr.schema, row)
	}
	out.Attributes, out.More = boundDetailAttributes(read, detailOpsMaxBytes)
	out.Truncated = out.More > 0
	if out.Truncated {
		out.TruncationReason = "the reading's byte bound (" + strconv.Itoa(detailOpsMaxBytes>>10) + " KiB of values)"
	}
	return out, nil
}

// detailIdentity computes the row's canonform digest and canonwire
// fingerprint as the pane's identity strip does.
func detailIdentity(recipe cardRecipe, rec arrow.RecordBatch, row int64) *DetailIdentity {
	comp, err := newIdentityComputer(recipe.table, recipe.ir, recipe.driver)
	if err != nil {
		return &DetailIdentity{Error: truncateRunes(err.Error(), 300)}
	}
	vals, err := comp.row(rec, row)
	if err != nil {
		return &DetailIdentity{Error: truncateRunes(err.Error(), 300)}
	}
	id := &DetailIdentity{Canonform: hex.EncodeToString(vals.canon[:]), Canonwire: hex.EncodeToString(vals.wire[:]),
		Canonical: vals.wireErr == nil, WireBytes: vals.wireLen}
	if vals.wireErr != nil {
		id.Verdict = truncateRunes(vals.wireErr.Error(), 300)
	}
	return id
}

// detailAttributes is the read model's one record as the reading's
// attributes, each with the handle the batch's header gives it.
func detailAttributes(m *lwread.Model) (out []DetailAttribute) {
	handles := map[string]string{}
	for _, h := range m.Header() {
		handles[h.Section+"\x00"+h.Name] = h.Handle
	}
	for _, a := range m.Records[0].Attributes {
		da := DetailAttribute{Name: a.Name, Section: a.Section, Plain: a.Plain, Hidden: a.Hidden, Handle: handles[a.Section+"\x00"+a.Name]}
		if !a.Plain && a.HandleSection != "" {
			da.Section = a.HandleSection
		}
		for _, l := range a.Labels {
			da.Labels = append(da.Labels, l.Text)
		}
		for _, v := range a.Values {
			dv := DetailValue{Column: v.Column, Type: v.Type, More: v.More}
			if v.Shape == lwread.ShapeScalar {
				if len(v.Items) > 0 {
					dv.Value = v.Items[0].Text
				}
			} else {
				for _, it := range v.Items {
					dv.Items = append(dv.Items, it.Text)
				}
			}
			da.Values = append(da.Values, dv)
		}
		out = append(out, da)
	}
	return
}

// adHocDetailAttributes groups a non-leeway row's non-empty columns as the
// pane's ad-hoc view does, one attribute per column.
func adHocDetailAttributes(rec arrow.RecordBatch, schema *arrow.Schema, row int64) (out []DetailAttribute) {
	for _, g := range []struct{ section, heading string }{
		{sectionPlain, "pinned"}, {sectionForeignKey, "relations"}, {sectionData, "data"}, {sectionRare, "meta"}} {
		for i := 0; i < schema.NumFields(); i++ {
			name := schema.Field(i).Name
			if sectionForColumn(name) != g.section {
				continue
			}
			val := formatDisplayCell(rec.Column(i), row)
			if val == "" || val == "[len=0]" {
				continue
			}
			label := shortColumnLabel(name)
			out = append(out, DetailAttribute{Name: label, Section: g.heading, Values: []DetailValue{{Column: label,
				Value: truncateBytes(strings.Clone(val), detailOpsMaxValue)}}})
		}
	}
	return
}

// boundDetailAttributes keeps attributes in order until the byte budget is
// spent, and counts the ones it left out.
func boundDetailAttributes(in []DetailAttribute, budget int) (out []DetailAttribute, more int) {
	used := 0
	for _, a := range in {
		cost := 24 + len(a.Name) + len(a.Section) + len(a.Handle)
		for _, l := range a.Labels {
			cost += len(l) + 4
		}
		for _, v := range a.Values {
			cost += len(v.Column) + len(v.Type) + len(v.Value) + 12
			for _, it := range v.Items {
				cost += len(it) + 3
			}
		}
		if more > 0 || used+cost > budget {
			more++
			continue
		}
		used += cost
		out = append(out, a)
	}
	return
}
