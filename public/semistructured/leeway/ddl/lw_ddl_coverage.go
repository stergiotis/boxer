package ddl

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/sample"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

type CoverageResult struct {
	NotCovered                 []string
	CoverageTypeMachineNumeric float64
	CoverageTypeTemporal       float64
	CoverageTypeString         float64
	CoverageTypeTotal          float64
}

func MeasureTechCoverage(techSpecificGen common.TechnologySpecificGeneratorI) (coverage CoverageResult) {
	var nErrorsMachineNumber, nErrorsTemporal, nErrorsString uint64
	notCovered := make([]string, 0, 128)
	for i := uint64(0); i < sample.SampleMachineNumericMaxExcl; i++ {
		canonicalType := sample.GenerateSampleMachineNumericType(i)
		err := techSpecificGen.GenerateType(canonicalType)
		if err != nil {
			nErrorsMachineNumber++
			notCovered = append(notCovered, canonicalType.String())
		}
	}
	for i := uint64(0); i < sample.SampleTemporalTypeMaxExcl; i++ {
		canonicalType := sample.GenerateSampleTemporalType(i)
		err := techSpecificGen.GenerateType(canonicalType)
		if err != nil {
			nErrorsTemporal++
			notCovered = append(notCovered, canonicalType.String())
		}
	}
	for i := uint64(0); i < sample.SampleStringTypeMaxExcl; i++ {
		canonicalType := sample.GenerateSampleStringType(i)
		err := techSpecificGen.GenerateType(canonicalType)
		if err != nil {
			nErrorsString++
			notCovered = append(notCovered, canonicalType.String())
		}
	}
	coverage.CoverageTypeTotal = 1.0 - float64(nErrorsMachineNumber+nErrorsTemporal+nErrorsString)/float64(sample.SampleMachineNumericMaxExcl+sample.SampleTemporalTypeMaxExcl+sample.SampleStringTypeMaxExcl)
	coverage.CoverageTypeMachineNumeric = 1.0 - float64(nErrorsMachineNumber)/float64(sample.SampleMachineNumericMaxExcl)
	coverage.CoverageTypeTemporal = 1.0 - float64(nErrorsTemporal)/float64(sample.SampleTemporalTypeMaxExcl)
	coverage.CoverageTypeString = 1.0 - float64(nErrorsString)/float64(sample.SampleStringTypeMaxExcl)
	coverage.NotCovered = notCovered

	return
}
