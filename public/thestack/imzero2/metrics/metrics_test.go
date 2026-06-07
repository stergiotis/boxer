//go:build llm_generated_opus47

package metrics

import (
	"math"
	"testing"
	"time"
)

func TestEMA(t *testing.T) {
	cases := []struct {
		name         string
		prev, sample float64
		want         float64
	}{
		{"zero prev seeds toward sample by alpha", 0, 10, 1.0},
		{"steady state stays put", 10, 10, 10},
		{"decrease pulls down by alpha gap", 10, 0, 9.0},
		{"halfway move", 5, 15, 6.0},
		{"negative sample (degenerate input still arithmetic)", 10, -10, 8.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ema(tc.prev, tc.sample)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("ema(%v,%v): got %v want %v", tc.prev, tc.sample, got, tc.want)
			}
		})
	}
}

func TestEndFrame_NoBegin_IsNoOp(t *testing.T) {
	fm := NewFrameMetrics()
	fm.EndFrame()

	if fm.LastTotalNs != 0 {
		t.Errorf("LastTotalNs: got %d want 0", fm.LastTotalNs)
	}
	if fm.emaInitialized {
		t.Error("emaInitialized should remain false when EndFrame is called before BeginFrame")
	}
	if snap := fm.Snapshot(); snap.FrameCounter != 0 {
		t.Errorf("FrameCounter: got %d want 0", snap.FrameCounter)
	}
}

func TestEndFrame_MissingBeforeSync_AccountsAllAsRender(t *testing.T) {
	fm := NewFrameMetrics()
	fm.tStart = time.Now()
	fm.EndFrame()

	if fm.LastSyncNs != 0 {
		t.Errorf("LastSyncNs: got %d want 0 (missing BeforeSync should yield syncNs=0)", fm.LastSyncNs)
	}
	if fm.LastRenderNs != fm.LastTotalNs {
		t.Errorf("LastRenderNs (%d) should equal LastTotalNs (%d) when BeforeSync was never called",
			fm.LastRenderNs, fm.LastTotalNs)
	}
	if !fm.emaInitialized {
		t.Error("emaInitialized should flip true on first EndFrame")
	}
	if snap := fm.Snapshot(); snap.FrameCounter != 1 {
		t.Errorf("FrameCounter after one EndFrame: got %d want 1", snap.FrameCounter)
	}
}

func TestEndFrame_BeforeSyncBeforeStart_TreatedAsMissing(t *testing.T) {
	fm := NewFrameMetrics()
	fm.tStart = time.Unix(100, 0)
	fm.tBeforeSync = time.Unix(50, 0)
	fm.EndFrame()

	if fm.LastSyncNs != 0 {
		t.Errorf("LastSyncNs: got %d want 0 (BeforeSync earlier than tStart should be discarded)", fm.LastSyncNs)
	}
	if fm.LastRenderNs != fm.LastTotalNs {
		t.Errorf("LastRenderNs (%d) != LastTotalNs (%d) on bogus-BeforeSync path",
			fm.LastRenderNs, fm.LastTotalNs)
	}
}

func TestEndFrame_NormalSequence_SplitsRenderAndSync(t *testing.T) {
	fm := NewFrameMetrics()
	t0 := time.Now()
	// Anchor both offsets in the past relative to t0 so EndFrame's tEnd
	// (time.Now, monotonic >= t0) yields a strictly positive syncNs, while the
	// render split (tBeforeSync-tStart) stays an exact 100ns. Setting
	// tBeforeSync = t0+100ns made syncNs depend on real elapsed time and flaked
	// negative when EndFrame ran in under 100ns.
	fm.tStart = t0.Add(-300 * time.Nanosecond)
	fm.tBeforeSync = t0.Add(-200 * time.Nanosecond)
	fm.EndFrame()

	if fm.LastRenderNs != 100 {
		t.Errorf("LastRenderNs: got %d want 100 (tBeforeSync-tStart)", fm.LastRenderNs)
	}
	if fm.LastSyncNs <= 0 {
		t.Errorf("LastSyncNs: got %d, expected > 0 (time.Now > tBeforeSync)", fm.LastSyncNs)
	}
	if fm.LastTotalNs != fm.LastRenderNs+fm.LastSyncNs {
		t.Errorf("LastTotalNs (%d) != LastRenderNs+LastSyncNs (%d+%d)",
			fm.LastTotalNs, fm.LastRenderNs, fm.LastSyncNs)
	}
}

