package datacatalog

import (
	"hash/fnv"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// An attribute key is one leeway column's identity, serialized: the normalized
// (scope, section, name, canonical type) tuple as `scope/section/name:type`.
// Two scopes exist because the leeway naming contract gives a tagged column
// name meaning only inside its section, while a plain column's meaning comes
// from its item type:
//
//	plain/entity-timestamp/observed-at:u64
//	tagged/metric/value:f64
//
// Keys are deliberately coarser than [common.TableOperations.Relate]: they do
// not carry aspect sets, membership specs, or grouping keys. Containment
// therefore implies key containment but not the reverse — two tables with the
// same key set can still relate as overlap. That is the right trade for what
// the keys are for: naming an intersection so that pairs which share one can be
// grouped under a single shape id.
const (
	// ScopePlain prefixes a plain/backbone column's key; the second component
	// is its [common.PlainItemTypeE].
	ScopePlain = "plain"
	// ScopeTagged prefixes a tagged column's key; the second component is its
	// section name.
	ScopeTagged = "tagged"
)

// AttrKeys returns tbl's attribute keys, sorted. The table is normalized first
// (via [common.TableOperations.NormalizedCopy]), so naming style and column
// order never reach the keys — a section authored "geoPoint" and one discovered
// as "geo-point" produce the same key.
//
// ops is borrowed and not safe for concurrent use; a catalog run holds one and
// walks its tables single-threaded.
func AttrKeys(ops *common.TableOperations, tbl *common.TableDesc) (keys []string, err error) {
	var norm common.TableDesc
	norm, err = ops.NormalizedCopy(tbl)
	if err != nil {
		err = eh.Errorf("unable to normalize table: %w", err)
		return
	}
	n := len(norm.PlainValuesNames)
	for _, sec := range norm.TaggedValuesSections {
		n += len(sec.ValueColumnNames)
	}
	keys = make([]string, 0, n)
	for i, name := range norm.PlainValuesNames {
		keys = append(keys, attrKey(ScopePlain, norm.PlainValuesItemTypes[i].String(), string(name), canonicalTypeString(norm.PlainValuesTypes[i])))
	}
	for _, sec := range norm.TaggedValuesSections {
		for i, name := range sec.ValueColumnNames {
			keys = append(keys, attrKey(ScopeTagged, string(sec.Name), string(name), canonicalTypeString(sec.ValueColumnTypes[i])))
		}
	}
	sort.Strings(keys)
	return
}

func attrKey(scope string, section string, name string, canonicalType string) (key string) {
	var b strings.Builder
	b.Grow(len(scope) + len(section) + len(name) + len(canonicalType) + 3)
	b.WriteString(scope)
	b.WriteByte('/')
	b.WriteString(section)
	b.WriteByte('/')
	b.WriteString(name)
	b.WriteByte(':')
	b.WriteString(canonicalType)
	return b.String()
}

// canonicalTypeString renders a canonical type, or the empty string for a table
// built by hand rather than through the manipulator. It mirrors the relation
// code's handling of a nil type: absent is its own value, not an error.
func canonicalTypeString(ct canonicaltypes.PrimitiveAstNodeI) (s string) {
	if ct == nil {
		return ""
	}
	return ct.String()
}

// HashAttrKeys is the catalog's schema_hash / shape_id: fnv64a over the sorted
// keys, each terminated by a newline so that a key boundary cannot be forged by
// a key containing the separator (a key never contains a newline).
//
// It is a grouping id, not a checksum: tables with equal key sets, and
// pair-intersections that coincide, unify on one number so a book chapter can
// draw them as one node. Zero is reserved — [HashAttrKeys] of an empty list
// still returns fnv64a's offset basis, and callers that mean "no intersection"
// write 0 explicitly (see [Pair.ShapeId]).
func HashAttrKeys(keys []string) (h uint64) {
	f := fnv.New64a()
	for _, k := range keys {
		_, _ = f.Write([]byte(k))
		_, _ = f.Write([]byte{'\n'})
	}
	return f.Sum64()
}
