package common

import (
	"fmt"
	"math/rand/v2"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	useaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

func generateExampleAspects(rnd *rand.Rand, nAspectsMax int) (r useaspects2.AspectSet) {
	asps := make([]useaspects2.AspectE, 0, nAspectsMax)
	for i := 0; i < nAspectsMax; i++ {
		asps = append(asps, useaspects2.AspectE(rnd.IntN(int(useaspects2.MaxAspectExcl))))
	}
	var err error
	r, err = useaspects2.EncodeAspects(asps...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to generate random aspect")
	}
	return
}
func generateExampleMembershipSpec(rnd *rand.Rand) (r MembershipSpecE) {
	u := rnd.Uint64()
	for i, m := range AllMembershipSpecs {
		if (u>>i)&0b1 != 0 {
			r = r | m
		}
	}
	return r
}
func GenerateSampleTableDesc(rnd *rand.Rand, acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string), acceptEncodingAspect func(asp encodingaspects2.AspectE) (ok bool, msg string)) (tbl TableDesc, err error) {
	var gen *TableManipulator
	gen, err = NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to initialize table manipulator: %w", err)
		return
	}
	err = PopulateManipulator(gen, rnd, acceptCanonicalType, acceptEncodingAspect)
	if err != nil {
		err = eh.Errorf("unable to populate manipulator: %w", err)
		return
	}
	tbl, err = gen.BuildTableDesc()
	return
}
func GenerateSampleTableDescDto(rnd *rand.Rand, acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string), acceptEncodingAspect func(asp encodingaspects2.AspectE) (ok bool, msg string)) (dto TableDescDto, err error) {
	var gen *TableManipulator
	gen, err = NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to initialize table manipulator: %w", err)
		return
	}
	err = PopulateManipulator(gen, rnd, acceptCanonicalType, acceptEncodingAspect)
	if err != nil {
		err = eh.Errorf("unable to populate manipulator: %w", err)
		return
	}
	gen.SetTableName("sample")
	dto, err = gen.BuildTableDescDto()
	return
}
func GenerateSampleEncodingAspectEx(nMembers int, r *rand.Rand, accept func(aspect encodingaspects2.AspectE) (ok bool, msg string)) (sample encodingaspects2.AspectSet) {
	if nMembers < 0 {
		log.Panic().Int("nMembers", nMembers).Msg("nMembers is negative")
		return
	}
	members := make([]encodingaspects2.AspectE, 0, nMembers)
	for i := 0; i < nMembers; i++ {
		var m encodingaspects2.AspectE
		for {
			m = encodingaspects2.AllAspects[r.IntN(len(encodingaspects2.AllAspects))]
			if accept == nil {
				break
			} else {
				ok, _ := accept(m)
				if ok {
					break
				}
			}
		}
		members = append(members, m)
	}
	return encodingaspects2.EncodeAspectsMustValidate(members...)
}
func GenerateSampleValueSemantics(nMembers int, rnd *rand.Rand) (valueSemantics valueaspects.AspectSet) {
	if nMembers < 0 {
		log.Panic().Int("nMembers", nMembers).Msg("nMembers is negative")
		return
	}
	members := make([]valueaspects.AspectE, 0, nMembers)
	for i := 0; i < nMembers; i++ {
		members = append(members, valueaspects.AllAspects[rnd.IntN(len(valueaspects.AllAspects))])
	}
	return valueaspects.EncodeAspectsMustValidate(members...)
}
func PopulateManipulator(manipulator *TableManipulator, rnd *rand.Rand, acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string), acceptEncodingAspect func(aspect encodingaspects2.AspectE) (ok bool, msg string)) (err error) {
	for _, t := range AllPlainItemTypes {
		if t == PlainItemTypeNone {
			continue
		}
		n := rnd.IntN(8)
		pfx := t.String()
		for i := 0; i < n; i++ {
			hints := GenerateSampleEncodingAspectEx(rnd.IntN(3)+1, rnd, acceptEncodingAspect)
			valueSemantics := GenerateSampleValueSemantics(rnd.IntN(3)+1, rnd)
			manipulator.AddPlainValueItem(t, naming.StylableName(fmt.Sprintf("%s%d", pfx, i)), sample.GenerateSamplePrimitiveType(rnd, acceptCanonicalType), hints, valueSemantics)
		}
	}

	sectionCount := rnd.IntN(4) + 1
	for i := 0; i < sectionCount; i++ {
		columnCount := rnd.IntN(8)
		for j := 0; j < columnCount; j++ {
			asp := generateExampleAspects(rnd, 4)
			mem := generateExampleMembershipSpec(rnd)
			hints := GenerateSampleEncodingAspectEx(rnd.IntN(2)+1, rnd, acceptEncodingAspect)
			valueSemantics := GenerateSampleValueSemantics(rnd.IntN(2)+1, rnd)
			manipulator.MergeTaggedValueColumn(naming.StylableName(fmt.Sprintf("section%d", i)),
				naming.StylableName(fmt.Sprintf("vm%d", j)),
				sample.GenerateSamplePrimitiveType(rnd, acceptCanonicalType),
				hints,
				valueSemantics,
				asp,
				mem, "", "")
		}
	}
	return
}
