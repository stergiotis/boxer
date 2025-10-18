package sample

import (
	"math/rand/v2"

	"github.com/rs/zerolog/log"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/statespace/mixedradix"
)

var SampleScalarModifier = []canonicaltypes2.ScalarModifierE{canonicaltypes2.ScalarModifierNone, canonicaltypes2.ScalarModifierHomogenousArray, canonicaltypes2.ScalarModifierSet}

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

var SampleMachineNumericTypeBaseType = []canonicaltypes2.BaseTypeMachineNumericE{canonicaltypes2.BaseTypeMachineNumericUnsigned, canonicaltypes2.BaseTypeMachineNumericSigned, canonicaltypes2.BaseTypeMachineNumericFloat}
var SampleMachineNumericTypeWidth = []canonicaltypes2.Width{8, 16, 32, 64}
var SampleMachineNumericTypeByteOrder = []canonicaltypes2.ByteOrderModifierE{canonicaltypes2.ByteOrderModifierNone, canonicaltypes2.ByteOrderModifierLittleEndian, canonicaltypes2.ByteOrderModifierBigEndian}
var sampleMachineNumericTypeRadixii = []uint64{uint64(len(SampleMachineNumericTypeBaseType)), uint64(len(SampleMachineNumericTypeWidth)), uint64(len(SampleMachineNumericTypeByteOrder)), uint64(len(SampleScalarModifier))}
var SampleMachineNumericMaxExcl = sliceProd(sampleMachineNumericTypeRadixii)

var SampleStringTypeBaseType = []canonicaltypes2.BaseTypeStringE{canonicaltypes2.BaseTypeStringBool, canonicaltypes2.BaseTypeStringBytes, canonicaltypes2.BaseTypeStringUtf8}
var SampleStringTypeWidthModifier = []canonicaltypes2.WidthModifierE{canonicaltypes2.WidthModifierNone, canonicaltypes2.WidthModifierFixed}
var SampleStringTypeWidth = []canonicaltypes2.Width{0, 128, 145, 192}
var sampleStringTypeRadixii = []uint64{uint64(len(SampleStringTypeBaseType)), uint64(len(SampleStringTypeWidthModifier)), uint64(len(SampleStringTypeWidth)), uint64(len(SampleScalarModifier))}
var SampleStringTypeMaxExcl = sliceProd(sampleStringTypeRadixii)

var SampleTemporalTypeBaseType = []canonicaltypes2.BaseTypeTemporalE{canonicaltypes2.BaseTypeTemporalUtcDatetime, canonicaltypes2.BaseTypeTemporalZonedDatetime, canonicaltypes2.BaseTypeTemporalZonedTime}
var SampleTemporalTypeWidth = []canonicaltypes2.Width{32, 64}
var sampleTemporalTypeRadixii = []uint64{uint64(len(SampleTemporalTypeBaseType)), uint64(len(SampleTemporalTypeWidth)), uint64(len(SampleScalarModifier))}
var SampleTemporalTypeMaxExcl = sliceProd(sampleTemporalTypeRadixii)

var sampleTypeU = []uint64{SampleMachineNumericMaxExcl, SampleStringTypeMaxExcl, SampleTemporalTypeMaxExcl}
var SampleTypeMaxExcl = sliceSum(sampleTypeU)

func GenerateSampleType(n uint64) (sample canonicaltypes2.PrimitiveAstNodeI) {
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
func GenerateSamplePrimitiveType(rnd *rand.Rand, accept func(ct canonicaltypes2.PrimitiveAstNodeI) (ok bool, msg string)) (sample canonicaltypes2.PrimitiveAstNodeI) {
	for {
		sample = GenerateSampleType(rnd.Uint64())
		if sample.IsValid() {
			if accept != nil {
				ok, _ := accept(sample)
				if ok {
					return
				}
			} else {
				return
			}
		}
	}
}
func GenerateSampleGroup(nMembers int, rnd *rand.Rand, accept func(ct canonicaltypes2.PrimitiveAstNodeI) (ok bool, msg string)) (sample canonicaltypes2.GroupAstNode) {
	if nMembers < 0 {
		log.Panic().Int("nMembers", nMembers).Msg("nMembers is negative")
		return
	}
	members := make([]canonicaltypes2.PrimitiveAstNodeI, 0, nMembers)
	for i := 0; i < nMembers; i++ {
		var ct canonicaltypes2.PrimitiveAstNodeI
		for {
			ct = GenerateSampleType(rnd.Uint64())
			if ct.IsValid() {
				if accept != nil {
					ok, _ := accept(ct)
					if ok {
						break
					}
				} else {
					break
				}
			}
		}
		members = append(members, ct)
	}
	return canonicaltypes2.NewGroupAstNode(members)
}

func GenerateSampleMachineNumericType(n uint64) (sample canonicaltypes2.MachineNumericTypeAstNode) {
	digits := mixedradix.ToDigits(sampleMachineNumericTypeRadixii, n)
	return canonicaltypes2.MachineNumericTypeAstNode{
		BaseType:          SampleMachineNumericTypeBaseType[digits[0]],
		Width:             SampleMachineNumericTypeWidth[digits[1]],
		ByteOrderModifier: SampleMachineNumericTypeByteOrder[digits[2]],
		ScalarModifier:    SampleScalarModifier[digits[3]],
	}
}
func GenerateSampleStringType(n uint64) (sample canonicaltypes2.StringAstNode) {
	digits := mixedradix.ToDigits(sampleStringTypeRadixii, n)
	sample = canonicaltypes2.StringAstNode{
		BaseType:       SampleStringTypeBaseType[digits[0]],
		WidthModifier:  SampleStringTypeWidthModifier[digits[1]],
		Width:          SampleStringTypeWidth[digits[2]],
		ScalarModifier: SampleScalarModifier[digits[3]],
	}
	return
}
func GenerateSampleTemporalType(n uint64) (sample canonicaltypes2.TemporalTypeAstNode) {
	digits := mixedradix.ToDigits(sampleTemporalTypeRadixii, n)
	return canonicaltypes2.TemporalTypeAstNode{
		BaseType:       SampleTemporalTypeBaseType[digits[0]],
		Width:          SampleTemporalTypeWidth[digits[1]],
		ScalarModifier: SampleScalarModifier[digits[2]],
	}
}
