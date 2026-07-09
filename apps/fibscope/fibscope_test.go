package fibscope

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// decode is oracle-checked against the identifier package rather than hardcoded
// bit patterns, so the app can never drift from the split contract it teaches.
func TestDecodeMatchesIdentifierOracle(t *testing.T) {
	for _, id := range []uint64{defaultId, invalidId, 0, 1729382256910270465, ^uint64(0)} {
		tid := identifier.TaggedId(id)
		wantTag, wantBody := tid.Split()
		d := decode(id)
		assert.Equal(t, id, d.id)
		assert.Equal(t, tid.IsValid(), d.valid)
		assert.Equal(t, wantTag, d.tag)
		assert.Equal(t, wantBody, d.body)
		assert.Equal(t, wantTag.GetValue(), d.tagValue)
		assert.Equal(t, tid.GetTagWidth(), d.tagWidth)
		assert.Len(t, d.bits, 64)
	}
}

func TestDecodeWorkedExample(t *testing.T) {
	// The howto's example: id 12393906174523604993 = tag value 12 (code
	// 101011, width 6), body 1.
	d := decode(defaultId)
	assert.True(t, d.valid)
	assert.Equal(t, identifier.TagValue(12), d.tagValue)
	assert.Equal(t, 6, d.tagWidth)
	assert.Equal(t, identifier.UntaggedId(1), d.body)
	assert.Equal(t, "101011", d.bits[:d.tagWidth])
}

func TestDecodeInvalid(t *testing.T) {
	d := decode(invalidId)
	assert.False(t, d.valid)
	assert.Equal(t, identifier.TagValue(0), d.tagValue)
	assert.Equal(t, 0, d.tagWidth)
	assert.Equal(t, identifier.UntaggedId(0), d.body)
}

func TestBitRunsSegmentTheStrip(t *testing.T) {
	d := decode(defaultId)
	runs := bitRuns(d.bits, d.valid, d.tagWidth)
	require.Len(t, runs, 3)
	// The three regions concatenate back to the full 64-bit string.
	assert.Equal(t, d.bits, runs[0].text+runs[1].text+runs[2].text)
	// The middle run is the comma — the trailing 11 of the fibonacci code.
	assert.Equal(t, "11", runs[1].text)
	assert.Equal(t, colComma, runs[1].col)
	assert.Equal(t, colTagCode, runs[0].col)
	assert.Equal(t, colBody, runs[2].col)
	// Region widths: tag code (minus comma) + comma + body == 64.
	assert.Equal(t, d.tagWidth-2, len(runs[0].text))
	assert.Equal(t, 64-d.tagWidth, len(runs[2].text))
}

func TestBitRunsInvalidIsOneNeutralRun(t *testing.T) {
	d := decode(invalidId)
	runs := bitRuns(d.bits, d.valid, d.tagWidth)
	require.Len(t, runs, 1)
	assert.Equal(t, d.bits, runs[0].text)
	assert.Equal(t, colInvalid, runs[0].col)
}

func TestComposeIdMasksBodyIntoTheBodyRegion(t *testing.T) {
	tag := identifier.TagValue(12).GetTag()
	// The worked example round-trips.
	assert.Equal(t, defaultId, composeId(tag, 1))
	// An oversized body is masked to the tag's body region and never bleeds
	// into the tag bits — the id keeps decoding to tag value 12.
	got := composeId(tag, ^uint64(0))
	assert.Equal(t, identifier.TagValue(12), decode(got).tagValue)
	assert.Equal(t, uint64(tag.GetMaxPossibleIdIncl()), uint64(decode(got).body))
}

func TestSetTagValuePreservesBodyAndClamps(t *testing.T) {
	inst := &App{id: defaultId}
	inst.setBody(5)
	assert.Equal(t, identifier.UntaggedId(5), decode(inst.id).body)

	// Narrowing to tag value 1 (width 2) keeps the body, which still fits.
	inst.setTagValue(1)
	d := decode(inst.id)
	assert.Equal(t, identifier.TagValue(1), d.tagValue)
	assert.Equal(t, identifier.UntaggedId(5), d.body)

	// Out-of-domain tag values clamp into [1, MaxTagValue] without panicking.
	inst.setTagValue(0)
	assert.Equal(t, identifier.TagValue(1), decode(inst.id).tagValue)
	inst.setTagValue(^uint64(0))
	assert.Equal(t, identifier.MaxTagValue, decode(inst.id).tagValue)
}

func TestSetBodyOnInvalidIdIsNoOp(t *testing.T) {
	inst := &App{id: invalidId}
	inst.setBody(3)
	assert.Equal(t, invalidId, inst.id)
}

// TestDragScrubberDelayedWritebackIsNotClobbered pins the fix for the reported
// bug: a DragValueU64 bound to a frame-local re-seeded from inst.id each frame
// lost every edit. dragScrubber holds a stable draft and re-seeds it only on an
// external change, so the frontend's one-frame-late databinding writeback
// survives to the frame HasChanged fires.
func TestDragScrubberDelayedWritebackIsNotClobbered(t *testing.T) {
	var s dragScrubber

	// Frame N: seed from the decoded value, then the user scrubs 12 -> 13. The
	// frontend writeback (StateManager.Sync) lands in the stable pointer after
	// the widget has rendered.
	require.Equal(t, uint64(12), s.sync(12))
	s.draft = 13

	// Frame N+1: inst.id has not been recomposed yet (the app applies this
	// frame), so the decode still reads 12. The old frame-local binding was
	// re-seeded to 12 here and the edit vanished; sync must keep the 13.
	assert.Equal(t, uint64(13), s.sync(12), "pending edit must survive the re-seed guard")

	// The app applies the edit and commits the value decoded back out of the
	// now-updated inst.id.
	s.commit(13)

	// Frame N+2: the decode reports 13; sync leaves the settled value alone.
	assert.Equal(t, uint64(13), s.sync(13))
}

