package cbor

import (
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

type PullParser struct {
	containerLengthStack *containers.Stack[int64]
	tokenizer            *Tokenizer
	completed            bool
	bytesConsumed        int64
}

func NewPullParser(rd io.ByteReader) *PullParser {
	const typicalMaxNestingLevels = 128
	return &PullParser{
		containerLengthStack: containers.NewStackSize[int64](typicalMaxNestingLevels),
		tokenizer:            NewTokenizer(rd),
		completed:            false,
		bytesConsumed:        0,
	}
}

func (inst *PullParser) Reset(rd io.ByteReader) {
	inst.containerLengthStack.Reset()
	inst.tokenizer.Reset(rd)
	inst.completed = false
	inst.bytesConsumed = 0
}

func (inst *PullParser) ContainerLengthStack() []int64 {
	return inst.containerLengthStack.Items
}

func (inst *PullParser) BytesConsumed() int64 {
	return inst.bytesConsumed
}

func (inst *PullParser) ConsumeIndefiniteByteString(w io.Writer, r io.Reader) (segments int, err error) {
	return inst.consumeIndefiniteString(w, r, TokenIndefByteStringStop)
}

func (inst *PullParser) ConsumeIndefiniteUtf8String(w io.Writer, r io.Reader) (segments int, err error) {
	return inst.consumeIndefiniteString(w, r, TokenIndefUtf8StringStop)
}

func (inst *PullParser) consumeIndefiniteString(w io.Writer, r io.Reader, endToken TokenE) (segments int, err error) {
	token, _, bytesToConsumeBeforeNextCall, _, err := inst.Consume()
	if err != nil {
		err = eh.Errorf("unable to consume indefinite byte string: %w", err)
		return
	}
	for token != endToken {
		segments++
		//w.Grow(int(bytesToConsumeBeforeNextCall))
		rl := io.LimitReader(r, int64(bytesToConsumeBeforeNextCall))
		_, err = io.Copy(w, rl)
		if err != nil {
			err = eh.Errorf("unable to consume %d bytes from supplied reader: %w", bytesToConsumeBeforeNextCall, err)
			return
		}
		token, _, bytesToConsumeBeforeNextCall, _, err = inst.Consume()
	}
	return
}

func (inst *PullParser) ConsumeCollectTags() (tags []uint64, token TokenE, retr uint64, bytesToConsumeBeforeNextCall uint64, completedNestings uint32, err error) {
	token, retr, bytesToConsumeBeforeNextCall, completedNestings, err = inst.Consume()
	if token == TokenTaggedValue {
		tags = make([]uint64, 0, 2)
	}
	for token == TokenTaggedValue && err == nil {
		tags = append(tags, retr)
		token, retr, bytesToConsumeBeforeNextCall, completedNestings, err = inst.Consume()
	}
	return
}

func (inst *PullParser) ConsumeIgnoreTags() (token TokenE, retr uint64, bytesToConsumeBeforeNextCall uint64, completedNestings uint32, err error) {
	token, retr, bytesToConsumeBeforeNextCall, completedNestings, err = inst.Consume()
	for token == TokenTaggedValue && err == nil {
		token, retr, bytesToConsumeBeforeNextCall, completedNestings, err = inst.Consume()
	}
	return
}

// Consume Depth-first tree traversal in pre-order (root -> left -> right). bytesToConsumeBeforeNextCall are accounted as consumed in BytesConsumed()
func (inst *PullParser) Consume() (token TokenE, retr uint64, bytesToConsumeBeforeNextCall uint64, completedNestings uint32, err error) {
	if inst.completed {
		token = TokenFinished
		return
	}

	tokenizer := inst.tokenizer
	skips := inst.containerLengthStack

	var bytesRead int
	token, bytesRead, retr, err = tokenizer.Next()
	inst.bytesConsumed += int64(bytesRead)
	if err != nil {
		if err == io.EOF {
			return
		} else {
			err = eh.Errorf("error while tokenizing stream: %w", err)
			return
		}
	}
	consume := true
	switch token {
	case TokenByteString, TokenUTF8String:
		bytesToConsumeBeforeNextCall = retr
		inst.bytesConsumed += int64(bytesToConsumeBeforeNextCall)
		break
	}

	switch token {
	case TokenTaggedValue:
		skips.Push(1)
		consume = false
		break
	case TokenArray:
		if retr > 0 {
			skips.Push(int64(retr))
			consume = false
		}
		break
	case TokenMap:
		if retr > 0 {
			skips.Push(int64(2 * retr))
			consume = false
		}
		break
	case TokenIndefArrayStart, TokenIndefMapStart, TokenIndefByteStringStart, TokenIndefUtf8StringStart:
		skips.Push(int64(-1))
		consume = false
		break
	case TokenIndefArrayStop, TokenIndefMapStop, TokenIndefByteStringStop, TokenIndefUtf8StringStop:
		// consume container element
		_, err = skips.Pop()
		if err != nil {
			err = eh.Errorf("unexpected state: %w", err)
			return
		}
		completedNestings++
		break
	}
	if consume {
	checkSkips:
		// consume one element
		if skips.Depth() > 0 {
			var l int64
			l, err = skips.Pop()
			if err != nil {
				err = eh.Errorf("unexpected state: %w", err)
				return
			}
			l--
			if l == 0 {
				completedNestings++
				// completed consuming definite container
				if skips.Depth() == 0 {
					// consumed full cbor "document"
					inst.completed = true
					return
				} else {
					goto checkSkips
				}
			} else {
				skips.Push(l)
			}
		} else {
			inst.completed = true
		}
	}
	return
}
