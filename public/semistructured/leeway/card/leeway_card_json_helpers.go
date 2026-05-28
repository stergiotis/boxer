//go:build llm_generated_opus47

package card

import (
	"bytes"
	"encoding/json/jsontext"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var _ = common.PlainItemTypeE(0) // keep import live; common is referenced via attribute traversal helpers

// encodeScalarTyped renders a formatted scalar string into a JSON value
// honouring the canonical type when JSON can represent it natively.
//
// Booleans, integers (within ±2^53), floats, and strings get JSON-native
// shapes; everything else falls back to a JSON string.
func encodeScalarTyped(ct canonicaltypes.PrimitiveAstNodeI, s string) (v jsontext.Value) {
	if ct == nil {
		return mustQuote(s)
	}
	switch n := ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		return encodeMachineNumeric(n, s)
	case *canonicaltypes.MachineNumericTypeAstNode:
		return encodeMachineNumeric(*n, s)
	case canonicaltypes.StringAstNode:
		return encodeStringLike(n, s)
	case *canonicaltypes.StringAstNode:
		return encodeStringLike(*n, s)
	}
	return mustQuote(s)
}

func encodeMachineNumeric(n canonicaltypes.MachineNumericTypeAstNode, s string) (v jsontext.Value) {
	if n.ScalarModifier != canonicaltypes.ScalarModifierNone {
		return mustQuote(s)
	}
	switch n.BaseType {
	case canonicaltypes.BaseTypeMachineNumericFloat:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return mustQuote(s)
		}
		// JSON cannot represent NaN/+Inf/-Inf natively; emit ADR-0018 string sentinels.
		switch {
		case math.IsNaN(f):
			return mustQuote("NaN")
		case math.IsInf(f, 1):
			return mustQuote("+Inf")
		case math.IsInf(f, -1):
			return mustQuote("-Inf")
		}
		return jsontext.Value(s)
	case canonicaltypes.BaseTypeMachineNumericSigned:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return mustQuote(s)
		}
		if i > (1<<53) || i < -(1<<53) {
			return mustQuote(s)
		}
		return jsontext.Value(strconv.FormatInt(i, 10))
	case canonicaltypes.BaseTypeMachineNumericUnsigned:
		u, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return mustQuote(s)
		}
		if u > (1 << 53) {
			return mustQuote(s)
		}
		return jsontext.Value(strconv.FormatUint(u, 10))
	}
	return mustQuote(s)
}

func encodeStringLike(n canonicaltypes.StringAstNode, s string) (v jsontext.Value) {
	if n.ScalarModifier != canonicaltypes.ScalarModifierNone {
		return mustQuote(s)
	}
	if n.BaseType == canonicaltypes.BaseTypeStringBool {
		if s == "true" {
			return jsontext.Value("true")
		}
		if s == "false" {
			return jsontext.Value("false")
		}
	}
	return mustQuote(s)
}

// stripScalarModifier returns the canonical type with its scalar modifier (h/m)
// cleared, so per-item encoding inside an array/set treats the value as a
// scalar of the underlying base type.
func stripScalarModifier(ct canonicaltypes.PrimitiveAstNodeI) canonicaltypes.PrimitiveAstNodeI {
	switch n := ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.MachineNumericTypeAstNode:
		out := *n
		out.ScalarModifier = canonicaltypes.ScalarModifierNone
		return out
	case canonicaltypes.StringAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.StringAstNode:
		out := *n
		out.ScalarModifier = canonicaltypes.ScalarModifierNone
		return out
	}
	return ct
}

func mustQuote(s string) (v jsontext.Value) {
	buf := &bytes.Buffer{}
	enc := jsontext.NewEncoder(buf)
	_ = enc.WriteToken(jsontext.String(s))
	out := bytes.TrimRight(buf.Bytes(), "\n")
	return jsontext.Value(out)
}

func wrapArray(items []jsontext.Value) (v jsontext.Value) {
	buf := &bytes.Buffer{}
	buf.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(it)
	}
	buf.WriteByte(']')
	return jsontext.Value(buf.Bytes())
}

