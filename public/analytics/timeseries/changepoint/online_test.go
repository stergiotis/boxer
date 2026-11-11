package changepoint_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/changepoint"
)

func TestMosumCriticalAgainstHandComputation(t *testing.T) {
	// n/G = 20: a = √(2 ln 20) = 2.44775, b = 2 ln 20 + ½ ln ln 20 + ln 1.5 −
	// ½ ln π = 6.37320, c = −ln ln(1/√0.95) = 3.66336.
	assert.InDelta(t, 4.1002, changepoint.MosumCritical(1000, 50, 0.05), 5e-4)
	assert.Greater(t, changepoint.MosumCritical(1000, 50, 0.01), changepoint.MosumCritical(1000, 50, 0.05))
}

func TestMosumStatisticAgainstDirectComputation(t *testing.T) {
	vals := steps(21, []int{90, 110}, []float64{0, 1.5}, unit(2))
	const g = 20
	res, err := changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: g})
	require.NoError(t, err)
	require.Equal(t, int32(g), res.First)
	require.Equal(t, int32(len(vals)-g), res.Last)
	for k := g; k <= len(vals)-g; k++ {
		ml, sl := oracleMeanSq(vals[k-g : k])
		mr, sr := oracleMeanSq(vals[k : k+g])
		sigma := math.Sqrt((sl + sr) / (2 * g))
		want := math.Sqrt(g/2.0) * math.Abs(mr-ml) / sigma
		assert.InDelta(t, want, res.Stat[k], 1e-9, "k=%d", k)
	}
	assert.Zero(t, res.Stat[g-1])
	assert.Zero(t, res.Stat[len(vals)-g+1])
}

func TestMosumFindsPlantedShiftsAndNothingInNoise(t *testing.T) {
	vals := steps(22, []int{300, 250, 350}, []float64{0, 2, -1}, unit(3))
	res, err := changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 60})
	require.NoError(t, err)
	assertNear(t, []int32{300, 550}, res.Starts, 8)

	quiet := steps(23, []int{3000}, []float64{1}, unit(1))
	res, err = changepoint.Mosum(quiet, changepoint.MosumConfig{Bandwidth: 60})
	require.NoError(t, err)
	assert.Empty(t, res.Starts)
}

func TestMosumConstantWindows(t *testing.T) {
	vals := make([]float64, 60)
	for i := 30; i < 60; i++ {
		vals[i] = 1
	}
	res, err := changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 10})
	require.NoError(t, err)
	assert.Equal(t, []int32{30}, res.Starts)
}

func TestMosumErrors(t *testing.T) {
	vals := make([]float64, 100)
	_, err := changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 1})
	assert.Error(t, err)
	_, err = changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 40})
	assert.Error(t, err)
	_, err = changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 10, Alpha: 1.5})
	assert.Error(t, err)
	_, err = changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 10, Eta: 2})
	assert.Error(t, err)
	vals[3] = math.Inf(1)
	_, err = changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 10})
	assert.Error(t, err)
}

func TestCusumAgainstHandComputation(t *testing.T) {
	// μ = 10, σ = 2, k = 0.5, h = 2. z: 0, 1, 1.5, 2, −1 …
	chart, err := changepoint.NewCusum(changepoint.CusumConfig{Mean: 10, Sigma: 2, Threshold: 2})
	require.NoError(t, err)
	in := []float64{10, 12, 13, 14, 8, 6, 5}
	upper := []float64{0, 0.5, 1.5, 3.0, 0, 0, 0}
	lower := []float64{0, 0, 0, 0, 0.5, 2.0, 2.0}
	alarm := []bool{false, false, false, true, false, false, false}
	for i, v := range in {
		r, err := chart.Push(v)
		require.NoError(t, err)
		assert.False(t, r.WarmUp)
		assert.InDelta(t, upper[i], r.Upper, 1e-12, "upper at %d", i)
		if i < 6 {
			assert.InDelta(t, lower[i], r.Lower, 1e-12, "lower at %d", i)
			assert.Equal(t, alarm[i], r.Alarm, "alarm at %d", i)
		}
		if i == 3 {
			assert.True(t, r.Up)
			// The upper sum was last zero after sample 0.
			assert.Equal(t, int64(1), r.Onset)
		}
		if i == 6 {
			// 2.0 + 2.5 − 0.5 crosses h; the lower sum left zero at sample 4,
			// the first after the reset.
			assert.InDelta(t, 4.0, r.Lower, 1e-12)
			assert.True(t, r.Alarm)
			assert.False(t, r.Up)
			assert.Equal(t, int64(4), r.Onset)
		}
	}
}

