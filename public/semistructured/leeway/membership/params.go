package membership

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The membership params channel carries the high-cardinality half of an
// attribute locator — for the canonical JSON mapping, the array indices elided
// from the low-cardinality path, so that `/tags/0` and `/tags/1` share the
// verbatim path `/tags/_` and differ only here.
//
// Leeway declares the channel as opaque bytes (`membershipSerializedType`, a
// BaseTypeStringBytes column) and the generated DML takes a raw []byte, so
// nothing in the wire format forced a shared encoding. Three writers grew three
// incompatible ones and no reader at all. These functions are that missing
// codec; every producer of a params blob should route through them.
//
// # Canonical form
//
// Fixed-width lowercase hex, [ParamsIndexWidth] digits per index, separated by
// [ParamsSeparator], in path order (outermost array first). The empty tuple
// encodes as the empty blob.
//
//	/did                    ->  ""
//	/commit/record/langs/1  ->  "0001"
//	/a/12/b/3               ->  "000c.0003"
//
// Four properties motivate the choice, and each is load-bearing somewhere:
//
//   - **Printable.** [DefaultParamsFormatter] renders a blob with
//     `string(raw)`, so a binary encoding would emit control characters through
//     ADR-0072's representation plane. Hex text renders as itself.
//   - **Fixed width.** Lexicographic order equals numeric order, so a SQL range
//     predicate over the blob needs no cast, and arity is O(1) from the length
//     ([ParamsArity]) rather than a split.
//   - **Built-in decode.** ClickHouse resolves an index with `unhex` and
//     `reinterpretAsUInt16`; no UDF has to be installed to query positionally.
//   - **Bounded.** 16 bits caps an index at [MaxParamsIndex]; a longer array is
//     an explicit [ErrParamsIndexOutOfRange] rather than a silent truncation.
//
// # Reading a blob in SQL
//
// Against a `mvhp` column, with `i` a 1-based position in the index tuple:
//
//	arrayMap(t -> reinterpretAsUInt16(reverse(unhex(t))), splitByChar('.', mvhp))  -- whole tuple
//	reinterpretAsUInt16(reverse(unhex(splitByChar('.', mvhp)[i])))                 -- one index
//	intDiv(length(mvhp) + 1, 5)                                                    -- arity, O(1)
//	mvhp = '0000'                                                                  -- first element of a 1-d array
const (
	// ParamsIndexWidth is the number of hex digits one index occupies.
	ParamsIndexWidth = 4

	// ParamsSeparator separates successive indices.
	ParamsSeparator = '.'

	// ParamsStride is the byte distance between the starts of two successive
	// indices — the width plus the separator.
	ParamsStride = ParamsIndexWidth + 1

	// MaxParamsIndex is the largest index [ParamsIndexWidth] hex digits carry.
	MaxParamsIndex = 1<<(ParamsIndexWidth*4) - 1
)

// ErrParamsIndexOutOfRange reports an index too large for the fixed width. It
// is a real corpus property (a JSON array longer than MaxParamsIndex+1
// elements), not a programming error, so producers are expected to count and
// report it the way they count undecodable documents.
var ErrParamsIndexOutOfRange = eh.Errorf("membership params index exceeds the maximum")

// ErrParamsMalformed reports a blob that is not in canonical form.
var ErrParamsMalformed = eh.Errorf("membership params blob is malformed")

const hexDigitsLower = "0123456789abcdef"

// AppendParams appends the canonical encoding of idx to dst and returns the
// extended slice. dst may be nil. On error the returned slice is nil and dst is
// left untouched, so a caller reusing a scratch buffer keeps whatever it held.
func AppendParams(dst []byte, idx ...uint64) (out []byte, err error) {
	for i, v := range idx {
		if v > MaxParamsIndex {
			err = eb.Build().Uint64("index", v).Int("position", i).Int("max", MaxParamsIndex).Errorf("%w", ErrParamsIndexOutOfRange)
			return
		}
	}
	out = dst
	for i, v := range idx {
		if i > 0 {
			out = append(out, ParamsSeparator)
		}
		out = append(out,
			hexDigitsLower[(v>>12)&0xf],
			hexDigitsLower[(v>>8)&0xf],
			hexDigitsLower[(v>>4)&0xf],
			hexDigitsLower[v&0xf])
	}
	return
}

// EncodeParams returns the canonical encoding of idx. The empty tuple encodes
// as a nil blob, which is what an attribute with no elided array index carries.
func EncodeParams(idx ...uint64) (raw []byte, err error) {
	if len(idx) == 0 {
		return
	}
	raw, err = AppendParams(make([]byte, 0, len(idx)*ParamsStride), idx...)
	return
}

// ParamsArity reports how many indices raw carries, in O(1), without decoding
// it. A malformed length yields the count the blob would have if it were
// canonical; use [DecodeParams] when the blob's shape is not already trusted.
func ParamsArity(raw []byte) (n int) {
	if len(raw) == 0 {
		return
	}
	n = (len(raw) + 1) / ParamsStride
	return
}

// DecodeParams parses a canonical blob back into its indices. Upper-case hex is
// accepted on read even though [AppendParams] never emits it.
func DecodeParams(raw []byte) (idx []uint64, err error) {
	if len(raw) == 0 {
		return
	}
	n := ParamsArity(raw)
	if len(raw) != n*ParamsStride-1 {
		err = eb.Build().Int("length", len(raw)).Int("stride", ParamsStride).Errorf("length is not a whole number of indices: %w", ErrParamsMalformed)
		return
	}
	idx = make([]uint64, n)
	for i := range n {
		off := i * ParamsStride
		if i > 0 && raw[off-1] != ParamsSeparator {
			err = eb.Build().Int("position", i).Int("offset", off-1).Errorf("expected a \""+string(ParamsSeparator)+"\" separator: %w", ErrParamsMalformed)
			idx = nil
			return
		}
		var v uint64
		for j := range ParamsIndexWidth {
			d, ok := hexValue(raw[off+j])
			if !ok {
				err = eb.Build().Int("position", i).Int("offset", off+j).Errorf("expected a hex digit: %w", ErrParamsMalformed)
				idx = nil
				return
			}
			v = v<<4 | uint64(d)
		}
		idx[i] = v
	}
	return
}

func hexValue(c byte) (v byte, ok bool) {
	switch {
	case c >= '0' && c <= '9':
		v, ok = c-'0', true
	case c >= 'a' && c <= 'f':
		v, ok = c-'a'+10, true
	case c >= 'A' && c <= 'F':
		v, ok = c-'A'+10, true
	}
	return
}
