package marshallreflect

import (
	"reflect"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// PlanFor returns the mappingplan.Plan derived from T's struct tags.
// Cached per reflect.Type via sync.Map — call once per type per
// process is the same cost as building the plan once at codegen time.
func PlanFor[T any]() (plan *mappingplan.Plan, err error) {
	rt := reflect.TypeOf((*T)(nil)).Elem()
	plan, err = planForType(rt)
	return
}

var planCache sync.Map // map[reflect.Type]*planEntry

// resolvedPlan bundles a built Plan with its section grouping. Both are
// pure functions of the DTO type, so they are computed once per type and
// cached together: Marshal, Unmarshal, and RowComposer all read the
// shared groups instead of recomputing goplan.ComputeGroups per row
// / per call.
type resolvedPlan struct {
	plan   *mappingplan.Plan
	groups []goplan.SectionGroup
}

// planEntry wraps the (resolvedPlan, err) result in a sync.OnceValues so
// concurrent first-touch goroutines collapse onto a single buildPlan +
// ComputeGroups call instead of stampeding the reflection path.
type planEntry struct {
	once func() (*resolvedPlan, error)
}

func resolveForType(rt reflect.Type) (*resolvedPlan, error) {
	if cached, ok := planCache.Load(rt); ok {
		return cached.(*planEntry).once()
	}
	entry := &planEntry{
		once: sync.OnceValues(func() (*resolvedPlan, error) {
			plan, err := buildPlan(rt)
			if err != nil {
				return nil, err
			}
			return &resolvedPlan{plan: plan, groups: goplan.ComputeGroups(plan)}, nil
		}),
	}
	actual, _ := planCache.LoadOrStore(rt, entry)
	return actual.(*planEntry).once()
}

func planForType(rt reflect.Type) (plan *mappingplan.Plan, err error) {
	r, err := resolveForType(rt)
	if err != nil {
		return nil, err
	}
	return r.plan, nil
}

// buildPlan is the reflect front-end of the shared plan builder: it
// classifies each struct field's reflect.Type into a goplan.FieldShape
// and feeds it to goplan.PlanBuilder, which applies exactly the same
// per-field validation + assembly the codegen front-end (marshallgen.ParsePlan)
// uses. The result is a mappingplan.Plan the Marshal / Unmarshal helpers
// drive via the shared TaggedField vocabulary.
func buildPlan(rt reflect.Type) (plan *mappingplan.Plan, err error) {
	if rt.Kind() != reflect.Struct {
		err = eb.Build().Str("type", rt.String()).Errorf("DTO must be a struct type")
		return
	}

	b := goplan.NewPlanBuilder(rt.PkgPath()+"/"+rt.Name(), pkgLastSegment(rt.PkgPath()), rt.Name())

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		st := f.Tag

		// `_` blank-identifier — entity-level metadata + optional const
		// declarations; validated by the shared builder.
		if f.Name == "_" {
			if err = b.AddUnderscoreField(st.Get("kind"), st.Get("plain"), st.Get("lw")); err != nil {
				return
			}
			continue
		}

		lwTag := st.Get("lw")
		if lwTag == "" {
			err = eb.Build().Str("field", f.Name).Errorf("non-`_` field missing `lw:` tag")
			return
		}

		// An unexported tagged field cannot be read or set through reflection;
		// the plan would build and then mustCall would panic at marshal time
		// (review E-2). Reject at plan-build, like the AST front-end.
		if !f.IsExported() {
			err = eb.Build().Str("field", f.Name).Errorf("unexported field carries an `lw:` tag; tagged fields must be exported")
			return
		}

		// Dynamic-membership tuple field (ADR-0103): a slice of a named
		// plain struct — not one of the special struct types the classifier
		// owns (marshalltypes carrier, option.Option, time.Time). The
		// element struct's fields are classified individually and validated
		// by the shared builder.
		if isTupleSliceType(f.Type) {
			if err = addReflectTupleField(b, rt, f.Name, lwTag, f.Type.Elem()); err != nil {
				return
			}
			continue
		}

		var shape goplan.FieldShape
		shape, err = classifyReflectType(f.Type)
		if err != nil {
			err = eb.Build().Str("field", f.Name).Errorf("classify field type: %w", err)
			return
		}

		if err = b.AddField(f.Name, lwTag, shape); err != nil {
			return
		}
	}

	return b.Finish()
}

// isTupleSliceType reports whether ft is `[]S` for a named plain struct S
// — the shape selecting the dynamic-membership tuple interpretation. The
// struct types with dedicated classifier lanes (marshalltypes carriers,
// option.Option, time.Time) are excluded and keep their existing paths.
func isTupleSliceType(ft reflect.Type) bool {
	if ft.Kind() != reflect.Slice {
		return false
	}
	e := ft.Elem()
	if e.Kind() != reflect.Struct || e.Name() == "" {
		return false
	}
	switch e.PkgPath() {
	case marshalltypesPkgPath, optionPkgPath, "time":
		return false
	}
	return true
}

// addReflectTupleField walks the tuple element struct's fields, classifies
// each with the shared reflect classifier, and hands them to
// goplan.PlanBuilder.AddTupleSliceField — the single home of the tuple
// validation rules, shared with the go/ast front-end.
func addReflectTupleField(b *goplan.PlanBuilder, dto reflect.Type, goFieldName, lwTag string, elemType reflect.Type) (err error) {
	if elemType.PkgPath() != dto.PkgPath() {
		// Front-end parity: the go/ast front-end resolves the element struct
		// from the DTO's own file, so a foreign-package element type would be
		// reflect-only. Reject it here with the reason.
		err = eb.Build().Str("field", goFieldName).Str("elemType", elemType.String()).Errorf("tuple element struct must be declared in the DTO's package (the marshallgen front-end resolves it from the DTO's file)")
		return
	}
	elems := make([]goplan.TupleElem, 0, elemType.NumField())
	for j := 0; j < elemType.NumField(); j++ {
		ef := elemType.Field(j)
		if ef.Name == "_" {
			err = eb.Build().Str("field", goFieldName).Errorf("`_` fields are not supported inside a tuple element struct — entity metadata belongs on the DTO")
			return
		}
		lw := ef.Tag.Get("lw")
		if lw == "" {
			err = eb.Build().Str("field", goFieldName).Str("elemField", ef.Name).Errorf("tuple element field missing `lw:` tag")
			return
		}
		if !ef.IsExported() {
			err = eb.Build().Str("field", goFieldName).Str("elemField", ef.Name).Errorf("unexported tuple element field carries an `lw:` tag; tagged fields must be exported")
			return
		}
		var shape goplan.FieldShape
		shape, err = classifyReflectType(ef.Type)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Str("elemField", ef.Name).Errorf("classify tuple element field type: %w", err)
			return
		}
		elems = append(elems, goplan.TupleElem{GoFieldName: ef.Name, LWTag: lw, Shape: shape})
	}
	return b.AddTupleSliceField(goFieldName, lwTag, elemType.Name(), elems)
}

func pkgLastSegment(pkg string) string {
	if pkg == "" {
		return "main"
	}
	for i := len(pkg) - 1; i >= 0; i-- {
		if pkg[i] == '/' {
			return pkg[i+1:]
		}
	}
	return pkg
}