func TestCusumWarmsUpDetectsAndRewarms(t *testing.T) {
	// A long warm-up and a threshold well above the default. With parameters
	// estimated from a hundred samples, a warm-up mean a quarter of a standard
	// deviation off is ordinary, and the chart then alarms on noise inside a
	// series this short — the caveat on CusumConfig.Warmup, and not what this
	// test is about.
	vals := steps(32, []int{700, 500}, []float64{0, 3}, unit(2))
	chart, err := changepoint.NewCusum(changepoint.CusumConfig{Warmup: 300, Threshold: 10})
	require.NoError(t, err)
	assert.Equal(t, 10.0, chart.Threshold())
	alarms := make([]int, 0, 2)
	onsets := make([]int64, 0, 2)
	warm := 0
	for i, v := range vals {
		r, err := chart.Push(v)
		require.NoError(t, err)
		if r.WarmUp {
			warm++
			assert.Zero(t, r.Upper)
		}
		if r.Alarm {
			alarms = append(alarms, i)
			onsets = append(onsets, r.Onset)
			assert.True(t, r.Up)
		}
	}
	require.Len(t, alarms, 1)
	assert.Greater(t, alarms[0], 700)
	assert.Less(t, alarms[0], 712)
	assert.InDelta(t, 700, float64(onsets[0]), 8)
	assert.LessOrEqual(t, onsets[0], int64(alarms[0]))
	assert.Equal(t, 600, warm, "one warm-up at the start and one after the alarm")
}

func TestCusumConstantWarmupKeepsWarming(t *testing.T) {
	chart, err := changepoint.NewCusum(changepoint.CusumConfig{Warmup: 3})
	require.NoError(t, err)
	for range 10 {
		r, err := chart.Push(4)
		require.NoError(t, err)
		assert.True(t, r.WarmUp)
	}
	r, _ := chart.Push(5)
	assert.True(t, r.WarmUp)
	r, _ = chart.Push(4)
	assert.False(t, r.WarmUp)
}

func TestCusumErrors(t *testing.T) {
	_, err := changepoint.NewCusum(changepoint.CusumConfig{Warmup: 1})
	assert.Error(t, err)
	_, err = changepoint.NewCusum(changepoint.CusumConfig{Sigma: -1})
	assert.Error(t, err)
	_, err = changepoint.NewCusum(changepoint.CusumConfig{Threshold: math.NaN()})
	assert.Error(t, err)
	chart, err := changepoint.NewCusum(changepoint.CusumConfig{})
	require.NoError(t, err)
	_, err = chart.Push(math.NaN())
	assert.Error(t, err)
}

// oracleStudentT is the Student-t density written from its textbook form,
// with the posterior parameters rebuilt from the run's samples each time
// rather than updated incrementally.
func oracleStudentT(prior changepoint.NormalGammaPrior, run []float64, v float64) (density float64) {
	n := float64(len(run))
	mean, ss := 0.0, 0.0
	if len(run) > 0 {
		mean, ss = oracleMeanSq(run)
	}
	kappa := prior.Kappa + n
	alpha := prior.Alpha + n/2
	mu := (prior.Kappa*prior.Mean + n*mean) / kappa
	beta := prior.Beta + 0.5*ss + prior.Kappa*n*(mean-prior.Mean)*(mean-prior.Mean)/(2*kappa)
	nu := 2 * alpha
	scale2 := beta * (kappa + 1) / (alpha * kappa)
	lgHi, _ := math.Lgamma((nu + 1) / 2)
	lgLo, _ := math.Lgamma(nu / 2)
	z := (v - mu) * (v - mu) / scale2
	density = math.Exp(lgHi-lgLo) / math.Sqrt(nu*math.Pi*scale2) * math.Pow(1+z/nu, -(nu+1)/2)
	return
}

// oracleRunPosterior is the untruncated run-length recursion.
func oracleRunPosterior(prior changepoint.NormalGammaPrior, hazard float64, vals []float64) (post []float64) {
	post = []float64{}
	for t, v := range vals {
		next := make([]float64, t+1)
		if t == 0 {
			next[0] = oracleStudentT(prior, nil, v)
		} else {
			var total float64
			for _, p := range post {
				total += p
			}
			next[0] = total * hazard * oracleStudentT(prior, nil, v)
			for r, p := range post {
				// post[r] is a run of r+1 samples ending at t−1.
				next[r+1] = p * (1 - hazard) * oracleStudentT(prior, vals[t-r-1:t], v)
			}
		}
		var sum float64
		for _, p := range next {
			sum += p
		}
		for i := range next {
			next[i] /= sum
		}
		post = next
	}
	return
}

