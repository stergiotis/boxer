package chpack

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/aspectcodec"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// The LW_ASPECT_* family (ADR-0182 SD4) decodes aspect segments directly
// from physical column names — over system.columns, DESCRIBE output, or any
// place a name travels. Bodies are generated from the live enum tables, so
// the SQL vocabulary can never drift from the Go one; the segment positions
// below are the naming convention's layout contract for the default ':'
// separator, pinned here and verified against the real composer by test
// (names composed with another separator are out of scope for the pack).
//
// v0 scope: single-char digits only — the escape char makes LW_ASPECT_DECODE
// throw and LW_ASPECT_HAS_* return false; no shipped vocabulary reaches the
// escape range. A segment that is "" or the legacy v1 marker "0" decodes as
// the empty set.
const (
	aspectSeparator      = ":"
	aspectPlainPartCount = 7 // scope:name:type:enc:sem:rowcfg:streaming
	aspectPlainEncPart   = 4
	aspectPlainSemPart   = 5

	aspectTaggedPartCount = 11 // tv:section:col:role:type:enc:use:sem:rowcfg:cosec:streaming
	aspectTaggedEncPart   = 6
	aspectTaggedUsePart   = 7
	aspectTaggedSemPart   = 8
)

// unknownAspectChar is the transform default in LW_ASPECT_HAS_* — a char the
// codec alphabet does not contain, so an unknown kebab name matches nothing.
// An empty-string default would match everything, because position with an
// empty needle returns 1.
const unknownAspectChar = "#"

func aspectSegBody(taggedPart int, plainPart int) (body string) {
	parts := fmt.Sprintf("splitByChar('%s', name)", aspectSeparator)
	plain := "''"
	if plainPart > 0 {
		plain = fmt.Sprintf("arrayElement(%s, %d)", parts, plainPart)
	}
	body = fmt.Sprintf("if(length(%s) = %d, arrayElement(%s, %d), if(length(%s) = %d, %s, ''))",
		parts, aspectTaggedPartCount, parts, taggedPart, parts, aspectPlainPartCount, plain)
	return
}

// aspectTables renders the three parallel constant arrays of one vocabulary:
// indices, kebab names, and single-char digits (alphabet[i+1]).
func aspectTables[E aspectEnumI](all []E) (indices string, names string, chars string) {
	is := make([]string, 0, len(all))
	ns := make([]string, 0, len(all))
	cs := make([]string, 0, len(all))
	for _, a := range all {
		is = append(is, fmt.Sprintf("%d", a.Value()))
		ns = append(ns, "'"+a.String()+"'")
		cs = append(cs, "'"+string(aspectcodec.Alphabet[a.Value()+1])+"'")
	}
	indices = "[" + strings.Join(is, ", ") + "]"
	names = "[" + strings.Join(ns, ", ") + "]"
	chars = "[" + strings.Join(cs, ", ") + "]"
	return
}

type aspectEnumI interface {
	Value() uint8
	String() string
}

func aspectNamesBody[E aspectEnumI](all []E) (body string) {
	indices, names, _ := aspectTables(all)
	body = fmt.Sprintf("arrayMap(i -> transform(i, %s, %s, concat('unknown-', toString(i))), LW_ASPECT_DECODE(seg))", indices, names)
	return
}

func aspectHasBody[E aspectEnumI](segFn string, all []E) (body string) {
	_, names, chars := aspectTables(all)
	body = fmt.Sprintf("position(%s(name), 'z') = 0 AND position(%s(name), transform(aspect, %s, %s, '%s')) > 0",
		segFn, segFn, names, chars, unknownAspectChar)
	return
}

func aspectFunctions() (fns []Function) {
	decodeGuard := fmt.Sprintf(
		"toUInt8(position('%s', c) - 2 + 0 * throwIf(position('%s', c) < 2 OR c = 'z', 'LW_ASPECT_DECODE: char outside the v0 aspect range'))",
		aspectcodec.Alphabet, aspectcodec.Alphabet)
	fns = []Function{
		{
			Name:   "LW_ASPECT_SEG_ENC",
			Params: []string{"name"},
			Body:   aspectSegBody(aspectTaggedEncPart, aspectPlainEncPart),
			Doc:    "encoding-hints segment of a physical column name; '' for foreign or aspect-free names",
		},
		{
			Name:   "LW_ASPECT_SEG_USE",
			Params: []string{"name"},
			Body:   aspectSegBody(aspectTaggedUsePart, 0),
			Doc:    "use-aspects segment of a physical column name; '' on plain columns, which carry none",
		},
		{
			Name:   "LW_ASPECT_SEG_SEM",
			Params: []string{"name"},
			Body:   aspectSegBody(aspectTaggedSemPart, aspectPlainSemPart),
			Doc:    "value-semantics segment of a physical column name; '' for foreign or aspect-free names",
		},
		{
			Name:   "LW_ASPECT_DECODE",
			Params: []string{"seg"},
			Body:   fmt.Sprintf("if(seg = '' OR seg = '0', CAST([], 'Array(UInt8)'), arrayMap(c -> %s, splitByString('', seg)))", decodeGuard),
			Doc:    "aspect indices of a v2 segment ('' and the legacy '0' are empty); throws on chars outside the v0 range",
		},
		{
			Name:   "LW_ASPECT_NAMES_ENC",
			Params: []string{"seg"},
			Body:   aspectNamesBody(encodingaspects.AllAspects),
			Doc:    "kebab names of an encoding-hints segment; future indices render as unknown-N",
		},
		{
			Name:   "LW_ASPECT_NAMES_USE",
			Params: []string{"seg"},
			Body:   aspectNamesBody(useaspects.AllAspects),
			Doc:    "kebab names of a use-aspects segment; future indices render as unknown-N",
		},
		{
			Name:   "LW_ASPECT_NAMES_SEM",
			Params: []string{"seg"},
			Body:   aspectNamesBody(valueaspects.AllAspects),
			Doc:    "kebab names of a value-semantics segment; future indices render as unknown-N",
		},
		{
			Name:   "LW_ASPECT_HAS_ENC",
			Params: []string{"name", "aspect"},
			Body:   aspectHasBody("LW_ASPECT_SEG_ENC", encodingaspects.AllAspects),
			Doc:    "whether the column's encoding hints contain the kebab-named aspect; false for unknown names or escape-range segments",
		},
		{
			Name:   "LW_ASPECT_HAS_USE",
			Params: []string{"name", "aspect"},
			Body:   aspectHasBody("LW_ASPECT_SEG_USE", useaspects.AllAspects),
			Doc:    "whether the column's section use-aspects contain the kebab-named aspect; false for unknown names or escape-range segments",
		},
		{
			Name:   "LW_ASPECT_HAS_SEM",
			Params: []string{"name", "aspect"},
			Body:   aspectHasBody("LW_ASPECT_SEG_SEM", valueaspects.AllAspects),
			Doc:    "whether the column's value semantics contain the kebab-named aspect; false for unknown names or escape-range segments",
		},
	}
	return
}
