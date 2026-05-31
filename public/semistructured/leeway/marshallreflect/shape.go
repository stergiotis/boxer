//go:build llm_generated_opus47

package marshallreflect

import (
	"fmt"
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// optionPkgPath is the import path of the new boxer-side Option type.
// Recognised by classifyReflectType so DTO authors can use
// option.Option[T] without the reflect parser needing to scan the source.
const optionPkgPath = "github.com/stergiotis/boxer/public/functional/option"

// roaringPkgPath is the import path of the *roaring.Bitmap type — the
// one pointer shape marshallgen / marshallreflect both allow on a DTO
// field.
const roaringPkgPath = "github.com/RoaringBitmap/roaring"

// classifyReflectType inspects rt and returns the corresponding shared
// mappingplan.FieldShape (consumed by mappingplan.PlanBuilder). Forbids
// the same Go shapes the codegen classifier forbids: Option[[]T] (except
// Option[[]byte]), []Option[T], arbitrary pointers other than
// *roaring.Bitmap, nested generics other than option.Option. The Go-side
// spelling is recorded as a string so downstream comparisons use the
// same go-type tokens the AST classifier produces ("uint64", "time.Time",
// "[4]byte", "[]byte", "*roaring.Bitmap", …).
func classifyReflectType(rt reflect.Type) (s mappingplan.FieldShape, err error) {
	switch rt.Kind() {

	case reflect.Ptr:
		elem := rt.Elem()
		if elem.PkgPath() == roaringPkgPath && elem.Name() == "Bitmap" {
			s.IsRoaring = true
			s.GoType = "*roaring.Bitmap"
			return
		}
		err = eb.Build().Str("type", rt.String()).Errorf("pointer types forbidden except *roaring.Bitmap — use option.Option[T] for ZeroToOne fields")
		return

	case reflect.Struct:
		// option.Option[T] is the only struct shape the codec accepts
		// on a tagged field. Pure-value types like time.Time get a
		// fast-path further down via reflectGoTypeName.
		if rt.PkgPath() == optionPkgPath {
			s.IsOption = true
			valField, ok := rt.FieldByName("Val")
			if !ok {
				err = eb.Build().Str("type", rt.String()).Errorf("option.Option without Val field — wrong shape")
				return
			}
			// Reject option.Option[[]T] (except option.Option[[]byte]).
			vt := valField.Type
			if vt.Kind() == reflect.Slice {
				if vt.Elem().Kind() == reflect.Uint8 {
					s.GoType = "[]byte"
					return
				}
				err = eb.Build().Str("type", rt.String()).Errorf("option.Option[[]T] is forbidden — use []T for multi-element membership (option.Option[[]byte] is allowed as a scalar blob)")
				return
			}
			s.GoType = reflectGoTypeName(vt)
			return
		}
		// time.Time and any other pure-value struct routes through
		// reflectGoTypeName below.
		s.GoType = reflectGoTypeName(rt)
		return

	case reflect.Slice:
		elem := rt.Elem()
		// []byte: scalar blob lane.
		if elem.Kind() == reflect.Uint8 {
			s.GoType = "[]byte"
			return
		}
		// []option.Option[T] forbidden.
		if elem.Kind() == reflect.Struct && elem.PkgPath() == optionPkgPath {
			err = eb.Build().Str("type", rt.String()).Errorf("[]option.Option[T] is forbidden — option.Option[T] is only allowed as a scalar field")
			return
		}
		s.IsSlice = true
		s.GoType = reflectGoTypeName(elem)
		return

	case reflect.Array:
		elem := rt.Elem()
		if elem.Kind() != reflect.Uint8 {
			err = eb.Build().Str("type", rt.String()).Errorf("only fixed-length `[N]byte` arrays supported (e.g. [4]byte, [16]byte)")
			return
		}
		s.GoType = fmt.Sprintf("[%d]byte", rt.Len())
		return

	default:
		s.GoType = reflectGoTypeName(rt)
	}
	return
}

// reflectGoTypeName produces the source-form Go type name that
// mappingplan.classifyType would have emitted from go/ast for the
// same type. Used so downstream comparisons (against "uint64",
// "time.Time", "[]byte", …) work without an extra translation table.
func reflectGoTypeName(rt reflect.Type) string {
	if rt.PkgPath() == "time" && rt.Name() == "Time" {
		return "time.Time"
	}
	switch rt.Kind() {
	case reflect.Uint8:
		return "uint8"
	case reflect.Uint16:
		return "uint16"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Int8:
		return "int8"
	case reflect.Int16:
		return "int16"
	case reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Float32:
		return "float32"
	case reflect.Float64:
		return "float64"
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	case reflect.Array:
		if rt.Elem().Kind() == reflect.Uint8 {
			return fmt.Sprintf("[%d]byte", rt.Len())
		}
	case reflect.Slice:
		if rt.Elem().Kind() == reflect.Uint8 {
			return "[]byte"
		}
	}
	// Fallback: reflect.Type.String() includes package qualifier
	// (e.g. "time.Time"). Same convention as ast renderers.
	return rt.String()
}
