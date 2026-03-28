package sample

import (
	"testing"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stretchr/testify/require"
	"github.com/yassinebenaid/godump"
)

func TestGenerateStringType(t *testing.T) {
	h := containers.NewHashSet[string](128)
	for i := uint64(0); i < SampleStringTypeMaxExcl; i++ {
		ct := GenerateSampleStringType(i)
		if ct.IsValid() {
			s := ct.String()
			require.False(t, h.Has(s), s)
			h.Add(s)
		}
	}
}
func TestGenerateNumericType(t *testing.T) {
	h := containers.NewHashSet[string](128)
	for i := uint64(0); i < SampleMachineNumericMaxExcl; i++ {
		ct := GenerateSampleMachineNumericType(i)
		if ct.IsValid() {
			s := ct.String()
			require.False(t, h.Has(s), s)
			h.Add(s)
		}
	}
}
func TestGenerateTemporalType(t *testing.T) {
	h := containers.NewHashSet[string](128)
	for i := uint64(0); i < SampleTemporalTypeMaxExcl; i++ {
		ct := GenerateSampleTemporalType(i)
		if ct.IsValid() {
			s := ct.String()
			require.False(t, h.Has(s), s)
			h.Add(s)
		}
	}
}
func TestGenerateSampleType(t *testing.T) {
	m := make(map[string]struct{}, SampleTypeMaxExcl)
	ks := make([]string, 0, SampleTypeMaxExcl)
	dumper := godump.Dumper{
		Indentation:             "",
		ShowPrimitiveNamedTypes: false,
		HidePrivateFields:       false,
		Theme:                   godump.Theme{},
	}

	for n := uint64(0); n < SampleTypeMaxExcl; n++ {
		typ := GenerateSampleType(n)
		if !typ.IsValid() {
			continue
		}
		s := typ.String()
		_, has := m[s]
		require.False(t, has, "n=%d,typ=%s,typ=%s,keys=%q", n, dumper.Sprint(typ), typ.String(), ks)
		m[s] = struct{}{}
		ks = append(ks, s)
	}
}
