package common

import (
	"io"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
)

func NewTableMarshaller() (inst *TableMarshaller, err error) {
	var enc cbor.EncMode
	var dec cbor.DecMode
	enc, err = cbor.EncOptions{
		Sort:                 0,
		ShortestFloat:        0,
		NaNConvert:           0,
		InfConvert:           0,
		BigIntConvert:        0,
		Time:                 0,
		TimeTag:              0,
		IndefLength:          0,
		NilContainers:        0,
		TagsMd:               0,
		OmitEmpty:            0,
		String:               0,
		FieldName:            0,
		ByteSliceLaterFormat: 0,
		ByteArray:            0,
		BinaryMarshaler:      0,
	}.EncMode()
	if err != nil {
		err = eh.Errorf("unable to create cbor encoder")
		return
	}
	dec, err = cbor.DecOptions{
		DupMapKey:                0,
		TimeTag:                  0,
		MaxNestedLevels:          0,
		MaxArrayElements:         0,
		MaxMapPairs:              0,
		IndefLength:              0,
		TagsMd:                   0,
		IntDec:                   0,
		MapKeyByteString:         0,
		ExtraReturnErrors:        0,
		DefaultMapType:           nil,
		UTF8:                     0,
		FieldNameMatching:        0,
		BigIntDec:                0,
		DefaultByteStringType:    nil,
		ByteStringToString:       0,
		FieldNameByteString:      0,
		UnrecognizedTagToAny:     0,
		TimeTagToAny:             0,
		SimpleValues:             nil,
		NaN:                      0,
		Inf:                      0,
		ByteStringToTime:         0,
		ByteStringExpectedFormat: 0,
		BignumTag:                0,
		BinaryUnmarshaler:        0,
	}.DecMode()
	if err != nil {
		err = eh.Errorf("unable to create cbor encoder")
		return
	}
	inst = &TableMarshaller{
		enc: enc,
		dec: dec,
		dto: NewTableDescDto(),
	}
	return
}

func (inst *TableMarshaller) EncodeTableCbor(w io.Writer, table *TableDesc) (err error) {
	dto := inst.dto
	dto.Reset()
	err = table.LoadTo(dto)
	if err != nil {
		err = eh.Errorf("unable to load from table to dto object: %w", err)
		return
	}
	err = inst.enc.NewEncoder(w).Encode(dto)
	return
}
func (inst *TableMarshaller) EncodeDtoCbor(w io.Writer, dto *TableDescDto) (err error) {
	err = inst.enc.NewEncoder(w).Encode(dto)
	return
}
func (inst *TableMarshaller) DecodeTableCbor(r io.Reader, table *TableDesc) (err error) {
	dto := inst.dto
	dto.Reset()
	err = inst.DecodeDtoCbor(r, dto)
	if err != nil {
		err = eh.Errorf("unable to unmarshall into dto object: %w", err)
		return
	}
	table.Reset()
	err = table.LoadFrom(dto)
	return
}
func (inst *TableMarshaller) DecodeDtoCbor(r io.Reader, dto *TableDescDto) (err error) {
	dto.Reset()
	err = inst.dec.NewDecoder(r).Decode(dto)
	if err != nil {
		err = eh.Errorf("unable to unmarshall into dto object: %w", err)
		return
	}
	return
}
