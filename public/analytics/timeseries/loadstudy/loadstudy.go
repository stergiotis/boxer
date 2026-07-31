package loadstudy

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Client is a minimal ClickHouse HTTP query surface — enough to pull a series
// out of the system logs and no more. The heavier clients in
// [github.com/stergiotis/boxer/public/db/clickhouse] carry a dependency chain
// this package has no use for.
type Client struct {
	Endpoint string
	User     string
	Password string
	HTTP     *http.Client
}

// NewClientFromEnv builds a client from the CLICKHOUSE_* registry entries.
// The endpoint falls back to CLICKHOUSE_URL, then to localhost.
func NewClientFromEnv() (inst *Client) {
	endpoint := clickhouseenv.Endpoint.Get()
	if endpoint == "" {
		endpoint = clickhouseenv.URL.Get()
	}
	if endpoint == "" {
		endpoint = "http://localhost:8123/"
	}
	inst = &Client{
		Endpoint: endpoint,
		User:     clickhouseenv.User.Get(),
		Password: clickhouseenv.Password.Get(),
		HTTP:     &http.Client{Timeout: 2 * time.Minute},
	}
	return
}

// QueryTSVE runs sql and returns the TSV body, one row per line.
func (inst *Client) QueryTSVE(ctx context.Context, sql string) (rows []string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.Endpoint, strings.NewReader(sql))
	if err != nil {
		err = eb.Build().Str("endpoint", inst.Endpoint).Errorf("build request: %w", err)
		return
	}
	if inst.User != "" {
		req.Header.Set("X-ClickHouse-User", inst.User)
	}
	if inst.Password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.Password)
	}

	resp, err := inst.HTTP.Do(req)
	if err != nil {
		err = eb.Build().Str("endpoint", inst.Endpoint).Errorf("query: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		err = eb.Build().Errorf("read response: %w", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Int("status", resp.StatusCode).Str("sql", truncate(sql, 600)).
			Errorf("clickhouse rejected the query: %s", truncate(string(body), 400))
		return
	}

	text := strings.TrimRight(string(body), "\n")
	if text == "" {
		return
	}
	rows = strings.Split(text, "\n")
	return
}

func truncate(s string, n int) (out string) {
	out = s
	if len(out) > n {
		out = out[:n] + "…"
	}
	return
}

// Channel names the series this study extracts. Each is either a single
// ClickHouse asynchronous metric or a sum over a family of per-device ones,
// which is what keeps the set portable across hosts: device and interface names
// differ per machine, the aggregate does not.
type Channel struct {
	Name string
	// Metric selects one metric exactly; Prefix sums every metric starting with
	// it, across devices, before binning. Exactly one is set.
	Metric string
	Prefix string
	// MaxPlausible discards samples at or above it, zero meaning no bound.
	//
	// ClickHouse derives the per-device byte counters as deltas, and a delta
	// underflows when an interface disappears — this host produced a
	// NetworkReceiveBytes reading of 2^64 minus a hundred thousand. Such a value
	// is not a large measurement, it is a wrapped one, and a detector handed it
	// will report the wrap as the most anomalous event in the series. The bound
	// belongs far above any real hardware and far below the wrap.
	MaxPlausible float64
}

// bytesPerSecondCeiling is the plausibility bound for the byte-rate channels:
// one terabyte per second, orders above any device this will meet and fifteen
// orders below an underflowed counter.
const bytesPerSecondCeiling = 1.0e12

// DefaultChannels is the load surface this study reads: CPU split three ways,
// run-queue depth, resident memory, block IO in both directions, and inbound
// network.
var DefaultChannels = []Channel{
	{Name: "cpu_user", Metric: "OSUserTimeNormalized"},
	{Name: "cpu_system", Metric: "OSSystemTimeNormalized"},
	{Name: "cpu_iowait", Metric: "OSIOWaitTimeNormalized"},
	{Name: "load1", Metric: "LoadAverage1"},
	{Name: "mem_resident", Metric: "MemoryResident"},
	{Name: "block_read", Prefix: "BlockReadBytes_", MaxPlausible: bytesPerSecondCeiling},
	{Name: "block_write", Prefix: "BlockWriteBytes_", MaxPlausible: bytesPerSecondCeiling},
	{Name: "net_rx", Prefix: "NetworkReceiveBytes_", MaxPlausible: bytesPerSecondCeiling},
}

