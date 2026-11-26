package applink

import (
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Tail is a tail read into its URL pieces, decoded: the help a codec
// takes so it writes no percent-encoding of its own. What the pieces mean
// is the codec's.
type Tail struct {
	// Segments are the path segments after the envelope, decoded; none is
	// empty.
	Segments []string
	// Query is the query, decoded.
	Query url.Values
	// Fragment is the fragment, decoded.
	Fragment string
}

// ParseTail reads a tail strictly: an empty path segment (`//`, a trailing
// `/`), a malformed escape or a malformed query is an error.
func ParseTail(tail string) (t Tail, err error) {
	if err = checkTail(tail); err != nil {
		return
	}
	rest, frag, hasFrag := strings.Cut(tail, "#")
	path, query, hasQuery := strings.Cut(rest, "?")
	if path != "" {
		for _, raw := range strings.Split(path[1:], "/") {
			if raw == "" {
				err = eb.Build().Errorf("applink: an empty path segment")
				return
			}
			var seg string
			seg, err = url.PathUnescape(raw)
			if err != nil {
				err = eb.Build().Errorf("applink: %w", err)
				return
			}
			t.Segments = append(t.Segments, seg)
		}
	}
	if hasQuery {
		if query == "" {
			err = eb.Build().Errorf("applink: an empty query")
			return
		}
		t.Query, err = url.ParseQuery(query)
		if err != nil {
			err = eb.Build().Errorf("applink: %w", err)
			return
		}
	}
	if hasFrag {
		t.Fragment, err = url.PathUnescape(frag)
		if err != nil {
			err = eb.Build().Errorf("applink: %w", err)
			return
		}
	}
	err = t.checkUTF8()
	return
}

// checkUTF8 refuses a piece whose escapes decode to bytes that are not
// UTF-8: no argument is such text, and an encoder of the arguments would
// refuse it or carry it to a decoder that does.
func (inst Tail) checkUTF8() (err error) {
	ok := utf8.ValidString(inst.Fragment)
	for _, seg := range inst.Segments {
		ok = ok && utf8.ValidString(seg)
	}
	for k, vs := range inst.Query {
		ok = ok && utf8.ValidString(k)
		for _, v := range vs {
			ok = ok && utf8.ValidString(v)
		}
	}
	if !ok {
		err = eb.Build().Errorf("applink: an escape decodes to bytes that are not UTF-8")
	}
	return
}

// String spells the tail canonically: segments path-escaped, query keys
// sorted, fragment escaped as a URL fragment is. ParseTail reads it back to
// the same Tail.
func (inst Tail) String() string {
	var b strings.Builder
	for _, s := range inst.Segments {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	if len(inst.Query) > 0 {
		b.WriteByte('?')
		b.WriteString(inst.Query.Encode())
	}
	if inst.Fragment != "" {
		b.WriteByte('#')
		b.WriteString((&url.URL{Fragment: inst.Fragment}).EscapedFragment())
	}
	return b.String()
}

// OnlyKeys refuses a query key outside allowed, or one given twice: the
// strictness a codec owes the text it reads.
func (inst Tail) OnlyKeys(allowed ...string) (err error) {
	for k, vs := range inst.Query {
		if !slices.Contains(allowed, k) {
			return eb.Build().Str("key", clip(k)).Errorf("applink: unknown query key")
		}
		if len(vs) != 1 {
			return eb.Build().Str("key", k).Errorf("applink: a query key given more than once")
		}
	}
	return
}

// Value is a query key's one value, and whether it was given.
func (inst Tail) Value(key string) (v string, ok bool) {
	vs, ok := inst.Query[key]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}
