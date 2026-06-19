package marshallreflect

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// Validate reports whether dml satisfies the leeway DML write contract for
// T's Plan — the method set Marshal / RowComposer drive by reflection. It is a
// preflight: every missing or wrong-arity method is aggregated into a single
// error, so a mis-wired DML fails before the first row instead of panicking
// mid-marshal (mustCall). Pass the same pointer you would pass to Marshal.
//
// A nil return means Marshal will not panic on a missing method for any row of
// T. Validate checks method existence, the SetId arity (one arg, or two when
// the DTO declares a naturalKey), and the entity-header / per-section /
// per-attribute method names the Plan's shapes and channels exercise. It does
// NOT check argument value types — the strict-1:1 setters enforce those at call
// time — nor variadic arities (e.g. BeginAttribute(values ...T)).
//
// See the package doc's "DML write contract" for the method set.
func Validate[T any](dml any) (err error) {
	if dml == nil {
		return eb.Build().Errorf("dml is nil")
	}
	r, err := resolveForType(reflect.TypeFor[T]())
	if err != nil {
		return
	}
	var problems []string
	checkWriteContract(reflect.TypeOf(dml), r.plan, r.groups, &problems)
	if len(problems) == 0 {
		return nil
	}
	return eb.Build().Str("dml", reflect.TypeOf(dml).String()).Str("kind", r.plan.KindName).Errorf("dml does not satisfy the write contract: %s", strings.Join(problems, "; "))
}

// requireMethod records a problem if typ lacks a method named `name`. When
// wantArgs >= 0 and the method is not variadic it also checks the argument
// count (excluding the receiver). Returns the method's first result type (or
// nil) and whether the method exists, so callers can descend into the
// section / attribute types the contract returns.
func requireMethod(typ reflect.Type, ctx, name string, wantArgs int, problems *[]string) (ret reflect.Type, ok bool) {
	m, found := typ.MethodByName(name)
	if !found {
		*problems = append(*problems, ctx+": missing method "+name)
		return nil, false
	}
	if wantArgs >= 0 && !m.Type.IsVariadic() {
		got := m.Type.NumIn() - 1 // drop the receiver
		if got != wantArgs {
			*problems = append(*problems, fmt.Sprintf("%s: %s takes %d arg(s), want %d", ctx, name, got, wantArgs))
		}
	}
	if m.Type.NumOut() > 0 {
		ret = m.Type.Out(0)
	}
	return ret, true
}

// checkWriteContract walks the entity-header setters and every section group,
// mirroring exactly what marshalRow / marshalSection / marshalField /
// addMembership call so the preflight cannot diverge from the codec.
func checkWriteContract(dmlType reflect.Type, plan *mappingplan.Plan, groups []goplan.SectionGroup, problems *[]string) {
	requireMethod(dmlType, "dml", "BeginEntity", 0, problems)

	idArgs := 1
	if goplan.FindPlainCol(plan, "naturalKey") != nil {
		idArgs = 2 // SetId(id, naturalKey)
	}
	requireMethod(dmlType, "dml", "SetId", idArgs, problems)
	if goplan.FindPlainCol(plan, "ts") != nil {
		requireMethod(dmlType, "dml", "SetTimestamp", 1, problems)
	}
	if goplan.FindPlainCol(plan, "expiresAt") != nil {
		requireMethod(dmlType, "dml", "SetLifecycle", 1, problems)
	}
	requireMethod(dmlType, "dml", "CommitEntity", -1, problems)

	for _, g := range groups {
		secType, ok := requireMethod(dmlType, "dml", "GetSection"+mappingplan.UpperFirst(g.Section), 0, problems)
		if !ok || secType == nil {
			continue // can't descend; the missing getter is already reported
		}
		secCtx := "section " + g.Section
		requireMethod(secType, secCtx, "EndSection", -1, problems)
		checkSectionAttrContract(secType, secCtx, g, problems)
	}
}

// checkSectionAttrContract checks the per-section Begin* methods and the
// attribute-level methods (container append, the channel's AddMembership…P,
// EndAttributeP), derived from the same shape / channel classification the
// codec uses.
func checkSectionAttrContract(secType reflect.Type, ctx string, g goplan.SectionGroup, problems *[]string) {
	needBegin := map[string]bool{}
	needContainer := false
	if len(g.SubColumns) > 1 {
		needBegin["BeginAttribute"] = true // multi-sub-column: BeginAttribute(v1, …, vn)
	} else {
		for _, f := range g.SubColumns[0].Fields {
			switch goplan.ClassifyBegin(f) {
			case goplan.ShapeScalarBegin, goplan.ShapeExplodeBegin:
				needBegin["BeginAttribute"] = true
			case goplan.ShapeScalarBeginSingle, goplan.ShapeExplodeBeginSingle:
				needBegin["BeginAttributeSingle"] = true
			case goplan.ShapeContainer:
				needBegin["BeginAttribute"] = true
				needContainer = true
			}
		}
	}

	// Check the begin methods in a fixed order (deterministic errors) and take
	// the attribute type from the first one present.
	var attrType reflect.Type
	for _, name := range []string{"BeginAttribute", "BeginAttributeSingle"} {
		if !needBegin[name] {
			continue
		}
		if ret, ok := requireMethod(secType, ctx, name, -1, problems); ok && attrType == nil {
			attrType = ret
		}
	}
	if attrType == nil {
		return // begin method missing (already reported); cannot check attribute methods
	}
	attrCtx := ctx + " attribute"
	if needContainer {
		requireMethod(attrType, attrCtx, "AddToContainerP", -1, problems)
	}
	requireMethod(attrType, attrCtx, "EndAttributeP", -1, problems)
	requireMethod(attrType, attrCtx, "AddMembership"+g.Channel().AddMethodSuffix()+"P", -1, problems)
}
