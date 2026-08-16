package gloss

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// MediaTypeTaggedId shows a fibonacci-tagged identifier (ADR-0106) as its two
// halves rather than as the one large decimal it is stored as: the tag value,
// a separator, and the per-tag counter — the *body* — both in hex.
//
// A tagged id is a single UInt64 carrying a category and a counter, so the
// decimal a grid prints (`3170534137668829242`) says nothing a reader can
// use, while the two halves say everything: which category, which id within
// it. The split is the same one `LW_ID_TAG_VALUE` / `LW_ID_BODY` compute
// server-side, done client-side so a column of ids reads without changing
// the projection.
//
// Hex for both halves because the split is a bit boundary: the tag's code
// width is a multiple of nothing in particular, but reading two hex halves
// beside the `%064b` view an inspector shows is possible, and reading two
// decimals beside it is not.
const MediaTypeTaggedId = "gloss/taggedid"

// TaggedIdSep separates the two halves of the inline face. A colon, because
// the halves are a pair and not a quotient, and because neither half can
// contain one.
const TaggedIdSep = ':'

// TaggedIdParts is a tagged id taken apart — what the inline face renders
// and what a host's block face spells out.
type TaggedIdParts struct {
	// Id is the whole 64-bit word.
	Id uint64
	// TagValue is the decoded tag: the category, minted by whoever assigned
	// tag values. Never 0 — that value is reserved as invalid.
	TagValue uint32
	// TagWidth is the fibonacci code's full width in bits, including its
	// trailing comma bit. It is a property of the tag value, not of the id.
	TagWidth int
	// Body is the per-tag counter. 0 is reserved as invalid, so a body of 0
	// is a structurally well-formed word that was never minted.
	Body uint64
	// MaxBody is the largest body this tag leaves room for — the body mask.
	MaxBody uint64
}

// SplitTaggedId takes a 64-bit word apart the way identifier does, without
// the panics its composition path carries: ok is false when the word is not
// a tagged id at all — it holds no fibonacci comma, or its code decodes past
// the uint32 tag-value domain (a raw bit pattern no minting path produces).
//
// A reserved body of 0 still splits: it is a real tag over a body that was
// never minted, which is worth showing as such rather than hiding behind
// "not a tagged id".
func SplitTaggedId(v uint64) (p TaggedIdParts, ok bool) {
	id := identifier.TaggedId(v)
	tag, body := id.Split()
	if tag == 0 {
		return p, false
	}
	tv := tag.GetValue()
	if !tv.IsValid() {
		return p, false
	}
	return TaggedIdParts{
		Id:       v,
		TagValue: tv.Value(),
		TagWidth: tag.GetTagWidth(),
		Body:     body.Value(),
		MaxBody:  tag.GetMaxPossibleIdIncl().Value(),
	}, true
}

// Inline is the face: `c:3a`, the tag value and the body in hex.
func (inst TaggedIdParts) Inline() string {
	// A stack buffer, not two FormatUint strings joined: this runs per
	// visible cell per frame, and the returned string is the only allocation
	// the contract allows. 8 hex digits of tag value, 16 of body, one
	// separator — 25, rounded up.
	var buf [32]byte
	b := strconv.AppendUint(buf[:0], uint64(inst.TagValue), 16)
	b = append(b, TaggedIdSep)
	b = strconv.AppendUint(b, inst.Body, 16)
	return string(b)
}

// Hex is the whole word, zero-padded to its 16 digits — what a debugger, an
// inspector and a `WHERE id = ` all take.
func (inst TaggedIdParts) Hex() string {
	var buf [18]byte
	b := append(buf[:0], '0', 'x')
	for shift := 60; shift >= 0; shift -= 4 {
		b = append(b, "0123456789abcdef"[(inst.Id>>uint(shift))&0xf])
	}
	return string(b)
}

// ReadTaggedId reads a cell as the 64-bit word a tagged id is: the unsigned
// value when the cell holds an integer, else its text parsed as decimal or
// `0x`-hex, since a `toString(id)` column is still a column of ids and the
// leeway card reaches every value as text.
func ReadTaggedId(cell CellI) (v uint64, ok bool) {
	if u, isUint := cell.Uint64(); isUint {
		return u, true
	}
	s := strings.TrimSpace(rawOrText(cell))
	if s == "" {
		return 0, false
	}
	u, err := strconv.ParseUint(s, 0, 64)
	return u, err == nil
}

// taggedIdGloss has no parameters — the split is a property of the value —
// and no affinity: `sem:` says a column is a surrogate key, not that its
// surrogates are fibonacci-tagged, and glossing a plain sequence as a tagged
// id would fill the column with warnings. A deployment that mints tagged ids
// binds this by rule (ADR-0186's rules-as-code) or by alias.
func taggedIdGloss() GlossI {
	return &simpleGloss{
		mediaType: MediaTypeTaggedId,
		doc:       "a fibonacci-tagged id (ADR-0106) split into tag value and counter, both hex",
		accepts:   []ValueKindE{ValueKindNumeric, ValueKindText},
		inline:    taggedIdFace,
	}
}

// taggedIdFace renders the split, and shows the plain value in the warning
// tone whenever it cannot: a null is empty and untoned, a word that carries
// no comma is not an id, and a reserved body of 0 is a tag over nothing.
func taggedIdFace(cell CellI) Inline {
	if cell.IsNull() {
		return Inline{}
	}
	v, ok := ReadTaggedId(cell)
	if !ok {
		return Inline{Text: cell.Text(), Tone: ToneWarning}
	}
	p, valid := SplitTaggedId(v)
	if !valid {
		return Inline{Text: cell.Text(), Tone: ToneWarning}
	}
	if p.Body == 0 {
		return Inline{Text: p.Inline(), Tone: ToneWarning}
	}
	return Inline{Text: p.Inline()}
}
