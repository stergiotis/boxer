package play

import (
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fxamacker/cbor/v2"

	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
)

// play_identity_notes.go writes the comments that make a row's canonical
// items readable in diagnostic notation (ADR-0289 §SD2): beside each
// position's role, what it holds — a membership's channel and name through
// the session's registries, a slot's sections and column types, a plain
// column's name, a time in ISO 8601, bytes that are text as text — and, for
// the canonform items, which leaf digest belongs to which attribute. The
// notes are comments: the items and their digests are what they were.

// identityNotes annotates one table's canonical items.
type identityNotes struct {
	tbl      *common.TableDesc
	slots    canonwire.SlotTable
	slotsOk  bool
	renderer *membership.Renderer
	// idOut is set when the canonform pin leaves the entity id out of the
	// digest; the entity item's plains say so.
	idOut bool
	// leafNames pairs a canonform leaf digest with its attribute's name,
	// learnt as each attribute item is annotated — before the entity item
	// that lists the digests. A digest names its item's bytes, so an entry
	// from an earlier row is never wrong; the map is dropped when it grows.
	leafNames map[string]string
	digester  canonform.DigesterI
	sigWords  map[string]string
}

// leafNamesMax bounds identityNotes.leafNames.
const leafNamesMax = 4096

func newIdentityNotes(tbl *common.TableDesc, renderer *membership.Renderer, pin string) (inst *identityNotes) {
	inst = &identityNotes{tbl: tbl, renderer: renderer, idOut: strings.Contains(pin, "entity-id=out"),
		leafNames: map[string]string{}, digester: canonform.NewBlake3Digester(), sigWords: map[string]string{}}
	if renderer == nil {
		inst.renderer = membership.DefaultRenderer()
	}
	if tbl != nil {
		var err error
		inst.slots, err = canonwire.BuildSlotTable(tbl)
		inst.slotsOk = err == nil
	}
	return
}

// noteText makes text safe inside a `/ … /` comment: a slash would end it.
func noteText(s string) string {
	return strings.ReplaceAll(s, "/", "∕")
}

// --- canonwire --------------------------------------------------------------

// canonwireOptions are the diagnostic options for a canonwire entity item.
func (inst *identityNotes) canonwireOptions() diag.Options {
	return diag.Options{TagComments: true, AnnotateItem: inst.canonwire}
}

// canonwire annotates the positions of a canonical-wire entity item
// (ADR-0210 SD1–SD4): `[version, plains, tagged]`, a plain item type's
// columns, a slot `[attribute…]`, an attribute `[memberships, v_1…]`, a
// membership `[channel, identity…]`.
func (inst *identityNotes) canonwire(path []diag.PathElem, item []byte) string {
	if n := len(path); n > 0 && path[n-1].Kind == diag.PathElemTag {
		return noteText(tagNote(path[n-1].Tag, item))
	}
	switch {
	case len(path) == 1 && path[0].Kind == diag.PathElemIndex:
		switch path[0].Index {
		case 0:
			return "version"
		case 1:
			return "plains"
		case 2:
			return "tagged"
		}
	case len(path) >= 2 && isIndex(path[0], 1) && path[1].Kind == diag.PathElemKey:
		it, ok := cborKeyUint(path[1].Key)
		if !ok {
			return ""
		}
		if len(path) == 2 {
			return common.PlainItemTypeE(it).String()
		}
		if len(path) == 3 && path[2].Kind == diag.PathElemIndex {
			return noteText(joinNotes(inst.plainColumn(common.PlainItemTypeE(it), path[2].Index), bytesNote(item)))
		}
	case len(path) >= 2 && isIndex(path[0], 2) && path[1].Kind == diag.PathElemKey:
		sig, ok := cborText(path[1].Key)
		if !ok {
			return ""
		}
		if len(path) == 2 {
			return noteText(inst.slotNote(sig))
		}
		if path[2].Kind != diag.PathElemIndex {
			return ""
		}
		switch {
		case len(path) == 3:
			return "attribute"
		case len(path) == 4 && isIndex(path[3], 0):
			return "memberships"
		case len(path) == 4 && path[3].Kind == diag.PathElemIndex:
			return noteText(joinNotes(inst.slotColumn(sig, path[3].Index-1), bytesNote(item)))
		case len(path) == 5 && isIndex(path[3], 0) && path[4].Kind == diag.PathElemIndex:
			if inst.slotSections(sig) > 1 {
				return noteText(inst.slotSectionName(sig, path[4].Index))
			}
			return noteText(inst.wireMembership(item))
		case len(path) == 6 && isIndex(path[3], 0) && inst.slotSections(sig) > 1:
			return noteText(inst.wireMembership(item))
		}
	}
	return ""
}

