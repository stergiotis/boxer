//go:build llm_generated_opus47

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
// shared groups instead of recomputing mappingplan.ComputeGroups per row
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
// classifies each struct field's reflect.Type into a mappingplan.FieldShape
// and feeds it to mappingplan.PlanBuilder, which applies exactly the same
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