func TestPropertyBocpdMatchesUntruncatedRecursion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vals := drawSeries(t, 1, 40)
		prior := changepoint.NormalGammaPrior{
			Mean:  rapid.Float64Range(-2, 2).Draw(t, "mean"),
			Kappa: rapid.Float64Range(0.1, 3).Draw(t, "kappa"),
			Alpha: rapid.Float64Range(0.5, 3).Draw(t, "alpha"),
			Beta:  rapid.Float64Range(0.1, 5).Draw(t, "beta"),
		}
		expected := rapid.Float64Range(2, 50).Draw(t, "expectedRun")
		lag := rapid.Int32Range(1, 5).Draw(t, "lag")
		det, err := changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: expected, Prior: prior, MaxRun: 100, Lag: lag})
		require.NoError(t, err)
		var reading changepoint.BocpdReading
		for _, v := range vals {
			reading, err = det.Push(v)
			require.NoError(t, err)
		}
		want := oracleRunPosterior(prior, 1/expected, vals)
		got := det.Posterior(nil)
		require.Len(t, got, len(want))
		var recent float64
		best := 0
		for r := range want {
			assert.InDelta(t, want[r], got[r], 1e-9, "run %d", r+1)
			if r < int(lag) {
				recent += want[r]
			}
			if want[r] > want[best] {
				best = r
			}
		}
		assert.InDelta(t, recent, reading.RecentChange, 1e-9)
		if want[best] > 0.5 {
			assert.Equal(t, int32(best+1), reading.MapRun)
		}
	})
}

func TestBocpdSeesAPlantedShift(t *testing.T) {
	vals := steps(41, []int{300, 300}, []float64{0, 4}, unit(2))
	prior, err := changepoint.PriorFromSample(vals[:50])
	require.NoError(t, err)
	det, err := changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: 250, Prior: prior, Lag: 10})
	require.NoError(t, err)
	peakAt, peak := -1, 0.0
	var quietMax float64
	for i, v := range vals {
		r, err := det.Push(v)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, r.RecentChange, 0.0)
		assert.LessOrEqual(t, r.RecentChange, 1.0)
		if i >= 50 && i < 295 {
			quietMax = max(quietMax, r.RecentChange)
		}
		if i >= 295 && r.RecentChange > peak {
			peakAt, peak = i, r.RecentChange
		}
		if i == 299 {
			assert.Greater(t, r.MapRun, int32(250))
		}
		if i == 340 {
			assert.InDelta(t, 41, float64(r.MapRun), 3)
		}
	}
	assert.Greater(t, peak, 0.95)
	assert.Less(t, peakAt, 312)
	assert.Less(t, quietMax, 0.5)
}

func TestBocpdTruncationKeepsAProperPosterior(t *testing.T) {
	vals := steps(42, []int{400, 200}, []float64{1, -3}, unit(2))
	det, err := changepoint.NewBocpd(changepoint.BocpdConfig{
		ExpectedRun: 100, MaxRun: 30, Lag: 5,
		Prior: changepoint.NormalGammaPrior{Mean: 0, Kappa: 1, Alpha: 1, Beta: 1},
	})
	require.NoError(t, err)
	post := make([]float64, 0, 30)
	for i, v := range vals {
		r, err := det.Push(v)
		require.NoError(t, err)
		post = det.Posterior(post)
		require.LessOrEqual(t, len(post), 30)
		var sum float64
		for _, p := range post {
			sum += p
		}
		require.InDelta(t, 1, sum, 1e-9)
		if i == 399 {
			// The regime has run far past the truncation; the mass sits in
			// the last slot and the estimate has not drifted off.
			assert.Equal(t, int32(30), r.MapRun)
			assert.Less(t, r.RecentChange, 0.2)
		}
		if i == 410 {
			assert.Less(t, r.MapRun, int32(15))
		}
	}
}

func TestBocpdSurvivesASampleNoHypothesisExplains(t *testing.T) {
	det, err := changepoint.NewBocpd(changepoint.BocpdConfig{
		ExpectedRun: 50,
		Prior:       changepoint.NormalGammaPrior{Mean: 0, Kappa: 1, Alpha: 200, Beta: 1e-3},
	})
	require.NoError(t, err)
	for range 20 {
		_, err = det.Push(0)
		require.NoError(t, err)
	}
	r, err := det.Push(1e200)
	require.NoError(t, err)
	assert.Equal(t, int32(1), r.MapRun)
	assert.InDelta(t, 1, r.RecentChange, 1e-12)
}

func TestBocpdErrors(t *testing.T) {
	good := changepoint.NormalGammaPrior{Kappa: 1, Alpha: 1, Beta: 1}
	_, err := changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: 1, Prior: good})
	assert.Error(t, err)
	_, err = changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: 10})
	assert.Error(t, err)
	_, err = changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: 10, Prior: good, Lag: -1})
	assert.Error(t, err)
	_, err = changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: math.NaN(), Prior: good})
	assert.Error(t, err)
	det, err := changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: 10, Prior: good})
	require.NoError(t, err)
	_, err = det.Push(math.Inf(-1))
	assert.Error(t, err)

	_, err = changepoint.PriorFromSample([]float64{1})
	assert.Error(t, err)
	_, err = changepoint.PriorFromSample([]float64{2, 2, 2})
	assert.Error(t, err)
	p, err := changepoint.PriorFromSample([]float64{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, changepoint.NormalGammaPrior{Mean: 2, Kappa: 1, Alpha: 1, Beta: 1}, p)
}