// wireMembership reads a canonwire membership `[channel, identity]`
// (ADR-0210 SD4) as its channel and its name.
func (inst *identityNotes) wireMembership(item []byte) string {
	var elems []cbor.RawMessage
	if cbor.Unmarshal(item, &elems) != nil || len(elems) != 2 {
		return ""
	}
	var ch uint64
	if cbor.Unmarshal(elems[0], &ch) != nil {
		return ""
	}
	return joinNotes(channelWords(mappingplan.MembershipChannel(ch)), inst.identity(elems[1]))
}

// identity renders a membership identity in the ADR-0201 SD5 shape both
// forms share: a ref by the renderer's name for it, a verbatim name as
// text, per-row parameters after either in brackets.
func (inst *identityNotes) identity(item []byte) string {
	var v any
	if cbor.Unmarshal(item, &v) != nil {
		return ""
	}
	name := func(x any) string {
		switch n := x.(type) {
		case uint64:
			return inst.renderer.RenderRef(n)
		case []byte:
			return inst.renderer.RenderVerbatim(string(n))
		}
		return ""
	}
	parts, isArray := v.([]any)
	if !isArray {
		return name(v)
	}
	switch len(parts) {
	case 1:
		if p, ok := parts[0].([]byte); ok {
			return "params [" + inst.renderer.RenderParams(string(p)) + "]"
		}
	case 2:
		if p, ok := parts[1].([]byte); ok {
			return name(parts[0]) + " [" + inst.renderer.RenderParams(string(p)) + "]"
		}
	}
	return ""
}

// channelWords names a membership channel.
func channelWords(ch mappingplan.MembershipChannel) string {
	switch ch {
	case mappingplan.MembershipChannelLowCardRef:
		return "low-card ref"
	case mappingplan.MembershipChannelLowCardVerbatim:
		return "low-card verbatim"
	case mappingplan.MembershipChannelHighCardRef:
		return "high-card ref"
	case mappingplan.MembershipChannelHighCardVerbatim:
		return "high-card verbatim"
	case mappingplan.MembershipChannelMixedLowCardRef:
		return "mixed low-card ref"
	case mappingplan.MembershipChannelMixedLowCardVerbatim:
		return "mixed low-card verbatim"
	case mappingplan.MembershipChannelLowCardRefParametrized:
		return "low-card ref, parametrized"
	case mappingplan.MembershipChannelHighCardRefParametrized:
		return "high-card ref, parametrized"
	}
	return "channel " + strconv.Itoa(int(ch))
}

// plainColumn names the k-th column of a plain item type's wire array.
func (inst *identityNotes) plainColumn(it common.PlainItemTypeE, k int) string {
	if !inst.slotsOk {
		return ""
	}
	for _, p := range inst.slots.Plains {
		if p.ItemType == it && k >= 0 && k < len(p.ColumnOrder) {
			if i := p.ColumnOrder[k]; i < len(p.Names) {
				return p.Names[i].String()
			}
		}
	}
	return ""
}

// slotsOf are the table's slots carrying signature sig.
func (inst *identityNotes) slotsOf(sig string) (out []*canonwire.Slot) {
	if !inst.slotsOk {
		return nil
	}
	for _, i := range inst.slots.BySignature[sig] {
		out = append(out, &inst.slots.Slots[i])
	}
	return
}

// slotNote reads a slot key: the section — or the co-section group — the
// signature stands for, every candidate when several share it, then the
// column types it is made of.
func (inst *identityNotes) slotNote(sig string) string {
	var secs []string
	for _, s := range inst.slotsOf(sig) {
		var names []string
		for _, sec := range s.Sections {
			names = append(names, sec.Name.String())
		}
		secs = append(secs, strings.Join(names, " + "))
	}
	out := "slot"
	switch {
	case len(secs) == 1:
		out += " · " + secs[0]
	case len(secs) > 1:
		// The wire item does not say which; the decoder's dispatch does
		// (ADR-0210 SD5).
		out += " · one of " + strings.Join(secs, ", ")
	}
	words, found := inst.sigWords[sig]
	if !found {
		words = signatureWords(sig)
		inst.sigWords[sig] = words
	}
	if words != "" {
		out += " · " + words
	}
	return out
}