func wrapSet(items []jsontext.Value) (v jsontext.Value) {
	sortJsonValues(items)
	buf := &bytes.Buffer{}
	buf.WriteString(`{"set":[`)
	for i, it := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(it)
	}
	buf.WriteString(`]}`)
	return jsontext.Value(buf.Bytes())
}

func sortJsonValues(items []jsontext.Value) {
	sort.Slice(items, func(i, j int) bool { return bytes.Compare(items[i], items[j]) < 0 })
}

// resolveKey produces the byAttribute key for a primary membership.
//
// Verbatim with paramsInIdentity: substitute "_" placeholders in path with params.
// Verbatim without params: use the verbatim string.
// Ref-shaped: use the human-readable form when present, otherwise "ref:N".
func resolveKey(rec membershipRec) (key string) {
	mv := rec.value
	switch mv.Kind {
	case membershiprole.MembershipKindVerbatim:
		key = chooseVerbatim(mv)
	case membershiprole.MembershipKindMixedLowCardVerbatimHighCardParam:
		key = chooseVerbatim(mv)
		if rec.paramTreatment == membershiprole.ParamTreatmentIdentity && mv.Params != "" {
			key = substituteParam(key, mv.Params)
		}
	case membershiprole.MembershipKindRef:
		key = chooseRef(mv)
	case membershiprole.MembershipKindRefParametrized:
		key = chooseRef(mv)
		if rec.paramTreatment == membershiprole.ParamTreatmentIdentity && mv.HumanReadableParams != "" {
			key = key + "/" + mv.HumanReadableParams
		}
	case membershiprole.MembershipKindMixedLowCardRefHighCardParam:
		key = chooseRef(mv)
		if rec.paramTreatment == membershiprole.ParamTreatmentIdentity && mv.HumanReadableParams != "" {
			key = key + "/" + mv.HumanReadableParams
		}
	}
	return
}

func chooseVerbatim(mv membershiprole.MembershipValue) (s string) {
	if mv.Verbatim != "" {
		return mv.Verbatim
	}
	return mv.HumanReadableValue
}

func chooseRef(mv membershiprole.MembershipValue) (s string) {
	if mv.HumanReadableRef != "" {
		return mv.HumanReadableRef
	}
	if mv.HumanReadableValue != "" {
		return mv.HumanReadableValue
	}
	return "ref:" + strconv.FormatUint(mv.Ref, 10)
}

func substituteParam(path string, param string) (out string) {
	idx := strings.Index(path, "_")
	if idx < 0 {
		return path + "/" + param
	}
	return path[:idx] + param + path[idx+1:]
}

// labelObject serializes a secondary membership as a label object.
func labelObject(mv membershiprole.MembershipValue) (v jsontext.Value) {
	buf := &bytes.Buffer{}
	buf.WriteString(`{"name":`)
	buf.Write(mustQuote(labelName(mv)))
	if mv.Params != "" || mv.HumanReadableParams != "" {
		buf.WriteString(`,"params":`)
		params := mv.HumanReadableParams
		if params == "" {
			params = mv.Params
		}
		buf.Write(mustQuote(params))
	}
	buf.WriteByte('}')
	return jsontext.Value(buf.Bytes())
}

// encodeParamsValue renders a params string as JSON. ADR-0018 example shows
// params as an array; this implementation wraps the single param in an array
// and emits each element as a JSON-native integer when parseable, otherwise
// as a quoted string. Multi-slot params (separator-delimited) are out of
// scope for the first cut and emit as a single string element.
func encodeParamsValue(params string) (v jsontext.Value) {
	if params == "" {
		return jsontext.Value("[]")
	}
	if i, err := strconv.ParseInt(params, 10, 64); err == nil {
		return jsontext.Value("[" + strconv.FormatInt(i, 10) + "]")
	}
	if u, err := strconv.ParseUint(params, 10, 64); err == nil {
		return jsontext.Value("[" + strconv.FormatUint(u, 10) + "]")
	}
	buf := &bytes.Buffer{}
	buf.WriteByte('[')
	buf.Write(mustQuote(params))
	buf.WriteByte(']')
	return jsontext.Value(buf.Bytes())
}

