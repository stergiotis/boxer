package gloss

import (
	"fmt"

	"github.com/dustin/go-humanize"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

// MediaTypeCBOR joins the content family after ADR-0123's eight: a column
// holding CBOR bytes, shown as RFC 8949 §8 diagnostic notation. It is the
// IANA type from RFC 8949 §9.1, so nothing about the declaration syntax is
// new — `SELECT item AS `entity@application/cbor“ reads like the JSON one
// beside it.
const MediaTypeCBOR = "application/cbor"

// ParamSequence declares that a cell holds an RFC 8742 CBOR *sequence* —
// items one after another — rather than a single item. Off by default,
// because bytes after the first item are a truncation far more often than
// they are a second item, and the default should say so.
const ParamSequence = "sequence"

// cborInlineMaxBytes bounds the item the inline face is willing to walk.
//
// Unlike a block face, an inline face has no cache: it runs for every
// visible cell of the column on every frame, and the walk plus its output
// is garbage each time. A kilobyte over a screenful of rows is a few tens
// of microseconds and a few hundred kilobytes a frame; a megabyte is not.
// Past the bound the cell shows the type and size, as an image cell does,
// and the Detail block face — which is cached per (row, column) — renders
// the item in full.
const cborInlineMaxBytes = 1 << 10

// cborGloss is `application/cbor`. It is not a simpleGloss because it
// parses a parameter: sequence changes what the notation means, so it is
// read once in Bind rather than per cell.
type cborGloss struct{}

var _ GlossI = (*cborGloss)(nil)

func (inst *cborGloss) MediaType() string { return MediaTypeCBOR }

func (inst *cborGloss) Doc() string {
	return "pretty-printed, highlighted RFC 8949 diagnostic notation (block); the compact notation, capped (inline)"
}

func (inst *cborGloss) Params() []ParamSpec {
	return []ParamSpec{
		{Name: ParamSequence, Doc: "1 to read the cell as an RFC 8742 sequence of items rather than one item", Values: []string{"0", "1"}},
		{Name: ParamEncoding, Doc: "reserved for a hex or base64 source; not supported yet — the cell must hold the bytes themselves"},
	}
}

// Affinities is nil: no aspect in the ADR-0182 vocabularies says "these
// bytes are CBOR", and being a Binary column does not make a column CBOR.
// Bind it by alias or by rule.
func (inst *cborGloss) Affinities() []string { return nil }

func (inst *cborGloss) Bind(params map[string]string) (InstanceI, error) {
	if v, set := params[ParamEncoding]; set {
		return nil, eb.Build().Str("mediaType", MediaTypeCBOR).Errorf("%s=%q is reserved and not supported yet by %s: the column must hold the CBOR bytes, not a text encoding of them", ParamEncoding, v, MediaTypeCBOR) //boxer:lint disable=CS013 reason="Bind's message becomes Diagnostic.Reason, which catalog.go renders"
	}
	return &cborInstance{g: inst, params: params, sequence: params[ParamSequence] == "1"}, nil
}

type cborInstance struct {
	g        *cborGloss
	params   map[string]string
	sequence bool
}

var _ InstanceI = (*cborInstance)(nil)

func (inst *cborInstance) Gloss() GlossI             { return inst.g }
func (inst *cborInstance) Params() map[string]string { return inst.params }

func (inst *cborInstance) Accepts(kind ValueKindE) (ok bool, reason string) {
	return acceptsKind(MediaTypeCBOR, textLike, kind)
}

// CborOptionsI is the seam a host's block face reads. The instance answers
// with the rendering it wants, so a column's pretty block and its compact
// cell cannot disagree about what its bytes mean — the host does not
// re-derive `;sequence` from the declaration's parameters.
type CborOptionsI interface {
	Options(compact bool) diag.Options
}

var _ CborOptionsI = (*cborInstance)(nil)

// Options is the rendering this instance asks for, pretty (the block face)
// or compact (the inline one).
func (inst *cborInstance) Options(compact bool) diag.Options {
	return diag.Options{
		Compact:     compact,
		Sequence:    inst.sequence,
		TagComments: !compact,
		BytesFold:   cborBlockBytesFold(compact),
	}
}

// cborBlockBytesFold breaks a long byte string across rows in the block
// face. A 4 KiB blob inside an item is one unreadable line otherwise, and
// RFC 8949 §8 permits the whitespace.
func cborBlockBytesFold(compact bool) int {
	if compact {
		return 0
	}
	return 32
}

// Inline is the compact notation, capped by FirstLine. Malformed input is
// not an empty cell: diag degrades to what it parsed plus the failure, and
// the error tone is what makes a bad row findable in a grid of good ones.
func (inst *cborInstance) Inline(cell CellI) Inline {
	raw := rawOrText(cell)
	if len(raw) == 0 {
		return Inline{}
	}
	if len(raw) > cborInlineMaxBytes {
		return Inline{Text: fmt.Sprintf("[%s · %s]", MediaTypeCBOR, humanize.IBytes(uint64(len(raw))))}
	}
	s, err := diag.String([]byte(raw), inst.Options(true))
	if err != nil {
		return Inline{Text: FirstLine(s), Tone: ToneError}
	}
	return Inline{Text: FirstLine(s)}
}
