package gloss

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// The content family: ADR-0123's vocabulary, registered first and in its
// original order. Their block faces — the markdown widget, the code view, the
// image — are the host's; what lives here is the name, the parameter
// contract and the inline face a table cell shows.
const (
	MediaTypeMarkdown = "text/markdown"
	MediaTypePlain    = "text/plain"
	MediaTypeJSON     = "application/json"
	MediaTypeSQL      = "application/sql"
	MediaTypeGo       = "text/x-go"
	MediaTypePNG      = "image/png"
	MediaTypeJPEG     = "image/jpeg"
	MediaTypeGIF      = "image/gif"
)

// ParamCharset is accepted and ignored on the text types, so a
// `text/markdown; charset=utf-8` declaration keeps working (ADR-0123 §SD2).
const ParamCharset = "charset"

// ParamEncoding is reserved on the image types for ADR-0123 §SD7's
// `;encoding=base64`. Declared so the name is taken, refused in Bind so a
// declaration cannot silently render base64 text as image bytes.
const ParamEncoding = "encoding"

// FirstLineMax bounds FirstLine. Truncation on width happens in the cell,
// but a multi-megabyte blob would otherwise be shipped across the wire in
// full to draw forty visible characters.
const FirstLineMax = 256

// FirstLine is the inline face of every text-shaped content type, and the
// fallback rendering of a declared cell that could not be rendered as
// declared: the first line, capped, made valid UTF-8.
func FirstLine(raw string) string {
	if i := strings.IndexByte(raw, '\n'); i >= 0 {
		raw = raw[:i]
	}
	if len(raw) > FirstLineMax {
		raw = raw[:FirstLineMax]
	}
	return utfsafe.EnsureUTF8(raw)
}

// simpleGloss is the shape of a gloss without per-column state: fixed
// accepted kinds, an inline function, no parameter parsing.
type simpleGloss struct {
	mediaType  string
	doc        string
	params     []ParamSpec
	affinities []string
	accepts    []ValueKindE
	inline     func(cell CellI) Inline
	// refuseParam, when set, refuses any binding that supplies the named
	// parameter — the reserved-parameter idiom.
	refuseParam string
}

var _ GlossI = (*simpleGloss)(nil)

func (inst *simpleGloss) MediaType() string    { return inst.mediaType }
func (inst *simpleGloss) Doc() string          { return inst.doc }
func (inst *simpleGloss) Params() []ParamSpec  { return inst.params }
func (inst *simpleGloss) Affinities() []string { return inst.affinities }

func (inst *simpleGloss) Bind(params map[string]string) (InstanceI, error) {
	if inst.refuseParam != "" {
		if v, set := params[inst.refuseParam]; set {
			return nil, eb.Build().Str("mediaType", inst.mediaType).Errorf("%s=%q is reserved and not supported yet by %s", inst.refuseParam, v, inst.mediaType)
		}
	}
	return &simpleInstance{g: inst, params: params}, nil
}

type simpleInstance struct {
	g      *simpleGloss
	params map[string]string
}

var _ InstanceI = (*simpleInstance)(nil)

func (inst *simpleInstance) Gloss() GlossI             { return inst.g }
func (inst *simpleInstance) Params() map[string]string { return inst.params }
func (inst *simpleInstance) Inline(cell CellI) Inline  { return inst.g.inline(cell) }
func (inst *simpleInstance) Accepts(kind ValueKindE) (ok bool, reason string) {
	return acceptsKind(inst.g.mediaType, inst.g.accepts, kind)
}

// acceptsKind is the shared refusal text: it names what the gloss wanted and
// what it got, since the host shows it beside the plain cell.
func acceptsKind(mediaType string, accepted []ValueKindE, kind ValueKindE) (ok bool, reason string) {
	if slices.Contains(accepted, kind) {
		return true, ""
	}
	names := make([]string, 0, len(accepted))
	for _, k := range accepted {
		names = append(names, k.String())
	}
	return false, fmt.Sprintf("%s expects %s, got %s", mediaType, strings.Join(names, " or "), kind)
}

var textLike = []ValueKindE{ValueKindText, ValueKindBytes}

// rawOrText is what a content face reads: the undecorated bytes when the
// cell has them, the plain rendering otherwise (`SELECT 42 AS x@text/plain`
// is odd but total).
func rawOrText(cell CellI) string {
	if raw, ok := cell.Raw(); ok {
		return raw
	}
	return cell.Text()
}

func firstLineFace(cell CellI) Inline {
	return Inline{Text: FirstLine(rawOrText(cell))}
}

func imageFace(mediaType string) func(cell CellI) Inline {
	return func(cell CellI) Inline {
		raw := rawOrText(cell)
		return Inline{Text: fmt.Sprintf("[%s · %s]", mediaType, humanize.IBytes(uint64(len(raw))))}
	}
}

func contentFamily() []GlossI {
	charset := []ParamSpec{{Name: ParamCharset, Doc: "accepted and ignored; the bytes are read as-is"}}
	encoding := []ParamSpec{{Name: ParamEncoding, Doc: "reserved for a base64 source (ADR-0123 §SD7); not supported yet"}}
	text := func(mt, doc string, affinities []string) GlossI {
		return &simpleGloss{mediaType: mt, doc: doc, params: charset, affinities: affinities, accepts: textLike, inline: firstLineFace}
	}
	image := func(mt, doc string) GlossI {
		return &simpleGloss{mediaType: mt, doc: doc, params: encoding, accepts: textLike, inline: imageFace(mt), refuseParam: ParamEncoding}
	}
	return []GlossI{
		text(MediaTypeMarkdown, "rendered markdown (block); first line (inline)", nil),
		text(MediaTypePlain, "wrapped, untruncated text (block); first line (inline)", nil),
		text(MediaTypeJSON, "pretty-printed, highlighted JSON (block); first line (inline)",
			[]string{`\bsem:json(-scalar|-array|-object)?\b`}),
		text(MediaTypeSQL, "highlighted SQL (block); first line (inline)", nil),
		text(MediaTypeGo, "highlighted Go (block); first line (inline)", nil),
		image(MediaTypePNG, "decoded image (block); type and size (inline)"),
		image(MediaTypeJPEG, "decoded image (block); type and size (inline)"),
		image(MediaTypeGIF, "decoded image (block); type and size (inline)"),
	}
}
