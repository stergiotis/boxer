// Package jsonshred turns a JSON document into leeway's semi-structured
// attributes and back: each leaf is a (path, parameters, value) triple in
// the section of its canonical type, as the leeway-advanced skill's JSON
// mapping describes, and Compose rebuilds the document from the triples
// (plan002 B1).
//
// # The path grammar
//
// A path is a JSON Pointer (RFC 6901): "/" before each step, "~0" for a
// "~" and "~1" for a "/" in an object key. An array step is the
// placeholder "_", and its index is the next of the attribute's
// parameters, in order, so every element of an array shares one path and
// the path stays low-cardinality. A key that is "_" itself is spelled
// "~2", so it never reads as the placeholder. A number kept as its lexical
// form — too large for int64, or a decimal float64 cannot hold exactly —
// carries "~n" at the end of its path. The root is the empty path.
//
// # Fidelity
//
// The lossless mode (the zero Options) keeps null, {} and [] as attributes
// of value-less sections, so Compose(Explode(x)) is Canonical(x) byte for
// byte. Canonical is the document with object keys sorted, insignificant
// whitespace dropped, integers within int64 written plainly, other numbers
// as the shortest form that reads back as the same float64 (or as their
// own text when no float64 is that number), and strings as jsontext quotes
// them. Key order is not preserved, and a duplicate key is refused. The
// plain mode drops null, {} and [], under the quotient "absent, null and
// empty are one". Low cardinality is never inferred from the data: a
// string goes to the symbol section only when Options.Symbol says so for
// its path.
package jsonshred

