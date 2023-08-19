package cbor

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/stergiotis/boxer/public/containers"
	ea2 "github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
	"math"
	"strconv"
	"strings"
)

type Diagnostics struct {
	consumer *PullParser
}

func NewDiagnostics() *Diagnostics {
	return &Diagnostics{
		consumer: NewPullParser(nil),
	}
}

type containerMode uint8

const containerModeNil containerMode = 0

const containerModeArray containerMode = 1

const containerModeMapStart containerMode = 2

const containerModeMapKey containerMode = 3

const containerModeMapValue containerMode = 4

const containerModeByteString containerMode = 5

const containerModeUtf8String containerMode = 6

const containerModeTag containerMode = 7

func (inst *Diagnostics) ToString(cbor []byte) (string, error) {
	return inst.ToStringIndent(cbor, "")
}
func (inst *Diagnostics) ToStringIndent(cbor []byte, indentStr string) (string, error) {
	r, err := ea2.NewByteBlockReaderDiscardReader(bytes.NewReader(cbor))
	if err != nil {
		return "", eh.Errorf("unable to create reader: %w", err)
	}
	b := &strings.Builder{}
	err = inst.RunIndent(b, r, indentStr)
	// in case of error: return string built so far, it may contain valuable information
	return b.String(), err
}

func (inst *Diagnostics) Run(w io.StringWriter, r ea2.ByteBlockDiscardReader) error {
	return inst.RunIndent(w, r, "")
}
func (inst *Diagnostics) RunIndent(w io.StringWriter, r ea2.ByteBlockDiscardReader, indentStr string) error {
	c := inst.consumer
	c.Reset(r)
	braceStack := containers.NewStackSize[rune](32)
	containerModeStack := containers.NewStackSize[containerMode](32)

	token, retr, bytesToConsumeBeforeNextCall, completedNestings, err := c.Consume()
	endOfContainer := false
	var endL string
	if indentStr == "" {
		endL = " "
	} else {
		endL = "\n"
	}
	indentDefinite := func() {
		endL = endL + indentStr
		if indentStr != "" {
			_, _ = w.WriteString(endL)
		}
	}
	indentIndefinite := func() {
		endL = endL + indentStr
		if indentStr != "" {
			_, _ = w.WriteString(endL)
		} else {
			_, _ = w.WriteString(" ")
		}
	}
	unindent := func() {
		endL = strings.TrimSuffix(endL, indentStr)
		if indentStr != "" {
			_, _ = w.WriteString(endL)
		}
	}
	endLine := func() {
		_, _ = w.WriteString(endL)
	}
	for err == nil && token != TokenFinished {
		{ // insert , between elements, skip if multiple closing parens
			lastEndOfContainer := endOfContainer
			switch token {
			case TokenIndefArrayStop, TokenIndefMapStop:
				// indefinite container end
				endOfContainer = true
				break
			default:
				// definite container end
				endOfContainer = completedNestings > 0 && !(token == TokenArray && retr == 0)
			}
			if lastEndOfContainer && !endOfContainer {
				// last lap ended in end of container
				_, _ = w.WriteString(",")
				endLine()
			}
		}

		switch token {
		case TokenIndefArrayStart:
			_, _ = w.WriteString("[_")
			indentIndefinite()
			containerModeStack.Push(containerModeArray)
			braceStack.Push(']')
			break
		case TokenIndefArrayStop:
			break
		case TokenIndefMapStart:
			_, _ = w.WriteString("{_")
			indentIndefinite()
			braceStack.Push('}')
			containerModeStack.Push(containerModeMapStart)
			break
		case TokenIndefMapStop:
			break
		case TokenIndefByteStringStart:
			_, _ = w.WriteString("(_")
			indentIndefinite()
			braceStack.Push(')')
			containerModeStack.Push(containerModeByteString)
			break
		case TokenIndefUtf8StringStart:
			_, _ = w.WriteString("(_")
			indentIndefinite()
			braceStack.Push(')')
			containerModeStack.Push(containerModeUtf8String)
			break
		case TokenIndefByteStringStop, TokenIndefUtf8StringStop:
			unindent()
			break
		case TokenArray:
			if retr == 0 {
				_, _ = w.WriteString("[]")
				endOfContainer = true
			} else {
				containerModeStack.Push(containerModeArray)
				_, _ = w.WriteString("[")
				indentDefinite()
				braceStack.Push(']')
			}
			break
		case TokenMap:
			if retr == 0 {
				_, _ = w.WriteString("{}")
				endOfContainer = true
			} else {
				_, _ = w.WriteString("{")
				indentDefinite()
				containerModeStack.Push(containerModeMapStart)
				braceStack.Push('}')
			}
			break
		case TokenTaggedValue:
			_, _ = w.WriteString(fmt.Sprintf("%d(", retr))
			indentDefinite()
			containerModeStack.Push(containerModeTag)
			braceStack.Push(')')
			break
		case TokenByteString, TokenUTF8String:
			bytesToConsumeBeforeNextCall -= retr
			tmp := make([]byte, retr, retr)
			if retr > 0 {
				_, err = io.ReadFull(r, tmp)
				if err != nil {
					return eh.Errorf("unable to read token %s (%d bytes): %w", TokenToString[token], retr, err)
				}
			}
			if token == TokenUTF8String {
				var j []byte
				j, err = json.Marshal(string(tmp))
				if err != nil {
					l := len(tmp) * 2
					h := make([]byte, l, l)
					_ = hex.Encode(h, tmp)
					_, _ = w.WriteString("h'")
					_, _ = w.WriteString(string(h))
					_, _ = w.WriteString("'")
				} else {
					_, _ = w.WriteString(string(j))
				}
			} else {
				l := len(tmp) * 2
				h := make([]byte, l, l)
				_ = hex.Encode(h, tmp)
				_, _ = w.WriteString("h'")
				_, _ = w.WriteString(string(h))
				_, _ = w.WriteString("'")
			}
			break
		case TokenUInt8, TokenUInt16, TokenUInt32, TokenUInt64:
			_, _ = w.WriteString(fmt.Sprintf("%d", retr))
			break
		case TokenInt8, TokenInt16, TokenInt32, TokenInt64:
			valI := -1 - int64(retr)
			_, _ = w.WriteString(fmt.Sprintf("%d", valI))
			break
		case TokenFloat16:
			return eh.Errorf("not implemented: token=%s", TokenToString[token])
		case TokenFloat32:
			val := math.Float32frombits(uint32(retr))
			_, _ = w.WriteString(strconv.FormatFloat(float64(val), 'f', -1, 32))
			break
		case TokenFloat64:
			val := math.Float64frombits(retr)
			_, _ = w.WriteString(strconv.FormatFloat(val, 'f', -1, 32))
			break
		case TokenUnassignedSimpleValue:
			_, _ = w.WriteString(fmt.Sprintf("simple(%d)", retr))
			break
		case TokenFalse, TokenTrue, TokenNull, TokenUndefined:
			_, _ = w.WriteString(TokenToString[token])
			break
		case TokenReservedSimpleValue:
			_, _ = w.WriteString(fmt.Sprintf("reservedSimple(%d)", retr))
			break
		}
		if bytesToConsumeBeforeNextCall != 0 {
			return eh.Errorf("internal error: bytesToConsumeBeforeNextCall=%d, should be zero", bytesToConsumeBeforeNextCall)
		}

		//log.Debug().Bool("endOfContainer", endOfContainer).Uint32("completedNestings", completedNestings).Ints64("containerLengthStack", c.ContainerLengthStack()).Str("token", TokenToString[token])).Uint64("retr", retr).Msg("consume")
		tokenNext, retrNext, bytesToConsumeBeforeNextCallNext, completedNestingsNext, errNext := c.Consume()
		if completedNestings > 0 {
			for i := uint32(0); i < completedNestings; i++ {
				var br rune
				br, err = braceStack.Pop()
				if err != nil {
					return eh.Errorf("nesting levels and braces stack do not line up")
				}
				unindent()
				_, _ = w.WriteString(string(br))
				_, _ = containerModeStack.Pop()
			}
		}
		{
			switch containerModeStack.PeekDefault(containerModeNil) {
			case containerModeArray:
				firstElement := token == TokenArray || token == TokenIndefArrayStart
				if !firstElement && !endOfContainer && tokenNext != TokenIndefArrayStop {
					_, _ = w.WriteString(",")
					endLine()
				}
				break
			case containerModeMapStart:
				_, _ = containerModeStack.Swap(containerModeMapKey)
				break
			case containerModeMapKey:
				lastElement := completedNestings > 0 || tokenNext == TokenIndefMapStop
				if !lastElement && !endOfContainer && tokenNext != TokenIndefMapStop {
					_, _ = w.WriteString(": ")
				}
				_, _ = containerModeStack.Swap(containerModeMapValue)
				break
			case containerModeMapValue:
				lastElement := completedNestings > 0 || tokenNext == TokenIndefMapStop
				if !lastElement && !endOfContainer && tokenNext != TokenIndefMapStop {
					_, _ = w.WriteString(",")
					endLine()
				}
				_, _ = containerModeStack.Swap(containerModeMapKey)
				break
			case containerModeByteString:
				if token != TokenIndefByteStringStart && token != TokenIndefByteStringStop && tokenNext != TokenIndefByteStringStop {
					_, _ = w.WriteString(",")
					endLine()
				}
				break
			case containerModeUtf8String:
				if token != TokenIndefUtf8StringStart && token != TokenIndefUtf8StringStop && tokenNext != TokenIndefUtf8StringStop {
					_, _ = w.WriteString(",")
					endLine()
				}
				break
			case containerModeTag:
				if retr > uint64(MaxTagSmallIncl) && retr < 0xff {
					switch TagUint8(retr) {
					case TagEncodedCBORSequence:
						// TODO support indefinite byte strings
						if tokenNext != TokenByteString {
							err = eh.Errorf("unhandled cbor sequence (tag %d) token type %s", TagEncodedCBORSequence, TokenToString[tokenNext])
							return err
						}
						tokenNext, retrNext, bytesToConsumeBeforeNextCallNext, _, errNext = c.Consume()
					}
				}
				break
			}
		}
		token, retr, bytesToConsumeBeforeNextCall, completedNestings, err =
			tokenNext, retrNext, bytesToConsumeBeforeNextCallNext, completedNestingsNext, errNext
	}
	if err != nil {
		return err
	}
	for braceStack.Depth() > 0 {
		_, _ = w.WriteString(string(braceStack.PopDefault(' ')))
		unindent()
	}
	return nil
}