func labelName(mv membershiprole.MembershipValue) (s string) {
	switch mv.Kind {
	case membershiprole.MembershipKindVerbatim, membershiprole.MembershipKindMixedLowCardVerbatimHighCardParam:
		return chooseVerbatim(mv)
	default:
		return chooseRef(mv)
	}
}

// flushEntity emits the buffered byStructure + byAttribute payload for the
// just-completed entity. Called from EndEntity.
func (inst *JsonCardEmitter) flushEntity() {
	if inst.ndjson {
		// One JSON object per line; the encoder handles the framing.
	}
	inst.writeToken(jsontext.BeginObject)

	inst.writeToken(jsontext.String("byStructure"))
	inst.writeByStructure()

	inst.writeToken(jsontext.String("byAttribute"))
	inst.writeByAttribute()

	inst.writeToken(jsontext.EndObject)
}

func (inst *JsonCardEmitter) writeByStructure() {
	inst.writeToken(jsontext.BeginObject)

	inst.writeToken(jsontext.String("plain"))
	inst.writeToken(jsontext.BeginArray)
	for _, p := range inst.plainBuf {
		inst.writeToken(jsontext.BeginObject)
		inst.writeToken(jsontext.String("itemType"))
		inst.writeToken(jsontext.String(p.itemType.String()))
		inst.writeToken(jsontext.String("values"))
		inst.writeToken(jsontext.BeginObject)
		for _, name := range p.colOrder {
			key := name.String()
			v, ok := p.values[key]
			if !ok {
				v = jsontext.Value("null")
			}
			inst.writeToken(jsontext.String(key))
			inst.writeValue(v)
		}
		inst.writeToken(jsontext.EndObject)
		inst.writeToken(jsontext.EndObject)
	}
	inst.writeToken(jsontext.EndArray)

	inst.writeToken(jsontext.String("sections"))
	inst.writeToken(jsontext.BeginArray)
	for _, s := range inst.sectionBuf {
		inst.writeToken(jsontext.BeginObject)
		inst.writeToken(jsontext.String("name"))
		inst.writeToken(jsontext.String(s.name.String()))
		inst.writeToken(jsontext.String("nAttrs"))
		inst.writeToken(jsontext.Int(int64(s.nAttrs)))
		inst.writeToken(jsontext.EndObject)
	}
	inst.writeToken(jsontext.EndArray)

	inst.writeToken(jsontext.String("coGroups"))
	inst.writeToken(jsontext.BeginArray)
	groups := append([]coGroupRec(nil), inst.coGroupBuf...)
	sort.Slice(groups, func(i, j int) bool { return groups[i].key < groups[j].key })
	for _, g := range groups {
		inst.writeToken(jsontext.BeginObject)
		inst.writeToken(jsontext.String("key"))
		inst.writeToken(jsontext.String(string(g.key)))
		inst.writeToken(jsontext.String("sections"))
		inst.writeToken(jsontext.BeginArray)
		for _, sn := range g.sections {
			inst.writeToken(jsontext.String(sn.String()))
		}
		inst.writeToken(jsontext.EndArray)
		inst.writeToken(jsontext.String("nAttrs"))
		inst.writeToken(jsontext.Int(int64(g.nAttrs)))
		inst.writeToken(jsontext.EndObject)
	}
	inst.writeToken(jsontext.EndArray)

	inst.writeToken(jsontext.EndObject)
}