func TestEndFrame_ResetsTimingsForNextFrame(t *testing.T) {
	fm := NewFrameMetrics()
	fm.tStart = time.Now()
	fm.tBeforeSync = fm.tStart.Add(time.Microsecond)
	fm.EndFrame()

	if !fm.tStart.IsZero() {
		t.Errorf("tStart should be zeroed after EndFrame; got %v", fm.tStart)
	}
	if !fm.tBeforeSync.IsZero() {
		t.Errorf("tBeforeSync should be zeroed after EndFrame; got %v", fm.tBeforeSync)
	}

	fm.EndFrame()
	if fm.Snapshot().FrameCounter != 1 {
		t.Errorf("Second EndFrame with zero tStart must be a no-op; FrameCounter=%d", fm.Snapshot().FrameCounter)
	}
}

func TestEndFrame_FrameCounterIncrementsPerCall(t *testing.T) {
	fm := NewFrameMetrics()
	for i := uint64(1); i <= 3; i++ {
		fm.tStart = time.Now()
		fm.EndFrame()
		if got := fm.Snapshot().FrameCounter; got != i {
			t.Errorf("after %d EndFrame call(s): FrameCounter got %d want %d", i, got, i)
		}
	}
}

func TestEndFrame_EmaSeedsRawOnFirstFrame(t *testing.T) {
	fm := NewFrameMetrics()
	fm.LastWritten = 1234
	fm.LastRead = 5678
	t0 := time.Now()
	fm.tStart = t0
	fm.tBeforeSync = t0.Add(200 * time.Nanosecond)
	fm.EndFrame()

	if fm.EmaRenderNs != float64(fm.LastRenderNs) {
		t.Errorf("EmaRenderNs (%v) should equal raw LastRenderNs (%d) on first EndFrame", fm.EmaRenderNs, fm.LastRenderNs)
	}
	if fm.EmaSyncNs != float64(fm.LastSyncNs) {
		t.Errorf("EmaSyncNs (%v) should equal raw LastSyncNs (%d)", fm.EmaSyncNs, fm.LastSyncNs)
	}
	if fm.EmaTotalNs != float64(fm.LastTotalNs) {
		t.Errorf("EmaTotalNs (%v) should equal raw LastTotalNs (%d)", fm.EmaTotalNs, fm.LastTotalNs)
	}
	if fm.EmaWritten != 1234 || fm.EmaRead != 5678 {
		t.Errorf("EmaWritten/EmaRead should seed from LastWritten/LastRead at first EndFrame: got %v/%v want 1234/5678",
			fm.EmaWritten, fm.EmaRead)
	}
}

func TestRecordBytes_BeforeFirstFrame_NoEmaUpdate(t *testing.T) {
	fm := NewFrameMetrics()
	fm.RecordBytes(100, 50)

	if fm.LastWritten != 100 || fm.LastRead != 50 {
		t.Errorf("LastWritten/LastRead: got %d/%d want 100/50", fm.LastWritten, fm.LastRead)
	}
	if fm.EmaWritten != 0 || fm.EmaRead != 0 {
		t.Errorf("Ema* should stay 0 until emaInitialized: got written=%v read=%v",
			fm.EmaWritten, fm.EmaRead)
	}
}

func TestRecordBytes_AfterFirstFrame_AppliesEma(t *testing.T) {
	fm := NewFrameMetrics()
	fm.LastWritten = 200
	fm.LastRead = 100
	fm.tStart = time.Now()
	fm.EndFrame()

	fm.RecordBytes(400, 200)

	// ema(200, 400) = 200 + 0.1*(400-200) = 220
	if math.Abs(fm.EmaWritten-220) > 1e-6 {
		t.Errorf("EmaWritten: got %v want 220", fm.EmaWritten)
	}
	// ema(100, 200) = 100 + 0.1*(200-100) = 110
	if math.Abs(fm.EmaRead-110) > 1e-6 {
		t.Errorf("EmaRead: got %v want 110", fm.EmaRead)
	}
	if fm.LastWritten != 400 || fm.LastRead != 200 {
		t.Errorf("LastWritten/LastRead after RecordBytes: got %d/%d want 400/200",
			fm.LastWritten, fm.LastRead)
	}
}

