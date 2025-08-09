package test

import (
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/sample"
)

func BenchmarkCanonicalTypes_Parse(b *testing.B) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	p := canonicalTypes.NewParser()
	for b.Loop() {
		ct := sample.GenerateSamplePrimitiveType(rnd, nil)
		ct2, err := p.ParsePrimitiveTypeAst(ct.String())
		if err != nil {
			b.Fail()
		}
		var _ = ct2
	}
}