// projectedAttribute holds the sorted/coalesced output shape for one byAttribute key.
type projectedAttribute struct {
	key      string
	section  naming.StylableName
	coGroup  naming.Key
	primary  []membershipRec
	labels   []membershipRec
	colShape valueShapeE
	colCount int
	colOrder []string
	colVals  map[string]jsontext.Value

	// indexedTreatment records that the canonical primary uses
	// ParamTreatmentIndex; multiple raw attributes sharing this key collapse
	// into one byAttribute entry with an indexed:[{params, value}, ...] field.
	indexedTreatment bool
	// indexedRows accumulates per-param rows when indexedTreatment is true.
	indexedRows []indexedRow

	// byCoSection is non-nil when multiple co-grouped sections at the same
	// attribute index contribute to one logical attribute. Keys are section
	// names; values are the per-section payload.
	byCoSection map[string]coSectionPayload
}

type coSectionPayload struct {
	colShape valueShapeE
	colOrder []string
	colVals  map[string]jsontext.Value
}

type indexedRow struct {
	params   string
	colShape valueShapeE
	colOrder []string
	colVals  map[string]jsontext.Value
}

func (inst *JsonCardEmitter) writeByAttribute() {
	projs := inst.projectAttributes()
	sort.Slice(projs, func(i, j int) bool { return projs[i].key < projs[j].key })

	inst.writeToken(jsontext.BeginObject)
	for _, p := range projs {
		inst.writeToken(jsontext.String(p.key))
		inst.writeAttribute(p)
	}
	inst.writeToken(jsontext.EndObject)
}

// projectAttributes turns buffered attributeRec entries into the output shape.
// Handles single-section attributes, paramTreatmentIndex grouping, and
// co-grouped multi-primary folding (multiple co-grouped sections contributing
// at the same attribute index → coGroup + byCoSection shape).
func (inst *JsonCardEmitter) projectAttributes() (projs []projectedAttribute) {
	byKey := make(map[string]*projectedAttribute)
	order := make([]string, 0, len(inst.attributes))

	// Map (coGroup, attrIdx) → canonical key already projected, so subsequent
	// sections in the same co-group at the same attribute index fold into the
	// existing entry's byCoSection.
	type coIdent struct {
		coGroup naming.Key
		attrIdx int32
	}
	coGroupKeyByIdent := make(map[coIdent]string)

	for _, attr := range inst.attributes {
		var primaries, secondaries []membershipRec
		for _, t := range attr.tags {
			switch t.role {
			case membershiprole.MembershipRolePrimary:
				primaries = append(primaries, t)
			case membershiprole.MembershipRoleSecondary:
				secondaries = append(secondaries, t)
			}
		}

		var keys []string
		for _, p := range primaries {
			keys = append(keys, resolveKey(p))
		}
		var canonicalKey string
		var canonicalRec membershipRec
		var indexed bool
		if len(keys) > 0 {
			canonicalKey = keys[0]
			canonicalRec = primaries[0]
			for i, k := range keys[1:] {
				if k < canonicalKey {
					canonicalKey = k
					canonicalRec = primaries[i+1]
				}
			}
			indexed = canonicalRec.paramTreatment == membershiprole.ParamTreatmentIndex
		} else {
			canonicalKey = "_unidentified/" + attr.sectionName.String() + "/" + strconv.FormatInt(int64(attr.attrIdx), 10)
		}

		colOrder := make([]string, 0, len(attr.colValues))
		for k := range attr.colValues {
			colOrder = append(colOrder, k)
		}
		sort.Strings(colOrder)
		colOrder = matchDeclaredOrder(inst.curSectionColNames, colOrder, attr.sectionName, inst)

		// Co-grouped multi-primary fold: when this attribute belongs to a
		// co-group AND another section in the same co-group already produced
		// an entry at this attrIdx, fold both into a byCoSection shape.
		if attr.coGroup != "" {
			ident := coIdent{coGroup: attr.coGroup, attrIdx: attr.attrIdx}
			if existingKey, found := coGroupKeyByIdent[ident]; found {
				existing := byKey[existingKey]
				if existing != nil {
					if existing.byCoSection == nil {
						existing.byCoSection = map[string]coSectionPayload{
							existing.section.String(): {
								colShape: existing.colShape,
								colOrder: existing.colOrder,
								colVals:  existing.colVals,
							},
						}
						existing.colShape = valueShapeUnset
						existing.colOrder = nil
						existing.colVals = nil
						existing.colCount = 0
					}
					existing.byCoSection[attr.sectionName.String()] = coSectionPayload{
						colShape: attr.colShape,
						colOrder: colOrder,
						colVals:  attr.colValues,
					}
					existing.labels = append(existing.labels, secondaries...)
					continue
				}
			}
		}

		if indexed {
			row := indexedRow{
				params:   canonicalRec.value.HumanReadableParams,
				colShape: attr.colShape,
				colOrder: colOrder,
				colVals:  attr.colValues,
			}
			if existing, ok := byKey[canonicalKey]; ok && existing.indexedTreatment {
				existing.indexedRows = append(existing.indexedRows, row)
				existing.labels = append(existing.labels, secondaries...)
				continue
			}
			rec := projectedAttribute{
				key:              canonicalKey,
				section:          attr.sectionName,
				coGroup:          attr.coGroup,
				primary:          primaries,
				labels:           secondaries,
				indexedTreatment: true,
				indexedRows:      []indexedRow{row},
			}
			byKey[canonicalKey] = &rec
			order = append(order, canonicalKey)
			continue
		}

		rec := projectedAttribute{
			key:      canonicalKey,
			section:  attr.sectionName,
			coGroup:  attr.coGroup,
			primary:  primaries,
			labels:   secondaries,
			colShape: attr.colShape,
			colCount: len(attr.colValues),
			colOrder: colOrder,
			colVals:  attr.colValues,
		}
		byKey[canonicalKey] = &rec
		order = append(order, canonicalKey)
		if attr.coGroup != "" {
			coGroupKeyByIdent[coIdent{coGroup: attr.coGroup, attrIdx: attr.attrIdx}] = canonicalKey
		}
	}

	for _, k := range order {
		projs = append(projs, *byKey[k])
	}
	return
}

