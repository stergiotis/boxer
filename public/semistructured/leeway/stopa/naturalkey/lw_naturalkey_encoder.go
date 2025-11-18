package naturalkey

import (
	"bytes"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	cbor2 "github.com/fxamacker/cbor/v2"
	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)


func EncodeTaggedIdJson(id identifier.TaggedId) (r string) {
	return JsonSpecialValuePrefix + taggedIdJsonValuePrefix + strconv.FormatUint(uint64(id), 16)
}
func IsTaggedIdJson(s string) bool {
	return strings.HasPrefix(s, JsonSpecialValuePrefix)
}
func DecodeTaggedIdJson(s string) (id identifier.TaggedId) {
	a, b, ok := strings.Cut(s, JsonSpecialValuePrefix+taggedIdJsonValuePrefix)
	if a != "" || !ok {
		return 0
	}
	n, err := strconv.ParseUint(b, 16, 64)
	if err != nil {
		return 0
	}
	id = identifier.TaggedId(n)
	return
}

func NewEncoder() *Encoder {
	bufCbor := bytes.NewBuffer(make([]byte, 0, 4096))
	bufJson := bytes.NewBuffer(make([]byte, 0, 4096))
	enc := cbor.NewEncoder(bufCbor, nil)
	jsonEnc := jsontext.NewEncoder(bufJson,
		jsontext.AllowInvalidUTF8(false),
		jsontext.CanonicalizeRawInts(true),
		jsontext.CanonicalizeRawFloats(true),
		jsontext.SpaceAfterColon(false),
		jsontext.SpaceAfterComma(false))
	return &Encoder{
		encCbor: enc,
		encJson: jsonEnc,
		bufCbor: bufCbor,
		bufJson: bufJson,
		errs:    make([]error, 0, 8),
		state:   encoderStateInitial,
	}
}
func (inst *Encoder) Reset() {
	inst.encCbor.Reset()
	inst.encJson.Reset(inst.bufJson)
	inst.bufCbor.Reset()
	inst.bufJson.Reset()
	clear(inst.errs)
	inst.errs = inst.errs[:0]
	inst.state = encoderStateInitial
}
func (inst *Encoder) handleErr(err error) *Encoder {
	if err != nil {
		inst.errs = append(inst.errs, err)
	}
	return inst
}

var ErrWrongState = eh.Errorf("wrong state")
var ErrInvalidArgument = eh.Errorf("invalid argument")

