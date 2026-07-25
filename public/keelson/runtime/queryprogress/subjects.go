package queryprogress

import (
	"encoding/json"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SubjectRoot is the subject family carrying inflight progress.
const SubjectRoot = "queryrun.progress"

// SubjectWildcard matches every run's progress subject. It is the pattern a
// subscriber that watches more than one run asks for, and the pattern the
// poller publishes under.
const SubjectWildcard = SubjectRoot + ".>"

// Subject returns the subject a run's ticks are published on. queryID must
// already have passed [ValidQueryID] — the poller only ever watches ids that
// have.
func Subject(queryID string) (subject string) {
	subject = SubjectRoot + "." + queryID
	return
}

// PublishFilter is the capability a poller needs. One process's poller
// publishes for every run it watches, so the grant is the whole family.
func PublishFilter() (f app.SubjectFilter) {
	f = app.SubjectFilter{
		Pattern:   SubjectWildcard,
		Reason:    "publish inflight query progress observed from system.processes",
		Direction: app.CapDirectionPub,
	}
	return
}

// SubscribeFilter is the capability an observer needs. Holding it means
// seeing the progress of runs this process did not issue, which is the
// entire point of the plane (R8) and the reason it is a capability rather
// than an assumption.
func SubscribeFilter() (f app.SubjectFilter) {
	f = app.SubjectFilter{
		Pattern:   SubjectWildcard,
		Reason:    "observe inflight progress of query runs, including runs issued by another party",
		Direction: app.CapDirectionSub,
	}
	return
}

// Tick is the wire payload of one progress observation.
//
// It is deliberately not a whole [runstream.Frame]: a frame is generic in
// its data payload, and this plane never carries data — only the advisory
// progress of a run somebody else is reading. [FrameOf] converts a received
// tick into a frame of whatever payload type the subscriber uses.
type Tick struct {
	QueryID  string             `json:"queryId"`
	Seq      uint64             `json:"seq"`
	Progress runstream.Progress `json:"progress"`
}

// FrameOf lifts a received tick into a runstream progress frame.
func FrameOf[T any](t Tick) (f runstream.Frame[T]) {
	f = runstream.ProgressFrame[T](runstream.Seq(t.Seq), t.Progress)
	return
}

// EncodeTick renders a tick for the wire.
func EncodeTick(t Tick) (payload []byte, err error) {
	payload, err = json.Marshal(t)
	if err != nil {
		err = eh.Errorf("queryprogress: encode tick: %w", err)
	}
	return
}

// DecodeTick parses a tick off the wire.
func DecodeTick(payload []byte) (t Tick, err error) {
	err = json.Unmarshal(payload, &t)
	if err != nil {
		err = eh.Errorf("queryprogress: decode tick: %w", err)
	}
	return
}

// ValidQueryID reports whether id is safe to use as a bus subject token and
// as a SQL string literal. The charset is `[A-Za-z0-9_.:-]`, which covers
// the client-minted ids in use (`play-<lane>-<pid>-<seq>`) and excludes the
// quote and wildcard characters that would let an id reach into either the
// statement the poller builds or the subject namespace.
//
// Rejecting is the whole defence: the poller never escapes or rewrites an
// id, so an id that gets in unchecked would be a hole in both.
func ValidQueryID(id string) (ok bool) {
	if id == "" || len(id) > 128 {
		return
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == ':'
		if !valid {
			return
		}
	}
	ok = true
	return
}