// matchDeclaredOrder reorders fallback to follow declared (BeginSection) column
// order when available. The first cut returns fallback unchanged; declared
// order will land alongside the multi-column section work that exercises it.
func matchDeclaredOrder(declared []naming.StylableName, fallback []string, _ naming.StylableName, _ *JsonCardEmitter) (out []string) {
	if len(declared) == 0 {
		return fallback
	}
	out = make([]string, 0, len(fallback))
	seen := make(map[string]bool, len(fallback))
	for _, n := range declared {
		s := n.String()
		if _, ok := lookupString(fallback, s); ok {
			out = append(out, s)
			seen[s] = true
		}
	}
	for _, n := range fallback {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

func lookupString(haystack []string, needle string) (val string, found bool) {
	for _, h := range haystack {
		if h == needle {
			return h, true
		}
	}
	return
}

// strconv is kept live by the resolveKey helper; strings via substituteParam.
var _ = strconv.IntSize
var _ = strings.HasPrefix

func (inst *JsonCardEmitter) writeAttribute(p projectedAttribute) {
	inst.writeToken(jsontext.BeginObject)

	if p.byCoSection == nil {
		inst.writeToken(jsontext.String("section"))
		inst.writeToken(jsontext.String(p.section.String()))
	}

	if p.coGroup != "" {
		inst.writeToken(jsontext.String("coGroup"))
		inst.writeToken(jsontext.String(string(p.coGroup)))
	}

	switch {
	case p.byCoSection != nil:
		secNames := make([]string, 0, len(p.byCoSection))
		for k := range p.byCoSection {
			secNames = append(secNames, k)
		}
		sort.Strings(secNames)
		inst.writeToken(jsontext.String("byCoSection"))
		inst.writeToken(jsontext.BeginObject)
		for _, sn := range secNames {
			payload := p.byCoSection[sn]
			inst.writeToken(jsontext.String(sn))
			inst.writeToken(jsontext.BeginObject)
			switch {
			case len(payload.colVals) == 0:
				// no value
			case len(payload.colVals) == 1 && payload.colShape == valueShapeScalar:
				inst.writeToken(jsontext.String("scalar"))
				inst.writeValue(payload.colVals[payload.colOrder[0]])
			case len(payload.colVals) == 1 && (payload.colShape == valueShapeArray || payload.colShape == valueShapeSet):
				inst.writeToken(jsontext.String("value"))
				inst.writeValue(payload.colVals[payload.colOrder[0]])
			default:
				inst.writeToken(jsontext.String("values"))
				inst.writeToken(jsontext.BeginObject)
				for _, n := range payload.colOrder {
					inst.writeToken(jsontext.String(n))
					inst.writeValue(payload.colVals[n])
				}
				inst.writeToken(jsontext.EndObject)
			}
			inst.writeToken(jsontext.EndObject)
		}
		inst.writeToken(jsontext.EndObject)
	case p.indexedTreatment:
		inst.writeToken(jsontext.String("indexed"))
		inst.writeToken(jsontext.BeginArray)
		for _, row := range p.indexedRows {
			inst.writeToken(jsontext.BeginObject)
			inst.writeToken(jsontext.String("params"))
			inst.writeValue(encodeParamsValue(row.params))
			switch {
			case len(row.colVals) == 0:
				// no value column; skip
			case len(row.colVals) == 1 && row.colShape == valueShapeScalar:
				inst.writeToken(jsontext.String("value"))
				inst.writeValue(row.colVals[row.colOrder[0]])
			case len(row.colVals) == 1 && (row.colShape == valueShapeArray || row.colShape == valueShapeSet):
				inst.writeToken(jsontext.String("value"))
				inst.writeValue(row.colVals[row.colOrder[0]])
			default:
				inst.writeToken(jsontext.String("values"))
				inst.writeToken(jsontext.BeginObject)
				for _, n := range row.colOrder {
					inst.writeToken(jsontext.String(n))
					inst.writeValue(row.colVals[n])
				}
				inst.writeToken(jsontext.EndObject)
			}
			inst.writeToken(jsontext.EndObject)
		}
		inst.writeToken(jsontext.EndArray)
	case p.colCount == 0:
		// no value (e.g. null section)
	case p.colCount == 1 && p.colShape == valueShapeScalar:
		inst.writeToken(jsontext.String("scalar"))
		inst.writeValue(p.colVals[p.colOrder[0]])
	case p.colCount == 1 && (p.colShape == valueShapeArray || p.colShape == valueShapeSet):
		inst.writeToken(jsontext.String("value"))
		inst.writeValue(p.colVals[p.colOrder[0]])
	default:
		inst.writeToken(jsontext.String("values"))
		inst.writeToken(jsontext.BeginObject)
		for _, n := range p.colOrder {
			inst.writeToken(jsontext.String(n))
			inst.writeValue(p.colVals[n])
		}
		inst.writeToken(jsontext.EndObject)
	}

	if len(p.primary) > 1 {
		var aliases []string
		canonical := ""
		for _, m := range p.primary {
			k := resolveKey(m)
			if canonical == "" || k < canonical {
				canonical = k
			}
		}
		for _, m := range p.primary {
			k := resolveKey(m)
			if k != canonical {
				aliases = append(aliases, k)
			}
		}
		sort.Strings(aliases)
		inst.writeToken(jsontext.String("aliases"))
		inst.writeToken(jsontext.BeginArray)
		for _, a := range aliases {
			inst.writeToken(jsontext.String(a))
		}
		inst.writeToken(jsontext.EndArray)
	}

	if len(p.labels) > 0 {
		labelVals := make([]jsontext.Value, 0, len(p.labels))
		for _, l := range p.labels {
			labelVals = append(labelVals, labelObject(l.value))
		}
		// stable order: by name then params (encoded form already includes both)
		sort.Slice(labelVals, func(i, j int) bool { return bytes.Compare(labelVals[i], labelVals[j]) < 0 })
		inst.writeToken(jsontext.String("labels"))
		inst.writeToken(jsontext.BeginArray)
		for _, l := range labelVals {
			inst.writeValue(l)
		}
		inst.writeToken(jsontext.EndArray)
	}

	inst.writeToken(jsontext.EndObject)
}