// slotSections is how many sections a slot of signature sig groups.
func (inst *identityNotes) slotSections(sig string) int {
	if s := inst.slotsOf(sig); len(s) > 0 {
		return len(s[0].Sections)
	}
	return strings.Count(sig, "_") + 1
}

// slotSectionName names the k-th section of a co-section slot.
func (inst *identityNotes) slotSectionName(sig string, k int) string {
	if s := inst.slotsOf(sig); len(s) > 0 && k >= 0 && k < len(s[0].Sections) {
		return s[0].Sections[k].Name.String()
	}
	return ""
}

// slotColumn names the k-th value column of an attribute in a slot of
// signature sig, in key order, when every slot carrying the signature names
// it alike and the slot has more than one column — a lone column needs no
// name.
func (inst *identityNotes) slotColumn(sig string, k int) string {
	name := ""
	for i, s := range inst.slotsOf(sig) {
		n, total := "", 0
		for _, sec := range s.Sections {
			cols := inst.sectionColumns(sec.SectionIdx)
			for _, ord := range sec.ColumnOrder {
				if total == k && ord < len(cols) {
					n = cols[ord]
				}
				total++
			}
		}
		if total <= 1 {
			return ""
		}
		if i > 0 && n != name {
			return ""
		}
		name = n
	}
	return name
}

func (inst *identityNotes) sectionColumns(idx int) (out []string) {
	if inst.tbl == nil || idx < 0 || idx >= len(inst.tbl.TaggedValuesSections) {
		return nil
	}
	for _, n := range inst.tbl.TaggedValuesSections[idx].ValueColumnNames {
		out = append(out, n.String())
	}
	return
}

// signatureWords reads a slot signature's column types: groups (co-sections)
// apart by " + ", columns by ", ".
func signatureWords(sig string) string {
	if sig == "" {
		return "no value"
	}
	parser := canonicaltypes.NewParser()
	var groups []string
	for _, g := range strings.Split(sig, "_") {
		if g == "" {
			groups = append(groups, "no value")
			continue
		}
		var cols []string
		for _, ct := range strings.Split(g, "-") {
			ast, err := parser.ParsePrimitiveTypeAst(ct)
			if err != nil {
				return ""
			}
			cols = append(cols, typeWords(ast))
		}
		groups = append(groups, strings.Join(cols, ", "))
	}
	return strings.Join(groups, " + ")
}

// typeWords reads one canonical type.
func typeWords(ast canonicaltypes.PrimitiveAstNodeI) (out string) {
	var mod canonicaltypes.ScalarModifierE
	switch n := ast.(type) {
	case canonicaltypes.StringAstNode:
		mod = n.ScalarModifier
		switch n.BaseType {
		case canonicaltypes.BaseTypeStringUtf8:
			out = "string"
		case canonicaltypes.BaseTypeStringBytes:
			out = "bytes"
		case canonicaltypes.BaseTypeStringBool:
			out = "bool"
		}
	case canonicaltypes.MachineNumericTypeAstNode:
		mod = n.ScalarModifier
		out = string(rune(n.BaseType)) + strconv.Itoa(int(n.Width))
	case canonicaltypes.TemporalTypeAstNode:
		mod = n.ScalarModifier
		switch n.BaseType {
		case canonicaltypes.BaseTypeTemporalUtcDatetime:
			out = "UTC time"
		case canonicaltypes.BaseTypeTemporalZonedDatetime:
			out = "zoned time"
		case canonicaltypes.BaseTypeTemporalZonedTime:
			out = "time of day"
		}
	case canonicaltypes.NetworkTypeAstNode:
		mod = n.ScalarModifier
		out = "IP address"
	}
	if out == "" {
		out = ast.String()
	}
	switch mod {
	case canonicaltypes.ScalarModifierHomogenousArray:
		out = "list of " + out
	case canonicaltypes.ScalarModifierSet:
		out = "set of " + out
	}
	return
}

// --- canonform --------------------------------------------------------------

// canonformOptions are the diagnostic options for a row's canonform items,
// a sequence of attribute items followed by the entity item.
func (inst *identityNotes) canonformOptions() diag.Options {
	return diag.Options{Sequence: true, TagComments: true, AnnotateItem: inst.canonform}
}

func (inst *identityNotes) leaf(item []byte) []byte {
	h := inst.digester.NewLeaf()
	_, _ = h.Write(item)
	return h.Sum(nil)
}

