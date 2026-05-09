//go:build llm_generated_opus47

// Vendored from github.com/dkorunic/betteralign@v0.8.0 (BSD-style license),
// itself a fork of golang.org/x/tools/go/analysis/passes/fieldalignment.
// Copyright 2020 The Go Authors. Modifications copyright 2022-2025 Dinko Korunic.
// Reused here to keep `AlignAndFormat` self-contained; betteralign's exported
// API is package-state-coupled and not suited to in-process use.

package golang

import (
	"go/types"
	"sort"
)

type gcSizes struct {
	WordSize int64
	MaxAlign int64
}

// optimalOrder returns a struct whose fields are reordered for minimal
// footprint and the permutation of original indices that produces it.
func optimalOrder(str *types.Struct, sizes *gcSizes) (*types.Struct, []int) {
	nf := str.NumFields()

	type elem struct {
		index   int
		alignof int64
		sizeof  int64
		ptrdata int64
	}

	elems := make([]elem, nf)
	for i := 0; i < nf; i++ {
		ft := str.Field(i).Type()
		elems[i] = elem{
			i,
			sizes.Alignof(ft),
			sizes.Sizeof(ft),
			sizes.ptrdata(ft),
		}
	}

	sort.SliceStable(elems, func(i, j int) bool {
		ei, ej := &elems[i], &elems[j]
		zeroi, zeroj := ei.sizeof == 0, ej.sizeof == 0
		if zeroi != zeroj {
			return zeroi
		}
		if ei.alignof != ej.alignof {
			return ei.alignof > ej.alignof
		}
		noptrsi, noptrsj := ei.ptrdata == 0, ej.ptrdata == 0
		if noptrsi != noptrsj {
			return noptrsj
		}
		if !noptrsi {
			traili := ei.sizeof - ei.ptrdata
			trailj := ej.sizeof - ej.ptrdata
			if traili != trailj {
				return traili < trailj
			}
		}
		if ei.sizeof != ej.sizeof {
			return ei.sizeof > ej.sizeof
		}
		return false
	})

	fields := make([]*types.Var, nf)
	indexes := make([]int, nf)
	for i, e := range elems {
		fields[i] = str.Field(e.index)
		indexes[i] = e.index
	}
	return types.NewStruct(fields, nil), indexes
}

var basicSizes = [...]byte{
	types.Bool:       1,
	types.Int8:       1,
	types.Int16:      2,
	types.Int32:      4,
	types.Int64:      8,
	types.Uint8:      1,
	types.Uint16:     2,
	types.Uint32:     4,
	types.Uint64:     8,
	types.Float32:    4,
	types.Float64:    8,
	types.Complex64:  8,
	types.Complex128: 16,
}

func (s *gcSizes) Alignof(T types.Type) int64 {
	switch t := T.Underlying().(type) {
	case *types.Array:
		return s.Alignof(t.Elem())
	case *types.Struct:
		m := int64(1)
		for i, nf := 0, t.NumFields(); i < nf; i++ {
			if a := s.Alignof(t.Field(i).Type()); a > m {
				m = a
			}
		}
		return m
	}
	a := s.Sizeof(T)
	if a < 1 {
		return 1
	}
	if a > s.MaxAlign {
		return s.MaxAlign
	}
	return a
}

func (s *gcSizes) Sizeof(T types.Type) int64 {
	switch t := T.Underlying().(type) {
	case *types.Basic:
		k := t.Kind()
		if int(k) < len(basicSizes) {
			if sz := basicSizes[k]; sz > 0 {
				return int64(sz)
			}
		}
		if k == types.String {
			return s.WordSize * 2
		}
	case *types.Array:
		return t.Len() * s.Sizeof(t.Elem())
	case *types.Slice:
		return s.WordSize * 3
	case *types.Struct:
		nf := t.NumFields()
		if nf == 0 {
			return 0
		}
		var o int64
		m := int64(1)
		for i := 0; i < nf; i++ {
			ft := t.Field(i).Type()
			a, sz := s.Alignof(ft), s.Sizeof(ft)
			if a > m {
				m = a
			}
			if i == nf-1 && sz == 0 && o != 0 {
				sz = 1
			}
			o = align(o, a) + sz
		}
		return align(o, m)
	case *types.Interface:
		return s.WordSize * 2
	}
	return s.WordSize
}

func (s *gcSizes) ptrdata(T types.Type) int64 {
	switch t := T.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.String, types.UnsafePointer:
			return s.WordSize
		}
		return 0
	case *types.Chan, *types.Map, *types.Pointer, *types.Signature, *types.Slice:
		return s.WordSize
	case *types.Interface:
		return 2 * s.WordSize
	case *types.Array:
		n := t.Len()
		if n == 0 {
			return 0
		}
		a := s.ptrdata(t.Elem())
		if a == 0 {
			return 0
		}
		z := s.Sizeof(t.Elem())
		return (n-1)*z + a
	case *types.Struct:
		nf := t.NumFields()
		if nf == 0 {
			return 0
		}
		var o, p int64
		for i := 0; i < nf; i++ {
			ft := t.Field(i).Type()
			a, sz := s.Alignof(ft), s.Sizeof(ft)
			fp := s.ptrdata(ft)
			o = align(o, a)
			if fp != 0 {
				p = o + fp
			}
			o += sz
		}
		return p
	}
	panic("impossible")
}

func align(x, a int64) int64 {
	y := x + a - 1
	return y - y%a
}