// TestDragScrubberReseedsOnExternalChange covers the complementary direction: a
// preset button or the raw-id field moves inst.id under an idle scrubber, and
// the scrubber must adopt the new decoded value (reflects was never advanced,
// so the change reads as external).
func TestDragScrubberReseedsOnExternalChange(t *testing.T) {
	var s dragScrubber
	require.Equal(t, uint64(12), s.sync(12))
	assert.Equal(t, uint64(40), s.sync(40))
	assert.Equal(t, uint64(40), s.draft)
}

// TestDragScrubberCommitSnapsToResolvedValue covers a clamped edit: the frontend
// writes an out-of-domain scrub value, the app clamps on apply, and commit snaps
// the draft to the resolved value so the next frame shows the real result
// instead of the rejected input — and does not re-seed over it.
func TestDragScrubberCommitSnapsToResolvedValue(t *testing.T) {
	var s dragScrubber
	s.sync(1)
	s.draft = 9999 // frontend writeback of an out-of-range scrub
	s.commit(500)  // app clamped to 500 and committed the decoded result
	assert.Equal(t, uint64(500), s.draft)
	assert.Equal(t, uint64(500), s.sync(500))
}

// TestTagScrubberEditAdvancesId drives the App the way renderControls does,
// minus the GUI, to prove the end-to-end symptom is gone: a tag-value scrub now
// advances inst.id instead of snapping back to the initial value.
func TestTagScrubberEditAdvancesId(t *testing.T) {
	inst := &App{id: defaultId} // tag value 12, body 1

	// Frame N: seed from the decode, user scrubs the tag to 13, frontend writes
	// it back into the stable draft.
	inst.tagScrub.sync(uint64(decode(inst.id).tagValue))
	inst.tagScrub.draft = 13

	// Frame N+1: sync keeps the pending 13 (inst.id still decodes to 12),
	// HasChanged fires, the app recomposes inst.id and commits.
	inst.tagScrub.sync(uint64(decode(inst.id).tagValue))
	inst.setTagValue(inst.tagScrub.draft)
	inst.tagScrub.commit(uint64(decode(inst.id).tagValue))

	assert.Equal(t, identifier.TagValue(13), decode(inst.id).tagValue,
		"tag scrub must advance inst.id, not reset to the initial value")
	assert.Equal(t, identifier.UntaggedId(1), decode(inst.id).body,
		"the body is preserved across a tag-value edit")
}

func TestZeckExplain(t *testing.T) {
	// The code spells the tag value as a sum of non-consecutive Fibonacci
	// numbers (the two encoder biases cancel).
	assert.Equal(t, "12 = 8 + 3 + 1", zeckExplain(12))
	assert.Equal(t, "1 = 1", zeckExplain(1))
	assert.Equal(t, "—", zeckExplain(0))

	// The listed parts always sum back to the tag value.
	for _, tv := range []identifier.TagValue{2, 7, 12, 100, 1000, 65535} {
		s := zeckExplain(tv)
		_, rhs, ok := strings.Cut(s, " = ")
		require.True(t, ok)
		sum := 0
		for p := range strings.SplitSeq(rhs, " + ") {
			v, err := strconv.Atoi(p)
			require.NoError(t, err)
			sum += v
		}
		assert.Equal(t, int(tv), sum, "parts of %q must sum to the tag value", s)
	}
}

func TestHumanSci(t *testing.T) {
	assert.Equal(t, "42", humanSci(42))
	assert.Equal(t, "99999", humanSci(99999))
	assert.Equal(t, "1e+05", humanSci(100000))
	assert.Equal(t, "2.88e+17", humanSci(288230376151711743)) // 2^58 − 1
}

func TestHumanizeExhaust(t *testing.T) {
	const yr = 365.25 * 24 * 3600.0
	// The bounded minute/hour/day ladder is exact.
	assert.Equal(t, "1.5 min", humanizeExhaust(90))
	assert.Equal(t, "1.5 h", humanizeExhaust(1.5*3600))
	assert.Equal(t, "2.0 d", humanizeExhaust(2*24*3600))
	// The SI extremes carry the right unit across the huge range.
	assert.Contains(t, humanizeExhaust(0.013), "ms")
	assert.Contains(t, humanizeExhaust(30), " s")
	assert.Contains(t, humanizeExhaust(2.5*yr), " yr")
	assert.Contains(t, humanizeExhaust(14600*yr), "kyr")
	assert.Contains(t, humanizeExhaust(1.46e9*yr), "Gyr")
	// A wide tag at 10 MHz fills in milliseconds; a narrow tag at 100 Hz
	// outlasts the universe — spot-check the two anchors used in the table.
	assert.Contains(t, humanizeExhaust(float64(uint64(1)<<17-1)/1e7), "ms") // width 47 @ 10MHz
	assert.Contains(t, humanizeExhaust(float64(uint64(1)<<62-1)/1e2), "Gyr") // width 2 @ 100Hz
}

func TestClampMaxExp(t *testing.T) {
	assert.Equal(t, uint64(1), clampMaxExp(0))
	assert.Equal(t, uint64(1), clampMaxExp(-5))
	assert.Equal(t, uint64(1_000_000), clampMaxExp(1e6))
	// The top of the range stays below the advisor's 2^60 rejection point.
	assert.Equal(t, uint64(1)<<60-1, clampMaxExp(advisorMaxExpCeil))
	assert.Equal(t, uint64(1)<<60-1, clampMaxExp(advisorMaxExpCeil*2))
}