// canonformName is an attribute item's first primary membership, rendered;
// an attribute with none is named for that.
func (inst *identityNotes) canonformName(item []byte) string {
	var elems []cbor.RawMessage
	if cbor.Unmarshal(item, &elems) != nil || len(elems) == 0 {
		return ""
	}
	var members []cbor.RawMessage
	if cbor.Unmarshal(elems[0], &members) != nil || len(members) == 0 {
		return "no membership"
	}
	return inst.identity(members[0])
}

// attributeNote names a canonform attribute item and its leaf digest's
// prefix, and remembers the pair for the entity item's digest list.
func (inst *identityNotes) attributeNote(item []byte) string {
	name, leaf := inst.canonformName(item), inst.leaf(item)
	if len(inst.leafNames) >= leafNamesMax {
		clear(inst.leafNames)
	}
	inst.leafNames[string(leaf)] = name
	return joinNotes("attribute "+name, "leaf "+hex.EncodeToString(leaf[:4])+"…")
}

// canonform annotates a canonform sequence: attribute items
// `[memberships, value]` and the entity item `{0: plains, 1: leaf digests}`.
func (inst *identityNotes) canonform(path []diag.PathElem, item []byte) string {
	if n := len(path); n > 0 && path[n-1].Kind == diag.PathElemTag {
		return noteText(tagNote(path[n-1].Tag, item))
	}
	if len(path) == 0 {
		switch {
		case len(item) == 0:
			return ""
		case item[0]>>5 == 4:
			return noteText(inst.attributeNote(item))
		case item[0]>>5 == 5:
			return "entity"
		}
		return ""
	}
	switch path[0].Kind {
	case diag.PathElemIndex:
		switch {
		case len(path) == 1 && path[0].Index == 0:
			return "memberships"
		case len(path) == 1 && path[0].Index == 1:
			return noteText(joinNotes("value", bytesNote(item)))
		case len(path) == 2 && path[0].Index == 0:
			return noteText(inst.identity(item))
		case len(path) == 2 && path[0].Index == 1:
			return noteText(bytesNote(item))
		}
	case diag.PathElemKey:
		k, ok := cborKeyUint(path[0].Key)
		if !ok {
			return ""
		}
		switch {
		case len(path) == 1 && k == 0:
			if inst.idOut {
				return "plains · the entity id is left out under this pin"
			}
			return "plains"
		case len(path) == 1 && k == 1:
			return "leaf digests, sorted bytewise · one per attribute item above"
		case len(path) == 2 && k == 0:
			return noteText(bytesNote(item))
		case len(path) == 2 && k == 1:
			var d []byte
			if cbor.Unmarshal(item, &d) == nil {
				if name, found := inst.leafNames[string(d)]; found {
					return noteText("leaf of " + name)
				}
			}
		}
	}
	return ""
}

// --- shared -----------------------------------------------------------------

func isIndex(e diag.PathElem, i int) bool {
	return e.Kind == diag.PathElemIndex && e.Index == i
}

// cborText decodes a CBOR text string key.
func cborText(key []byte) (s string, ok bool) {
	return s, cbor.Unmarshal(key, &s) == nil
}

func joinNotes(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

// bytesNote is a byte string's text when it is printable UTF-8, quoted.
func bytesNote(item []byte) string {
	if len(item) == 0 || item[0]>>5 != 2 {
		return ""
	}
	var b []byte
	if cbor.Unmarshal(item, &b) != nil || len(b) == 0 || !utf8.Valid(b) {
		return ""
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return strconv.Quote(string(b))
}

// tagNote reads a tag's content: an RFC 9581 time (tag 1001) in ISO 8601.
func tagNote(tag uint64, item []byte) string {
	if tag != 1001 {
		return ""
	}
	var m map[int64]any
	if cbor.Unmarshal(item, &m) != nil {
		return ""
	}
	var sec int64
	switch v := m[1].(type) {
	case uint64:
		sec = int64(v)
	case int64:
		sec = v
	case float64:
		sec = int64(math.Floor(v))
	default:
		return ""
	}
	var nsec int64
	for key, scale := range map[int64]int64{-3: 1_000_000, -6: 1_000, -9: 1} {
		switch v := m[key].(type) {
		case uint64:
			nsec += int64(v) * scale
		case int64:
			nsec += v * scale
		}
	}
	return time.Unix(sec, nsec).UTC().Format(time.RFC3339Nano)
}
