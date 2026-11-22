package recordstore

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SampleRowWriter writes rows a store really writes — its own components,
// under its own memberships — through exec, for a check that needs such
// rows and has no data to take them from: the round trip of an insert
// transform (ADR-0402 §SD6) is one. Name tells the writers apart.
type SampleRowWriter struct {
	Name  string
	Write func(ctx context.Context, exec ExecutorI) error
}

var sampleRows struct {
	mu sync.RWMutex
	m  map[string][]SampleRowWriter
}

// RegisterSampleRows registers w as a writer of sample rows for table. A
// package that holds a generated store over a table other stores share
// registers one from an init, so that whatever stores a binary links
// contribute their rows to a check over that table. It panics on a table
// reference that does not name its database, an empty name, a nil writer,
// or a second writer of one name for one table.
func RegisterSampleRows(table string, w SampleRowWriter) {
	key, qualified, ok := CanonicalTableRef(table)
	if !ok || !qualified {
		panic("recordstore.RegisterSampleRows: " + table + " is not a database-qualified table reference")
	}
	if w.Name == "" || w.Write == nil {
		panic("recordstore.RegisterSampleRows: a writer needs a name and a function")
	}
	sampleRows.mu.Lock()
	defer sampleRows.mu.Unlock()
	if slices.ContainsFunc(sampleRows.m[key], func(o SampleRowWriter) bool { return o.Name == w.Name }) {
		panic("recordstore.RegisterSampleRows: " + w.Name + " is already registered for " + key)
	}
	if sampleRows.m == nil {
		sampleRows.m = make(map[string][]SampleRowWriter, 1)
	}
	sampleRows.m[key] = append(sampleRows.m[key], w)
}

// SampleRowWriters are the writers registered for table, by name.
func SampleRowWriters(table string) (ws []SampleRowWriter) {
	key, _, ok := CanonicalTableRef(table)
	if !ok {
		return
	}
	sampleRows.mu.RLock()
	defer sampleRows.mu.RUnlock()
	ws = slices.Clone(sampleRows.m[key])
	slices.SortFunc(ws, func(a, b SampleRowWriter) int { return strings.Compare(a.Name, b.Name) })
	return
}

// SampleOrder is the order value the sample rows carry when a store orders
// by time.
var SampleOrder = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

var (
	contextType = reflect.TypeFor[context.Context]()
	errorType   = reflect.TypeFor[error]()
	timeType    = reflect.TypeFor[time.Time]()
)

// IngestSamples buffers one entity per component of a generated store and
// flushes: for every method Ingest<Component>(ctx, order, rows) the store
// has, one row whose fields [FillSample] set. base keeps the keys of this
// store's rows apart from another's; each component's row gets a key of its
// own above it. It returns the components it wrote.
//
// It finds the methods by their name and shape, which the generator emits
// for every component that binds the store's key. A store without one, or
// without Flush(ctx), is an error: there would be nothing to check.
func IngestSamples(ctx context.Context, store any, base uint64) (components []string, err error) {
	v := reflect.ValueOf(store)
	t := v.Type()
	flush, ok := t.MethodByName("Flush")
	if !ok || flush.Type.NumIn() != 2 || flush.Type.In(1) != contextType {
		err = eb.Build().Stringer("store", t).Errorf("recordstore: sample rows: the store has no Flush(ctx)")
		return
	}
	for i := range t.NumMethod() {
		m := t.Method(i)
		mt := m.Type
		if !strings.HasPrefix(m.Name, "Ingest") || mt.NumIn() != 4 || mt.In(1) != contextType || mt.In(3).Kind() != reflect.Slice ||
			mt.NumOut() != 1 || mt.Out(0) != errorType {
			continue
		}
		order := reflect.New(mt.In(2)).Elem()
		switch {
		case mt.In(2) == timeType:
			order.Set(reflect.ValueOf(SampleOrder))
		case order.CanUint():
			order.SetUint(base + uint64(len(components)) + 1)
		case order.CanInt():
			order.SetInt(int64(base) + int64(len(components)) + 1)
		default:
			err = eb.Build().Str("method", m.Name).Stringer("order", mt.In(2)).Errorf("recordstore: sample rows: no sample value for this order type")
			return
		}
		row := reflect.New(mt.In(3).Elem())
		FillSample(row.Interface(), base+uint64(len(components))+1)
		rows := reflect.Append(reflect.MakeSlice(mt.In(3), 0, 1), row.Elem())
		if out := v.Method(i).Call([]reflect.Value{reflect.ValueOf(ctx), order, rows}); !out[0].IsNil() {
			err = eb.Build().Str("method", m.Name).Errorf("recordstore: sample rows: %w", out[0].Interface().(error))
			return
		}
		components = append(components, strings.TrimPrefix(m.Name, "Ingest"))
	}
	if len(components) == 0 {
		err = eb.Build().Stringer("store", t).Errorf("recordstore: sample rows: the store has no Ingest<Component> method")
		return
	}
	out := v.MethodByName("Flush").Call([]reflect.Value{reflect.ValueOf(ctx)})
	if last := out[len(out)-1]; !last.IsNil() {
		err = eh.Errorf("recordstore: sample rows: flush: %w", last.Interface().(error))
	}
	return
}

// FillSample sets every exported field of the struct ptr points to, to a
// value that is not its zero value and that depends on n: text names its
// field, numbers are small and distinct per n, a slice holds one element,
// a time is [SampleOrder]. The values mean nothing; they are there so that
// every column a component writes holds something a check can lose.
func FillSample(ptr any, n uint64) {
	fillSample(reflect.ValueOf(ptr).Elem(), "", n)
}

func fillSample(v reflect.Value, name string, n uint64) {
	if !v.CanSet() {
		return
	}
	if v.Type() == timeType {
		v.Set(reflect.ValueOf(SampleOrder))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("sample-" + name)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Uint8, reflect.Uint16:
		v.SetUint(1 + n%100)
	case reflect.Uint, reflect.Uint32, reflect.Uint64:
		v.SetUint(n)
	case reflect.Int8, reflect.Int16:
		v.SetInt(1 + int64(n%100))
	case reflect.Int, reflect.Int32, reflect.Int64:
		v.SetInt(int64(n))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5 + float64(n%100))
	case reflect.Slice:
		el := reflect.New(v.Type().Elem()).Elem()
		fillSample(el, name, n)
		v.Set(reflect.Append(reflect.MakeSlice(v.Type(), 0, 1), el))
	case reflect.Array:
		for i := range v.Len() {
			fillSample(v.Index(i), name, n+uint64(i))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			fillSample(v.Field(i), v.Type().Field(i).Name, n)
		}
	case reflect.Pointer:
		el := reflect.New(v.Type().Elem())
		fillSample(el.Elem(), name, n)
		v.Set(el)
	}
}