// Spec parameterizes an extraction.
type Spec struct {
	// From and To bound the study window. To of zero means now.
	From time.Time
	To   time.Time
	// StepSeconds is the bin width the irregular 1 Hz samples are averaged onto.
	StepSeconds int32
	Channels    []Channel
	// EventKinds are the boxer.facts symbol values that count as events. Empty
	// accepts [DefaultEventKinds].
	EventKinds []string
}

// DefaultEventKinds are the fact symbols that mark a change in what the machine
// was being asked to do. Heartbeats are deliberately excluded: they fire on a
// timer rather than on anything happening.
var DefaultEventKinds = []string{"app-lifecycle", "runtime-run", "started", "stopped"}

// factsTimestampColumn is the leeway-generated timestamp column of boxer.facts.
//
// The name is an encoded column descriptor, not something a human chose, and it
// moves if the schema is regenerated. Hard-coding it is acceptable for a study
// and would not be for a product path.
const factsTimestampColumn = "`ts:ts:z64:2k:0:0:`"

// factsSymbolColumn holds each row's symbol values, which is where the fact kind
// lives.
const factsSymbolColumn = "`tv:symbol:value:val:s:m:0:24:0::data`"

// Series is the extracted study data: a regular time grid, one value slice per
// channel, the binned event count, and the bins an event fell into.
type Series struct {
	// Start is the timestamp of bin 0; Step is the bin width.
	Start time.Time
	Step  time.Duration

	// Names indexes Values: Values[i] is the series for Names[i].
	Names  []string
	Values [][]float64

	// EventRate counts events per bin — a channel in its own right, and the one
	// place the cause side of the correlation is visible.
	EventRate []float64

	// EventBins marks bins that contained at least one event.
	EventBins []bool

	// Gaps counts grid bins no sample landed in, worst channel. They are
	// forward-filled, so a large count means the series is partly fabricated and
	// the study should say so rather than quietly average over it.
	Gaps int32

	// ChannelGaps is the same count per channel, aligned with Names. Metric
	// families are not sampled on a common schedule, so a grid that is gap-free
	// for CPU can still be sparse for block IO.
	ChannelGaps []int32

	// Rejected counts samples dropped by a channel's plausibility bound.
	Rejected int32
}

// Span is a stretch of the metric grid with no missing bin, together with how
// many events fell inside it.
//
// Studying spans rather than a fixed window is not tidiness. This host records
// intermittently, so a fixed window is mostly forward-filled — invented — and a
// detector run over invented data reports on the invention.
type Span struct {
	From   time.Time
	To     time.Time
	Bins   int32
	Events int32
}

// FindSpansE returns the gap-free spans of the metric grid over the lookback,
// longest first, keeping those with at least minBins bins and minEvents events.
func FindSpansE(ctx context.Context, client *Client, lookback time.Duration, step int32, minBins int32, minEvents int32, kinds []string) (spans []Span, err error) {
	if step < 1 {
		err = eb.Build().Int32("step", step).Errorf("step must be at least one second")
		return
	}
	if len(kinds) == 0 {
		kinds = DefaultEventKinds
	}
	from := time.Now().UTC().Add(-lookback)

	rows, err := client.QueryTSVE(ctx, spanSQL(from, step, minBins))
	if err != nil {
		return
	}
	stamps, err := eventStampsE(ctx, client, from, kinds)
	if err != nil {
		return
	}

	stepDur := time.Duration(step) * time.Second
	for _, row := range rows {
		fields := strings.Split(row, "\t")
		if len(fields) != 3 {
			continue
		}
		start, parseErr := time.ParseInLocation("2006-01-02 15:04:05", fields[0], time.UTC)
		if parseErr != nil {
			continue
		}
		last, parseErr := time.ParseInLocation("2006-01-02 15:04:05", fields[1], time.UTC)
		if parseErr != nil {
			continue
		}
		bins, convErr := strconv.ParseInt(fields[2], 10, 32)
		if convErr != nil {
			continue
		}
		// The span ends one step past its last covered bin.
		end := last.Add(stepDur)

		var events int32
		for _, at := range stamps {
			if !at.Before(start) && at.Before(end) {
				events++
			}
		}
		if events < minEvents {
			continue
		}
		spans = append(spans, Span{From: start, To: end, Bins: int32(bins), Events: events})
	}
	sort.SliceStable(spans, func(a int, b int) bool { return spans[a].Bins > spans[b].Bins })
	return
}

