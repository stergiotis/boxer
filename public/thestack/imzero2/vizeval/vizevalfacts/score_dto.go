package vizevalfacts

import "time"

// VizevalScore is one scorecard as a `boxer.facts` row (ADR-0257 §SD8). Id
// is the xxh3 of NaturalKey, and NaturalKey names what makes two scorings
// the same measurement — scenario, candidate, build and batch digest — so a
// second scoring of it lands under the same key and Latest finds the newest.
// Ts is when it was scored.
type VizevalScore struct {
	_ struct{} `kind:"vizevalScore"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindVizevalScore,symbol"`
	// Scenario, CandidateId and Sink identify what was rendered; Candidate is
	// the candidate's canonical JSON, the options included.
	Scenario    string   `lw:"vizevalScenario,symbol"`
	CandidateId string   `lw:"vizevalCandidateId,symbol"`
	Sink        string   `lw:"vizevalSink,symbol"`
	Candidate   []string `lw:"vizevalCandidate,stringArray"`
	// Build and BatchDigest say what code drew which data; two rows compare
	// only when both agree.
	Build       string `lw:"vizevalBuild,symbol"`
	BatchDigest string `lw:"vizevalBatchDigest,symbol"`
	Rows        uint64 `lw:"vizevalRows,u64Array,unit"`
	// Status is scored, gated, inadmissible or failed; Reason says why when
	// it is not scored: one element when there is one, none otherwise.
	Status string   `lw:"vizevalStatus,symbol"`
	Reason []string `lw:"vizevalReason,stringArray"`
	// Dir is where the captures were written, relative to the run's output
	// directory; none when nothing was rendered.
	Dir []string `lw:"vizevalDir,stringArray"`
	// Area is the measured rect, x0 y0 x1 y1, empty when nothing was measured.
	Area []float64 `lw:"vizevalArea,f64Array"`
	// MetricName and MetricValue are parallel: the i-th value is the i-th
	// metric's.
	MetricName  []string  `lw:"vizevalMetricName,symbolArray"`
	MetricValue []float64 `lw:"vizevalMetricValue,f64Array"`
	// GatePassed and GateFailed name the scenario's gates by outcome.
	GatePassed []string `lw:"vizevalGatePassed,symbolArray"`
	GateFailed []string `lw:"vizevalGateFailed,symbolArray"`
}