func TestRecordRust_Uninitialized_SeedsRaw(t *testing.T) {
	fm := NewFrameMetrics()
	fm.RecordRust(1000, 7)

	if fm.LastInterpretNs != 1_000_000 {
		t.Errorf("LastInterpretNs: got %d want 1_000_000 (interpretUs*1000)", fm.LastInterpretNs)
	}
	if fm.LastPassNr != 7 {
		t.Errorf("LastPassNr: got %d want 7", fm.LastPassNr)
	}
	if fm.EmaInterpretNs != 1_000_000 {
		t.Errorf("EmaInterpretNs: got %v want 1_000_000 (uninitialized seed path)", fm.EmaInterpretNs)
	}
}

func TestRecordRust_AfterEndFrame_AppliesEma(t *testing.T) {
	fm := NewFrameMetrics()
	fm.tStart = time.Now()
	fm.EndFrame()
	// EndFrame seeds Render/Sync/Total/Written/Read EMAs but NOT EmaInterpretNs.
	// So after EndFrame: emaInitialized=true, EmaInterpretNs=0.

	fm.RecordRust(1000, 1)
	// ema(0, 1_000_000) = 100_000
	if math.Abs(fm.EmaInterpretNs-100_000) > 1.0 {
		t.Errorf("EmaInterpretNs after first RecordRust: got %v want 100000", fm.EmaInterpretNs)
	}

	fm.RecordRust(2000, 2)
	// ema(100_000, 2_000_000) = 100_000 + 0.1*1_900_000 = 290_000
	if math.Abs(fm.EmaInterpretNs-290_000) > 1.0 {
		t.Errorf("EmaInterpretNs after second RecordRust: got %v want 290000", fm.EmaInterpretNs)
	}
	if fm.LastPassNr != 2 {
		t.Errorf("LastPassNr: got %d want 2", fm.LastPassNr)
	}
}

func TestSnapshot_FieldsMappedFromEmas(t *testing.T) {
	fm := NewFrameMetrics()
	fm.EmaRenderNs = 5_000_000
	fm.EmaSyncNs = 3_000_000
	fm.EmaTotalNs = 10_000_000
	fm.LastTotalNs = 9_500_000
	fm.EmaInterpretNs = 6_000_000
	fm.EmaWritten = 1024
	fm.EmaRead = 512
	fm.LastPassNr = 42
	fm.frameCounter = 17

	s := fm.Snapshot()

	if s.RenderNs != 5_000_000 || s.SyncNs != 3_000_000 || s.TotalNs != 10_000_000 {
		t.Errorf("RenderNs/SyncNs/TotalNs: got %d/%d/%d want 5M/3M/10M", s.RenderNs, s.SyncNs, s.TotalNs)
	}
	if s.RawTotalNs != 9_500_000 {
		t.Errorf("RawTotalNs: got %d want 9_500_000 (must come from LastTotalNs, not EMA)", s.RawTotalNs)
	}
	if s.InterpretNs != 6_000_000 {
		t.Errorf("InterpretNs: got %d want 6_000_000", s.InterpretNs)
	}
	if s.SlackNs != 4_000_000 {
		t.Errorf("SlackNs: got %d want 4_000_000 (TotalNs-InterpretNs)", s.SlackNs)
	}
	if s.WrittenBytes != 1024 || s.ReadBytes != 512 {
		t.Errorf("WrittenBytes/ReadBytes: got %d/%d want 1024/512", s.WrittenBytes, s.ReadBytes)
	}
	if s.RustPassNr != 42 {
		t.Errorf("RustPassNr: got %d want 42", s.RustPassNr)
	}
	if s.FrameCounter != 17 {
		t.Errorf("FrameCounter: got %d want 17", s.FrameCounter)
	}
}

func TestSnapshot_SlackClampsToZeroWhenInterpretExceedsTotal(t *testing.T) {
	fm := NewFrameMetrics()
	fm.EmaTotalNs = 5_000_000
	fm.EmaInterpretNs = 7_000_000

	s := fm.Snapshot()
	if s.SlackNs != 0 {
		t.Errorf("SlackNs should clamp to 0 when InterpretNs > TotalNs; got %d", s.SlackNs)
	}
}