// spanSQL finds maximal runs of consecutive covered bins.
//
// Two things here are deliberate. Runs are delimited with lagInFrame and a
// running sum over an explicit window frame, rather than the shorter
// rowNumberInAllBlocks trick: that function does not respect a subquery's ORDER
// BY once ClickHouse splits the scan across blocks, and it silently reported
// runs four times longer than the data supports.
//
// Every timestamp is rendered through toDateTime(_, 'UTC'). ClickHouse renders
// a DateTime in the *server's* timezone, and this server runs on Europe/Zurich,
// so a value read back and re-parsed as UTC lands two hours away — which is
// exactly far enough to select a neighbouring stretch of real data and look
// plausible.
func spanSQL(from time.Time, step int32, minBins int32) (sql string) {
	sql = fmt.Sprintf(`WITH covered AS (
  SELECT toStartOfInterval(event_time, INTERVAL %d SECOND) AS b
  FROM system.asynchronous_metric_log
  WHERE metric = 'OSUserTimeNormalized' AND event_time >= %s
  GROUP BY b
),
marked AS (
  SELECT b,
         if(toUInt32(b) - lagInFrame(toUInt32(b), 1, toUInt32(b))
              OVER (ORDER BY b ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) > %d, 1, 0) AS isNew
  FROM covered
),
grouped AS (
  SELECT b, sum(isNew) OVER (ORDER BY b ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS grp
  FROM marked
)
SELECT toString(toDateTime(min(b), 'UTC')), toString(toDateTime(max(b), 'UTC')), toString(count())
FROM grouped GROUP BY grp HAVING count() >= %d ORDER BY count() DESC
FORMAT TSV`, step, chTime(from), step, minBins)
	return
}

// eventStampsE returns every event timestamp since from.
func eventStampsE(ctx context.Context, client *Client, from time.Time, kinds []string) (stamps []time.Time, err error) {
	quoted := make([]string, 0, len(kinds))
	for _, k := range kinds {
		quoted = append(quoted, quote(k))
	}
	sql := fmt.Sprintf(`SELECT toString(toDateTime(%s, 'UTC')) FROM boxer.facts
WHERE %s >= %s AND hasAny(%s, [%s]) ORDER BY 1 FORMAT TSV`,
		factsTimestampColumn, factsTimestampColumn, chTime64(from),
		factsSymbolColumn, strings.Join(quoted, ", "))

	rows, err := client.QueryTSVE(ctx, sql)
	if err != nil {
		return
	}
	for _, row := range rows {
		at, parseErr := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(row), time.UTC)
		if parseErr != nil {
			continue
		}
		stamps = append(stamps, at)
	}
	return
}

// Len returns the number of bins.
func (inst *Series) Len() (n int32) {
	n = int32(len(inst.EventRate))
	return
}

