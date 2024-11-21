package cbor

import (
	"fmt"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

type TokenizerState int

const TokenizerStateConsumeSingle TokenizerState = 0

const TokenizerStateConsumeArray TokenizerState = 1

const TokenizerStateConsumeMap TokenizerState = 2

const TokenizerStateConsumeIndefByteChunks TokenizerState = 3

const TokenizerStateConsumeIndefUtf8StringChunks TokenizerState = 4

const TokenizerStateConsumeIndefArray TokenizerState = 5

const TokenizerStateConsumeIndefMapKey TokenizerState = 6

const TokenizerStateConsumeIndefMapValue TokenizerState = 7

type Tokenizer struct {
	states *containers.Stack[TokenizerState]
	vals   *containers.Stack[uint64]
	reader io.ByteReader
	buf    []byte
}

func NewTokenizer(rd io.ByteReader) *Tokenizer {
	states := containers.NewStack[TokenizerState]()
	vals := containers.NewStack[uint64]()
	tk := &Tokenizer{
		states: states,
		vals:   vals,
		buf:    make([]byte, 0, 8),
	}
	if rd != nil {
		tk.Reset(rd)
	}
	return tk
}

func (inst *Tokenizer) Reset(rd io.ByteReader) {
	inst.buf = inst.buf[:0]
	inst.states.Reset()
	inst.vals.Reset()

	inst.states.Push(TokenizerStateConsumeSingle)
	inst.vals.Push(1)
	inst.reader = rd
}

type TokenE uint8

const TokenFinished TokenE = 0

const TokenIndefArrayStart TokenE = 1

const TokenIndefArrayStop TokenE = 2

const TokenIndefMapStart TokenE = 3

const TokenIndefMapStop TokenE = 4

const TokenIndefByteStringStop TokenE = 5

const TokenIndefByteStringStart TokenE = 6

const TokenIndefUtf8StringStart TokenE = 7

const TokenIndefUtf8StringStop TokenE = 8

const TokenArray TokenE = 9

const TokenMap TokenE = 10

const TokenTaggedValue TokenE = 11

const TokenByteString TokenE = 12

const TokenUTF8String TokenE = 13

const TokenUInt8 TokenE = 14

const TokenUInt16 TokenE = 15

const TokenUInt32 TokenE = 16

const TokenUInt64 TokenE = 17

const TokenInt8 TokenE = 18

const TokenInt16 TokenE = 19

const TokenInt32 TokenE = 20

const TokenInt64 TokenE = 21

const TokenUnassignedSimpleValue TokenE = 22

const TokenFalse TokenE = 23

const TokenTrue TokenE = 24

const TokenNull TokenE = 25

const TokenUndefined TokenE = 26

const TokenReservedSimpleValue TokenE = 27

const TokenFloat16 TokenE = 28

const TokenFloat32 TokenE = 29

const TokenFloat64 TokenE = 30

var TokenToString = [31]string{
	"finished",
	"indefArrayStart",
	"indefArrayStop",
	"indefMapStart",
	"indefMapStop",
	"indefByteStringStop",
	"indefByteStringStart",
	"indefUtf8StringStart",
	"indefUtf8StringStop",
	"array",
	"map",
	"taggedValue",
	"byteString",
	"utf8String",
	"uint8",
	"uint16",
	"uint32",
	"uint64",
	"int8",
	"int16",
	"int32",
	"int64",
	"unassignedSimpleValue",
	"false",
	"true",
	"null",
	"undefined",
	"reservedSimpleValue",
	"float16",
	"float32",
	"float64",
}

func (inst TokenE) String() string {
	return TokenToString[inst]
}

var _ fmt.Stringer = TokenE(0)

func (inst *Tokenizer) processInitialBytesDefinite(additionalInfo uint8) (val uint64, bytesRead int, err error) {
	if additionalInfo < 24 {
		val = uint64(additionalInfo)
		return
	}
	switch additionalInfo {
	case 24:
		var b byte
		b, err = inst.read1()
		if err != nil {
			return
		}
		bytesRead = 1
		val = uint64(b)
		return
	case 25:
		val, bytesRead, err = inst.read2()
		return
	case 26:
		val, bytesRead, err = inst.read4()
		return
	case 27:
		val, bytesRead, err = inst.read8()
		return
	case 28, 29, 30:
		err = eh.Errorf("fail: reserved, not well formed in RFC8949")
		return
	default:
		err = eh.Errorf("fail: unhandled case additionalInfo = %d", additionalInfo)
	}
	return
}

func (inst *Tokenizer) processInitialBytesIndefinite(majorType uint8, additionalInfo uint8) (token TokenE, err error) {
	if additionalInfo != 31 {
		err = eh.Errorf("expecting additional information 31, got %d", additionalInfo)
		return
	}
	switch majorType {
	case 2:
		inst.states.Push(TokenizerStateConsumeIndefByteChunks)
		token = TokenIndefByteStringStart
		return
	case 3:
		inst.states.Push(TokenizerStateConsumeIndefUtf8StringChunks)
		token = TokenIndefUtf8StringStart
		return
	case 4:
		inst.states.Push(TokenizerStateConsumeIndefArray)
		token = TokenIndefArrayStart
		return
	case 5:
		inst.states.Push(TokenizerStateConsumeIndefMapKey)
		token = TokenIndefMapStart
		return
	case 7:
		_, err = inst.states.Pop()
		if err != nil {
			err = eh.Errorf("unexpected break: state stack is empty")
			return
		}
		return
	default:
		err = eh.Errorf("fail: wrong majorType %d, not applicable in state %d", majorType, int(inst.states.PeekDefault(TokenizerStateConsumeSingle)))
		return
	}
}

func (inst *Tokenizer) handleDataItemMajorType(majorType byte, additionalInfo byte, val uint64) (token TokenE, retr uint64, err error) {
	switch majorType {
	case 0:
		// uintx
		retr = val
		if additionalInfo < 24 {
			token = TokenUInt8
		} else {
			token = []TokenE{TokenUInt8, TokenUInt16, TokenUInt32, TokenUInt64}[additionalInfo-24]
		}
		return
	case 1:
		// intx
		retr = val
		if additionalInfo < 24 {
			token = TokenInt8
		} else {
			token = []TokenE{TokenInt8, TokenInt16, TokenInt32, TokenInt64}[additionalInfo-24]
		}
		return
	case 2:
		// bytes
		retr = val
		token = TokenByteString
		return
	case 3:
		// utf-8 string
		retr = val
		token = TokenUTF8String
		return
	case 4:
		// array of length val
		inst.vals.Push(val)
		retr = val
		if val > 0 {
			inst.states.Push(TokenizerStateConsumeArray)
		}
		token = TokenArray
		return
	case 5:
		// map of length val
		inst.vals.Push(2 * val)
		retr = val
		if val > 0 {
			inst.states.Push(TokenizerStateConsumeMap)
		}
		token = TokenMap
		return
	case 6:
		// tag
		inst.vals.Push(val)
		retr = val
		inst.states.Push(TokenizerStateConsumeSingle)
		token = TokenTaggedValue
	case 7:
		if additionalInfo >= 25 {
			switch additionalInfo {
			case 24:
				if val < 24 {
					err = eh.Errorf("fail: short simple encoded with 1 byte extension")
					return
				} else if 24 <= val && val <= 31 {
					token = TokenReservedSimpleValue
				} else {
					token = TokenUnassignedSimpleValue
				}
				retr = val
				return
			case 25:
				token = TokenFloat16
				retr = val
				return
			case 26:
				token = TokenFloat32
				retr = val
				return
			case 27:
				token = TokenFloat64
				retr = val
				return
			}
		} else if 20 <= additionalInfo && additionalInfo <= 23 {
			switch additionalInfo {
			case 20:
				token = TokenFalse
				return
			case 21:
				token = TokenTrue
				return
			case 22:
				token = TokenNull
				return
			case 23:
				token = TokenUndefined
				return
			}
		} else if additionalInfo <= 31 {
			token = TokenReservedSimpleValue
			retr = val
			return
		} else {
			token = TokenUnassignedSimpleValue
			retr = val
			return
		}
		return
	}
	return
}

func (inst *Tokenizer) Next() (token TokenE, bytesRead int, retr uint64, err error) {
	val := uint64(0)
	var state TokenizerState
	state, err = inst.states.Peek()
	if err != nil {
		err = eh.Errorf("unexpected state: states stack is missing an element")
		return
	}

	var ib byte
	ib, err = inst.read1()
	if err != nil {
		return
	}
	mt := ib >> 5
	ai := ib & 0x1f
	bytesRead = 1

	//log.Trace().Int("majorType", int(mt)).Int("additionalInfo", int(ai)).Msg("read token")

	switch state {
	case TokenizerStateConsumeIndefByteChunks, TokenizerStateConsumeIndefUtf8StringChunks:
		switch mt {
		case 2:
			if state != TokenizerStateConsumeIndefByteChunks {
				err = eh.Errorf("fail: expecting byte chunk in indefinite byte stream, got majorType %d", mt)
				return
			}
			token = TokenByteString
			break
		case 3:
			if state != TokenizerStateConsumeIndefUtf8StringChunks {
				err = eh.Errorf("fail: expecting string chunk in indefinite string stream, got majorType %d", mt)
				return
			}
			token = TokenUTF8String
			break
		case 7:
			if state == TokenizerStateConsumeIndefByteChunks {
				token = TokenIndefByteStringStop
			} else {
				token = TokenIndefUtf8StringStop
			}
			_, err = inst.states.Pop()
			if err != nil {
				err = eh.Errorf("unexpected state: %w", err)
				return
			}
			return
		default:
			if state == TokenizerStateConsumeIndefByteChunks {
				err = eh.Errorf("fail: expecting byte chunk in indefinite byte stream, got majorType %d", mt)
				return
			} else {
				err = eh.Errorf("fail: expecting string chunk in indefinite string stream, got majorType %d", mt)
				return
			}
		}
		var u int
		retr, u, err = inst.processInitialBytesDefinite(ai)
		bytesRead += u
		return
	case TokenizerStateConsumeIndefArray:
		if mt == 7 {
			token = TokenIndefArrayStop
			_, err = inst.states.Pop()
			if err != nil {
				err = eh.Errorf("unexpected state: %w", err)
				return
			}
			return
		}
		break
	case TokenizerStateConsumeIndefMapKey:
		_, err = inst.states.Pop()
		if err != nil {
			err = eh.Errorf("unexpected state: %w", err)
			return
		}
		if mt == 7 {
			token = TokenIndefMapStop
			return
		}
		inst.states.Push(TokenizerStateConsumeIndefMapValue)
		break
	case TokenizerStateConsumeIndefMapValue:
		_, err = inst.states.Pop()
		if err != nil {
			err = eh.Errorf("unexpected state: %w", err)
			return
		}
		inst.states.Push(TokenizerStateConsumeIndefMapKey)
		break
	case TokenizerStateConsumeArray, TokenizerStateConsumeMap:
		var v uint64
		v, err = inst.vals.Pop()
		if err != nil {
			err = eh.Errorf("invariance violation: states/vals stack do not have the same length")
			return
		}
		switch v {
		case 0, 1:
			_, err = inst.states.Pop()
			if err != nil {
				err = eh.Errorf("invariance violation: states/vals stack do not have the same length")
				return
			}
			break
		default:
			inst.vals.Push(v - 1)
		}
		break
	}

	if ai == 31 {
		// side effect: push state
		token, err = inst.processInitialBytesIndefinite(mt, ai)
		if err != nil {
			return
		}
	} else {
		// side effect: read
		var u int
		val, u, err = inst.processInitialBytesDefinite(ai)
		bytesRead += u
		if err != nil {
			return
		}
		token, retr, err = inst.handleDataItemMajorType(mt, ai, val)
		if err != nil {
			return
		}
	}
	return
}

func (inst *Tokenizer) read1() (b byte, err error) {
	r := inst.reader
	b, err = r.ReadByte()
	return
}

func (inst *Tokenizer) read2() (v uint64, bytesRead int, err error) {
	r := inst.reader
	var b0, b1 byte
	b0, err = r.ReadByte()
	if err != nil {
		return
	}
	bytesRead = 1
	b1, err = r.ReadByte()
	if err != nil {
		return
	}
	v = uint64(b0)<<8 | uint64(b1)
	bytesRead = 2
	return
}

func (inst *Tokenizer) read4() (v uint64, bytesRead int, err error) {
	r := inst.reader
	var b0, b1, b2, b3 byte
	b0, err = r.ReadByte()
	if err != nil {
		return
	}
	bytesRead = 1
	b1, err = r.ReadByte()
	if err != nil {
		return
	}
	bytesRead = 2
	b2, err = r.ReadByte()
	if err != nil {
		return
	}
	bytesRead = 3
	b3, err = r.ReadByte()
	if err != nil {
		return
	}
	bytesRead = 4
	v = uint64(b0)<<24 | uint64(b1)<<16 | uint64(b2)<<8 | uint64(b3)
	return
}

func (inst *Tokenizer) read8() (v uint64, bytesRead int, err error) {
	var v0, v1 uint64
	v0, bytesRead, err = inst.read4()
	if err != nil {
		return
	}
	var b int
	v1, b, err = inst.read4()
	bytesRead += b
	v = v0<<32 | v1
	return
}