func TestShouldWarnSlowFrame_GatesOnWorkNotTotal(t *testing.T) {
	const thr = SlowFrameThresholdNs // 25ms
	cases := []struct {
		name                  string
		renderNs, interpretNs int64
		thresholdNs           int64
		want                  bool
	}{
		// The occlusion/idle case the gate exists for: trivial render+interpret,
		// huge sync wait elsewhere — must stay quiet.
		{"trivial work stays quiet", 4_000_000, 6_000_000, thr, false},
		// Real regressions on either side must still fire.
		{"slow Go render fires", 30_000_000, 1_000_000, thr, true},
		{"slow Rust interpret fires", 2_000_000, 40_000_000, thr, true},
		{"sum crossing threshold fires", 13_000_000, 13_000_000, thr, true},
		// Strict >: exactly at the threshold stays quiet.
		{"exactly at threshold stays quiet", thr, 0, thr, false},
		// Zero/negative threshold disables regardless of work.
		{"zero threshold disables", 100_000_000, 100_000_000, 0, false},
		{"negative threshold disables", 100_000_000, 100_000_000, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldWarnSlowFrame(tc.renderNs, tc.interpretNs, tc.thresholdNs); got != tc.want {
				t.Errorf("shouldWarnSlowFrame(%d,%d,%d): got %v want %v",
					tc.renderNs, tc.interpretNs, tc.thresholdNs, got, tc.want)
			}
		})
	}
}

func TestPushFps_RefreshesOnCadence(t *testing.T) {
	fm := NewFrameMetrics()
	// No rebuild until the cadence boundary is crossed: the digest stays empty.
	for range fpsDigestRefreshFrames - 1 {
		fm.pushFps(60)
	}
	if got := fm.FpsDigest().Count(); got != 0 {
		t.Errorf("digest Count before first refresh: got %d want 0", got)
	}
	fm.pushFps(60) // crosses the boundary -> first rebuild
	if got := fm.FpsDigest().Count(); got != int64(fpsDigestRefreshFrames) {
		t.Errorf("digest Count after first refresh: got %d want %d", got, fpsDigestRefreshFrames)
	}
}

func TestPushFps_WindowCapsCount(t *testing.T) {
	fm := NewFrameMetrics()
	// Push well past the window, landing on a cadence boundary so the final
	// rebuild reflects the full (capped) window rather than an all-time count.
	n := fpsWindowFrames + 5*fpsDigestRefreshFrames
	for range n {
		fm.pushFps(60)
	}
	if fm.fpsRingLen != fpsWindowFrames {
		t.Errorf("fpsRingLen: got %d want %d (window must cap)", fm.fpsRingLen, fpsWindowFrames)
	}
	if got := fm.FpsDigest().Count(); got != int64(fpsWindowFrames) {
		t.Errorf("digest Count: got %d want %d (windowed, not all-time)", got, fpsWindowFrames)
	}
}

func TestPushFps_MedianRobustToSlowFrame(t *testing.T) {
	// The headline median must not lurch when a single frame stalls — the
	// failure mode of 1/EMA(period). One 10 fps hitch in an otherwise steady
	// 60 fps window leaves the median near 60; the hitch lives in the tail.
	fm := NewFrameMetrics()
	fm.pushFps(10) // the stall
	for range fpsWindowFrames - 1 {
		fm.pushFps(60)
	}
	d := fm.FpsDigest()
	if med := d.Quantile(0.5); med < 55 {
		t.Errorf("median fps with one slow frame: got %v want ~60 (robust, not dragged toward 10)", med)
	}
	if mn := d.Min(); math.Abs(mn-10) > 1e-6 {
		t.Errorf("min fps should retain the hitch in the tail: got %v want 10", mn)
	}
}

func TestPushFps_WindowEvictsOldSamples(t *testing.T) {
	// Recency: once the window turns over, the old regime is gone entirely —
	// the property an all-time (non-forgetting) digest cannot provide.
	fm := NewFrameMetrics()
	for range fpsWindowFrames {
		fm.pushFps(30) // old regime
	}
	for range fpsWindowFrames {
		fm.pushFps(90) // new regime fully displaces the old
	}
	d := fm.FpsDigest()
	if med := d.Quantile(0.5); math.Abs(med-90) > 1e-6 {
		t.Errorf("median after window turnover: got %v want 90 (old 30 fps regime evicted)", med)
	}
	if mn := d.Min(); math.Abs(mn-90) > 1e-6 {
		t.Errorf("min after turnover should see the new regime only: got %v want 90", mn)
	}
}