// ExtractE pulls the study series out of ClickHouse.
//
// Metric values are averaged within each bin. Per-device families are summed
// across devices at each source timestamp *before* averaging, so a machine with
// three disks and one with one produce comparable numbers.
func ExtractE(ctx context.Context, client *Client, spec Spec) (inst *Series, err error) {
	if spec.StepSeconds < 1 {
		err = eb.Build().Int32("stepSeconds", spec.StepSeconds).Errorf("step must be at least one second")
		return
	}
	if spec.From.IsZero() {
		err = eb.Build().Errorf("study window has no start")
		return
	}
	to := spec.To
	if to.IsZero() {
		to = time.Now()
	}
	if !to.After(spec.From) {
		err = eb.Build().Errorf("study window ends before it starts")
		return
	}
	channels := spec.Channels
	if len(channels) == 0 {
		channels = DefaultChannels
	}
	kinds := spec.EventKinds
	if len(kinds) == 0 {
		kinds = DefaultEventKinds
	}

	step := time.Duration(spec.StepSeconds) * time.Second
	start := spec.From.UTC().Truncate(step)
	bins := int32(to.UTC().Sub(start) / step)
	if bins < 2 {
		err = eb.Build().Int32("bins", bins).Errorf("study window holds fewer than two bins")
		return
	}

	inst = &Series{
		Start:       start,
		Step:        step,
		Names:       make([]string, 0, len(channels)),
		Values:      make([][]float64, 0, len(channels)),
		ChannelGaps: make([]int32, 0, len(channels)),
		EventRate:   make([]float64, bins),
		EventBins:   make([]bool, bins),
	}

	metricRows, err := client.QueryTSVE(ctx, metricSQL(channels, start, to, spec.StepSeconds))
	if err != nil {
		return
	}
	byChannel := make(map[string][]float64, len(channels))
	seen := make(map[string][]bool, len(channels))
	bounds := make(map[string]float64, len(channels))
	for _, c := range channels {
		byChannel[c.Name] = make([]float64, bins)
		seen[c.Name] = make([]bool, bins)
		bounds[c.Name] = c.MaxPlausible
	}
	for _, row := range metricRows {
		fields := strings.Split(row, "\t")
		if len(fields) != 3 {
			continue
		}
		idx, ok := binIndex(fields[0], start, step, bins)
		if !ok {
			continue
		}
		values, known := byChannel[fields[1]]
		if !known {
			continue
		}
		v, convErr := strconv.ParseFloat(fields[2], 64)
		if convErr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if bound := bounds[fields[1]]; bound > 0.0 && v >= bound {
			// Leave the bin unseen so it is forward-filled rather than poisoned.
			inst.Rejected++
			continue
		}
		values[idx] = v
		seen[fields[1]][idx] = true
	}

	var worstGaps int32
	for _, c := range channels {
		gaps := forwardFill(byChannel[c.Name], seen[c.Name])
		if gaps > worstGaps {
			worstGaps = gaps
		}
		inst.Names = append(inst.Names, c.Name)
		inst.Values = append(inst.Values, byChannel[c.Name])
		inst.ChannelGaps = append(inst.ChannelGaps, gaps)
	}
	inst.Gaps = worstGaps

	eventRows, err := client.QueryTSVE(ctx, eventSQL(kinds, start, to, spec.StepSeconds))
	if err != nil {
		return
	}
	for _, row := range eventRows {
		fields := strings.Split(row, "\t")
		if len(fields) != 2 {
			continue
		}
		idx, ok := binIndex(fields[0], start, step, bins)
		if !ok {
			continue
		}
		count, convErr := strconv.ParseFloat(fields[1], 64)
		if convErr != nil {
			continue
		}
		inst.EventRate[idx] = count
		inst.EventBins[idx] = count > 0
	}
	return
}

// binIndex maps a ClickHouse datetime string onto the grid.
func binIndex(stamp string, start time.Time, step time.Duration, bins int32) (idx int32, ok bool) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, time.UTC)
	if err != nil {
		return
	}
	offset := t.Sub(start)
	if offset < 0 {
		return
	}
	i := int32(offset / step)
	if i >= bins {
		return
	}
	idx = i
	ok = true
	return
}

// forwardFill carries the last observed value across bins nothing landed in and
// returns how many it had to invent. Leading bins before the first observation
// take that observation.
func forwardFill(values []float64, seen []bool) (gaps int32) {
	first := -1
	for i, ok := range seen {
		if ok {
			first = i
			break
		}
	}
	if first < 0 {
		gaps = int32(len(values))
		return
	}
	for i := range first {
		values[i] = values[first]
		gaps++
	}
	last := values[first]
	for i := first; i < len(values); i++ {
		if seen[i] {
			last = values[i]
			continue
		}
		values[i] = last
		gaps++
	}
	return
}

func metricSQL(channels []Channel, from time.Time, to time.Time, step int32) (sql string) {
	var cases strings.Builder
	var filter strings.Builder
	cases.WriteString("multiIf(")
	for i, c := range channels {
		if i > 0 {
			filter.WriteString(" OR ")
		}
		if c.Prefix != "" {
			fmt.Fprintf(&cases, "startsWith(metric, %s), %s, ", quote(c.Prefix), quote(c.Name))
			fmt.Fprintf(&filter, "startsWith(metric, %s)", quote(c.Prefix))
			continue
		}
		fmt.Fprintf(&cases, "metric = %s, %s, ", quote(c.Metric), quote(c.Name))
		fmt.Fprintf(&filter, "metric = %s", quote(c.Metric))
	}
	cases.WriteString("'')")

	// Two levels: sum a per-device family within one source timestamp, then
	// average those over the bin. Averaging first would divide a machine's total
	// IO by its device count.
	sql = fmt.Sprintf(`SELECT toString(toDateTime(bin, 'UTC')) AS b, ch, toString(avg(v)) AS value
FROM (
  SELECT toStartOfInterval(event_time, INTERVAL %d SECOND) AS bin,
         event_time AS et,
         %s AS ch,
         sum(value) AS v
  FROM system.asynchronous_metric_log
  WHERE event_time >= %s AND event_time < %s AND (%s)
  GROUP BY bin, et, ch
  HAVING ch != ''
)
GROUP BY b, ch
ORDER BY b, ch
FORMAT TSV`, step, cases.String(), chTime(from), chTime(to), filter.String())
	return
}

