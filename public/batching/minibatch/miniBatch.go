package minibatch

import (
	"bytes"
	"errors"
	"io"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type MessageValidationFunc func(msg []byte) error

type MiniBatcher struct {
	startTime             time.Time
	messageValidationFunc MessageValidationFunc

	buf               *bytes.Buffer
	sizeCriteria      int
	durationCriteria  time.Duration
	countCriteria     int
	numberOfMessages  int
	lastMessageLength int

	numberOfEmitsDueToSize     int
	numberOfEmitsDueToDuration int
	numberOfEmitsDueToCount    int
	inMessage                  bool

	needsEmit bool
}

var ErrWrongNestingState = errors.New("unable to execute operation: Nesting of messages detected")

var ErrNeedsEmit = errors.New("unable to execute operation: Emit of mini batch needed")

var _ io.Writer = (*MiniBatcher)(nil)

var _ io.ByteWriter = (*MiniBatcher)(nil)

var _ io.StringWriter = (*MiniBatcher)(nil)

var _ io.ReaderFrom = (*MiniBatcher)(nil)

func NewMiniBatcher(sizeCriteria int, countCriteria int, durationCriteria time.Duration, messageValidationFunc MessageValidationFunc) (*MiniBatcher, error) {
	if sizeCriteria < 0 {
		sizeCriteria = 0
	}
	if countCriteria < 0 {
		countCriteria = 0
	}
	estMaxBatchSize := sizeCriteria
	if estMaxBatchSize == 0 {
		estMaxBatchSize = 1 * 1024 * 1024
	}
	return &MiniBatcher{
		sizeCriteria:          sizeCriteria,
		durationCriteria:      durationCriteria,
		countCriteria:         countCriteria,
		messageValidationFunc: messageValidationFunc,
		buf:                   bytes.NewBuffer(make([]byte, 0, estMaxBatchSize)),
		numberOfMessages:      0,
		startTime:             time.Now(),
		inMessage:             false,
		lastMessageLength:     0,
		needsEmit:             false,

		numberOfEmitsDueToSize:     0,
		numberOfEmitsDueToDuration: 0,
		numberOfEmitsDueToCount:    0,
	}, nil
}

func (inst *MiniBatcher) SetMessageValidationFunc(f MessageValidationFunc) (previouslySetFunc MessageValidationFunc) {
	previouslySetFunc = inst.messageValidationFunc
	inst.messageValidationFunc = f
	return
}

func (inst *MiniBatcher) ResetStats() {
	inst.numberOfEmitsDueToSize = 0
	inst.numberOfEmitsDueToSize = 0
	inst.numberOfEmitsDueToCount = 0
}

func (inst *MiniBatcher) EmitCriteriaStats() (numberOfEmitsDueToSize int, numberOfEmitsDueToDuration int, numberOfEmitsDuetToCount int) {
	return inst.numberOfEmitsDueToSize, inst.numberOfEmitsDueToDuration, inst.numberOfEmitsDueToCount
}

func (inst *MiniBatcher) NeedsEmit() bool {
	return inst.needsEmit
}

func (inst *MiniBatcher) MaxBatchSizeSoFar() int {
	return inst.buf.Cap()
}

func (inst *MiniBatcher) ResetTimer() {
	inst.startTime = time.Now()
}

func (inst *MiniBatcher) ForceEmit(w io.Writer) (n int, err error) {
	inst.needsEmit = true
	return inst.Emit(w)
}

// Emit a no-op when NeedsEmit() is false
func (inst *MiniBatcher) Emit(w io.Writer) (n int, err error) {
	if !inst.needsEmit {
		return 0, nil
	}

	var written int64
	written, err = io.Copy(w, inst.buf)
	n = int(written)

	inst.needsEmit = false
	inst.numberOfMessages = 0
	inst.ResetTimer()
	return
}

func (inst *MiniBatcher) BeginMessage() error {
	if inst.inMessage {
		return ErrWrongNestingState
	}
	if inst.needsEmit {
		return ErrNeedsEmit
	}
	inst.inMessage = true
	inst.lastMessageLength = 0
	return nil
}

func (inst *MiniBatcher) EndMessage() error {
	if !inst.inMessage {
		return ErrWrongNestingState
	}
	inst.inMessage = false
	inst.numberOfMessages++

	buf := inst.buf.Bytes()
	if inst.messageValidationFunc != nil {
		verr := inst.messageValidationFunc(buf[len(buf)-inst.lastMessageLength:])
		inst.lastMessageLength = 0
		if verr != nil {
			inst.buf.Truncate(len(buf) - inst.lastMessageLength)
			return eh.Errorf("unable to accept message: validation error: %w", verr)
		}
	}

	needsEmit := inst.checkCriteria()
	if needsEmit {
		inst.needsEmit = true
	}

	return nil
}

func (inst *MiniBatcher) checkCriteria() (needsEmit bool) {
	c := inst.numberOfMessages >= inst.countCriteria
	if c {
		inst.numberOfEmitsDueToCount++
	}
	d := time.Now().Sub(inst.startTime) >= inst.durationCriteria
	if d {
		inst.numberOfEmitsDueToDuration++
	}
	s := inst.buf.Len() >= inst.sizeCriteria
	if s {
		inst.numberOfEmitsDueToSize++
	}
	needsEmit = c || d || s
	return
}

func (inst *MiniBatcher) WriteByte(c byte) (err error) {
	if inst.needsEmit {
		return ErrNeedsEmit
	}
	err = inst.buf.WriteByte(c)
	if err != nil {
		return err
	}
	inst.lastMessageLength++
	return
}

func (inst *MiniBatcher) Write(p []byte) (n int, err error) {
	if inst.needsEmit {
		return 0, ErrNeedsEmit
	}
	n, err = inst.buf.Write(p)
	inst.lastMessageLength += n
	return
}

func (inst *MiniBatcher) WriteString(s string) (n int, err error) {
	if inst.needsEmit {
		return 0, ErrNeedsEmit
	}
	n, err = inst.buf.WriteString(s)
	inst.lastMessageLength += n
	return
}

func (inst *MiniBatcher) ReadFrom(r io.Reader) (n int64, err error) {
	if inst.needsEmit {
		return 0, ErrNeedsEmit
	}
	n, err = inst.ReadFrom(r)
	inst.lastMessageLength += int(n)
	return
}