import (
	"bytes"
	"encoding/json/jsontext"
	"io"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SectionE is the facts section an attribute's value belongs to; String is
// the section's name.
type SectionE uint8

const (
	SectionString SectionE = iota + 1
	SectionSymbol
	SectionI64
	SectionF64
	SectionBool
	SectionNull
	SectionEmptyObject
	SectionEmptyArray
)

var sectionNames = map[SectionE]string{
	SectionString: "stringArray", SectionSymbol: "symbol", SectionI64: "i64Array", SectionF64: "f64Array",
	SectionBool: "bool", SectionNull: "null", SectionEmptyObject: "emptyObject", SectionEmptyArray: "emptyArray",
}

func (inst SectionE) String() string {
	if n, ok := sectionNames[inst]; ok {
		return n
	}
	return "unknown"
}

// Valueless reports whether the section carries no value: null, {} or [].
func (inst SectionE) Valueless() bool {
	return inst == SectionNull || inst == SectionEmptyObject || inst == SectionEmptyArray
}

// Attr is one leaf: its path, the array indices along it, the section of
// its value, and the value — a string for the string and symbol sections
// (the lexical form under a "~n" path), an int64, a float64, a bool, or nil
// for a value-less section.
type Attr struct {
	Path    string
	Params  []uint64
	Section SectionE
	Value   any
}

// Options parameterise Explode. The zero value is the lossless mode with
// no symbol hint.
type Options struct {
	// Plain drops null, {} and [] (the quotient "absent, null and empty
	// are one").
	Plain bool
	// Symbol says which string paths are low-cardinality, from a schema's
	// enum; nil sends every string to the string section.
	Symbol func(path string) bool
}

// NumberSuffix ends the path of a number kept as its lexical form.
const NumberSuffix = "~n"

// Placeholder is the path step of an array element.
const Placeholder = "_"

// EscapeKey spells an object key as a path step.
func EscapeKey(k string) string {
	if k == Placeholder {
		return "~2"
	}
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

// UnescapeKey reads a path step back as the object key it spells.
func UnescapeKey(step string) string {
	if step == "~2" {
		return Placeholder
	}
	return strings.ReplaceAll(strings.ReplaceAll(step, "~1", "/"), "~0", "~")
}

// Explode reads doc into its attributes, in document order. Invalid JSON,
// a duplicate key and trailing data are refused.
func Explode(doc []byte, opts Options) (attrs []Attr, err error) {
	dec := jsontext.NewDecoder(bytes.NewReader(doc))
	st := &exploder{opts: opts}
	if err = st.value(dec, "", nil); err != nil {
		return nil, err
	}
	if _, terr := dec.ReadToken(); terr != io.EOF {
		return nil, eh.Errorf("jsonshred: trailing data after the document")
	}
	return st.attrs, nil
}

type exploder struct {
	opts  Options
	attrs []Attr
}

func (inst *exploder) emit(path string, params []uint64, sec SectionE, v any) {
	if inst.opts.Plain && sec.Valueless() {
		return
	}
	inst.attrs = append(inst.attrs, Attr{Path: path, Params: slices.Clone(params), Section: sec, Value: v})
}

func (inst *exploder) value(dec *jsontext.Decoder, path string, params []uint64) (err error) {
	switch dec.PeekKind() {
	case '{':
		if _, err = dec.ReadToken(); err != nil {
			return eh.Errorf("jsonshred: %w", err)
		}
		empty := true
		for dec.PeekKind() != '}' {
			tok, terr := dec.ReadToken()
			if terr != nil {
				return eh.Errorf("jsonshred: %w", terr)
			}
			empty = false
			if err = inst.value(dec, path+"/"+EscapeKey(tok.String()), params); err != nil {
				return
			}
		}
		if _, err = dec.ReadToken(); err != nil {
			return eh.Errorf("jsonshred: %w", err)
		}
		if empty {
			inst.emit(path, params, SectionEmptyObject, nil)
		}
	case '[':
		if _, err = dec.ReadToken(); err != nil {
			return eh.Errorf("jsonshred: %w", err)
		}
		var i uint64
		for dec.PeekKind() != ']' {
			inner := append(slices.Clone(params), i)
			if err = inst.value(dec, path+"/"+Placeholder, inner); err != nil {
				return
			}
			i++
		}
		if _, err = dec.ReadToken(); err != nil {
			return eh.Errorf("jsonshred: %w", err)
		}
		if i == 0 {
			inst.emit(path, params, SectionEmptyArray, nil)
		}
	case 'n':
		if _, err = dec.ReadToken(); err != nil {
			return eh.Errorf("jsonshred: %w", err)
		}
		inst.emit(path, params, SectionNull, nil)
	case 't', 'f':
		tok, terr := dec.ReadToken()
		if terr != nil {
			return eh.Errorf("jsonshred: %w", terr)
		}
		inst.emit(path, params, SectionBool, tok.Bool())
	case '"':
		tok, terr := dec.ReadToken()
		if terr != nil {
			return eh.Errorf("jsonshred: %w", terr)
		}
		sec := SectionString
		if inst.opts.Symbol != nil && inst.opts.Symbol(path) {
			sec = SectionSymbol
		}
		inst.emit(path, params, sec, tok.String())
	case '0':
		raw, verr := dec.ReadValue()
		if verr != nil {
			return eh.Errorf("jsonshred: %w", verr)
		}
		sec, v := Number(string(raw))
		if sec == SectionString {
			path += NumberSuffix
		}
		inst.emit(path, params, sec, v)
	default:
		// The decoder's own error names what is wrong with the input.
		if _, err = dec.ReadToken(); err != nil {
			return eh.Errorf("jsonshred: %w", err)
		}
		return eh.Errorf("jsonshred: unexpected token")
	}
	return
}

// Number classifies a JSON number by its value, not its spelling: an
// integer within int64 is SectionI64; another number whose shortest
// float64 form is the same number is SectionF64; anything else keeps its
// text, SectionString. A float64 whose shortest form is plain digits
// (`1e+02` is 100) is the integer it names, since that is how the
// canonical form writes it and how the canonical form reads back.
// Negative zero stays a float64, whose sign the canonical form keeps.
func Number(raw string) (sec SectionE, v any) {
	if !strings.ContainsAny(raw, ".eE") {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if i == 0 && strings.HasPrefix(raw, "-") {
				return SectionF64, math.Copysign(0, -1)
			}
			return SectionI64, i
		}
		return SectionString, raw
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return SectionString, raw
	}
	short := FormatFloat(f)
	want, ok1 := new(big.Rat).SetString(raw)
	got, ok2 := new(big.Rat).SetString(short)
	if !ok1 || !ok2 || want.Cmp(got) != 0 {
		return SectionString, raw
	}
	if !strings.ContainsAny(short, ".eE") && !(f == 0 && math.Signbit(f)) {
		if i, ierr := strconv.ParseInt(short, 10, 64); ierr == nil {
			return SectionI64, i
		}
	}
	return SectionF64, f
}

// FormatFloat is a float64 as Compose and Canonical write it: the shortest
// text that reads back as the same float64.
func FormatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// node is one value of the document Compose rebuilds.
type node struct {
	object map[string]*node
	array  []*node
	leaf   []byte
	// isObject and isArray mark a container that holds no child.
	isObject, isArray bool
}

// Compose rebuilds the canonical document from attributes. An attribute
// whose path does not fit the others — a leaf where a container is, an
// array with a missing index, parameters that do not match the
// placeholders — is refused.
func Compose(attrs []Attr) (doc []byte, err error) {
	var root *node
	for _, a := range attrs {
		if err = place(&root, a); err != nil {
			return nil, eb.Build().Str("path", a.Path).Errorf("jsonshred: %w", err)
		}
	}
	if root == nil {
		return nil, eh.Errorf("jsonshred: no attributes to compose")
	}
	return write(nil, root)
}

func place(root **node, a Attr) (err error) {
	path := a.Path
	numberText := strings.HasSuffix(path, NumberSuffix)
	if numberText {
		path = strings.TrimSuffix(path, NumberSuffix)
	}
	leaf, err := leafOf(a, numberText)
	if err != nil {
		return
	}
	var steps []string
	if path != "" {
		if !strings.HasPrefix(path, "/") {
			return eh.Errorf("a path starts with /")
		}
		steps = strings.Split(path[1:], "/")
	}
	if *root == nil {
		*root = &node{}
	}
	n, params := *root, a.Params
	for _, step := range steps {
		if n.leaf != nil || n.isObject && step == Placeholder || n.isArray && step != Placeholder {
			return eh.Errorf("a value or container where another kind is")
		}
		if step == Placeholder {
			if n.object != nil {
				return eh.Errorf("an array where an object is")
			}
			if len(params) == 0 {
				return eh.Errorf("fewer parameters than array steps")
			}
			idx := params[0]
			params = params[1:]
			for uint64(len(n.array)) <= idx {
				n.array = append(n.array, nil)
			}
			if n.array[idx] == nil {
				n.array[idx] = &node{}
			}
			n = n.array[idx]
			continue
		}
		if n.array != nil {
			return eh.Errorf("an object where an array is")
		}
		if n.object == nil {
			n.object = make(map[string]*node)
		}
		key := UnescapeKey(step)
		if n.object[key] == nil {
			n.object[key] = &node{}
		}
		n = n.object[key]
	}
	if len(params) != 0 {
		return eh.Errorf("more parameters than array steps")
	}
	if n.leaf != nil || n.object != nil || n.array != nil || n.isObject || n.isArray {
		return eh.Errorf("two attributes at one place")
	}
	switch a.Section {
	case SectionEmptyObject:
		n.isObject = true
	case SectionEmptyArray:
		n.isArray = true
	default:
		n.leaf = leaf
	}
	return
}

func leafOf(a Attr, numberText bool) (leaf []byte, err error) {
	bad := func() ([]byte, error) {
		return nil, eb.Build().Stringer("section", a.Section).Errorf("a value of the wrong type for its section")
	}
	switch a.Section {
	case SectionString, SectionSymbol:
		s, ok := a.Value.(string)
		if !ok {
			return bad()
		}
		if numberText {
			if !jsontext.Value(s).IsValid() || jsontext.Value(s).Kind() != '0' {
				return nil, eh.Errorf("a ~n path whose value is not a number")
			}
			return []byte(s), nil
		}
		return jsontext.AppendQuote(nil, s)
	case SectionI64:
		i, ok := a.Value.(int64)
		if !ok {
			return bad()
		}
		return strconv.AppendInt(nil, i, 10), nil
	case SectionF64:
		f, ok := a.Value.(float64)
		if !ok {
			return bad()
		}
		return []byte(FormatFloat(f)), nil
	case SectionBool:
		b, ok := a.Value.(bool)
		if !ok {
			return bad()
		}
		return strconv.AppendBool(nil, b), nil
	case SectionNull:
		return []byte("null"), nil
	case SectionEmptyObject, SectionEmptyArray:
		return nil, nil
	}
	return nil, eb.Build().Stringer("section", a.Section).Errorf("an unknown section")
}

func write(dst []byte, n *node) (out []byte, err error) {
	switch {
	case n.leaf != nil:
		return append(dst, n.leaf...), nil
	case n.object != nil || n.isObject:
		keys := make([]string, 0, len(n.object))
		for k := range n.object {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			if dst, err = jsontext.AppendQuote(dst, k); err != nil {
				return
			}
			dst = append(dst, ':')
			if dst, err = write(dst, n.object[k]); err != nil {
				return
			}
		}
		return append(dst, '}'), nil
	case n.array != nil || n.isArray:
		dst = append(dst, '[')
		for i, c := range n.array {
			if c == nil {
				return nil, eb.Build().Int("index", i).Errorf("jsonshred: an array element is missing")
			}
			if i > 0 {
				dst = append(dst, ',')
			}
			if dst, err = write(dst, c); err != nil {
				return
			}
		}
		return append(dst, ']'), nil
	}
	return nil, eh.Errorf("jsonshred: an empty node")
}

// Canonical is the canonical form of doc that Compose(Explode(doc)) yields
// in the lossless mode, computed without the attributes: the reference the
// round-trip tests hold Compose to.
func Canonical(doc []byte) (out []byte, err error) {
	dec := jsontext.NewDecoder(bytes.NewReader(doc))
	n, err := canonicalValue(dec)
	if err != nil {
		return
	}
	if _, terr := dec.ReadToken(); terr != io.EOF {
		return nil, eh.Errorf("jsonshred: trailing data after the document")
	}
	return write(nil, n)
}

func canonicalValue(dec *jsontext.Decoder) (n *node, err error) {
	switch dec.PeekKind() {
	case '{':
		if _, err = dec.ReadToken(); err != nil {
			return nil, eh.Errorf("jsonshred: %w", err)
		}
		n = &node{isObject: true, object: make(map[string]*node)}
		for dec.PeekKind() != '}' {
			tok, terr := dec.ReadToken()
			if terr != nil {
				return nil, eh.Errorf("jsonshred: %w", terr)
			}
			if n.object[tok.String()], err = canonicalValue(dec); err != nil {
				return
			}
		}
		_, err = dec.ReadToken()
	case '[':
		if _, err = dec.ReadToken(); err != nil {
			return nil, eh.Errorf("jsonshred: %w", err)
		}
		n = &node{isArray: true}
		for dec.PeekKind() != ']' {
			c, cerr := canonicalValue(dec)
			if cerr != nil {
				return nil, cerr
			}
			n.array = append(n.array, c)
		}
		_, err = dec.ReadToken()
	case '0':
		raw, verr := dec.ReadValue()
		if verr != nil {
			return nil, eh.Errorf("jsonshred: %w", verr)
		}
		sec, v := Number(string(raw))
		var leaf []byte
		switch sec {
		case SectionI64:
			leaf = strconv.AppendInt(nil, v.(int64), 10)
		case SectionF64:
			leaf = []byte(FormatFloat(v.(float64)))
		default:
			leaf = []byte(v.(string))
		}
		n = &node{leaf: leaf}
	case '"':
		tok, terr := dec.ReadToken()
		if terr != nil {
			return nil, eh.Errorf("jsonshred: %w", terr)
		}
		leaf, qerr := jsontext.AppendQuote(nil, tok.String())
		if qerr != nil {
			return nil, eh.Errorf("jsonshred: %w", qerr)
		}
		n = &node{leaf: leaf}
	default:
		tok, terr := dec.ReadToken()
		if terr != nil {
			return nil, eh.Errorf("jsonshred: %w", terr)
		}
		n = &node{leaf: []byte(tok.String())}
	}
	if err != nil {
		err = eh.Errorf("jsonshred: %w", err)
	}
	return
}