func eventSQL(kinds []string, from time.Time, to time.Time, step int32) (sql string) {
	quoted := make([]string, 0, len(kinds))
	for _, k := range kinds {
		quoted = append(quoted, quote(k))
	}
	sql = fmt.Sprintf(`SELECT toString(toDateTime(toStartOfInterval(%s, INTERVAL %d SECOND), 'UTC')) AS b, toString(count()) AS c
FROM boxer.facts
WHERE %s >= %s AND %s < %s
  AND hasAny(%s, [%s])
GROUP BY b
ORDER BY b
FORMAT TSV`,
		factsTimestampColumn, step,
		factsTimestampColumn, chTime64(from), factsTimestampColumn, chTime64(to),
		factsSymbolColumn, strings.Join(quoted, ", "))
	return
}

func chTime(t time.Time) (lit string) {
	lit = "toDateTime(" + quote(t.UTC().Format("2006-01-02 15:04:05")) + ", 'UTC')"
	return
}

func chTime64(t time.Time) (lit string) {
	lit = "toDateTime64(" + quote(t.UTC().Format("2006-01-02 15:04:05")) + ", 9, 'UTC')"
	return
}

// quote renders a SQL string literal. Every value reaching it is a metric name
// or fact kind chosen in Go, never user input, but escaping it costs nothing and
// removes the question.
func quote(s string) (lit string) {
	lit = "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
	return
}

// EventLabels turns the bins that held an event into a label vector, widened by
// tolerance bins on each side.
//
// The widening is not slack. An app start changes what the machine is doing over
// the seconds that follow, not in the bin the log line landed in, so a detector
// that fires shortly after is agreeing with the event rather than missing it.
// The tolerance is a judgement about that lag and is the study's most arguable
// parameter — vary it and see.
func EventLabels(series *Series, tolerance int32) (labels []bool) {
	n := series.Len()
	labels = make([]bool, n)
	for i, hit := range series.EventBins {
		if !hit {
			continue
		}
		lo := int32(i) - tolerance
		if lo < 0 {
			lo = 0
		}
		hi := int32(i) + tolerance
		if hi >= n {
			hi = n - 1
		}
		for j := lo; j <= hi; j++ {
			labels[j] = true
		}
	}
	return
}

// LabelledFraction returns the share of bins the labels cover, which is the
// prevalence any precision figure has to be read against.
func LabelledFraction(labels []bool) (frac float64) {
	var count int
	for _, l := range labels {
		if l {
			count++
		}
	}
	frac = float64(count) / float64(len(labels))
	return
}

// Summarize describes a channel's distribution, for reporting what the detector
// was actually handed.
type Summarize struct {
	Min      float64
	Max      float64
	Mean     float64
	StdDev   float64
	Constant bool
}

// Describe summarizes one channel.
func Describe(values []float64) (out Summarize) {
	if len(values) == 0 {
		return
	}
	out.Min = math.Inf(1)
	out.Max = math.Inf(-1)
	for _, v := range values {
		if v < out.Min {
			out.Min = v
		}
		if v > out.Max {
			out.Max = v
		}
		out.Mean += v
	}
	out.Mean /= float64(len(values))
	var ss float64
	for _, v := range values {
		d := v - out.Mean
		ss += d * d
	}
	out.StdDev = math.Sqrt(ss / float64(len(values)))
	out.Constant = out.StdDev == 0.0
	return
}

// SortedNames returns the channel names in a stable order, so a report reads the
// same way twice.
func (inst *Series) SortedNames() (names []string) {
	names = append(names, inst.Names...)
	sort.Strings(names)
	return
}

// Channel returns a channel's values by name.
func (inst *Series) Channel(name string) (values []float64, ok bool) {
	for i, n := range inst.Names {
		if n == name {
			values = inst.Values[i]
			ok = true
			return
		}
	}
	return
}

// GapsFor returns how many bins were forward-filled for one channel.
func (inst *Series) GapsFor(name string) (gaps int32, ok bool) {
	for i, n := range inst.Names {
		if n == name {
			gaps = inst.ChannelGaps[i]
			ok = true
			return
		}
	}
	return
}
