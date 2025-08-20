package sample

import (
	"math/rand/v2"

	"github.com/rs/zerolog/log"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/statespace/mixedradix"
)

var SampleScalarModifier = []canonicalTypes2.ScalarModifierE{canonicalTypes2.ScalarModifierNone, canonicalTypes2.ScalarModifierHomogenousArray, canonicalTypes2.ScalarModifierSet}

func sliceProd(nums []uint64) (r uint64) {
	r = 1
	for _, n := range nums {
		r *= n
	}
	return
}
func sliceSum(nums []uint64) (r uint64) {
	for _, n := range nums {
		r += n
	}
	return
}

var SampleMachineNumericTypeBaseType = []canonicalTypes2.BaseTypeMachineNumericE{canonicalTypes2.BaseTypeMachineNumericUnsigned, canonicalTypes2.BaseTypeMachineNumericSigned, canonicalTypes2.BaseTypeMachineNumericFloat}
var SampleMachineNumericTypeWidth = []canonicalTypes2.Width{32, 64}
var SampleMachineNumericTypeByteOrder = []canonicalTypes2.ByteOrderModifierE{canonicalTypes2.ByteOrderModifierNone, canonicalTypes2.ByteOrderModifierLittleEndian, canonicalTypes2.ByteOrderModifierBigEndian}
var sampleMachineNumericTypeRadixii = []uint64{uint64(len(SampleMachineNumericTypeBaseType)), uint64(len(SampleMachineNumericTypeWidth)), uint64(len(SampleMachineNumericTypeByteOrder)), uint64(len(SampleScalarModifier))}
var SampleMachineNumericMaxExcl = sliceProd(sampleMachineNumericTypeRadixii)

var SampleStringTypeBaseType = []canonicalTypes2.BaseTypeStringE{canonicalTypes2.BaseTypeStringBool, canonicalTypes2.BaseTypeStringBytes, canonicalTypes2.BaseTypeStringUtf8}
var SampleStringTypeWidthModifier = []canonicalTypes2.WidthModifierE{canonicalTypes2.WidthModifierNone, canonicalTypes2.WidthModifierFixed}
var SampleStringTypeWidth = []canonicalTypes2.Width{0, 128, 145, 192}
var sampleStringTypeRadixii = []uint64{uint64(len(SampleStringTypeBaseType)), uint64(len(SampleStringTypeWidthModifier)), uint64(len(SampleStringTypeWidth)), uint64(len(SampleScalarModifier))}
var SampleStringTypeMaxExcl = sliceProd(sampleStringTypeRadixii)

var SampleTemporalTypeBaseType = []canonicalTypes2.BaseTypeTemporalE{canonicalTypes2.BaseTypeTemporalUtcDatetime, canonicalTypes2.BaseTypeTemporalZonedDatetime, canonicalTypes2.BaseTypeTemporalZonedTime}
var SampleTemporalTypeWidth = []canonicalTypes2.Width{32, 64}
var sampleTemporalTypeRadixii = []uint64{uint64(len(SampleTemporalTypeBaseType)), uint64(len(SampleTemporalTypeWidth)), uint64(len(SampleScalarModifier))}
var SampleTemporalTypeMaxExcl = sliceProd(sampleTemporalTypeRadixii)

var sampleTypeU = []uint64{SampleMachineNumericMaxExcl, SampleStringTypeMaxExcl, SampleTemporalTypeMaxExcl}
var SampleTypeMaxExcl = sliceSum(sampleTypeU)

func GenerateSampleType(n uint64) (sample canonicalTypes2.PrimitiveAstNodeI) {
	n = n % SampleTypeMaxExcl
	for i, u := range sampleTypeU {
		if n < u {
			switch i {
			case 0:
				return GenerateSampleMachineNumericType(n)
			case 1:
				return GenerateSampleStringType(n)
			case 2:
				return GenerateSampleTemporalType(n)
			}
		}
		n -= u
	}
	return
}
func GenerateSamplePrimitiveType(rnd *rand.Rand, accept func(ct canonicalTypes2.PrimitiveAstNodeI) (ok bool)) (sample canonicalTypes2.PrimitiveAstNodeI) {
	for {
		sample = GenerateSampleType(rnd.Uint64())
		if sample.IsValid() && (accept == nil || accept(sample)) {
			return
		}
	}
}
func GenerateSampleGroup(nMembers int, rnd *rand.Rand, accept func(ct canonicalTypes2.PrimitiveAstNodeI) (ok bool)) (sample canonicalTypes2.GroupAstNode) {
	if nMembers < 0 {
		log.Panic().Int("nMembers", nMembers).Msg("nMembers is negative")
		return
	}
	members := make([]canonicalTypes2.PrimitiveAstNodeI, 0, nMembers)
	for i := 0; i < nMembers; i++ {
		var ct canonicalTypes2.PrimitiveAstNodeI
		for {
			ct = GenerateSampleType(rnd.Uint64())
			if ct.IsValid() && (accept == nil || accept(ct)) {
				break
			}
		}
		members = append(members, ct)
	}
	return canonicalTypes2.NewGroupAstNode(members)
}

func GenerateSampleMachineNumericType(n uint64) (sample canonicalTypes2.MachineNumericTypeAstNode) {
	digits := mixedradix.ToDigits(sampleMachineNumericTypeRadixii, n)
	return canonicalTypes2.MachineNumericTypeAstNode{
		BaseType:          SampleMachineNumericTypeBaseType[digits[0]],
		Width:             SampleMachineNumericTypeWidth[digits[1]],
		ByteOrderModifier: SampleMachineNumericTypeByteOrder[digits[2]],
		ScalarModifier:    SampleScalarModifier[digits[3]],
	}
}
func GenerateSampleStringType(n uint64) (sample canonicalTypes2.StringAstNode) {
	digits := mixedradix.ToDigits(sampleStringTypeRadixii, n)
	sample = canonicalTypes2.StringAstNode{
		BaseType:       SampleStringTypeBaseType[digits[0]],
		WidthModifier:  SampleStringTypeWidthModifier[digits[1]],
		Width:          SampleStringTypeWidth[digits[2]],
		ScalarModifier: SampleScalarModifier[digits[3]],
	}
	return
}
func GenerateSampleTemporalType(n uint64) (sample canonicalTypes2.TemporalTypeAstNode) {
	digits := mixedradix.ToDigits(sampleTemporalTypeRadixii, n)
	return canonicalTypes2.TemporalTypeAstNode{
		BaseType:       SampleTemporalTypeBaseType[digits[0]],
		Width:          SampleTemporalTypeWidth[digits[1]],
		ScalarModifier: SampleScalarModifier[digits[2]],
	}
}