func (inst *Encoder) Begin() *Encoder {
	switch inst.state {
	case encoderStateEnded, encoderStateInitial:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}

	inst.Reset()
	_, err := inst.encCbor.EncodeArrayIndefinite()
	inst.handleErr(err)
	if err == nil {
		inst.state = encoderStateBegun
	}
	return inst
}
func (inst *Encoder) AddStr(v string) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeString(v)
	return inst.handleErr(err)
}
func (inst *Encoder) AddName(v naming.StylableName) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	if !v.IsValid() {
		return inst.handleErr(eb.Build().Stringer("v", v).Errorf("unable to add name: %w", ErrInvalidArgument))
	}
	_, err := inst.encCbor.EncodeString(v.Convert(naming.ShortestNamingStyle).String())
	return inst.handleErr(err)
}
func (inst *Encoder) AddKey(v naming.Key) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	if !v.IsValid() {
		return inst.handleErr(eb.Build().Stringer("v", v).Errorf("unable to add kex: %w", ErrInvalidArgument))
	}
	_, err := inst.encCbor.EncodeString(v.String())
	return inst.handleErr(err)
}
func (inst *Encoder) AddBool(v bool) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeBool(v)
	return inst.handleErr(err)
}
func (inst *Encoder) AddBytes(v []byte) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeByteSlice(v)
	return inst.handleErr(err)
}
func (inst *Encoder) AddTimeUTC(v time.Time) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeTimeUTC(v.UTC())
	return inst.handleErr(err)
}
func (inst *Encoder) AddUint8(v uint8) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeUint(uint64(v))
	return inst.handleErr(err)
}
func (inst *Encoder) AddUint16(v uint16) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeUint(uint64(v))
	return inst.handleErr(err)
}
func (inst *Encoder) AddUint32(v uint32) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeUint(uint64(v))
	return inst.handleErr(err)
}
func (inst *Encoder) AddId(v identifier.TaggedId) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeTag8(cbor.TagIdentifier)
	_ = inst.handleErr(err)
	_, err = inst.encCbor.EncodeUint(v.Value())
	return inst.handleErr(err)
}
func (inst *Encoder) AddUint64(v uint64) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeUint(v)
	return inst.handleErr(err)
}
func (inst *Encoder) AddInt8(v int8) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeInt(int64(v))
	return inst.handleErr(err)
}
func (inst *Encoder) AddInt16(v int16) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeInt(int64(v))
	return inst.handleErr(err)
}
func (inst *Encoder) AddInt32(v int32) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeInt(int64(v))
	return inst.handleErr(err)
}
func (inst *Encoder) AddInt64(v int64) *Encoder {
	switch inst.state {
	case encoderStateBegun:
		break
	default:
		inst.handleErr(ErrWrongState)
		return inst
	}
	_, err := inst.encCbor.EncodeInt(v)
	return inst.handleErr(err)
}
func (inst *Encoder) checkErrors() (err error) {
	if len(inst.errs) > 0 {
		err = errors.Join(inst.errs...)
	}
	return
}
func (inst *Encoder) end() {
	if inst.state == encoderStateBegun {
		_, err := inst.encCbor.EncodeBreak()
		inst.handleErr(err)
	}
}
func (inst *Encoder) EndAndResolve(resolver ResolverI, format SerializationFormatE) (id identifier.TaggedId, err error) {
	switch inst.state {
	case encoderStateBegun, encoderStateEnded:
		break
	default:
		err = ErrWrongState
		return
	}
	inst.end()
	err = inst.checkErrors()
	if err != nil {
		return
	}
	var key []byte
	key, err = inst.getSerialized(format, false)
	if err != nil {
		err = eh.Errorf("unable to serialize key: %w", err)
		return
	}
	inst.state = encoderStateEnded
	return resolver.ResolveNaturalKey(key)
}
func (inst *Encoder) EndAndGenerate(idGen identifier.IdGeneratorI, format SerializationFormatE) (id identifier.TaggedId, fresh bool, err error) {
	switch inst.state {
	case encoderStateBegun, encoderStateEnded:
		break
	default:
		err = ErrWrongState
		return
	}
	inst.end()
	err = inst.checkErrors()
	if err != nil {
		return
	}
	var key []byte
	key, err = inst.getSerialized(format, false)
	if err != nil {
		err = eh.Errorf("unable to serialize key: %w", err)
		return
	}
	inst.state = encoderStateEnded
	return idGen.GetId(key)
}
func (inst *Encoder) EndAndGenerate2(idGen identifier.IdGeneratorI, format SerializationFormatE) (id identifier.TaggedId, key []byte, fresh bool, err error) {
	switch inst.state {
	case encoderStateBegun, encoderStateEnded:
		break
	default:
		err = ErrWrongState
		return
	}
	inst.end()
	err = inst.checkErrors()
	if err != nil {
		return
	}
	key, err = inst.getSerialized(format, true)
	if err != nil {
		err = eh.Errorf("unable to serialize key: %w", err)
		return
	}
	inst.state = encoderStateEnded
	id, fresh, err = idGen.GetId(key)
	if err != nil {
		err = eh.Errorf("unable to generate id: %w", err)
		return
	}
	return
}
func (inst *Encoder) End(format SerializationFormatE) (naturalKey []byte, err error) {
	switch inst.state {
	case encoderStateBegun, encoderStateEnded:
		break
	default:
		err = ErrWrongState
		return
	}
	inst.end()
	err = inst.checkErrors()
	if err != nil {
		return
	}
	naturalKey, err = inst.getSerialized(format, true)
	if err != nil {
		err = eh.Errorf("unable to serialize key: %w", err)
		return
	}
	inst.state = encoderStateEnded
	return
}
func (inst *Encoder) getSerialized(format SerializationFormatE, copy bool) (r []byte, err error) {
	switch format {
	case SerializationFormatCbor:
		return inst.getSerializedCbor(copy)
	case SerializationFormatJson:
		return inst.getSerializedJson(copy)
	default:
		err = eh.Errorf("unhandled format")
	}
	return
}
func (inst *Encoder) getSerializedCbor(copy bool) (r []byte, err error) {
	r = inst.bufCbor.Bytes()
	if copy {
		r = slices.Clone(r)
	}
	return
}
func (inst *Encoder) getSerializedJson(copy bool) (r []byte, err error) {
	// TODO encode on the fly...
	var vs []any
	var b []byte
	b, err = inst.getSerializedCbor(false)
	if err != nil {
		err = eh.Errorf("unable to get cbor data: %w", err)
		return
	}
	err = cbor2.Unmarshal(b, &vs)
	if err != nil {
		err = eh.Errorf("unable to unmarshall cbor: %w", err)
		return
	}
	for i, v := range vs {
		switch vt := v.(type) {
		case cbor2.Tag:
			switch vt.Number {
			case uint64(cbor.TagIdentifier):
				var n uint64
				switch ct := vt.Content.(type) {
				case uint8:
					n = uint64(ct)
					break
				case uint16:
					n = uint64(ct)
					break
				case uint32:
					n = uint64(ct)
					break
				case uint64:
					n = ct
					break
				default:
					err = eb.Build().Uint64("tagNumber", vt.Number).Type("valueType", vt.Content).Errorf("encountered unhandled cbor tag value")
					return
				}
				id := identifier.TaggedId(n)
				if !id.IsValid() {
					err = eb.Build().Uint64("rawValue", n).Errorf("found invalid tagged id in cbor stream")
					return
				}
				vs[i] = EncodeTaggedIdJson(id)
				break
			default:
				err = eb.Build().Uint64("tagNumber", vt.Number).Type("valueType", vt.Content).Errorf("encountered unhandled cbor tag")
				return
			}
			break
		}
	}

	buf2 := inst.bufJson
	buf2.Reset()
	err = json.MarshalEncode(inst.encJson,
		&vs,
		json.Deterministic(true),
		json.StringifyNumbers(false))
	if err != nil {
		err = eh.Errorf("unable to serialize to json: %w", err)
		return
	}
	r = buf2.Bytes()
	r = bytes.TrimRight(r, "\n")
	if copy {
		r = slices.Clone(r)
	}
	return
}
